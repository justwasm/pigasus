# Cycle `rc-conversation-config` — `UpsertRemoteAgentConversationConfig` per-conversation override

## TL;DR / 摘要

Cycles 1–25 of `exp/rc-server-mode` shipped 25 JSON-RPC methods. The
re-investigation matrix still flags `rc-conversation-config` as **TODO**: a
per-conversation config override surface that lets a dashboard pin model /
effort / system prompt patch / allowed-tools / disallowed-tools / max-turns /
temperature / top-p / metadata for a single conversation thread. This cycle
ships the **write side** of that surface:

- A single `UpsertRemoteAgentConversationConfig` JSON-RPC method that lets a
  dashboard upsert (insert-or-replace) a per-conversation config row and
  persists it to `$ANTIGRAVITY_HOME/conversation_config.json`;
- A `conversation_config/changed` server-push notification broadcast (via the
  existing `(*Dispatcher).BroadcastNotify` seam added in
  `rc-permissions-grants` and shared with `rc-plugins` / `rc-triggers` /
  `rc-fastpush-toggle` / `rc-resources`) fires on every successful Upsert, so
  a dashboard that subscribes to the channel re-renders without polling and
  dashboards on other conns stay in sync with each other;
- Closed-set / bounded-range validation
  (`model_effort ∈ {low, medium, high}` / `max_turns ∈ [0, 1000]` /
  `temperature ∈ [0, 2]` / `top_p ∈ [0, 1]`) short-circuits bad inputs before
  any on-disk write so a dashboard with malformed input never triggers an
  atomic-save cycle.

The cycle is **write-only** — the dispatcher's `Upsert` path is the only
mutator; there is no `GetRemoteAgentConversationConfig` /
`ListRemoteAgentConversationConfigs` JSON-RPC method today (the response
envelope echoes the post-state so polling is always possible). The future
`rc-conversation-config-read` cycle adds the corresponding read methods so
dashboards can render the post-upsert state without their own copy. Documented
in the **硬約束** section.

| Method | Direction | Params | Result | Errors |
|---|---|---|---|---|
| `UpsertRemoteAgentConversationConfig` | write | `{conversation_id, config:{model?, model_effort?, system_prompt_patch?, allowed_tools?, disallowed_tools?, max_turns?, temperature?, top_p?, metadata?}}` | `{ok:true, conversation_id, created, config:{…full echo with server-stamped updated_at…}, updated_at}` | `-32602` (empty / missing `conversation_id`; missing / null / wrong-type `config`; `ValidateConversationConfig` failure: out-of-set `model_effort` / out-of-range `max_turns` / `temperature` / `top_p`), `-32603` (no store / persistence failure) |

Every successful Upsert emits a **`conversation_config/changed`** notification
to **every** open ws connection (the same broadcast-to-all shape
`rc-permissions-grants` introduced for `permissions/changed`,
`rc-plugins` reused for `plugins/changed`, `rc-triggers` reused for
`triggers/changed`, `rc-fastpush-toggle` reused for
`fastpush/setting_changed`, `rc-resources` reused for
`resources/changed`).

| Piece | Where |
|---|---|
| `ConversationConfig` per-row record (`model` / `model_effort` / `system_prompt_patch` / `allowed_tools` / `disallowed_tools` / `max_turns` / `temperature` / `top_p` / `metadata` / `updated_at`) + `ConversationConfigState` single-document envelope (`version:1` / `configs:{<id>: ConversationConfig}` / `updated_at`) + `ConversationConfigStore` interface (`Upsert(ctx, convID, cfg) → (created, err)` / `Get(ctx, convID) → (ConversationConfig, bool)` / `Snapshot(ctx) → (ConversationConfigState, error)`) + `ErrConversationConfigPersistenceFailed` sentinel (the dispatcher maps to `-32603`) + `ConversationConfigStateVersion = 1` + `NotifConversationConfigChangedMethod = "conversation_config/changed"` + `AllModelEfforts` closed-set (canonical order: low / medium / high) + `IsValidModelEffort` validator + `MaxConversationConfigMaxTurns = 1000` / `MaxConversationConfigTemperature = 2.0` / `MaxConversationConfigTopP = 1.0` bounded-range constants + `normaliseConversationID` (whitespace-trim) + `ValidateConversationConfig` (closed-set + bounded-range walk; `{"must be one of low\|medium\|high"}` / `{"must be >= 0"}` / `{"must be <= N"}` verbatim messages) + `conversationConfigChangedToWire` (per-row wire projection — byte-for-byte identical to the broadcast notification) + `mapConversationConfigError` (sentinel → JSON-RPC envelope) | `internal/remotecontrol/conversation_config.go` (new, ~470 lines) |
| `FSConversationConfigStore` (production JSON-file store rooted at `$ANTIGRAVITY_HOME/conversation_config.json`; atomic temp + rename + dir fsync + 0o600 perms + 0o700 dir; injectable `now` for tests; **`sync.Once`-guarded `ensureParentDir`** so concurrent first-use calls don't race on the MkdirAll; `load()` enforces `version ≤ 1` (refuses a `version > 1` file with the parse-error envelope so a future binary's file isn't silently downgraded); `Snapshot` / `Get` take the read lock so concurrent reads don't race a writer's in-progress mutation; `Upsert` stamps `updated_at` server-side so a dashboard that posts a pre-populated value gets it overwritten with the server's clock — matches the `rc-fastpush-toggle` / `rc-resources` server-side stamping precedent) + `MemConversationConfigStore` (in-memory test fixture; `now` injectable; `Get` / `Snapshot` deep-copy the returned slice / map so a caller can't observe in-place mutations) + `NewMemConversationConfigStore` (empty + now) + `NewMemConversationConfigStoreWith` (custom-snapshot constructor with deep-clone of each `ConversationConfig` row so a test that mutates the input doesn't race the store's internal state) + `onDiskConversationConfigState` struct + `load` / `atomicWrite` / `ensureParentDir` + `emptyState` + `cloneConversationConfigState` defensive copy (deep-clones `Configs` map and each `ConversationConfig` row's `AllowedTools` / `DisallowedTools` / `Metadata`) + `cloneConversationConfig` defensive copy (used by `Get` and `Snapshot`) + `sortConversationIDs` helper (parity with the `sortResourcesByID` / `sortSettingsByCascadeID` precedent, parked for the future read cycle) | `internal/remotecontrol/conversation_config_store.go` (new, ~530 lines) |
| `upsertRemoteAgentConversationConfigParams` envelope type + `handleUpsertRemoteAgentConversationConfig` handler (param-shape check first, then `ValidateConversationConfig`, then store mutation, then `BroadcastNotify` BEFORE returning the response — the originating conn's outbound channel holds `[notification, response]`) + `nonNullObjectInJSON` helper (enforces the brief's "config is a required JSON object — not `null`, not absent" rule; rejects `{"conversation_id":"..."}` without a `config` key as well as `{"conversation_id":"...", "config":null}` before any validation runs) | `internal/remotecontrol/conversation_config_dispatcher.go` (new, ~260 lines) |
| `case "UpsertRemoteAgentConversationConfig":` switch arm + `NewDispatcher` nil-fabrication (fabricates a fresh empty `MemConversationConfigStore` when `cfg.ConversationConfigStore == nil` — mirrors the `PermissionsStore` / `PluginsStore` / `TriggersStore` / `FastpushStore` / `ResourceStore` nil-guard); package-level comment on the `Dispatch` switch updated to document the rc-conversation-config cycle | `internal/remotecontrol/dispatch.go` (+~40 / -0) |
| `Config.ConversationConfigStore ConversationConfigStore` + `Config.ConversationConfigPath string` (default `auth.AntigravityHome()/conversation_config.json`); `HostConfig.ConversationConfigStore` / `HostConfig.ConversationConfigPath` mirror; propagation through `StartHostSession` | `internal/remotecontrol/daemon.go` (+~50 / -0), `internal/remotecontrol/host.go` (+~45 / -0) |
| `buildHostConversationConfigStore()` helper at the bottom of `cmd/wagyu/remote_control.go` (mirrors `buildHostResourceStore` / `buildHostFastpushStore` — reads `auth.AntigravityHome()` for the path; `nil` `now` → `time.Now(UTC)`); wired into Unix-socket (`maybeStartRemoteControlDaemon`), TCP (`maybeStartRemoteControlTCPDaemon`), and foreground (`runRemoteControlForeground`) paths as `ConversationConfigStore: buildHostConversationConfigStore()`. All three transports share the same `$ANTIGRAVITY_HOME/conversation_config.json` file so the Upsert path is visible across transports. The Unix-socket path's `BroadcastNotify` is a no-op (the WS transport owns the broadcaster registration, matching the cycle-13 / cycle-19 / cycle-20 / cycle-21 / cycle-24 / cycle-25 seam contract); the mutation still lands in the JSON; the response envelope echoes the post-state so polling is always possible. | `cmd/wagyu/remote_control.go` (+~50 / -0), `cmd/wagyu/remote_control_foreground.go` (+~20 / -0) |
| In-process tests: `TestNormaliseConversationID` × 4 sub-tests + `TestAllModelEfforts` (canonical-order pin) + `TestIsValidModelEffort` (every closed-set value + a dozen non-matches) + `TestNotifConversationConfigChangedMethod` (exact literal `"conversation_config/changed"` pinned so a typo fails the build) + `TestConversationConfigStateVersion` (= 1) + `TestMaxConversationConfigBounds` (1000 / 2.0 / 1.0) + 6 `TestValidateConversationConfig_*` (happy path / all-empty-ok / bad-model-effort × 3 non-set values / bad-max-turns × 3 (negative + above 1000 + exactly 1001) / bad-temperature × 3 (below 0 + above 2 + 2.5) / bad-top-p × 3 / bounds-inclusive (0 → ok, 1000 → ok, 2.0 → ok, 1.0 → ok)) + 6 store round-trips (`TestConversationConfigStore_DefaultState_Empty` / `_Upsert_New` / `_Upsert_Update` (created=false on second call) / `_Get_Missing` / `_EmptyConvID` (returns `ErrConversationConfigPersistenceFailed` — the store's defence-in-depth guard) / `_Snapshot_DeepCopy`) + `TestMemConversationConfigStoreWith_DeepCopy` + 5 `TestFSConversationConfigStore_*` (round-trip / fresh-host (no file → empty state) / atomic-save (parallel `Upsert` calls — single mutex serialises) / version-bump (refuses `version > 1` with the parse-error envelope) / malformed (bad JSON → error envelope rather than silent fallback to empty state — operator sees "conversation_config.json is corrupt" on the wire)) + 12 `TestUpsertRemoteAgentConversationConfig_*` dispatcher round-trips (happy path / `_Update` created=false / `_BadModelEffort` (-32602 + verbatim `"model_effort must be one of low|medium|high"`) / `_BadTemperature` / `_BadMaxTurns` / `_BadParams_EmptyConvID` / `_BadParams_MissingConvID` / `_BadParams_MissingConfig` (rejects `{"conversation_id":"..."}` without a `config` key) / `_BadParams_NullConfig` (rejects `{"conversation_id":"...", "config":null}`) / `_BadParams_BadConvIDType` (numeric `conversation_id` → -32602, not silent default-walk) / `_NoStore` (-32603 when `cfg.ConversationConfigStore == nil`)) + `TestConversationConfigChangedToWire_Pins` (envelope shape pin: `ok` / `conversation_id` / `created` / `config` / `updated_at` and the verbatim RFC3339Nano) + `TestMapConversationConfigError` (nil → nil envelope; `ErrConversationConfigPersistenceFailed` → -32603; arbitrary error → -32603) = **~45 tests** | `internal/remotecontrol/conversation_config_test.go` (new, ~1170 lines) |
| Wire-level WS round-trip tests + `newTestTCPServerWithConversationConfig` / `assertConversationConfigChangedEnvelope` / `assertConversationConfigChangedNotification` helpers (5 tests: `TestUpsertRemoteAgentConversationConfig_WS_Ok` (created=true on first call → created=false on second call, asserts that the inner `config` JSON object decodes to `map[string]any` with snake_case keys post wire round-trip) / `TestUpsertRemoteAgentConversationConfig_WS_BadModelEffort` (-32602 + `data:"model_effort must be one of low\|medium\|high"`) / `TestUpsertRemoteAgentConversationConfig_WS_BadTemperature` (-32602) / `TestUpsertRemoteAgentConversationConfig_WS_BadParams_EmptyConvID` (-32602) / `TestUpsertRemoteAgentConversationConfig_Broadcasts_Changed` (two ws conns use `waitForTwoConnIDs` to confirm both receive the `conversation_config/changed` broadcast — a buggy broadcaster that only writes to the originating conn trips the 2-second deadline)) | `internal/remotetransport/tcp_test.go` (+~340 / -0) |

What lands for forward consumers:

- A `ConversationConfigStore` seam the dispatcher carries, ready for the future
  `rc-conversation-config-read` cycle to add `GetRemoteAgentConversationConfig`
  / `ListRemoteAgentConversationConfigs` / `DeleteRemoteAgentConversationConfig`
  JSON-RPC methods without re-touching the dispatcher;
- An `FSConversationConfigStore` JSON-file layout that matches the cycle-9
  `FSProjectStore` / cycle-19 `FSPermissionStore` / cycle-20 `FSPluginStore` /
  cycle-21 `FSTriggerStore` / cycle-24 `FSFastpushStore` / cycle-25
  `FSResourceStore` style (atomic temp + rename + dir fsync, 0600 perms,
  0700 dir);
- A `MemConversationConfigStore` in-memory fixture tests inject without a
  tempdir;
- A `MemConversationConfigStore.With(custom snapshot)` constructor that
  deep-clones each `ConversationConfig` row so a test that mutates the
  returned state's `Configs` map doesn't race the store's internal state;
- A `sortConversationIDs` helper (parked, parity with the
  `sortResourcesByID` / `sortSettingsByCascadeID` precedent) the future read
  cycle consumes without re-touching this file;
- The `ValidateConversationConfig` validator's closed-set + bounded-range
  walk is a single function re-usable by a future CLI flag-parser or script
  without instantiating a `ConversationConfig`;
- Reuse of the `BroadcastNotify` / `SetBroadcaster` callback seam from
  `rc-permissions-grants` (shared with `rc-plugins` / `rc-triggers` /
  `rc-fastpush-toggle` / `rc-resources`) — future cycles that fan their own
  "X changed" notifications (`rc-terminals`, etc.) share the same plumbing.

---

## JSON-RPC surface (precise)

### `UpsertRemoteAgentConversationConfig`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "UpsertRemoteAgentConversationConfig", "params": { "conversation_id": "conv_abc123def456", "config": { "model": "claude-3-7-sonnet", "model_effort": "medium", "max_turns": 50, "temperature": 0.7, "top_p": 0.9, "metadata": { "dashboard_tab": "qa" } } } }
```

`conversation_id` is **required**, non-empty after trim. Whitespace-only
values are trimmed and rejected as `-32602` (matches the
`cancel-cascade` / `permissions-grants` / `resources` validation rule).
Missing / non-string values are rejected by `nonStringFieldInJSON` so a
numeric `conversation_id` doesn't silently default-walk to `""`.

`config` is **required, non-null JSON object**. The brief pins it as a JSON
object — `nonNullObjectInJSON` rejects three shapes:

| Param | Reason |
|---|---|
| missing `config` key entirely (`{"conversation_id":"..."}`) | dashboard forgot to fill in the field |
| explicit `null` (`{"conversation_id":"...", "config":null}`) | brief disallows null |
| wrong-type primitive (`{"conversation_id":"...", "config":"claude"}` or `"config":42`) | brief requires a JSON object |

All three reject as `-32602 "Invalid params"` BEFORE any field-level
validation runs. Server-stamped `updated_at` overwrites any dashboard-supplied
value (matches the `rc-fastpush-toggle` / `rc-resources` server-side stamping
precedent).

Per-field validation chain (`ValidateConversationConfig`):

- `model_effort`: empty → ok ("no override"). Non-empty → must be in
  `AllModelEfforts` (`low` / `medium` / `high`); unknown value → -32602 with
  verbatim `"model_effort must be one of low|medium|high"`.
- `max_turns`: 0 → ok. Negative or > 1000 → -32602 with verbatim
  `"max_turns must be >= 0"` or `"max_turns must be <= 1000"`.
- `temperature`: 0 → ok. Negative or > 2 → -32602 with verbatim
  `"temperature must be >= 0"` or `"temperature must be <= 2"`.
- `top_p`: 0 → ok. Negative or > 1 → -32602 with verbatim
  `"top_p must be >= 0"` or `"top_p must be <= 1"`.
- `metadata`: nil / empty → ok. Keys and values are opaque strings; the cycle
  does not validate specific keys (a future cycle that adds a typed-key
  closed-set can extend `ValidateConversationConfig` without re-touching the
  dispatcher).
- `system_prompt_patch` / `allowed_tools` / `disallowed_tools` / `model`:
  free-form / opaque today (the brief pins no validation; the future
  read cycle can extend the validator with a typed closed-set).

Response (success, post-mutation state):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok":              true,
    "conversation_id": "conv_abc123def456",
    "created":         true,
    "config": {
      "model":               "claude-3-7-sonnet",
      "model_effort":        "medium",
      "system_prompt_patch": "",
      "allowed_tools":       null,
      "disallowed_tools":    null,
      "max_turns":           50,
      "temperature":         0.7,
      "top_p":               0.9,
      "metadata":            { "dashboard_tab": "qa" },
      "updated_at":          "2026-10-19T12:34:56.789012345Z"
    },
    "updated_at": "2026-10-19T12:34:56.789012345Z"
  }
}
```

`config` is the brief's full `ConversationConfig` echo with the server-stamped
`updated_at` (the same RFC3339Nano string the top-level `updated_at` carries —
the helper fabricates `{}` for `metadata` when the input was nil so the wire
always sees a non-null `metadata` value, although the `omitempty` JSON tag on
the inner field keeps the wire minimal when nothing was supplied). The
top-level `updated_at` rides alongside `config.updated_at` so dashboards that
read only the lean envelope (e.g. a status pill) don't have to walk into
`config`.

`created` is `true` when no prior row existed for `conversation_id` (insert)
and `false` when an existing row was replaced (update) — the brief's
upsert-creates-or-replaces semantic.

Error envelopes:

| Code | Meaning | Trigger |
|---|---|---|
| `-32602` "Invalid params" | `conversation_id` missing / empty / whitespace / non-string; `config` missing / null / wrong-type; `ValidateConversationConfig` failure (out-of-set `model_effort` / out-of-range `max_turns` / `temperature` / `top_p`) | dashboard sent bad JSON |
| `-32603` "Internal error" | `cfg.ConversationConfigStore == nil` (post-construction clear); `ErrConversationConfigPersistenceFailed` (the store's defence-in-depth blank-id guard); I/O error during atomic-save; malformed `conversation_config.json` | transient storage problem |

---

## `conversation_config.json` schema (the on-disk file `$ANTIGRAVITY_HOME/conversation_config.json`)

```json
{
  "version": 1,
  "configs": {
    "conv_abc123def456": {
      "model":               "claude-3-7-sonnet",
      "model_effort":        "medium",
      "system_prompt_patch": "",
      "allowed_tools":       null,
      "disallowed_tools":    null,
      "max_turns":           50,
      "temperature":         0.7,
      "top_p":               0.9,
      "metadata":            { "dashboard_tab": "qa" },
      "updated_at":          "2026-10-19T12:34:56.789012345Z"
    }
  },
  "updated_at": "2026-10-19T12:34:56.789012345Z"
}
```

A fresh host with no `conversation_config.json` returns the empty state
(`version:1, configs:{}`). The file is written atomically (temp + rename +
dir fsync) with `0o600` perms under the `0o700` parent dir
(`$ANTIGRAVITY_HOME/`), matching the cycle-9 `FSProjectStore` /
cycle-19 `FSPermissionStore` / cycle-20 `FSPluginStore` /
cycle-21 `FSTriggerStore` / cycle-24 `FSFastpushStore` /
cycle-25 `FSResourceStore` style.

The `version` field is bumped only on breaking changes (renamed keys,
removed fields); non-breaking additions (new optional fields with
`omitempty`) leave it alone so older readers still parse the file. The
brief pins `v1`; the reader refuses any file with `version > 1` (returns
a `"newer binary required"` envelope that the dispatcher maps to
`-32603`). A missing `version` is coerced to `1` so a hand-edited file
without the version stamp parses cleanly. A `version < 0` or
non-numeric is rejected as malformed JSON and the dispatcher surfaces
the parse error verbatim (operator sees "conversation_config.json is
corrupt" on the wire rather than a silent fallback to empty state —
matches the `rc-permissions-grants` / `rc-triggers` / `rc-resources`
"explicit error on corrupt file" rule).

The on-disk `metadata` shape uses `omitempty` on the inner
`ConversationConfig.Metadata` field, so a config the dashboard posted
without a `metadata` key carries an empty map or omits the key entirely
on a reload (the in-memory helper fabricates `{}` so the wire always
sees a non-null `metadata` value post `Get`).

---

## Broadcast semantics

A successful `UpsertRemoteAgentConversationConfig` emits a
`conversation_config/changed` notification to **every** open ws
connection via `(*Dispatcher).BroadcastNotify(method, params)`:

```json
{
  "jsonrpc": "2.0",
  "method":  "conversation_config/changed",
  "params":  {
    "ok":              true,
    "conversation_id": "conv_abc123def456",
    "created":         true,
    "config": {
      "model":               "claude-3-7-sonnet",
      "model_effort":        "medium",
      "system_prompt_patch": "",
      "allowed_tools":       null,
      "disallowed_tools":    null,
      "max_turns":           50,
      "temperature":         0.7,
      "top_p":               0.9,
      "metadata":            { "dashboard_tab": "qa" },
      "updated_at":          "2026-10-19T12:34:56.789012345Z"
    },
    "updated_at": "2026-10-19T12:34:56.789012345Z"
  }
}
```

The notification's params shape is byte-for-byte identical to the
response envelope the originating dash-caller receives — subscribers
that watch the channel and the originator that polls
`UpsertRemoteAgentConversationConfig` see the same shape. The
`conversationConfigChangedToWire` helper renders both response envelope
and notification params so the two paths can't drift.

The broadcaster is `nil` for in-process dispatchers and the Unix-socket
transport (request/response only); the broadcast becomes a no-op there.
The mutation still lands in the JSON file via `ConversationConfigStore.Upsert`;
the response envelope echoes the post-state so polling is always possible. The
`(*TCPServer)` registered broadcaster (set at `NewTCPServer` time) fans the
frame to every open ws connection.

The originating conn's outbound channel holds
`[notification, response]` — `BroadcastNotify` is called BEFORE the
handler returns the response. The notification is the same
byte-for-byte shape as the response, but delivered without an `"id"`
field (per JSON-RPC 2.0 §4.1 notification rules).

---

## Forward compatibility with `rc-conversation-config-read`

The cycle's write side is the foundation the
`rc-conversation-config-read` cycle will plug read methods into. The seam
is deliberately narrow today (one method, one store, one notification)
so the future cycle can extend it without breaking existing dashboards.

What the future cycle is expected to add (out of scope here):

- A `GetRemoteAgentConversationConfig` JSON-RPC method that returns
  the stored `ConversationConfig` row for a given `conversation_id`;
  the dispatcher calls `(*ConversationConfigStore).Get(ctx, convID)`
  (the cycle's interface already exposes `Get` so the future cycle
  doesn't have to extend the interface);
- A `ListRemoteAgentConversationConfigs` JSON-RPC method that returns the
  full `ConversationConfigState` snapshot (the existing `Snapshot(ctx)`
  method on the store interface; the brief's preferred envelope is
  `{ok:true, configs:{<id>:…}, updated_at}` — the wire helper
  `sortConversationIDs` is parked for parity with the rc-resources /
  rc-triggers / rc-fastpush-toggle precedent so the future cycle only
  has to call the snapshot in sorted order);
- A `DeleteRemoteAgentConversationConfig` JSON-RPC method that removes
  a row (the future cycle's store interface gains a `Delete(ctx,
  convID) → (deleted bool, err error)` method; the cycle ships the
  hook in `conversation_config_store.go`'s doc-comment so the future
  cycle only has to write the method);
- A `ResetRemoteAgentConversationConfig` JSON-RPC method that nukes
  the entire state (the future cycle, parked — most likely never
  lands; the cycle's `Snapshot` interface method is the foundation).

The cycle ships:

- `(*ConversationConfigStore).Upsert(ctx, convID, cfg) → (created, err)`
  — the single write method the future cycle's `ListConfigs` /
  `GetConfig` calls continue to read from;
- `(*ConversationConfigStore).Get(ctx, convID) → (ConversationConfig, bool)`
  — the read primitive the future cycle's `GetConfig` call wraps;
- `(*ConversationConfigStore).Snapshot(ctx) → (ConversationConfigState, error)`
  — the snapshot primitive the future cycle's `ListConfigs` call wraps;
- `NotifConversationConfigChangedMethod = "conversation_config/changed"`
  constant — the future cycle's per-row delete notif
  (`conversation_config/deleted`) is a fresh constant;
- `ValidateConversationConfig` — a single function re-usable by the
  future cycle's read path (`GetConfig` echoes the post-state verbatim
  so a future cycle's read path doesn't have to re-validate);
- `sortConversationIDs` — the sort helper the future cycle's list
  handler consumes without re-touching this file.

The cycle's doc-comments on `conversation_config.go` /
`conversation_config_dispatcher.go` flag every extension point the future
cycle will touch, so the auditor can grep the cycle boundary.

---

## Hard constraints / 硬約束

What the cycle **did not** ship, by deliberate scope decision:

1. **No read methods.** The cycle ships `UpsertRemoteAgentConversationConfig`
   only — there is no `GetRemoteAgentConversationConfig` /
   `ListRemoteAgentConversationConfigs` /
   `DeleteRemoteAgentConversationConfig`. Dashboards that want to read the
   post-upsert state without holding onto their own copy must wait for the
   future `rc-conversation-config-read` cycle. Today's read path is: the
   response envelope echoes the post-state (so polling the response is
   always possible); the `conversation_config/changed` broadcast carries
   the same envelope so subscribers see the same shape; and the
   dispatcher calls `(*ConversationConfigStore).Get(ctx, convID)`
   internally to fetch the post-stamp value (the test seam is exposed
   even though no JSON-RPC method calls it yet — the future cycle only
   has to wire the dispatcher handler, not extend the interface).
2. **No model-id validation.** The brief enumerates the model_id (the
   `model` JSON tag on `ConversationConfig`) as a "free-form string" —
   today's `model` field accepts any non-empty value, including arbitrary
   labels a dashboard might post for a model the host doesn't recognise.
   The future `rc-conversation-config-read` cycle (or a separate
   `rc-conversation-config-validate` cycle) will add a closed-set
   validation against the host's `internal/models` catalog.
3. **No `system_prompt_patch` rendering.** The `system_prompt_patch` JSON
   tag persists; today's clone **does not** render the patch into the
   pipeline's pre-prompt path. A future cycle that wires the
   render-to-prompt path reads the row from the store at every turn's
   system-prompt construction phase. The wire surface is identical;
   the host's runtime behaviour is unchanged.
4. **No `allowed_tools` / `disallowed_tools` enforcement.** Both
   `[]string` slices persist; today's clone **does not** consult them
   when the pipeline renders the tools list. A future cycle that wires
   the tools-policy path consults the row at every turn's tool-list
   construction phase.
5. **No per-conversation subscription.** Every successful Upsert
   broadcasts the same `conversation_config/changed` notification to
   every open ws connection; consumers that want "only conversation
   X" must filter on `params.conversation_id`. A future cycle that
   adds a per-conversation channel suffix
   (`conversation_config/<conv_id>`) only has to extend
   `NotifConversationConfigChangedMethod` and bump
   `ConversationConfigStateVersion`.
6. **The Unix-socket transport's broadcast is a no-op.** Only the
   `(*TCPServer)` registers a broadcaster; the Unix-socket daemon
   doesn't install one. Mutations still land in the store, and the
   response envelope echoes the post-state so polling is always
   possible. This matches the `rc-plugins` / `rc-triggers` /
   `rc-fastpush-toggle` / `rc-resources` precedent: a
   Unix-socket-only deployment that wants the broadcast must either
   switch to the WS transport or accept the polling-only surface.
7. **No on-disk lock-file.** The `FSConversationConfigStore`'s atomic
   write is mutex-serialised within a single process; a concurrent
   second-process writer (e.g. two daemons sharing
   `$ANTIGRAVITY_HOME/conversation_config.json`) would interleave
   read-modify-write cycles with last-writer-wins semantics. The
   production wiring is single-daemon-by-design
   (`AGY_CLI_DISABLE_AUTO_UPDATE` / `~/.local/bin/agy --print` paths
   don't share the file), so the simple-mutex write is correct. A
   future cycle that wants multi-writer semantics adds a file-lock
   guard around `load` / `atomicWrite` (mirrors the
   `flock`-based pattern the upstream agy uses for
   `$ANTIGRAVITY_HOME/auth.json`).
8. **`metadata` values are opaque strings.** A dashboard may post any
   string; today's `ValidateConversationConfig` does not pin a
   typed-key closed-set for `metadata`. A future cycle that wants a
   typed catalog (e.g. `metadata.dashboard_tab ∈ {"qa", "ops",
   "support"}`) extends `ValidateConversationConfig` without
   re-touching the dispatcher.
9. **`max_turns == 0` is "no override"**, not "0 turns". A dashboard
   that wants to pin "exactly 0 turns" (force the pipeline to refuse
   the next user message) must use a different semantic — the cycle
   interprets `0` as "no override" so a dashboard can post a config
   with `max_turns` unset without it silently capping the pipeline at
   0. The future read cycle pins the verbatim contract in the cycle's
   doc-comment on `ConversationConfig.MaxTurns`.
10. **`normaliseConversationID` is applied at the dispatcher, not at
    the store lookup.** A dashboard that posts `"  conv_abc  "` and
    `"conv_abc"` produces two distinct rows today (`normaliseConversationID`
    trims the dispatcher-side input before the store's `Upsert` call,
    so both inputs land as `"conv_abc"` after the dispatcher trim —
    but the store's `load` / `Snapshot` map lookup uses the trimmed
    string verbatim). The store does not auto-normalise on `Get` /
    `Snapshot` iteration. A future cycle that wants server-side
    canonicalisation swaps the lookup line in
    `conversation_config_store.go` for `normaliseConversationID(...)`.
    Today's cycle binds the contract explicitly: the dispatcher
    normalises; the store defends with the same trim; the lookup is
    always against the trimmed key.

The cycle's `docs/cycles/PLAN.md` row now reads **DONE `<this-commit>`**
for `rc-conversation-config`; the FutureMatrix row reduces from "TODO"
to "rc-conversation-config-read" once the verifier's next matrix run
reflects the change.

---

## Reviewer commands

```bash
# Build the cycle's binary (must exit 0).
go build -o /tmp/wagyu ./cmd/wagyu

# Run the in-process + wire-level tests (must PASS).
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# Vet the cycle's touched packages (pre-existing
# model_default_effort_test.go:67 3-arg Errorf is out of scope; the
# cycle does not touch it).
go vet ./internal/remotecontrol/ ./internal/remotetransport/

# Format-check the cycle's touched files (must emit no
# filenames; pre-existing dirt across internal/ is out of scope).
gofmt -l \
    internal/remotecontrol/conversation_config.go \
    internal/remotecontrol/conversation_config_store.go \
    internal/remotecontrol/conversation_config_dispatcher.go \
    internal/remotecontrol/conversation_config_test.go \
    internal/remotecontrol/dispatch.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotetransport/tcp_test.go \
    cmd/wagyu/remote_control.go \
    cmd/wagyu/remote_control_foreground.go

# Run only the cycle's in-process tests.
go test -race -count=1 ./internal/remotecontrol/ \
    -run 'TestNormaliseConversationID|TestAllModelEfforts|TestIsValidModelEffort|TestNotifConversationConfigChangedMethod|TestConversationConfigStateVersion|TestMaxConversationConfigBounds|TestValidateConversationConfig|TestConversationConfigStore_|TestMemConversationConfigStoreWith_|TestFSConversationConfigStore_|TestUpsertRemoteAgentConversationConfig_|TestConversationConfigChangedToWire_Pins|TestMapConversationConfigError'

# Run only the cycle's wire-level tests.
go test -race -count=1 ./internal/remotetransport/ \
    -run 'TestUpsertRemoteAgentConversationConfig|newTestTCPServerWithConversationConfig|assertConversationConfigChanged'
```

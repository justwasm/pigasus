# Cycle `rc-triggers` — five JSON-RPC trigger methods

## TL;DR / 摘要

Cycles 1–21 of `exp/rc-server-mode` landed the transport + JSON-RPC envelope
(rc-ws-transport), the cascade lifecycle surface (rc-cascade-start / -message /
-stream / -cancel), the bi-directional user-interaction RPC
(rc-handle-user-interaction), the four read-only catalog probes
(rc-list-readonly), the two read-only filesystem probes (rc-file-ops), the
three project CRUD methods (rc-projects), the deep-link surface (rc-deep-link),
the four permission-mutation methods (rc-permissions-grants), and the two
plugin methods (rc-plugins). This cycle wires the **five trigger methods** that
close the "triggers cluster" line of the original re-investigation:

| Method | Direction | Params | Result | Errors |
|---|---|---|---|---|
| `ListRemoteAgentTriggers` | read | `{project_id?: "..."}` | `{ok:true, triggers:[{trigger_id, project_id, name, kind, spec, enabled, created_at, updated_at}, …]}` | `-32603` (no store) |
| `ListRemoteAgentTriggerOptions` | read | `{}` (params ignored) | `{ok:true, kinds:["file-changed", "schedule", "webhook", "manual"], spec_schemas:{…}}` | (none — read-only) |
| `CreateRemoteAgentTrigger` | write | `{trigger:{project_id, name, kind, spec, enabled?}}` | `{ok:true, trigger_id:"tri_<hex>", trigger:{…full row…}}` | `-32602` (missing/empty/non-string field, bad spec), `-32016 Trigger exists`, `-32603` (no store) |
| `UpdateRemoteAgentTrigger` | write | `{trigger_id, patch:{name?, enabled?, spec?}}` | `{ok:true, trigger:{…full row…}}` | `-32602` (missing/empty/non-string id, bad spec), `-32017 Trigger not found`, `-32016 Trigger exists` (rename collision), `-32603` (no store) |
| `DeleteRemoteAgentTrigger` | write | `{trigger_id}` | `{ok:true, trigger_id, deleted_at}` | `-32602` (missing/empty/non-string id), `-32017 Trigger not found`, `-32603` (no store) |

Every successful `CreateRemoteAgentTrigger` / `UpdateRemoteAgentTrigger` /
`DeleteRemoteAgentTrigger` emits a **`triggers/changed`** notification (the
same broadcast-to-all shape `rc-permissions-grants` introduced for
`permissions/changed` and `rc-plugins` reused for `plugins/changed`) to
**every** open ws connection, so a dashboard that subscribes to the channel
re-renders without polling and dashboards on other conns stay in sync with
each other.

The cycle ships a **persistence-only** surface:

- Production `FSTriggerStore` writes one
  `$ANTIGRAVITY_HOME/triggers.json` (atomic temp + rename + dir fsync +
  0o600 perms, 0700 dir) matching the cycle-9 `FSProjectStore`, cycle-19
  `FSPermissionStore`, and cycle-20 `FSPluginStore` style.
- Tests use `MemTriggerStore` / `NewMemTriggerStoreWith` (custom-snapshot
  constructor with deterministic `tri_test_NNNN` ids).
- The four `kind`s ship today: `file-changed`, `schedule`, `webhook`,
  `manual`. Each has its own spec validator
  (`ValidateTriggerSpec`) and a canned `spec_schemas` row in the
  `ListRemoteAgentTriggerOptions` envelope.

This is **persistence only** — no scheduling engine, no fsnotify watcher, no
webhook receiver. A future `rc-triggers-runner` cycle (not yet scheduled)
will wire the runner that consumes the on-disk state. Documented in the
硬约束 section.

| Piece | Where |
|---|---|
| `TriggerKind` (closed set: `file-changed` / `schedule` / `webhook` / `manual`) + `IsValidTriggerKind` + `AllTriggerKinds` + `TriggerSpec` + `Trigger` + `TriggerPatch` + `TriggerState` + `TriggerStore` interface + `TriggerStateVersion = 1` + `TriggerIDPrefix = "tri_"` + sentinels (`ErrTriggerNotFound` / `ErrTriggerExists` / `ErrInvalidSpec`) + `normaliseName` helper | `internal/remotecontrol/triggers.go` (new, ~384 lines) |
| `ValidateTriggerSpec` + per-kind validators (`validateFileChangedSpec` / `validateScheduleSpec` / `validateWebhookSpec` / `validateManualSpec`) + helpers (`specFieldMissingErr` / `specWrongTypeErr` / `specBadValueErr` / `requireStringField`) + `TriggerOptionsResponse` / `TriggerSpecEntry` wire types + `DefaultTriggerOptionsResponse` (canned envelope with `spec_schemas` for all four kinds) | `internal/remotecontrol/triggers_options.go` (new, ~373 lines) |
| `FSTriggerStore` (production JSON-file store rooted at `$ANTIGRAVITY_HOME/triggers.json`; atomic temp + rename + dir fsync + 0600 perms + 0700 dir; injectable `now` for tests) + `MemTriggerStore` (in-memory test fixture; deterministic `tri_test_NNNN` ids) + `NewMemTriggerStoreWith` (custom-snapshot constructor with sort-by-CreatedAt + deep clone) + `onDiskTriggerState` struct + `load` / `atomicWrite` / `ensureParentDir` + `cloneTrigger` / `cloneTriggerSpec` / `cloneTriggerState` defensive copies + `sortTriggersByCreatedAt` | `internal/remotecontrol/triggers_store.go` (new, ~860 lines) |
| `listRemoteAgentTriggersParams` / `createRemoteAgentTriggerParams` / `updateRemoteAgentTriggerParams` / `deleteRemoteAgentTriggerParams` envelope types + `handleListRemoteAgentTriggers` / `handleListRemoteAgentTriggerOptions` / `handleCreateRemoteAgentTrigger` / `handleUpdateRemoteAgentTrigger` / `handleDeleteRemoteAgentTrigger` handlers + `triggerToWire` per-trigger projection + `triggersListEnvelope` per-list projection + `mapTriggersError` sentinel → JSON-RPC mapping + `broadcastTriggersChanged` / `broadcastTriggersChangedFromState` (delegates to `d.BroadcastNotify`); `TriggersChangedMethod = "triggers/changed"`; `TriggerStateWireFormat = 1` | `internal/remotecontrol/triggers_dispatcher.go` (new, ~560 lines) |
| `case "ListRemoteAgentTriggers", "ListRemoteAgentTriggerOptions", "CreateRemoteAgentTrigger", "UpdateRemoteAgentTrigger", "DeleteRemoteAgentTrigger"` switch arm + `NewDispatcher` nil-fabrication (fabricates a fresh empty `MemTriggerStore` when `cfg.TriggersStore == nil` — mirrors the PluginsStore nil-guard) | `internal/remotecontrol/dispatch.go` (+~30 / -0) |
| `Config.TriggersStore TriggerStore` + `Config.TriggersPath string` (default `auth.AntigravityHome()/triggers.json`); `HostConfig.TriggersStore` / `HostConfig.TriggersPath` mirror; propagation through `StartHostSession` | `internal/remotecontrol/daemon.go` (+~40 / -0), `internal/remotecontrol/host.go` (+~25 / -0) |
| `buildHostTriggerStore()` helper at the bottom of `cmd/wagyu/remote_control.go` (mirrors `buildHostPluginStore`); wired into both Unix-socket (`maybeStartRemoteControlDaemon`) and TCP (`maybeStartRemoteControlTCPDaemon`) paths as `TriggersStore: buildHostTriggerStore()` | `cmd/wagyu/remote_control.go` (+~50 / -0) |
| In-process tests: spec validator (4 kinds × ok/bad cases) + `IsValidTriggerKind` / `AllTriggerKinds` / `normaliseName` + `DefaultTriggerOptionsResponse` shape + 9 `MemTriggerStore_*` (CRUD, dedup, sort, no-store, clone) + 9 `FSTriggerStore_*` (roundtrip, atomic, perms, version, malformed) + dispatcher round-trips for all 5 methods (including `TestTriggerStore_Create_NoStore`, `TestUpdate_PatchName_WhitespaceTrimmed`, `TestCreate_Update_Rename_Exists`, `TestListRemoteAgentTriggerOptions_DefaultEnvelope` etc.) = **~30 tests** | `internal/remotecontrol/triggers_test.go` (new, ~830 lines) |
| Wire-level WS round-trip tests + `newTestTCPServerWithTriggers` / `assertTriggersListEnvelope` / `assertTriggersChangedNotification` / `codeAsIntForWS` helpers (11 tests: `TestListRemoteAgentTriggers_WS_Ok` + `_WS_OkNoParams`, `TestListRemoteAgentTriggerOptions_WS_Ok`, `TestCreateRemoteAgentTrigger_WS_Ok` + `_WS_Duplicate` + `_WS_BadSpec`, `TestUpdateRemoteAgentTrigger_WS_Ok` + `_WS_NotFound`, `TestDeleteRemoteAgentTrigger_WS_Ok` + `_WS_NotFound`, `TestTriggersChanged_Broadcast`) | `internal/remotetransport/tcp_test.go` (+~1040 / -0) |

What lands for forward consumers:

- A `TriggerStore` seam the dispatcher carries, ready for the future
  `rc-triggers-runner` to add `Subscribe` / `Run` / `ListByKind` etc.
  without re-touching the dispatcher;
- An `FSTriggerStore` JSON-file layout that matches the cycle-9/19/20
  siblings (atomic temp + rename + dir fsync, 0600 perms, 0700 dir);
- A `MemTriggerStore` in-memory fixture tests inject without a tempdir;
- A `MemTriggerStore.With(custom snapshot)` constructor that sorts the
  input by `CreatedAt` ascending and deep-clones each `Spec` map so a
  test that mutates the returned state's Triggers doesn't race the
  store's internal state;
- Three new sentinel errors: `ErrTriggerNotFound → -32017`,
  `ErrTriggerExists → -32016`, `ErrInvalidSpec → -32602`;
- A closed-set `TriggerKind` (4 kinds) the spec validator
  (`ValidateTriggerSpec`) and the options envelope
  (`DefaultTriggerOptionsResponse`) both draw from — change one,
  change the other, and the validator's `default` arm
  (`unknown trigger kind`) trips for any kind added without
  registering both sides;
- Reuse of the `BroadcastNotify` / `SetBroadcaster` callback seam from
  `rc-permissions-grants` (and `rc-plugins`) — the future
  `rc-conversation-config`, `rc-resources`, `rc-terminals` etc. that
  emit their own "X changed" notifications share the same plumbing.

---

## JSON-RPC surface (precise)

### `ListRemoteAgentTriggers`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "ListRemoteAgentTriggers", "params": { "project_id": "" } }
```

(`params` is optional; missing params / empty `project_id` returns all
triggers; non-empty `project_id` filters to that project's triggers.)

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok": true,
    "triggers": [
      {
        "trigger_id":  "tri_3f7b5c9a8b2e1d4f",
        "project_id":  "proj-1",
        "name":        "on-pull",
        "kind":        "file-changed",
        "spec":        { "pattern": "*.go", "event": "modify" },
        "enabled":     true,
        "created_at":  "2026-10-19T12:00:00.000000000Z",
        "updated_at":  "2026-10-19T12:00:00.000000000Z"
      },
      ...
    ]
  }
}
```

- `triggers` is sorted by `CreatedAt` ascending so dashboards see a stable
  sequence (the test pins this by posting two triggers in reverse CreatedAt
  order and asserting `tri_aaa` precedes `tri_bbb` after the wire round-trip).
- `triggers: []` (never `null`) on the wire so dashboards never have to
  nil-check the array — the dispatcher stamps `[]Trigger{}` before
  projecting to `[]any`.
- The brief's triggers envelope is intentionally lean: **no top-level
  `updated_at`** (unlike the plugins envelope) — each trigger row carries
  its own `updated_at`. A future cycle that wants versioning on the wire
  branches on `TriggerStateWireFormat = 1` and revs the envelope.
- `spec` is `{}` (never `null`) on the wire when the trigger has no
  spec — `triggerToWire` stamps `TriggerSpec{}` before projecting so a
  dashboard never has to nil-check the spec.
- Errors: `-32603 "Internal error"` if no `TriggersStore` is installed
  (the dispatcher's nil-fabrication installs a fresh empty `MemTriggerStore`,
  so this case only trips a dispatcher that explicitly cleared the field
  post-construction — the negative test).
- The dispatcher does **not** emit `triggers/changed` on List (read with no
  side effects), so the originating conn's outbound channel holds only the
  response.

### `ListRemoteAgentTriggerOptions`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "ListRemoteAgentTriggerOptions", "params": {} }
```

(`params` is optional; the dispatcher ignores its contents.)

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok": true,
    "kinds": ["file-changed", "schedule", "webhook", "manual"],
    "spec_schemas": {
      "file-changed": {
        "description":  "Fire when a file matching the glob has the event reported by the fsnotify watcher",
        "required":     ["pattern", "event"],
        "field_types":  { "pattern": "string", "event": "string" }
      },
      "schedule": {
        "description":  "Fire on a cron / interval expression with a prompt",
        "required":     ["prompt"],
        "optional":     ["cron", "every"],
        "field_types":  { "prompt": "string", "cron": "string", "every": "string" }
      },
      "webhook": {
        "description":  "Fire when an HTTP request hits the path; optional secret verifies the request",
        "required":     ["path", "prompt"],
        "optional":     ["secret"],
        "field_types":  { "path": "string", "prompt": "string", "secret": "string" }
      },
      "manual": {
        "description":  "Fire when the user clicks the Run button (no auto-event)",
        "required":     ["prompt"],
        "field_types":  { "prompt": "string" }
      }
    }
  }
}
```

- `kinds` is the closed set the spec validator (`ValidateTriggerSpec`)
  draws from — change one, change the other. Rendered as `[]any` (not
  `[]string`) so an in-process caller and a JSON-roundtripped caller see
  the same field type (encoding/json decodes a JSON array into
  `[]interface{}`; without this normalisation, in-process tests would
  see `kinds.([]string)` and wire tests would see `kinds.([]any)` —
  same field, two types).
- `spec_schemas` is descriptive — the validator
  `ValidateTriggerSpec` enforces the same rules at Create / Update time.
  A dashboard that builds a per-field form from `spec_schemas` sees the
  same hints the validator surfaces in error envelopes.
- No store call (no read from disk, no lock taken), so this RPC works
  even when the production `FSTriggerStore` is wedged on disk.
- No broadcast.

### `CreateRemoteAgentTrigger`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "CreateRemoteAgentTrigger",
  "params": {
    "trigger": {
      "project_id": "proj-1",
      "name":       "on-pull",
      "kind":       "file-changed",
      "spec":       { "pattern": "*.go", "event": "modify" },
      "enabled":    true
    }
  }
}
```

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "ok":         true,
    "trigger_id": "tri_3f7b5c9a8b2e1d4f",
    "trigger": {
      "trigger_id":  "tri_3f7b5c9a8b2e1d4f",
      "project_id":  "proj-1",
      "name":        "on-pull",
      "kind":        "file-changed",
      "spec":        { "pattern": "*.go", "event": "modify" },
      "enabled":     true,
      "created_at":  "2026-10-19T12:00:00.000000000Z",
      "updated_at":  "2026-10-19T12:00:00.000000000Z"
    }
  }
}
```

- `trigger_id` carries the `tri_` prefix the brief pins (the store stamps
  `tri_<16hex>` via `crypto/rand.Read` in production; the
  `MemTriggerStore` test fixture stamps `tri_test_NNNN` for snapshot
  predictability).
- The `trigger_id` / `CreatedAt` / `UpdatedAt` fields the dashboard posts
  are ignored — the store stamps all three (defence against a malicious
  / buggy client trying to overwrite another user's trigger id).
- Validation:
  - Empty / whitespace / missing / non-string `project_id` or `name` →
    `-32602 "Invalid params"` (the "dashboard bug" envelope).
  - Unknown `kind` (anything outside the closed set
    `file-changed|schedule|webhook|manual`) → `-32602`.
  - Spec fails `ValidateTriggerSpec` for the trigger's kind →
    `-32602` with the failing field name in `data`.
  - `(project_id, name)` collides with an existing trigger → `-32016
    "Trigger exists"`.
- Side effects:
  - Atomic disk write (temp + rename + dir fsync + 0o600 perms) on the
    production store.
  - Broadcasts `triggers/changed` on every open ws connection
    **immediately before** this response is enqueued (matches the
    `rc-permissions-grants` / `rc-plugins` pattern; the originating
    conn's outbound channel holds `[notification, response]`).

### `UpdateRemoteAgentTrigger`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "UpdateRemoteAgentTrigger",
  "params": {
    "trigger_id": "tri_3f7b5c9a8b2e1d4f",
    "patch":      { "enabled": true }
  }
}
```

(`patch` is an object with three optional fields: `name`, `enabled`,
`spec`. The dispatcher applies only the non-nil fields and leaves the
rest alone. `spec`, when present, replaces the existing spec atomically
— no partial-merge of keys.)

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "ok": true,
    "trigger": {
      "trigger_id":  "tri_3f7b5c9a8b2e1d4f",
      "project_id":  "proj-1",
      "name":        "on-pull",
      "kind":        "file-changed",
      "spec":        { "pattern": "*.go", "event": "modify" },
      "enabled":     true,
      "created_at":  "2026-10-19T12:00:00.000000000Z",
      "updated_at":  "2026-10-19T12:34:56.000000000Z"
    }
  }
}
```

- Validation:
  - Empty / whitespace / missing / non-string `trigger_id` → `-32602
    "Invalid params"`.
  - `patch.name` present-but-empty → `-32602`.
  - Unknown `trigger_id` → `-32017 "Trigger not found"`.
  - New `patch.name` collides with another trigger in the same
    `(project_id, name)` → `-32016 "Trigger exists"` (parity with
    Create — both paths share the unique-within-project rule, so the
    dashboard's retry / fallback treats them uniformly).
  - `patch.spec` present but fails `ValidateTriggerSpec` for the
    trigger's kind → `-32602`.
- Side effects:
  - Atomic disk write (temp + rename + dir fsync + 0o600 perms) on the
    production store.
  - Broadcasts `triggers/changed` on every open ws connection
    immediately before this response is enqueued.
- `created_at` is preserved (the original creation timestamp survives
  the update); `updated_at` is the new mutation timestamp.

### `DeleteRemoteAgentTrigger`

Request:

```json
{ "jsonrpc": "2.0", "id": 4, "method": "DeleteRemoteAgentTrigger", "params": { "trigger_id": "tri_3f7b5c9a8b2e1d4f" } }
```

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "ok":          true,
    "trigger_id":  "tri_3f7b5c9a8b2e1d4f",
    "deleted_at":  "2026-10-19T12:34:56.000000000Z"
  }
}
```

- Response envelope is the brief's lean `{ok, trigger_id, deleted_at}`
  shape. The full post-state (with the deleted trigger removed) is
  available via the `triggers/changed` broadcast that fires on every
  open ws connection immediately before this response is enqueued.
- Validation:
  - Empty / whitespace / missing / non-string `trigger_id` → `-32602
    "Invalid params"`.
  - Unknown `trigger_id` → `-32017 "Trigger not found"`.
- Side effects:
  - Atomic disk write (temp + rename + dir fsync + 0o600 perms) on the
    production store.
  - Broadcasts `triggers/changed` on every open ws connection
    immediately before this response is enqueued.

---

## `triggers.json` schema (`version: 1`)

Single JSON file at `$ANTIGRAVITY_HOME/triggers.json`:

```json
{
  "version": 1,
  "triggers": [
    {
      "trigger_id":  "tri_3f7b5c9a8b2e1d4f",
      "project_id":  "proj-1",
      "name":        "on-pull",
      "kind":        "file-changed",
      "spec":        { "pattern": "*.go", "event": "modify" },
      "enabled":     true,
      "created_at":  "2026-10-19T12:00:00.000000000Z",
      "updated_at":  "2026-10-19T12:00:00.000000000Z"
    }
  ],
  "updated_at": "2026-10-19T12:00:00.000000000Z"
}
```

Field semantics:

- `version` — on-disk format version. Bumped on breaking changes (renamed
  keys, removed fields); non-breaking additions (new optional fields
  with `omitempty`) leave the version alone. Pinned to `1` this cycle.
- `triggers` — the live trigger slice. Stored as a JSON array even when
  empty (`[]`); a missing file yields the same shape on first read.
- `updated_at` — RFC3339Nano UTC stamp the store bumped at the
  most-recent mutation. Surfaced on the wire only via the
  `triggers/changed` broadcast, not on the per-trigger envelopes
  (where each row carries its own `updated_at`).

---

## Spec validator (per-kind rules)

The `ValidateTriggerSpec` function is a closed switch over `TriggerKind`.
Each kind has its own validator; the rules:

| Kind | Required | Optional | Field types | Notes |
|---|---|---|---|---|
| `file-changed` | `pattern` (string, non-empty), `event` (string ∈ {`create`, `modify`, `delete`, `rename`}) | (none) | string | `pattern` is a glob; the future runner wires the fsnotify watcher. |
| `schedule` | `prompt` (string, non-empty); exactly one of `cron` OR `every` (string, non-empty) | (none beyond cron/every) | string | Both `cron` and `every` set → error. The string's *value* is not parsed today — the future runner's cron / interval parser is the second line of defence. |
| `webhook` | `path` (string, must start with `/`), `prompt` (string, non-empty) | `secret` (string, future HMAC verifier authoritative) | string | `path` validates the leading `/` only — the future runner registers the actual route. |
| `manual` | `prompt` (string, non-empty) | (none) | string | No auto-event; the runner listens for a future `RunRemoteAgentTrigger` RPC. |

The validator is intentionally *strict on shape* and *light on value
semantics*: it checks types and required fields, but does not parse
cron expressions or webhook paths. This keeps the persisted state
forward-compatible — a spec that parses today can be scheduled by a
future runner with no data migration.

Unknown keys are silently dropped (forward-compat for future kinds'
optional fields); the validator does not trip on extra keys. Adding a
new required field to an existing kind is a breaking change and bumps
the `TriggerSpec` JSON tags' wire-format documentation; the brief's
"closed set on `TriggerKind`" rule means new kinds extend the constants
and the validator's switch arm rather than mutate an existing kind.

---

## Storage / Wire — production wiring

- `$ANTIGRAVITY_HOME/triggers.json` (production path resolved by
  `auth.AntigravityHome()`; honours `$ANTIGRAVITY_HOME`, falls back to
  `$HOME/.gemini/antigravity-cli/triggers.json`).
- Atomic write: temp file in the same directory, `fsync`, `rename(2)`,
  dir `fsync`, file mode `0600`, parent dir mode `0700`. Crash-consistent
  with the cycle-9 / 19 / 20 sibling stores.
- Concurrent per-process safety: a single `RWMutex` on
  `FSTriggerStore` serialises every read-modify-write. Snapshot / List
  take the read lock so concurrent reads don't race a writer's
  in-progress mutation.
- `NewDispatcher` nil-fabricates a fresh empty `MemTriggerStore` when
  `cfg.TriggersStore == nil` — mirrors the LastDeepLink /
  PermissionsStore / PluginsStore nil-guard pattern.
- `cmd/wagyu/remote_control.go`'s `buildHostTriggerStore()` is wired
  into both Unix-socket (`maybeStartRemoteControlDaemon`) and TCP
  (`maybeStartRemoteControlTCPDaemon`) paths.

---

## Broadcasting — `triggers/changed`

Every successful Create / Update / Delete invokes
`(*Dispatcher).broadcastTriggersChanged` (or
`broadcastTriggersChangedFromState` for Delete) **before** dispatchOne
marshals and enqueues the response. The seam is the same
`(*Dispatcher).BroadcastNotify` / `(*Dispatcher).SetBroadcaster`
pattern `rc-permissions-grants` introduced and `rc-plugins` reused:
the broadcast seam is transport-agnostic, the inner-case handlers
here don't know which transport is registered, they just call
`BroadcastNotify` and the registered broadcaster fans the frame out.

Wire envelope (the same envelope `ListRemoteAgentTriggers` returns —
delivered without an `id` field per JSON-RPC 2.0 §4.1 notification
rules):

```json
{
  "jsonrpc": "2.0",
  "method": "triggers/changed",
  "params": {
    "ok": true,
    "triggers": [
      { ...full post-state trigger row... }
    ]
  }
}
```

The broadcast **always carries the full post-mutation state**, not a
delta. This matches the `permissions/changed` /
`plugins/changed` contract: a dashboard that listens for the channel
never has to merge deltas — the next frame is authoritative. List does
not broadcast (read with no side effects).

Wire-order subtlety (pinned by the wire tests):

- Originating conn's outbound channel after a Create / Update / Delete:
  `[notification, response]` (broadcaster enqueues first, dispatchOne
  enqueues the response second).
- Passive conn's outbound channel: `[notification]` only.
- `readJSONRPCResponse` drops the notification (no `id` field) so a
  test that just wants the response can call it directly; a test that
  wants to assert the broadcast explicitly calls `readJSONRPC` first
  to drain the notification, then `readJSONRPCResponse` for the
  response.

---

## Tests

### In-process (`internal/remotecontrol/triggers_test.go`)

~30 tests covering:

- **Spec validator (per-kind)**:
  - `TestValidateTriggerSpec_FileChanged` (4 sub-cases: ok, missing pattern,
    missing event, bad event value).
  - `TestValidateTriggerSpec_Schedule` (4 sub-cases: cron, every, both
    [error], neither [error], missing prompt).
  - `TestValidateTriggerSpec_Webhook` (3 sub-cases: ok, missing path,
    path without leading slash).
  - `TestValidateTriggerSpec_Manual` (2 sub-cases: ok, missing prompt).
  - `TestValidateTriggerSpec_UnknownKind` (closed-set check).
  - `TestValidateTriggerSpec_NilSpec` (the `spec == nil` → "(spec)" hint).
- **Types & helpers**: `TestIsValidTriggerKind`, `TestAllTriggerKinds`,
  `TestNormaliseName`.
- **Options envelope**: `TestDefaultTriggerOptionsResponse` (4 kinds,
  every schema entry has description / required / optional /
  field_types), `TestTriggerOptionsResponse_KindsMatchClosedSet`.
- **`MemTriggerStore`**:
  - `TestMemTriggerStore_DefaultState` (empty + version=1).
  - `TestMemTriggerStore_Create` (id has `tri_` prefix, deterministic
    `tri_test_NNNN` id, deep-cloned spec).
  - `TestMemTriggerStore_Create_Duplicate` (collide on
    `(project_id, name)` → `ErrTriggerExists`).
  - `TestMemTriggerStore_List_SortedByCreatedAt` (sort contract).
  - `TestMemTriggerStore_List_FilterByProject` (`project_id` filter).
  - `TestMemTriggerStore_Update_PatchEnabled` (post-mutation row).
  - `TestMemTriggerStore_Update_Rename_Exists` (collision → `ErrTriggerExists`).
  - `TestMemTriggerStore_Update_NotFound` (unknown id → `ErrTriggerNotFound`).
  - `TestMemTriggerStore_Delete` (returns post-state, broadcastable).
  - `TestMemTriggerStore_Delete_NotFound` (unknown id → `ErrTriggerNotFound`).
  - `TestNewMemTriggerStoreWith_DeepClone` (input slice mutation
    doesn't race the store's internal state).
- **`FSTriggerStore`**:
  - `TestFSTriggerStore_Roundtrip` (Create → re-load → Snapshot).
  - `TestFSTriggerStore_Atomic` (concurrent writers don't tear the
    file; partial writes never observed).
  - `TestFSTriggerStore_Permissions` (file mode `0600`, dir mode
    `0700`).
  - `TestFSTriggerStore_Version` (`version=1` always).
  - `TestFSTriggerStore_Malformed` (graceful degradation: malformed
    JSON → empty state, not crash).
  - `TestFSTriggerStore_NoFile` (first read returns empty state).
  - `TestFSTriggerStore_Create_Broadcast` (post-mutation state).
  - `TestFSTriggerStore_Update_Broadcast` (post-mutation state).
  - `TestFSTriggerStore_Delete_Broadcast` (post-mutation state).
- **Dispatcher round-trips** (in-process, no transport):
  - `TestListRemoteAgentTriggers_Ok` / `_WithProjectFilter` /
    `_NoParams`.
  - `TestListRemoteAgentTriggerOptions_DefaultEnvelope`.
  - `TestCreateRemoteAgentTrigger_Ok` / `_Duplicate` /
    `_BadSpec` / `_MissingProjectID` / `_MissingName` /
    `_UnknownKind` / `_NoStore` (post-construction nil-clear).
  - `TestUpdateRemoteAgentTrigger_Ok` / `_NotFound` /
    `_Rename_Exists` / `_PatchEnabled` / `_PatchSpec`.
  - `TestDeleteRemoteAgentTrigger_Ok` / `_NotFound`.
  - `TestTriggerPatch_NameWhitespaceTrimmed` (normaliseName path).
  - `TestTriggerStore_Create_NoStore` (dispatcher path bypasses
    `NewDispatcher`'s nil-fabrication when `d.cfg.TriggersStore` is
    explicitly cleared post-construction).

All pass under `go test -race -count=1 ./internal/remotecontrol/`.

### Wire-level (`internal/remotetransport/tcp_test.go`)

11 tests covering:

- `TestListRemoteAgentTriggers_WS_Ok` — pre-populated
  `NewMemTriggerStoreWith` (2 triggers, reverse CreatedAt order) →
  WS round-trip → assert the brief's lean `{ok, triggers:[...]}`
  envelope, sorted by `CreatedAt` ascending (the wire re-asserts the
  sort contract end-to-end).
- `TestListRemoteAgentTriggers_WS_OkNoParams` — params absent /
  null → same envelope (the spec's permissive "params may be absent"
  shape).
- `TestListRemoteAgentTriggerOptions_WS_Ok` — assert the canned
  `{kinds, spec_schemas}` envelope, kinds as `[]any` of 4 strings
  (matching in-process shape), every schema entry has description /
  required / field_types.
- `TestCreateRemoteAgentTrigger_WS_Ok` — pre-empty store → WS round-trip
  → assert `trigger_id` has `tri_` prefix, the trigger row carries the
  8 per-trigger fields, follow-up `List` shows the new row.
- `TestCreateRemoteAgentTrigger_WS_Duplicate` — first Create succeeds
  (drain broadcast + response), second Create with same
  `(project_id, name)` returns `-32016 "Trigger exists"`.
- `TestCreateRemoteAgentTrigger_WS_BadSpec` — file-changed kind with
  missing `pattern` → `-32602 "Invalid params"` (validator's
  `specFieldMissingErr` path).
- `TestUpdateRemoteAgentTrigger_WS_Ok` — pre-create via wire, post
  `Update` with `{enabled: true}` (drain broadcast), assert
  post-mutation row reflects the patch; unchanged fields stay as
  Create set them.
- `TestUpdateRemoteAgentTrigger_WS_NotFound` — unknown `trigger_id` →
  `-32017 "Trigger not found"`.
- `TestDeleteRemoteAgentTrigger_WS_Ok` — pre-create via wire, post
  `Delete` (drain broadcast with the empty post-state), assert the
  brief's lean `{ok, trigger_id, deleted_at}` envelope; follow-up
  `List` returns 0 entries.
- `TestDeleteRemoteAgentTrigger_WS_NotFound` — unknown `trigger_id` →
  `-32017 "Trigger not found"`.
- `TestTriggersChanged_Broadcast` — two ws conns; pre-create via a
  throwaway conn; `Delete` on c1 → c1 sees `[notification, response]`
  in that order; c2 (passive) sees `[notification]` only; both observe
  the post-Delete empty state.

All pass under `go test -race -count=1 ./internal/remotetransport/`.

### Helpers

- `newTestTCPServerWithTriggers(t, ts)` — mirror of
  `newTestTCPServerWithPlugins`; brings up a TCPServer whose
  dispatcher's `TriggersStore` seam is the supplied `TriggerStore` (one
  per test, so state doesn't leak).
- `assertTriggersListEnvelope(t, label, result, wantCount)` — asserts
  the `{ok, triggers}` shape, the per-trigger field set (trigger_id /
  project_id / name / kind / spec / enabled / created_at /
  updated_at), and that spec is a non-nil object map.
- `assertTriggersChangedNotification(t, label, frame, wantCount)` —
  asserts a `triggers/changed` frame has no `id` field, the right
  method name (`remotecontrol.TriggersChangedMethod = "triggers/changed"`),
  and a params that satisfies `assertTriggersListEnvelope`.
- `codeAsIntForWS(t, v)` — wire-equivalent of `remotecontrol.codeAsInt`:
  a JSON-RPC error `code` travels over the wire as a JSON number,
  which `encoding/json` decodes into a `float64` in Go. This thin
  wrapper asserts the type and returns an `int` so the wire tests can
  compare against the literal sentinel codes (`-32016` / `-32017` /
  `-32602` / `-32603`).

---

## Style / vetting / formatting

- `gofmt -l` clean on all new + modified files
  (`internal/remotecontrol/triggers.go`,
  `internal/remotecontrol/triggers_dispatcher.go`,
  `internal/remotecontrol/triggers_options.go`,
  `internal/remotecontrol/triggers_store.go`,
  `internal/remotecontrol/triggers_test.go`,
  `internal/remotecontrol/dispatch.go`,
  `internal/remotecontrol/daemon.go`,
  `internal/remotecontrol/host.go`,
  `cmd/wagyu/remote_control.go`,
  `internal/remotetransport/tcp_test.go`).
- `go vet ./internal/remotecontrol/ ./internal/remotetransport/
  ./cmd/wagyu/` exit 0 (pre-existing
  `cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf` vet warning
  stays as-is — out of scope per the brief).
- `go test -race -count=1 ./internal/remotecontrol/
  ./internal/remotetransport/` PASS (323 tests in `remotecontrol/`,
  100 tests in `remotetransport/`, all under `-race`).
- `go build -o /tmp/wagyu ./cmd/wagyu` exit 0.

---

## 硬约束 / explicit defers

- **No scheduling engine, no fsnotify watcher, no webhook receiver**.
  This cycle ships the persistence layer only. The five methods are
  the CRUD surface; a future `rc-triggers-runner` cycle (not yet
  scheduled) wires the runner that consumes the on-disk state. The
  spec validator is strict on shape but light on value semantics — it
  checks types and required fields, but does not parse cron
  expressions or webhook paths — so the persisted state stays
  forward-compatible: a spec that parses today can be scheduled by a
  future runner with no data migration. Documented in `PLAN.md`'s
  gap-closure table as a future cycle.
- **`MemTriggerStore.Create` uses deterministic `tri_test_NNNN`
  ids**, **not** `crypto/rand.Read`. The deterministic ids let tests
  pin the `trigger_id` shape without entropy. **The production
  `FSTriggerStore.Create` uses `crypto/rand.Read`** (the same
  randomness source the cycle-19 / 20 stores use). Tests that assert
  the wire envelope do so against the prefix (`tri_`), not the
  suffix, so the deterministic-vs-random distinction is invisible to
  them.
- **No `RunRemoteAgentTrigger` method**. The brief pins the five
  CRUD methods + Options; firing a manual trigger (or firing a
  schedule / webhook from outside) is a separate RPC. A future cycle
  that wants to drive a manual trigger wires
  `RunRemoteAgentTrigger` against the same `TriggerStore` seam and
  adds the `Trigger.Run(ctx, ...)` method to the interface. Today's
  `manual` kind stores its `prompt` and waits for the future runner.
- **Closed set on `TriggerKind`**. Adding a new kind requires
  extending the constants in `triggers.go`, the closed set in
  `IsValidTriggerKind`, the slice in `AllTriggerKinds`, the
  per-kind validator in `ValidateTriggerSpec`, the schema entry in
  `DefaultTriggerOptionsResponse`, and (for any kind that needs it)
  the runner in `rc-triggers-runner`. The closed-set check at the
  dispatcher layer (`IsValidTriggerKind`) means a missing kind
  surfaces as `-32602` before the store call, so a half-wired kind
  fails closed rather than open.
- **No spec-validator `defaults`**. The `TriggerSpecEntry.Defaults`
  field is `nil` for every kind today — the dashboard pre-fills from
  a hardcoded shape if it wants to. A future cycle that wants
  server-side defaults extends the helper and bumps
  `TriggerStateWireFormat` (cycle document required).
- **`schedule` kind: cron / every value not parsed**. The validator
  accepts any non-empty string for `cron` or `every`. The future
  runner's cron / interval parser is the second line of defence; a
  cycle that wants persistence-side validation blocks future spec
  formats the runner wants to accept, so today's strict-on-shape
  contract is the deliberate trade-off.
- **No `spec.updated_at`**. Each trigger row carries `created_at` /
  `updated_at` for the whole record; the spec itself does not have a
  timestamp. A future cycle that wants per-spec change tracking adds
  `spec_updated_at` and bumps `TriggerStateWireFormat`.
- **`Spec` is not parsed at List time**. `ListRemoteAgentTriggers`
  returns the full trigger rows (including `spec`) verbatim. The
  dashboard sees the same shape the dashboard posted — no
  re-projection, no field reordering. A future cycle that wants a
  `summary` projection (e.g. `spec.description` only) branches on
  `TriggerStateWireFormat` and revs the envelope.
- **File-vs-store consistency**: `FSTriggerStore.Create` /
  `Update` / `Delete` write the post-state to disk atomically (temp
  + rename + dir fsync + 0o600 perms). A second daemon running on a
  different host (or a second user) reading the same file will see
  the post-state but only via the file — concurrent inter-process
  writes are not gated by the `FSTriggerStore` mutex (the mutex is
  per-process). This is the same scope rule as `FSProjectStore` /
  `FSPermissionStore` / `FSPluginStore`; the `rc-triggers-runner`
  cycle that introduces a runner will pick up the inter-process
  locking discipline (file locks, `flock`, etc.) as part of the
  runner-port wiring.
- **Pre-existing project hygiene**: pre-existing files were not
  reformatted (`gofmt -l` was clean only on the new + directly-
  modified files). The pre-existing `model_default_effort_test.go`
  vet warning is out of scope.
- **In-process vs wire type parity for `kinds`**: the dispatcher
  converts `Kinds []string` to `[]any` (and `SpecSchemas
  map[string]TriggerSpecEntry` to `map[string]any`) so an
  in-process caller and a JSON-roundtripped caller see the same
  field types. Without this, in-process tests would see
  `kinds.([]string)` and wire tests would see `kinds.([]any)` — same
  field, two types, and the assertions would diverge.

---

## Reviewer commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# 1. Build
go build -o /tmp/wagyu ./cmd/wagyu

# 2. Vet (the pre-existing model_default_effort_test.go:67
#    3-arg Errorf warning stays; everything else clean)
go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/

# 3. gofmt (clean on new + modified files)
gofmt -l \
  internal/remotecontrol/triggers.go \
  internal/remotecontrol/triggers_dispatcher.go \
  internal/remotecontrol/triggers_options.go \
  internal/remotecontrol/triggers_store.go \
  internal/remotecontrol/triggers_test.go \
  internal/remotecontrol/dispatch.go \
  internal/remotecontrol/daemon.go \
  internal/remotecontrol/host.go \
  cmd/wagyu/remote_control.go \
  internal/remotetransport/tcp_test.go

# 4. In-process + wire-level tests (race detector)
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# 5. Run just the trigger tests for a tight loop
go test -race -count=1 ./internal/remotecontrol/ -run "Trigger"
go test -race -count=1 ./internal/remotetransport/ \
  -run "TestListRemoteAgentTrigger|TestCreateRemoteAgentTrigger|TestUpdateRemoteAgentTrigger|TestDeleteRemoteAgentTrigger|TestTriggersChanged_Broadcast"
```

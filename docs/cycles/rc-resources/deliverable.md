# Cycle `rc-resources` — three JSON-RPC resource methods (`ListRemoteAgentResourceProviders` / `ListRemoteAgentAvailableResources` / `UpdateRemoteAgentProjectResources`)

## TL;DR / 摘要

Cycles 1–24 of `exp/rc-server-mode` shipped 24 JSON-RPC methods. The
re-investigation matrix tracks the remaining `Resource*` surface (G14 line)
as **TODO**: `ListRemoteAgentResourceProviders`, `ListRemoteAgentAvailableResources`,
`UpdateRemoteAgentProjectResources`. This cycle ships all three over a
new `ResourceStore` seam:

- Two pure-read methods
  (`ListRemoteAgentResourceProviders` /
  `ListRemoteAgentAvailableResources`) backed by a JSON-file registry
  rooted at `$ANTIGRAVITY_HOME/resources.json` (4 fixture providers + 12
  fixture resources);
- One write method
  (`UpdateRemoteAgentProjectResources`) that **binds** a list of
  `resource_id`s to a `project_id`. The bind is **replace-style**: the
  supplied slice becomes the project's new bound set; an explicit empty
  list is a valid unbind. Unknown `resource_id`s are silently counted as
  `unbound` (the brief's "unknown resource is no-op" rule).
- A `resources/changed` server-push notification fires on every
  successful Bind via the existing `(*Dispatcher).BroadcastNotify` seam
  (`rc-permissions-grants` added it; shared with `rc-plugins` /
  `rc-triggers` / `rc-fastpush-toggle`). The notification carries the
  same shape the response envelope emits — polling callers and
  notification subscribers see byte-for-byte identical frames.
- The cycle also extends the existing `Project` record with a new
  `Resources []string` field (and `ProjectPatch.Resources *[]string` so
  Update's "leave unchanged" semantic distinguishes "absent" from
  "explicit empty list"). The persisted field appears on
  `ListRemoteAgentProjects`'s envelope under the `resources` key when
  non-nil — a pre-rc-resources dashboard (one that doesn't know about
  bindings) parses the envelope unchanged because the key is **omitted
  when nil**.

The cycle is **registry + bind only** — the dispatcher reads the
provider/resource fixtures (or the on-disk JSON the FS store hands back)
and writes the bound ids back through the bound `ProjectStore` so the
project JSON is the source of truth for binding state. A future
`rc-resources-sync` cycle will pull live provider data; this cycle
ships the fixture set + the bind surface the live cycle will keep
using. Documented in the **硬約束** section.

| Method | Direction | Params | Result | Errors |
|---|---|---|---|---|
| `ListRemoteAgentResourceProviders` | read | `{kind?}` (closed-set filter) | `{providers:[{provider_id, name, kind, configured, scopes?, version?}, …], total}` | `-32602` (unknown `kind`), `-32603` (no store) |
| `ListRemoteAgentAvailableResources` | read | `{provider_id?, kind?}` (optional filters) | `{resources:[{resource_id, provider_id, kind, label, ref, payload_summary?}, …], total}` | `-32602` (unknown `provider_id` filter, unknown `kind`), `-32603` (no store) |
| `UpdateRemoteAgentProjectResources` | write | `{project_id, resource_ids[]}` | `{ok:true, project_id, bound_count, unbound_count, updated_at}` | `-32602` (empty `project_id`), `-32004` "Project not found", `-32603` (no store / I/O) |

Every successful Bind emits a **`resources/changed`** notification to
**every** open ws connection (the same broadcast-to-all shape
`rc-permissions-grants` introduced for `permissions/changed`,
`rc-plugins` reused for `plugins/changed`, `rc-triggers` reused for
`triggers/changed`, `rc-fastpush-toggle` reused for
`fastpush/setting_changed`).

| Piece | Where |
|---|---|
| `ResourceKind` closed-set constants (`"vcs"` / `"observability"` / `"pm"` / `"docs"` / `"ci"`) + `AllResourceKinds` (canonical order) + `IsValidResourceKind` validator + `ResourceProvider` (per-row record: `provider_id` / `name` / `kind` / `configured` / `scopes` / `version`) + `Resource` (per-row record: `resource_id` / `provider_id` / `kind` / `label` / `ref` / `payload_summary`) + `ResourceState` (single-document envelope: `version:1` / `providers:[…]` / `resources:[…]` / `updated_at`) + `ResourceStore` interface (`Providers` / `Resources(providerID, kind)` / `Bind(projectID, resourceIDs) → (bound, unbound, err)`) + `ErrResourceProviderUnknown` (→ `-32602`) + `ErrResourceProjectNotFound` (→ `-32004`) + `ResourceStateVersion = 1` + `NotifResourcesChangedMethod = "resources/changed"` + `normaliseProviderID` / `normaliseResourceID` helpers + `sortProvidersByID` / `sortResourcesByID` + `resourcesChangedToWire` (per-update wire projection — byte-for-byte identical to the broadcast notification) + `mapResourceError` (sentinel → JSON-RPC envelope) | `internal/remotecontrol/resources.go` (new, ~475 lines) |
| `FSResourceStore` (production JSON-file store rooted at `$ANTIGRAVITY_HOME/resources.json`; atomic temp + rename + dir fsync + 0o600 perms + 0o700 dir; injectable `now` for tests; **optional `checkSecretDir` closure** the production `buildHostResourceStore` wires from `$ANTIGRAVITY_HOME/secrets/<provider_id>` so each snapshot's `configured` flag reflects the operator's secrets directory; **optional `Projects` ProjectStore** the Bind path writes back through — the cycle-9 `FSProjectStore` is the canonical seam so the bind state lives on the project JSON, not on the resources.json registry) + `MemResourceStore` (in-memory test fixture) + `NewMemResourceStore` (fixture-seed default + optional `Projects` for Bind) + `NewMemResourceStoreWith` (custom-snapshot constructor with sort-by-ProviderID + deep clone of provider + resource slices) + `DefaultResourceProviders` (4 well-known providers: github / sentry / linear / gdocs) + `DefaultResourceResources` (12 well-known resources spread across the 4 providers) + `onDiskResourceState` struct + `load` / `atomicWrite` / `ensureParentDir` + `fixtureState` / `snapshot` + `cloneResourceState` defensive copies | `internal/remotecontrol/resources_store.go` (new, ~953 lines) |
| `listRemoteAgentResourceProvidersParams` / `listRemoteAgentAvailableResourcesParams` / `updateRemoteAgentProjectResourcesParams` envelope types + `handleListRemoteAgentResourceProviders` + `handleListRemoteAgentAvailableResources` + `handleUpdateRemoteAgentProjectResources` handlers (the Bind handler calls `BroadcastNotify` BEFORE returning the response so the originating conn's outbound channel holds `[notification, response]` — the `rc-plugins` / `rc-triggers` / `rc-fastpush-toggle` precedent) | `internal/remotecontrol/resources_dispatcher.go` (new, ~395 lines) |
| `case "ListRemoteAgentResourceProviders", "ListRemoteAgentAvailableResources", "UpdateRemoteAgentProjectResources":` switch arm with a 3-arm inner switch; `NewDispatcher` nil-fabrication (fabricates a fresh empty `MemResourceStore` seeded with the fixture set when `cfg.ResourceStore == nil` — mirrors the `PermissionsStore` / `PluginsStore` / `TriggersStore` / `FastpushStore` nil-guard); package-level comment on the `Dispatch` switch updated to document the rc-resources cycle | `internal/remotecontrol/dispatch.go` (+~70 / -0) |
| `Config.ResourceStore ResourceStore` + `Config.ResourcePath string` (default `auth.AntigravityHome()/resources.json`); `HostConfig.ResourceStore` / `HostConfig.ResourcePath` mirror; propagation through `StartHostSession` | `internal/remotecontrol/daemon.go` (+~44 / -0), `internal/remotecontrol/host.go` (+~43 / -0) |
| `buildHostResourceStore(projects)` helper at the bottom of `cmd/wagyu/remote_control.go` (mirrors `buildHostFastpushStore` — reads `auth.AntigravityHome()` for the path + secrets directory; wires a `checkSecretDir` closure that stats `$ANTIGRAVITY_HOME/secrets/<provider_id>` and reports `Size() > 0` as `configured`; nil matches a 0 byte / absent file → false); wired into both Unix-socket (`maybeStartRemoteControlDaemon`), TCP (`maybeStartRemoteControlTCPDaemon`), and the foreground `LaunchAgent` daemon (`runRemoteControlForeground`) paths as `ResourceStore: buildHostResourceStore(...)`. All three paths share the same `*FSProjectStore` so the Bind path's project-JSON write is visible across transports | `cmd/wagyu/remote_control.go` (+~126 / -0), `cmd/wagyu/remote_control_foreground.go` (+~20 / -0) |
| `Project.Resources []string` + `ProjectPatch.Resources *[]string` (pointer-form guard distinguishes "absent" from "explicit empty list" — matches `Project.Patch.FailIfBound *bool` from `rc-conversation-deletions`); `onDiskProject.Resources []string` JSON field with `omitempty` so a pre-rc-resources project JSON (without the `resources` key) parses as `nil` (back-compat); `MemProjectStore.Update` + `(*FSProjectStore).Update` `Resources` patch handling (replace-style bind: when `patch.Resources != nil`, set `rec.Resources` to the supplied slice — even an explicit empty slice is a valid unbind; nil = "leave unchanged"); `projectFromDisk` defensive copy of the `Resources` field | `internal/remotecontrol/projects.go` (+~33 / -0), `internal/remotecontrol/projects_store.go` (+~82 / -9) |
| `projectSliceToWire` projection extended with the `resources` field: included on the wire envelope when `Project.Resources` is non-nil, **omitted** when nil (back-compat for pre-rc-resources dashboards); defensive copy of the bound id slice so a dashboard that holds onto the returned map doesn't observe in-place mutations the store performs on its internal state | `internal/remotecontrol/project_dispatcher.go` (+~30 / -3) |
| In-process tests: `TestNormaliseResourceKind` × 10 sub-tests + `TestAllResourceKinds` (pinned canonical order) + `TestNotifResourcesChangedMethod` + `TestResourceStateVersion` + `TestNormaliseProviderIDAndResourceID` × 5 sub-tests + 9 `TestResourceStore_Mem*` (default providers + default resources + provider filter + kind filter + combined filter + unknown project bind + happy bind + unknown resource bind + mixed known/unknown + no projects + replace-style + empty-slice unbind) + `TestResourceStore_MemStoreWith_Custom` (custom-snapshot constructor + deep copy) + `TestResourcesChangedToWire` (response/notification envelope pin) + `TestMapResourceError` × 3 (project-not-found, provider-unknown, I/O) + 3 `TestFSResourceStore_*` (fresh-host fixture-seeded state + bind writes through to project JSON + `configured` flag derived from `$ANTIGRAVITY_HOME/secrets/`) + 11 `TestListRemoteAgentResourceProviders_*` / `TestListRemoteAgentAvailableResources_*` / `TestUpdateRemoteAgentProjectResources_*` dispatcher round-trips (happy paths + kind filter + provider filter + bad kind / unknown provider / unknown project / bad params / persist-to-project / `resources` field on List envelope + back-compat nil-omit) = **~40 tests** | `internal/remotecontrol/resources_test.go` (new, ~800 lines) |
| Wire-level WS round-trip tests + `newTestTCPServerWithResources` / `assertResourcesChangedNotification` helpers (5 tests: `TestListRemoteAgentResourceProviders_WS_Ok` (asserts `gdocs, github, linear, sentry` alphabetical order), `TestListRemoteAgentAvailableResources_WS_Ok` (12 resources + `total:12`), `TestUpdateRemoteAgentProjectResources_WS_Ok` (Bind happy path + broadcast notification), `TestUpdateRemoteAgentProjectResources_WS_UnknownProject` (-32004 envelope), `TestUpdateRemoteAgentProjectResources_WS_BadParams` (-32602 envelope)) | `internal/remotetransport/tcp_test.go` (+~340 / -0) |

What lands for forward consumers:

- A `ResourceStore` seam the dispatcher carries, ready for the future
  `rc-resources-sync` cycle to add a per-provider refresh hook
  (`RefreshProvider(providerID) ([]Resource, error)`) without
  re-touching the dispatcher;
- An `FSResourceStore` JSON-file layout that matches the cycle-9
  `FSProjectStore` / cycle-19 `FSPermissionStore` / cycle-20
  `FSPluginStore` / cycle-21 `FSTriggerStore` / cycle-24
  `FSFastpushStore` style (atomic temp + rename + dir fsync, 0600
  perms, 0700 dir);
- A `MemResourceStore` in-memory fixture tests inject without a
  tempdir;
- A `MemResourceStore.With(custom snapshot)` constructor that sorts
  the input by `ProviderID` ascending and deep-clones each
  `ResourceProvider` + `Resource` row so a test that mutates the
  returned state's providers doesn't race the store's internal state;
- `Project.Resources` + `ProjectPatch.Resources *[]string` on the
  existing project surface — the future cycle that wants to add
  per-resource binding metadata (e.g. `bound_at` / `bound_by`) only
  has to extend `onDiskProject.Resources` to a typed struct without
  re-touching the dispatcher;
- `projectSliceToWire`'s `resources` field (omitted when nil) — a
  pre-rc-resources dashboard that doesn't know about bindings parses
  the envelope unchanged; the future cycle that wants a richer
  envelope (e.g. `{resource_id, bound_at, bound_by}`) only modifies
  this single helper;
- Reuse of the `BroadcastNotify` / `SetBroadcaster` callback seam
  from `rc-permissions-grants` (shared with `rc-plugins` /
  `rc-triggers` / `rc-fastpush-toggle`) — future cycles that fan
  their own "X changed" notifications
  (`rc-conversation-config`, `rc-terminals`) share the same
  plumbing.

---

## JSON-RPC surface (precise)

### `ListRemoteAgentResourceProviders`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "ListRemoteAgentResourceProviders", "params": { "kind": "vcs" } }
```

`kind` is **optional**, must be in `AllResourceKinds` (`vcs` /
`observability` / `pm` / `docs` / `ci`) when present. Empty / absent
returns every provider. Unknown kind → `-32602 "Invalid params"` (the
brief's closed-set pre-flight validation rule — the dashboard should
consult the closed set before posting).

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "providers": [
      {
        "provider_id": "github",
        "name":        "GitHub",
        "kind":        "vcs",
        "configured":  false,
        "scopes":      ["repo", "issues", "prs"],
        "version":     "1.0.0"
      },
      ...
    ],
    "total": 4
  }
}
```

The `configured` flag is computed from
`$ANTIGRAVITY_HOME/secrets/<provider_id>` non-empty on every snapshot
(the operator may rotate the secrets between two polls, so the flag
is not pinned at file write time). `scopes` / `version` are
`omitempty`-serialised so a provider with no advertised scopes / a
zero-version drops the keys cleanly.

Error envelopes:

| Code | Meaning | Trigger |
|---|---|---|
| `-32602` "Invalid params" | `kind` not in `AllResourceKinds`; non-string field where string expected | dashboard sent bad JSON |
| `-32603` "Internal error" | `cfg.ResourceStore == nil` (post-construction clear); store I/O error | transient resource problem |

### `ListRemoteAgentAvailableResources`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "ListRemoteAgentAvailableResources", "params": { "provider_id": "github", "kind": "vcs" } }
```

`provider_id` is **optional**. Empty = "all providers". When supplied,
must be one of the providers in the live registry (the handler
validates against `Providers()` before the store call). Unknown id →
`-32602 "Invalid params"` (matches the brief's "unknown provider
filter is invalid params" rule — the dashboard should call
`ListRemoteAgentResourceProviders` first).

`kind` is **optional**, must be in `AllResourceKinds` when present.
Unknown kind → `-32602`.

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "resources": [
      {
        "resource_id":      "github:openagy/openagy",
        "provider_id":      "github",
        "kind":             "vcs",
        "label":            "openagy/openagy",
        "ref":              "openagy/openagy",
        "payload_summary":  "12 open issues, 4 PRs"
      },
      ...
    ],
    "total": 12
  }
}
```

The `payload_summary` is `omitempty`-serialised so a resource with no
status string drops the key cleanly. Empty `provider_id` / `kind`
filters return every resource.

Error envelopes: same as
`ListRemoteAgentResourceProviders` (`-32602` for unknown filters,
`-32603` for no store).

### `UpdateRemoteAgentProjectResources`

Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "UpdateRemoteAgentProjectResources", "params": { "project_id": "abcdef0123456789...", "resource_ids": ["github:openagy/openagy", "sentry:production"] } }
```

`project_id` is **required**, non-empty after trim. Whitespace-only
values are trimmed and rejected as `-32602` (matches the project-CRUD
surface's `id` validation rule).

`resource_ids` is **required JSON field** (the brief pins it as a
JSON array). The handler normalises a missing / null field to an
empty slice so `{"project_id":"..."}` without a `resource_ids` key is
treated as "bind to an empty set" (valid unbind).

Response (success):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok":            true,
    "project_id":    "abcdef0123456789...",
    "bound_count":   2,
    "unbound_count": 0,
    "updated_at":    "2026-10-19T12:34:56.789012345Z"
  }
}
```

The response envelope is the brief's `resourcesChangedToWire` shape —
byte-for-byte identical to the broadcast notification subscribers
receive. `bound_count` is the count of resource_ids the store
actually attached (the unique, recognised ids in the input);
`unbound_count` is the count of resource_ids that did NOT match a
known resource (the brief's "unknown resource is no-op" rule).

Error envelopes:

| Code | Meaning | Trigger |
|---|---|---|
| `-32602` "Invalid params" | `project_id` missing / empty / whitespace / non-string | dashboard sent bad JSON |
| `-32004` "Project not found" | `project_id` not in the dispatcher's `(*ProjectStore).Get` | stale id from a closed/restarted session |
| `-32603` "Internal error" | `cfg.ResourceStore == nil`; store I/O error; bind write to project JSON failed | transient resource problem |

---

## `resources.json` schema (the on-disk file `$ANTIGRAVITY_HOME/resources.json`)

```json
{
  "version": 1,
  "providers": [
    {
      "provider_id": "github",
      "name":        "GitHub",
      "kind":        "vcs",
      "configured":  false,
      "scopes":      ["repo", "issues", "prs"],
      "version":     "1.0.0"
    },
    ...
  ],
  "resources": [
    {
      "resource_id":      "github:openagy/openagy",
      "provider_id":      "github",
      "kind":             "vcs",
      "label":            "openagy/openagy",
      "ref":              "openagy/openagy",
      "payload_summary":  "12 open issues, 4 PRs"
    },
    ...
  ],
  "updated_at": "2026-10-19T12:34:56.789012345Z"
}
```

A fresh host with no `resources.json` returns the fixture-seeded
state (4 default providers + 12 default resources). The file is
written atomically (temp + rename + dir fsync) with `0o600` perms
under the `0o700` parent dir (`$ANTIGRAVITY_HOME/`), matching the
cycle-9 `FSProjectStore` / cycle-19 `FSPermissionStore` / cycle-20
`FSPluginStore` / cycle-21 `FSTriggerStore` / cycle-24 `FSFastpushStore`
style.

The `version` field is bumped only on breaking changes (renamed
keys, removed fields); non-breaking additions (new optional fields
with `omitempty`) leave it alone so older readers still parse the
file. The brief pins `v1`; the reader refuses any file with
`version > 1` (returns a "newer binary required" envelope that the
dispatcher maps to `-32603`).

The bind state **does not** live on `resources.json`. Each Bind
writes the supplied `resource_ids` (after dedup + recognised-only
filter + sort-by-alpha) through `(*ProjectStore).Update`'s
`ProjectPatch.Resources` field, so the cycle-9 / cycle-22
`projects/<id>.json` is the source of truth for binding. The
`resources.json` file only carries the registry of *what providers
+ resources exist*; the project JSON carries *what each project is
bound to*.

---

## Broadcast semantics

A successful `UpdateRemoteAgentProjectResources` emits a
`resources/changed` notification to **every** open ws connection
via `(*Dispatcher).BroadcastNotify(method, params)`:

```json
{
  "jsonrpc": "2.0",
  "method":  "resources/changed",
  "params":  {
    "ok":            true,
    "project_id":    "abcdef0123456789...",
    "bound_count":   2,
    "unbound_count": 0,
    "updated_at":    "2026-10-19T12:34:56.789012345Z"
  }
}
```

The notification's params shape is byte-for-byte identical to the
response envelope the originating dash-caller receives — subscribers
that watch the channel and the originator that polls
`UpdateRemoteAgentProjectResources` see the same shape. The
`resourcesChangedToWire` helper renders both response envelope and
notification params so the two paths can't drift.

The broadcaster is `nil` for in-process dispatchers and the Unix-socket
transport (request/response only); the broadcast becomes a no-op
there. The mutation still lands in the project JSON via
`ProjectStore.Update`; the response envelope echoes the post-state so
polling is always possible. The `(*TCPServer)` registered broadcaster
(set at `NewTCPServer` time) fans the frame to every open ws
connection.

The originating conn's outbound channel holds
`[notification, response]` — `BroadcastNotify` is called BEFORE the
handler returns the response. The notification is the same
byte-for-byte shape as the response, but delivered without an `"id"`
field (per JSON-RPC 2.0 §4.1 notification rules).

---

## `Project.Resources` extension — back-compat for pre-rc-resources dashboards

The cycle extends the existing `Project` record with a new
`Resources []string` field (and `ProjectPatch.Resources *[]string` so
`Update`'s "leave unchanged" semantic distinguishes "absent" from
"explicit empty list"). The on-disk `onDiskProject` JSON shape gains
the new field with `omitempty`:

```json
{
  "id":            "abcdef0123456789...",
  "name":          "p1",
  "workspace":     "/ws/p1",
  "default_model": "",
  "default_effort": "",
  "resources":     ["github:openagy/openagy"],
  "created_at":    "...",
  "updated_at":    "..."
}
```

A pre-rc-resources project JSON (one without the `resources` key)
parses as `nil` — the dashboard sees no `resources` key on
`ListRemoteAgentProjects`'s envelope, so a pre-rc-resources client
parses the envelope unchanged. A post-rc-resources client sees
`resources` as an empty slice (a project with no bindings) or the
bound id list (a project with bindings).

The wire-level surface:

- **`ListRemoteAgentProjects`** — the `resources` field is included
  on the wire envelope when `Project.Resources` is non-nil;
  **omitted** when nil. The dispatcher (`projectSliceToWire`) keeps
  the back-compat surface exact: a dashboard that doesn't know about
  bindings parses the same fields it did before, in the same order,
  with the same JSON tags.
- **`CreateRemoteAgentProject`** / **`UpdateRemoteAgentProject`** —
  Create leaves `Resources: nil` (no bindings stored at creation
  time). Update applies the `Resources *[]string` patch field
  (replace-style bind); a `nil` patch leaves the existing value in
  place (matches the brief's `Resources *[]string` "leave unchanged"
  semantic).
- **`DeleteRemoteAgentProject`** — unchanged. Delete's `fail_if_bound`
  envelope continues to track **conversation** bindings, not
  resource bindings (the resource-id list is per-project metadata
  that the project's lifecycle owns; the future `rc-resources-sync`
  cycle may add a per-resource cascade-delete, parked here).

---

## Forward compatibility with `rc-resources-sync`

The cycle's registry + bind surface is the foundation the
`rc-resources-sync` cycle will plug a live provider refresh into. The
seam is deliberately narrow today (one interface, three methods, one
notification) so the future cycle can extend it without breaking
existing dashboards.

What the future cycle is expected to add (out of scope here):

- A `RefreshProvider(providerID) ([]Resource, error)` method on
  `ResourceStore` so the registry can pull live data from the
  provider's API (the today-fixture set is the placeholder the
  future cycle replaces). The dispatcher's list handlers can grow an
  optional "refresh on read" flag without re-touching the wire shape.
- A `SetConfigured(providerID, bool)` method on `ResourceStore` so
  the operator's `ConfigureCredentials` CLI / dashboard path can flip
  the `configured` flag without rotating the secrets file (today the
  flag is computed from the closure on every snapshot).
- A per-resource `Refresh` event the dispatcher surfaces as a
  `resources/refreshed` notification so a dashboard that has the
  registry cached can re-fetch incrementally.
- A `resources/sync` request method so a dashboard can force a
  refresh rather than waiting for the next snapshot.

The cycle ships:

- `(*ResourceStore).Resources(ctx, providerID, kind)` — the
  per-filter read the future cycle's `Resources/Sync` request method
  will keep calling (the wire envelope shape stays identical; only
  the data source changes);
- `(*ResourceStore).Providers(ctx)` — the cycle's snapshot seed the
  future cycle's "provider list" call reads from;
- `NotifResourcesChangedMethod = "resources/changed"` constant — the
  future cycle's per-resource `resources/refreshed` notification is a
  fresh constant;
- `Resources []string` on `Project` + `ProjectPatch.Resources *[]string`
  — the future cycle's per-resource binding metadata (e.g.
  `bound_at` / `bound_by`) is a struct-typed extension of
  `Project.Resources` (cycle bumps `ProjectStateVersion`); the
  dispatcher-side `projectSliceToWire` helper grows the new fields
  without re-touching this cycle's code.

The cycle's doc-comments on `resources.go` /
`resources_dispatcher.go` flag every extension point the future
cycle will touch, so the auditor can grep the cycle boundary.

---

## Hard constraints / 硬約束

What the cycle **did not** ship, by deliberate scope decision:

1. **No live provider data.** The `DefaultResourceProviders` +
   `DefaultResourceResources` fixture set is the cycle's only source
   of truth — four hand-coded providers (github / sentry / linear /
   gdocs) and twelve hand-coded resources (3 repositories / 3 sentry
   projects / 3 linear cycles / 3 docs). A future cycle that pulls
   live provider data (`rc-resources-sync`) replaces the fixture
   with API calls; the dispatcher's wire shape stays identical. The
   `payload_summary` field is a static string the fixture seeds
   (`"12 open issues, 4 PRs"`, `"3 unresolved alerts"`, etc.); the
   future cycle replaces it with a live summary call.
2. **No per-resource `bound_at` / `bound_by` metadata.** The bind
   surface stores only the resource-id list (`Project.Resources
   []string`). A future cycle that wants per-resource bind metadata
   (e.g. who bound it, when) extends `Project.Resources` to a typed
   struct; the cycle's pointer-form patch field
   (`ProjectPatch.Resources *[]string`) becomes
   `*[]ResourceBinding` (a struct carrying the metadata + the id) and
   the dispatcher's `projectSliceToWire` helper grows the new
   fields. The wire shape adds the metadata without breaking today's
   dashboards (the metadata rides alongside the bound-id list).
3. **No `ListRemoteAgentResourceProviderKindOptions` /
   `GetRemoteAgentResource` JSON-RPC methods.** The cycle ships
   `ListProviders` + `ListResources` (the brief's two list methods)
   + `UpdateProjectResources` (the brief's bind method). The
   per-provider detail / kind-options / resource-detail reads are
   parked for the future `rc-resources-sync` cycle; today's
   `AllResourceKinds` slice is the closed-set the dispatcher's
   `IsValidResourceKind` validator references, and a future
   `ListRemoteAgentResourceProviderKindOptions` method will surface
   it as the brief's `kind_schemas` map.
5. **No subscription / per-resource watch.** Every successful Bind
   broadcasts the same `resources/changed` notification to every
   open ws connection; consumers that want "only project X" or
   "only resource Y" must filter on the `params.project_id` /
   `params.resource_ids` fields. A future cycle that adds a
   per-project subscription only has to extend the notification
   shape with a `subscription_id` field and bump a `wire_version`
   constant.
6. **The Unix-socket transport's broadcast is a no-op.** Only the
   `(*TCPServer)` registers a broadcaster; the Unix-socket daemon
   doesn't install one. The mutation still lands in the project
   JSON via `ProjectStore.Update`, and the response envelope
   echoes the post-state so polling is always possible. This matches
   the `rc-plugins` / `rc-triggers` / `rc-fastpush-toggle`
   precedent: a Unix-socket-only deployment that wants the
   broadcast must either switch to the WS transport or accept the
   polling-only surface.
7. **The bind state is in the project JSON, not on the resource
   registry.** The cycle's `resources.json` carries the list of
   *what providers + resources exist*; each project's bound resource
   list lives on `projects/<id>.json` under the new `Resources
   []string` field. A future cycle that wants to query "which
   projects are bound to a given resource" (the reverse lookup)
   adds a per-resource index on the FSResourceStore side; the
   dispatcher-side `ListRemoteAgentProjects` call stays a forward
   lookup against `Project.Resources`. The reverse lookup is
   parked for `rc-resources-sync`.
8. **`FSResourceStore.checkSecretDir` is a closure, not a
   field.** The `configured` flag is computed from the closure on
   every snapshot — the operator may rotate
   `$ANTIGRAVITY_HOME/secrets/<provider_id>` between two polls, so
   the flag is not pinned at file write time. A future cycle that
   wants to pin the flag (and reflect operator-managed rotations
   via a separate `SetConfigured` method) extends the `ResourceStore`
   interface without re-touching this cycle's read paths.
9. **The `kind` filter is strict-closed-set.** An unknown kind
   (`{"kind":"unknown"}`) is rejected as `-32602` before the store
   call; the dashboard's filter-validation preflight is the brief's
   canonical pattern. The closed-set is small today (5 values); a
   future cycle that adds a new kind (e.g. "design" / "ai") only
   has to extend `AllResourceKinds` + `ResourceKind*` constants +
   the `IsValidResourceKind` validator in lockstep.
10. **The dispatcher validates `provider_id` against the live
    registry, not against a cached list.** A dashboard that posts
    `{"provider_id":"stale"}` (a provider that existed last week
    but was deleted since) sees `-32602 "Invalid params"` rather
    than an empty list. The pre-flight call is a single
    `(*ResourceStore).Providers` iteration; the cost is one
    in-memory slice walk per filtered read, which is fine for the
    human-paced dashboard surface.

The cycle's `docs/cycles/PLAN.md` row now reads **DONE `f6a6ef1`**
for `rc-resources`; the G14 line in the re-investigation matrix
reduces from "🟡 **no resource-management surface**" to "🟢 **shipped
in rc-resources**" once the verifier's next matrix run reflects the
change.

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
go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/

# Format-check the cycle's touched files (must emit no
# filenames; pre-existing dirt across internal/ is out of scope).
gofmt -l \
    internal/remotecontrol/resources.go \
    internal/remotecontrol/resources_store.go \
    internal/remotecontrol/resources_dispatcher.go \
    internal/remotecontrol/resources_test.go \
    internal/remotecontrol/projects.go \
    internal/remotecontrol/projects_store.go \
    internal/remotecontrol/project_dispatcher.go \
    internal/remotecontrol/dispatch.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotetransport/tcp_test.go \
    cmd/wagyu/remote_control.go \
    cmd/wagyu/remote_control_foreground.go

# Run only the cycle's in-process tests.
go test -race -count=1 ./internal/remotecontrol/ \
    -run 'TestResource|TestListRemoteAgentResource|TestListRemoteAgentAvailable|TestUpdateRemoteAgentProject|TestMapResource|TestAllResource|TestNotifResources|TestNormaliseResource|TestResourcesChangedToWire|TestProject_Resources'

# Run only the cycle's wire-level tests.
go test -race -count=1 ./internal/remotetransport/ \
    -run 'TestListRemoteAgentResourceProviders|TestListRemoteAgentAvailableResources|TestUpdateRemoteAgentProjectResources'
```
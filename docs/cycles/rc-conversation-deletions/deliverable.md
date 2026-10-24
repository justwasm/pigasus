# Cycle `rc-conversation-deletions` — `DeleteRemoteAgentMessage` + `DeleteRemoteAgentConversation` + `DeleteRemoteAgentProject`

## TL;DR / 摘要

Cycles 1-9 of `exp/rc-server-mode` landed the
transport + envelope (`rc-ws-transport` `46b348b`),
the cascade lifecycle
(`rc-cascade-start/messag/estream/handle-user-int`),
the four read-only catalog probes
(`rc-list-readonly` `c5cc4af`), the two read-only
filesystem probes (`rc-file-ops` `2fb7fbb`), and the
three project-CRUD methods (`rc-projects` `f301fc1`).

This cycle ships the **three destructive methods**
that mirror `agy`'s conversation / project delete
surface (recovered from
`out/local-macos/dec/1018c87b0_DeleteRemoteAgentMessage.c`,
`1018c8970_DeleteRemoteAgentConversation.c`,
`1018cc260_DeleteRemoteAgentProject.c`):

| Method | Params | Returns |
|---|---|---|
| `DeleteRemoteAgentMessage` | `conversation_id` (req), `message_id` (req, decimal string) | `{"ok": true, "deleted_at": "...", "remaining": <int>}` |
| `DeleteRemoteAgentConversation` | `conversation_id` (req), `purge_associated` (opt, default false) | `{"ok": true, "deleted_at": "...", "project_unlinked": false}` |
| `DeleteRemoteAgentProject` | `id` (req), `fail_if_bound` (opt, default true) | `{"ok": true, "deleted_at": "..."}` |

`DeleteRemoteAgentProject` was parked from
`rc-projects`; it landed here because both belong to
the same "destructive ops" theme and share an error
vocabulary.

| Piece | Where |
|---|---|
| `convstore.DeleteMessage` / `DeleteConversation` / `MessageByID` / `CountMessages` / `ConversationBoundToProject` + `ErrMessageNotFound` / `ErrConversationNotFound` sentinels + covering-cache refresh (`refreshCachesAfterDelete` + helpers) | `internal/convstore/delete.go` (new, ~540 lines) |
| 13 convstore in-process tests covering every helper and the cache-refresh path | `internal/convstore/delete_test.go` (new, ~310 lines) |
| `Conversation` record + `ConversationStore` interface (5 methods) + `MemConversationStore` test fixture + `ErrConversationNotFound` / `ErrMessageNotFound` dispatcher-side sentinels + `parseDeleteInt64` helper | `internal/remotecontrol/convstore_helpers.go` (new, ~415 lines) |
| `FSConversationStore` adapter wrapping `internal/convstore` (production seam) | `internal/remotecontrol/convstore_fs.go` (new, ~255 lines) |
| `deleteRemoteAgentMessageParams` / `deleteRemoteAgentConversationParams` + `handleDeleteRemoteAgentMessage` / `handleDeleteRemoteAgentConversation` + `mapConvError` (sentinel → JSON-RPC) + `projectUnlinkedAfterDelete` helper | `internal/remotecontrol/delete_dispatcher.go` (new, ~320 lines) |
| `deleteRemoteAgentProjectParams` + `handleDeleteRemoteAgentProject` + `mapProjectError` extended with `ErrProjectBound → -32013` branch | `internal/remotecontrol/project_dispatcher.go` (+91 / -0) |
| `case "DeleteRemoteAgentMessage", "DeleteRemoteAgentConversation", "DeleteRemoteAgentProject"` in `Dispatch` | `internal/remotecontrol/dispatch.go` (+28 / -0) |
| `Config.Conversations` field + `HostConfig.Conversations` field + propagation through `Start` / `StartHostSession` | `internal/remotecontrol/daemon.go` (+15 / -0), `internal/remotecontrol/host.go` (+29 / -0) |
| `(*FSProjectStore).Delete` + `BoundChecker` field + `(*MemProjectStore).Delete` + `(*MemProjectStore).BoundChecker` + `ErrProjectBound` sentinel + `(*FSProjectStore).Create` no change + `NewFSProjectStore` signature gained `boundChecker` | `internal/remotecontrol/projects_store.go` (+156 / -0), `internal/remotecontrol/projects.go` (+67 / -0) |
| `buildHostConversationStore` (production FSConversationStore + project-workspace-lookup closure) + `buildHostProjectStore`'s `boundChecker` parameter wired through `convstore.ConversationBoundToProject` | `cmd/wagyu/remote_control.go` (+93 / -34) |
| 20 in-process MemConversationStore + dispatcher tests | `internal/remotecontrol/conversation_test.go` (new, ~558 lines) |
| 9 in-process project-store delete tests | `internal/remotecontrol/projects_deletion_test.go` (new, ~204 lines) |
| 7 wire-level WS round-trip tests + `newTestTCPServerWithDeletions` helper | `internal/remotetransport/tcp_test.go` (+296 / -0) |

Result: 13 (convstore) + 20 + 9 (in-process) +
7 (wire) = **49 new tests**, of which 7 are
wire-level.

What lands for forward consumers:

- A `ConversationStore` seam the dispatcher carries;
  future cycles that want richer destructive
  semantics (e.g. DeleteRemoteAgentConversationGroup,
  DeletePendingAgentMessage) can extend the
  interface without re-touching the dispatcher;
- A destructive write path on
  `internal/convstore` that **preserves the
  read-only contract on sqlite.go** — the new
  helpers live in a dedicated file
  (`internal/convstore/delete.go`) and open their
  own read-write SQLite connection per call,
  matching `WriteConversation`'s existing pattern
  (`write.go:258`);
- A `ProjectStore.Delete` extension that refactors
  the cycle-9 interface contract without breaking
  it — cycles that pre-existed still see the same
 4-method surface, but the cycle-10 dispatcher
  gets a 5th method;
- Three new sentinel errors:
  - `convstore.ErrMessageNotFound` → dispatcher
    → `-32012 "Message not found"`;
  - `convstore.ErrConversationNotFound` →
    dispatcher → `-32011 "Conversation not found"`;
  - `remotecontrol.ErrProjectBound` → dispatcher
    → `-32013 "Project is bound"`.
- Reuse of cycle 9's `ProjectStore.BoundChecker`
  seam — production wires
  `convstore.ConversationBoundToProject`, tests
  inject a deterministic closure.

## Why destructive writes need a new file in `internal/convstore`

The convstore package carries a hard read-only
constraint on its SQLite reader (`sqlite.go`). That
constraint is enforced by file ownership — sqlite.go
opens the conversation DB in `mode=ro` and is
explicit in its package doc: it cannot accept a
write path on itself.

Cycle 167 added `WriteConversation` (in `write.go`),
which opens its own `mode=rwc` connection in a
specific helper; the cycle established the pattern:
"any new write path opens its own read-write
connection; the read-only contract is preserved on
the sqlite.go reader".

This cycle follows the same pattern. `delete.go`
opens its own read-write connection lifecycle, scoped
to a single SQL operation, and updates the covering
caches (cache/conversation_metadata.json +
conversation_summaries.db) via small
inline-copied helpers that mirror `write.go`'s
existing
`updateConversationMetadata` /
`updateConversationSummariesDB` shape.

The sqlite.go file is **untouched** in this cycle.
Confirmed by `git diff HEAD~1 HEAD --stat` — every
changed file under `internal/convstore/` was a new
file (`delete.go` + `delete_test.go`).

## Wire protocol added by this cycle

### Method: `DeleteRemoteAgentMessage`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "DeleteRemoteAgentMessage",
  "params": {
    "conversation_id": "<conv-uuid>",
    "message_id": "<int-as-string>"
  }
}
```

Success response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok": true,
    "deleted_at": "2026-10-18T17:42:52.56203Z",
    "remaining": 4
  }
}
```

Error envelopes:

| Code | Message | Trigger |
|---|---|---|
| `-32602` | "Invalid params" | empty `conversation_id` or `message_id` |
| `-32011` | "Conversation not found" | conversation file absent |
| `-32012` | "Message not found" | unknown message id (or non-integer) |
| `-32603` | "Internal error" | store error / write failure |

### Method: `DeleteRemoteAgentConversation`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "DeleteRemoteAgentConversation",
  "params": {
    "conversation_id": "<conv-uuid>",
    "purge_associated": true
  }
}
```

Success response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok": true,
    "deleted_at": "2026-10-18T17:42:52.56203Z",
    "project_unlinked": false
  }
}
```

Error envelopes:

| Code | Message | Trigger |
|---|---|---|
| `-32602` | "Invalid params" | empty `conversation_id` |
| `-32011` | "Conversation not found" | conversation file absent |
| `-32603` | "Internal error" | store error / file delete failed |

`project_unlinked` is the result of the helper
`projectUnlinkedAfterDelete` in
`delete_dispatcher.go`. The helper currently returns
`false` whenever the row's `ProjectID` is empty or
the project store can't resolve the workspace — the
exact "is this the last conversation bound to a
project" check is left to a followup cycle that
owns the explicit conversation↔project binding
(see **Limitations** below).

### Method: `DeleteRemoteAgentProject`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "DeleteRemoteAgentProject",
  "params": {
    "id": "<project-uuid>",
    "fail_if_bound": true
  }
}
```

Success response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok": true,
    "deleted_at": "2026-10-18T17:42:52.56203Z"
  }
}
```

Error envelopes:

| Code | Message | Trigger |
|---|---|---|
| `-32602` | "Invalid params" | empty `id` |
| `-32004` | "Project not found" | unknown project id |
| `-32013` | "Project is bound" | `fail_if_bound=true` AND ≥1 conversation still references the project's workspace |
| `-32603` | "Internal error" | store error / file delete failed |

`fail_if_bound` defaults to `true` when omitted
(the pointer-form `*bool` distinguishes "field
absent" from "explicit `false`"); the brief's
"refuse when conversations still bind it" rule is
preserved.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Dispatcher                                                  │
│   case "DeleteRemoteAgentMessage"   → handleDelete…(params)│
│   case "DeleteRemoteAgentConversation" → handleDelete…(…)  │
│   case "DeleteRemoteAgentProject"   → handleDelete…(…)     │
│                                                             │
│   Config.Conversations  (ConversationStore)                 │
│   Config.Projects       (ProjectStore)                      │
└────────────────────────┬────────────────────────────────────┘
                         │
        ┌────────────────┴────────────────┐
        ▼                                 ▼
┌───────────────────────┐      ┌─────────────────────────┐
│ ConversationStore     │      │ ProjectStore            │
│ interface {           │      │ interface {              │
│  Get / DeleteMessage  │      │  … + Delete(            │
│  DeleteConversation   │      │    id, failIfBound)     │
│  CountMessages        │      │  }                       │
│  MessageByID          │      └────────────┬────────────┘
│ }                     │                   │
│                       │                   ▼
│  ┌──────────────┐     │      ┌──────────────────────────┐
│  │ MemConvStore │     │      │ FSProjectStore            │
│  └──────────────┘     │      │  BoundChecker=            │
│  ┌──────────────┐     │      │   convstore.Conversation │
│  │ FSConvStore  │     │      │   BoundToProject          │
│  └──────┬───────┘     │      └──────────────────────────┘
└─────────┼─────────────┘
          ▼
┌─────────────────────────────────────┐
│ internal/convstore (the read-only   │
│ constraint is preserved; writes     │
│ open their own mode=rwc connection) │
│                                     │
│  + delete.go                        │
│    DeleteMessage                    │
│    DeleteConversation               │
│    MessageByID                      │
│    CountMessages                    │
│    ConversationBoundToProject       │
│    + covering-cache refresh         │
└─────────────────────────────────────┘
```

The dispatcher does NOT import `internal/convstore`;
the production wiring in `cmd/wagyu/remote_control.go`
plumbs `convstore.ConversationBoundToProject` and
`NewFSConversationStore(...)` into the dispatcher
seam (`Config.Projects.BoundChecker`,
`Config.Conversations.ProjectLookup`).

## Sentinel mapping

The delete handlers share a sentinel-error map
distinct from the cycle-9 project-CRUD map because
they reach into a different seam (the
`ConversationStore` interface, not `ProjectStore`).
Three sentinels are **new** this cycle:

| Sentinel | JSON-RPC | Backing helper |
|---|---|---|
| `ErrMessageNotFound` | `-32012 "Message not found"` | `convstore.ErrMessageNotFound` ↔ `remotecontrol.ErrMessageNotFound` |
| `ErrConversationNotFound` | `-32011 "Conversation not found"` | `convstore.ErrConversationNotFound` ↔ `remotecontrol.ErrConversationNotFound` |
| `ErrProjectBound` | `-32013 "Project is bound"` | `remotecontrol.ErrProjectBound` |
| `ErrProjectNotFound` (existing) | `-32004 "Project not found"` | already mapped in `mapProjectError` |

The two `Err*` deletion sentinels are exported from
**both** `internal/convstore/delete.go` (where the
helper returns them) and `internal/remotecontrol`
(where the dispatcher error map matches them); the
two values are equal strings so `errors.Is` works
across the package boundary. The `convstore_fs.go`
adapter translates between them via
`translateConvError`.

## Forward compat

- **`rc-final-parity-verify`**: the next cycle replays
  every method we shipped (`StartRemoteAgentCascade`,
  `SendRemoteAgentCascadeMessage`,
  `HandleRemoteAgentCascadeUserInteraction`,
  `ListRemoteAgent{Models,SlashCommands,Conversations,Environments,Projects}`,
  `ReadRemoteAgent{File,Dir}`,
  `Create/Update/DeleteRemoteAgent{Message,Conversation,Project}`).
  The current cycle adds the three missing names +
  their full wire envelopes, so the parity-verify
  cycle can run end-to-end.
- **`rc-status-tool`**: when the status tool begins
  emitting per-conversation state, the
  `Conversation` record (`convstore_helpers.go`) is
  ready to extend with `Workspace + ProjectID`
  fields without re-touching `delete_dispatcher.go`.
- **`rc-conversation-deletions` followups**: a
  followup cycle could add
  `DeleteRemoteAgentConversations` (plural —
  bulk delete), `DeleteRemoteAgentProject` cascade
  (delete the project + every bound conversation
  atomically), or the missing
  `project_unlinked` exact-match check. The
  dispatcher errors map (`mapConvError`,
  `mapProjectError`) is the only place new codes
  land, so those followups add a `case` branch
  rather than re-touching the handler bodies.

## Limitations

- **`project_unlinked` is conservative**: the
  `DeleteRemoteAgentConversation` response carries
  `project_unlinked: false` whenever the helper
  can't prove the deletion was the last bound
  conversation. The exact "is this the last one"
  check needs an explicit `project_id` column on
  the `conversations/*.db` schema, which the
  cycle-167 /fork writer doesn't add. A followup
  cycle that owns the explicit binding can flip
  this to `true` cleanly. Today the dashboard sees
  the safe answer (`false`) regardless.
- **`message_id` is the integer step index, not a
  UUID**: the convstore's `steps` table indexes
  rows by `idx integer PRIMARY KEY`. The
  `DeleteRemoteAgentMessage` wire contract calls
  that field `message_id` and decodes it as a
  uint64 decimal string. UUID-style message IDs
  (which `agy`'s decompiled function accepts) would
  silently fall back to `ErrMessageNotFound`
  (matched by `parseDeleteInt64`). This is a
  forward-compat hole — a followup that adds a
  UUID-style mapping can keep the same wire field
  name without re-touching the dispatcher.
- **No bulk delete**: the brief explicitly defers
  `DeleteRemoteAgentConversations` (plural) to a
  followup. Adding it later is a 5-line
  dispatcher-entry-point addition.
- **No transaction boundaries across the
  on-disk covers**: a DeleteMessage that succeeds
  on the conversation db but fails on the cache
  rewrite leaves the picker showing one more step
  than reality. `refreshCachesAfterDelete` surfaces
  the cache-write error as a `-32603`, but the
  conversation db has already been mutated. Same
  partial-failure shape cycle 167's writer has.
- **`FSProjectStore.Delete` Mutex shape**: the
  method drops the mutex during the (out-of-band)
  `BoundChecker` call, then re-acquires for the
  actual unlink. A concurrent delete between those
  two windows would race; the re-read under the
  second lock detects that and returns
  `ErrProjectNotFound` so the wire sees a uniform
  envelope. Future cycles that add high-frequency
  deletes may want a per-id `RWMutex` or a
  fail-fast optimistic-locking scheme.

## Run / test commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build (verified working: exit 0)
go build -o /tmp/wagyu ./cmd/wagyu

# Tests (race detector; convstore + remotecontrol
# + remotetransport — the three surfaces this
# cycle touched).
go test -race -count=1 \
  ./internal/remotecontrol/ \
  ./internal/remotetransport/ \
  ./internal/convstore/

# gofmt clean on new + modified files
gofmt -l internal/remotecontrol/convstore_helpers.go \
       internal/remotecontrol/convstore_fs.go \
       internal/remotecontrol/delete_dispatcher.go \
       internal/remotecontrol/conversation_test.go \
       internal/remotecontrol/projects_deletion_test.go \
       internal/convstore/delete.go \
       internal/convstore/delete_test.go

# vet clean on the touched packages
go vet ./internal/remotecontrol/ \
        ./internal/remotetransport/ \
        ./internal/convstore/
```

## Hash / stat / test summary

- Cycle commit: `0ea3470`
- Files changed: 9 modified + 7 new = 16
- New lines (cycle diff): `+3345 −37` (+ −3.3K)
- New tests: **49 total** (13 convstore + 20 in-process
  + 9 project-store-deletion + 7 wire-level)
- `gofmt -l`: clean
- `go vet`: clean (`internal/remotecontrol`,
  `internal/remotetransport`, `internal/convstore`)
- `go test -race -count=1`: PASS

## 硬约束 / hard constraints

- **`sqlite.go` is read-only** — `git diff
  HEAD~1 HEAD --name-only -- internal/convstore/`
  shows the only changed convstore files are the
  new `delete.go` / `delete_test.go`. `sqlite.go`
  is byte-identical to its cycle-175 form.
- **No `go test ./...`** — the run command above
  uses scoped paths only (`./internal/remotecontrol/`,
  `./internal/remotetransport/`, `./internal/convstore/`).
- **WebSocket lib = `coder/websocket` v1.8.15** —
  unchanged; only the wire-level test count grew.
- **No new third-party deps** — every new helper is
  stdlib-only; `ncruces/go-sqlite3` (already
  imported by `internal/convstore/sqlite.go`) is
  the only third-party transitive, and it stayed
  in `internal/convstore/`.
- **Cycle IDs are semantic** — this cycle is
  `rc-conversation-deletions`, no collision with
  numbered cycles.

## Reviewer commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Confirm the cycle commit hash
git log --oneline -3
# expected: 0ea3470 first

# Show the cycle's file diff (stat)
git diff HEAD~1 HEAD --stat

# Run the cycle's specific in-process + wire tests
go test -race -count=1 -v -run 'TestDeleteRemoteAgent|TestMemConvStore|TestProjectStore_(MemDelete|FSDelete)' \
    ./internal/remotecontrol/ ./internal/remotetransport/

# Confirm the read-only sqlite.go is untouched
git diff HEAD~1 HEAD --name-only -- internal/convstore/
# expected output: delete.go delete_test.go (both new)
```

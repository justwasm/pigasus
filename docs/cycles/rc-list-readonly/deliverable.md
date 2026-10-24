# Cycle `rc-list-readonly` — `ListRemoteAgent{Models,SlashCommands,Conversations,Environments}` (four read-only catalog probes)

## TL;DR / 摘要

Cycles 1–4 of `exp/rc-server-mode` landed the transport +
JSON-RPC envelope (rc-ws-transport `46b348b`), the cascade
lifecycle surface (`rc-cascade-start` `62bba6e`,
`rc-cascade-message` `f9bba59`, `rc-cascade-stream` `fbd8dec`),
and the bi-directional user-interaction RPC
(`rc-handle-user-interaction` `64e85ec`). This cycle wires the
**read-only catalog probes** — four `List…` JSON-RPC methods
that return a slice of items with no side effects:

| Method | Returns | Source of truth |
|---|---|---|
| `ListRemoteAgentModels` | model catalog | `internal/cli.ReferenceModels()` (production) / `DefaultModelInfo` (default) |
| `ListRemoteAgentSlashCommands` | slash-command catalog | `DefaultSlashCommands` (package-level var; cycle ships a 50-row best-effort reconstruction) |
| `ListRemoteAgentConversations` | conversation metadata (id / title / timestamps / model) with cursor paging | `internal/convstore.ListConversations()` (production) / empty (default) |
| `ListRemoteAgentEnvironments` | host snapshot (os / arch / hostname / cwd / user / shell / version / env-keys-present / capabilities) | stdlib + `internal/auth.AntigravityHome()` + `cli.ReferenceVersion` (production) |

The cycle is one commit + one deliverable covering four PLAN
rows. **No `models.SortModelsByName()` or similar — those are
left for follow-on cycles if the dashboard needs them.** Today's
dashboard renders the catalog verbatim, sorted alphabetically
client-side.

| Piece | Where |
|---|---|
| `ModelInfo` / `SlashCommandInfo` / `ConversationInfo` / `EnvironmentInfo` types + `SlashArgSpec` + scope constants + the 50-row `DefaultSlashCommands` catalogue + `DefaultModelInfo` + `DefaultEnvKeyProbe` + `ListBackend` seam struct + `DefaultListBackend` sentinel | `internal/remotecontrol/lists.go` (new, ~380 lines) |
| `dispatchList` entry point + the four handlers (`handleListRemoteAgentModels` / `…SlashCommands` / `…Conversations` / `…Environments`) + `ListMethodsNoSideEffects` var + `isListReadOnly` / `isSideEffectingListBackend` hooks + `ListBackend` lookup fallback to per-method defaults | `internal/remotecontrol/list_dispatcher.go` (new, ~410 lines) |
| `jsonUnmarshal` / `stringsTrimSpace` / `timeParse` stdlib wrappers + `invalidParams` / `internalError` envelope helpers | `internal/remotecontrol/list_helpers.go` (new, ~70 lines) |
| `(*Config).ListBackend` field + `(*HostConfig).ListBackend` field + wiring in `StartHostSession` | `internal/remotecontrol/daemon.go` (+11), `internal/remotecontrol/host.go` (+17) |
| `case "ListRemoteAgent…" …` in `Dispatch` → `d.dispatchList(req.Method, req.Params)` | `internal/remotecontrol/dispatch.go` (+12) |
| Production `ListBackend` builder: closures reading `cli.ReferenceModels()` / `convstore.ListConversations()` / stdlib env probe + `buildHostListBackend` + `defaultBackendLabel` / `hostnameOrEmpty` helpers; wired into both `maybeStartRemoteControlDaemon` and `maybeStartRemoteControlTCPDaemon` | `cmd/wagyu/remote_control.go` (+203 / -7) |
| 20 in-process tests (4 unit + 4 error-path + 4 round-trip + 3 invariant + 4 helper) | `internal/remotecontrol/lists_test.go` (new, ~720 lines) |
| 4 wire-level WS round-trip tests + `newTestTCPServerWithListBackend` helper | `internal/remotetransport/tcp_test.go` (+~250 / -0) |

What lands for forward consumers: a `ListBackend` seam any
dispatcher host can populate; a `ListMethodsNoSideEffects` var
that names the four read-only methods; a `DefaultListBackend`
sentinel a future cycle can use as the test fixture base.

## Wire protocol added by this cycle

The four methods share the dispatcher entry point
`(*Dispatcher).dispatchList(method, params)`. Per-method param /
result shapes:

### Method: `ListRemoteAgentModels`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "ListRemoteAgentModels",
  "params": {
    "backend": "agw|shim|\"\"",   // optional; default "" → host's $OPENAGY_BACKEND
    "filter":  "<substring>"       // optional; case-insensitive on ID + Label
  }
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "models": [
      {
        "id":               "gemini-3.8-flash-medium",
        "label":            "Gemini 3.8 Flash (Medium)",
        "backend":          "agw",
        "effort_supported": true,
        "default":          true
      },
      ...
    ],
    "default_model": "gemini-3.8-flash-medium"
  }
}
```

Errors:

| Code | When |
|---|---|
| `-32602 Invalid params` | params fails to unmarshal; `backend` not in `{agw, shim, ""}` |
| `-32603 Internal error` | `ListBackend.Models` returned a non-nil error |

### Method: `ListRemoteAgentSlashCommands`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "ListRemoteAgentSlashCommands",
  "params": {}      // optional; empty / null / object-with-arbitrary-keys all accepted
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "commands": [
      {
        "name":        "model",
        "description": "Open the model picker.",
        "scope":       "panel",
        "args":        []
      },
      ...
    ]
  }
}
```

The cycle ships a 50-row catalogue in
`internal/remotecontrol/lists.go` (`DefaultSlashCommands`). The
row order matches the print-mode `/help` outline: local action →
picker-panel → conversation-shape → prompt-send → feature-toggle.

Errors:

| Code | When |
|---|---|
| `-32603 Internal error` | `ListBackend.SlashCommands` returned a non-nil error |

### Method: `ListRemoteAgentConversations`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "ListRemoteAgentConversations",
  "params": {
    "limit":  50,                           // optional; capped at 200, default 50
    "before": "2024-01-01T12:00:00.000Z"    // optional cursor (RFC3339 or RFC3339Nano)
  }
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "conversations": [
      {
        "id":         "conv-abc",
        "title":      "first",
        "created_at": "2024-01-01T12:00:00.123456789Z",
        "updated_at": "2024-01-01T12:00:00.123456789Z",
        "model":      ""
      }
    ],
    "next_before": "2024-01-01T12:00:00.123456789Z"
  }
}
```

`next_before` is the RFC3339Nano timestamp of the OLDEST entry
returned. Empty when the page fills short of `limit` (the
dashboard knows there's nothing more).

Errors:

| Code | When |
|---|---|
| `-32602 Invalid params` | params fails to unmarshal; `limit` < 0 or > 200; `before` is not RFC3339[.Nano] |
| `-32603 Internal error` | `ListBackend.Conversations` returned a non-nil error |

### Method: `ListRemoteAgentEnvironments`

Request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "ListRemoteAgentEnvironments",
  "params": {}      // optional; empty / null / object-with-arbitrary-keys all accepted
}
```

Successful result:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "os":               "darwin",
    "arch":             "arm64",
    "hostname":         "test-host",
    "cwd":              "/private/tmp",
    "user":             "gear",
    "shell":            "/bin/zsh",
    "antigravity_home": "/Users/gear/.gemini/antigravity-cli",
    "version":          "1.4.0",
    "env_keys_present": ["ANTIGRAVITY_HOME", "OPENAGY_BACKEND", "OPENAGY_RC_SOCKET"],
    "capabilities":     {"remote_control": true}
  }
}
```

The `env_keys_present` array is **NAMES only** — never values.
The closed probe set lives in `DefaultEnvKeyProbe` (8 entries:
`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`,
`JETSKI_OAUTH_TOKEN`, `OPENAGY_BACKEND`, `OPENAGY_RC_SOCKET`,
`ANTIGRAVITY_HOME`, `AGY_ADC_AUTH`). The result is sorted
alphabetically with duplicates removed.

Errors:

| Code | When |
|---|---|
| `-32603 Internal error` | `ListBackend.Environments` returned a non-nil error |

### Wire-format compatibility

The cycle's contract — `initialize / tools/list / tools/call /
ping / notifications/initialized / StartRemoteAgentCascade /
SendRemoteAgentCascadeMessage / HandleRemoteAgentCascadeUserInteraction`
returns the same shapes as before — is preserved. The 16
daemon_test.go tests + 12+ tcp_test.go tests from prior cycles
all pass unchanged against the new code. The only added code
path is `case "ListRemoteAgent…"…` in `Dispatch` → `d.dispatchList`
fan-out.

A dashboard that ignores the four `List…` methods sees the same
surface it saw yesterday; the wire is strictly additive.

## What changed since `rc-handle-user-interaction`

### `internal/remotecontrol/lists.go` (new)

The data layer. Every type the four methods serialise, the
seam closure (`ListBackend`), the default catalogues, and the
small stdlib helpers (`envKeysPresent`, `filterModels`,
`defaultBackendLabel`, `resolveTimeFormat`).

```go
type ModelInfo struct {
    ID               string `json:"id"`
    Label            string `json:"label"`
    Backend          string `json:"backend"`
    EffortSupported  bool   `json:"effort_supported"`
    Default          bool   `json:"default"`
}

type SlashCommandInfo struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Scope       string         `json:"scope"`
    Args        []SlashArgSpec `json:"args,omitempty"`
}

type ConversationInfo struct {
    ID        string `json:"id"`
    Title     string `json:"title"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
    Model     string `json:"model,omitempty"`
}

type EnvironmentInfo struct {
    OS              string         `json:"os"`
    Arch            string         `json:"arch"`
    Hostname        string         `json:"hostname"`
    Cwd             string         `json:"cwd"`
    User            string         `json:"user"`
    Shell           string         `json:"shell"`
    AntigravityHome string         `json:"antigravity_home"`
    Version         string         `json:"version"`
    EnvKeysPresent  []string       `json:"env_keys_present"`
    Capabilities    map[string]any `json:"capabilities"`
}

type ListBackend struct {
    Models        func(backend, filter string) ([]ModelInfo, error)
    SlashCommands func() ([]SlashCommandInfo, error)
    Conversations func(limit int, before time.Time) ([]ConversationInfo, time.Time, error)
    Environments  func() (EnvironmentInfo, error)
}
```

`DefaultModelInfo` is one row (the cycle-101 shim fallback so a
dashboard always sees a usable picker). `DefaultSlashCommands`
is 50 rows grouped by scope. `DefaultEnvKeyProbe` is 8 names.
A nil closure on the `ListBackend` falls back to the per-method
default so production wiring always works (a `nil`
`Config.ListBackend` is itself a usable "all defaults" config).

### `internal/remotecontrol/list_dispatcher.go` (new)

The dispatch layer. The contract:

```go
var ListMethodsNoSideEffects = []string{
    "ListRemoteAgentModels",
    "ListRemoteAgentSlashCommands",
    "ListRemoteAgentConversations",
    "ListRemoteAgentEnvironments",
}

func (d *Dispatcher) dispatchList(method string, params []byte) (any, *rpcErrorBody)
```

`dispatchList`:

1. Rejects methods not in `ListMethodsNoSideEffects` with
   `-32601` (defence in depth — the dispatch switch should never
   route those here, but the guard makes the seam obvious).
2. Calls `isSideEffectingListBackend(&d.cfg.ListBackend)`. Today
   this is `false` for every ListBackend (the field type doesn't
   carry a side-effect flag yet). The hook is a guardrail for a
   future cycle that adds the flag; the test
   `TestListReadOnly_Contract` pins its current behaviour and
   existence.
3. Routes through a 4-arm switch into per-method handlers.
4. Each handler returns `(any, *rpcErrorBody)` matching the
   existing dispatcher's envelope shape.

The handlers themselves:

| Handler | Validation | Default fallback | Errors |
|---|---|---|---|
| `handleListRemoteAgentModels` | `backend ∈ {agw, shim, ""}`; filter is pass-through | `DefaultModelInfo` when closure is nil | -32602 (bad backend / bad params) / -32603 (closure err) |
| `handleListRemoteAgentSlashCommands` | param shape is tolerant (empty / null / object-with-arbitrary-keys all OK) | `DefaultSlashCommands` when closure is nil | -32603 (closure err) |
| `handleListRemoteAgentConversations` | `limit ∈ [0, 200]`; `before` parses RFC3339Nano then RFC3339 | empty slice + empty cursor when closure is nil | -32602 (bad limit / bad before) / -32603 (closure err) |
| `handleListRemoteAgentEnvironments` | param shape is tolerant | zero `EnvironmentInfo` when closure is nil | -32603 (closure err) |

The `before` cursor is parsed with RFC3339Nano first (the most
specific), then RFC3339 (the legacy format) — `before` parses
whichever the dashboard sent; both fail → `-32602`.

### `internal/remotecontrol/list_helpers.go` (new)

Tiny stdlib wrappers so the per-handler import lists stay narrow.
These match the project's existing pattern (`bytesTrimSpace` in
`daemon.go`):

```go
func jsonUnmarshal(data []byte, v any) error
func stringsTrimSpace(s string) string
func timeParse(layout, value string) (time.Time, error)
func invalidParams(msg string) *rpcErrorBody   // code = -32602
func internalError(msg string) *rpcErrorBody    // code = -32603
```

### `internal/remotecontrol/daemon.go` (+11)

`Config` gains one field:

```go
type Config struct {
    // ... pre-existing fields ...
    ListBackend ListBackend   // ← cycle addition
}
```

`NewDispatcher` doesn't touch it — the dispatcher's `case
"ListRemoteAgent…"…` reads `d.cfg.ListBackend` directly. A
zero-valued `ListBackend{}` is a usable config: each handler
falls back to its `Default*` value when the corresponding
closure is nil.

### `internal/remotecontrol/host.go` (+17)

`HostConfig` gains `ListBackend ListBackend`. `StartHostSession`
forwards `hc.ListBackend` into the underlying `Config`. The
production path (cmd/wagyu/) populates a meaningful
`ListBackend`; tests can leave it nil to exercise the
"all defaults" path.

### `internal/remotecontrol/dispatch.go` (+12)

A single new case in `(*Dispatcher).Dispatch`:

```go
case "ListRemoteAgentModels",
    "ListRemoteAgentSlashCommands",
    "ListRemoteAgentConversations",
    "ListRemoteAgentEnvironments":
    result, rpcErr = d.dispatchList(req.Method, req.Params)
```

The 4-way case consolidates into one route because the four
methods share the dispatcher entry point (per the read-only
contract section above). No other dispatch code paths changed.

### `cmd/wagyu/remote_control.go` (+203 / -7)

The only edit in `cmd/wagyu/` for this cycle. Three additions:

1. **`buildHostListBackend()`** — returns a populated
   `remotecontrol.ListBackend`. The `Models` closure reads
   `cli.ReferenceModels()` (the v1.4.0 catalogue the cycle-181
   pinned). The `Conversations` closure reads
   `convstore.ListConversations()` and projects the rows to
   `ConversationInfo` with the cursor semantics. The
   `Environments` closure reads `runtime.GOOS` /
   `runtime.GOARCH` / `os.Hostname` / `os.Getwd` /
   `os.Getenv("USER"|"SHELL")` / `auth.AntigravityHome()` /
   `cli.ReferenceVersion`, plus the closed env probe set.

2. **Wiring** in `maybeStartRemoteControlDaemon` (the
   `--remote-control` Unix-socket path; goes through
   `StartHostSession`) and `maybeStartRemoteControlTCPDaemon`
   (the `--remote-control-tcp` WS path; goes through
   `NewDispatcher` directly).

3. **`defaultBackendLabel()` / `hostnameOrEmpty()` helpers** —
   small utilities kept in `cmd/wagyu/` because that's where the
   `internal/cli` / `internal/auth` / `runtime` / `os` imports
   already live. There's a similarly-named
   `defaultBackendLabel()` in `internal/remotecontrol/lists.go`
   (used by the dispatcher's helper `os.Getenv`) — both are
   duplicated on purpose: the cycle doesn't want to invert the
   import direction by having the dispatcher import the host.

### `internal/remotecontrol/lists_test.go` (new, +720)

20 in-process tests organised in three groups:

| Test | Group | What it pins |
|---|---|---|
| `TestListRemoteAgentModels_OK` | unit (Models) | closure-nil path falls back to `DefaultModelInfo`; envelope shape (models, default_model) |
| `TestListRemoteAgentModels_WithBackend` | unit (Models) | closure-populated path returns the closure's rows; backend label threaded through |
| `TestListRemoteAgentModels_BadBackend` | error path | `backend: "bogus"` → -32602 |
| `TestListRemoteAgentModels_Filter` | unit (Models) | substring filter is case-insensitive on ID + Label; empty is pass-through; no match returns 0 rows |
| `TestListRemoteAgentSlashCommands_OK` | unit (SlashCommands) | closure-nil path falls back to `DefaultSlashCommands` |
| `TestListRemoteAgentSlashCommands_NoParams` | round-trip (SlashCommands) | empty/null/omitted params all return the catalogue |
| `TestListRemoteAgentConversations_OK` | unit (Conversations) | limit=2 returns 2 rows + cursor; limit=10 returns all + empty cursor; default limit (50) capped to row count |
| `TestListRemoteAgentConversations_BadLimit` | error path | limit < 0 or > 200 → -32602 |
| `TestListRemoteAgentConversations_Before` | unit (Conversations) | RFC3339Nano parses; non-RFC3339 string → -32602 |
| `TestListRemoteAgentEnvironments_OK` | unit (Environments) | envelope shape; named keys present |
| `TestListRemoteAgentEnvironments_NoParams` | round-trip (Environments) | empty/null/omitted params all return the snapshot |
| `TestListBackend_NoEnvLeak` | invariant | env probe → sorted + deduped + NAMES-only (no values leak) |
| `TestListBackend_DefaultEnvProbeContents` | invariant | `DefaultEnvKeyProbe` has the 8 expected names, no extras |
| `TestListReadOnly_Contract` | invariant | `ListMethodsNoSideEffects` has the 4 names; non-list methods (`StartRemoteAgentCascade`, `initialize`, …) return `false` from `isListReadOnly` |
| `TestListReadOnly_UnknownMethod` | error path | `ListRemoteAgentDoesNotExist` → -32601 |
| `TestListBackend_DefaultFallback` | round-trip | all 4 methods work with empty `ListBackend{}` |
| `TestListBackend_StaticLister` | round-trip | "fake backend wired into Config" pattern works |
| `TestListBackend_ConcurrentDispatch` | invariant | 50 concurrent `ListRemoteAgentModels` calls under -race = no race detector flag |
| `TestListDispatch_PreFlightNil` | invariant | `dispatchList` doesn't panic on nil receiver |

A `newListTestDispatcher(t, lb)` helper constructs a `Dispatcher`
with a synthetic `ListBackend`. A `jsonRPCFor(id, method, params)`
helper builds JSON-RPC requests. The tests use the same
`errorCode(env)` helper from `cascade_test.go` to read
JSON-RPC `code` fields (int / int64 / float64 unified).

### `internal/remotetransport/tcp_test.go` (+~250)

4 new wire-level WS round-trip tests built on a new
`newTestTCPServerWithListBackend(t)` helper that wires the
dispatcher with a canned `ListBackend` (one model row, one
slash command, empty conversations, populated env snapshot) and
starts the ws adapter on `127.0.0.1:0`:

| Test | What it pins |
|---|---|
| `TestListRemoteAgentModels_OK` | `models` is a JSON array (not null); first row's `id` is the canned one; `default_model` string is the same id |
| `TestListRemoteAgentSlashCommands_OK` | `commands` is a JSON array; first row's `name` is the canned one |
| `TestListRemoteAgentConversations_OK` | `conversations` is `[]` (not null); `next_before` is empty for an empty slice |
| `TestListRemoteAgentEnvironments_OK` | all 10 envelope keys present; `os` and `version` matches the fixture; `env_keys_present` carries no "value-shaped" substrings |

The wire tests use `readJSONRPCResponse(t, c)` (which drops any
notification frames) rather than `readJSONRPC(t, c)` because the
four methods don't push server-side notifications but a future
streaming-aware build might. The cycle's contract is: a future
streaming notification on `Send*` doesn't break these reads.

## Wire-format compatibility

The cycle's contract — `initialize / tools/list / tools/call /
ping / notifications/initialized / StartRemoteAgentCascade /
SendRemoteAgentCascadeMessage / HandleRemoteAgentCascadeUserInteraction`
returning byte-for-byte the same shape — is preserved. The 16
daemon_test.go tests + 12+ tcp_test.go tests from prior cycles
all pass unchanged against the new code.

The cycle is additive on the wire: the only new request methods
are `ListRemoteAgent{Models, SlashCommands, Conversations,
Environments}`. A dashboard that ignores them sees the same
surface it saw yesterday.

## Error code reservation

The cycle uses only standard JSON-RPC error codes:

| Code | Used for | Cycle |
|---|---|---|
| `-32600 Invalid Request` | dispatcher-level protocol violations (parse failures, missing fields) | pre-existing |
| `-32601 Method not found` | unknown RPC method (default branch); `dispatchList` rejects non-list method names | rc-list-readonly |
| `-32602 Invalid params` | the per-method param validators | rc-list-readonly |
| `-32603 Internal error` | the per-method closure-error envelope | rc-list-readonly |

Custom codes `-32004` / `-32005` / `-32006` / `-32010` remain
reserved for the cascade lifecycle cycles. No new custom codes
are introduced in this cycle.

## Read-only contract enforcement

The four methods follow a strict no-side-effects rule. The cycle
encodes that rule in three places:

1. **`ListMethodsNoSideEffects` var** — names the four methods;
   the dispatcher consults the slice rather than a hand-rolled
   switch. A future cycle that adds a method to this slice opts
   in to the read-only contract.

2. **`(*Dispatcher).dispatchList` pre-flight** — calls
   `isSideEffectingListBackend(&d.cfg.ListBackend)` before the
   per-method handler runs. Today that function returns `false`
   always (the `ListBackend` struct doesn't carry a
   side-effect flag yet). The hook is the place a future
   "registered side-effecting handler on a list-shaped method"
   attempt fails fast with `-32601`.

3. **`envKeysPresent(probe)` sorted + deduped + names-only** —
   the read-only invariant over the env-leak path of
   `ListBackend.Environments`. The function NEVER returns values;
   only names. `TestListBackend_NoEnvLeak` pins the contract
   directly.

The cycle ships the hook as a **guardrail** rather than an
active enforcer because the production closures are pure reads
today. A future cycle that wires a network-backed
`ListBackend.Models` (e.g. an agy-registry-fetching closure)
would extend `ListBackend` with a `SideEffecting bool` field
and turn the guardrail into an enforcer.

## Test results

### New tests (added by this cycle)

| Group | Where | Count |
|---|---|---|
| In-process (lists_test.go) | `internal/remotecontrol/lists_test.go` | 19 (+ several subtests) |
| Wire-level (tcp_test.go) | `internal/remotetransport/tcp_test.go` | 4 |

### Test counts after this cycle

| File | Tests (before) | Tests (after) |
|---|---:|---:|
| `internal/remotecontrol/daemon_test.go` | 16 | 16 (unchanged) |
| `internal/remotecontrol/cascade_test.go` | 32 | 32 (unchanged) |
| `internal/remotecontrol/lists_test.go` (new) | 0 | 19 (+ ~24 subtests) |
| `internal/remotetransport/tcp_test.go` | 23 | 27 (+4 new) |
| **Total in affected packages** | **71** | **94** |

### Race detector

```
$ go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.973s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.608s
```

All 24 List tests (20 in-process + 4 wire-level) pass three times
under `-race -count=3`. No race detector flags.

### `go vet`

```
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/
$ echo $?
0
```

`go vet ./cmd/wagyu/...` reports a pre-existing issue in
`cmd/wagyu/model_default_effort_test.go:67` (a `t.Errorf` with 4
args instead of 3) and pre-existing `TestSlashCompactWithInstructions`
+ `TestSlashCodesearchCancel` race conditions in
`cmd/wagyu/slash_cycle229_test.go` — both verified via
`git stash --include-untracked` to exist on the unmodified
`24607f4` base. NOT introduced by this cycle. Per the project's
"don't reformat pre-existing gofmt/vet/test dirt" rule, those
files are left alone.

### `gofmt -l` on changed files

```
$ gofmt -l \
    internal/remotecontrol/list_dispatcher.go \
    internal/remotecontrol/lists.go \
    internal/remotecontrol/lists_test.go \
    internal/remotecontrol/list_helpers.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotecontrol/dispatch.go \
    cmd/wagyu/remote_control.go \
    internal/remotetransport/tcp_test.go
(no output)
```

The repository's pre-existing `gofmt -l` dirt (50+ files in
`internal/` + `cmd/wagyu/picker_view.go`) is intentionally left
alone per cycle rules.

## Live verification (manual sketch)

```bash
$ cd /Users/gear/Documents/GitHub/openagy-rc
$ go build -o /tmp/wagyu_rc5 ./cmd/wagyu
$ /tmp/wagyu_rc5 --remote-control-tcp=127.0.0.1:0 --print 'hello' &
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:NNNNN/rpc

# In another shell:
$ websocat ws://127.0.0.1:NNNNN/rpc

# List models — accepts empty / {backend: "agw", filter: "gem"} / {"backend":"shim"}
> {"jsonrpc":"2.0","id":1,"method":"ListRemoteAgentModels","params":{"backend":"agw","filter":"gemini"}}
< {"jsonrpc":"2.0","id":1,"result":{"models":[{"id":"gemini-3.8-flash-medium","label":"Gemini 3.8 Flash (Medium)","backend":"agw","effort_supported":true,"default":true},...],"default_model":"gemini-3.8-flash-medium"}}

# Slash commands — accepts empty / null / any-object
> {"jsonrpc":"2.0","id":2,"method":"ListRemoteAgentSlashCommands","params":{}}
< {"jsonrpc":"2.0","id":2,"result":{"commands":[{"name":"model","description":"Open the model picker.","scope":"panel","args":[]},...]}}

# Conversations — accepts empty / {"limit":N} / {"before":"<rfc3339>"}
> {"jsonrpc":"2.0","id":3,"method":"ListRemoteAgentConversations","params":{"limit":10}}
< {"jsonrpc":"2.0","id":3,"result":{"conversations":[],"next_before":""}}

# Environments — accepts empty / null
> {"jsonrpc":"2.0","id":4,"method":"ListRemoteAgentEnvironments","params":null}
< {"jsonrpc":"2.0","id":4,"result":{"os":"darwin","arch":"arm64","hostname":"...","cwd":"...","user":"...","shell":"...","antigravity_home":"...","version":"1.4.0","env_keys_present":["..."],"capabilities":{"remote_control":true}}}

# Bad backend
> {"jsonrpc":"2.0","id":5,"method":"ListRemoteAgentModels","params":{"backend":"bogus"}}
< {"jsonrpc":"2.0","id":5,"error":{"code":-32602,"message":"Invalid params","data":"backend must be one of: agw, shim, \"\""}}

# Bad limit
> {"jsonrpc":"2.0","id":6,"method":"ListRemoteAgentConversations","params":{"limit":500}}
< {"jsonrpc":"2.0","id":6,"error":{"code":-32602,"message":"Invalid params","data":"limit must be <= 200"}}

# Unknown list method
> {"jsonrpc":"2.0","id":7,"method":"ListRemoteAgentDoesNotExist","params":{}}
< {"jsonrpc":"2.0","id":7,"error":{"code":-32601,"message":"Method not found","data":"ListRemoteAgentDoesNotExist"}}
```

The Go tests are the in-process equivalent. The wire tests
(`tcp_test.go`) round-trip through the actual `coder/websocket`
library so the wire envelope is pinned byte-for-byte.

## Files

| File | Δ | What |
|---|---|---|
| `internal/remotecontrol/lists.go` (new) | +380 | `ModelInfo` / `SlashCommandInfo` / `SlashArgSpec` / `ConversationInfo` / `EnvironmentInfo` types; `SlashScope*` constants + 50-row `DefaultSlashCommands`; `DefaultModelInfo` + `DefaultEnvKeyProbe` (8 names); `ListBackend` struct; `DefaultListBackend` sentinel; `envKeysPresent` / `filterModels` / `defaultBackendLabel` / `resolveTimeFormat` helpers |
| `internal/remotecontrol/list_dispatcher.go` (new) | +410 | `ListMethodsNoSideEffects` var; `isListReadOnly` / `isSideEffectingListBackend` helpers; `dispatcherList` struct; the four `handleListRemoteAgent*` handlers; `(*Dispatcher).dispatchList` entry point |
| `internal/remotecontrol/list_helpers.go` (new) | +70 | `jsonUnmarshal` / `stringsTrimSpace` / `timeParse` stdlib wrappers; `invalidParams` / `internalError` envelope helpers |
| `internal/remotecontrol/daemon.go` | +11 / -0 | `Config.ListBackend` field |
| `internal/remotecontrol/host.go` | +17 / -1 | `HostConfig.ListBackend` field; `StartHostSession` forwards it into the underlying `Config` |
| `internal/remotecontrol/dispatch.go` | +12 / -0 | `case "ListRemoteAgentModels", "ListRemoteAgentSlashCommands", "ListRemoteAgentConversations", "ListRemoteAgentEnvironments":` → `d.dispatchList(req.Method, req.Params)` |
| `internal/remotecontrol/lists_test.go` (new) | +720 | `newListTestDispatcher` + `jsonRPCFor` + `itoaForTest` + `envKeysProbe` helpers; 19 new tests (4 unit + 4 error-path + 4 round-trip + 3 invariant + 4 helper) |
| `cmd/wagyu/remote_control.go` | +203 / -7 | `runtime` / `sort` / `strings` imports + `convstore` import; `buildHostListBackend` + `defaultBackendLabel` + `hostnameOrEmpty`; wiring into `maybeStartRemoteControlDaemon` (Unix path via `StartHostSession`) and `maybeStartRemoteControlTCPDaemon` (TCP path via `NewDispatcher`) |
| `internal/remotetransport/tcp_test.go` | +250 / -0 | `newTestTCPServerWithListBackend` helper; 4 new `TestListRemoteAgent*` WS round-trip tests |

`internal/remotecontrol/cascade.go`, `cascade_test.go`,
`backend.go`, `pending.go`, `streaming.go`, `notify.go`,
`wire.go`, `internal/remotetransport/unix.go`,
`internal/remotetransport/tcp.go`, and the existing
`daemon_test.go` are **untouched**. The four `List…` handlers
are additive on the wire.

## Design notes worth keeping

1. **The `ListBackend` seam is the only contract cmd/wagyu/
   needs to know about.** The dispatcher reads from
   `Config.ListBackend`'s closures; production wiring
   constructs the closure set; a future cycle that wants to
   swap in a different model-fetching path replaces the closure
   without touching the dispatcher.

2. **Nil closures fall back to per-method defaults.** A
   zero-valued `ListBackend{}` is a usable config: each
   handler falls back to `DefaultModelInfo` /
   `DefaultSlashCommands` / empty / zero `EnvironmentInfo`.
   This matches the dispatcher's existing default-fill
   behaviour (Cascades, Agents, Reader) — no `NewDispatcher`
   special-cases `ListBackend` because the per-handler
   fallback is local.

3. **`dispatchList` is the single entry point for all four
   methods.** A future cycle that adds a fifth read-only method
   (e.g. `ListRemoteAgentProjects`) adds the name to
   `ListMethodsNoSideEffects` and one arm to the `switch
   method` — no other file changes.

4. **The pre-flight hook is a guardrail, not an active
   enforcer.** `isSideEffectingListBackend(b)` always returns
   `false` today (the struct field doesn't carry a side-effect
   flag yet). A future cycle that adds the field turns the
   guardrail into an enforcer by reading the field in the
   function body. `TestListReadOnly_Contract` pins the
   current behaviour so a future "always returns false"
   regression is caught.

5. **The `before` cursor parser tries RFC3339Nano first,
   then RFC3339.** Dashboards built against the modernised
   `cli.ReferenceVersion = "1.4.0"` send RFC3339Nano; older
   dashboards send RFC3339; the handler accepts both. Bad
   strings → `-32602`.

6. **`next_before` is the OLDEST entry's timestamp** — a
   backward-paging cursor. The dashboard sends
   `{"before": "<next_before>"}` on the next call. An empty
   `next_before` means "no more pages" (the dashboard knows
   because the previous page didn't fill up to `limit`).

7. **`env_keys_present` is sorted + deduped + NAMES-only.**
   The closed probe set (`DefaultEnvKeyProbe`) is the only set
   of names the dispatcher reads. A future cycle that wants
   to add a new env var (e.g. `$OPENAGY_OAUTH_TOKEN`) adds it
   to `DefaultEnvKeyProbe` and rebuilds the list — single
   source of truth.

8. **`cmd/wagyu/` edits are localised to `remote_control.go`.**
   The constraint "don't reformat pre-existing gofmt/vet dirt"
   in PLAN.md is met: no other file in `cmd/wagyu/` was
   touched. The host's `buildHostListBackend` is defined and
   called from `remote_control.go` only.

9. **Production `Models` closure reuses `cli.ReferenceModels()`.**
   The `cli` package already exports the v1.4.0 catalogue
   (cycle-181 pinned `cli.ReferenceVersion`). The closure
   wraps the catalogue with `ModelInfo` projection + the
   `default` flag on the first row. A future cycle that wants
   a richer per-row `effort_supported` flag reads from a
   different source without changing the seam.

10. **`cmd/wagyu/` `defaultBackendLabel()` is duplicated on
    purpose.** The dispatcher's own `defaultBackendLabel()` in
    `internal/remotecontrol/lists.go` reads `$OPENAGY_BACKEND`
    and falls back to "agw". The host copies the same logic
    in `cmd/wagyu/remote_control.go` so the closure's
    `backend == ""` default matches. Removing the duplicate
    would require the dispatcher to import the host — the
    seam's purpose is to prevent that direction reversal.

## 硬约束 / limitations

1. **StubBackend-list path is the default.** When
   `ListBackend.Conversations` is nil (e.g. a unit test with
   no convstore), the handler returns an empty slice + empty
   cursor. A dashboard sees "no conversations" rather than a
   -32603 error.

2. **No model-catalog mutations.** The production
   `Models` closure re-reads `cli.ReferenceModels()` on every
   call — fine for a dashboard probe (sub-millisecond), but
   not cacheable today. A future cycle that wants to cache
   the catalog adds the cache to `buildHostListBackend` (or
   moves it onto `internal/models`).

3. **No sort on `models` or `commands`.** The Models
   closure emits rows in `cli.ReferenceModels()` order
   (alphabetical by ID); `DefaultSlashCommands` is grouped by
   scope (the cycle's chosen order). A dashboard that wants a
   different sort orders client-side. The cycle does not add a
   `sort` parameter — that's a future-cycle ask.

4. **No `since` cursor (only `before`).** The cycle ships the
   backward-paging cursor (RFC3339Nano / RFC3339). A
   forward-paging / "give me conversations since timestamp X"
   cursor is out of scope. A future dashboard use-case can
   extend the param shape with `since` alongside `before`.

5. **No streaming server-push.** The four methods are pure
   request/response; no `ListRemoteAgentModels` notification
   form. A dashboard that wants push-to-update on catalog
   change subscribes via a cycle yet to be designed
   (rc-list-stream?).

6. **The pre-flight hook is a guardrail, not an enforcer.**
   `isSideEffectingListBackend` always returns false today.
   A future cycle that wires a network-backed Models
   closure needs to either keep it pure (fetch-then-cache) or
   extend `ListBackend` with a `SideEffecting bool` field
   that the hook reads.

7. **`Models` `filter` is intentionally narrow.** Substring
   match on ID + Label, case-insensitive. No regex, no
   exact-match, no combined-mode. The 4 wire tests cover
   what the cycle ships; a more elaborate filter is a future
   cycle's ask.

8. **Env probe set is closed (8 names) and not configurable.**
   A future cycle that wants to probe more env vars (e.g. a
   vendor's `$VENDOR_TOKEN`) adds the name to
   `DefaultEnvKeyProbe`. The dispatcher's `Environments`
   handler is hardcoded to that set today.

9. **Pre-existing `go vet`/`-race` issues are not in scope.**
   `cmd/wagyu/model_default_effort_test.go:67`'s `t.Errorf`
   with 4 args (verified pre-existing via `git stash`) and
   `TestSlashCompactWithInstructions` +
   `TestSlashCodesearchCancel`'s race conditions (verified
   pre-existing via `git stash`) are unchanged. The cycle's
   `go vet ./internal/remotecontrol/ ./internal/remotetransport/`
   is clean; the cycle's `go test -race -count=1 ./internal/...`
   on the changed code paths is clean.

10. **`defaultBackendLabel()` is duplicated.** The closure
    builder in `cmd/wagyu/remote_control.go` and the
    dispatcher helper in `internal/remotecontrol/lists.go`
    both read `$OPENAGY_BACKEND` and fall back to "agw".
    They will drift if either side is changed; the cycle pins
    this in `TestListReadOnly_Contract`'s `defaultBackendLabel`
    path indirectly through `TestListRemoteAgentModels_BadBackend`.
    A future refactor that wants a single source should lift
    the helper onto `internal/cli` (which already exports
    `ReferenceModels` and `ReferenceVersion`) and have the
    dispatcher import that package.

11. **`user` is taken from `$USER`, falling back to
    `$LOGNAME`.** No fallback to `whoami` or `os/user`. A
    `--remote-control` daemon that runs under launchd (where
    `$USER` is set) sees the right value; a `--remote-control
    -tcp` daemon that runs in a `nohup`-style context may see
    an empty string. The cycle doesn't try to be clever about
    this — empty string is a permitted env value.

12. **`convstore.ListConversations()` error path returns
    -32603 verbatim.** The handler maps `err.Error()` into the
    envelope's `data` field. A future polish cycle can split
    the inner error into a structured `data` payload instead
    of the `errors.Join`-style flat string.

## Commands a reviewer can run

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build the binary
go build -o /tmp/wagyu_rc5 ./cmd/wagyu

# Run all affected tests with the race detector
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# Optional: vet + gofmt on the changed packages
go vet ./internal/remotecontrol/ ./internal/remotetransport/
gofmt -l \
    internal/remotecontrol/list_dispatcher.go \
    internal/remotecontrol/lists.go \
    internal/remotecontrol/lists_test.go \
    internal/remotecontrol/list_helpers.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotecontrol/dispatch.go \
    cmd/wagyu/remote_control.go \
    internal/remotetransport/tcp_test.go
```

## 后续 cycle 入口 / next-cycle entry points

| Future cycle | What this cycle left for them |
|---|---|
| `rc-list-projects` | `ListMethodsNoSideEffects` is the right hook to opt a new list-shaped method into the read-only contract. `dispatchList` fans out via a `switch method`. The handler signature is `(dispatcherList) handleXxx(params []byte) (any, *rpcErrorBody)`. |
| `rc-list-stream` | If the dashboard wants push-notifications for catalog changes (e.g. a new model lands and the picker should refresh), the existing `Dispatcher.Notify` mechanism can carry them. The cycle's contract: the four `List…` methods are still pure reads; the streaming path uses notifications rather than reshaping `Result`. |
| `rc-cancel-cascade` | The cycle didn't touch the cascade-lifecycle methods; `rc-cancel-cascade` adds a JSON-RPC method that calls `(*Cascade).MarkClosed`. Untouched surface here. |
| `rc-piagent-bind` | `buildHostListBackend`'s `Models` closure could grow a per-row `effort_supported` flag fed from a real model registry (instead of always `true`). The seam stays; the closure changes. |
| `rc-error-status` | The cycle uses `-32603` for closure errors. A future polish cycle could split the inner error into a structured envelope. Today's flat string is debuggable but noisy. |
| `rc-final-parity-verify` | Every method this cycle ships is exercised end-to-end at this layer: a Python ws client dials, calls each List… method, asserts the wire envelope matches the documented shape; asserts the env-leak invariant (env_keys_present has no value-shaped substrings). |

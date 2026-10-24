# Cycle `rc-ws-transport` — TCP+WebSocket JSON-RPC transport for `--remote-control`

## TL;DR

`--remote-control` is still wired to a Unix-socket JSON-RPC server, but the
transport is now one of two. Cycle `rc-ws-transport` adds a parallel
TCP+WebSocket adapter that lets an external frontend drive the daemon over
`ws://host:port/rpc` — the same shape `agy --remote-control serve` uses for
its browser-side companion. The wire format is unchanged (JSON-RPC 2.0:
`initialize` / `tools/list` / `tools/call` / `ping` /
`notifications/initialized`); only the framing moves. The 613 lines of
`internal/remotecontrol/daemon_test.go` pass byte-for-byte against the
refactored Unix-socket path.

What lands in this cycle:

| Piece | Where |
|---|---|
| `Dispatcher` (transport-agnostic JSON-RPC engine) | `internal/remotecontrol/dispatch.go` (new, ~415 lines) |
| `Daemon` slimmed to its accept loop + lifecycle | `internal/remotecontrol/daemon.go` (rewritten — accept loop stays here, see 硬约束 §1) |
| `UnixServer` (parallel Unix-socket transport) | `internal/remotetransport/unix.go` (new, ~300 lines) |
| `TCPServer` (TCP+WebSocket transport) | `internal/remotetransport/tcp.go` (new, ~270 lines) |
| Smoke test (initialize + tools/list round-trip) | `internal/remotetransport/tcp_test.go` (new, 4 tests) |
| `--remote-control-tcp=<addr>` flag | `internal/cli/parser_specs.go` (extra flag, hidden) |
| Host wiring (`maybeStartRemoteControlTCPDaemon`) | `cmd/wagyu/remote_control.go` (rewritten — TCP helper added next to existing one) |
| Host tests for the new flag | `cmd/wagyu/remote_control_test.go` (+3 tests) |

Direct dep: `github.com/coder/websocket v1.8.15` (Jun 2026 release; not
`gorilla/websocket`, not hand-rolled `net/http.Hijacker`).

## What changed since the cycle-230 daemon

### Architecture (before vs after)

```
                          BEFORE (cycle 230)                 AFTER (rc-ws-transport)
                          ────────────────────                ──────────────────────
                          internal/remotecontrol              internal/remotecontrol
                            Start()  ─┐                         NewDispatcher()  ─┐
                            Daemon     │  cfg + accept loop      Daemon.Start()     │  cfg + accept loop
                            wire.go    │  wire helpers           wire.go / host.go  │  (unchanged)
                            host.go    │  host wiring            dispatch.go (NEW)  │  Dispatcher.Dispatch +
                                                                                    │  tool handlers +
                                                                                    │  rpcError / rpcResult
                                                                                    │
                                                                                    ▼
                                                                                  internal/remotetransport
                                                                                    unix.go (parallel impl)
                                                                                    tcp.go (NEW — coder/websocket)
                                                                                    │
                                                                                    ▼
                                                                                  /rpc  ws://0.0.0.0:port/rpc
```

Both transports share the same `*remotecontrol.Dispatcher`; the dispatcher
owns the wire format and the tool catalogue. The two transports differ only
in framing:

- **Unix socket** — newline-delimited JSON-RPC, one request per line, one
  response per line. `bufio.Scanner` over the raw `net.Conn`. Identical to
  pre-rc-ws-transport bytes.
- **TCP+WebSocket** — one JSON-RPC frame per ws message, both directions.
  The HTTP upgrade happens at `/rpc`; everything else returns 404.

### `Dispatcher` (the new shared engine)

`internal/remotecontrol/dispatch.go` is the only file a transport needs to
read to understand the wire format. Public surface:

```go
type Dispatcher struct { /* … */ }

func NewDispatcher(cfg Config) (*Dispatcher, error)
func (d *Dispatcher) Dispatch(ctx context.Context, raw []byte) map[string]any
func (d *Dispatcher) Accepted() uint64
func (d *Dispatcher) MarkAccepted()
func (d *Dispatcher) NotifyConnected()
func (d *Dispatcher) NotifyDisconnected()
func (d *Dispatcher) Session() *SessionState
```

`Dispatch` returns `nil` for notifications (no `id`) — both transports
treat that as "skip the write" so the two transports behave identically for
the same input. The four handlers (`handleInitialize`, `handleToolsList`,
`handleToolsCall`, plus `case "ping":`, `case "notifications/initialized":`)
are byte-identical to the cycle-230 daemon's switch in `dispatch.go`. Wire
envelopes are unchanged (the existing 613-line test pins them).

### `remotetransport` package

Two adapters, one shared interface:

```go
// internal/remotetransport (no import of internal/remotecontrol)
type Dispatcher interface {
    Dispatch(ctx context.Context, raw []byte) map[string]any
    Accepted() uint64
    MarkAccepted()
    NotifyConnected()
    NotifyDisconnected()
}
```

`*remotecontrol.Dispatcher` satisfies this interface structurally — the
remotetransport package deliberately imports only the interface so its
package graph is acyclic (see 硬约束 §1). The interface lives here so both
`UnixServer` and `TCPServer` share the same construction shape:

```go
func NewUnixServer(ctx context.Context, disp Dispatcher, cfg UnixConfig) (*UnixServer, error)
func NewTCPServer(ctx context.Context, disp Dispatcher, cfg TCPConfig) (*TCPServer, error)
```

`TCPServer` uses `coder/websocket` (v1.8.15) end-to-end:

```go
c, err := websocket.Accept(w, r, &websocket.AcceptOptions{ /* … */ })
c.SetReadLimit(16 << 20) // match Unix-socket 16 MiB scanner ceiling
// per-connection loop:
for {
    _, raw, err := c.Read(ctx)
    if err != nil { return }
    resp := srv.dispatcher.Dispatch(ctx, raw)
    if resp == nil { continue }            // notification — no reply
    if err := wsjson.Write(ctx, c, resp); err != nil { return }
}
```

Ping/pong, close-handshake, and context-aware Read/Write are all from the
library — we do not hand-roll any of that. Cross-origin requests are
rejected by default (`InsecureSkipVerify: false`); a follow-up cycle will
add `OriginPatterns` for the browser-side companion that `agy` ships.

### `--remote-control-tcp=<addr>` flag

The flag is registered in `internal/cli/parser_specs.go` as a Hidden
extra flag (not part of the v1.3.5 binary's 1:1 surface). Cycle
`rc-final-parity-verify` may re-surface it once the dashboard wired up
through it is stable.

```go
{Name: "remote-control-tcp", Type: "string", Def: "", Hidden: true,
 Usage: "Bind addr (host:port) for the WebSocket JSON-RPC transport; " +
        "empty disables. Replaces --remote-control when set"},
```

Host wiring (in `cmd/wagyu/remote_control.go`) follows the
`--remote-control` shape:

- `maybeStartRemoteControlTCPDaemon(fs, sess)` returns `*remotetransport.TCPServer` (or
  nil when the flag is unset/empty).
- `shutdownRemoteControlTCPDaemon(srv, w)` is the symmetric teardown
  helper (5-second shutdown timeout, matching `remotetransport.TCPServer.Shutdown`'s
  internal cap).
- Precedence rule: when both `--remote-control` and `--remote-control-tcp`
  are set, the WebSocket adapter wins (the Unix-socket helper returns
  nil and prints a one-line stderr message). Documented in the helper
  comment; pinned by `TestRemoteControlBothFlagsSetPrefersTCP`.

The TCP daemon's bound address is logged to stderr:

```
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:51048/rpc
```

so an operator knows where to point `websocat` / `wscat` / the cycle-230
dashboard. When `--remote-control-tcp=127.0.0.1:0` is passed the OS picks
the port; `(*TCPServer).HTTPURL()` returns the printable URL after bind.

## Wire-format compatibility (the contract this cycle honours)

`daemon_test.go`'s 613 lines test the cycle-230 Unix-socket wire format
end-to-end (initialize / tools/list / tools/call / status / list_agents /
get_output / unknown method / malformed input / shutdown cleanup). After
this cycle, the same 16 tests pass **byte-for-byte** against the new code
path. The Dispatcher is the single source of truth for the response
envelopes; both transports consume it.

The TCP+WebSocket smoke test (`internal/remotetransport/tcp_test.go`) sends
the exact JSON-RPC frames the cycle-230 dashboard would send and asserts
the response carries the same 3-tool catalogue:

```go
writeJSONRPC(t, c, map[string]any{
    "jsonrpc": "2.0", "id": 1, "method": "initialize",
    "params": map[string]any{
        "protocolVersion": "2024-11-05",
        "clientInfo":      map[string]any{"name": "smoke", "version": "0"},
    },
})
resp := readJSONRPC(t, c)
// asserts protocolVersion echoed + serverInfo.name == ServerName

writeJSONRPC(t, c, map[string]any{
    "jsonrpc": "2.0", "id": 2, "method": "tools/list",
})
resp := readJSONRPC(t, c)
// asserts tools == {status, list_agents, get_output} (same 3 the unix daemon returns)
```

## Test results

### New tests (added by this cycle)

| Test | Where | What it pins |
|---|---|---|
| `TestWSAdapter_InitializeAndToolsList` | `internal/remotetransport/tcp_test.go` | WS upgrade + initialize handshake + the same 3-tool catalogue the unix daemon returns |
| `TestWSAdapter_HTTPPathRoutes404` | `internal/remotetransport/tcp_test.go` | Port-scan hardening — only `/rpc` is registered; everything else 404s |
| `TestWSAdapter_StatusToolReflectsSession` | `internal/remotetransport/tcp_test.go` | The shared `Dispatcher` is reachable over WS too: `tools/call status` reflects the host's `SessionState` |
| `TestWSAdapter_ConnectedCounterIncrements` | `internal/remotetransport/tcp_test.go` | The shared `SessionState.connected` counter flips when a ws client dials — proves both transports share the same counter (Unix socket and WS see a single number in the status tool) |
| `TestRemoteControlTCPFlagStartsWSAdapter` | `cmd/wagyu/remote_control_test.go` | `--remote-control-tcp=127.0.0.1:0` brings up `*remotetransport.TCPServer`; `HTTPURL()` returns a `ws://.../rpc` URL |
| `TestRemoteControlTCPFlagUnsetSkipsAdapter` | `cmd/wagyu/remote_control_test.go` | `--remote-control-tcp=""` is a no-op (helper returns nil; deferred shutdown is a no-op) |
| `TestRemoteControlBothFlagsSetPrefersTCP` | `cmd/wagyu/remote_control_test.go` | Precedence — when both flags are set, the WS adapter wins, the unix-socket helper returns nil |

7 tests added.

### Modified tests

Zero. The cycle-230 daemon_test.go (613 lines, 16 tests) and
cmd/wagyu/remote_control_test.go's 9 tests all pass against the
refactored code without a single line of test code changed.

### Total tests passing on this branch

```
$ go test -race ./internal/remotecontrol/ ./internal/remotetransport/
ok      github.com/chadgpt/openagy/internal/remotecontrol       1.380s
ok      github.com/chadgpt/openagy/internal/remotetransport     1.415s

$ go test -race -vet=off ./cmd/wagyu/ -run 'TestRemoteControl|TestMCP|TestSocket'
ok      github.com/chadgpt/openagy/cmd/wagyu                    3.010s
```

16 (existing daemon_test.go) + 4 (new tcp_test.go) + 12 (existing
remote_control_test.go + 3 new) = **32 remote-control tests pass**.

### `go vet`

```
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/
$ echo $?
0
```

### `gofmt -l` on the changed files

```
$ gofmt -l internal/remotecontrol/daemon.go \
              internal/remotecontrol/dispatch.go \
              internal/remotetransport/unix.go \
              internal/remotetransport/tcp.go \
              internal/remotetransport/tcp_test.go \
              cmd/wagyu/remote_control.go \
              cmd/wagyu/remote_control_test.go \
              internal/cli/parser_specs.go
(no output)
```

The repository's pre-existing `gofmt -l` dirt (50+ files in `internal/` +
a handful in `cmd/wagyu/`) is intentionally left alone per the cycle
rules.

### `go.mod`

```
require (
    …
    github.com/coder/websocket v1.8.15  // direct, not indirect
    …
)
```

`go mod tidy` after this cycle puts `coder/websocket` in the **direct**
require block (the prompt's hard rule). No transitive-dep churn.

## Live verification (manual)

```
$ go build -o /tmp/wagyu ./cmd/wagyu
$ /tmp/wagyu --remote-control-tcp=127.0.0.1:0 --print 'hello' &
[remote-control-tcp] ws daemon listening on: ws://127.0.0.1:NNNNN/rpc
…
```

The Go smoke test (`tcp_test.go`) is the in-process equivalent of the
operator flow above: bring up the WS adapter on `127.0.0.1:0`, dial it
with `websocket.Dial`, send initialize + tools/list, assert the responses.
A reviewer can also use the `--remote-control-tcp` flag directly and
point `websocat` (or any ws client) at the printed URL — the responses
will match the in-process test byte-for-byte.

## Commands a reviewer can run

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# Build the binary
go build -o /tmp/wagyu ./cmd/wagyu

# Run all affected tests with race detector
go test -race ./internal/remotecontrol/ ./internal/remotetransport/

# Full cmd/wagyu suite (the cmd/wagyu vet error in model_default_effort_test.go:67
# is pre-existing on the base commit 655b128; -vet=off works around it)
go test -race -vet=off ./cmd/...

# Live WS adapter — reviewer's local check
PYTHONUNBUFFERED=1 /tmp/wagyu --remote-control-tcp=127.0.0.1:0 --print 'hello' &
# in another shell: websocat ws://127.0.0.1:<port>/rpc
# (or use the python `websockets` library; the JSON-RPC frames are
# exactly what the in-process smoke test sends)
```

## 硬约束 / limitations

1. **Two parallel accept loops (cycle-temp duplication).** Both
   `internal/remotecontrol.Daemon.acceptLoop` and
   `internal/remotetransport.UnixServer.acceptLoop` keep their own copy
   of the Unix-socket accept loop. The duplication is small (~30 lines)
   and intentional: the cleanest layering (Daemon → remotetransport)
   creates an import cycle through `tcp_test.go` (that test imports
   `*remotecontrol.Dispatcher`). A future cycle that wants to break the
   duplication will need to lift both implementations onto a shared
   interface owned by `internal/remotetransport` — out of scope here.
   The comment block at `daemon.go:acceptLoop` documents the constraint
   and the path to a fix.

2. **Cross-origin WS upgrade is rejected by default.** `InsecureSkipVerify:
   false` means a browser frontend served from `https://dashboard.local`
   cannot connect to the WS adapter today. The reference (`agy remote-
   control serve`) is more permissive — it allows the dashboard's origin
   via `OriginPatterns`. A follow-up cycle (`rc-auth-tokens` is the
   natural seat for this) should add `OriginPatterns: []string{
   "dashboard.local", "localhost:*"}` so the browser companion works
   without a TLS tunnel.

3. **No TLS on the WS adapter yet.** Production dashboards usually expect
   `wss://` (TLS-terminated). Cycle rc-ws-transport ships plain `ws://` to
   keep the test surface hermetic. A follow-up cycle (likely
   `rc-auth-tokens` or `rc-persistent-daemon`) should add `*tls.Config`
   wiring so a real frontend can connect without `stunnel` / `caddy` in
   front.

4. **Pre-existing vet error in cmd/wagyu.** `cmd/wagyu/model_default_effort_test.go:67`
   has a `(*testing.common).Errorf` call with the wrong number of args.
   This is on the base commit (cycle 228 territory) and unrelated to
   `rc-ws-transport`. The build itself is clean (`go build ./cmd/wagyu`
   succeeds); only `go test` rejects the package under vet. A reviewer
   running `go test -vet=off ./cmd/...` sees the same pass / fail pattern
   the cycle-230 baseline had.

5. **Pre-existing race in `internal/tools` and `cmd/wagyu` race tests.**
   `TestRunShellCommand_GitEditorSet` (internal/tools) and
   `TestSlashCompactWithInstructions` (cmd/wagyu) both fail under
   `go test -race` on the base commit (verified by `git stash` round-
   trip). Not introduced by this cycle; not in scope.

6. **No live `websocat` smoke recorded in this cycle.** The Python
   `websockets` library wasn't available in the sandbox Python env
   (PEP 668), so the live `ws://` handshake is verified only through the
   in-process Go test (`tcp_test.go`). A reviewer with `websocat`
   installed can reproduce the cycle's smoke test by pointing it at the
   URL the daemon prints on stderr.

7. **`gorilla/websocket` is still an indirect dep.** `go mod tidy`
   leaves `github.com/gorilla/websocket v1.5.3` in the indirect block
   because one of the charmbracelet TUI deps (charm.land/glamour/v2 or
   similar) imports it. This cycle does not use it directly; the
   `coder/websocket` direct dep is the only ws library rc-ws-transport
   touches. A future cycle that wants to drop the indirect dep will
   need to chase it through the charm graph — out of scope here.
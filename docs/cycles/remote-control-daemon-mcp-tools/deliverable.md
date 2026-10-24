# Cycle remote-control-daemon-mcp-tools — `--remote-control` daemon + MCP tools

## Goal

AGY 1.2.16 changelog:

> Added the `--remote-control` daemon flag that exposes a small set of
> MCP tools (status, list_agents, get_output) over a Unix socket so
> external dashboards can read live session state.

This cycle closes that gap on the `cmd/wagyu` clone. When the user
passes `--remote-control`, the binary brings up an in-process
JSON-RPC server that listens on a Unix-domain socket. External
dashboards can connect, run the MCP handshake, list the tools the
daemon exposes, and call `status`, `list_agents`, and `get_output`
to read the host's live session state.

## Wire protocol — what dashboards see

The daemon speaks a strict subset of MCP JSON-RPC over a Unix
socket (newline-delimited, one request per line, one response per
line). The handshake echoes the client's `protocolVersion` so
pinning-aware dashboards see negotiation succeed:

```
→ {"jsonrpc":"2.0","id":1,"method":"initialize",
   "params":{"protocolVersion":"2024-11-05",
             "clientInfo":{"name":"dashboard","version":"0.1.0"}}}
← {"jsonrpc":"2.0","id":1,
   "result":{"protocolVersion":"2024-11-05",
             "serverInfo":{"name":"openagy-remote-control","version":"0.1.0"},
             "capabilities":{"tools":{}}}}
```

`tools/list` returns exactly three tools:

| Name | Description | Input |
|---|---|---|
| `status` | Live session state: pid, version, backend, model, workspace, conversation id, connected dashboards, uptime | `{}` |
| `list_agents` | Built-in + custom agent names (mirrors `--agent` validation) | `{}` |
| `get_output` | Most recent turn output for a conversation id, or for the host's currently-running conversation when no id is passed | `{"conversation_id"?: string}` |

The `get_output` tool's wire envelope:

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "content": [{"type": "text", "text": "{\"conversation_id\":\"...\",\"output\":\"...\"}"}],
    "isError": false
  }
}
```

Tool failures carry `isError: true` with the message in `content[0].text`;
the JSON-RPC envelope itself stays 200-shaped (matches the MCP
convention so a generic dashboard can render the error inline).

## Architecture

```
cmd/wagyu (main.go / remote_control.go)
   │
   │ --remote-control flag detected
   │ rcSess := remotecontrol.NewSessionState(cliVersion)
   │ rcSess.SetConversationID(conversationID)   ── chunk handler
   │ rcSess.AppendTurnOutput(clean)             ── prints each chunk
   │ rcSess.SetBackend(resolved)
   ▼
internal/remotecontrol/daemon.go
   net.Listen("unix", $OPENAGY_RC_SOCKET)
        │
        │ JSON-RPC over newline-delimited frames
        ▼
   ┌──────────────────────────────────────────────────┐
   │ tools/call dispatch                              │
   │   "status"      → SessionState.statusSnapshot()  │
   │   "list_agents" → DefaultAgentLister.ListAgents()│
   │   "get_output"  → in-memory snapshot →           │
   │                   internal/convstore fallback     │
   └──────────────────────────────────────────────────┘
```

Two read paths for `get_output`:

1. **In-memory snapshot** (`SessionState.TurnOutput`) — what the
   host's chunk handler just pushed. Always preferred when the
   caller asks for the host's currently-running conversation
   (no explicit `conversation_id`). This is what a dashboard
   wants to see as the user types — it is strictly fresher than
   any disk-backed read.
2. **`ConvstoreOutputReader`** (configured in `host.go`) — used
   when the caller asks for a historical conversation. Reads
   `internal/convstore` via the public `ListConversations` API so
   `internal/convstore/sqlite.go` stays untouched (the AGENTS.md
   hard constraint).

## Files

| File | Status | Purpose |
|---|---|---|
| `internal/remotecontrol/daemon.go` | **new** | Wire protocol + tool handlers (stdlib only) |
| `internal/remotecontrol/host.go` | **new** | Production wiring (`ConvstoreOutputReader`, `DefaultAgentLister`) |
| `internal/remotecontrol/wire.go` | **new** | `ConvstoreOutputReader` (host-side) |
| `internal/remotecontrol/daemon_test.go` | **new** | 16 unit tests against the wire protocol |
| `cmd/wagyu/remote_control.go` | **new** | Host helper: flag → daemon, teardown |
| `cmd/wagyu/remote_control_test.go` | **new** | 7 host-helper integration tests |
| `cmd/wagyu/main.go` | modified | Wires `--remote-control` into `runPrintMode` (37 lines added) |

`internal/convstore/sqlite.go` was **NOT** touched (AGENTS.md hard
constraint).

## Tests

Per the targeted test pattern
`TestRemote|TestDaemon|TestMCP|TestSocket`:

```
=== RUN   TestRemoteControlFlagStartsDaemon        (cmd/wagyu)
=== RUN   TestRemoteControlFlagUnsetSkipsDaemon    (cmd/wagyu)
=== RUN   TestRemoteControlDaemonListensOnUnixSocket (cmd/wagyu)
=== RUN   TestMCPStatusToolReturnsLiveSessionState (cmd/wagyu)
=== RUN   TestMCPListAgentsToolReturnsAgentList    (cmd/wagyu)
=== RUN   TestMCPGetOutputToolReturnsTurnOutput    (cmd/wagyu)
=== RUN   TestSocketCleanupOnShutdown              (cmd/wagyu)
=== RUN   TestRemoteControlStatus_1to1             (internal/cli, pre-existing)
=== RUN   TestRemoteControlFeedback                (cmd/wagyu, pre-existing)
=== RUN   TestMCPServersFeedback                   (cmd/wagyu, pre-existing)
=== RUN   TestMCPList_*                            (internal/cli, pre-existing)
```

Plus the 16 internal/remotecontrol/daemon_test.go tests:

```
TestStartCreatesSocket               TestToolsList
TestStartDefaultSocketPath           TestStatusToolReturnsSnapshot
TestStartRefusesBadPath              TestListAgentsTool
TestInitialize                       TestGetOutputToolByID
TestGetOutputToolNotFound            TestGetOutputPropagatesReaderError
TestMalformedRequestReturnsParseError TestShutdownRemovesSocket
TestSessionStateTruncatesLargeOutput TestStatusConnectedReflectsClients
TestUnknownMethodReturnsJSONRPCError
```

## End-to-end smoke

Live binary test (Python dashboard over the actual `wagyu-rcd`):

```python
$ /tmp/wagyu-rcd --remote-control --print "say hi" --backend shim \
                  --print-timeout 20s &
$ # Open socket, send 3 requests:
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{...}}
  → {"result":{"capabilities":{"tools":{}}, "serverInfo":{...}, ...}}

{"jsonrpc":"2.0","id":3,"method":"tools/call",
 "params":{"name":"status","arguments":{}}}
  → {"result":{"content":[{"text":"{\"backend\":\"shim\",
       \"connected\":1,\"conversation_id\":\"234f...\",\"model\":\"
       Gemini 3.8 Flash (Medium)\",\"pid\":64876,\"started_at\":\"...\",
       \"uptime_seconds\":0,\"version\":\"1.4.0\",\"workspace\":\"...\"}",
       "type":"text"}],"isError":false}}

{"jsonrpc":"2.0","id":4,"method":"tools/call",
 "params":{"name":"list_agents","arguments":{}}}
  → {"result":{"content":[{"text":"{\"agents\":[
       {\"name\":\"default\",\"label\":\"Default\"},
       {\"name\":\"Default\",\"label\":\"Default\"}]}",
       "type":"text"}],"isError":false}}
```

The `connected: 1` in the status output is the live dashboard
counter — proves the accept loop is wired.

## Configuration

| Mechanism | Default |
|---|---|
| `--remote-control` flag | unset (no daemon) |
| `$OPENAGY_RC_SOCKET` env var | `$TMPDIR/openagy-remote-control-<pid>.sock` (or `os.TempDir()`) |
| Socket file permissions | `0o600` |

## Hard constraints

| Constraint | Status |
|---|---|
| `internal/convstore/sqlite.go` not touched | ✅ |
| New files gofmt-clean | ✅ (`gofmt -l` empty) |
| Pre-existing not-clean files untouched | ✅ (no reformat to other files) |
| `go build -o /tmp/wagyu-rcd ./cmd/wagyu` exits 0 | ✅ |
| Targeted test filter passes | ✅ (`TestRemote\|TestDaemon\|TestMCP\|TestSocket`) |
| ≥ 5 unit tests | ✅ (16 in daemon_test.go + 7 in remote_control_test.go = 23) |
| Single commit | ✅ |
| Branch not pushed | ✅ |

## What was *not* done (limitations)

- **No interactive-TUI wiring.** `runLiveTUI` is not wired into the
  daemon. A user wanting `--remote-control` on a TUI session
  should add the same three lines `runPrintMode` now has. The
  daemon itself is long-lived; only the host-side hookup is
  missing.
- **`get_output` only mirrors the host's live chunk stream.**
  Historical turns are read from `internal/convstore` via the
  `ListConversations` API. The convstore-backed reader returns
  the conversation's `Title` rather than the full body — body
  parsing is intentionally minimal so this package does not
  become a duplicate of the convstore package.
- **No mTLS / authentication.** A dashboard connecting to the
  socket gets full read access to the host's session state. The
  default `0o600` socket permissions mean only the same user
  (and root) can connect — this matches the v1.3.4 binary's
  recovered behaviour but is not enterprise-grade.
- **MCP subset only.** We implement `initialize`, `tools/list`,
  `tools/call`, `ping`, and the `notifications/initialized`
  handshake completion. `resources/*`, `prompts/*`, sampling,
  notifications, and progress are intentionally out of scope —
  the changelog ships three tools, not a full MCP server.
- **No reconnection / streaming.** A dashboard that drops its
  connection has to reconnect; turn text is read once per
  `get_output` call rather than pushed. A later cycle can layer
  `resources/subscribe` over the in-memory snapshot for live
  updates if the changelog calls for it.

## Source pointers

- Wire protocol: `internal/remotecontrol/daemon.go`
- Production wiring: `internal/remotecontrol/host.go`,
  `internal/remotecontrol/wire.go`
- Host helpers: `cmd/wagyu/remote_control.go`
- Print-mode wiring: `cmd/wagyu/main.go` (runPrintMode, ~37 lines)
- Tests: `internal/remotecontrol/daemon_test.go`,
  `cmd/wagyu/remote_control_test.go`
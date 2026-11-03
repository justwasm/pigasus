# The agent server and its client: `pigasus server` and `pigasus tui --server`

2026-10-31. This cycle adds a client/server mode to the clone: `pigasus
server` hosts the agent and serves it over a websocket, and `pigasus tui
--server …` attaches the interactive session to it. The turn runs on the
server — on that machine's workspace, tools and credentials — and the
conversation renders on the client.

The architecture follows the `../crush` reference project's pattern (a
host process and an attaching client), with one deliberate difference:
**websockets**, not HTTP + SSE.

---

## What was built

| piece | where | what it is |
|---|---|---|
| the wire | [`agentlink/`](../../agentlink) | **public** — the frames, the client, the server, the token |
| the turn seam | `internal/app/agentturn/` | `Engine`, `Local`, `Remote` — both engines speak the same frames |
| the host | `internal/app/agentserver/` | the session: workspace, tools, policy, prompt, transport |
| the commands | `internal/cli/subcommand_server.go`, `internal/cli/subcommand_tui.go` | the parser-level subcommands |
| the bridges | `internal/cli/server_bridge.go`, `internal/cli/tui_bridge.go` | the parser → `cliapp` seam |
| the implementations | `internal/cliapp/server_host.go`, `internal/cliapp/tui_attach.go` | the two commands |
| the TUI's turn loop | `internal/cliapp/tui_stream_prompt.go` | rewritten around the `turnCollector` |
| the smoke client | `devutils/agentsmoke/` | the end-to-end check, and the shortest complete client |

> **Placed later.** Cycle 375 moved the two internal packages into their
> tier: `internal/agentturn` → `internal/app/agentturn` and
> `internal/agentserver` → `internal/app/agentserver`, and lifted the four
> session cores they shared with the print path into
> `internal/app/session/setup`. The paths below are the current ones; see
> [`cycle-375-tier-placement-agent-server/`](../cycle-375-tier-placement-agent-server/deliverable.md).

## The wire

One JSON object per websocket text frame, one `Frame` type, discriminated by
its `Kind`.

| direction | kind | what it carries |
|---|---|---|
| client → server | `hello` | the greeting |
| client → server | `prompt` | the model to run under and the transcript to replay |
| client → server | `cancel` | stop the turn |
| server → client | `hello` | the version, the workspace, the model, the session id |
| server → client | `text` | a text delta |
| server → client | `thought` | a reasoning delta |
| server → client | `tool` | `tool_start` / `tool_end` |
| server → client | `compact` | the compaction's start / end (with the server's own duration) |
| server → client | `notice` | auto-continue, eligibility, an error |
| server → client | `result` | the terminator: the whole answer, the usage, the error |

`agentlink` is **public**: it sits at the top of the tree and has no
`internal/` dependency, so a third party implementing the client side imports
it directly.

```
go list -deps ./agentlink | grep pigasus/internal     # nothing
```

## The turn seam

`internal/app/agentturn` is the seam between *what a turn is* and *where it
runs*.

```go
type Engine interface {
    Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error)
}
```

`Local` runs the turn over `internal/agent.Loop` and reports the loop's
events as frames; `Remote` sends the transcript to a server and reports the
server's frames unchanged. Because both answer with the same frames, the
TUI's transcript does not know which one it is rendering — the local path and
the remote path **cannot** render differently.

This is what made the TUI's turn loop a rewrite rather than an addition: the
old `streamCollector` / `wrapToolRunner` / `buildLoopOptions` /
`runStreamTurnLoop` machinery became one `turnCollector` that maps frames to
rows.

## Three bugs the wire's own tests found

The protocol has invariants that are easy to state and easy to break. Three
of them were broken by the first implementation and are now pinned by tests.

### 1. A cancelled turn sent no terminator

The client waits for the `result` frame. A cancelled turn dropped it — the
frame emitter gave up when the turn's context was cancelled — so the client
waited forever and the session hung.

**The rule**: a turn ends with exactly one `result` frame, a cancelled turn
included. The terminator's write is therefore *bounded* rather than
*cancelled* (`agentturn.emitResult`, `resultTimeout` = 2 s): a reader that is
gone must not hold the engine's goroutine, and a reader that is there must
not be left waiting.

A cancelled turn's `result` carries **no error** — the reference reports a
cancellation through the composer, not as a failure.

### 2. The server wrote the terminator on a cancelled context

The server's `runTurn` wrote the frames with the **turn's** context. A
cancelled turn therefore failed to write exactly the frame the client was
waiting for.

**The rule**: the frames are written with the **connection's** context; the
turn's context governs the turn, not the wire.

### 3. The remote engine's read was tied to the turn

The first remote engine read the connection inside the turn's goroutine. A
read interrupted by the turn's cancellation **closes the websocket**
(`coder/websocket` closes the connection when a read is cancelled), so a
cancelled turn took the *session's next turn* with it.

**The rule**: the connection's frames are read by a reader goroutine that
lives for the whole session; the cancel frame is sent on the still-open
connection. `agentturn.Remote.readLoop` is that reader.

## The server

`internal/app/agentserver` owns the session: the workspace, the tool set, the
permission policy, the prompt and the transport. It has no dependency on
`internal/cliapp` (that would be an import cycle), so the setup is rebuilt
from `internal/printmode`, `internal/tools`,
`internal/features/tools/toolrunner` and `internal/agyprompt`.

Two decisions:

- **The server always demands a bearer token**, checked **before** the
  websocket upgrade (`agentlink.Server.authorized`), because it runs shell
  commands against its own workspace. A token is minted when none is
  configured, and it is printed at start-up. The comparison is constant time
  (`agentlink.tokenMatches`).
- **The transport is resolved per turn** from the model the client asked for,
  so a client can switch the model without reconnecting.

Each connection gets its own conversation id — two clients on one server must
not share one.

## The commands

Both are registered at the `cliapp` parser level, next to `proxy` and `web`,
so the reference-parity golden help file (`internal/cli/testdata/help_1.4.6.txt`)
and the hidden-surface inventory are untouched.

```
pigasus server [--addr host:port] [-p|--port N] [--host H]
               [-w|--workspace DIR] [--model M] [--effort E]
               [--mode auto|plan|acceptEdits] [--dangerously-skip-permissions]
               [--token T] [--agent A] [--backend gateway|cloudcode|shim]
               [--add-dir DIR]

pigasus tui [--server ws://host:port] [--token T] [the session's own flags]
```

`pigasus tui` owns only `--server` and `--token`; every other flag is the
root command's own and is passed through to the session, which parses it
(`cli.NewFlagSet`, built from `cli.StandardFlags()` + `cli.ExtraFlags()`).

## The refactor the split forced

Three flag-free cores were lifted out of `internal/cliapp` so the server
could use them without importing `cliapp`:

| core | where | what it is |
|---|---|---|
| `PolicyFrom(mode, skipPermissions)` | `internal/app/session/setup/setup.go` | the permission policy, without the flag set |
| `SandboxFrom(skipPermissions, workspace, conversationID)` | `internal/app/session/setup/setup.go` | the sandbox, without the flag set |
| `CustomSubagents()` | `internal/app/session/setup/subagents.go` | the user's own subagent definitions |

(They landed in `internal/printmode` first and moved down to
`internal/app/session/setup` in cycle 375 — a host may not import another
host, and the print path is an entry surface. See
[`cycle-375-tier-placement-agent-server/`](../cycle-375-tier-placement-agent-server/deliverable.md).)

`cliapp`'s own `customSubagents` now delegates to the third one, so there is
one implementation of each.

`internal/cliapp/tui_stream_prompt.go` lost ~120 lines of turn machinery and
gained one collector; the turn's frames are now the only thing the
transcript knows about.

## Verification

```
go build ./cmd/... ./internal/... ./agentlink/... ./endpoints/... ./configdir/...
go test ./agentlink/... ./internal/app/agentturn/... ./internal/app/agentserver/... ./internal/cli/...
```

- `agentlink` — the frame codec, the client's handshake, the token gate.
- `internal/app/agentturn` — the local engine (the answer, the model's choice,
  the missing transport), the frame runner (the call is reported twice), the
  wire message round trip, and the remote engine (the transcript travels, the
  cancel frame is sent, a second turn is refused).
- `internal/app/agentserver` — the token is minted, the wrong token is rejected
  before the upgrade, a turn is answered end to end, and **a cancelled turn
  leaves the connection alive** (the session greets a second time on it).
- `internal/cli` — the reference's golden help and the hidden-surface
  inventory are unchanged.

End to end, against a real server on a real websocket:

```
$ pigasus server --port 7799 --token smoketoken --workspace /tmp
pigasus server listening on ws://127.0.0.1:7799
workspace: /tmp
model: Gemini 3.8 Flash (High)
token: smoketoken

$ go run ./devutils/agentsmoke -server ws://127.0.0.1:7799 -token smoketoken -prompt "hello"
server 1.4.11
  workspace: /tmp
  model:     Gemini 3.8 Flash (High)
  session:   36c0087d-8669-ad06-6f8a-e870f5e82f75
Hello! How can I help you today? Let me know what you're working on or what you'd like to do.
[usage] 14027 in / 27 out
```

The interactive client was driven through a pty as well: the prompt is
submitted, the server runs the turn, and the answer is rendered in the
transcript — the local path renders the same way.

## The remote-control surface is untouched

The client/server pair adds two **visible** subcommands at the parser
level, next to `web` and `proxy`. The remote-control surface is older and
bigger — it is the Web UI's channel, the terminal it serves and the cloud
tunnel the dashboard reaches — so it was checked rather than assumed.

**Static.** The remote-control packages are green:
`internal/rchost`, `internal/refbackend`, `internal/remotecontrol`,
`internal/lsserver`, `rcterminal`, `remotetransport`, `lsproto`. The
reference's golden help and the hidden-surface inventory are unchanged —
the two new subcommands are registered at the cliapp level, so they are
not in `StandardSubcommands()` and the reference's own surface cannot
see them.

**Live.** Against a running `pigasus web` and a running
`remote-control serve` (the latter with its cloud tunnel pointed at a
dead endpoint, so the check touched no cloud row):

| surface | result |
|---|---|
| the page | 200, 104,495 bytes, the app config and the CSRF token in it |
| `CreateTerminal` | a live pty, `title: bash`, a pid, the workspace uri |
| `ListTerminals` | the terminal, with its id, pid and title |
| `SendTerminalInput` | `ackedOffset` — the bytes are base64 (proto-JSON) |
| `StreamTerminalOutput` | the terminal's own output, live, over the stream |
| `CloseTerminal` | `{}` — the terminal is gone |
| the Connect JSON dialect | `POST /exa.language_server_pb.LanguageServerService/<Method>` |
| the Web UI's own transport | `ws://…/connect-websocket` — ping/pong, unary, stream |
| the legacy `/rpc` websocket | the same three, over `/rpc` |
| the Mendel flag set | the CLI's identity and the desktop's identity answer different sets (`enable-jetbox-terminal-v2` is on in the desktop's, off in the CLI's) |
| the in-session `/remote-control` | the account-chooser link, naming the id the daemon registered |

The terminal round trip was verified end to end: `echo LIVE-REMOTE-OK`
was typed into the terminal and the string came back out of the output
stream.

**The regression guard.** `internal/cli/remote_control_surface_test.go`
pins the surface against a parser that carries the new pair:

- the `remote-control` usage block is still the reference's — `start`,
  `status`, `stop`, and none of the clone's hidden `serve`, `run`,
  `rename`;
- both hidden foreground spellings still reach the serve bridge, with
  their own label;
- `remote-control`, `server` and `tui` each reach **their own** handler.

### The test suite was hijacking the user's daemon

Checking the surface found a bug that had nothing to do with the
client/server pair and had been there all along: `go test ./internal/cliapp/`
**replaced the user's launchd daemon**.

The chain: the `/remote-control` handler starts the official daemon
(`persistentrc.StartOfficialDaemon`), and on macOS that call registers a
**launchd agent**. The launchd label is global to the login session, so a
registration does not add a second daemon — it replaces the one already
registered under that label, and `launchctl bootstrap` stops the running job
first. The cliapp tests point `$HOME` at a temporary directory, so the
replacement's stdout and stderr pointed into a directory that was deleted
with the test: the user's daemon kept running, but its log went to a deleted
temporary file and the user's own `~/Library/Logs/pigasus-antigravity-cli-daemon.log`
went silent. `launchctl print` named a plist that did not exist.

Two rules, on two levels:

- **the production path refuses it** — `persistentrc.StartOfficialDaemon`
  now checks the resolved plist path and returns a `*TemporaryHomeError`
  when it lies inside the operating system's temporary directory. The
  symlinks are resolved on both sides, because on macOS `/var` is a symlink
  to `/private/var` and the test's home carries the resolved prefix — the
  comparison misses exactly the case it exists for without that.
- **the cliapp test package stubs the call** — `internal/cliapp`'s
  `TestMain` replaces `persistentrc.StartOfficialDaemon` with a no-op, so
  the suite never asks launchd for anything. The two tests that *do* care
  about the call replace the variable themselves.

Verified: the loaded job's `path`, `stdout path` and `pid` are identical
before and after a full `go test ./internal/cliapp/`, and the user's daemon
was restored (bootout, `remote-control stop`, `remote-control start`) with
the real plist, the real log and the cloud channel answering `GetStatus`.

## 硬约束 / 存疑 / limitations

- **硬约束**: the server demands a bearer token, and the check happens
  **before** the websocket upgrade. A server with no configured token mints
  one, and the token is printed at start-up.
- **硬约束**: the default bind is the **loopback**. The server runs shell
  commands against its own workspace, so it does not listen on the network
  unless `--host`/`--addr` says so.
- **硬约束**: a turn ends with exactly one `result` frame — a cancelled turn
  included — and the frames are written with the **connection's** context.
- **硬约束**: the remote engine's reader goroutine lives for the whole
  session; a read interrupted by a turn's cancellation closes the websocket.
- **硬约束**: the transcript travels with the prompt; the server keeps no
  conversation, and every connection gets its own conversation id.
- **硬约束**: the TUI's local and remote paths render the same frames
  through the same `turnCollector`.
- **存疑**: the reference (`agy` 1.4.11) has **no** client/server mode of
  this shape. The frame names and the `prompt`'s shape are the clone's own
  design, not a recovered contract — the wire is a promise of *this*
  repository only. Nothing here was verified against the binary, because
  there is nothing in the binary to verify it against.
- **存疑**: the tool **results** do not travel with the transcript. The
  client's transcript carries the user and assistant messages; the tool calls
  and results live in the separate trajectory the reference keeps. A
  conversation that continues across a server restart therefore replays the
  user's and the assistant's messages, not the tool's output.
- **limitations**: **no interaction round trip**. The server runs the tools
  under its own permission policy; there is no approval prompt that travels
  to the client.
- **limitations**: **no subagents on the server**. The server's prompt lists
  the subagent types but does not run them.
- **limitations**: the remote session's diff preview reads the file from the
  **client's** disk, so it degrades rather than failing.
- **limitations**: one turn at a time per connection; a second turn while one
  is in flight is refused.
- **limitations**: the attached client still builds the local transport (the
  model switch panel asks for one), but it is never used for a turn.

---

Guide: [`docs/guide/agent-server.md`](../../guide/agent-server.md).

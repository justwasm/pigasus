# The agent server and its client

> `pigasus server` hosts the agent; `pigasus tui --server …` attaches to it.
> The wire is `agentlink`, the turn seam is `agentturn`, the host is
> `agentserver`.

## What the split is

The clone runs a turn in one of two places:

| | where the turn runs | whose workspace, tools and credentials |
|---|---|---|
| **local** | in this process | this machine |
| **remote** | on a `pigasus server` | the **server's** machine |

The conversation belongs to the **client**. The transcript travels with every
prompt, and the server replays what it is given — so the server keeps no
conversation of its own, and two clients on one server never see each other's
history. What the server *does* keep is the session's own state: the
workspace, the tool set, the permission policy, the credentials. Those belong
to the machine the tools run on, not to the conversation.

```
  pigasus tui --server ws://host:7777 --token T
        │  agentlink frames over one websocket
        ▼
  pigasus server --addr 127.0.0.1:7777 --token T
        │
        ▼
  internal/app/agentturn.Local → internal/agent.Loop → the model
```

## The two commands

### `pigasus server`

```
pigasus server [--addr host:port] [-p|--port N] [--host H]
               [-w|--workspace DIR] [--model M] [--effort E]
               [--mode auto|plan|acceptEdits] [--dangerously-skip-permissions]
               [--token T] [--agent A] [--backend gateway|cloudcode|shim]
               [--add-dir DIR]
```

The server **always** demands a bearer token, and the token is checked
**before** the websocket upgrade (`agentlink.Server.authorized`), so a client
without the right one never reaches a websocket at all. A token is minted when
`--token` is not given, and it is printed when the server starts:

```
pigasus server listening on ws://127.0.0.1:7777
workspace: /Users/gear/Documents/GitHub/openagy
model: Gemini 3.8 Flash (High)
token: 3f9c…
```

The reason is the server's own: it runs shell commands against its own
workspace, so an unauthenticated peer is a peer with the machine's shell. The
default bind is the **loopback** — the server does not listen on the network
unless `--host`/`--addr` says so.

The token comparison is constant time (`agentlink.tokenMatches`).

### `pigasus tui --server`

```
pigasus tui [--server ws://host:port] [--token T] [the session's own flags]
```

Without `--server` it is the ordinary local session. With it, the session
attaches: the turn runs on the server, and the conversation renders here.

The subcommand owns only `--server` and `--token`; every other flag is the
root command's own and is passed through to the session, which parses it
(`cli.NewFlagSet`). A bare `host:port` is accepted and normalised to
`ws://host:port`.

## The wire: `agentlink`

`agentlink` is a **public** package — it sits at the top of the tree and has
no `internal/` dependency, so a third party implementing the client side
imports it directly.

One JSON object per websocket **text** frame. One `Frame` type for the whole
protocol: the frame's `Kind` says what it carries, and the other members are
only meaningful for the kinds that use them.

| direction | kind | what it carries |
|---|---|---|
| client → server | `hello` | the greeting; the server answers with its own `hello` (version, workspace, model, session) |
| client → server | `prompt` | the turn: the model to run under and the **transcript** to replay |
| client → server | `cancel` | stop the turn |
| server → client | `text` | a text delta |
| server → client | `thought` | a reasoning delta |
| server → client | `tool` | a tool call's start (`tool_start`) or its outcome (`tool_end`) |
| server → client | `compact` | a compaction's start or its end (with the server's own duration) |
| server → client | `notice` | an out-of-band notice: auto-continue, eligibility, an error |
| server → client | `result` | the turn's terminator: the whole answer, the usage, the error |

Three rules are load-bearing:

- **A turn ends with exactly one `result` frame.** Even a cancelled turn
  sends one: the client is waiting for it, and a turn that ends without one
  leaves the client hanging. A cancelled turn's `result` carries **no
  error** — the reference reports a cancellation through the composer, not
  as a failure.
- **The frames are written with the connection's context, not the turn's.**
  A cancelled turn still owes the client its terminator, and a write on the
  cancelled context would fail exactly when it is needed.
- **The `prompt` carries the transcript.** The server keeps no conversation;
  the client owns it.

`agentlink.Compact.ElapsedMS` exists because the client **cannot** measure a
compaction that ran on the server: the duration travels with the frame.

`devutils/agentsmoke` is the shortest complete client: it greets a server,
asks for a turn and prints the frames. It is the end-to-end smoke step for
this path.

## The turn seam: `agentturn`

`internal/app/agentturn` is the seam between *what a turn is* and *where it
runs*. Both engines answer with the same `agentlink.Frame`s, so the TUI's
transcript does not know which one it is rendering — the two paths cannot
drift apart.

```go
type Engine interface {
    Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error)
}
```

| engine | what it does |
|---|---|
| `agentturn.Local` | runs the turn **here**, over `internal/agent.Loop`; this is what a server runs, and what the local session runs |
| `agentturn.Remote` | sends the transcript to a server and reports the server's frames unchanged |

The local engine is the reference implementation of the protocol. The remote
engine has one subtlety worth knowing: **the connection's frames are read by
a reader of their own**. A read interrupted by a turn's cancellation would
close the websocket (`coder/websocket` closes the connection when a read is
cancelled), which would take the *session's next turn* with it. The reader
goroutine therefore lives for the whole session, and the cancel frame is sent
on the still-open connection.

## The host: `agentserver`

`internal/app/agentserver` owns the session:

- the **workspace** (the folder the tools act on) and the extra `--add-dir`s;
- the **tool set** — the same registry, the same permission policy and the
  same sandbox the print path uses (`setup.PolicyFrom`, `setup.SandboxFrom`);
- the **prompt** — the reference's own, byte for byte (`agyprompt.Build`);
- the **transport** — resolved *per turn* from the model the client asked
  for, so a client can switch the model without reconnecting.

It has no dependency on `internal/cliapp` (that would be an import cycle), so
the session's setup is rebuilt from `internal/app/session/setup`,
`internal/app/tools`, `internal/app/toolrunner` and
`internal/app/prompt/agyprompt`.

The four cores the server shares with the other two hosts — the permission
policy, the terminal sandbox, the discovered subagents and the MCP wiring —
live in `internal/app/session/setup`, **below** every host. A host never
imports another host: the server cannot import `internal/cliapp` (a cycle)
and it must not import `internal/printmode` either, because an entry surface
is above a tier package. See
[`architecture-tiers.md`](architecture-tiers.md), rule 5.

Each connection gets **its own** conversation id: two clients on one server
must not share one.

### The bind address is the server's fact

The server binds `127.0.0.1:7777` unless the command line says otherwise,
and the **loopback is the default on purpose**: the server runs shell
commands against its own workspace, so it must not listen on the network
unless the user asks for it. Those facts live in
`internal/app/agentserver/addr.go` — `DefaultHost`, `DefaultPort` and
`ResolveAddr(addr, host string, port int) string`.

The signature takes **plain values**, not `cli.ServerArgs`: the server is
tier 2 and may not import `internal/cli`, which is a host. The host maps
its own argv carrier onto the three values and asks.

The flag *registration* stays in the host (`internal/cliapp/server_host.go`'s
`serverFlagSet`): a flag's registration belongs to the parser, while a
flag's *policy* belongs to the tier that owns the state it names.

## What the TUI does with the frames

`internal/cliapp/tui_stream_prompt.go` was rewritten around a
**`turnCollector`**: the frames are the protocol's, not the transcript's, and
the collector is the one place that knows how a frame becomes a row.

| frame | the row it produces |
|---|---|
| `text` | the streamed answer (`backendChunkMsg`) |
| `thought` | the thought row — opened by the **first** delta, filled by the rest |
| `tool` (`tool_start`) | the running tool row (`toolRunningMsg`) |
| `tool` (`tool_end`) | the finished tool row (`toolStepMsg`), with the tool's own output |
| `compact` | the compaction spinner and its summary |
| `notice` | the auto-continue banner, the eligibility notice, or the error text |
| `result` | the turn's end: the whole answer, the usage, the error |

The `result` frame's reply **replaces** what the text deltas accumulated —
it is the whole answer, including the tail the sanitizer held back. That is
the reference's own behaviour: the tail is not streamed live.

## What this does **not** touch

The remote-control surface is a different thing and it is unaffected:
`pigasus web`, `pigasus remote-control serve|run|start`, the Web UI's
`/connect-websocket` transport, the `/rpc` websocket, the terminal the
Web UI creates (`CreateTerminal` / `ListTerminals` /
`SendTerminalInput` / `StreamTerminalOutput` / `CloseTerminal`) and the
cloud tunnel the dashboard reaches.

The two live in the same process, so the split had to be *checked*, not
assumed — and it was: the terminal round trip was driven end to end
against a running `pigasus web` and a running `remote-control serve`,
over the Connect JSON dialect and over both websockets.
`internal/cli/remote_control_surface_test.go` pins it against a parser
that carries this pair, so a future subcommand cannot quietly take one
of them.

The one thing they share is the *transport library*
(`github.com/coder/websocket`): the agentlink wire and the
remote-control websockets are different protocols on the same
connection type.

## 硬约束 / 存疑 / limitations

- **硬约束**: the server demands a token, and the check happens **before**
  the websocket upgrade. A server with no configured token mints one.
- **硬约束**: a turn ends with exactly one `result` frame — a cancelled turn
  included. Without it the client waits for a frame that never comes.
- **硬约束**: the frames are written with the **connection's** context. A
  cancelled turn's terminator is written on a live connection.
- **硬约束**: the remote engine's reader goroutine lives for the whole
  session. A read interrupted by a turn's cancellation closes the websocket.
- **硬约束**: the transcript travels with the prompt. The server keeps no
  conversation of its own, and every connection gets its own conversation id.
- **硬约束**: the TUI's local path and the remote path render the **same**
  frames through the same collector. A difference between them is a bug in
  the collector, not in the paths.
- **存疑**: the protocol's frame names and the shape of the `prompt` are the
  clone's own design. The reference (`agy` 1.4.11) has **no** client/server
  mode of this shape — this is a clone extension, not a recovered contract.
  The wire is therefore a promise of *this* repository only.
- **存疑**: the tool calls travel with the transcript as *declarations*, but
  the tool **results** do not: the transcript the client sends carries the
  user and assistant messages, and the tool calls and results live in the
  separate trajectory the reference keeps. A conversation that continues
  across a server restart therefore replays the user's and the assistant's
  messages, not the tool's output.
- **limitations**: **no interaction round trip**. The server runs the tools
  under its own permission policy; there is no approval prompt that travels
  to the client. A tool the policy denies is denied on the server.
- **limitations**: **no subagents on the server**. The client's subagents run
  locally, if at all; the server's prompt lists the subagent types but does
  not run them.
- **limitations**: the remote session's diff preview reads the file from the
  **client's** disk, so a file the server changed is previewed against the
  client's copy — the preview degrades rather than failing.
- **limitations**: one turn at a time per connection. A second turn while one
  is in flight is refused, because it would read the first one's frames.
- **limitations**: the local transport is still built by the attached client
  (the model switch panel asks for one), but it is never used for a turn —
  the remote engine owns the turn.

---

Reports: [`cycle-374-agent-server-client-split`](../cycles/cycle-374-agent-server-client-split/deliverable.md).

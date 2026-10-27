# Cycle `rc-web-chat-continuity` — the web chat is one conversation

**Status**: done
**Scope**: `pigasus web` / the cloud remote view — every message in a
conversation was answered as if it were the first one.

## Gap

The Antigravity Web UI's composer sends **one message at a time**. The
message before it is not in the prompt the server receives: the frontend's
`SendUserCascadeMessage` carries the new text and the conversation's own id,
and nothing else. A server that answers that prompt on a fresh transcript
answers each message in isolation — the chat has no memory of itself.

Measured in the browser before the fix (cycle `rc-cloud-remote-view`'s web
view): the first message asked "the first question", the second asked "the
second question", and the model's request carried the second prompt alone.

## Root cause

The remote-control turn engine mints a **fresh** `*pigrt.Runtime` per turn:
`piagentbridge.NewRemoteCascadeSession`'s factory builds a new session for
every `SendStream` call, because the daemon has no session to resume. A
PiG session owns its transcript, so a fresh session starts empty — and
nothing ever put the conversation's earlier turns into it.

The seam for that already existed and was used by the CLI: `pigrt.Runtime.
SeedHistory([]pigrt.HistoryTurn)` appends turns to the session's log, and the
session re-projects the log before every provider request. The print path
uses it for `-c` (`internal/printmode/print_pig.go`); the remote-control path
did not use it at all. The previous cycle's report recorded this as a known
limit: *"A resumed turn does not replay the stored history into the model:
`pigrt.Runtime.SeedHistory` exists and is not wired to the remote-control
path."*

Two facts decided the shape of the fix:

1. **On this path the conversation id is the cascade id.** The frontend mints
   the uuid and sends it as the cascade id; the clone stores the turn under
   it (`lsStoreTurn` → `conversations/<id>.db`). `MessagePrompt.
   ConversationID` is *not* the conversation id here — the PiG backend never
   sets `NewConversationID`, so it stays empty. The history therefore
   resolves by `c.ID`.
2. **The store is written after the turn.** `lsStoreTurn` appends the user
   input and the model's steps when the turn ends, so a turn's own prompt is
   never in the history it reads.

## Fix

`internal/remotecontrol` resolves the conversation's earlier turns and hands
them to the backend; `internal/piagentbridge` replays them into the session.

| File | What |
|---|---|
| `internal/remotecontrol/ls_context.go` (new) | `lsConversationHistory(id)` — the conversation's chat turns, from the store |
| `internal/remotecontrol/backend.go` | `MessageHistoryTurn` + `MessagePrompt.History` |
| `internal/remotecontrol/dispatch_ls.go` | `runLSCascadeTurn` fills `prompt.History` |
| `internal/piagentbridge/piagent_remote.go` | the factory seeds the runtime with `rt.SeedHistory(...)` |

The rules are the clone's own, not new ones:

- **Which turns** — `Conversation.ChatTurns()`: the user and assistant prose
  in order. The tool calls are dropped, and they are dropped **everywhere**:
  the seam is prose-shaped (`pigrt.HistoryTurn` is a role and a text), and
  the clone's other two paths drop them the same way — the TUI's resumed
  conversation seeds its session with the same prose, and `--print -c`
  seeds the same way. The store *does* keep the tool results
  (`convstore.Step.ToolResult`, recovered from the reference's own blob), so
  the seam could carry them; that is a change to all three paths at once.
- **How much** — `convstore.LastChatTurnsBudget(convstore.DefaultHistoryTurns,
  convstore.DefaultHistoryChars)`: 40 turns and 100 000 characters, the caps
  the `--continue` path uses.
- **When** — the history is read before the turn and the turn's own steps are
  appended after it, so the message being sent is never in its own history.
- **How** — `pigrt.Runtime.SeedHistory`, the same seam `--print -c` uses, so
  the wire is byte-identical to the CLI's resumed turn.

The replay is **best-effort**: a store that cannot be read leaves the session
as it was and the turn runs (the print path's implicit-trajectory rule). The
failure is logged.

## Evidence

【一手】 measured in the browser, against a local OpenAI-compatible mock model
(`pigasus web` on `127.0.0.1:8899`, an isolated `HOME`, a provider pointing
at `http://127.0.0.1:8900/v1`):

```
first message  →  POST /v1/chat/completions
                   system: <identity> You are Antigravity, …
                   user: the first question

second message →  POST /v1/chat/completions
                   user: the first question
                   assistant: I received 2 message(s): …
                   system: <identity> You are Antigravity, …
                   user: the second question
```

The second request carries the first turn ✓ — before the fix it carried the
system prompt and the new prompt only.

【一手】 the same request shape comes out of the CLI's own `--conversation`
path on the same store: the seeded turns land ahead of the system prompt,
then the new prompt follows. The web path and the CLI path agree.

【strings】 the reference's own `exa.cortex_pb.CascadeConfig` carries the
field `use_db_trajectory` — "load the trajectory from the db" — next to
`conversation_history_config`
(`out/local-macos/protos/cortex_1.4.6.txt`), so the reference resolves the
context from the store the same way.

【一手】 the reference's own language server answers a message sent into a
conversation it did not start, and that conversation's file grows by two
steps — the message and the answer — with the earlier steps still in place.

## Tests

- `internal/remotecontrol/ls_context_test.go` (new): the second message of
  a conversation is answered against the first (the dispatcher level, with a
  recording backend); a conversation the daemon has no cascade for carries
  its stored history (the resume path); a conversation that does not exist
  replays nothing.
- `internal/piagentbridge/piagent_remote_test.go` (new): the provider
  request carries the earlier turns, and a turn without a history asks the
  model exactly one question.

`go build ./cmd/...`, `go vet` on the two packages, and
`go test ./internal/remotecontrol/ ./internal/piagentbridge/ ./internal/
convstore/ ./internal/pigrt/` are green.

## Hard constraints / 存疑

- **The replay is the prose only.** A tool call from an earlier turn is not
  in the model's context, so a follow-up that asks "what did you just read?"
  is answered from the model's summary of what it did, not from the tool
  result. The tool calls of the **current** turn are in the context — the
  agent runs its tool loop inside the turn — so the gap is only about the
  earlier turns. The store keeps the tool results (`convstore.Step.
  ToolResult`), but the seam is prose-shaped (`pigrt.HistoryTurn` is a role
  and a text), so carrying them means changing the seam and all three
  paths that use it (the TUI's resumed conversation, `--print -c` and this
  one), not this one alone.
- **The history is bounded by the caps**, not by the model. 40 turns and
  100 000 characters; a longer conversation is truncated from the front, so
  the oldest turns fall out of the context.
- **The seed happens per turn.** Each turn mints a new session and replays
  the history into it, so the cost of the replay grows with the
  conversation. A single session per cascade would avoid that, but the
  daemon's cascade registry is in-memory and a resumed cascade would then
  have no session to resume.
- **The PiG session is still persisted per turn** (`NoSession` is not set on
  this path), so the seeded turns also land in PiG's own session files. That
  is pre-existing behaviour and was left alone.

## Open questions

- Whether the reference's own web view replays the **tool calls** as well as
  the prose. The `use_db_trajectory` flag says the trajectory is loaded from
  the db, and the reference's trajectory holds the tool calls — but the
  reference's own tool-result *message* format is not recovered, so the
  byte-level comparison is 【推测】.

## Limitations

- The fix covers the **LanguageServerService** path the Antigravity Web UI
  speaks (`StartCascade` / `SendUserCascadeMessage`). The clone's legacy
  JSON-RPC surface (`StartRemoteAgentCascade` /
  `SendRemoteAgentCascadeMessage`) still runs every turn on an empty
  session: there the conversation id is minted by the backend, not by the
  client, so the store cannot be resolved by the cascade id.
- The history is read from the **conversation store**, so a conversation the
  reference wrote is replayed in the clone's own prose shape — the reference's
  own blobs are read for the *display* (`ls_page.go`), not for the model's
  context.

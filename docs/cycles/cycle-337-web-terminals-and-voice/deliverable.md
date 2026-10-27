# The Web UI's terminal and its voice — deliverable

**Cycle:** `cycle-337-web-terminals-and-voice`
**Subject:** the desktop UI's **Terminal** tab and its **microphone** were
drawn but not served. The clone answered the dashboard's
`RemoteAgentTerminal*` contract, the reference answers five *different*
LanguageServerService methods for the terminal and three more for the
voice, and the bundle (`internal/rcfrontend/assets/main.js`, the
reference's own bytes) calls the reference's names. This cycle wires the
reference's own contracts: a pty-backed terminal registry, a voice
session registry, and the Connect error codes the reference answers with.
**Evidence:** 【一手】 the full reference-vs-clone transcript of the
terminal contract (`out/web-terminals/terminal-transcript.txt`); 【一手】
the error shapes side by side (`out/web-terminals/error-shapes.txt`); 【一手】
the two voice transcripts (`out/web-terminals/audio-transcript.txt`); 【一手】
the browser (`out/web-terminals/browser-dom.txt`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the Terminal tab** | drawn, opened an empty pane | opens an xterm bound to a **live shell on a pty** |
| **the five terminal methods** | not routed (the alias table sent them to the dashboard's contract) | routed to the reference's own handlers |
| **the terminal's answers** | — | the reference's shapes, field for field (`ackedOffset` as a string, `{}` for a zero offset, `{}` for an empty list, the canonicalised workspace URI, the `split-<root>` group) |
| **the microphone** | `Unknown response case: undefined`, `Failed to start: ConnectError: [canceled]` | the reference's three methods, the reference's frames, and a **real transcription** |
| **a failed call** | HTTP 200 `{}` for everything | the reference's code and status, for the failures that name one |

---

## 1. The contract, measured

【一手】 2026-10-27, against the reference's own `language_server`
(`127.0.0.1:64019`, `-csrf_token=…`), with the RPC path
`POST /exa.language_server_pb.LanguageServerService/<Method>`, a unary
call on `content-type: application/json` and a stream on
`application/connect+json` (the 5-byte envelope: a flag byte and a
big-endian uint32).

### The terminal

| call | the reference's answer |
|---|---|
| `CreateTerminal{workspaceUri,conversationId,projectId}` | `{"terminal":{"terminalId":…,"pid":…,"title":"bash","conversationId":…,"projectId":…,"workspaceUri":"file:///private/tmp/web-ws","createdAt":…}}` |
| `CreateTerminal{splitFromTerminalId:<root>}` | the same, plus `"splitGroupId":"split-<root id>"` |
| `ListTerminals{conversationId,projectId,workspaceUri}` | `{"terminals":[…]}` — and **`{}`** when the filter matches nothing |
| `SendTerminalInput{input,inputStreamId,inputOffset}` | `{"ackedOffset":"18"}` — a **string** |
| `SendTerminalInput{resize:{cols,rows}}` | **`{}`** — the offset is zero, and proto-JSON omits a zero field |
| `SendTerminalInput` with an offset that skips ahead | the chunk is **dropped**; the acknowledged offset does not move |
| `CloseTerminal{terminalId}` | `{}`; a second close answers `404 not_found` |
| `StreamTerminalOutput{terminalId}` | frames `{"output":"<base64>"}` and `{"title":"bash"}`, then the end-of-stream envelope |

Two rules worth their own lines:

- the **workspace URI is canonicalised**: `file:///tmp/web-ws` comes back
  as `file:///private/tmp/web-ws` (on macOS `/tmp` is a symlink);
- the **split group is named after its head**, and the head terminal only
  learns its own group once a split is created *from* it — the create
  answer of the head carries no `splitGroupId`.

### The voice

| call | the reference's answer |
|---|---|
| `StreamAudioTranscription{mimeType:"audio/pcm;rate=16000",…}` | a stream: `{"ready":{"sessionId":…}}`, then `{"transcription":{"text":…}}` for the partials, then `{"transcription":{"text":…,"isFinal":true}}`, then `{"complete":{}}`, then the end-of-stream envelope |
| `SendAudioChunk{sessionId,data,sequenceNumber}` | `{}` |
| `EndAudioSession{sessionId}` | `{}` — and the final transcription arrives on the stream *afterwards* |

### The errors

| request | the reference's answer |
|---|---|
| `CreateTerminal{"workspaceUri":123}` | `400 {"code":"invalid_argument","message":"unmarshal message: unmarshal into *language_server_go_proto.CreateTerminalRequest: proto: (line 1:18): invalid value for string field workspaceUri: 123"}` |
| `SendTerminalInput{…,"input":"hello"}` | `400 {"code":"invalid_argument","message":"… invalid value for bytes field input: \"hello\""}` |
| `SendTerminalInput{"terminalId":"no-such-terminal",…}` | `404 {"code":"not_found","message":"not_found: terminal no-such-terminal not found (error ID: 1e810286922440fd9ed35e5d0b1db3cf)"}` |
| `EndAudioSession{"sessionId":"no-such-session"}` | `404 {"code":"not_found","message":"not_found: audio session not found: no-such-session (error ID: …)"}` |
| an unknown method | `404` with the plain text `404 page not found` |

---

## 2. What was wrong

Three separate things.

**The routing.** `CreateTerminal`, `ListTerminals`, `SendTerminalInput`,
`CloseTerminal` and `StreamTerminalOutput` are all in the clone's alias
table (`internal/remotecontrol/reference_surface.go`), where they point at
the dashboard's own `RemoteAgentTerminal*` methods. The clone answered the
*dashboard's* contract under the *Web UI's* names.

**The surface.** There was no terminal at all: the clone's `TerminalsRegistry`
(`internal/remotecontrol/terminals_registry*.go`) is the dashboard's
cascade-terminal bookkeeping — it has no pty, no output stream, and no
answer for `StreamTerminalOutput`.

**The voice.** `StreamAudioTranscription`, `SendAudioChunk` and
`EndAudioSession` were not routed at all, so the stream ended immediately
with an empty body; the bundle's `message.case` was `undefined`
("Unknown response case: undefined") and the microphone reported
`Failed to start: ConnectError: [canceled]`.

And one more, found while measuring: the clone answered **HTTP 200 `{}`**
for *every* failed dispatch, so a terminal that was not there looked like
a success. The reference answers 404 with the code.

---

## 3. What this cycle ships

### 3.1 `internal/rcterminal` — the terminal registry

A registry of live terminals, each a shell on a pty
(`github.com/creack/pty`):

| file | what |
|---|---|
| `rcterminal.go` | the registry: create, list (with the three filters), send, close, stream |
| `session.go` | the session: the pty, the output pump, the subscribers, the scrollback |
| `session_unix.go` / `session_windows.go` | the pty, and the Windows stub |
| `osc.go` | the OSC scanner: `OSC 0` (the title) and `OSC 7` (the cwd), including a sequence split across two reads |
| `ids.go` | the uuid-shaped ids |
| `rcterminal_test.go` | the rules: the create answer, the split group, the list filters, the acknowledged offset, the stream, the close, the canonicalised URI, the OSC scanner |

The rules the registry keeps: the input is acknowledged **per
`inputStreamId`** as a byte offset; a chunk whose offset does not line up
is dropped; the list filters on each field independently and an empty
result is an empty list (the handler omits it, the way proto-JSON does);
a split joins its source's group and the head learns the group only when
a split is created from it.

The spawn is `$SHELL -l` in the workspace — what the reference does, and
the title is the shell's name.

### 3.2 `internal/rcaudio` — the voice sessions

| file | what |
|---|---|
| `rcaudio.go` | the session registry, the session (the bytes, the frames, the end), the `Transcriber` seam |
| `cloudcode.go` | the Gemini-backed transcriber: the recording rides an `inlineData` part, `audio/pcm;rate=16000` |
| `rcaudio_test.go` | the frame sequence, the failure path, the registry, the idempotent end |

The reference's STT is **local**: its `language_server` produces the
transcript without a cloud call. The clone has no local engine, so the
recording rides the Gemini transport the clone already speaks — the same
`inlineData` part an image attachment uses, MIME-agnostic mechanically.

【一手】 the transport answers a synthetic sample (16 kHz mono s16le,
"Hello terminal, this is a test.") with **"Hello terminal, this is a
test."** — the reference's own engine answers "Hello terminal." for the
same bytes.

The difference is *when* the text arrives: the reference streams a partial
hypothesis while the user speaks; the clone transcribes once, when the
session ends. One model call per recording instead of one per second of
speech.

The model is `gemini-3.8-flash-high` unless `$OPENAGY_AUDIO_MODEL` says
otherwise; the token is the macOS keychain's, like every other CloudCode
call.

### 3.3 The LS handlers

| file | what |
|---|---|
| `internal/remotecontrol/ls_terminals.go` | `CreateTerminal`, `ListTerminals`, `CloseTerminal`, `SendTerminalInput` |
| `internal/remotecontrol/ls_terminals_stream.go` | `StreamTerminalOutput` |
| `internal/remotecontrol/ls_audio.go` | `StreamAudioTranscription`, `SendAudioChunk`, `EndAudioSession` |
| `internal/remotecontrol/ls_unmarshal.go` | the reference's message for a malformed request |

The routing is the **LS table first** (`dispatch.go`'s `routeLanguageServerMethods`
runs before `canonicalMethod`), so the Web UI's names reach the Web UI's
handlers and the dashboard's `RemoteAgentTerminal*` contract is untouched.

### 3.4 The Connect error codes

`internal/remotetransport/connect_errors.go` maps a failure that **names a
code** onto the Connect code and the HTTP status the reference answers
with:

| JSON-RPC | Connect | HTTP |
|---|---|---|
| `-32602` | `invalid_argument` | 400 |
| `-32018`, `-32019` | `not_found` | 404 |

The body is `{"code":…,"message":…}`. The dispatcher carries the code
through a duck-typed `JSONRPCCode()` / `ConnectMessage()` pair
(`internal/remotecontrol/dispatch_helpers.go`'s `rpcCodedError`) — the
dispatcher must not import the transport, so the interface is duck-typed.

A failure that names **no** code keeps the catch-all (HTTP 200, empty
body): the page calls methods the clone does not implement, and those
must not take the page down. That is why the unknown-method answer
differs from the reference's `404 page not found` — deliberately.

---

## 4. The live diff

【一手】 `out/web-terminals/terminal-transcript.txt` — the same script
against both servers. Every line matches except two:

```
-- input to an unknown terminal:
   reference: 404 {"code":"not_found","message":"not_found: terminal no-such-terminal not found (error ID: 83bec7427fd246c694520ddbc9008feb)"}
   clone:     404 {"code":"not_found","message":"not_found: terminal no-such-terminal not found"}
```

The reference's error carries a **trace id**; the clone has no trace ids
and does not fake them.

The frames match too, including the `{"title":"bash"}` frame (the clone
sends it when the stream opens; the reference's arrives a few frames
later, after its terminal manager has learned the title asynchronously).

【一手】 `out/web-terminals/audio-transcript.txt` — the two voice
transcripts.

【一手】 `out/web-terminals/browser-dom.txt` — the browser: the Terminal
tab opens an xterm, `pwd` answers with the workspace path, the composer
carries the microphone button.

---

## 5. The roads not taken

| candidate | measured |
|---|---|
| route the Web UI's names to the dashboard's `RemoteAgentTerminal*` | the answers' shapes differ (no `pid`, no `title`, no `ackedOffset`, no output stream) — a separate surface, not an alias |
| answer the voice with the clone's configured provider | the configured provider is an OpenAI-dialect gateway; it has no audio input |
| keep the HTTP 200 `{}` masking for the terminal's errors | the reference answers 404 with `not_found`; the page shows the difference |
| map *every* JSON-RPC code onto a Connect code | the clone's `-3200x` family (project, conversation, permission) is its own invention; mapping it would be a guess, so those keep the catch-all |
| stream the transcription with a model call per chunk | a model call per second of speech; the reference's engine is local and free, this one is not |

---

## Known limits

- 【一手】 the reference's errors carry a trace id (`(error ID: <32 hex>)`)
  and its unmarshal errors a position (`(line 1:18)`); the clone has
  neither — Go's decoder does not report a position, and the clone does
  not generate trace ids.
- 【一手】 the clone's transcription is **one-shot**: the text appears when
  the session ends, not while the user is speaking.
- 【一手】 the unknown-method answer is `200 {}` in the clone and
  `404 page not found` in the reference — deliberately (see above).
- 【推测】 whether the reference's Terminal tab can split a terminal into
  several panes was not measured in the browser; only the RPC side of a
  split was.
- 【一手】 the window size goes through the pty's `TIOCSWINSZ`; the
  reference's mechanism was not traced.
- 【一手】 the clone's terminal is **not** the reference's terminal: the
  reference's own terminal manager is a separate process's worth of
  machinery (its own title bookkeeping, its own group rules), and the
  clone reimplements the contract, not the manager.

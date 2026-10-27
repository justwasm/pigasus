# Cycle `rc-web-parity` — the reference as a backend, the bubble's height, and the missing think trace

**Status**: done
**Reference**: Antigravity IDE's `language_server`, LS version `1.11.0`,
sha256 `6f7e5004629070ac0c4651a1dc29030eaadcd5dc68fa9edf170047d7f8424f4d`
**Scope**: `pigasus web` — the three asks of the review round: a
`--reference-backend` flag, the user-message bubble's height, and the
collapsible "Thought for Xs" trace.

## TL;DR

1. **`pigasus web --reference-backend=<endpoint|binary>`** swaps the
   clone's own `LanguageServerService` implementation for a proxy to a
   running reference `language_server`. The browser still talks to the
   clone; every RPC is answered by the reference, so the UI shows the
   reference's own catalogue, its own conversation titles and its own
   answers. Verified live: the picker lists the reference's
   `gemini-3.1-pro`, and the reference's mocked agent answers
   "Hello! I am your mocked Jetbox agent. How can I help you today?".
2. **The user-message bubble is 36px again** (it was 72px). The clone's
   own touch patch had re-flowed the action-button row into the bubble
   (`position: relative` + `flex-direction: column`); the reference keeps
   the row absolutely positioned in the bubble's bottom-right corner, so
   the bubble's height is the height of its text.
3. **The think trace is back.** The Web UI renders a planner-response
   step's `thinking` in a collapsible panel labelled "Thought for Ns"
   (or "Thought Process" when the step carries no duration). The clone's
   step carried only `response`, so the panel never appeared — and the
   `<think>…</think>` block the CloudCode transport wraps around a
   model's reasoning ended up **inside the answer bubble** instead. The
   clone now splits the markers out, keeps the reasoning, times it, and
   puts both on the step.
4. **`thinkingDuration` is a proto-JSON *string*** ("1.500s"), not an
   object. The frontend's protobuf-es decoder rejects the object form
   outright — and a rejected frame kills the **whole** trajectory
   stream, not just the field.
5. **The clone's own `LanguageServerService` answers are now in the
   reference's wire form.** The new shape diff
   (`out/ide-language-server/ls_shape_diff.py`) compares every method
   against the live reference: two of the six real gaps broke the UI —
   the project stream spelled its oneof the way protobuf-es *decodes*
   it, and the update status answered a string where the proto says a
   message — and the field names, the enum spelling and the empty
   frames are fixed too (§5).

## 1. `--reference-backend` 【一手】

### The wire

The reference speaks plain Connect-RPC over HTTP POST:

```
POST <base>/exa.language_server_pb.LanguageServerService/<Method>
Content-Type: application/json
{}
```

A **unary** call answers with the proto-JSON message. A
**server-streaming** call needs the Connect *streaming* codec: the
request body is one length-prefixed envelope (a flags byte, a big-endian
uint32 length, the payload) and the response body is a sequence of
them, chunked. The reference rejects `application/json` on a streaming
method with **HTTP 415** — the codec is not optional there.

```bash
curl -s -D - -X POST \
  http://127.0.0.1:8791/exa.language_server_pb.LanguageServerService/JetboxSubscribeToState \
  -H 'Content-Type: application/json' -d '{}'
# HTTP/1.1 415 Unsupported Media Type
# Accept-Post: application/connect+json, application/connect+proto, ...
```

`internal/refbackend` implements both: `callUnary` for the JSON codec,
`pumpEnvelopes` for the envelope stream. The list of the reference's
**24 server-streaming methods** was recovered from the IDE's own service
descriptor (`out/ide-language-server/extract_ls_descriptor.py`) and is
hard-coded in `internal/refbackend/connect.go`; a method outside that set
is a unary call.

### The flag

```
pigasus web --port 8899 --reference-backend=127.0.0.1:8791
pigasus web --port 8899 --reference-backend=/Applications/Antigravity.app/Contents/Resources/bin/language_server
```

The value is an **endpoint** (`host:port`, `http://host:port`,
`https://host:port`) or the **path of a reference binary**, which the web
server then starts for the lifetime of the command:

```
language_server -http_server_port=<free port> -gemini_dir=<tmp> \
                -standalone=true -persistent_mode=true -use_mocked_data=true
```

`-use_mocked_data=true` is on by default because the real catalogue needs
`loadCodeAssist` (network plus auth); `OPENAGY_REFERENCE_BACKEND_ARGS`
adds or overrides arguments. A `-gemini_dir` in there **wins**: the
spawn puts the effective directory last (the reference takes the last
occurrence of a flag), makes no temporary directory when the caller
named one, and never removes a directory it did not create.

The proxy is a *pure* proxy: the clone's own dispatcher is not
consulted, so a difference the user sees in this mode is the
reference's own answer, not a mixture. A method the reference does not
implement is reported with the transport's own "Method not found"
spelling, which the transport turns into an empty object instead of an
error frame — the Web UI must not break on a method the peer lacks.

### Live, in Chrome

`pigasus web --port 8902 --reference-backend=127.0.0.1:8791`:

* the model picker lists **`gemini-3.1-pro`** — the *reference's* mocked
  catalogue, not the clone's configured models;
* the sidebar shows the reference's own conversation list, and the
  conversation it created is titled **"Mocked generated title"**;
* the answer is the reference's mocked agent's, and the thought panel
  reads **"Thought Process"** — because the reference's own mocked step
  carries `thinking` but **no** `thinkingDuration` (see §4).

The **spawn** mode was checked the same way
(`--reference-backend=/Applications/Antigravity.app/Contents/Resources/bin/language_server`):
the web server started its own reference on a free port, the picker and
the turn answered from it, and killing the web server took the child
with it and removed the temporary gemini directory.

## 2. The bubble's height 【一手】

The reference's user-message bubble is the text plus its padding: the
action-button row (Undo, Copy, Timestamp) is absolutely positioned in
the bubble's bottom-right corner and floats over it.

The clone's own touch patch (`internal/rctunnel/patches/registry.go`,
`mobile-user-message-actions`) had replaced that className with a
*relative*, in-flow row, and the injected CSS (`safe-area-insets`) forced
the bubble into `flex-direction: column` so the row could sit under the
text. Every bubble grew by the row's height: **72px** for a 24px line.

The fix keeps the reference's geometry and neutralises only the hover
gating (a touch device has no hover):

```
absolute bottom-0.5 right-0.5 flex flex-row items-center p-1 rounded-full
opacity-90 pointer-events-auto transition-all bg-card user-input-buttons-shadow
user-input-buttons-container select-none
```

Measured in Chrome against the running clone, same message, same
viewport:

| | before | after |
|---|---|---|
| bubble height | 72px | **36px** (24px text + `p-1.5`) |
| button row | in flow, `position: relative` | `position: absolute`, `bottom-0.5 right-0.5` |

## 3. The think trace 【一手】

### What the frontend renders

The IDE bundle's own code (`internal/rcfrontend/assets/main.js`):

* the planner-response renderer reads `rawThinking || thinking` for the
  text and `thinkingDuration` for the label;
* the panel's component returns **null** when the text is empty — no
  thought text, no panel at all;
* the label is `Thought for ${Math.max(1, Number(seconds))}s` when a
  duration is present, and **"Thought Process"** when it is not.

### Where the clone's thinking comes from

The CloudCode transport wraps a model's reasoning in `<think>…</think>`
markers (`internal/backend/cloudcode_stream.go`: `thinkOpen` /
`thinkClose`). The reference's own CLI strips that block before it
prints the answer; the Web UI shows it. The clone's web path had
neither: the markers went straight into the answer bubble and no panel
was rendered.

`internal/remotecontrol/ls_thought.go` does the split:

* a streaming scanner (`lsThinkScanner`) separates the visible text from
  the thinking, holding back the tail of a chunk when it could be the
  start of a marker — the transport splits one think block across many
  chunks, so a per-chunk match would miss the boundary;
* the whitespace that follows a think block is dropped, so the answer
  does not start with a paragraph break (the reference CLI's rule);
* the think block's wall-clock span is measured, and the thinking is
  carried on the trajectory step as `thinking` + `thinkingDuration`.

### The `Duration` wire form

`exa.cortex_pb.CortexStepPlannerResponse.thinking_duration` is a
`google.protobuf.Duration`, and proto-JSON encodes a Duration as a
**string** ("1.500s"). The frontend's protobuf-es decoder says so
outright:

```js
case "google.protobuf.Duration":
  if (typeof b !== "string") throw Error(`cannot decode message ${a.$typeName} from JSON: ${cc(b)}`);
  f = b.match(/^(-?[0-9]+)(?:\.([0-9]+))?s/);
```

The clone's first attempt sent `{"seconds": "1"}`. The browser answered:

```
[consumeAgentStateStream] streamAgentStateUpdates error:
Error: cannot decode message google.protobuf.Duration from JSON: object
```

and the **whole trajectory stream died** — the reply never appeared. The
clone now sends the string form.

### Live, in Chrome

Against the clone with a reasoning model (a throwaway OpenAI-shaped
gateway that answers `<think>…</think>Hello …`):

* the answer bubble reads **"Hello from the thinking gateway!"** — no
  markers, no leading blank lines;
* the panel above it reads **"Thought for 1s"**, and expanding it shows
  the reasoning text;
* the console is clean.

## 4. The reference's own planner step 【一手】

Captured off the wire with `out/ide-language-server/ref_ls_turn.py`
(the reference, `-use_mocked_data=true`, one turn):

```json
{"type": "CORTEX_STEP_TYPE_PLANNER_RESPONSE",
 "status": "CORTEX_STEP_STATUS_DONE",
 "metadata": {"sourceTrajectoryStepInfo": {...}},
 "plannerResponse": {
   "response": "Hello! I am your mocked Jetbox agent. How can I help you today?",
   "modifiedResponse": "Hello! I am your mocked Jetbox agent. How can I help you today?",
   "thinking": "Mocked thinking details."}}
```

Two things the clone did not know before this capture:

* `thinking` is a **plain string** ✓ (the clone's shape);
* the mocked step carries **no `thinkingDuration`** — which is why the
  reference's own panel reads "Thought Process", not "Thought for Ns".
  The duration is the real model's field, not the mock's.

The reference streams the step **incrementally**: every frame carries the
whole step list with the response grown by a few characters
("Hello!" → "Hello! I" → …), and the status moves RUNNING → DONE.

## 5. The clone's own answers, against the reference's 【一手】

`--reference-backend` makes the difference between the clone and the
reference *observable*: both answer the same `LanguageServerService`
methods, so their shapes can be compared method by method.
`out/ide-language-server/ls_shape_diff.py` does that — it calls every
method on both servers, flattens the answers into `path: type` lines and
prints the ones that differ:

```bash
python3 out/ide-language-server/ls_shape_diff.py \
    --clone http://127.0.0.1:8901 --reference http://127.0.0.1:8791 \
    --only-diffs
```

A **streaming** method is called with the Connect streaming codec (a
five-byte envelope per frame); the reference answers `application/json`
on a streaming method with 415.

The first run found 25 methods whose shapes differ. Most of them are
*value* differences (the reference is mocked: its catalogue, its plan
name, its diagnostics) or the reference's own errors (it answers 400,
404, 500 or 501 to the empty request the diff sends). Six were real
gaps, and two of them broke the UI.

### The oneof's wire form 【一手】

`ProjectUpdatesStream` answers a message with one field, a **oneof**
named `update`:

```protobuf
message ProjectUpdatesStreamResponse {
  oneof update {
    ProjectList project_list = 1;
    string project_updated_id = 2;
  }
}
```

proto-JSON spells a oneof by its **member**. The reference's own stream
says so — its first frame is a Connect envelope carrying:

```json
{"projectList": {}}
```

The clone answered the *decoded* form instead:

```json
{"update": {"case": "projectList", "value": {"projectIds": []}}}
```

That is what protobuf-es builds **after** decoding a frame — the
frontend reads `frame.update.case` — but the decoder matches the JSON
keys against the message's **field names**, and "update" is not one of
them:

```js
if (!h && !c.ignoreUnknownFields) throw Error(`cannot decode ${a.desc} from JSON: key "${g}" is unknown`);
```

The Web UI's client always decodes with `ignoreUnknownFields: true`, so
the key is dropped silently: the oneof stays unset (`{case: undefined}`),
the frontend's `frame.update.case` matches no branch, and the project
list never arrives. The clone answers `{"projectList":{}}` now — the
reference's own bytes.

### `GetUpdateStatus` answered a string where the proto says a message 【一手】

`GetUpdateStatusResponse.status` is `exa.host_bridge_pb.UpdateStatus`
(`current_version`, `latest_version`, `update_available`). The clone
answered `{"status":"upToDate"}`; the frontend's decoder throws on it
("cannot decode message exa.host_bridge_pb.UpdateStatus from JSON:
string") and the update check never settles. It is a message now, with
the version the host advertised.

### The field names the descriptor knows 【反编译】

The IDE's own descriptor (`out/ide-language-server/extract_ls_descriptor.py`)
names the fields the clone got wrong — and a key the decoder does not
know is dropped, so a wrong name is a silent feature failure, not an
error:

| method | the clone sent | the descriptor says |
|---|---|---|
| `GetSidecarEvents` | `sidecars` | `events` |
| `GetRepoInfos` | `repoInfos` | `repos` |
| `GetAllCascadeTrajectories` | `trajectories` | `trajectorySummaries` (a map) |
| `ListTriggerOptions` | `triggers` | `options` |
| `JetboxSubscribeToSummaries` | `summaries` | `updates` (a map), `deletes` |

`Heartbeat` answers its own message now: `last_extension_heartbeat` is a
`google.protobuf.Timestamp`, which proto-JSON spells as a **string**
("2026-10-25T19:31:11.993337Z"), and the reference's own answer says
exactly that.

### The enums are spelled by name 【一手】

The reference's `JetboxSubscribeToState` carries

```json
{"appState":{"postOnboarding":{"completedSteps":["POST_ONBOARDING_STEP_TYPE_MANAGER_WELCOME", "…"]},"agentOnboardingCompleted":"AGENT_ONBOARDING_STATE_COMPLETED"},"userConfig":{"userSettings":{"themeMode":"THEME_MODE_DARK"}}}
```

— proto-JSON's rule for an enum is its **name**. The clone sent the
number `2`; the Web UI's decoder accepts both and maps the name back to
the number, so this was a byte difference, not a break. The clone spells
it by name now, and the store re-spells it when the Web UI writes the
state back.

### The streaming path's empty frame 【反编译】

A streaming method the dispatcher does not stream fell back to an empty
frame `{}` on the HTTP path, while the websocket path fell back to the
dispatcher's own answer. The reference's `ProjectUpdatesStream` answers
`{"projectList":{}}` and its `JetboxSubscribeToState` answers the app
state — an empty frame carries neither. Both paths fall back to the
dispatcher's answer now.

### Live, in Chrome

Against the fixed clone, a fresh page load and one turn: the console
carries no `[ProjectUpdates] Disconnected` warning (the reference-backed
UI does not log it either), the answer arrives, and the think trace
reads "Thought for 1s". The shape diff against the reference is down to
value differences and to the empty lists the clone deliberately keeps.

## 硬约束

* A `google.protobuf.Duration` on the wire is a **string** ("1.500s").
  The object form is not a "wrong value" — it makes the frontend's
  decoder throw, and one bad frame kills the whole trajectory stream.
* The step's `thinking` must be **non-empty** for the panel to render at
  all; the frontend returns null for an empty thought.
* `thinkingDuration` is **optional**: with it the label is "Thought for
  Ns", without it "Thought Process". The reference's own mocked step has
  no duration.
* The action-button row must stay **out of the bubble's flow**. Any
  `position: relative` + `flex-direction: column` on the bubble grows
  every user message by the row's height.
* In `--reference-backend` mode the clone's dispatcher must **not** be
  consulted: a mixture of the two implementations is not a comparison.
* A **oneof** is spelled on the wire by its **member**
  (`{"projectList":{}}`), not by the `{case, value}` envelope — that is
  protobuf-es's *decoded* form, and the decoder drops it (the Web UI's
  client sets `ignoreUnknownFields: true`, so a wrong key is silent).
* A message field is a **message**: a string where the proto says a
  message makes the frontend's decoder throw, and one bad frame kills
  the stream it is on.
* A key the decoder does not know is **dropped**: a wrong field name is
  a silent feature failure, not an error.
* proto-JSON spells an **enum by its name**, a `Timestamp` and a
  `Duration` as **strings**, and a 64-bit integer as a decimal string.

## 存疑

* The reference's **real** `thinkingDuration` was never observed — the
  mock does not send one. The string form follows proto-JSON's own rule
  and the decoder's own regex, but the reference's exact rendering
  (how many fractional digits it writes) is unverified.
* The clone's thinking duration is the **wall-clock span of the think
  block**, not the model's own thinking time. The reference's number
  comes from the planner itself, which the clone has no access to.
* The reference's `modifiedResponse` (a second copy of the answer) and
  `metadata.sourceTrajectoryStepInfo` are not carried by the clone's
  step. The frontend reads `response`, and the metadata's `source` is
  what the clone sends — the two are different fields of a different
  message, so the shapes are not comparable yet.
* The reference's `JetboxSubscribeToState` answers with an `appState`
  and a `userConfig`; the clone's own state store has a different
  shape. In `--reference-backend` mode the reference's shape is what
  the UI sees, which is the point of the flag, but the clone's own
  state store was not changed by this cycle.
* The reference's mocked answers are **hand-built per method**: the mock
  sends `supportsHookResultProtoBytes: false` where plain proto-JSON
  would omit the zero, and it answers `{}` where it has no data. The
  mock is therefore an oracle for the *field names*, not for every
  omission rule.
* The clone deliberately keeps the **empty lists** in `ListProjects`
  and `ReadProjects` (`projectIds`, `projects`, `notFoundIds`): the Web
  UI spreads them without a null guard (`[...a.localProjectIds]`), and
  the reference's mock omits them. The clone's answers are the safe
  ones, not the reference's bytes.
* The reference's **real** `UpdateStatus` values were never observed (the
  mock answers `{}`); the clone reports the version the host advertised.

## limitations

* The live think trace was produced with a **throwaway gateway** that
  emits `<think>` markers, because the configured provider (MiniMax
  through an OpenAI-shaped gateway) has no reasoning channel: the
  gateway transport does not map `reasoning_content` to a marker. The
  CloudCode path (Gemini Flash) is the one that carries markers in
  production, and it was not exercised live here — it needs the
  reference's OAuth token.
* The reference's **real** catalogue was not observed (it needs
  `loadCodeAssist`); the live comparison used `-use_mocked_data=true`.
* The reference's websocket endpoint (`/connect-websocket`) still closes
  on the first frame from a raw client; the proxy uses the HTTP POST
  path, which the reference answers correctly.
* The browser caches `main.js` for an hour
  (`Cache-Control: public, max-age=3600`): a frontend patch needs a
  **new port** to be picked up.
* The shape diff sends an **empty request** to every method, so the
  reference answers 400, 404, 500 or 501 to a number of them — those
  rows are not comparable. The diff ran against the **mocked**
  reference only.

## Reproducing

```bash
# 1. the reference, live
/Applications/Antigravity.app/Contents/Resources/bin/language_server \
    -http_server_port=8791 -gemini_dir=$(mktemp -d) \
    -standalone=true -use_mocked_data=true -persistent_mode=true &

# 2. the clone's own turn frames, and the reference's
python3 out/ide-language-server/ref_ls_turn.py 8791 "ping"

# 3. the two web modes
go build -o /tmp/pigasus ./cmd/pigasus
/tmp/pigasus web --port 8899 --host 127.0.0.1                    # the clone
/tmp/pigasus web --port 8902 --host 127.0.0.1 \
    --reference-backend=127.0.0.1:8791                          # the reference

# 4. the answers, side by side
python3 out/ide-language-server/ls_shape_diff.py \
    --clone http://127.0.0.1:8899 --reference http://127.0.0.1:8791 \
    --only-diffs

# 5. the regressions
go test ./internal/remotecontrol/ ./internal/refbackend/
python3 scripts/check_constraints.py
```

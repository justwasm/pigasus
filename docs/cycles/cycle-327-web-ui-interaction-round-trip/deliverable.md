# The Web UI can answer the tools that need the user — deliverable

**Cycle:** `cycle-327-web-ui-interaction-round-trip`
**Subject:** the interaction round trip — the tools that cannot answer
themselves, the waiting step the Web UI renders as a card, and the answer
that resumes the turn. Written against the reference's **own** bytes: its
real `language_server` serving its own web UI, captured live, and the
clone's own web UI run side by side with it.
**Evidence:** 【一手】 the reference's live waiting step and its live answer
payload, taken from the real `language_server` (not from a proxy);
【一手】 the IDE web bundle that renders the card
(`internal/rcfrontend/assets/main.js`); 【一手】 the authoritative protobuf
descriptor (`internal/lsproto/descriptor/language_server.protoset`);
【一手】 the two browser runs, side by side.

---

## 0. Summary

| | before | after |
|---|---|---|
| **`ask_question` in the Web UI** | the tool answered for itself: *"The user cannot be asked in this mode, so no answer was provided."* | the card appears; the user's answer becomes the tool's result |
| **`ask_permission` / `ask_custom_permission`** | always denied (`permissions.DeniedToolResult`) — the user was never asked | the user grants or refuses, and the verdict becomes the tool's result |
| **the turn** | never blocked, but the model got a refusal, not an answer | the turn **parks** on the waiting step and **resumes** when the answer arrives |
| **the seam** | none — each tool knew its own transport (or had none) | `internal/interaction`: a `Host` on the turn's context that a tool asks and a transport answers |
| **the waiting step** | none | `CORTEX_STEP_TYPE_ASK_QUESTION` (138) in `CORTEX_STEP_STATUS_WAITING` (9), carrying the questions twice — as the step's own `askQuestion` and as `requestedInteraction.askQuestion` |
| **the answer** | the legacy dashboard's `HandleRemoteAgentCascadeUserInteraction` only | the IDE dialect's `HandleCascadeUserInteraction`, resolved by the `(trajectoryId, stepIndex)` the answer names |
| **the completed step** | — | `status: 3` plus `completedInteractions[0].{request,response}` — the exchange, kept on the step |

The headless paths did not change: with no host on the context the three
tools still answer for themselves, and the TUI's subagent-question picker
still works where it worked before.

---

## 1. The audit — which tools need the user

The task was to fix *every* tool of this class, not just `ask_question`.
The audit ran over the reference's own tool surface, not over the clone's
names:

| source | what it is | the names |
|---|---|---|
| `out/ide-language-server/tools/matrix.tsv` | the CLI-vs-IDE tool matrix, 60 names | `ask_custom_permission`, `ask_permission`, `ask_question` are the only ones whose subject is *the user* |
| `internal/tools/catalog.go` | the clone's advertised surface — 60 names | the same three are real executors; the other 57 are placeholders or unrelated executors |
| `internal/tools/model_tools_1.4.6.json` | the reference's **twelve** model-facing declarations | **only `ask_question`** |

Two facts fall out of that, and both matter:

1. **Three tools need the user, and the clone now wires all three.**
   `ask_question` asks; `ask_permission` and `ask_custom_permission` ask
   for a verdict on one action.
2. **Only `ask_question` is model-reachable**, and that matches the
   reference: the twelve declarations the model receives carry
   `ask_question` and neither of the two permission tools. The other two
   exist in the advertised surface — the list the headless client is told
   about — so they are reachable, but not by the model. (The reference's
   own model said the same thing when it was asked: *"there's no
   `ask_permission` tool in my current toolset"* — accurate, not a
   mis-wiring.)

The names that look like neighbours are not of this class:
`browser_input` types into a *page*, `send_command_input` writes to a
*background command's* stdin, `list_permissions` lists, `manage_task`
manages background tasks.

---

## 2. The gap, exactly

Before this cycle the three tools answered for themselves
(`internal/tools/tools_interaction.go`, at `93006e15^`):

```go
func (a *AskQuestion) Run(ctx context.Context, args json.RawMessage) (any, error) {
	…
	if a.SubAgentQuestions == nil {
		return noUserText, nil          // "The user cannot be asked in this mode…"
	}
	return a.askSubagentQuestions(p.Questions), nil
}
```

and the two permission tools returned `permissions.DeniedToolResult(...)`
whatever the user would have said.

The default catalogue registers `&AskQuestion{}` with **no**
`SubAgentQuestions` manager (`internal/tools/catalog.go:110`), so the Web
UI's turn — which builds the default catalogue — always took the
`noUserText` branch. **The Web UI never rendered a card, and the model
never got to ask.**

> The commit message of `93006e15` says the turn *"blocked on a tool that
> never returned"*. That is not what the pre-change code did: the tools
> returned immediately with a refusal. The turn did not block; it simply
> never asked. The cycle report states the accurate behaviour.

---

## 3. The seam — `internal/interaction`

The two packages that have to meet here cannot meet directly:

```
internal/remotecontrol → internal/piagentbridge → internal/tools
```

so `internal/tools` cannot import `internal/remotecontrol`, and
`internal/remotecontrol` *can* import `internal/tools` — one direction
only. The seam had to be a third package, and it had to be neutral:

| | |
|---|---|
| **`internal/interaction`** | `Kind`, `Question`, `Request`, `Answer`, `Host`, `WithHost`/`HostFrom` — 116 lines, `context` and nothing else |
| **`Host`** | `Ask(ctx, Request) (Answer, error)` — blocks until the user answered, the interaction expired, or the context is done |
| **`Request`** | the kind, the tool's name, the tool's raw arguments, the questions, the action/target/reason of a permission |
| **`Answer`** | the text, the permission verdict, the cancelled flag |

The host travels on the **turn's context**: `(*CascadeRegistry).SendStream`
(`internal/remotecontrol/cascade_io.go`) installs it with
`lsInstallInteractionHost(turnCtx, c, sink)`. The host needs the
trajectory, and the trajectory comes from the sink through the
`lsTrajectoryCarrier` capability (`Trajectory()`), so a turn whose sink
does not carry one — the legacy JSON-RPC surface — gets **no** host and
the tools keep their headless behaviour.

The tools then ask in this order (`internal/tools/tools_interaction.go`):

1. **the host**, if the context carries one;
2. **the subagent-question store** (the TUI's picker, the print runtime's
   store) — unchanged;
3. **nobody** — the headless text, unchanged.

A refused permission returns the reference's own denial text
(`permissions.DeniedToolResult`), so the model's next turn is the same
either way. A granted permission names the action
(`permissionGrantedText`) — 【推测】 the reference's own grant text is not
in the recovered evidence. A dismissed question returns
`questionCancelledText` — 【推测】 for the same reason.

---

## 4. The waiting step

### 4.1 The reference's own bytes

【一手】 captured from the real `language_server`'s state stream, with the
question *"Would you like tea or coffee?"* (full file:
`out/web-ui-interaction/reference-waiting-step.json`):

```json
{
  "type": "CORTEX_STEP_TYPE_GENERIC",
  "status": "CORTEX_STEP_STATUS_WAITING",
  "metadata": {
    "toolCall": { "id": "call_1759214", "name": "ask_question", "thinkingSignature": "…" },
    "sourceTrajectoryStepInfo": { "trajectoryId": "4ff8c9cf-…", "stepIndex": 2, "cascadeId": "f4026a1a-…" },
    "preToolHookResults": [{ "decision": "allow" }],
    "toolSource": "CORTEX_COMPONENT_SOURCE_BUILTIN"
  },
  "requestedInteraction": {
    "askQuestion": { "questions": [ { "question": "Would you like tea or coffee?",
                                      "options": [ { "id": "1", "text": "Tea" },
                                                   { "id": "2", "text": "Coffee" } ] } ] }
  },
  "generic": {
    "args": { "questions": "[{\"is_multi_select\":false,…}]" },
    "result": {
      "payload": {
        "@type": "type.googleapis.com/gemini_coder.Step",
        "type": "CORTEX_STEP_TYPE_ASK_QUESTION",
        "status": "CORTEX_STEP_STATUS_WAITING",
        "askQuestion": { "questions": [ … ] }
      }
    }
  }
}
```

Three things to read out of it:

1. **The question appears twice** — at the top level in
   `requestedInteraction.askQuestion.questions`, and inside the payload
   step's own `askQuestion.questions`.
2. **The option ids are numbered from "1"** — `"1": "Tea"`, `"2": "Coffee"`.
3. **The tool's raw arguments are a JSON *string*** —
   `generic.args.questions` is text, not an array.

### 4.2 The clone's shape

The clone publishes **the payload step itself** as the trajectory step —
type 138, status 9 — with the questions in both places
(`internal/remotecontrol/ls_interaction.go`,
`lsAskQuestionStep`):

```json
{
  "type": 138,
  "status": 9,
  "askQuestion":            { "questions": [ … ] },
  "requestedInteraction":   { "askQuestion": { "questions": [ … ] } },
  "metadata": { "toolCall": { "id": "ask_question", "name": "ask_question", "argumentsJson": "…" },
                "sourceTrajectoryStepInfo": { "trajectoryId": "…", "stepIndex": 3, "cascadeId": "…" } }
}
```

The numbers are not guesses — they are the descriptor's own
(`internal/lsproto/descriptor/language_server.protoset`, 【一手】):
`CORTEX_STEP_TYPE_ASK_QUESTION = 138`, `CORTEX_STEP_STATUS_WAITING = 9`,
`CORTEX_STEP_STATUS_DONE = 3`, and the step message `gemini_coder.Step`
carries `requested_interaction` (56), `completed_interactions` (147) and
a `step` oneof whose `ask_question` member is `CortexStepAskQuestion`
(154).

### 4.3 The one difference, and why it does not matter

The reference publishes **one** trajectory step (generic, 132) whose
`generic.result.payload` nests the ask-question step; the clone publishes
the payload step **directly**, so its trajectory carries one extra step
(the tool call) before it. The rendered card is identical, because the
bundle finds the question on **three** paths
(`internal/rcfrontend/assets/main.js`, `vca()`):

| path | where it looks | who uses it |
|---|---|---|
| 1 | `generic.result.payload`, decoded as a `gemini_coder.Step`, then its oneof case `askQuestion` | the **reference** |
| 2 | the step's own oneof case `askQuestion` | the **clone** |
| 3 | `generic.args.questions`, parsed as JSON (option ids assigned `(h+1).toString()`) | the fallback |

The clone's own copy of the bundle is byte-identical to the one the
language server serves, so the clone's card is the *same card*.

---

## 5. The answer

### 5.1 The reference's own bytes

【一手】 the reference's frontend, answering its own language server
(`out/web-ui-interaction/reference-answer-payload.json`):

```json
{
  "cascadeId": "bc0dff73-0720-4b0e-9fe3-fefe765b8f83",
  "interaction": {
    "trajectoryId": "3632ec77-3273-43bc-822a-77cce6124518",
    "stepIndex": 2,
    "askQuestion": {
      "responses": [
        { "question": "Would you like tea or coffee?",
          "options": [ { "id": "1", "text": "Tea" }, { "id": "2", "text": "Coffee" } ],
          "selectedOptionIds": ["1"] }
      ]
    }
  }
}
```

- the **oneof member is at the top level** (`interaction.askQuestion`) —
  that is how proto-JSON spells it;
- the step is named by **the trajectory id and the step index** — the
  same two numbers the waiting step's own
  `metadata.sourceTrajectoryStepInfo` carries;
- **the default fields are absent** — no `skipped`, no
  `writeInResponse`, no `cancelled`.

### 5.2 The clone's answer path

| step | where |
|---|---|
| the request arrives | `Dispatcher.lsHandleCascadeUserInteraction` (`internal/remotecontrol/ls_interaction_rpc.go`) |
| the routing | `routeLanguageServerMethods` (`dispatch_languageserver.go`) — the IDE dialect is matched **before** `canonicalMethod`, so the reference-surface alias (`reference_surface.go`) cannot turn `HandleCascadeUserInteraction` into the clone's legacy `HandleRemoteAgentCascadeUserInteraction` |
| the lookup | `Pending().GetByStep(trajectoryID, stepIndex)` (`internal/remotecontrol/pending.go`) — the pending entry carries the step index and the trajectory id it was registered with |
| the resolution | `Pending().Resolve(id, InteractionResult{Response: …})` — the parked tool's `select` wakes up |
| the decoding | `lsAnswerFromResponse` (`ls_interaction_answer.go`) — **three** shapes are accepted: the canonical proto-JSON, the same message with the oneof wrapped in `interaction` (the shape the bundle builds in memory), and a bare JSON string (the legacy dashboard's payload) |
| the answer text | the bundle's own rendering: the picked options joined with `", "`, the write-in as `"<text> (write-in)"`, `"Skipped"`, `"No answer provided"` |

### 5.3 The completed step

【一手】 the clone's own stream, after the answer
(`out/web-ui-interaction/clone-steps.json`): the waiting step becomes
`status: 3` and carries the exchange:

```json
"completedInteractions": [
  { "request":  { "askQuestion": { "questions": [ … ] } },
    "response": { "trajectoryId": "…", "stepIndex": 3,
                  "askQuestion": { "responses": [ { "question": "…", "options": [ … ],
                                                   "selectedOptionIds": ["1"] } ] } } }
]
```

The descriptor agrees: `CompletedInteraction{request: RequestedInteraction,
response: CascadeUserInteraction}` — the clone writes exactly those two
fields, and **proto-JSON leaves the default fields out**, which is what
the reference's live answer does too.

---

## 6. Side by side in the browser

Both UIs were open at once — the reference's own, served by the real
`language_server` on `127.0.0.1:8802`, and the clone's `pigasus web` on
`127.0.0.1:8901` — with the **same** IDE web bundle in both.

| | the reference (Gemini 3.8 Flash High) | the clone (MiniMax M3) |
|---|---|---|
| the model calls `ask_question` | ✓ | ✓ |
| the card appears | ✓ — *"Would you like tea or coffee?"*, **Tea**, **Coffee**, **Other (write your answer)**, **Skip**, **Submit** | ✓ — *"Which tea do you want?"*, the same card, the same rows (the model's own options) |
| the user picks **Tea** and submits | the answer leaves the browser (captured above) | ✓ |
| the card collapses | — | ✓ — *"1 question" / "Which tea do you want?" / "Tea"* |
| the turn resumes | **no** (§7) | ✓ — the model answers *"You chose Tea."* |
| console errors / warnings | none | none |

The two cards are the same card: the clone's `main.js` is byte-identical
to the one the language server serves, so the difference in the wording
above is the **model's** question, not the UI's.

Two cosmetic notes, so they are not mistaken for defects later:

- *"Waiting for user input"* is a bundle literal followed by an animated
  1–3 dot indicator; the dot count changes between frames, so the
  reference's "…" and the clone's "." are the same thing.
- The card's title is `Asking` while the step's status is WAITING and
  `Asked` afterwards; its content is `<n> question(s)`.

---

## 7. The reference's own turn does not resume

【一手】 the reference's **standalone** `language_server` does not finish
the round trip:

- the unary `HandleCascadeUserInteraction` request **never returns** —
  the frontend's call stays *"(pending)"*, and a manual replay of the
  same call from the page context times out;
- the trajectory step stays at `CORTEX_STEP_STATUS_WAITING`, and the
  *"Waiting for user input…"* row never clears;
- the server log shows **no model call after the answer** — only the
  `streamGenerateContent` that preceded it.

This is on the reference's side, not the clone's: the clone's round trip
completes, the card collapses, and the model continues. Whether the
reference's standalone server needs a second answer, a different
transport, or a warm IDE session is 【存疑】 — it was not established
here.

---

## 8. What the clone changed

| file | change |
|---|---|
| `internal/interaction/interaction.go` | **new** — the seam: `Kind`, `Question`, `Request`, `Answer`, `Host`, `WithHost`/`HostFrom` |
| `internal/tools/tools_interaction.go` | the three tools ask the host first; the headless and the subagent-question paths are untouched |
| `internal/tools/tools_interaction_test.go` | **new** — the host path for all three tools |
| `internal/remotecontrol/ls_interaction.go` | **new** — the host, the waiting step, the step completion |
| `internal/remotecontrol/ls_interaction_answer.go` | **new** — the answer's three shapes and the answer text |
| `internal/remotecontrol/ls_interaction_rpc.go` | **new** — the IDE dialect's `HandleCascadeUserInteraction` |
| `internal/remotecontrol/ls_interaction_test.go` | **new** — the round trip, the permission verdict, the unknown step, the answer text, the default omission |
| `internal/remotecontrol/pending.go` | the pending entry carries its step index and trajectory id; `GetByStep` finds it |
| `internal/remotecontrol/cascade_io.go` | `SendStream` installs the host on the turn's context |
| `internal/remotecontrol/dispatch_languageserver.go` | the IDE dialect is routed before `canonicalMethod` |
| `internal/remotecontrol/ls_trajectory.go` | `lsStepTypeAskQuestion = 138`, `lsStepStatusWaiting = 9` |
| `docs/guide/remote-control.md` | the section *"The tools that need the user"*, the *Where to look* rows, two known limits |

Two rules about the trajectory's steps are worth stating, because they
are easy to break:

- **The steps are copied, never mutated in place** (`lsCopyStep`). The
  trajectory's step maps are shared with the readers that serialize them,
  and a reader must never see a half-written step.
- **The host's `complete` writes to the step it owns** — the index it got
  from `Append`, not "the last step".

---

## 9. 硬约束 / the load-bearing rules

- **The answer's oneof member is at the top level** —
  `interaction.askQuestion`, `interaction.permission`. Not
  `interaction.interaction.askQuestion`. (The clone accepts the wrapped
  shape too, because the bundle builds it in memory, but the canonical
  proto-JSON form is the flat one.)
- **The option ids start at "1"** and the answer names the picked ones in
  `selectedOptionIds`. The request — which the host kept — is the
  authority for what an id means.
- **`stepIndex` is the trajectory step index**, not the card's ordinal.
  It is the same number the step's own
  `metadata.sourceTrajectoryStepInfo.stepIndex` carries.
- **proto-JSON leaves the default fields out.** The reference's live
  answer carries no `skipped`, no `writeInResponse`, no `cancelled`; the
  clone's completed step matches that.
- **The waiting step's status must be `CORTEX_STEP_STATUS_WAITING` (9).**
  The bundle's card filter is `status === WAITING` — a step that waits
  and says so in any other status is invisible to the card.
- **The interactive tools' fallback order is: host → subagent-question
  store → nobody.** A tool that reaches for the store before the host
  would block the TUI's own picker out.
- **The IDE dialect of `HandleCascadeUserInteraction` must be routed
  before `canonicalMethod`**, or the reference-surface alias swallows it
  and the answer never reaches the pending interaction.
- **A turn whose sink carries no trajectory gets no host.** The legacy
  JSON-RPC surface keeps its headless behaviour.

---

## 10. 存疑 / open questions

- **【存疑】 Why the reference's standalone server does not resume the
  turn** (§7). The answer arrives, the request hangs, no model call
  follows. Not established whether that is a standalone-mode limit, a
  missing second round trip, or something about the session.
- **【存疑】 The reference's own text for a granted permission and for a
  dismissed question.** The clone's `permissionGrantedText` and
  `questionCancelledText` are its own prose; the reference's are not in
  the recovered evidence.
- **【存疑】 The reference's permission card** is raised by the agent's
  permission subsystem on a *generic* step, not by a model-called
  `ask_permission` — the two are different events. The clone wires the
  card for a tool that asks; whether the clone's own permission store
  should raise one of its own is not decided here.
- **【存疑】 `CascadeUserInteraction.step_index` is `uint32`** in the
  descriptor, while the clone decodes it as a JSON `int`. Harmless for a
  well-formed answer (a negative index simply matches nothing), but the
  clone's decoder is looser than the reference's.

---

## 11. Limitations

- **The reference's side was observed, not fixed.** Its standalone
  `language_server` does not resume the turn (§7); the comparison is
  therefore "the reference's card and bytes" vs "the clone's complete
  round trip".
- **The comparison used different models** — Gemini 3.8 Flash High for
  the reference, MiniMax M3 for the clone. The wording of the question
  differs because of that, not because of the UI.
- **The reference's `thinkingSignature`, `preToolHookResults`,
  `generatorModel`, `executionId` and `modelApiContentId`** are carried
  by the reference's step and by none of the clone's; they belong to the
  agent's own bookkeeping, not to the interaction.
- **The clone's permission card was not exercised against the
  reference's own**, for the reason in §10 — the reference's permission
  card is not raised by a tool call.
- **The reference's step count differs from the clone's** (§4.3): one
  generic step with a nested payload, against two steps. The rendered
  card is the same; the trajectory is not.
- **The bundle was read as the contract.** The card's three lookup paths
  and the answer renderer were read out of the minified bundle; the
  reference's own TypeScript source was not available.

---

## 12. The files

### The evidence

| file | what |
|---|---|
| `out/web-ui-interaction/README.md` | the zh-CN walk-through of the three captures |
| `out/web-ui-interaction/reference-waiting-step.json` | 【一手】 the reference's WAITING step, as its own stream published it |
| `out/web-ui-interaction/reference-answer-payload.json` | 【一手】 the reference's answer, as its own frontend sent it |
| `out/web-ui-interaction/clone-steps.json` | 【一手】 the clone's waiting step and completed step |

### The code

| file | what |
|---|---|
| `internal/interaction/interaction.go` | the seam |
| `internal/tools/tools_interaction.go` | the three tools |
| `internal/remotecontrol/ls_interaction.go` | the host, the waiting step, the completion |
| `internal/remotecontrol/ls_interaction_answer.go` | the answer's three shapes |
| `internal/remotecontrol/ls_interaction_rpc.go` | the answer RPC |
| `internal/remotecontrol/pending.go` | the pending entry, `GetByStep` |
| `internal/remotecontrol/cascade_io.go` | `SendStream` installs the host |
| `internal/remotecontrol/dispatch_languageserver.go` | the IDE dialect's routing |
| `internal/remotecontrol/ls_trajectory.go` | the step type and status constants |
| `internal/rcfrontend/assets/main.js` | the IDE web bundle — the card's contract |
| `internal/lsproto/descriptor/language_server.protoset` | the authoritative proto shapes |

### The reference

- `language_server --standalone --override_ide_name antigravity
  --subclient_type hub --override_ide_version 2.26.0
  --override_user_agent_name antigravity --http_server_port=8802
  --csrf_token=… --app_data_dir antigravity --api_server_url
  https://generativelanguage.googleapis.com --cloud_code_endpoint
  https://cloudcode-pa.googleapis.com --gemini_dir=…`
- its own web UI at `http://127.0.0.1:8802/`
- the clone's web UI at `http://127.0.0.1:8901/` (`pigasus web`)

# The remote-control delivery strategy — deliverable

**Cycle:** `cycle-322-remote-control-delivery-strategy`
**Subject:** what the Web UI's message request does when a turn is
already running — the `delivery_strategy` field of
`exa.language_server_pb.SendUserCascadeMessageRequest` and the queue
behind it.
**Evidence:** 【一手】 the reference's own Web UI bundle
(`internal/rcfrontend/assets/main.js`) — the composer's request, the
setting that picks the strategy, the pending predicate, the queued
card and the two queue RPCs; 【反编译】 the FileDescriptorProto set
embedded in the same bundle (the field numbers and the enum);
【strings】 the reference binary's own literals, with the virtual
addresses this pass recovered for them (§2.5) and the reference's own
changelog.

> **Follow-up (2026-10-26).** The first version of this report had two
> things wrong: five literal addresses that were *file* offsets (the
> `strings-addrs.tsv` column is not a virtual address), and a reading
> of `NEXT_INVOCATION` that came from the setting's own description
> instead of from the implementation. **The reference does not
> interrupt a running turn.** §2.7 has the rule, read out of the
> instruction stream, plus the live experiment that confirms it; §9's
> open questions are answered; §12 lists what changed in the clone.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the strategy in the request** | ignored — `deliveryStrategy` was never read | read, and applied the reference's way: a message that arrives during a turn waits for it — whatever the strategy |
| **a message during a turn** | started a **second concurrent turn** | waits in the conversation's queue |
| **the first turn** | killed by the second turn's `StartCancel` → `context canceled` → RPC `-32603` → the page toasted **"Failed to send"** | ends normally; an interrupted turn is *interrupted*, not failed |
| **the queued message** | never existed | the server holds it and publishes it as `pendingAgentMessages` |
| **the Web UI's queued card** | never rendered | the reference's own card — "Queued Messages 1 · Sends after agent finishes working" |
| **`SendAllQueuedMessages`** | "Method not found" | implemented |
| **`DeleteQueuedUserInputStep`** | "Method not found" | implemented |

Two facts about the *reference* do not fit in the table, because they
are about the reference and not about the clone:

* the reference's language server **stubs** `SendAllQueuedMessages`
  and `DeleteQueuedUserInputStep` — both answer HTTP 500
  `"deprecated"` (§2.7). The Web UI's "Send all (N)" button therefore
  cannot work against the reference at all. The clone keeps its own
  working implementations, because the clone's own Web UI does use
  them.
* the reference's `NEXT_INVOCATION` **does not interrupt** a running
  turn: the executor queues *every* message while its loop is running
  (§2.7). The clone used to interrupt; it does not any more (§12).

The two symptoms the user reported had two causes, and they are two
different cycles:

* **the turn ended with only a "Thought for Ns" panel** — the model
  had no tools to call (the previous cycle, commit `710cdcb3`: "fix(rc):
  the remote-control turn offers the model its tools");
* **"Failed to send"** — this cycle: the second message killed the
  first turn.

---

## 1. The defect

A message that arrived while a turn was in flight went straight into
`runLSCascadeTurn`, so the conversation ran **two turns at once** on
one cascade.

That is fatal, because the cascade holds **one** turn's cancel func:

```go
// internal/remotecontrol/cascade_lifecycle.go
func (c *Cascade) StartCancel(parent context.Context) context.Context {
	…
	if c.cancel != nil {
		// Defensive cleanup of a leaked prior cancel
		// func — fire it so the cancel call doesn't get
		// lost.
		c.cancel()
		c.cancel = nil
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	return ctx
}
```

【反编译】 the comment is the clone's own, but the behaviour is the
reference's: the cascade is *at most one active cancel*. So the
second turn's `SendStream` → `StartCancel` fired the **first** turn's
cancel func, and the first turn died with `context canceled`.

The first turn's error was then reported to the Web UI as an RPC
failure (`-32603`, "Internal error"), which is what the page reports
as **"Failed to send"** — a toast about the *first* message, over a
message the user had just sent successfully.

The request's own strategy was never consulted: `deliveryStrategy` was
not a field of the clone's request struct at all.

---

## 2. The reference's contract

### 2.1 The enum and the field

【反编译】 from the FileDescriptorProto set the reference's own bundle
embeds (recovered in `internal/lsproto/descriptor/language_server.protoset`):

```
exa.language_server_pb.SendUserCascadeMessageRequest
	cascade_id        = 1
	items             = 2
	cascade_config    = 5
	blocking          = 8
	delivery_strategy = 22      ← exa.cortex_pb.MessageDeliveryStrategy

exa.cortex_pb.MessageDeliveryStrategy
	MESSAGE_DELIVERY_STRATEGY_UNSPECIFIED     = 0
	MESSAGE_DELIVERY_STRATEGY_NEXT_INVOCATION = 1
	MESSAGE_DELIVERY_STRATEGY_WHEN_IDLE       = 2
```

### 2.2 The two settings, in the reference's own words

【一手】 the Web UI's own settings table (`main.js`, 3616824):

```js
[ii.QUEUED_MESSAGE_DELIVERY_STRATEGY, {
  isProviderSetting: true, screen: "General",
  label: "Queued Messages",
  description: "Configure when follow-up messages are sent.",
  options: [V.MessageDeliveryStrategy.WHEN_IDLE, V.MessageDeliveryStrategy.NEXT_INVOCATION],
  resolveOptionToString: b => b === NEXT_INVOCATION ? "Send Immediately" : "Queue",
  resolveOptionToDescription: b => b === NEXT_INVOCATION
      ? "Interrupt the agent and send immediately."
      : "Queue until after the current turn.",
  settingType: "toggle-group"}]
```

So the reference's own vocabulary is:

| strategy | the setting says | the reference does | the clone does |
|---|---|---|---|
| `WHEN_IDLE` (2) | "Queue until after the current turn." | the message waits for the turn in flight and runs after it | the same |
| `NEXT_INVOCATION` (1) | "Interrupt the agent and send immediately." | **the same** — the message waits for the turn in flight (§2.7) | the same, since §12 |

`UNSPECIFIED` is treated like `WHEN_IDLE`: a message that does not say
what it wants waits.

The second column is the *setting's copy*, and the copy overstates the
implementation: "Interrupt the agent and send immediately." is what the
Web UI's settings table promises, and the reference's executor does
not deliver it (§2.7). The reference's own frontend sides with the
executor and not with the copy: `RSa` (§2.4) predicts that a
`NEXT_INVOCATION` message is **pending** exactly while the executor
loop is `RUNNING` — it waits for the turn in flight, like `WHEN_IDLE`.

### 2.3 The composer sends it

【一手】 `main.js` (5719743):

```js
await c(vZ({cascadeId: h, items: a.items, media: a.media,
             ...(a.deliveryStrategy !== void 0 ? {deliveryStrategy: a.deliveryStrategy} : {}),
             ...g}, b));
```

### 2.4 The frontend reads the server's pending messages back

【一手】 `main.js` (3159758) — the predicate that decides whether the
message the user just sent is still on its way:

```js
function RSa(a, b) {
  return a === void 0 ? false
    : b === V.MessageDeliveryStrategy.NEXT_INVOCATION
      ? a.executorLoopStatus === V.CascadeRunStatus.RUNNING
      : a.status !== V.CascadeRunStatus.IDLE || !a.fullyIdle;
}
```

【一手】 `main.js` (6727118) — the pending list the queued card is
built from:

```js
function u6b(a, b) {
  var c = [];
  for (let e of a)
    if ((e.deliveryStrategy === b || e.deliveryStrategy === UNSPECIFIED || e.deliveryStrategy === void 0)
        && e.payload.case === "stepPayload")
      try {
        let f = Kc(ke.StepSchema, e.payload.value);
        if (f.step.case === "userInput")
          c.push({msgId: e.id, step: f.step.value, metadata: f.metadata, deliveryStrategy: e.deliveryStrategy});
      } catch {}
  return c;
}
```

Three things follow from `u6b`, and all three are the wire contract:

1. a pending message carries its **step as bytes** — the frontend
   decodes it with its own `Step` schema (`Kc(ke.StepSchema, …)`);
2. the payload is a **user-input** step (`f.step.case === "userInput"`);
3. the message carries the **strategy** it was sent with, and the
   frontend matches on it (an `UNSPECIFIED` message matches any
   filter).

### 2.5 The reference's own words about the queue

【strings】 in the reference binary. The addresses below are the
literals' **virtual addresses** in the macOS `agy` binary; the first
version of this table carried *file* offsets, which are not addresses
(`out/local-macos/xref/strings-addrs.tsv` records the offset):

| address | literal |
|---|---|
| 0x103595835 | `Queue until after the current turn.` |
| 0x1035bdab8 | `Interrupt the agent and send immediately.` |
| 0x103633884 | `Send-now ignored: the active turn cannot take the %d queued input(s) now` |
| 0x103620cf2, 0x103644f7c | `early-termination condition met by a message from %s` |
| 0x103630cad | `cannot change pending queue interleaving mode while queue is not empty` |

The first two are the setting's copy — the strings §2.2 quotes.

The third is the reference's own refusal to deliver a message *now*,
and it belongs to the **CLI's own local queue**, a layer in front of
the language server: the literal is at 0x102968ca0, inside
`cli/store/store.(*Manager).userInputLoop` (0x1029659b0). It is the
TUI's "send now" — the CLI tries to hand its queued inputs to the
turn that is already running, and says why when it cannot. The clone
has no such refusal on the web path, because the web path has no
"inject into the running turn" (see §2.7).

The fourth is the **timer tool's** wording (`manage_task`), not the
delivery queue: a timer ends when a message arrives, and the tool
says so.

The fifth is the CLI's setting switch — the "interleaving mode" is
*how a new message relates to the ones already queued*, and the
reference refuses to change it while the queue is not empty. The
clone reads the strategy per message and has no such switch.

### 2.6 The two queue RPCs

【反编译】 the reference's `LanguageServerService` carries both, and
the clone's route answered them with "Method not found":

```
exa.language_server_pb.SendAllQueuedMessagesRequest
	cascade_id     = 2
	cascade_config = 3

exa.language_server_pb.DeleteQueuedUserInputStepRequest
	cascade_id = 1
	step_index = 2      (int32)
```

【一手】 the Web UI's "Send all (N)" button (`main.js`, 6728770):

```js
A.createElement(Pz, {variant: "primary", size: "sm",
                      onClick: () => { e({type: 5, cascadeId: c}); },
                      className: "mt-4"}, `Send all (${g.length})`)
```

### 2.7 What the reference actually does

【反编译】 the reference's **IDE language server** (Antigravity 2.26.0,
LS 1.11.0) — `agentexecutor.(*AgentExecutor).scheduleUserCascadeMessageRequest`
at 0x101e35620. The rule is the tail of that function; this is its
instruction sequence, read out of the binary:

```
101e3597c:  blr   x8                  ; AgentState.GetExecutorLoopStatus
101e35998:  ldrsw x6, [x5, #0x128]   ; request.DeliveryStrategy (field 22)
101e359bc:  ldrsw x10, [sp, #0x5c]   ; the loop status
101e359c0:  cmp   w10, #0x2
101e359c4:  b.ne  0x101e359d0
101e359c8:  orr   x1, xzr, #0x1       ; queue = true
101e359cc:  b     0x101e359fc
101e359d0:  cmp   w6, #0x2            ; WHEN_IDLE?
101e359d4:  b.eq  0x101e359e0
101e359d8:  mov   x1, xzr             ; queue = false
101e359dc:  b     0x101e359fc
101e359e0:  …                         ; AgentState.IsFullyIdle
101e359f0:  eor   x1, x0, #0x1        ; queue = !fullyIdle
```

Which is:

```
queue = (executorLoopStatus == RUNNING) || (deliveryStrategy == WHEN_IDLE && !fullyIdle)
```

【反编译】 the enum, from the reference's own descriptor set
(`internal/lsproto/descriptor/language_server.protoset`):
`exa.cortex_pb.CascadeRunStatus` is `UNSPECIFIED=0, IDLE=1, RUNNING=2,
CANCELING=3, BUSY=4`, and the state's `executor_loop_status` is of that
type — so the comparison above is against `RUNNING`.

Three consequences, and they are the whole difference between the
reference and the clone:

1. **while the executor loop is RUNNING, every message waits** —
   `NEXT_INVOCATION` included. The reference does not interrupt a
   running turn.
2. when the loop is not running, `WHEN_IDLE` waits iff the state is not
   fully idle, and `NEXT_INVOCATION` / `UNSPECIFIED` never wait: they
   start an invocation.
3. the strategy is read from the **request** (field 22, at struct offset
   0x128), not from the user's setting, so a message's own strategy
   decides its own fate.

【一手】 the reference's own frontend agrees — `RSa` (§2.4) predicts a
`NEXT_INVOCATION` message is *pending* exactly while
`executorLoopStatus === RUNNING`.

【一手】 **the live experiment**, against the reference's own language
server (a real instance, `-http_server_port=8791 -standalone=true
-use_mocked_data=true -persistent_mode=true`), on 2026-10-26:

* a message sent **during** a running turn is accepted (HTTP 200) and
  **queued**; when the turn ends the reference starts a **new turn**
  for it on its own — the state stream carries `turnsUpdate
  {index: 1, startStepIndex: 2}`;
* `NEXT_INVOCATION` (1) and `WHEN_IDLE` (2) behaved **identically**:
  no interrupt in either case, the running turn streamed to `DONE` and
  the queued message ran afterwards;
* `SendAllQueuedMessages` → HTTP 500
  `{"code":"unknown","message":"deprecated (error ID: …)"}`;
* `DeleteQueuedUserInputStep` → the same "deprecated";
* `DeleteAgentMessage` → HTTP 500, "failed to delete agent message:
  message has already been delivered and cannot be deleted" — a
  delivered message cannot be deleted;
* the state stream published **no** `pendingAgentMessages` at all, and
  `RequestAgentStatePageUpdate` answered `{}`.

【反编译】 the two "deprecated" answers are stubs in the reference's
language server: `SendAllQueuedMessages` (0x1025cdfb0) and
`DeleteQueuedUserInputStep` (0x1025ce000) both return
`errutil.NewWithDepth(1, &UNK_102d67e8b, 10)` — the string is ten
characters long: `deprecated`. `RecordUserStepSnapshot` (0x1025ce050)
is stubbed the same way.

So the reference's *server* keeps no list of pending user messages on
this path: the executor's own queue holds them, and the Web UI's
"Queued Messages" card is fed by the **client's** optimistic mirror
(§2.4's `SSa`), which the client disperses when the server's list
grows or the message reaches the trajectory. The bundle defines a
`QueuedStepsUpdateSchema` but never reads `queuedSteps`.

---

## 3. The queue

【一手】 the reference's own description of the pending card, in the
bundle's live-region text (`main.js`, 6967480):

> "Message queued." / "Message queued. N messages in queue. Sends
> after agent finishes working. Actions available: Send Now, Edit,
> Delete."

The clone implements that shape exactly: the **server** holds the
message, and the Web UI only mirrors it.

### 3.1 One turn per conversation

The queue lives on the conversation's trajectory
(`internal/remotecontrol/ls_queue.go`), which owns `running`,
`queue`, `turnCancel`, `interrupted` and `pendingSeq`.

| situation | what happens |
|---|---|
| the conversation is idle | the message runs right away, and the request waits for the turn |
| a turn is in flight, WHEN_IDLE | the message waits at the **back** of the queue |
| a turn is in flight, NEXT_INVOCATION | the same: the message waits at the **back** of the queue |

The third row is the correction this follow-up makes: the reference
never interrupts a running turn (§2.7), so the two strategies differ
only when the conversation is idle and not fully idle — a state the
clone does not have (its `fullyIdle` is "no turn and nothing queued").

`Admit` returns `true` for the message that owns the turn; the caller
then runs it and everything the queue hands it afterwards.

### 3.2 The drain

`runLSTurnQueue` runs the admitted message and then every message the
queue hands it:

```go
for {
	steps, err := d.runLSTurn(turnCtx, c, msg)
	interrupted, next, hasNext := traj.FinishTurn()
	…
	if !hasNext { break }
	msg = next
}
```

`FinishTurn` hands the next queued message over and only marks the
conversation idle when the queue is **empty** — a conversation with a
message waiting is not idle.

### 3.3 An interrupted turn is not a failure

The cancellation an interrupt causes is swallowed: the error is not
reported to the Web UI (that was the "Failed to send" toast), and the
stored conversation gets **no error step** — the reference marks such a
turn interrupted, not failed.

Since §12, the only interrupt left is the "Send all (N)" button: the
delivery strategy does not interrupt anything any more (§2.7).

### 3.4 The turn outlives the request that queued it

The turn's context is derived with `context.WithoutCancel(ctx)`, so a
queued message survives the request that queued it, and the request's
values — the websocket connection the notifications go to — are kept.

### 3.5 The state frame publishes the queue

`lsAgentStateFrame` carries the pending messages the way the reference
publishes them:

```
exa.jetski_cortex_pb.AgentStateUpdate
	queued_steps_update          = 9
	fully_idle                   = 18
	pending_agent_messages_update = 20

exa.jetski_cortex_pb.PendingAgentMessagesUpdate
	indices               = 1
	pending_agent_messages = 2
	total_length          = 3

exa.cortex_pb.AgentMessage
	id                = 1
	content           = 4
	step_payload      = 9
	delivery_strategy = 13
	(oneof payload)
```

The clone sends:

* `pendingAgentMessagesUpdate` — the whole list every time, with the
  indices as the list's own positions (the frontend applies it as a
  slice update, like the trajectory's steps update);
* `fullyIdle` — false while a message waits;
* `executorLoopStatus` — `RUNNING` while a turn is in flight **or**
  while the queue is not empty (the frontend's `RSa` reads exactly
  this field for the NEXT_INVOCATION case).

---

## 4. The wire shape of a pending message

The payload is **not** the step's proto-JSON form — it is the step's
**bytes**, and proto-JSON spells a bytes field as base64.

The oneof is spelled the way proto-JSON spells a oneof: the member's
own name at the top level.

```json
{"id": "…-q1", "stepPayload": "<base64 of gemini_coder.Step>", "deliveryStrategy": 2}
```

The frontend reads it through its runtime's *decoded* shape
(`e.payload.case === "stepPayload"`), which is what protobuf-es's
`fromBinary` produces — the wire carries the member name.

This is not a detail: the Connect bridge re-encodes the state frame
with protojson before it reaches the browser
(`internal/lsproto/convert.go` → `jsonToMessage` → `proto.Marshal`),
and **a field the schema does not know is dropped**. A payload spelled
`{"payload": {"case": "stepPayload", …}}` would silently vanish, and
the Web UI would render no queued card at all.

The step itself is hand-encoded (`lsEncodeUserInputStep`), because the
schema is closed-source and the clone has no generated Go types for
`gemini_coder.Step`:

| field | number | value |
|---|---|---|
| `gemini_coder.Step.type` | 1 | `CortexStepType.USER_INPUT` = 14 |
| `gemini_coder.Step.status` | 4 | `CortexStepStatus.QUEUED` = 11 |
| `gemini_coder.Step.metadata` | 5 | `created_at` (1), `source` (3) = `USER_EXPLICIT` (4) |
| `gemini_coder.Step.user_input` | 19 | `items` (3), `is_queued_message` (6) |

`is_queued_message` is the reference's own flag for exactly this case.

The test `TestThePendingMessagesSurviveTheProtobufBridge` encodes the
frame with the reference's **own** descriptor set and reads the
messages back — so the shape is pinned against the schema, not against
the clone's belief about it.

---

## 5. The two queue RPCs

【反编译】 2026-10-26: in the reference's language server **both** are
stubs. `SendAllQueuedMessages` (0x1025cdfb0) and
`DeleteQueuedUserInputStep` (0x1025ce000) return
`errutil.NewWithDepth(1, &UNK_102d67e8b, 10)` — the string is ten
characters long: `deprecated`. `RecordUserStepSnapshot` (0x1025ce050)
is stubbed the same way. 【一手】 the live answer is HTTP 500
`{"code":"unknown","message":"deprecated (error ID: …)"}`.

So the Web UI's "Send all (N)" button cannot work against the
reference at all: the reference's own frontend calls a method the
server answers with "deprecated". The clone keeps its own working
implementations, because the clone's own Web UI uses them.

### `SendAllQueuedMessages`

The clone's queue already drains itself when the turn in flight ends —
that is the reference's own setting, "Queue until after the current
turn." — so the button's job here is to take the turn away from the
work in flight: the queued messages run next.

The reference says something similar in its own words when it *cannot*
honour a send-now request ("Send-now ignored: the active turn cannot
take the %d queued input(s) now") — but that refusal belongs to the
CLI's own local queue (§2.5), not to the language server, and the
language server does not have this RPC at all.

### `DeleteQueuedUserInputStep`

The step index is the position in the queue — the same position the
`pendingAgentMessages` update's `indices` carry — so the card the Web
UI renders and the message the user deleted are the same one.

The reference's own delete path is a different RPC:
`DeleteAgentMessage`, which refuses a message that has already been
delivered ("message has already been delivered and cannot be
deleted") 【一手】.

---

## 6. What the clone changed

| file | what |
|---|---|
| `internal/remotecontrol/ls_delivery.go` | the strategy enum, and the JSON forms (number, numeric string, proto-JSON name) |
| `internal/remotecontrol/ls_queue.go` | the queue: `Admit` / `Interrupt` / `BeginTurn` / `FinishTurn` / `Pending` / `DropPending`, and the hand-rolled `gemini_coder.Step` encoder |
| `internal/remotecontrol/ls_queue_rpc.go` | the two reference RPCs |
| `internal/remotecontrol/ls_trajectory.go` | the queue's fields on the trajectory, `lsSnapshot.Pending`, the state frame's `pendingAgentMessagesUpdate` and `fullyIdle` |
| `internal/remotecontrol/dispatch_ls.go` | `deliveryStrategy` in the request, `admitLSCascadeMessage`, `runLSTurnQueue`, `runLSTurn`, the route table |
| `internal/remotecontrol/ls_queue_test.go` | the tests below |

The old `runLSCascadeTurn` and `SetRunning` are gone: the turn is
owned by the queue, not by the request.

---

## 7. The live verification

### 7.1 The clone's rig

Against an isolated rig (`pigasus web` on 127.0.0.1:8899 with an
isolated `HOME`, and a mock OpenAI-compatible model on 127.0.0.1:8900
that sleeps 8 s when the message says "slow"), on 2026-10-26, in the
real browser:

**WHEN_IDLE (Enter).** The first message ("first slow message") took
the turn. A second message sent two seconds later ("second message
while busy") produced the reference's own card:

> **Queued Messages  1** · Sends after agent finishes working
> `second message while busy`   [→] [✎] [🗑]

— with the message's text, which the browser could only render by
decoding the clone's hand-encoded step bytes. When the first turn
ended, the queued message ran: the model's second request carried the
history (`user: first slow message`, `assistant: …`) plus the queued
message, so the conversation continued rather than restarted.

**NEXT_INVOCATION (Alt+Enter).** The third message ("third slow
message") took the turn; a fourth ("interrupt me now", Alt+Enter)
cancelled it. The mock's log shows the third turn's request was
abandoned and the fourth ran with the history replayed — including the
third message as a user turn with no answer, which is what an
interrupted turn looks like. No error toast, and the browser console
carried no error (only the app's own info lines).

> **Superseded.** That interrupt was the clone's own reading of the
> setting's copy. §2.7 shows the reference does not do it, and §12
> removed it: a `NEXT_INVOCATION` message now waits for the turn in
> flight, like a `WHEN_IDLE` one.

### 7.2 The reference's own language server

【一手】 2026-10-26, against a real instance of the reference's IDE
language server (`language_server -http_server_port=8791
-standalone=true -use_mocked_data=true -persistent_mode=true`), driven
over the reference's own Connect surface:

* a message sent **during** a running turn was accepted (HTTP 200) and
  **queued**; when the turn ended, the reference started a **new turn**
  for it on its own — the state stream carried `turnsUpdate
  {index: 1, startStepIndex: 2}`;
* `NEXT_INVOCATION` (1) and `WHEN_IDLE` (2) behaved **identically** —
  no interrupt in either case;
* `SendAllQueuedMessages` and `DeleteQueuedUserInputStep` answered
  HTTP 500 `"deprecated"`; `DeleteAgentMessage` answered HTTP 500
  "message has already been delivered and cannot be deleted";
* the state stream published **no** `pendingAgentMessages`, and
  `RequestAgentStatePageUpdate` answered `{}`.

The reference's mock streams the planner's answer character by
character, so a turn lasts ~2.6 s — a wide enough window to land a
second message inside a running turn.

---

## 8. 硬约束 / the load-bearing rules

1. **One turn per conversation.** The cascade holds one cancel func;
   a second turn would kill the first. The queue is what makes the
   second message legal.
2. **The strategy is the contract, and the reference's contract is the
   executor's.** While the executor loop is RUNNING every message
   waits — `WHEN_IDLE`, `NEXT_INVOCATION` and `UNSPECIFIED` alike.
   Never read the strategy as "always start a turn", and never read
   `NEXT_INVOCATION` as "interrupt": the reference's setting *copy*
   says that, its implementation does not (§2.7).
3. **An interrupted turn is not a failure.** Its cancellation is
   swallowed and it stores no error step. (Only the "Send all (N)"
   button interrupts anything now.)
4. **A pending message is published as bytes.** `stepPayload` is the
   step's *bytes*, base64-encoded, under the oneof member's own name.
   The Connect bridge's protojson drops a field the schema does not
   know, so a payload spelled any other way disappears silently.
5. **The queue is the server's.** The Web UI mirrors it
   optimistically; the server's `pendingAgentMessages` is the truth.
6. **The turn outlives the request.** The turn's context comes from
   `context.WithoutCancel(ctx)`, or a queued message dies with the
   request that queued it.

---

## 9. 存疑 → the answers

The five open questions of the first version, answered.

1. **Does the reference auto-drain the WHEN_IDLE queue?** **Yes.**
   【一手】 live, against the reference's own language server: the
   message sent during the running turn was queued, and when the turn
   ended the reference started a **new turn** for it by itself — the
   state stream carried `turnsUpdate {index: 1, startStepIndex: 2}`.
   The "Send all (N)" button is the *manual* path, and the reference's
   server answers that RPC with "deprecated" (§5) — so the auto-drain
   is the only path that works against the reference. The clone
   auto-drains **and** implements the manual RPC.
2. **What is the reference's "pending agent messages" list, exactly?**
   【反编译】 it is fed by the executor's **message-hub watcher**:
   `startPermanentMessageWatcher` → `AgentState.SyncPendingMessages` →
   `PendingAgentMessagesUpdate{indices, pending_agent_messages,
   total_length}` — the messages the *hub* is holding, each with the
   strategy it was sent with. 【一手】 the runnable (mocked) instance
   never publishes it, and `RequestAgentStatePageUpdate` answers `{}`:
   there, the Web UI's card is fed by the **client's** optimistic
   mirror, which it disperses when the server's list grows or the
   message reaches the trajectory. The clone publishes its own queue in
   that field — a superset of the reference's behaviour.
3. **The reference's refusal.** "Send-now ignored: the active turn
   cannot take the %d queued input(s) now" belongs to the **CLI's own
   local queue**, a layer in front of the language server: the literal
   is at 0x102968ca0, inside `cli/store/store.(*Manager).userInputLoop`
   (0x1029659b0) 【反编译】. It is the TUI's "send now": the CLI tries
   to hand its queued inputs to the turn that is already running, and
   says why when it cannot. The web path has no such injection — the
   language server queues and delivers later.
4. **The interleaving mode.** "cannot change pending queue interleaving
   mode while queue is not empty" is the **CLI's own setting switch**:
   the mode is *how a new message relates to the messages already
   queued*, and the reference refuses to change it while the queue is
   not empty 【strings】. The reference's changelog spells the mode's
   behaviour out: a message sent while earlier messages are still
   queued "goes to the agent together with them, **in order**" — FIFO.
   The clone's queue is FIFO and has no switch.
5. **The daemon's log destination.** **stderr.** The clone uses the
   standard `log` package and never calls `log.SetOutput`, so
   `log.Printf` goes to stderr 【一手】. The first version's "no log
   output" was an artifact of the rig, not a fact about the daemon.

Still open: the **exact condition** under which the CLI's "send now"
refuses ("the active turn cannot take the %d queued input(s) now"). The
reference's executor has an *injection* concept of its own — "no last
user step found for continuing after injection" — and the CLI asks the
turn to take the queued inputs mid-turn. That is the CLI's own layer
(§2.5), not the web path, so it does not change anything in this
cycle. 【推测】

---

## 10. Limitations

1. **The legacy JSON-RPC surface keeps its one-shot semantics.** The
   old `SendRemoteAgentCascadeMessage` (the JSON-RPC path, not the
   Connect one) still runs one message per request and does not queue.
   There is no client for it in the tree, and the reference has no
   such surface — it is the clone's own.
2. **The queue is per conversation, in memory.** A daemon restart
   loses the pending messages. The reference keeps its trajectory in a
   database (`useDbTrajectory`), so its queue survives.
3. **One strategy per message.** The reference's setting is a
   *mode*; the clone's queue takes the strategy from each message, so
   a user can mix them in one conversation. The clone does not
   implement the reference's refusal to change the mode while the
   queue is not empty.
4. **The step encoder is hand-rolled.** `gemini_coder.Step` is
   encoded field by field (§4) against the recovered field numbers.
   A field the reference sets that the clone does not know is missing
   from the pending card — the frontend only reads the step's
   `userInput` items, so this does not show.
5. **The two queue RPCs are not exercised by the reference's own UI.**
   The bundle calls `SendAllQueuedMessages`; nothing in the tree calls
   `DeleteQueuedUserInputStep`, so its index space (the queue's
   position) is the clone's own reading of the field.
6. **The runnable reference uses the mocked backend.** The live
   experiment (§7.2) ran the reference with
   `-use_mocked_data=true`: the planner's answer is canned and the
   turn lasts ~2.6 s. The rule in §2.7 was read out of the
   instruction stream, not inferred from the mock, and the mock and
   the real backend agree on the queue — but a real conversation's
   *timing* is not what was measured.
7. **The rule is the executor's, not the hub's.** The reference has two
   layers that hold a message: the executor's own queue (the rule in
   §2.7) and the cortex message hub's pending messages (§9.2). The
   clone implements the executor's. The hub's own delivery rules —
   `messages.WithDeliveryStrategy` — were not traced.

---

## 11. The files

| file | what |
|---|---|
| `internal/remotecontrol/ls_delivery.go` | the strategy enum and its JSON forms |
| `internal/remotecontrol/ls_queue.go` | the queue and the step encoder |
| `internal/remotecontrol/ls_queue_rpc.go` | `SendAllQueuedMessages`, `DeleteQueuedUserInputStep` |
| `internal/remotecontrol/ls_trajectory.go` | the trajectory's queue fields, the state frame |
| `internal/remotecontrol/dispatch_ls.go` | the request's strategy, the admission, the drain |
| `internal/remotecontrol/ls_queue_test.go` | the tests |

### The tests

| test | what it pins |
|---|---|
| `TestAMessageThatArrivesDuringATurnWaitsForIt` | the queue: the second message waits and runs after the turn |
| `TestNextInvocationWaitsForTheTurnLikeWhenIdle` | NEXT_INVOCATION: the message waits too, behind the one already queued, and no turn is cancelled |
| `TestTheStateFrameCarriesTheQueuedMessages` | the frame's `pendingAgentMessagesUpdate` |
| `TestTheDeliveryStrategyReadsEveryForm` | number, numeric string and proto-JSON name |
| `TestTheQueuedStepDecodesAsTheReferenceBuildsIt` | the hand-encoded step, read field by field against the recovered field numbers |
| `TestThePendingMessagesSurviveTheProtobufBridge` | the frame survives the Connect bridge's protojson re-encoding |
| `TestTheSendAllButtonReleasesTheQueue` | `SendAllQueuedMessages` |
| `TestTheQueuedMessageCanBeDropped` | `DeleteQueuedUserInputStep` |
| `TestTheQueuedMessageIsNotTheUsersOwnStep` | the pending step is the *queued* step, not the user's own |

---

## 12. The follow-up

What this second pass changed, and where it is pinned.

| | the first version | now |
|---|---|---|
| **the five literal addresses** | file offsets, not addresses | the literals' virtual addresses in the `agy` binary (§2.5) |
| **`NEXT_INVOCATION`** | interrupts the turn in flight | waits for it, like `WHEN_IDLE` (§2.7) — the reference's executor's rule |
| **the queue** | the interrupting message jumped the head | FIFO, like the reference's |
| **the five 存疑** | open | answered (§9); one residual question about the CLI's own "send now" |
| **`SendAllQueuedMessages` / `DeleteQueuedUserInputStep`** | the clone's own working RPCs | unchanged — the reference's stubs answer "deprecated" (§5) |
| **`DeleteAgentMessage`** | not implemented | still not implemented; the reference refuses a delivered message (§5) |

The code change is one commit — `fix(rc): a message never interrupts
the turn in flight` — and it touches three files:

| file | what |
|---|---|
| `internal/remotecontrol/dispatch_ls.go` | the admission: no interrupt, `Admit(msg)` without a "front" flag |
| `internal/remotecontrol/ls_queue.go` | `Admit` appends to the back of the queue, always |
| `internal/remotecontrol/ls_delivery.go` | the doc comment: the rule, and the copy that overstates it |

`Interrupt` stays — the "Send all (N)" button is its only caller now.


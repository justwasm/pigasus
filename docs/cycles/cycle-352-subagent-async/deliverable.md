# Cycle 352 deliverable: the asynchronous delegation — a subagent that runs in the background and reports back — and the permission gate that reaches it

**Cycle:** `cycle-352-subagent-async`
**Subject:** cycle 349 gave the subagent its authorization model and left
two gaps in its own *open questions* section: *"The reference's delegations
are asynchronous"* and *"The permission policy does not reach the
subagent's calls"*. This cycle closes both. `invoke_subagent` no longer
waits for the child — it returns the reference's launch acknowledgement and
the child reports back later, as a message to the parent — and the
conversation's one permission policy now judges the child's own tool calls.
**Method:** the reference's own captured bytes read first-hand (the 1.4.4
live capture), the decompiled and string-table literals of 1.4.4's subagent
handlers, and every change reproduced against the clone's own tool set
before it was made.

---

## 0. The short answer

| | before | now |
|---|---|---|
| `invoke_subagent` | **waits** for the child and returns its final text as the tool result | returns the reference's **launch acknowledgement** at once; the child runs in the background |
| the parent's turn | continues after the delegation | **ends** with the acknowledgement (`AgentToolResult.Terminate`) |
| the child's task | the child's first *user* message is the raw prompt | the child's first user message is the reference's `<SYSTEM_MESSAGE>` **envelope**, with `sender` = the parent's conversation id |
| the report | the tool result | a **user-role content part** carrying the same envelope, in the parent's *next* invocation |
| `send_message` | always answers *"recipient not found"* | answers `Message sent to "<id>".` when the recipient is live, and the not-found text when it is not |
| `manage_subagents` | lists nothing; kills nothing | lists the live children with the reference's own entry shape, and kills them |
| the permission policy | never reaches the child | the child's calls are judged under the conversation's one policy, under **both** of a tool's names, with the child's worktree in the sandbox |

The two callers keep their own shapes: the TUI hands the report to the
session's inbox (the notification path a background task already uses), and
`--print` runs the reference's own second invocation — the report is the
next turn's prompt.

---

## 1. The evidence

### 1.1 The launch acknowledgement 【一手 — the 1.4.4 live capture】

`docs/cycles/subagent-async-framing-capture/deliverable.md` holds the
reference's own bytes:

```
Created At: 2026-10-22T11:24:33+08:00
Completed At: 2026-10-22T11:24:33+08:00
Created the following subagent:
{
  "conversationId":  "73ab3617-5a99-4e26-b1f1-c88e712efe39",
  "logAbsoluteUri":  "file:///Users/gear/.gemini/antigravity-cli/brain/73ab3617-5a99-4e26-b1f1-c88e712efe39/.system_generated/logs/transcript.jsonl",
  "workspaceUris":  [
    "file:///private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode"
  ]
}
The subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.
```

Three details survive into the clone:

- the block is **protojson** — two spaces after the colon, a two-space
  indent, one URI per line. A plain `json.MarshalIndent` writes *one*
  space after the colon and is therefore not the same bytes;
- the acknowledgement **ends the parent's turn**. The reference has
  nothing to do until the child reports, and says so;
- the child's own id is a **version-4 UUID** (`73ab3617-5a99-4e26-b1f1-…`),
  and it is the address the parent and the child use for each other.

The clone's `logAbsoluteUri` points at the transcript the clone *does*
write (`<AgentDir>/subagents/<id>.jsonl`) rather than at the reference's
`brain/<id>/.system_generated/logs/transcript.jsonl`, which the clone never
creates. The reference's path is a file the clone does not have; pointing
at a file that does not exist would be a worse lie than pointing at the one
that does.

The clone's `workspaceUris` reports the **child's worktree**
(`<AgentDir>/worktrees/<id>`), while the capture reports the **parent's
workspace**. The capture asked for no worktree, so it cannot say whether
the reference's field means *"the directory the subagent runs in"* or
*"the workspace the subagent may touch"* — the clone reads it as the
first. The clone allocates a worktree by default (that predates this
cycle), so its acknowledgement names a directory the reference's would
not. The shape is right; the value is the clone's own. See *open
questions*.

### 1.2 The delivery 【一手 — the same capture】

The report is **not** a tool result and **not** a `functionResponse`. It
is a plain user-role content part carrying the reference's own envelope:

```
The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.

<SYSTEM_MESSAGE>
[Message] timestamp=2026-10-22T03:24:39Z sender=73ab3617-5a99-4e26-b1f1-c88e712efe39 priority=MESSAGE_PRIORITY_HIGH content=PONG
</SYSTEM_MESSAGE>
```

The reference's own converter is
`chatconverters.(*SystemMessageStringConverter).ToOutputString`
(`0x10155e320`, 【反编译】): `FormatWithXMLTag("SYSTEM_MESSAGE", content)`
prepended with a 142-byte preamble. `FormatWithXMLTag` is
`Sprintf("<%[1]s>\n%[2]s\n</%[1]s>")`.

Two details the capture pins and the clone adopts:

- the timestamp is **UTC** (`03:24:39Z`), while every other time in the
  same request is `+08:00`;
- the sender is the **child's conversation id** — the same id the
  acknowledgement reported.

The 1.4.4 decompilation splits the lead-in from the tag — the preamble is
the 142 bytes at `0x10353cdda` (the 141-byte sentence and its newline) —
which would leave no blank line between the sentence and the tag. The
1.4.7 binary carries the `\n\n` inside its own format string, and the
1.4.4 capture shows the blank line. The clone follows the captured bytes.

### 1.3 The child's side 【反编译 + strings】

`handlers.(*InvokeSubagentHandler).launchSubagent` (`0x101d2c690`) delivers
the task through the messaging system (the experiment flag
`send-subagent-initial-prompt-as-message: true`, 【strings】) and gives the
child this 224-byte system instruction 【反编译】:

```
Your task arrives as a system message from the agent that invoked you. Treat that message exactly as if the user had sent it to you directly: it is the user's complete request, and you respond to it as you would to the user.
```

The mirror image is confirmed by the capture: the child's first user
message is the envelope carrying the parent's `Prompt`, with `sender` = the
parent's conversation id 【一手】.

A subagent that ends without saying anything gets this content instead
【strings】:

```
Subagent %s went idle without reporting anything back. Check its work, or send it a message at that id asking it to report back.
```

### 1.4 The result texts 【反编译 for the pairing, strings for the text】

`tools.(*SendMessageTool).Execute` (`0x101d89520`) carries
`Message sent to %q.`. The 1.4.7 result-text capture holds the failed
delivery's own text —
`Encountered error in tool execution: recipient "abc123" not found`
【一手 — `internal/tools/testdata/result_texts_1.4.7.txt`】. It is a
*result*, not an error: the reference stamps it and returns it like any
other.

`tools.(*ManageSubagentsTool).list` (`0x101d5fff0`) carries
`You have %d active subagent(s):\n%s` and `(*ManageSubagentsTool).kill`
carries the kill sentence; the entry's four fields come from the struct
tags 【strings】: `type`, `role`, `conversationId`, `state`.

The reference's `send_message` **sets an initial result and then updates
the step render info asynchronously** — the tell that the reference's
messaging is asynchronous too, and that the clone's "always not found"
answer was never right.

---

## 2. The model

### 2.1 The registry

`internal/subagenttools/registry.go` holds the conversation's live
children. It belongs to the **conversation**, not to the turn: the parent's
tools and the children's tools share it, so a subagent is reachable by its
id from either side and a child's own message reaches the parent through
it. The reference scopes the same thing to the conversation —
`manage_subagents` *"Manage the direct subagents of this conversation"* —
so the registry holds the direct children.

Each `Child` carries its conversation id, its type, its role, its
worktree, its state and the mailbox the parent's messages arrive in. A
child **stays alive** after it reports: the parent can message it again
(*"send it a message at that id asking it to report back"*), and the
message wakes it for another turn.

`Registry.Busy()` answers *"is any child still working?"* — it is the
print path's wait condition, and it is deliberately not *"are there any
children?"*: a child that has reported stays alive but will not speak on
its own, so waiting for it would hang the run.

`Registry.SetNotify` is the delivery channel to the parent. The parent's
own transport owns it — the TUI puts the message in the session's inbox,
the print mode hands it to the next turn.

### 2.2 The launch

`internal/subagenttools/launch.go`'s `Launch` is the new entry point:

1. resolve the model, the agent directory and the system prompt;
2. mint the child's conversation id (a version-4 UUID);
3. resolve the child's working directory and its worktree
   (`resolveSubagentCWD`);
4. register the child;
5. **rebind the messaging tool** — the tool set was built before the
   launch (the grant decides it), so the child needs its own
   `send_message`, addressed to the parent;
6. start the child's goroutine and **return the acknowledgement**.

The child's context is `context.WithoutCancel(ctx)`: the parent's turn
ends the moment the acknowledgement is returned, and the child is still
running then. The registry owns the child's lifetime — a killed subagent,
or a closed conversation, ends it.

### 2.3 The child's loop

`runChild` is the child's own loop: run a turn, report what it has to say,
then wait for the parent to message it.

The report is delivered **once**: a child that already called
`send_message` itself is not reported again (*"a second delivery would say
it twice"*). A child whose turn ended without saying anything gets the
reference's idle notice instead. A child whose turn *failed* gets the
clone's own sentence — the reference's wording for a failed delegation is
not recovered (see *open questions*).

### 2.4 The envelope

The envelope is rendered by a **neutral package**,
`internal/agentmessage`. Two callers need the same bytes: `internal/agent`'s
task notification (the background-task subsystem, which had the rendering
inline) and the subagent delivery. The package holds the preamble, the
wrapper, the line format and the priorities, and `Render` composes them.

`internal/agent/task_inbox.go` now delegates to it. The bytes did not
change — the existing notification tests still pass unchanged.

---

## 3. The permission gate

The reference's model is **one policy per conversation** — the 1.4.6
changelog: *"allowing an action for the conversation, in the main agent or
in a subagent, applies across the main conversation and all of its
subagents"* 【一手】. The clone now honours that.

### 3.1 Two names for one tool

A subagent calls the **catalogue's** spelling of a tool (`write_to_file`)
while the parent calls the **declaration's** (`write_file`) — and the
user's allow and deny rules are written against the declaration. The
mapping is new: `internal/tools/declaration_names.go`.

```go
internal/tools.PermissionNames("write_to_file") // ["write_to_file", "write_file"]
```

The pairing is 【推测】: the reference's binary publishes no table, so each
pair is read off the two surfaces' own descriptions.

### 3.2 The decision

`internal/permissions.Policy.DecideNames(target, names...)` judges the
call under **every** name it was given: a deny under any of them denies,
an allow under any of them allows, and only then do the mode's rules
decide — the same order `Policy.Decide` uses for a single name.

`internal/toolrunner.Decide(policy, sandbox, call)` is that decision as a
pure function, and `PermissionRunner.Run` is now a thin wrapper around it.
The gate the callers write is therefore the *same* decision the parent's
own calls go through.

### 3.3 The worktree

A subagent that runs in its own directory must be allowed to work in it.
`sandbox.Sandbox` grew `ExtraPaths` — the fourth kind of allowed root,
next to the workspace, the artifact directory and the per-conversation
scratch directory.

### 3.4 The seam

`Deps.PermissionGate` (a single gate) became
`Deps.PermissionGateFor func(worktree string) BeforeToolCallGate` — a
**factory**, because the gate is built *for* the directory the child runs
in, and each child has its own worktree.

`internal/subagenttools` cannot import `internal/tools` (the import cycle:
`internal/tools` → `internal/browsersubagent` → `internal/subagenttools`),
so the gate adapter lives with the callers, which own the policy and the
sandbox anyway. `internal/subagenttools` keeps only the decision seam:

```go
type BeforeToolCallGate interface {
    Decide(ctx context.Context, toolName string, args json.RawMessage) GateDecision
}
```

The child's session runs the gate as a PiG `BeforeToolCall` hook
(`Deps.beforeToolCallHooks(worktree)`).

---

## 4. The two callers

### 4.1 The TUI — `internal/cliapp/tui_subagent_async.go` (new)

- **the registry** — `ensureSubagentRegistry` keeps one registry per
  conversation. When the conversation changes, the old registry is closed
  and its children are killed: they belong to the conversation that
  created them;
- **the notifier** — `notifySubagentMessage` puts the child's message into
  the session's **task inbox**, the same inbox a background task's
  completion notification uses. The turn's end starts the next turn when
  the inbox is not empty, so the report is picked up without the user
  typing anything;
- **the gate** — `subagentPermissionGate` judges the child's calls under
  the conversation's own policy and the session's own sandbox, plus the
  child's worktree.

### 4.2 The print mode — `internal/printmode/print_pig.go`

The print mode runs the reference's own shape: the acknowledgement ends
the first invocation, and the delivery starts the next one.

`executePigPrintRunWith` loops: run a turn, wait for a delivery, and if
one arrives, run the next turn with it as the prompt. The wait
(`waitForSubagentDelivery`) takes an already-arrived delivery first — the
child may have reported while the parent's turn was still running — and
then waits only while `registry.Busy()`.

The wait is **bounded** by `deliveryWaitTimeout` (10 minutes). The bound is
the clone's own addition: the reference has no headless timeout of its own
that was recovered, and a subagent that never reports must not hang a
headless run forever.

The print mode's sandbox is built with `printModeSandbox(nil, …)`: the
`--dangerously-skip-permissions` flag reaches it through
`Policy.SkipPermissions` instead of the flagset, so the print mode's own
`main_print*.go` files are not touched.

---

## 5. 硬约束 (hard constraints)

- **`internal/subagenttools` must not import `internal/tools`,
  `internal/toolrunner` or `internal/agent`.** The cycle goes through
  `internal/browsersubagent` for the catalogue, and the gate adapter
  lives with the callers. The shared envelope rendering therefore lives in
  a **neutral package**, `internal/agentmessage`.
- **The advertised catalogue stays at sixty names**, and the twelve
  model-facing declarations stay the parent's surface only (cycle 349).
  The bridge tools reach the browser agent's tool set and nowhere else.
- **The acknowledgement's bytes are protojson.** Two spaces after the
  colon. `json.MarshalIndent` is one space, and is not the reference's
  bytes.
- **A terminated tool batch is not an error.** `AgentToolResult.Terminate`
  ends the batch *and* does not raise `ErrToolResultsUnanswered` — the
  parent's turn ends with the acknowledgement, quietly.
- **The child's context outlives the parent's turn.** `WithoutCancel` is
  what makes the delegation asynchronous; the registry is what ends it.
- **The envelope is rendered in exactly one place.** `internal/agentmessage`
  is shared by the task notification and the subagent delivery; a second
  rendering would be a second set of bytes.
- **The permission policy is the conversation's one policy.** The gate is
  a *factory*, because the sandbox is built for the child's directory.

---

## 6. 存疑 (open questions)

- **The parent's model-role "Waiting for the subagent to respond…"** is
  part of the reference's second invocation (38 bytes, 【反编译】) and the
  clone does not emit it. PiG offers no way to inject a model-role text
  after a tool result, and the print mode's transcript lives inside PiG's
  session. The clone's second invocation therefore carries the delivery
  without that line.
- **The reference's wording for a failed delegation** is not recovered.
  The clone's `Subagent %s failed: %v` is its own sentence.
- **The list entry's field *order*** is the struct's own 【推测】 — Go
  marshals in declaration order, and the reference's order is not
  recovered. The clone uses `type`, `role`, `conversationId`, `state`.
- **The killed roles' separator** is the clone's own: the reference's
  `Killed roles: %s` is a single `%s`, and the join it uses is not
  recovered. The clone joins with `", "`.
- **`manage_subagents` from a subagent's own side** sees the whole
  conversation's direct children, where the reference scopes the list to
  *"the direct subagents of this conversation"*. The clone's registry is
  flat; the messaging still works in both directions (a grandchild reaches
  its parent child, and the top-level parent), but the *list* is not
  scoped per level.
- **An unnamed `TypeName`** is the clone's own leniency — the reference's
  declaration requires the field. The list entry's `type` is then empty.
- **The child's workspace.** The reference's capture shows
  `workspaceUris` naming the **parent's workspace**, in a run that asked
  for no worktree; the clone allocates a worktree by default (older
  behaviour, not this cycle's) and names that. The child's *session* runs
  in the worktree, but the child's **tools** are drawn from the parent's
  catalogue, which is built once for the parent: a relative path in a
  child's file tool still resolves against the parent's workspace. The
  clone's two halves therefore disagree about the child's directory.
  Closing it needs the catalogue rebuilt per child — and `internal/tools`
  cannot be imported from `internal/subagenttools` (the import cycle), so
  the caller would have to rebuild the whole registry, MCP and browser
  tools included.
- **`Message sent to %d subagents: %s.`** and the partial-delivery
  variants exist in the reference's binary; the clone answers only the
  single-recipient form.

---

## 7. Limitations

- **The end-to-end test uses the faux provider.** The child's turn runs
  against PiG's test provider, so the *shape* of the delegation (launch →
  acknowledgement → background turn → delivery) is pinned end to end, but
  not a real model's behaviour.
- **The child's catalogue is the parent's.** The catalogue is built once,
  for the parent, and the child's tool set is drawn from it. The child's
  *session* runs in its worktree, but a relative path in a child's file
  tool resolves against the parent's workspace (see *open questions*).
  Rebuilding the catalogue per child needs `internal/tools`, and the
  import cycle forbids it.
- **The TUI's transcript does not replay tool calls.** The reference's
  second invocation carries the acknowledgement as a `functionResponse`;
  the TUI's session holds only user and assistant text, so the second turn
  carries the delivery alone. The print mode keeps the whole transcript in
  PiG's session and does carry the acknowledgement's result.
- **The print mode's wait is bounded, the reference's is not.**
- **The child's `send_message` binds to the parent's registry.** A nested
  delegation registers its child in the same flat registry.

---

## Files

| | |
|---|---|
| `internal/agentmessage/agentmessage.go` | **new** — the shared `<SYSTEM_MESSAGE>` envelope |
| `internal/agentmessage/agentmessage_test.go` | **new** — the captured bytes |
| `internal/subagenttools/registry.go` | **new** — the live children, the mailbox, the states |
| `internal/subagenttools/ack.go` | **new** — the protojson acknowledgement, the UUID, the file URI |
| `internal/subagenttools/ack_test.go` | **new** — the captured acknowledgement |
| `internal/subagenttools/launch.go` | **new** — the background launch and the child's loop |
| `internal/subagenttools/async_test.go` | **new** — the messaging, the list, the kill, the delivery |
| `internal/subagenttools/invoke.go` | `Execute` launches and returns the acknowledgement |
| `internal/subagenttools/send.go` | the live recipient, the not-found text, the two directions |
| `internal/subagenttools/manage.go` | the list and the kill, against the live children |
| `internal/subagenttools/grant.go` | the messaging tool is bound to the registry and its owner |
| `internal/subagenttools/session.go` | the child's session runs the gate |
| `internal/subagenttools/subagenttools.go` | `Deps.Registry`, `Deps.ConversationID`, `Deps.PermissionGateFor` |
| `internal/agent/task_inbox.go` | the notification delegates to `internal/agentmessage` |
| `internal/tools/declaration_names.go` | **new** — the catalogue↔declaration pairing |
| `internal/tools/declaration_names_test.go` | **new** — the pairing |
| `internal/permissions/policy.go` | `DecideNames` — the decision under several names |
| `internal/permissions/policy_names_test.go` | **new** — the decision |
| `internal/toolrunner/runner.go` | `Decide` — the decision as a pure function |
| `internal/sandbox/sandbox.go` | `ExtraPaths` — the child's worktree |
| `internal/sandbox/extra_paths_test.go` | **new** — the fourth root |
| `internal/cliapp/tui_subagent_async.go` | **new** — the TUI's registry, notifier and gate |
| `internal/cliapp/tui_run.go` | the run mode holds the registry |
| `internal/cliapp/tui_runmode_agent.go` | the TUI hands over the registry, the id and the gate factory |
| `internal/printmode/print_pig.go` | the print mode's registry, gate and the multi-turn loop |
| `docs/guide/tools.md` | *The asynchronous delegation*, *The permission gate reaches the subagent* |

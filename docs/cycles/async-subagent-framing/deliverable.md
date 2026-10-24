# Cycle `async-subagent-framing` — the subagent→parent delivery contract, measured

**Status**: evidence complete, **no clone code changed** (this cycle was scoped to measurement)
**Reference**: `~/.local/bin/agy` 1.4.4, sha256 `9abb478686dc08d48699ba1604c14d15819ef6a2542d5d32173acbd5f02d712d`
**Question it answers**: the clone's `invoke_subagent` runs subagents **synchronously** on purpose, because
the async framing was unmeasured. It is now measured — including the exact text the parent model reads.

## TL;DR

The reference's async path is now recovered end to end:

1. **A delivered message is not a user turn and not a tool result.** It is an `AgentMessage` carried on a
   `CortexStepSystemMessage` step, and its trajectory **input** string is empty (the converter is a 32-byte
   `return ""`). It enters the parent's context through the **output** string of that step, at the **start of
   the next invocation** (`MessageDeliveryStrategy = NEXT_INVOCATION | WHEN_IDLE`).
2. **The exact text the parent model reads is recovered verbatim:**

   ```
   The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.\n<SYSTEM_MESSAGE>\n{content}\n</SYSTEM_MESSAGE>
   ```

   142-byte preamble ending in a single `\n`, then `FormatWithXMLTag`'s output, which is
   `fmt.Sprintf("<%[1]s>\n%[2]s\n</%[1]s>", …)`. The UI title for the same message is `Message from %s`.
3. **`invoke_subagent` sends the subagent's task as a *message*, not a user turn** — titled `Message from
   <parent>`, and the subagent's own system instruction is *"Your task arrives as a system message from the
   agent that invoked you. Treat that message exactly as if the user had sent it to you directly: it is the
   user's complete request, and you respond to it as you would to the user."*
4. **`send_message`'s result texts, `manage_subagents list`/`kill` texts, and the `list` JSON schema are all
   recovered** (section below). `kill_all` already matched; the rest did not.
5. **Verdict: implement it.** The framing is no longer a guess, so a mailbox is now a port rather than an
   invention. Two small unknowns remain (the wire *role*, and the JSON field order) — neither changes the
   design, and both are one live capture away.

## A correction first: the committed seeds are stale

The first round of decompilation in this cycle produced **garbage**, and the reason matters for every future
cycle:

| | functions |
|---|---|
| `out/local-macos/seeds/seeds.txt` (committed) | **154,525** |
| `~/.local/bin/agy` 1.4.4 (regenerated, 0.94 s) | **160,459** |

The committed seeds — and therefore the Ghidra project `out/local-macos/proj` and every `.c` under
`out/local-macos/dec/` — are from an **older build**. `utils.DeliverMessages` sits at `0x101245200` in the
committed seeds and at `0x10130f920` in 1.4.4; every target in this cycle had moved. A first-pass decompile
"confirmed" things that were in fact neighbouring functions. **Those results are discarded.** Everything
below is from 1.4.4 addresses regenerated with:

```sh
cd rea-go-seeds && go run . ~/.local/bin/agy /tmp/seeds144     # 0.94 s, no network
```

Two tooling consequences, both now fixed in the repo:

* Ghidra's `&UNK_<addr>,<len>` refs are **not trustworthy** for literals whose pointer it failed to track —
  the synthesised address lands in unrelated code or mid-blob. Reading them as strings produces confident
  nonsense. `out/local-macos/scan_strings.py` pairs `adrp`+`add` with the following length immediate from
  the instruction stream instead; `out/local-macos/find_callers.py` maps `bl` targets back to names.
* `out/local-macos/protos/README.md` documents the descriptor extraction; that is generalised into
  `out/local-macos/extract_descriptors.py` (it is how `exa.cortex_pb` was pulled — see below).

## The delivery contract

### Wire data model 【descriptor】

`exa.cortex_pb` — 485 messages, 86 enums, extracted from the 1.4.4 binary at file offset `0x3b72ddd`
(206,947 bytes). The subagent/message subset is committed as
`out/local-macos/protos/cortex_pb_1.4.4.subagents.txt`; regenerate any other slice with
`extract_descriptors.py`.

```proto
message AgentMessage {
  int64  id;  string recipient;  string sender;  string content;
  MessagePriority priority;  int64 timestamp;
  MessageRenderDetails render_details;   // { message_title }
  bool   hide_from_user;
  CortexStep step_payload;  ... source_metadata;
  MessageDeliveryStrategy delivery_strategy;
}
message CortexStepSystemMessage { string message; StepRenderInfo render_info;
                                  string event_type; AgentMessage agent_message; ... }
enum MessageDeliveryStrategy { …_NEXT_INVOCATION; …_WHEN_IDLE; }
message SubagentResult { string conversation_id; string log_absolute_uri; string workspace_uris; }
```

`SubagentResult`'s three fields are exactly the ack JSON the clone already emits for `invoke_subagent`
(`conversationId` / `logAbsoluteUri` / `workspaceUris`) — that part was already right 【descriptor + 一手】.

### What the parent model reads 【反编译】

`chatconverters.(*SystemMessageStringConverter).ToOutputString` (`0x10155e320`, 416 B) is, in full:

```
wrapped = FormatWithXMLTag("SYSTEM_MESSAGE", []string{content})
return  concatstrings([]string{PREAMBLE, wrapped})
```

with `PREAMBLE` = the 142 bytes at `0x10353cdda`:

```
The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.\n
```

and `FormatWithXMLTag` (`cortex/utils/utils`, `0x10131ab90`) = `Sprintf` of the 22-byte format at
`0x1033d8501`: `<%[1]s>\n%[2]s\n</%[1]s>`.

`{content}` is `AgentMessage.content` verbatim — nothing is prepended (the sender name lives only in the
UI title). That title is built by `utils.buildMessageRenderInfo` from `Message from %s`.

The step-side converters are all empty:

```
10155e300  (*SystemMessageStringConverter).ToInputString        size 32
10155d910  (*InvokeSubagentStringConverter).ToInputString       size 32
101564920  (*EphemeralMessageStringConverter).ToInputString     size 32
10155e300: mov x0, xzr; mov x1, xzr; mov x2, xzr; mov x3, xzr; ret
```

so the message arrives as replayed **output**, at the next invocation — which is exactly what the prompt we
already ship promises: *"You receive messages automatically at the start of each invocation. All messages
are delivered in full directly into your context"* and *"A message arrives from a subagent or peer agent"*
under `## Reactive Wakeup (No Polling Needed)` 【一手, `internal/agyprompt/testdata/capture_1.4.4.txt`】.

`utils.DeliverMessages` (3440 B) is the pre-hook that dequeues at invocation start; its log tags are
`MessageDeliveryH…` and `agent_me…`, and `utils.receiveMessages` distinguishes *"failed to check for next
invocation messages: %w"* from *"failed to receive when idle messages: %w"* — the two delivery strategies,
visible in the code.

If a subagent ends without sending anything, the parent is handed this as the content 【strings】:

```
Subagent %s went idle without reporting anything back. Check its work, or send it a message at that id asking it to report back.
```

which is the fix the 1.4.x changelog describes ("*the parent being handed an empty notice when a subagent
went idle without a final response*").

### The subagent's side 【反编译 + strings】

`handlers.(*InvokeSubagentHandler).launchSubagent` (`0x101d2c690`) ends with:

```
failed to resolve workspace URIs for subagent: %w
failed to create subagent conversation: %w
failed to get transcript path: %w
Message from %s                                  ← title of the initial message
failed to send initial message to subagent: %w
message client is not available
failed to execute subagent script: %w
```

The task is delivered through the messaging system (matching the experiment flag
`send-subagent-initial-prompt-as-message: true` 【strings】), and the subagent's system instruction is the
224-byte literal:

```
Your task arrives as a system message from the agent that invoked you. Treat that message exactly as if the user had sent it to you directly: it is the user's complete request, and you respond to it as you would to the user.
```

## Result texts

All of these are literals referenced from the named function (address → function from the 1.4.4 seeds),
read out of the instruction stream by `scan_strings.py`. Grade 【反编译】 for the pairing, 【strings】 for the
text itself.

### `send_message` (`tools.(*SendMessageTool).Execute`, `0x101d89520`)

| text |
|---|
| `Message sent to %q.` |
| `Message sent to %d subagents: %s.` |
| `Message sent to %d of %d subagents: %s.` |
| `failed to deliver the message to %q: %w` |
| `the sending conversation is unavailable` |
| `messaging is not supported in this environment` |
| `a tool context is required to send a message` |
| `send_message does not deliver input to tasks; use the %s tool with action 'send_input' instead` |
| `internal error: failed to set initial tool result: %w` |

The last one is the tell: **`Execute` sets an initial result and then updates the step render info
asynchronously** (`failed to update step render info: %w` in the handler). The clone's `sendMessageTool`
returns a hard error for every recipient instead — that is the piece to replace.

`handlers.(*SendMessageSubHandler).Handle` (`0x101d44760`) carries the render labels `Recipient`, `Message`,
`Priority`, the action `send`, the summary format `**Summary:** *%s*\n***\n`, and the errors
`agent manager not available` / `message client not available`.

### `manage_subagents`

`(*ManageSubagentsTool).list` (`0x101d5fff0`):

| text |
|---|
| `You have %d active subagent(s):\n%s` |
| `internal error: failed to marshal the subagent states: %w` |

`%s` is the marshalled states. The entry schema is recoverable from the struct tags 【strings】:

| field | description |
|---|---|
| `type` | The subagent's type name. |
| `role` | The subagent's role, or its type name if no role was set. |
| `conversationId` | The subagent's conversation ID, used to message or kill it. |
| `state` | The subagent's current lifecycle state. — `enum=running,idle,waiting_for_input,waiting_for_dependents,waiting_for_message,canceling,errored,unspecified` |

`(*ManageSubagentsTool).kill` (`0x101d60560`):

| text |
|---|
| `Successfully killed %d subagent(s) and their descendants.\nKilled roles: %s` |
| `No active subagents to kill.` |
| `conversation %q is not an active subagent of this conversation, so nothing was killed; run the %q action to see the active subagents and their ids` |
| `internal error: this environment has no subagent manager, so subagents cannot be killed here` |
| `failed to kill subagent %q: %w` |

The last two are **new**: the clone answers `No active subagents to kill.` for `kill_all` and a generic
failure for `kill`, where the reference distinguishes "unknown id" and "no manager".

### `invoke_subagent` (`tools.(*InvokeSubagentTool).Execute`, `0x101d5ace0`)

| text |
|---|
| `invalid arguments: %w; pass a JSON object matching the %s schema` |
| `maximum subagent nesting depth (%d) exceeded` |
| `subagents are not supported in this environment` |
| `internal error: the current conversation's trajectory is unavailable, so subagents cannot be launched; retrying will not help` |
| `internal error: %s was invoked without a tool context; this is a harness bug, not an argument problem` |

Spinner/terminal step labels recovered alongside: `Starting…`→`Started`, `Finding…`→`Found`,
`Killing…`→`Killed`, `Messaged`, `Spawning` 【strings】. These matter for the TUI renderer in
`cmd/wagyu/tui_tools.go`, which currently invents its own.

## What this would mean for the clone (not implemented)

The design is now constrained rather than guessed:

* `subagenttools` needs a mailbox: `Deps` gains a queue, `invokeTool.Execute` enqueues and returns the
  `SubagentResult` ack, and a delivery path replays `AgentMessage`s into the parent's next turn as the
  recovered `PREAMBLE + <SYSTEM_MESSAGE>…` text. Two call sites to thread: `cmd/wagyu/print_pig.go:138`
  and `cmd/wagyu/tui_run.go:3008`.
* The parent's turn must be **re-armed**, not interrupted. PiG's `coding.Session` has no mid-turn injection
  (verified earlier), so delivery has to open the *next* turn — which matches `NEXT_INVOCATION` and is the
  only mechanism the reference's own prompt describes.
* `send_message` becomes a real enqueue with the three success texts and the four error texts.
* `manage_subagents list`/`kill` get the recovered texts and the `{type, role, conversationId, state}` JSON.
* Everything the prompt already promises the model becomes true.

Cost, honestly: it is one focused subsystem, and the risky part is turn re-arming inside PiG — the thing
that has produced silent hangs before. Keeping it in one file with the framing constants exported makes it
revertible in one commit.

## 硬约束 / what is measured

* Every literal above is paired with the function that references it, from **agy 1.4.4**, with addresses
  regenerated for this binary.
* The `<SYSTEM_MESSAGE>` framing, the `NEXT_INVOCATION`/`WHEN_IDLE` split, the `AgentMessage` field set, the
  `SubagentResult` ack, the subagent-side task message and all result texts are recovered.
* The clone is **unchanged** by this cycle; `invoke_subagent` remains synchronous, as documented in
  `docs/cycles/cycle-293-model-tool-surface/deliverable.md`.

## 存疑 / open

1. **The wire role of a delivered message is not recovered.** The step converts to an output string, but
   which `ChatMessage` role it becomes (`user` vs merged into the previous content) lives in
   `constructMessagesForStep`, whose role constants are not literals. The flags
   `GetDisableSystemMessageMerging` / `GetDisableSystemMessageTag` /
   `GetAppendEphemeralToPreviousToolResult` imply the default is *not* merged, i.e. **【推测】 user role**.
2. **The JSON field order for the `list` entries** is inferred from struct-tag order in the string blob
   (`type`, `role`, `conversationId`, `state`) 【推测】. Go marshals in declaration order, but the
   declaration order was not read from the type descriptor.
3. **No live capture was made.** Everything here is static. The two open items are the ones a single
   mock-backend capture would settle — the mock harness used in cycles 292/293 is not committed.

## limitations

* `out/local-macos/seeds/` (154,525) and `out/local-macos/proj` / `dec/` are **pre-1.4.4** and must not be
  cited for 1.4.4 findings. Regenerating the seeds is 1 second; rebuilding the project is ~11 minutes.
  Nothing here requires the rebuild.
* `send_message`'s `Priority` and `CellIDs` fields exist in the handler but **not** in the CLI's 1.4.4
  declaration (`Recipient`, `Message` only, committed in `internal/tools/model_tools_1.4.4.json`), so they
  are not part of the CLI surface.
* `list_peers` appears in the strings but **not** in the 1.4.4 `manage_subagents` `Action` enum
  (`list`, `kill`, `kill_all`) — it belongs to another surface or an older build.
* The `%s` in `Message sent to %d subagents: %s.` (broadcast summary) and in
  `Successfully killed … Killed roles: %s` is a comma/space-joined list of roles; the separator was not
  recovered 【推测】 `", "`.

## New artifacts

| File | What |
|---|---|
| `out/local-macos/extract_descriptors.py` | pull any embedded `FileDescriptorProto` out of the binary |
| `out/local-macos/scan_strings.py` | recover Go string literals from arm64 disassembly (`adrp`+`add` + length immediate) |
| `out/local-macos/find_callers.py` | map `bl` targets back to Go function names |
| `out/local-macos/protos/cortex_pb_1.4.4.subagents.txt` | the subagent/message slice of `exa.cortex_pb` |

All three scripts are standard-library only (plus `google.protobuf` for the descriptor parse), per the
repo's constraint on `out/local-macos/*.py`.
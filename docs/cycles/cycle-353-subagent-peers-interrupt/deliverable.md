# Cycle 353 deliverable: the peers, the broadcast, and the cancelled turn's own answer

**Cycle:** `cycle-353-subagent-peers-interrupt`
**Subject:** cycle 352 left a deviation list for the subagent surface, and one
of its entries was not a subagent entry at all: the reference answers a turn
the user cancels with `Esc` **in the composer**, not with an error row in the
transcript, and the clone answered with a row. This cycle closes that and
four of the subagent deviations: `manage_subagents` grows the reference's
fourth action, `list_peers`; `send_message` answers `Recipient='@children'`
with the reference's broadcast; a failed subagent says the reference's
sentence; and a `kill` with no ids says what the reference says.
**Method:** the 1.4.8 binary read first-hand — its string table for the
literals, its Go reflect metadata for the schema tags, and Ghidra for the
two handler functions that decide the answers. No live capture was
possible: `~/.config/antigravity` is empty, so the reference is not logged in
on this machine.

---

## 0. The short answer

| | before | now |
|---|---|---|
| `Esc` during a turn | the turn's failure became a transcript row | **no row**; the composer answers `  ⎿  Interrupted · What should <CLI> do instead?` |
| the cancelled turn's own stream error | travelled to the transcript as text | **dropped** — the cancellation is not part of the answer |
| `manage_subagents` `Action` | `list` / `kill` / `kill_all` (3 values) | `list` / **`list_peers`** / `kill` / `kill_all` (4 values), with the reference's own field description |
| `list_peers` | — | the parent conversation's subagents; a conversation with no parent answers *"You have no parent conversation, so you have no peer subagents: []"* |
| `kill` with no ids | *"No active subagents to kill."* | *"the \"kill\" action requires ConversationIds; run \"list\" to get the ids of the active subagents, or use \"kill_all\" to kill all of them"* |
| `send_message` `Recipient='@children'` | *"recipient \"@children\" not found"* | the reference's **broadcast**, with its three summary forms |
| a failed subagent | *"Subagent <id> failed: …"* | *"The subagent <type> with id \"<id>\" encountered an error and has either stopped or failed to start execution: …"* |

---

## 1. The evidence

### 1.1 The interrupted hint 【反编译】

The reference's answer to a cancelled turn is a **literal in the composer**.
The binary holds it at `0x103617a41`, 48 bytes, no NUL:

```
  ⎿  Interrupted · What should %s do instead?
```

The two-space indent, the `⎿` hook (U+23BF) and the middle dot (U+00B7) are
the reference's own bytes — 48 is exactly their length in UTF-8.

Who formats it:

| function | address |
|---|---|
| `cli/model/model.(*PromptModel).interruptedLine` | `0x102bb48b0` |
| `cli/model/model.PromptModel.IsInterrupted` | `0x102bb4800` |
| `cli/model/model.(*PromptModel).handleEscInterrupt` | `0x102bb4690` |
| `cli/model/model.(*PromptModel).View` | `0x102bb49f0` |

`View` asks `IsInterrupted` and, when the latest turn was interrupted, calls
`interruptedLine`. That is the decisive part: **the hint belongs to the
composer**, and the transcript keeps nothing. The same function has a second
branch for a cancelled *compaction* — the literal at `0x103647b2e` is
`  ⎿  Compaction cancelled. Conversation was not compacted.`

The format's `%s` is the name of the CLI the user is talking to. The binary
holds both names: `Jetski CLI` at `0x1034386de` and `Antigravity CLI` at
`0x1034b2dca` — the reference picks one of them by the product name the
binary was built with.

### 1.2 The peers 【反编译】

`handlers.(*ManageSubagentsSubHandler).handleListPeers` lives at
`0x101db9560`; its caller `handlers.(*ManageSubagentsSubHandler).Handle` at
`0x101db91a0`. The decompilation answers three questions at once:

1. **Where do the peers come from?** The handler reads the *parent*
   conversation id out of the caller's context (`+0xf0`/`+0xf8` — a string
   header) and calls `utils.GetDirectSubagentsFromHub` with it. So the peer
   list is *the parent conversation's direct subagents* — not the caller's
   own children.
2. **What if there is no parent?** The length of that string is tested
   first; when it is zero the handler returns a fixed literal:
   `You have no parent conversation, so you have no peer subagents:\n[]`
   (66 bytes, at `0x10365bc95`). The root conversation therefore has *no*
   peers — its subagents are its children.
3. **What does the answer look like otherwise?** The states are marshalled
   (`encoding_json_v2_Marshal`) and formatted with
   `You have %d active peer subagent(s) (parent %s):\n%s`
   (51 bytes, at `0x1036265c4`).

The handler's own error strings are on the same table:
`manage_subagents list_peers: listing the subagents of parent conversation
%q: %w` (`0x1036785f9`) and `manage_subagents list_peers: failed to marshal
peer states: %w` (`0x10364f00e`).

### 1.3 The broadcast 【反编译】

`sendmessage.(*Delivery).Summary` is at `0x10155c580`. It writes one of
three sentences:

| literal | address | length |
|---|---|---|
| `Message sent to %q.` | `0x10350f243` | 19 |
| `Message sent to %d subagents: %s.` | `0x1035beb7f` | 33 |
| `Message sent to %d of %d subagents: %s.` | `0x1035e8e1a` | 39 |

and appends ` Failed to deliver to %q: %v.` (at `0x10359af8c`, 29 bytes —
note the **leading space**) for every recipient that failed.

The recipient form itself is named in the binary's string table: `Workspace`
at `0x1034214d6` sits next to `@children` in the same run, and the
reference's *second* `send_message` description names it outright —

> Send a message to another agent, such as a subagent or a peer, or pass
> `Recipient='@children'` to broadcast to all active direct subagents. Not a
> channel to the user.

(`0x1036a5d08`; the first description, the one a live session sends, is at
`0x1036824e9` and does not mention it.)

### 1.4 The failure notice 【strings】

The reference's sentence for a subagent that stopped on an error is at
`0x10368c746`:

```
The subagent %s with id %q encountered an error and has either stopped or failed to start execution: %s
```

### 1.5 The two declarations that coexist in 1.4.8 【strings + Go reflect metadata】

The 1.4.8 binary carries **two** `manage_subagents` parameter schemas, and
they do not agree:

| package | `Action` enum | `Action` description |
|---|---|---|
| `cortex/tools` (the CLI) | `required,enum=list,enum=kill,enum=kill_all` | *"'kill' terminates the subagents named in ConversationIds; 'kill_all' terminates all."* |
| `oneharness/internal/tools` (the SDK's harness) | `required,enum=list,enum=list_peers,enum=kill,enum=kill_all` | *"'list' lists active direct subagents; 'list_peers' lists active sibling subagents sharing the same parent conversation; 'kill' terminates the subagents named in ConversationIds; 'kill_all' terminates all."* |

The CLI's tag is at file offset `0x9404b5a`; the harness's at `0x4f35ddc`
(vaddr `0x104f35deb`), where it is the `Action string` field of an inline
`go.shape.struct` inside
`utils.ToJsonSchemaRawMessage[go.shape.struct { Action string "jsonschema:…"
… }]`.

This settles a question cycle 350 left open: **the three-value enum the
clone's captured declaration carries is the CLI's, and it is right.** The
four-value enum is the harness's. The clone is a clone of the CLI, so the
advertised declaration stays as captured; the *runtime* schema of the tool
(the one the executor validates against) carries the four values, because
the CLI's own handler knows the fourth action — `handleListPeers` is in the
CLI's `cortex/handlers` package, not in the harness's.

The reference's own unknown-action error still names three actions
(`manage_subagents: unknown action %q (must be 'list', 'kill', or
'kill_all')`, at `0x103670e2d`) — a quirk of the reference that the clone
reproduces byte for byte.

### 1.6 A third description, and the entry's schema 【strings】

The 1.4.8 binary also holds a **third** `manage_subagents` description, one
the 1.4.7 variant table does not list (it starts at `0x1036d1493`):

```
Manage existing subagents.
	Actions:
	* 'list': List active direct subagents with their conversation IDs and live state. Each subagent is reported as a JSON object with this schema:
	%s
	* 'list_peers': List active sibling subagents sharing the same parent conversation with their conversation IDs and live state.
	* 'kill': Terminate specific subagents and all their descendants.
	* 'kill_all': Terminate all subagents and all their descendants.

	When a subagent is killed, its branched workspaces will be deleted, but its logs and artifacts will be preserved.
```

The `%s` is filled at runtime — the schema of one entry is generated, not a
literal, so the reference's *field order* is still not readable from the
binary (see §4).

---

## 2. The model

### 2.1 The cancelled turn

The clone's turn has one owner of the "the user pressed Esc" fact:
`runMode.turnInterrupted`.

- `handleEscKeyMsg` cancels the turn **and** sets the flag.
- `recordTurnCompletion` skips the error row when the flag is set — the
  cancelled turn's failure *is* the cancellation.
- `updateBackendChunkMsg` drops the `"context canceled"` chunk that the
  loop emits for a cancelled turn (the loop reports the cancellation as a
  stream error; the reference does not put it in the transcript).
- `resetLiveTurnState` and `flushNextQueued` clear the flag: the hint
  belongs to the turn that was cancelled, not to the next one.
- `interruptedHintRow` renders the hint in the composer, under the input
  box, and nowhere else.

The literal lives in `internal/agent` (`agent.InterruptedHintFormat` /
`agent.InterruptedHintLine`), so the headless surface can use it too. The
name that fills the `%s` is `branding.Current().CLIName` — the clone's own
brand, which is the reference's own string under the reference's brand.

The split the clone paints — the `⎿` hook in the blue the other `⎿` rows
use, the sentence in dim gray — is the clone's own styling. The reference
paints the line with one lipgloss style that the decompilation does not
name.

### 2.2 The peers

The registry is the conversation's own: `Registry.ParentID()` is the
conversation that owns it, and `Registry.States()` is that conversation's
direct children. A subagent shares the parent's registry (cycle 352), so the
only thing that distinguishes the two callers is *who is asking* — which is
what the new `manageSubagentsTool.owner` field carries.

- The owner **is** the registry's parent → the caller is the root → the
  reference's *"no parent conversation"* answer.
- The owner **is not** → the caller is a subagent → the peers are the
  parent's subagents, and the answer is the reference's format string.

`rebindMessaging` (now also rebinding the manage tool) gives the child its
own owner when it is launched.

### 2.3 The broadcast

`send_message` reads `Recipient` first; `@children` is a recipient form, not
an id. The broadcast walks the registry's live children, delivers to each,
and picks the summary:

- every delivery succeeded → `Message sent to %d subagents: %s.` with the
  ids joined by `", "`;
- some failed → `Message sent to %d of %d subagents: %s.` followed by one
  ` Failed to deliver to %q: %v.` per failure.

The single-recipient path keeps its own `Message sent to %q.` and its own
not-found sentence.

### 2.4 The failure notice

`reportFailure` now says the reference's sentence, with the subagent's type
name (its role when the call named one) and its conversation id.

---

## 3. 硬约束 (hard constraints)

- **The hint belongs to the composer.** `PromptModel.View` renders it; the
  transcript stays empty. A cancelled turn must not leave a row — not an
  error row, and not the `"context canceled"` chunk either.
- **The flag is per turn.** It is set by the Esc that cancels, and cleared
  by the next turn's start (`resetLiveTurnState`) or by the next queued
  prompt's start (`flushNextQueued`).
- **The peer's scope is the parent conversation.** `list_peers` asks the
  *parent's* subagents, never the caller's own children, and a conversation
  without a parent has no peers at all.
- **The reference's texts are the reference's texts**, including the ones
  that name fewer actions than the tool accepts: the unknown-action error
  names three actions, and `kill_all` with nothing live still answers
  *"No active subagents to kill."*
- **`@children` is a recipient, not an id.** It must never reach the
  not-found sentence.
- **The advertised declaration is the CLI's.** The clone's captured
  three-value `Action` enum stays; the four-value enum belongs to the
  harness's declaration, and the CLI's *handler* is what knows the fourth
  action.

## 4. 存疑 (open questions)

- **The entry's field order is still a guess.** The reference's four field
  names and tags are known (`type`, `role`, `conversationId`, `state` — the
  Go reflect metadata names them `TypeName`, `Role`, `ConversationID`,
  `State`), but the *order* is not: Go's reflect data stores the fields in
  a type descriptor, and the tag blob the compiler emits is not grouped by
  struct, so the order cannot be read out of the binary the way the text
  can. The clone keeps `type, role, conversationId, state`.
- **Whether the peer list includes the caller.** `handleListPeers` asks the
  hub for the parent conversation's direct subagents, and the caller is one
  of them — so the reference's list *includes the asking subagent*. The
  clone does the same (it lists the whole registry). The word "peer"
  suggests otherwise, so this is a reading of the decompilation, not a
  certainty.
- **Where exactly the hint sits inside the composer block.** `View` renders
  it, but the reference's block order (mode hint, input, notices) does not
  name the hint's slot. The clone puts it under the input box.
- **The hint's colours.** The reference paints the line with one lipgloss
  style; the decompilation does not name it, so the clone's split (blue
  hook, dim sentence) is the clone's own.
- **The wording for a recipient that cannot be reached.** The reference
  wraps a real error in ` Failed to deliver to %q: %v.`; the error it wraps
  is not recoverable, so the clone supplies its own ("the recipient is no
  longer reachable").
- **The third `manage_subagents` description** (§1.6) is in the 1.4.8
  binary but not in the 1.4.7 variant table, and the selector between the
  three descriptions is still untraced.

## 5. Limitations

- **No live capture.** The reference is not logged in on this machine
  (`~/.config/antigravity` is empty), so every claim here is read off the
  1.4.8 binary — its string table, its Go reflect metadata, and its
  decompiled handlers. Nothing was driven through a live session.
- **The advertised declaration still carries the three-value enum.** The
  clone's model therefore never *sees* `list_peers` in the `Action` enum,
  and the reference's second `send_message` description — the one that
  names `@children` — is not the one the clone ships. Both features are
  implemented and reachable (PiG validates the *required* fields and the
  *types*, not the enum), but a model that only reads the advertisement
  will not find them. Upgrading the captured declaration is a separate
  cycle: it needs either a live 1.4.8 capture or a full reconstruction of
  the twelve declarations from the binary, and several golden tests pin the
  fixture's bytes.
- **The registry is flat.** The reference's tree is per conversation: a
  subagent's *own* children are distinct from its siblings'. The clone's
  registry is the parent's, so a subagent that launches a sub-subagent
  puts it in the same flat list as its siblings, and a subagent's
  `@children` broadcast reaches its siblings rather than its own children.
- **The grant model is unchanged.** `manage_subagents` still reaches a
  subagent only through the `self` type, which inherits the parent's tool
  set; the base and research grants do not carry it. The reference's own
  `define_subagent` description says a subagent always gets "tools to
  research the codebase and to communicate with other agents" — nothing
  about managing them — so the clone leaves the model alone.
- **The headless surface is untouched.** `internal/printmode` does not
  answer a cancelled turn with the hint; only the TUI does.

---

## Files

| | |
|---|---|
| `internal/agent/interrupted.go` | **new** — the reference's interrupted hint, its literal and its renderer |
| `internal/cliapp/tui_interrupted.go` | **new** — the composer hint row |
| `internal/cliapp/tui_interrupted_test.go` | **new** — the Esc, the missing error row, the missing chunk, the hint's lifetime |
| `internal/cliapp/tui_run.go` | the run mode holds `turnInterrupted` |
| `internal/cliapp/tui_update_keys.go` | the Esc marks the turn as interrupted |
| `internal/cliapp/tui_update_stream.go` | no error row, no cancellation chunk, the flag's lifetime |
| `internal/cliapp/tui_view.go` | the hint's slot in the composer block |
| `internal/cliapp/tui_prompt_submit.go` | the next turn clears the flag |
| `internal/subagenttools/manage.go` | the four-value `Action`, `list_peers`, the kill text, the unknown-action text |
| `internal/subagenttools/send.go` | `@children`, the three summaries, the per-recipient failure |
| `internal/subagenttools/launch.go` | the rebind covers the manage tool; the reference's failure notice |
| `internal/subagenttools/grant.go` | the manage tool knows its owner |
| `internal/subagenttools/subagenttools.go` | the manage tool knows its owner |
| `internal/subagenttools/peers_test.go` | **new** — the peers, the broadcast, the failure notice |
| `internal/subagenttools/manage_subagents_test.go` | the kill texts |
| `internal/subagenttools/subagenttools_test.go` | the four-value enum |

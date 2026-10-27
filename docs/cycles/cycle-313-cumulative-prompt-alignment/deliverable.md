# The cumulative prompt, aligned — deliverable

**Cycle:** `cycle-313-cumulative-prompt-alignment`
**Subject:** the four details cycle 312 left open about the reference's
cumulative-prompt handler — the step-id base, the message role, the run
command's input string and the gate's rule — plus the trimming rules, closed
against the reference `agy` 1.4.7.
**Evidence:** 【反编译】 the handler's and the converters' control flow in the
1.4.7 binary; 【strings】 the reference's own format strings, recovered by
address; 【一手】 the reference's recovered `step_strings/run_command.tmpl` and
the clone's own implicit-trajectory record.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the step id** | 【推测】 the step's own index in the trajectory | 【反编译】 the reference's own formula, with its two config knobs — `trajectory_index_increment` and `trajectory_start_index` — modelled in `Options` |
| **the role** | 【推测】 `"user"`, argued from the message's position | 【反编译】 the message is a `chat_go_proto.ChatMessagePrompt` whose `source` is `ChatMessageSource_USER` (1) — the same source the current turn's own user message carries |
| **the run command's input string** | 【推测】 the bare command line | 【反编译】 `"Command: %s"`, plus `"CWD: %s"` on a second line — the proposed command line first, the command line second |
| **the gate** | 【推测】 "the step's type must be on the list" | 【反编译】 `allowed_implicit_step_types` is a **map**: the entry must be present **and true** (and 【一手】, in cycle 314, a `map<string, bool>` — the key is the step type's name) |
| **the trimming** | the trajectory is never cut | the per-step token limit is implemented (and inert, like the reference's default); the trajectory-level cut is documented as still open |

Two of the four changed the clone's output: the trajectory's messages now say
`Command: ls -la` — and `CWD: …` — where they used to say the bare command
line, and the gate is a map.

---

## 1. The reference's shape

### 1.1 The step-id base

【反编译】 the handler's renumbering, at `0x102236488`–`0x1022364ac`:

```
ldr  x6, [x5, #0x110]     ; the message's own step id (an int32)
ldr  x7, [sp, #0x240]     ; the config the handler holds
ldr  x8, [x7, #0x68]     ; the start index — an optional int32
cbz  x8, 1022364a8        ; unset → the id is the step's own index
ldr  x2, [x8]            ; *the start index
add  x2, x2, x6          ; start + stepIndex
ldr  x3, [sp, #0x58]     ; the handler's cached-message count
sub  x6, x2, x3          ; start + stepIndex - cached
ldr  x1, [x7, #0x70]     ; the index increment
add  x0, x1, x6          ; increment + (start + stepIndex - cached)
```

So

```
id = increment + (start + stepIndex - cached)   when start is set
id = increment + stepIndex                        otherwise
```

【反编译】 the two knobs are named. The cumulative-prompt config
(`exa.prompt_pb.CumulativePromptConfig`) carries **field 24
`trajectory_start_index`** — a proto3 *optional* int32, in the oneof
`_trajectory_start_index`, which is why the code reads a pointer and tests
it for nil — and **field 25 `trajectory_index_increment`**, a plain int32.
The config's field 23 is `disable_custom_step_id`.

> **Corrected in cycle 314.** The config the handler reads at +0x68 and
> +0x70 is **not** the proto: the proto's own layout puts the two fields at
> **+0x88 and +0x90** (the generated getters, `0x100978a80` and
> `0x100978aa0`), and its +0x68/+0x70 are the len and the cap of the
> `allowed_cascade_step_types` slice. The handler holds a *derived* config
> that carries the same two knobs at +0x68/+0x70. See
> [`cycle-314-cumulative-prompt-config`](../cycle-314-cumulative-prompt-config/deliverable.md).

【反编译】 the defaults come from
`prompt.ReadCumulativePromptConfigFromUnleash` (`0x101b16a60`): when the
server-side flag is absent the reference builds its own config, and it
writes **nil** to the proto's +0x88 (the start index is unset) and **zero**
to its +0x90 (the increment).

> **Corrected in cycle 314.** The two globals this builder reads
> (`0x10b50d0a8` and `0x10b50d0b0`) are *not* the two knobs: they are the
> len and the cap of the compile-time `allowed_cascade_step_types` slice at
> `0x10b50d0a0`, and both are zero. The conclusion below is unchanged — the
> knobs are zeroed explicitly.

**So with the reference's own default config the id is the step's own index in
the trajectory** — which is what the clone already did, for the wrong reason.

### 1.2 The role

【反编译】 the message the converter builds is a
`chat_go_proto.ChatMessagePrompt` (type descriptor `0x109804f68`, 408 B). Its
fields, in the hybrid API's layout:

| offset | field |
|---|---|
| +0x18 | `source` (int32) |
| +0x20 | `prompt` (string) |
| +0x40 | `tool_calls` |
| +0x58 | `tool_call_id` |
| +0x70 | `tool_result_is_error` |
| +0x78 | `images` |
| +0xa8 | `thinking` |
| +0xf8 | `prompt_annotation_ranges` |
| **+0x110** | **`step_idx` (int32)** — the id the handler renumbers |
| +0x118 | `merged_step_idxs` |
| +0x130 | `provider_assigned_message_id` |
| +0x140 | `side_channels` |
| +0x158 | `passback_context` |

`source` is a `ChatMessageSource`: the converter sets **USER (1)** by default.
It becomes **CONTEXT (6)** only when the converter's `enable_context_role`
flag is on *and* the step's payload case is neither a user input (19) nor a
plan input (8).

【反编译】 the current turn's own user message is built by
`prompt.ConstructChatMessagesForChatCompletion` (`0x101b14240`) with the same
source. The trajectory's messages therefore travel in the **user-content
position, like the user's own message** — the clone's `RoleUser` was right,
and is now 【反编译】 rather than 【推测】.

### 1.3 The run command's input string

【反编译】 the reference's run-command converter lives in its own package:
`code.(*RunCommandStringConverter)` in
`google3/third_party/jetski/cortex/chatconverters/code/code`. Its
`GetPayloadCase` (`0x101508760`) returns **28** — the run command's payload
case — so the converter map *is* keyed by the payload case, and the run
command is covered.

【反编译】 `ToInputString` (`0x101508360`):

1. the command is the **proposed** command line, or the **command** line when
   the proposed one is empty, or the **args joined with a space** when both
   are empty;
2. the text is `fmt.Sprintf("Command: %s", command)` — the 11-byte format
   string at `0x103438634`;
3. a second line `fmt.Sprintf("CWD: %s", cwd)` — the 7-byte format string at
   `0x1033d2761` — when the command has a working directory;
4. the two parts are joined with a newline (the separator at `0x103d91ed8`).

The proto's declaration order puts the three strings where the code reads
them — `command_line` at +0x08, `proposed_command_line` at +0x18, `cwd` at
+0x28 — and the args slice at +0x108.

【反编译】 `ToOutputString` (`0x1015085e0`) is the template rendering: the
template provider is asked for `step_strings` / `run_command`
(`0x103450c97`, `0x10341ef4a`), which is the template the clone already
renders.

### 1.4 The gate's rule

【反编译】 `prompt.shouldFilterTrajectoryStep` (`0x101b16250`) takes the
allowed-types list, the per-type map and a mode flag, and:

- **list mode** (the flag at `*(int *)(*(int *)(param_1 + 0x10) + 0x100) == 1`):
  the step's type must be *in* `allowed_cascade_step_types`;
- **map mode**: the step's type name is looked up in the map
  (`runtime_mapaccess1_faststr`) and the step survives only when the entry is
  **present and true** (`*pbVar6 & 1`).

The map is the config's field 22, `allowed_implicit_step_types` — a
`map<string, bool>`, keyed by the type's *name*. A type that is
absent and a type that is present-and-false are the same thing to the
reference: both are filtered.

> **Corrected in cycle 314.** The map's *type* was written here as
> `map<CortexStepType, bool>`. The recovered descriptor says
> `map<string, bool>` — the key is the step type's name, not the enum
> value. The rule (present **and** true) is unchanged, and so is the
> clone's behaviour; the clone's own key is now the name too. See
> [`cycle-314-cumulative-prompt-config`](../cycle-314-cumulative-prompt-config/deliverable.md).

### 1.5 The trimming

Three truncation helpers exist, and they are **not** all on this path.

| helper | what it does | on the cumulative-prompt path? |
|---|---|---|
| `chatconverters.truncateOutput` (`0x101514080`) | `go_utils.Truncate(s, 2048, priority 2)` — a 2048-**byte** cap, keeping both ends | **no** — `constructMessagesForStep` never calls it |
| `chatconverters.maybeTruncateString` (`0x10151b680`) | `go_utils.Truncate(s, maxLen, priority 1)` | no |
| `(*TrajectoryChatConverter).truncateStringToTokenLimit` (`0x10151c8b0`) | a limit below one leaves the text alone; otherwise the token count decides, and a text over the limit is cut to `limit × 3` bytes and gets the 135-byte note appended | **yes** |

【反编译】 `constructMessagesForStep` calls `truncateStringToTokenLimit` —
twice, on the input side and on the output side — and never `truncateOutput`.
The 2048-byte cap belongs to the tool-result path.

【反编译】 the limit comes from
`(*TrajectoryChatConverter).getMaxTokensForStep` (`0x10151c800`):
`max_tokens_per_step` (the config's optional int32 at the converter's +0x50)
unless the step's payload case is a plan input (8) or a user input (19), in
which case it is `max_tokens_per_user_input_step` (+0x78). Both default to
zero, and a limit below one leaves the text alone — **so with the
reference's own default config the trajectory's messages are not truncated at
all.**

【反编译】 the note the reference appends is the 135-byte literal at
`0x103667fcb`:

```
\n\nNOTE: The output was truncated because it was too long. Use a more targeted query or a smaller range to get the information you need.
```

【反编译】 the cut itself is `go_utils.truncateFromEnd` (`0x1007f8670`): the
text is cut to the byte budget from the front, and when the cut does not land
on a line boundary the remainder is cut back to its last newline, with a
carriage return before it dropped. A tail with no newline at all is empty.
The reference's own call passes **no** truncation-priority option, so the
priority is the default — zero — and the default cuts from the end.

【反编译】 the *trajectory-level* cut is a different thing:
`TrajectoryToChatMessagesToTokenLimitOrStepIndex` (`0x101520770`) adds up each
step's tokens and, when the running total passes the limit, drops the oldest
steps and inserts a checkpoint step
(`checkpointStepToChatMessages`, `0x1015123a0`). The limit itself is computed
from the model's context window through the config's multipliers
(`ephemeral_context_multiplier`, `intent_reservation_tokens`, …) and the
tokens the model reported for the prefix
(`modelReportedPrefixTokens`, `0x10151f370`). The clone does not do this —
see §5.

### 1.6 The conversation's own converter

【反编译】 the reference's conversation history is a trajectory step of its
own — `CORTEX_STEP_TYPE_CONVERSATION_HISTORY` (98), payload case 111 — with
its own string converter,
`chatconverters.(*ConversationHistoryStringConverter)` (`0x10151a920`):

- `GetPayloadCase` → **111**;
- `ToInputString` → **the empty string**;
- `ToOutputString` → the step's status string, else the payload's text.

The user-input converter (`0x101517ca0`) has the same shape: an empty input
and the payload's text as the output. So those steps contribute **one**
message, not a pair — and every message the converter builds gets the
three-line header.

---

## 2. The clone's shape

### 2.1 What changed

| where | what |
|---|---|
| `internal/cumulativeprompt/cumulativeprompt.go` | the converter: the step id, the gate, the input string, the per-step token limit |
| `internal/cumulativeprompt/cumulativeprompt_test.go` | the exact text of both messages, the id formula, the gate, the input string, the token limit |
| `internal/cliapp/tui_cumulative_test.go` | the TUI's expectation of the action message |
| `internal/printmode/print_cumulative_test.go` | the print path's expectation of the action message |

**The step id.** `Options` gained the reference's three inputs —
`StepIndexIncrement`, `TrajectoryStartIndex` (a `*int`, because the
reference's field is a proto3 optional) and `CachedMessages` — and
`stepID` implements the formula above. The defaults reproduce the old
behaviour exactly: the increment is zero and the start index is unset, so the
id is the step's own index.

**The gate.** `Options.AllowedStepTypes` changed from `[]int` to
`map[int]bool`, the reference's own shape. A nil map means the clone's
default (the run command, the only type its store holds); an empty map
filters everything.

> **Corrected in cycle 314.** Two claims here are wrong: the reference's
> default config's gate is **not** empty — it carries eight step types, all
> true, the run command among them — and the map's key is the step type's
> **name**, so the clone's key is now the name too
> (`map[string]bool`).

**The input string.** `inputString` now renders the reference's text:

```
Command: ls -la
CWD: /private/var/folders/…/T/opencode
```

The clone's store records the command in both the proposed and the plain
field, so the two first cases agree; the args case cannot fire, because the
clone's store does not record the args.

**The per-step token limit.** `Options.MaxTokensPerStep` models the config's
`max_tokens_per_step`. It is zero by default — inert, like the reference's
own default — and the token count is the clone's own
`compaction.EstimateTokens`, which divides by the reference's own
three-bytes-per-token divisor.

### 2.2 What did not change

- The two messages per step, the header's three lines and their order, the
  lead-ins and the step string: cycle 312's shape, unchanged.
- The cursor: only the steps no prompt has carried yet are converted.
- The store, the TUI wiring and the print path.

---

## 3. 硬约束 / the load-bearing rules

1. **The action message says `Command: …`, not the bare command line.** The
   reference's `RunCommandStringConverter.ToInputString` labels the command
   and, when the record carries a working directory, adds a `CWD: …` line.
   The clone's store records that directory, so the line is there.
2. **The proposed command line comes first.** The reference reads
   `proposed_command_line` and falls back to `command_line` — the opposite
   order to the one the clone used.
3. **The gate is a map, and the entry must be true.** "On the list" and
   "present and true" differ for a type the server knows about but
   disallows.
4. **The id is the step's own index — unless the server says otherwise.**
   The two knobs that change it (`trajectory_index_increment`,
   `trajectory_start_index`) are server-side; the clone's defaults reproduce
   the reference's own default config.
5. **The trajectory's messages travel in the user-content position.** The
   message's `source` is `ChatMessageSource_USER`, the same source the
   current turn's own user message carries.
6. **The 2048-byte cap is not on this path.** The reference's
   `truncateOutput` belongs to the tool-result path; the cumulative prompt's
   messages are cut by the *per-step token limit*, which the reference's own
   default config leaves at zero.

---

## 4. 存疑 / open questions

- **【存疑】 The conversation's step ids and timestamps.** The reference's
  conversation history is a trajectory step (98 / payload case 111) with its
  own converter, so a conversation step that joins the cumulative prompt
  *would* carry the header. Whether it does join it depends on the cascade
  config that `prompt.GetCascadeConfigForCumulativePrompt` (`0x101b16ba0`)
  builds, and its allowed-type list could not be read off the decompilation.
  The clone's `m.session` is a list of chat messages, not a trajectory, so
  giving them ids and timestamps would be a design change, not a detail.
- **【存疑】 `enable_context_role`.** The flag that would move a run command's
  message from `USER` to `CONTEXT` is a converter option; the request role a
  `CONTEXT` source maps to is not recoverable here. The clone keeps `user`.
- **【存疑】 The trajectory-level cut.** The rule is recovered (§1.5) but not
  the budget: it needs the model's context window, the config's multipliers,
  the model's reported prefix tokens and the checkpoint machinery. (Cycle 314
  recovered the multipliers — 0.65 / 0.9 / 0.5, with 512 tokens reserved for
  the intent — but not the formula that uses them.)
- **【推测】 The `CWD:` line.** The reference reads the run command's `cwd`
  field, and the clone's store records it — but the reference's own `!`
  command record for a workspace was only verified byte for byte in cycle
  304's capture, not re-verified here.

---

## 5. Limitations

- **The trajectory is never cut.** The reference's
  `TrajectoryToChatMessagesToTokenLimitOrStepIndex` drops the oldest steps
  when the running token total passes the limit and inserts a checkpoint
  step; the clone converts the whole trajectory, every time. Reproducing it
  needs a token counter, the model's context window and the checkpoint
  machinery.
- **The token count is an estimate.** The reference counts tokens with its
  own counter; the clone uses `compaction.EstimateTokens` — the reference's
  own three-bytes-per-token divisor, but a divisor, not a tokenizer.
- **The gate's map is the clone's, not the server's.** The reference's
  `allowed_implicit_step_types` arrives with the config; the clone's default
  is the reference's own default gate — the eight step types, all true
  (cycle 314) — and the run command is the only one its store can produce.
- **The clone converts one step type.** Its store holds run commands only.
- **The print path re-converts.** The reference's handler *caches* the
  converted messages (`SetPersistentCachedContext`); the TUI keeps a cursor
  in memory and the print path has one turn and converts once, which is the
  same result.
- **`hide_user_implicit_steps` has no counterpart.** It is a log-artifacts
  gate, and the clone writes no log artifacts.

---

## 6. The evidence

| artefact | what it is |
|---|---|
| `internal/cumulativeprompt/cumulativeprompt.go` | the converter: the step id, the gate, the input string, the token limit |
| `internal/cumulativeprompt/cumulativeprompt_test.go` | the exact text of both messages, the id formula, the gate, the input string, the limit |

### The binary

Reference `agy` 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.

| what | where |
|---|---|
| the handler's renumbering | `cumulative_prompt_handler` `0x10223640c`–`0x102236538` |
| the handler's id computation | `0x102236488`–`0x1022364ac` |
| the default config | `prompt.ReadCumulativePromptConfigFromUnleash` `0x101b16a60` |
| the gate | `prompt.shouldFilterTrajectoryStep` `0x101b16250` |
| the converter | `chatconverters.(*TrajectoryChatConverter).constructMessagesForStep` `0x10151d9d0` |
| the message struct | `chat_go_proto.ChatMessagePrompt`, descriptor `0x109804f68` |
| the current turn's message | `prompt.ConstructChatMessagesForChatCompletion` `0x101b14240` |
| the run command's converter | `code.(*RunCommandStringConverter)` `0x101508360` / `0x1015085e0` / `0x101508760` |
| the conversation's converter | `chatconverters.(*ConversationHistoryStringConverter)` `0x10151a920`–`0x10151a950` |
| the user input's converter | `chatconverters.(*UserInputStringConverter)` `0x101517ca0`–`0x10151a500` |
| the per-step limit | `(*TrajectoryChatConverter).getMaxTokensForStep` `0x10151c800` |
| the per-step truncation | `(*TrajectoryChatConverter).truncateStringToTokenLimit` `0x10151c8b0` |
| the byte cut | `go_utils.truncateFromEnd` `0x1007f8670` |

Runtime identities, from the 1.4.7 pclntab: `fmt.Sprintf` `0x10011d5f0` ·
`runtime.concatstring2` `0x10006c1e0` · `strings.Join` · `strings.LastIndex` ·
`strings.Index` · `runtime.mapaccess1_faststr` · `regexp.MustCompile`
`0x100159b90` · `regexp.(*Regexp).ReplaceAllString` `0x10015a680` ·
`(*Step).WhichStep` `0x1008f0350` · `(*Timestamp).AsTime` `0x10057ccd0` ·
`time.Time.Format` `0x1000ecdd0`.

The strings' addresses were recovered by searching the Mach-O's bytes for the
exact literal and adding the image base (`0x100000000`); each is unique in
the binary.

| literal | address | bytes |
|---|---|---|
| `Command: %s` | `0x103438634` | 11 |
| `CWD: %s` | `0x1033d2761` | 7 |
| `step_strings` | `0x103450c97` | 12 |
| `run_command` | `0x10341ef4a` | 11 |
| `run command step is nil` | `0x103520dde` | 23 |
| `<truncated %d bytes>` | `0x1034dd520` | 20 |
| the truncation note | `0x103667fcb` | 135 |
| `per-step token limit hit: truncating from %d to %d tokens` | `0x103608190` | 57 |

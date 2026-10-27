# The cumulative prompt — deliverable

**Cycle:** `cycle-312-cumulative-prompt`
**Subject:** the reference's cumulative-prompt handler — the machinery that
turns the `!` commands the user ran outside the conversation into chat
messages for the next prompt — and the clone's side of it.
**Evidence:** 【反编译】 the handler's and the converter's control flow in the
1.4.7 binary; 【strings】 the reference's own format strings, recovered by
address; 【一手】 the reference's recovered `step_strings/run_command.tmpl` and
the clone's own implicit-trajectory record.

---

## 0. Summary

| | before | after |
|---|---|---|
| **a `!` command** | recorded in the implicit trajectory — and never seen by the model | converted into the reference's own two messages and carried ahead of the next prompt |
| **the conversion** | none | `internal/cumulativeprompt`: one step → two messages, with the reference's header and its two lead-ins |
| **the TUI** | the turn's messages came from `m.session` alone | `runMode.implicitMsgs` + a cursor; the trajectory's messages lead the turn |
| **the print path** | the seed was the resumed conversation's turns alone | `pigSeedHistory` puts the trajectory's messages first |
| **the conversation store** | no `!` record | still none — the messages live outside `m.session` |
| **the `!` command's own text** | the CLI formatter's shape (`Command exited with code N.`) | the *step string* the reference's template renders (`The command exited with code N.`) — the shape the reference's cumulative prompt carries |

The `!` command's text is the same in both paths: the reference's cumulative
prompt carries the **step string**, not the CLI's own `functionResponse`
formatter. That is the finding that changed the shape.

---

## 1. The reference's shape

### 1.1 The two halves

The reference's cumulative prompt has two halves, and this cycle is about one
of them.

- The **persistent context** — what the handler keeps between turns.
  【反编译】 the package
  `google3/third_party/jetski/prompt/cumulative_prompt_handler/cumulative_prompt_handler`
  (init `0x1022357c0`) owns `getNewPersistentChatMessagePrompts`
  (`0x1022358d0`), `SetPersistentCachedContext` (`0x102235c40`),
  `SetTrajectoryStartIndexForCachedContext` (`0x102235e60`),
  `FetchCachedContextAndRefreshDecision` (`0x102236070`) and
  `ConstructCumulativePrompt` (`0x1022369f0`).
- The **converter** — the thing that turns a trajectory's steps into chat
  messages. 【反编译】
  `google3/third_party/jetski/cortex/chatconverters/chatconverters` owns
  `(*TrajectoryChatConverter).constructMessagesForStep` (`0x10151d9d0`),
  `StepToChatMessages` (`0x10151f640`), `TrajectoryToChatMessages`
  (`0x101520680`),
  `TrajectoryToChatMessagesToTokenLimitOrStepIndex` (`0x101520770`),
  `checkpointStepToChatMessages` (`0x1015123a0`) and
  `ConvertStepToOutputString` (`0x10151c600`).

The implicit trajectory is a trajectory, so it goes through the converter. The
handler then renumbers the step ids the converter wrote — see §1.4.

### 1.2 One step → two messages

【反编译】 `constructMessagesForStep` (`0x10151d9d0`):

1. It looks the step's converter up in a map keyed by the step's type:
   `(*Step).WhichStep` (`0x1008f0350`) → `runtime.mapaccess2_fast64`
   (`0x100009f20`). A step type with no converter is an error.
2. It calls the converter's **input** side — the itab's `fun[1]`
   (`0x10151de10`) — and truncates the result against
   `getMaxTokensForStep`.
3. It calls the converter's **output** side — the itab's `fun[2]`
   (`0x10151da88`).
4. It reads the step's **source** at `metadata+0x40` and compares it with
   **4** — `CORTEX_STEP_SOURCE_USER_EXPLICIT` — and picks the two lead-ins
   from that:

   | source | message 1 | message 2 |
   |---|---|---|
   | `USER_EXPLICIT` (4) | `The USER performed the following action:\n<input>` | the raw output |
   | anything else | `The following action was requested:\n<input>` | `The action produced the following result:\n<output>` |

5. It writes the step's own id into the first message at `+0x110`.

So a step the *user* performed becomes two messages: one that says what the
user did, one that carries the result. A step nobody performed gets the same
pair with two other lead-ins.

【strings】 the three lead-ins, and the header's three pieces, are the
reference's own format strings:

| literal | address | bytes |
|---|---|---|
| `The USER performed the following action:\n%s` | `0x1035c9fb0` | 43 |
| `The following action was requested:\n%s` | `0x1035ab99b` | 38 |
| `The action produced the following result:\n%s` | `0x1035cf7c0` | 44 |
| `Step Id: %d\n` | `0x10343bdc3` | 12 |
| `Step Id: %d\n%s` | `0x10346ba66` | 14 |
| `Created At: %s\n` | `0x103480ecf` | 15 |
| `Completed At: %s\n` | `0x1034a9808` | 17 |
| `2006-01-02T15:04:05Z07:00` | `0x10352f4af` | 25 |

### 1.3 The step header

【反编译】 a closure inside `constructMessagesForStep` (`0x10151e204`) walks
the messages it just built and prepends a three-line header to each of
them. The concatenation is `runtime.concatstring2` (`0x10006c1e0`), and the
order is fixed:

```
Step Id: N
Created At: <timestamp>
Completed At: <timestamp>
<the message's own text>
```

The timestamps come from the step's metadata:
`(*Timestamp).AsTime` (`0x10057ccd0`) → `time.Time.Format` (`0x1000ecdd0`)
with the layout `2006-01-02T15:04:05Z07:00` — RFC 3339, to the second, in
the machine's local zone. A step with no timestamp gets **no line** for it:
the reference tests the timestamp's presence before formatting it
(`0x10151e30c`, `0x10151e3f8`).

### 1.4 The renumbering

【反编译】 the handler, after converting a trajectory
(`TrajectoryToChatMessagesToTokenLimitOrStepIndex` at `0x1022363b4`), walks
the messages it got and rewrites the header (`0x10223640c`–`0x102236538`):

1. `regexp.MustCompile` (`0x100159b90`) with the literal `^Step Id: \d+\n`
   (`0x103481046`, 15 bytes — the regex's own escape sequence).
2. `regexp.(*Regexp).ReplaceAllString` (`0x10015a680`) strips the old
   header's first line from the message's content.
3. `fmt.Sprintf` (`0x10011d5f0`) with `Step Id: %d\n%s` (`0x10346ba66`)
   writes the new header back into the content.

The new id is **not** the step's own id. It is computed at
`0x102236488`–`0x1022364ac`:

```
id = handler.field70 + (*handler.field68 + message.stepId − trajectoryStartIndex)
```

`message.stepId` is the `int32` at `+0x110`; `trajectoryStartIndex` is the
handler's window start (the handler sets it through
`SetTrajectoryStartIndexForCachedContext`). The two handler fields are
opaque: the base the reference numbers from is not recoverable.

### 1.5 The gates

【strings】 the reference's cumulative-prompt config carries the field
`allowed_implicit_step_types`, and the prompt package filters the trajectory
through `shouldFilterTrajectoryStep` (`0x101b16250`) and
`ProcessTrajectoryForCumulativePrompt` (`0x101b163f0`). 【strings】 the
reference also carries a `hide_user_implicit_steps` flag — it is a
**log-artifacts** gate, not a prompt gate.

---

## 2. The clone's shape

### 2.1 The package

`internal/cumulativeprompt` is the converter, for the one step type the
clone's store holds:

```go
func Convert(steps []implicit.Step, opts Options) []Message
```

- `Options.From` is the cursor: the reference converts only the steps no
  prompt has carried yet (`SetPersistentCachedContext` +
  `getNewPersistentChatMessagePrompts`), so the clone converts only the new
  steps and keeps what it already converted.
- `Options.AllowedStepTypes` is the gate. Empty means the run command — the
  only type the clone's store holds.
- Each step becomes two `Message`s, both in the `"user"` role, each carrying
  the step header and then the reference's own text: the input side (the
  command line) and the output side (the reference's `run_command` step
  string, rendered from the recovered template through `internal/agystep`).

The message the model sees for a `!` command that ran `ls -la` is therefore:

```
Step Id: 0
Created At: 2026-10-26T12:34:56+08:00
Completed At: 2026-10-26T12:34:57+08:00
The USER performed the following action:
ls -la
```

and, as the second message, the step string — the template's rendering,
verbatim, leading newline included:

```
Step Id: 0
Created At: 2026-10-26T12:34:56+08:00
Completed At: 2026-10-26T12:34:57+08:00

The command exited with code 0.
Output:
total 12
```

### 2.2 The TUI wiring

`internal/cliapp/tui_cumulative.go` holds `collectImplicitMessages`: it
reads the workspace's trajectory from the implicit store
(`implicit.Open(convstore.StoreDir())`), converts the steps past the cursor
and appends them to `runMode.implicitMsgs`.

`internal/cliapp/tui_stream_prompt.go`'s `prepareStreamTurnMessages` puts
those messages **ahead** of `m.session` — the reference's cumulative prompt
carries the persistent context before the turn's own messages.

They are deliberately **not** in `m.session`: the reference records a `!`
command in the implicit trajectory only, and the conversation store holds no
record of it. Putting them in the session would write a `!` command into the
conversation store that the reference never wrote there.

The converted messages carry no `<USER_REQUEST>` envelope: the reference
wraps only what the user typed.

### 2.3 The print path

`internal/printmode/print_pig.go`'s `pigSeedHistory` is the print path's
seed: the trajectory's messages first, then the resumed conversation's
earlier turns. The cumulative prompt is not a TUI feature — it builds the
prompt for every turn, headless or not.

The TUI keeps a cursor in memory; the print path has one turn and converts
the whole trajectory once.

### 2.4 What closes the loop

The clone's own implicit record carries `source = USER_EXPLICIT` (4) — the
same value the reference's record carries — so the `!` command lands on the
reference's *user* branch and takes the `The USER performed the following
action:` lead-in. The record was already byte-compatible (cycle 304); this
cycle is what makes it *used*.

---

## 3. 硬约束 / the load-bearing rules

1. **The text a `!` command contributes is the step string, not the CLI's
   formatter.** The cumulative prompt carries
   `The command exited with code 0.\nOutput:\n…` — the template's rendering
   — and *not* `Command exited with code 0.\nCommand output:\n…`. The two
   shapes coexist in the clone (`internal/agystep` reads both); the
   cumulative prompt uses the step-string one.
2. **Two messages per step.** The reference emits the action and the result
   as two separate chat messages, each with the full step header. Collapsing
   them into one changes the prompt.
3. **The header is three lines, in this order**: `Step Id`, `Created At`,
   `Completed At` — then the message's own text. The timestamps use the
   reference's own layout (`2006-01-02T15:04:05Z07:00`, local zone) and are
   omitted entirely when the step has none.
4. **The `!` command stays out of the conversation.** The messages live in
   their own field (`runMode.implicitMsgs`), never in `m.session`, so the
   conversation store keeps holding no `!` record.
5. **The trajectory's messages lead the turn.** They are the persistent
   context; the turn's own messages follow.
6. **No `<USER_REQUEST>` envelope on the trajectory's messages.** The
   reference wraps what the user typed, not its own rendering of a step.
7. **Only new steps are converted.** The reference caches what it already
   converted; the clone keeps a cursor so a second turn does not carry the
   same `!` command twice.

---

## 4. 存疑 / open questions

> **Closed by `cycle-313-cumulative-prompt-alignment`** — the four items below
> are resolved there (the step-id base, the role, the run command's input
> string and the gate's rule). The section is kept as it was written.

- **【推测】** The step-id base. The reference computes
  `field70 + (*field68 + stepId − trajectoryStartIndex)`; two of the three
  terms are opaque. The clone numbers the steps by their own index in the
  implicit trajectory — the right value for a trajectory whose window starts
  at zero, but a guess in general.
- **【推测】** The role. The reference's chat-message struct's role field was
  not located; the clone uses `"user"`, on the grounds that a message about
  the user's own action can only travel in the user-content position.
- **【推测】** The run command's **input** string. The converter's input slot
  is recovered (it is the itab's `fun[1]`), but what the run command's
  converter puts there is not: the clone uses the command line, which is
  what the template's own "The user changed the command to be run to:"
  branch implies.
- **【推测】** The gate's rule. `allowed_implicit_step_types` is the
  reference's own field name, and the filtering functions are named; the
  clone applies it as "the step's type must be on the list".
- **【反编译】** The reference's *persistent-context* machinery is much
  larger than this cycle: open documents, CCIs, cached-context refresh
  decisions, the token multipliers (`ephemeral_context_multiplier`) and
  `intent_reservation_tokens`. Only the implicit trajectory's conversion is
  in scope here.

---

## 5. Limitations

- **The clone's conversation messages carry no step ids and no timestamps.**
  The reference's conversation trajectory goes through the same converter and
  gets the same three-line header. The clone's `m.session` messages have
  neither, so a prompt that mixes the two does not read like the
  reference's. This cycle only covers the implicit trajectory.
- **The trajectory is never trimmed.** The reference's
  `TrajectoryToChatMessagesToTokenLimitOrStepIndex` cuts the trajectory at a
  token limit; the clone converts the whole trajectory, every time.
- **The print path re-converts.** The reference's handler *caches* the
  converted messages (`SetPersistentCachedContext`). The TUI keeps a cursor
  in memory; the print path has one turn and converts once, which is the
  same result.
- **`hide_user_implicit_steps` has no counterpart.** It is a log-artifacts
  gate, and the clone writes no log artifacts.
- **The clone converts one step type.** Its store holds run commands only,
  so the reference's other lead-ins (`The following action was requested:`,
  `The action produced the following result:`) are never emitted. They are
  exported for the reader of this report, not used.

---

## 6. The evidence

| artefact | what it is |
|---|---|
| `internal/cumulativeprompt/cumulativeprompt.go` | the converter: two messages per step, the header, the gate, the cursor |
| `internal/cumulativeprompt/cumulativeprompt_test.go` | the exact text of both messages, the canceled step, the cursor, the gate |
| `internal/cliapp/tui_cumulative.go` | the TUI's read of the trajectory and the cursor |
| `internal/cliapp/tui_cumulative_test.go` | the delivery, the store, the carry-once |
| `internal/cliapp/tui_run.go` | `runMode.implicitMsgs` / `runMode.implicitCursor` |
| `internal/cliapp/tui_stream_prompt.go` | the seam: the trajectory's messages lead the turn |
| `internal/printmode/print_pig.go` | `pigSeedHistory` / `pigImplicitHistory` |
| `internal/printmode/print_cumulative_test.go` | the print path's delivery and the seed's order |
| `internal/implicit/trajectory.go` | the store the conversion reads (`source = USER_EXPLICIT`) |
| `internal/agystep/render.go` | the step string the second message carries |

### The binary

Reference `agy` 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.

| what | where |
|---|---|
| the converter | `chatconverters.(*TrajectoryChatConverter).constructMessagesForStep` `0x10151d9d0` |
| the converter's header closure | `0x10151e204`–`0x10151e538` |
| the source comparison (4 = `USER_EXPLICIT`) | `0x10151ded0`–`0x10151e0ac` |
| the handler's renumbering | `cumulative_prompt_handler` `0x10223640c`–`0x102236538` |
| the handler's id computation | `0x102236488`–`0x1022364ac` |

Runtime identities, from the 1.4.7 pclntab: `fmt.Sprintf` `0x10011d5f0` ·
`runtime.concatstring2` `0x10006c1e0` · `runtime.convT64` `0x100087380` ·
`runtime.convTstring` `0x1000873f0` · `regexp.MustCompile` `0x100159b90` ·
`regexp.(*Regexp).ReplaceAllString` `0x10015a680` ·
`(*Step).WhichStep` `0x1008f0350` · `(*Timestamp).AsTime` `0x10057ccd0` ·
`time.Time.Format` `0x1000ecdd0` · `runtime.mapaccess2_fast64`
`0x100009f20` · `runtime.growslice` `0x10008cab0`.

The strings' addresses were recovered by searching the Mach-O's bytes for the
exact literal and adding the image base (`0x100000000`); each is unique in
the binary.

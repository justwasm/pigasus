# The invalid tool call

**Cycle:** `cycle-310-harness-invalid-tool-calls`
**Subject:** `out/localharness/PARITY.md` §4.6 recorded that the reference
validates a call against its **runtime** schema before it dispatches the
tool and refuses an invalid one as an *invalid tool call* — while the clone
relaxed every `required` and ran the call anyway. This cycle captures the
reference's rules, its frame, its paragraph and its retry counter, makes
the clone match, and records what is still unknown.
**Evidence:** 【一手】 the reference binary's own frames and request bodies,
captured live through the SDK's protocol · 【反编译】 decompiled output at
confirmed Mach-O addresses · 【strings】 string-table hits only · 【推测】
hypothesis

---

## 0. Summary

| | before | after |
|---|---|---|
| a call the reference refuses | ran in the clone | never runs; the clone refuses it the same way |
| the frames | the announcement, the start, the result, the done | **one** frame: `STATE_ERROR`, `SOURCE_SYSTEM`, `TARGET_USER`, no action |
| the frame's `errorMessage` | — | `The model produced an invalid tool call.` (or the tool's own sentence) |
| the frame's text | — | `<sentence> ("<reason>")` |
| the model's text | the tool's own error | the reference's paragraph: the invalid branch, the reason, the guidance, the retries left |
| the retries | — | four, falling by one per refusal, reset by a call that ran; the run fails when they run out |
| the write tool's file | the content as the model gave it | the content **with a trailing newline**, and `Append: true` **appends** (§6) |

Differentials — twenty-one probes, one per rule of the reference's runtime
schema plus the write tool's own semantics: **0 frame differences** and
byte-identical request bodies after normalisation, except for the reference's
own racy `usage_update` position, the diff block's `endLine` in the append
case (§7.2) and the model-facing text of the unknown-tool case (§7.1).

---

## 1. The method

The probe drives the reference binary and the clone through the SDK's own
protocol: it writes a `HarnessConfig` into the `InitializeConversationEvent`,
points the model at a fake Gemini endpoint that replies with one scripted
tool call, and records every frame the harness writes and every request
body it sends. The workspace is snapshotted before the reference runs and
restored before the clone runs.

The comparison normalises what cannot match: the sequence number, the
timestamp, the prompt's local time, the `Created At` / `Completed At`
stamps, the tool call id (a UUID) and the session id.

---

## 2. The refusal

### 2.1 The frame

A call the reference refuses produces **one** frame. It is not an
announcement and it has no start frame — the call never ran, so there is
nothing to announce and nothing that started:

```json
{
  "stepIndex": 1,
  "state": "STATE_ERROR",
  "source": "SOURCE_SYSTEM",
  "target": "TARGET_USER",
  "errorMessage": "The model produced an invalid tool call.",
  "textDelta": "The model produced an invalid tool call. (\"model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly.\")",
  "text": "The model produced an invalid tool call. (\"model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly.\")",
  "error": {
    "errorMessage": "The model produced an invalid tool call. (\"model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly.\")",
    "httpCode": 0
  }
}
```

| field | value |
|---|---|
| `state` | `STATE_ERROR` |
| `source` | `SOURCE_SYSTEM` — the harness reports **its own** refusal, not the model's answer |
| `target` | `TARGET_USER` |
| `errorMessage` | `The model produced an invalid tool call.` |
| `text` / `textDelta` | `<sentence> ("<reason>")` |
| `error.errorMessage` | the same text |
| `error.httpCode` | `0` |
| the action | **none** — the call never ran |

The step index still advances: the refusal is a step of its own (1, 2, 3 …).

The **sentence** is not always the generic one. A refusal the reference
words of its own carries that sentence — an edit of a file that is not
there says:

```
Agent cannot edit files that do not exist. ("model output error: invalid tool call error (invalid_args) /ws/nope.txt does not exist in the current location. …")
```

### 2.2 What the model reads

The model reads a paragraph, not the tool's error:

```
Created At: 2026-10-26T04:51:13+08:00
Error invalid tool call: There was a problem parsing the tool call. 
Error Message: model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly. 
Guidance: You are trying to correct your previous tool call error, you must focus on fixing the failed tool call with sequential tool calls and try again. Do not do parallel tool calls and if you are fixing multiple tool calls, do them one at a time. Do not apologize. 
Retries remaining: 4.
```

Three things are worth pinning:

* there is **one** stamp line (`Created At:`) and no `Completed At:` — the
  call never ran, so there is no completion time (the same shape as a
  refused call, cycle 307 §3.7);
* every line but the last one ends with a **space**, the reason's line
  included;
* the reason is the reference's own sentence:
  `model output error: invalid tool call error (<kind>) <detail>`.

The three kinds are `invalid_args`, `invalid_signature` and `unknown_tool`.

### 2.3 The retry counter

The paragraph tells the model how many retries are left. The counter
starts at **four** and falls by one per refusal — 4, 3, 2, 1 — and a call
that **ran** starts it over:

```
view_file {}          → Retries remaining: 4.
view_file <ok>        → (the call works)
view_file {}          → Retries remaining: 4.
```

A call that ran and **failed** starts it over as well: the reference counts
the *refusals* in a row, and a call that was not refused breaks the row —
whether it worked or not.

```
view_file {}                    → Retries remaining: 4.
view_file <a file that is not there>  → (the step fails)
view_file {}                    → Retries remaining: 4.
```

### 2.4 The last retry

When the model has **one** retry left, the reference turns the function
calling **off** for the next request:

```json
"toolConfig": {"functionCallingConfig": {"mode": "NONE"}}
```

The tool declarations stay on the wire — only the mode changes. It is the
model's last chance: it has to answer with text instead of asking for
another call. The mode goes back to `AUTO` as soon as the counter is reset
(a call that ran).

The mode follows the counter, and the counter is the number of refusals in
a row:

| request | refusals so far | the model is told | mode |
|---|---|---|---|
| 1 | 0 | — | `AUTO` |
| 2 | 1 | 4 | `AUTO` |
| 3 | 2 | 3 | `AUTO` |
| 4 | 3 | 2 | `AUTO` |
| 5 | 4 | 1 | **`NONE`** |
| 6 | 4 + a call that ran | 1 | `AUTO` |

### 2.5 When the retries run out

The fifth refusal in a row ends the run. The reference writes the refusal's
frame and then the trajectory, and the trajectory carries the **reason**:

```json
{
  "trajectoryStateUpdate": {
    "trajectoryId": "cascade-conformance-000000000000000",
    "state": "STATE_FULLY_IDLE",
    "error": "model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly."
  }
}
```

There is **no** further model request: the model's fifth refusal is the
last thing the model sees — the run fails before the request that would
have carried it. The reference's own log says so:

```
errorreport.go:224] model output error: invalid tool call error (invalid_signature) …
executor.go:521] error in generator: model output error: invalid tool call error (invalid_signature) …
Agent run failed: executor run failed: model output error: invalid tool call error (invalid_signature) …
```

and the harness process then closes the connection. The **frames** are
byte-identical to the clone's; the **disconnect** is reproduced too — the
clone closes the connection with the same normal close (`1000 OK`), and
its process stays up, as the reference's does (measured: the reference's
process is still alive after the run).

Two things make the clone's close work, and both are worth writing down
because the obvious version of each is wrong:

* the read loop **cannot** be woken by cancelling its context — the
  websocket's read context closes the connection when it is cancelled, and
  the client sees a broken pipe where the reference shows a normal close.
  The close frame is written from the failure path instead, and the read
  loop ends when the peer answers it;
* the frame that carries the reason is written with a context of **its
  own**: the turn's context is cancelled by the halt that ends the run, so
  a write with it fails and the client never hears why the run failed.

A third one turned up while measuring this, and it is the same trap with a
different mask. `coder/websocket` closes the **whole connection** when the
context of a write **in flight** is cancelled — `setupWriteTimeout`
registers an `AfterFunc` that calls `c.close()`. The turn's context is
cancelled when the turn ends *and* when the run is halted, so a frame
written with it can be cancelled **on the wire**: the frame is truncated,
the connection is dropped (the client sees `no close frame received or
sent`), and the frame after it — the one that carries the reason — never
goes out.

It reproduced as an **intermittent** failure: the five-refusal probe passed
three times in a row and then failed with the connection gone, the
trajectory frame missing and the close turned into a broken pipe. The
clone's transport now refuses to pass the caller's context to the
websocket: `ServerConn.Send` drops a frame whose context is already
cancelled (the caller gave up) and writes every other frame with a
context of its own, bounded by a 30 s timeout so a client that stopped
reading cannot pin the harness forever.

With the fix, the five-refusal probe was run **five times in a row**:
**0 frame differences** every time, and the close line byte-identical to
the reference's:

```
ConnectionClosedOK: received 1000 (OK); then sent 1000 (OK)
```

With a real model the fifth refusal never happens: the request that
follows the fourth refusal carries `mode: NONE` (§2.4), so the model
answers with text and the run ends there. The failure is the corner case
— a model that produces an invalid call even when the function calling is
off.

---

## 3. The rules

The reference validates the call against its **runtime** schema, which is
**not** the schema it advertises. The advertised schema requires
`CommandLine`, `WaitMsBeforeAsync`, `DirectoryPath`, `Pattern`,
`Description` and a dozen more; the runtime schema requires far less, and
in a different order, with a different wording per tool.

The rules the captures pin:

| tool | # | kind | the reference's reason |
|---|---|---|---|
| `view_file` | 1 | `invalid_signature` | `AbsolutePath is a required parameter. Follow the function call schema exactly.` |
| | 2 | `invalid_args` | `<path> must be an absolute path: path is not absolute` |
| `list_dir` | 1 | `invalid_args` | `<path> must be an absolute path: path is not absolute` |
| `grep_search` | 1 | `invalid_signature` | `Query is required` |
| | 2 | `invalid_signature` | `SearchPath is required` |
| | 3 | `invalid_args` | `<path> must be an absolute path: path is not absolute` |
| `find_by_name` | 1 | `invalid_signature` | `SearchDirectory is required` |
| `write_to_file` | 1 | `invalid_args` | `argument TargetFile not found` |
| | 2 | `invalid_args` | `argument Overwrite not found` |
| | 3 | `invalid_signature` | `CodeContent is a required parameter. Please follow the function call schema exactly.` |
| | 4 | `invalid_args` | `<path> must be an absolute path: path is not absolute` |
| `replace_file_content` | 1 | `invalid_args` | `argument TargetFile not found` |
| | 2 | `invalid_signature` | `ReplacementChunk is required` |
| | 3 | `invalid_args` | `<path> must be an absolute path: path is not absolute` |
| | 4 | `invalid_args` | `<path> does not exist in the current location. Make sure the file path correct. In addition, the user may have moved, renamed, or deleted file.` |
| any other name | 1 | `unknown_tool` | ``unknown tool name: `<name>` `` |

Four details are easy to get wrong:

* **the wording is per tool.** `view_file` says "Follow the function call
  schema exactly.", `write_to_file` says "**Please** follow the function
  call schema exactly.", and the other three say "<field> is required" —
  a third shape.
* **the order is per tool.** The write tools check their **own** arguments
  first (`argument TargetFile not found`), then the schema's required
  fields, then the path: a `write_to_file` with a relative target and no
  `Overwrite` is refused for the missing `Overwrite`, not for the path.
* **a required field has to be filled.** `Query: ""` is still "Query is
  required"; `Overwrite: false` is an answer, not a missing argument.
* **`ReplacementChunk` is a wording, not a field name.** The field that
  satisfies `replace_file_content` is `TargetContent` **or**
  `ReplacementContent`; the reference's message names a third name.

Two tools have **no** path check of their own: a `find_by_name` with a
relative directory and a `run_command` with a relative `Cwd` both **run**,
and the step fails at runtime:

```
error executing cascade step: CORTEX_STEP_TYPE_FIND: rel must be an absolute path: path is not absolute
error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: rel: no such directory
```

---

## 4. What the clone does

```
internal/harnesstools/invalid.go   the kinds, the reason, the paragraph,
                                    the frame's sentence, and the rule table
internal/harnesstools/invalid_test.go  the rule table, the paragraph, the
                                    frame's text
cmd/harness/invalid.go              the advertised tool names, the verdict,
                                    the retry counter, the failure, the
                                    provider that turns the function calling
                                    off
cmd/harness/invalid_test.go         the frame's shape, the counter, the
                                    mode
cmd/harness/conversation.go         the verdict's fields, the guard that
                                    runs before the client's own, the
                                    evaluation that happens once per call,
                                    the run's failure, the session's end
cmd/harness/events.go               the announcement that is skipped, the
                                    start frame that is skipped, the
                                    refusal's frame, the trajectory that
                                    carries the failure
cmd/harness/writetool.go            the write tool's newline and its
                                    append (§6)
cmd/harness/writetool_test.go       the newline, the append, the other tools
internal/backend/gemini.go          the function calling mode of a request
internal/localharness/server.go     the connection's graceful close
```

The clone's guard runs **before** the client's own guards, because the
reference never announces a call it is going to refuse — and the client is
asked about a call only when it is announced.

The guards run **once per call**. PiG's hook can reach the call before the
announcement has finished — the two run in different goroutines — so the
evaluation is done once and the second caller waits for it. Without that,
the client's policy would be asked twice for one call, and the refusal
counter would fall twice for one refusal.

The refused call is handed to the runtime as a blocked call, so the model
reads the reference's paragraph through the hook's reason; PiG replaces a
thrown error's result with the error's message, and a blocked call keeps
the reason instead.

---

## 5. The differentials

| probe | the call | frames | request bodies |
|---|---|---|---|
| `i_two` | `view_file {}` | match | identical |
| `w_none` | `write_to_file {}` | match | identical |
| `w_target` | `write_to_file` without `Overwrite` | match | identical |
| `w_ov` | `write_to_file` without `CodeContent` | match | identical |
| `w_code` | `write_to_file` that runs | match | identical |
| `x_none` | `replace_file_content {}` | match | identical |
| `x_rel` | `replace_file_content` with a relative target | match | identical |
| `x_missing` | `replace_file_content` of a file that is not there | match | identical |
| `v_rel` | `view_file` with a relative path | match | identical |
| `l_rel` | `list_dir` with a relative path | match | identical |
| `g_none` | `grep_search {}` | match | identical |
| `g_q` | `grep_search` without a search path | match | identical |
| `f_none` | `find_by_name {}` | match | identical |
| `u_tool` | a tool that does not exist | match | **differ** (§7.1) |
| `i_reset` | a refusal, a call that works, a refusal | match | identical |
| `i_reset2` | a refusal, a call that runs and fails, a refusal | match | identical |
| `i_mode` | four refusals, then a call that works | match | identical |
| `i_five` | five refusals in a row | match | identical |
| `w_nl2` | a write of `x\n` | match | identical |
| `w_empty` | a write of an empty content | match | identical |
| `w_app2` | a write of `x`, then an append of `y` | **3** — the usage race (2) and the `endLine` (1) | identical |

The **usage race** is the reference's own: the token count of the response
that made the call lands either before or after the frame that reports the
refusal (cycle 307 §4.2). The clone takes the majority order. The
comparison counts a swapped pair as **two** differences, so a probe that
hits the race reports `2` (or `3` with one more difference) rather than
`0`.

The `i_five` probe also compares the **disconnect**: the client sees
`ConnectionClosedOK: received 1000 (OK); then sent 1000 (OK)` on both
sides — byte for byte the same line — and the reference's process is still
alive after the run, as the clone's is.

The frame that differs in `w_app2`, beyond the race, is the diff block's
**`endLine`**: the reference says `1`, the clone says `2`. The block's
lines and their actions are identical. The rule behind `endLine` is not
pinned — three samples give three different rules — so the difference is
recorded rather than fitted (§7.2).

---

## 6. The write tool

The write tool is a by-product of the differential: the probe that wrote a
file and looked at the diff found that the reference's file did not hold the
content the model gave it.

The reference's **write** tool does two things the clone's catalogue tool
does not:

* the file **ends with a newline** — the content it is given is written with
  one, and a content that already ends with one does not get a second;
* `Append: true` **appends** — the file's own content is left alone and the
  new content goes behind it. A file that is not there is created.

Measured against the reference binary:

| the call | the file the reference leaves |
|---|---|
| write `x` | `x\n` — two bytes |
| write `x\n` | `x\n` — two bytes, no second newline |
| write `x`, then append `y` | `x\ny\n` — four bytes |
| append `y` (the file is not there) | `y\n` — two bytes, the file is created |
| write an empty content | **refused** — `CodeContent is a required parameter` |

The reference's **edit** tool adds no newline: an edit of a file that ends
without one leaves it that way. The compensation is the write tool's alone.

The catalogue's write tool writes the content as it is and refuses a file
that exists, so the harness does the reference's part before the call reaches
it (`cmd/harness/writetool.go`). The catalogue is shared with the CLI, whose
own reference has a different write tool; the harness's reference is this
one.

With it the write's diff block matches the reference's byte for byte.

---

## 7. Deliberate divergence

### 7.1 The unknown tool's model-facing text

The **frames** of the unknown-tool case match the reference byte for byte.
The **text the model reads** does not: PiG's runtime looks the tool up
*before* the pre-tool hooks run, so a call to a tool that is not in the
catalogue is rejected by the runtime itself and the clone's hook never
sees it. The transcript therefore carries PiG's own

```
Tool no_such_tool not found
```

where the reference carries its paragraph.

Rewriting it would mean rewriting PiG's transcript at the provider
boundary — the runtime owns the transcript and offers no hook for it
(`ai.TranscriptContext` has no exported constructor from a message list).
The clone keeps the divergence and pins it in the differential.

The same class covers a call to a tool that is **not enabled**: the
clone's catalogue holds the enabled tools only, so a model that calls a
disabled side tool meets the same path.

### 7.2 The diff block's `endLine`

The clone computes **one** change region — a common prefix and suffix with
three lines of context — where the reference computes a real line diff with
possibly several hunks (cycle 307 §4). The block's *lines* and their
actions match the reference in every sample taken; the block's `endLine`
does not always.

Three samples, three shapes:

| the change | the old file | the reference | the clone |
|---|---|---|---|
| create (`x` → `x\n`) | empty | `start 0`, `end 0` | `start 0`, `end 0` |
| append (`y` → `x\ny\n`) | `x\n` | `start 0`, `end 1` | `start 0`, `end 2` |
| edit (`beta` → `BETA`) | `alpha\nbeta\ngamma\n` | `start 0`, `end 4` | `start 0`, `end 4` |

The reference's rule is not pinned by three samples — the exclusive end of
the old lines, the exclusive end of the changed old range and the old file's
line count each explain two of the three. The clone keeps its own rule
(the exclusive end of the old range the block covers, clipped to the file)
and the difference is recorded rather than fitted.

---

## 8. 硬约束 / 存疑 / limitations

**硬约束 (hard constraints).**

1. A call the reference refuses is **never announced** and has **no**
   start frame; it is reported in **one** frame.
2. That frame is `STATE_ERROR`, `SOURCE_SYSTEM`, `TARGET_USER`, and it
   carries **no** action.
3. Its `errorMessage` is `The model produced an invalid tool call.`; its
   text is `<sentence> ("<reason>")`; its `error.httpCode` is `0`.
4. A refusal the reference words of its own carries **that** sentence —
   an edit of a file that is not there says `Agent cannot edit files that
   do not exist.`
5. The model reads the reference's paragraph: one stamp line, the invalid
   branch, the reason, the guidance and the retries left. Every line but
   the last ends with a space.
6. The retries start at **four**, fall by one per refusal and are reset
   by a call that ran — whether it worked or failed.
7. When the model has **one** retry left, the next request carries
   `toolConfig.functionCallingConfig.mode: NONE`; the declarations stay.
   The mode goes back to `AUTO` when the counter is reset.
8. When the retries run out, the run **fails**: the trajectory carries
   the reason, and there is no further model request. The **connection is
   closed** with a normal close (`1000 OK`) — the process stays up.
9. The frame that carries the reason is written with a context of **its
   own**: the turn's context is cancelled by the halt that ends the run.
10. The runtime schema is **not** the advertised one; only the rules the
    captures pin may be implemented, and a call the reference runs must
    never be refused.
11. The **write** tool ends the file with a newline and **appends** when
    the call says so; the **edit** tool adds no newline.
12. The guards run **once per call**: PiG's hook can reach a call before
    the announcement has finished, so the evaluation is done once per call
    id and the second caller waits for it.
13. A frame must never be written with the **caller's** context: the
    websocket closes the whole connection when the context of a write in
    flight is cancelled, and the turn's context is cancelled when the turn
    ends and when the run is halted. A frame whose context is already
    cancelled is **dropped**; every other frame is written with a context
    of its own.

**存疑 (open questions).**

- The reference's runtime schema for the tools that were not probed. The
  rules table covers the six tools the captures pinned; `ask_question`,
  `send_message`, `search_web`, `read_url_content`, `define_subagent`
  and the rest may have rules of their own.
- Whether the reference's *other* refusals (a lifecycle hook's refusal, a
  budget's refusal) use the same frame — they use a different sentence
  (cycle 307 §3.7).
- Whether the reference validates a **batch** of parallel calls one by one
  or all at once, and what the frame order is then.
- What the reference does with the *fifth* refusal's paragraph — the run
  fails before the model can read it, so the wording of the last line
  (`Retries remaining: 0.`) was never observed.
- The rule behind the diff block's `endLine` (§7.2).
- Whether the reference's connection close after a failed run is a
  protocol rule or the process exiting.

**limitations.**

- **Only the arm64/macOS wheel was sampled.**
- **The rules were probed per tool, not per argument.** A rule that
  depends on an argument's *value* (not only on its presence) would not
  show up here.
- **The unknown-tool case's model-facing text was not reproduced** (§7.1).
- **The reference's tool frame count is racy.** The same edit wrote four
  active frames in four captures and three in a fifth (the result frame and
  the diff frame merged). The clone always writes the majority shape.


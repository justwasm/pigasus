# A failed tool call, in the reference's words

**Cycle:** `cycle-309-harness-tool-errors`
**Subject:** `out/localharness/PARITY.md` §4.7 recorded that the reference
reports a tool call that failed in its own words — the model reads a failed
*step*, and the frame carries the tool's own sentence — while the clone
passed the Go error through. This cycle captures the reference's wording
across the tools, makes the clone match, and records what is still unknown.
**Evidence:** 【一手】 the reference binary's own frames and request bodies,
captured live through the SDK's protocol · 【反编译】 decompiled output at
confirmed Mach-O addresses · 【strings】 string-table hits only · 【推测】
hypothesis

---

## 0. Summary

| | before | after |
|---|---|---|
| the model's text for a failed call | the Go error | the step's text: the two stamp lines and `Encountered error in step execution: <reason>` |
| the reason | the Go error | the reference's own wording, for the four tools whose wording the captures show |
| the frame's `errorMessage` | the whole result text | the tool's own sentence (`Cannot view file file:///… which does not exist.`) |
| a failed step's action | carried the empty output | carries **no** output: no names, no exit code, no command output |
| a command whose working directory is gone | ran in the workspace | **fails the step**, the way the reference fails it |

Differentials — seven successful calls (`v_ok`, `l_ok`, `g_ok`, `f_ok`,
`c_ok`, `w_ok`, `x_ok`) and five failures (`e_view`, `e_list`, `e_grep`,
`e_find`, `e_cwd`): **0 frame differences** and byte-identical request
bodies after normalisation, except for the reference's own racy
`usage_update` position (four of the twelve runs).

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

## 2. The failed step

A call that ran and failed is reported as a failed **step**. The number of
frames is the same as for a call that succeeded — announcement, start,
result, done — and only the done frame differs: its state is `STATE_ERROR`
and it carries a sentence of the tool's own.

### 2.1 What the model reads

```
Created At: 2026-10-26T04:52:19+08:00
Completed At: 2026-10-26T04:52:19+08:00
Encountered error in step execution: path file:///…/nope.txt does not exist
```

**Two** stamp lines — a *refused* call has only one, because it never ran
(cycle 307 §3.7).

### 2.2 What the frame carries

```
Cannot view file file:///…/nope.txt which does not exist.
```

The reference puts the **tool's own sentence** on the frame, not the whole
result text the model reads.

### 2.3 The wording is per tool

| tool | the step's reason | the frame's sentence |
|---|---|---|
| `view_file` | `path file:///… does not exist` | `Cannot view file file:///… which does not exist.` |
| `list_dir` | `directory … does not exist` (a **bare** path) | `Cannot list directory file:///… which does not exist.` |
| `grep_search` | `search path file:///… does not exist` | `Cannot search path file:///… which does not exist.` |
| `find_by_name` | `search directory … does not exist` (a **bare** path) | `Cannot search directory file:///… which does not exist.` |

The frame's sentence always prints the URI; two of the four print a bare
path in the step's reason.

The reference's *reason* comes from its own tools, and the clone runs the
CLI's — so the wording is the reference's and the reason inside it is the
clone's, unless the reference's wording for that failure is known. The
four tools above are the ones the captures pin; every other failure keeps
the Go error in the reference's shape.

### 2.4 A failed step has no output

* the `find_by_name` action leaves its `output` field out — a step that
  failed has no names to report (a successful one carries them);
* the `run_command` action carries neither `exitCode` nor
  `combinedOutput` — the command never ran.

### 2.5 The error text is not truncated

A 422-byte error survived a 30-byte truncation budget: the budget applies
to the tool's **output**, and a failed step's text reaches the model
whole.

---

## 3. The command whose working directory is gone

The reference runs the command in the directory the model named and
**fails the step** when there is none:

```
error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: /…/nodir: no such directory
```

The CLI's own runner does the opposite — it keeps the workspace when the
directory is gone, so that a stale value does not brick the call — so the
harness checks the directory before the call reaches the runner.

The model reads the reason through the step string, with the blank line
the step string puts between the stamp and the error:

```
Created At: 2026-10-26T05:11:11+08:00
Completed At: 2026-10-26T05:11:11+08:00

Encountered error in step execution: error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: /…/nodir: no such directory
```

and the frame carries the reason itself.

---

## 4. What the clone does

```
internal/harnesstools/failure.go   the wording table, the reason, the
                                   frame's sentence, the missing-directory
                                   error
internal/harnesstools/harnesstools.go  Outcome gains ErrorMessage and
                                   Failed; the step string's error branch
                                   for a command that could not run
cmd/harness/tools.go               the wrapper renders the failure and
                                   checks the working directory
cmd/harness/events.go              the frame's errorMessage, and the
                                   failed step's action without an output
```

The failed call is handed to the runtime as a result with `IsError` set
and the step's text as its content: a Go error would reach the model as
the error's own words, because PiG replaces a thrown error's result with
the error's message.

`internal/harnesstools/failure_test.go` pins the wording of the four
tools, the URI and bare-path forms, the fallback and the command's shape;
`cmd/harness/events_test.go` pins the frame's sentence and the failed
step's action.

---

## 5. The differentials

| probe | frames | request bodies |
|---|---|---|
| `v_ok` (a file that exists) | match (the usage race in one run) | identical |
| `l_ok` (a directory that exists) | match (the usage race in one run) | identical |
| `g_ok` (a search that matches) | match (the usage race in one run) | identical |
| `f_ok` (a name search that matches) | match | identical |
| `c_ok` (a command that runs) | match | identical |
| `w_ok` (a file that is written) | match | identical |
| `x_ok` (a file that is edited) | match | identical |
| `e_view` (a file that does not exist) | match (the usage race in one run) | identical |
| `e_list` (a directory that does not exist) | match | identical |
| `e_grep` (a search in a directory that does not exist) | match | identical |
| `e_find` (a name search in a directory that does not exist) | match | identical |
| `e_cwd` (a command whose working directory is gone) | match | identical |

The **usage race** is the reference's own: the token count of the response
that made the call lands either before or after the tool's start frame
(24 of 27 captures put it after; cycle 307 §4.2). The clone takes the
majority order, so a run that lands the other way shows two differing
frames — the usage frame and the frame it displaced.

---

## 6. Deliberate divergence

None beyond what §7 records.

---

## 7. 硬约束 / 存疑 / limitations

**硬约束 (hard constraints).**

1. A call that ran and failed is reported as a failed **step**: the model
   reads the two stamp lines and `Encountered error in step execution:
   <reason>`; the frame carries the tool's own sentence.
2. The wording is **per tool**: "path", "directory", "search path",
   "search directory".
3. The frame's sentence always prints the path as a `file://` URI; two of
   the four tools print a bare path in the step's reason.
4. A failed step's action carries **no** output — no names, no exit code,
   no command output.
5. A command whose working directory is gone **fails the step**; the
   reference does not fall back to the workspace.
6. The error text is **not** truncated.

**存疑 (open questions).**

- The wording for every *other* kind of failure. The reference has a
  sentence per tool **and** per kind (permission denied, the path is a
  directory, a malformed argument); only "the path does not exist" was
  measured. The clone keeps its own error in the reference's shape for the
  rest.
- Which failures the reference counts as a *step execution* failure — the
  `error executing cascade step: CORTEX_STEP_TYPE_…` prefix — and which as
  the tool's own error. Two were measured (a command with a missing
  working directory, a `list_dir` with an empty path); a missing file is
  the tool's own error.
- Whether the reference's step types have names beyond `RUN_COMMAND` and
  `LIST_DIRECTORY` in this dialect.

**limitations.**

- **Only the arm64/macOS wheel was sampled.**
- **The failure wording was probed for the "does not exist" kind only.**
- **The write tools' failures were not probed against the reference.** Most
  of them are caught by the reference's runtime validation before the tool
  runs (cycle 308 §5.2), which the clone does not have, so the write
  tools' failure shape has no reference capture.
- **The reference's runtime call validation is still not reproduced**, so a
  call the reference rejects still runs in the clone.

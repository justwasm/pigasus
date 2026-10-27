# The reference's background-task subsystem — deliverable

**Cycle:** `cycle-302-background-task-subsystem`
**Subject:** in the reference every backgrounded unit of work is a *task* — a
shell command that outlived its wait window, a schedule — with an id of the
form `<conversation-id>/task-<n>`, a log file under
`brain/<conversation-id>/.system_generated/tasks/`, a launch acknowledgement
and a completion notification. The clone now speaks that shape.
**Evidence:** 【一手】 the reference's own artifacts on this machine — the
`transcript.jsonl` of a live **agy 1.4.7** session on this repository
(`~/.gemini/antigravity-cli/brain/45fde9e1-76d5-4b87-bd23-1b2c626bbf26/`,
2026-10-25), the reference's own conversation store (SQLite, 500 files), the
reference's own task-log files, and the string table of **agy 1.4.7**
(`~/.local/bin/agy`, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`).

---

## 0. Summary

Cycle 298 recovered the reference's step strings and left the **task
subsystem** on the table with a note that it "is not a text swap". This
cycle ports the part of it the model can see.

| | before | after |
|---|---|---|
| **background id** | `cmd-<n>` (a local process counter) | `<conversation-id>/task-<n>` |
| **task log** | an in-memory buffer | a file at `brain/<conv>/.system_generated/tasks/task-<n>.log` |
| **`run_command` parameters** | `background: true` (a clone extension) | the reference's `WaitMsBeforeAsync` and `IsDaemon` |
| **a backgrounded command's result** | `Background command ID: cmd-1` (the template's *server-side* step string) | the reference's launch acknowledgement |
| **`manage_task`** | `list`, `kill` | `list`, `kill`, `status`, `send_input` |
| **completion notification** | a `<task-notification>` block the clone invented | the reference's `Task id "<id>" finished with result: …` |

The single most load-bearing finding: **the reference's `run_command` has a
wait window**, and the clone ignored it. The reference's own runs always
carry `"WaitMsBeforeAsync": "5000"` or `"10000"`; a command that outlives
the window is sent to the background as a task. That one parameter explains
the whole subsystem — the task ids, the task logs, the launch
acknowledgement, the completion notification — and it also retires the
clone's **VEOF deviation** (cycle 277), which existed only because a
blocking stdin read would otherwise wedge the turn.

## 1. What the reference does

### 1.1 The wait window

【一手】 the reference's own transcript, conversation `45fde9e1-…`:

```json
{"name": "run_command", "args": {"CommandLine": "\"go test -v ./internal/cliapp -run TestAccountsPanelIsInvocableButNotListed\"", "WaitMsBeforeAsync": "5000", "toolSummary": "\"Run TestAccountsPanelIsInvocableButNotListed\""}}
```

and the step that call produced (`step_index` 56, `status` RUNNING) is the
launch acknowledgement. The step index is the task number: step 4 →
`task-4`, step 56 → `task-56`, step 61 → `task-61`, step 100 → `task-100`,
step 114 → `task-114` — 147 of 147 task ids in the reference's transcripts
agree with their step index.

【strings】 the reference's own format strings carry the rest:

| offset | string |
|---|---|
| `0x35f8822` | `Tool is running as a background task with task id: %s` |
| `0x34f03ba` | `\nTask Description: %s` |
| `0x3574b5b` | `\nTask logs are available at: %s` |
| `0x3480ec0` | `\nLog output:\n%s` |
| `0x34058bb` | `%s/task-%d` |
| `0x3420f8f` | `task-%d.log` |
| `0x3491ccf` | `Task id %q %s%s` |
| `0x34a57a7` | ` with result:\n%s` |
| `0x33e9f4f` | `\nLog: %s` |

### 1.2 The launch acknowledgement

【一手】 the reference's own **conversation store** (the `schedule` tool's
function response):

```
Created At: 2026-10-25T03:43:15+08:00
Tool is running as a background task with task id: 0a9d1d33-7055-4467-9028-ca5a1b3e636c/task-4
Task logs are available at: file:///Users/gear/.gemini/antigravity-cli/brain/0a9d1d33-7055-4467-9028-ca5a1b3e636c/.system_generated/tasks/task-4.log
YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.
 DO NOTHING ELSE.
```

【一手】 the reference's own **transcript** (a backgrounded `run_command`)
carries the same text *plus* two extras:

```
Created At: 2026-10-25T03:15:13+08:00
Tool is running as a background task with task id: 45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56
Task Description: go test -v ./internal/cliapp -run TestAccountsPanelIsInvocableButNotListed
Task logs are available at: file:///…/tasks/task-56.log
YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: …
 DO NOTHING ELSE.
```

So there are **two shapes**, and the difference is exactly the
`Task Description:` line and the `Log output:` section:

| shape | what it is | clone |
|---|---|---|
| **function response** | what the model receives | ✅ implemented |
| **step content** | what the reference logs for the step | ❌ not implemented (the clone has no step-content renderer) |

The clone's `schedule` result was already the function-response shape and is
pinned byte-for-byte by `internal/tools/schedule_parity_test.go`; the
backgrounded `run_command` now renders **the same text** through the same
function, so the two cannot drift.

### 1.3 The completion notification

【一手】 the reference's own store, the function response of a backgrounded
`go test`:

```
Task id "45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56" finished with result:
Command exited with code 0.

Command output:
=== RUN   TestAccountsPanelIsInvocableButNotListed
--- PASS: TestAccountsPanelIsInvocableButNotListed (0.00s)
PASS
ok  	github.com/justwasm/pigasus/internal/cliapp	1.579s

Log: file:///Users/gear/.gemini/antigravity-cli/brain/45fde9e1-…/.system_generated/tasks/task-56.log
```

and the reference's own transcript carries it as a **system message**:

```
The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.

<SYSTEM_MESSAGE>
[Message] timestamp=2026-10-24T19:15:22Z sender=45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56 priority=MESSAGE_PRIORITY_HIGH content=Task id "45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56" finished with result:
…
</SYSTEM_MESSAGE>
```

The reference formats it `Task id %q %s%s` and appends `\nLog: %s`; the
status word is one of `finished`, `was canceled` or `failed`. The blank line
before `Log:` is the *result's* trailing newline, not a separator of its own
— the clone's renderer now matches that byte for byte.

### 1.4 The task log

【一手】 the reference's own files:

```
~/.gemini/antigravity-cli/brain/45fde9e1-…/.system_generated/tasks/task-56.log   175 bytes, CRLF
~/.gemini/antigravity-cli/brain/45fde9e1-…/.system_generated/tasks/task-114.log    0 bytes   (a `sleep 10` still running)
```

`task-56.log` holds exactly the command's raw output — no exit-reason line.
The file exists from the moment the task starts.

## 2. What the clone changed

| file | change |
|---|---|
| `internal/tools/background_task.go` | **new** — the task subsystem: `taskLaunchResult` (the launch acknowledgement, shared with `schedule`), `taskCompletionNotification` / `taskNotification`, `taskLogPath`, `createTaskLog`, `nextTaskID` |
| `internal/tools/command.go` | `run_command` honours `WaitMsBeforeAsync` and `IsDaemon`; a command that outlives the window becomes a task and the result is the launch acknowledgement |
| `internal/tools/support.go` | the task table is keyed by the reference-shaped id; every task carries a log path and a `finished` channel; the monitor writes the task log |
| `internal/tools/pty_unix.go`, `pty_windows.go` | `startWithPTYStdin` — `RunWithPTYStdin` split into *start* and *wait*, so a command can outlive the wait window; the VEOF is now a parameter |
| `internal/tools/tools_tasks.go` | `manage_task` implements the reference's four actions; `schedule` shares the launch renderer |
| `internal/agent/bg_finish.go` | `TaskNotificationText` — the reference's notification text |
| `internal/tools/testdata/reference_background_task_1.4.7.txt` | **new** — the reference's own texts, verbatim |
| `internal/tools/background_task_test.go` | **new** — the parity tests |

`internal/tools/background_result.go` is **gone**: it rendered the
reference's *server-side* step string (`Background command ID: …`) as the
model-facing result. That text does not occur once in the reference's 500
conversation stores, while the launch acknowledgement occurs in both the
store and the transcript — so it was never what the model saw.

## 3. The wait window, and the VEOF deviation it retires

Cycle 277 gave `run_command` a pty on stdin and then had to send a single
`VEOF` (^D) to end the input side, because the clone's `run_command` was
synchronous and a command that reads stdin would otherwise wedge the turn.
The reference has no such problem, and the reason is the parameter this
cycle implements: **the reference's pty belongs to a persistent terminal
whose input end stays open**, a command that reads stdin simply blocks, the
wait window elapses, and the command is handed to the background as a task.

So the reference-shaped path now

1. starts the command with the pty **and without the VEOF**;
2. waits up to `WaitMsBeforeAsync`;
3. if the command finished — the foreground result, unchanged;
4. if it is still running — it stays registered as a task, and the tool
   returns the launch acknowledgement.

The legacy foreground path (no `WaitMsBeforeAsync`) keeps the VEOF: it has
no wait window, so a blocking read would still wedge the turn.

Two consequences worth naming:

- A backgrounded command **outlives the turn** that started it. Its
  `exec.Cmd` is built on `context.Background()`, not on the tool call's
  context.
- A backgrounded command's stdin is the live pty master, so
  `send_command_input` (and `manage_task send_input`) can still write to
  it. The VEOF is *not* sent on that path — sending it would close the
  input the reference keeps open.

## 4. Hard constraints

- **The launch acknowledgement is one function.** `schedule` and a
  backgrounded `run_command` both call `taskLaunchResult`. A future change
  that edits one of the two texts must edit that function.
- **The task id is the table key.** `command_status`, `manage_task` and
  `send_command_input` address a task by the id the model was told
  (`<conversation-id>/task-<n>`). Changing the key shape breaks all three.
- **The task-number counter is shared with the schedule table**
  (`ScheduleTableFor(conv).nextTaskID()`), so a conversation has *one*
  task-number space and a schedule and a backgrounded command never mint
  the same id.
- **The VEOF is gone from the reference-shaped path.** Re-adding it would
  close the input the reference keeps open and would make
  `send_command_input` a no-op for a backgrounded command.
- **`background: true` stays.** It is the clone's own extension (the
  reference has no such parameter); it keeps the pipe-stdin path, which is
  what `send_command_input` was written against.

## 5. Open questions (存疑)

- **The task number is the reference's *step index*.** The clone has no step
  index at the tool layer, so it keeps a per-conversation counter: the ids
  are dense (`task-1`, `task-2`, …) where the reference's are sparse
  (`task-4`, `task-56`, `task-61`, …). The id *shape* is what the model and
  the reference's own tools address a task by, and that matches.
- **The completion notification is not delivered yet.** The seam
  (`internal/agent.BgFinishCoordinator`) has no production caller: the
  clone still does not tell the model that a background task finished. The
  *text* is now the reference's (`TaskNotificationText`), but wiring it into
  the turn loop — the reference delivers it as a system message with
  `sender = <task id>` and `priority = MESSAGE_PRIORITY_HIGH` — is
  outstanding.
- **The reference's command output is CRLF; the clone's is LF.** The
  reference's output passes through a pty, whose `onlcr` turns every `\n`
  into `\r\n`: 1,121 of the 1,265 foreground command results in the
  reference's own transcripts carry CRLF, and its task logs do too. The
  clone pipes stdout/stderr, so its output is LF. This is a pre-existing
  divergence (cycle 277 established the *pty on stdin* disposition), not
  something this cycle introduced — but it is a real byte-level gap.
- **The `manage_task status` result is the clone's own.** The reference
  advertises the action ("Check the task's current status and log file
  location") but no live capture of its output exists; the clone returns
  the task's status line.
- **The task log is written when the command exits.** The reference rewrites
  the file as the command progresses (`LogProgress`), so a task that is
  still running has an empty log in both — but a task that is running *and*
  producing output has a non-empty log in the reference and an empty one in
  the clone.
- **The `Step Id: %d` format string** (offset `0x343bdc3`) belongs to a
  shape this cycle did not observe; it is not the launch acknowledgement's.

## 6. Limitations

- The evidence is one live **agy 1.4.7** session on this machine
  (2026-10-25) plus the reference's 500 conversation stores. The IDE's
  `language_server` binary carries the same strings but was not run.
- The reference's *step content* shape (with `Task Description:` and
  `Log output:`) is captured but **not** implemented: the clone has no
  step-content renderer for any tool, so there would be nothing to render
  it into.
- The completion notification's **delivery** is out of scope here; only its
  text is ported.
- The pty's `onlcr` translation (the CRLF gap above) is a separate change
  with a wide blast radius — it touches every command result the clone
  produces, and the TUI's rendering of them.
- No live TUI session was driven for this cycle: the verification is unit
  tests plus the reference's own captured bytes.

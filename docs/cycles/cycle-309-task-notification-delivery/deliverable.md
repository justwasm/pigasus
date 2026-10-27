# The task notification's delivery — deliverable

**Cycle:** `cycle-309-task-notification-delivery`
**Subject:** a background task's completion notification reaching the model
— the `SYSTEM_MESSAGE` envelope, the session's inbox, and the turn the
reference's agent starts on its own.
**Evidence:** 【一手】 the reference's own captured notification (the
fixture the renderer is pinned against) and 【反编译】 the three format
strings the 1.4.7 binary holds for the envelope.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the completion notification** | the tools layer could produce one; nothing delivered it | it reaches the model, in the reference's own `SYSTEM_MESSAGE` envelope |
| **the envelope** | — | the reference's three literals, byte for byte, pinned by a test against the reference's own capture |
| **the queue** | — | `agent.TaskInbox` — session-level, so a notification that arrives between turns is not lost |
| **the turn** | — | the TUI starts a turn of its own when the agent is idle; a running turn is not interrupted |
| **the role** | — | the notification is a *system* message in the session, and reaches the model in the user-content position |
| **the trajectory** | — | the reference's own step type, `CORTEX_STEP_TYPE_SYSTEM_MESSAGE = 101` |

## 1. The reference's shape

### 1.1 The captured notification

The reference wrote its own completion notification into its own
transcript. The capture is committed as a fixture:

`internal/tools/testdata/reference_background_task_1.4.7.txt`, the
`---- completion notification (the SYSTEM_MESSAGE) ----` section.

```
The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.

<SYSTEM_MESSAGE>
[Message] timestamp=2026-10-24T19:15:22Z sender=45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56 priority=MESSAGE_PRIORITY_HIGH content=Task id "45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56" finished with result:
Command exited with code 0.

Command output:
=== RUN   TestAccountsPanelIsInvocableButNotListed
--- PASS: TestAccountsPanelIsInvocableButNotListed (0.00s)
PASS
ok  	github.com/justwasm/pigasus/internal/cliapp	1.579s

Log: file:///Users/gear/.gemini/antigravity-cli/brain/45fde9e1-76d5-4b87-bd23-1b2c626bbf26/.system_generated/tasks/task-56.log
</SYSTEM_MESSAGE>
```

Three things are visible in it:

- the **lead-in** — the reference tells the model that the message "was
  not actually sent by the user";
- the **tags** — `<SYSTEM_MESSAGE>` … `</SYSTEM_MESSAGE>`;
- the **message** — `[Message] timestamp=%s sender=%s priority=%s content=%s`.

The `content=` value is the task's own notification text (the clone's
`taskCompletionNotification`, byte for byte), and the `Log:` line names the
task log, which holds the command's whole output.

### 1.2 The format strings

【反编译】 The 1.4.7 binary holds the three literals the envelope is built
from, one hit each:

| literal | where |
|---|---|
| `The following is a ` | 0x3672794 (the lead-in's first half) |
| `<SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.\n\n<SYSTEM_MESSAGE>\n%s\n</SYSTEM_MESSAGE>` | 0x3672794 |
| `<SYSTEM_MESSAGE>\n%s\n</SYSTEM_MESSAGE>` | 0x3672810 (the bare wrapper, used where the lead-in is not) |
| `[Message] timestamp=%s sender=%s priority=%s content=%s` | 0x35ff419 |

The priority's spelling is the reference's own enum member name:
`MESSAGE_PRIORITY_HIGH` (the recovered .proto,
`out/local-macos/protos/cortex_1.4.6.proto`, `enum MessagePriority`).

### 1.3 Where the message travels

The lead-in says the message "was not actually sent by the user". That
sentence only makes sense where a *user* message would be read — a system
prompt would need no disclaimer. So the reference delivers the
notification in the **user-content position** of the model's conversation,
not in the system instruction.

The clone's transport agrees: `internal/piagentbridge/stream.go` maps a
PiG `SystemMessage` onto `StreamRequest.SystemPrompt`, and
`internal/backend/cloudcode_request.go` folds a `"system"` role into the
system instruction. A notification in that position would become part of
the system prompt — the wrong place. So the clone's session keeps the
notification under the role **`system`** (the reference's own step type),
and the turn's message builder turns it into a **user** message on the way
to the model.

## 2. What the clone does now

### 2.1 The tools layer fires it (`internal/tools`)

`bgTask.notifyCompletion` builds the notification when the command is gone:

```
Task id "<conv>/task-<n>" finished with result:
Command exited with code <n>.

Command output:
<the command's output>

Log: file://<the task log>
```

The result is the CLI's own formatter body — `commandResultBody`, factored
out of `renderResult` — **without** the `Created At:` / `Completed At:`
stamp the tool result carries. The reference's capture has no stamp.

The notification is *armed*, not fired, when the task becomes a task:
`notifyOnCompletion` waits on the task's `finished` channel. A command
that finished inside the wait window never became a task, so it owes no
notification — the foreground result is what the model reads.

A task that was stopped on purpose reports the reference's own status word,
`was canceled`, and carries no result. A non-zero exit is **not** a
failure here — the repo's own invariant is that a non-zero exit is a
nil-error result — so the task "finished" and the result carries the exit
code the model has to read.

### 2.2 The inbox holds it (`internal/agent/task_inbox.go`)

`agent.TaskInbox` is a session-level queue. It belongs to the session and
not to a turn: the TUI builds a fresh `agent.Loop` for every turn
(`internal/cliapp/tui_stream_prompt.go`), so a per-loop queue would drop
every notification that arrives *between* turns — which is exactly when a
background task finishes.

`agent.RenderTaskNotification` renders the reference's envelope;
`agent.TaskNotificationMessage` renders it into the message the session
carries.

### 2.3 The TUI delivers it (`internal/cliapp`)

| piece | where |
|---|---|
| the notifier | `runMode.installTaskNotifier` — the tools' `SetTaskNotifier` hook puts the notification in the inbox and sends a `taskNotificationMsg` to the program |
| the drain | `runMode.drainTaskNotifications` — the notification joins the session and is rendered in the transcript |
| the turn | `runMode.startNotificationTurn` — a turn of its own, started when the agent is idle |
| the role | `runMode.prepareStreamTurnMessages` — a `"system"` message becomes a user message, without the `<USER_REQUEST>` envelope the reference wraps around what the user typed |
| the trajectory | `runMode.trajectorySteps` — the step type is `convstore.StepSystemNotice` (101) |

A turn that is already in flight is **not** interrupted: the
notification waits, and the turn's end starts a turn of its own for it
(`flushNextQueued`).

## 3. The flow

```
1. the model starts a background command
2. the wait window closes with the command still running
   → the command becomes a task
   → the model gets the launch acknowledgement
   → the completion notification is armed
   → the "still running" timer is armed (cycle 308)
3. the command is gone
   → the task's log holds the command's output
   → the completion notification fires
   → the session's inbox takes it
   → the TUI is woken (taskNotificationMsg)
4. the agent is idle
   → a turn starts
   → the notification joins the session as a system message
   → the turn's messages carry it in the user-content position
   → the model reads it and carries on
```

## 4. 存疑 / open questions

- **【推测】** The reference's message *title* is a render detail, not part
  of the content: its own `AgentMessage` carries it in
  `render_details.message_title`, and the format string
  `[Message] timestamp=%s sender=%s priority=%s content=%s` has no slot
  for it. The clone therefore puts a titled notification's title in front
  of its body, so the model reads the reference's own wording. The
  captured completion notification has no title and is unaffected.
- **【推测】** The reference's separator before `Log:` is a CRLF pair in
  the capture (`…1.579s\r\n\r\nLog: …`) while the clone emits LF. The
  capture's own line endings come from the pty the reference ran the
  command under; the fixture's other test normalises CRLF to LF the same
  way, so the difference is not observable in the text.
- **【推测】** The reference's exact wire position of the notification is
  inferred, not observed: no captured request carries a `SYSTEM_MESSAGE`,
  so the "user-content position" is the reading of the lead-in, not a
  capture.
- **【推测】** The reference's status word for a task that exited non-zero
  is not observed. The clone reports `finished` and carries the exit code
  in the result, following the repo's own invariant that a non-zero exit
  is not a failure.

## 5. Limitations

- The **print path** (`--print`) has no delivery: it runs one turn and
  exits, so a background task that outlives the turn cannot reach it. The
  inbox lives in the TUI's run mode.
- A notification that arrives **during** a turn is delivered by the *next*
  turn, not injected into the running one. The reference's agent pump can
  read a task's message channel mid-turn; the clone's PiG-backed loop
  cannot take a message into a turn that is already running.
- The clone's notification is delivered through the tools' process-wide
  `SetTaskNotifier` hook, so the last run mode to install it owns the
  notifications. That is the TUI's case (one run mode per process), but a
  host with two sessions in one process would cross the streams.
- The reference's *other* notification in the same family — the output
  stabilisation one — is still not implemented (cycle 308's
  limitations).

## 6. The evidence

| artefact | what it is |
|---|---|
| `internal/tools/testdata/reference_background_task_1.4.7.txt` | the reference's own captured completion notification — the fixture the envelope is pinned against |
| `internal/agent/task_inbox_test.go` | the renderer reproduces the capture, byte for byte |
| `internal/tools/task_completion_test.go` | the notification fires when the task is done — and not for a foreground command |
| `internal/cliapp/tui_task_notify_test.go` | the delivery: the role, the position, the trajectory's step type |
| `out/local-macos/protos/cortex_1.4.6.proto` | `enum MessagePriority` (the priority's spelling) and `CORTEX_STEP_TYPE_SYSTEM_MESSAGE = 101` |

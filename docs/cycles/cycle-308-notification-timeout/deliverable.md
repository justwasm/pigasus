# The notification timeout — deliverable

**Cycle:** `cycle-308-notification-timeout`
**Subject:** `run_command`'s `NotificationTimeoutSeconds` — the parameter
the reference bolts onto its schema so the *system* tells the model that a
command is still running, instead of the model polling for it.
**Evidence:** 【反编译】 five functions decompiled from the live 1.4.7
binary, plus 【一手】 the reference's own descriptor (the proto field) and
the literals read out of the binary's rodata.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the parameter** | unknown to the clone | advertised with the reference's own name, type and 386-byte description |
| **the behaviour** | — | the model is told when the command is still running, N seconds after it went to the background |
| **the notification text** | — | the reference's own two literals, byte for byte |
| **the delivery** | — | a `SetTaskNotifier` hook in `internal/tools`; the turn loop installs it (cycle 309) |
| **the trajectory** | the field was dropped | the reference's field number (30) is written and read back |

## 1. The reference's shape

### 1.1 The schema field

`tools.(*RunCommandTool).ParameterSchema` (0x101de04d0) builds a JSON
schema with `invopop/jsonschema` and then calls
`withTimeoutNotificationField(schema)`. That helper (0x101de06c0) does
**not** reflect a field — it bolts one on:

```
utils.AugmentSchemaField(schema,
    "NotificationTimeoutSeconds",  26,      <- vm 0x10353caa0
    "integer",                      7,      <- vm 0x1033bc13b
    <the description>,           0x182)      <- vm 0x10368c1d2
```

The description, byte for byte (386 bytes):

> Optional. If the command runs for longer than this duration (in seconds)
> after the wait time (WaitMsBeforeAsync), the system will notify you that
> it is still running. Use this instead of setting a manual timer for
> checking status of commands. Highly recommended if the command is
> expected to terminate at all. Do not set this for daemon commands, which
> are expected to run indefinitely.

The helper is **conditional**: the field is added only when the tool
carries the notification capability (a flag byte on the tool and a bool
behind one of its pointers). The clone has no such gate, so its catalogue
always advertises the parameter.

### 1.2 The timer

`handlers.(*RunCommandHandler).runTaskListeners` (0x101dacfe0) is the
handler's listener loop — a `select` over six channels. One of its cases is
a timer:

```c
iVar17 = **(int **)(iVar16 + 0x60);          // the timeout in seconds
puVar26 = time_NewTimer(iVar17 * 1000000000);
```

and the case that fires on it does this:

```c
auVar28 = fmt_Sprintf(&UNK_1035e8bb6, 0x31, ...);   // the body
auVar28 = task.Send(ctx, ..., auVar28, &UNK_103556dd0, 0x1c, 3);  // + the title
// on failure: "failed to send timeout notification via task: %v"
```

So the reference sends the message **via the task** — into the task's own
message channel, which is what the agent's pump reads.

### 1.3 The two literals

| literal | bytes | vm |
|---|---|---|
| `Notification timeout reached` | 28 | 0x103556dd0 |
| `Background command is still running after %ds: %s` | 49 | 0x1035e8bb6 |
| `failed to send timeout notification via task: %v` | 48 | 0x1035e3d46 |

### 1.4 The proto field

【一手】 The reference's own descriptor
(`out/local-macos/protos/cortex_1.4.6.proto`, decoded verbatim from the
binary) has the parameter as a real field:

```proto
message CortexStepRunCommand {
  ...
  uint64 wait_ms_before_async = 12;
  uint64 notification_timeout_seconds = 30;
  ...
}
```

So the value the model sends becomes a field on the step the reference
records — the same step the clone writes into the implicit trajectory.

## 2. What this cycle changed

- `internal/tools/notification.go` — **new**. The notification seam
  (`TaskNotification`, `SetTaskNotifier`, `notifyTask`) and the timer
  (`armNotificationTimeout`), with the reference's two literals.
- `internal/tools/command.go` — the catalogue's `run_command` schema
  carries the parameter (the reference's name, type and description,
  verbatim); `runCommandParams` parses it; both background paths arm the
  timer.
- `internal/implicit/trajectory.go` — the trajectory writes and reads
  `notification_timeout_seconds` (field 30) when it is set.
- `internal/tools/notification_test.go` — **new**. Six tests.
- `internal/implicit/implicit_test.go` — the field's number and its
  round trip.

## 3. Why the wire declaration did not change

The reference's **default** declaration does not carry the parameter: the
field is added only when the tool has the notification capability, and the
request bodies captured on 2026-10-22 (with and without `--sandbox`) do
not have it. Cycle 306's finding stands — the live 1.4.7 session sends the
*older* description variants.

So `internal/tools/model_tools_1.4.6.json` — the fixture the clone sends
on the wire — is **untouched**. The parameter is advertised in the
*catalogue* (`internal/tools/catalog.go`), which is the clone's own
surface for custom agents, where the model needs a way to reach the
feature at all. A test pins both halves: the catalogue has the field, the
wire declaration does not.

## 4. How it works in the clone

1. The model calls `run_command` with `WaitMsBeforeAsync` and
   `NotificationTimeoutSeconds`.
2. The command starts; the wait window opens.
3. The command finishes inside the window → the foreground result, no
   timer.
4. The window closes with the command still running → the command becomes
   a task, the model gets the reference's launch acknowledgement, and the
   **notification timer** is armed for `NotificationTimeoutSeconds`.
5. The timer fires and the command is still running → the notification
   goes out: title `Notification timeout reached`, body
   `Background command is still running after 300s: <command>`.
6. The command finishes before the timer → no notification; the
   completion notification is the one it gets.

The delivery itself (the message reaching the model as a turn) is cycle
309's work; this cycle owns the parameter, the timer and the text.

## 5. 存疑 / open questions

- **【推测】** The reference's second `%s` — the command or the task id?
  The decompile reads a string out of the handler's frame and the
  reference's own wording does not say which. The clone puts the command
  line there: the model may have several commands running at once, and the
  task id is already in the launch acknowledgement.
- **【推测】** The reference's capability gate has no clone counterpart.
  The clone advertises the parameter in its catalogue unconditionally; a
  session with notifications disabled would still get it. The reference's
  gate is a flag byte plus a bool behind a pointer — which flag is which
  was not traced.
- **【推测】** The reference's `%d` is the *configured* timeout
  ("still running after 300s"), not the elapsed time. The decompile reads
  the value out of the step's proto field, which is the configured one.

## 6. Limitations

- The delivery into the turn loop is **not** wired yet: the hook exists,
  nothing installs it outside the tests. Until cycle 309 lands, a
  notification is produced and dropped.
- The reference's *other* notification in the same family — the output
  stabilisation one (`The command output has stabilized for %v. …`, title
  `Command output has stabilized`, plus the `Command may require input`
  variant) — is **not** implemented. Its literals were read out of the
  same two functions; it is a separate feature and stays out of this
  cycle.
- The description's 386 bytes are pinned by a test, but the *reference's*
  capability gate means a future reference build may add or drop the
  parameter; the catalogue would then be ahead of the wire.

## 7. The evidence

| artefact | what it is |
|---|---|
| `out/local-macos/dec/101de04d0_ParameterSchema.c` | the schema builder — the reflect + augment step |
| `out/local-macos/dec/101de06c0_withTimeoutNotificationField.c` | the field's name, type and description |
| `out/local-macos/dec/101dacfe0_runTaskListeners.c` | the timer, the notification, the send-via-task call |
| `out/local-macos/dec/101dada00_notifyOutputStabilized.c` | the sibling notification (output stabilisation) |
| `out/local-macos/dec/101de0f70_runCommand.c` | the wait-window timer |
| `out/local-macos/protos/cortex_1.4.6.proto` | the proto field (number 30) |

The five functions were added to `out/local-macos/priority-funcs.txt` and
decompiled with the guide's step ② (26 s against the existing project).

# The `/tasks` detail views — deliverable

**Cycle:** `cycle-315-tui-subagent-task-panels`
**Subject:** the reference's two `/tasks` detail models — the background task's
and the agent (subagent) task's — and the interaction that reaches them. The
clone's `/tasks` panel opened on Enter by *killing* the task and showing
nothing; the reference opens a detail view.
**Evidence:** 【反编译】 the two detail models' render functions, the shared
status function and the two keybinding-scope hint lists, all decompiled from
the reference `agy` 1.4.7 binary; 【strings】 every label the two views
render, read out of the binary's read-only data at the addresses the
decompilation points at.

---

## 0. Summary

| | before | after |
|---|---|---|
| **Enter on a `/tasks` row** | killed a running task, showed nothing | opens the row's detail view |
| **the kill key** | `enter` | `k` — the reference's `KeyTaskKill` |
| **the detail views** | none | two: the task detail and the subagent detail |
| **the state word** | `running` / `pending` / `done` / `failed` / `killed` | `running` / `waiting` / `completed` / `failed` / `cancelled` — the reference's `model.taskStatus()` (cycle 318 removed `waiting`: the function has four leaves) |
| **the status suffix** | ` (exit N)` on the completed / failed forms | unchanged — but it is now rendered by the same function the reference uses |

---

## 1. The reference's two detail models

【反编译】 The reference's TUI layer (`google3/third_party/jetski/cli/model`)
has **two** detail models, not one:

| model | file | what it shows |
|---|---|---|
| `model.TaskDetailModel` | `cli/model/task_detail.go` | a **background task** — a shell the model ran |
| `model.SubagentDetailModel` | `cli/model/subagent_detail.go` | an **agent (subagent) task** |

Their functions in the 1.4.7 binary:

| function | address |
|---|---|
| `model.TaskDetailModel.buildTaskDetailLines` | `0x102bbbaa0` |
| `model.TaskDetailModel.KeyboardHints` | `0x102bbb910` |
| `model.TaskDetailModel.View` | `0x102bbb3d0` |
| `model.TaskDetailModel.RenderHeader` | `0x102bbb850` |
| `model.SubagentDetailModel.buildHeaderLines` | `0x102bb3ed0` |
| `model.SubagentDetailModel.buildContentLines` | `0x102bb4710` |
| `model.SubagentDetailModel.renderHints` | `0x102bb12d0` |
| `model.SubagentDetailModel.NewSubagentDetailModel` | `0x102bb0880` |
| `model.taskStatus` | `0x102bbcd60` |
| `model.TasksModel.updateList` | `0x102bbd900` |
| `model.TasksModel.KeyboardHints` | `0x102bbd280` |

【反编译】 `model.TasksModel.updateList` is where the `/tasks` panel's keys are
dispatched. The `KeyEnter` branch copies the selected row into the model's
detail fields and returns a command that opens the detail view. The clone's
`tasksPanelKey` did the opposite: it killed the row.

### 1.1 The task detail's fields

【反编译】 `buildTaskDetailLines` emits, in this order:

```
Task Details
Command: <command>
Status:  <status>
Type:    <type>

Output
<the captured output, one line per row>     (or "(no output)")
Started: <2006-01-02 15:04:05>
```

Every label is the reference's own string:

| label | address |
|---|---|
| `Task Details` | `0x10343ae6f` |
| `Command: ` | `0x1033ebf2c` |
| `Status:  ` | `0x1033ebf35` |
| `Type:    ` | `0x1033ebf3e` |
| `Started: ` | `0x1033ebf47` |
| `Output` | `0x1033ab4df` |
| `(no output)` | `0x10341fe28` |
| `2006-01-02 15:04:05` | `0x1034cc572` |

Three details the decompilation settles:

- **The labels are a nine-cell column.** `Command: `, `Status:  ` and
  `Type:    ` are each nine characters wide — the value starts at the same
  column in all three lines.
- **`Type:` carries the task's *origin*, not its kind.** The value is the
  literal `Agent` (`0x10339d721`) or `User` (`0x103392689`), picked by a
  flag bit in the model. Every task the clone registers comes from a tool
  call, so the origin would be constant; the clone renders the task's
  **kind** (`shell`, `subagent`, …) there instead — the axis its rows
  actually distinguish. Documented as a divergence in §4.
- **The output block is split on newlines and each row is trimmed.** The
  empty form is `(no output)`.

### 1.2 The subagent detail's fields

【反编译】 `buildHeaderLines` and `buildContentLines` split the subagent view
into a header and a body:

```
Agent(<id>)
ID: [<id>]
...
Prompt
<prompt>                                   (or "(no prompt available)")
Tools
<the tools the subagent called>           (or "No tools used yet")
Answer
<the assistant's text>
```

| label | address |
|---|---|
| `Agent(%s)` | `0x1033ebfd7` |
| `ID: [%s]` | `0x103392681` |
| `Prompt` | `0x1033aa963` |
| `(no prompt available)` | `0x1034ef424` |
| `Tools` | `0x10339d492` |
| `No tools used yet` | `0x1034a866f` |
| `Answer` | `0x1033ab4d9` |
| `  (no subagent data)` | `0x1034de254` |
| `⚠ Error%s — %q` | `0x1033ebf1a` |

The reference's body also carries two sections the clone does not reproduce:

- `Trajectory — %s (%d steps)` (`0x103555f98`) with the empty form
  `No steps yet` (`0x10343ac6b`) — the subagent's step list. The clone's
  registry does not keep the step list.
- `User Backgrounded` (`0x1034a8680`) — a badge the reference shows when the
  *user* (not the model) started the subagent.

### 1.3 The status word

【反编译】 `model.taskStatus` (`0x102bbcd60`) returns one of:

| the clone's state | the reference's word | address |
|---|---|---|
| `StateRunning` | `running` | `0x1033bc1d5` |
| `StatePending` | `waiting` | (the reference's queue word — cycle 318: the reference has no such word; a task that has not started is a `running` one) |
| `StateDone` | `completed` | `0x1033eb2ba` |
| `StateFailed` | `failed` | `0x1033aa66f` |
| `StateKilled` | `cancelled` | `0x1033eac42` |

and appends ` (exit %d)` (`0x1034051bd`) to the **completed** and **failed**
forms only — a cancelled or a running task never shows one.

The clone's `/tasks` row used its own words (`done`, `killed`, `pending`).
The rows were **never driven against the reference** — the tasks-panel cycle
report says so explicitly (`docs/cycles/tasks-panel/deliverable-tasks.md`,
the `tasks-kill-running.raw.txt` row: *"ref not driven — no LLM to spawn bg
shells"*) — so nothing contradicted the change. `internal/taskregistry` now
has one `StateWord` / `TaskStatus` pair and both the row and the detail view
use it.

---

## 2. The keys

【反编译】 The reference's keybinding scopes, read out of the two
`KeyboardHints` functions' static tables:

| scope | hints |
|---|---|
| `tasks` (`model.TasksModel.KeyboardHints`) | `(KeyUp, KeyDown)`, `(KeyLeft, KeyRight)`, `(Scope "tasks", KeyEnter)`, `(Scope "tasks", KeyTaskKill)`, `(Scope "tasks", KeyTaskRemove)` |
| `task_detail` (`model.TaskDetailModel.KeyboardHints`) | `(KeyUp, KeyDown)`, `(KeyLeft, KeyRight)`, `(Scope "task_detail", KeyEscape)` |
| `subagent_detail` | `KeyToggleTrajectory`, `KeySubagentRespond`, `KeySubagentKill` (+ the approval flow's `KeyApprove` / `KeyReject` / `KeyApproveAll` / `KeyPreview` / `KeyOpenEditor`) |

The scope names live in the binary's read-only data at `0x10822da20`
(`subagent_detail`), `0x10822da60` (`task_detail`) and `0x10822da70`
(`tasks`); the action names at `0x10822da80` (`KeyTaskKill`) and
`0x10822da90` (`KeyTaskRemove`).

So the reference's `/tasks` panel binds **Enter** (open the detail),
**kill** and **remove** as separate actions — the clone had collapsed
kill into Enter, which is why the clone's Enter killed the task.

The clone's `internal/keybind` translates alt-keys only; it has no
scope/action model, so the detail views use the clone's own key handling
while the reference's scope and action names are recorded in the code
comments.

---

## 3. What the clone changed

| file | what it does |
|---|---|
| `internal/taskregistry/detail.go` | **new** — the two renderers (`TaskDetailBody`, `SubagentDetailHeader`, `SubagentDetailBody`) and the reference's label constants |
| `internal/taskregistry/format.go` | `StateWord`, `TaskStatus`, and `StateStyle` now speak the reference's vocabulary |
| `internal/taskregistry/taskregistry.go` | `Task` gained `Command`, `Output`, `Prompt` and `Tools` |
| `internal/taskregistry/bridge.go` | `AttachSubagent` fills `Command`; the terminal-state hook copies the shell's output |
| `internal/subagent/subagent.go` | `Output()` — a mutex-guarded read of the captured stdout |
| `internal/subagenttools/session.go` | the registry row carries the prompt; the run's answer and the tools it called are written back |
| `internal/cliapp/slash_tasks_detail.go` | **new** — the two panel kinds, the render, the scroll and the key handling |
| `internal/cliapp/slash_tasks_panel.go` | Enter opens the detail; `k` kills |
| `internal/cliapp/slash_panels.go` | the two new panel kinds; `taskDetailID` |
| `internal/cliapp/slash_panel_keymap.go`, `slash_panel_routing.go`, `tui_update_keys.go` | the routing |

Two fixes came along for the ride, both in the code the cycle touched:

- **A failed subagent no longer reads as `done`.** The registry update that
  runs on the way out of `subagenttools.Invoke` overwrote the failure state
  the error path had just recorded.
- **A shell task's output no longer races its own writer.** The bridge read
  the subagent's public `Result` field from the state-change hook; it now
  reads it through `Subagent.Output()`, which takes the subagent's mutex.

---

## 4. 硬约束 / the load-bearing rules

1. **Enter opens the detail; `k` kills; `x` removes.** The reference's
   `/tasks` scope binds three separate actions. Collapsing kill into Enter
   is what the cycle-315 report is about.
2. **The detail view is a sub-view, not a panel of its own.** Esc leaves it
   and returns to the `/tasks` list *without* the `Exited /tasks command`
   line; a second Esc leaves `/tasks` and prints it.
3. **The detail view is modal.** It owns every key while it is open — the
   global keys (`ctrl+g`, `ctrl+o`, …) must not reach the editor.
4. **One status function.** The row and the detail view render the same
   word; the exit code suffix belongs to the completed and failed forms
   only.
5. **The registry carries the payload.** `Task.Command`, `Task.Output`,
   `Task.Prompt` and `Task.Tools` are what the detail views render; the
   clone must not re-derive them from the panel layer.

---

## 5. 存疑 / open questions

- **【存疑】 The reference's Enter is gated on a per-row flag.** In
  `model.TasksModel.updateList` the Enter branch reads
  `if ((*pbVar1 & 1) == 0) { … }` on the selected flat-list item — and the
  *same* gate guards the kill and remove branches. The flag's meaning was
  not recovered: it may be "the row is a background task" or "the row is
  actionable". The clone opens a detail for **both** row kinds, because the
  brief asks for the subagent details to be reachable the same way.
- **【存疑】 The task detail's field order.** The decompilation emits
  `Started: ` *after* the output block, in the `else` branch of a condition
  that was not resolved. The clone follows that order.
- **【存疑】 `Type:`'s flag.** The reference's `Type:` value is `Agent` or
  `User`; the bit that picks it lives in the model and was not traced to the
  task record.
- **【存疑】 `waiting`.** `model.taskStatus` returns four words
  (`running`, `completed`, `failed`, `cancelled`). The clone's fifth state
  (`pending`) maps to `waiting`, which is a word in the binary but was not
  seen in `taskStatus`'s own returns. — **Resolved by cycle 318:** the
  function has exactly four leaves, so a task that has not started is a
  `running` one and the clone's `waiting` word was removed.

---

## 6. Limitations

- **The trajectory section is missing.** The reference's subagent detail
  lists the subagent's steps (`Trajectory — %s (%d steps)`); the clone's
  registry does not keep them, so the section is not rendered.
- **The approval flow is missing.** The reference's subagent detail can ask
  the user to approve or reject a tool call (`KeyApprove` / `KeyReject` /
  `KeyApproveAll` / `KeyPreview` / `KeyOpenEditor`) and can answer a
  question the subagent asked (`KeySubagentRespond`,
  `EnterJumpToWaiting`). The clone's subagents do not ask.
- **The `/tasks` row is still the clone's own shape.** The reference's row
  is built from a different set of parts — it carries a `[15:04:05]`
  timestamp and a status word with the exit code in it. Aligning the row's
  *layout* is the next step; this cycle aligned its **vocabulary** and gave
  it a detail view.
- **The status-row suffix is unchanged.** The reference's 1.4.7 binary
  carries the format `%d task(s) · /tasks`; the clone renders ` · N tasks`.
  The clone's form is capture-backed (cycle 205) and the reference's format
  string was not tied to a call site, so it was left alone.
- **The reference's detail view is a full-screen model.** The clone's is a
  modal panel over the conversation, like every other panel the clone
  implements.
- **A running shell's output is empty.** The clone captures a shell's
  stdout when the process exits, so a task that is still running renders
  `(no output)` until it finishes.

---

## 7. The files

| file | what it is |
|---|---|
| `internal/taskregistry/detail.go` | the two renderers and the reference's labels |
| `internal/taskregistry/format.go` | the reference's state vocabulary |
| `internal/cliapp/slash_tasks_detail.go` | the two panel kinds, the scroll, the keys |
| `internal/cliapp/slash_tasks_detail_test.go` | the views' text, the scroll, Esc → back to the list |
| `docs/cycles/cycle-315-tui-subagent-task-panels/deliverable.md` | this report |

### The binary

Reference `agy` 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.

### The extraction

The decompilations were produced with the repo's own script, run from a
neutral working directory:

```
cd /tmp && MAXMEM=8G VMARGS="-XX:ParallelGCThreads=4 -XX:CICompilerCount=4" \
  "$GHIDRA_INSTALL_DIR/support/analyzeHeadless" \
  /Users/gear/Documents/GitHub/openagy/out/local-macos/proj agy \
  -process -noanalysis -max-cpu 4 \
  -scriptPath /tmp/ghscripts -postScript DecompileAt.java <address…>
```

The string addresses were read out of the binary directly (a scan of the
read-only data for each label, mapped back to a virtual address through the
Mach-O segment table) — not taken from the decompilation's own annotations.

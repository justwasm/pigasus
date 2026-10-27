# The `/tasks` row's status cell — deliverable

**Cycle:** `cycle-318-tui-tasks-row-status`
**Subject:** the `/tasks` row's *status cell* — the state vocabulary it
uses and the exit code it carries. Cycle 315 gave the row the
reference's five state words (one of them a guess) and cycle 316 gave
the two detail views their own shape; this cycle fixes the cell the row
and the detail view share.
**Evidence:** 【反编译】 `model.taskStatus` (0x102bbcd60) and
`model.TasksModel.View` (0x102bbe370) decompiled from the reference
`agy` 1.4.7 binary; 【strings】 every literal the two functions render,
read out of the binary's read-only data at the addresses the
decompilation points at.
**Superseded in part:** cycle 319
(`docs/cycles/cycle-319-tui-tasks-row-layout/`) paired the six
arguments of the row format that this cycle could not pair, and
shipped the row.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the state vocabulary** | five words — `running` / `waiting` / `completed` / `failed` / `cancelled` | four words — `running` / `completed` / `failed` / `cancelled`; a registered-but-not-started task is a `running` one |
| **the row's status cell** | the bare word, padded | the word **plus the exit code**: `completed (exit 0)`, `failed (exit 1)` |
| **the detail view's status** | the same bare word | unchanged — it already carried the exit code |

---

## 1. The vocabulary has four words, not five

【反编译】 `model.taskStatus` (0x102bbcd60) is a four-leaf cascade, and
each leaf names one literal:

| leaf | literal | address | references |
|---|---|---|---|
| running | `running` | 0x1033bc1d5 | 0x102bbce28 (taskStatus), 0x102bbea34 (View) |
| cancelled | `cancelled` | 0x1033eac42 | 0x102bbce54 (taskStatus), 0x102bbea60 (View) |
| completed | `completed` | 0x1033eb2ba | 0x102bbcfd4 (taskStatus), 0x102bbebe0 (View) |
| failed | `failed` | 0x1033aa66f | 0x102bbcf14 (taskStatus), 0x102bbeb20 (View) |

There is no fifth leaf and no `waiting` literal anywhere in the
function — the whole function is 4 KB of stack copies and four string
constants. Cycle 315 marked the clone's fifth word (`waiting`, for a
task that is registered but not started yet) as 【存疑】; this cycle
resolves it: **the reference has no separate word for a task that has
not started** — such a task is a *running* one.

The same four leaves are in the panel's own row (`model.TasksModel.View`
references all four), so the row and the detail view agree.

The clone's `StateWord` and `StateStyle` now map `StatePending` to
`running`, exactly like `StateRunning`.

---

## 2. The status cell carries the exit code

【反编译】 the two leaves that end a task append the exit code to the
word before the cell is styled:

```c
if ((*(byte *)((int)register0x00000008 + 0xe1c8) & 1) == 0) {
  uVar4 = runtime_convT64(*(undefined8 *)((int)register0x00000008 + -0x68));
  auVar12 = fmt_Sprintf(&UNK_1034051bd, 10, …, 1, 1);   /* " (exit %d)" */
  auVar12 = runtime_concatstring2(0, &UNK_1033eb2ba, 9, …);  /* "completed" */
}
```

The suffix literal is ` (exit %d)` (0x1034051bd, ten bytes) and it is
referenced twice inside `model.taskStatus` (0x102bbcee8, 0x102bbcfa8)
and twice inside `model.TasksModel.View` (0x102bbeaf4, 0x102bbebb4) —
once per ending leaf.

So the cell is:

| state | the cell |
|---|---|
| running | `running` |
| cancelled | `cancelled` — a killed task never shows an exit code |
| completed | `completed (exit 0)` |
| failed | `failed (exit 1)` |

The clone's row rendered the bare word (`completed`) and left the exit
code to the detail view. The row now renders the same cell the
reference builds — `internal/taskregistry.RowStatus` — and the detail
view keeps using `TaskStatus`.

---

## 3. The rest of the reference's row, mapped

The row's *assembly* is still not recovered, but every piece of it is
now located, so the next cycle can pair them. 【反编译】
`model.TasksModel.View` (0x102bbe370):

| piece | how the reference builds it | the literals |
|---|---|---|
| **the status marker** | `render.StatusMarker()` (0x10289fe70) — the first three bytes of 0x10338b3f9 (`●◉□⛁`), so `●`, in one of four state colours | 0x10338b3f9 |
| **the status cell** | the word (+ the exit suffix), styled with the state's colour | see §1 |
| **the name** | `render.TruncateAnsi` (0x1028a00a0) to `max(10, available - 17)` cells | — |
| **the timestamp** | `[%s]` over the time formatted as `15:04:05`; `n/a` when there is no time; ten spaces when the time is the zero value | 0x103392685, 0x1033d4327, 0x10338b447, 0x1034051c7 |
| **the padding** | `strings.Repeat(" ", max(2, available - used - nameWidth - 15))` — the name is padded so the timestamp sits at the right | 0x103d91f08 (`" "`) |
| **the row** | `fmt.Sprintf("%s%s %s %s%s%s", …)` | 0x10346a9b8 (0x102bbf210) |

The six arguments of that format could not be paired: Ghidra's argument
slots for the `[]any` the `Sprintf` takes are shifted, and the six
values it does resolve (three of them the model's own string fields,
one the styled cell, two the model's fields again) do not map onto the
pieces above one to one. **No row may be shipped from this.**

### The other row: the ActiveItems panel's

The panel at the bottom of the TUI is drawn by the `cli/render` package,
and its task row is `render.RenderTaskItem` (0x1028a0d60). It is the
same machinery as the `/tasks` row, and its assembly is *readable*:

```c
auVar13 = fmt_Sprintf(&UNK_1033aa603, 5, …, 2, 2);      /* "%s %s" */
auVar13 = runtime_concatstring2(0, &UNK_103d91f08, 1, …, …);  /* " " + it */
auVar13 = render_TruncateAnsi(…);                        /* the name */
auVar13 = lipgloss_Style_Render(…);
runtime_concatstring3(0, …, …, auVar13._0_8_, auVar13._8_8_, …, …);
```

so the row is

```
" " + <A + " " + B> + " " + <the truncated, styled name> + <Y>
```

and its pieces are the same as the `/tasks` row's: the marker
(`render.StatusMarker`), the bracketed timestamp — here `"[%s] "`
(0x10339d73f, five bytes, **with** a trailing space; the `/tasks` row
uses `"[%s]"` without one), the status cell, the name and the padding.
Which of `A`, `B` and `Y` is the marker, the timestamp and the status
was not resolved.

The clone's ActiveItems row — `● [15:04:05] <ID> <Name> <elapsed>` in
`internal/cliapp/tui_active_items.go` — was written from the binary's
*string table*, not from the decompilation, so its id and its elapsed
time are the clone's own.

---

## 4. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/format.go` | `statusParts` — the word and the exit suffix, split; `TaskStatus` uses it; **`RowStatus`** — the row's cell; `StateWord`/`StateStyle` map `StatePending` to `running`; `RowLine` renders `RowStatus` |
| `internal/taskregistry/format_test.go` | the `waiting` assertion becomes `running`; new `TestRowStatus`; the row test asserts the exit code |

The row before and after (a completed task with exit code 0):

```
  ⎿  completed         shell-1  npm test  9s
  ⎿  completed (exit 0)  shell-1  npm test  9s
```

---

## 5. 硬约束 / the load-bearing rules

- **Four words.** `running`, `completed`, `failed`, `cancelled` — a
  task that has not started is a `running` one.
- **The exit code belongs to the cell**, not to a separate field: the
  reference concatenates ` (exit %d)` onto `completed` / `failed` and
  styles the *whole* cell.
- **A cancelled task never shows an exit code**, and neither does a
  running one.
- **The row and the detail view use the same cell.**

---

## 6. 存疑 / open questions

- **【存疑】 The row's layout.** The order of the six `%s` arguments is
  unknown (§3). The clone's row order — marker, status, id, name,
  elapsed — is the clone's own.
- **【存疑】 The alignment.** The reference pads the *name* so the
  timestamp lands at the right; the clone pads the *status word* to nine
  cells and puts the elapsed time last. A cell with an exit code is
  wider than the plain words, so the clone's columns shift for those
  rows — the reference's do too, but in the other direction.
- **【存疑】 The marker's colours.** `render.StatusMarker` picks one of
  four lipgloss styles by the task's state; the styles themselves live
  in `render.init` and were not decoded.

---

## 7. Limitations

- **The row's shape is still the clone's.** Only the status cell is
  the reference's now. The marker, the timestamp, the truncation and
  the right-aligned padding are documented above, not implemented.
- **The clone's `ExitCode` is often unset.** `Registry.Register`
  turns a caller's `0` into `ExitCodeUnset` (-1), so a task that
  finished successfully shows the bare word unless the caller set the
  exit code explicitly. The reference always knows the exit code of a
  finished task.
- **The `/tasks` panel's `Ctrl-C`-style kill path and the subagent
  detail's approval flow are still missing** (cycle 317's notes).

---

## 8. The files

### The binary

`~/.local/bin/agy` — Antigravity CLI 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.

### The extraction

`analyzeHeadless` with `DecompileAt.java`, run from `/tmp`:

```bash
cd /tmp && GHIDRA_INSTALL_DIR=~/tools/ghidra_12.1.4_PUBLIC \
  JAVA_HOME=/opt/homebrew/opt/openjdk@21 MAXMEM=8G \
  ~/tools/ghidra_12.1.4_PUBLIC/support/analyzeHeadless \
  out/local-macos/proj agy -process -noanalysis -max-cpu 4 \
  -scriptPath /tmp/ghscripts -postScript DecompileAt.java \
  0x102bbcd60 0x102bbe370 0x10289fe70
```

### The functions this cycle is based on

| address | function |
|---|---|
| 0x102bbcd60 | `model.taskStatus` |
| 0x102bbe370 | `model.TasksModel.View` (the row) |
| 0x10289fe70 | `render.StatusMarker` |
| 0x1028a00a0 | `render.TruncateAnsi` |

### The literals this cycle is based on

| address | literal | length |
|---|---|---|
| 0x1033bc1d5 | `running` | 7 |
| 0x1033eac42 | `cancelled` | 9 |
| 0x1033eb2ba | `completed` | 9 |
| 0x1033aa66f | `failed` | 6 |
| 0x1034051bd | ` (exit %d)` | 10 |
| 0x103392685 | `[%s]` | 4 |
| 0x1033d4327 | `15:04:05` | 8 |
| 0x10338b447 | `n/a` | 3 |
| 0x1034051c7 | ten spaces | 10 |
| 0x10346a9b8 | `%s%s %s %s%s%s` | 14 |
| 0x10338b3f9 | `●◉□⛁` — the marker takes the first three bytes | 12 |

The references were found with `devutils/macho_strrefs.py`:

```bash
python3 devutils/macho_strrefs.py 0x1033bc1d5 0x1033eac42 0x1033eb2ba 0x1033aa66f
```

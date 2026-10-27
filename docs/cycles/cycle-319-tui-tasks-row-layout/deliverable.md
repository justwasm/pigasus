# The `/tasks` row's layout — deliverable

**Cycle:** `cycle-319-tui-tasks-row-layout`
**Subject:** the `/tasks` row's *shape* — the six arguments of the
reference's row format, in order, and the arithmetic that sizes the
name and the padding. Cycle 318 recovered the row's status cell and
left the row's assembly unpaired ("the six arguments of that format
could not be paired"); this cycle pairs them, reads them out of the
instructions rather than the decompilation.
**Evidence:** 【反编译】 the disassembly of `model.TasksModel.View`
(0x102bbe370–0x102bbf540) in the reference `agy` 1.4.7 binary, read
instruction by instruction; 【反编译】 `render.TruncateAnsi`
(0x1028a00a0), `render.TruncateAnsiLine` (0x1028a0110),
`render.FlattenToLine` (0x1028a0050) and `render.StatusMarker`
(0x10289fe70) decompiled; 【strings】 every literal, read out of the
binary's read-only data at the addresses the code points at.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the row's shape** | the clone's own — `⎿ ⠿ <state> <id> <name> <elapsed>` | the reference's — `<prefix><marker> [<time>] <name><padding><status>` |
| **the prefix** | the `⎿` bullet | `> ` on the cursor's row, `  ` on every other |
| **the marker** | the clone's `⠿` | the reference's `●`, in the state's colour |
| **the time** | none | `[15:04:05]` — the task's start time, in brackets |
| **the name** | the raw name, cut at 60 cells | cut to `max(10, width − len(status) − 17)` cells, with `...` |
| **the padding** | none | `max(2, width − len(status) − width(name) − 15)` spaces — the status cell lands on the right edge |
| **the id and the elapsed time** | in the row | gone — the reference's row has neither |
| **the failed reason** | `Error: …` in the row | gone — the reference's row has no such cell |

The row the clone renders now (a 220-cell panel, two tasks):

```
> ● [15:29:40] echo done                                     completed
  ● [15:29:51] ddgr --num=5 cats                             running
```

---

## 1. The row is one `Sprintf`, and its six arguments are now paired

【反编译】 the row is built by a single call:

```c
/* 0x102bbf22c */
auVar = fmt_Sprintf(&UNK_10346a9b8, 14, …, 6, 6);   /* "%s%s %s %s%s%s" */
```

The `[]any` lives on the stack at `sp+0x38<<12+0x980` — six 16-byte
slots — and each slot is filled by a `runtime.convTstring` whose two
registers hold a string's pointer and its length. Tracing those two
registers back to the slot they were loaded from says which argument of
the format came from where:

| # | the argument | where it comes from | the slot it is loaded from |
|---|---|---|---|
| 0 | the prefix | `"> "` (0x103389576) or `"  "` (0x103389194) | 0x4500 / 0x4408 |
| 1 | the marker | `render.StatusMarker` (0x102bbe9f4) | 0x4520 / 0x4428 |
| 2 | the time cell | `fmt.Sprintf("[%s]", …)` (0x102bbecf8), then styled | 0x44f8 / 0x4400 |
| 3 | the name | `render.TruncateAnsi` (0x102bbed54), then styled | sp+0x38<<12+0xa00 / 0x4468 |
| 4 | the padding | `strings.Repeat(" ", …)` (0x102bbeda0) | 0x4518 / 0x4418 |
| 5 | the status cell | the word (+ the exit suffix), then styled | 0x4508 / 0x4410 |

The format's own two spaces sit between the marker and the time cell,
and between the time cell and the name. So the row reads

```
<prefix><marker> [<time>] <name><padding><status>
```

**The decompiler's slot model is what made this hard.** Ghidra shifts
the `[]any` slots per slot, so its pairing of the six arguments with
the six values was wrong (cycle 318). The instructions do not shift:
each `convTstring` is preceded by the two `ldr`s that fill its
registers.

---

## 2. The prefix — `"> "` on the cursor's row, `"  "` on the others

【反编译】 the prefix is chosen by one comparison — the record's index
against the model's selected index:

```asm
0x102bbec20: ldr  x6,[x27, #0xac0]     ; the record's index
0x102bbec28: ldr  x7,[x27, #0x4440]    ; the selected index
0x102bbec2c: cmp  x7,x6
0x102bbec30: adrp x6,0x103389000
0x102bbec34: add  x6,x6,#0x576         ; "> "
0x102bbec38: adrp x8,0x103389000
0x102bbec3c: add  x8,x8,#0x194         ; "  "
0x102bbec40: csel x6,x6,x8,eq
```

Both literals are two bytes long, so the prefix is always two cells
wide — the row's columns never shift. The selected row's `"> "` is
*styled* (a `lipgloss.Style.Render` at 0x102bbee34 with the style
copied from `sp+0x1e<<12+0xdb0`); the idle row's two spaces are
passed through unstyled.

The clone's `⎿` bullet — the shape it inherited from the transcript's
tool-step rows — is gone: the reference's prefix says *selected*, not
*tool call*.

---

## 3. The marker — `●`, in the state's colour

【反编译】 `render.StatusMarker` (0x10289fe70) returns **three bytes at
0x10338b3f9** — the first three bytes of the run `●◉□⛁`, i.e. the
filled circle `●` — in all four of its branches; what differs between
the branches is the *style*, one per state. The row's marker is
therefore the same glyph for every state, coloured by the state.

The clone's `⠿` (a Braille spinner) is gone.

---

## 4. The time cell — `[15:04:05]`, `[n/a]` or a blank column

【反编译】 the record's time is read as a `time.Time` (the wall and ext
words at `sp+0x1c<<12+0x4c8`, the location at 0x44d8) and formatted:

```asm
0x102bbec58: cbnz x8,0x102bbec70   ; both words zero → the blank form
0x102bbec60: mov  x8,#0xa           ; ten spaces
0x102bbec68: add  x9,x9,#0x1c7      ; 0x1034051c7
…
0x102bbec70: cbnz x8,0x102bbec88   ; exactly one word zero → "n/a"
0x102bbec78: orr  x8,xzr,#0x3
0x102bbec80: add  x9,x9,#0x447      ; 0x10338b447
…
0x102beca4: bl   time.Time.Format  ; "15:04:05" (0x1033d4327)
…
0x102bbecf8: bl   fmt.Sprintf       ; "[%s]" (0x103392685)
```

So the cell is `[15:04:05]` normally, `[n/a]` for a half-set time and
`[          ]` (ten spaces, so the column keeps its width) for the zero
time. The `"n/a"` branch is a defensive path: a Go `time.Time` is never
half-zero in practice.

The record carries **one** time per row. The clone shows the task's
*start* time — which time the reference's field holds is still 存疑
(§10).

---

## 5. The name — the truncation and its ellipsis

【反编译】 the name is cut with `render.TruncateAnsi` (0x1028a00a0) to

```
max(10, available − len(status) − 17)
```

where `available` is the model's width field (0x43e0, loaded from
`[sp+0x38<<12+0xab0]+0x50` at 0x102bbe54c) and `len(status)` is the
byte length of the status cell (0x4458). The instruction sequence is

```asm
0x102bbed30: ldr  x5,[x27, #0x43e0]   ; available
0x102bbed34: sub  x4,x5,x4            ; − len(status)
0x102bbed38: sub  x4,x4,#0x11         ; − 17
0x102bbed3c: cmp  x4,#0xa
0x102bbed44: csel x4,x5,x4,lt         ; max(10, …)
```

The 17 is the row's fixed cells (15 — see §6) plus the smallest padding
(2), so a name that is cut still leaves the status cell its room.

`render.TruncateAnsi` itself is a two-step: it flattens the string
onto one line (`render.FlattenToLine`, 0x1028a0050, which is
`strings.Join(strings.Fields(s), " ")`) and then cuts it
(`render.TruncateAnsiLine`, 0x1028a0110):

```c
if (maxWidth < 1) return "";
if (lipgloss.Width(s) <= maxWidth) return s;
if (maxWidth < 4) return ".";                        /* 0x103d91dd8 */
return truncate_BytesWithTail(s, maxWidth-3, "") + "...";   /* 0x10338add5 */
```

**The ellipsis is three ASCII dots, not `…`** — the literal at
0x10338add5 is `...`, and the one-character form at 0x103d91dd8 is a
single `.`.

---

## 6. The padding — the status cell lands on the right edge

【反编译】 the gap between the name and the status cell is a cell of its
own, `strings.Repeat(" ", …)` with

```
max(2, available − len(status) − width(name) − 15)
```

where `width(name)` is `lipgloss.Width` of the *cut* name (0x1026610c0
at 0x102bbed68) and the 15 is the width of the cells the row always
carries:

```
the prefix (2) + the marker (1) + the space (1) + the time cell (10) + the space (1) = 15
```

So the row comes out exactly `available` cells wide and the status cell
ends on the panel's right edge — the reference right-aligns the status,
it does not pad the word.

The clone's own alignment (padding the *word* to nine cells, and
putting the elapsed time last) was an invention; it is gone.

---

## 7. The status cell — the word, and the exit code

【反编译】 the same four leaves as cycle 318, and the same exit-code
suffix — the row's status cell is `TaskStatus`'s string:

```asm
0x102bbea24: ldrb w4,[x27, #0x498]    ; running?
0x102bbea34: add  x5,x5,#0x1d5        ; "running"    (0x1033bc1d5)
0x102bbea40: ldrb w5,[x27, #0x4c1]    ; cancelled?
0x102bbea4c: ldrsw x5,[x27, #0x4c4]   ; …and an int
0x102bbea54: cmp  w5,#0x6             ; …that must be 6
0x102bbea60: add  x5,x5,#0xc42        ; "cancelled"  (0x1033eac42)
0x102bbea6c: ldrb w5,[x27, #0x4b0]    ; failed?
0x102bbeb20: add  x1,x1,#0x66f        ; "failed"     (0x1033aa66f)
0x102bbebe0: add  x1,x1,#0x2ba        ; "completed"  (0x1033eb2ba)
```

and the exit code is appended to the two ending leaves with
`fmt.Sprintf(" (exit %d)", …)` (0x102bbeb10, 0x102bbebd0) before the
cell is styled. The style is picked by the same predicates — four
styles at `sp+0x24<<12+0x900` (running), `+0xb88` (cancelled),
`+0xe10` (failed) and `+0x678` (completed).

---

## 8. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/format.go` | the row's cells and their arithmetic (`RowLine`); `rowTimeCell`; `truncateAnsi` / `flattenToLine`; `StateStyle` returns the bare word (the padding is gone); **`RowStatus` removed** — the reference's cell is `TaskStatus`'s string, unpadded |
| `internal/taskregistry/format_test.go` | the row test rewritten: the cell order, the `[15:04:05]` cell, the `> ` / `  ` prefixes, the width, the truncation, the flattened name; the padded-label assertions replaced |
| `internal/cliapp/slash_tasks_panel.go` | the panel passes its width to the row; the layout comment is the reference's |
| `internal/cliapp/slash_panels_test.go` | the panel test keys off the name and the status cell instead of the id |
| `out/local-macos/ghidra-scripts/DisasmRange.java` | the parameterised disassembler this cycle's evidence came from |

The row before and after (a completed task with exit code 0, in an
80-cell panel):

```
  ⎿  completed         shell-1  npm test  9s
  ● [15:04:05] npm test                                 completed (exit 0)
```

---

## 9. 硬约束 / the load-bearing rules

- **The row's format is `%s%s %s %s%s%s`** — prefix, marker, time cell,
  name, padding, status cell. The two spaces belong to the format, not
  to the cells.
- **The prefix is two cells wide**, `"> "` on the cursor's row and
  `"  "` on the others; the selected one is styled, the idle one is
  not.
- **The marker is `●`** — the same glyph for every state, coloured by
  the state.
- **The time cell is bracketed**: `[15:04:05]`, or `[          ]` when
  the task has no time.
- **The name is cut to `max(10, width − len(status) − 17)` cells** and
  the padding is `max(2, width − len(status) − width(name) − 15)`
  spaces — so the row is exactly `width` cells wide and the status cell
  ends on the panel's right edge.
- **The status cell is `TaskStatus`'s string** — the word, plus
  ` (exit N)` on the completed and failed forms only.
- **The row carries no id, no elapsed time and no error reason.**

---

## 10. 存疑 / open questions

- **【存疑】 Which time the row shows.** The reference's record carries
  one time per row; the clone shows the task's *start* time. The
  reference's field was not traced back to the code that fills it, so
  "start" is the clone's reading, not a proven one. The blank form
  (`[          ]`) is the zero time — a *finished* task with no time —
  which is why the clone's zero case is the blank cell.
- **【存疑】 The three lipgloss styles the row uses.** The prefix
  (`sp+0x1e<<12+0xdb0`), the time cell (`sp+0x1e<<12+0x8a0`) and the
  name (`sp+0x1e<<12+0x390`) are copies of styles that live in the
  model's style set; they were not decoded. The clone uses the brand
  blue for the prefix, dim gray for the time cell and no colour for
  the name.
- **【存疑】 The `cancelled` rule.** A task is cancelled when a boolean
  at +0x4c1 is set *and* an integer at +0x4c4 is 6. What that integer
  is (an exit code? a signal?) was not resolved.
- **~~【存疑】 `render.RenderTaskItem`'s exact concatenation order.~~**
  The ActiveItems panel's row (0x1028a0d60) is the same machinery — the
  marker, a `"[%s] "` cell (with a trailing space, where the `/tasks`
  row has none), the cut name and the status. Cycle 320 read the order
  out of the instructions — **cell, name, status cell** — and shipped
  the row.

---

## 11. Limitations

- **The reference's `available` is the model's width field**, and the
  clone uses the terminal's width. The reference's panel may well be
  narrower than the terminal (a frame around it, a gutter); the clone
  has no frame, so the two agree only when the panel is full width.
- **The clone's `ExitCode` is often unset.** `Registry.Register` turns
  a caller's `0` into `ExitCodeUnset` (-1), so a task that finished
  successfully shows the bare word unless the caller set the exit code
  explicitly. The reference always knows the exit code of a finished
  task.
- **The clone's `StartedAt` is the registry's own clock**, so the time
  cell is the moment the clone registered the task, not necessarily the
  moment the reference's task started.
- **The `/tasks` panel's kill path and the subagent detail's approval
  flow are still missing** (cycle 317's notes).

---

## 12. The files

### The binary

`~/.local/bin/agy` — Antigravity CLI 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.

### The extraction

`analyzeHeadless` with `DecompileAt.java` and `DisasmRange.java`, run
from `/tmp`:

```bash
cd /tmp && GHIDRA_INSTALL_DIR=~/tools/ghidra_12.1.4_PUBLIC \
  JAVA_HOME=/opt/homebrew/opt/openjdk@21 MAXMEM=8G \
  ~/tools/ghidra_12.1.4_PUBLIC/support/analyzeHeadless \
  out/local-macos/proj agy -process -noanalysis -max-cpu 4 \
  -scriptPath out/local-macos/ghidra-scripts \
  -postScript DisasmRange.java 0x102bbe370 1139
```

### The functions this cycle is based on

| address | function |
|---|---|
| 0x102bbe370 | `model.TasksModel.View` (the row) |
| 0x10289fe70 | `render.StatusMarker` |
| 0x1028a00a0 | `render.TruncateAnsi` |
| 0x1028a0110 | `render.TruncateAnsiLine` |
| 0x1028a0050 | `render.FlattenToLine` |
| 0x1026610c0 | `lipgloss.Width` |
| 0x1026614e0 | `lipgloss.Style.Render` |
| 0x1000b4440 | `strings.Repeat` |
| 0x1000873f0 | `runtime.convTstring` |
| 0x10011d5f0 | `fmt.Sprintf` |

### The literals this cycle is based on

| address | literal | length |
|---|---|---|
| 0x10346a9b8 | `%s%s %s %s%s%s` | 14 |
| 0x103389576 | `> ` | 2 |
| 0x103389194 | `  ` | 2 |
| 0x10338b3f9 | `●◉□⛁` — the marker takes the first three bytes | 12 |
| 0x103392685 | `[%s]` | 4 |
| 0x1033d4327 | `15:04:05` | 8 |
| 0x10338b447 | `n/a` | 3 |
| 0x1034051c7 | ten spaces | 10 |
| 0x103d91f08 | ` ` — the padding's cell | 1 |
| 0x10338add5 | `...` — the truncation's ellipsis | 3 |
| 0x103d91dd8 | `.` — the one-character ellipsis | 1 |
| 0x1033bc1d5 | `running` | 7 |
| 0x1033eac42 | `cancelled` | 9 |
| 0x1033eb2ba | `completed` | 9 |
| 0x1033aa66f | `failed` | 6 |
| 0x1034051bd | ` (exit %d)` | 10 |

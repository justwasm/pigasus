# The ActiveItems row — deliverable

**Cycle:** `cycle-320-tui-active-items-row`
**Subject:** the block under the input box — the row
`model.ActiveItemsModel` renders for every active task, and the
renderer it hands the row to. Cycle 319 recovered the `/tasks` row and
left this one as the clone's own shape ("the clone's ActiveItems row is
still its own"); this cycle reads the row out of the instructions.
**Evidence:** 【反编译】 the disassembly of `render.RenderTaskItem`
(0x1028a0d60–0x1028a1410) and `model.ActiveItemsModel.renderItem`
(0x102b8afd0–0x102b8b370) in the reference `agy` 1.4.7 binary, read
instruction by instruction; 【反编译】 `model.ActiveItemsModel.View`
(0x102b8a790) decompiled; 【strings】 every literal, read out of the
binary's read-only data at the addresses the code points at.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the row's shape** | the clone's own — `● [15:04:05] <id> <name>  <elapsed>` | the reference's — `● [15:04:05] <name> <status>` |
| **the id** | in the row | gone — the reference's record has no other string field |
| **the elapsed time** | in the row | gone |
| **the status cell** | missing | the state word, in the state's colour |
| **the time cell** | `[15:04:05]` | `[15:04:05] ` — the format carries a trailing space |
| **the name** | cut at 60 cells | cut to `max(0, width − width(cell) − width(statusCell))` cells, with `...` |
| **the focused row** | the whole row went bold | only the `> ` prefix is styled |

The row the clone renders now (two running tasks, in an 80-cell panel):

```
  ● [15:04:05] Search Shanghai Weather running
> ● [15:04:05] Running tests in background running
```

---

## 1. The row is `render.RenderTaskItem` — the shared task row

【反编译】 `model.ActiveItemsModel.renderItem` (0x102b8afd0) is a
dispatcher on the record's first byte:

```asm
102b8b060: ldrb w3,[x27, #0x368]   ; the record's kind
102b8b064: cbz  w3,0x102b8b110     ; kind 0 → the task row
102b8b068: cmp  w3,#0x1
102b8b06c: b.ne 0x102b8b098        ; kind 1 → the subagent's own row
102b8b098: cmp  w3,#0x2
102b8b09c: b.eq 0x102b8b0ac        ; kind 2 → the subagent row
…
102b8b15c: bl   0x1028a0d60        ; render.RenderTaskItem
…
102b8b210: bl   0x1028a0a60        ; render.RenderSubagentItem
```

The kind-0 path — the one a *task* takes — ends in a call to
`render.RenderTaskItem` (0x1028a0d60) with the record's fields flattened
onto the stack. **The ActiveItems row is the reference's own shared task
row**, not a second format.

`render.RenderTaskItem` builds the row out of three parts and joins
them with `runtime.concatstring3` (0x10006c250):

```go
marker     := render.StatusMarker(item)              // the styled "●"
timeCell   := fmt.Sprintf("[%s] ", timeStr)           // "[15:04:05] "
statusCell := " " + stateStyle.Render(statusWord)    // " running"
cell       := fmt.Sprintf("%s %s", marker, timeCell)  // "● [15:04:05] "
nameWidth  := maxWidth - lipgloss.Width(cell) - lipgloss.Width(statusCell)
if nameWidth < 0 { nameWidth = 0 }
name       := render.TruncateAnsi(item.Name, nameWidth)
return cell + nameStyle.Render(name) + statusCell
```

The three `concatstring3` arguments are read out of the instructions
(the decompiler's slot model shifts them):

```asm
0x1028a13b4: ldr  x2,[x27, #0x6218]   ; the cell's length
0x1028a13b8: mov  x3,x0              ; the styled name's pointer
0x1028a13bc: mov  x4,x1              ; the styled name's length
0x1028a13c4: ldr  x5,[x27, #0x6230]   ; the status cell's pointer
0x1028a13cc: ldr  x6,[x27, #0x6200]   ; the status cell's length
0x1028a13d0: mov  x0,xzr             ; no temporary buffer
0x1028a13d8: ldr  x1,[x27, #0x6240]   ; the cell's pointer
0x1028a13dc: bl   runtime.concatstring3
```

`runtime.concatstring3(buf, a, b, c)` takes `a` in x1/x2, `b` in x3/x4
and `c` in x5/x6 — so the order is **cell, name, status cell**.

---

## 2. The time cell — `"[%s] "`, with the space the format carries

【反编译】 the time cell is built from the record's `time.Time` (the
wall and ext words at +0x48/+0x50):

```asm
0x1028a0e5c: ldp  x3,x4,[x2, #0x48]   ; the wall and the ext word
0x1028a0e60: cbnz x3,0x1028a0e74
0x1028a0e64: cbnz x4,0x1028a0e74      ; both zero → the empty cell
0x1028a0e68: mov  x3,xzr
0x1028a0e6c: mov  x4,xzr
0x1028a0e70: b    0x1028a0f0c
0x1028a0e74: cbnz x3,0x1028a0e8c
0x1028a0e78: cbnz x4,0x1028a0e8c      ; exactly one zero → "n/a"
0x1028a0e7c: orr  x3,xzr,#0x3
0x1028a0e80: adrp x4,0x10338b000
0x1028a0e84: add  x4,x4,#0x447        ; "n/a"
…
0x1028a0ea4: bl   time.Time.Format    ; "15:04:05" (0x1033d4327)
…
0x1028a0edc: adrp x0,0x10339d000
0x1028a0ee0: add  x0,x0,#0x73f        ; "[%s] " (five bytes)
0x1028a0ef8: bl   fmt.Sprintf
```

So the cell is `[15:04:05] ` normally, `[n/a] ` for a half-set time and
`[] ` for the zero time — **not** the `[          ]` the `/tasks` row
uses. The two rows pad the column differently: the `/tasks` row keeps
the column's width with ten spaces, the ActiveItems row leaves the cell
empty.

The cell's trailing space is why the name follows the cell directly:
the cell is `fmt.Sprintf("%s %s", marker, timeCell)` — one space between
the marker and the cell, and the cell's own trailing space.

---

## 3. The status cell — the word, in the state's colour, behind a space

【反编译】 the same four leaves as the `/tasks` row, and the same exit
suffix — but the polarity of the flag that suppresses the exit code is
now readable from the instructions:

```asm
0x1028a0f60: ldrb w5,[x2, #0x40]      ; the flag
0x1028a0f94: tbz  w5,#0x0,0x1028a0fa4 ; flag clear → the exit code is shown
0x1028a0f98: mov  x5,xzr              ; flag set → no suffix
0x1028a0f9c: mov  x6,xzr
…
0x1028a0fbc: adrp x1,0x108eb0000     ; the exit code, as an `any`
0x1028a0fd0: adrp x0,0x103405000
0x1028a0fd4: add  x0,x0,#0x1bd        ; " (exit %d)"
0x1028a0fec: bl   fmt.Sprintf
…
0x1028a1010: bl   runtime.concatstring2   ; "failed" + the suffix
```

The same pattern is in the `/tasks` row at +0x4c0 — so the flag at
+0x40 **suppresses** the exit code; it is not a "has an exit code"
flag. The clone's `ExitCodeUnset` plays the same role.

The status cell is the space in front of the word
(`runtime.concatstring2` with the one-byte literal at 0x103d91f08),
and the word itself is styled with the state's style — one style per
state, copied out of the model's style set.

---

## 4. The name — cut to what is left

【反编译】 the name is cut with `render.TruncateAnsi` (0x1028a00a0) to
the room the other cells leave:

```asm
0x1028a12ec: bl   lipgloss.Width      ; the cell's width
0x1028a12f4: str  x0,[x27, #0x6210]
…
0x1028a132c: bl   lipgloss.Width      ; the status cell's width
0x1028a1334: ldr  x2,[x27, #0x6210]   ; the cell's width
0x1028a1338: add  x2,x2,x0
0x1028a133c: ldr  x3,[x27, #0x4498]   ; maxWidth
0x1028a1344: sub  x2,x3,x2
0x1028a1354: cmp  x2,#0x0
0x1028a1358: csel x2,x2,xzr,ge        ; max(0, …)
0x1028a135c: bl   render.TruncateAnsi
```

So the name's width is `max(0, maxWidth − width(cell) −
width(statusCell))` — the same truncation the `/tasks` row uses, but
without the row's padding cell and without the floor of ten cells.

---

## 5. The block around the row — the prefix, the width, the overflow lines

【反编译】 `model.ActiveItemsModel.View` (0x102b8a790) wraps the row:

- **the prefix** — two cells, `"> "` (styled) on the focused cursor's
  row and `"  "` on every other one; the condition is the model's
  focused flag *and* the cursor's index against the row's index.
- **the row's width** — the body is laid out in `max(1, W − 2)` cells,
  where `W` is the width the block is given:

  ```asm
  0x102b8a8ac: ldr  x17,[x27, #0x44b8]  ; W
  0x102b8a8b0: sub  x17,x17,#0x2        ; W − 2
  0x102b8a8c4: csel x15,x20,x17,le      ; max(1, W − 2)
  0x102b8a8f4: str  x15,[x27, #0x6238]  ; the width handed to the row
  ```

  The two cells the prefix takes are exactly the two cells the prefix
  occupies.
- **the visible window** — `height / 10` clamped to 1…5, and 5 when the
  height is unknown.
- **the overflow lines** — `"  ↑ %d more"` (0x1034534ce) in front of the
  rows when the block is scrolled, `"  ↓ %d more"` (0x1034534db) behind
  them when rows are left out.
- **the join** — `strings.Join(rows, "\n")` (the separator at
  0x103d91ed8).

The clone already had the overflow lines and the window; the prefix's
styling, the width arithmetic and the row itself are what this cycle
changed.

---

## 6. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/format.go` | `ActiveRowLine` — the active-items row; `activeTimeCell` — its `"[%s] "` cell |
| `internal/taskregistry/format_test.go` | `TestActiveRowLine`, `TestActiveRowLineTruncation` |
| `internal/cliapp/tui_active_items.go` | `renderActiveItem` now hands the row to `taskregistry.ActiveRowLine`; the id, the elapsed time and the whole-row bold are gone |
| `internal/cliapp/tui_active_items_test.go` | the panel test keys off the name, not the id |

The row before and after (a running task, in an 80-cell panel):

```
  ● [15:04:05] subagent-1001 Search Shanghai Weather  15s
  ● [15:04:05] Search Shanghai Weather running
```

---

## 7. 硬约束 / the load-bearing rules

- **The row is `render.RenderTaskItem`'s** — the same shared renderer
  the reference uses wherever a task is listed. There is no second
  ActiveItems format.
- **The row is `cell + name + statusCell`**, joined by
  `runtime.concatstring3` — the order is the cell, then the name, then
  the status.
- **The time cell is `"[%s] "`** — the format carries the space; the
  cell is empty for a task without a time (`[] `), not ten spaces.
- **The name is cut to `max(0, width − width(cell) − width(statusCell))`**
  cells; the width is the block's width minus the prefix's two cells.
- **The status cell is a space and the styled word** — the word is
  `TaskStatus`'s string, plus ` (exit N)` on the completed and failed
  forms.
- **The row carries no id, no elapsed time and no error reason.**
- **The prefix is two cells** — `"> "` on the focused cursor's row,
  `"  "` on the others — and nothing else in the row is styled for the
  focus.

---

## 8. 存疑 / open questions

- **【存疑】 Which time the row shows.** The reference's record carries
  one time per row; the clone shows the task's *start* time. The
  reference's field was not traced back to the code that fills it, so
  "start" is the clone's reading, not a proven one.
- **【存疑】 The flag at +0x40.** It suppresses the exit code (and it
  also picks the marker's colour, in `render.StatusMarker`'s first
  branch), so it is *not* a "has an exit code" flag — but what it *is*
  was not resolved. The clone's `ExitCodeUnset` stands in for it.
- **【存疑】 The lipgloss styles the row uses** — the marker's
  (`sp+0x8<<12+0x410` and its three siblings), the name's
  (`sp+0x10<<12+0x178`) and the four status styles
  (`sp+0x16<<12+0x460`, `+0x6e8`, `+0x970`, `+0xbf8`) — were not
  decoded. The clone uses the state's colour for the marker and the
  status word, and no colour for the name.
- **【存疑】 The subagent's own row.** `render.RenderSubagentItem`
  (0x1028a0a60) is the kind-1/kind-2 path of the same dispatcher; it
  was not decoded, so the clone renders a subagent like a task.
  **Resolved in cycle 321** — see
  `docs/cycles/cycle-321-tui-active-items-subagent-row/deliverable.md`:
  the subagent row is `render.RenderSubagentItem`, and the clone draws
  it now.
- **【存疑】 The `cancelled` rule.** A task is cancelled when a boolean
  at +0x41 is set *and* an integer at +0x44 is 6. What that integer is
  (an exit code? a signal?) was not resolved.

---

## 9. Limitations

- **The reference's width is the block's width**, and the clone uses the
  terminal's width. The reference's block may well be narrower than the
  terminal (it sits under the input box, which may be framed or
  indented); the clone has no frame, so the two agree only when the
  block is full width.
- **The clone's `StartedAt` is the registry's own clock**, so the time
  cell is the moment the clone registered the task, not necessarily the
  moment the reference's task started.
- **The clone's `ExitCode` is often unset** — `Registry.Register` turns a
  caller's `0` into `ExitCodeUnset` (-1) — so a task that finished
  successfully shows the bare word unless the caller set the exit code
  explicitly. The reference always knows the exit code of a finished
  task.
- **The clone sorts the active items by start time.** The reference
  sorts them too (`getActiveItems.func1`, 0x102bec370), but by what key
  was not read out.
- **The `/tasks` panel's kill path and the subagent detail's approval
  flow are still missing** (cycle 317's notes).

---

## 10. The files

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
  -postScript DisasmRange.java 0x1028a0d60 500
```

`model.ActiveItemsModel.renderItem` is not in the seeded project, so its
instructions were read with `llvm-objdump` on the same binary (the
addresses agree — the image's `__text` spans 0x1000011c0–0x1033861f4
and `llvm-objdump` prints them without the image base):

```bash
llvm-objdump -d --start-address=0x102b8afd0 --stop-address=0x102b8b370 \
  ~/.local/bin/agy
```

### The functions this cycle is based on

| address | function |
|---|---|
| 0x102b8a790 | `model.ActiveItemsModel.View` (the block) |
| 0x102b8afd0 | `model.ActiveItemsModel.renderItem` (the dispatcher) |
| 0x1028a0d60 | `render.RenderTaskItem` (the row) |
| 0x10289fe70 | `render.StatusMarker` |
| 0x1028a00a0 | `render.TruncateAnsi` |
| 0x1026610c0 | `lipgloss.Width` |
| 0x1026614e0 | `lipgloss.Style.Render` |
| 0x1000873f0 | `runtime.convTstring` |
| 0x100087380 | `runtime.convT64` |
| 0x10006c1e0 | `runtime.concatstring2` |
| 0x10006c250 | `runtime.concatstring3` |
| 0x10011d5f0 | `fmt.Sprintf` |
| 0x1000ecdd0 | `time.Time.Format` |

### The literals this cycle is based on

| address | literal | length |
|---|---|---|
| 0x10339d73f | `[%s] ` — the ActiveItems time cell | 5 |
| 0x1033aa603 | `%s %s` — the cell's own format | 5 |
| 0x1033d4327 | `15:04:05` | 8 |
| 0x10338b447 | `n/a` | 3 |
| 0x10338b3f9 | `●◉□⛁` — the marker takes the first three bytes | 12 |
| 0x103d91f08 | ` ` — the status cell's leading space | 1 |
| 0x1033bc1d5 | `running` | 7 |
| 0x1033eac42 | `cancelled` | 9 |
| 0x1033eb2ba | `completed` | 9 |
| 0x1033aa66f | `failed` | 6 |
| 0x1034051bd | ` (exit %d)` | 10 |
| 0x1034534ce | `  ↑ %d more` | 13 |
| 0x1034534db | `  ↓ %d more` | 13 |
| 0x103d91ed8 | `\n` — the rows' separator | 1 |
| 0x103389576 | `> ` — the selected row's prefix | 2 |
| 0x103389194 | `  ` — every other row's prefix | 2 |

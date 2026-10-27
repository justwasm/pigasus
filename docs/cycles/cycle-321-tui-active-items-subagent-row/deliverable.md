# The ActiveItems subagent row — deliverable

**Cycle:** `cycle-321-tui-active-items-subagent-row`
**Subject:** the row the block under the input box renders for a
*subagent* — `render.RenderSubagentItem` (0x1028a0a60) and the
`render.GetSubagentRenderInfo` (0x1028a0220) behind it. Cycle 320
recovered the *task* row and left this one open ("the clone renders a
subagent like a task"); this cycle reads the subagent row out of the
instructions.
**Evidence:** 【反编译】 the disassembly of `render.RenderSubagentItem`
(0x1028a0a60–0x1028a0d28) and `render.GetSubagentRenderInfo`
(0x1028a0220–0x1028a0988) in the reference `agy` 1.4.7 binary, read
instruction by instruction; 【反编译】 `render.formatSubagentName`
(0x1028cb820) decompiled; 【strings】 every literal, read out of the
binary's read-only data at the addresses the code points at.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the subagent's row** | the task row — `● [15:04:05] <name> running` | the reference's — `● Agent(<name>)  <status> · <elapsed>` |
| **the tool info** | missing | `Agent(<name>)` — the reference's label for a subagent that calls no tool |
| **the status word** | the task row's `running` / `completed` / `failed` / `cancelled` | the reference's own — `Done`, `Error: <message>`, `Killed`, `Processing...` |
| **the elapsed time** | missing | there — bare while the subagent runs, behind a `worked` once it is done |
| **the flexible cell** | the name | the status word — the name is never cut |
| **the marker's colour** | the task row's | the subagent's own — 220 / 34 / 196, the ANSI-256 palette |

The rows the clone renders now (a running subagent and a running task,
in an 80-cell panel):

```
  ● Agent(Search Shanghai Weather)  Processing... · 1m35s
  ● [15:04:05] Running tests in background running
```

---

## 1. The dispatcher picks the row by the record's kind

【反编译】 `model.ActiveItemsModel.renderItem` (0x102b8afd0) is a
dispatcher on the record's first byte, and it has three arms — cycle 320
read the first one:

```asm
102b8b060: ldrb w3,[x27, #0x368]   ; the record's kind
102b8b064: cbz  w3,0x102b8b110     ; kind 0 → render.RenderTaskItem
102b8b068: cmp  w3,#0x1
102b8b06c: b.ne 0x102b8b098
102b8b098: cmp  w3,#0x2
102b8b09c: b.eq 0x102b8b0ac        ; kind 2 → the subagent's own renderer
…
102b8b210: bl   0x1028a0a60        ; render.RenderSubagentItem
```

The kind-1 arm is the one this cycle is about. It looks the record's id
up in the model's own list and hands the subagent it finds to
`render.RenderSubagentItem` (0x1028a0a60) with three arguments — the
subagent, the panel's width and a flag:

```asm
102b8b204: mov  x1,x15             ; the width
102b8b208: mov  x2,x0              ; the flag
102b8b20c: mov  x0,x3              ; the subagent
102b8b210: bl   0x1028a0a60
```

The flag is set when the record's id was found in the model's list
(`cset x0, gt` at 0x102b8b288, on the loop counter that is still
positive after the match) — and it turns out to mean *the subagent is
blocked*, because the reference then prefixes its status word with
`"Blocked"` (see §5).

---

## 2. The row is three cells

【反编译】 `render.RenderSubagentItem` (0x1028a0a60) asks
`render.GetSubagentRenderInfo` (0x1028a0220) for four strings and joins
three of them:

```asm
1028a0ad4: adrp x1,0x10338b000
1028a0ad8: add  x1,x1,#0x3f9        ; the marker "●"
1028a0ae4: bl   0x1028a0220          ; render.GetSubagentRenderInfo
…
1028a0bbc: bl   0x10011d5f0          ; fmt.Sprintf — "%s %s  "
1028a0bd0: bl   0x1026610c0          ; lipgloss.Width — the cell
1028a0bf8: bl   0x1026610c0          ; lipgloss.Width — the elapsed cell
1028a0c10: sub  x4,x5,x4             ; the width − cell − elapsed
1028a0c28: bl   0x1028a00a0          ; render.TruncateAnsi — the status word
1028a0d18: bl   0x10006c250          ; runtime.concatstring3
```

So the row is

```go
marker, toolInfo, status, elapsed := render.GetSubagentRenderInfo(subagent, "●", blocked)
cell   := fmt.Sprintf("%s %s  ", marker, toolInfo)
w      := maxWidth - lipgloss.Width(cell) - lipgloss.Width(elapsed)
if w < 0 { w = 0 }
return cell + statusStyle.Render(render.TruncateAnsi(status, w)) + elapsedStyle.Render(elapsed)
```

The format `"%s %s  "` (0x1033bd50e) carries **two** trailing spaces —
the cell ends with them.

**The flexible cell is the status word, not the name.** The reference
cuts the status word to what is left of the row; the name and the tool
info are never cut, so a long name overflows the row.

The two styles are the theme's own: the status word's style is copied
from `sp+0x17000+0x320` (0x1028a0c3c) and the elapsed cell's from
`sp+0x19000+0x180` (0x1028a0cac). They were not decoded — see 存疑.

---

## 3. The tool info — `Agent(<name>)`

【反编译】 `render.GetSubagentRenderInfo` (0x1028a0220) builds the tool
info in two ways:

```asm
1028a04f4: adrp x0,0x1033eb000
1028a04f8: add  x0,x0,#0xfd7        ; "Agent(%s)"
1028a04fc: mov  x1,#0x9             ; the length
1028a050c: bl   0x10011d5f0          ; fmt.Sprintf
```

and, when the subagent is inside a tool call,

```asm
1028a09d8: adrp x3,0x10339c000
1028a09dc: add  x3,x3,#0xe0c        ; " › "
1028a09e0: mov  x4,#0x5             ; five bytes
1028a09ec: bl   0x10006c250          ; runtime.concatstring3
```

A subagent that calls no tool is labelled **`Agent(<name>)`**; one that
is inside a tool call is labelled **`<tool> › <name>`**. The clone keeps
no "current tool step" — its registry only remembers the tools a
subagent called — so it always takes the first form.

---

## 4. The status word — the reference's own vocabulary

【反编译】 the status word is read off the subagent's own status string
(the field at +0x30) and picked from four literals:

| the subagent's status | the word | the literal |
|---|---|---|
| `done`, `idle` | `Done` | 0x1033926cd |
| `error` | `Error: ` + the message at +0x108 | 0x1033bcf03 |
| `killed` | `Killed` | 0x1033ab533 |
| anything else | the tool's own name (or `Tool`, 0x10339203d) | — |
| … and no tool either | `Processing...` | 0x1034535d2 |

```asm
1028a031c: ldp  x3,x4,[x2, #0x30]   ; the status
1028a05ac: adrp x9,0x103453000
1028a05b0: add  x9,x9,#0x5d2        ; "Processing..."
1028a05b8: cmp  x11,#0x4            ; "done" / "idle"
1028a05e4: orr  x5,xzr,#0x4         ; the length
1028a05e8: adrp x8,0x103392000
1028a05ec: add  x8,x8,#0x6cd        ; "Done"
```

The clone maps its registry states onto that vocabulary: `pending` and
`running` → `Processing...`, `done` → `Done`, `failed` → `Error: ` plus
the task's error, `killed` and `removed` → `Killed`.

---

## 5. The marker's colour, and the `Blocked` prefix

【反编译】 the marker is the plain `●` (three bytes at 0x10338b3f9)
painted with a lipgloss colour picked from the subagent's status:

```asm
1028a034c: orr  x3,xzr,#0x2         ; the length
1028a0354: add  x4,x4,#0x540        ; "34"  — done / idle
1028a03bc: add  x4,x4,#0x444        ; "196" — error / killed
1028a03cc: add  x4,x4,#0x441        ; "220" — anything else
1028a03fc: bl   0x102657610          ; lipgloss.Color
1028a0444: bl   0x10265c940          ; lipgloss.(*Style).set — 0x400 = the foreground
```

Those are the same three colours the subagent detail's state word
carries (cycle 316) — the **ANSI-256** palette, not the true-colour one
the rest of the clone's palette uses: `\x1b[38;5;220m`, `\x1b[38;5;34m`
and `\x1b[38;5;196m`.

When the row's flag is set — the subagent was found in the model's
list — the reference prefixes the status word with `Blocked`, painted in
a fourth colour, `214` (0x10338b468), and joins the two with ` · `:

```asm
1028a06c4: add  x0,x0,#0x468        ; "214"
1028a0748: add  x4,x4,#0x507        ; "Blocked"
1028a079c: add  x3,x3,#0xf99        ; " · "
1028a07b4: bl   0x10006c250          ; runtime.concatstring3
```

The clone has no blocked state — the reference's approval flow is still
missing (cycle 317's notes) — so the row never carries the prefix, and
the orange never appears. The status word itself is painted with the
theme's own style, which the clone cannot read; it borrows the
subagent's state colour instead — the colour its marker carries.

---

## 6. The elapsed cell — bare while running, `worked` once done

【反编译】 the elapsed cell has two forms, and the reference's subagent
detail header draws the same line (cycle 315's `detailElapsedLine`).

A subagent whose status is `running` shows the bare duration — the time
since it started:

```asm
1028a07d8: ldp  x9,x10,[x4, #0x30]  ; the status
1028a07dc: cmp  x10,#0x7             ; "running"
1028a081c: bl   0x1000f4cb0          ; time.Since — the time at +0x70
1028a0828: bl   0x1000f4970          ; time.Duration.Round — one second
1028a084c: add  x0,x0,#0x539        ; " · %s"
1028a0860: bl   0x10011d5f0          ; fmt.Sprintf
```

Any other status shows the duration of the finished subagent — the
difference of the two times its record carries — behind a `worked`:

```asm
1028a0880: ldp  x9,x10,[x4, #0x88]  ; the end time
1028a08b0: bl   0x1000f4b50          ; time.Time.Sub
1028a08b8: b.gt 0x1028a08d8         ; only a positive duration counts
1028a091c: add  x0,x0,#0x5df        ; " · worked %s"
1028a0930: bl   0x10011d5f0          ; fmt.Sprintf
```

So the row reads `· 1m30s` while the subagent works and
`· worked 1m30s` once it is done — the same rule the clone's subagent
detail header already follows.

The `%s` formats a `time.Duration`, so the text is Go's own duration
form: `1m30s`, `1h5m0s`, `0s`.

---

## 7. The name

【反编译】 `render.formatSubagentName` (0x1028cb820) joins the
subagent's name and its label with `": "` (0x1033891ca) and falls back
to the generic `Subagent` (0x1033d3807) when it has neither:

```c
if len(name) != 0 {
    if len(label) != 0 {
        return name + ": " + render.Truncate(label, 40)
    }
    return name
}
if len(label) != 0 {
    return render.Truncate(label, 50)
}
return "Subagent"
```

`render.GetSubagentRenderInfo` (0x1028a0220) then notices that generic
name and uses the subagent's **id** instead, cut to eight bytes:

```asm
1028a02d4: ldp  x3,x4,[x2]          ; the id
1028a02e8: cmp  x1,#0x8
1028a02f0: orr  x1,xzr,#0x8         ; eight bytes
```

The clone's registry has no label of its own, so the name stands alone
— and an empty name takes the id's place, cut the same way.

---

## 8. What the clone changed

| file | change |
|---|---|
| `internal/taskregistry/format.go` | `ActiveSubagentRowLine` and its helpers — the marker, the tool info, the status word, the elapsed cell, the name |
| `internal/cliapp/tui_active_items.go` | `renderActiveItem` picks the renderer by the record's kind |
| `internal/taskregistry/format_test.go` | `TestActiveSubagentRowLine`, `TestActiveSubagentRowLineTruncation` |
| `internal/cliapp/tui_active_items_test.go` | the subagent row's tool info and elapsed cell |

---

## 9. 存疑 / open questions

- **【存疑】 The theme's two styles.** The status word's style
  (`sp+0x17000+0x320`) and the elapsed cell's style
  (`sp+0x19000+0x180`) were not decoded. The clone paints the status
  word with the subagent's own state colour — the colour its marker
  carries — and the elapsed cell with the dim grey.
- **【存疑】 The `Blocked` prefix's flag.** The flag is set when the
  record's id was found in the model's list at +0x88, and the reference
  then writes `Blocked · ` before the status word — so the list holds
  the subagents waiting for the user's approval. The clone has no
  approval flow at all (cycle 317's notes), so the prefix never
  appears.
- **【存疑】 The reference's own elapsed cell for a running subagent.**
  The reference reads `time.Since` of the time at +0x70 — the same field
  the worked cell's subtraction starts from — so a running subagent's
  elapsed grows with the clock ✓. Whether the record's second time
  (+0x88) is the end time or "now" for a live subagent was not traced
  back to the code that fills it.
- **【存疑】 The tool branch.** `<tool> › <name>` needs the subagent's
  *current* tool step; `render.toolStepInfo` (0x1028c8c40) is a large
  type switch over the tool's kind. The clone keeps no such state.

---

## 10. Limitations

- **The clone has no "current tool step"**, so every subagent row is
  labelled `Agent(<name>)` — the reference's label for a subagent
  outside a tool call.
- **The clone has no blocked state**, so the `Blocked · ` prefix and
  its orange are missing.
- **The clone's `StartedAt` is the registry's own clock**, so the
  elapsed cell measures from the moment the clone registered the
  subagent.
- **The clone's error message is the registry's own `Err`** — the
  reference reads it from the field at +0x108 of its subagent record.
- **The reference's row can overflow the panel** — the name is never
  cut — and so can the clone's.

---

## 11. The files

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
  -scriptPath out/local-macos/ghidra-scripts \
  -postScript DecompileAt.java 0x1028cb820 0x1028c8c40
```

The two row functions were read with `llvm-objdump` on the same binary:

```bash
llvm-objdump -d --start-address=0x1028a0220 --stop-address=0x1028a0990 \
  ~/.local/bin/agy
llvm-objdump -d --start-address=0x1028a0a60 --stop-address=0x1028a0d28 \
  ~/.local/bin/agy
```

### The functions this cycle is based on

| address | function |
|---|---|
| 0x1028a0220 | `render.GetSubagentRenderInfo` — the four strings |
| 0x1028a0a60 | `render.RenderSubagentItem` — the row |
| 0x1028cb820 | `render.formatSubagentName` — the name |
| 0x1028c8c40 | `render.toolStepInfo` — the tool's own name |
| 0x1028a0050 | `render.FlattenToLine` |
| 0x1028a00a0 | `render.TruncateAnsi` |
| 0x1026610c0 | `lipgloss.Width` |
| 0x1026614e0 | `lipgloss.Style.Render` |
| 0x102657610 | `lipgloss.Color` |
| 0x10265c940 | `lipgloss.(*Style).set` |
| 0x10011d5f0 | `fmt.Sprintf` |
| 0x10006c250 | `runtime.concatstring3` |
| 0x100087380 | `runtime.convT64` |
| 0x1000f4b50 | `time.Time.Sub` |
| 0x1000f4cb0 | `time.Since` |
| 0x1000f4970 | `time.Duration.Round` |

### The literals this cycle is based on

| address | literal | length |
|---|---|---|
| 0x1033bd50e | `%s %s  ` — the cell's format | 7 |
| 0x1033ebfd7 | `Agent(%s)` — the tool info | 9 |
| 0x1033926cd | `Done` | 4 |
| 0x1033bcf03 | `Error: ` | 7 |
| 0x1033ab533 | `Killed` | 6 |
| 0x1034535d2 | `Processing...` | 13 |
| 0x10339203d | `Tool` | 4 |
| 0x1033ab539 | ` · %s` — the running elapsed cell | 6 |
| 0x1034535df | ` · worked %s` — the finished elapsed cell | 13 |
| 0x10339ce0c | ` › ` — the tool info's separator | 5 |
| 0x1033bd507 | `Blocked` | 7 |
| 0x103391f99 | ` · ` — the blocked prefix's separator | 4 |
| 0x1033891ca | `: ` — `formatSubagentName`'s separator | 2 |
| 0x1033d3807 | `Subagent` — the generic name | 8 |
| 0x10338b468 | `214` — the live status word's colour | 3 |
| 0x103389540 | `34` — the finished marker's colour | 2 |
| 0x10338b444 | `196` — the failed marker's colour | 3 |
| 0x10338b441 | `220` — the running marker's colour | 3 |
| 0x10338b3f9 | `●◉□⛁` — the marker takes the first three bytes | 12 |

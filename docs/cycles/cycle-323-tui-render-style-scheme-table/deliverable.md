# The colour-scheme table and the style tree — deliverable

**Cycle:** `cycle-323-tui-render-style-scheme-table`
**Subject:** the reference's eight colour schemes, the two branches its
style set is built from, and the four styles the task row paints its
marker and its status word with.
**Evidence:** 【一手】 the scheme table, read out of `types.init`
(0x102671f50) — the colours themselves, the eight names and the map
they live in; 【反编译】 `render.resolveStyles` (0x1028daef0) read
instruction by instruction — its two branches, the ninety-one styles it
builds and the tail that copies them into the result; 【反编译】
`render.StatusMarker` (0x10289fe70) and `render.RenderTaskItem`
(0x1028a0d60) — the four styles they pick and the order they pick them
in; 【一手】 the clone's own truecolour capture, which turns out to be
one of the eight schemes.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the colour schemes** | unknown — the clone carried a hand-copied palette and the guide said the scheme "cannot be read" | the reference's eight schemes, all twenty slots each, in `internal/palette` |
| **the style tree** | unknown | 87 of the 91 styles, slot by slot, on **both** branches of the resolver |
| **the task row's marker and status word** | "one of four state colours", the styles' names unknown | `ToolDone` / `ToolRunning` / `ToolCanceled` / `ToolError`, and the order they are picked in |
| **the clone's own palette** | capture-derived | unchanged — and now pinned to the **solarized light** scheme by a test |

This cycle closes two open questions from earlier cycles:

- cycle 318, §6: "**【存疑】 The marker's colours.** `render.StatusMarker`
  picks one of four lipgloss styles by the task's state; the styles
  themselves live in `render.init` and were not decoded." → decoded:
  they are `render.Styles`' `ToolDone`, `ToolRunning`, `ToolCanceled`
  and `ToolError`, and their colours come from the scheme table.
- cycle 320, §8: "**【存疑】 The lipgloss styles the row uses** — the
  marker's (… +0x410 and its three siblings), the name's (… +0x178)
  and the four status styles (… +0x460, +0x6e8, +0x970, +0xbf8) were
  not decoded." → decoded: the four status styles are the same four
  tool styles, and the name's style is `HelpCmd`.

---

## 1. The eight schemes

【一手】 the reference's `types` package builds a map of eight named
colour schemes when it initialises. `types.init` (0x102671f50) writes
each scheme as a struct literal of twenty `lipgloss.Color` values
(0x102657610) into a stack slot and inserts it into the map with
`runtime.mapassign` (0x100008e50); the map's type descriptor is
0x108c51e40 and the eight key headers sit at 0x1082276f0 + 0x10·k.

The names are the ones a user would type: `light`, `solarized light`,
`colorblind-friendly light`, `dark`, `solarized dark`,
`colorblind-friendly dark`, `tokyo night`, `terminal`.

Seven of the eight carry true colours; `terminal` carries terminal
indices and leaves the six background slots empty.

| slot | light | solarized light | colorblind-friendly light | dark | solarized dark | colorblind-friendly dark | tokyo night | terminal |
|---|---|---|---|---|---|---|---|---|
| `Bg` | — | `#586e75` | `#3c4043` | `#bdc1c6` | `#93a1a1` | `#bdc1c6` | `#a9b1d6` | — |
| `Fg` | `#202124` | `#073642` | `#202124` | `#e8eaed` | `#eee8d5` | `#e8eaed` | `#c0caf5` | — |
| `Accent` | `#4285f4` | `#268bd2` | `#4285f4` | `#8ab4f8` | `#268bd2` | `#8ab4f8` | `#7aa2f7` | ANSI 12 |
| `Border` | `#dadce0` | `#eee8d5` | `#dadce0` | `#3c4043` | `#073642` | `#3c4043` | `#414868` | ANSI 8 |
| `UserFg` | `#1967d2` | `#268bd2` | `#1967d2` | `#8ab4f8` | `#268bd2` | `#8ab4f8` | `#7aa2f7` | ANSI 4 |
| `AgentFg` | `#188038` | `#859900` | `#1967d2` | `#81c995` | `#859900` | `#8ab4f8` | `#9ece6a` | ANSI 2 |
| `AddFg` | `#188038` | `#859900` | `#1967d2` | `#81c995` | `#859900` | `#8ab4f8` | `#9ece6a` | ANSI 2 |
| `AddBg` | `#e6f4ea` | `#eee8d5` | `#e8f0fe` | `#0d2e16` | `#073642` | `#041e49` | `#1c2d26` | — |
| `AddWordBg` | `#a8dab5` | `#cde0b4` | `#aecbfa` | `#1e5c2e` | `#1d4e2f` | `#0c3d8a` | `#2b5340` | — |
| `RemoveFg` | `#c5221f` | `#dc322f` | `#c5221f` | `#f28b82` | `#dc322f` | `#f28b82` | `#f7768e` | ANSI 1 |
| `RemoveBg` | `#fce8e6` | `#eee8d5` | `#fce8e6` | `#3c0000` | `#073642` | `#3c0000` | `#3b2229` | — |
| `RemoveWordBg` | `#f6aea9` | `#f2c6be` | `#f6aea9` | `#731414` | `#4c1e2a` | `#731414` | `#732e40` | — |
| `LineNumFg` | `#80868b` | `#839496` | `#80868b` | `#80868b` | `#657b83` | `#80868b` | `#787c99` | ANSI 8 |
| `ToolFg` | `#e37400` | `#b58900` | `#e8710a` | `#fdd663` | `#b58900` | `#f9ab00` | `#e0af68` | ANSI 3 |
| `TaskFg` | `#9334e6` | `#d33682` | `#1967d2` | `#d7aefb` | `#d33682` | `#8ab4f8` | `#bb9af7` | ANSI 5 |
| `ErrorFg` | `#d93025` | `#dc322f` | `#d93025` | `#f28b82` | `#dc322f` | `#f28b82` | `#f7768e` | ANSI 1 |
| `WarningFg` | `#e37400` | `#cb4b16` | `#e8710a` | `#f9ab00` | `#cb4b16` | `#f9ab00` | `#ff9e64` | ANSI 3 |
| `LinkFg` | `#1a73e8` | `#268bd2` | `#1a73e8` | `#8ab4f8` | `#268bd2` | `#8ab4f8` | `#7aa2f7` | ANSI 4 |
| `MutedFg` | `#3c4043` | `#586e75` | `#3c4043` | `#bdc1c6` | `#93a1a1` | `#3c4043` | `#a9b1d6` | ANSI 7 |
| `DimFg` | `#5f6368` | `#657b83` | `#5f6368` | `#9aa0a6` | `#839496` | `#9aa0a6` | `#9aa5ce` | ANSI 8 |

Two properties of the table are worth naming because they are *not*
what a reader would guess:

- **`AgentFg` and `AddFg` are the same colour in every scheme.** So a
  capture cannot tell an agent's colour from an added line's colour —
  and neither can the style tree, which reads the same slot for both.
- **`Bg` is not a background.** It is a colour like the rest, and the
  style tree uses it as a *foreground* (`BtnApprove`, `SystemCommandText`
  and `SystemCommandArgsText` all paint their text with it).

---

## 2. `types.SchemeColors` — the layout

【反编译】 the type descriptor is at 0x1097d1f30. The struct is 320
bytes: twenty slots, sixteen bytes each — a `lipgloss.Color` is an
interface (a type word and a data word).

| offset | slot | | offset | slot |
|---|---|---|---|---|
| +0x0 | `Bg` | | +0x90 | `RemoveFg` |
| +0x10 | `Fg` | | +0xa0 | `RemoveBg` |
| +0x20 | `Accent` | | +0xb0 | `RemoveWordBg` |
| +0x30 | `Border` | | +0xc0 | `LineNumFg` |
| +0x40 | `UserFg` | | +0xd0 | `ToolFg` |
| +0x50 | `AgentFg` | | +0xe0 | `TaskFg` |
| +0x60 | `AddFg` | | +0xf0 | `ErrorFg` |
| +0x70 | `AddBg` | | +0x100 | `WarningFg` |
| +0x80 | `AddWordBg` | | +0x110 | `LinkFg` |
| | | | +0x120 | `MutedFg` |
| | | | +0x130 | `DimFg` |

---

## 3. `render.resolveStyles` — two branches, one tail

【反编译】 `render.resolveStyles` (0x1028daef0) turns a scheme name
into the reference's style set. Its frame is 0x1c330 bytes — the style
set alone is 57,720 of them.

`render.Styles` (type descriptor 0x109862768) is **91 fields**:
89 `lipgloss.Style` (648 bytes each) and two 24-byte slices. The four
styles this cycle is about sit at:

| style | offset | style | offset |
|---|---|---|---|
| `HelpCmd` | +0x1e60 | `ToolDone` | +0x8148 |
| `ToolRunning` | +0x83d0 | `ToolCanceled` | +0x8658 |
| `ToolError` | +0x88e0 | | |

The function has **two branches** and **one tail**:

```asm
0x1028dafec: tbz  w1, #0x0, 0x1028dfcec   ; the scheme is not in the map
```

- the **scheme path** (0x1028dafec–0x1028dfcec) — the name was in the
  map. The scheme is copied into the frame at +0x588 and every style
  takes its colour out of a slot; a slot's index is
  (offset − 0x588) / 0x10.
- the **fallback path** (0x1028dfcec–0x1028e4490) — the name was not in
  the map. The same ninety-one styles are built, but out of literal
  terminal indices (`lipgloss.Color("2")` and friends).

Both paths write into the same frame slots, so the **common tail** at
0x1028e4490 can serve either: it zeroes the result (901 iterations ×
64 bytes + 56 = 57,720 bytes — exactly the struct) and then copies
**87** of the ninety-one styles into it. The four it leaves zeroed are
the gradient ones — `EffortXHighGradient`, `EffortMaxRainbow`,
`SearchHighlight` and `SearchHighlightActive` — which are not built by
this function at all.

**The two paths do not agree.** The failed task's style is the clearest
case:

| | scheme path | fallback path |
|---|---|---|
| `ToolDone` | `AgentFg` + bold | ANSI 2 |
| `ToolRunning` | `ErrorFg` + bold | ANSI 3 |
| `ToolCanceled` | `LinkFg` + bold | ANSI 8 |
| `ToolError` | `AgentFg` (no bold) | ANSI 1 |

Under the scheme path a **failed** task is painted with the *agent*
colour — green in `solarized light` — and a **running** one with the
*error* colour. The fallback path paints the failed task red and the
running one yellow, which is what a reader would expect. The mapping
was checked twice: the field offsets come from the type descriptor and
from `RenderTaskItem`'s own reads, and the colours come from the
per-slot reads in the branch's own code.

---

## 4. The style tree — 87 of the 91 styles

【反编译】 the whole table, both branches. `scheme` is the slot the
colour comes from on the scheme path; `ANSI n` is the literal index the
fallback path uses; `bold` is the bold flag; `bg` is the background.

| style | scheme path | fallback path |
|---|---|---|
| `AgentActive` | `MutedFg` | ANSI 12 + bold |
| `AgentCursor` | `MutedFg` | ANSI 12 |
| `AgentDesc` | `ErrorFg` | ANSI 7 |
| `AgentKey` | `Accent` | ANSI 12 |
| `AgentName` | `DimFg` | — |
| `AgentPath` | bold | ANSI 7 |
| `AgentTitle` | `ToolFg` | bold |
| `Box` | `Accent` | — |
| `BtnApprove` | `Bg` + bg `Accent` | ANSI 2 + bold |
| `BtnApproveActive` | — | — |
| `BtnOpen` | `MutedFg` | ANSI 4 + bold |
| `BtnOpenActive` | — | — |
| `BtnReject` | `TaskFg` + bold | ANSI 1 + bold |
| `BtnRejectActive` | — | — |
| `BtwHint` | `DimFg` | ANSI 8 |
| `BtwLabel` | `DimFg` | ANSI 3 + bold |
| `Chevron` | `MutedFg` + bold | ANSI 12 |
| `CloudSession` | `WarningFg` | ANSI 12 |
| `Comment` | `DimFg` | ANSI 4 |
| `CommentLineNum` | `Accent` + bold | ANSI 4 |
| `CompactionMarker` | `Border` | ANSI 7 |
| `CursorLineNum` | `LineNumFg` + bg `RemoveBg` | ANSI 12 + bold |
| `CursorMarker` | `Accent` | ANSI 12 + bold |
| `DiffAdd` | `DimFg` | ANSI 2 |
| `DiffAddLineNum` | `AddFg` + bg `AddBg` | ANSI 8 |
| `DiffHunk` | `LineNumFg` | ANSI 6 |
| `DiffLineNum` | `DimFg` | ANSI 8 |
| `DiffRemove` | `ToolFg` + bold | ANSI 1 |
| `DiffRemoveLineNum` | `RemoveFg` + bg `RemoveBg` | ANSI 8 |
| `Dim` | `LineNumFg` + bg `AddBg` | ANSI 8 |
| `EffortHigh` | `AddFg` | ANSI 1 |
| `EffortLow` | `MutedFg` | ANSI 2 |
| `EffortMax` | `ErrorFg` | ANSI 1 + bold |
| `EffortMaxGlint` | `ErrorFg` + bold | bold |
| `EffortMedium` | `MutedFg` | ANSI 3 |
| `EffortXHigh` | `WarningFg` | ANSI 1 + bold |
| `Error` | `ToolFg` | ANSI 1 |
| `ExitHint` | `WarningFg` | ANSI 8 |
| `HelpCmd` | bold | ANSI 12 |
| `HelpDesc` | `Accent` | ANSI 8 |
| `HelpKey` | `Accent` | ANSI 12 |
| `HelpLabel` | `DimFg` | bold |
| `HelpTitle` | `DimFg` | bold |
| `HighlightHint` | `Border` | bold |
| `HighlightedText` | `ToolFg` + bold | ANSI 4 |
| `Hint` | — | ANSI 8 |
| `ModeAcceptEdits` | `Bg` + bg `Accent` | ANSI 2 |
| `ModeAuto` | `DimFg` + bold | ANSI 3 |
| `ModeBestOfN` | `AgentFg` | ANSI 5 |
| `ModePlan` | `DimFg` + bold | ANSI 12 |
| `PendingHint` | `ToolFg` + bold | ANSI 8 |
| `PendingInput` | `ErrorFg` + bold | bold |
| `ScrollThumb` | `DimFg` | ANSI 8 |
| `ScrollThumbActive` | `DimFg` | ANSI 7 |
| `ScrollTrack` | `MutedFg` | ANSI 0 |
| `SelCursor` | `UserFg` + bold | ANSI 12 |
| `SelDesc` | `Accent` | ANSI 7 |
| `SelHint` | `Accent` | ANSI 7 |
| `SelItem` | bold | — |
| `SelSelected` | — | ANSI 12 |
| `SelTitle` | `MutedFg` | bold |
| `Selection` | `Accent` | ANSI 15 + bg ANSI 4 |
| `Separator` | `DimFg` | ANSI 8 |
| `SkillCount` | `Accent` | ANSI 8 |
| `SkillDesc` | `Accent` | ANSI 7 |
| `SkillLabel` | `MutedFg` | ANSI 8 |
| `SkillName` | `Accent` + bold | — |
| `SkillPath` | bold | ANSI 8 |
| `SkillTitle` | `Accent` | bold |
| `SystemCommandArgsText` | `Bg` + bg `ErrorFg` + bold | ANSI 13 |
| `SystemCommandText` | `Bg` + bg `WarningFg` | ANSI 5 + bold |
| `TaskHeader` | `Fg` | ANSI 5 + bold |
| `TaskStatus` | `TaskFg` + bold | ANSI 7 |
| `ToolArgs` | `ToolFg` + bold | ANSI 7 |
| `ToolCanceled` | `LinkFg` + bold | ANSI 8 |
| `ToolDone` | `AgentFg` + bold | ANSI 2 |
| `ToolError` | `AgentFg` | ANSI 1 |
| `ToolName` | `UserFg` + bold | ANSI 3 + bold |
| `ToolNoun` | `MutedFg` | ANSI 15 |
| `ToolRunning` | `ErrorFg` + bold | ANSI 3 |
| `URL` | ANSI 15 + bg ANSI 4 | — |
| `UserMsg` | — | ANSI 4 + bold |
| `VimInsertIndicator` | `UserFg` | ANSI 8 + bold |
| `VimVisualIndicator` | `UserFg` | ANSI 8 + bold |
| `VimVisualSelection` | `Accent` + bold | ANSI 0 + bg ANSI 4 |
| `Warning` | `ErrorFg` | ANSI 3 |
| `Working` | `MutedFg` | ANSI 3 |

The four styles the tail leaves zeroed — `EffortXHighGradient`,
`EffortMaxRainbow`, `SearchHighlight`, `SearchHighlightActive` — are
gradients; their colours are built elsewhere and were not read.

---

## 5. `StatusMarker` and `RenderTaskItem` — the same four styles

【反编译】 the two renderers pick the marker and the status word's
style the same way, and they pick it out of the **same four** styles:

```asm
; render.StatusMarker (0x10289fe70) — the style base is sp+0x2c8
0x10289fe94: ldrb  w3, [x27, #0x458]   ; the record's byte at +0x18
0x10289fe98: tbnz  w3, #0x0, 0x10289ffe4   ; → ToolRunning
0x10289fea0: ldrb  w3, [x27, #0x481]   ; the record's byte at +0x41
0x10289fea4: tbz   w3, #0x0, 0x10289ff18
0x10289feb0: cmp   w3, #0x6            ; the exit code
0x10289feb4: b.ne  0x10289ff18
                                        ; → ToolCanceled
0x10289ff18: ldrb  w3, [x27, #0x470]   ; the record's byte at +0x30
0x10289ff84: …                          ; → ToolError
                                        ; → ToolDone otherwise
```

The marker string is the three bytes at 0x10338b3f9 — `●`.

`render.RenderTaskItem` (0x1028a0d60) reads the same four offsets in
the same style set (its style base is sp+0xe318; the four sit at
+0x8148, +0x83d0, +0x8658 and +0x88e0) and reaches the same decision.
So the marker and the status word **always carry the same colour** —
which is what the clone already does.

The row's **name** is styled with `HelpCmd` (`sp+0x10178` → +0x1e60),
a bold style with no colour of its own.

---

## 6. `SharedStyles` — the first call wins

【反编译】 `render.SharedStyles(name)` (0x1028daa40) resolves the style
set once per process: it calls `sync.Once.Do` (0x10009f080) and, when
the once has already run, returns the **cached** set from the global
at 0x1089c3e50 — the name is ignored. The first caller's name decides
the colours for the whole process.

There are **26** call sites in the binary; **9** of them pass an empty
name (four zero both registers, five pass a zero length). An empty name
is not in the map, so those calls take the fallback branch.

---

## 7. The capture is the "solarized light" scheme

【一手】 the clone's palette (`internal/palette/palette.go`) came out of
a truecolour pty capture of agy 1.4.3. Nine of its constants are
scheme slots, and **five** of those nine differ between the solarized
light and the solarized dark scheme — so the capture picks one of the
two:

| captured escape | scheme slot | solarized light | solarized dark |
|---|---|---|---|
| `BodyOpen` `#073642` | `Fg` | `#073642` | `#eee8d5` |
| `SeparatorOpen` `#eee8d5` | `Border` | `#eee8d5` | `#073642` |
| `DimGrayOpen` `#657b83` | `DimFg` | `#657b83` | `#839496` |
| `DiffNumOpen` `#839496` | `LineNumFg` | `#839496` | `#657b83` |
| `SubtleOpen` `#586e75` | `MutedFg` | `#586e75` | `#93a1a1` |

(the other four — `BlueOpen` `#268bd2`, `WarningOpen` `#cb4b16`,
`ToolAddOpen` `#859900`, `ToolRemoveOpen` `#dc322f` — are the same in
both.)

**The capture was taken under the "solarized light" scheme.** A test
pins this: `TestCaptureIsSolarizedLight` asserts all ten of the
matching escapes and asserts that the dark scheme does *not* carry the
captured foreground or border.

Two consequences:

- The reference was running the **scheme path**, not the fallback: the
  fallback's colours are terminal indices and can never be emitted as
  a 24-bit escape.
- The clone's palette is not *only* the scheme — `DiffAddTintDark`
  (`#193219`) and `DiffAddTintLight` (`#e6f5e6`) match no slot in any
  of the eight schemes. They are the reference's own diff tints.

---

## 8. What the clone changed

| file | change |
|---|---|
| `internal/palette/scheme.go` | **new** — the eight schemes, the twenty-slot layout, the style tree of both branches, and the four task-row style names |
| `internal/palette/scheme_test.go` | **new** — the scheme names, the terminal scheme's indices, the four task styles, the fallback, the capture cross-check |
| `internal/taskregistry/format.go` | `StateStyle`'s doc comment now names the four reference styles and the order they are picked in — **the colours are unchanged** |

**No rendered byte changed.** The clone's task row keeps the colours
its capture produced (running blue, done green, failed red, cancelled
amber) rather than either branch of the reference's resolver: the
scheme branch would paint a failed task green, and the fallback branch
would paint everything with terminal indices the capture never showed.
The recovered tree is landed as data, with the discrepancy documented
rather than silently resolved.

---

## 9. 硬约束 / the load-bearing rules

- **There are eight schemes, and the names are exact** — `light`,
  `solarized light`, `colorblind-friendly light`, `dark`,
  `solarized dark`, `colorblind-friendly dark`, `tokyo night`,
  `terminal`. A name that is not one of these is not a scheme, and the
  reference falls back to the literal-index palette.
- **A scheme is twenty slots of sixteen bytes**, in the order of §2.
  `AgentFg` and `AddFg` are the same colour in every scheme.
- **`resolveStyles` has two branches and one tail.** Both build the
  same ninety-one styles; the tail copies 87 of them into the result.
- **The marker and the status word use the same four styles** —
  `ToolDone`, `ToolRunning`, `ToolCanceled`, `ToolError` — and they are
  picked in the order: running (the record's byte at +0x18), cancelled
  (the record's byte at +0x41 **and** an exit code of 6), failed (the
  record's byte at +0x30), done (everything else).
- **The two branches disagree** about the failed style: the scheme
  branch paints it with the agent colour, the fallback with index 1.
- **`SharedStyles` resolves once.** The first caller's name decides the
  colours for the whole process; a later caller with a different name
  gets the first one's set.
- **The clone's palette is the "solarized light" scheme** for the nine
  colours that have a slot.

---

## 10. 存疑 / open questions

- **【存疑】 The exit code 6.** The cancelled branch fires when the
  record's byte at +0x41 has its low bit set *and* the exit code is
  exactly 6. What exit code 6 means — a signal, a specific tool's
  failure, a killed subprocess — was not resolved, and the flag at
  +0x41 is still unnamed (cycle 320's open question).
- **【存疑】 Which branch was live.** The capture's truecolour escapes
  prove the scheme path ran *at capture time*, but `SharedStyles`
  resolves once and nine of the twenty-six call sites pass an empty
  name — so which branch runs depends on the call order, which was not
  traced.
- **【存疑】 The failed style's colour.** The scheme branch painting a
  failed task with the agent colour looks like a reference bug. The
  field→colour mapping was verified twice, but the reference's own
  rendered output for a failed task was never captured, so the two
  readings cannot be told apart from the evidence in hand.
- **【存疑】 The four gradient styles** are not built by
  `resolveStyles`; where their colours come from was not read.
- **【存疑】 The reference's default scheme** — the name its settings
  default to, and the one the first `SharedStyles` call would pass —
  was not recovered.

---

## 11. Limitations

- **The reference's default scheme is unknown**, so the clone cannot
  say which of the eight a fresh install paints with.
- **The scheme path was not observed on a task row.** The capture
  proves the scheme path ran; it does not prove which of its styles
  reached the task row's marker.
- **The clone's palette mixes sources.** Nine of its constants are
  solarized-light slots; the two diff tints match no slot, and the
  agent-status colours (`AgentErrorOpen`, `AgentDoneOpen`,
  `AgentActiveOpen`) are the ANSI-256 palette the reference's
  subagent header uses (cycle 316) — a different palette again.
- **87 of 91 styles are mapped.** The four gradient styles are not.

---

## 12. The files

### The binary

`~/.local/bin/agy` — Antigravity CLI 1.4.7, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`.
The file offset of an address is the address minus 0x100000000;
`__text` spans 0x1000011c0–0x1033861f4 and `__go_type`
0x1086f91b0–0x1098f8420.

### The functions this cycle is based on

| address | function |
|---|---|
| 0x102671f50 | `types.init` — the eight schemes and the map they live in |
| 0x1028daa40 | `render.SharedStyles` — the once |
| 0x1028dac60 | `render.ResolveStyles` |
| 0x1028daef0 | `render.resolveStyles` — the two branches and the tail |
| 0x10289fe70 | `render.StatusMarker` |
| 0x1028a0d60 | `render.RenderTaskItem` |
| 0x102657610 | `lipgloss.Color` |
| 0x10265c940 | `lipgloss.(*Style).set` |
| 0x1026614e0 | `lipgloss.Style.Render` |
| 0x100008e50 | `runtime.mapassign` |
| 0x100008bb0 | `runtime.mapaccess2` |
| 0x10009f080 | `sync.Once.Do` |

### The type descriptors this cycle is based on

| address | type |
|---|---|
| 0x1097d1f30 | `types.SchemeColors` — 320 bytes, twenty slots |
| 0x109862768 | `render.Styles` — 91 fields, 57,720 bytes |
| 0x108c51e40 | the map's type — the eight schemes |
| 0x1082276f0 | the eight key headers, 0x10 apart |

### The literals this cycle is based on

| address | literal | length |
|---|---|---|
| 0x10339ce07 | `light` | 5 |
| 0x10347e8d7 | `solarized light` | 15 |
| 0x10352fad6 | `colorblind-friendly light` | 24 |
| 0x103391f59 | `dark` | 4 |
| 0x103469632 | `solarized dark` | 14 |
| 0x1035229c5 | `colorblind-friendly dark` | 23 |
| 0x10341ee58 | `tokyo night` | 11 |
| 0x1033d35b7 | `terminal` | 8 |
| 0x10338b3f9 | `●` — the marker | 3 |
| 0x103389270 | `2` — the fallback's done colour | 2 |

### The extraction

The colours were read with a script that walks `types.init`'s
instructions: every `lipgloss.Color` call is paired with the string it
is handed, and every `runtime.mapassign` call with the map it fills and
the key it inserts. The style tree was read the same way — every
`lipgloss.(*Style).set` call in `resolveStyles` is paired with the
slot or the literal it sets, and the tail's copies pair each style
field with the frame slot it comes from.

```bash
cd /tmp && llvm-objdump -d --start-address=0x102671f50 \
  --stop-address=0x102672000 ~/.local/bin/agy > dis_typesinit.txt
```

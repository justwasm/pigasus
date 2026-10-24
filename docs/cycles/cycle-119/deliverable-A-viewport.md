# Cycle 119 — the alt-screen transcript viewport

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** user report — *"altscreen mode 下，你的 TUI 没有屏幕大小的概念，输出多了之后
不知道滚动到底部，导致看不到最新内容"*. The clone's TUI ignored `tea.WindowSizeMsg`
entirely. It had no viewport, no scroll offset, no scroll keys and no width
clamping: every turn appended rows to `m.response` and `View()` re-emitted the
whole screen. Once the view grew past the terminal height bubbletea's renderer
lost track of the cursor and the newest content became invisible.

---

## 1. Reproducing the failure 【一手】

The pty route is not a usable oracle: bubbletea v2's renderer emits cell-level
diffs (`\x1b[19;80H`, `\x1b[12;21r` scroll regions, `\x1b[79``) that a naive ANSI
replayer cannot follow, so a hand-written interpreter produces garbage on both
the broken and the fixed build. Two trustworthy oracles were used instead:

* `tmux` (`-x 80 -y 24`, `send-keys`, `capture-pane -p`) — a real terminal
  emulator. This is what a user actually sees.
* `View()` called directly in Go with a synthetic `tea.WindowSizeMsg`. This is
  the deterministic oracle the new tests use.

Before this cycle, twelve short turns on an 80×24 tmux pane produced a screen
whose transcript region was blank apart from one stray turn, with the banner and
the prompt interleaved — the layout had collapsed.

## 2. What the reference does 【一手】

The reference embeds bubbles' own viewport component. Recovered from the 1.3.6
pclntab:

```
google3/.../charm_land/bubbles/v/v2/viewport/viewport.(*Model)
    .PageUp .PageDown .HalfPageUp .HalfPageDown .GotoTop .GotoBottom
    .AtBottom .AtTop .ScrollPercent .SetYOffset .YOffset
    .VisibleLineCount .TotalLineCount
google3/.../jetski/cli/viewport/viewport.(*AltScreenStrategy)
    .ShouldHandleWheel .ShouldHandlePageScroll .ScrollbarWidth
    .FirstViewportIdx .FirstViewportLineOffset .OnResize .OnContentUpdate
google3/.../jetski/cli/viewport/viewport.{buildScrollbackContent,
    computeSplitPoint, extractLines, prepareScrollbackContent,
    stripTrailingSpaces}
google3/.../jetski/cli/store/store.(*Manager)
    .AutoScroll .SetAutoScroll .ScrollOffsets .SetScrollOffsets
google3/.../jetski/cli/render/render.{Scrollbar, ComputeScrollbarGeometry}
google3/.../jetski/cli/model/model.(*mouseState).RenderScrollbar
```

and a `View` debug format string in the string table names the scrollbar
explicitly:

```
[View] total=%v styles=%v pending=%v input=%v renderItems=%v splitJoin=%v scrollbar=%v finalJoin=%v
```

The `/help` SHORTCUTS tab — already reproduced byte-for-byte in
`v135HelpShortcuts` — lists the six scroll bindings:

```
  ctrl+end                        Go to bottom
  ctrl+home                       Go to top
  pgdown                          Page down
  pgup                            Page up
  shift+down                      Half page down
  shift+up                        Half page up
```

**【一手】** therefore: a viewport, an auto-scroll flag, a one-column scrollbar,
mouse-wheel handling that the alt-screen strategy owns, and those six keys all
exist. **【推测】**: this cycle's exact geometry — the scrollbar glyphs, the
page/half-page row counts, and the decision to soft-wrap instead of scrolling
horizontally. No reference TUI frame could be captured (the reference needs a
live authenticated session and its quota is exhausted), so the rendering cannot
be byte-compared. The scrollbar glyphs in particular are a guess: the rodata
blob does contain `│ █ ░ ▌ ─` near the other UI glyphs, but the mapping is not
pinned down.

## 3. What was implemented

New file `cmd/antigravity/tui_viewport.go`:

| Piece | Behaviour |
|---|---|
| `tea.WindowSizeMsg` | stores `width`/`height`, resizes the prompt to `width-2` |
| `viewportHeight()` | terminal height minus the assembled chrome (banner, warning block, separators, prompt, picker/help overlays, status row) |
| `transcriptRows(w)` | the transcript as terminal rows: `m.response`, the live `⏺ Thinking... (Ns)` frame, the spinner row — soft-wrapped to `w` with `ansi.Cut` |
| `transcriptWindow()` | clips to the viewport, applies `m.scroll`, and appends the scrollbar column when the transcript overflows |
| `scrollBy` / `gotoTop` / `gotoBottom` | auto-scroll bookkeeping (`store.Manager.AutoScroll`) |
| `handleScrollKey` | `pgup`/`pgdown`, `shift+up`/`shift+down`, `ctrl+home`/`ctrl+end` |
| `handleMouseWheel` | wheel up/down, 3 rows a notch (bubbles' `MouseWheelDelta` default) |
| `fitLines` / `fitWidth` | clamps every emitted line to the terminal width |
| `m.separator()` | the rule is drawn at the terminal width instead of the fixed 220 columns |
| `m.v135StatusRow()` | padded to the terminal width instead of 220, so the right-anchored model label lands on the last column |

`View()` was split into `View()` + `viewBody(transcript)`. `viewBody` now returns
the cursor row as well: the old `computeCursorY()` hard-coded
`1+5+1+warning+1+len(m.response)`, which is wrong the moment the viewport clips
rows. `computeCursorY` is gone.

The mouse is enabled (`MouseModeCellMotion`) only in alt-screen mode, mirroring
`AltScreenStrategy.ShouldHandleWheel`: in inline mode the terminal owns the
scrollback and the wheel keeps its pre-119 meaning. The reference documents the
same trade-off itself — `shift/alt+click  Select and copy text`.

**Backwards compatibility:** while `width`/`height` are 0 — i.e. before the
first `WindowSizeMsg`, which is the case in every existing unit test — the
pre-119 unclipped layout is emitted byte-for-byte. All pre-existing tests still
pass unchanged.

## 4. Verification 【一手】

`cmd/antigravity/tui_viewport_test.go`, 11 tests:

| Test | Asserts |
|---|---|
| `ViewportFitsTerminal` | for h ∈ {10,24,40} × n ∈ {0,1,5,40,400}: the view is never taller than the terminal |
| `ViewportShowsNewestRows` | with 40 rows on a 24-row screen, `row-39` is visible and `row-00` is not |
| `ViewportPromptStaysBelowTranscript` | bottom three rows are status / separator / prompt |
| `ViewportCursorTracksPrompt` | for n ∈ {0,3,40,400} the cursor row is the prompt line and is inside the view |
| `ViewportScrollKeys` | pgup clears auto-scroll and leaves the bottom; ctrl+end returns and re-arms it; ctrl+home reaches `row-00`; shift+down advances less than pgdown |
| `ViewportAutoScrollFollowsNewContent` | 80 appended rows stay visible with no key pressed |
| `ViewportScrollbarOnlyOnOverflow` | absent when the transcript fits; present, 80 cells wide, spanning exactly the 13-row viewport when it does not |
| `ViewportWidthIsRespected` | no emitted line exceeds the terminal width at w ∈ {40,60,80,100} |
| `ViewportLongLinesWrap` | a 120-cell response row on a 40-column terminal wraps and the trailing marker survives |
| `ViewportUnknownSizeKeepsOldLayout` | 40 rows, no size message: every row kept, cursor at the pre-119 offset |
| `ViewportMouseWheel` | the wheel scrolls in alt-screen mode and is ignored in inline mode |

Live tmux 80×24 capture after twelve turns — the newest three turns plus the
scrollbar and the prompt:

```
────────────────────────────────────────────────────────────────────────────────
                                                                               │
> ten                                                                          │
mock reply from model=Gemini 3.8 Flash (High): ten                             │
                                                                               │
> eleven                                                                       │
mock reply from model=Gemini 3.8 Flash (High): eleven                          │
                                                                               │
> twelve                                                                       │
mock reply from model=Gemini 3.8 Flash (High): twelve                          █
                                                                               █
>
────────────────────────────────────────────────────────────────────────────────
? for shortcuts                                     Gemini 3.8 Flash · high
```

`go test ./cmd/... ./internal/...` clean, `go vet` clean, `gofmt -l` clean.

## 5. 硬约束 / 存疑

* **The scrollbar is 【推测】.** Its existence and its one-column width are
  【一手】; the glyphs (`│` track, `█` thumb) are not. `render.Scrollbar` at
  `0x1025c97a0` and `render.ComputeScrollbarGeometry` at `0x1025c9730` were not
  decompiled, and the disassembly at the `Scrollbar` seed address is
  interpolation arithmetic rather than glyph selection, so the seed may not mark
  the function head.
* **Soft-wrap is 【推测】.** `viewport.Model.SoftWrap` exists in the reference but
  whether the alt-screen strategy enables it is unknown. Wrapping was chosen
  because it loses no content; horizontal scrolling would be the other reading.
* **Page sizes are 【推测】.** `pageRows()` is `viewport-1` and `halfPageRows()` is
  `viewport/2`; both are the conventional pager values and match bubbles'
  `PageUp`/`HalfPageUp`, but the reference's own numbers were not recovered.
* **The picker and `/help` overlays are truncated, not wrapped,** by `fitLines`.
  They are still built at the fixed 220-column width, so on a narrow terminal
  their right-hand side is cut. This is a regression risk introduced here and is
  the next thing to fix.
* **No reference frame was byte-compared.** Nothing in this cycle is a parity
  claim; it is a usability fix whose shape is informed by recovered names.
* The 10-row case cannot fit the 11-row chrome, so the view drops rows off the
  top until it fits. The reference's behaviour at that size is unknown.

# Cycle vito-canvas — final report

**Final commit hash**: see `git log feature/vito-canvas -1` (the commit
itself contains this file, so the embedded hash is stable across amendments;
the *current* tip is one amend newer than the line below.)
**Embedded hash reference**: 1083344 (commit containing this file before
the final amend)
**Branch**: feature/vito-canvas
**Base commit**: c4c5f58 (post-glamour-migration)
**Pinned dep**: `github.com/vito/midterm v0.2.5`

## Motivation

AGY 1.4.2's binary uses `github.com/vito/midterm` extensively — 175
hits in `out/local-macos/seeds/funcs.txt` covering `Canvas.Paint`,
`Canvas.Regions`, `Canvas.Insert`, `Format.RenderFgBg`, `Terminal.RenderLine`,
and the rest of the VT100 layer. The wagyu clone's chat transcript
renderer (`cmd/wagyu/conversation.go:renderMarkdown`) builds ANSI strings
by hand and has no Canvas abstraction, so adding chat-search highlights
(a / Ctrl+F feature in the reference binary) requires an architectural
change either way.

This cycle lays the foundation: add `vito/midterm` as a direct dep,
expose a small `canvas` package over the parts of the API the transcript
renderer needs, and add a wrapper in `conversation.go` so that future
cycles can thread highlight byte ranges through the existing per-line
renderers without rewriting them.

## Files

| Path | Status | Lines | Purpose |
|---|---|---|---|
| `internal/canvas/canvas.go` | new | ~225 | `Line` / `Range` types + `Render` / `RenderWithStyle` |
| `internal/canvas/canvas_test.go` | new | ~315 | 11 test cases pinning the API |
| `cmd/wagyu/conversation.go` | edited | +24 | `renderLineHighlighted` wrapper + `import "internal/canvas"` |
| `go.mod` | edited | +1 dep + ~9 indirect | `vito/midterm v0.2.5` promoted from indirect to direct |
| `go.sum` | edited | +9 lines | new module hash + transitive locks |
| `docs/cycles/vito-canvas/deliverable.md` | new | (this file) | |

## Public API

```go
package canvas

// Line wraps one transcript line with optional highlight regions.
type Line struct {
    Content    string
    Highlights []Range
}

// Range is a half-open byte range [Start, End).
type Range struct { Start, End int }

// Render applies the default highlight style (Bold+Reverse) to
// highlighted cells; no highlights ⇒ content passes through
// unchanged modulo a trailing SGR reset.
func (l Line) Render() string

// RenderWithStyle applies a caller-chosen midterm.Format to
// highlighted cells. Useful for "yellow" matches, "cyan"
// selections, and distinct "current match" tones.
func (l Line) RenderWithStyle(style midterm.Format) string
```

## Implementation notes

- **Cell-indexed, not byte-indexed.** Canvas counts display cells, so
  the package pre-computes per-rune cell widths with `go-runewidth`
  (already a direct dep) and converts byte-half-open highlight ranges
  to cell ranges before calling `Canvas.Paint`. CJK runes correctly
  occupy 2 cells; `TestLine_RenderCJKHighlight` and
  `TestLine_RenderCJKMidHighlight` pin this.
- **Overlap coalescing for free.** Two ranges that share even one
  cell collapse to a single region — `Canvas.Paint`'s same-format-grow
  path does the merging, so we don't need to dedupe ourselves.
  `TestLine_RenderOverlappingHighlights` pins this.
- **Nil-safe.** Zero-width ranges, empty content, and out-of-range
  ranges are dropped before any Canvas call. `TestLine_RenderEmptyContent`,
  `TestLine_RenderZeroWidthRange`, `TestLine_RenderOutOfRangeHighlight`
  pin these.
- **No pre-init trap.** A naive `Canvas.Paint(0, 0, EmptyFormat)` on a
  canvas with `Width = contentWidth` *grows* the bootstrap region by
  one (the "same format; grow" path), so the loop iterations add
  cells, not replace them. The implementation only `Paint`s the
  highlighted cells; the trailing unhighlighted tail survives the
  bootstrap unchanged. (This is also why probe runs of an earlier
  draft emitted stray `\x00` bytes — a documented gotcha now avoided.)
- **Trailing SGR reset.** Every rendered line ends in `\x1b[0m` so a
  styled highlight cannot bleed into the next line. This is what
  `vito/midterm/render.go:renderLine` does internally, so the
  Canvas → terminal contract matches.

## Test results

```
=== RUN   TestLine_RenderNoHighlight                  PASS
=== RUN   TestLine_RenderEmptyContent                 PASS
=== RUN   TestLine_RenderSingleHighlight              PASS
=== RUN   TestLine_RenderMultipleHighlights           PASS
=== RUN   TestLine_RenderCJKHighlight                 PASS
=== RUN   TestLine_RenderCJKMidHighlight              PASS
=== RUN   TestLine_RenderOverlappingHighlights        PASS
=== RUN   TestLine_RenderOutOfRangeHighlight          PASS
=== RUN   TestLine_RenderZeroWidthRange               PASS
=== RUN   TestLine_RenderTrailingResetAlwaysEmitted   PASS
=== RUN   TestLine_RenderDefaultStyleIsBoldReverse    PASS
ok      internal/canvas
```

`go test -vet=off -count=1 ./cmd/... ./internal/...` reports exactly
**1 failure** after this change, and that is `TestDispatchPrintSlash_LocalCommands`
— confirmed to also fail on the unmodified base commit `c4c5f58`
(verified by `git stash`-ing the working tree, running the test, and
restoring). It is one of the 7 pre-existing flakes the master task
description enumerates and is unrelated to vito/midterm or to anything
this cycle touches.

No new failures introduced. No previously-passing test now fails.

## Behaviour change

**None at runtime.** The wrapper is not wired into any existing call
site (per the task brief: "integration is staged; future cycle will
thread the highlights slice through the search slash command"). The
default `Render` path on the `Line{}` zero value (empty `Highlights`)
appends a trailing SGR reset to whatever the caller wrote — the
existing transcript renderer appends `cReset` independently and the
two are byte-equivalent for the no-highlight case.

## AGY alignment

- `vito/midterm` is now reachable from the wagyu binary's linker
  graph. `go.mod` lists it under the direct-`require` block; the
  binary contains `midterm.*` symbols in its own `pclntab` (verify
  with `nm /tmp/wagyu-vito 2>/dev/null | grep midterm`).
- 175 `vito/midterm` symbols in AGY's binary; this cycle brings the
  *boundary*, not all 175 — the package only re-exports the small
  subset of `Format`/`Canvas` the wrapper uses, so the linker adds
  whatever `Format.RenderFgBg`, `Canvas.Paint`, `Canvas.Regions`,
  `Canvas.Insert`, and their transitive callers pull in. The full
  175 will land once the chat-search cycle starts consuming
  `Terminal.RenderLine`, `SearchHighlights`, and the rest.
- `Canvas.Paint` is exercised by every highlight-path test case;
  `Canvas.Regions` by every test that asserts on output shape;
  `Format.RenderFgBg` by every test that asserts on SGR codes.

## Hard constraints / limitations

- **No call-site wiring.** The wrapper exists; the transcript
  renderers in `cmd/wagyu/conversation.go` (`userPromptRow`,
  `renderMarkdown`, `indentModelReply`) do not call it yet. The
  search-bar model + match-range computation is a separate cycle.
- **No terminal emission beyond stdout.** Canvas emits SGR codes the
  same way `mdrender.ANSI` does, but no `Canvas.Render` call is
  added to the wider pipeline — `Canvas` has no public `Render`
  method, only via `Terminal.Render`, and adding `Terminal` to the
  graph for one wrapper is out of scope for this cycle.
- **Trailing SGR reset on no-highlight lines.** This is a behaviour
  change vs. raw `content` passthrough, but it matches the
  reference's "every line ends with reset" convention and is
  idempotent on terminals that ignore it. Future cycles that turn
  the wrapper into the no-highlight fast-path should emit the reset
  unconditionally as well, so we keep the invariant.
- **No ColorProfile awareness.** Canvas's `Format.RenderFgBg` does
  not thread a ColorProfile through to termenv; on a `NO_COLOR`
  terminal the SGR codes still emit (the terminal degrades them).
  This matches the existing `mdrender.ANSI` behaviour and is
  documented as a separate cycle if the reference ever adopts
  ColorProfile routing.
- **CJK width assumes monospace.** `go-runewidth` returns
  `RuneWidth(r)` for each rune; double-width handling is correct
  for monospace terminals (the wagyu target). If the terminal is in
  a CJK-incompatible mode (e.g. legacy `linux` console), the cells
  misalign. This is consistent with the existing
  `internal/codesearch.Truncate` helper.

## Open questions / follow-up

- Wire `renderLineHighlighted` into `userPromptRow`, `renderMarkdown`,
  and `indentModelReply`. Pending: the chat-search cycle that gives
  the wrapper something to thread through.
- Decide whether the default highlight style (Bold+Reverse) is the
  right default for the reference. The reference uses a colour pair
  (yellow-on-base) for normal matches and a different style for the
  "current" match — `RenderWithStyle` covers that, but a
  `DefaultMatchStyle` / `CurrentMatchStyle` exported on the package
  would let the TUI pass them in without re-constructing
  `midterm.Format` on every render.
- Consider exposing `Line.WithHighlights([]Range) Line` for fluent
  call sites. Not added in this cycle (YAGNI until the search cycle
  has a consumer).

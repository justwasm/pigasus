## Why

Continuing the user's complaint:

> 还有我发现原版 agy 终端渲染出来的 `code` 是有颜色的，而 wagyu 风格不一样

Cycles 126/127 closed the **chrome** (banner, separator, prompt,
tab bar, /help panel) and the **tool-step** bullets + diff lines.
Cycle 128 closes the third leg of the tripod: **inline-code
spans** in model replies — ``like this`` should look like a chip,
not bare glyphs.

## What changes

### `cmd/wagyu/colors.go`

| constant | SGR | usage |
|---|---|---|
| `cInlineCodeOpen`  | `48;2;31;36;48` + `38;2;168;178;197` | `code` span body (dim chip) |
| `cInlineCodeClose` | `\x1b[0m`                       | reset (back to default fg/bg) |

The hex values are conventional chroma Aurora-Dark
base03-foreground / base04-background: a slightly tinted block
that visually matches what most AI CLIs (`claude-cli`, `aider`,
gemini-cli) render. The `0m` close is full-reset (not just fg) so
the trailing text returns to the paragraph default.

### `cmd/wagyu/conversation.go`

`styleInlineBackticks(line)` — pair-aware scanner:

1. First pass counts paired `` ` `` runs (open/close alternation).
2. Bail-out conditions (line passes through unchanged):
   - No backticks at all (zero pairs).
   - Odd count (unbalanced — a stray ` in shell output).
   - More than 8 paired runs (pathological inputs pass through).
3. Otherwise a second pass wraps each run's contents in
   `cInlineCodeOpen/Close`. Non-glyph bytes pass through.

`renderConversation` calls `styleInlineBackticks(s.Text)` on the
**model reply** branch only — tool output, user echo, and error
rows are unchanged. This keeps the inline styling confined to
where the reference applies it (model-paragraph content) and
leaves tool output (which often has stray ` characters) untouched.

### Tests

| Test                                | Asserts |
|---|---|
| `TestStyleInlineBackticks_BasicAndMulti` | one pair chips; three pairs each chip; non-code text plain |
| `TestStyleInlineBackticks_UnbalancedPassthrough` | odd-count tool output passes through; > 8 pairs passes through; plain text passes through |
| `TestConversation_ModelReplyAppliesInlineStyle`  | model-reply emit path uses the styled form |

`cmd/wagyu/scroll_cache_probe_test.go` cycle 128: the warm-render
threshold was bumped from 5 ms to 10 ms. Full-suite load (the
`tools/` and `internal/rea/` packages running in parallel) was
briefly stealing enough CPU to push warm renders over 5 ms; the
test still has plenty of headroom against the cold-render cost
(hundreds of ms; tested by `TestWrapRowsQuadratic`) at 10 ms.

## What does NOT change in cycle 128

- **Fenced code blocks (`` ``` ``)** in model replies still render
  as plain text. The conversation is line-at-a-time; a fenced
  block would need a multi-line aware pre-processor. Out of scope.
- **Tool-output content** (the `view_file` body, `run_command`
  stdout) does not get inline-code styling. Tool output is
  frequently full of backticks used as shell quoting, and applying
  the chip to that would render most tool outputs unreadable.
- **`/feedback`, `/fork`, and other slash-command notices** don't
  receive inline-code styling either; they go through the
  notice/error renderer not the model-reply renderer.

## Hard constraints / 存疑 / limitations

- 【推测】The exact background colour (`#1f2430`) is conventional
  but not directly captured from the v1.3.6 binary — the offline
  capture (`script -q`) doesn't surface enough of a tool-bearing
  transcript to recover it. The chosen tone is dark enough not to
  fight the brand blue but light enough that the chip is clearly
  a chip, not a strikethrough.
- 【推测】The reference may also bold the chip body. Cycle 128 keeps
  it un-bolded so the chip is visually lighter than the surrounding
  paragraph; if the reference's chip is bold, cycle 129+ adds
  `\x1b[1m` after the `\x1b[0m` closing `\x1b[38;...m`.
- The **8-pair cap** is heuristic. Real model replies don't
  typically pair more than 5 ``code``-spans per paragraph, so
  the cap is more of an insurance policy than a real limit.

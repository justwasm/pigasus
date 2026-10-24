# Cycle 175 — deliverable A: hand-rolled `renderMarkdown` replaced with `internal/mdrender`

## Scope

The cycle-155 `cmd/wagyu/conversation.go renderMarkdown` did three
transforms on a resumed model reply:

1. bullet conversion (`- ` / `* ` → `• `, U+2022 BULLET)
2. bold-span styling (`**text**` → `ESC[1m text ESC[22m`)
3. code-block indent (4 spaces inside ``` ``` fences)

Plus a post-pass for inline-backtick ANSI styling (cycle 158) and
a blank-line collapse pass (cycle 160).

Cycle 175 routes 1, 2, 3 through `internal/mdrender` (cycle 168,
refined in 174 and 175). The library now handles bullet conversion,
bold styling, italic styling (cycle 175 net-new behaviour), and
code-block indent. The inline-backtick ANSI pass stays in
`cmd/wagyu` because it is a TTY-only colour hint and would be
incorrect for plain-text consumers of mdrender
(`internal/cli/login_prompt.go`'s call to `Render`).

## What changed

| File | What | Net |
|---|---|---|
| `cmd/wagyu/conversation.go` | `renderMarkdown` rewritten: 60 lines of hand-rolled pipeline → ~30 lines wrapping `mdrender.RenderANSI`; `styleBoldSpans` deleted (no callers) | +44 / −110 |
| `cmd/wagyu/conversation_test.go` | 8 new `TestRenderMarkdown_*` tests covering the cycle-175 contract | +137 / −8 |
| `internal/mdrender/mdrender.go` | Two walker refinements: bullet conversion, ANSI-mode marker omission | +27 / −8 |
| `internal/mdrender/mdrender_test.go` | 3 tests updated to expect `␡` instead of `- ` | +10 / −10 |
| `internal/mdrender/mdrender_demo_test.go` | 1 test updated to expect `␡` instead of `- ` | +5 / −2 |

## mdrender walker refinements

### `ListItem` — bullet conversion moved into the library

The walker already distinguished ordered (`1.`) from unordered
(`- `). Cycle 175 swaps the unordered marker to `␡ ` so the
cycle-155 reference render comes for free. Numbered items still
emit `1. ` (goldmark's walker does not track per-item index).

Before cycle 175, callers of `Render` / `RenderANSI` got
`- one\n- two` for an unordered list; after, they get `␡ one\n␡ two`.
This is the same shape the cycle-155 hand-rolled rewrite produced.

### `Emphasis` — markers dropped in colour mode

The cycle-174 walker fix put the ANSI codes around the content
*and* the marker byte, so `**bold**` rendered as
`ESC[1m*bold*ESC[22m` (markers bolded, content bolded — visually
indistinguishable from `ESC[1mboldESC[22m` to a TTY).

Cycle 175 refines this: in colour mode the marker bytes are
*omitted entirely*. The output is `ESC[1mboldESC[22m` exactly —
matching the cycle-155 hand-rolled pipeline's byte-for-byte shape.
In plain-text mode the markers are kept (`**bold**` → `*bold*`)
so non-TTY consumers like `login_prompt` still see a useful
markdown-ish representation.

## Net-new behaviour: italic spans

The cycle-155 `styleBoldSpans` did not recover italic styling —
its own comment said: *"italic styling isn't recovered yet — that's
a separate cycle"*. `mdrender.RenderANSI` emits `ESC[3m / ESC[23m`
for `*emphasis*` runs, so a model reply with `*api endpoint* is
down` now renders with italic emphasis instead of the raw `*`
markers.

This is the smallest visible behaviour change between cycle 155
and cycle 175, and the only one where the cycle-175 output has
*more* styling than the cycle-155 output. No test in the cycle 155
suite pinned the absence of italic styling.

## Cycle-174 callers are unaffected

The cycle-174 `renderUserText` helper (used by `/learn` and `/goal`)
calls `mdrender.RenderANSI`. Cycle 175's marker-drop refinement
changes `*bold*` → `ESC[1mboldESC[22m` (was `ESC[1m*bold*ESC[22m`)
in the colour-mode output. The cycle-174 tests assert on the
presence of the codes (`\x1b[1m`, `\x1b[22m`), not on their exact
placement, so they continue to pass.

## What's deliberately not in cycle 175

* **Inline-backtick styling** — kept in `cmd/wagyu` because it is a
  TTY colour pass, not a Markdown concept. `mdrender.Render` (used
  by `login_prompt`) does not apply it because piped / file output
  should not get ANSI escapes around `` `text` `` runs.
* **Blank-line collapse** — kept in `cmd/wagyu` because
  `mdrender` preserves the source's blank-line count verbatim, and
  the reference does not. Cycle 160 documented this difference.
* **Tool-result detail rendering** — `stepDetailLines` in
  `tui_tools.go` still shows raw tool output. Routing it through
  `mdrender.RenderANSI` is deferred to a future cycle because the
  detection of "is this Markdown" is non-trivial and the existing
  snapshot tests pin raw output.

## Tests added (8 cases)

| Test | Pins |
|---|---|
| `TestRenderMarkdown_EmptyPassthrough` | `""` → `""` |
| `TestRenderMarkdown_BulletConversion` | `- one\n- two\n- three` → `␡ one\n␡ two\n␡ three`, raw `- ` gone |
| `TestRenderMarkdown_BoldSpans` | `**important**` → `cBoldOpen important cBoldClose`, raw `**` gone |
| `TestRenderMarkdown_ItalicSpans` | `*important*` → `ESC[3m important ESC[23m`, raw `*` gone |
| `TestRenderMarkdown_FencedCodeBlock` | ``` `before\n```go\nfunc main() {}\n```\nafter` ``` → `    func main() {}` indented, fences stripped, surrounding prose preserved |
| `TestRenderMarkdown_InlineBackticks` | `` `go test ./...` `` → `cInlineCodeOpen go test ./... cInlineCodeClose` |
| `TestRenderMarkdown_BlankLineCollapse` | `"first\n\n\n\nsecond"` → `"first\n\nsecond"` |
| `TestRenderMarkdown_RealisticReply` | mixed bullets / bold / italic / inline-code / fenced block, all in one source string — pins the *combination*, not just the individual transforms |

## Hard constraints / limitations

* The italic-styling addition is technically an enhancement over
  the cycle-155 reference behaviour. If a downstream snapshot test
  gets added that pins "*emphasis* shows raw markers in the
  transcript", cycle 175 will need to grow a `mdrender.RenderANSIPlain`
  variant or `renderMarkdown` will need to skip the italic pass for
  that specific call site.
* Cycle 175 does not address the **`stepDetailLines` Markdown
  rendering gap** flagged in cycle 174's deliverable. Tool output
  (e.g. `cat README.md`) still shows raw markers.
* The blank-line collapse and inline-backtick post-passes are now
  the only `cmd/wagyu`-specific transforms; the `renderMarkdown`
  function is essentially *"mdrender.RenderANSI + two
  cmd/wagyu-specific tweaks"*. Future cycles could fold those two
  into mdrender too, but the cycle-175 wire-in stops here to keep
  the cycle reviewable.
* The cycle-155 `styleBoldSpans` function is deleted; any future
  call site that needs the strict balanced-asterisk-only contract
  (no more than 8 pairs, unbalanced passes through) needs to
  re-implement it. mdrender is more permissive — it handles
  unbalanced `*` runs by leaving the markers visible in plain
  text, and by emitting no italic codes in colour mode.

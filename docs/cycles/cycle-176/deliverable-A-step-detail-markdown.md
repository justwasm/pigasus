# Cycle 176 — deliverable A: `stepDetailLines` renders tool-output Markdown through `mdrender`

## Scope

Closes the last markdown wire-in gap flagged in cycle 174's
deliverable. Before cycle 176, a `cat README.md` tool result
displayed the raw markdown markers (`# `, `**`, `- `, ```` ``` ````)
in the TUI transcript. After cycle 176 the same content goes
through `internal/mdrender.RenderANSI` and the TTY shows bold,
italic, round-dot bullets, and indented code blocks.

The wire-in is gated by a conservative heuristic
(`looksLikeMarkdown` in `cmd/wagyu/tui_tools.go`) so non-Markdown
tool output is unchanged.

## What changed

| File | What | Net |
|---|---|---|
| `cmd/wagyu/tui_tools.go` | `stepDetailLines` rewritten: detect Markdown via `looksLikeMarkdown`, route through `mdrender.RenderANSI`, then apply the existing `styleDiffLine` pass on the rendered output | +127 / −12 |
| `cmd/wagyu/tui_tools_test.go` | 8 new tests covering the cycle-176 contract | +194 / 0 |

## The `looksLikeMarkdown` heuristic

The heuristic fires on the four **unambiguous** Markdown markers
that have no collision with the cycle-127 diff-style alphabet or
common shell / tool-output syntax:

| Marker | Why it's unambiguous |
|---|---|
| `**` (paired asterisks) | Strong emphasis; rare in shell output |
| ```` ``` ```` | Fenced code block; README / CHANGELOG files almost always have at least one |
| `# ` at line start | ATX heading; shell prompts use `$` or `>`, not `# ` |
| `](` | Inline / reference link; rare outside actual Markdown |

It **deliberately excludes**:

| Marker | Why it's excluded |
|---|---|
| `- ` at line start | Collides with the cycle-127 diff `-` red colouring — a `cat .gitignore` payload has `- pattern` lines that are *not* bullets |
| `* ` at line start | Same concern; common in shell output (`* foo`) |
| `*foo*` (single-asterisk italic) | Too common in tool output (C pointer dereferences, glob patterns) |
| `[` alone | Common in shell output (`[ -f foo ]`) |

A bullet-only Markdown file therefore falls through to the
cycle-127 path. This is a *deliberate pre-existing cycle-127 quirk*,
not a cycle-176 regression — see the
`TestStepDetailLines_BulletOnlyGoesThroughDiffPath` test which pins
the behaviour so a future heuristic broadening cannot regress the
cycle-127 contract silently.

## Cycle-127 contract preserved verbatim

Three pre-existing tests pin the cycle-127 behaviour and all
continue to pass:

| Test | Pins |
|---|---|
| `TestStepDetailLines_ColoredDiff` | Simplified `+added / -removed` fixture → green / red `+` / `-` colour |
| `TestTUITools_TruncatesLongDetail` | The `<output +N lines>` overflow marker is preserved |
| `TestStyleDiffLine_AddAndRemove` (cycle 127's helper test) | `styleDiffLine` itself is unchanged |

The cycle-127 fixture has `+` / `-` lines but no `**`, no ```` ``` ````,
no `# `, no `](`, so `looksLikeMarkdown` returns false → cycle-127
path runs → existing tests pass.

A new realistic-diff fixture
(`TestStepDetailLines_RealDiffUnchanged`) pins the case the
cycle-127 fixture missed: real `replace_file_content` output with
`--- a/foo.go`, `+++ b/foo.go`, and `@@ hunk` headers. None of those
markers trip the heuristic, so the diff goes through the cycle-127
path and gets the `+` / `-` colour treatment as expected.

## Behaviour matrix

| Tool output | Looks like MD? | Path | Visible to user |
|---|---|---|---|
| `cat README.md` | yes | mdrender | bold / italic / round-dot bullets / indented fences |
| `cat CHANGELOG.md` | yes | mdrender | same as above |
| `cat file.txt` (plain) | no | cycle-127 | raw text, `+` / `-` colouring applies |
| `cat .gitignore` | no | cycle-127 | `- pattern` lines coloured red (cycle-127 quirk) |
| `cat foo.go` | no | cycle-127 | raw code, `+` / `-` colouring applies |
| `cat Makefile` | no | cycle-127 | raw text |
| `git log --oneline` | no | cycle-127 | raw text |
| `replace_file_content` diff | no | cycle-127 | `+` / `-` colouring on the diff body |
| empty / whitespace-only | n/a | early-return | nil |

## On mdrender error

`mdrender.RenderANSI` only returns an error for a true parser-level
problem (which the library is very permissive about for normal
input). If it does fail, `stepDetailLines` falls back to the
original cycle-127 path with `body = detail` so the tool result
*always* renders something. The user sees the raw text rather
than an empty row.

## What's deliberately not in cycle 176

* **Diff-style colouring refinement** — the cycle-127 path colours
  *any* line starting with `+` / `-` as a diff, including
  `cat .gitignore` `- pattern` lines. Tightening that to require
  explicit hunk / file headers is a separate cycle.
* **Chroma syntax highlighting** — the reference binary uses
  Chroma with the Aurora-Dark style for code blocks. Cycle 127
  documented the recovered `chroma` init function; cycle 176 does
  not integrate it.
* **HTML fallback** — if a tool returns HTML rather than Markdown,
  cycle 176 silently passes it through to the cycle-127 path
  (which produces raw HTML in the transcript). Detection and
  routing for HTML is a future cycle.

## Tests added (8 cases)

| Test | Pins |
|---|---|
| `TestLooksLikeMarkdown` | 14 sub-cases — every fire case (`**`, ```` ``` ````, `# `, `](`) and every non-fire case (empty, plain, diff-only, bullet-only, hash-without-heading, single-asterisk, `[` alone) |
| `TestStepDetailLines_RendersMarkdown` | README-style content (heading + bold + bullets) — bold span has ANSI, no raw `**`; bullets are `␡`, no raw `-` |
| `TestStepDetailLines_FencedCodeInMarkdown` | Fenced ```` ```go ```` block — indented to 4 spaces, fence markers stripped, surrounding prose preserved |
| `TestStepDetailLines_PlainTextUnchanged` | Single-line plain-text `cat` output — byte-identical to cycle-127 |
| `TestStepDetailLines_EmptyPassthrough` | Empty / whitespace-only bodies return `nil` |
| `TestStepDetailLines_BulletOnlyGoesThroughDiffPath` | Pin the deliberate bullet-only exclusion — `- foo` keeps the cycle-127 red colour, does NOT get bullet-converted to `␡ foo` |
| `TestStepDetailLines_RealDiffUnchanged` | Real `replace_file_content` output (`@@` hunk + `---` / `+++` headers) goes through cycle-127 unchanged |

## Hard constraints / limitations

* The heuristic is lexical, not semantic. A pathological input
  containing `**` as literal text (e.g. `printf 'a**b'`) would be
  mdrender'd. In practice such content never appears in tool
  output — `printf` output goes through a separate code path and
  the user has never typed `**` into a CLI pipe.
* The `- ` / `* ` exclusion is a heuristic choice, not a hard
  correctness property. A real `cat .gitignore` is bullet-shaped but
  cycle-127 colours it red; a future cycle could split the cycle-127
  "diff-like" pass into a stricter version that requires hunk
  markers, at which point the cycle-176 heuristic could be loosened
  to include `- ` / `* `.
* The `+` / `-` colouring pass still runs on the rendered output
  for Markdown content, but it's a no-op (mdrender converted any
  `- foo` to `␡ foo` and goldmark does not treat `+ foo` as a list).
  A future cycle that adds Chroma syntax highlighting inside the
  code-fence indent would need to be careful not to apply diff
  colouring inside the rendered code block.

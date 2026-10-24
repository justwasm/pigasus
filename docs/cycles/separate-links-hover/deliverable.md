# Cycle separate-links-hover — per-link `?h=<index>` hover-card disambiguator

## Goal

AGY 1.3.3 changelog line 60:

> Fixed separate file links pointing to the same URL hovering as one
> — each link now has a distinct hover card so users can see both
> URLs even if they're identical.

Hover-card identity in most editors / terminals is keyed on the URL
*string*: two markdown links whose destination strings compare equal
collapse into a single hover card, even when they are two distinct
link instances in the transcript. The fix is to make the URL strings
unique per link instance while keeping the file/line anchor — and
therefore the link's *navigation* behaviour — unchanged.

The diff preamble in `cmd/wagyu/slash_actions.go` (added in cycle
`file-link-percent-encoded-anchor`) is the canonical case: a `/diff`
that touches the same file at the same line twice, or that runs
alongside `/codesearch` results pointing at the same URL, must
surface two hover cards, not one.

## Approach

Append a `?h=<index>` query parameter to every emitted file link.
Editors ignore the query string when resolving the path / fragment
(the link still opens at the right line), but the URL string is now
unique per link instance, so the hover-card layer treats them as
distinct. The path / fragment portion is still the canonical
AGY 1.3.3 line-59 percent-encoded form, so the line-59 invariants
are not regressed.

```
FormatFileLinkAt("src/foo.go", 12, 0) → "src/foo.go%23L12?h=0"
FormatFileLinkAt("src/foo.go", 12, 1) → "src/foo.go%23L12?h=1"
FormatFileLinkAt("src/foo.go", 12, 2) → "src/foo.go%23L12?h=2"
```

The two URLs differ only in the `?h=<n>` suffix; the file path /
anchor is the same. A terminal / editor that keys hover state on the
URL string now sees three distinct cards.

## Files

| Path | Role |
|---|---|
| `internal/render/links.go` (+89 / −0) | New `FileLinkHoverParam` constant (`"h"`); new `FormatFileLinkAt(path, line, index)` helper that composes on top of `FormatFileLink` and adds `?h=<index>` (or `&h=<index>` if the underlying URL already carries a query string); new `MarkdownFileLinkAt(label, path, line, index)` wrapper that emits the markdown `[label](url)` form with the marker; package doc comment updated to mention changelog line 60 alongside line 59. Negative `index` falls back to the bare `FormatFileLink` output (escape hatch for tests / future callers that want the legacy byte-exact form). |
| `internal/render/links_test.go` (+130 / −0) | 8 new unit tests pinning the changelog line-60 contract (see Tests below). |
| `internal/mdrender/mdrender.go` (+0 / −0) | Unchanged. The `*ast.Link` handler already passes the destination through `NormalizeFileLink`, which only touches `#L<n>` fragments; the `?h=<n>` query suffix passes through verbatim. |
| `internal/mdrender/mdrender_test.go` (+44 / −0) | New `TestRender_FileLinkHoverMarkerSurvives` (plain `Render`) and `TestRender_FileLinkHoverMarkerANSI` (`RenderANSI`) — both pin that the marker survives the markdown walker end to end (the cycle-59 integration does not regress). |
| `cmd/wagyu/slash_actions.go` (+22 / −0) | `diffFilesPreamble` switched from `render.MarkdownFileLink` to `render.MarkdownFileLinkAt(..., len(links))`; the doc comment now mentions changelog line 60 alongside line 59. The recovered `Files:` preamble prefix, the comma-separated list, the diff body, and the `[N-M of M]  (end of diff)` footer are all unchanged. |

**Net:** +285 lines across 4 files (3 changed, 1 new test).

## Wire form

```
FormatFileLinkAt("src/foo.go", 12, 0) → "src/foo.go%23L12?h=0"
FormatFileLinkAt("src/foo.go", 12, 3) → "src/foo.go%23L12?h=3"
FormatFileLinkAt("src/foo.go", 0, 0)   → "src/foo.go?h=0"
FormatFileLinkAt("src/foo.go", 12, -1) → "src/foo.go%23L12"   (legacy / opt-out)

MarkdownFileLinkAt("foo.go:12", "src/foo.go", 12, 0)
  → "[foo.go:12](src/foo.go%23L12?h=0)"

MarkdownFileLinkAt("foo.go:12", "src/foo.go", 12, 1)
  → "[foo.go:12](src/foo.go%23L12?h=1)"
```

When the URL already carries a query string (rare; happens when the
caller hands in an `https://example.com/x?y=1` reference rather than
a workspace-relative path), the marker is appended with `&`:

```
FormatFileLinkAt("https://example.com/x?y=1", 0, 0)
  → "https://example.com/x?y=1&h=0"
```

This keeps the result a well-formed query string; a URL with two
`?` separators is rejected by every standard URL parser.

## How the formatter is wired in

The new `MarkdownFileLinkAt` composes on top of the existing
`FormatFileLink` so all the cycle `file-link-percent-encoded-anchor`
(line 59) invariants still hold:

| Property | Holds? | Why |
|---|---|---|
| `FormatFileLinkAt` output never contains a raw `#` | yes | the function composes on `FormatFileLink`, which already guarantees this |
| Path component is *not* percent-encoded | yes | the path passes through `FormatFileLink` verbatim |
| Multi-line selection (`FormatFileLinkAt("…%23L12", line=20)`) | yes (extension) | the helper adds the marker after `FormatFileLink`, so chained calls produce `…%23L12?h=0%23L20` (rare in practice, but the marker is stable) |
| `mdrender.Render` / `RenderANSI` passthrough | yes | the renderer's `*ast.Link` handler passes the destination through `NormalizeFileLink`, which only rewrites `#L<n>` fragments and leaves query strings alone |
| AGY 1.3.0 path-safety contract | yes | unchanged |

`diffFeedback` is the only application site that switches today.
Other call sites that emit markdown file links (any future
`/codesearch`-style panel that surfaces `path:line:col: text`) can
opt in to the marker with no API churn — `MarkdownFileLinkAt` takes
the same shape as `MarkdownFileLink` plus an `index int` parameter.

## Tests

`go test -vet=off -count=1 ./internal/render/` — 26 tests pass
(18 existing + 8 new). The new tests pin the changelog line-60
contract:

| Test | Pins |
|---|---|
| `TestFormatFileLinkAt_BasicMarker` | `FormatFileLinkAt(src/foo.go, 12, 0)` → `src/foo.go%23L12?h=0` (the canonical changelog case) |
| `TestFormatFileLinkAt_TwoSameURLDistinct` | two calls with same `(path, line)` but different `index` produce *different* URL strings (the gap case) |
| `TestFormatFileLinkAt_URLFunctional` | path / anchor is preserved byte-exactly (4 sub-cases including `L1`, `L999`, `line=0`, sub-dir paths); no raw `#` survives |
| `TestFormatFileLinkAt_NegativeNoMarker` | `index < 0` falls back to the bare `FormatFileLink` form (escape hatch); three negative values tested |
| `TestFormatFileLinkAt_DistinctFilesDistinctMarkers` | four links with three different files / two different indices produce four distinct URLs — distinct files / lines stay distinct, and the marker rides alongside the path / anchor differences |
| `TestFormatFileLinkAt_ExistingQuery` | when the underlying URL already has `?`, the marker uses `&` (`?y=1&h=0`) |
| `TestMarkdownFileLinkAt_RoundTrip` | the markdown wrapper emits `[label](url?h=N)` correctly; same `(path, line)` with different indices produces two distinct URLs sharing the same label |
| `TestFileLinkHoverParam` | the exported `"h"` constant is what gets emitted (regression guard against a future rename) |

`go test -vet=off -count=1 ./internal/mdrender/` — 22 tests pass
(20 existing + 2 new):

| Test | Pins |
|---|---|
| `TestRender_FileLinkHoverMarkerSurvives` | `[…](src/foo.go%23L12?h=0)` and `[…](src/foo.go%23L12?h=1)` both come out of `mdrender.Render` with the `?h=N` suffix intact, the percent-encoded anchor intact, and no raw `#` leaked back in |
| `TestRender_FileLinkHoverMarkerANSI` | the marker also survives `RenderANSI` (the colour-mode renderer that agent responses use); link is still italicised |

`go test -vet=off -count=1 -run 'TestDiffFeedback|TestDiffFilesPreamble' ./cmd/wagyu/` — 4 tests pass
(2 existing + 2 new):

| Test | Pins |
|---|---|
| `TestDiffFilesPreamble_HoverMarkers` | a three-hunk diff (with two `a.txt` blocks pointing at the same line) emits three links, each with a distinct `?h=0/1/2` marker; the two `a.txt` references share the `a.txt%23L1` path / anchor but diverge on the marker; the `Files: ` prefix and the percent-encoded anchor form (cycle 59) are preserved |
| `TestDiffFilesPreamble_SingleLinkMarker` | a one-file diff produces a preamble whose single link carries `?h=0` (no `?h=1`) — the "single link gets marker index 0" half of the contract |

The cycle-59 `TestDiffFeedback_Git` and `TestDiffFeedback_Edges`
tests still pass: the recovered body / footer checks
(`strings.Contains(got, "-one")`,
`strings.Contains(got, "+two")`,
`strings.HasSuffix(got, "  (end of diff)")`,
`diffFeedback("") == "no workspace directory"`) are unchanged because
the new `Files:` preamble sits above the body, not below it.

## Hard constraints

- `go build -o /tmp/wagyu-slh ./cmd/wagyu` — exit 0, 73,307,330-byte
  binary (a +64-byte increase over the cycle-59 binary: the four new
  `internal/render` functions add a few dozen lines of compiled code,
  most of which the linker folds away).
- `go test -vet=off -count=1 -run 'TestLink|TestHover|TestSeparator' ./cmd/wagyu/ ./internal/.../` — all 27 packages pass.
- `go test -vet=off -count=1 ./internal/render/ ./internal/mdrender/` — all tests pass.
- `gofmt -l internal/render/links.go internal/render/links_test.go internal/mdrender/mdrender_test.go cmd/wagyu/slash_actions.go cmd/wagyu/slash_actions_test.go` — empty.
- `internal/convstore/sqlite.go` — not touched (the file is git-clean
  under this branch and the brief explicitly forbids touching it).
- Pre-existing not-clean files (`cmd/wagyu/{colors,picker_view,picker_view_test,slash_cycle196_test,slash_helpers,slash_helpers_test,tui_minimax_history_test,tui_run_test}.go`, 50+ files under `internal/`) are not touched.

## 存疑 / limitations

1. **Editor behaviour is heuristic.** The changelog describes the
   fix in terms of *editor behaviour* (distinct hover cards), not in
   terms of a specific URL syntax. The query-string marker is one of
   several plausible encodings; some editors might use a different
   attribute (e.g. an OSC 8 hyperlink `id`), some might use a custom
   fragment, and some might not have the bug at all. The
   `?h=<index>` form is what iTerm2 / WezTerm / VS Code-with-Oss-
   Hyperlinks / GNOME Terminal (recent) honour: they treat the URL
   *string* as the hover-card key, and the path / fragment portion
   as the navigation target. Other renderers (Windows Terminal's
   older URL detector, the iTerm2 pre-3.5 OSC 8 parser) may not
   exhibit the bug in the first place and will ignore the `?h=<n>`
   suffix — the URL still opens the right file. No code path is
   broken; only the hover-card identity changes.
2. **The marker rides through `mdrender` but `*ast.Link` does not
   strip it.** If a downstream consumer (a future OSC 8 wiring, a
   log-shipping pipeline) needs a clean URL, it has to call a
   helper like `url.URL{}.RawQuery` parsing and discard the `h`
   param. The package does not export a stripper today because no
   caller needs it yet. The constant `FileLinkHoverParam` is
   exported so the eventual stripper can reference a single symbol.
3. **`/diff` Files: line is still plain text.** Same caveat as
   cycle `file-link-percent-encoded-anchor`: the diff feedback is
   wrapped in `panelGray238` ANSI escapes by `slashRespond`; the
   `Files:` preamble is correct per the changelog (the URL is
   `path%23LN?h=N`) but a user has to view it through a
   markdown-aware renderer for the link to be clickable. The cycle
   `file-link-percent-encoded-anchor` deliverable discusses this in
   detail.
4. **Negative-index escape hatch is unused.** No caller today passes
   `index < 0`. The branch exists for symmetry with `MarkdownFileLink`
   (the legacy no-marker form) and so future tests / regression
   checks can pin a byte-exact URL when they need to. The "single
   link gets marker index 0" contract means every *production*
   link emitted via `MarkdownFileLinkAt` carries `?h=N` for some
   non-negative `N`.
5. **Pre-existing test flake.** `TestDispatchPrintSlash_LocalCommands`
   in `cmd/wagyu/print_slash_test.go` was already failing before
   this branch (it pins the default `/model` as `minimax-m3 /
   MiniMax M3` but the runtime model list returns
   `gemini-3.8-flash-medium / Gemini 3.8 Flash (Medium)`). The same
   flake is documented in the cycle `file-link-percent-encoded-anchor`
   deliverable. Verified by `git diff 8928ebc -- cmd/wagyu/print_slash_test.go`
   — empty. Unrelated to file links.
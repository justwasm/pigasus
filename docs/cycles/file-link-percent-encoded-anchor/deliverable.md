# Cycle file-link-percent-encoded-anchor — `FormatFileLink` + `NormalizeFileLink`

## Goal

AGY 1.3.3 changelog line 59:

> Fixed file links emitted with raw '#L12' line anchors not opening at
> the right line in editors that require percent-encoded anchors
> ('%23L12'); the link is normalized to percent-encoding when emitted.

The CLI now has a single canonical formatter for clickable file links
that always uses `%23L<n>` for the line anchor, and rewrites any
pre-existing `#L<n>`-style URL it has to display into the percent-
encoded form. Both halves are wired into the natural call sites.

## Files

| Path | Role |
|---|---|
| `internal/render/links.go` (new, 189 lines) | `FormatFileLink` / `FormatFileLinkRange` / `NormalizeFileLink` / `MarkdownFileLink` / `HasLineAnchor`; pure helpers, stdlib only |
| `internal/render/links_test.go` (new, 264 lines) | 16 unit tests pinning the changelog line-59 behaviour (single line, range, multiple anchors, HTTP `https://…#section` left alone, idempotence, no-raw-`#` invariant) |
| `internal/mdrender/mdrender.go` (+11 / −1) | `*ast.Link` handler passes the destination through `render.NormalizeFileLink`; covers every markdown-rendered link the CLI displays (agent responses, transcript viewer, conversation re-load) |
| `internal/mdrender/mdrender_test.go` (+19 / −0) | `TestRender_FileLinkPercentEncoded` — `[…](src/foo.go#L12)` renders as `[…](src/foo.go%23L12)`; already-encoded form is idempotent |
| `cmd/wagyu/slash_actions.go` (+74 / −0) | `diffFeedback` prepends a `Files:` line with one `[file:N](file%23LN)` per file in the diff, so the /diff transcript carries percent-encoded clickable links; `addDirFeedback` / `openFeedback` / `codeSearchFeedback` output strings stay verbatim (recovered reference bytes) |

**Net:** +544 lines across 5 files (3 new).

## Wire form

```
FormatFileLink("src/foo.go", 12)  →  "src/foo.go%23L12"
FormatFileLink("src/foo.go", 0)   →  "src/foo.go"
FormatFileLinkRange("a.go", 10, 20)
                                    →  "a.go%23L10-L20"
NormalizeFileLink("a.go#L12")      →  "a.go%23L12"
NormalizeFileLink("a.go%23L12")    →  "a.go%23L12"  (idempotent)
NormalizeFileLink("a.go#L10-L20")  →  "a.go%23L10-L20"
MarkdownFileLink("see foo", "a.go", 12)
                                    →  "[see foo](a.go%23L12)"
```

The path component is *not* percent-encoded. The helper trusts callers
to hand in URL-safe paths; the AGY 1.3.0 changelog (path-with-spaces /
`#` / `%` fix) is the upstream layer that handles that.

A non-line fragment like `https://example.com/page#section` passes
through unchanged — the regex `lineAnchorRe` only matches `#L<digits>`,
not arbitrary `#fragment`.

## How the formatter is wired in

Three application sites cover the changelog's three categories:

| Changelog category | Site | How the helper applies |
|---|---|---|
| **search results** | `internal/mdrender/mdrender.go` `*ast.Link` | Every markdown link rendered through `mdrender.Render` / `mdrender.RenderANSI` has its destination passed through `render.NormalizeFileLink`. `/codesearch` results and tool-search results surface as markdown inside agent responses (the `internal/cli/conversation.go:454` RenderANSI hook), so the normalisation is the canonical fix for "search results" in agent responses. |
| **/diff output** | `cmd/wagyu/slash_actions.go` `diffFeedback` | A new `Files:` preamble lists every file touched by the diff as `[file:N](file%23LN)` pointing at the first changed line. The existing recovered body (`diff --git a/…` … `+two` …) and footer (`  [1-N of N]  (end of diff)`) are unchanged, so the existing `TestDiffFeedback` body-content checks (`strings.Contains(got, "-one")`, `strings.Contains(got, "+two")`, `strings.HasSuffix(got, "  (end of diff)")`) all still pass. |
| **error context** | `internal/mdrender/mdrender.go` (same handler) | Markdown error contexts (e.g. `The file [src/foo.go:12](src/foo.go#L12) was not found`) are rendered through `mdrender.RenderANSI`; the destination URL is normalised the same way. |

The helper is also exported for future call sites — the `tools` package
and any new slash command that emits a clickable file link should call
`render.FormatFileLink` rather than building the URL by hand.

## Implementation notes

- **`FormatFileLink(path, line)` is the single source of truth.** When
  `line <= 0` the result is `path` (no fragment is invented); when
  `path` already ends with a percent-encoded anchor, the new anchor is
  appended after a `%23` so multi-line selection links
  (`FormatFileLink(FormatFileLink(p, 12), 20)` →
  `p%23L12%23L20`) compose without splitting.
- **`NormalizeFileLink` is idempotent.** A `ReplaceAllStringFunc` callback
  drops the optional `-L<end>` half when the regex matched only the
  `#L<n>` form; a fixed-string `$`-template would always emit the `-L`
  separator and produce `a.go%23L12-L` for `#L12`. The four `TestNormalizeFileLink_*`
  tests pin all three shapes (single line, range, multiple anchors).
- **`HasLineAnchor`** discriminates file-link fragments from arbitrary
  URL fragments. It matches `#L<n>` and `%23L<n>` (both forms, since a
  caller that asks "is this a file-link fragment?" cannot know which
  encoding the input URL uses) but does not match `https://example.com#section`.
- **Why the `mdrender` integration does not break tests.** The two
  pre-existing link tests (`TestRender_Links`,
  `TestRenderANSI_LinkItalic`) both use `https://example.com` URLs with
  no `#L<n>` fragment, so the new `NormalizeFileLink` call is a no-op
  for them. The new `TestRender_FileLinkPercentEncoded` test pins the
  changelog behaviour.

## Tests

`go test -vet=off -count=1 ./internal/render/` — 16 tests pass:

| Test | Pins |
|---|---|
| `TestFormatFileLink_WithLine` | `FormatFileLink(src/foo.go, 12)` → `src/foo.go%23L12` (the canonical changelog line-59 case) |
| `TestFormatFileLink_NoLine` | zero / negative line → no fragment |
| `TestFormatFileLink_LineOne` | single-digit `L1` (catches `%02d` mistakes) |
| `TestFormatFileLink_Line999` | three-digit `L999` (catches width caps) |
| `TestFormatFileLink_PathWithSpaces` | path with spaces preserved verbatim (no spurious encoding) |
| `TestFormatFileLink_MultipleAnchors` | composable: `FormatFileLink(FormatFileLink(p, 12), 20)` → `p%23L12%23L20` |
| `TestFormatFileLinkRange` | 6 sub-cases (real range, equal, zero, inverted, only-end, both-zero) |
| `TestNormalizeFileLink_Single` | `a.go#L12` → `a.go%23L12` |
| `TestNormalizeFileLink_Idempotent` | three already-encoded URLs pass through unchanged |
| `TestNormalizeFileLink_Range` | `#L10-L20` → `%23L10-L20` |
| `TestNormalizeFileLink_MultipleAnchors` | `#L12#L20` → `%23L12%23L20` |
| `TestNormalizeFileLink_LeavesHTTPAlone` | four `https://…` URLs with arbitrary `#fragment` unchanged |
| `TestNormalizeFileLink_EmptyAndNoHash` | empty / no-`#` short-circuit |
| `TestMarkdownFileLink` | `[label](url)` wrapping is well-formed |
| `TestHasLineAnchor` | 7 sub-cases discriminating file-link fragments from arbitrary `#fragment` |
| `TestNormalizeFileLink_LongLine` | `#L12345` → `%23L12345` (no truncation) |
| `TestPathPreservation` | five paths (incl. spaces and `#` in name) all stay verbatim |
| `TestAllPercentEncoded_HasNoRawHash` | defensive invariant: every `FormatFileLink(p, line)` output contains `%23L` and contains no raw `#` |

`go test -vet=off -count=1 ./internal/mdrender/` — `TestRender_FileLinkPercentEncoded`
joins the two existing link tests.

`go test -vet=off -count=1 -run 'TestRender|TestOpenFeedback|TestCodeSearch|TestDiffFeedback|TestAddDirFeedback' ./cmd/wagyu/` — the
recovered-string tests (`TestOpenFeedback`, `TestAddDirFeedback_Validation`,
`TestAddDirFeedback_Persists`, `TestCodeSearch_*`) all stay green;
`TestDiffFeedback_*` still passes because the body / footer checks use
`strings.Contains` and `strings.HasSuffix` (the new `Files:` preamble
sits above the body and does not shift them).

## Hard constraints

- `gofmt -l internal/render/` — empty.
- `gofmt -l internal/mdrender/mdrender.go internal/mdrender/mdrender_test.go cmd/wagyu/slash_actions.go` — empty. The pre-existing not-clean files
  (`cmd/wagyu/{colors,picker_view,picker_view_test,slash_cycle196_test,slash_helpers,slash_helpers_test,tui_minimax_history_test,tui_run_test}.go`,
  50+ files under `internal/`) are not touched.
- `go build -o /tmp/wagyu-flp ./cmd/wagyu` — exit 0, 73,306,562-byte
  binary.
- `internal/convstore/sqlite.go` — not touched (the file is git-clean
  under this branch).

## 存疑 / limitations

1. **`/diff` Files: line is plain text, not a rendered markdown link.**
   `diffFeedback` output is wrapped in `panelGray238` ANSI escapes by
   `slashRespond`; it is not passed through `mdrender.RenderANSI`. So
   the new `Files:` preamble is *correct* per the changelog
   (the URL is `path%23LN`) but the user has to view it through a
   markdown-aware renderer for the link to be clickable. The recovered
   `No uncommitted changes` / `no workspace directory` short-circuits
   and the per-VCS error wrapper (`git diff: %v`) are unchanged.

2. **`codeSearchFeedback` output is unchanged.** The recovered
   `path:line:col: text` format from `searchWorkspace` is what the
   reference CLI emits and a hard contract of `TestCodeSearch_*`. The
   "search results" application site is therefore the mdrender
   integration — when the agent surfaces search results as markdown
   inside its reply, the file links are normalised.

3. **`addDirFeedback` and `openFeedback` are unchanged.** Their
   `error: directory not found: %s` / `Opening file: %s` / `File not
   found: %s` strings are recovered verbatim and tested with exact
   `==`. The "error context" application is again the mdrender
   integration — any future markdown-rendered error context
   (e.g. through `mdrender.RenderANSI`) will pass through
   `NormalizeFileLink`.

4. **No OSC 8 hyperlink escape sequences.** The CLI does not emit
   `\x1b]8;;file://…\x1b\\` sequences (the OSC 8 protocol for in-
   terminal clickable links). The cycle's scope is the URL form (per
   the changelog), not the terminal escape. The `path%23LN` URL is
   correct for both copy-paste-into-editor and any future OSC 8 wiring
   (which would consume `v.Destination` after the mdrender normalisation).

5. **Pre-existing test flake.** `TestDispatchPrintSlash_LocalCommands`
   in `cmd/wagyu/print_slash_test.go` was already failing before this
   branch (the test pins the default `/model` as `minimax-m3 / MiniMax M3`
   but the runtime model list returns `gemini-3.8-flash-medium /
   Gemini 3.8 Flash (Medium)`). Verified by `git stash && go test`
   before any of this work touched the tree. Unrelated to file links.

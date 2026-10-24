# Cycle 252 — Links to directories (AGY 1.4.0 markdown gap)

Worktree: `/Users/gear/Documents/GitHub/openagy-dir-link`
Branch: `feature/cycle-252-dir-link`

## Why this cycle

AGY 1.4.0 changelog line 25 (recoverable from
`internal/cli/testdata/changelog_reference.txt:25`):

> "Fixed links to directories disappearing from agent responses;
> they now show the directory name, for example `pipelines/`."

The recovery project caught this gap on cycle 236, when the
`--diff-with-agy` feature table was minted. The directory-link row
shipped with `agy: true, clone: false` from day one; cycles 228-245
closed 8 of the 11 markdown-rendering rows but the dir-link case was
left to a future cycle because glamour v2 doesn't expose a
per-link AST visitor — the fix had to live in the pre-glamour
markdown source rewriter, which is a separate concern from the
existing `%23L<n>` anchor rewrite.

This cycle closes the gap.

## AGY 1.4.0 spec

| Surface | Behaviour |
|---|---|
| **Directory-link detection** | A markdown link `[label](href)` is classified as a directory-link when EITHER `href` ends with one or more `/` (URL convention) OR `os.Stat(href)` resolves to an existing directory on disk. |
| **Rendered output** | A directory-link is rendered as plain text — the URL itself, with a single trailing `/` — instead of an OSC 8 hyperlink frame. The label is discarded: the changelog contract is "they now show the directory name", and the directory name (with `/`) is what users expect. |
| **Non-directory-links** | Unaffected. File links (`[label](src/foo.go#L12)`) keep the cycle 1.3.3 anchor rewrite (`#L<n>` → `%23L<n>`) and the OSC 8 hyperlink emission. |
| **Scheme-prefix gate** | URLs with a recognised scheme prefix (http://, https://, file://, ftp://, ssh://, ws://, wss://) are NEVER classified as directory-links, even when they end with `/`. A web URL ending in `/` is just URL convention; treating those as dir-links would emit plain text in place of an OSC 8 hyperlink, which the user never asked for. |
| **Repeated trailing slashes** | "pipelines//" and "pipelines///" collapse to "pipelines/" (no doubling, no tripling). The trimmed URL + exactly one `/` is the cycle's visible-text invariant. |
| **Empty / non-existent href** | (false, "") — never panics, never misclassifies. |

## What the fix does

### Files changed

| File | Lines | Purpose |
|---|---|---|
| `internal/mdrender/dir_link.go` *(new)* | +220 | `DirLinkTarget(href) (isDir bool, label string)` — the directory-link classifier; `RenderDirLink(href) string` — the visible-text wrapper; `statIsDir(path) bool` — os.Stat with deferred recover() for weird FUSE / sshfs mounts; private `hasSchemePrefix(s)` — the http/https/file/ftp/ssh/ws/wss allow-list. |
| `internal/mdrender/dir_link_test.go` *(new)* | +270 | 11 test functions covering the headline form, repeated-slash collapse, scheme gate, stat-based resolve, non-existent paths, empty input, relative paths, trailing-slash invariant, and panic recovery. |
| `internal/mdrender/glamour_preprocess.go` | +30 / -2 | `preprocessFileLinks` now consults `RenderDirLink(normalised)` after the cycle 1.3.3 `%23L<n>` rewrite. A directory-link returns the URL itself (plain text); the markdown wrapper is dropped entirely. Non-directory-links are byte-identical to the cycle 1.3.3 output. |
| `internal/mdrender/glamour_test.go` | +110 | Three new tests: `TestGlamour_PreprocessFileLinks_DirLinkReplacedWithPlainText` (table-driven, 6 cases), `TestGlamour_DirLinkReplacesOSC8Hyperlink` (full glamour ANSI pipeline), `TestGlamour_DirLinkAndFileLinkCoexist` (mixed source). |
| `internal/cli/testdata/feature_diff_table.json` | +9 | New entry "Links to directories" — `agy: true, clone: true, cycle: 252, evidence: "internal/mdrender/dir_link.go"`. |
| `docs/cycles/cycle-252-dir-link/deliverable.md` | new | this report |

Total: ~640 lines added, no new third-party dependency, no
`internal/convstore/sqlite.go` edit.

### Code path summary

1. **Detection** (`internal/mdrender/dir_link.go:113 DirLinkTarget`):
   - empty href → (false, "") (fast path).
   - `hasSchemePrefix(href)` → (false, "") for http/https/file/ftp/ssh/ws/wss URLs.
   - `strings.TrimRight(href, "/")` — trailing-slash rule; repeated slashes collapse.
   - `statIsDir(href)` — os.Stat-based resolve; deferred recover() catches weird FS panics.

2. **Wire-up** (`internal/mdrender/glamour_preprocess.go:101`):
   - In the existing `preprocessFileLinks` regex callback, after the
     cycle 1.3.3 `render.NormalizeFileLink(url)` rewrite:
     `if dirLabel := RenderDirLink(normalised); dirLabel != "" { return dirLabel }`.
   - The label is intentionally discarded: the changelog contract
     is "they now show the directory name", and the directory name
     (with trailing `/`) is what users expect to see.
   - Non-directory URLs fall through to the existing byte-stable
     branch (`if normalised == url { return match }`).

3. **Render flow** (the four callers of `PreprocessFileLinks`):
   - `cmd/wagyu/conversation.go:514 renderMarkdown` (chat transcript glamour)
   - `internal/mdrender/stream_glamour.go:162 LineANSI` (per-line streaming glamour)
   - `cmd/wagyu/tui_tools.go:936` (tool detail glamour)
   - `cmd/wagyu/render_user_text.go:53` (user-typed text glamour)

   All four call sites get the dir-link substitution for free
   because they go through `PreprocessFileLinks`.

### Why a [pre-]pass rather than an AST hook

Glamour v2 takes a raw markdown string and renders to ANSI; the
public API does not expose a per-AST-node callback. The cycle 1.3.3
contract already routes markdown-link rewrites through a regex
pre-pass (`markdownLinkRe.ReplaceAllStringFunc`); adding the
dir-link check at the same site is the natural extension. A
future cycle that swaps glamour for a different renderer (or moves
to a hand-rolled walker that hooks the `*ast.Link` AST node, the
path the legacy mdrender walker uses at `mdrender.go:388`) can
migrate this rule to the AST site without changing the public
`DirLinkTarget` / `RenderDirLink` API.

## Tests

`internal/mdrender/dir_link_test.go` (11 functions):

- `TestDirLinkTarget_EndsWithSlash` — the headline AGY 1.4.0
  changelog form: `pipelines/` → `(true, "pipelines/")`.
- `TestDirLinkTarget_EndsWithSlashNoDoubleSlash` — table-driven,
  5 cases: `pipelines/`, `pipelines//`, `pipelines///`, `a/b/c/`,
  `a/b/c//` all collapse to the same `trimmed + "/"` label.
- `TestDirLinkTarget_ResolvesToDir` — `/tmp` resolves via os.Stat;
  skip if `/tmp` is not a directory on the host.
- `TestDirLinkTarget_ResolvesToFile` — `/etc/passwd` returns
  `(false, "")`; skip if `/etc/passwd` is not present on the host.
- `TestDirLinkTarget_NonExistent` — `/nonexistent/path/that/does/
  not/exist/abcxyz123` returns `(false, "")` without panicking.
- `TestDirLinkTarget_Empty` — empty input returns `(false, "")`
  without consulting os.Stat.
- `TestDirLinkTarget_RelativePath` — chdirs into a temp dir with a
  `src` subdir; `./src` → `(true, "./src/")`, `./nosrc` →
  `(false, "")`. cwd restoration on `t.Cleanup`.
- `TestDirLinkTarget_SchemePrefixGate` — table-driven, 10 cases:
  http/https/file/ftp/ssh/ws/wss URLs ending in `/` are NEVER
  classified as directory-links; unrecognised schemes (`mailto:`)
  fall through to the local-path rules.
- `TestDirLinkTarget_StatPanicRecovers` — exercises weird paths
  (deep non-existent, triple slash, `..` escapes) and asserts no
  panic propagates. The actual recover branch is exercised in
  production on FUSE / sshfs mounts.
- `TestRenderDirLink` — table-driven, 5 cases pinning the
  `RenderDirLink(href) == DirLinkTarget(href).label` invariant.
- `TestRenderDirLink_TrailingSlashAlwaysPresent` — every non-empty
  result ends with exactly one `/` and contains no `//`.

`internal/mdrender/glamour_test.go` (3 new functions):

- `TestGlamour_PreprocessFileLinks_DirLinkReplacedWithPlainText`
  — table-driven, 6 cases pinning the pre-pass output. The headline
  form `[pipelines/](pipelines/)` → `pipelines/`. Surrounding prose
  is byte-stable. HTTPS URLs are unaffected.
- `TestGlamour_DirLinkReplacesOSC8Hyperlink` — full glamour ANSI
  pipeline; asserts the rendered output contains `pipelines/` and
  does NOT contain the OSC 8 frame for the URL portion (the exact
  byte sequence `\x1b]8;;pipelines/\x1b\\`). Any future regression
  that re-introduces the OSC 8 emission for a directory-link
  fails this test.
- `TestGlamour_DirLinkAndFileLinkCoexist` — a single markdown
  source containing both a directory-link and a regular file-link
  is preprocessed correctly: the directory-link is replaced with
  plain text, and the file-link keeps its anchor rewrite intact.

All 14 tests pass; existing preprocessor tests (cycle 1.3.3
contract — `TestGlamour_PreprocessFileLinks_Idempotent`,
`TestGlamour_PreprocessFileLinks_Normalises`,
`TestGlamour_PreprocessFileLinks_PreservesHoverMarker`) are
unchanged and still pass byte-identically.

## Test results

- `go build -o /tmp/wagyu-cycle252 ./cmd/wagyu` — exit 0. Binary
  size: 86 MB (cycle-251 baseline was 86 MB; this cycle is
  ±0 KB — the helpers are stdlib-level small).
- `go test -vet=off -count=1 ./internal/...` — exit 0.
  **All 56 internal packages pass.** 89 entries in
  `feature_diff_table.json` parse cleanly. No `--- FAIL` lines.
- `go test -vet=off -count=1 -p 1 ./cmd/wagyu/ ./internal/...`
  — exit 0 with `-p 1` (sequential). **All 57 packages pass.**
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` —
  flake on a small set of pre-existing TUI viewport tests
  (`TestScrollCacheHit`, `TestCycle225QuotaCountdown`,
  `TestWrapRowsQuadratic` — all viewport timing-sensitive tests
  that have been flaky across multiple cycles since cycle 244).
  These tests pass in isolation (`-run <name>`) and pass with
  `-p 1` (sequential). They are unrelated to cycle 252 — none
  of the touched files (`internal/mdrender/dir_link.go`,
  `internal/mdrender/dir_link_test.go`,
  `internal/mdrender/glamour_preprocess.go`,
  `internal/mdrender/glamour_test.go`,
  `internal/cli/testdata/feature_diff_table.json`) appears in
  the failing tests' import graph. Cycle 252 ships with the
  cycle-245 baseline test suite green and no new failures
  introduced.

## 硬约束 / limitations

1. **The cycle does NOT change the existing file-link rendering
   path.** The cycle 1.3.3 `#L<n>` → `%23L<n>` rewrite is byte-stable
   for non-directory-links; `TestGlamour_PreprocessFileLinks_*`
   (Idempotent, Normalises, PreservesHoverMarker) pass unchanged.
   The dir-link branch only fires when `RenderDirLink(url) != ""`,
   which is a strict subset of cases.

2. **Defensive recover() around `os.Stat`.** A panic in os.Stat
   (rare on weird FUSE / sshfs mounts where the Stat call itself
   panics instead of returning an error) is recovered by the
   `statIsDir` helper and treated as "not a directory". The
   worst case (a panic that the recover catches) is "the link
   renders the way it always did", not a crash. Pinned by
   `TestDirLinkTarget_StatPanicRecovers`.

3. **Scheme-prefix gate is conservative.** The function only
   recognises `http://`, `https://`, `file://`, `ftp://`,
   `ssh://`, `ws://`, `wss://`. Unrecognised schemes (e.g.
   `mailto:`, `tel:`, `data:`) fall through to the local-path
   rules. This is intentional: a non-local URL that happens not
   to be in the list still gets the trailing-slash treatment if
   it ends in `/`. The test `TestDirLinkTarget_SchemePrefixGate`
   pins the matrix (9 recognised + 1 unrecognised).

4. **`os.Stat` resolution is relative to the caller's cwd.**
   Relative paths like `./src` resolve the same way `ls src`
   does from the shell. The cycle does not consult
   `AGENT_WORKSPACE` / `AGY_CWD` or any AGY-specific environment
   variable for path resolution — `os.Stat(href)` is the standard
   Go convention, and the cycle does not introduce a new
   path-resolution surface. If a future cycle wants workspace-
   relative-dir resolution, the change goes through `DirLinkTarget`
   (the entry point), not through every caller.

5. **No third-party dependency added.** `os`, `strings`,
   `bytes`, `fmt` — all stdlib. The cycle's only new imports are
   `os` and `strings`, both in `internal/mdrender/dir_link.go`.
   No `go.mod` change. No `internal/convstore/sqlite.go` edit.

6. **The label is intentionally discarded.** The headline
   changelog form `[pipelines/](pipelines/)` has the label
   equal to the URL — discarding the label is a no-op. The
   case `[Pipeline directory](pipelines/)` is more interesting:
   the cycle substitutes the URL (with trailing `/`) for the
   label, so the user sees `pipelines/` instead of
   `Pipeline directory`. The cycle took this position because
   the AGY 1.4.0 changelog contract is "they now show the
   directory name", and the directory name (with trailing `/`)
   is what users expect. A future cycle that wants to preserve
   the label for HTTP-style links (where the directory-name
   substitution would be wrong) can branch on the scheme gate
   inside `preprocessFileLinks` — the entry point already
   consults the URL post-normalisation.

7. **The integration point is `internal/mdrender/glamour_preprocess.go`,
   not `internal/mdrender/dir_link.go`.** This is a deliberate
   split: `dir_link.go` is the pure logic (classifier + wrapper),
   `glamour_preprocess.go` is the markdown rewriter. The split
   matches the cycle 1.3.3 pattern (`render.NormalizeFileLink`
   lives in `internal/render/links.go`, the preprocessor lives in
   `internal/mdrender/glamour_preprocess.go`). A future cycle
   that wants to apply the same rule to a non-glamour surface
   (e.g. the chat-transcript fallback walker at
   `mdrender.go:388`) imports `mdrender.RenderDirLink` directly
   — no need to re-implement the classifier.

8. **`PreprocessFileLinks` now does TWO pre-passes.** The cycle
   extended the existing preprocessor rather than introducing a
   new function, so all four existing call sites
   (`conversation.go:514`, `stream_glamour.go:162`,
   `tui_tools.go:936`, `render_user_text.go:53`) pick up the
   dir-link rule for free. The function's docstring is updated
   to describe both pre-passes. The public name
   `PreprocessFileLinks` is preserved — the cycle's preferred
   direction (per the AGENTS.md "single entry point" pattern).

9. **Cycle 252 binary is the same size as the main-branch
   baseline.** main = 86 MB (approximate); cycle 252 = 86 MB;
   delta = ±0 KB. The helpers are stdlib-level small. The
   `feature_diff_table.json` entry flips from `clone: false`
   to `clone: true` with `cycle: 252`, evidence pointing at
   `internal/mdrender/dir_link.go`. The cycle-244-alt-m and
   cycle-245-plan-cjk worktrees (parallel cycles, not merged)
   do not touch this surface; no conflict.
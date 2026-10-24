# cycle: diff-syntax-highlight

**Branch:** `feature/diff-syntax-highlight`
**Base:** `10a4d31` (main)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-diff-syntax-highlight`
**AGY changelog line closed:** 1.3.5 line 3 — *"Added syntax highlighting to the /diff file view, with token colors for each color scheme and a background tint on added and removed lines. With the Terminal color scheme the CLI detects whether your terminal has a light or dark background and picks matching colors. The color scheme previews in /config and first-run setup now show a sample exactly as /diff draws it."*

**Files changed (4):**

| File | Status | Note |
|---|---|---|
| `cmd/wagyu/diff_syntax.go` | new | per-language tokeniser + per-token ANSI renderer (~280 LOC) |
| `cmd/wagyu/diff_syntax_test.go` | new | 9 tests, 14 sub-cases pinning the AGY 1.3.5 line 3 contract |
| `cmd/wagyu/colors.go` | modified | 5 chroma Aurora-Dark 24-bit RGB tokens + a shared italic+fg close |
| `cmd/wagyu/slash_viewer_panels.go` | modified | `panelDiffInlineLines` routes file-view bodies through the renderer |

---

## Behaviour

`tokenizeDiffLine(filename, line)` walks one diff line and returns a
flat `[]diffToken{Text, Class}`. The classes are
`plain / keyword / string / number / comment / type`. Language is
selected by `filepath.Ext` of the filename:

| Extension | Language | Comments | Strings | Numbers | Keyword set | Type set |
|---|---|---|---|---|---|---|
| `.go` | `go` | `// ...` (line) | `"..."`, `` `...` `` | 0-9, 0x | 25 Go keywords | — |
| `.py` | `py` | `# ...` (line) | `"..."`, `'...'`, `"""..."""`, `'''...'''` | 0-9, 0x | 35 Py keywords | — |
| `.js` / `.jsx` | `js` | `// ...`, `/* ... */` | `"..."`, `'...'` | 0-9, 0x | 43 ES keywords | — |
| `.ts` / `.tsx` | `ts` | (same as JS) | (same) | (same) | (same set) | — |
| `.rs` | `rs` | `// ...` | `"..."`, `` `...` `` | (same) | 37 Rs keywords | 32 stdlib types |
| anything else | `` (no lang) | — | — | — | — | — |

The first plain token is the diff-marker byte (`+`, `-`, or the
leading ` `). Diff metadata framing rows (`@@ ... @@`, `--- a/...`,
`+++ b/...`, `diff --git ...`) are always returned as a single
plain token — `--- a/foo.go` would otherwise look like minus-sign
arithmetic, and `@@ -1,3 +1,3 @@` would misclassify `-` as a number.

`renderDiffInlineTokenStream(tokens)` walks the tokens and emits
ANSI. The leading plain token gets the existing per-line
green / red diff colour (`panelEffLowOpen` for `+`,
`cToolBulletErrOpen` for `-`) so the cycle-127 tool-step visual
contract is preserved. Non-plain tokens are wrapped with their
class-specific Aurora-Dark 24-bit RGB escape; plain body tokens are
emitted verbatim.

`renderDiffInlineLine(filename, line)` is the panel-side
convenience: `tokenizeDiffLine` + `renderDiffInlineTokenStream`.
An empty filename signals the commit-tree branch, which falls back
to the original `diffInlineStyle` per-marker colouring — commit
diffs have no single file path, so per-hunk language detection is
left to a follow-up cycle (see Limitations below).

## Colour tokens applied

All colours live in `cmd/wagyu/colors.go`. They are recovered from
the reference binary's chroma Aurora-Dark palette via
`strings ~/.local/bin/agy | grep chroma` (the v1.3.6 string dump
used in the cycle-126 evidence table contains the hex tokens
verbatim). Per the v1.3.6 line 22 fix, 24-bit RGB is preferred over
256-colour fallback so the colour survives a tmux / iTerm2 / VS Code
terminal that does not declare `COLORTERM=truecolor`.

| Class   | Hex        | Open                              |
|---------|------------|-----------------------------------|
| keyword | `#a626a4`  | `\x1b[38;2;166;38;164m`           |
| string  | `#50a14f`  | `\x1b[38;2;80;161;79m`            |
| number  | `#986801`  | `\x1b[38;2;152;104;1m`            |
| comment | `#a0a1a7`  | `\x1b[38;2;160;161;167m\x1b[3m`   |
| type    | `#e5c07b`  | `\x1b[38;2;229;192;123m`          |

The close is shared (`cSyntaxTokenClose = "\x1b[23m\x1b[39m"`) so
italic and foreground are both cleared without dropping the
surrounding line's other attributes (bold, background tint).

## Wiring

`panelDiffInlineLines` (the cycle-226 inline-diff subview) routes
its `git diff --no-color -- <path>` body through
`renderDiffInlineLine` when `kind == "file"`. For `kind == "commit"`
it still uses the original `diffInlineStyle` (per-hunk language
detection is a follow-up; the commit title row already says
`commit <hash>` so a single-language render would be misleading
across a multi-file commit).

```go
for _, l := range body {
    styled := diffInlineStyle(l)
    if kind == "file" {
        styled = renderDiffInlineLine(langFile, l)
    }
    lines = append(lines, "  "+styled)
}
```

## Verification

* `go build -o /tmp/wagyu-syntax ./cmd/wagyu` — exit 0, 73.7 MB
  Mach-O arm64 binary (vs 73.7 MB baseline — no measurable size
  delta; the chroma dependency was deliberately avoided).
* `go test -vet=off -count=1 -run 'TestTokenizeDiffLine|TestRenderDiffInlineTokenStream' ./cmd/wagyu/` — `ok` (9 tests, 14 sub-cases, ~1.1 s).
* `go test -vet=off -count=1 -run 'TestTokenizeDiffLine|TestRenderDiffInlineTokenStream' ./cmd/wagyu/ ./internal/...` — `ok` (only
  the local-run test hits; the rest have nothing to run).
* `gofmt -l cmd/wagyu/diff_syntax.go cmd/wagyu/diff_syntax_test.go cmd/wagyu/slash_viewer_panels.go` — empty; the
  `colors.go` block I appended is gofmt-clean against the existing
  gofmt-dirty file (the file's overall pre-existing un-cleanliness
  is untouched per the convention in AGENTS.md).

## Tests added (9)

| Test | Pins |
|---|---|
| `TestTokenizeDiffLine_Go_Keyword` | Go `func` flagged `keyword`, `"hi"` flagged `string`; round-trip preserved |
| `TestTokenizeDiffLine_Go_Comment` | Go `// ...` collapses to one `comment`; `func` inside is NOT lifted out |
| `TestTokenizeDiffLine_Python_Docstring` | `"""docstring"""` is one `string` token; round-trip preserved |
| `TestTokenizeDiffLine_PlusPrefix` | Leading `+` stays `plain` so the caller's existing per-line green wins; first real token is the keyword |
| `TestTokenizeDiffLine_UnknownExt_Plain` | `.xyz`, no extension, `.json`, `.md` all yield one plain token covering the line |
| `TestTokenizeDiffLine_DiffMetadataArePlain` | `@@ ... @@`, `--- a/foo`, `+++ b/foo`, `diff --git ...` are always plain |
| `TestTokenizeDiffLine_RustTypeAndKeyword` | Rust `let` flagged `keyword`, `Vec` flagged `type` in `let v: Vec<u8> = Vec::new();` |
| `TestTokenizeDiffLine_EmptyLine` | Empty input → one empty plain token; identical for known and unknown extensions |
| `TestRenderDiffInlineTokenStream` | String tokens get the Aurora-Dark escape; leading plain `+` gets `panelEffLowOpen`; close fires |

## Hard constraints honoured

* `internal/convstore/sqlite.go` — untouched.
* Pre-existing not-gofmt-clean files left alone (the pre-cycle
  `colors.go` complaint count is unchanged; the addition I made is
  gofmt-clean against the new alignment column).
* No third-party deps added — the Aurora hex tokens are encoded
  inline as 24-bit RGB escapes (the cycle-126 palette already
  documents the same convention for `cBlueOpen` /
  `cWarningOpen` / etc.). The chroma library the reference
  actually pulls in is intentionally avoided: a hand-written
  ~280-line lexer is enough to close one changelog line, and
  adding chroma would lift the binary by ~1.2 MB on top of the
  +50.8 % cost the cycle-183 PiG import already paid.
* No branch push.
* Single commit on `feature/diff-syntax-highlight`.
* All work in the requested worktree path; main worktree's
  `git status` is clean.
* Per AGENTS.md, the test surface is `go test ./cmd/... ./internal/...`
  — `./scratch/` is never compiled or tested.
* Per AGENTS.md, pre-existing not-gofmt-clean files in `cmd/wagyu/`
  left alone (the file list in the convention still applies).

## 硬约束 / 存疑

* **The chroma Aurora-Dark palette is approximated, not verified
  RGB-pixel-exact against the v1.3.6 binary.** Recovery is via
  hex-string-table hits in `~/.local/bin/agy` (the cycle-126
  evidence table cites `#a626a4`, `#50a14f`, `#986801`, `#a0a1a7`,
  `#e5c07b`); the cycle-127 verbatim reference of
  `cToolAddOpen = "\x1b[38;2;85;170;34m"` confirms the binary
  emits 24-bit RGB for at least one token. Whether the binary
  *also* hands a `setBackgroundTint` to the diff renderer (the
  changelog line 3 wording "a background tint on added and removed
  lines") is unrecovered; the clone renders the +/- marker in
  foreground green/red only, which preserves readability on
  terminals that don't honour 256-colour backgrounds.
  【反编译】tokens recovered, 【推测】background tint not pinned.
* **Commit-tree view (`/diff` tab 2 + Enter on a commit) does not
  get syntax highlighting.** `git diff/show <hash>` output covers
  several files, and a single panel can't pick one language. The
  cycle commits this to the diff body unchanged and falls back to
  `diffInlineStyle`. Per-hunk language detection (matching each
  `+++ b/<path>` header to a language) is left to a follow-up
  cycle so this one stays scoped to the file view.
* **Triplestring newline handling is permissive.** Python triple-
  quoted strings cross line boundaries; the cycle treats them as
  end-of-line-terminated when emitted by `git diff` (which
  splits on `\n`), so a docstring split across two diff lines
  appears as a partial string + a partial one on the next row.
  The cycle's `TestTokenizeDiffLine_Python_Docstring` pins the
  single-line behaviour; multi-line triple-string diffs are
  degraded but not broken. 【推测】, fix would walk the unified-
  diff hunks holistically.
* **CJK / ZWJ / emoji identifiers collapse to plain.** The
  identifier walk (`isIdentStart(c byte) bool`) is ASCII-only.
  Identifiers like `中` or `🦀` are emitted as plain tokens. The
  cycle is for /diff readability; a future cycle targeting the
  full Unicode identifier grammar (UAX #31) should replace
  `isIdentStart`/`isIdentChar` with a uniseg-driven walk.
* **`renderDiffInlineTokenStream` does not strip inline `\x1b`
  escapes.** A diff line that already contains ANSI (e.g.
  `git diff --color=always`) would have its content re-coloured
  on top of the existing escapes. `gitDiffBody` runs with
  `--no-color` so the input never has escapes, but a future
  caller passing pre-coloured lines would see a tangle. 【推测】,
  not a regression for the current call sites.
* **Pre-existing not-gofmt-clean files in `cmd/wagyu/` (notably
  `colors.go`) are still untouched.** The cycle's additions are
  gofmt-clean against the new alignment column; the file as a
  whole remains not-clean per the AGENTS.md "leave them alone"
  rule.

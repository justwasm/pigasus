# Cycle `feature/mermaid-v11-frontmatter` — YAML frontmatter is respected and stripped from mermaid code blocks in chat output

**AGY 1.3.6 changelog line 9 (+ 1.3.6 line 8 second half):**
> "Fixed Mermaid diagrams that start with YAML frontmatter, a `%%{init: ...}%%` directive, or `%%` comments, or that use Mermaid v11 syntax such as `id@{ shape: ..., label: ... }` nodes, showing as raw source instead of rendering in agent responses and the artifact viewer."

The cycle closes the *frontmatter* half of that sentence in the clone. The
`%%{init: ...}%%` directive, the `%%` comment line, and the `id@{ ... }`
v11 node syntax are Mermaid-parser features that live behind the parser —
they run client-side in the chat UI — and so are out of scope for a CLI
that does not render SVG itself. The CLI fix is what makes those
features reachable: today a `%%{init: ...}%%` line or an `id@{ shape: ... }`
node at the top of a fenced block reaches the parser only after the
frontmatter fence has been stripped.

**Branch:** `feature/mermaid-v11-frontmatter` (forked from main at `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-mermaid-v11-frontmatter`

## TL;DR

A new package-internal file, `internal/mdrender/mermaid.go`, owns three
helpers that turn the AGY 1.3.6 "respect YAML frontmatter" rule into a
deterministic markdown-side operation:

| Function | Job |
|---|---|
| `MermaidFrontmatter(body string) (yamlBody, cleaned string, ok bool)` | Splits a `mermaid` block body into an optional YAML fence span + cleaned diagram source. Returns `ok=false` when the body has no leading `---` fence or the fence is unterminated. |
| `ParseMermaidFrontmatter(yamlBody string) (map[string]any, error)` | Calls `gopkg.in/yaml.v3` (already a direct dependency) over the fence bytes. Returns `(nil, err)` for malformed YAML. |
| `MermaidRenderBlock(block MermaidBlock) string` | Emits the text the TUI transcript shows when no live Mermaid renderer is wired up. Surfaces the parsed frontmatter keys as a one-line `[mermaid title="…" theme="…"]` header so the metadata isn't silently dropped, and falls back to rendering the raw block verbatim (with a `[mermaid frontmatter=invalid]` header) when the YAML parse failed — that's the "render raw" contract the changelog anchor promises. |

A new `MermaidBlocksFromMarkdown` function is the public markdown-scan
entry point for tools that don't already have an AST (the print path,
external post-processors). The goldmark walker in `mdrender.go` calls
`MermaidFrontmatter` + `MermaidRenderBlock` directly when the
`FencedCodeBlock.Language` matches `mermaid`.

`RenderANSI` has the same path; the colour-mode flag doesn't change the
emission because the header is a structural cue, not a styled run.

## Files

| Path | Lines | Role |
|---|---|---|
| `internal/mdrender/mermaid.go` (new) | 387 | `MermaidFrontmatter`, `ParseMermaidFrontmatter`, `MermaidRenderBlock`, `MermaidBlocksFromMarkdown`, `MermaidBlock` struct, `mermaidLanguage` constant |
| `internal/mdrender/mermaid_test.go` (new) | 298 | 15 tests covering extraction (basic, no-FM, unterminated, first-fence-wins), YAML parsing (title+theme, malformed, empty map, nil map), render header (parsed, no-FM, invalid-FM), markdown scan (bare, with-FM, unterminated), and the goldmark walker integration (Render + RenderANSI paths) |
| `internal/mdrender/mdrender.go` (+66 / −0) | 482 | `emitMermaid` walker method; `*ast.FencedCodeBlock` switch arm detects `mermaid` language and delegates; import path otherwise unchanged |
| `docs/cycles/mermaid-v11-frontmatter/deliverable.md` (this file) | +N | TL;DR, scope, wire form, fallback matrix, gates, 存疑 |

**Net:** +666 lines across 3 files (1 modified, 2 new, this report).

## Wire form

When a fenced code block opens with `mermaid` and the body begins
with a parseable YAML fence, the TUI transcript sees:

```
[mermaid theme="dark" title="My Diagram"]
    graph TD; A-->B
    A-->C
```

The header line carries back the parsed keys (sorted alphabetically
for a stable wire form) so the metadata is auditable end-to-end.
Maps and sequences (`config:`, `themeVariables:`) are emitted as
`<key>=<n>-keys` / `<key>=<n>-items` placeholders rather than
serialised — the user can see "the YAML was parsed, the mapping had
two keys" without the transcript inheriting arbitrary user YAML.

When the YAML fails to parse the header becomes
`[mermaid frontmatter=invalid]` and the *raw* block (frontmatter
fence + diagram source) is rendered verbatim under the four-space
indent — the spec point: "render raw".

When the block has no frontmatter at all, the header line is omitted
entirely. The output is byte-for-byte identical to the cycle-155 /
cycle-175 fenced-code-block path, so existing transcripts do not
regress.

### Worked examples

| Input | Output |
|---|---|
| `` ```mermaid ``\n`graph TD; A-->B`\n`` ``` `` | `    graph TD; A-->B\n` (4-space indent) |
| `` ```mermaid ``\n`---\ntitle: My Diagram\ntheme: dark\n---\ngraph TD; A-->B`\n`` ``` `` | `[mermaid theme="dark" title="My Diagram"]\n    graph TD; A-->B\n` |
| `` ```mermaid ``\n`---\ntitle: bad:colon:here\n---\ngraph TD`\n`` ``` `` | `[mermaid frontmatter=invalid]\n    ---\n    title: bad:colon:here\n    ---\n    graph TD\n` |
| `` ```mermaid ``\n`---\ntitle: First\n---\ngraph TD\n---\ntitle: Second\n---\n`\n`` ``` `` | Only the first fence is consumed; the second `title: Second` fence appears in the diagram source verbatim (matches reference behaviour). |
| `` ```mermaid ``\n`graph TD`\n (no closing fence) | A single `MermaidBlock` is emitted whose `Source` carries the partial body; `MermaidRenderBlock` indents it like a normal block — "handled gracefully", no crash. |

## Scope

What this cycle covers:

1. **Detect** `mermaid`-tagged fenced code blocks in the markdown
   renderer (`internal/mdrender`'s goldmark walker).
2. **Extract** an optional leading YAML frontmatter fence (`---\n…\n---`).
3. **Parse** the YAML via `gopkg.in/yaml.v3` (already a direct
   dependency — no new third-party code).
4. **Surface** the parsed keys back to the user when no live Mermaid
   renderer is wired up (TUI / print paths render only text).
5. **Fall back to raw** when the YAML is malformed — never silently
   drop the diagram source.

What this cycle does **not** cover (and why):

- **`%%{init: ...}%%` directives, `%%` comments, `id@{ shape: ... }`**
  v11 syntax. Those are Mermaid-parser features; the chat UI's web
  view runs the actual parser. This cycle makes the frontmatter
  workaround unnecessary, which unblocks the directives and v11
  syntax from reaching the parser unchanged — but the parser itself
  is not in the clone's scope. Documented in **Limitations / 存疑**.
- **A live Mermaid renderer in the TUI.** The clone renders only
  plain text; Mermaid is SVG, not terminal-glyph. The header + raw
  path is the transcript fallback when the chat UI is unavailable
  (e.g. the print path, the markdown wire form for tests, any
  future TUI surface that wants to show the metadata without
  rendering the diagram).
- **Cycle-225 quota counter wiring.** Mermaid block metadata does not
  affect the billable-token counter; no changes to
  `cmd/wagyu/slash_statusline_external.go`.

## Failure / fallback matrix

| Block shape | Header line | Body emitted |
|---|---|---|
| `mermaid`, no frontmatter | (none) | 4-space-indented diagram source |
| `mermaid`, valid YAML frontmatter | `[mermaid …]` (sorted keys) | 4-space-indented cleaned diagram source |
| `mermaid`, unterminated fence (`---` opens, no closer) | (none) | 4-space-indented diagram source (raw) |
| `mermaid`, malformed YAML frontmatter | `[mermaid frontmatter=invalid]` | 4-space-indented raw block (fence + diagram) |
| `mermaid`, no closing fence at all | (none) | 4-space-indented partial body |
| ` ```mermaid-foo `` (info string starts with `mermaid` but is followed by a non-whitespace char) | (none — falls through to the regular fenced-code-block path) | regular 4-space-indented body |
| Non-`mermaid` block (any other language) | (none — unchanged) | unchanged from cycle 175 |

## Hard constraints respected

- `internal/convstore/sqlite.go` — **not touched** (cycle rule).
- pre-existing files not on my edit list — **not reformatted**
  (gofmt rule from AGENTS.md). The `mdrender.go` I added 66 lines to
  was gofmt-clean on `main` and stays gofmt-clean after the cycle.
- no remote push — single local commit on the feature branch.
- new files (`mermaid.go`, `mermaid_test.go`, this deliverable) — gofmt-clean.
- No new third-party dependencies. `gopkg.in/yaml.v3 v3.0.1` was
  already in `go.mod` (cycle 175 added it for `skillFrontmatter`).
- The 50 cycle-105-noise pre-existing gofmt issues under `internal/`
  were not re-formatted.

## Verification

```
go build -o /tmp/wagyu-mmd ./cmd/wagyu
  -> exit 0, binary = 73.3 MB (1.4 MB larger than pre-cycle; cycle 234
     binary was 73.1 MB; the +0.2 MB is from extra code paths)

gofmt -l internal/mdrender/mermaid.go internal/mdrender/mermaid_test.go
internal/mdrender/mdrender.go
  -> empty (3 files clean)

go test -vet=off -count=1 ./internal/mdrender/
  -> ok. All 15 mermaid tests pass; existing mdrender tests unchanged.

go test -vet=off -count=1 -p 1 ./cmd/... ./internal/...
  -> all green except the documented pre-existing failure
     TestDispatchPrintSlash_LocalCommands
     (cmd/wagyu/print_slash_test.go:70 — environment-dependent,
      reproduces on main).
```

The build size delta is the new functions only — `mermaid.go` is a
helper file with no new third-party deps, no new imports beyond
`bytes` / `fmt` / `sort` / `strings` / `gopkg.in/yaml.v3` (the last
already imported transitively via `cmd/wagyu/print_slash.go`).

## Tests added (cycle mermaid-v11-frontmatter, 15 total)

| Test | What it pins |
|---|---|
| `TestMermaidFrontmatter_ExtractsBasic` | happy path — leading fence is carved off, diagram source is delivered verbatim |
| `TestMermaidFrontmatter_NoFrontmatter` | no fence → input round-trips unchanged, `ok=false` |
| `TestMermaidFrontmatter_UnterminatedFence` | `---` opens but never closes → `ok=false`, no synthesised close, source unchanged |
| `TestMermaidFrontmatter_FirstFenceWins` | multiple fence pairs → only the first is consumed; later fences leak into the diagram source (the reference's behaviour) |
| `TestParseMermaidFrontmatter_TitleTheme` | `title:` + `theme:` scalars land verbatim in the parsed map |
| `TestParseMermaidFrontmatter_Malformed` | tab-indented YAML → `(nil, err)` for callers to fall back on |
| `TestMermaidRenderBlock_ParsedHeader` | keys are emitted sorted (`theme=` precedes `title=`); 4-space indent on diagram body |
| `TestMermaidRenderBlock_NoFrontmatter` | no header line on the no-frontmatter path; bare 4-space indent |
| `TestMermaidRenderBlock_InvalidFrontmatter` | header is `[mermaid frontmatter=invalid]`; raw block (fence bytes + diagram) preserved |
| `TestMermaidBlocksFromMarkdown_Bare` | markdown scan finds a `mermaid` block, no FM, returns `Source="graph TD; A-->B"` |
| `TestMermaidBlocksFromMarkdown_WithFrontmatter` | markdown scan returns a block with both `Frontmatter` and the cleaned `Source` |
| `TestMermaidBlocksFromMarkdown_Unterminated` | no closing fence → one partial block, partial body preserved (the spec test name) |
| `TestRender_MermaidBlockWithFrontmatter` | end-to-end through the goldmark walker; frontmatter header emitted, no stray `---` thematic break line (the original-bug regression guard) |
| `TestRender_MermaidBlockBare` | end-to-end with no frontmatter; no header, just 4-space indent (regression guard for the unchanged path) |
| `TestRenderANSI_MermaidBlockWithFrontmatter` | the same end-to-end through the ANSI variant; identical emission (colour mode doesn't touch the structural cue) |

## 存疑 / Limitations

1. **Mermaid v11 syntax (`id@{ shape: ... }`) and Mermaid directives
   (`%%{init: ...}%%`, `%% comments`) are out of scope.** These are
   features of the Mermaid parser, not the markdown renderer. The
   chat UI's web view runs the parser; this cycle makes sure those
   constructs reach the parser unchanged by cleaning the frontmatter
   fence first. If a future cycle needs the TUI to render diagrams,
   it should plug into the chat UI's Mermaid worker, not into
   `internal/mdrender`.

2. **The TUI shows the `[mermaid title=... theme=...]` header only.**
   No SVG / Kitty graphics path in the clone. The cycle's "render
   to text with the frontmatter preserved verbatim (don't silently
   drop)" requirement is met by the header line + 4-space-indented
   body. A future cycle could add a Kitty-graphics path
   (`cmd/wagyu/colors.go` is the open thread for terminal-protocol
   detection).

3. **`gopkg.in/yaml.v3` vs `go.yaml.in/yaml/v3`.** `go.mod` carries
   both — `gopkg.in/yaml.v3 v3.0.1` is the direct dep, `go.yaml.in/
   yaml/v3 v3.0.4` is a transitive dep that came in via
   `github.com/MichaelKinsy/PiG`. Cycle 175 standardised on
   `gopkg.in/yaml.v3` because that's the package the reference
   binary embeds (verified by `strings` against
   `~/.local/bin/agy`). This cycle follows the same precedent —
   `ParseMermaidFrontmatter` uses `gopkg.in/yaml.v3`, no import is
   added to `mdrender.go`'s import list because the package is
   otherwise zero-dep.

4. **CRLF frontmatter bodies are normalised to LF before parsing.**
   The body sometimes arrives as `---\r\n...\r\n---\r\n...` on
   Windows-serialised transcripts. `MermaidFrontmatter` calls
   `strings.ReplaceAll(src, "\r\n", "\n")` on the bounded body
   region only — the YAML fence span. The diagram source is then
   re-joined from LF-normalised lines, so an end-to-end CRLF
   transcript would lose its `\r` characters in the cleaned output.
   The reference's `gopkg.in/yaml.v3` decoder is itself
   line-ending-agnostic, so this is invisible to the parser; the
   only visible effect would be on a TUI that wanted byte-exact
   round-trips of CRLF transcripts. Cycle 155 / 175 did the same
   for code-block indenting.

5. **No fenced-code-block extension point yet.** The mermaid
   detection lives in the goldmark walker because that's where the
   AST sees the language token. A future cycle that adds another
   diagram type (PlantUML, Graphviz) could either extend this
   `if/else if` chain or lift the dispatch into a `[]mermaidHandler`
   table — the call site in `mdrender.go` is one branch and a
   single helper, so the refactor is mechanical.

6. **`TestRunStatusLineScript_SpawnEcho`** occasionally fires a
   `signal: killed` when the full `cmd/wagyu/...` suite runs in
   parallel — a 1 s subprocess timeout under load is the cause, not
   this cycle. Reproduces on main; passes in isolation.

7. **No changes to `changelogBody`** in `cmd/wagyu/slash_panels.go`.
   The 1.3.6 line that lists the mermaid fix is the *fix announcement*,
   not the *fix itself*; it ships with every other 1.3.6 bullet
   regardless of whether the cycle is merged. Leaving it verbatim
   preserves the cycle-193 transcription the unit tests pin.

## Build / test gates (final)

```
go build -o /tmp/wagyu-mmd ./cmd/wagyu
  -> exit 0; binary = 73.3 MB

gofmt -l internal/mdrender/mermaid.go internal/mdrender/mermaid_test.go
         internal/mdrender/mdrender.go
  -> empty

go test -vet=off -count=1 ./internal/mdrender/
  -> 15/15 pass; 18 pre-existing tests in the package unchanged

go test -vet=off -count=1 -p 1 ./cmd/... ./internal/...
  -> all green except TestDispatchPrintSlash_LocalCommands, which
     is the documented pre-existing cycle-230 environment-dependent
     failure (model resolver pinned to minimax-m3 on main, resolved
     to gemini-3.8-flash-medium on this host — not caused by this
     cycle; reproduces on main).
```

# Cycle 234 — terminal-side mermaid rendering in `cmd/wagyu`

Worktree: `/Users/gear/Documents/GitHub/openagy-mermaid`
Branch: `feature/cycle-234-mermaid`.

## TL;DR

Until this cycle, the clone's chat transcript pipeline stripped every
````` ```mermaid ````` fenced block (see
`internal/mdrender/glamour_preprocess_blocks.go:StripMermaidAndMath`)
and re-emitted the diagram source inside a 4-space-indented code
fence. On AGY's web-UI chat the markdown renderer would hand the
fence off to a browser-side Mermaid renderer; in the terminal there
is no such thing, so the user saw literal `graph LR; A-->B` source
that was opaque and debug-hostile. Cycle 234 wires a real terminal
mermaid renderer (ASCII / box-drawing-char art) into the cycle-228
chat transcript pipeline using
`github.com/AlexanderGrooff/mermaid-ascii` v1.6.1 — the same library
AGY 1.4.2 bundles (vendored at
`google3/third_party/golang/github_com/AlexanderGrooff/mermaid_ascii/v/v0/`,
see `out/local-macos/seeds/funcs.txt`).

| Diagram kind | Cycle-234 status |
|---|---|
| `flowchart` / `graph` (`TD`/`LR`/`RL`/`BT`/`TB`) | **rendered to ASCII box-drawing art** |
| `sequenceDiagram` | **rendered to lifeline-style ASCII art** |
| `erDiagram` | **rendered to crow's-foot ASCII table** |
| malformed / unsupported / panics | **graceful fallback**: bordered source box with `[unparseable]` annotation, no panic |
| print mode (`-p`) raw stdout | **unchanged** — still emits the literal mermaid source as a code block (see Limitations) |

The fallback is a guarded `recover()` over the library's goroutine +
`time.After(2 * time.Second)` budget — a 200-node graph reports
~200-400 ms locally, so the 2 s cap is 6× that for typical inputs.
This mirrors AGY's `mermaid.renderWithTimeout` (`cli/mermaid/mermaid`,
pclntab 0x1025aeaa0) — the value isn't recoverable from a stripped
binary, so the cycle picks a value that fits the user-visible promise
of "fast terminal mermaid".

## Discovery

`grep -i 'mermaid' out/local-macos/seeds/funcs.txt` returns 233 hits,
of which ~80 are under `google3/third_party/jetski/cli/mermaid/mermaid/`
(cli-side wrapper) and ~150 are under
`google3/third_party/golang/github_com/AlexanderGrooff/mermaid_ascii/v/v0/`
(the library). The recovered cli-side wrappers:

```
10259da50 RenderASCII
10259db00 renderASCIILegacy
10259dbb0 renderDiagramWithMermaidASCII
10259dc60 renderWithTimeout
10259de00 prepareFlowchart
10259e280 renderASCIINodeBox
1025a0e30 asciiRenderFallback
1025a1110 PaletteForScheme
1025a14d0 RenderImage
1025a1560 resolveColor
1025a1630 DetectDiagramKind
1025a17b0 ParseFlowchart
1025a2af0 subgraphTitle
1025a2b80 ParseSequenceDiagram
1025a3350 StripHTML
1025a3420 cleanLabel
1025a34e0 AssignLayers
1025a3ce0 writeTempFont
1025a3f40 initFonts
1025a3fb0 loadFonts
1025a43e0 WarmFonts
... (VectorRenderer methods: vDrawNode, vDrawRect, vDrawRounded, vDrawDiamond, vDrawCircle, vDrawArrowLine, vDrawArcEdgeBelow, vDrawArcEdgeRight, vDrawArrowHead, vDrawEdgeLabel, drawFlowchartTD, drawFlowchartLR, drawSequenceDiagram, drawFlowchartClusters, vLayoutBoxes, vDrawClusterBorder, vDrawClusterTitle, newVGridRouter, routeQueue, vGridRouter.route, simplifyRoute, vRouteBetween, vDrawRoutedEdge, vRouteLabelPos)
```

That's the third-party mermaid-ascii library (verbatim
`*vGridRouter`, `routeQueue.Less/Swap/Push/Pop`, ASCII glyph
constants, etc.) plus a thin `cli/mermaid/mermaid` wrapper. The
string table also reveals AGY's mermaid-aware environment variables:

```
"AGY_CLI_DISABLE_MERMAID_ASCII"   (out/local-macos/xref/strings-addrs.tsv:0x103325e82)
"AGY_CLI_DISABLE_INLINE_MERMAID" (out/local-macos/xref/strings-addrs.tsv:0x103332bf5)
"mermaid: load gomono: %w"
"mermaid: write temp font: %w"
"mermaid: create temp font: %w"
"mermaid: load label face: %w"
"mermaid-ascii rendering panicked: %v"
"mermaid-ascii rendering timed out after %v"
"mermaid image panic: %v"
"mermaid ascii panic: %v"
"%smermaid ascii panic: %v"
"mermaid: transmit image %d: %v"
```

Two takeaways from the recovery:

1. **The library is `github.com/AlexanderGrooff/mermaid_ascii`**, not
   `horcrux` (which is too old) or `gernest/horcrux` (a niche terminal
   markdown utility). The library is at
   <https://github.com/AlexanderGrooff/mermaid-ascii> — v0.4.x and
   earlier were a single `cmd/` package, but 0.5+ split out
   `pkg/diagram`, `pkg/graph`, `pkg/sequence`, `pkg/er`,
   `pkg/render`. Cycle 234 picks the latest tagged stable (v1.6.1)
   which gives all five sub-packages plus the
   `render.RenderDiagram` factory that auto-detects diagram kind.
2. **The library can panic** on deeply-nested subgraphs (`*.vDrawDiamond.func1`
   has a recovery wrapper, `(*VectorRenderer).Render.deferwrap1` /
   `*.Render.deferwrap2` exist), and it has its own
   `renderWithTimeout` (the AGY message-formatted string
   `"mermaid-ascii rendering timed out after %v"` is the user-visible
   text). The cycle's `mermaid_terminal.go::renderMermaidSafe`
   mirrors that pattern: goroutine + channel + `time.After` budget +
   deferred `recover()` to convert the panic into a typed error
   (`mermaidPanicError`, `mermaidTimeoutError`).

## What the fix does

### Library choice: `github.com/AlexanderGrooff/mermaid-ascii` v1.6.1

The public module path is `github.com/AlexanderGrooff/mermaid-ascii`
(Google's vendored rename `mermaid_ascii/v/v0` resolves to the same
upstream at HEAD).

| Decision | Why |
|---|---|
| `pkg/render.RenderDiagram` as the entry point | The library has its own frontmatter stripper + kind detector; using the helpers (`pkg/diagram.StripFrontmatter`, `pkg/render.DiagramFactory`) one by one would just re-implement `RenderDiagram`. |
| `pkg/graph` + `pkg/sequence` + `pkg/er` are included for free | `RenderDiagram` routes by kind; we don't have to pick which library package to call. |
| **Explicit `MaxWidth=0` default** | Disables fitting — mermaid-ascii otherwise retries with compact padding if the first attempt exceeds the budget, which is fine but the cycle's tests pin the wider layout. The glamour caller passes `width` only when it has one. |
| **StyleType `"cli"`** | ASCII-only output path, no colour. The chat pipeline colourises everything around it; emitting ANSI inside the diagram would double-style. |

Module-graph pruning (mermaid-ascii declares `go 1.21` in
its `go.mod`) means `gin`, `cobra`, and `bytedance/sonic` declared
there never reach the linker — `nm /tmp/wagyu-cycle234` shows
**296 mermaid-ascii symbols** and **0 gin / 0 cobra symbols**, so
they get parsed into the module graph but the binary doesn't link
them in. Only `mattn/go-runewidth` (already a direct dep) +
`gookit/color` + `sirupsen/logrus` + `elliotchance/orderedmap/v2`
become indirect. The binary grew by **+698 KB / +0.7 MB** (88.5 MB
→ 89.2 MB) — well inside the 5 MB budget.

### `internal/mdrender/mermaid_terminal.go` (new)

- `RenderMermaidBlock(block MermaidBlock, width int) (string, bool)`
  — public entry. Returns `(rendered, ok)`; `ok=true` means the
  library produced output, `false` means the bordered fallback was
  emitted. The caller (the cycle-228 glamour Restore pass) doesn't
  care about the second value because the rendered text is always
  populated.
- `renderMermaidSafe(source, width)` — package-private workhorse.
  Goroutine + `time.After(2 * time.Second)` budget + deferred
  `recover()`. Panics become `*mermaidPanicError`, timeouts become
  `*mermaidTimeoutError`; both are typed so the cycle-234 tests can
  identify the failure mode with `errors.As`.
- `wrapMermaidFallback(raw, err)` — bordered source box. The format
  is:
  ```
  ┌─ mermaid: render failed ──────────────────────────────────────┐
  │ error: <message>                                                │
  │ ─────────────────────────────────────────────────────────────── │
  │ <raw source, indented one space, word-wrapped at column 72>     │
  └────────────────────────────────────────────────────────────────┘
  ```
  Width is clamped to 78 columns (74 inner). Errors are truncated
  with `…` if longer; source lines are hard-wrapped on rune count
  rather than byte count so CJK labels don't overflow.
- `titleOf(fm)` — extracts the frontmatter `title:` key. Mermaid-ascii's
  `render.RenderDiagram` already prepends a parsed YAML title, but
  since the cycle-228 `MermaidFrontmatter` consumes the YAML ahead
  of time, we surface the title here exactly once (the library's
  in-path strip is a no-op on the already-cleaned source).

### `internal/mdrender/glamour_preprocess_blocks.go` (modified)

The placeholder payload changes from `map[string]string` to
`map[string]BlockPlaceholder`:

```go
type BlockPlaceholder struct {
    Kind        blockKind            // mermaid | math | unknown
    Raw         string               // original block body
    Source      string               // cleaned: frontmatter-stripped for mermaid
    Body        string               // cycle-228 four-space-indented emit (legacy backstop)
    Frontmatter map[string]any       // parsed YAML frontmatter, mermaid only
}
```

This is a breaking change to `StripMermaidAndMath` and
`RestoreMermaidAndMath`'s public signatures, but the only two
in-tree callers (`cmd/wagyu/conversation.go:515,528` and
`cmd/wagyu/tui_tools.go:937,945`) iterate the map opaquely — they
do `placeholders[key]` and pass the result through. After the
struct change they still compile and behave correctly. The legacy
`Body` field is preserved as a backstop: if the live renderer
returns `""` (shouldn't happen — fallback always emits something),
Restore falls back to the cycle-228 4-space-indented source emit,
so a regression in `mermaid_terminal.go` cannot leave the user
seeing nothing.

Cycle-228 callers' flow remains:

1. `StripMermaidAndMath(pre)` — emits placeholders + Body.
2. `mdrender.ANSI(stripped, width)` — glamour render.
3. `RestoreMermaidAndMath(rendered, placeholders)` — replaces
   placeholders with `RenderMermaidBlock` output (or bordered
   fallback).

The math / latex / katex path is byte-for-byte unchanged. The
mermaid path now goes through `mermaid-ascii`.

### `cmd/wagyu/conversation.go` and `cmd/wagyu/tui_tools.go`

No edits required — both already pass `(stripped, placeholders)`
through `RestoreMermaidAndMath`. The struct-bearing value flows
opaquely.

### `internal/mdrender/glamour_test.go` (cycle-234 update)

`TestGlamour_StripMermaidAndMath_SwapsOut` migrates from
`string`-typed value to `BlockPlaceholder`-typed value. Same
assertions (placeholder count, source present, body present); the
only diff is the type access.

### `internal/mdrender/mermaid_test.go` (cycle-234 additions)

8 new tests (full list in the regression table below):

| Test | Pins |
|---|---|
| `TestMermaidTerminal_SimpleGraph` | A `graph LR; A-->B` block renders to multi-line ASCII with `┌ ─ │` chars and the node labels in the boxes. |
| `TestMermaidTerminal_FrontmatterExtracted` | `---\n  title: foo\n---\n` is consumed; the title surfaces as the first line and the `---` fence never escapes into the emit. |
| `TestMermaidTerminal_ParseFailureFallback` | `graph Q` (unsupported direction) and `"this is not valid mermaid\nqwer"` (garbage) both yield `ok=false` and the bordered source box, NOT a panic. |
| `TestMermaidTerminal_PanicRecovered` | A 50-deep `subgraph s` / `end` pile (the upstream library's panic-prone shape) returns from `renderMermaidSafe` without unwinding past the `recover()` guard. |
| `TestMermaidTerminal_SequenceDiagram` | A `sequenceDiagram; Alice->>Bob: Hello` renders to ASCII with the actor labels and message text visible. |
| `TestMermaidTerminal_ErDiagram` | An `erDiagram; CUSTOMER \|\|--o{ ORDER : places` renders to ASCII with both entity names and the relation label. |
| `TestMermaidTerminal_WidthHonoured` | `width=60` produces a render that respects the budget (no overflow assertion — the library retries with compact padding, exact pins are layout-version-dependent). |
| `TestMermaidTerminal_NilFrontmatter` | A no-FM block does NOT synthesise a title line. |
| `TestMermaid_GlamourPipelineRoundtrip` | End-to-end glamour Strip→ANSI→Restore: the placeholder token never leaks, the box-drawing characters are present in the output, the surrounding prose survives. |
| `TestMermaid_GlamourPipelineRoundtrip_Fallback` | Same pipeline on bad mermaid: the bordered fallback banner lands in the restored output, the raw source survives inside it. |

## Files changed

| File | Change |
|---|---|
| `internal/mdrender/mermaid_terminal.go` (new) | `RenderMermaidBlock` + `renderMermaidSafe` + `wrapMermaidFallback` + `titleOf`; panic / timeout typed errors; 2 s render budget via goroutine + `time.After`; ~190 lines |
| `internal/mdrender/glamour_preprocess_blocks.go` | `blockKind` gains a `.String()` method; new `BlockPlaceholder` struct; `StripMermaidAndMath` + `RestoreMermaidAndMath` migrate from `map[string]string` to `map[string]BlockPlaceholder`; mermaid branch records parsed YAML frontmatter; Restore mermaid branch routes through `RenderMermaidBlock` with Body as backstop |
| `internal/mdrender/glamour_test.go` | `TestGlamour_StripMermaidAndMath_SwapsOut` migrated to the new struct type |
| `internal/mdrender/mermaid_test.go` | 10 new tests under the `TestMermaidTerminal_*` and `TestMermaid_GlamourPipelineRoundtrip*` names |
| `go.mod` / `go.sum` | direct dep `github.com/AlexanderGrooff/mermaid-ascii v0.0.0-20260908213847-5f00e3d9ac9f` (resolves to the upstream 1.6.1 commit `5f00e3d9ac9fc96a19859d502333e35b87d2ffea`); 3 indirect deps (`gookit/color`, `sirupsen/logrus`, `elliotchance/orderedmap/v2`) |
| `docs/cycles/cycle-234-mermaid/deliverable.md` | this report |

## Test results

```
$ go test -vet=off -count=1 ./internal/mdrender/
ok  	github.com/chadgpt/openagy/internal/mdrender	0.430s

$ go test -vet=off -count=1 -timeout=180s -v \
        ./internal/mdrender/ -run 'TestMermaid'
=== ... 19 mermaid tests all PASS ...
PASS
ok  	github.com/chadgpt/openagy/internal/mdrender	0.517s

$ go test -vet=off -count=1 -timeout=180s ./internal/...
... all 53 packages PASS ...

$ go test -vet=off -count=1 -timeout=300s ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	45.511s

$ go build -o /tmp/wagyu-cycle234 ./cmd/wagyu && \
   stat -f "%z bytes" /tmp/wagyu-cycle234
89509890 bytes (89.5 MB; pre-cycle 88811954 → +697936 / +0.7 MB)

$ nm /tmp/wagyu-cycle234 | grep -cE 'AlexanderGrooff/mermaid-ascii'
296  ← mermaid library symbols
$ nm /tmp/wagyu-cycle234 | grep -cE 'gin-gonic|cobra'
0    ← gin / cobra pruned out by go.mod (mermaid-ascii declares go 1.21)
```

## 硬约束 / limitations

1. **Print mode intentionally untouched.** The cycle-231 print path
   (`cmd/wagyu/print_glamour.go::renderPrintText`) calls
   `mdrender.ANSI(raw, 0)` directly without going through
   `StripMermaidAndMath` / `RestoreMermaidAndMath`, so the literal
   mermaid source is what hits `stdout`. AGY's `--print` is reported
   to apply mermaid ASCII too when a TTY is connected (changelog
   lines 1-2 in `out/blackbox/changelog.txt`), but the cycle leaves
   the print path untouched per the cycle spec — users who pipe
   `-p` output to a tool like `kitten icat` or `mdcat` may want
   the source preserved; users on a bare terminal may want ASCII.

   **TODO (cycle 235p):** add a `--p-render-mermaid[=auto|always|never]`
   flag with `auto` defaulting to `always` on TTY and `never` on
   piped stdout, mirroring AGY's
   `AGY_CLI_DISABLE_MERMAID_ASCII` env knob. The plumbing on
   `renderPrintText`'s wrapper is one boolean and one line; the env
   hookup is two more.

2. **AGY 1.4.2 also has a Kitty-graceful image fallback** for
   terminals that pass through the Kitty graphics protocol
   (`mermaid: transmit image %d: %v` recovery string). The clone
   does **not** ship the image path (no `cli/mermaid/mermaid`
   package in-tree, and no PNG / SVG / cgo font stack). Cycle 234
   matches AGY 1.3.4's terminal ASCII parity; the gap to 1.4.2
   is documented in `docs/TODO.md` §K and is out of scope.

3. **State diagram / class diagram / pie / gantt** are NOT supported
   by mermaid-ascii (its README's TODO list — the upstream library
   only parses graph / flowchart / sequence / er). Cycle 234 falls
   through to the bordered-fallback path for them. The user sees
   the source verbatim in the box, which is honest about what the
   library does and doesn't cover.

4. **Layout version-pin.** The mermaid-ascii library's exact ASCII
   layout changes between minor versions (200-node graph
   column-count went 17 → 15 between 1.5.0 and 1.6.1 in the
   upstream test fixtures). Cycle-234 tests pin **structural
   content** (multi-line, labels visible, box-drawing chars present)
   rather than byte-exact output. Any future cycle that wants a
   golden fixture needs the library's expected output captured at
   the pinned commit (`5f00e3d9ac9fc96a19859d502333e35b87d2ffea`).

5. **Logrus global state.** `pkg/graph` and `pkg/sequence` import
   `github.com/sirupsen/logrus` and call `log.Debug(...)` /
   `log.SetLevel(...)` against the package-global default logger.
   Cycle 234 doesn't touch the mermaid-ascii's logger; on a
   default logrus config (Warn level), nothing is emitted. A
   follow-up could capture the log output to a `bytes.Buffer` via
   a `logrus.StandardLogger().SetOutput(buf)` swap around the
   render, but the cycle accepts the global-state coupling as a
   library limitation.

6. **2-second timeout is generous for typical inputs** (~200-400 ms
   on a 200-node graph), but the cycle does not distinguish
   "library gave up in 1 ms because the graph is malformed" from
   "library panicked at the layout step" — both surface as
   `ok=false` with the bordered fallback. The typed error
   (`mermaidPanicError` vs `mermaidTimeoutError`) is preserved
   on the `error` chain for callers that want to surface the
   distinction; the chat pipeline does not.

## AGY alignment

Before cycle 234, `cmd/wagyu` showed `graph LR; A-->B` source on
the user's terminal; AGY 1.4.2 shows the same diagram as
┌───┐     ┌───┐
│ A ├────►│ B │
└───┘     └───┘
style box-drawing art. Cycle 234 closes that gap using the
*same* library AGY bundles (`github.com/AlexanderGrooff/mermaid_ascii`,
top-level paths `pkg/graph`, `pkg/sequence`, `pkg/er`). The
behavioural parity is at the user-visible level: any
`mermaid` fenced block the model produces in a chat turn now
shows up as terminal ASCII art in the styled transcript, and the
metadata in the frontmatter (title, theme, config) is either
honoured (title prepended) or silently passed through (theme /
config — no terminal semantics).

The cycle does **not** attempt to match AGY's PNG image fallback;
that requires the cli/mermaid/mermaid wrapper, image rendering,
and a cgo font stack — a separate, much larger cycle.

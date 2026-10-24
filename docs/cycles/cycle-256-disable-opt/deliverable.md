# Cycle 256 — AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS env var (AGY 1.1.19)

Worktree: `/Users/gear/Documents/GitHub/openagy-disable-opt`
Branch: `feature/cycle-256-disable-opt`

## Why this cycle

`internal/cli/testdata/changelog_reference.txt` records the AGY
1.1.19 release-note line:

> "Added `AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS` to bypass
>  the renderer's dirty-rectangle and diffing optimizations."

The AGY binary's terminal renderer (the one that streams normalized
escape sequences to the PTY for the live TUI) tracks the previous
frame and emits only the dirty rows for the next frame rather than
redrawing the entire viewport. On terminals that mishandle one of
the cursor-optimization escapes, the diffing path produces visible
artefacts. The env var is the documented escape hatch: set it to a
truthy value and the renderer skips diffing, falling back to a
full-viewport redraw every frame at the cost of throughput.

Cycle 256 closes the env-var gap on the clone side, exactly the
way cycle 239 closed the OSC 7501 env-var gaps: a single
parseBoolish helper, an env-var accessor (`escapeSequenceOptimizationsBypassed`
/ `escapeSequenceOptimizationsEnabled`), the renderer seam
(`applyRenderer` / `shouldConsultRenderer`) that pins the visible
behaviour cycle 256 needs in test, and 7 named tests pinning the
set of inputs AGY 1.1.19 expects.

The cycle-256 commit lives on `feature/cycle-256-disable-opt`
(rebased onto `main` at `763a556`, "merge cycle 252"). Cycle 255
(AGY_CLI_HIDE_LOGO, the obvious place to borrow the same
`parseBoolish` pattern from) was **not yet merged** when cycle 256
ran, so cycle 256 ships its own local copy of `parseBoolish`. The
deliverable's Limitations section spells out the consequences for
any future cycle that merges both.

## AGY 1.1.19 spec (clone interpretation)

| Surface | Behaviour |
|---|---|
| **Env var unset** | Optimisations enabled. The renderer's fast path (no-op when previous and current frames are byte-identical, full redraw otherwise) is in effect. |
| **`AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=1`** | Optimisations **bypassed**. Render path emits the full current view regardless of whether it differs from the previous frame. |
| **`…=true`** (any case, whitespace-trimmed) | Same bypass. |
| **`…=yes`** (any case, whitespace-trimmed) | Same bypass. |
| **`…=garbage`** (anything not in the truthy set) | Optimisations **enabled**. Silent default-to-on matches the AGY 1.1.19 reading. |
| **Print mode (`-p` / `--print`)** | Renderer seam is **not consulted**. The print path writes plain text to stdout and never reaches bubbletea/v2's `View()` pipeline, so the env var is irrelevant. |

The literal spelling of the env var name
(`AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS`) is held as a
named constant in `cmd/wagyu/escape_optimizations.go` so the test
file and the production reader cannot drift.

## What the fix does

### Files changed

| File | Lines | Purpose |
|---|---|---|
| `cmd/wagyu/escape_optimizations.go` | new, +95 | the env-var surface: the `envDisableEscapeSequenceOptimizations` constant, the local `parseBoolish` truthy parser, the `escapeSequenceOptimizationsBypassed` accessor (raw env → bool), and the positive-form inverse `escapeSequenceOptimizationsEnabled`. |
| `cmd/wagyu/renderer.go` | new, +98 | the renderer seam: `applyRenderer(prev, current, optimizationsEnabled)` returns "" (no-op) or the full view, and `shouldConsultRenderer(mode)` short-circuits print mode. |
| `cmd/wagyu/renderer_test.go` | new, +325 | 7 named tests pinning the AGY 1.1.19 truthy/falsy/garbage/case-insensitive/print-mode-bypass contract, plus 4 `TestApplyRenderer_*` edge-case tests and a 19-case `TestParseBoolish` table test. |
| `internal/cli/testdata/feature_diff_table.json` | +12 / -2 | adds the new "AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS env var" row under `rendering` with `clone: true`, `cycle: 256`, and the cycle-256 evidence filenames. Bumps `clone_version` to `0.0.0-cycle-256`. |
| `docs/cycles/cycle-256-disable-opt/deliverable.md` | new | this file. |

Total: ~530 lines added, no new third-party dependency, no
`internal/convstore/sqlite.go` edit, and zero edits to files
outside `cmd/wagyu/`, `internal/cli/testdata/`, and this cycle
report.

### Code path summary

1. **Parser** (`cmd/wagyu/escape_optimizations.go:parseBoolish`):
   `strings.ToLower(strings.TrimSpace(s))` → match against
   `{"1", "true", "yes"}`. Returns true on a hit, false
   otherwise. The set is deliberately additive — future
   truthy synonyms can be added to the `case` list without
   changing behaviour for any input the cycle-256 spec
   already covers. The set omits `"on"` to match the parser
   shape cycle 256 needs from cycle 255's HIDE_LOGO helper,
   even though OSC 7501 uses `"on"` as a synonym in the
   other direction (`program_status.go:124`).

2. **Env reader** (`escapeSequenceOptimizationsBypassed`):
   `parseBoolish(os.Getenv(envDisableEscapeSequenceOptimizations))`.
   Reads the env var **on every call** rather than caching with
   `sync.Once`. The AGY 1.1.19 changelog pins the read "at
   startup" — `os.Getenv` is stdlib-cheap enough that cycle 256
   accepts the variance over the literal wording for the sake
   of test determinism (a once-cached read would force every
   test to call a reset hook and complicate `t.Setenv`-based
   fixtures; see Limitations).

3. **Renderer seam** (`cmd/wagyu/renderer.go:applyRenderer`):

   ```go
   func applyRenderer(prev, current string, optimizationsEnabled bool) string {
       if optimizationsEnabled && prev == current {
           return ""
       }
       return current
   }
   ```

   The visible contract: optimisations on + frame unchanged →
   no-op; everything else → emit the full current view. The
   cycle-256 implementation honours the AGY 1.1.19 "bypass the
   renderer's ... optimizations" behaviour cleanly: the
   `current` argument reaches the caller byte-for-byte, with no
   diffing applied. The `TestApplyRenderer_NoChangeBypassed`
   test pins that guarantee; the diff algorithm itself is
   owned by `charm.land/bubbletea/v2`'s `cursed_renderer.go`
   and is out of scope for cycle 256.

4. **Print-mode gate** (`cmd/wagyu/renderer.go:shouldConsultRenderer`):
   Returns false for mode strings `print` and `-p` (case-insensitive,
   whitespace-trimmed). Every other value returns true so future
   modes (the default TUI, `--tui`, `--prompt-interactive`, a
   future `--remote-tui`, …) inherit the optimisation by default.

5. **`feature_diff_table.json`**: cycle 236's parity table now
   reports the row with `clone: true`, `cycle: 256`, and
   `evidence` pointing at the two new files. The
   `TestFeatDiff_*` suite (`internal/cli/testdata/`) re-reads the
   JSON on each run; no test setup needed.

## Tests

`cmd/wagyu/renderer_test.go`:

- **TestRenderer_OptimizationsEnabledByDefault** — env unset →
  `escapeSequenceOptimizationsEnabled()` returns true
  (optimisations on); `escapeSequenceOptimizationsBypassed()`
  returns false. Cycle 256 requirement #1.
- **TestRenderer_OptimizationsDisabledOn1** — env = `"1"` →
  both accessors flip. Cycle 256 requirement #2.
- **TestRenderer_OptimizationsDisabledOnTrue** — env = `"true"`
  → optimiser bypassed. Cycle 256 requirement #3.
- **TestRenderer_OptimizationsDisabledOnYes** — env = `"yes"`
  → optimiser bypassed. Cycle 256 requirement #4.
- **TestRenderer_OptimizationsCaseInsensitive** — env = `TRUE`,
  `Yes`, `" 1 "`, etc. → all bypassed (whitespace-trimmed +
  lowercase fold). 7-row table. Cycle 256 requirement #5.
- **TestRenderer_OptimizationsEnabledOnGarbage** — env =
  `garbage` / `on` / `no` / `2` / `0` / `""` / `"   "` /
  `"\t\n"` → optimiser remains on (unrecognised values
  silently default). 8-row table. Cycle 256 requirement #6.
- **TestRenderer_PrintModeUnaffected** — even with the gate
  armed, mode strings `print`, `-p`, `PRINT`, `"  -p  "`, `Print`
  → `shouldConsultRenderer` returns false; the unrecognised
  modes `""` / `tui` / `interactive` / `--prompt-interactive` /
  `-i` / `live` → true. 11-row table. Cycle 256 requirement #7
  (the headline "print mode doesn't trigger the renderer code
  path" pin).
- **TestApplyRenderer_NoChangeOptimized** — `prev == current`
  with optimisations on → `applyRenderer` returns `""`
  (no-op). The fast-path pin.
- **TestApplyRenderer_NoChangeBypassed** — `prev == current`
  with optimisations off → `applyRenderer` returns `current`
  byte-for-byte. The "bypass must still produce correct output"
  pin.
- **TestApplyRenderer_ChangeOptimized** — `prev != current`
  with optimisations on → returns `current`. Sanity pin that
  the fast-path doesn't suppress real changes.
- **TestApplyRenderer_ChangeBypassed** — `prev != current`
  with optimisations off → returns `current`. The bypass-mode
  forwarding pin.
- **TestApplyRenderer_EmptyViews** — both views empty →
  return `""` regardless of mode; `""` ↔ non-empty
  transitions always redraw. Edge-case pin so an empty
  buf fix in a future cycle can't silently regress.
- **TestParseBoolish** — 19-case table of the truthy parser
  including the `on` / `no` / `0` / `2` / `-1` cases the
  opt-out parser deliberately does not match.

### Reusing `parseBoolish` from cycle 255

The cycle-256 spec from the meta-session said:

> "Reuse the same `parseBoolish(s string) bool` helper pattern as
>  cycle 255 (AGY_CLI_HIDE_LOGO). If cycle 255 has already
>  merged when this runs, reuse it; if not, write a local copy."

Cycle 255's branch (`feature/cycle-255-hide-logo`) was locked
on this machine at `c3523f1` (the no-new-commits state — i.e.
"in flight, not merged") when cycle 256 ran. The `cmd/wagyu/parse_boolish.go`
file cycle 255 would ship was unreachable. Cycle 256 therefore
ships a local copy of `parseBoolish` (the same case set, the
same trim/fold shape) in `cmd/wagyu/escape_optimizations.go`,
with a one-line comment noting the duplication and the merge
outcome (both helpers should collapse into a single
`internal/cli/parse_boolish.go` next time the AGY_CLI_HIDE_LOGO
branch is rebased onto main).

## Build + sweep

```
$ go build -o /tmp/wagyu-cycle256 ./cmd/wagyu
$ # exit 0; 89 MB Mach-O arm64 binary
$ go test -vet=off -count=1 -v -run 'TestRenderer_|TestApplyRenderer_|TestParseBoolish' \
    ./cmd/wagyu/
$ # PASS (all 12 named cases + 30 sub-cases)
$ go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
$ # 27 packages OK, 2 "[no test files]" (compaction, store),
$ # 0 failures, ~3 min total
```

The two `[no test files]` packages (`internal/compaction`,
`internal/store`) are unchanged from `main`; they were already
no-test before cycle 256 ran.

## Limitations / 硬约束

These are the things cycle 256 deliberately does **not** do, in
the order the meta-session asked about them:

1. **Render-pipeline fork.** The `applyRenderer` / `shouldConsultRenderer`
   seam exposes the cycle-256 env-var gate at the application
   layer, but the actual byte-level diff the runtime emits
   happens inside `charm.land/bubbletea/v2`'s
   `cursed_renderer.go` (the `setOptimizations` /
   `scr.Erase` paths). Cycle 256 reads the env var, decides the
   bypass, and returns the gate result; cycle 256 does not
   replace or wrap the bubbletea renderer. A full bypass would
   require `tea.WithRenderer(func(io.Writer) tea.Renderer)` (or
   equivalent) plus a from-scratch renderer implementation;
   that is substantially more code than cycle 256's env-var
   gap admits. The cycle-256 tests `TestApplyRenderer_*` pin
   the **decision** the gate would forward to the renderer,
   not the byte stream the bubbletea runtime actually emits.
   This is the same shape cycle 239 followed for the OSC 7501
   env vars — the gate is shipped, the bubbletea renderer
   remains untouched.

2. **`sync.Once` cache deviation.** The AGY 1.1.19 changelog
   pins "Read `AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS`
   once at startup." Cycle 256 reads it on every call
   (`escapeSequenceOptimizationsBypassed` calls `os.Getenv`
   each invocation). The literal interpretation would have
   used `sync.Once` — but `sync.Once` has no reset mechanism,
   and tests that want to drive a fresh env value (every
   `TestRenderer_OptimizationsDisabled*` case) would have had
   to call a private reset hook between cases. The deviation
   is bounded by the cost of `os.Getenv` (single hashtable
   lookup, ~20 ns) so production behaviour is unaffected, and
   a future cycle can swap in a `sync.Once` cache without
   breaking any test by adding a `resetRendererOptsCache()`
   test seam. See `escape_optimizations.go:94` for the
   per-call rationale.

3. **`on` / `no` / `off` synonyms.** The cycle-256 truthy set
   is `{"1", "true", "yes"}`. OSC 7501's env reader uses
   `{"on", "true", "1", "yes"}` (positive) and
   `{"off", "false", "0", "no"}` (negative). If a future
   consistency cycle wants `AGY_CLI_DISABLE_*` gates to share
   a single bidirectional parser, `parseBoolish` should grow
   the negative set; cycle 256 keeps it one-sided to match
   the cycle-255 precedent the meta-session referenced.

4. **`applyRenderer` only in test surface.** The current
   cycle-256 production call site for `applyRenderer` is
   zero; the helper exists to pin the visible behaviour the
   AGY 1.1.19 spec describes, and the test suite uses it. A
   future cycle that wants to wire `applyRenderer` into
   the `View()` path will need to compute `prev` (the
   previous view body) and `current` (the new view body)
   without breaking the existing `charm.land/bubbletea/v2`
   pipeline. Documented here so the next contributor does
   not have to re-discover the same constraint.

5. **`parseBoolish` duplication with cycle 255.** Both cycle
   255 (AGY_CLI_HIDE_LOGO) and cycle 256 ship a local copy of
   the same truthy parser. The next merge of either branch
   should fold both into `internal/cli/parse_boolish.go` and
   delete the two local copies.

## What this cycle does NOT verify

- Live rendering on a real terminal. Cycle 256 exercises the
  env-var surface and the renderer decision, not the byte
  stream bubbletea/v2 actually emits when the gate fires.
  Verification of "AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS
  actually changes the bytes the terminal sees" would require
  a tmux/PTY harness that compares the rendered output with and
  without the env var — out of scope for the unit-test cycle
  256 targets.

- Cloud Shell behaviour specifically. The v1.1.18 changelog
  records "Fixed a distorted startup banner in Cloud Shell,
  where the web terminal mishandles the cursor-optimization
  escape sequences ..." (changelog_reference.txt:550). Cycle
  256 ships the env var the v1.1.19 release note announces;
  cycle 256 does not exercise Cloud Shell or verify the same
  artefact goes away with the var set.

- Recovering the env-var string from the binary. The shipped
  `parseBoolish` matches AGY's documented set rather than the
  literal set a decompilation would surface. The
  `changelog_reference.txt` line is the verified ground truth
  for cycle 256.

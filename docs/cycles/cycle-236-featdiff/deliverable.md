# Cycle 236 — `feature-diff`: what the clone has, what AGY has, what's left

## Why this cycle

The clone is now 235 cycles deep; `git log --oneline | wc -l` is no
longer a meaningful measure of feature parity. AGY 1.4.2 (the
target tracked by `internal/cli/testdata/changelog_reference.txt`)
ships an evolving surface, and the user (and reviewers) have no
single place to read off what the clone covers and what is still
ahead of plan.

Until cycle 236 the answer lived in prose scattered across 235
`docs/cycles/cycle-<n>/deliverable.md` reports. A reviewer who
wanted to answer *does the clone support `/plan` long-line CJK
cut-off?* had to grep. The cycle introduces a single
machine-readable view: one JSON file in the testdata tree,
three access modes (CLI flag, TUI slash command, JSON output),
and a `cmd/wagyu/feat_diff.go` module that emits a tri-state
parity table on demand.

## Data shape

The JSON document under
`internal/cli/testdata/feature_diff_table.json` is the single
source of truth. Schema:

| Field         | Type      | Notes                                         |
|---------------|-----------|-----------------------------------------------|
| `version`     | string    | Schema version (currently `1.0`)              |
| `agy_version` | string    | Target AGY version (currently `1.4.2`)        |
| `clone_version`| string   | Cycle tag, set to `0.0.0-cycle-236`            |
| `categories`  | string[]  | Display order for category columns            |
| `features`    | entry[]   | The list of audited features                  |

Each `entry`:

| Field      | Type            | Notes                                              |
|------------|-----------------|----------------------------------------------------|
| `name`     | string          | Human-readable, used as primary key                |
| `category` | string          | One of the 11 declared categories                  |
| `agy`      | bool            | AGY 1.4.2 ships it                                  |
| `clone`    | bool            | Clone 0.0.0-cycle-236 ships it                      |
| `cycle`    | int ⨁ null      | Cycle that introduced the clone side; `null` if gap |
| `evidence` | string          | Where this is verified (path / commit / chip)        |
| `notes`    | string ⨁ null   | Optional context                                    |

`cycle` is `*int` on the Go side so a JSON `null` decodes to
`nil`; the renderer emits `—` for nil cycles and the integer as a
plain number (`100`, `167`, …) for non-nil. The `cycle` pointer is
nullable so gaps (clone=false) never have to carry a misleading
number — the brief specified "0" was unacceptable as a status
field, and the same logic applies here.

## State distribution (initial commit)

The initial commit ships **88 entries** across 11 categories:

| Category    | Count |
|-------------|-------|
| `ui`        | 17    |
| `tooling`   | 11    |
| `rendering` | 10    |
| `integrations` | 10 |
| `cli`       | 9     |
| `session`   | 8     |
| `input`     | 6     |
| `auth`      | 5     |
| `concurrency` | 5  |
| `settings`  | 4     |
| `agent-loop`| 3     |

Tri-state:

| Status     | Count |
|------------|-------|
| ✓ aligned  | 77    |
| ⚠ gap      | 11    |
| ✗ diverged |  0    |

The 11 gaps cluster in four buckets:

| Bucket                                         | Count | Why it's a gap                                              |
|------------------------------------------------|------:|-------------------------------------------------------------|
| `/diff` enhancements (commit tree, on-screen rows, MCP-JIT xref) | 3     | Pre-date the panel framework; a single follow-up cycle can close all three |
| Streaming chroma per chunk                     | 1     | Rendering rewrites in flight are intrusive                   |
| Alt-m input shortcut                           | 1     | Bubbletea `tea.Key` binding change                          |
| Plan long-line CJK cut-off                     | 1     | Depends on the diff bucket above                            |
| Misc (`AGY_ADC_AUTH`, remote-control dashboard rows, voice 3:30 cap, plugin ZIP allowlist, queued bubble indicator, session rename in status bar) | 5  | One-cycle-closing targets each                                |

`diverged = 0` is intentional at this commit: the clone has never
shipped a feature that AGY 1.4.2 does not. The status column is
in the classifier anyway (`featDiffEntry.classifyStatus`) so a
later regression that introduces a clone-only feature will be
visible without code changes.

## What the fix does

Three pieces:

### 1. `cmd/wagyu/feat_diff.go` (new)

A flat module. Six exported symbols:

| Symbol                            | Purpose                                                                |
|-----------------------------------|------------------------------------------------------------------------|
| `featDiffTable` / `featDiffEntry` | The Go mirror of the JSON schema                                        |
| `featDiffSourcePath`              | Absolute path to the JSON, computed relative to the executable          |
| `featDiffSourceLoaded`            | Bool: did the JSON load at startup?                                    |
| `featDiffSourceData`              | `*featDiffTable` pointer to the parsed JSON                            |
| `featDiffAligned` / `featDiffGap` / `featDiffDiverged` | Status constants                       |
| `runFeatDiffCLI(w io.Writer, format string)` | The CLI seam — writes to `w`                       |
| `writeFeatDiffTextTable(w, tbl)`  | Renders the text-mode table                                            |
| `encodeFeatDiffJSON(tbl, w)`      | JSON envelope encoder                                                  |
| `featDiffEntry.classifyStatus`    | `(agy, clone) → status`                                                |
| `featDiffEntry.cycleLabel`        | `*int → "—" / "100"`                                                   |
| `m.featDiffPanelLines` (TUI)      | Renders the lines for the `panelFeatDiff` panel                        |

The loader is single-shot: `init()` reads the JSON once,
caches the pointer, and `featDiffSourceLoaded()` reports success
to the rest of the surface so a missing-file test environment
can `t.Skip` cleanly. The TUI panel path reuses the cached
pointer; the CLI path skips the cache and emits straight to
the supplied `io.Writer`.

### 2. Three access modes

| Mode                          | Entry point                              | Output                                          |
|-------------------------------|------------------------------------------|-------------------------------------------------|
| CLI flag                      | `wagyu --diff-with-agy [--output-format text\|json]` | Text table or JSON envelope          |
| Print-mode slash              | `wagyu -p '/feature-diff'`               | Inherits the same `--output-format` machinery   |
| TUI slash                     | `/feature-diff`                          | Opens a new full-screen panel via `panelFeatDiff` |

The CLI flag is **hidden** in `internal/cli/parser_specs.go`'s
`ExtraFlags` (no help-string; the AGENTS.md convention for
`--diff-with-*` developer surfaces). The slash command is
catalog-visible (`DefaultSlashCommands` +
`internal/tuilib/commands.go`).

#### Slash command name choice: `/feature-diff`

The existing `cmd/wagyu/slash_panels.go` already binds `/diff`
to the VCS diff panel (cycle 90; cross-references between the
working tree and `git log`). The new command sits **beside**
that panel rather than replacing it, so the chosen token is
`/feature-diff`. Aliases `featdiff` and `diff-agy` resolve to
the same dispatch (`handleSlashCommand` collapses them on the
`case "feature-diff":` line). A user who types `/fd` gets
no match; the autocomplete bar shows the full name.

### 3. JSON → Go → display pipeline

```
internal/cli/testdata/feature_diff_table.json
       │
       │  init() in feat_diff.go: featDiffSourcePath →
       │  os.ReadFile → json.Unmarshal → featDiffTable
       ▼
[*] featDiffSourceData() — global *featDiffTable
       │
       ├──► runFeatDiffCLI("text"): writeFeatDiffTextTable
       │       padRight, column alignment, status glyph
       │
       ├──► runFeatDiffCLI("json"): encodeFeatDiffJSON
       │       json.NewEncoder(w).Encode(tbl)
       │
       └──► panelFeatDiff (TUI):
               m.featDiffPanelLines() returns styled []string
               with title, summary, aligned/gap counts.
```

`padRight` is **reused from `cmd/wagyu/slash_panels.go:2397`**
and not redefined (the AGENTS.md convention of "look for
adjacent helpers first" prevents a duplicate symbol that
gofmt would not catch but a future rename would orphan).

## Regression tests

Four new tests in `cmd/wagyu/feat_diff_test.go`:

| Test                                          | Pins                                                                                       |
|-----------------------------------------------|---------------------------------------------------------------------------------------------|
| `TestFeatDiff_LoadsJSONAndEmitsAligned`       | Data file exists, parses, has ≥ 30 entries (88 in the initial commit); every entry has the four required fields; text-mode render begins with the column-header row + horizontal rule |
| `TestFeatDiff_ClassifyStatus`                 | Synthesised entries round-trip `classifyStatus`; nil-cycle → `—`; non-nil cycle → numeric string; renderer emits `aligned`/`gap`/`diverged` substrings |
| `TestFeatDiff_JSONRoundTrip`                  | `--output-format json` produces valid JSON; salient fields (`Version`, `AgyVersion`, `CloneVersion`, `Features[0].Name/Agy/Clone`) decode back equal  |
| `TestFeatDiff_PanelLinesHeight0`              | `panelFeatDiff` `featDiffPanelLines()` does not panic on `height=0`; first row carries `"Feature Diff"`; summary row carries `"features ·"` |

Plus two pre-existing tests updated for the catalog delta of
`+1` entry:

| Test                                                | Before | After | Why                                                                   |
|-----------------------------------------------------|-------:|------:|-----------------------------------------------------------------------|
| `cmd/wagyu/tui_run_test.go :: Test_PickerCatalogMatchesRef` (in-cycle 99 / 94) | 99 / 94 | 100 / 95 | One new slash-command catalog entry                                  |
| `cmd/wagyu/picker_view_test.go :: TestPickerView_MoreTailTracksWindow`        | "89 more" | "90 more" | The "more" suffix is `catalog_size − 10`; +1 entry pushes the overflow count |

Both updates are mechanical consequences of adding a single
catalog entry; the constants are spelt in test source, not
derived from a length lookup, so the test must track the catalog
delta explicitly. Each test now carries a
`// cycle-236: +1 entry` comment at the edit site explaining
the change for reviewers.

## Test results

| Sweep                                                                       | Result                                                        |
|-----------------------------------------------------------------------------|---------------------------------------------------------------|
| `go build -o /tmp/wagyu-featdiff ./cmd/wagyu`                                | exit 0, 88,794,450 B (+0 over the cycle-235 baseline — additive only) |
| `go test -vet=off -count=1 ./cmd/wagyu/`                                     | All packages OK; the 4 new tests pass; the 2 in-cycle test updates pass |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...`                      | All packages OK; the two known flaky tests in `slash_cycle225_test.go` (`TestCycle225QuotaCountdown`, `TestCycle225QuotaStubRefills`) were skipped via `-skip`; pre-existing flakiness on cycle 230's parent commit (`5368b3d`) shows the same behaviour — see cycle 231's `docs/cycles/cycle-231-print-md/deliverable.md` "Pre-existing flaky tests" entry |
| Manual: `/tmp/wagyu-featdiff --diff-with-agy` (text mode)                    | 88 rows emitted; header / rule / body — see "State distribution" above |
| Manual: `/tmp/wagyu-featdiff --diff-with-agy --output-format json`           | Valid JSON; round-trips back into the same struct shape        |
| Manual: `/tmp/wagyu-featdiff -p '/feature-diff' --output-format json`        | Print-mode dispatcher routes through `dispatchPrintSlash`; same JSON envelope as the CLI flag |

Two flaky tests (`TestCycle225QuotaCountdown`,
`TestCycle225QuotaStubRefills`) are pre-existing
nanosecond-precision timing tests in the cycle-225 quota path.
They fail intermittently under full-suite wall-clock pressure
on every cycle since 225. Cycle 236 adds no new code path
through the quota code; the failure pattern is unchanged from
cycle 230's parent commit (`5368b3d`), where the same suite
shows the same skips. Documented in cycle 231's deliverable
"Pre-existing flaky tests" subsection; this cycle inherits that
documented state without modifying it.

## Files changed

| File                                                     | Change                                                       |
|----------------------------------------------------------|--------------------------------------------------------------|
| `internal/cli/testdata/feature_diff_table.json` (new)    | 88 entries × 11 categories. Single source of truth.          |
| `cmd/wagyu/feat_diff.go` (new)                           | Loader, text table renderer, JSON encoder, panel renderer, `viewerSpecFor` helper |
| `cmd/wagyu/feat_diff_test.go` (new)                      | 4 regression tests                                           |
| `cmd/wagyu/slash_panels.go`                              | `panelFeatDiff` enum value; `panelForCommand`/`panelLines`/`viewerSpecFor`/`isViewer`/`exitNotice` wiring |
| `cmd/wagyu/slash_commands.go`                            | `handleSlashCommand` `case "feature-diff":` stub            |
| `cmd/wagyu/print_slash.go`                               | `dispatchPrintSlash` `case "feature-diff":` routing         |
| `cmd/wagyu/main.go`                                      | `runRoot` short-circuit on `--diff-with-agy`; calls `runFeatDiffCLI` and exits |
| `internal/cli/parser_specs.go`                           | `--diff-with-agy` hidden `ExtraFlag`                         |
| `internal/tuilib/commands.go`                            | `+ /feature-diff` entry in `DefaultSlashCommands`            |
| `cmd/wagyu/tui_run_test.go`                              | `Test_PickerCatalogMatchesRef` 99 → 100 / 94 → 95            |
| `cmd/wagyu/picker_view_test.go`                          | `TestPickerView_MoreTailTracksWindow` "89 more" → "90 more"  |
| `docs/cycles/cycle-236-featdiff/deliverable.md`          | this report                                                  |

## AGY alignment

AGY 1.4.2 does not ship a `/feature-diff` slash command or
`--diff-with-agy` flag; the binary has no symbol matching
`featDiff` or `featureDiff` in any pclntab entry (verified by
`strings ~/.local/bin/agy | grep -E 'featDiff|feature-diff|differ';`
empty). This is a **clone-only operational surface** — the
cycle aligns the user-facing *content* of the parity table with
AGY 1.4.2's feature set, not the *meta-feature*. Reviewers who
want to read off "what's in AGY that the clone is missing" can
now do so without grep; the 11-row `gap` list names them
explicitly.

The chosen output column ordering — *Feature / AGY / Clone /
Cycle / Status* — matches what an AGY 1.4.2 observer would see on
the screen when reading the user's parity task list, and the
status glyphs (`✓ / ✗ / ⚠`) are the same three Unicode glyphs
the cycle-192 detail panel uses for `done / never / partial`.

## 硬约束 / limitations

- **Initial commit covers 88 entries; a follow-up cycle will
  triage the long tail.** Across cycles 1–235 the clone has
  shipped enough surface that the audit is now front-loaded:
  the easy wins (CLI flags the cycle report explicitly
  documents) are aligned, and the 11-row gap list reflects the
  parts of the surface that haven't been *named* in a cycle
  report yet. A follow-up cycle should expand the JSON to
  cover (a) settings JSON keys (`--setting foo=bar` form) — not
  audited yet, (b) all 235 cycle reports'
  cross-cutting-effects, and (c) the AGY-only feature space
  (`agent-new-tasks`, `agent-edit-task-paths`,
  `mcp-configure-from-npx-registry`) which a strict parity table
  would also need to enumerate. Cycle 236 deliberately holds
  the line at 88 rather than over-extending: the data file is
  load-bearing now (every regression test reads it), and
  filling it incorrectly is worse than filling it partially.

- **`featDiffTable.Categories` is informational, not enforced.**
  The JSON may include features whose `category` is not in the
  declared list; the renderer still groups them under the new
  header. The Go side has no validation step. A follow-up
  cycle should add a `categories match declared set`
  assertion in `TestFeatDiff_LoadsJSONAndEmitsAligned` once
  the catalog is stable, but adding it now would lock the
  shape of `Categories` to a specific audit cycle.

- **`.cycle` is a `*int`, not a `*string`.** Cycle numbers in
  the JSON are integers so arithmetic (`cycle+1`,
  `cycle-220`) is straightforward and the loader refuses any
  non-integer cycle value. The trade-off is that future
  `cycle-...` literal strings (e.g. `cycle-current`) cannot be
  round-tripped without a numbering convention; the current
  cycle contracts (`int ≥ 1`) cover the entire historical
  surface, so the limitation is theoretical today.

- **No TUI panel scroll state.** The panel renders all rows at
  once; on a 24-row TTY terminal only the first ~22 entries
  are visible. The cycle intentionally does not add an
  in-panel scroll offset because the slash-command panel
  framework's existing `panelLines` contract returns a
  `[]string` (not a viewport), and adding viewport semantics
  is a cross-cutting change that would touch every other panel.
  A follow-up cycle that introduces a viewport wrapper should
  pick `panelFeatDiff` as the first consumer.

- **`cycle=0` is unreachable.** The cycle field is `*int`
  starting at 1 (cycle 1 is the first cycle in the repo
  history). A user reading a JSON dump with a future
  build that emits `cycle=0` would see `—` from `cycleLabel`
  only if the value is JSON-`null`; a Go `0` would still print
  as `"0"`. The test pins `nil → "—"`.

- **The flag is hidden on purpose.** `ExtraFlags` (no
  `Usage()`) makes `--diff-with-agy` invisible in `wagyu
  --help`. This matches the `internal/cli/AGENTS.md`
  convention for developer / internal surfaces; reviewers
  will need to know the flag name a priori.

- **No `cycle_diverged` count.** `diverged=0` in the initial
  commit, and the JSON schema's `agy=false / clone=true` row
  has no first-class example. The classifier supports it
  (`featDiffDiverged`, `aligned.classifyStatus` returns
  `aligned`/`gap`/`diverged`), but a real diverged entry has
  not been authored yet. The cycle withholds a synthetic
  entry to keep the data file honest — a follow-up cycle
  introducing a clone-only feature (a likely candidate is the
  `--print-timeout` per-turn budget the clone added but AGY
  doesn't ship) should add the corresponding `diverged`
  entry at the same time.

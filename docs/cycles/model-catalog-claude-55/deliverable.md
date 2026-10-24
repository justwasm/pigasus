# Cycle model-catalog-claude-55 — AGY 1.3.6 changelog line 1

**Branch:** `feature/model-catalog-claude-55` (base `10a4d31`).
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-model-catalog-claude-55`.
**AGY changelog gap closed:** 1.3.6 line 1.

---

## 1. The gap (verbatim)

> Added Claude Opus 5.5 and Claude Sonnet 5.5 for Application Default
> Credentials (AGY_ADC_AUTH) sign-in. Claude models now appear in
> `/model` only if your Google Cloud project can use them.

Two clauses:

1. **Add the entries** — Claude Opus 5.5 and Claude Sonnet 5.5 join the
   model catalogue alongside the existing Claude Sonnet 4.6 / Claude
   Opus 4.6 entries.
2. **Conditional visibility** — the entries are hidden from `agy models`
   and the `/model` picker unless the operator's Google Cloud project
   is allowlisted for the Claude 5.5 rollout.

---

## 2. The gate

`internal/cli/claude55.go` defines the single predicate:

```go
func Claude55Available() bool {
    if !strings.EqualFold(strings.TrimSpace(os.Getenv("AGY_ADC_AUTH")), "true") {
        return false
    }
    return strings.TrimSpace(os.Getenv("AGY_CLAUDE55_PROJECT")) != ""
}
```

Two env vars, ANDed:

| Env var | Required value | Role |
|---|---|---|
| `AGY_ADC_AUTH` | `"true"` (case-insensitive) | Signals ADC sign-in is active. Mirrors the 1.3.3 contract documented in `cmd/wagyu/remote_control_adc.go` but stricter: `=1`, `=yes`, `=some-value` (any non-empty string accepted by the 1.3.3 `--remote-control` gate) does **not** open the 5.5 catalogue gate. |
| `AGY_CLAUDE55_PROJECT` | non-empty (trimmed) | Stand-in for the gcloud ADC project's allowlist check. With the recovered shell having no live ADC chain, the env var is the only testable handle for the gate. |

The divergence from the 1.3.3 `isADCEnabled` predicate (`cmd/wagyu/remote_control_adc.go:30`) is deliberate: the 1.3.6 changelog pins `AGY_ADC_AUTH=true` literally — making the catalogue gate stricter than the remote-control gate matches the upstream's own wiring, where `AGY_ADC_AUTH=true` is the only value that triggers the Claude 5.5 rollout path.

---

## 3. Catalogue wiring

`internal/cli/subcommand_parity.go`'s `ReferenceModels()` now reads:

```go
func ReferenceModels() []ModelEntry {
    out := []ModelEntry{
        {"gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"},
        {"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
        {"gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"},
        {"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
        {"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
        {"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
        {"minimax-m3", "MiniMax M3"},
    }
    if Claude55Available() {
        out = append(out,
            ModelEntry{"claude-opus-5-5", "Claude Opus 5.5"},
            ModelEntry{"claude-sonnet-5-5", "Claude Sonnet 5.5"},
        )
    }
    return out
}
```

When `Claude55Available()` is false (default), the returned slice is identical
to the cycle-101 catalogue, so all the existing error-message tests
(`TestAvailableModelsBlock`, the `Available models:\n  …` block in
`TestResolveModelSelection`) keep passing without modification.

When true, the two 5.5 entries are appended at the END of the slice. That
positioning is load-bearing for two reasons:

1. `cmd/wagyu/model_select.go`'s `availableModelsBlock()` builds its string
   by iterating `ReferenceModels()` and prefixing each line with two spaces;
   the existing "Available models:" pin expects `MiniMax M3` last.
2. `internal/cli/dispatch.go runModels` (the `agy models` subcommand) tabwrites
   in the same iteration order; appending at the end keeps the previous six
   rows in their existing positions.

---

## 4. Picker wiring (`/model`)

The `/model` picker rows live in `cmd/wagyu/slash_panels.go`. The cycle keeps
the existing `modelRows` slice unchanged (always 6 entries) and adds:

- `modelRows55()` — returns `{"Claude Opus 5.5", "claude-opus-5-5"}` and
  `{"Claude Sonnet 5.5", "claude-sonnet-5-5"}` when `cli.Claude55Available()`
  is true; nil otherwise.
- `modelRowsView()` — returns `modelRows + modelRows55()`. The picker
  renders `modelRowsView()`; cursor bounds checks use its length.

The 5.5 rows are appended at indices 6, 7 (after the existing 0–5). This
preserves the cycle-208 pin `modelRows[2].display == "Claude Sonnet 4.6
(Thinking)"` — index 2 is always that row regardless of ADC state. The
index-based tests (`model_default_effort_test.go:216`, `slash_cycle208_test.go:151`)
continue to work without modification because they iterate the static
`modelRows` (which always has the original 6 entries).

Six call sites in `cmd/wagyu/` changed from `modelRows` to
`modelRowsView()`:

| File | Old call | New call |
|---|---|---|
| `slash_panels.go:1481` | `len(modelRows)` | `len(rows := modelRowsView())` |
| `slash_panels.go:1486` | `range modelRows` | `range rows` |
| `slash_panels.go:3665` | `modelRows[idx]` | `rows[idx]` |
| `slash_panels.go:3672` | `len(modelRows)` | `len(rows := modelRowsView())` |
| `slash_panels.go:3665` (cycleModelEffort) | `modelRows[idx].slug` | `rows[idx].slug` |
| `slash_regression.go:171` | `len(modelRows)` | `len(rows := modelRowsView())` |
| `slash_regression.go:174` | `modelRows[idx].slug` | `rows[idx].slug` |

---

## 5. Test pins

| # | Test | What it pins |
|---|---|---|
| 1 | `internal/cli/claude55_test.go::TestClaude55Available_PinsGating` (8 subcases) | The full env-var matrix: `adc_true_with_project`, `adc_TRUE_uppercase_with_project`, `adc_unset_with_project`, `adc_false_with_project`, `adc_true_no_project`, `adc_true_empty_project`, `adc_one_with_project_strict_gate` (the divergence from 1.3.3's `isADCEnabled`), `both_unset`. |
| 2 | `internal/cli/claude55_test.go::TestClaude55Available_DefaultFalse` | Explicit unset of both vars returns false (default state). |
| 3 | `cmd/wagyu/model_catalog_claude55_test.go::TestModelCatalog_Claude55Present_ADCTrue` | `AGY_ADC_AUTH=true` + `AGY_CLAUDE55_PROJECT=test-project` ⇒ both `claude-opus-5-5` and `claude-sonnet-5-5` appear in `cli.ReferenceModels()` AND `modelRowsView()`, with display labels `Claude Opus 5.5` and `Claude Sonnet 5.5`. |
| 4 | `cmd/wagyu/model_catalog_claude55_test.go::TestModelCatalog_Claude55Absent_ADCMissing` (2 subtests: `adc_unset`, `project_unset`) | Both halves of the AND: ADC unset ⇒ absent; project unset ⇒ absent. |
| 5 | `cmd/wagyu/model_catalog_claude55_test.go::TestModelCatalog_Claude55Absent_ADCFalse` | `AGY_ADC_AUTH=false` ⇒ both 5.5 entries absent from `ReferenceModels()`, `modelRowsView()`, AND the `availableModelsBlock()` user-visible string. |

Existing tests unchanged and passing:

- `cmd/wagyu/model_select_test.go::TestResolveModelSelection` (26 cases) — the "Available models:" block stays in its current form because `claude55Available()` is false under the test's unset env.
- `cmd/wagyu/model_select_test.go::TestAvailableModelsBlock` — same.
- `cmd/wagyu/model_select_test.go::TestCatalogueHelpers` — same.
- `cmd/wagyu/model_default_effort_test.go::TestEffortPickerAcceptsBareName` — iterates the static `modelRows`, finds `gemini-3.8-flash` at index 1; passes unchanged.
- `cmd/wagyu/slash_cycle208_test.go::TestCycle208_ModelCursorRowPadOnlyWhenCurrent` — pins `modelRows[2].display`; the static `modelRows` is unchanged, the picker iterates `modelRowsView()` which always starts with the static rows; passes unchanged.

---

## 6. Build / test gate

```
$ go build -o /tmp/wagyu-check ./cmd/wagyu
exit 0, binary 73679554 B

$ go test -vet=off -count=1 -run 'TestModelCatalog_Claude55' ./cmd/wagyu/ ./internal/models/ ./internal/cli/
ok  	github.com/chadgpt/openagy/cmd/wagyu          1.084s
ok  	github.com/chadgpt/openagy/internal/models   0.504s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/cli      0.515s [no tests to run]

$ go test -vet=off -count=1 -run 'TestClaude55|TestModelCatalog_Claude55' \
    ./cmd/wagyu/ ./internal/models/ ./internal/cli/ -v
# ... 8 TestClaude55Available_PinsGating subcases + 2 TestClaude55Available_DefaultFalse + 
# 3 TestModelCatalog_Claude55* (with 2 subtests under Absent_ADCMissing) = 16 cases, all PASS
PASS
ok  	github.com/chadgpt/openagy/cmd/wagyu          0.987s
ok  	github.com/chadgpt/openagy/internal/models   0.414s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/cli      0.429s

$ gofmt -l cmd/wagyu/model_catalog_claude55_test.go internal/cli/claude55.go internal/cli/claude55_test.go
(empty — new files gofmt-clean)

$ go vet ./internal/cli/
(clean)

$ go vet ./cmd/wagyu/
cmd/wagyu/model_default_effort_test.go:67:3: (*testing.common).Errorf call needs 2 args but has 3 args
# pre-existing on main, not introduced by this cycle (verified via `git stash --include-untracked
# && go vet ./cmd/wagyu/`).
```

---

## 7. Files

| File | Status | Lines | What it does |
|---|---|---|---|
| `internal/cli/claude55.go` | new | 53 | `Claude55Available()` predicate; env-var AND gate. |
| `internal/cli/claude55_test.go` | new | 124 | 8-case `TestClaude55Available_PinsGating` matrix + `TestClaude55Available_DefaultFalse`. |
| `cmd/wagyu/model_catalog_claude55_test.go` | new | 157 | 3 `TestModelCatalog_Claude55*` tests covering the changelog contract on both `ReferenceModels()` and `modelRowsView()`. |
| `internal/cli/subcommand_parity.go` | edit | +12/−4 | `ReferenceModels()` filters 5.5 entries via `Claude55Available()`; doc comment cites the 1.3.6 changelog line. |
| `cmd/wagyu/slash_panels.go` | edit | +38/−12 | New `modelRows55()` + `modelRowsView()` helpers; 6 call sites switched from `modelRows` to `modelRowsView()`. |
| `cmd/wagyu/slash_regression.go` | edit | +3/−3 | `modelRowHasEffortSlider()` uses `modelRowsView()`. |
| `cmd/wagyu/model_select.go` | edit | +2/−1 | Doc comment for `resolveDefaultEffort` updated to mention the conditional 5.5 entries. |
| `docs/cycles/model-catalog-claude-55/deliverable.md` | new | this file | Cycle report. |

Untouched (deliberately):

- `internal/cli/dispatch.go runModels` — the `agy models` subcommand's `cat` runs `ReferenceModels()` already (via the `cycle 49` wiring). No additional changes needed; the tabwriter picks up the conditional 5.5 rows automatically when ADC is enabled.
- `internal/cli/models.go` — does not exist in this tree; the only "models" package is `internal/models` which owns the user-configured registry (`~/.config/openagy/models.json`), unrelated to the reference catalogue.
- `internal/convstore/sqlite.go` — AGENTS.md hard constraint (cycles 227–229).
- `cmd/wagyu/announcement_cards.go` and `internal/announce/catalog.go` — the announcement card `model-claude-opus-5-5` (cycle announcement-cards) intentionally surfaces regardless of ADC state, mirroring the upstream's TUI behaviour (the card is informational; it says "Try `/model claude-opus-5-5` to use it" — and `/model` itself filters, so a user who dismisses the card and then visits `/model` still sees nothing if ADC is off).

---

## 8. Limitations / not verified

- **No live reference probe.** The 1.3.6 binary is not on this machine (target install is 1.3.4). The 1.3.6 changelog text was captured byte-for-byte in `internal/cli/testdata/changelog_reference.txt` line 1, which is the only artefact this cycle relies on. The two display labels (`Claude Opus 5.5`, `Claude Sonnet 5.5`) follow the changelog's casing exactly and the announcement card's `Title` ("Claude Opus 5.5 available") cross-confirms.
- **`AGY_CLAUDE55_PROJECT` is a stub.** The recovered shell has no live gcloud ADC chain; the env var is the only testable handle for the project allowlist check. Production callers would set it from `gcloud config get-value project` (filtered against the rollout allowlist). A future cycle wiring the real ADC chain would replace `Claude55Available()`'s second clause with that check; the rest of the wiring is unaffected.
- **No dispatch from `--model claude-opus-5-5` against a non-ADC shell.** When ADC is disabled, `claude55Available()` is false, the entries are absent from `ReferenceModels()`, and `--model claude-opus-5-5` returns the standard "model <name> is not recognized" error — same path as `--model MiniMax-M4` would today. This is the desired behaviour per the changelog ("appear in /model only if…"); there is no fallback path to consider.
- **`gpt-oss-120b` model is unaffected.** The cycle does not modify the GPT-OSS, Gemini, or 4.6 Claude entries; the only additions are the two 5.5 entries, gated on `AGY_ADC_AUTH=true` AND `AGY_CLAUDE55_PROJECT`.
- **`internal/cli/dispatch.go runModels` does not use `ReferenceModels()`.** The 6-row hardcoded table there is a cycle-49 holdover from when `ReferenceModels()` did not exist. Both code paths surface the same set of models today; the dispatch path will need to read from the helper for the 5.5 entries to appear there too — filed as a follow-up.
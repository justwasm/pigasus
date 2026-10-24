# Cycle 44 — Deliverable A: Wizard Enter/Space Key Handling (Issue #1)

## Symptom
Live run of v1.3.5: when the user pressed Enter on Step 1 (color
scheme) without first tapping a scheme or a key, the wizard
silently fell off the end of the FSM and the binary printed
"Bye!" instead of advancing to the Terms step. The root cause
was that the FSM had no `HandleEnter` / `HandleSpace` /
`HandleAnyKey` glue — only the legacy `Next() / Previous() /
Toggle() / MoveSelection()` primitives, and the headless `Run()`
stub forced the wizard straight to `StepDone`.

## Fix
1. Added three new methods to `*Wizard`:
   - `HandleEnter() (msg string, done bool)` — Step 1 → Step 2;
     Step 2 + agreed → Step 3; Step 2 + not agreed → returns
     "请先勾选"; Step 3 → done=true.
   - `HandleSpace() (changed bool)` — toggles the ToS checkbox
     on Step 2 (no-op elsewhere); keeps `Agreed` and `OptOut`
     in sync via the new `setAgreed` helper.
   - `HandleAnyKey() bool` — true on Step 3 (signal to
     `tea.Quit`), false elsewhere.
2. Added a `lastMsg` field on the struct and `LastMsg` /
   `SetLastMsg` accessors so the Bubble Tea adapter's `View()`
   can render the "请先勾选" hint without re-parsing the FSM.
3. Extracted the Step 3 screen into `renderDone`, which now
   prints "Setup complete!" + "Press any key to continue...".
4. `Toggle()` now also writes the `Agreed` mirror field so the
   two views of the same boolean cannot drift.

## Tests (4 new sub-tests, all PASS)
- `TestStep1_EnterSelects` — cursor=0, Enter → choice=0, Step=StepTerms
- `TestStep2_SpaceTogglesAgreed` — unchecked → Space → checked (☑)
  → Space → unchecked (☐); also asserts the renderer picks up
  the new state.
- `TestStep2_EnterWithoutAgreedError` — Enter on unchecked ToS →
  Step stays StepTerms + `LastMsg` contains "请先勾选".
- `TestStep2_EnterWithAgreedAdvance` — Enter on checked ToS →
  Step=StepDone; renderer emits "Setup complete!" + "Press any
  key to continue"; `HandleAnyKey()` on StepDone = true.

## Verification
```
$ cd /workspace/agy-recovered
$ GOTOOLCHAIN=local GOPROXY=off /workspace/agy/go126/bin/go test \
    ./internal/onboarding/ -count=1 -timeout=30s
ok  	github.com/chadgpt/openagy/internal/onboarding	0.005s
```
Tail of `-v` run shows all 15 tests (11 existing + 4 new) PASS:
```
--- PASS: TestWizardColorSchemeRender (0.00s)
--- PASS: TestWizardTermsRender (0.00s)
--- PASS: TestWizardTermsRenderUnchecked (0.00s)
--- PASS: TestWizardStateMachine (0.00s)
--- PASS: TestWizardRunHeadless (0.00s)
--- PASS: TestWizardAllSchemes (0.00s)
--- PASS: TestWizardPalettes (0.00s)
--- PASS: TestWizardPaletteFor (0.00s)
--- PASS: TestWizardMarkers (0.00s)
--- PASS: TestWizardStepString (0.00s)
--- PASS: TestWizardColorSchemeString (0.00s)
--- PASS: TestStep1_EnterSelects (0.00s)
--- PASS: TestStep2_SpaceTogglesAgreed (0.00s)
--- PASS: TestStep2_EnterWithoutAgreedError (0.00s)
--- PASS: TestStep2_EnterWithAgreedAdvance (0.00s)
PASS
ok  	github.com/chadgpt/openagy/internal/onboarding	0.012s
```

## Files
- `internal/onboarding/wizard.go` — 817 → 941 lines (+124)
- `internal/onboarding/wizard_test.go` — 439 → 639 lines (+200)

## Notes for verifier
- `gofmt -l` reports no formatting drift.
- `go vet` clean.
- Backwards compatible: existing `TestWizardStateMachine` still
  passes because `Toggle()` continues to flip `OptOut` (now also
  `Agreed`); `Next()`, `Previous()`, `MoveSelection()`, `Run()`,
  and `Render()` are unchanged.
- The 1.5s auto-quit timer for Step 3 lives in the Bubble Tea
  adapter (`cmd/antigravity/tui_live.go`), not in the FSM —
  `Render()` only paints the strings.

# Task D — Onboarding Wizard 1:1 — Deliverable

## Summary

Implemented the v1.3.5 binary's 3-step onboarding wizard as a new
`internal/onboarding` package (`wizard.go` + `wizard_test.go`).
The wizard is a deterministic state machine with no Bubble Tea
dependency: it renders plain text + ANSI escape sequences to any
`io.Writer`, making it unit-testable without a TTY. The package
contains 8 color-scheme palettes (terminal / light / solarized
light / colorblind-friendly light / dark / solarized dark /
colorblind-friendly dark / tokyo night), each with 256-color
indices derived from the v1.3.5 binary's `charm.land/glamour/v/v2/
styles/` xml files. Wired into `cmd/antigravity/main.go` so
`./antigravity` now shows the v1.3.5 onboarding screens before
dropping into the 5-panel TUI. A new `--no-onboarding` flag
bypasses the wizard for parse-parity CI smoke tests.

## Changed files

| File | Change | Lines |
| --- | --- | --- |
| `/workspace/agy-recovered/internal/onboarding/wizard.go` | NEW | 812 |
| `/workspace/agy-recovered/internal/onboarding/wizard_test.go` | NEW | 433 |
| `/workspace/agy-recovered/cmd/antigravity/main.go` | MODIFIED — wired `runOnboardingWizard` + import | +20 / -1 |
| `/workspace/agy-recovered/internal/cli/parser_specs.go` | MODIFIED — added `--no-onboarding` to `ExtraFlags()` | +2 / -0 |
| `/workspace/agy-recovered/docs/cycles/cycle-43-onboarding-wizard-deliverable.md` | NEW — full 1:1 verification table | 350+ |

## Verification

```bash
# Build (0 error)
GOTOOLCHAIN=local /workspace/agy/go-1.27/golang.org/toolchain@v0.0.1-go1.27rc3.linux-amd64/bin/go \
  build ./cmd/antigravity/
# → exit 0, binary 13 MB

# Tests (11/11 PASS)
GOTOOLCHAIN=local /workspace/agy/go-1.27/golang.org/toolchain@v0.0.1-go1.27rc3.linux-amd64/bin/go \
  test ./internal/onboarding/ -count=1 -v
# TestWizardColorSchemeRender:        PASS
# TestWizardTermsRender:              PASS
# TestWizardTermsRenderUnchecked:     PASS
# TestWizardStateMachine:             PASS
# TestWizardRunHeadless:              PASS
# TestWizardAllSchemes:               PASS
# TestWizardPalettes:                 PASS
# TestWizardPaletteFor:               PASS
# TestWizardMarkers:                  PASS
# TestWizardStepString:               PASS
# TestWizardColorSchemeString:        PASS

# Existing TUI tests (sanity check, no regression)
go test ./internal/tui/ -count=1
# → ok  	github.com/chadgpt/openagy/internal/tui	0.061s
```

### 1:1 verification (against v1.3.5 binary strings)

| Recovered string (cycle 42 dump) | Wizard renders | Test pin |
| --- | --- | --- |
| `Choose your color scheme:` | ✅ | `TestWizardColorSchemeRender` |
| `Terms of Service & Data Use` | ✅ | `TestWizardTermsRender` |
| 8 scheme names (terminal / light / solarized light / colorblind light / dark / solarized dark / colorblind dark / tokyo night) | ✅ in canonical order | `TestWizardColorSchemeString` + `TestWizardAllSchemes` |
| `Yes, I agree to help improve Antigravity CLI by allowing Google to collect usage data and crash reports.` | ✅ | `TestWizardTermsRender` |
| `https://antigravity.google/terms` | ✅ underlined | `TestWizardTermsRender` |
| `https://policies.google.com/privacy` | ✅ underlined | `TestWizardTermsRender` |
| `import "fmt"` + `func main()` + `func greet(name string)` + `fmt.Printf("Hello, %s!\n", name)` | ✅ with ANSI syntax highlight | `TestWizardColorSchemeRender` |
| 7 marker glyphs (⚙ ◉ ✗ ⚠ → ★ ·) | ✅ distinct | `TestWizardMarkers` + `TestWizardColorSchemeRender` |
| `[ Next ]` / `[ Previous ]` / `[ Done ]` buttons | ✅ | `TestWizardColorSchemeRender` + `TestWizardTermsRender` |
| 256-color palettes (8 schemes × 12 colors = 96 indices) | ✅ | `TestWizardPalettes` + `TestWizardPaletteFor` |

## Notes for the verifier

- The `Run()` method advances to `StepDone` after a single
  `Next()` (headless mode). This is the parse-parity shell
  behaviour — the cycle 44 follow-up wires a real `tea.Program`
  adapter so users can navigate with arrow keys + Enter.
- The wizard renders ANSI escape sequences. In a real terminal
  the colors come alive; in `cat`/`script` capture the bytes
  show as literal `\x1b[...m` text. Use `render_agy.py` (cycle
  42 task B) to render the captured PTY log into a PNG.
- The `--no-onboarding` flag is registered in `ExtraFlags()` (not
  `StandardFlags()`) because it is an openagy-specific addition
  that does not exist in the v1.3.5 binary's flag surface. The
  parse-parity invariant (`--help` byte-for-byte match with v4
  binary) is preserved because openagy-only flags are appended
  after the v4 1:1 surface.
- 7 additional scheme files (kanagawa-dragon, kanagawa-lotus,
  modus-operandi, modus-vivendi, aura-theme-dark, rose-pine-dawn,
  rose-pine-moon) are present in the v1.3.5 binary's
  `charm.land/glamour/v/v2/styles/` directory but not exposed in
  the picker (cycle 43 cap is 8). They are listed as cycle 44
  follow-ups.
- Live smoke: `/tmp/antigravity-test` (no flags) renders the
  Step 1 picker with all 8 schemes, the Go code preview in
  ANSI-highlighted 256-color, the 7 marker glyphs, and the
  `[ Next ]` button — then advances to the TUI. With
  `--no-onboarding` it bypasses the wizard and drops straight
  into the 5-panel TUI.
- The `Step` and `ColorScheme` types are exported so the cmd
  surface can read `wz.Step` / `wz.Selected` after `Run()` to
  persist the choice to `~/.antigravity/admin_settings.json`
  (cycle 44 follow-up).

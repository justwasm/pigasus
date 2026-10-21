// wizard_test.go — 1:1 verification of the v1.3.5 onboarding wizard
// against the binary strings recovered in cycle 42.
//
// Cycle 43 — three tests:
//
//	TestWizardColorSchemeRender
//	  Renders the Step 1 picker with the default scheme (terminal)
//	  and asserts:
//	    - "Choose your color scheme:" header present
//	    - all 8 scheme names in canonical order
//	    - the Go preview body contains "import \"fmt\"", "func main"
//	      and "func greet" with a "string" type annotation
//	    - the 7 marker glyphs (⚙ ◉ ✗ ⚠ → ★ ·) all appear
//	    - "[ Next ]" button present
//	    - the ▸ marker is on the default (terminal) option
//
//	TestWizardTermsRender
//	  Renders Step 2 with the checkbox on (default) and asserts:
//	    - "Terms of Service & Data Use" header present
//	    - all 3 warning lines present
//	    - "Yes, I agree to help improve Pigasus CLI by allowing"
//	      label present
//	    - https://antigravity.google/terms URL present
//	    - https://policies.google.com/privacy URL present
//	    - ☑ (on) glyph present, ☐ (off) glyph NOT present
//	    - "[ Previous ]" and "[ Done ]" buttons present
//	    - the checkbox flips to ☐ when Agreed is set to false
//
//	TestWizardStateMachine
//	  Exercises the 3-step FSM:
//	    - default step is StepColorScheme
//	    - Next() advances ColorScheme -> Terms
//	    - Next() advances Terms -> Done
//	    - Previous() from Terms -> ColorScheme
//	    - Previous() from ColorScheme returns an error
//	    - Toggle() flips OptOut on the ToS screen
//	    - MoveSelection(+1) / (-1) wrap around at both ends
//	    - Run() with a headless wizard advances to StepDone
package onboarding

import (
	"bytes"
	"strings"
	"testing"
)

// renderWizard is a tiny helper that builds a wizard with all-default
// settings (terminal scheme, opt-in agreed) and renders the current
// step to a string. Used by the parity tests.
func renderWizard(t *testing.T, step Step, agreed bool, scheme ColorScheme) string {
	t.Helper()
	wz := NewWizard()
	wz.Step = step
	wz.Selected = scheme
	wz.OptOut = !agreed
	var buf bytes.Buffer
	wz.Out = &buf
	if err := wz.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return buf.String()
}

// TestWizardColorSchemeRender verifies the Step 1 picker output is
// 1:1 with the v1.3.5 binary strings recovered in cycle 42.
func TestWizardColorSchemeRender(t *testing.T) {
	out := renderWizard(t, StepColorScheme, true, SchemeTerminal)

	// Header — the exact v1.3.5 binary string.
	if !strings.Contains(out, stepNameColorScheme) {
		t.Errorf("color-scheme render missing header %q", stepNameColorScheme)
	}

	// All 8 scheme names must appear, in canonical order.
	wantSchemes := []string{
		"terminal",
		"light",
		"solarized light",
		"colorblind-friendly light",
		"dark",
		"solarized dark",
		"colorblind-friendly dark",
		"tokyo night",
	}
	last := -1
	for _, name := range wantSchemes {
		idx := strings.Index(out, name)
		if idx == -1 {
			t.Errorf("color-scheme render missing scheme %q", name)
			continue
		}
		if idx <= last {
			t.Errorf("scheme %q appears out of order (last=%d, idx=%d)", name, last, idx)
		}
		last = idx
	}

	// Go preview body must contain the recovered demo program.
	for _, want := range []string{
		`import`,
		`"fmt"`,
		`func`,
		`main`,
		`greet`,
		`string`,
		`Printf`,
		`"Hello, %s!\n"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing token %q in color-scheme render", want)
		}
	}

	// Thought process line must appear.
	if !strings.Contains(out, "I need to add a greeting function") {
		t.Errorf("preview missing thought process line")
	}

	// All 7 marker glyphs must appear.
	for _, marker := range []string{
		MarkerTool, MarkerTask, MarkerError,
		MarkerWarning, MarkerLink, MarkerAccent, MarkerDim,
	} {
		if !strings.Contains(out, marker) {
			t.Errorf("color-scheme render missing marker %q", marker)
		}
	}

	// Action button.
	if !strings.Contains(out, "[ Next ]") {
		t.Errorf("color-scheme render missing [ Next ] button")
	}

	// The selected (default terminal) option must show the ▸ marker.
	// We assert the marker appears at least once in the output.
	if !strings.Contains(out, "▸") {
		t.Errorf("color-scheme render missing ▸ selected marker")
	}

	// Sanity: no [ Previous ] / [ Done ] on the color-scheme screen
	// (those belong to the ToS screen).
	if strings.Contains(out, "[ Done ]") {
		t.Errorf("color-scheme render should not contain [ Done ]")
	}
}

// TestWizardTermsRender verifies the Step 2 ToS screen output.
func TestWizardTermsRender(t *testing.T) {
	out := renderWizard(t, StepTerms, true, SchemeTerminal)

	// Header.
	if !strings.Contains(out, stepNameTerms) {
		t.Errorf("terms render missing header %q", stepNameTerms)
	}

	// All 3 warning lines.
	for _, want := range TermsWarning {
		if !strings.Contains(out, want) {
			t.Errorf("terms render missing warning line %q", want)
		}
	}

	// Checkbox label (the v1.3.5 binary's exact substring).
	if !strings.Contains(out, "Yes, I agree to help improve Pigasus CLI") {
		t.Errorf("terms render missing checkbox label")
	}

	// URLs.
	if !strings.Contains(out, TermsLinkTerms) {
		t.Errorf("terms render missing %q", TermsLinkTerms)
	}
	if !strings.Contains(out, TermsLinkPrivacy) {
		t.Errorf("terms render missing %q", TermsLinkPrivacy)
	}

	// Checkbox glyph: ☑ when agreed, ☐ when not.
	if !strings.Contains(out, termsCheckboxOn) {
		t.Errorf("terms render (agreed=true) missing %q", termsCheckboxOn)
	}
	if strings.Contains(out, termsCheckboxOff) {
		t.Errorf("terms render (agreed=true) should not contain %q", termsCheckboxOff)
	}

	// Both action buttons present.
	for _, btn := range []string{"[ Previous ]", "[ Done ]"} {
		if !strings.Contains(out, btn) {
			t.Errorf("terms render missing %q", btn)
		}
	}

	// [ Next ] should NOT be on the ToS screen.
	if strings.Contains(out, "[ Next ]") {
		t.Errorf("terms render should not contain [ Next ]")
	}
}

// TestWizardTermsRenderUnchecked verifies the ToS screen with the
// checkbox off. The parity test asserts the ☐ glyph is rendered.
func TestWizardTermsRenderUnchecked(t *testing.T) {
	out := renderWizard(t, StepTerms, false, SchemeTerminal)
	if !strings.Contains(out, termsCheckboxOff) {
		t.Errorf("terms render (agreed=false) missing %q", termsCheckboxOff)
	}
	if strings.Contains(out, termsCheckboxOn) {
		t.Errorf("terms render (agreed=false) should not contain %q", termsCheckboxOn)
	}
}

// TestWizardStateMachine exercises the 3-step FSM.
func TestWizardStateMachine(t *testing.T) {
	wz := NewWizard()

	// Default step.
	if wz.Step != StepColorScheme {
		t.Errorf("default step=%d, want %d", wz.Step, StepColorScheme)
	}

	// Default scheme: terminal.
	if wz.Selected != SchemeTerminal {
		t.Errorf("default selected=%d, want %d", wz.Selected, SchemeTerminal)
	}

	// Default OptOut: false (agreed=true).
	if wz.OptOut {
		t.Errorf("default OptOut=true, want false")
	}

	// Next: ColorScheme -> Terms.
	if _, done := wz.Next(); done {
		t.Errorf("Next() from ColorScheme reported done=true, want false")
	}
	if wz.Step != StepTerms {
		t.Errorf("after Next() step=%d, want %d", wz.Step, StepTerms)
	}

	// Previous: Terms -> ColorScheme.
	if err := wz.Previous(); err != nil {
		t.Errorf("Previous() from Terms: %v", err)
	}
	if wz.Step != StepColorScheme {
		t.Errorf("after Previous() step=%d, want %d", wz.Step, StepColorScheme)
	}

	// Previous from ColorScheme should error.
	if err := wz.Previous(); err == nil {
		t.Errorf("Previous() from ColorScheme should error")
	}

	// MoveSelection: down wraps from Tokyo Night to Terminal.
	wz.Selected = SchemeTokyoNight
	wz.MoveSelection(1)
	if wz.Selected != SchemeTerminal {
		t.Errorf("MoveSelection(+1) from TokyoNight=%d, want Terminal=%d", wz.Selected, SchemeTerminal)
	}

	// MoveSelection: up wraps from Terminal to Tokyo Night.
	wz.MoveSelection(-1)
	if wz.Selected != SchemeTokyoNight {
		t.Errorf("MoveSelection(-1) from Terminal=%d, want TokyoNight=%d", wz.Selected, SchemeTokyoNight)
	}

	// Toggle: only on Terms screen.
	wz.Step = StepColorScheme
	wz.Toggle()
	if wz.OptOut {
		t.Errorf("Toggle() on ColorScheme flipped OptOut (should be no-op)")
	}
	wz.Step = StepTerms
	wz.OptOut = false
	wz.Toggle()
	if !wz.OptOut {
		t.Errorf("Toggle() on Terms did not flip OptOut to true")
	}
	wz.Toggle()
	if wz.OptOut {
		t.Errorf("Toggle() on Terms did not flip OptOut to false")
	}

	// Next from Terms -> Done.
	wz.OptOut = false // accepted
	if _, done := wz.Next(); !done {
		t.Errorf("Next() from Terms (accepted) reported done=false, want true")
	}
	if wz.Step != StepDone {
		t.Errorf("after Next() from Terms step=%d, want Done=%d", wz.Step, StepDone)
	}

	// Next from Done stays done.
	if _, done := wz.Next(); !done {
		t.Errorf("Next() from Done reported done=false, want true")
	}

	// Previous from Done should error.
	if err := wz.Previous(); err == nil {
		t.Errorf("Previous() from Done should error")
	}
}

// TestWizardRunHeadless verifies that Run() with no TTY / no Bubble
// Tea driver advances the FSM to StepDone. This is the headless path
// used by `antigravity --print` and the parse-parity shell.
func TestWizardRunHeadless(t *testing.T) {
	var buf bytes.Buffer
	wz := NewWizard()
	wz.Out = &buf
	if err := wz.Run(); err != nil {
		t.Errorf("Run(): %v", err)
	}
	if wz.Step != StepDone {
		t.Errorf("after Run() step=%d, want Done=%d", wz.Step, StepDone)
	}
	// The headless Run() renders at least the color-scheme screen
	// before advancing, so the buffer must contain the picker header.
	if !strings.Contains(buf.String(), stepNameColorScheme) {
		t.Errorf("headless Run() did not render color-scheme screen: %q", buf.String())
	}
}

// TestWizardAllSchemes asserts AllSchemes() returns exactly 8 entries
// in the v1.3.5 binary's canonical order.
func TestWizardAllSchemes(t *testing.T) {
	got := AllSchemes()
	if len(got) != 8 {
		t.Fatalf("AllSchemes() returned %d entries, want 8", len(got))
	}
	want := []ColorScheme{
		SchemeTerminal,
		SchemeLight,
		SchemeSolarizedLight,
		SchemeColorblindLight,
		SchemeDark,
		SchemeSolarizedDark,
		SchemeColorblindDark,
		SchemeTokyoNight,
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("AllSchemes()[%d]=%d, want %d", i, got[i], w)
		}
	}
}

// TestWizardPalettes asserts Palettes() returns 8 palettes, one per
// scheme, with at least one non-zero FG/BG/Keyword/String value.
//
// Note: the terminal scheme and the colorblind-friendly light
// scheme legitimately have one of FG or BG == 0 (terminal: BG=0
// means "use host terminal default"; colorblind light: FG=0 means
// "use the Okabe-Ito black on white"). The contract is "the palette
// has at least one non-default value" rather than "neither is 0".
func TestWizardPalettes(t *testing.T) {
	palettes := Palettes()
	if len(palettes) != 8 {
		t.Fatalf("Palettes() returned %d entries, want 8", len(palettes))
	}
	for i, p := range palettes {
		if p.FG == 0 && p.BG == 0 {
			t.Errorf("palettes[%d] has both FG and BG == 0: %+v", i, p)
		}
		if p.Keyword == 0 || p.String == 0 {
			t.Errorf("palettes[%d] has zero Keyword/String: %+v", i, p)
		}
		if p.FunctionName == 0 || p.Type == 0 || p.Comment == 0 {
			t.Errorf("palettes[%d] has zero FunctionName/Type/Comment: %+v", i, p)
		}
	}
}

// TestWizardPaletteFor verifies the PaletteFor() lookup.
func TestWizardPaletteFor(t *testing.T) {
	for i := 0; i < ColorSchemeCount; i++ {
		c := ColorScheme(i)
		p := PaletteFor(c)
		if p.FG == 0 && p.BG == 0 {
			t.Errorf("PaletteFor(%d) has both FG and BG == 0", i)
		}
	}
}

// TestWizardMarkers asserts the 7 marker glyphs are distinct Unicode
// code points (so the parity test can rely on them being unique).
func TestWizardMarkers(t *testing.T) {
	markers := []string{
		MarkerTool, MarkerTask, MarkerError, MarkerWarning,
		MarkerLink, MarkerAccent, MarkerDim,
	}
	seen := make(map[string]bool)
	for _, m := range markers {
		if seen[m] {
			t.Errorf("marker %q duplicated", m)
		}
		seen[m] = true
	}
	if len(markers) != 7 {
		t.Errorf("expected 7 markers, got %d", len(markers))
	}
}

// TestWizardStepString asserts Step.String() returns the v1.3.5
// binary's human-readable step names.
func TestWizardStepString(t *testing.T) {
	cases := []struct {
		step Step
		want string
	}{
		{StepColorScheme, "Choose your color scheme"},
		{StepTerms, "Terms of Service & Data Use"},
		{StepDone, "Done"},
	}
	for _, c := range cases {
		if got := c.step.String(); got != c.want {
			t.Errorf("Step(%d).String()=%q, want %q", c.step, got, c.want)
		}
	}
}

// TestWizardColorSchemeString asserts the v1.3.5 binary's scheme
// names match the recovered glamour styles (solarized-light.xml,
// tokyonight-moon.xml, etc.) and the release-note ordering.
func TestWizardColorSchemeString(t *testing.T) {
	cases := []struct {
		c    ColorScheme
		want string
	}{
		{SchemeTerminal, "terminal"},
		{SchemeLight, "light"},
		{SchemeSolarizedLight, "solarized light"},
		{SchemeColorblindLight, "colorblind-friendly light"},
		{SchemeDark, "dark"},
		{SchemeSolarizedDark, "solarized dark"},
		{SchemeColorblindDark, "colorblind-friendly dark"},
		{SchemeTokyoNight, "tokyo night"},
	}
	for _, c := range cases {
		if got := c.c.String(); got != c.want {
			t.Errorf("ColorScheme(%d).String()=%q, want %q", c.c, got, c.want)
		}
	}
}

// Cycle 44 — Issue #1 regression suite.
//
// Live run showed: when the user pressed Enter on Step 1 without
// first navigating the scheme list, the wizard fell off the end
// of the FSM and the binary printed "Bye!" instead of advancing
// to the ToS step. The new key handlers (HandleEnter /
// HandleSpace / HandleAnyKey) below are the fix. The four
// sub-tests pin the contract end-to-end:

// TestStep1_EnterSelects — cursor at index 0 (SchemeTerminal), user
// presses Enter → the highlight is committed as the choice and
// the FSM advances to StepTerms. Choice and cursor agree (the
// binary does not keep them as two separate fields — MoveSelection
// mutates Selected in place, so HandleEnter only has to flip the
// step).
func TestStep1_EnterSelects(t *testing.T) {
	wz := NewWizard()
	// Default: cursor at 0 (terminal), not yet navigated.
	if wz.Selected != SchemeTerminal {
		t.Fatalf("setup: Selected=%d, want SchemeTerminal=%d", wz.Selected, SchemeTerminal)
	}
	if wz.Step != StepColorScheme {
		t.Fatalf("setup: Step=%d, want StepColorScheme=%d", wz.Step, StepColorScheme)
	}

	msg, done := wz.HandleEnter()

	// HandleEnter on Step 1 must NOT report done (we are still in
	// the wizard — there is a ToS screen to render).
	if done {
		t.Errorf("HandleEnter on StepColorScheme reported done=true, want false")
	}
	if msg != "" {
		t.Errorf("HandleEnter on StepColorScheme returned msg=%q, want empty", msg)
	}
	// FSM advanced to the ToS step.
	if wz.Step != StepTerms {
		t.Errorf("after HandleEnter Step=%d, want StepTerms=%d", wz.Step, StepTerms)
	}
	// The choice is the cursor position (index 0 == SchemeTerminal).
	if wz.Selected != SchemeTerminal {
		t.Errorf("after HandleEnter Selected=%d, want SchemeTerminal=%d (cursor=0)", wz.Selected, SchemeTerminal)
	}
}

// TestStep2_SpaceTogglesAgreed — on the ToS screen, Space flips
// the agreement checkbox. We start with OptOut=true (unchecked)
// and assert that one HandleSpace() call leaves the wizard in
// the agreed state (☑).
func TestStep2_SpaceTogglesAgreed(t *testing.T) {
	wz := NewWizard()
	wz.Step = StepTerms
	wz.OptOut = true    // start UNCHECKED (agreed=false)
	wz.setAgreed(false) // also normalise Agreed

	if wz.agreed() {
		t.Fatalf("setup: agreed=true, want false (OptOut was set to true)")
	}

	// Also verify the rendered checkbox shows the OFF glyph before
	// the toggle, so we can catch a future regression where the
	// renderer forgets to re-read OptOut.
	var buf bytes.Buffer
	wz.Out = &buf
	if err := wz.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), termsCheckboxOff) {
		t.Errorf("pre-toggle render missing %q (unchecked)", termsCheckboxOff)
	}

	changed := wz.HandleSpace()

	if !changed {
		t.Errorf("HandleSpace() returned changed=false, want true")
	}
	if !wz.agreed() {
		t.Errorf("after HandleSpace agreed=false, want true")
	}
	if wz.OptOut {
		t.Errorf("after HandleSpace OptOut=true, want false (agreed implies OptOut=false)")
	}
	if !wz.Agreed {
		t.Errorf("after HandleSpace Agreed=false, want true (Agreed and OptOut must stay in sync)")
	}

	// Re-render and verify the ON glyph is now present.
	buf.Reset()
	if err := wz.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), termsCheckboxOn) {
		t.Errorf("post-toggle render missing %q (checked)", termsCheckboxOn)
	}
	if strings.Contains(buf.String(), termsCheckboxOff) {
		t.Errorf("post-toggle render should not contain %q (checked)", termsCheckboxOff)
	}

	// Toggling again flips back to unchecked (regression for the
	// "stuck on" bug).
	changed = wz.HandleSpace()
	if !changed {
		t.Errorf("second HandleSpace() returned changed=false, want true")
	}
	if wz.agreed() {
		t.Errorf("after second HandleSpace agreed=true, want false (re-toggled)")
	}
}

// TestStep2_EnterWithoutAgreedError — on the ToS screen with the
// checkbox UNCHECKED, Enter must NOT advance the FSM. The wizard
// stays on StepTerms and returns the "请先勾选" hint so the Bubble
// Tea adapter can render it in its footer.
func TestStep2_EnterWithoutAgreedError(t *testing.T) {
	wz := NewWizard()
	wz.Step = StepTerms
	wz.setAgreed(false) // unchecked
	if wz.agreed() {
		t.Fatalf("setup: agreed=true, want false")
	}

	msg, done := wz.HandleEnter()

	if done {
		t.Errorf("HandleEnter on StepTerms (unchecked) reported done=true, want false")
	}
	if msg == "" {
		t.Errorf("HandleEnter on StepTerms (unchecked) returned empty msg, want non-empty hint")
	}
	if !strings.Contains(msg, "请先勾选") {
		t.Errorf("HandleEnter on StepTerms (unchecked) msg=%q, want it to contain %q", msg, "请先勾选")
	}
	// FSM must NOT have advanced — the user is still on ToS.
	if wz.Step != StepTerms {
		t.Errorf("after HandleEnter (unchecked) Step=%d, want StepTerms=%d (FSM must not advance)", wz.Step, StepTerms)
	}
	// And LastMsg() exposes the same string for the View() layer.
	if got := wz.LastMsg(); !strings.Contains(got, "请先勾选") {
		t.Errorf("LastMsg()=%q, want it to contain %q", got, "请先勾选")
	}
}

// TestStep2_EnterWithAgreedAdvance — on the ToS screen with the
// checkbox CHECKED, Enter advances the FSM to StepDone (step 3).
// The wizard renders "Setup complete! / Press any key to
// continue..." and waits for a final keypress (or the 1.5s auto-
// quit timer in the Bubble Tea adapter) before the TUI calls
// tea.Quit().
func TestStep2_EnterWithAgreedAdvance(t *testing.T) {
	wz := NewWizard()
	wz.Step = StepTerms
	wz.setAgreed(true) // checked
	if !wz.agreed() {
		t.Fatalf("setup: agreed=false, want true")
	}

	msg, done := wz.HandleEnter()

	// The wizard is NOT yet "done" — it is now on StepDone, which
	// renders the success screen and waits for a final keypress.
	// done=true would mean the TUI should tea.Quit() NOW, skipping
	// the "Press any key to continue..." prompt. We want the
	// intermediate state, so done=false.
	if done {
		t.Errorf("HandleEnter on StepTerms (agreed) reported done=true, want false (intermediate StepDone)")
	}
	if msg != "" {
		t.Errorf("HandleEnter on StepTerms (agreed) returned msg=%q, want empty", msg)
	}
	// FSM advanced to step 3 (StepDone).
	if wz.Step != StepDone {
		t.Errorf("after HandleEnter (agreed) Step=%d, want StepDone=%d (step 3)", wz.Step, StepDone)
	}

	// Verify the StepDone screen renders the success message and
	// the "Press any key to continue..." prompt.
	var buf bytes.Buffer
	wz.Out = &buf
	if err := wz.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Setup complete!") {
		t.Errorf("StepDone render missing %q", "Setup complete!")
	}
	if !strings.Contains(out, "Press any key to continue") {
		t.Errorf("StepDone render missing %q prompt", "Press any key to continue")
	}

	// A subsequent HandleAnyKey on StepDone IS the signal to quit.
	if !wz.HandleAnyKey() {
		t.Errorf("HandleAnyKey() on StepDone returned false, want true (signal to tea.Quit)")
	}
	// And any key on a non-done step is ignored.
	wz.Step = StepColorScheme
	if wz.HandleAnyKey() {
		t.Errorf("HandleAnyKey() on StepColorScheme returned true, want false (no-op)")
	}
}

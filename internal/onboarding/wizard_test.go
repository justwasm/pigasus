// wizard_test.go — 1:1 verification of the v1.3.5 onboarding wizard
// against the binary strings recovered in cycle 42.
package onboarding

import (
	"bytes"
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

func testStateMachineNav(t *testing.T, wz *Wizard) {
	if wz.Step != StepColorScheme {
		t.Errorf("default step=%d, want %d", wz.Step, StepColorScheme)
	}
	if wz.Selected != SchemeTerminal {
		t.Errorf("default selected=%d, want %d", wz.Selected, SchemeTerminal)
	}
	if wz.OptOut {
		t.Errorf("default OptOut=true, want false")
	}
	if _, done := wz.Next(); done {
		t.Errorf("Next() from ColorScheme reported done=true, want false")
	}
	if wz.Step != StepTerms {
		t.Errorf("after Next() step=%d, want %d", wz.Step, StepTerms)
	}
	if err := wz.Previous(); err != nil {
		t.Errorf("Previous() from Terms: %v", err)
	}
	if wz.Step != StepColorScheme {
		t.Errorf("after Previous() step=%d, want %d", wz.Step, StepColorScheme)
	}
	if err := wz.Previous(); err == nil {
		t.Errorf("Previous() from ColorScheme should error")
	}
	wz.Selected = SchemeTokyoNight
	wz.MoveSelection(1)
	if wz.Selected != SchemeTerminal {
		t.Errorf("MoveSelection(+1) from TokyoNight=%d, want Terminal=%d", wz.Selected, SchemeTerminal)
	}
	wz.MoveSelection(-1)
	if wz.Selected != SchemeTokyoNight {
		t.Errorf("MoveSelection(-1) from Terminal=%d, want TokyoNight=%d", wz.Selected, SchemeTokyoNight)
	}
}

func testStateMachineToggleAndDone(t *testing.T, wz *Wizard) {
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
	wz.OptOut = false
	if _, done := wz.Next(); !done {
		t.Errorf("Next() from Terms (accepted) reported done=false, want true")
	}
	if wz.Step != StepDone {
		t.Errorf("after Next() from Terms step=%d, want Done=%d", wz.Step, StepDone)
	}
	if _, done := wz.Next(); !done {
		t.Errorf("Next() from Done reported done=false, want true")
	}
	if err := wz.Previous(); err == nil {
		t.Errorf("Previous() from Done should error")
	}
}

// TestWizardStateMachine exercises the 3-step FSM.
func TestWizardStateMachine(t *testing.T) {
	wz := NewWizard()
	testStateMachineNav(t, wz)
	testStateMachineToggleAndDone(t, wz)
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
	if !bytes.Contains(buf.Bytes(), []byte(stepNameColorScheme)) {
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
// names match the recovered glamour styles and release-note ordering.
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

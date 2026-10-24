// wizard_steps_test.go — step rendering and key event tests.
package onboarding

import (
	"bytes"
	"strings"
	"testing"
)

func verifyColorSchemeHeaderAndList(t *testing.T, out string) {
	if !strings.Contains(out, stepNameColorScheme) {
		t.Errorf("color-scheme render missing header %q", stepNameColorScheme)
	}
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
}

func verifyPreviewCodeAndMarkers(t *testing.T, out string) {
	for _, want := range []string{
		`import`, `"fmt"`, `func`, `main`,
		`greet`, `string`, `Printf`, `"Hello, %s!\n"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing token %q in color-scheme render", want)
		}
	}
	if !strings.Contains(out, "I need to add a greeting function") {
		t.Errorf("preview missing thought process line")
	}
	for _, marker := range []string{
		MarkerTool, MarkerTask, MarkerError,
		MarkerWarning, MarkerLink, MarkerAccent, MarkerDim,
	} {
		if !strings.Contains(out, marker) {
			t.Errorf("color-scheme render missing marker %q", marker)
		}
	}
	if !strings.Contains(out, "[ Next ]") {
		t.Errorf("color-scheme render missing [ Next ] button")
	}
	if !strings.Contains(out, "▸") {
		t.Errorf("color-scheme render missing ▸ selected marker")
	}
	if strings.Contains(out, "[ Done ]") {
		t.Errorf("color-scheme render should not contain [ Done ]")
	}
}

// TestWizardColorSchemeRender verifies the Step 1 picker output is
// 1:1 with the v1.3.5 binary strings recovered in cycle 42.
func TestWizardColorSchemeRender(t *testing.T) {
	out := renderWizard(t, StepColorScheme, true, SchemeTerminal)
	verifyColorSchemeHeaderAndList(t, out)
	verifyPreviewCodeAndMarkers(t, out)
}

// TestWizardTermsRender verifies the Step 2 ToS screen output.
func TestWizardTermsRender(t *testing.T) {
	out := renderWizard(t, StepTerms, true, SchemeTerminal)
	if !strings.Contains(out, stepNameTerms) {
		t.Errorf("terms render missing header %q", stepNameTerms)
	}
	for _, want := range TermsWarning {
		if !strings.Contains(out, want) {
			t.Errorf("terms render missing warning line %q", want)
		}
	}
	if !strings.Contains(out, "Yes, I agree to help improve Pigasus CLI") {
		t.Errorf("terms render missing checkbox label")
	}
	if !strings.Contains(out, TermsLinkTerms) {
		t.Errorf("terms render missing %q", TermsLinkTerms)
	}
	if !strings.Contains(out, TermsLinkPrivacy) {
		t.Errorf("terms render missing %q", TermsLinkPrivacy)
	}
	if !strings.Contains(out, termsCheckboxOn) {
		t.Errorf("terms render (agreed=true) missing %q", termsCheckboxOn)
	}
	if strings.Contains(out, termsCheckboxOff) {
		t.Errorf("terms render (agreed=true) should not contain %q", termsCheckboxOff)
	}
	for _, btn := range []string{"[ Previous ]", "[ Done ]"} {
		if !strings.Contains(out, btn) {
			t.Errorf("terms render missing %q", btn)
		}
	}
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

// TestStep1_EnterSelects — cursor at index 0 (SchemeTerminal), user
// presses Enter → the highlight is committed as the choice and
// the FSM advances to StepTerms.
func TestStep1_EnterSelects(t *testing.T) {
	wz := NewWizard()
	if wz.Selected != SchemeTerminal {
		t.Fatalf("setup: Selected=%d, want SchemeTerminal=%d", wz.Selected, SchemeTerminal)
	}
	if wz.Step != StepColorScheme {
		t.Fatalf("setup: Step=%d, want StepColorScheme=%d", wz.Step, StepColorScheme)
	}
	msg, done := wz.HandleEnter()
	if done {
		t.Errorf("HandleEnter on StepColorScheme reported done=true, want false")
	}
	if msg != "" {
		t.Errorf("HandleEnter on StepColorScheme returned msg=%q, want empty", msg)
	}
	if wz.Step != StepTerms {
		t.Errorf("after HandleEnter Step=%d, want StepTerms=%d", wz.Step, StepTerms)
	}
	if wz.Selected != SchemeTerminal {
		t.Errorf("after HandleEnter Selected=%d, want SchemeTerminal=%d (cursor=0)", wz.Selected, SchemeTerminal)
	}
}

func assertRenderedCheckbox(t *testing.T, wz *Wizard, wantGlyph, avoidGlyph string) {
	t.Helper()
	var buf bytes.Buffer
	wz.Out = &buf
	if err := wz.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), wantGlyph) {
		t.Errorf("render missing %q", wantGlyph)
	}
	if strings.Contains(buf.String(), avoidGlyph) {
		t.Errorf("render should not contain %q", avoidGlyph)
	}
}

// TestStep2_SpaceTogglesAgreed — on the ToS screen, Space flips
// the agreement checkbox.
func TestStep2_SpaceTogglesAgreed(t *testing.T) {
	wz := NewWizard()
	wz.Step = StepTerms
	wz.OptOut = true
	wz.setAgreed(false)

	if wz.agreed() {
		t.Fatalf("setup: agreed=true, want false (OptOut was set to true)")
	}
	assertRenderedCheckbox(t, wz, termsCheckboxOff, termsCheckboxOn)

	changed := wz.HandleSpace()
	if !changed {
		t.Errorf("HandleSpace() returned changed=false, want true")
	}
	if !wz.agreed() || wz.OptOut || !wz.Agreed {
		t.Errorf("after HandleSpace agreement fields out of sync")
	}
	assertRenderedCheckbox(t, wz, termsCheckboxOn, termsCheckboxOff)

	changed = wz.HandleSpace()
	if !changed {
		t.Errorf("second HandleSpace() returned changed=false, want true")
	}
	if wz.agreed() {
		t.Errorf("after second HandleSpace agreed=true, want false (re-toggled)")
	}
}

// TestStep2_EnterWithoutAgreedError — on the ToS screen with the
// checkbox UNCHECKED, Enter must NOT advance the FSM.
func TestStep2_EnterWithoutAgreedError(t *testing.T) {
	wz := NewWizard()
	wz.Step = StepTerms
	wz.setAgreed(false)
	if wz.agreed() {
		t.Fatalf("setup: agreed=true, want false")
	}

	msg, done := wz.HandleEnter()
	if done {
		t.Errorf("HandleEnter on StepTerms (unchecked) reported done=true, want false")
	}
	if !strings.Contains(msg, "请先勾选") {
		t.Errorf("HandleEnter on StepTerms (unchecked) msg=%q, want it to contain %q", msg, "请先勾选")
	}
	if wz.Step != StepTerms {
		t.Errorf("after HandleEnter (unchecked) Step=%d, want StepTerms=%d (FSM must not advance)", wz.Step, StepTerms)
	}
	if got := wz.LastMsg(); !strings.Contains(got, "请先勾选") {
		t.Errorf("LastMsg()=%q, want it to contain %q", got, "请先勾选")
	}
}

func verifyStepDoneOutput(t *testing.T, wz *Wizard) {
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
}

// TestStep2_EnterWithAgreedAdvance — on the ToS screen with the
// checkbox CHECKED, Enter advances the FSM to StepDone (step 3).
func TestStep2_EnterWithAgreedAdvance(t *testing.T) {
	wz := NewWizard()
	wz.Step = StepTerms
	wz.setAgreed(true)
	if !wz.agreed() {
		t.Fatalf("setup: agreed=false, want true")
	}

	msg, done := wz.HandleEnter()
	if done {
		t.Errorf("HandleEnter on StepTerms (agreed) reported done=true, want false (intermediate StepDone)")
	}
	if msg != "" {
		t.Errorf("HandleEnter on StepTerms (agreed) returned msg=%q, want empty", msg)
	}
	if wz.Step != StepDone {
		t.Errorf("after HandleEnter (agreed) Step=%d, want StepDone=%d (step 3)", wz.Step, StepDone)
	}

	verifyStepDoneOutput(t, wz)

	if !wz.HandleAnyKey() {
		t.Errorf("HandleAnyKey() on StepDone returned false, want true (signal to tea.Quit)")
	}
	wz.Step = StepColorScheme
	if wz.HandleAnyKey() {
		t.Errorf("HandleAnyKey() on StepColorScheme returned true, want false (no-op)")
	}
}

package onboarding

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOnboardingTerms_TextAndLinksWrap(t *testing.T) {
	width := 40
	wz := NewWizard()
	wz.Step = StepTerms
	wz.Width = width

	var buf bytes.Buffer
	if err := wz.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, l := range lines {
		w := ansi.StringWidth(l)
		if w > width {
			t.Errorf("line %d exceeds width %d (w=%d): %q", i+1, width, w, l)
		}
	}

	// Verify checkbox label wrapped into multiple lines
	output := buf.String()
	if !strings.Contains(output, "Yes, I agree to help") {
		t.Errorf("missing checkbox text in output")
	}
	if !strings.Contains(output, "Terms of Service:") {
		t.Errorf("missing terms link in output")
	}
	if !strings.Contains(output, "[ Done ]") {
		t.Errorf("missing [ Done ] button")
	}
}

func TestOnboardingColorScheme_NarrowTerminalPreviewHidingAndCropping(t *testing.T) {
	// Width = 50 (< 60): Preview should be completely hidden
	wzNarrow := NewWizard()
	wzNarrow.Step = StepColorScheme
	wzNarrow.Width = 50

	var bufNarrow bytes.Buffer
	if err := wzNarrow.Render(&bufNarrow); err != nil {
		t.Fatalf("Render narrow: %v", err)
	}
	outNarrow := bufNarrow.String()
	if strings.Contains(outNarrow, "─── Preview") {
		t.Errorf("preview should be hidden at width 50, but found preview separator")
	}
	if strings.Contains(outNarrow, "import") {
		t.Errorf("preview code should be hidden at width 50")
	}
	if !strings.Contains(outNarrow, "[ Next ]") {
		t.Errorf("expected [ Next ] button at width 50")
	}

	// Width = 70 (60 <= width < 80): Preview should be cropped
	wzMid := NewWizard()
	wzMid.Step = StepColorScheme
	wzMid.Width = 70

	var bufMid bytes.Buffer
	if err := wzMid.Render(&bufMid); err != nil {
		t.Fatalf("Render mid: %v", err)
	}
	outMid := bufMid.String()
	if !strings.Contains(outMid, "─── Preview") {
		t.Errorf("preview should be visible at width 70")
	}
	for i, l := range strings.Split(strings.TrimRight(outMid, "\n"), "\n") {
		w := ansi.StringWidth(l)
		if w > 70 {
			t.Errorf("line %d exceeds width 70 (w=%d): %q", i+1, w, l)
		}
	}
}

func TestOnboardingScrolling_ButtonsRemainVisible(t *testing.T) {
	wz := NewWizard()
	wz.Step = StepTerms
	wz.Width = 80
	wz.Height = 10

	var buf bytes.Buffer
	if err := wz.Render(&buf); err != nil {
		t.Fatalf("Render: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "[ Previous ]") || !strings.Contains(lastLine, "[ Done ]") {
		t.Errorf("bottom action buttons must be pinned, got last line: %q", lastLine)
	}

	// Test scroll navigation
	wz.HandleKey("shift+down")
	if wz.Scroll != 1 {
		t.Errorf("after shift+down: scroll = %d, want 1", wz.Scroll)
	}

	wz.HandleKey("pgdown")
	if wz.Scroll <= 1 {
		t.Errorf("after pgdown: scroll should have increased, got %d", wz.Scroll)
	}

	// Two pgup calls should return scroll back to top (0)
	wz.HandleKey("pgup")
	wz.HandleKey("pgup")
	if wz.Scroll != 0 {
		t.Errorf("after pgup: scroll = %d, want 0", wz.Scroll)
	}
}

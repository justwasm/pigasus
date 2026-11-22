package toolsteps

import (
	"strings"
	"testing"
	"time"
)

// TestThoughtDetailLines_NoToolReturnBullet pins the AGY 1.5.4 indentation fix:
// thoughts expanded following a tool result are not indented with tool return bullets (  ⎿  ).
func TestThoughtDetailLines_NoToolReturnBullet(t *testing.T) {
	s := Step{
		Kind:   KindThought,
		Dur:    3 * time.Second,
		Tokens: 120,
		Result: "Line 1: I will analyze this.\nLine 2: Continuing analysis.",
	}
	lines := thoughtStepLines(s, true, 80)
	for _, l := range lines {
		if strings.Contains(l, "⎿") {
			t.Errorf("thought lines should not contain detail bullet '⎿', got line: %q", l)
		}
	}
	if len(lines) < 3 {
		t.Fatalf("expected head + 2 thought detail lines, got: %v", lines)
	}
}

package sidequestion

import (
	"strings"
	"testing"
)

func TestSpinner(t *testing.T) {
	s := Spinner()
	if s == "" {
		t.Errorf("Spinner returned empty string")
	}
}

func TestHintText(t *testing.T) {
	collapsedOne := HintText(false, 1)
	if !strings.Contains(collapsedOne, "Tab to expand") {
		t.Errorf("expected Tab to expand, got %q", collapsedOne)
	}
	if strings.Contains(collapsedOne, "←/→") {
		t.Errorf("single card should not show navigation, got %q", collapsedOne)
	}

	expandedMulti := HintText(true, 3)
	if !strings.Contains(expandedMulti, "Tab to collapse") {
		t.Errorf("expected Tab to collapse, got %q", expandedMulti)
	}
	if !strings.Contains(expandedMulti, "←/→ to navigate") {
		t.Errorf("multi card should show navigation, got %q", expandedMulti)
	}
}

func TestRenderInputCard(t *testing.T) {
	rendered := RenderInputCard(80, "test content")
	if !strings.Contains(rendered, "test content") {
		t.Errorf("RenderInputCard missing content: %q", rendered)
	}
}

func TestFormatAnswerLines(t *testing.T) {
	short := "line 1\nline 2"
	lines := FormatAnswerLines(short, 5, 0, false)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	long := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6"
	truncated := FormatAnswerLines(long, 3, 0, false)
	if len(truncated) != 4 || truncated[3] != "... (truncated)" {
		t.Errorf("expected truncated tail, got %v", truncated)
	}

	expanded := FormatAnswerLines(long, 3, 1, true)
	if len(expanded) != 4 || !strings.Contains(expanded[3], "↑/↓ to scroll") {
		t.Errorf("expected scroll footer, got %v", expanded)
	}
}

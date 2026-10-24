package sidequestion

import (
	"errors"
	"strings"
	"testing"
)

func TestCard_RenderInputCard(t *testing.T) {
	rendered := RenderInputCard(80, "test content")
	if !strings.Contains(rendered, "┃") {
		t.Errorf("RenderInputCard missing thick border: %q", rendered)
	}
	if !strings.Contains(rendered, "test content") {
		t.Errorf("RenderInputCard missing content: %q", rendered)
	}

	small := RenderInputCard(5, "narrow")
	if !strings.Contains(small, "narrow") {
		t.Errorf("RenderInputCard narrow width missing content: %q", small)
	}
}

func TestMaxBtwAnswerLines(t *testing.T) {
	// Zero/negative height should default to 24
	if got := MaxBtwAnswerLines(0, false); got != 7 {
		t.Errorf("MaxBtwAnswerLines(0, false) = %d, want 7", got)
	}
	if got := MaxBtwAnswerLines(-5, true); got != 16 {
		t.Errorf("MaxBtwAnswerLines(-5, true) = %d, want 16", got)
	}

	// Height 10: collapsed 3 -> min 5, expanded 2 -> min 5
	if got := MaxBtwAnswerLines(10, false); got != 5 {
		t.Errorf("MaxBtwAnswerLines(10, false) = %d, want 5", got)
	}
	if got := MaxBtwAnswerLines(10, true); got != 5 {
		t.Errorf("MaxBtwAnswerLines(10, true) = %d, want 5", got)
	}

	// Height 100: collapsed 30, expanded 92
	if got := MaxBtwAnswerLines(100, false); got != 30 {
		t.Errorf("MaxBtwAnswerLines(100, false) = %d, want 30", got)
	}
	if got := MaxBtwAnswerLines(100, true); got != 92 {
		t.Errorf("MaxBtwAnswerLines(100, true) = %d, want 92", got)
	}

	// MaxAnswerLines alias
	if got := MaxAnswerLines(100, true); got != 92 {
		t.Errorf("MaxAnswerLines(100, true) = %d, want 92", got)
	}
}

func TestFormatBtwAnswerLines(t *testing.T) {
	short := "one\ntwo"
	lines := FormatBtwAnswerLines(short, 80, false, 24, 0)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}

	long := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10"
	truncated := FormatBtwAnswerLines(long, 80, false, 24, 0)
	if !strings.Contains(truncated[len(truncated)-1], "... (truncated)") {
		t.Errorf("expected truncation footer, got %v", truncated)
	}

	scrolled := FormatBtwAnswerLines(long, 80, true, 10, 1)
	if !strings.Contains(scrolled[len(scrolled)-1], "↑/↓ to scroll") {
		t.Errorf("expected scroll footer, got %v", scrolled)
	}

	// Custom renderer
	customMd := func(text string, width int) string {
		return "CUSTOM: " + text
	}
	customLines := FormatBtwAnswerLines("hello", 80, false, 24, 0, customMd)
	if len(customLines) == 0 || !strings.Contains(customLines[0], "CUSTOM: hello") {
		t.Errorf("expected custom rendered output, got %v", customLines)
	}
}

func TestRenderBtwBodyLines_Thinking(t *testing.T) {
	pending := RenderBtwBodyLines(StatePending, "", nil, 80, false, 24, 0)
	if len(pending) != 1 || !strings.Contains(pending[0], "Thinking...") {
		t.Errorf("expected Thinking... in pending, got %v", pending)
	}

	streamingEmpty := RenderBtwBodyLines(StateStreaming, "", nil, 80, false, 24, 0)
	if len(streamingEmpty) != 1 || !strings.Contains(streamingEmpty[0], "Thinking...") {
		t.Errorf("expected Thinking... in streaming empty, got %v", streamingEmpty)
	}
}

func TestRenderBtwBodyLines_Errors(t *testing.T) {
	withErr := RenderBtwBodyLines(StateFailed, "", errors.New("boom"), 80, false, 24, 0)
	if len(withErr) != 2 || !strings.Contains(withErr[0], "Error: boom") {
		t.Errorf("expected Error: boom, got %v", withErr)
	}

	failedNoErr := RenderBtwBodyLines(StateFailed, "", nil, 80, false, 24, 0)
	if len(failedNoErr) != 2 || !strings.Contains(failedNoErr[0], "Error: unknown error") {
		t.Errorf("expected Error: unknown error, got %v", failedNoErr)
	}

	doneEmpty := RenderBtwBodyLines(StateDone, "", nil, 80, false, 24, 0)
	if len(doneEmpty) != 1 || !strings.Contains(doneEmpty[0], "ended unexpectedly") {
		t.Errorf("expected ended unexpectedly error, got %v", doneEmpty)
	}
}

func TestRenderBtwBodyLines_Answer(t *testing.T) {
	doneAnswer := RenderBtwBodyLines(StateDone, "computed answer", nil, 80, false, 24, 0)
	if len(doneAnswer) == 0 || !strings.Contains(doneAnswer[0], "computed answer") {
		t.Errorf("expected answer lines, got %v", doneAnswer)
	}

	streamAnswer := RenderBtwBodyLines(StateStreaming, "partial answer", nil, 80, false, 24, 0)
	if len(streamAnswer) == 0 || !strings.Contains(streamAnswer[0], "partial answer") {
		t.Errorf("expected streaming answer lines, got %v", streamAnswer)
	}
}

func TestRenderBtwCard_NilAndStates(t *testing.T) {
	if got := RenderBtwCard(nil, CardRenderOptions{}); got != "" {
		t.Errorf("RenderBtwCard(nil) should be empty string, got %q", got)
	}

	cPending := &Card{Question: "Why is the sky blue?\nExtra line", State: StatePending}
	renderedPending := RenderBtwCard(cPending, CardRenderOptions{Width: 80, Height: 24})
	if !strings.Contains(renderedPending, "Why is the sky blue?") {
		t.Errorf("missing question in pending card:\n%s", renderedPending)
	}
	if strings.Contains(renderedPending, "Extra line") {
		t.Errorf("firstLine should have stripped extra line:\n%s", renderedPending)
	}
	if !strings.Contains(renderedPending, "Generating... (Enter/Esc to cancel)") {
		t.Errorf("missing generating prompt in pending card:\n%s", renderedPending)
	}
}

func TestRenderBtwCard_DoneAndExpanded(t *testing.T) {
	cDone := &Card{
		Question: "How to exit vim?",
		Answer:   "Type :wq and hit enter.",
		State:    StateDone,
	}
	rendered := RenderBtwCard(cDone, CardRenderOptions{Width: 80, Height: 24, CardCount: 1})
	if !strings.Contains(rendered, "Tab to expand") {
		t.Errorf("missing 'Tab to expand' in collapsed mode:\n%s", rendered)
	}
	if strings.Contains(rendered, "←/→ to navigate") {
		t.Errorf("single card should not show navigation:\n%s", rendered)
	}

	cDone.SetExpanded(true)
	multiRendered := RenderBtwCard(cDone, CardRenderOptions{Width: 80, Height: 24, CardCount: 3})
	if !strings.Contains(multiRendered, "Tab to collapse") {
		t.Errorf("missing 'Tab to collapse' in expanded mode:\n%s", multiRendered)
	}
	if !strings.Contains(multiRendered, "←/→ to navigate") {
		t.Errorf("multi-card should show navigation:\n%s", multiRendered)
	}

	methodRendered := cDone.Render(CardRenderOptions{Width: 80, Height: 24, CardCount: 3})
	if methodRendered != multiRendered {
		t.Errorf("c.Render() != RenderBtwCard(c)")
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("single line"); got != "single line" {
		t.Errorf("firstLine(single) = %q", got)
	}
	if got := firstLine("line 1\nline 2\nline 3"); got != "line 1" {
		t.Errorf("firstLine(multi) = %q", got)
	}
	if got := firstLine(""); got != "" {
		t.Errorf("firstLine(empty) = %q", got)
	}
}

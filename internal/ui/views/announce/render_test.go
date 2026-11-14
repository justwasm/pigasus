package announce

import (
	"strings"
	"testing"
)

func TestRenderCard_Nil(t *testing.T) {
	if got := RenderCard(nil); got != "" {
		t.Fatalf("expected empty string for nil card, got %q", got)
	}
}

func TestRenderCard_Format(t *testing.T) {
	card := &Card{
		ID:    "test-card",
		Title: "Test Announcement",
		Body:  "This is a test announcement body.",
		Hint:  "Custom hint",
	}
	rendered := RenderCard(card)
	if !strings.Contains(rendered, "Test Announcement") {
		t.Errorf("rendered card missing title: %q", rendered)
	}
	if !strings.Contains(rendered, "This is a test announcement body.") {
		t.Errorf("rendered card missing body: %q", rendered)
	}
	if !strings.Contains(rendered, "Custom hint") {
		t.Errorf("rendered card missing custom hint: %q", rendered)
	}
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines in rendered card, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "┌─") || !strings.Contains(lines[0], "┐") {
		t.Errorf("expected top border with ┌─ and ┐: %q", lines[0])
	}
	if !strings.Contains(lines[4], "└") || !strings.Contains(lines[4], "┘") {
		t.Errorf("expected bottom border with └ and ┘: %q", lines[4])
	}
}

func TestRenderBoxedCard(t *testing.T) {
	if got := RenderBoxedCard(nil, 60); got != "" {
		t.Fatalf("expected empty string for nil card, got %q", got)
	}

	card := &Card{
		ID:    "box-test",
		Title: "Boxed Announcement",
		Body:  "Fully enclosed box drawing card.",
		Hint:  "Press esc to dismiss",
	}

	rendered := RenderBoxedCard(card, 60)
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	// Check top border begins with ┌─ and ends with ─┐
	top := lines[0]
	if !strings.Contains(top, "┌─") || !strings.Contains(top, "─┐") {
		t.Errorf("top border must contain ┌─ and ─┐, got %q", top)
	}

	// Check bottom border begins with └─ and ends with ─┘
	bottom := lines[len(lines)-1]
	if !strings.Contains(bottom, "└─") || !strings.Contains(bottom, "─┘") {
		t.Errorf("bottom border must contain └─ and ─┘, got %q", bottom)
	}

	// Check content
	if !strings.Contains(rendered, "Boxed Announcement") {
		t.Errorf("missing title in boxed card")
	}
	if !strings.Contains(rendered, "Fully enclosed box drawing card.") {
		t.Errorf("missing body in boxed card")
	}
}

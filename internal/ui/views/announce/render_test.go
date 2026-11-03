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
}

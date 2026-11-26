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
	if !strings.Contains(rendered, "┃") {
		t.Errorf("expected thick left border ┃ in rendered card, got:\n%s", rendered)
	}
}

func TestRenderBoxedCard(t *testing.T) {
	if got := RenderBoxedCard(nil, 60); got != "" {
		t.Fatalf("expected empty string for nil card, got %q", got)
	}

	card := &Card{
		ID:    "box-test",
		Title: "Boxed Announcement",
		Body:  "Thick left border card.",
		Hint:  "Press esc to dismiss",
	}

	rendered := RenderBoxedCard(card, 60)
	if !strings.Contains(rendered, "┃") {
		t.Errorf("rendered card must contain ┃, got:\n%s", rendered)
	}

	// Check content
	if !strings.Contains(rendered, "Boxed Announcement") {
		t.Errorf("missing title in card")
	}
	if !strings.Contains(rendered, "Thick left border card.") {
		t.Errorf("missing body in card")
	}
	if !strings.Contains(rendered, "Press esc to dismiss") {
		t.Errorf("missing hint in card")
	}
}

package creditspanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCreditsPanel_OpenURLActions(t *testing.T) {
	openedURL := ""
	p := New(Seams{
		OpenURL: func(url string) error {
			openedURL = url
			return nil
		},
	})

	// Cursor is at 0: "Get More AI Credits"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if openedURL != "https://one.google.com/ai-premium" {
		t.Errorf("action 0 opened %q, want https://one.google.com/ai-premium", openedURL)
	}

	// Move cursor to 1: "See Activity"
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if openedURL != "https://myaccount.google.com/subscriptions" {
		t.Errorf("action 1 opened %q, want https://myaccount.google.com/subscriptions", openedURL)
	}
}

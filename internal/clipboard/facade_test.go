package clipboard_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/clipboard"
)

func TestClipboardFacade(t *testing.T) {
	opts := clipboard.DefaultOptions()
	if !opts.WebPEnabled {
		t.Fatal("expected WebPEnabled true by default")
	}
	s := clipboard.NormalizeText("hello\r\nworld")
	if s != "hello\nworld" {
		t.Fatalf("unexpected normalized text: %q", s)
	}
}

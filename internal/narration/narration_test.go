package narration_test

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/narration"
)

func TestNarrationSelectText(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := narration.SelectText("", start)
	if len(got) == 0 {
		t.Errorf("expected non-empty default text")
	}

	custom := "working on task xyz"
	gotCustom := narration.SelectText(custom, start)
	if !strings.Contains(gotCustom, custom) {
		t.Errorf("got %q, want custom text %q", gotCustom, custom)
	}
}

func TestNarrationFeedback(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Off when not muted
	text, echo, mutedNow, already := narration.Feedback("off", start, false)
	if text != "" || echo != narration.DisabledMsg || !mutedNow || already {
		t.Errorf("unexpected off feedback: text=%q, echo=%q, mutedNow=%v, already=%v", text, echo, mutedNow, already)
	}

	// Off when already muted
	text, echo, mutedNow, already = narration.Feedback("off", start, true)
	if text != "" || echo != narration.AlreadyDisabledMsg || mutedNow || !already {
		t.Errorf("unexpected already-off feedback: text=%q, echo=%q, mutedNow=%v, already=%v", text, echo, mutedNow, already)
	}

	// Normal call when already muted
	text, echo, mutedNow, already = narration.Feedback("auto", start, true)
	if text != "" || echo != "" || mutedNow || !already {
		t.Errorf("unexpected muted auto feedback: text=%q, echo=%q, mutedNow=%v, already=%v", text, echo, mutedNow, already)
	}
}

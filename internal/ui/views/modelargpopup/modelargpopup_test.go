// modelargpopup_test.go — the "/model <argument>" completion's data.
//
// The reference's own answers are pinned here byte for byte: the
// catalogue rows the completion popup offers, the argument's resolution
// against the catalogue, and the error an argument that names no model
// produces. 【一手】 agy 1.4.6, live.
package modelargpopup

import (
	"testing"

	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

// isolate points the provider configuration at an empty scratch file and
// closes the Claude 5.5 gate, so the catalogue is the reference's own
// fourteen models and nothing else.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("AGY_ADC_AUTH", "")
	configtest.Isolate(t)
}

// The rows the popup offers are the catalogue's rows: the reference's
// own models first, then the ones the user configured.
//
// The six rows the reference printed at 1.4.6 are still there, in the
// same order; the three families the reference's own binary carries
// between Gemini 3.8 Flash and the Claude rows come with them
// (internal/features/models/catalog/reference_models_test.go pins the
// catalogue itself).
func TestRows_ListsTheCatalogue(t *testing.T) {
	isolate(t)
	rows := Rows()
	want := []struct{ label, id string }{
		{"Gemini 3.8 Flash (High)", "gemini-3.8-flash-high"},
		{"Gemini 3.8 Flash (Medium)", "gemini-3.8-flash-medium"},
		{"Gemini 3.8 Flash (Low)", "gemini-3.8-flash-low"},
		{"Gemini 3.7 Flash (High)", "gemini-3.7-flash-high"},
		{"Gemini 3.7 Flash (Medium)", "gemini-3.7-flash-medium"},
		{"Gemini 3.7 Flash (Low)", "gemini-3.7-flash-low"},
		{"Gemini 3.6 Flash (High)", "gemini-3.6-flash-high"},
		{"Gemini 3.6 Flash (Medium)", "gemini-3.6-flash-medium"},
		{"Gemini 3.6 Flash (Low)", "gemini-3.6-flash-low"},
		{"Gemini 3.1 Pro (High)", "gemini-3.1-pro-high"},
		{"Gemini 3.1 Pro (Low)", "gemini-3.1-pro-low"},
		{"Claude Sonnet 4.6 (Thinking)", "claude-sonnet-4-6"},
		{"Claude Opus 4.6 (Thinking)", "claude-opus-4-6-thinking"},
		{"GPT-OSS 120B (Medium)", "gpt-oss-120b-medium"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d (%v)", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].Label != w.label || rows[i].ID != w.id {
			t.Errorf("row %d = %q/%q, want %q/%q", i, rows[i].Label, rows[i].ID, w.label, w.id)
		}
	}
}

// The argument resolves through the display label or through the
// catalogue id, case-insensitively — and only in full.
func TestResolve_MatchesTheLabelOrTheID(t *testing.T) {
	isolate(t)
	cases := []struct {
		arg  string
		want string
	}{
		{"Claude Sonnet 4.6 (Thinking)", "Claude Sonnet 4.6 (Thinking)"},
		{"claude sonnet 4.6 (thinking)", "Claude Sonnet 4.6 (Thinking)"},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
		{"CLAUDE-SONNET-4-6", "Claude Sonnet 4.6 (Thinking)"},
		{"  gpt-oss-120b-medium  ", "GPT-OSS 120B (Medium)"},
		{"GPT-OSS 120B (Medium)", "GPT-OSS 120B (Medium)"},
	}
	for _, c := range cases {
		entry, ok := Resolve(c.arg)
		if !ok {
			t.Errorf("Resolve(%q) found nothing, want %q", c.arg, c.want)
			continue
		}
		if entry.Label != c.want {
			t.Errorf("Resolve(%q) = %q, want %q", c.arg, entry.Label, c.want)
		}
	}
}

// An argument that names no model resolves to nothing: a prefix does not
// name a model (the popup completes the row instead), and neither does an
// empty argument.
func TestResolve_NamesNoModel(t *testing.T) {
	isolate(t)
	for _, arg := range []string{"", "   ", "gpt-oss", "claude", "zzz"} {
		if entry, ok := Resolve(arg); ok {
			t.Errorf("Resolve(%q) = %q, want no model", arg, entry.Label)
		}
	}
}

// The reference's error for an argument that names no model, byte for
// byte. 【一手】 agy 1.4.6, live — the reference printed the same
// sentence with the six ids its own catalogue carried then; the list is
// the catalogue's, so the clone's carries the three families the
// reference's binary knows as well.
func TestInvalidMessage_IsTheReferenceText(t *testing.T) {
	isolate(t)
	want := `Invalid model "zzz" (available: gemini-3.8-flash-high, ` +
		`gemini-3.8-flash-medium, gemini-3.8-flash-low, gemini-3.7-flash-high, ` +
		`gemini-3.7-flash-medium, gemini-3.7-flash-low, gemini-3.6-flash-high, ` +
		`gemini-3.6-flash-medium, gemini-3.6-flash-low, gemini-3.1-pro-high, ` +
		`gemini-3.1-pro-low, claude-sonnet-4-6, ` +
		`claude-opus-4-6-thinking, gpt-oss-120b-medium)`
	if got := InvalidMessage("zzz"); got != want {
		t.Errorf("InvalidMessage = %q, want %q", got, want)
	}
}

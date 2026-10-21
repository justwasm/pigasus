// narration_catalogue_test.go — cycle 250: catalogue determinism + precedence.
//
// Three tests pin the cycle-250 catalogue contract:
//
//  1. userText wins verbatim — even when mode is "off" or "auto",
//     a non-empty userText is returned untouched.
//  2. mode == "off" returns "" — no catalogue pick, no error.
//  3. Same (mode, taskType, sessionStart) → same paragraph. Two
//     calls in a row, with no Math/random in the helper, must yield
//     identical strings. The test also asserts the catalogue is
//     bounded (5-8 entries per the spec) and that every entry is
//     non-empty.
package cli

import (
	"strings"
	"testing"
	"time"
)

// TestSelectNarration_UserTextWins pins the verbatim contract:
// any non-empty userText is returned as-is, regardless of mode or
// taskType. The catalogue is bypassed entirely.
func TestSelectNarration_UserTextWins(t *testing.T) {
	seed := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		text string
		mode string
		task string
	}{
		{"default_mode", "I'll fix the bug", "", ""},
		{"auto_mode", "Plan: write a CLI", "auto", "refactor"},
		{"off_mode_user_text_wins", "explicit override", "off", "ignored"},
		{"whitespace_trimmed", "  trimmed  ", "auto", "bugfix"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectNarration(tc.text, tc.mode, tc.task, seed)
			// Whitespace is trimmed; that's the only normalisation.
			want := strings.TrimSpace(tc.text)
			if got != want {
				t.Errorf("SelectNarration(userText=%q, mode=%q, taskType=%q) = %q, want %q",
					tc.text, tc.mode, tc.task, got, want)
			}
		})
	}
}

// TestSelectNarration_ModeOffReturnsEmpty pins the "off" branch:
// mode == "off" with an empty userText returns "". The caller
// (/narration slash handler) interprets "" as "do not append
// anything to m.response" and additionally flips m.narrationOff
// so subsequent /narration invocations in the same session
// continue to no-op.
func TestSelectNarration_ModeOffReturnsEmpty(t *testing.T) {
	seed := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if got := SelectNarration("", "off", "", seed); got != "" {
		t.Errorf("SelectNarration off: got %q, want \"\"", got)
	}
	if got := SelectNarration("", "off", "ignored", seed); got != "" {
		t.Errorf("SelectNarration off + taskType: got %q, want \"\"", got)
	}
	// Default mode with same seed returns a catalogue paragraph,
	// not "" — sanity-check that "off" is the only empty branch.
	if got := SelectNarration("", "", "", seed); got == "" {
		t.Errorf("SelectNarration default returned \"\"; expected a catalogue pick")
	}
}

// TestSelectNarration_CatalogueDeterministic pins the headline
// cycle-250 guarantee: same (mode, taskType, sessionStart) always
// returns the same paragraph. Two consecutive calls do not depend
// on Math/random. The test also asserts the catalogue stays
// within the spec's 5-8 entry band and that every entry is a
// non-empty, non-whitespace paragraph.
func TestSelectNarration_CatalogueDeterministic(t *testing.T) {
	seed := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	calls := []struct {
		mode string
		task string
	}{
		{"", ""},
		{"default", ""},
		{"auto", ""},
		{"auto", "refactor"},
		{"auto", "bugfix"},
	}
	for _, c := range calls {
		first := SelectNarration("", c.mode, c.task, seed)
		second := SelectNarration("", c.mode, c.task, seed)
		if first != second {
			t.Errorf("non-deterministic: mode=%q task=%q first=%q second=%q",
				c.mode, c.task, first, second)
		}
		if first == "" {
			t.Errorf("empty paragraph returned for mode=%q task=%q", c.mode, c.task)
		}
	}
	// Two DIFFERENT modes with the same seed should — when taskType
	// is empty — return the *same* paragraph (auto with empty
	// taskType is just a different prefix for a length-0 payload,
	// which still mixes the timestamp; but the spec only guarantees
	// determinism, not "different modes → different paragraphs").
	// Documenting that explicitly:
	a := SelectNarration("", "default", "", seed)
	b := SelectNarration("", "auto", "", seed)
	if a == "" || b == "" {
		t.Fatalf("expected non-empty catalogue picks; default=%q auto=%q", a, b)
	}
}

// TestSelectNarration_CatalogueBounded pins the catalogue shape:
// 5-8 entries per the spec, every entry non-empty. Adding or
// removing entries invalidates this test, which is intentional
// — the catalogue is the cycle-250 evidence contract.
func TestSelectNarration_CatalogueBounded(t *testing.T) {
	n := len(narrationCatalogue)
	if n < 5 || n > 8 {
		t.Fatalf("catalogue has %d entries; spec mandates 5-8", n)
	}
	for i, p := range narrationCatalogue {
		if strings.TrimSpace(p) == "" {
			t.Errorf("catalogue[%d] is whitespace/empty", i)
		}
	}
}

package palette_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

func TestRule(t *testing.T) {
	if got := palette.Rule(0); got != "" {
		t.Errorf("Rule(0) = %q, want empty", got)
	}
	r := palette.Rule(5)
	if !strings.HasPrefix(r, palette.SeparatorOpen) {
		t.Errorf("Rule(5) missing separator open: %q", r)
	}
	if !strings.HasSuffix(r, palette.SeparatorClose) {
		t.Errorf("Rule(5) missing separator close: %q", r)
	}
	if count := strings.Count(r, "─"); count != 5 {
		t.Errorf("Rule(5) contains %d dashes, want 5", count)
	}
}

// TestStatusModeColor pins the status-row prefixes.
//
// Every real mode draws: a stop in the shift+tab cycle that renders nothing
// is indistinguishable from a dropped row, so auto takes the dim foreground
// and the other two keep their own colours. The stored-empty value draws
// nothing because it is the absence of a choice, not a mode.
func TestStatusModeColor(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"auto", palette.DimGrayBoldOpen},
		{"plan", palette.BlueOpen},
		{"accept-edits", palette.AgentFgOpen},
		{"default", ""},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			if got := palette.StatusModeColor(c.mode); got != c.want {
				t.Errorf("StatusModeColor(%q) = %q, want %q", c.mode, got, c.want)
			}
		})
	}
}

// TestStatusModePrefix pins which modes carry the "<mode> · " prefix.
func TestStatusModePrefix(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"auto", "auto · "},
		{"plan", "plan · "},
		{"accept-edits", "accept-edits · "},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			if got := palette.StatusModePrefix(c.mode); got != c.want {
				t.Errorf("StatusModePrefix(%q) = %q, want %q", c.mode, got, c.want)
			}
		})
	}
}

// TestStatusModePrefixFollowsTheScheme is the invariant the two slots owe:
// moving the scheme must move them, or they are a hard-coded colour that
// ignores /config — which is what the reference does not do.
func TestStatusModePrefixFollowsTheScheme(t *testing.T) {
	// The scheme is package state; a test that moves it puts it back.
	prev, _ := palette.CurrentScheme()
	defer palette.SetScheme(prev)

	seen := map[string]string{}
	for _, name := range []string{"light", "dark"} {
		palette.SetScheme(name)
		seen[name] = palette.StatusModeColor("plan")
	}
	if seen["light"] == seen["dark"] {
		t.Errorf("plan's prefix colour is %q under both schemes: it is not a slot", seen["light"])
	}
}

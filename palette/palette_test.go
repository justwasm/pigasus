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

func TestStatusModeColor(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"auto", "\x1b[38;2;203;75;22m"},
		{"plan", palette.BlueOpen},
		{"accept-edits", "\x1b[38;2;133;153;0m"},
		{"default", palette.DimGrayOpen},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			if got := palette.StatusModeColor(c.mode); got != c.want {
				t.Errorf("StatusModeColor(%q) = %q, want %q", c.mode, got, c.want)
			}
		})
	}
}

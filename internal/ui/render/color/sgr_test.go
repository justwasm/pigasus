package color_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/render/color"
)

func TestStripSGR(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"sgr-color", "\x1b[31mred\x1b[0m", "red"},
		{"sgr-bold-rgb", "\x1b[1;38;2;25;103;210mblue\x1b[0m text", "blue text"},
		{"empty", "", ""},
		{"stray-escape", "\x1b", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := color.StripSGR(c.in)
			if got != c.want {
				t.Errorf("StripSGR(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

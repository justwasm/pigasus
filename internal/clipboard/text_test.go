package clipboard_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/clipboard"
)

func TestNormalizeText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello\nworld", "hello\nworld"},
		{"crlf", "hello\r\nworld\r\n", "hello\nworld\n"},
		{"stray-cr", "hello\rworld", "helloworld"},
		{"empty", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := clipboard.NormalizeText(c.in)
			if got != c.want {
				t.Errorf("NormalizeText(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

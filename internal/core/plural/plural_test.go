// plural_test.go — the singular/plural rule, pinned.
//
// The rule is the reference's own: one item takes the singular word,
// every other count takes the plural one, and zero is plural ("0
// results", not "0 result").
package plural

import "testing"

// TestCountLabel pins the rule for every count the surfaces render.
func TestCountLabel(t *testing.T) {
	cases := []struct {
		n    int
		word [2]string
		want string
	}{
		{0, [2]string{"result", "results"}, "0 results"},
		{1, [2]string{"result", "results"}, "1 result"},
		{2, [2]string{"result", "results"}, "2 results"},
		{1, [2]string{"line", "lines"}, "1 line"},
		{7, [2]string{"line", "lines"}, "7 lines"},
		{1, [2]string{"file", "files"}, "1 file"},
	}
	for _, c := range cases {
		if got := CountLabel(c.n, c.word); got != c.want {
			t.Errorf("CountLabel(%d, %v) = %q, want %q", c.n, c.word, got, c.want)
		}
	}
}

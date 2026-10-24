package tuilib_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/tuilib"
)

func TestCountLabel(t *testing.T) {
	pairs := map[string][2]string{
		"result": {"result", "results"},
		"match":  {"match", "matches"},
		"file":   {"file", "files"},
		"line":   {"line", "lines"},
	}
	cases := []struct {
		n    int
		name string
		want string
	}{
		{0, "result", "0 results"},
		{1, "result", "1 result"},
		{2, "result", "2 results"},
		{7, "result", "7 results"},
		{0, "match", "0 matches"},
		{1, "match", "1 match"},
		{5, "match", "5 matches"},
		{0, "file", "0 files"},
		{1, "file", "1 file"},
		{3, "file", "3 files"},
		{0, "line", "0 lines"},
		{1, "line", "1 line"},
		{2, "line", "2 lines"},
		{42, "line", "42 lines"},
	}
	for _, c := range cases {
		t.Run(c.name+"-"+strconv.Itoa(c.n), func(t *testing.T) {
			got := tuilib.CountLabel(c.n, pairs[c.name])
			if got != c.want {
				t.Errorf("CountLabel(%d, %s) = %q, want %q", c.n, c.name, got, c.want)
			}
		})
	}
}

func TestFileLineCount(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"single line no terminator", "hello", 1},
		{"single line with terminator", "hello\n", 1},
		{"two lines unterminated", "a\nb", 2},
		{"two lines terminated", "a\nb\n", 2},
		{"three lines terminated", "a\nb\nc\n", 3},
		{"five lines unterminated", "1\n2\n3\n4\n5", 5},
		{"blank line only", "\n", 1},
		{"blank lines interspersed", "a\n\nb\n", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tuilib.FileLineCount(c.content)
			if got != c.want {
				t.Errorf("FileLineCount(%q) = %d, want %d", c.content, got, c.want)
			}
		})
	}
}

func TestFileLineCount_NoPhantomTrailingLine(t *testing.T) {
	for _, s := range []string{"a\n", "a\nb\n", "a\nb\nc\n", "single\n"} {
		got := tuilib.FileLineCount(s)
		buggy := len(strings.Split(s, "\n"))
		if got == buggy {
			t.Errorf("FileLineCount(%q) = %d matched buggy formula N+1", s, got)
		}
	}
}

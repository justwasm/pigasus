// codesearch_test.go — unit tests for codesearch argument parsing and truncation.
package codesearch

import (
	"errors"
	"strings"
	"testing"
)

type parseArgsTestCase struct {
	name      string
	args      string
	wantQuery string
	wantLit   bool
	wantInc   []string
	wantExc   []string
	wantErr   error
}

var parseArgsTestCases = []parseArgsTestCase{
	{
		name:      "regex-default",
		args:      "TODO",
		wantQuery: "TODO",
	},
	{
		name:      "literal-short",
		args:      "-F needle",
		wantQuery: "needle",
		wantLit:   true,
	},
	{
		name:      "literal-long",
		args:      "--literal n.edle",
		wantQuery: "n.edle",
		wantLit:   true,
	},
	{
		name:      "include-glob",
		args:      "TODO f:*.go",
		wantQuery: "TODO",
		wantInc:   []string{"*.go"},
	},
	{
		name:      "exclude-glob",
		args:      "TODO file:*_test.go",
		wantQuery: "TODO",
		wantExc:   []string{"*_test.go"},
	},
	{
		name:      "include-and-exclude",
		args:      "TODO f:*.go file:*_test.go file:*.pb.go",
		wantQuery: "TODO",
		wantInc:   []string{"*.go"},
		wantExc:   []string{"*_test.go", "*.pb.go"},
	},
	{
		name:    "empty",
		args:    "",
		wantErr: ErrEmptyQuery,
	},
	{
		name:    "flags-only",
		args:    "-F",
		wantErr: ErrEmptyQuery,
	},
	{
		name:      "unknown-flag-becomes-pattern",
		args:      "--zzz TODO",
		wantQuery: "--zzz TODO",
	},
	{
		name:      "double-dash-makes-flag-is-pattern",
		args:      "-- -F",
		wantQuery: "-F",
	},
}

func checkParseArgsCase(t *testing.T, tc parseArgsTestCase) {
	a, err := ParseArgs(tc.args)
	if tc.wantErr != nil {
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("ParseArgs(%q) err = %v, want %v", tc.args, err, tc.wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("ParseArgs(%q) err = %v", tc.args, err)
	}
	if a.Query != tc.wantQuery {
		t.Errorf("Query = %q, want %q", a.Query, tc.wantQuery)
	}
	if a.Literal != tc.wantLit {
		t.Errorf("Literal = %v, want %v", a.Literal, tc.wantLit)
	}
	if !stringSliceEqual(a.Include, tc.wantInc) {
		t.Errorf("Include = %v, want %v", a.Include, tc.wantInc)
	}
	if !stringSliceEqual(a.Exclude, tc.wantExc) {
		t.Errorf("Exclude = %v, want %v", a.Exclude, tc.wantExc)
	}
}

// TestParseArgs locks the cycle-228 argument contract: -F and --literal
// are recognised flags, -- terminates the flag list, f:<glob> and
// file:<glob> are repeatable include / exclude flags, and an empty
// pattern tail yields ErrEmptyQuery.
func TestParseArgs(t *testing.T) {
	for _, tc := range parseArgsTestCases {
		t.Run(tc.name, func(t *testing.T) {
			checkParseArgsCase(t, tc)
		})
	}
}

// TestParseArgsMalformed is the dedicated short-circuit check.
func TestParseArgsMalformed(t *testing.T) {
	if _, err := ParseArgs(""); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("empty args: err = %v, want ErrEmptyQuery", err)
	}
	if _, err := ParseArgs("-F"); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("flags-only args: err = %v, want ErrEmptyQuery", err)
	}
	if _, err := ParseArgs("f:*.go"); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("glob-only args: err = %v, want ErrEmptyQuery", err)
	}
}

// TestCJKTruncation exercises the AGY 1.4.0 fix: long lines and CJK
// paths are no longer rendered as replacement glyphs. The Truncate
// helper must end the result with an ellipsis and never emit a U+FFFD.
func TestCJKTruncation(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
	}{
		{name: "ascii-fits", text: "hello world", width: 20},
		{name: "ascii-clips", text: strings.Repeat("a", 80), width: 10},
		{name: "cjk-clips", text: strings.Repeat("\u4e2d", 50), width: 6},
		{name: "mixed", text: "TODO \u4e2d\u6587 " + strings.Repeat("z", 100), width: 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.text, tc.width)
			if strings.Contains(got, "\ufffd") {
				t.Errorf("Truncate emitted replacement glyph: %q", got)
			}
			if !strings.HasSuffix(got, "\u2026") && len(tc.text) > tc.width && tc.width > 0 {
				t.Errorf("Truncate should end with ellipsis when over budget: %q", got)
			}
		})
	}

	// Width = 1 always returns the ellipsis alone — never the first
	// rune, never empty.
	if got := Truncate("\u4e2d\u6587", 1); got != "\u2026" {
		t.Errorf("Truncate(width=1) = %q, want \u2026", got)
	}
}

// stringSliceEqual is a tiny helper for test assertions.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

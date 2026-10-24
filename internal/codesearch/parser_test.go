// parser_test.go — unit tests for ParseArgsFlat and CompileMatcher.
package codesearch

import (
	"errors"
	"slices"
	"testing"
)

func TestParseArgsFlat_Basic(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      string
		wantQuery string
		wantInc   []string
		wantExc   []string
	}{
		{name: "simple", args: "hello world", wantQuery: "hello world"},
		{name: "include", args: "f:*.go findme", wantQuery: "findme", wantInc: []string{"*.go"}},
		{name: "exclude", args: "file:*.tmp findme", wantQuery: "findme", wantExc: []string{"*.tmp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, literal, inc, exc, err := ParseArgsFlat(tc.args)
			if err != nil || literal || query != tc.wantQuery || !slices.Equal(inc, tc.wantInc) || !slices.Equal(exc, tc.wantExc) {
				t.Fatalf("ParseArgsFlat(%q) = (%q, %v, %v, %v, %v)", tc.args, query, literal, inc, exc, err)
			}
		})
	}
}

func TestParseArgsFlat_Flags(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      string
		wantQuery string
		wantInc   []string
		wantExc   []string
	}{
		{name: "short flag", args: "-F hello world", wantQuery: "hello world"},
		{name: "long flag", args: "--literal target", wantQuery: "target"},
		{name: "combined", args: "-F f:*.go file:*_test.go needle", wantQuery: "needle", wantInc: []string{"*.go"}, wantExc: []string{"*_test.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, literal, inc, exc, err := ParseArgsFlat(tc.args)
			if err != nil || !literal || query != tc.wantQuery || !slices.Equal(inc, tc.wantInc) || !slices.Equal(exc, tc.wantExc) {
				t.Fatalf("ParseArgsFlat(%q) = (%q, %v, %v, %v, %v)", tc.args, query, literal, inc, exc, err)
			}
		})
	}
}

func TestParseArgsFlat_Errors(t *testing.T) {
	for _, args := range []string{"", "  ", "-F", "f:*.go", "--literal"} {
		if _, _, _, _, err := ParseArgsFlat(args); !errors.Is(err, ErrEmptyQuery) {
			t.Fatalf("ParseArgsFlat(%q) err = %v, want ErrEmptyQuery", args, err)
		}
	}
}

func TestCompileMatcher_Case(t *testing.T) {
	re, err := CompileMatcher("foo", false)
	if err != nil || !re.MatchString("FOO") || !re.MatchString("foo") {
		t.Fatalf("CompileMatcher lowercase failed: %v", err)
	}
	re, err = CompileMatcher("Foo", false)
	if err != nil || !re.MatchString("Foo") || re.MatchString("foo") {
		t.Fatalf("CompileMatcher mixed-case failed: %v", err)
	}
}

func TestCompileMatcher_LiteralAndError(t *testing.T) {
	re, err := CompileMatcher("foo[bar]", true)
	if err != nil || !re.MatchString("foo[bar]") || re.MatchString("foob") {
		t.Fatalf("CompileMatcher literal failed: %v", err)
	}
	if _, err := CompileMatcher("[unclosed", false); err == nil {
		t.Fatalf("expected error for unclosed regex")
	}
}

package changelog_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/changelog"
)

func TestPlainInline(t *testing.T) {
	in := "Fixed `foo` and **bar**"
	want := "Fixed foo and bar"
	if got := changelog.PlainInline(in); got != want {
		t.Errorf("PlainInline(%q) = %q, want %q", in, got, want)
	}
}

func TestWrapParagraph(t *testing.T) {
	text := "This is a simple bullet text that wraps nicely across lines."
	lines := changelog.WrapParagraph(text, true, 30)
	if len(lines) < 2 {
		t.Errorf("expected wrapped lines >= 2, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "\u2022") {
		t.Errorf("first line missing bullet: %q", lines[0])
	}
}

func TestIsSourceHeader(t *testing.T) {
	if !changelog.IsSourceHeader("1.3.6:") {
		t.Error("expected 1.3.6: to be header")
	}
	if changelog.IsSourceHeader("1.3.6") {
		t.Error("expected 1.3.6 without colon to not be header")
	}
	if changelog.IsSourceHeader("not a header") {
		t.Error("expected false")
	}
}

func TestSourceLines(t *testing.T) {
	lines := changelog.Lines()
	if len(lines) == 0 {
		t.Skip("changelog_reference.txt not found from test runner")
	}
	if !changelog.Loaded() {
		t.Error("expected Loaded() = true")
	}
}

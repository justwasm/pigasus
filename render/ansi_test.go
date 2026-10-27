package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestCellWidth_ANSI verifies that cellWidth ignores ANSI escape sequences
// and reports the true terminal visible width.
func TestCellWidth_ANSI(t *testing.T) {
	plain := "hello"
	styled := "\x1b[31;1mhello\x1b[0m"
	if w := cellWidth(styled); w != len(plain) {
		t.Fatalf("cellWidth(%q) = %d, want %d", styled, w, len(plain))
	}
}

// TestCellWidth_CJK verifies wide characters consume two cells each.
func TestCellWidth_CJK(t *testing.T) {
	cjk := "你好世界"
	if w := cellWidth(cjk); w != 8 {
		t.Fatalf("cellWidth(%q) = %d, want 8", cjk, w)
	}
}

// TestTruncateToCells_ANSI verifies truncation accounts for visible width
// and does not split escape sequences.
func TestTruncateToCells_ANSI(t *testing.T) {
	styled := "\x1b[32mhello world\x1b[0m"
	truncated := truncateToCells(styled, 5)
	if w := ansi.StringWidth(truncated); w != 5 {
		t.Fatalf("truncateToCells visible width = %d, want 5: %q", w, truncated)
	}
	if !strings.Contains(truncated, "hello") {
		t.Fatalf("expected 'hello' in %q", truncated)
	}
}

// TestTruncateToCells_CJK verifies truncation lands on rune boundaries.
func TestTruncateToCells_CJK(t *testing.T) {
	cjk := "你好世界" // 8 cells
	// Truncating to 5 cells can fit 2 characters (4 cells).
	truncated := truncateToCells(cjk, 5)
	if w := ansi.StringWidth(truncated); w != 4 {
		t.Fatalf("truncateToCells(%q, 5) width = %d, want 4", cjk, w)
	}
	if truncated != "你好" {
		t.Fatalf("truncateToCells(%q, 5) = %q, want '你好'", cjk, truncated)
	}
}

// TestDivider_Styled verifies Divider with ANSI style emits style prefix and reset.
func TestDivider_Styled(t *testing.T) {
	got := Divider(10, "\x1b[34m")
	want := "\x1b[34m" + strings.Repeat("─", 10) + "\x1b[0m"
	if got != want {
		t.Fatalf("Divider styled got %q, want %q", got, want)
	}
	if w := ansi.StringWidth(got); w != 10 {
		t.Fatalf("Divider visible width = %d, want 10", w)
	}
}

// TestDividerWithLabel_Styled verifies DividerWithLabel with CJK and ANSI style.
func TestDividerWithLabel_Styled(t *testing.T) {
	got := DividerWithLabel(20, "测试", "\x1b[33m")
	if w := ansi.StringWidth(got); w != 20 {
		t.Fatalf("DividerWithLabel visible width = %d, want 20: %q", w, got)
	}
	if !strings.HasPrefix(got, "\x1b[33m─── 测试 ") {
		t.Fatalf("DividerWithLabel missing prefix: %q", got)
	}
}

// TestBox_Styled verifies Box with ANSI style wraps lines correctly.
func TestBox_Styled(t *testing.T) {
	got := Box(14, []string{"测试"}, "\x1b[35m")
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("Box returned %d lines, want 3", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 14 {
			t.Errorf("line %d visible width = %d, want 14: %q", i, w, l)
		}
	}
}

package changelog_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/changelog"
)

// TestExcerptRows pins the panel's opening screen: the title row, the
// tagline and the 1.3.6 bullet points.
//
// The excerpt is the shape the panel shows when it has neither the
// reference's release-notes file nor a terminal size to measure, so it
// is the panel's own document — and BuildRestRows skips the embedded
// markdown's first section because this excerpt already carries it.
func TestExcerptRows(t *testing.T) {
	rows := changelog.ExcerptRows()
	if len(rows) < 3 {
		t.Fatalf("ExcerptRows() = %d rows, want at least 3", len(rows))
	}
	if !strings.Contains(rows[0], "Pigasus CLI Changelog") {
		t.Errorf("first row = %q, want the changelog title", rows[0])
	}
	if !strings.Contains(strings.Join(rows, "\n"), "1.3.6") {
		t.Errorf("excerpt has no 1.3.6 section: %v", rows)
	}
}

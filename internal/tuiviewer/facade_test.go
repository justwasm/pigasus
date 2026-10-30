package tuiviewer_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/tuiviewer"
)

func TestTuiviewerFacade(t *testing.T) {
	if tuiviewer.ChangelogTotalLines != 2014 {
		t.Fatalf("unexpected ChangelogTotalLines: %d", tuiviewer.ChangelogTotalLines)
	}
	c := tuiviewer.Counter(1, 10, 100)
	if c == "" {
		t.Fatal("expected non-empty Counter")
	}
}

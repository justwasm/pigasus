package logspanel

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
)

func TestDocumentAndPanelLines(t *testing.T) {
	st := State{
		Path:      "/tmp/cli-20261124_000000.log",
		Follow:    true,
		Width:     80,
		Height:    20,
		SizeBytes: 2048,
		Lines: []string{
			"[2026-11-24 00:00:01.000] [tui] interactive session starting",
			"[2026-11-24 00:00:02.000] [streampipe] endpoint=https://example.com attempt=2/10 retry_in=2s reason=HTTP 503: MODEL_CAPACITY_EXHAUSTED",
		},
	}

	spec := Spec(st)
	if spec.Visible != VisibleRows(20) {
		t.Fatalf("Visible = %d, want %d", spec.Visible, VisibleRows(20))
	}
	if spec.Total < 2 {
		t.Fatalf("Total = %d, want >= 2", spec.Total)
	}

	rows, start, end, _ := tuiviewer.Window(spec, 0)
	lines := PanelLines(st, rows, start, end, spec.Total)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Pigasus Session Logs") {
		t.Errorf("missing title in:\n%s", joined)
	}
	if !strings.Contains(joined, "[LIVE FOLLOW]") {
		t.Errorf("missing [LIVE FOLLOW] badge in:\n%s", joined)
	}
	if !strings.Contains(joined, st.Path) {
		t.Errorf("missing file path in:\n%s", joined)
	}
	if !strings.Contains(joined, "MODEL_CAPACITY_EXHAUSTED") {
		t.Errorf("missing log content in:\n%s", joined)
	}
	if !strings.Contains(joined, "auto-tail ON") {
		t.Errorf("missing auto-tail footer in:\n%s", joined)
	}
}

func TestDocumentEmptyState(t *testing.T) {
	doc := Document(State{Width: 80, Height: 20})
	if len(doc) != 1 || !strings.Contains(ansi.Strip(doc[0]), "No session log file yet") {
		t.Fatalf("unexpected empty doc: %v", doc)
	}
}

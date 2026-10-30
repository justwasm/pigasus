package bashmode

import (
	"strings"
	"testing"
)

func TestHeadRow(t *testing.T) {
	row := HeadRow("ls -la", false, false)
	if !strings.Contains(row, "! ls -la") || !strings.Contains(row, "●") {
		t.Errorf("HeadRow success unexpected: %q", row)
	}

	rowFail := HeadRow("false", true, false)
	if !strings.Contains(rowFail, "! false") || !strings.Contains(rowFail, "●") {
		t.Errorf("HeadRow fail unexpected: %q", rowFail)
	}

	rowRunning := HeadRow("sleep 10", false, true)
	if !strings.Contains(rowRunning, "○") {
		t.Errorf("HeadRow running unexpected: %q", rowRunning)
	}
}

func TestBodyRows(t *testing.T) {
	rows := BodyRows([]string{"out1", "out2"}, false)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0], "  ⎿  ") || !strings.Contains(rows[0], "out1") {
		t.Errorf("row[0] unexpected: %q", rows[0])
	}
	if strings.Contains(rows[1], "  ⎿  ") || !strings.Contains(rows[1], "out2") {
		t.Errorf("row[1] unexpected: %q", rows[1])
	}
}

func TestBlockLines(t *testing.T) {
	lines := BlockLines("echo hi", "hi", false, false, false, 40)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	// Truncated block
	bigOutput := strings.Repeat("line\n", 50)
	lines = BlockLines("seq 50", bigOutput, false, false, false, 40)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "<output +") || !strings.Contains(joined, "(ctrl+o to expand)") {
		t.Errorf("expected truncated output with expand hint, got: %q", joined)
	}
}

func TestTrimOutput(t *testing.T) {
	if got := TrimOutput("foo\r\n\n"); got != "foo" {
		t.Errorf("TrimOutput = %q, want 'foo'", got)
	}
}

package logts_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/logts"
)

// TestFormatLogTS pins the log-file timestamp's shape.
//
// The shape is the reference's: the log file's name carries a
// `YYYYMMDD_HHMMSS` suffix.
func TestFormatLogTS(t *testing.T) {
	got := logts.FormatLogTS()
	if len(got) != len("20060102_150405") {
		t.Errorf("FormatLogTS() = %q, want 15 characters", got)
	}
	if strings.ContainsAny(got, "-:/. ") {
		t.Errorf("FormatLogTS() = %q, want only digits and one underscore", got)
	}
}

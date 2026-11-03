package tuilib

import (
	"strings"

	"github.com/justwasm/pigasus/internal/core/plural"
)

// FileLineCount counts the number of lines in a file's content, matching wc -l.
// Trims trailing newlines so that content ending with '\n' does not add an
// extra phantom line.
func FileLineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(content, "\n"), "\n") + 1
}

// CountLabel renders "<n> <word>" using the singular form when n is 1
// ("1 file", never "1 files").
//
// The rule itself lives in tier 1 (`internal/core/plural`): a feature
// package needs it too, and a feature may not import a view.
func CountLabel(n int, word [2]string) string {
	return plural.CountLabel(n, word)
}

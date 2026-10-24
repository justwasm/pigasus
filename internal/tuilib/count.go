package tuilib

import (
	"fmt"
	"strings"
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
func CountLabel(n int, word [2]string) string {
	if n == 1 {
		return "1 " + word[0]
	}
	return fmt.Sprintf("%d %s", n, word[1])
}

// Copyright (c) 2026, the openagy project authors.
//
// trim.go — trailing-whitespace normalisation for the /diff viewer inline subview.
//
// AGY 1.2.16: "Fixed the diff viewer not trimming trailing whitespace
// from individual diff lines, so a file with trailing tabs was rendered
// with explicit trailing whitespace markers that hurt readability."
//
// The underlying file on disk is unchanged — only the rendered string
// loses the trailing tabs and spaces. The leading marker byte (` `, `+`,
// `-`, `@`, or a file/hunk header) is never part of what "trailing"
// means, so a `-hello\t\t` line becomes `-hello` (marker kept, tail
// dropped) and a header line like `@@ -1,2 +1,2 @@` is untouched because
// it has no trailing whitespace to begin with.
package diffsyntax

import "strings"

// TrimTrailing returns line with every trailing U+0020 / U+0009
// removed. The leading marker is left alone because "trailing whitespace"
// is by definition a suffix; the function only inspects the tail.
//
// A line that consists entirely of spaces and tabs is collapsed to "" so
// it renders as a blank row rather than a row that ends in whitespace
// markers. Truly empty lines stay empty.
func TrimTrailing(line string) string {
	return strings.TrimRight(line, " \t")
}

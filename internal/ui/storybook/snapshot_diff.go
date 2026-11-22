package storybook

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// CharDiff details a single cell character or attribute mismatch.
type CharDiff struct {
	Row          int
	Col          int
	ExpectedR    rune
	ActualR      rune
	ExpectedAttr CellAttribute
	ActualAttr   CellAttribute
}

// Diff compares expected and actual ScreenBuffers and returns a character-level diff report.
// If both buffers are visually and stylistically identical, it returns an empty string.
func Diff(expected, actual *ScreenBuffer) string {
	if expected == nil && actual == nil {
		return ""
	}
	if expected == nil {
		return "expected ScreenBuffer is nil, actual is non-nil"
	}
	if actual == nil {
		return "expected ScreenBuffer is non-nil, actual is nil"
	}

	if expected.Width != actual.Width || expected.Height != actual.Height {
		return fmt.Sprintf("ScreenBuffer dimension mismatch: expected %dx%d, got %dx%d",
			expected.Width, expected.Height, actual.Width, actual.Height)
	}

	diffs := CharacterDiffs(expected, actual)
	if len(diffs) == 0 {
		return ""
	}

	// Group diffs by row
	rowDiffs := make(map[int][]CharDiff)
	var orderedRows []int
	for _, d := range diffs {
		if _, seen := rowDiffs[d.Row]; !seen {
			orderedRows = append(orderedRows, d.Row)
		}
		rowDiffs[d.Row] = append(rowDiffs[d.Row], d)
	}

	var sb strings.Builder
	sb.WriteString("=== 2D SCREEN BUFFER REGRESSION DETECTED ===\n")
	sb.WriteString(fmt.Sprintf("Dimensions: %dx%d | Total cell differences: %d across %d rows\n\n",
		expected.Width, expected.Height, len(diffs), len(orderedRows)))

	for _, y := range orderedRows {
		cds := rowDiffs[y]
		expRow := expected.RowText(y)
		actRow := actual.RowText(y)

		// Build ruler marker line
		marker := make([]rune, expected.Width)
		for i := range marker {
			marker[i] = ' '
		}
		for _, d := range cds {
			if d.Col >= 0 && d.Col < len(marker) {
				marker[d.Col] = '^'
			}
		}

		sb.WriteString(fmt.Sprintf("--- Row %02d Mismatch ---\n", y))
		sb.WriteString(fmt.Sprintf("Exp: |%s|\n", expRow))
		sb.WriteString(fmt.Sprintf("Act: |%s|\n", actRow))
		sb.WriteString(fmt.Sprintf("Dif:  %s\n", strings.TrimRight(string(marker), " ")))

		// Detail individual character / attribute discrepancies
		for _, d := range cds {
			var details []string
			if d.ExpectedR != d.ActualR {
				details = append(details, fmt.Sprintf("rune mismatch: exp %q (U+%04X), act %q (U+%04X)",
					d.ExpectedR, d.ExpectedR, d.ActualR, d.ActualR))
			}
			if !d.ExpectedAttr.Equals(d.ActualAttr) {
				details = append(details, fmt.Sprintf("attr mismatch: exp [%s], act [%s]",
					d.ExpectedAttr.String(), d.ActualAttr.String()))
			}
			sb.WriteString(fmt.Sprintf("  • Col %02d: %s\n", d.Col, strings.Join(details, " | ")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// CharacterDiffs returns a slice of all individual cell differences between two ScreenBuffers.
func CharacterDiffs(expected, actual *ScreenBuffer) []CharDiff {
	if expected == nil || actual == nil {
		return nil
	}

	var diffs []CharDiff
	minH := expected.Height
	if actual.Height < minH {
		minH = actual.Height
	}
	minW := expected.Width
	if actual.Width < minW {
		minW = actual.Width
	}

	for y := 0; y < minH; y++ {
		for x := 0; x < minW; x++ {
			expR := expected.TextAt(y, x)
			actR := actual.TextAt(y, x)
			expAttr := expected.AttrAt(y, x)
			actAttr := actual.AttrAt(y, x)

			if expR != actR || !expAttr.Equals(actAttr) {
				diffs = append(diffs, CharDiff{
					Row:          y,
					Col:          x,
					ExpectedR:    expR,
					ActualR:      actR,
					ExpectedAttr: expAttr,
					ActualAttr:   actAttr,
				})
			}
		}
	}

	// Height overhang
	if actual.Height > minH {
		for y := minH; y < actual.Height; y++ {
			for x := 0; x < actual.Width; x++ {
				diffs = append(diffs, CharDiff{
					Row:        y,
					Col:        x,
					ExpectedR:  0,
					ActualR:    actual.TextAt(y, x),
					ActualAttr: actual.AttrAt(y, x),
				})
			}
		}
	}

	return diffs
}

// DiffFormattedLines produces a unified-like diff of formatted lines for terminal rendering.
func DiffFormattedLines(expected, actual string) string {
	expLines := strings.Split(expected, "\n")
	actLines := strings.Split(actual, "\n")

	maxLen := len(expLines)
	if len(actLines) > maxLen {
		maxLen = len(actLines)
	}

	var sb strings.Builder
	for i := 0; i < maxLen; i++ {
		var expL, actL string
		if i < len(expLines) {
			expL = expLines[i]
		}
		if i < len(actLines) {
			actL = actLines[i]
		}

		if expL != actL {
			sb.WriteString(fmt.Sprintf("- L%02d: %s\n", i, expL))
			sb.WriteString(fmt.Sprintf("+ L%02d: %s\n", i, actL))
		}
	}
	return sb.String()
}

// CellCount returns width * height of buffer cells.
func CellCount(sb *ScreenBuffer) int {
	if sb == nil {
		return 0
	}
	return sb.Width * sb.Height
}

// RuneWidth computes display width of a rune.
func RuneWidth(r rune) int {
	if r == 0 {
		return 0
	}
	return utf8.RuneLen(r)
}

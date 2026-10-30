package viewer

import (
	"strconv"

	"github.com/justwasm/pigasus/palette"
)

// DiffFirstChangedPath returns the path of the first change in the working copy.
func DiffFirstChangedPath(tracked, untracked []string) string {
	if len(tracked) > 0 {
		return tracked[0]
	}
	if len(untracked) > 0 {
		return untracked[0]
	}
	return ""
}

// FormatTurnRow renders one row of the Per Turn diff tab.
func FormatTurnRow(turnIdx int, cursor int, file string) string {
	marker := "  "
	if turnIdx == cursor {
		marker = palette.BlueOpen + "> " + palette.BlueClose
	}
	return marker + "Turn " + strconv.Itoa(turnIdx+1) + " · " + palette.DimGrayOpen + file + palette.DimGrayClose
}

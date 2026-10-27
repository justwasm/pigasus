// Package diffaction render.go — rendering helpers for the /diff panel.
package diffaction

import (
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/palette"
)

// Views are the three tabs of the /diff viewer, in REF order:
// All Changes, Per Turn, Commit Tree.
var Views = []string{"All Changes", "Per Turn", "Commit Tree"}

// Counts renders a change's " +N -M" suffix.
func Counts(c Change) string {
	var parts []string
	if c.Added > 0 {
		parts = append(parts, "\x1b[38;2;133;153;0m+"+strconv.Itoa(c.Added)+"\x1b[39m")
	}
	if c.Deleted > 0 {
		parts = append(parts, "\x1b[38;2;220;50;47m-"+strconv.Itoa(c.Deleted)+"\x1b[39m")
	}
	return strings.Join(parts, " ")
}

// TabRow renders the /diff tab header row.
func TabRow(vcs string, active int, leading bool, views []string) string {
	if len(views) == 0 {
		views = Views
	}
	var b strings.Builder
	if leading {
		b.WriteString(" ")
	}
	b.WriteString("\x1b[1mDiff (" + vcs + ")\x1b[0m")
	for i, name := range views {
		b.WriteString("  ")
		if i == active {
			b.WriteString(palette.BlueOpen + name + palette.BlueClose)
		} else {
			b.WriteString(name)
		}
	}
	return b.String()
}

// AllChangesRow renders one change row in the All Changes tab.
func AllChangesRow(c Change, cursor bool) string {
	var b strings.Builder
	switch {
	case cursor && c.Untracked:
		b.WriteString(palette.BlueOpen + "> " + "\x1b[38;5;172m? [Untracked] " + palette.BlueOpen + c.Path + palette.BlueClose)
	case cursor:
		b.WriteString(palette.BlueOpen + "> " + c.Path + palette.BlueClose)
	case c.Untracked:
		b.WriteString("  " + "\x1b[38;5;172m? [Untracked] " + "\x1b[39m" + c.Path)
	default:
		b.WriteString("  " + c.Path)
	}
	if counts := Counts(c); counts != "" {
		b.WriteString("  ")
		b.WriteString(counts)
	}
	return b.String()
}

// AllChangesLines renders the All Changes tab's file list or empty state.
func AllChangesLines(tracked, untracked []Change, cursor int, emptyHint, navHint string) []string {
	total := len(tracked) + len(untracked)
	if total == 0 {
		return []string{
			TabRow("git", 0, true, Views),
			"",
			"   " + palette.DimGrayOpen + "No uncommitted changes" + palette.DimGrayClose,
			"",
			"   " + emptyHint,
		}
	}
	if cursor < 0 || cursor >= total {
		cursor = 0
	}
	lines := []string{
		TabRow("git", 0, false, Views),
		palette.SubtleOpen + "  " + strconv.Itoa(total) + " file(s) changed\x1b[39m",
		"",
	}
	for i, c := range tracked {
		lines = append(lines, AllChangesRow(c, cursor == i))
	}
	if len(tracked) > 0 && len(untracked) > 0 {
		lines = append(lines, "")
	}
	for i, c := range untracked {
		lines = append(lines, AllChangesRow(c, cursor == len(tracked)+i))
	}
	lines = append(lines, "", "  "+navHint)
	return lines
}

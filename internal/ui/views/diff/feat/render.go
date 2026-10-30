package feat

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RunCLI is the `--diff-with-agy` CLI handler.
func RunCLI(w io.Writer, mode string) {
	t := SourceData()
	if t == nil {
		fmt.Fprintln(w, "feature-diff: data file not found (run from the project root, or set up go.mod)")
		return
	}
	if mode == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(t)
		return
	}
	WriteTextTable(w, t)
}

// WriteTextTable renders the human-readable table to w.
func WriteTextTable(w io.Writer, t *Table) {
	entries := SortedEntries(t)
	featureWidth := computeFeatureWidth(entries)

	const (
		agyWidth    = 3
		cloneWidth  = 5
		cycleWidth  = 5
		statusWidth = 9
	)

	header := padRight("Feature", featureWidth) + " " +
		padRight("AGY", agyWidth) + " " +
		padRight("Clone", cloneWidth) + " " +
		padRight("Cycle", cycleWidth) + " " +
		padRight("Status", statusWidth)
	fmt.Fprintln(w, header)

	rule := strings.Repeat("─", featureWidth) + " " +
		strings.Repeat("─", agyWidth) + " " +
		strings.Repeat("─", cloneWidth) + " " +
		strings.Repeat("─", cycleWidth) + " " +
		strings.Repeat("─", statusWidth)
	fmt.Fprintln(w, rule)

	for _, e := range entries {
		feature := e.Name
		if len(feature) > featureWidth {
			feature = feature[:featureWidth-3] + "..."
		}
		row := padRight(feature, featureWidth) + " " +
			padRight(CheckOrCross(e.Agy), agyWidth) + " " +
			padRight(CheckOrCross(e.Clone), cloneWidth) + " " +
			padRight(e.CycleLabel(), cycleWidth) + " " +
			padRight(string(e.ClassifyStatus()), statusWidth)
		fmt.Fprintln(w, row)
	}
}

func computeFeatureWidth(entries []Entry) int {
	const featureCap = 54
	featureWidth := len("Feature")
	for _, e := range entries {
		n := len(e.Name)
		if n > featureWidth {
			featureWidth = n
		}
	}
	if featureWidth > featureCap {
		featureWidth = featureCap
	}
	return featureWidth
}

// CheckOrCross returns ✓ when true and ✗ when false.
func CheckOrCross(v bool) string {
	if v {
		return "✓"
	}
	return "✗"
}

// PanelTitle is the title strip the panel renders at the top.
func PanelTitle() string {
	return "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Feature Diff \x1b[0m"
}

// PanelDoc renders every feature as a styled row for the TUI overlay panel.
func PanelDoc(t *Table) []string {
	entries := SortedEntries(t)

	var aligned, gap, diverged int
	for _, e := range entries {
		switch e.ClassifyStatus() {
		case Aligned:
			aligned++
		case Gap:
			gap++
		case Diverged:
			diverged++
		}
	}
	summary := fmt.Sprintf("  \x1b[38;5;235m%d features · %d aligned · %d gap · %d diverged · AGY %s · clone %s\x1b[39m",
		len(entries), aligned, gap, diverged, t.AgyVersion, t.CloneVersion)

	header := "\x1b[38;5;238m  Feature" + strings.Repeat(" ", 48) + " AGY  Clone Cycle  Status\x1b[39m"

	out := []string{
		PanelTitle(),
		"",
		"",
		summary,
		"",
		header,
		"",
	}
	for _, e := range entries {
		out = append(out, formatPanelRow(e))
	}
	return out
}

func formatPanelRow(e Entry) string {
	name := e.Name
	if len(name) > 54 {
		name = name[:51] + "..."
	}
	statusStyle := "\x1b[38;5;244m"
	switch e.ClassifyStatus() {
	case Aligned:
		statusStyle = "\x1b[38;5;34m"
	case Gap:
		statusStyle = "\x1b[38;5;166m"
	case Diverged:
		statusStyle = "\x1b[38;5;172m"
	}
	return fmt.Sprintf("  %-54s  %s  %s    %-5s  %s%s\x1b[39m",
		name,
		CheckOrCross(e.Agy),
		CheckOrCross(e.Clone),
		e.CycleLabel(),
		statusStyle,
		e.ClassifyStatus(),
	)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

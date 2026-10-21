// feat_diff.go — cycle 236: `wagyu --diff-with-agy` + the
// `/feature-diff` slash panel.
//
// Source of truth: internal/cli/testdata/feature_diff_table.json. The
// loader walks up from cwd looking for go.mod (same shape as
// slash_changelog_source.go's loadChangelogSource), parses the JSON
// once, and caches it behind a sync.Once.
//
// Three access modes:
//
//   1. `wagyu --diff-with-agy`         — human-readable table to stdout
//   2. `wagyu --diff-with-agy --json`  — raw JSON to stdout
//   3. `wagyu` (TUI) /feature-diff     — overlay panel mirror of /changelog
//
// The flag path is wired in cmd/wagyu/main.go (one block in runRoot,
// after the --tools / --services / --oauth short-circuits). The
// slash path is wired in slash_panels.go (one entry in
// panelForCommand + one switch arm in panelLines + one branch in
// viewerSpecFor) and slash_commands.go (one case in
// handleSlashCommand for tests that drive the handler directly).
// The print-mode path lives in print_slash.go (one switch arm in
// dispatchPrintSlash). The picker entry is in
// internal/tuilib/commands.go.
//
// Status mapping:
//
//   agy=true  clone=true  → "aligned"
//   agy=true  clone=false → "gap"
//   agy=false clone=true  → "diverged"
//   agy=false clone=false → "diverged" (clone-only extras; rare)
//
// Categories come from the JSON's `categories` array (alphabetical);
// features are sorted by category then name within category.
//
// The text-mode table renderer pads to a fixed column width and emits
// the four columns "Feature / AGY / Clone / Cycle / Status" with
// box-drawing unicode (─) for the header rule. No external
// table-rendering dep — `lipgloss/v2` is already in go.mod but the
// renderer is hand-rolled to match the byte-exact style /changelog's
// counter line uses, and to keep the JSON `--json` path the only
// thing that produces ANSI colour (the text path is plain ASCII so
// `wagyu --diff-with-agy | cat` reads cleanly in a pipe).

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

const featDiffDataFile = "internal/cli/testdata/feature_diff_table.json"

// featDiffTable is the parsed JSON shape. Field order is the JSON's
// field order; the renderer is agnostic to extra fields so the cycle's
// later revisions don't need to touch this struct.
type featDiffTable struct {
	Version      string          `json:"version"`
	AgyVersion   string          `json:"agy_version"`
	CloneVersion string          `json:"clone_version"`
	Notes        string          `json:"notes"`
	Categories   []string        `json:"categories"`
	Features     []featDiffEntry `json:"features"`
}

// featDiffEntry is one row in the source-of-truth JSON. Cycle is a
// pointer so the JSON's missing / null fields come back as nil (the
// "no originating cycle" case for gap rows).
type featDiffEntry struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Agy      bool   `json:"agy"`
	Clone    bool   `json:"clone"`
	Cycle    *int   `json:"cycle"`
	Evidence string `json:"evidence"`
	Notes    string `json:"notes"`
}

// featDiffStatus classifies one entry. The three states the user sees
// in the table are aligned / diverged / gap.
type featDiffStatus string

const (
	featDiffAligned  featDiffStatus = "aligned"
	featDiffDiverged featDiffStatus = "diverged"
	featDiffGap      featDiffStatus = "gap"
)

// classifyStatus folds the booleans into the three-word status.
func (e featDiffEntry) classifyStatus() featDiffStatus {
	switch {
	case e.Agy && e.Clone:
		return featDiffAligned
	case e.Agy && !e.Clone:
		return featDiffGap
	default:
		return featDiffDiverged
	}
}

// cycleLabel renders the cycle number as a 5-wide field, or "—" when
// the entry has no originating cycle (a gap row).
func (e featDiffEntry) cycleLabel() string {
	if e.Cycle == nil {
		return "—"
	}
	return strconv.Itoa(*e.Cycle)
}

// Loader state — sync.Once-protected, populated on first read.
var (
	featDiffOnce sync.Once
	featDiffData *featDiffTable // nil on miss
	featDiffErr  error          // surfaced by tests that want strict mode
	featDiffPath string         // absolute path the data was loaded from
)

// featDiffSourceLoaded reports whether the JSON file was found and
// parsed. Mirrors changelogSourceLoaded so the same skip-if-not-found
// pattern is available to callers that want graceful degradation.
func featDiffSourceLoaded() bool {
	featDiffOnce.Do(loadFeatDiffSource)
	return featDiffData != nil
}

// featDiffSourceData returns the parsed table or nil when the file was
// not found. Production paths should pair this with a fallback
// message; tests should call featDiffSourceLoaded first.
func featDiffSourceData() *featDiffTable {
	featDiffOnce.Do(loadFeatDiffSource)
	return featDiffData
}

// featDiffSourcePath returns the absolute path the JSON was loaded
// from ("" when the file was not found). Used by tests.
func featDiffSourcePath() string {
	featDiffOnce.Do(loadFeatDiffSource)
	return featDiffPath
}

// loadFeatDiffSource reads internal/cli/testdata/feature_diff_table.json
// from the project root (the nearest ancestor of cwd that contains
// go.mod) and stores the parsed shape. The result is cached; a missing
// or unreadable file leaves featDiffData nil so callers fall back.
func loadFeatDiffSource() {
	path := findFeatDiffDataPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		featDiffErr = err
		return
	}
	var t featDiffTable
	if err := json.Unmarshal(data, &t); err != nil {
		featDiffErr = err
		return
	}
	featDiffData = &t
	featDiffPath = path
}

// findFeatDiffDataPath returns the absolute path to the JSON file
// relative to the project root. Walks up from cwd looking for go.mod;
// returns "" when no project root is found or the file is missing.
func findFeatDiffDataPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root := findProjectRoot(cwd)
	if root == "" {
		return ""
	}
	candidate := filepath.Join(root, featDiffDataFile)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// sortedFeatDiffEntries returns the entries sorted by category then
// name within category. Categories come from the JSON's `categories`
// array (alphabetical), so the result is deterministic across
// environments that ship the same JSON.
func sortedFeatDiffEntries(t *featDiffTable) []featDiffEntry {
	catOrder := map[string]int{}
	for i, c := range t.Categories {
		catOrder[c] = i
	}
	out := make([]featDiffEntry, len(t.Features))
	copy(out, t.Features)
	sort.SliceStable(out, func(i, j int) bool {
		// Unknown categories sink to the bottom.
		oi, oki := catOrder[out[i].Category]
		oj, okj := catOrder[out[j].Category]
		switch {
		case oki && !okj:
			return true
		case !oki && okj:
			return false
		case oki && okj && oi != oj:
			return oi < oj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// runFeatDiffCLI is the `--diff-with-agy` CLI handler. mode is
// "text" (default) or "json". Both modes write to w, never to a
// temp file, so a `--diff-with-agy > file.md` redirect works as
// expected. The function never returns an error — missing data
// degrades to a single-line notice.
//
// The flag-style interface is intentional: it lets the call site
// pass either `*flag.FlagSet` (production) or `nil` (tests that
// always want text). `output-format json` is the trigger for the
// JSON output path; everything else is text. The explicit `json`
// argument lets a future cycle add more output formats without
// changing the call site.
func runFeatDiffCLI(w io.Writer, mode string) {
	t := featDiffSourceData()
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
	writeFeatDiffTextTable(w, t)
}

// writeFeatDiffTextTable renders the human-readable table. Columns
// (in order): Feature / AGY / Clone / Cycle / Status.
//
// Width strategy:
//   - Feature:    min(54, longest feature name)
//   - AGY:        3
//   - Clone:      5
//   - Cycle:      5
//   - Status:     9   (longest is "diverged")
//
// Each cell is right-padded with spaces; no tabs (the column header
// uses `─` which is wider than one cell in some terminals, so
// monospace alignment depends on the same column width everywhere).
// The header rule is `─` repeated to width.
func writeFeatDiffTextTable(w io.Writer, t *featDiffTable) {
	entries := sortedFeatDiffEntries(t)

	// Compute Feature column width: clamp at 54 so the table fits a
	// 100-wide terminal (cycle conventions: tests run at 100 wide).
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

	const (
		agyWidth    = 3
		cloneWidth  = 5
		cycleWidth  = 5
		statusWidth = 9
	)

	// Header row.
	header := padRight("Feature", featureWidth) + " " +
		padRight("AGY", agyWidth) + " " +
		padRight("Clone", cloneWidth) + " " +
		padRight("Cycle", cycleWidth) + " " +
		padRight("Status", statusWidth)
	fmt.Fprintln(w, header)

	// Rule under the header — uses Unicode BOX DRAWINGS LIGHT HORIZONTAL
	// (U+2500) so a redirect-to-file keeps the alignment visible in a
	// pager.
	rule := strings.Repeat("─", featureWidth) + " " +
		strings.Repeat("─", agyWidth) + " " +
		strings.Repeat("─", cloneWidth) + " " +
		strings.Repeat("─", cycleWidth) + " " +
		strings.Repeat("─", statusWidth)
	fmt.Fprintln(w, rule)

	// Body rows. The AGY / Clone columns get a fixed glyph:
	//   agy=true  → "✓"
	//   agy=false → "✗"
	for _, e := range entries {
		feature := e.Name
		// Truncate with a trailing ellipsis if a name exceeds the
		// column cap so the table stays monospace-aligned.
		if len(feature) > featureWidth {
			feature = feature[:featureWidth-3] + "..."
		}
		row := padRight(feature, featureWidth) + " " +
			padRight(checkOrCross(e.Agy), agyWidth) + " " +
			padRight(checkOrCross(e.Clone), cloneWidth) + " " +
			padRight(e.cycleLabel(), cycleWidth) + " " +
			padRight(string(e.classifyStatus()), statusWidth)
		fmt.Fprintln(w, row)
	}
}

// checkOrCross is the AGY / Clone cell glyph: ✓ when true, ✗ when
// false. The reference cycle-231 convention uses these glyphs for
// feature parity cells.
func checkOrCross(v bool) string {
	if v {
		return "✓"
	}
	return "✗"
}

// ---- TUI panel integration ----------------------------------------------

// featDiffPanelTitle is the title strip the cycle-236 panel renders
// at the top — same shape as the cycle-193 panelChangelog title bar
// (yellow title with brand-blue background).
func featDiffPanelTitle() string {
	return "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Feature Diff \x1b[0m"
}

// featDiffPanelLines renders the panel body for the TUI. The output
// is a slice of terminal rows with ANSI colour (the cycle-193 panel
// contract). The first screen is a title + summary line + the first
// (height-window) rows; windowed scroll fills the rest.
//
// Falls back to a "data file not found" placeholder when the JSON is
// unreachable — the same graceful-degradation pattern the
// /changelog viewer uses for a missing source file.
//
// Scroll handling: the TUI reuses the cycle-207 viewerSpec pattern
// (panelChangelog's window/visible/total), so ↑/↓ / pgup/pgdown /
// ctrl+home / ctrl+end all work without per-panel key wiring.
func (m *runMode) featDiffPanelLines() []string {
	t := featDiffSourceData()
	if t == nil {
		return []string{
			featDiffPanelTitle(),
			"",
			cDimGrayOpen + "  data file not found (run from the project root)." + cDimGrayClose,
			"",
			panelDotHint("  ", "esc", "Close"),
		}
	}
	// Render the full document; the viewer pattern in viewerSpecFor
	// slices the visible window based on m.panel.window. The
	// integration with the rest of the panel infrastructure is in
	// viewerSpecFor and panelLines (cycle 236 wiring).
	return featDiffPanelDoc(t)
}

// featDiffPanelDoc renders every feature as a styled row. Header +
// summary line + 88 rows is ~92 lines; the cycle-207 viewer pattern
// slices them into a height-aware window.
func featDiffPanelDoc(t *featDiffTable) []string {
	entries := sortedFeatDiffEntries(t)

	// Summary counts.
	var aligned, gap, diverged int
	for _, e := range entries {
		switch e.classifyStatus() {
		case featDiffAligned:
			aligned++
		case featDiffGap:
			gap++
		case featDiffDiverged:
			diverged++
		}
	}
	summary := fmt.Sprintf("  \x1b[38;5;235m%d features · %d aligned · %d gap · %d diverged · AGY %s · clone %s\x1b[39m",
		len(entries), aligned, gap, diverged, t.AgyVersion, t.CloneVersion)

	header := panelGray238 + "  Feature" + strings.Repeat(" ", 48) + " AGY  Clone Cycle  Status" + panelGray238C

	out := []string{
		featDiffPanelTitle(),
		"",
		"",
		summary,
		"",
		header,
		"",
	}
	for _, e := range entries {
		name := e.Name
		if len(name) > 54 {
			name = name[:51] + "..."
		}
		// Per-status colour: aligned green (34), gap orange (166),
		// diverged orange-red (172). The colour wraps the status
		// word; the rest of the row stays default-fg.
		statusStyle := cDimGrayOpen
		switch e.classifyStatus() {
		case featDiffAligned:
			statusStyle = "\x1b[38;5;34m"
		case featDiffGap:
			statusStyle = "\x1b[38;5;166m"
		case featDiffDiverged:
			statusStyle = "\x1b[38;5;172m"
		}
		row := fmt.Sprintf("  %-54s  %s  %s    %-5s  %s%s\x1b[39m",
			name,
			checkOrCross(e.Agy),
			checkOrCross(e.Clone),
			e.cycleLabel(),
			statusStyle,
			e.classifyStatus(),
		)
		out = append(out, row)
	}
	return out
}

// featDiffViewerSpec returns the viewerSpec the cycle-207 pattern
// uses to scroll the /feature-diff panel. visible = height-4 (the
// cycle-207 contract verified against REF's footer counter math);
// the height-0 fallback is 17, matching /usage's static-doc contract
// (TestCycle207UsageDocGeometry pins that).
//
// total is set to len(doc) (not 1962 like /changelog), so the counter
// reads "(1–N of M lines)" with M = the actual feature count.
func (m *runMode) featDiffViewerSpec() viewerSpec {
	t := featDiffSourceData()
	if t == nil {
		return viewerSpec{}
	}
	doc := featDiffPanelDoc(t)
	vis := 17
	if m.height > 0 {
		vis = m.height - 4
		if vis < 1 {
			vis = 1
		}
	}
	return viewerSpec{lines: doc, visible: vis, total: len(doc)}
}

// handleFeatDiffPanelKey routes ↑/↓ / pgup/pgdown / ctrl+home /
// ctrl+end to m.panel.window when the panel is /feature-diff. The
// cycle-207 handleViewerPanelKey handles all of these for any
// panel where isViewer() returns true — we wire /feature-diff into
// isViewer in slash_panels.go and the central router in
// handlePanelKey picks it up automatically. This function is the
// per-panel entry hook for any future /feature-diff specific keys
// (e.g. cycle-237's filter toggle); today it's a thin pass-through.
func (m *runMode) handleFeatDiffPanelKey(key tea.Key) (bool, tea.Cmd) {
	if !m.panelFeatDiffOpen() {
		return false, nil
	}
	// No panel-specific bindings yet; defer to the central viewer
	// handler which honours ↑/↓ / pgup / pgdown / ctrl+home / ctrl+end.
	return m.handleViewerPanelKey(key)
}

// panelFeatDiffOpen reports whether the open panel is the
// /feature-diff panel. Mirrors the cycle-193 sentinel checks in
// handlePanelKey.
func (m *runMode) panelFeatDiffOpen() bool {
	return m.panel.kind == panelFeatDiff
}
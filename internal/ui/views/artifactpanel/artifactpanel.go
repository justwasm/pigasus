// Package artifactpanel holds the /artifact panel: the picker list and the
// file viewer it opens.
//
// # What it is
//
// The reference (`agy`) answers `/artifact` with a two-layer surface
// (official-docs/cli/artifacts.md, "The TUI partitions these assets into two
// interactive layers"):
//
//   - the **artifact picker**: a list whose selected row carries a `›`
//     cursor, navigated with ↑/↓;
//   - the **artifact detail viewer**: a full-screen view of the selected
//     file with a line-number gutter, a scroll position and a pager footer
//     — the same shape as the /changelog markdown viewer.
//
// The clone used to render only the empty frame — "No artifacts" — and then
// briefly only a flat, static list. Neither was selectable and neither could
// open a file, so `/artifact` could not do the one thing it exists for. This
// package now owns the whole surface: the list, its cursor, the inline
// preview and the viewer.
//
// # Where it lives
//
// The rows and the keys are here (see panel.go and panel_keys.go). The host
// owns only the seams: the conversation's artifacts, the terminal size, the
// way out and the editor. The empty frame is Lines, which stays byte-for-byte
// the reference's own capture (cycle 185's ppref.artifact.raw.txt, replayed
// as internal/ui/storybook/testdata/snapshots/panel-artifacts_empty-state.golden).
//
// # What is deliberately not here
//
// The reference's picker also carries a review workflow — `[ ]`/`[✓]`/`[✗]`
// markers, approve/reject, bulk approve. That state lives in the agent thread,
// and the clone has no such plumbing, so painting those markers or those keys
// would be a UI claiming a capability it does not have (AGENTS.md §13). The
// panel therefore shows, previews and opens artifacts; it does not pretend to
// approve them.
package artifactpanel

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"
)

// title is the panel's title row.
func title() string { return panelTitleOpen + "Artifacts" + panelTitleClose }

// legend is the reference's keyboard row, shared by the empty and populated
// frames.
//
// 【一手】 this is the row cycle 185 captured from the reference
// (ppref.artifact.raw.txt) and the storybook golden replays. Every key it
// names is implemented here: ↑/↓ move the cursor, `p` toggles the inline
// preview, `enter` opens the viewer and `ctrl+g` opens the editor.
func legend() string {
	return tuilib.PanelKeyboardHint("↑/↓", "Navigate", "p", "preview", "enter", "open",
		"ctrl+g", "open in editor", "esc", "Dismiss")
}

// Lines renders the panel's empty frame.
//
// The four rows are the reference's own: the title, the empty state, a blank
// and the legend. It is byte-identical to the capture cycle 185 aligned
// against.
func Lines() []string {
	return []string{
		title(),
		"  " + palette.DimGrayOpen + "No artifacts" + palette.DimGrayClose,
		"",
		legend(),
	}
}

// displayName is the artifact's row label.
//
// 【一手】 the reference's picker prints the **filename with its extension**
// ("› artifact-alignment-plan.md   open"); its checkpoint's resume block prints
// the stem, so the two labels differ on purpose. artifacts.List strips the
// extension for the checkpoint, so the picker asks the file for its base name
// instead — see Artifact.FileName.
func displayName(ref Artifact) string {
	if name := ref.FileName(); name != "" {
		return name
	}
	return strings.TrimSpace(ref.Name)
}

// row renders one artifact as a picker row.
//
// 【一手】 the capture shows the selected row carrying the `›` marker, the
// filename, a run of three spaces, and an inline `open` button:
//
//	› artifact-alignment-plan.md   open
//	  artifact-picker-and-viewer.md
//	  config-key-audit.md
//
// The `open` button is the row's "enter opens the viewer" affordance; the
// reference focuses it with h/l. The clone has no focus model, so it is painted
// on the selected row only and Enter already opens the viewer.
func row(ref Artifact, selected bool) string {
	name := displayName(ref)
	if selected {
		return palette.BlueOpen + "›" + palette.BlueClose + " " + name +
			"   " + palette.DimGrayOpen + "open" + palette.DimGrayClose
	}
	return "  " + palette.DimGrayOpen + name + palette.DimGrayClose
}

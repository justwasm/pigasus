// Package artifactpanel holds the /artifact panel's frame: the title,
// the empty state and the legend.
//
// # What it is
//
// The reference (`agy`) answers `/artifact` with a panel that lists the
// artifacts of the turn and offers to preview, open or edit them.
//
// The clone used to render only the empty frame — "No artifacts" — because
// the host handed the panel no data: Lines() takes no arguments and the
// registry wired it straight in as the row source. LinesFor is the populated
// frame the host now calls with artifacts.List(conversationID), the same
// source the checkpoint's resume block reads.
//
// # Where it lives
//
// The rows live here, so the host holds only the panel's kind and the
// fact that the panel echoes the "⎿  Exited /artifact command" notice
// when it closes. The frame keeps no state of its own: the rows are rebuilt
// from the conversation on every open, so there is no cursor to persist.
package artifactpanel

import (
	"github.com/justwasm/pigasus/internal/core/compaction"
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

// legend is the reference's keyboard row, shared by the empty and populated
// frames.
func legend() string {
	return tuilib.PanelKeyboardHint("↑/↓", "Navigate", "p", "preview", "enter", "open",
		"ctrl+g", "open in editor", "esc", "Dismiss")
}

// Lines renders the /artifact panel's empty frame.
//
// The four rows are the reference's own: the title, the empty state, a
// blank and the legend. This is the frame the reference draws when the
// conversation has no artifacts, and it is byte-identical to the capture
// cycle 185 aligned against (ppref.artifact.raw.txt).
func Lines() []string {
	return []string{
		panelTitleOpen + "Artifacts" + panelTitleClose,
		"  " + palette.DimGrayOpen + "No artifacts" + palette.DimGrayClose,
		"",
		legend(),
	}
}

// LinesFor renders the panel over the conversation's artifacts, falling back
// to the empty frame when there are none.
//
// 【存疑】 the reference's *populated* /artifact frame was never captured on
// this machine — cycle 206 recorded it as unverified, and the only golden
// (ppref.artifact.raw.txt) is the empty state. So the row shape below is the
// conservative one the other list panels use (two-space indent, the name,
// dim grey), and it deliberately does not claim reference parity. What it
// does fix is the real defect: the panel now reflects the conversation
// instead of answering "No artifacts" unconditionally.
func LinesFor(refs []compaction.ArtifactRef) []string {
	if len(refs) == 0 {
		return Lines()
	}
	lines := make([]string, 0, len(refs)+3)
	lines = append(lines, panelTitleOpen+"Artifacts"+panelTitleClose)
	for _, ref := range refs {
		lines = append(lines, "  "+palette.DimGrayOpen+ref.Name+palette.DimGrayClose)
	}
	lines = append(lines, "", legend())
	return lines
}

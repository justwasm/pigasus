// panel.go — the /artifact panel's state and rows.
//
// # What it is
//
// The panel has two layers, and the whole of both lives here:
//
//	Artifacts                    ← title
//	› artifact-alignment-plan    ← the cursor's row
//	  config-key-audit
//	  ────                       ← the inline preview's divider (see panel_rows.go)
//
//	Keyboard: ↑/↓ Navigate  p preview  …
//
//	(enter on a row opens the viewer)
//
//	artifact-alignment-plan      ← the viewer: the file's name as its title
//	>   1  # Artifact alignment plan
//	    2
//	    3  The chain is broken in four places…
//	  (1–20 of 20 lines)
//
//	Keyboard: ↑/↓ scroll  esc close
//
// # Where the facts live
//
// The artifacts are the host's (`artifacts.List(conversationID)`, the same
// source the checkpoint's resume block reads), so they arrive as an argument
// and the panel reads them at draw time — a list that changed while the panel
// was open is never stale. Everything else is the panel's own: the cursor, the
// preview toggle, whether the viewer is open and its scroll window.
//
// The panel keeps no cache of the file bodies: a viewer re-reads its file on
// every draw, so an artifact rewritten by the agent while it is open shows its
// new text.

package artifactpanel

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/palette"
)

// previewRows is how many lines of the selected artifact the inline preview
// shows.
//
// 【一手】 the reference's `p` "opens a 12-line truncated and indented code
// block preview directly under the selected row"
// (official-docs/cli/artifacts.md, "Interaction keybindings").
const previewRows = 12

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as if
// the seam were answered with "nothing happened", which keeps the package
// usable from a test without a running TUI.
type Seams struct {
	// Refs returns the conversation's artifacts, in list order.
	//
	// It is read on every draw, so the panel always shows the current
	// conversation rather than the one it opened on.
	Refs func() []Artifact

	// Size returns the terminal's size.
	//
	// The width lays out the viewer's gutter and the height sizes its
	// window.
	Size func() (width, height int)

	// Read returns an artifact's text.
	//
	// The second value is false when the file cannot be read — the panel
	// then shows the reason instead of an empty document.
	Read func(path string) (string, bool)

	// OpenInEditor hands an artifact to the user's editor.
	//
	// The panel does not know which editor is configured, so the host
	// resolves it and returns its command.
	OpenInEditor func(path string) tea.Cmd

	// Close leaves the panel.
	Close func() tea.Cmd
}

// Artifact is one entry of the picker: the name the row shows and the path
// the viewer reads.
type Artifact struct {
	// Name is the artifact's stem, as artifacts.List reports it — the label
	// the checkpoint's resume block prints.
	Name string
	// Path is the file's absolute path.
	Path string
}

// FileName is the file's base name, extension included.
//
// 【一手】 the reference's picker and viewer title print this form
// ("artifact-alignment-plan.md"), while its checkpoint prints the stem. The
// path is the only place the extension survives, so it is read back from there.
func (a Artifact) FileName() string {
	if a.Path == "" {
		return ""
	}
	if i := strings.LastIndexByte(a.Path, '/'); i >= 0 && i+1 < len(a.Path) {
		return a.Path[i+1:]
	}
	return a.Path
}

// Panel is the /artifact panel.
//
// The panel owns the whole surface: the list, its cursor, the preview toggle,
// the viewer and the viewer's scroll window. Nothing of it is left in the
// host but the seams.
type Panel struct {
	seams Seams

	// cursor is the selected row's index in the list.
	cursor int

	// preview says the selected row's inline preview is open.
	preview bool

	// viewer says the detail viewer is open.
	viewer bool

	// window is the viewer's scroll offset, in body rows.
	window int

	// line is the viewer's line cursor, 0-based in the document's rows.
	//
	// 【一手】 the reference's viewer has its own cursor — the `>` in the
	// gutter — and `j`/`k` move *it*, not the window. The capture after
	// `j j j` shows `L4` in the counter with the range still `1-28`, so the
	// two are separate state. The window follows the cursor only when the
	// cursor would leave it.
	line int
}

// New builds the /artifact panel.
//
// The panel opens with the first row under the cursor.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside ------------------------------

// Cursor is the selected row's index in the list.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index, clamped into the
// rows the panel has.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	n := p.RowCount()
	if idx < 0 {
		idx = 0
	}
	if n > 0 && idx >= n {
		idx = n - 1
	}
	p.cursor = idx
}

// Viewer reports whether the detail viewer is open.
//
// The host asks: a panel with an open viewer takes the full terminal
// viewport, the way /tasks' detail view and /changelog do.
func (p *Panel) Viewer() bool {
	if p == nil {
		return false
	}
	return p.viewer
}

// Previewing reports whether the selected row's inline preview is open.
func (p *Panel) Previewing() bool {
	if p == nil {
		return false
	}
	return p.preview
}

// RowCount is how many rows the picker has.
//
// The count is the artifact count, so a stale cursor cannot be stranded past
// the last row when the list shrinks.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	return len(p.refs())
}

// Selected returns the artifact under the cursor.
//
// A cursor that names no row returns false: an empty list has nothing to
// select.
func (p *Panel) Selected() (Artifact, bool) {
	if p == nil {
		return Artifact{}, false
	}
	refs := p.refs()
	if p.cursor < 0 || p.cursor >= len(refs) {
		return Artifact{}, false
	}
	return refs[p.cursor], true
}

// ---- the panel's rows --------------------------------------------------

// Lines renders the panel.
//
// The viewer, when open, replaces the whole surface — the same rule /tasks'
// detail view and /changelog follow.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.viewer {
		return p.viewerLines()
	}
	return p.listLines()
}

// listLines renders the picker: the title, one row per artifact, the selected
// row's inline preview when it is open, a blank and the legend.
//
// The empty frame is Lines, the reference's own capture, so a conversation
// with no artifacts shows exactly what agy shows.
func (p *Panel) listLines() []string {
	refs := p.refs()
	if len(refs) == 0 {
		return Lines()
	}
	lines := make([]string, 0, len(refs)+8)
	lines = append(lines, title())
	for i, ref := range refs {
		lines = append(lines, row(ref, i == p.cursor))
		if i == p.cursor && p.preview {
			lines = append(lines, p.previewLines(ref)...)
		}
	}
	lines = append(lines, "", legend())
	return lines
}

// previewLines renders the inline preview under the selected row.
//
// 【一手】 measured from a live capture of agy 1.5.17 (`p` on the picker):
//
//	› artifact-alignment-plan.md   open
//	    │ # pigasus `/artifact` … 修复与对齐方案
//	    │
//	    │ > 结论先行：这不是"文档没生成"，而是 **artifact 链路在 pigasus 里断了 4 处**。即使我把文件完美写进 ar…
//	    ...
//	      ... 381 more lines
//	  artifact-picker-and-viewer.md
//
// so: twelve content rows, each prefixed by four spaces and a dim `│` and one
// more space; each row cut at 74 columns with an `…` when it overflows; and,
// when the file has more, a final gutter-less `      ... N more lines`
// (six spaces). N counts the rows *not* shown.
func (p *Panel) previewLines(ref Artifact) []string {
	const (
		bar   = "    │ "
		plain = "      "
	)
	out := make([]string, 0, previewRows+2)
	body, ok := p.read(ref.Path)
	if !ok {
		out = append(out, bar+palette.DimGrayOpen+"(cannot read this artifact)"+palette.DimGrayClose)
		return out
	}
	rows := strings.Split(strings.TrimRight(body, "\n"), "\n")
	shown := rows
	if len(shown) > previewRows {
		shown = shown[:previewRows]
	}
	for _, r := range shown {
		out = append(out, bar+palette.DimGrayOpen+clipPreviewRow(r)+palette.DimGrayClose)
	}
	if rest := len(rows) - len(shown); rest > 0 {
		out = append(out, plain+palette.DimGrayOpen+
			"... "+strconv.Itoa(rest)+" more lines"+palette.DimGrayClose)
	}
	return out
}

// previewRowWidth is the column at which the preview cuts a row.
//
// 【一手】 the capture's truncated row is 74 cells wide including the six-cell
// gutter prefix, so the text is clipped at 68 and the ellipsis takes the 74th.
const previewRowWidth = 68

// clipPreviewRow cuts a row to the preview's width, ending it with `…` when it
// was cut.
//
// The cut is by rune, not by byte: the capture's own truncated row is full of
// CJK, and a byte cut would split a character.
func clipPreviewRow(row string) string {
	rs := []rune(row)
	if len(rs) <= previewRowWidth {
		return row
	}
	return string(rs[:previewRowWidth]) + "…"
}

// ---- the seams ---------------------------------------------------------

// refs returns the conversation's artifacts.
func (p *Panel) refs() []Artifact {
	if p == nil || p.seams.Refs == nil {
		return nil
	}
	return p.seams.Refs()
}

// read returns an artifact's text.
func (p *Panel) read(path string) (string, bool) {
	if p == nil || p.seams.Read == nil {
		return "", false
	}
	return p.seams.Read(path)
}

// size returns the terminal's size.
func (p *Panel) size() (width, height int) {
	if p == nil || p.seams.Size == nil {
		return 0, 0
	}
	return p.seams.Size()
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// SelectionGutter is how many leading cells of each viewer row are the
// gutter rather than the document, or 0 when the viewer is closed.
//
// The host asks this when it copies a drag: the line number is the
// viewer's furniture, so a selection that began in the gutter must not
// paste it.
func (p *Panel) SelectionGutter() int {
	if p == nil || !p.viewer {
		return 0
	}
	return tuiviewer.SelectionIndent(p.viewerSpec().Total)
}

// viewerSpec is the viewer's document.
//
// The document is the artifact's text, one row per line, with the line
// numbers in the gutter. The spec is what the shared window arithmetic
// (tuiviewer.Window) scrolls, so the viewer scrolls exactly like /changelog.
func (p *Panel) viewerSpec() tuiviewer.Spec {
	_, height := p.size()
	ref, ok := p.Selected()
	if !ok {
		return tuiviewer.Spec{}
	}
	body, readable := p.read(ref.Path)
	rows := documentRows(body, readable)
	return tuiviewer.Spec{
		Lines:   rows,
		Visible: p.viewerVisibleRows(height),
		Total:   len(rows),
	}
}

// viewerVisibleRows is how many body rows the viewer shows.
//
// 【一手】 the reference's geometry, measured from the capture at height 34:
// the body ran 28 rows, and the chrome took the other six — the title row, the
// counter row, a blank, two footer rows and the status line. The body is
// therefore height-6, not the height-4 the generic viewer panels use; those
// have a one-row footer and no title.
func (p *Panel) viewerVisibleRows(height int) int {
	if height <= 0 {
		return viewerFallbackRows
	}
	vis := height - 6
	if vis < 1 {
		vis = 1
	}
	return vis
}

// viewerFallbackRows is how many body rows the viewer shows when the terminal
// height is unknown (the unit-test path). It is the same fallback /tasks'
// detail view uses.
const viewerFallbackRows = 17

// navLegend is the viewer's footer.
//
// 【一手】 the reference's viewer footer names its own keys, in this order:
// "↑/↓ scroll · pgup/pgdown page · shift+g bottom · g top … esc close"
// (official-docs/cli/artifacts.md, the detail-viewer capture). The clone
// implements the scrolling and the way out; the review keys (`c` comment,
// `m` mermaid, `l` gutter) are not implemented, so they are not named — see
// the package comment.

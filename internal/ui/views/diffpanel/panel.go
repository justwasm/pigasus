// Package diffpanel holds the /diff panel: the tabbed working-copy
// viewer with its inline diff subview.
//
// # What it is
//
// REF 1.4.0 detects the workspace's VCS. In a git tree /diff's All
// Changes tab shows a header row, a "N file(s) changed" count, the
// tracked files (path-sorted, each with its +added/-deleted counts),
// a blank, the untracked files and a legend. The other two tabs are
// Per Turn (one row per user turn, cycle 226) and Commit Tree (the
// working-copy row plus the repository's commits).
//
// Pressing Enter on a file row opens the inline diff subview: the
// file's `git diff` body, styled line by line, with the hunk folding
// the reference added in 1.3.5 (AGY 1.3.5 changelog line 4: "hunk
// headers can be collapsed with the f key").
//
// The no-VCS shape — the warning row and the "Diff" tab row without a
// VCS name — is the reference's too: it is what /diff shows in a
// directory that is not a repository.
//
// # Where it lives
//
// The panel is here: the three tabs, the cursor, the visible window,
// the inline subview, its row cursor, the per-hunk fold state, the
// keys and the rows.
//
// The git rules — the working-copy change list, the diff body, the
// tab row, the row renderers and the hunk primitives — live in
// internal/ui/views/diff (the action and fold packages). The panel
// drives them; it does not reimplement them.
//
// The host keeps only the seams: the workspace, the terminal's size,
// the conversation's turn count and the way out of the panel. The
// panel is rebuilt every time it opens, so a stale cursor, a stale
// tab or a stale fold state never survives a close.
package diffpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/linecomments"
	"github.com/justwasm/pigasus/palette"
)

// Change is one working-copy file with its line counts.
//
// The model itself lives in internal/ui/views/diff/action; the alias
// keeps the panel's own vocabulary.
type Change = diffaction.Change

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are re-read whenever the scheme changes.
const (
	// blueClose is the default-foreground close the panel's blue spans
	// end with.
	blueClose = palette.BlueClose
	// dimGrayClose is the default-foreground close the panel's dim
	// grey spans end with.
	dimGrayClose = palette.DimGrayClose
	// gray238Close is the default-foreground close the muted
	// description spans end with.
	gray238Close = "\x1b[39m"
)

var (
	blueOpen          = palette.BlueOpen
	dimGrayOpen       = palette.DimGrayOpen
	gray238           = palette.SubtleOpen
	toolBulletErrOpen = palette.ToolBulletErrOpen
)

func init() {
	palette.OnSchemeChange(refreshColours)
}

// refreshColours re-reads the palette after the selected colour scheme
// changed.
func refreshColours() {
	blueOpen = palette.BlueOpen
	dimGrayOpen = palette.DimGrayOpen
	gray238 = palette.SubtleOpen
	toolBulletErrOpen = palette.ToolBulletErrOpen
}

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps
// the package usable from a test without a running TUI.
type Seams struct {
	// Cwd returns the workspace the working copy is read from.
	Cwd func() string

	// Size returns the terminal's width and height in cells.
	//
	// The height sizes the visible window; the width lays the inline
	// diff's rows out.
	Size func() (width, height int)

	// Turns returns the number of user-role turns in the conversation.
	//
	// The Per Turn tab renders one row per turn, so the count is the
	// conversation's, not the panel's.
	Turns func() int

	// VimEnabled reports whether vim modal editing is active in the CLI.
	VimEnabled func() bool

	// Changes overrides the local working-copy scan. A remote session
	// returns the proxy's turn diff instead of reading the local git
	// tree; ok=false falls back to the local scan.
	Changes func() (tracked, untracked []Change, ok bool)

	// Body overrides the local inline diff body. ok=false falls back to
	// git.
	Body func(target, kind string) (lines []string, ok bool)

	// VCS names the version-control source; empty falls back to the
	// local detection.
	VCS func() string

	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /diff panel.
//
// The panel owns the whole surface: the three tabs, the cursor, the
// visible window, the inline subview, its row cursor and the per-hunk
// fold state. Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// tab is the active tab: 0 = All Changes, 1 = Per Turn,
	// 2 = Commit Tree.
	tab int

	// cursor is the selected row of the active tab's row list.
	//
	// The mapping depends on the tab:
	//   - All Changes (0): the index into tracked+untracked.
	//   - Per Turn (1):    the index into the turns list.
	//   - Commit Tree (2): the index into the working-copy row (0)
	//     plus the N commits (1..N).
	cursor int

	// window is the index of the first visible row of the active tab.
	window int

	// view is the subview: 0 = the file list, 1 = the inline diff of
	// a working-copy file, 2 = the inline diff of a commit.
	view int

	// viewPath is the file path (view 1) or the commit hash (view 2)
	// the inline subview shows.
	viewPath string

	// inlineRow indexes the body lines of the inline subview (the
	// styled @@, +/- and context rows the renderer emits below the
	// title).
	inlineRow int

	// folded is the per-hunk fold state of the inline diff, keyed by
	// the hunk's index in document order. A true entry collapses the
	// hunk's body into the @@ header plus a dim grey "+N" indicator.
	folded map[int]bool

	// commentEditor is the line-comment editor for the diff viewer.
	commentEditor *linecomments.Editor

	// comments maps a file path to its line comments store.
	comments map[string]*linecomments.Store
}

// New builds the /diff panel.
//
// The panel opens on the All Changes tab with the cursor on the first
// row — the way the reference opens it.
func New(seams Seams) *Panel {
	vimOn := false
	if seams.VimEnabled != nil {
		vimOn = seams.VimEnabled()
	}
	return &Panel{
		seams:         seams,
		commentEditor: linecomments.NewEditor(vimOn),
		comments:      map[string]*linecomments.Store{},
	}
}

// ---- the panel's state, read from outside ------------------------------

// Tab is the active tab: 0 = All Changes, 1 = Per Turn, 2 = Commit
// Tree.
func (p *Panel) Tab() int {
	if p == nil {
		return 0
	}
	return p.tab
}

// SetTab switches the active tab.
//
// The tab is clamped into the three the reference offers.
func (p *Panel) SetTab(tab int) {
	if p == nil {
		return
	}
	if tab < 0 {
		tab = 0
	}
	if tab > len(diffaction.Views)-1 {
		tab = len(diffaction.Views) - 1
	}
	p.tab = tab
}

// Cursor is the selected row of the active tab.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the given row.
//
// The index is clamped at zero; the row list's own bounds are applied
// by the navigation, which knows how many rows the tab has.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	if idx < 0 {
		idx = 0
	}
	p.cursor = idx
}

// View is the active subview: 0 = the file list, 1 = the inline diff
// of a working-copy file, 2 = the inline diff of a commit.
func (p *Panel) View() int {
	if p == nil {
		return 0
	}
	return p.view
}

// ViewPath is the file path or the commit hash the inline subview
// shows.
func (p *Panel) ViewPath() string {
	if p == nil {
		return ""
	}
	return p.viewPath
}

// InlineRow is the cursor's row in the inline subview's body.
func (p *Panel) InlineRow() int {
	if p == nil {
		return 0
	}
	return p.inlineRow
}

// SetInlineRow puts the inline subview's cursor on the given body
// row.
func (p *Panel) SetInlineRow(row int) {
	if p == nil {
		return
	}
	p.inlineRow = row
}

// Folded is the inline subview's per-hunk fold state.
func (p *Panel) Folded() map[int]bool {
	if p == nil {
		return nil
	}
	return p.folded
}

// ---- the seams ---------------------------------------------------------

// cwd is the workspace the working copy is read from.
func (p *Panel) cwd() string {
	if p == nil || p.seams.Cwd == nil {
		return ""
	}
	return p.seams.Cwd()
}

// size is the terminal's size in cells.
func (p *Panel) size() (int, int) {
	if p == nil || p.seams.Size == nil {
		return 0, 0
	}
	return p.seams.Size()
}

// width is the terminal's width in cells.
func (p *Panel) width() int {
	w, _ := p.size()
	return w
}

// height is the terminal's height in cells.
func (p *Panel) height() int {
	_, h := p.size()
	return h
}

// turns is the number of user-role turns in the conversation.
func (p *Panel) turns() int {
	if p == nil || p.seams.Turns == nil {
		return 0
	}
	return p.seams.Turns()
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// ---- the working copy --------------------------------------------------

// changes returns the tracked then untracked changes of the git
// working copy rooted (anywhere) at the workspace.
//
// It returns ok=false when git is unavailable or the workspace is not
// a repo.
func (p *Panel) changes() (tracked, untracked []Change, ok bool) {
	if p.seams.Changes != nil {
		if t, u, override := p.seams.Changes(); override {
			return t, u, true
		}
	}
	return diffaction.GitChanges(p.cwd())
}

// vcsName is the version-control source the header names.
func (p *Panel) vcsName() string {
	if p.seams.VCS != nil {
		if name := p.seams.VCS(); name != "" {
			return name
		}
	}
	return diffaction.DetectVCS(p.cwd())
}

// commitLog returns the repository's commit log in the reference's own
// format: one line per commit, "<short hash>\t<subject>", newest
// first.
//
// The empty string is returned when git is unavailable or the command
// fails.
func (p *Panel) commitLog() string {
	log, err := diffaction.RunGit(p.cwd(), "log", "--pretty=%h%x09%s")
	if err != nil {
		return ""
	}
	return log
}

// commits returns the repository's commits, newest first, as a
// (hash, subject) pair each.
//
// The log line is the reference's own: `git log --pretty=%h%x09%s`.
func (p *Panel) commits() [][2]string {
	// A remote workspace's history is not local; the panel must not show
	// the client's own repository.
	if p.vcsName() == "remote" {
		return nil
	}
	log := p.commitLog()
	if log == "" {
		return nil
	}
	var out [][2]string
	for _, line := range strings.Split(log, "\n") {
		if fields := strings.SplitN(line, "\t", 2); len(fields) == 2 {
			out = append(out, [2]string{fields[0], fields[1]})
		}
	}
	return out
}

// body returns the diff body for the given target: the working-copy
// file's diff ("file") or the commit's diff ("commit").
func (p *Panel) body(target, kind string) []string {
	if p.seams.Body != nil {
		if lines, override := p.seams.Body(target, kind); override {
			return lines
		}
	}
	return diffaction.GitDiffBody(p.cwd(), target, kind)
}

// firstChangedPath is the path of the first change in the working
// copy.
func firstChangedPath(tracked, untracked []Change) string {
	var t, u []string
	for _, c := range tracked {
		t = append(t, c.Path)
	}
	for _, c := range untracked {
		u = append(u, c.Path)
	}
	return diffaction.FirstChangedPath(t, u)
}

// ---- the visible window ------------------------------------------------

// visibleRowsFor returns the number of visible rows in the /diff panel
// based on the height.
//
// The panel spends six rows on the frame (the tab row, the count row,
// a blank, the legend and the rows below the status line), and never
// shows fewer than three.
func visibleRowsFor(height int) int {
	if height <= 0 {
		return 0
	}
	vis := height - 6
	if vis < 3 {
		return 3
	}
	return vis
}

// adjustWindow keeps the visible window around the cursor.
func (p *Panel) adjustWindow(cursor, visible, total int) {
	if p == nil {
		return
	}
	nav := tuiviewport.ListNav{Cursor: cursor, Window: p.window}
	nav.Sync(total, visible)
	p.window = nav.Window
}

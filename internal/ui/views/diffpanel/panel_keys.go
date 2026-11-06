// panel_keys.go — the /diff panel's keys.
//
// The panel owns every key it receives: the arrows move its own cursor
// and switch its own tabs, Enter opens the inline diff subview, `f`
// folds the hunk under the cursor and Esc leaves. A key the panel does
// not own is reported as unhandled, so the host's global handlers
// (Ctrl+C) still see it.

package diffpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/linecomments"
)

// HandleKey routes a key to the panel.
//
// The panel owns the navigation: ←/→ switch the tab, ↑/↓ move the
// cursor over the active tab's rows, Enter opens the inline diff
// subview, `f` folds the hunk under the cursor and Esc leaves the
// subview or the panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.commentEditor != nil && p.commentEditor.Active {
		switch key.Code {
		case tea.KeyEnter:
			p.saveComment()
			return true, nil
		case tea.KeyEsc:
			p.commentEditor.Close()
			return true, nil
		default:
			p.commentEditor.Buffer.HandleKey(key)
			return true, nil
		}
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavLeft:
		if p.view == 0 && p.tab > 0 {
			p.tab--
			p.cursor = 0
		}
		return true, nil
	case tuilib.NavRight:
		if p.view == 0 && p.tab < len(diffaction.Views)-1 {
			p.tab++
			p.cursor = 0
		}
		return true, nil
	case tuilib.NavUp:
		p.navStep(-1)
		return true, nil
	case tuilib.NavDown:
		p.navStep(1)
		return true, nil
	case tuilib.NavPageUp:
		p.navStep(-10)
		return true, nil
	case tuilib.NavPageDown:
		p.navStep(10)
		return true, nil
	case tuilib.NavTop:
		p.navStep(-999999)
		return true, nil
	case tuilib.NavBottom:
		p.navStep(999999)
		return true, nil
	case tuilib.NavSelect:
		if p.view == 0 {
			p.enter()
		}
		return true, nil
	case tuilib.NavCancel:
		if p.view != 0 {
			p.closeInline()
			return true, nil
		}
		if p.seams.Close != nil {
			return true, p.seams.Close()
		}
		return true, func() tea.Msg { return MsgDismissed{} }
	}
	// `f` folds the hunk under the cursor; Ctrl+O is the host's
	// shortcut for the same gesture (the host intercepts it before
	// the key reaches the panel).
	if (key.Mod == 0 && key.Code == 'f') || (key.Code == 'o' && key.Mod.Contains(tea.ModCtrl)) {
		if p.view != 0 {
			p.ToggleFoldAtCursor()
		}
		return true, nil
	}
	// `c` adds/edits a comment on the current inline diff line.
	if key.Mod == 0 && key.Code == 'c' && p.view != 0 {
		p.openComment()
		return true, nil
	}
	if key.Mod == 0 {
		// Any other unmodified key is swallowed: the panel has no text
		// input, and the reference's /diff does not react to letters.
		return true, nil
	}
	return false, nil
}

// CommentEditor returns the active comment editor, if open.
func (p *Panel) CommentEditor() *linecomments.Editor {
	if p == nil {
		return nil
	}
	return p.commentEditor
}

func (p *Panel) storeFor(path string) *linecomments.Store {
	if p.comments == nil {
		p.comments = map[string]*linecomments.Store{}
	}
	s, ok := p.comments[path]
	if !ok {
		s = linecomments.NewStore()
		p.comments[path] = s
	}
	return s
}

func (p *Panel) openComment() {
	if p.commentEditor == nil || p.viewPath == "" {
		return
	}
	initialText := ""
	s := p.storeFor(p.viewPath)
	if c, ok := s.At(p.inlineRow); ok {
		initialText = c.Body
	}
	p.commentEditor.Open(p.inlineRow, initialText)
}

func (p *Panel) saveComment() {
	if p.commentEditor == nil || !p.commentEditor.Active || p.viewPath == "" {
		return
	}
	body := strings.TrimSpace(p.commentEditor.Buffer.Value())
	line := p.commentEditor.Line
	p.commentEditor.Close()
	if body == "" {
		return
	}
	s := p.storeFor(p.viewPath)
	s.Add(line, body)
}

// navStep moves the cursor by delta rows.
//
// The inline subview owns its own scroll, so the cursor is inert
// there: the step moves the inline body's row cursor instead.
func (p *Panel) navStep(delta int) {
	if p.view != 0 {
		p.moveInlineCursor(delta)
		return
	}
	p.moveCursor(delta)
}

// moveCursor moves the cursor over the active tab's row list.
//
// The bound depends on the tab:
//
//   - All Changes (0): the index into tracked+untracked.
//   - Per Turn (1):    the index into the turns list.
//   - Commit Tree (2): the index into the working-copy row (0) plus
//     the N commits (1..N).
//
// Cycle 207 retained the no-VCS short-circuit; this still holds.
func (p *Panel) moveCursor(delta int) {
	if p == nil {
		return
	}
	if p.cwd() == "" || p.view != 0 {
		return
	}
	var hi int
	switch p.tab {
	case 0: // All Changes
		tracked, untracked, ok := p.changes()
		if !ok {
			return
		}
		hi = len(tracked) + len(untracked) - 1
	case 1: // Per Turn
		turns := p.turns()
		tracked, untracked, ok := p.changes()
		if !ok || turns == 0 || len(tracked)+len(untracked) == 0 {
			return
		}
		hi = turns - 1
	case 2: // Commit Tree
		log := p.commitLog()
		if log == "" {
			return
		}
		// log has one line per commit (no trailing newline); the
		// cursor indexes the working-copy row (0) plus the N commits
		// (1..N), so hi = N.
		hi = strings.Count(log, "\n") + 1
	default:
		return
	}
	if hi < 0 {
		return
	}
	nav := tuiviewport.ListNav{Cursor: p.cursor, Window: p.window}
	if delta < 0 {
		nav.MoveUp(-delta, hi+1, visibleRowsFor(p.height()))
	} else {
		nav.MoveDown(delta, hi+1, visibleRowsFor(p.height()))
	}
	p.cursor, p.window = nav.Cursor, nav.Window
}

// enter is the binding `enter` triggers on All Changes / Commit Tree.
//
// On Per Turn the binding is inert (the placeholder has no rows to
// drill into; the cycle-226 row-list mode is also inert because the
// per-turn model has no per-file body to show). When the cursor points
// at a valid row the subview is opened; when it does not (empty list)
// the binding is a no-op.
func (p *Panel) enter() {
	switch p.tab {
	case 0: // All Changes
		tracked, untracked, ok := p.changes()
		if !ok {
			return
		}
		all := append(append([]Change{}, tracked...), untracked...)
		if p.cursor < 0 || p.cursor >= len(all) {
			return
		}
		p.openInline(1, all[p.cursor].Path)
	case 2: // Commit Tree
		commits := p.commits()
		// Cursor: 0 = working-copy row (the "▌ *" line at index 3),
		// 1..N = the N commits. Skip 0 (working-copy has no per-commit
		// diff hash to drill into), so commit i is at cursor==i.
		idx := p.cursor - 1
		if idx < 0 || idx >= len(commits) {
			return
		}
		p.openInline(2, commits[idx][0])
	}
}

// openInline opens the inline-diff subview.
//
// The cycle-226 substitute is plain text (not REF's side-by-side
// viewer); see inlineLines for the wiring.
//
// Cycle diff-folding (AGY 1.3.5): also resets the inline row cursor
// and the fold state so each new file's diff starts unfolded at the
// top. Without this reset the cursor and fold state would carry over
// from the previous diff (e.g. scroll position from one file would
// land you in the middle of another).
func (p *Panel) openInline(kind int, target string) {
	if p == nil {
		return
	}
	p.view = kind
	p.viewPath = target
	p.inlineRow = 0
	p.folded = nil
}

// closeInline returns to the file-list view.
//
// Cycle 226 binds Esc on the inline subview to this; the panel itself
// stays open.
//
// Cycle diff-folding (AGY 1.3.5): resets the inline row cursor and
// clears the fold state so the next Enter opens a fresh, unfolded diff
// at the top row. (Esc is treated as "leave this diff" rather than
// "minimise this diff" — the reference's side-by-side viewer collapses
// to a re-open on Esc, but Esc-then-Enter in the clone opens a new
// view.)
//
// The list-view cursor is the row into the file/commit list and is
// left alone so Esc returns to the same row the user pressed Enter on.
func (p *Panel) closeInline() {
	if p == nil {
		return
	}
	p.view = 0
	p.viewPath = ""
	p.inlineRow = 0
	p.folded = nil
}

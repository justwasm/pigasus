// Package adddirpanel holds the /add-dir panel: the path input that
// adds a directory to the session's workspace.
//
// # What it is
//
// The panel lists the trusted workspaces the session already knows
// about and takes the path of one more. Enter commits it; Esc leaves.
//
// # Where it lives
//
// The panel itself is here: the path buffer, the trusted-workspace
// list, the rows and the keys. The host keeps only the seams — where
// the trusted list comes from, what to do with a submitted path and
// the way out of the panel.
//
// The panel is rebuilt every time it opens, so a stale buffer never
// survives a close.
package adddirpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Trusted returns the workspaces the session already trusts.
	//
	// The list is read when the panel draws, not when it opens: the
	// user may add a workspace in another session in between.
	Trusted func() []string
	// Submit commits the typed path and leaves the panel.
	//
	// The host owns the transcript block and the trust store: the
	// panel does not own them.
	Submit func(path string) tea.Cmd
	// Close leaves the panel without committing.
	Close func() tea.Cmd
}

// Panel is the /add-dir panel.
type Panel struct {
	seams Seams

	// text is the typed path.
	text string
}

// New builds the /add-dir panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside --------------------------------

// Text is the path the user typed.
func (p *Panel) Text() string {
	if p == nil {
		return ""
	}
	return p.text
}

// SetText replaces the typed path.
func (p *Panel) SetText(s string) {
	if p == nil {
		return
	}
	p.text = s
}

// ---- the seams ------------------------------------------------------------

// trusted is the list of workspaces the session already trusts.
func (p *Panel) trusted() []string {
	if p == nil || p.seams.Trusted == nil {
		return nil
	}
	return p.seams.Trusted()
}

// submit hands the typed path to the host.
func (p *Panel) submit() tea.Cmd {
	if p == nil || p.seams.Submit == nil {
		return nil
	}
	return p.seams.Submit(p.text)
}

// close leaves the panel without committing.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// ---- the keys -------------------------------------------------------------

// HandleKey owns the keys the panel receives.
//
// Esc leaves the panel, Enter commits the typed path, Backspace pops
// the last rune, and a printable key appends to the buffer.
//
// A key the panel does not own — a modified key that is not typed
// text — is *not* handled, so the host's global handlers still see it.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch {
	case key.Code == tea.KeyEsc:
		return true, p.close()
	case key.Code == tea.KeyEnter:
		return true, p.submit()
	case key.Code == tea.KeyBackspace:
		if r := []rune(p.text); len(r) > 0 {
			p.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if keybind.IsTypedText(key) {
		p.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		// An unmodified key the panel does not use: swallow it, so it
		// never reaches the prompt below.
		return true, nil
	}
	return false, nil
}

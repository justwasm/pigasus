// Package renamepanel holds the /rename panel: the single-line input
// that renames the current conversation.
//
// # What it is
//
// The clone's `/rename` takes a name on the command line
// (`/rename Refactor auth`). The bare form (`/rename`, or the F2 key)
// opens a panel with a single-line input instead, so the name can be
// typed and corrected in place.
//
// The input is charm.land/bubbles/v2/textinput — the same widget the
// /pool tab's free-text rows use. It is the reason the panel owns
// arrow-key navigation, Home/End, Ctrl+W and the rest: the earlier
// hand-rolled rune buffer dropped every one of them.
//
// # Where it lives
//
// The panel itself is here: the input, the prompt, the placeholder, the
// rows and the keys. The host keeps only the seams — the current
// conversation name, what to do with a submitted name and the way out
// of the panel.
//
// The panel is rebuilt every time it opens, so a stale seed or a stale
// cursor never survives a close.
package renamepanel

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/justwasm/pigasus/palette"
)

// placeholder is the grey "type a name…" line shown when the user has
// not typed anything yet.
//
// The colour is applied through the textinput's Placeholder style
// rather than being embedded in the string: bubbles' placeholderView
// slices the placeholder *runes* by its lipgloss visible width, so an
// inline SGR (20 runes) ate into the text and truncated it to
// "type a na".
const placeholder = "type a name and press enter…"

// limit caps the typed-name length at 200 runes. The conversation
// store's title column is sized to fit a UTF-8 name; 200 is generous
// and matches the /permissions keybinding panel's max-width for typed
// sequences.
const limit = 200

// inputWidth is the visible-width budget the bubbles textinput is told
// to render into.
//
// The textinput package's placeholderView() only renders more than the
// first character of the placeholder when Width() > 0 (the
// `if m.Width() < 1 && len(p) <= 1 { return ... }` shortcut in
// bubbles/v2 textinput.go:756 — verified against the v2.1.1 source).
//
// 60 cells matches the recovered reference's panel-row width: REF's
// /rename panel, observed at 220×50, fills its input row to ~60 cells
// before the trailing space and the reverse-video cursor.
const inputWidth = 60

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Seed returns the current conversation name — the input's initial
	// value.
	//
	// The panel reads it once, when it is built. A conversation with
	// no name opens an empty input, and the placeholder is visible.
	Seed func() string
	// Submit commits the typed name and leaves the panel.
	//
	// The host owns the transcript block and the conversation store:
	// the panel does not own them.
	Submit func(name string) tea.Cmd
	// Close leaves the panel without committing.
	Close func() tea.Cmd
}

// Panel is the /rename panel.
type Panel struct {
	seams Seams

	// input is the bubbles textinput. It is value-typed — Update()
	// returns a new model and the panel assigns it back.
	input textinput.Model
}

// New builds the /rename panel.
//
// The input is primed the way the reference's own panel is: the
// "Rename:" prompt in front, the placeholder visible when the field is
// empty, focused so the first keystroke edits the field rather than
// opening the prompt, and the current conversation name as its value.
func New(seams Seams) *Panel {
	in := textinput.New()
	// The prompt's colour is the selected scheme's accent, so it is
	// read when the panel is built rather than baked into a constant.
	in.Prompt = "\x1b[1m" + palette.BlueOpen + "Rename:" + "\x1b[0m "
	in.Placeholder = placeholder
	styles := in.Styles()
	// Solarized base00 #657B83 — the same dim grey the reference uses
	// for secondary text.
	placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#657B83"))
	styles.Focused.Placeholder = placeholderStyle
	styles.Blurred.Placeholder = placeholderStyle
	in.SetStyles(styles)
	in.CharLimit = limit
	in.SetWidth(inputWidth)
	in.Focus()
	if seams.Seed != nil {
		in.SetValue(seams.Seed())
	}
	return &Panel{seams: seams, input: in}
}

// ---- the panel's state, read from outside --------------------------------

// Value is the name the user typed.
func (p *Panel) Value() string {
	if p == nil {
		return ""
	}
	return p.input.Value()
}

// SetValue replaces the typed name and puts the cursor at its end.
//
// The cursor's end is the bubbles textinput's own behaviour on
// SetValue: the reference's own field opens with the cursor behind the
// seed.
func (p *Panel) SetValue(s string) {
	if p == nil {
		return
	}
	p.input.SetValue(s)
}

// Position is the cursor's offset into the value, in runes.
func (p *Panel) Position() int {
	if p == nil {
		return 0
	}
	return p.input.Position()
}

// Focused reports whether the input takes the keystrokes.
func (p *Panel) Focused() bool {
	if p == nil {
		return false
	}
	return p.input.Focused()
}

// ---- the seams ------------------------------------------------------------

// submit hands the typed name to the host.
func (p *Panel) submit() tea.Cmd {
	if p == nil {
		return nil
	}
	if p.seams.Submit != nil {
		return p.seams.Submit(p.input.Value())
	}
	return RenameCmd(p.input.Value())
}

// close leaves the panel without committing.
func (p *Panel) close() tea.Cmd {
	if p == nil {
		return nil
	}
	if p.seams.Close != nil {
		return p.seams.Close()
	}
	return DismissCmd()
}

// ---- the input ------------------------------------------------------------

// Focus focuses the input and returns the command that arms the
// cursor blink.
//
// The command is the textinput's own `Blink`; the host sends it to the
// running program. It is best-effort: there is no program in a test.
func (p *Panel) Focus() tea.Cmd {
	if p == nil {
		return nil
	}
	return p.input.Focus()
}

// HandleKey owns every key the panel receives.
//
// Esc leaves the panel, Enter commits the typed name, and everything
// else goes through the bubbles textinput — that is the migration's
// primary UX win: arrow keys, Home/End, Ctrl+W and Ctrl+U all work.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, p.close()
	case tea.KeyEnter:
		return true, p.submit()
	}
	updated, cmd := p.input.Update(tea.KeyPressMsg{
		Code: key.Code,
		Mod:  key.Mod,
		Text: key.Text,
	})
	p.input = updated
	return true, cmd
}

// HandlePaste inserts a pasted string into the input.
//
// It never submits: a pasted name is edited like any other text.
func (p *Panel) HandlePaste(s string) tea.Cmd {
	if p == nil || s == "" {
		return nil
	}
	updated, cmd := p.input.Update(tea.PasteMsg{Content: s})
	p.input = updated
	return cmd
}

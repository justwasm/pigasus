// Package onboarding implements the v1.3.5 1:1 onboarding wizard that
// the recovered antigravity binary runs on first launch (and on
// /settings → Onboarding from the TUI).
package onboarding

import (
	"fmt"
	"io"
)

// Step identifies the current position in the 3-step wizard FSM.
// Step is exported because the cmd surface (cmd/wagyu) reads it
// after Run() returns to decide whether to drop into the main TUI
// (DoneStep) or short-circuit (TermsDeclined / ColorSchemePicked but
// terms not yet shown).
type Step int

const (
	// StepColorScheme is the first screen: 8 color scheme options +
	// Go code preview.
	StepColorScheme Step = iota
	// StepTerms is the second screen: ToS + data-collection opt-in.
	StepTerms
	// StepDone is the post-wizard state. The wizard returns nil from
	// Run() when the user accepts terms and we hand off to the main
	// TUI.
	StepDone
)

// String returns the human-readable name of a Step. Used by the
// "you are here" indicator at the top of the wizard screen.
func (s Step) String() string {
	switch s {
	case StepColorScheme:
		return "Choose your color scheme"
	case StepTerms:
		return "Terms of Service & Data Use"
	case StepDone:
		return "Done"
	default:
		return fmt.Sprintf("Step(%d)", int(s))
	}
}

// Wizard is the public state object. Run() mutates Step / Selected
// / Agreed and returns nil when the user accepts the ToS opt-in. A
// returning nil from Run() means "drop into the main TUI"; a non-nil
// error means the user aborted (e.g. pressed Esc on the ToS screen).
type Wizard struct {
	Step     Step
	Selected ColorScheme
	Agreed   bool
	// OptOut is true when the user toggles the "Help improve" checkbox
	// off. The v1.3.5 binary stores the boolean (default: on for
	// new installs) and writes it to ~/.antigravity/admin_settings.json.
	OptOut bool
	// lastMsg is the most recent user-visible info / error message
	// (e.g. "请先勾选" when the user hits Enter on the ToS screen
	// without checking the box). The Bubble Tea adapter reads this
	// field in its View() to render a hint line below the body. It
	// is cleared as soon as the next valid key is processed.
	lastMsg string
	// Out is the writer the wizard renders to. Defaults to os.Stdout
	// if nil. Tests inject bytes.Buffer.
	Out io.Writer
}

// NewWizard constructs a Wizard with sensible defaults. The default
// selected color scheme is SchemeTerminal (the v1.3.5 binary's "moves
// the **terminal** color scheme to the top of the selection list,
// making it the default choice during onboarding" release note).
func NewWizard() *Wizard {
	return &Wizard{
		Step:     StepColorScheme,
		Selected: SchemeTerminal,
		Agreed:   true, // opt-in by default
		Out:      nil,
	}
}

// Render writes the current step to w. Public so the cmd surface
// can re-render (e.g. on a key event) without re-running the FSM.
func (wz *Wizard) Render(w io.Writer) error {
	if w == nil {
		w = wz.Out
	}
	if w == nil {
		w = defaultWriter()
	}
	switch wz.Step {
	case StepColorScheme:
		return renderColorScheme(w, wz.Selected)
	case StepTerms:
		return renderTerms(w, !wz.OptOut)
	case StepDone:
		return renderDone(w)
	default:
		return fmt.Errorf("onboarding: unknown step %d", int(wz.Step))
	}
}

// renderDone writes the Step 3 "Setup complete!" screen.
func renderDone(w io.Writer) error {
	if _, err := fmt.Fprintln(w, "Setup complete!"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "  Press any key to continue..."); err != nil {
		return err
	}
	return nil
}

// Next advances the FSM by one step. After StepColorScheme it moves
// to StepTerms; after StepTerms it sets StepDone and returns
// (nil, true) so Run() can hand off to the main TUI.
func (wz *Wizard) Next() (err error, done bool) {
	switch wz.Step {
	case StepColorScheme:
		wz.Step = StepTerms
		return nil, false
	case StepTerms:
		if wz.OptOut {
			wz.Step = StepDone
			return nil, true
		}
		wz.Step = StepDone
		return nil, true
	case StepDone:
		return nil, true
	default:
		return fmt.Errorf("onboarding: Next from unknown step %d", int(wz.Step)), false
	}
}

// Previous moves the FSM one step back. Only valid on StepTerms
// (returns to StepColorScheme); on StepColorScheme it returns an
// error because there is no prior step.
func (wz *Wizard) Previous() error {
	switch wz.Step {
	case StepTerms:
		wz.Step = StepColorScheme
		return nil
	case StepColorScheme:
		return fmt.Errorf("onboarding: no previous step (already at color-scheme)")
	case StepDone:
		return fmt.Errorf("onboarding: no previous step (already done)")
	default:
		return fmt.Errorf("onboarding: Previous from unknown step %d", int(wz.Step))
	}
}

// Toggle flips the opt-in checkbox on the ToS screen. It is a no-op
// on other steps.
func (wz *Wizard) Toggle() {
	switch wz.Step {
	case StepTerms:
		wz.setAgreed(!wz.agreed())
	}
}

func (wz *Wizard) agreed() bool {
	return !wz.OptOut
}

func (wz *Wizard) setAgreed(v bool) {
	wz.OptOut = !v
	wz.Agreed = v
}

// HandleEnter processes an Enter key press on the wizard.
func (wz *Wizard) HandleEnter() (msg string, done bool) {
	wz.lastMsg = ""
	switch wz.Step {
	case StepColorScheme:
		wz.Step = StepTerms
		return "", false
	case StepTerms:
		if !wz.agreed() {
			wz.lastMsg = "请先勾选"
			return wz.lastMsg, false
		}
		wz.Step = StepDone
		return "", false
	case StepDone:
		return "", true
	default:
		wz.lastMsg = fmt.Sprintf("onboarding: HandleEnter from unknown step %d", int(wz.Step))
		return wz.lastMsg, false
	}
}

// HandleSpace processes a Space key press.
func (wz *Wizard) HandleSpace() (changed bool) {
	if wz.Step != StepTerms {
		return false
	}
	wz.setAgreed(!wz.agreed())
	wz.lastMsg = ""
	return true
}

// HandleAnyKey processes an arbitrary key press on the StepDone
// screen and returns true.
func (wz *Wizard) HandleAnyKey() bool {
	return wz.Step == StepDone
}

// LastMsg returns the most recent user-visible hint.
func (wz *Wizard) LastMsg() string { return wz.lastMsg }

// SetLastMsg lets the Bubble Tea adapter inject a custom hint.
func (wz *Wizard) SetLastMsg(s string) { wz.lastMsg = s }

// MoveSelection moves the highlighted color scheme up (-1) or down
// (+1). It wraps around at both ends. No-op on non-color-scheme steps.
func (wz *Wizard) MoveSelection(delta int) {
	if wz.Step != StepColorScheme {
		return
	}
	n := int(wz.Selected) + delta
	for n < 0 {
		n += ColorSchemeCount
	}
	for n >= ColorSchemeCount {
		n -= ColorSchemeCount
	}
	wz.Selected = ColorScheme(n)
}

// Run is the entry point called before the live TUI starts.
func (wz *Wizard) Run() error {
	if wz.Out == nil {
		wz.Out = defaultWriter()
	}
	if err := wz.Render(wz.Out); err != nil {
		return err
	}
	_, done := wz.Next()
	if !done {
		wz.Step = StepDone
	}
	return nil
}

var defaultWriter = func() io.Writer { return stdoutOrDiscard() }

func stdoutOrDiscard() io.Writer { return io.Discard }

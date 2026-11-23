// Package battlepanel implements the interactive Battle Mode comparison overlay
// for inspecting concurrent model arms, viewing their responses and unified
// diffs, eliminating arms, and picking a winner.
package battlepanel

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/battlemode"
)

// MsgDismissed is emitted when the user closes the Battle Mode overlay via Esc
// and no custom OnClose callback was provided.
type MsgDismissed struct{}

// MsgSnapshot delivers an updated Battle Mode session snapshot to the panel.
type MsgSnapshot struct {
	Snapshot battlemode.SessionSnapshot
}

// MsgTick advances the running-arm spinner animation.
type MsgTick struct{}

// Options configures callbacks for the Battle Mode overlay panel.
type Options struct {
	OnPickWinner func(armIndex int)
	OnEliminate  func(armIndex int)
	OnClose      func() tea.Cmd
}

// Panel is the interactive Bubble Tea overlay for Battle Mode.
type Panel struct {
	active       bool
	width        int
	height       int
	snapshot     battlemode.SessionSnapshot
	activeArm    int
	scroll       int
	spinnerFrame int
	statusNotice string
	opts         Options
}

// NewPanel creates a new Battle Mode overlay panel.
func NewPanel(opts Options) *Panel {
	return &Panel{
		snapshot: battlemode.SessionSnapshot{WinnerIndex: -1},
		opts:     opts,
	}
}

// Active reports whether the overlay is currently visible.
func (p *Panel) Active() bool {
	return p != nil && p.active
}

// Open makes the overlay visible.
func (p *Panel) Open() {
	if p == nil {
		return
	}
	p.active = true
	p.clampActiveArm()
}

// Close hides the overlay.
func (p *Panel) Close() {
	if p == nil {
		return
	}
	p.active = false
}

// Toggle flips the overlay visibility.
func (p *Panel) Toggle() {
	if p == nil {
		return
	}
	if p.active {
		p.Close()
	} else {
		p.Open()
	}
}

// SetSize updates the viewport dimensions in terminal cells.
func (p *Panel) SetSize(width, height int) {
	if p == nil {
		return
	}
	p.width = width
	p.height = height
	p.clampScroll()
}

// SetSnapshot updates the displayed Battle Mode session snapshot.
func (p *Panel) SetSnapshot(snap battlemode.SessionSnapshot) {
	if p == nil {
		return
	}
	p.snapshot = snap
	p.clampActiveArm()
	p.clampScroll()
}

// Snapshot returns the current session snapshot held by the panel.
func (p *Panel) Snapshot() battlemode.SessionSnapshot {
	if p == nil {
		return battlemode.SessionSnapshot{WinnerIndex: -1}
	}
	return p.snapshot
}

// ActiveArm returns the currently selected arm index.
func (p *Panel) ActiveArm() int {
	if p == nil {
		return 0
	}
	return p.activeArm
}

// SetActiveArm selects the arm at index and resets the body scroll offset.
func (p *Panel) SetActiveArm(index int) {
	if p == nil || len(p.snapshot.Arms) == 0 {
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(p.snapshot.Arms) {
		index = len(p.snapshot.Arms) - 1
	}
	if p.activeArm != index {
		p.activeArm = index
		p.scroll = 0
		p.statusNotice = ""
	}
}

// ScrollOffset returns the current vertical scroll offset of the body pane.
func (p *Panel) ScrollOffset() int {
	if p == nil {
		return 0
	}
	return p.scroll
}

// SetStatusNotice sets a transient status message shown in the metadata bar.
func (p *Panel) SetStatusNotice(msg string) {
	if p == nil {
		return
	}
	p.statusNotice = msg
}

// Tick advances the spinner frame for running arms.
func (p *Panel) Tick() {
	if p == nil {
		return
	}
	p.spinnerFrame = (p.spinnerFrame + 1) % len(spinnerFrames)
}

// Init implements tea.Model-style initialization.
func (p *Panel) Init() tea.Cmd {
	return nil
}

// Update handles Bubble Tea messages when the panel is driven as a sub-model.
func (p *Panel) Update(msg tea.Msg) (*Panel, tea.Cmd) {
	if p == nil {
		return nil, nil
	}
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.SetSize(m.Width, m.Height)
		return p, nil
	case MsgSnapshot:
		p.SetSnapshot(m.Snapshot)
		return p, nil
	case MsgTick:
		p.Tick()
		return p, nil
	case tea.KeyPressMsg:
		_, cmd := p.HandleKey(tea.Key(m))
		return p, cmd
	}
	return p, nil
}

// HandleKey processes a key event while the overlay is active.
// Returns handled=true when the overlay consumes the key so it does not leak
// to the host (e.g. Esc closes the overlay without cancelling an active turn).
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil || !p.active {
		return false, nil
	}

	// Allow Ctrl+C to fall through to the host's global interrupt handler.
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		return false, nil
	}

	// Esc closes the overlay without leaking.
	if key.Code == tea.KeyEsc || key.String() == "esc" {
		p.Close()
		if p.opts.OnClose != nil {
			return true, p.opts.OnClose()
		}
		return true, func() tea.Msg { return MsgDismissed{} }
	}

	// Tab / Shift+Tab cycle through arms.
	if key.Code == tea.KeyTab || key.String() == "tab" || key.String() == "shift+tab" {
		n := len(p.snapshot.Arms)
		if n > 0 {
			delta := 1
			if key.Mod.Contains(tea.ModShift) || key.String() == "shift+tab" {
				delta = -1
			}
			next := (p.activeArm + delta + n) % n
			p.SetActiveArm(next)
		}
		return true, nil
	}

	// Left / Right switch active arm.
	if key.Code == tea.KeyLeft || (key.Mod == 0 && key.Code == 'h') {
		if p.activeArm > 0 {
			p.SetActiveArm(p.activeArm - 1)
		}
		return true, nil
	}
	if key.Code == tea.KeyRight || (key.Mod == 0 && key.Code == 'l') {
		if p.activeArm < len(p.snapshot.Arms)-1 {
			p.SetActiveArm(p.activeArm + 1)
		}
		return true, nil
	}

	// 1..9 jump directly to arm index 0..8.
	if key.Mod == 0 && key.Code >= '1' && key.Code <= '9' {
		idx := int(key.Code - '1')
		if idx < len(p.snapshot.Arms) {
			p.SetActiveArm(idx)
		}
		return true, nil
	}

	// Up / Down / PgUp / PgDn / Home / End scroll response + diff body.
	switch {
	case key.Code == tea.KeyUp || (key.Mod == 0 && key.Code == 'k'):
		p.scrollBy(-1)
		return true, nil
	case key.Code == tea.KeyDown || (key.Mod == 0 && key.Code == 'j'):
		p.scrollBy(1)
		return true, nil
	case key.Code == tea.KeyPgUp:
		p.scrollBy(-p.pageStep())
		return true, nil
	case key.Code == tea.KeyPgDown:
		p.scrollBy(p.pageStep())
		return true, nil
	case key.Code == tea.KeyHome || (key.Mod == 0 && key.Code == 'g'):
		p.scroll = 0
		return true, nil
	case key.Code == tea.KeyEnd || (key.Mod == 0 && key.Code == 'G'):
		p.scroll = p.maxScroll()
		return true, nil
	}

	// w or Enter triggers onPickWinner(armIndex).
	if key.Code == tea.KeyEnter || (key.Mod == 0 && (key.Code == 'w' || key.Text == "w")) {
		p.triggerPickWinner()
		return true, nil
	}

	// x triggers onEliminate(armIndex).
	if key.Mod == 0 && (key.Code == 'x' || key.Text == "x") {
		p.triggerEliminate()
		return true, nil
	}

	// Modal overlay claims all other keys while visible.
	return true, nil
}

func (p *Panel) triggerPickWinner() {
	if len(p.snapshot.Arms) == 0 || p.activeArm < 0 || p.activeArm >= len(p.snapshot.Arms) {
		return
	}
	arm := p.snapshot.Arms[p.activeArm]
	switch arm.Status {
	case battlemode.StatusEliminated:
		p.statusNotice = "Cannot pick eliminated arm"
		return
	case battlemode.StatusRunning, battlemode.StatusPending:
		p.statusNotice = fmt.Sprintf("Arm [%d] is still %s", p.activeArm+1, arm.Status)
		return
	}
	p.statusNotice = fmt.Sprintf("Winner selected: [%d] %s", p.activeArm+1, arm.Spec.DisplayName())
	if p.opts.OnPickWinner != nil {
		p.opts.OnPickWinner(p.activeArm)
	}
}

func (p *Panel) triggerEliminate() {
	if len(p.snapshot.Arms) == 0 || p.activeArm < 0 || p.activeArm >= len(p.snapshot.Arms) {
		return
	}
	arm := p.snapshot.Arms[p.activeArm]
	if arm.Winner {
		p.statusNotice = "Cannot eliminate winning arm"
		return
	}
	p.statusNotice = fmt.Sprintf("Eliminated: [%d] %s", p.activeArm+1, arm.Spec.DisplayName())
	if p.opts.OnEliminate != nil {
		p.opts.OnEliminate(p.activeArm)
	}
}

func (p *Panel) clampActiveArm() {
	if len(p.snapshot.Arms) == 0 {
		p.activeArm = 0
		return
	}
	if p.activeArm < 0 {
		p.activeArm = 0
	}
	if p.activeArm >= len(p.snapshot.Arms) {
		p.activeArm = len(p.snapshot.Arms) - 1
	}
}

func (p *Panel) effectiveSize() (int, int) {
	w := p.width
	if w <= 0 {
		w = 80
	}
	h := p.height
	if h <= 0 {
		h = 24
	}
	return w, h
}

func (p *Panel) pageStep() int {
	_, h := p.effectiveSize()
	step := h / 2
	if step < 4 {
		step = 4
	}
	return step
}

func (p *Panel) scrollBy(delta int) {
	p.scroll += delta
	p.clampScroll()
}

func (p *Panel) clampScroll() {
	if p.scroll < 0 {
		p.scroll = 0
	}
	max := p.maxScroll()
	if p.scroll > max {
		p.scroll = max
	}
}

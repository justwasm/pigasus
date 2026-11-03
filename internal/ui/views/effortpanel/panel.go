// Package effortpanel holds the /effort panel and the effort slider it
// draws.
//
// # What it is
//
// The reference (`agy`) answers `/effort` with a horizontal slider: the
// levels the running model publishes, a ◉ marker on the tier the session
// runs at, and the blurb of that tier below the labels.
//
// The slider's geometry was recovered by driving the reference with ←/→
// (/tmp/agycmp/effort_probe.py) and is shared by two panels: this one,
// which *is* the slider, and the /model picker, which embeds it under
// its row list. The geometry lives here, so the picker borrows it
// (internal/ui/views/modelpicker) instead of holding a second copy.
//
// # Where the panel lives
//
// The panel itself is here: the marker's position, the frame and the
// keys. The host keeps only the seams — the levels the model publishes,
// the tier the session runs at, the way a chosen tier is applied, and the
// way out of the panel.
//
// The panel is rebuilt every time it opens, so a stale marker is never
// shown.
package effortpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/models"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Tiers are the reasoning levels the running model publishes, in the
	// order the slider shows them.
	//
	// The slider is the model's own: the reference draws the levels the
	// model publishes (【反编译】 agy 1.4.12,
	// backend.EffortsForBase, sorted by model.sortEfforts, and
	// model.EffortSelectorModel.currentEffortIndex indexes into that
	// list), so a model that publishes five levels gets a five-place
	// slider and a model that publishes none gets the reference's "not
	// adjustable" answer instead of a bar.
	//
	// It is read on every draw, not cached: the model can change while
	// the panel is open.
	Tiers func() []string

	// Current is the tier the session runs at — the word, not the index.
	//
	// The index is an index into Tiers, and only the host knows which
	// model the session runs at, so the host hands over the word and the
	// panel maps it into its own list.
	//
	// It is read on every draw, not cached: the tier changes when the
	// user applies one, and the panel is still open at that moment.
	Current func() string

	// Apply takes the chosen tier and does the work: the transcript's
	// echo, the flag the runtime reads and the persisted model label.
	//
	// The argument is the index into Tiers.
	Apply func(level int) tea.Cmd

	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /effort panel.
//
// The panel owns the whole surface: the marker's position, the frame and
// the keys. Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// cursor is the tier the marker sits on: an index into the model's
	// own list of tiers (0 is the lightest, the last one the heaviest).
	cursor int
}

// New builds the /effort panel.
//
// The marker starts on the tier the session runs at, the way the
// reference opens the panel on the current tier.
func New(seams Seams) *Panel {
	p := &Panel{seams: seams}
	p.cursor = p.currentIndex()
	return p
}

// tiers returns the levels the slider offers.
//
// A panel built without the seam falls back to the reference's own three
// levels, so the package stays usable from a test without a host. A
// panel whose seam answers with no levels at all is a model with a fixed
// reasoning budget — the reference shows its "not adjustable" answer
// instead of a bar.
func (p *Panel) tiers() []string {
	if p == nil || p.seams.Tiers == nil {
		return EffortTiers
	}
	return p.seams.Tiers()
}

// adjustable reports whether the model's levels can be chosen between.
//
// The rule is the reference's own: the effort is adjustable when the
// model publishes more than one level (models.EffortAdjustable — see
// the reference's backend.CurrentEffortSelection). A model with one
// level has a fixed reasoning budget, and the slider has nothing to
// move.
func (p *Panel) adjustable() bool {
	return models.EffortAdjustable(p.tiers())
}

// currentIndex is the index of the tier the session runs at.
//
// A tier the model's list does not carry leaves the marker on the
// heaviest level: the reference's own slider parks on the current tier,
// and a tier the model does not publish has no place of its own.
func (p *Panel) currentIndex() int {
	tiers := p.tiers()
	if len(tiers) == 0 {
		return 0
	}
	if p == nil || p.seams.Current == nil {
		return len(tiers) - 1
	}
	if i := models.TierIndexOf(tiers, p.seams.Current()); i >= 0 {
		return i
	}
	return len(tiers) - 1
}

// Cursor is the tier the marker sits on.
func (p *Panel) Cursor() int {
	if p == nil {
		return EffortHigh
	}
	return p.cursor
}

// SetCursor puts the marker on the given tier.
//
// The index is clamped into the model's own list of tiers, so a stale
// index cannot strand the marker off the bar.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	p.cursor = clampLevel(idx, len(p.tiers()))
}

// RowCount is the number of tiers the slider offers.
func (p *Panel) RowCount() int {
	return len(p.tiers())
}

// moveCursor moves the marker by delta tiers and clamps it into the
// model's own list of tiers.
func (p *Panel) moveCursor(delta int) {
	if p == nil {
		return
	}
	p.cursor = clampLevel(p.cursor+delta, len(p.tiers()))
}

// apply applies the tier the marker sits on.
//
// A model whose effort cannot be adjusted has nothing to apply: the
// panel shows the reference's "not adjustable" answer and stays put.
func (p *Panel) apply() tea.Cmd {
	if p == nil || p.seams.Apply == nil {
		return nil
	}
	if !p.adjustable() {
		return nil
	}
	return p.seams.Apply(p.cursor)
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// clampLevel clamps a slider index into the model's own list of tiers.
func clampLevel(level, tiers int) int {
	if tiers <= 0 {
		return 0
	}
	if level < 0 {
		return 0
	}
	if level > tiers-1 {
		return tiers - 1
	}
	return level
}

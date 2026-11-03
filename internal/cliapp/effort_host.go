// effort_host.go — the host's side of the /effort panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/effortpanel: it owns the
// slider's marker, the frame and the keys. The host supplies the four
// things the panel cannot answer itself — the levels the running model
// publishes, the tier the session runs at, the way a chosen tier is
// applied, and the way out of the panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale marker is never shown.
//
// # The tier
//
// The tier itself is the host's: it lives on the flag set (the --effort
// value at start-up, the value a /effort <level> shortcut or the panel's
// own Enter writes at runtime), and the model's own label is the
// fallback. The panel only ever asks for it.
//
// # The model
//
// The model the slider is about is the host's fact too (effortModel):
// the running model when the catalogue knows it, the settings-resolved
// effective model otherwise. The catalogue answers which levels that
// model publishes, and the slider draws exactly those — so a model that
// publishes five of them gets a five-place slider, and a model with a
// fixed reasoning budget gets the reference's "not adjustable" answer
// instead of a bar.
//
// The tiers' names and the slider's geometry live with the slider, in
// internal/ui/views/effortpanel; the host names them through that
// package directly, and it holds no forwarding helper of its own.

package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
)

// newEffortPanel builds the /effort panel with the host's seams.
func (m *runMode) newEffortPanel() *effortpanel.Panel {
	return effortpanel.New(effortpanel.Seams{
		// The levels the running model publishes: the slider draws the
		// model's own levels, so a model that publishes five of them
		// gets a five-place slider and a model that publishes none gets
		// the reference's "not adjustable" answer instead of a bar.
		Tiers: m.effortTiers,
		// The tier the session runs at: the marker starts there and the
		// blurb's "(current)" annotation names it.
		Current: m.currentEffortName,
		// Enter applies the tier: the transcript's echo, the flag the
		// runtime reads and the persisted model label.
		Apply: m.applyEffortSelection,
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// effortTiers returns the reasoning levels the running model publishes.
//
// The running model is the host's fact; the catalogue answers which
// levels that model publishes. A model with a fixed reasoning budget
// publishes none, and the slider then has nothing to draw.
func (m *runMode) effortTiers() []string {
	return catalog.EffortTiersFor(m.effortModel())
}

// effortModel returns the model the effort surface is about.
//
// The slider, the "not adjustable" guard and the apply path must all
// talk about the *same* model, so the fact has one owner: the running
// model when the catalogue knows it (the reference's own models and the
// ones the user configured), the settings-resolved effective model when
// the running one is a name the catalogue does not carry, and the
// reference's own default when neither answers.
//
// A model the catalogue does not know has no published levels, and a
// model with fewer than two published levels has a fixed reasoning
// budget — the reference's own rule (models.EffortAdjustable).
func (m *runMode) effortModel() string {
	if label := strings.TrimSpace(m.model); label != "" && catalog.ModelAvailable(label) {
		return label
	}
	if effective, _ := catalog.ResolveEffectiveModel(""); strings.TrimSpace(effective) != "" {
		return effective
	}
	return catalog.DefaultModel
}

// effortPanel returns the open /effort panel.
//
// The panel is built when the /effort panel opens — the host's slashPanel
// struct is reset whenever a panel closes — so the accessor is here so
// that a caller which finds the panel missing still gets one that
// answers.
func (m *runMode) effortPanel() *effortpanel.Panel {
	if m.panel.ef == nil {
		m.panel.ef = m.newEffortPanel()
	}
	return m.panel.ef
}

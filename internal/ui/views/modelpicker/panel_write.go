// panel_write.go — the /model panel's writes.
//
// Every write goes through settings.json: the panel builds the
// catalogue slug the reference persists and hands it to the settings
// layer, which edits the one key that changed and leaves the rest of
// the document alone.
//
// The session's own model, its status row and its transport are the
// host's, so the panel hands the display label over through the
// ApplyModel seam.

package modelpicker

import (
	settings "github.com/justwasm/pigasus/internal/core/settings"
)

// cycleEffort advances the effort slider by dir (+1 / -1) with
// wrap-around (low ⇆ high across the boundary, medium stays in the
// middle), then persists the new (model, effort) catalogue slug to
// settings.json and updates the session's model so the status row
// reflects the change on the very next frame.
//
// Cycle `model-left-right-effort` deepens cycle 197: the prior clamp
// behaviour only moved the slider — settings.json was written only on
// Enter, so a user that cycled the effort and then closed the panel
// without selecting (Esc, or selecting a different row) lost the
// preview. Now ←/→ is the change-of-record: settings.json sees the
// same slug `selectModel` would write on Enter, the status row picks up
// the new tier on the next render, and the picker slider redraws
// because the panel reads its own effort cursor live.
//
// Only a row that offers more than one effort is multi-effort; the
// other rows are fixed and ←/→ remain inert on them — the panel must
// never overwrite the user's pinned model just because their cursor
// wandered onto Claude Sonnet and they pressed an arrow.
//
// The wrap is over the row's *own* tiers: the reference's Gemini row has
// three (low ⇆ high), a configured model may have five.
func (p *Panel) cycleEffort(dir int) {
	if p == nil {
		return
	}
	row, ok := p.currentRow()
	if !ok || !row.HasEffortSlider() {
		return
	}
	// Wrap: the lowest tier ⇆ the highest one in either direction.
	c := p.effortCursor + dir
	if c < 0 {
		c = len(row.Tiers) - 1
	} else if c > len(row.Tiers)-1 {
		c = 0
	}
	p.effortCursor = c
	p.persist(row.CatalogueID(c))
}

// selectModel persists the highlighted (model, effort) as the new
// settings.json "model" key and returns the display label.
//
// Persisted shape: the catalogue ID (e.g. "gemini-3.8-flash-medium") —
// the same form `agy -p /model` prints as its first column and the form
// modelAvailable() matches. The session's model is set to the *label* so
// the status row renders "Gemini 3.8 Flash · medium" rather than the raw
// slug. Returns "" when the row has no catalogue ID (Gemini 4 Argon).
func (p *Panel) selectModel() string {
	if p == nil {
		return ""
	}
	row, ok := p.currentRow()
	if !ok {
		return ""
	}
	if row.Slug == "" {
		return "" // not selectable on this account
	}
	return p.persist(row.CatalogueID(p.effortCursor))
}

// persist writes the catalogue slug to settings.json and hands the
// display label to the session.
//
// The tier ids are always in the catalogue (catalog.ReferenceModels pins
// Gemini's three, and the configured models' own tier rows are listed
// there too), so the fallback branch — a slug no catalogue carries — is
// unreachable in practice. Falling back to the slug mirrors the custom
// name row's own shape so we don't leak a half-applied state.
func (p *Panel) persist(slug string) string {
	_ = settings.SetField("model", slug)
	if entry, ok := CatalogueEntry(slug); ok {
		p.applyModel(entry.Label)
		return entry.Label
	}
	// Custom-name row (MiniMax M3 etc.): the slug IS the label.
	p.applyModel(slug)
	return slug
}

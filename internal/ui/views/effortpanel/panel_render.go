// panel_render.go — the /effort panel's frame.
//
// The panel is the slider: the title, the bar with its two arrows, the
// tier labels, the blurb of the tier the marker sits on and the legend.
// The geometry is the reference's own and lives in slider.go.
//
// The colours are read when the panel is drawn, so they follow the
// selected scheme: the panel does not cache them at the time it opens.

package effortpanel

// NotAdjustableMessage is the reference's own answer for a model whose
// effort cannot be changed.
//
// The string is the reference's own — 【strings】 agy 1.4.12, file offset
// 0x36f64d5, and the binary stores the length 45 beside it, with no
// trailing period — and the slider's view renders it when the model
// publishes no *adjustable* effort level
// (【反编译】 model.EffortSelectorModel.View, 0x102c14544: the view
// branches on the model's level list being empty and renders this string
// on its own; the reference's own rule for "adjustable" is more than one
// level, so a model with a single level is answered the same way).
//
// The command's own answer for the same situation is a *different*
// string — "Effort isn't adjustable for %s."
// (models.NotAdjustableMessage) — and the two are not interchangeable:
// the panel's carries no period and names no model.
const NotAdjustableMessage = "Effort isn't adjustable for the current model"

// Lines renders the /effort panel.
//
// Byte-for-byte layout recovered from the reference's own capture
// (/tmp/agycmp/panels/ppref.effort.raw.txt) by driving it with ←/→.
//
// A model whose reasoning budget is fixed — one that publishes no
// adjustable level — gets the reference's "not adjustable" answer
// instead of a bar. The rule is the reference's own: the effort is
// adjustable when the model publishes more than one level
// (models.EffortAdjustable).
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if !p.adjustable() {
		return []string{NotAdjustableMessage}
	}
	return RebuildEffortSliderN(p.cursor, p.currentIndex(), p.tiers(), true)
}

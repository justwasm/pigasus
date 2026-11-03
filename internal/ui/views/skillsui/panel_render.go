// panel_render.go — the /skills picker's rows.
//
// The layout is the reference's own, recovered from the caps/ref.h*.raw.txt
// probes (agy 1.4.0, 220 wide):
//
//   - the header block ("Skills", the catalogue size, the "Create new
//     skills" block) comes from HeaderLines;
//   - one group header per catalogue root, in bold, with the group's
//     directory dimmed;
//   - one row per skill, the cursor's row carrying the "› " marker
//     inside the blue name span;
//   - a "[a-b of N items]" counter while the window is shorter than the
//     item list;
//   - the standard "Keyboard: ↑/↓ Navigate  enter View  esc Cancel" legend.
//
// The detail view is the reference's own SKILL.md viewer: the skill's
// name, its description, its path and the body, with a line counter and
// the scroll legend.

package skillsui

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// Lines renders the panel: the grouped skill list, or the per-skill
// detail view when it is open.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.detail {
		return p.detailLines()
	}
	return p.listLines()
}

// listLines renders the grouped skill list.
func (p *Panel) listLines() []string {
	model := NewPanelModel(p.catalog())
	width, height := p.size()
	if width <= 0 {
		width = PanelDefaultWidth
	}
	lines := HeaderLines(len(model.Skills))
	if len(model.Items) == 0 {
		return append(lines, "", skillsPanelHint())
	}

	cursor := p.cursor
	if cursor < -1 || cursor >= len(model.Skills) {
		cursor = -1
	}
	n := VisibleItemsFor(height)
	if n > len(model.Items) {
		n = len(model.Items)
	}
	start := 0
	if cursor >= 0 {
		start = model.SkillItem[cursor] - (n - 1)
		if start < 0 {
			start = 0
		}
	}
	end := start + n
	if end > len(model.Items) {
		end = len(model.Items)
	}

	lines = append(lines, p.renderItems(model, start, end, cursor, width)...)
	if n < len(model.Items) {
		lines = append(lines, palette.DimGrayOpen+
			fmt.Sprintf("  [%d-%d of %d items]", start+1, end, len(model.Items))+
			palette.DimGrayClose)
	}
	return append(lines, "", skillsPanelHint())
}

// renderItems renders the visible slice of the flat item list.
//
// A group header is preceded by a blank row — except the first one — and
// a skill row may be followed by its marketplace link.
func (p *Panel) renderItems(model PanelModel, start, end, cursor, width int) []string {
	var lines []string
	first := true
	for i := start; i < end; i++ {
		it := model.Items[i]
		if it.Header {
			if !first {
				lines = append(lines, "")
			}
			g := model.Groups[it.Group]
			lines = append(lines, "\x1b[1m"+g.Left+" · "+PanelGray238+g.Label+"\x1b[0m")
		} else {
			s := model.Skills[it.Skill]
			lines = append(lines, PanelRow(s, it.Skill == cursor, width))
			if link := MarketplaceLink(s, p.osc8()); link != "" {
				lines = append(lines, link)
			}
		}
		first = false
	}
	return lines
}

// detailLines renders the per-skill detail view.
//
// A skill without a path has no body to show, so the reference draws the
// short form — the name, the description and the path — instead of the
// viewer.
func (p *Panel) detailLines() []string {
	s := p.selected()
	if s.Path == "" {
		return p.legacyDetailLines(s)
	}
	width, height := p.size()
	doc := p.detailDocument(s, width)
	vis := SkillsDetailVisible(height)
	total := len(doc) + 1
	maxOffset := total - vis
	if maxOffset < 0 {
		maxOffset = 0
	}
	if p.window < 0 {
		p.window = 0
	}
	if p.window > maxOffset {
		p.window = maxOffset
	}
	hi := p.window + vis
	if hi > len(doc) {
		hi = len(doc)
	}
	var out []string
	for _, l := range doc[p.window:hi] {
		out = append(out, l.Rows...)
	}
	if total > vis {
		out = append(out, viewer.Counter(p.window+1, p.window+vis, total))
	} else {
		out = append(out, "")
	}
	out = append(out, "",
		tuilib.PanelDotHint("  ", "↑/↓", "scroll", "pgup/pgdown", "page",
			"ctrl+end", "bottom", "ctrl+home", "top", "esc", "back"))
	return out
}

// legacyDetailLines renders the short form of the detail view: the one
// the reference draws for a skill whose body it cannot read.
func (p *Panel) legacyDetailLines(s SkillInfo) []string {
	return []string{
		"\x1b[1m\x1b[39mSkills\x1b[0m",
		"",
		"  " + "\x1b[1m" + palette.BlueOpen + DisplayName(s) + "\x1b[0m",
		"",
		"  " + PanelGray238 + s.Description + PanelGray238C,
		"",
		"  " + PanelGray238 + "Path: " + s.Path + PanelGray238C,
		"",
		tuilib.PanelKeyboardHint("esc", "Back"),
	}
}

// selected is the catalogue entry the cursor is on.
//
// The cursor indexes the *grouped* catalogue — shared skills, then the
// builtin bundle, then the plugin groups — so the entry is looked up in
// the same order the list renders.
func (p *Panel) selected() SkillInfo {
	cat := PanelCatalog(p.catalog())
	if p == nil || p.cursor < 0 || p.cursor >= len(cat) {
		return SkillInfo{}
	}
	return cat[p.cursor]
}

// detailDocument is the logical-line sequence the detail view scrolls.
func (p *Panel) detailDocument(s SkillInfo, width int) []DetailLine {
	return SkillsDetailDocument(DisplayName(s), s.Description, s.Path, width)
}

// skillsPanelHint is the picker's footer.
func skillsPanelHint() string {
	return tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "View", "esc", "Cancel")
}

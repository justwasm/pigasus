package tuiviewport

import (
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// ComputeVisibleWindow extracts and clips the visible slice of rows.
func ComputeVisibleWindow(rows []string, prefix []int, scroll, availScroll int, autoScroll bool) ([]string, int, bool) {
	maxScroll := len(rows) - availScroll
	if autoScroll {
		scroll = maxScroll
	}
	scroll = tuilib.ClampInt(scroll, 0, maxScroll)
	if scroll >= maxScroll {
		autoScroll = true
	}
	visible := append([]string(nil), rows[scroll:scroll+availScroll]...)
	visible = ClipToScreenRowsPrefix(visible, prefix, scroll, availScroll)
	for len(visible) < availScroll {
		visible = append(visible, "")
	}
	return visible, scroll, autoScroll
}

// FormatVisibleRows attaches sticky/pinned headers and pads rows to effective width.
func FormatVisibleRows(visible []string, effective int, pinnedPromptText, stickyBarRow string, altScreen bool) []string {
	if altScreen && pinnedPromptText != "" {
		visible = append([]string{tuilib.PadTo(pinnedPromptText, effective)}, visible...)
	} else if stickyBarRow != "" {
		sticky := []string{tuilib.PadTo(stickyBarRow, effective), palette.Rule(effective)}
		visible = append(sticky, visible...)
	}
	out := make([]string, len(visible))
	for i, row := range visible {
		out[i] = tuilib.PadTo(row, effective)
	}
	return out
}

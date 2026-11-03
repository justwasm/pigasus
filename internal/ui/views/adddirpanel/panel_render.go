// panel_render.go — the /add-dir panel's rows.
//
// The frame is the clone's own (the reference has no /add-dir panel to
// compare — it answers the command inline):
//
//	Add Directory to Workspace
//
//	  Trusted workspaces:
//	  /Users/foo/a
//
//	  Path: /tmp/foo▋
//
//	Keyboard: enter Add Directory  esc Cancel
//
// The trusted-workspace list is the dim grey the clone uses for the
// /tasks and /artifact empty-state lines, and the path row ends in the
// one reverse-video cell the reference draws as the input cursor.

package adddirpanel

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The title span is the reference's: bold on the default foreground.
const (
	panelTitleOpen  = "\x1b[1m\x1b[39m"
	panelTitleClose = "\x1b[0m"
)

// searchCursorBlock is the input cursor the panel draws as one
// reverse-video cell.
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// Lines renders the panel.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	trusted := p.trusted()
	lines := []string{
		panelTitleOpen + "Add Directory to Workspace" + panelTitleClose,
		"",
	}
	if len(trusted) == 0 {
		lines = append(lines,
			palette.DimGrayOpen+"  No trusted workspaces yet."+palette.DimGrayClose)
	} else {
		lines = append(lines,
			palette.DimGrayOpen+"  Trusted workspaces:"+palette.DimGrayClose)
		for _, dir := range trusted {
			lines = append(lines,
				"  "+palette.DimGrayOpen+dir+palette.DimGrayClose)
		}
	}
	lines = append(lines,
		"",
		"  Path: "+p.text+searchCursorBlock,
		"",
		tuilib.PanelKeyboardHint("enter", "Add Directory", "esc", "Cancel"),
	)
	return lines
}

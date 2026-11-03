// panel_render.go — the /agents panel's rows.
//
// The frame is the reference's own: the title (with the fork notice
// once a switch is prepared), the two path rows, the agent list, the
// subagent section and the keyboard legend.

package agentspanel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"

	// panelGray238C is the dim grey's closing span.
	panelGray238C = "\x1b[39m"

	// panelGreen34 is the green the reference paints the ● marker of
	// the active agent with.
	panelGreen34 = "\x1b[38;5;34m"

	// panelBoldOpen is the section header's bold span.
	panelBoldOpen = "\x1b[1m"
	// panelBoldClose closes the section header's bold span.
	panelBoldClose = "\x1b[0m"
)

// Lines renders the panel: the title, the two path rows, the agent
// list, the subagent section and the keyboard legend.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.detailMode && p.detailSubagent != nil {
		return p.detailLines()
	}
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	title := "Create New Agents"
	if p.prepared {
		if p.hasSession() {
			title += "  [ Switch will fork the current conversation on exit ]"
		} else {
			title += "  [ Switch will create a new conversation on exit ]"
		}
	}
	lines := []string{
		panelTitleOpen + title + panelTitleClose,
		"  " + palette.SubtleOpen + "Workspace: " +
			filepath.Join(wd, ".agents", "agents", "{agent_name}", "agent.md") + panelGray238C,
		"  " + palette.SubtleOpen + "Global: " +
			filepath.Join(home, ".gemini", "config", "agents", "{agent_name}", "agent.md") + panelGray238C,
		"",
		"",
	}
	active := p.activeAgent()
	header := "Available Agents"
	if active != "" {
		header += "  (active: " + active + ")"
	}
	lines = append(lines, panelBoldOpen+header+panelBoldClose)
	lines = append(lines, p.agentRows(active)...)

	subs := p.subagents()
	if len(subs) > 0 {
		running, done := subagentCounts(subs)
		lines = append(lines, "", panelBoldOpen+subagentsHeader(running, done)+panelBoldClose)
		lines = append(lines, p.subagentRows(subs)...)
	}

	lines = append(lines,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Select / Toggle",
			"k", "Kill Active Subagent", "esc", "Go Back"),
	)
	return lines
}

func (p *Panel) detailLines() []string {
	sa := p.detailSubagent
	if sa == nil {
		return nil
	}
	lines := []string{
		panelTitleOpen + "Subagent Detail: " + sa.Name + panelTitleClose,
		palette.SubtleOpen + "Status: " + sa.StatusLine() + panelGray238C,
		"",
	}
	if sa.Title != "" {
		lines = append(lines,
			panelBoldOpen+"Task Title:"+panelBoldClose,
			"  "+sa.Title,
			"",
		)
	}
	if appr, ok := p.pendingAuthorization(sa); ok {
		lines = append(lines,
			panelBoldOpen+palette.WarningOpen+"Pending Approval:"+palette.SeparatorClose+panelBoldClose,
			"  "+approvalSuffix(appr),
			"",
		)
	}
	out := sa.Output()
	lines = append(lines,
		panelBoldOpen+"Execution Output / Log:"+panelBoldClose,
	)
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			lines = append(lines, "  "+line)
		}
	} else {
		lines = append(lines, "  (no output captured yet)")
	}
	lines = append(lines,
		"",
		tuilib.PanelKeyboardHint("k", "Kill Subagent", "a", "Approve", "d", "Deny", "esc", "Back to list"),
	)
	return lines
}

// agentRows renders one row per agent: the built-in default first, then
// the catalogue's markdown agents.
//
// The active agent's row carries a green ● marker, the row under the
// cursor is blue, and the description is dim grey.
func (p *Panel) agentRows(active string) []string {
	type agentRow struct {
		name string
		desc string
	}
	rows := []agentRow{{"default", "Default agent"}}
	for _, a := range p.agents {
		rows = append(rows, agentRow{a.Name, a.Description})
	}
	cursor := p.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	var lines []string
	for i, r := range rows {
		rowActive := (i == 0 && active == "") || (i > 0 && active == r.name)
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		marker := "  "
		if rowActive {
			marker = panelGreen34 + "● " + palette.WarningBlueOpen
		}
		name := r.name
		if i == cursor {
			name = palette.BlueOpen + name + palette.BlueClose
		} else if rowActive {
			name = palette.WarningBlueOpen + name + palette.BlueClose
		}
		line := palette.BlueOpen + prefix + palette.BlueClose + marker + name
		if r.desc != "" {
			line += "  " + palette.SubtleOpen + r.desc + panelGray238C
		}
		lines = append(lines, line)
	}
	return lines
}

// subagentRows renders one row per subagent: its name and its status
// line, plus the authorization the row is waiting on.
//
// The rows are unstyled plain text — the panel's blue/green palette
// is reserved for the "Available Agents" cursor row above, so the
// subagent list stays distinguishable. The row under the cursor is
// marked with the panel's own "> " prefix, because the cursor reaches
// the subagent rows: the reference's cursor moves "between headers,
// subagents, and available agents"
// (official-docs/cli/commands/agents.md:112), and the kill and the
// inline approvals act on the subagent row under it.
func (p *Panel) subagentRows(subs []*subagent.Subagent) []string {
	rows := make([]string, 0, len(subs))
	offset := p.agentRowCount()
	for i, sa := range subs {
		if sa == nil {
			continue
		}
		prefix := "  "
		if p.cursor-offset == i {
			prefix = "> "
		}
		line := prefix + fmt.Sprintf("%s  %s", sa.Name, sa.StatusLine())
		if appr, ok := p.pendingAuthorization(sa); ok {
			line += approvalSuffix(appr)
		}
		rows = append(rows, line)
	}
	return rows
}

// approvalSuffix is the inline authorization the panel draws on a
// subagent's row.
//
// 【strings】 the reference's own two option labels are "Yes, allow
// tool call" and "No, deny tool call". The deny half survives in the
// pruned string index (out/local-macos-1.4.11/strings-index.txt, the
// A-段 window around line 66329), beside the reference's keybinding
// name "KeySubagentRespondAgent"; the allow half's copy was in the
// retired dump and did not survive into the index (存疑). The
// reference's other two names — "KeySubagentApproveFast" and
// "KeySubagentApproveFastNo" — are in the same index (the A-段 window
// around line 66521).
//
// The layout *around* the two labels is the clone's own: the
// reference's inline row was never captured, so only its vocabulary is
// reproduced (存疑).
func approvalSuffix(appr lifecycle.PendingApproval) string {
	what := appr.ToolName
	if appr.Kind == lifecycle.ApprovalUserInput {
		what = appr.Question
	}
	suffix := "  [A] Yes, allow tool call  [D] No, deny tool call"
	if what == "" {
		return suffix
	}
	return "  awaiting approval: " + what + suffix
}

// subagentsHeader is the group header the reference puts above the
// subagent rows.
//
// The reference's own fragment is `%d running, %d done` — a string of
// the reference's own table, and the surviving pruned index of that table
// is out/local-macos-1.4.11/strings-index.txt (the full dump is retired) —
// and the reference's
// documentation shows the header as `▸ Subagents (1 running, 2 done)`
// (`official-docs/cli/commands/agents.md:80`).
//
// The glyph is the documentation's: the reference's string table is
// ASCII-filtered, so it carries no Unicode to confirm or deny it.
func subagentsHeader(running, done int) string {
	return fmt.Sprintf("▸ Subagents (%d running, %d done)", running, done)
}

// subagentCounts splits the panel's subagents into the two counts the
// group header carries.
//
// A subagent that has not started yet ("Pending") is neither running
// nor done; every finished subagent — done, errored, killed or stopped
// — is done.
func subagentCounts(subs []*subagent.Subagent) (running, done int) {
	for _, sa := range subs {
		if sa == nil {
			continue
		}
		switch sa.StatusLine() {
		case subagent.StatusRunning:
			running++
		case subagent.StatusPending:
			// not started yet: neither running nor done
		default:
			done++
		}
	}
	return running, done
}

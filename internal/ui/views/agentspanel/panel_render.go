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
	"time"

	"github.com/charmbracelet/x/ansi"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/lifecycle"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
// subagentModelMinWidth is the narrow-terminal cutoff for the model
// cell in the /agents subagent rows (AGY 1.5.3).
const subagentModelMinWidth = 64

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
			filepath.Join(confhome.Gemini(), "agents", "{agent_name}", "agent.md") + panelGray238C,
		"",
	}

	subs := p.subagents()
	if len(subs) > 0 {
		running, done := subagentCounts(subs)
		arrow := "▸"
		if p.expandedSubagents {
			arrow = "▾"
		}
		headerPrefix := "  "
		if p.cursor == 0 {
			headerPrefix = "> "
		}
		header := headerPrefix + arrow + " " + p.groupTitle()
		if counts := formatSubagentCounts(running, done); counts != "" {
			header += "  " + counts
		}
		lines = append(lines, header)
		if p.expandedSubagents {
			for i, sa := range subs {
				prefix := "    "
				if p.cursor == 1+i {
					prefix = "  > "
				}
				line := prefix + "|_ * " + p.formatSubagentChild(sa)
				if appr, ok := p.pendingAuthorization(sa); ok {
					line += approvalSuffix(appr)
				}
				lines = append(lines, line)
			}
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, "")
	}

	active := p.activeAgent()
	header := "Available Agents"
	if active != "" {
		header += "  (active: " + active + ")"
	}
	lines = append(lines, panelBoldOpen+header+panelBoldClose)
	lines = append(lines, p.agentRows(active)...)

	if len(subs) > 0 {
		lines = append(lines, "", "\x1b[38;2;154;160;166mTip: Press enter on a subagent to view details.\x1b[39m")
	}

	lines = append(lines,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Select / Toggle",
			"k", "Kill Active Subagent", "esc", "Go Back"),
	)
	return lines
}

func taskFromSubagent(sa *subagent.Subagent) *taskregistry.Task {
	if sa == nil {
		return nil
	}
	name := sa.Name
	if name == "" {
		name = sa.Title
	}
	state := taskregistry.StateRunning
	switch strings.ToLower(sa.State) {
	case "running":
		state = taskregistry.StateRunning
	case "done", "completed":
		state = taskregistry.StateDone
	case "failed", "error":
		state = taskregistry.StateFailed
	case "killed", "canceling", "cancelled":
		state = taskregistry.StateKilled
	case "stopped":
		state = taskregistry.StateStopped
	case "pending":
		state = taskregistry.StatePending
	}
	var steps []string
	if sa.Result != "" {
		steps = []string{sa.Result}
	}
	prompt := sa.Title
	if prompt == "" {
		prompt = sa.Name
	}
	return &taskregistry.Task{
		ID:     sa.ID,
		Name:   name,
		Kind:   taskregistry.KindSubagent,
		State:  state,
		Model:  sa.Model,
		Effort: sa.Effort,
		Prompt: prompt,
		Output: sa.Output(),
		Err:    sa.Err,
		Steps:  steps,
	}
}

// BuildSubagentDetail renders the reference's subagent-detail view for task t:
// five header lines and scrollable body rows according to AGY 1.5.9 decompilation.
func BuildSubagentDetail(t *taskregistry.Task, width int, expanded bool, pendingAuth ...string) (header, body []string) {
	if t == nil {
		return []string{"  (no subagent data)"}, []string{"  (no subagent data)"}
	}
	w := width
	if w <= 0 {
		w = 80
	}

	ruleWidth := 60
	if w-4 < ruleWidth {
		ruleWidth = w - 4
		if ruleWidth < 10 {
			ruleWidth = 10
		}
	}
	ruleColor := "\x1b[38;2;60;64;67m"
	rule := "  " + ruleColor + strings.Repeat("─", ruleWidth) + "\x1b[39m"

	typeName := t.Name
	if idx := strings.Index(typeName, ":"); idx != -1 {
		typeName = strings.TrimSpace(typeName[:idx])
	}
	if strings.HasPrefix(typeName, "Agent(") && strings.HasSuffix(typeName, ")") {
		typeName = typeName[6 : len(typeName)-1]
		if idx := strings.Index(typeName, ":"); idx != -1 {
			typeName = strings.TrimSpace(typeName[:idx])
		}
	}

	stateWord := taskregistry.SubagentStateWord(t.State)
	stateColor := taskregistry.SubagentStateColor(t.State)

	model := strings.TrimSpace(t.Model)
	if effort := strings.TrimSpace(t.Effort); effort != "" && !strings.Contains(model, "(") {
		model = fmt.Sprintf("%s (%s)", model, strings.Title(effort))
	}
	if model == "" {
		model = "Gemini 3.8 Flash (High)"
	}

	elapsed := "worked 0s"
	if !t.StartedAt.IsZero() {
		dur := time.Since(t.StartedAt)
		if !t.EndedAt.IsZero() {
			dur = t.EndedAt.Sub(t.StartedAt)
		}
		secs := int(dur.Seconds())
		elapsed = fmt.Sprintf("worked %ds", secs)
	}

	// Load trajectory steps from transcript or task fallback
	trajSteps, totalSteps := LoadTrajectoryFromTranscript(t.ID)
	if len(trajSteps) == 0 {
		trajSteps = ParseFallbackSteps(t.Steps, t.Output, t.StepOutputs...)
	} else if len(t.StepOutputs) > 0 {
		toolIdx := 0
		for i := range trajSteps {
			if trajSteps[i].Kind == "tool" {
				if trajSteps[i].Output == "" && toolIdx < len(t.StepOutputs) {
					trajSteps[i].Output = t.StepOutputs[toolIdx]
				}
				toolIdx++
			}
		}
	}
	stepsCount := t.StepCount
	if stepsCount == 0 {
		if totalSteps > 0 {
			stepsCount = totalSteps
		} else if len(t.Steps) > 0 {
			stepsCount = len(t.Steps) + 1
		} else {
			stepsCount = 2
		}
	}

	statusBullet := stateColor + "● " + stateWord + palette.SeparatorClose
	metaLine := fmt.Sprintf("  %s · %s · %s · %d steps", statusBullet, model, elapsed, stepsCount)

	header = []string{
		"  \x1b[38;5;111mmain \xe2\x80\xba " + typeName + "\x1b[39m",
		metaLine,
		"  \x1b[38;5;109mID: " + t.ID + "\x1b[39m",
		rule,
		"",
	}

	for _, pa := range pendingAuth {
		if pa != "" {
			body = append(body,
				"  "+palette.WarningOpen+"Pending Approval: "+pa+palette.SeparatorClose,
				"",
			)
		}
	}

	body = append(body, "  "+panelBoldOpen+"Prompt"+panelBoldClose)
	prompt := strings.TrimSpace(t.Prompt)
	promptColor := "\x1b[38;5;146m"
	if prompt != "" {
		for _, raw := range strings.Split(prompt, "\n") {
			wrapped := strings.Split(ansi.Wrap(raw, w-4, ""), "\n")
			for _, wl := range wrapped {
				body = append(body, "  "+promptColor+wl+"\x1b[39m")
			}
		}
	} else {
		body = append(body, "  (no prompt available)")
	}
	body = append(body, rule, "", "  "+panelBoldOpen+"Tools"+panelBoldClose)
	toolsList := t.Tools
	if len(toolsList) == 0 {
		seenTool := make(map[string]bool)
		for _, s := range trajSteps {
			if s.Kind == "tool" && s.ToolName != "" && !seenTool[s.ToolName] {
				seenTool[s.ToolName] = true
				toolsList = append(toolsList, s.ToolName)
			}
		}
	}
	if len(toolsList) == 0 {
		body = append(body, "    \x1b[38;5;109mNo tools used yet\x1b[39m")
	} else {
		var toolNames []string
		seen := make(map[string]bool)
		for _, tool := range toolsList {
			name := FormatToolName(tool)
			if !seen[name] {
				seen[name] = true
				toolNames = append(toolNames, name)
			}
		}
		body = append(body, "    \x1b[38;5;146m"+strings.Join(toolNames, ", ")+"\x1b[39m")
	}

	if out := strings.TrimSpace(t.Output); out != "" {
		body = append(body, rule, "", "  "+panelBoldOpen+"Answer"+panelBoldClose)
		for _, raw := range strings.Split(out, "\n") {
			wrapped := strings.Split(ansi.Wrap(raw, w-4, ""), "\n")
			for _, wl := range wrapped {
				body = append(body, "  \x1b[38;2;232;234;237m"+wl+"\x1b[39m")
			}
		}
	}

	body = append(body, rule, "", "  "+panelBoldOpen+fmt.Sprintf("Trajectory — %s (%d steps)", typeName, stepsCount)+panelBoldClose, "")

	if len(trajSteps) > 0 {
		var trajRows []string
		lastCollapsibleIdx := -1
		for i := len(trajSteps) - 1; i >= 0; i-- {
			if trajSteps[i].Kind == "tool" || trajSteps[i].Kind == "thought" {
				lastCollapsibleIdx = i
				break
			}
		}

		hint := " \x1b[38;5;146m(ctrl+o to expand)\x1b[39m"
		if expanded {
			hint = " \x1b[38;5;146m(ctrl+o to collapse)\x1b[39m"
		}

		for i, s := range trajSteps {
			stepLines := RenderTrajectoryStep(s, w, expanded)
			if i == lastCollapsibleIdx && len(stepLines) > 0 {
				if !expanded {
					stepLines[0] += hint
				} else {
					stepLines[len(stepLines)-1] += hint
				}
			}
			trajRows = append(trajRows, stepLines...)
		}
		if lastCollapsibleIdx == -1 && len(trajRows) > 0 {
			trajRows[len(trajRows)-1] += hint
		}
		body = append(body, trajRows...)
	} else {
		body = append(body, "  No steps yet")
	}

	if t.Err != nil {
		body = append(body, "", fmt.Sprintf("  \x1b[38;5;196m⚠ Error — %q\x1b[39m", t.Err.Error()))
	}

	wrapList := func(list []string) []string {
		var out []string
		for _, l := range list {
			if strings.IndexByte(l, '\t') != -1 {
				l = strings.ReplaceAll(l, "\t", "    ")
			}
			if w > 0 && len(l) > w && ansi.StringWidth(l) > w {
				for _, wl := range strings.Split(ansi.Wrap(l, w, ""), "\n") {
					out = append(out, wl)
				}
			} else {
				out = append(out, l)
			}
		}
		return out
	}
	header = wrapList(header)
	body = wrapList(body)

	return header, body
}

func (p *Panel) detailLines() []string {
	sa := p.detailSubagent
	if sa == nil {
		return nil
	}
	var t *taskregistry.Task
	if p.seams.TaskLookup != nil {
		t = p.seams.TaskLookup(sa.ID)
	}
	if t == nil {
		t = taskFromSubagent(sa)
	}
	w, h := p.size()
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 30
	}

	var pendingAuth []string
	if appr, ok := p.pendingAuthorization(sa); ok {
		pendingAuth = append(pendingAuth, approvalSuffix(appr))
	}
	pendingAuthStr := strings.Join(pendingAuth, "|")

	taskErrStr := ""
	if t.Err != nil {
		taskErrStr = t.Err.Error()
	}

	transPath, transMtime, transSize := TranscriptFileStat(sa.ID)

	var header, body []string
	c := &p.detailCache
	if c.valid &&
		c.subagentID == sa.ID &&
		c.width == w &&
		c.expanded == p.expandedDetail &&
		c.pendingAuth == pendingAuthStr &&
		c.taskState == t.State &&
		c.taskStepCnt == t.StepCount &&
		c.taskStepsLen == len(t.Steps) &&
		c.taskOutLen == len(t.Output) &&
		c.taskErr == taskErrStr &&
		c.transPath == transPath &&
		c.transMtime.Equal(transMtime) &&
		c.transSize == transSize {
		header = c.header
		body = c.body
	} else {
		header, body = BuildSubagentDetail(t, w, p.expandedDetail, pendingAuth...)
		c.valid = true
		c.subagentID = sa.ID
		c.width = w
		c.expanded = p.expandedDetail
		c.pendingAuth = pendingAuthStr
		c.taskState = t.State
		c.taskStepCnt = t.StepCount
		c.taskStepsLen = len(t.Steps)
		c.taskOutLen = len(t.Output)
		c.taskErr = taskErrStr
		c.transPath = transPath
		c.transMtime = transMtime
		c.transSize = transSize
		c.header = header
		c.body = body
	}

	visible := h - len(header) - 3
	if visible < 4 {
		visible = 15
	}
	total := len(body)
	win := tuiviewport.ScrollWindow(total, p.window, visible)
	p.window = win.Start()
	lines := append([]string{}, header...)
	lines = append(lines, win.SliceRows(body)...)

	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	if total > visible {
		startLine := win.Start() + 1
		endLine := win.Start() + visible
		if endLine > total {
			endLine = total
		}
		lines = append(lines, fmt.Sprintf("  \x1b[38;5;109m(%d–%d of %d lines)\x1b[39m", startLine, endLine, total))
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, p.formatDetailFooterSingleLine())
	return lines
}

func (p *Panel) formatDetailFooterSingleLine() string {
	keyColor := "\x1b[1m\x1b[38;5;111m"
	labelColor := "\x1b[38;5;109m"
	reset := "\x1b[0m"
	resetColor := "\x1b[39m"

	expandDesc := "expand"
	if p != nil && p.expandedDetail {
		expandDesc = "collapse"
	}

	hints := []struct {
		key  string
		desc string
	}{
		{"↑/↓", "scroll"},
		{"pgup/pgdown", "page"},
		{"ctrl+end", "bottom"},
		{"ctrl+home", "top"},
		{"ctrl+o", expandDesc},
		{"shift+k", "kill"},
		{"esc", "back"},
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts, keyColor+h.key+reset+labelColor+" "+h.desc+resetColor)
	}
	return "  " + strings.Join(parts, labelColor+" · "+resetColor)
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
	kind, _, selectedIdx := p.classifyRow(p.cursor)
	cursor := -1
	if kind == rowAgent {
		cursor = selectedIdx
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

func cleanModelName(model string) string {
	if idx := strings.Index(model, " ("); idx != -1 {
		return model[:idx]
	}
	return model
}

func (p *Panel) formatSubagentChild(sa *subagent.Subagent) string {
	name := sa.Name
	if !strings.HasPrefix(name, "Agent(") {
		name = "Agent(" + name + ")"
	}
	if model := strings.TrimSpace(sa.Model); model != "" && p.width() >= subagentModelMinWidth {
		name += palette.DimGrayOpen + " · " + cleanModelName(model) + panelGray238C
	}
	status := sa.StatusLine()
	statusText := status + " · worked 0s"
	return fmt.Sprintf("%s  %s", name, statusText)
}

func formatSubagentCounts(running, done int) string {
	if running > 0 && done > 0 {
		return fmt.Sprintf("(%d running, %d done)", running, done)
	}
	if running > 0 {
		return fmt.Sprintf("(%d running)", running)
	}
	if done > 0 {
		return fmt.Sprintf("(%d done)", done)
	}
	return ""
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
		name := sa.Name
		if model := strings.TrimSpace(sa.Model); model != "" && p.width() >= subagentModelMinWidth {
			name += palette.DimGrayOpen + " · " + model + panelGray238C
		}
		line := prefix + fmt.Sprintf("%s  %s", name, sa.StatusLine())
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

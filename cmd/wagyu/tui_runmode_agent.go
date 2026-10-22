package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	piagent "github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agenttools"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

func pickerHiddenCommands() []string {
	if !IsBusinessAccount() {
		return nil
	}
	return []string{"/remote-control"}
}

func tokenKMid(tokens int) string {
	if tokens <= 0 {
		return "-"
	}
	if tokens < 1000 {
		return strconv.Itoa(tokens)
	}
	if tokens < 1_000_000 {
		kilo := float64(tokens) / 1000.0
		return strings.TrimSuffix(strconv.FormatFloat(kilo, 'f', 1, 64), ".0") + " K"
	}
	mega := float64(tokens) / 1_000_000.0
	return strings.TrimSuffix(strconv.FormatFloat(mega, 'f', 1, 64), ".0") + " M"
}

func compactDuration(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "0s"
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}

func (m *runMode) Init() tea.Cmd {
	cmds := []tea.Cmd{tas.Blink, armTasksTick(), fetchPlanTierCmd()}
	if m.statusLineExternal {
		cmds = append(cmds, armExternalStatusLineTick())
	}
	return tea.Batch(cmds...)
}

func (m *runMode) cycleMode() {
	next := permissions.NextMode(m.mode)
	m.mode = next
	if m.policy != nil {
		m.policy.Mode = next
	}
	m.input.Placeholder = permissions.ModeBanner(next)
	m.modeHintDismissed = false
}

func (m *runMode) EnableTools(decls []backend.ToolDecl, runner agent.ToolRunner) {
	m.tools = decls
	m.toolRunner = runner
}

func (m *runMode) moveWordLeft() bool {
	return m.moveWord(true)
}

func (m *runMode) moveWordRight() bool {
	return m.moveWord(false)
}

func (m *runMode) moveWord(back bool) bool {
	row := m.input.Line()
	col := m.input.Column()
	val := m.input.Value()
	var newRow, newCol int
	var moved bool
	if back {
		newRow, newCol, moved = wordMoveLeft(val, row, col)
	} else {
		newRow, newCol, moved = wordMoveRight(val, row, col)
	}
	if !moved {
		return false
	}
	for row > newRow {
		m.input.CursorUp()
		row--
	}
	for row < newRow {
		m.input.CursorDown()
		row++
	}
	m.input.SetCursorColumn(newCol)
	return true
}

func agentToolRegistry(workspace, agentBody string) *tools.Registry {
	catalog := tools.RuntimeCatalog(workspace)
	if agentBody != "" {
		return agenttools.Registry(catalog)
	}
	return catalog
}

func (m *runMode) registerSubagentQuestionTool(catalog *tools.Registry) {
	if m.agentBody != "" {
		return
	}
	catalog.Register(&tools.AskQuestion{
		SubAgentQuestions: m.subAgentQuestions,
		SubAgentID:        subagentquestions.ParentKey,
	})
}

func (m *runMode) applyAgentSwitch(name, body string) {
	m.saveSession()
	m.agentName = name
	m.agentBody = body
	workspace := m.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	if m.policy != nil {
		catalog := agentToolRegistry(workspace, body)
		m.registerSubagentQuestionTool(catalog)
		m.EnableTools(advertisedToolDecls(body, catalog), &permissionRunner{
			inner:   &registryRunner{reg: catalog},
			policy:  m.policy,
			sandbox: m.toolSandbox,
		})
	}
	m.response = nil
	m.session = nil
	m.blocks = nil
	m.liveSteps = nil
	m.activeTool = ""
	m.autoTitle = ""
	m.conversationID = newConversationID()
}

func (m *runMode) subAgentExtras(_ context.Context, be backend.Client, modelLabel string) ([]piagent.AgentTool, func()) {
	if be == nil || m.agentBody != "" {
		return nil, func() {}
	}
	piModel := pigPrintModel(be, modelLabel)
	if piModel == nil {
		return nil, func() {}
	}
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	agentDir := auth.AntigravityConfigHome()
	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      workspace,
		AgentDir: agentDir,
	})
	if err != nil {
		printModeSilenceLogger.Printf("[tui] subagent services: %v", err)
		return nil, func() {}
	}
	subTools := subagenttools.Tools(subagenttools.Deps{
		Services: services,
		Model:    piModel,
		CWD:      workspace,
		AgentDir: agentDir,
	})
	closeFn := func() { services.Close() }
	return subTools, closeFn
}

func defaultWorkspaceForRunMode() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

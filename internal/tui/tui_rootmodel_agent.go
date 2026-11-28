package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/agenttools"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	builtinsubagent "github.com/justwasm/pigasus/internal/app/subagent/builtin"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/browser"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/app/tools/tasks"
	terminaltool "github.com/justwasm/pigasus/internal/app/tools/terminal"
	"github.com/justwasm/pigasus/internal/app/tools/workflow"
	"github.com/justwasm/pigasus/internal/app/tuistore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	"github.com/justwasm/pigasus/internal/infra/backend/backendselect"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/modelargpopup"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/internal/ui/views/wirehud"
)

// ---- tui_rootmodel_agent.go ----

func pickerHiddenCommands() []string {
	if !auth.IsBusinessAccount() {
		return nil
	}
	return []string{"/remote-control"}
}

func (m *RootModel) Init() tea.Cmd {
	cmds := []tea.Cmd{tas.Blink, m.ensureTasksTick(), m.fetchPlanTierCmd(), m.warmCredentialCmd()}
	if m.statusLineScriptActive() || m.statusLine.titleCfg.Active() {
		m.session.store.StatusLineTickArmed = true
		cmds = append(cmds, armExternalStatusLineTick())
	}
	if m.prompt.initialPrompt != "" {
		p := m.prompt.initialPrompt
		m.prompt.initialPrompt = ""
		cmds = append(cmds, m.submitInitialPrompt(p))
	}
	return tea.Batch(cmds...)
}

func (m *RootModel) submitInitialPrompt(prompt string) tea.Cmd {
	prompt = PrependSlashPlanIfNeeded(prompt, m.session.mode)
	if handled, cmd := m.dispatchPromptPrefix(prompt); handled {
		return cmd
	}
	return m.startModelTurn(prompt)
}

func (m *RootModel) cycleMode() {
	var next permissions.AgentMode
	if m.session.remoteAgent {
		next = permissions.NextRemoteAgentMode(m.session.mode)
	} else {
		next = permissions.NextMode(m.session.mode)
	}
	m.session.mode = next
	if m.session.policy != nil {
		m.session.policy.Mode = next
	}
	m.prompt.input.Placeholder = m.promptPlaceholder(next)
	m.prompt.modeHintDismissed = false
}

func (m *RootModel) enableTools(decls []backend.ToolDecl, runner backend.ToolRunner) {
	m.tools.decls = decls
	m.tools.runner = runner
}

func (m *RootModel) moveWord(back bool) bool {
	seams := m.composerSeams()
	if back {
		return composer.HandleWordLeft(seams)
	}
	return composer.HandleWordRight(seams)
}

func (m *RootModel) reloadPluginsAndMCP() {
	if m == nil {
		return
	}
	workspace := m.session.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	setup.InstallMCPProvider(auth.AntigravityHome(), workspace)
	catalog := agentToolRegistry(workspace, m.session.agentBody)
	if m.tools.catalog != nil {
		m.tools.catalog.ReplaceWith(catalog)
	} else {
		m.tools.catalog = catalog
	}
	m.registerSubagentQuestionTool(m.tools.catalog)
	m.rebuildToolSurface()
}

// rebuildToolSurface re-derives the advertised declarations and the
// tool runner from what the session already holds: the catalogue, the
// sandbox and the policy.
//
// It is the half of reloadPluginsAndMCP that a mid-session state change
// needs. The other half — reading the MCP configuration from the
// config home and replacing the process-global provider — is a plugin
// reload's business, not a permission switch's, and doing it for every
// state change would re-read the user's files from paths the command
// has no reason to touch.
//
// The two values that must be re-derived together are the declarations
// (AdvertisedToolDeclsWithSandbox decides whether run_command offers
// the bypass-sandbox parameter) and the runner (it holds a *copy* of
// the sandbox). Rebuilding only one leaves the surface describing a
// sandbox it does not have.
func (m *RootModel) rebuildToolSurface() {
	if m == nil || m.tools.catalog == nil {
		return
	}
	decls := toolrunner.AdvertisedToolDeclsWithSandbox(m.session.agentBody, m.tools.catalog, m.tools.sandbox.Enabled)
	decls = m.registerTerminalTool(m.tools.catalog, decls)
	decls = m.registerUpdateStepsTool(m.tools.catalog, decls)
	m.enableTools(decls, m.newToolRunner())
}

// newToolRunner builds the runner that gates this session's tool calls.
//
// One owner for the wiring: the policy, the sandbox copy, the hooks and
// the voice seam. It used to be assembled at three sites, and they had
// drifted — the plugin-reload one had lost the voice notification seam.
func (m *RootModel) newToolRunner() backend.ToolRunner {
	if m.session.policy == nil {
		return &toolrunner.RegistryRunner{Reg: m.tools.catalog}
	}
	return m.permissionRunnerFor(m.tools.catalog)
}

// permissionRunnerFor builds the gated runner over a specific
// catalogue. applyAgentSwitch hands in the catalogue it just built; the
// ordinary paths pass m.tools.catalog.
func (m *RootModel) permissionRunnerFor(catalog *tools.Registry) *permissionRunner {
	return &permissionRunner{
		inner:    &toolrunner.RegistryRunner{Reg: catalog},
		policy:   m.session.policy,
		sandbox:  m.tools.sandbox,
		hook:     m.hookToolGate,
		post:     m.hookPostToolUse,
		onPrompt: m.onPermissionPrompt(),
	}
}

// onPermissionPrompt is the voice seam: when a permission prompt opens,
// a dictate-mode session says so out loud. Nil when the session has no
// voice surface, which keeps the runner's nil check meaningful.
func (m *RootModel) onPermissionPrompt() func(permissionprompt.Request) {
	return func(req permissionprompt.Request) {
		m.notifyVoiceWaitingOnUser(formatPermissionForVoice(req))
	}
}

func agentToolRegistry(workspace, agentBody string) *tools.Registry {
	catalog := surface.RuntimeCatalog(workspace)
	if agentBody != "" {
		return agenttools.Registry(catalog)
	}
	return catalog
}

func (m *RootModel) registerSubagentQuestionTool(catalog *tools.Registry) {
	if m.session.agentBody != "" {
		return
	}
	catalog.Register(&tasks.AskQuestion{
		Host:              m.turnInteractionHost(),
		SubAgentQuestions: m.askQuestion.manager,
		SubAgentID:        subagentquestions.ParentKey,
		OnAsk:             m.requestAskQuestion,
	})
}

func (m *RootModel) registerTerminalTool(catalog *tools.Registry, decls []backend.ToolDecl) []backend.ToolDecl {
	if m == nil || m.session.agentBody != "" {
		return decls
	}
	if catalog != nil {
		catalog.Register(&terminaltool.Tool{Manager: m.terminalBackend()})
	}
	for _, d := range decls {
		if d.Name == terminaltool.ToolName {
			return decls
		}
	}
	return append(decls, terminaltool.MinimalDecl())
}

func (m *RootModel) registerUpdateStepsTool(catalog *tools.Registry, decls []backend.ToolDecl) []backend.ToolDecl {
	if m == nil || m.session.agentBody != "" {
		return decls
	}
	if catalog != nil {
		catalog.Register(&tasks.UpdateSteps{})
	}
	for _, d := range decls {
		if d.Name == tasks.UpdateStepsToolName {
			return decls
		}
	}
	return append(decls, tasks.UpdateStepsDecl())
}

// requestAskQuestion sends the open request to the running program.
//
// The tool calls this from the turn's goroutine. With no program (unit
// tests, print mode) there is no UI to open, so it is a no-op.
func (m *RootModel) requestAskQuestion(scope string) {
	if m == nil || m.turn.prog == nil {
		return
	}
	m.turn.prog.Send(askQuestionPendingMsg{scope: scope})
}

func (m *RootModel) applyAgentSwitch(name, body string) {
	m.saveSession()
	m.session.agentName = name
	m.session.agentBody = body
	workspace := m.session.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	if m.session.policy != nil {
		catalog := agentToolRegistry(workspace, body)
		m.registerSubagentQuestionTool(catalog)
		decls := toolrunner.AdvertisedToolDeclsWithSandbox(body, catalog, m.tools.sandbox.Enabled)
		decls = m.registerTerminalTool(catalog, decls)
		decls = m.registerUpdateStepsTool(catalog, decls)
		m.enableTools(decls, &permissionRunner{
			inner:    &toolrunner.RegistryRunner{Reg: catalog},
			policy:   m.session.policy,
			sandbox:  m.tools.sandbox,
			hook:     m.hookToolGate,
			post:     m.hookPostToolUse,
			onPrompt: m.onPermissionPrompt(),
		})
	}
	m.conversation.response = nil
	m.session.bumpEpoch()
	if sup := m.session.sup(); sup != nil {
		sup.Clear()
	}
	m.conversation.blocks = nil
	m.conversation.liveSteps = nil
	m.conversation.activeTool = ""
	m.forgetConversationState()
	m.session.autoTitle = ""
	m.session.conversationID = newConversationID()
	// The conversation changed, so the session did too: the documented
	// SessionStart event fires for the new one.
	m.session.hookSessionStarted = false
	// The definitions belong to the conversation that just ended.
	m.agents.defs = subagenttools.NewStore()
	m.resetTrajectory()
}

// tuiSilenceLogger is the destination for the recovered subagent debug-log lines.
var tuiSilenceLogger = log.New(io.Discard, "", 0)

func (m *RootModel) subAgentExtras(_ context.Context, be backend.Client, modelLabel string) ([]actor.Tool, func()) {
	imageProvider := surface.ImageProviderFromBackend(backendselect.ImageGeneratorFor(be))
	if m.tools.catalog != nil {
		surface.ConfigureConversation(m.tools.catalog, m.session.conversationID)
		surface.ConfigureImageGenerator(m.tools.catalog, imageProvider)
	}
	if be == nil || m.session.agentBody != "" {
		return nil, func() {}
	}
	workspace := m.session.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRootModel()
	}
	agentDir := confhome.Antigravity()
	provider := agentturn.NewBackendProviderAdapter(be, modelLabel)
	catalogue := allCatalogActorTools(m.tools.catalog)
	if len(catalogue) == 0 && m.tools.catalog != nil {
		catalogue = browsersubagent.BrowserTools(m.tools.catalog)
	}
	subDeps := subagenttools.Deps{
		Provider:          provider,
		Backend:           be,
		Model:             modelLabel,
		Effort:            m.currentEffortName(),
		CWD:               workspace,
		AgentDir:          agentDir,
		Definitions:       m.agents.defs,
		Tasks:             m.tasks.reg,
		Registry:          m.ensureSubagentRegistry(),
		ConversationID:    m.session.conversationID,
		PermissionGateFor: m.subagentGateFactory,
		SystemPrompt:      m.systemPrompt,
		Catalogue:         catalogue,
		Questions:         m.askQuestion.manager,
		OnAsk:             m.requestAskQuestion,
		ImageGenerator:    imageProvider,
	}
	subTools := subagenttools.Tools(subDeps)
	subDir := filepath.Join(agentDir, "subagents")
	_, _ = subagenttools.RehydrateSubagents(subDir, subDeps.Registry, m.tasks.reg, subDeps)
	if m.tools.catalog != nil {
		browser.SetBrowserSubagentProvider(browsersubagent.New(subagenttools.Deps{
			Provider: provider,
			Backend:  be,
			Model:    modelLabel,
			CWD:      workspace,
			AgentDir: agentDir,
		}, browsersubagent.Tools(m.tools.catalog)))
		reg := builtinsubagent.Default()
		if err := browsersubagent.InstallBuiltin(reg, subDeps, m.tools.catalog); err != nil {
			tuiSilenceLogger.Printf("[tui] browser built-in: %v", err)
		} else {
			subagenttools.SetBuiltinSubagentRegistry(reg)
		}
		surface.ConfigureWorkflow(m.tools.catalog, func(wf *workflow.RunWorkflow) {
			wf.ConversationID = m.session.conversationID
			wf.Workspaces = []string{workspace}
			wf.AgentExecutor = func(ctx context.Context, req workflow.AgentRequest) (any, error) {
				if req.Close {
					return nil, nil
				}
				prompt := req.Prompt
				if len(req.Schema) > 0 && string(req.Schema) != "null" {
					prompt += "\n\nRespond with valid JSON matching this schema:\n" + string(req.Schema)
				}
				typeName := req.TypeName
				if typeName == "" {
					typeName = "self"
				}
				res, err := subagenttools.Invoke(ctx, subagenttools.Invocation{
					Prompt:   prompt,
					TypeName: typeName,
					Role:     req.Role,
					Model:    req.Model,
				}, subDeps)
				if err != nil {
					return nil, err
				}
				if len(req.Schema) > 0 && string(req.Schema) != "null" {
					var parsed any
					if json.Unmarshal([]byte(strings.TrimSpace(res.Text)), &parsed) == nil {
						return parsed, nil
					}
				}
				return res.Text, nil
			}
			wf.TrajectoryProvider = func(_ context.Context, filter workflow.TrajectoryFilter) ([]workflow.TrajectoryStep, error) {
				rawSteps := convstore.BuildSteps(m.trajectoryFacts())
				out := make([]workflow.TrajectoryStep, 0, len(rawSteps))
				for _, st := range rawSteps {
					idx := int(st.Idx)
					if filter.MinIndex != nil && idx < *filter.MinIndex {
						continue
					}
					if filter.MaxIndex != nil && idx > *filter.MaxIndex {
						continue
					}
					typ := workflowStepType(st.Type)
					if len(filter.Types) > 0 && !containsFold(filter.Types, typ) {
						continue
					}
					if len(filter.Tools) > 0 && !containsFold(filter.Tools, st.ToolName) {
						continue
					}
					var argsMap map[string]any
					if st.ToolArgs != "" {
						_ = json.Unmarshal([]byte(st.ToolArgs), &argsMap)
					}
					out = append(out, workflow.TrajectoryStep{
						Index:  idx,
						Type:   typ,
						Status: workflowStepStatus(st.Status),
						Text:   st.Text,
						Tool:   st.ToolName,
						Args:   argsMap,
						Output: st.ToolResult,
					})
				}
				if filter.Last > 0 && len(out) > filter.Last {
					out = out[len(out)-filter.Last:]
				}
				return out, nil
			}
		})
	}
	return subTools, func() {}
}

func workflowStepType(t int64) string {
	switch t {
	case convstore.StepUserInput:
		return "user_input"
	case convstore.StepModelResponse:
		return "planner_response"
	case convstore.StepToolCall:
		return "tool_call"
	case convstore.StepSystemNotice:
		return "system_notice"
	default:
		return "unknown"
	}
}

func workflowStepStatus(s int64) string {
	switch s {
	case 0:
		return "done"
	default:
		return "done"
	}
}

func containsFold(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}

func defaultWorkspaceForRootModel() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func allCatalogActorTools(reg *tools.Registry) []actor.Tool {
	if reg == nil {
		return nil
	}
	_, hasReadFile := reg.Get("read_file")
	_, hasWriteFile := reg.Get("write_file")
	_, hasEditFile := reg.Get("edit_file")

	var out []actor.Tool
	for _, name := range reg.List() {
		// Filter out obsolete 1.3.6 tools and duplicate legacy aliases
		// so subagents receive modern, lean tool declarations.
		switch name {
		case "command_status", "wait", "notebook_edit", "notebook_execution", "sed_file":
			continue
		case "view_file":
			if hasReadFile {
				continue
			}
		case "write_to_file":
			if hasWriteFile {
				continue
			}
		case "replace_file_content", "multi_replace_file_content":
			if hasEditFile {
				continue
			}
		}

		tool, ok := reg.Get(name)
		if !ok || tool == nil {
			continue
		}
		var params map[string]any
		desc := tool.Name()
		if raw := tool.Schema(); len(raw) > 0 {
			_ = json.Unmarshal(raw, &params)
			if d, ok := params["description"].(string); ok && d != "" {
				desc = d
			}
		}
		t := tool
		out = append(out, actor.NewFuncTool(t.Name(), desc, func(ctx context.Context, input string) (string, error) {
			res, err := t.Run(ctx, json.RawMessage(input))
			if err != nil {
				return "", err
			}
			if s, ok := res.(string); ok {
				return s, nil
			}
			b, _ := json.Marshal(res)
			return string(b), nil
		}, actor.WithParameters(params)))
	}
	return out
}

// newConversationID returns an RFC-4122-style 8-4-4-4-12 hex UUID
// for the current session.
func newConversationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		ts := uint64(time.Now().UnixNano())
		for i := 0; i < 16; i++ {
			b[i] = byte(ts >> (uint(i) * 4))
		}
	}
	hex := hex.EncodeToString(b[:])
	return hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32]
}

func resolveRootModelDefaults(b backend.Client, email, model, tier, cwd string) (string, string, string, string) {
	if email == "" {
		email = auth.EffectiveUserEmail()
	}
	if email == "" {
		_, email = accounts.Current()
	}
	if b != nil && model == "" {
		if gb, ok := b.(gatewayBackend); ok {
			if label, ok := modelcatalog.WireLabel(gb.GatewayConfig().ExternalModel); ok {
				model = label
			}
		}
	}
	if model != "" && !backendselect.IsReferenceModel(model, modelpicker.ResolveCatalogueID) {
		// A configured model keeps its own label: the label has to name
		// the one the endpoint is serving, not collapse to another.
		model = modelcatalog.FormatModelLabel(model)
	}
	if model == "" {
		model = "Gemini 3.8 Flash (High)"
	}
	if cwd == "" {
		if c, err := os.Getwd(); err == nil {
			cwd = c
		} else {
			cwd = "/"
		}
	}
	// The tier is returned as it came in: it stays empty until the
	// server names one, so the banner shows the email alone. A
	// placeholder would flash into the real tier under the user's eyes.
	return email, model, tier, cwd
}

func newRootModelBase(b backend.Client, email, model, tier, cwd string, altScreen bool) *RootModel {
	tuilib.SetWorkspaceRoot(cwd)
	initConvID := newConversationID()
	initialMode := setup.DefaultMode()
	domainSess := session.New(session.Config{
		ID:    initConvID,
		Model: model,
		Mode:  initialMode,
		CWD:   cwd,
	})
	m := &RootModel{
		session: sessionState{
			domain:                domainSess,
			email:                 email,
			model:                 model,
			statusModel:           tuilib.StripTier(model),
			tier:                  tier,
			cwd:                   cwd,
			store:                 *tuistore.New(),
			conversationID:        initConvID,
			initialConversationID: initConvID,
			narrationStart:        time.Now(),
			wireSettingsPending:   true,
			hooks:                 hooks.NewManager(),
			mode:                  permissions.AgentMode(initialMode),
			startMode:             permissions.AgentMode(initialMode),
		},
		turn: TurnService{
			backend: b,
		},
		display: DisplayModel{
			streamingSearch: true,
			verbosity:       settings.String("verbosity"),
		},
		agents: AgentsModel{
			defs:     domainSess.SubagentDefs(),
			registry: domainSess.SubagentRegistry(),
			router:   defaultRouter,
		},
		tasks: TasksModel{
			reg:             tools.SharedTaskRegistry(),
			ctrl:            taskregistry.NewController(tools.SharedTaskRegistry()),
			subagentManager: tools.SharedSubagentManager(),
			inbox:           agentturn.NewTaskInbox(),
		},
		viewport: uiState{
			altScreen:          altScreen,
			autoScroll:         true,
			rowsCacheDirty:     true,
			pinnedPromptRowIdx: -1,
		},
		overlays: OverlayModel{
			activeBtwIndex: -1,
		},
		conversation: ConversationModel{
			replyStart:        -1,
			liveReplyIdx:      -1,
			liveThoughtRunIdx: -1,
			voiceReplyStart:   -1,
		},
		askQuestion: AskQuestionModel{
			manager: subagentquestions.NewManager(false),
		},
	}
	m.prompt.picker = m.newSlashPicker()
	// The composer's Vim bridge is built with the host's seams once, so
	// the engine and the two settings live between keys.
	m.prompt.SetVimBridge(m.newVimBridge())
	m.activeItems.Panel = m.newActiveItemsPanel()
	m.prompt.atFilePicker = m.newMentionPicker()
	m.overlays.resumePicker = resumepicker.New(resumepicker.Seams{
		CurrentConversationID: func() string { return m.session.conversationID },
		CurrentWorkspace:      func() string { return m.session.cwd },
		ListConversations:     m.listResumeConversations,
		ResumeConversation:    m.resumeConversation,
		RenameConversation:    m.renameResumeConversation,
	})
	m.askQuestion.panel = m.newSubAgentQuestionPanel()
	m.help.Panel = m.newHelpPanel()
	m.audio.Surface = m.newVoiceSurface()
	m.swarm.Board = m.newSwarmBoard()
	m.overlays.wireHUD = wirehud.New()
	return m
}

func initRootModelInput(m *RootModel) {
	m.prompt.input = tas.New()
	m.prompt.input.SetVirtualCursor(false)
	m.prompt.input.Prompt = inputPrompt
	m.prompt.input.SetPromptFunc(2, func(info tas.PromptInfo) string {
		if info.LineNumber == 0 {
			if m.session.store.BashMode {
				return "! "
			}
			return "> "
		}
		return "  "
	})
	m.prompt.input.Placeholder = m.promptPlaceholder(m.session.mode)
	m.prompt.input.ShowLineNumbers = false
	m.prompt.input.SetWidth(inputWidth)
	m.prompt.input.SetHeight(1)
	m.prompt.input.MaxHeight = 24
	m.prompt.input.CharLimit = 0
	m.prompt.input.EndOfBufferCharacter = ' '
	m.prompt.input.DynamicHeight = true
	m.prompt.input.MinHeight = 1

	// The prompt wears the selected scheme's accent and the placeholder
	// its dim foreground; the colours follow the scheme, so they are
	// resolved from it rather than hard-coded.
	initScheme := settings.String("colorScheme")
	// The session wears the configured scheme from the first frame, so
	// the whole interface is painted in it — the reference resolves
	// every colour from the selected scheme.
	m.viewport.colorScheme = initScheme
	promptColor, placeholderColor := themepreview.ColorsFor(initScheme)
	promptBlueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
	placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
	st := m.prompt.input.Styles()
	st.Focused.Base = lipgloss.NewStyle()
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Focused.Text = lipgloss.NewStyle()
	st.Focused.Prompt = promptBlueStyle
	st.Focused.Placeholder = placeholderStyle
	st.Focused.EndOfBuffer = lipgloss.NewStyle()
	st.Blurred.Base = lipgloss.NewStyle()
	st.Blurred.Text = lipgloss.NewStyle()
	st.Blurred.Prompt = promptBlueStyle
	st.Blurred.Placeholder = placeholderStyle
	st.Blurred.EndOfBuffer = lipgloss.NewStyle()
	m.prompt.input.SetStyles(st)
	m.prompt.input.Focus()
	// The prompt styles above are built from initScheme directly, so the
	// only thing left to carry the scheme is the markdown renderer's own
	// global. It is set here deliberately and alone.
	//
	// applyThemeLive is deliberately NOT called: besides mdrender it
	// would also SetScheme the palette's process-global colours, which is
	// only licensed *inside* a frame — Update and View each PushScheme and
	// pop it, so a frame paints in the selected scheme and the package
	// keeps its default colours outside one. A constructor that also set
	// the palette global leaves the whole process in the configured
	// scheme, for every later render that happens outside a frame and for
	// every later test in the same binary.
	mdrender.SetColorScheme(initScheme)
}

// newRootModel constructs the v1.3.5 binary 1:1 run mode.
func newRootModel(b backend.Client, email, model, tier, cwd string, altScreen bool) *RootModel {
	pickerHidden := pickerHiddenCommands()
	email, model, tier, cwd = resolveRootModelDefaults(b, email, model, tier, cwd)
	m := newRootModelBase(b, email, model, tier, cwd, altScreen)
	if settings.String("queuedMessages") == "send-immediately" {
		m.session.store.QueueMode = false
	} else {
		m.session.store.QueueMode = true
	}

	if kb, err := keybind.Load(keybind.DefaultPath()); err == nil {
		m.prompt.keybindings = kb
	}
	m.startTasksSubscription()
	initRootModelInput(m)

	m.conversation.spinner = sp.New(sp.WithSpinner(sp.Dot))
	// 【一手】 agy 1.4.7: with a seasonal event switched on the
	// streaming spinner is the event's own — the Halloween event shows
	// a four-frame moon phase ("◐  Generating…").
	m.refreshSeasonalTheme()
	if len(pickerHidden) > 0 {
		m.prompt.picker.SetHiddenCommands(pickerHidden...)
	}
	// The "/model <query>" popup filters the catalogue the CLI can
	// select: the reference's own models plus the ones the user
	// configured.
	m.prompt.picker.SetModelCatalogueSource(modelargpopup.Rows)
	return m
}

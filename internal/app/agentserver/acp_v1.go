package agentserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	schemav1 "github.com/ironpark/acp-go/schema/v1"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
)

type acpV1Session struct {
	cwd      string
	mode     string
	session  *Session
	history  []agentlink.Message
	commands []acp1.AvailableCommand
	mu       sync.Mutex
}

var _ acp1.SessionModesReporter = (*acpV1Session)(nil)
var _ acp1.SessionConfigOptionsReporter = (*acpV1Session)(nil)
var _ acp1.SessionCommandsReporter = (*acpV1Session)(nil)

func (s *acpV1Session) SessionInfo() acp1.SessionInfo {
	return acp1.SessionInfo{Cwd: s.cwd}
}

func (s *acpV1Session) SessionModes() *acp1.SessionModeState {
	s.mu.Lock()
	m := s.mode
	s.mu.Unlock()
	return buildV1Modes(m)
}

func (s *acpV1Session) SessionConfigOptions() []acp1.SessionConfigOption {
	return buildV1ConfigOptions(s)
}

func (s *acpV1Session) AvailableCommands() []acp1.AvailableCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.commands) == 0 {
		return defaultV1Commands()
	}
	return s.commands
}

func (s *acpV1Session) applyMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
	if s.session != nil {
		if pr, ok := s.session.Runner.(*toolrunner.PermissionRunner); ok && pr != nil && pr.Policy != nil {
			switch mode {
			case "auto":
				pr.Policy.Mode = permissions.ModeAuto
			case "accept-edits":
				pr.Policy.Mode = permissions.ModeAcceptEdits
			case "plan":
				pr.Policy.Mode = permissions.ModePlan
			default:
				pr.Policy.Mode = ""
			}
		}
	}
}

func defaultV1Commands() []acp1.AvailableCommand {
	return []acp1.AvailableCommand{
		{Name: "/model", Description: "Set a model, or run a single prompt on another model"},
		{Name: "/clear", Description: "Clear conversation and start a new one"},
		{Name: "/plan", Description: "Plan carefully before executing a task"},
		{Name: "/goal", Description: "Run until the specified goal is completely finished"},
		{Name: "/tasks", Description: "View background tasks"},
		{Name: "/agents", Description: "List available custom agents"},
		{Name: "/context", Description: "Visualize current context usage"},
	}
}

// ACPV1Agent implements the stable ACP v1 Agent interface.
type ACPV1Agent struct {
	*acp1.SessionManager[*acpV1Session]
	srv    *Server
	client acp1.Client
}

var _ acp1.Agent = (*ACPV1Agent)(nil)
var _ acp1.SessionLister = (*ACPV1Agent)(nil)
var _ acp1.SessionCloser = (*ACPV1Agent)(nil)
var _ acp1.SessionResumer = (*ACPV1Agent)(nil)
var _ acp1.SessionLoader = (*ACPV1Agent)(nil)
var _ acp1.SessionModeSetter = (*ACPV1Agent)(nil)
var _ acp1.SessionConfigOptionSetter = (*ACPV1Agent)(nil)
var _ acp1.Authenticator = (*ACPV1Agent)(nil)
var _ acp1.LogoutHandler = (*ACPV1Agent)(nil)

// NewACPV1Agent creates a new ACP v1 agent backed by Server.
func NewACPV1Agent(srv *Server, client acp1.Client) *ACPV1Agent {
	a := &ACPV1Agent{
		srv:    srv,
		client: client,
	}
	var store acp1.SessionStore[*acpV1Session]
	if srv != nil {
		store = srv.getACPV1Store()
	} else {
		store = acp1.NewMemoryStore[*acpV1Session]()
	}
	a.SessionManager = acp1.NewSessionManager(
		store,
		func(ctx context.Context, params *acp1.NewSessionRequest) (acp1.SessionID, *acpV1Session, error) {
			cwd := params.Cwd
			var sess *Session
			var err error
			if srv != nil {
				sess, err = srv.sessionFor(cwd, srv.cfg.Model)
				if err != nil {
					return "", nil, err
				}
			}
			var sessID acp1.SessionID
			if sess != nil && sess.ConversationID != "" {
				sessID = acp1.SessionID(sess.ConversationID)
			} else {
				sessID = acp1.SessionID(fmt.Sprintf("sess_%d", time.Now().UnixNano()))
			}
			if cwd == "" && sess != nil {
				cwd = sess.Workspace
			}
			var cmds []acp1.AvailableCommand
			if srv != nil {
				cmds = v1CommandsFor(srv.cfg)
			} else {
				cmds = defaultV1Commands()
			}
			return sessID, &acpV1Session{
				cwd:      cwd,
				mode:     "default",
				session:  sess,
				commands: cmds,
			}, nil
		},
	)
	return a
}

// Initialize implements acp1.Agent.
func (a *ACPV1Agent) Initialize(ctx context.Context, params *acp1.InitializeRequest) (*acp1.InitializeResponse, error) {
	caps := acp1.CapabilitiesOf(a)
	caps.LoadSession = ptr(true)
	caps.PromptCapabilities = &acp1.PromptCapabilities{
		Image:           ptr(true),
		Audio:           ptr(true),
		EmbeddedContext: ptr(true),
	}
	if caps.MCPCapabilities == nil {
		caps.MCPCapabilities = &acp1.MCPCapabilities{}
	}
	caps.MCPCapabilities.HTTP = ptr(true)
	caps.MCPCapabilities.SSE = ptr(true)
	if caps.SessionCapabilities == nil {
		caps.SessionCapabilities = &acp1.SessionCapabilities{}
	}
	caps.SessionCapabilities.AdditionalDirectories = &acp1.SessionAdditionalDirectoriesCapabilities{}
	caps.Meta = map[string]jsontext.Value{
		"antigravity": jsontext.Value(`{"tier":true,"systemPrompt":{"append":true}}`),
	}

	authMethods := []acp1.AuthMethod{
		acp1.NewAuthMethod(acp1.AuthMethodAgent{
			ID:          "oauth-personal",
			Name:        "Log in with Google",
			Description: ptr("Log in with your Google account"),
		}),
		acp1.NewAuthMethod(acp1.AuthMethodAgent{
			ID:          "oauth-business",
			Name:        "Log in with Gemini Enterprise",
			Description: ptr("Log in with your Gemini Enterprise account"),
		}),
		acp1.NewAuthMethod(acp1.AuthMethodAgent{
			ID:          "gemini-api-key",
			Name:        "Gemini API key",
			Description: ptr("Use an API key with Gemini Developer API"),
		}),
		acp1.NewAuthMethod(acp1.AuthMethodAgent{
			ID:          "agent-platform",
			Name:        "Gemini Enterprise Agent Platform",
			Description: ptr("Use Gemini Enterprise Agent Platform (formerly Vertex AI) with Application Default Credentials or an API key"),
		}),
	}

	return &acp1.InitializeResponse{
		ProtocolVersion: acp1.ProtocolVersion,
		AgentInfo: &acp1.Implementation{
			Name:    "antigravity-acp",
			Title:   ptr("Google Antigravity"),
			Version: Version,
		},
		AgentCapabilities: caps,
		AuthMethods:       authMethods,
	}, nil
}

func buildV1ConfigOptions(s *acpV1Session) []acp1.SessionConfigOption {
	currentModel := s.session.Model
	if currentModel == "" {
		currentModel = catalog.DefaultModel
	}
	currentEffort := s.session.Effort
	if currentEffort == "" {
		currentEffort = "high"
	}
	s.mu.Lock()
	currentMode := s.mode
	s.mu.Unlock()
	if currentMode == "" {
		currentMode = "default"
	}

	catModel := acp1.SessionConfigOptionCategoryModel
	catMode := acp1.SessionConfigOptionCategoryMode
	catThought := acp1.SessionConfigOptionCategoryThoughtLevel

	descModel := "Model to use for generation"
	descMode := "Permission mode for tool approvals"
	descEffort := "Thinking budget and reasoning effort"

	var modelChoices []acp1.SessionConfigSelectOption
	labelCounts := make(map[string]int)
	var rawModels []catalog.ModelEntry
	for _, m := range catalog.ModelCatalogue() {
		if strings.HasPrefix(m.ID, "MODEL_PLACEHOLDER_") || strings.Contains(m.ID, "PLACEHOLDER") {
			continue
		}
		labelCounts[m.Label]++
		rawModels = append(rawModels, m)
	}
	for _, m := range rawModels {
		displayName := m.Label
		if labelCounts[m.Label] > 1 {
			displayName = fmt.Sprintf("%s (%s)", m.Label, m.ID)
		}
		modelChoices = append(modelChoices, acp1.SessionConfigSelectOption{
			Value: acp1.SessionConfigValueID(m.ID),
			Name:  displayName,
		})
	}
	if len(modelChoices) == 0 {
		modelChoices = []acp1.SessionConfigSelectOption{
			{Value: "gemini-3.8-flash", Name: "Gemini 3.8 Flash"},
			{Value: "gemini-3.7-flash", Name: "Gemini 3.7 Flash"},
			{Value: "gemini-3.1-pro", Name: "Gemini 3.1 Pro"},
		}
	}

	modeChoices := []acp1.SessionConfigSelectOption{
		{Value: "default", Name: "Default"},
		{Value: "accept-edits", Name: "Accept Edits"},
		{Value: "auto", Name: "Auto"},
		{Value: "plan", Name: "Plan"},
	}

	effortChoices := []acp1.SessionConfigSelectOption{
		{Value: "low", Name: "Low"},
		{Value: "medium", Name: "Medium"},
		{Value: "high", Name: "High"},
	}

	return []acp1.SessionConfigOption{
		acp1.NewSessionConfigOption(acp1.SessionConfigOptionSelect{
			ID:           "model",
			Name:         "Model",
			Category:     &catModel,
			Description:  &descModel,
			CurrentValue: acp1.SessionConfigValueID(currentModel),
			Options:      acp1.SelectOptions(modelChoices...),
		}),
		acp1.NewSessionConfigOption(acp1.SessionConfigOptionSelect{
			ID:           "thinking_level",
			Name:         "Thinking Level",
			Category:     &catThought,
			Description:  &descEffort,
			CurrentValue: acp1.SessionConfigValueID(currentEffort),
			Options:      acp1.SelectOptions(effortChoices...),
		}),
		acp1.NewSessionConfigOption(acp1.SessionConfigOptionSelect{
			ID:           "mode",
			Name:         "Session Mode",
			Category:     &catMode,
			Description:  &descMode,
			CurrentValue: acp1.SessionConfigValueID(currentMode),
			Options:      acp1.SelectOptions(modeChoices...),
		}),
	}
}

func (a *ACPV1Agent) buildConfigOptions(s *acpV1Session) []acp1.SessionConfigOption {
	return buildV1ConfigOptions(s)
}

func buildV1Modes(currentMode string) *acp1.SessionModeState {
	if currentMode == "" {
		currentMode = "default"
	}
	descDefault := "Default permission prompt flow"
	descAcceptEdits := "Auto-approve file edits, prompt for commands"
	descAuto := "Auto-approve edits in workspace and commands in sandbox, ask before commands leaving sandbox"
	descPlan := "Research and plan without making changes"

	return &acp1.SessionModeState{
		CurrentModeID: acp1.SessionModeID(currentMode),
		AvailableModes: []acp1.SessionMode{
			{ID: "default", Name: "Default", Description: &descDefault},
			{ID: "accept-edits", Name: "Accept Edits", Description: &descAcceptEdits},
			{ID: "auto", Name: "Auto", Description: &descAuto},
			{ID: "plan", Name: "Plan", Description: &descPlan},
		},
	}
}

func (a *ACPV1Agent) buildModes(s *acpV1Session) *acp1.SessionModeState {
	s.mu.Lock()
	m := s.mode
	s.mu.Unlock()
	return buildV1Modes(m)
}

// NewSession implements acp1.Agent.
func (a *ACPV1Agent) NewSession(ctx context.Context, params *acp1.NewSessionRequest) (*acp1.NewSessionResponse, error) {
	resp, err := a.SessionManager.NewSession(ctx, params)
	if err != nil {
		return nil, err
	}
	s, _ := a.Lookup(ctx, resp.SessionID)
	if s != nil {
		resp.ConfigOptions = a.buildConfigOptions(s)
		resp.Modes = a.buildModes(s)
		resp.Meta = map[string]jsontext.Value{
			"tier": jsontext.Value(`"free"`),
		}
	}
	if a.client != nil {
		send := func(c context.Context) {
			stream := acp1.NewSessionStream(a.client, resp.SessionID)
			_ = stream.SendCommands(c, sessionCommandsV1(s))
		}
		if !acp.AfterReply(ctx, send) {
			send(ctx)
		}
	}
	return resp, nil
}

// SetSessionConfigOption implements acp1.SessionConfigOptionSetter.
func (a *ACPV1Agent) SetSessionConfigOption(ctx context.Context, params *acp1.SetSessionConfigOptionRequest) (*acp1.SetSessionConfigOptionResponse, error) {
	change, ok := acp1.ConfigChangeOf(params)
	if !ok {
		return nil, fmt.Errorf("invalid set_config_option request")
	}
	s, err := a.Lookup(ctx, change.SessionID)
	if err != nil {
		return nil, err
	}
	switch change.ConfigID {
	case "model":
		s.session.Model = string(change.Value)
	case "thinking_level":
		s.session.Effort = string(change.Value)
	case "mode":
		m := string(change.Value)
		if m == "auto_edit" || m == "yolo" {
			m = "accept-edits"
		}
		s.applyMode(m)
	}
	return &acp1.SetSessionConfigOptionResponse{
		ConfigOptions: a.buildConfigOptions(s),
	}, nil
}

// SetSessionMode implements acp1.SessionModeSetter.
func (a *ACPV1Agent) SetSessionMode(ctx context.Context, params *acp1.SetSessionModeRequest) (*acp1.SetSessionModeResponse, error) {
	s, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	m := string(params.ModeID)
	if m == "auto_edit" || m == "yolo" {
		m = "accept-edits"
	}
	s.applyMode(m)
	return &acp1.SetSessionModeResponse{}, nil
}

// LoadSession implements acp1.SessionLoader, restoring conversation state and replaying history for editors (Zed, VS Code, JetBrains).
func (a *ACPV1Agent) LoadSession(ctx context.Context, params *acp1.LoadSessionRequest) (*acp1.LoadSessionResponse, error) {
	if params == nil {
		return nil, fmt.Errorf("load session: nil params")
	}
	s, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		// External editor reconnection (e.g. Zed / VS Code): session might need restoration
		cwd := params.Cwd
		if cwd == "" && a.srv != nil {
			cwd = a.srv.cfg.Workspace
		}
		var sess *Session
		var sErr error
		if a.srv != nil {
			sess, sErr = a.srv.sessionFor(cwd, a.srv.cfg.Model)
			if sErr != nil {
				return nil, err
			}
		}
		recoveredHist := loadHistoryFromTranscript(string(params.SessionID))
		var cmds []acp1.AvailableCommand
		if a.srv != nil {
			cmds = v1CommandsFor(a.srv.cfg)
		} else {
			cmds = defaultV1Commands()
		}
		s = &acpV1Session{
			cwd:      cwd,
			mode:     "default",
			session:  sess,
			history:  recoveredHist,
			commands: cmds,
		}
		if saveErr := a.SessionManager.Save(ctx, params.SessionID, s); saveErr != nil {
			return nil, saveErr
		}
	}

	if params.Cwd != "" {
		s.cwd = params.Cwd
	}

	// Apply any initial config options requested by client
	for _, opt := range params.ConfigOptions {
		if u, ok := opt.As[schemav1.InitialSessionConfigOptionUntagged](); ok {
			switch u.ConfigID {
			case "model":
				if s.session != nil {
					s.session.Model = string(u.Value)
				}
			case "thinking_level":
				if s.session != nil {
					s.session.Effort = string(u.Value)
				}
			case "mode":
				s.applyMode(string(u.Value))
			}
		}
	}

	// Replay retained history and slash commands to client upon reconnection
	if a.client != nil {
		stream := acp1.NewSessionStream(a.client, params.SessionID)
		_ = stream.SendCommands(ctx, sessionCommandsV1(s))
		s.mu.Lock()
		hist := slices.Clone(s.history)
		s.mu.Unlock()
		for _, msg := range hist {
			switch msg.Role {
			case "user":
				_ = stream.SendUserMessage(ctx, msg.Content)
			case "assistant":
				_ = stream.SendText(ctx, msg.Content)
			}
		}
	}

	return &acp1.LoadSessionResponse{
		Modes:         a.buildModes(s),
		ConfigOptions: a.buildConfigOptions(s),
	}, nil
}

// ResumeSession implements acp1.SessionResumer.
func (a *ACPV1Agent) ResumeSession(ctx context.Context, params *acp1.ResumeSessionRequest) (*acp1.ResumeSessionResponse, error) {
	if params == nil {
		return nil, fmt.Errorf("resume session: nil params")
	}
	s, err := a.Lookup(ctx, params.SessionID)
	if err != nil {
		cwd := params.Cwd
		if cwd == "" && a.srv != nil {
			cwd = a.srv.cfg.Workspace
		}
		var sess *Session
		var sErr error
		if a.srv != nil {
			sess, sErr = a.srv.sessionFor(cwd, a.srv.cfg.Model)
			if sErr != nil {
				return nil, err
			}
		}
		recoveredHist := loadHistoryFromTranscript(string(params.SessionID))
		var cmds []acp1.AvailableCommand
		if a.srv != nil {
			cmds = v1CommandsFor(a.srv.cfg)
		} else {
			cmds = defaultV1Commands()
		}
		s = &acpV1Session{
			cwd:      cwd,
			mode:     "default",
			session:  sess,
			history:  recoveredHist,
			commands: cmds,
		}
		if saveErr := a.SessionManager.Save(ctx, params.SessionID, s); saveErr != nil {
			return nil, saveErr
		}
	}

	if params.Cwd != "" {
		s.cwd = params.Cwd
	}

	for _, opt := range params.ConfigOptions {
		if u, ok := opt.As[schemav1.InitialSessionConfigOptionUntagged](); ok {
			switch u.ConfigID {
			case "model":
				if s.session != nil {
					s.session.Model = string(u.Value)
				}
			case "thinking_level":
				if s.session != nil {
					s.session.Effort = string(u.Value)
				}
			case "mode":
				s.applyMode(string(u.Value))
			}
		}
	}

	if a.client != nil {
		stream := acp1.NewSessionStream(a.client, params.SessionID)
		_ = stream.SendCommands(ctx, sessionCommandsV1(s))
	}

	return &acp1.ResumeSessionResponse{
		Modes:         a.buildModes(s),
		ConfigOptions: a.buildConfigOptions(s),
	}, nil
}

type transcriptRecord struct {
	Source  string `json:"source"`
	Content string `json:"content"`
	Type    string `json:"type"`
}

func loadHistoryFromTranscript(convID string) []agentlink.Message {
	if convID == "" {
		return nil
	}
	transcriptPath := filepath.Join(confhome.Antigravity(), "brain", convID, ".system_generated", "logs", "transcript.jsonl")
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		return nil
	}
	var messages []agentlink.Message
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec transcriptRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		switch rec.Source {
		case "USER", "USER_EXPLICIT":
			if rec.Content != "" {
				messages = append(messages, agentlink.Message{
					Role:    "user",
					Content: rec.Content,
				})
			}
		case "MODEL":
			if rec.Content != "" && rec.Type != "PLANNER_RESPONSE" {
				messages = append(messages, agentlink.Message{
					Role:    "assistant",
					Content: rec.Content,
				})
			}
		}
	}
	return messages
}

// Authenticate implements acp1.Authenticator.
func (a *ACPV1Agent) Authenticate(ctx context.Context, params *acp1.AuthenticateRequest) (*acp1.AuthenticateResponse, error) {
	return &acp1.AuthenticateResponse{}, nil
}

// Logout implements acp1.LogoutHandler.
func (a *ACPV1Agent) Logout(ctx context.Context, params *acp1.LogoutRequest) (*acp1.LogoutResponse, error) {
	return &acp1.LogoutResponse{}, nil
}

func ptr[T any](v T) *T {
	return &v
}

// Prompt implements acp1.Agent.
func (a *ACPV1Agent) Prompt(ctx context.Context, params *acp1.PromptRequest) (*acp1.PromptResponse, error) {
	return a.RunTurnResponse(ctx, params.SessionID, func(turnCtx context.Context, s *acpV1Session) (*acp1.PromptResponse, error) {
		return a.runTurn(turnCtx, params.SessionID, s, extractPromptTextV1(params.Prompt))
	})
}

func extractPromptTextV1(blocks []acp1.ContentBlock) string {
	var texts []string
	var resources []string
	for _, block := range blocks {
		if txt, ok := block.As[schemav1.ContentBlockText](); ok {
			texts = append(texts, txt.Text)
		} else if res, ok := block.As[schemav1.ContentBlockResource](); ok {
			if tr, err := res.Resource.As[schemav1.TextResourceContents](); err == nil {
				uri := tr.URI
				path := uri
				if strings.HasPrefix(path, "file://") {
					path = strings.TrimPrefix(path, "file://")
				}
				header := uri
				if path != "" {
					header = path + " (editor contents)"
				}
				resources = append(resources, fmt.Sprintf("\n[Attached Resource: %s]\n%s", header, tr.Text))
			}
		}
	}
	prompt := strings.Join(texts, "\n")
	if len(resources) > 0 {
		if prompt != "" {
			prompt += "\n"
		}
		prompt += strings.Join(resources, "\n")
	}
	return prompt
}

func (a *ACPV1Agent) runTurn(ctx context.Context, sessID acp1.SessionID, s *acpV1Session, promptText string) (*acp1.PromptResponse, error) {
	trimmed := strings.TrimSpace(promptText)
	if trimmed == "/clear" {
		s.mu.Lock()
		s.history = nil
		s.mu.Unlock()
	}
	if trimmed == "/tasks" {
		stream := acp1.NewSessionStream(a.client, sessID)
		summary := formatTasksSummary()
		_ = stream.SendText(ctx, summary)
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
	}
	if trimmed == "/agents" {
		stream := acp1.NewSessionStream(a.client, sessID)
		summary := formatAgentsSummary(s.cwd)
		_ = stream.SendText(ctx, summary)
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
	}

	engine := agentturn.NewLocal(s.session.env)
	defer engine.Close()

	s.mu.Lock()
	turnHistory := append(slices.Clone(s.history), agentlink.Message{
		Role:    "user",
		Content: promptText,
	})
	s.mu.Unlock()

	turn := agentturn.Turn{
		Model:   s.session.Model,
		History: turnHistory,
	}

	stream := acp1.NewSessionStream(a.client, sessID)
	_ = stream.SendCommands(ctx, sessionCommandsV1(s))

	var origPromptUser func(context.Context, backend.ToolCall) (bool, error)
	if pr, ok := s.session.Runner.(*toolrunner.PermissionRunner); ok && pr != nil {
		origPromptUser = pr.PromptUser
		pr.PromptUser = func(callCtx context.Context, call backend.ToolCall) (bool, error) {
			callID := call.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%s_%d", call.Name, time.Now().UnixNano())
			}
			var title *string
			target := toolrunner.ToolTarget(call)
			if target != "" {
				t := fmt.Sprintf("%s: %s", call.Name, target)
				title = &t
			} else {
				t := call.Name
				title = &t
			}
			_, allowed, err := stream.RequestPermission(callCtx, acp1.ToolCallUpdate{
				ToolCallID: acp1.ToolCallID(callID),
				Title:      title,
			}, acp1.DefaultPermissionOptions()...)
			return allowed, err
		}
		defer func() {
			pr.PromptUser = origPromptUser
		}()
	}

	frames, err := engine.Turn(ctx, turn)
	if err != nil {
		return &acp1.PromptResponse{StopReason: acp1.StopReasonRefusal}, err
	}

	stopReason := acp1.StopReasonEndTurn
	var lastUsage *acp1.Usage
	var usageSent bool
	var assistantText strings.Builder

	for frame := range frames {
		switch frame.Kind {
		case agentlink.KindText:
			assistantText.WriteString(frame.Text)
			_ = stream.SendText(ctx, frame.Text)
		case agentlink.KindThought:
			_ = stream.SendThought(ctx, frame.Text)
		case agentlink.KindTool:
			if frame.Tool != nil {
				callID := acp1.ToolCallID(frame.ID)
				if callID == "" {
					callID = acp1.ToolCallID(fmt.Sprintf("call_%s", frame.Tool.Name))
				}
				if frame.Tool.Phase == agentlink.ToolStart {
					var opts []acp1.ToolCallOption
					if rawInput := rawJSONOrQuoted(frame.Tool.Args); rawInput != nil {
						opts = append(opts, acp1.WithRawInput(rawInput))
					}
					_ = stream.StartToolCall(ctx, callID, frame.Tool.Name, acp1.ToolKindExecute, opts...)
				} else if frame.Tool.Phase == agentlink.ToolEnd {
					var opts []acp1.ToolCallOption
					if rawOutput := rawJSONOrQuoted(frame.Tool.Result); rawOutput != nil {
						opts = append(opts, acp1.WithRawOutput(rawOutput))
					}
					if frame.Tool.Result != "" {
						opts = append(opts, acp1.WithToolContent(acp1.ToolText(frame.Tool.Result)))
					}
					if frame.Tool.Err {
						_ = stream.FailToolCall(ctx, callID, opts...)
					} else {
						_ = stream.CompleteToolCall(ctx, callID, opts...)
					}
				}
			}
		case agentlink.KindResult:
			if frame.Error != "" {
				stopReason = acp1.StopReasonRefusal
			}
			if frame.Usage != nil {
				total := frame.Usage.TotalTokens
				if total == 0 {
					total = frame.Usage.PromptTokens + frame.Usage.CompletionTokens
				}
				lastUsage = &acp1.Usage{
					InputTokens:  uint64(frame.Usage.PromptTokens),
					OutputTokens: uint64(frame.Usage.CompletionTokens),
					TotalTokens:  uint64(total),
				}
				if total > 0 || frame.Usage.PromptTokens > 0 {
					_ = stream.SendUsage(ctx, uint64(total), 1_000_000, nil)
					usageSent = true
				}
			}
		}
	}

	if !usageSent && lastUsage != nil && (lastUsage.TotalTokens > 0 || lastUsage.InputTokens > 0) {
		used := lastUsage.TotalTokens
		if used == 0 {
			used = lastUsage.InputTokens + lastUsage.OutputTokens
		}
		_ = stream.SendUsage(ctx, used, 1_000_000, nil)
	}

	if acp.TurnCancelled(ctx) {
		stopReason = acp1.StopReasonCancelled
	}

	if stopReason != acp1.StopReasonRefusal && stopReason != acp1.StopReasonCancelled && strings.TrimSpace(promptText) != "/clear" {
		s.mu.Lock()
		s.history = append(s.history,
			agentlink.Message{Role: "user", Content: promptText},
			agentlink.Message{Role: "assistant", Content: assistantText.String()},
		)
		s.mu.Unlock()
	}

	return &acp1.PromptResponse{
		StopReason: stopReason,
		Usage:      lastUsage,
	}, nil
}

// ListSessions implements acp1.SessionLister.
func (a *ACPV1Agent) ListSessions(ctx context.Context, params *acp1.ListSessionsRequest) (*acp1.ListSessionsResponse, error) {
	return a.SessionManager.List(ctx, params)
}

// CancelSession implements acp1.Agent, stopping background tasks and subagents.
func (a *ACPV1Agent) CancelSession(ctx context.Context, params *acp1.CancelNotification) error {
	stopSessionTasks(string(params.SessionID))
	return a.SessionManager.CancelSession(ctx, params)
}

// CloseSession implements acp1.SessionCloser, stopping background tasks and subagents.
func (a *ACPV1Agent) CloseSession(ctx context.Context, params *acp1.CloseSessionRequest) (*acp1.CloseSessionResponse, error) {
	stopSessionTasks(string(params.SessionID))
	return a.SessionManager.CloseSession(ctx, params)
}

package agentserver

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
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

func (s *acpV1Session) SessionInfo() acp1.SessionInfo {
	return acp1.SessionInfo{Cwd: s.cwd}
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
	a.SessionManager = acp1.NewSessionManager(
		acp1.NewMemoryStore[*acpV1Session](),
		func(ctx context.Context, params *acp1.NewSessionRequest) (acp1.SessionID, *acpV1Session, error) {
			sess, err := srv.session()
			if err != nil {
				return "", nil, err
			}
			sessID := acp1.SessionID(sess.ConversationID)
			if sessID == "" {
				sessID = acp1.SessionID(fmt.Sprintf("sess_%d", time.Now().UnixNano()))
			}
			cwd := params.Cwd
			if cwd == "" {
				cwd = sess.Workspace
			}
			return sessID, &acpV1Session{
				cwd:      cwd,
				mode:     "default",
				session:  sess,
				commands: v1CommandsFor(srv.cfg),
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

func (a *ACPV1Agent) buildConfigOptions(s *acpV1Session) []acp1.SessionConfigOption {
	currentModel := s.session.Model
	if currentModel == "" {
		currentModel = catalog.DefaultModel
	}
	currentEffort := s.session.Effort
	if currentEffort == "" {
		currentEffort = "high"
	}
	currentMode := s.mode
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
	for _, m := range catalog.ModelCatalogue() {
		modelChoices = append(modelChoices, acp1.SessionConfigSelectOption{
			Value: acp1.SessionConfigValueID(m.ID),
			Name:  m.Label,
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

func (a *ACPV1Agent) buildModes(s *acpV1Session) *acp1.SessionModeState {
	currentMode := s.mode
	if currentMode == "" {
		currentMode = "default"
	}
	descDefault := "Default permission prompt flow"
	descAcceptEdits := "Auto-approve file edits, prompt for commands"
	descPlan := "Research and plan without making changes"

	return &acp1.SessionModeState{
		CurrentModeID: acp1.SessionModeID(currentMode),
		AvailableModes: []acp1.SessionMode{
			{ID: "default", Name: "Default", Description: &descDefault},
			{ID: "accept-edits", Name: "Accept Edits", Description: &descAcceptEdits},
			{ID: "plan", Name: "Plan", Description: &descPlan},
		},
	}
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
		s.mode = m
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
	s.mode = m
	return &acp1.SetSessionModeResponse{}, nil
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
		return a.runTurn(turnCtx, params.SessionID, s, acp1.JoinTexts(params.Prompt))
	})
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

	frames, err := engine.Turn(ctx, turn)
	if err != nil {
		return &acp1.PromptResponse{StopReason: acp1.StopReasonRefusal}, err
	}

	stream := acp1.NewSessionStream(a.client, sessID)
	_ = stream.SendCommands(ctx, sessionCommandsV1(s))

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

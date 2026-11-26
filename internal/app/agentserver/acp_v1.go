package agentserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net"
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
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
)

type pendingACPReply struct {
	allowed bool
	choice  string
	err     error
}

type pendingACPPrompt struct {
	id        string
	title     string
	kind      interaction.Kind
	questions []interaction.Question
	options   []string
	replyCh   chan pendingACPReply
	once      sync.Once
}

func (p *pendingACPPrompt) resolve(allowed bool, choice string) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.replyCh <- pendingACPReply{allowed: allowed, choice: choice}
	})
}

func (p *pendingACPPrompt) resolveErr(err error) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.replyCh <- pendingACPReply{err: err}
	})
}

func isTransportClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed)
}

type acpV1Session struct {
	cwd            string
	mode           string
	session        *Session
	history        []agentlink.Message
	commands       []acp1.AvailableCommand
	pendingPrompts []*pendingACPPrompt
	activeStream   *acp1.SessionStream
	turns          acp.TurnTracker[acp1.SessionID]
	mu             sync.Mutex
}

func (s *acpV1Session) setStream(st *acp1.SessionStream) {
	if s == nil || st == nil {
		return
	}
	s.mu.Lock()
	s.activeStream = st
	s.mu.Unlock()
}

func (s *acpV1Session) getStream(fallback *acp1.SessionStream) *acp1.SessionStream {
	if s == nil {
		return fallback
	}
	s.mu.Lock()
	st := s.activeStream
	s.mu.Unlock()
	if st != nil {
		return st
	}
	return fallback
}

func (s *acpV1Session) setPendingPrompt(p *pendingACPPrompt) {
	if s == nil || p == nil {
		return
	}
	s.mu.Lock()
	s.pendingPrompts = append(s.pendingPrompts, p)
	s.mu.Unlock()
}

func (s *acpV1Session) clearPendingPrompt(p *pendingACPPrompt) {
	if s == nil || p == nil {
		return
	}
	s.mu.Lock()
	for i, cur := range s.pendingPrompts {
		if cur == p {
			s.pendingPrompts = append(s.pendingPrompts[:i], s.pendingPrompts[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
}

func (s *acpV1Session) getPendingPrompts() []*pendingACPPrompt {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.pendingPrompts)
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
			initMode := setup.DefaultMode()
			if initMode == "" {
				initMode = "default"
			}
			return sessID, &acpV1Session{
				cwd:      cwd,
				mode:     initMode,
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
		if !hasPersistedSession(string(params.SessionID)) {
			return nil, acp.SessionNotFound(params.SessionID)
		}
		// External editor reconnection (e.g. Zed / VS Code): restore persisted session
		cwd := params.Cwd
		if cwd == "" && a.srv != nil {
			cwd = a.srv.cfg.Workspace
		}
		var sess *Session
		var sErr error
		if a.srv != nil {
			sess, sErr = a.srv.sessionForID(string(params.SessionID), cwd, a.srv.cfg.Model)
			if sErr != nil {
				return nil, sErr
			}
		}
		recoveredHist := loadHistoryFromTranscript(string(params.SessionID))
		var cmds []acp1.AvailableCommand
		if a.srv != nil {
			cmds = v1CommandsFor(a.srv.cfg)
		} else {
			cmds = defaultV1Commands()
		}
		initMode := setup.DefaultMode()
		if initMode == "" {
			initMode = "default"
		}
		s = &acpV1Session{
			cwd:      cwd,
			mode:     initMode,
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

	// Replay full retained history and slash commands to client upon reconnection
	if a.client != nil {
		stream := acp1.NewSessionStream(a.client, params.SessionID)
		s.setStream(stream)
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
		if pending := s.getPendingPrompts(); len(pending) > 0 {
			reemit := func(c context.Context) {
				for _, p := range pending {
					dispatchPendingV1Prompt(c, stream, p)
				}
			}
			if !acp.AfterReply(ctx, reemit) {
				go reemit(context.Background())
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
		if !hasPersistedSession(string(params.SessionID)) {
			return nil, acp.SessionNotFound(params.SessionID)
		}
		cwd := params.Cwd
		if cwd == "" && a.srv != nil {
			cwd = a.srv.cfg.Workspace
		}
		var sess *Session
		var sErr error
		if a.srv != nil {
			sess, sErr = a.srv.sessionForID(string(params.SessionID), cwd, a.srv.cfg.Model)
			if sErr != nil {
				return nil, sErr
			}
		}
		recoveredHist := loadHistoryFromTranscript(string(params.SessionID))
		var cmds []acp1.AvailableCommand
		if a.srv != nil {
			cmds = v1CommandsFor(a.srv.cfg)
		} else {
			cmds = defaultV1Commands()
		}
		initMode := setup.DefaultMode()
		if initMode == "" {
			initMode = "default"
		}
		s = &acpV1Session{
			cwd:      cwd,
			mode:     initMode,
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
		s.setStream(stream)
		_ = stream.SendCommands(ctx, sessionCommandsV1(s))
		if pending := s.getPendingPrompts(); len(pending) > 0 {
			reemit := func(c context.Context) {
				for _, p := range pending {
					dispatchPendingV1Prompt(c, stream, p)
				}
			}
			if !acp.AfterReply(ctx, reemit) {
				go reemit(context.Background())
			}
		}
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
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
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
	return a.RunTurnResponse(context.WithoutCancel(ctx), params.SessionID, func(protocolCtx context.Context, s *acpV1Session) (*acp1.PromptResponse, error) {
		// ACP's manager follows the connection even after WithoutCancel.
		// Retained permission work instead belongs to the session, so it can
		// resume through LoadSession on another connection. Keep the outer
		// manager for ordered early session/cancel delivery, but forward only
		// explicit cancellation, never a transport disconnect, to that work.
		turnCtx, done, err := s.turns.Begin(context.Background(), params.SessionID)
		if err != nil {
			return nil, err
		}
		defer done()
		forwardCancel := func() {
			if acp.TurnCancelled(protocolCtx) {
				s.turns.Cancel(params.SessionID)
			}
		}
		stop := context.AfterFunc(protocolCtx, forwardCancel)
		defer stop()
		forwardCancel()

		if a.client != nil {
			s.setStream(acp1.NewSessionStream(a.client, params.SessionID))
		}
		var cfg Config
		if a.srv != nil {
			cfg = a.srv.cfg
		}
		parsed := parseACPPromptV1(params.Prompt, cfg)
		response, err := a.runTurn(turnCtx, params.SessionID, s, parsed)
		if acp.TurnCancelled(turnCtx) {
			return &acp1.PromptResponse{StopReason: acp1.StopReasonCancelled}, nil
		}
		return response, err
	})
}

func extractPromptTextV1(blocks []acp1.ContentBlock) string {
	return parseACPPromptV1(blocks, Config{}).ModelPrompt
}

func dispatchPendingV1Prompt(ctx context.Context, stream *acp1.SessionStream, p *pendingACPPrompt) {
	if stream == nil || p == nil {
		return
	}
	var opts []acp1.PermissionOption
	if len(p.options) > 0 {
		for i, opt := range p.options {
			opts = append(opts, acp1.PermissionOption{
				OptionID: acp1.PermissionOptionID(fmt.Sprintf("opt_%d", i)),
				Name:     opt,
				Kind:     acp1.PermissionOptionKindAllowOnce,
			})
		}
		opts = append(opts, acp1.PermissionOption{
			OptionID: "reject",
			Name:     "Skip / Cancel",
			Kind:     acp1.PermissionOptionKindRejectOnce,
		})
	} else {
		opts = acp1.DefaultPermissionOptions()
	}
	title := p.title
	choice, allowed, err := stream.RequestPermission(ctx, acp1.ToolCallUpdate{
		ToolCallID: acp1.ToolCallID(p.id),
		Title:      &title,
	}, opts...)
	if err != nil {
		if ctx.Err() == nil && (isTransportClosedErr(err) || errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "closed")) {
			return
		}
		p.resolveErr(err)
		return
	}
	p.resolve(allowed, choice.Name)
}

type acpV1InteractionHost struct {
	sess   *acpV1Session
	stream *acp1.SessionStream
}

func (h *acpV1InteractionHost) Ask(ctx context.Context, req interaction.Request) (interaction.Answer, error) {
	if req.Kind == interaction.KindQuestion && len(req.Questions) > 0 {
		answers := make([]string, 0, len(req.Questions))
		for _, q := range req.Questions {
			p := &pendingACPPrompt{
				id:        fmt.Sprintf("ask_%d", time.Now().UnixNano()),
				title:     q.Prompt,
				kind:      req.Kind,
				questions: []interaction.Question{q},
				options:   q.Options,
				replyCh:   make(chan pendingACPReply, 1),
			}
			h.sess.setPendingPrompt(p)
			go dispatchPendingV1Prompt(ctx, h.sess.getStream(h.stream), p)

			var rep pendingACPReply
			select {
			case <-ctx.Done():
				h.sess.clearPendingPrompt(p)
				return interaction.Answer{Cancelled: true}, ctx.Err()
			case rep = <-p.replyCh:
				h.sess.clearPendingPrompt(p)
			}
			if rep.err != nil {
				return interaction.Answer{Cancelled: true}, rep.err
			}
			if !rep.allowed {
				return interaction.Answer{Cancelled: true}, nil
			}
			ans := rep.choice
			if ans == "" && len(q.Options) > 0 {
				ans = q.Options[0]
			}
			answers = append(answers, ans)
		}
		return interaction.Answer{Answers: answers, Text: strings.Join(answers, "\n")}, nil
	}

	title := req.ToolName
	if req.Action != "" && req.Target != "" {
		title = fmt.Sprintf("%s: %s", req.Action, req.Target)
	} else if req.Reason != "" {
		title = req.Reason
	}
	p := &pendingACPPrompt{
		id:      fmt.Sprintf("ask_%d", time.Now().UnixNano()),
		title:   title,
		kind:    req.Kind,
		replyCh: make(chan pendingACPReply, 1),
	}
	h.sess.setPendingPrompt(p)
	defer h.sess.clearPendingPrompt(p)

	go dispatchPendingV1Prompt(ctx, h.sess.getStream(h.stream), p)

	select {
	case <-ctx.Done():
		return interaction.Answer{Cancelled: true}, ctx.Err()
	case rep := <-p.replyCh:
		if rep.err != nil {
			return interaction.Answer{Cancelled: true}, rep.err
		}
		if !rep.allowed {
			return interaction.Answer{Cancelled: true}, nil
		}
		return interaction.Answer{Allowed: true, Text: rep.choice}, nil
	}
}

func (a *ACPV1Agent) runTurn(ctx context.Context, sessID acp1.SessionID, s *acpV1Session, parsed parsedACPPrompt) (*acp1.PromptResponse, error) {
	if parsed.Command == "clear" && parsed.CommandArgs == "" {
		s.mu.Lock()
		s.history = nil
		s.mu.Unlock()
	}
	if parsed.Command == "tasks" && parsed.CommandArgs == "" {
		stream := acp1.NewSessionStream(a.client, sessID)
		summary := formatTasksSummary()
		_ = stream.SendText(ctx, summary)
		return &acp1.PromptResponse{StopReason: acp1.StopReasonEndTurn}, nil
	}
	if parsed.Command == "agents" && parsed.CommandArgs == "" {
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
		Content: parsed.ModelPrompt,
	})
	s.mu.Unlock()

	stream := acp1.NewSessionStream(a.client, sessID)
	_ = stream.SendCommands(ctx, sessionCommandsV1(s))

	turnCtx := interaction.WithHost(ctx, &acpV1InteractionHost{sess: s, stream: stream})

	var origPromptUser func(context.Context, backend.ToolCall) (bool, error)
	if pr, ok := s.session.Runner.(*toolrunner.PermissionRunner); ok && pr != nil {
		origPromptUser = pr.PromptUser
		pr.PromptUser = func(callCtx context.Context, call backend.ToolCall) (bool, error) {
			callID := call.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%s_%d", call.Name, time.Now().UnixNano())
			}
			title := call.Name
			if target := toolrunner.ToolTarget(call); target != "" {
				title = fmt.Sprintf("%s: %s", call.Name, target)
			}
			p := &pendingACPPrompt{
				id:      callID,
				title:   title,
				kind:    interaction.KindPermission,
				replyCh: make(chan pendingACPReply, 1),
			}
			s.setPendingPrompt(p)
			defer s.clearPendingPrompt(p)

			go dispatchPendingV1Prompt(callCtx, s.getStream(stream), p)

			select {
			case <-callCtx.Done():
				return false, callCtx.Err()
			case rep := <-p.replyCh:
				return rep.allowed, rep.err
			}
		}
		defer func() {
			pr.PromptUser = origPromptUser
		}()
	}

	var baseSubPrompt, baseSubOut, baseSubTotal int
	if s.session.SubagentRegistry != nil {
		baseSubPrompt, baseSubOut, baseSubTotal, _ = s.session.SubagentRegistry.TotalUsage()
	}

	var totalPromptTokens, totalOutputTokens, totalTokens uint64
	var usageSent bool
	var allAssistantText strings.Builder
	stopReason := acp1.StopReasonEndTurn

	runSingleTurn := func(hist []agentlink.Message) (string, bool, error) {
		turn := agentturn.Turn{
			Model:   s.session.Model,
			History: hist,
		}
		frames, err := engine.Turn(turnCtx, turn)
		if err != nil {
			return "", false, err
		}

		inProgressTools := make(map[string]string)
		var turnAssistant strings.Builder
		var turnPrompt, turnOutput, turnTotal uint64
		var turnFailed bool

		for frame := range frames {
			curStream := s.getStream(stream)
			switch frame.Kind {
			case agentlink.KindText:
				turnAssistant.WriteString(frame.Text)
				allAssistantText.WriteString(frame.Text)
				_ = curStream.SendText(turnCtx, frame.Text)
			case agentlink.KindThought:
				_ = curStream.SendThought(turnCtx, frame.Text)
			case agentlink.KindTool:
				if frame.Tool != nil {
					callID := acp1.ToolCallID(frame.ID)
					if callID == "" {
						callID = acp1.ToolCallID(fmt.Sprintf("call_%s", frame.Tool.Name))
					}
					if frame.Tool.Phase == agentlink.ToolStart {
						inProgressTools[string(callID)] = frame.Tool.Name
						var opts []acp1.ToolCallOption
						if rawInput := rawJSONOrQuoted(frame.Tool.Args); rawInput != nil {
							opts = append(opts, acp1.WithRawInput(rawInput))
						}
						_ = curStream.StartToolCall(turnCtx, callID, frame.Tool.Name, acp1.ToolKindExecute, opts...)
					} else if frame.Tool.Phase == agentlink.ToolEnd {
						delete(inProgressTools, string(callID))
						failed := frame.Tool.Err || tools.IsFailureText(frame.Tool.Result)
						resultText := frame.Tool.Result
						if failed && resultText == "" {
							resultText = frame.Error
							if resultText == "" {
								resultText = "Tool execution failed"
							}
						}
						var opts []acp1.ToolCallOption
						if rawOutput := rawJSONOrQuoted(resultText); rawOutput != nil {
							opts = append(opts, acp1.WithRawOutput(rawOutput))
						}
						if resultText != "" {
							opts = append(opts, acp1.WithToolContent(acp1.ToolText(resultText)))
						}
						if failed {
							_ = curStream.FailToolCall(turnCtx, callID, opts...)
						} else {
							_ = curStream.CompleteToolCall(turnCtx, callID, opts...)
						}
					}
				}
			case agentlink.KindResult:
				if frame.Error != "" {
					turnFailed = true
					stopReason = acp1.StopReasonRefusal
				}
				if frame.Usage != nil {
					t := frame.Usage.TotalTokens
					if t == 0 {
						t = frame.Usage.PromptTokens + frame.Usage.CompletionTokens
					}
					turnPrompt = uint64(frame.Usage.PromptTokens)
					turnOutput = uint64(frame.Usage.CompletionTokens)
					turnTotal = uint64(t)
				}
			}
		}

		if len(inProgressTools) > 0 {
			failCtx, cancel := context.WithTimeout(context.WithoutCancel(turnCtx), 2*time.Second)
			curStream := s.getStream(stream)
			for id := range inProgressTools {
				var opts []acp1.ToolCallOption
				if rawOutput := rawJSONOrQuoted("Cancelled"); rawOutput != nil {
					opts = append(opts, acp1.WithRawOutput(rawOutput))
				}
				opts = append(opts, acp1.WithToolContent(acp1.ToolText("Cancelled")))
				_ = curStream.FailToolCall(failCtx, acp1.ToolCallID(id), opts...)
			}
			cancel()
		}

		totalPromptTokens += turnPrompt
		totalOutputTokens += turnOutput
		totalTokens += turnTotal

		return turnAssistant.String(), turnFailed, nil
	}

	firstReply, failed, err := runSingleTurn(turnHistory)
	if err != nil {
		return &acp1.PromptResponse{StopReason: acp1.StopReasonRefusal}, err
	}
	if firstReply != "" {
		turnHistory = append(turnHistory, agentlink.Message{Role: "assistant", Content: firstReply})
	}

	// Keep session/prompt open while non-daemon background tasks or subagents are active,
	// running follow-up turns as completion notifications arrive.
	if !failed && !acp.TurnCancelled(turnCtx) && turnCtx.Err() == nil {
		for {
			if acp.TurnCancelled(turnCtx) || turnCtx.Err() != nil {
				stopReason = acp1.StopReasonCancelled
				break
			}
			if s.session.TaskInbox != nil && s.session.TaskInbox.Len() > 0 {
				items := s.session.TaskInbox.Drain()
				var parts []string
				for _, item := range items {
					parts = append(parts, agentturn.RenderTaskNotification(item))
				}
				turnHistory = append(turnHistory, agentlink.Message{
					Role:    "user",
					Content: strings.Join(parts, "\n\n"),
				})
				reply, f, tErr := runSingleTurn(turnHistory)
				if tErr != nil {
					stopReason = acp1.StopReasonRefusal
					break
				}
				if reply != "" {
					turnHistory = append(turnHistory, agentlink.Message{Role: "assistant", Content: reply})
				}
				if f {
					break
				}
				continue
			}
			if !hasNonDaemonBackgroundWork(s.session) {
				break
			}
			ticker := time.NewTicker(50 * time.Millisecond)
			select {
			case <-turnCtx.Done():
				ticker.Stop()
			case <-s.session.notifyWake:
				ticker.Stop()
			case <-ticker.C:
				ticker.Stop()
			}
		}
	}

	if s.session.SubagentRegistry != nil {
		curSubPrompt, curSubOut, curSubTotal, _ := s.session.SubagentRegistry.TotalUsage()
		if curSubPrompt > baseSubPrompt {
			totalPromptTokens += uint64(curSubPrompt - baseSubPrompt)
		}
		if curSubOut > baseSubOut {
			totalOutputTokens += uint64(curSubOut - baseSubOut)
		}
		if curSubTotal > baseSubTotal {
			totalTokens += uint64(curSubTotal - baseSubTotal)
		}
	}

	var lastUsage *acp1.Usage
	if totalTokens > 0 || totalPromptTokens > 0 || totalOutputTokens > 0 {
		if totalTokens == 0 {
			totalTokens = totalPromptTokens + totalOutputTokens
		}
		lastUsage = &acp1.Usage{
			InputTokens:  totalPromptTokens,
			OutputTokens: totalOutputTokens,
			TotalTokens:  totalTokens,
		}
		_ = s.getStream(stream).SendUsage(context.WithoutCancel(turnCtx), totalTokens, 1_000_000, nil)
		usageSent = true
	}
	_ = usageSent

	if acp.TurnCancelled(turnCtx) || turnCtx.Err() != nil {
		stopReason = acp1.StopReasonCancelled
	}

	if stopReason != acp1.StopReasonRefusal && stopReason != acp1.StopReasonCancelled && !(parsed.Command == "clear" && parsed.CommandArgs == "") {
		s.mu.Lock()
		s.history = append(s.history,
			agentlink.Message{Role: "user", Content: parsed.HistoryText},
			agentlink.Message{Role: "assistant", Content: allAssistantText.String()},
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
	var sess *Session
	if s, err := a.Lookup(ctx, params.SessionID); err == nil && s != nil {
		sess = s.session
		s.turns.Cancel(params.SessionID)
	}
	err := a.SessionManager.CancelSession(ctx, params)
	stopSessionWork(string(params.SessionID), sess)
	return err
}

// CloseSession implements acp1.SessionCloser, stopping background tasks and subagents.
func (a *ACPV1Agent) CloseSession(ctx context.Context, params *acp1.CloseSessionRequest) (*acp1.CloseSessionResponse, error) {
	var sess *Session
	if s, err := a.Lookup(ctx, params.SessionID); err == nil && s != nil {
		sess = s.session
		s.turns.Cancel(params.SessionID)
	}
	stopSessionWork(string(params.SessionID), sess)
	return a.SessionManager.CloseSession(ctx, params)
}

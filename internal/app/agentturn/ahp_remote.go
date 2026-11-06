package agentturn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/microsoft/agent-host-protocol/clients/go/ahp"
	"github.com/microsoft/agent-host-protocol/clients/go/ahptypes"
	"github.com/microsoft/agent-host-protocol/clients/go/ahpws"
)

func init() {
	Register(Driver{
		Name:         "ahp",
		Aliases:      []string{"ahp-remote", "ahp-client"},
		Description:  "Agent Host Protocol (AHP) remote client",
		DefaultModel: "gemini-3.8-flash",
		Spawn: func(ctx context.Context, opts SpawnOptions) (Engine, error) {
			serverAddr := "ws://127.0.0.1:8888/ahp"
			token := ""
			sessionURI := ""
			for i := 0; i < len(opts.Args); i++ {
				arg := opts.Args[i]
				if (arg == "--server" || arg == "-server") && i+1 < len(opts.Args) {
					serverAddr = opts.Args[i+1]
					i++
				} else if strings.HasPrefix(arg, "--server=") {
					serverAddr = strings.TrimPrefix(arg, "--server=")
				} else if (arg == "--token" || arg == "-token") && i+1 < len(opts.Args) {
					token = opts.Args[i+1]
					i++
				} else if strings.HasPrefix(arg, "--token=") {
					token = strings.TrimPrefix(arg, "--token=")
				} else if (arg == "--session" || arg == "-session") && i+1 < len(opts.Args) {
					sessionURI = opts.Args[i+1]
					i++
				} else if strings.HasPrefix(arg, "--session=") {
					sessionURI = strings.TrimPrefix(arg, "--session=")
				}
			}
			return DialAHPRemote(ctx, serverAddr, token, opts.CWD, AHPRemoteOptions{
				SessionURI: sessionURI,
				Model:      opts.Model,
			})
		},
	})
}

// AHPRemote runs turns against an AHP (Agent Host Protocol) server.
type AHPRemote struct {
	client         *ahp.Client
	chatSub        *ahp.Subscription
	sessionURI     string
	chatURI        string
	defaultModel   string
	models         []ModelInfo
	ownsSession    bool
	disposeOnClose bool

	mu            sync.Mutex
	busy          bool
	closed        bool
	readErr       error
	currentTurnID string
	lastUsage     *agentlink.Usage
	activeTools   map[string]*ahpToolTracker
	toolMu        sync.Mutex
	permHandler   PermissionHandler

	frames chan agentlink.Frame
	doneCh chan struct{}
}

type ahpToolTracker struct {
	name    string
	args    string
	started bool
}

var _ Engine = (*AHPRemote)(nil)
var _ DefaultModeler = (*AHPRemote)(nil)
var _ ModelLister = (*AHPRemote)(nil)
var _ CommandLister = (*AHPRemote)(nil)
var _ CapabilityProvider = (*AHPRemote)(nil)
var _ ActionHandler = (*AHPRemote)(nil)
var _ PermissionAware = (*AHPRemote)(nil)

// AHPRemoteOptions configures an AHP remote turn engine.
type AHPRemoteOptions struct {
	SessionURI     string
	ChatURI        string
	Model          string
	CWD            string
	Token          string
	DisposeOnClose bool
}

// DialAHPRemote connects to an AHP server over WebSocket and initializes the AHPRemote engine.
func DialAHPRemote(ctx context.Context, rawURL, token, cwd string, opts ...AHPRemoteOptions) (*AHPRemote, error) {
	u := strings.TrimSpace(rawURL)
	if !strings.Contains(u, "://") {
		u = "ws://" + u
	}

	var opt AHPRemoteOptions
	if len(opts) > 0 {
		opt = opts[0]
	} else {
		opt.DisposeOnClose = true
	}
	if opt.Token == "" {
		opt.Token = strings.TrimSpace(token)
	}
	if opt.CWD == "" {
		opt.CWD = cwd
	}

	// Safely parse URL to preserve query strings while ensuring /ahp endpoint
	parsed, err := url.Parse(u)
	if err == nil {
		if opt.Token == "" {
			if qTok := parsed.Query().Get("token"); qTok != "" {
				opt.Token = qTok
			} else if qAuth := parsed.Query().Get("auth"); qAuth != "" {
				opt.Token = qAuth
			}
		}
		if opt.SessionURI == "" {
			if qSess := parsed.Query().Get("session"); qSess != "" {
				opt.SessionURI = qSess
			}
		}

		cleanPath := strings.TrimRight(parsed.Path, "/")
		if idx := strings.Index(cleanPath, "/ahp-session:/"); idx >= 0 {
			if opt.SessionURI == "" {
				opt.SessionURI = cleanPath[idx+1:]
			}
			cleanPath = cleanPath[:idx]
		}

		if cleanPath == "" || cleanPath == "/" {
			parsed.Path = "/ahp"
		} else if !strings.HasSuffix(cleanPath, "/ahp") {
			parsed.Path = cleanPath + "/ahp"
		}
		u = parsed.String()
	} else if !strings.HasSuffix(strings.TrimRight(u, "/"), "/ahp") {
		u = strings.TrimRight(u, "/") + "/ahp"
	}

	if opt.SessionURI != "" {
		if !strings.HasPrefix(opt.SessionURI, "ahp-session:/") {
			opt.SessionURI = "ahp-session:/" + strings.TrimPrefix(opt.SessionURI, "/")
		}
		// Attaching to an existing session should not destroy it on close
		opt.DisposeOnClose = false
	}

	dialOpts := ahpws.DialOptions{}
	if opt.Token != "" {
		dialOpts.HTTPHeader = http.Header{
			"Authorization": []string{"Bearer " + opt.Token},
		}
	}

	transport, err := ahpws.Connect(ctx, u, dialOpts)
	if err != nil {
		return nil, fmt.Errorf("ahp: connect %s: %w", u, err)
	}

	client, err := ahp.Connect(ctx, transport, ahp.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("ahp: client connect: %w", err)
	}

	remote, err := NewAHPRemote(ctx, client, opt)
	if err != nil {
		_ = client.Shutdown(context.Background())
		return nil, err
	}
	return remote, nil
}

// NewAHPRemote initializes an AHPRemote turn engine around an existing AHP client.
func NewAHPRemote(ctx context.Context, client *ahp.Client, opt AHPRemoteOptions) (*AHPRemote, error) {
	if client == nil {
		return nil, errors.New("ahp: nil client")
	}

	clientID := fmt.Sprintf("pigasus-tui-%s", uuid.New().String()[:8])
	initRes, err := client.Initialize(ctx, clientID, ahptypes.SupportedProtocolVersions(), []string{"ahp-root://"})
	if err != nil {
		return nil, fmt.Errorf("ahp: initialize: %w", err)
	}

	if opt.Token != "" {
		_ = client.Request(ctx, "authenticate", ahptypes.AuthenticateParams{
			Channel: ahptypes.RootResourceURI,
			Token:   opt.Token,
		}, nil)
	}

	// Discover models and default provider from root snapshots
	var models []ModelInfo
	var defaultProvider string
	for _, snap := range initRes.Snapshots {
		if snap.Resource == ahptypes.RootResourceURI && snap.State.Root != nil {
			for _, agent := range snap.State.Root.Agents {
				if defaultProvider == "" && agent.Provider != "" {
					defaultProvider = agent.Provider
				}
				for _, m := range agent.Models {
					models = append(models, ModelInfo{
						ID:          m.Id,
						DisplayName: m.Name,
					})
				}
			}
		}
	}

	defaultModel := opt.Model
	if defaultModel == "" && len(models) > 0 {
		defaultModel = models[0].ID
	}
	if defaultModel == "" {
		defaultModel = "gemini-3.8-flash"
	}

	var (
		sessURI     string
		chatURI     string
		ownsSession bool
	)

	if opt.SessionURI != "" && opt.ChatURI != "" {
		sessURI = opt.SessionURI
		chatURI = opt.ChatURI
	} else if opt.SessionURI != "" {
		sessURI = opt.SessionURI
		sessSnap, _, err := client.Subscribe(ctx, sessURI)
		if err != nil {
			return nil, fmt.Errorf("ahp: subscribe session %s: %w", sessURI, err)
		}
		if sessSnap != nil && sessSnap.Snapshot != nil && sessSnap.Snapshot.State.Session != nil {
			sState := sessSnap.Snapshot.State.Session
			if sState.DefaultChat != nil && *sState.DefaultChat != "" {
				chatURI = *sState.DefaultChat
			} else if len(sState.Chats) > 0 {
				chatURI = sState.Chats[0].Resource
			}
		}
		if chatURI == "" {
			return nil, fmt.Errorf("ahp: session %s has no chat channel", sessURI)
		}
	} else {
		createParams := ahptypes.CreateSessionParams{
			Channel: ahptypes.RootResourceURI,
		}
		if defaultProvider != "" {
			createParams.Provider = &defaultProvider
		}
		if opt.CWD != "" {
			createParams.WorkingDirectories = []ahptypes.URI{"file://" + opt.CWD}
		}
		if defaultModel != "" {
			createParams.Config = map[string]json.RawMessage{
				"model": json.RawMessage(fmt.Sprintf("%q", defaultModel)),
			}
		}

		var createRes map[string]any
		if err := client.Request(ctx, "session.create", createParams, &createRes); err != nil {
			return nil, fmt.Errorf("ahp: create session: %w", err)
		}
		ownsSession = true
		if s, ok := createRes["session"].(string); ok && s != "" {
			sessURI = s
		} else if s, ok := createRes["resource"].(string); ok && s != "" {
			sessURI = s
		}
		if c, ok := createRes["chat"].(string); ok && c != "" {
			chatURI = c
		}
		if chatURI == "" && sessURI != "" {
			sessSnap, _, err := client.Subscribe(ctx, sessURI)
			if err != nil {
				return nil, fmt.Errorf("ahp: subscribe session %s: %w", sessURI, err)
			}
			if sessSnap != nil && sessSnap.Snapshot != nil && sessSnap.Snapshot.State.Session != nil {
				sState := sessSnap.Snapshot.State.Session
				if sState.DefaultChat != nil && *sState.DefaultChat != "" {
					chatURI = *sState.DefaultChat
				} else if len(sState.Chats) > 0 {
					chatURI = sState.Chats[0].Resource
				}
			}
		}
		if chatURI == "" {
			return nil, fmt.Errorf("ahp: failed to determine chat channel for session %s", sessURI)
		}
	}

	_, chatSub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		return nil, fmt.Errorf("ahp: subscribe chat %s: %w", chatURI, err)
	}

	r := &AHPRemote{
		client:         client,
		chatSub:        chatSub,
		sessionURI:     sessURI,
		chatURI:        chatURI,
		defaultModel:   defaultModel,
		models:         models,
		ownsSession:    ownsSession,
		disposeOnClose: opt.DisposeOnClose,
		frames:         make(chan agentlink.Frame, 512),
		doneCh:         make(chan struct{}),
		activeTools:    make(map[string]*ahpToolTracker),
	}
	go r.pumpEvents()
	return r, nil
}

// Turn asks the AHP server to run one turn and reports its frames on the returned channel.
func (r *AHPRemote) Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed || r.readErr != nil {
		r.mu.Unlock()
		return nil, errors.New("agentturn: the engine is closed")
	}
	if r.busy {
		r.mu.Unlock()
		return nil, errors.New("agentturn: a turn is already running")
	}
	if r.client == nil {
		r.mu.Unlock()
		return nil, errors.New("agentturn: the engine has no connection")
	}
	r.busy = true
	r.mu.Unlock()

	// Drain any stale unread frames from previous cancelled turns
drain:
	for {
		select {
		case <-r.frames:
		default:
			break drain
		}
	}

	turnID, err := turnID()
	if err != nil {
		r.mu.Lock()
		r.busy = false
		r.mu.Unlock()
		return nil, err
	}

	var promptText string
	for i := len(t.History) - 1; i >= 0; i-- {
		if t.History[i].Role == "user" {
			promptText = t.History[i].Content
			break
		}
	}
	if promptText == "" && len(t.History) > 0 {
		promptText = t.History[len(t.History)-1].Content
	}

	r.mu.Lock()
	r.currentTurnID = turnID
	r.mu.Unlock()

	action := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    turnID,
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: promptText,
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}
	targetModel := t.Model
	if targetModel == "" {
		targetModel = r.DefaultModel()
	}
	if targetModel != "" {
		action.Value.(*ahptypes.ChatTurnStartedAction).Message.Model = &ahptypes.ModelSelection{
			Id: targetModel,
		}
	}

	if _, err := r.client.Dispatch(ctx, r.chatURI, action); err != nil {
		r.mu.Lock()
		r.busy = false
		r.currentTurnID = ""
		r.mu.Unlock()
		return nil, fmt.Errorf("ahp: dispatch turn: %w", err)
	}

	ch := make(chan agentlink.Frame, frameBuffer)
	go func() {
		defer close(ch)
		defer func() {
			r.mu.Lock()
			r.busy = false
			r.currentTurnID = ""
			r.mu.Unlock()
		}()
		r.read(ctx, turnID, ch)
	}()

	return ch, nil
}

func (r *AHPRemote) read(ctx context.Context, id string, out chan<- agentlink.Frame) {
	for {
		select {
		case <-ctx.Done():
			r.cancel(id)
			return
		case f, ok := <-r.frames:
			if !ok {
				out <- agentlink.Frame{Kind: agentlink.KindResult, ID: id, Error: r.connectionError()}
				return
			}
			if f.ID != "" && f.ID != id {
				continue
			}
			select {
			case out <- f:
			case <-ctx.Done():
				r.cancel(id)
				return
			}
			if f.Kind == agentlink.KindResult {
				return
			}
		}
	}
}

func (r *AHPRemote) cancel(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	cancelAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnCancelledAction{
			Type:   ahptypes.ActionTypeChatTurnCancelled,
			TurnId: id,
		},
	}
	_, _ = r.client.Dispatch(ctx, r.chatURI, cancelAction)
}

func (r *AHPRemote) pumpEvents() {
	defer close(r.frames)
	for {
		select {
		case <-r.doneCh:
			return
		case ev, ok := <-r.chatSub.Events():
			if !ok {
				r.mu.Lock()
				r.readErr = errors.New("ahp: chat subscription closed")
				r.mu.Unlock()
				return
			}
			r.handleSubscriptionEvent(ev)
		}
	}
}

func (r *AHPRemote) handleSubscriptionEvent(ev ahp.SubscriptionEvent) {
	switch e := ev.(type) {
	case ahp.SubscriptionEventAuthRequired:
		r.sendFrame(agentlink.Frame{
			Kind: agentlink.KindNotice,
			Notice: &agentlink.Notice{
				Kind: agentlink.NoticeError,
				Text: "authentication required",
			},
		})
	case ahp.SubscriptionEventAction:
		r.handleAction(e.Envelope)
	}
}

func (r *AHPRemote) handleAction(env ahptypes.ActionEnvelope) {
	if env.Action.Value == nil {
		return
	}

	switch a := env.Action.Value.(type) {
	case *ahptypes.ChatTurnStartedAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		r.sendFrame(agentlink.Frame{
			Kind:  agentlink.KindState,
			ID:    a.TurnId,
			Phase: agentlink.PhaseWorking,
		})

	case *ahptypes.ChatResponsePartAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		switch p := a.Part.Value.(type) {
		case *ahptypes.ReasoningResponsePart:
			if p.Content != "" {
				r.sendFrame(agentlink.Frame{
					Kind: agentlink.KindThought,
					ID:   a.TurnId,
					Text: p.Content,
				})
			}
		case *ahptypes.MarkdownResponsePart:
			if p.Content != "" {
				r.sendFrame(agentlink.Frame{
					Kind: agentlink.KindText,
					ID:   a.TurnId,
					Text: p.Content,
				})
			}
		}

	case *ahptypes.ChatReasoningAction:
		if !r.isCurrentTurn(a.TurnId) || a.Content == "" {
			return
		}
		r.sendFrame(agentlink.Frame{
			Kind: agentlink.KindThought,
			ID:   a.TurnId,
			Text: a.Content,
		})

	case *ahptypes.ChatDeltaAction:
		if !r.isCurrentTurn(a.TurnId) || a.Content == "" {
			return
		}
		r.sendFrame(agentlink.Frame{
			Kind: agentlink.KindText,
			ID:   a.TurnId,
			Text: a.Content,
		})

	case *ahptypes.ChatToolCallStartAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		name := a.DisplayName
		if name == "" {
			name = a.ToolName
		}
		r.toolMu.Lock()
		if _, ok := r.activeTools[a.ToolCallId]; !ok {
			r.activeTools[a.ToolCallId] = &ahpToolTracker{name: name}
		}
		r.toolMu.Unlock()

	case *ahptypes.ChatToolCallReadyAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		var rawArgs string
		if a.ToolInput != nil && a.ToolInput.Inline != nil {
			rawArgs = *a.ToolInput.Inline
		}
		r.toolMu.Lock()
		tracker, ok := r.activeTools[a.ToolCallId]
		toolName := a.ToolCallId
		alreadyStarted := false
		if ok {
			tracker.args = rawArgs
			toolName = tracker.name
			alreadyStarted = tracker.started
			tracker.started = true
		} else {
			r.activeTools[a.ToolCallId] = &ahpToolTracker{name: toolName, args: rawArgs, started: true}
		}
		r.toolMu.Unlock()

		if !alreadyStarted {
			r.sendFrame(agentlink.Frame{
				Kind: agentlink.KindTool,
				ID:   a.TurnId,
				Tool: &agentlink.Tool{
					Name:  toolName,
					Phase: agentlink.ToolStart,
					Args:  rawArgs,
				},
			})
		}

		if a.Confirmed == nil {
			go r.handleToolConfirmation(a, toolName, rawArgs)
		}

	case *ahptypes.ChatToolCallDeltaAction:
		if !r.isCurrentTurn(a.TurnId) || a.Content == nil {
			return
		}
		r.toolMu.Lock()
		if tracker, ok := r.activeTools[a.ToolCallId]; ok {
			tracker.args += *a.Content
		}
		r.toolMu.Unlock()

	case *ahptypes.ChatToolCallCompleteAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		r.toolMu.Lock()
		tracker, ok := r.activeTools[a.ToolCallId]
		toolName := a.ToolCallId
		var toolArgs string
		alreadyStarted := false
		if ok {
			toolName = tracker.name
			toolArgs = tracker.args
			alreadyStarted = tracker.started
			delete(r.activeTools, a.ToolCallId)
		}
		r.toolMu.Unlock()

		if !alreadyStarted {
			r.sendFrame(agentlink.Frame{
				Kind: agentlink.KindTool,
				ID:   a.TurnId,
				Tool: &agentlink.Tool{
					Name:  toolName,
					Phase: agentlink.ToolStart,
					Args:  toolArgs,
				},
			})
		}

		resultStr := extractToolResultString(a.Result)

		r.sendFrame(agentlink.Frame{
			Kind: agentlink.KindTool,
			ID:   a.TurnId,
			Tool: &agentlink.Tool{
				Name:   toolName,
				Phase:  agentlink.ToolEnd,
				Args:   toolArgs,
				Result: resultStr,
				Err:    !a.Result.Success,
			},
		})

	case *ahptypes.ChatUsageAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		var promptTok, compTok int
		if a.Usage.InputTokens != nil {
			promptTok = int(*a.Usage.InputTokens)
		}
		if a.Usage.OutputTokens != nil {
			compTok = int(*a.Usage.OutputTokens)
		}
		u := &agentlink.Usage{
			PromptTokens:     promptTok,
			CompletionTokens: compTok,
			TotalTokens:      promptTok + compTok,
		}
		r.mu.Lock()
		r.lastUsage = u
		r.mu.Unlock()
		r.sendFrame(agentlink.Frame{
			Kind:  agentlink.KindState,
			ID:    a.TurnId,
			Usage: u,
		})

	case *ahptypes.ChatTurnCompleteAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		r.mu.Lock()
		u := r.lastUsage
		r.mu.Unlock()
		r.sendFrame(agentlink.Frame{
			Kind:  agentlink.KindResult,
			ID:    a.TurnId,
			Usage: u,
		})

	case *ahptypes.ChatErrorAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		errMsg := a.Part.Error.Message
		if errMsg == "" {
			errMsg = a.Part.Error.ErrorType
		}
		if errMsg == "" {
			errMsg = "turn failed"
		}
		r.sendFrame(agentlink.Frame{
			Kind:  agentlink.KindResult,
			ID:    a.TurnId,
			Error: errMsg,
		})

	case *ahptypes.ChatTurnCancelledAction:
		if !r.isCurrentTurn(a.TurnId) {
			return
		}
		r.sendFrame(agentlink.Frame{
			Kind:  agentlink.KindResult,
			ID:    a.TurnId,
			Error: "turn cancelled",
		})
	}
}

func (r *AHPRemote) isCurrentTurn(turnID string) bool {
	if turnID == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy && (r.currentTurnID == "" || r.currentTurnID == turnID)
}

func (r *AHPRemote) sendFrame(f agentlink.Frame) {
	select {
	case r.frames <- f:
	case <-r.doneCh:
	}
}

// Close terminates the AHP client connection and releases resources.
func (r *AHPRemote) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.doneCh)
	r.mu.Unlock()

	if r.client != nil {
		if r.ownsSession && r.disposeOnClose && r.sessionURI != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = r.client.Request(ctx, "session.dispose", ahptypes.DisposeSessionParams{Channel: r.sessionURI}, nil)
			cancel()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return r.client.Shutdown(ctx)
	}
	return nil
}

func (r *AHPRemote) connectionError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readErr != nil {
		return r.readErr.Error()
	}
	return "agentturn: the connection closed"
}

// SessionURI returns the active session URI on the AHP server.
func (r *AHPRemote) SessionURI() string {
	return r.sessionURI
}

// ChatURI returns the active chat URI on the AHP server.
func (r *AHPRemote) ChatURI() string {
	return r.chatURI
}

// Client returns the underlying AHP client.
func (r *AHPRemote) Client() *ahp.Client {
	return r.client
}

// DefaultModel returns the default model advertised by the session or host.
func (r *AHPRemote) DefaultModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defaultModel
}

// SetModel overrides the default model for subsequent turns.
func (r *AHPRemote) SetModel(m string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultModel = m
}

// ListModels returns the model catalogue discovered from the AHP server.
func (r *AHPRemote) ListModels(ctx context.Context) ([]ModelInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ModelInfo, len(r.models))
	copy(out, r.models)
	return out, nil
}

// EngineName identifies this engine as ahp.
func (r *AHPRemote) EngineName() string {
	return "ahp"
}

// Capabilities returns capabilities supported by the remote AHP agent.
func (r *AHPRemote) Capabilities() Capability {
	return CapModelSwitch
}

// ExecuteAction executes a backend action against the AHP agent.
func (r *AHPRemote) ExecuteAction(ctx context.Context, action string, args string) (ActionResult, error) {
	switch strings.ToLower(action) {
	case "model":
		targetModel := strings.TrimSpace(args)
		if targetModel == "" {
			return ActionResult{Success: false, Message: "model name required"}, nil
		}
		r.SetModel(targetModel)
		return ActionResult{Success: true, Message: "Model switched to " + targetModel}, nil
	}
	return ActionResult{Success: false, Message: "unknown action: " + action}, nil
}

// ListSessions queries available sessions on the AHP server.
func (r *AHPRemote) ListSessions(ctx context.Context) ([]ahptypes.SessionSummary, error) {
	var listRes ahptypes.ListSessionsResult
	if err := r.client.Request(ctx, "session.list", ahptypes.ListSessionsParams{Channel: ahptypes.RootResourceURI}, &listRes); err != nil {
		return nil, err
	}
	return listRes.Items, nil
}

// CreateChat allocates a new chat channel within the current session.
func (r *AHPRemote) CreateChat(ctx context.Context, title string) (string, error) {
	var res map[string]any
	params := ahptypes.CreateChatParams{
		Channel: r.sessionURI,
	}
	if err := r.client.Request(ctx, "createChat", params, &res); err != nil {
		return "", err
	}
	chatURI, _ := res["chat"].(string)
	return chatURI, nil
}

// DisposeSession tears down the specified session on the AHP server.
func (r *AHPRemote) DisposeSession(ctx context.Context, sessionURI string) error {
	if sessionURI == "" {
		sessionURI = r.sessionURI
	}
	return r.client.Request(ctx, "session.dispose", ahptypes.DisposeSessionParams{Channel: sessionURI}, nil)
}

// SetPermissionHandler registers a handler for tool confirmation requests from the remote AHP server.
func (r *AHPRemote) SetPermissionHandler(h PermissionHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permHandler = h
}

// AvailableCommands exposes dynamic slash commands provided by the AHP remote engine.
func (r *AHPRemote) AvailableCommands() []CommandInfo {
	return []CommandInfo{
		{
			Name:        "model",
			Description: "Switch the remote model for subsequent turns",
			InputHint:   "<model-name>",
		},
	}
}

func (r *AHPRemote) handleToolConfirmation(a *ahptypes.ChatToolCallReadyAction, toolName, rawArgs string) {
	r.mu.Lock()
	handler := r.permHandler
	r.mu.Unlock()

	approved := true
	var selectedOpt *string

	if handler != nil {
		title := toolName
		if a.ConfirmationTitle != nil {
			title = a.ConfirmationTitle.AsText()
		}
		opts := make([]PermissionOption, 0, len(a.Options))
		for _, o := range a.Options {
			opts = append(opts, PermissionOption{
				ID:   o.Id,
				Name: o.Label,
				Kind: string(o.Kind),
			})
		}
		req := PermissionRequest{
			SessionID:  r.sessionURI,
			ToolCallID: a.ToolCallId,
			Title:      title,
			RawInput:   rawArgs,
			Options:    opts,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		dec, err := handler(ctx, req)
		if err != nil || !dec.Approved {
			approved = false
		} else {
			approved = true
			if dec.OptionID != "" {
				selectedOpt = &dec.OptionID
			}
		}
	}

	confAction := ahptypes.ChatToolCallConfirmedAction{
		Type:             ahptypes.ActionTypeChatToolCallConfirmed,
		TurnId:           a.TurnId,
		ToolCallId:       a.ToolCallId,
		Approved:         approved,
		SelectedOptionId: selectedOpt,
	}
	if approved {
		confirmed := ahptypes.ToolCallConfirmationReasonUserAction
		confAction.Confirmed = &confirmed
	} else {
		reason := ahptypes.ToolCallCancellationReasonDenied
		confAction.Reason = &reason
	}

	if r.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = r.client.Dispatch(ctx, r.chatURI, ahptypes.StateAction{Value: &confAction})
	}
}

func extractToolResultString(res ahptypes.ToolCallResult) string {
	resultStr := res.PastTenseMessage.AsText()
	if resultStr != "" {
		return resultStr
	}
	if len(res.Content) > 0 {
		var sb strings.Builder
		for _, c := range res.Content {
			switch tc := c.Value.(type) {
			case *ahptypes.ToolResultTextContent:
				sb.WriteString(tc.Text)
			case *ahptypes.ToolResultTerminalContent:
				if tc.Result != nil && tc.Result.Preview != nil {
					sb.WriteString(*tc.Result.Preview)
				}
			case *ahptypes.ToolResultFileEditContent:
				if tc.After != nil && tc.After.Uri != "" {
					sb.WriteString(string(tc.After.Uri))
				} else if tc.Before != nil && tc.Before.Uri != "" {
					sb.WriteString(string(tc.Before.Uri))
				}
			case *ahptypes.ToolResultSubagentContent:
				sb.WriteString(tc.Title)
			case *ahptypes.ToolResultContentUnknown:
				sb.Write(tc.Raw)
			}
		}
		resultStr = sb.String()
	}
	if resultStr == "" && res.Error != nil {
		resultStr = string(*res.Error)
	}
	return resultStr
}

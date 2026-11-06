package agentserver

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/microsoft/agent-host-protocol/clients/go/ahp"
	"github.com/microsoft/agent-host-protocol/clients/go/ahptypes"
)

// AHPHost manages AHP (Agent Host Protocol) channels, client subscriptions,
// session lifetimes, and agent turns.
type AHPHost struct {
	srv       *Server
	mu        sync.RWMutex
	seq       int64
	root      ahptypes.RootState
	sessions  map[string]*ahpSession       // keyed by "ahp-session:/<uuid>"
	chats     map[string]*ahpChat          // keyed by "ahp-chat:/<uuid>"
	terminals map[string]*ahpTerminal      // keyed by "ahp-terminal:/<uuid>"
	subs      map[string]map[*ahpConn]bool // channel URI -> set of subscribed connections
	clients   map[string]*ahpConn          // clientID -> connection
	replay    []ahptypes.ActionEnvelope    // replay buffer for reconnection
}

// NewAHPHost creates a new AHP host backed by Server.
func NewAHPHost(srv *Server) *AHPHost {
	models := availableAHPModels()
	agents := []ahptypes.AgentInfo{
		{
			Provider:    "antigravity",
			DisplayName: "Google Antigravity",
			Description: "Autonomous agent host powered by Google DeepMind",
			Models:      models,
			Capabilities: &ahptypes.AgentCapabilities{
				MultipleChats: &ahptypes.MultipleChatsCapability{},
			},
		},
	}
	zero := int64(0)
	h := &AHPHost{
		srv: srv,
		root: ahptypes.RootState{
			Agents:         agents,
			ActiveSessions: &zero,
			Terminals:      []ahptypes.TerminalInfo{},
		},
		sessions:  make(map[string]*ahpSession),
		chats:     make(map[string]*ahpChat),
		terminals: make(map[string]*ahpTerminal),
		subs:      make(map[string]map[*ahpConn]bool),
		clients:   make(map[string]*ahpConn),
		replay:    make([]ahptypes.ActionEnvelope, 0, 1024),
	}
	return h
}

func availableAHPModels() []ahptypes.SessionModelInfo {
	var res []ahptypes.SessionModelInfo
	for _, m := range catalog.ModelCatalogue() {
		res = append(res, ahptypes.SessionModelInfo{
			Id:   m.ID,
			Name: m.Label,
		})
	}
	if len(res) == 0 {
		res = append(res, ahptypes.SessionModelInfo{
			Id:   "gemini-3.8-flash",
			Name: "Gemini 3.8 Flash",
		})
	}
	return res
}

func (h *AHPHost) nextSeq() int64 {
	return atomic.AddInt64(&h.seq, 1)
}

func (h *AHPHost) currentSeq() int64 {
	return atomic.LoadInt64(&h.seq)
}

// ahpSession represents a persistent agent session in AHP.
type ahpSession struct {
	id          string
	resource    ahptypes.URI
	state       ahptypes.SessionState
	session     *Session
	defaultChat ahptypes.URI
	createdAt   string
	modifiedAt  string
}

func (s *ahpSession) summary() ahptypes.SessionSummary {
	return ahptypes.SessionSummary{
		Resource:           s.resource,
		Provider:           s.state.Provider,
		Title:              s.state.Title,
		Status:             s.state.Status,
		Activity:           s.state.Activity,
		WorkingDirectories: s.state.WorkingDirectories,
		CreatedAt:          s.createdAt,
		ModifiedAt:         s.modifiedAt,
	}
}

// ahpChat represents an individual chat channel within an AHP session.
type ahpChat struct {
	id            string
	resource      ahptypes.URI
	sessionURI    ahptypes.URI
	parentSession *ahpSession
	state         ahptypes.ChatState
	engine        agentturn.Engine
	history       []agentlink.Message
	turnCancel    context.CancelFunc
	turnMu        sync.Mutex
}

func (c *ahpChat) summary() ahptypes.ChatSummary {
	return ahptypes.ChatSummary{
		Resource:   c.resource,
		Title:      c.state.Title,
		Status:     c.state.Status,
		Activity:   c.state.Activity,
		ModifiedAt: c.state.ModifiedAt,
	}
}

func (c *ahpChat) setCancel(cancel context.CancelFunc) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.turnCancel != nil {
		c.turnCancel()
	}
	c.turnCancel = cancel
}

func (c *ahpChat) cancelTurn() {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.turnCancel != nil {
		c.turnCancel()
		c.turnCancel = nil
	}
}

// ahpConn represents a single client connection.
type ahpConn struct {
	host          *AHPHost
	clientID      string
	clientInfo    *ahptypes.Implementation
	authenticated bool
	subs          map[string]bool
	subsMu        sync.Mutex
	writeMu       sync.Mutex
	sendFn        func(msg ahptypes.JsonRpcMessage) error
	closeFn       func() error
}

func (c *ahpConn) send(msg ahptypes.JsonRpcMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.sendFn(msg)
}

func (c *ahpConn) sendResult(id uint64, result any) error {
	var raw json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		raw = b
	} else {
		raw = json.RawMessage("{}")
	}
	return c.send(ahptypes.JsonRpcMessage{
		SuccessResponse: &ahptypes.JsonRpcSuccessResponse{
			JsonRpc: ahptypes.JsonRpcV2,
			ID:      id,
			Result:  raw,
		},
	})
}

func (c *ahpConn) sendError(id uint64, code int32, message string) error {
	return c.sendErrorWithData(id, code, message, nil)
}

func (c *ahpConn) sendErrorWithData(id uint64, code int32, message string, data any) error {
	var rawData json.RawMessage
	if data != nil {
		if b, err := json.Marshal(data); err == nil {
			rawData = b
		}
	}
	return c.send(ahptypes.JsonRpcMessage{
		ErrorResponse: &ahptypes.JsonRpcErrorResponse{
			JsonRpc: ahptypes.JsonRpcV2,
			ID:      id,
			Error: ahptypes.JsonRpcError{
				Code:    code,
				Message: message,
				Data:    rawData,
			},
		},
	})
}

func (c *ahpConn) sendNotification(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	return c.send(ahptypes.JsonRpcMessage{
		Notification: &ahptypes.JsonRpcNotification{
			JsonRpc: ahptypes.JsonRpcV2,
			Method:  method,
			Params:  raw,
		},
	})
}

// canonicalURI normalizes channel URIs into standard AHP format.
func canonicalURI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "ahp-root://" || raw == "root" || raw == "ahp-root:" || raw == "ahp-root:///" {
		return ahptypes.RootResourceURI
	}
	if strings.HasPrefix(raw, "ahp-session:/") || strings.HasPrefix(raw, "ahp-chat:/") || strings.HasPrefix(raw, "ahp-terminal:/") {
		return raw
	}
	if strings.HasPrefix(raw, "ahp-session://") {
		return "ahp-session:/" + strings.TrimPrefix(raw, "ahp-session://")
	}
	if strings.HasPrefix(raw, "ahp-chat://") {
		return "ahp-chat:/" + strings.TrimPrefix(raw, "ahp-chat://")
	}
	if strings.HasPrefix(raw, "ahp-terminal://") {
		return "ahp-terminal:/" + strings.TrimPrefix(raw, "ahp-terminal://")
	}
	if strings.HasPrefix(raw, "ahp-session:") {
		return "ahp-session:/" + strings.TrimPrefix(raw, "ahp-session:")
	}
	if strings.HasPrefix(raw, "ahp-chat:") {
		return "ahp-chat:/" + strings.TrimPrefix(raw, "ahp-chat:")
	}
	if strings.HasPrefix(raw, "ahp-terminal:") {
		return "ahp-terminal:/" + strings.TrimPrefix(raw, "ahp-terminal:")
	}
	if strings.HasPrefix(raw, "session:") {
		return "ahp-session:/" + strings.TrimPrefix(raw, "session:")
	}
	if strings.HasPrefix(raw, "chat:") {
		return "ahp-chat:/" + strings.TrimPrefix(raw, "chat:")
	}
	if strings.HasPrefix(raw, "terminal:") {
		return "ahp-terminal:/" + strings.TrimPrefix(raw, "terminal:")
	}
	return "ahp-session:/" + raw
}

// snapshotFor builds a snapshot for the requested channel URI.
func (h *AHPHost) snapshotFor(uri string) any {
	uri = canonicalURI(uri)
	seq := h.currentSeq()

	if uri == ahptypes.RootResourceURI {
		return &ahptypes.Snapshot{
			Resource: uri,
			State:    ahptypes.SnapshotState{Root: &h.root},
			FromSeq:  seq,
		}
	}

	if sess, ok := h.sessions[uri]; ok {
		return &ahptypes.Snapshot{
			Resource: uri,
			State:    ahptypes.SnapshotState{Session: &sess.state},
			FromSeq:  seq,
		}
	}

	if chat, ok := h.chats[uri]; ok {
		return &ahptypes.Snapshot{
			Resource: uri,
			State:    ahptypes.SnapshotState{Chat: &chat.state},
			FromSeq:  seq,
		}
	}

	if term, ok := h.terminals[uri]; ok {
		return term.snapshotWire(seq)
	}

	return nil
}

// addSubscription registers a client connection to a channel URI.
func (h *AHPHost) addSubscription(conn *ahpConn, channel string) {
	channel = canonicalURI(channel)
	h.mu.Lock()
	defer h.mu.Unlock()

	set, ok := h.subs[channel]
	if !ok {
		set = make(map[*ahpConn]bool)
		h.subs[channel] = set
	}
	set[conn] = true

	conn.subsMu.Lock()
	conn.subs[channel] = true
	conn.subsMu.Unlock()
}

// removeSubscription unregisters a client connection from a channel URI.
func (h *AHPHost) removeSubscription(conn *ahpConn, channel string) {
	channel = canonicalURI(channel)
	h.mu.Lock()
	defer h.mu.Unlock()

	if set, ok := h.subs[channel]; ok {
		delete(set, conn)
		if len(set) == 0 {
			delete(h.subs, channel)
		}
	}

	conn.subsMu.Lock()
	delete(conn.subs, channel)
	conn.subsMu.Unlock()
}

// removeConn cleans up a disconnected client connection.
func (h *AHPHost) removeConn(conn *ahpConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conn.subsMu.Lock()
	for ch := range conn.subs {
		if set, ok := h.subs[ch]; ok {
			delete(set, conn)
			if len(set) == 0 {
				delete(h.subs, ch)
			}
		}
	}
	conn.subs = make(map[string]bool)
	conn.subsMu.Unlock()

	if conn.clientID != "" && h.clients[conn.clientID] == conn {
		delete(h.clients, conn.clientID)
	}
}

// broadcastAction delivers an action to all subscribers of the channel,
// updates the channel state via reducers, and records to the replay buffer.
func (h *AHPHost) broadcastAction(channel string, action ahptypes.StateAction, origin ...*ahptypes.ActionOrigin) {
	channel = canonicalURI(channel)
	h.mu.Lock()

	seq := h.nextSeq()
	var actOrigin *ahptypes.ActionOrigin
	if len(origin) > 0 {
		actOrigin = origin[0]
	}

	env := ahptypes.ActionEnvelope{
		Channel:   channel,
		ServerSeq: seq,
		Action:    action,
		Origin:    actOrigin,
	}

	// Apply reducer to in-memory state
	if channel == ahptypes.RootResourceURI {
		ahp.ApplyActionToRoot(&h.root, action)
	} else if chat, ok := h.chats[channel]; ok {
		ahp.ApplyActionToChat(&chat.state, action)
		chat.state.ModifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if chat.parentSession != nil {
			chat.parentSession.modifiedAt = chat.state.ModifiedAt
		}
	} else if sess, ok := h.sessions[channel]; ok {
		ahp.ApplyActionToSession(&sess.state, action)
		sess.modifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
	} else if term, ok := h.terminals[channel]; ok {
		term.mu.Lock()
		ahp.ApplyActionToTerminal(&term.state, action)
		term.mu.Unlock()
	}

	// Append to replay buffer
	h.replay = append(h.replay, env)
	if len(h.replay) > 2048 {
		h.replay = h.replay[len(h.replay)-1024:]
	}

	// Gather target connections: direct subscribers, plus session subscribers if channel is a chat
	targetConns := make(map[*ahpConn]bool)
	for conn := range h.subs[channel] {
		targetConns[conn] = true
	}
	if chat, ok := h.chats[channel]; ok && chat.sessionURI != "" {
		for conn := range h.subs[chat.sessionURI] {
			targetConns[conn] = true
		}
	}

	conns := make([]*ahpConn, 0, len(targetConns))
	for c := range targetConns {
		conns = append(conns, c)
	}
	h.mu.Unlock()

	for _, c := range conns {
		_ = c.sendNotification("action", env)
	}
}

// broadcastNotification sends an arbitrary notification to channel subscribers.
func (h *AHPHost) broadcastNotification(channel string, method string, params any) {
	channel = canonicalURI(channel)
	h.mu.RLock()
	var conns []*ahpConn
	for c := range h.subs[channel] {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	for _, c := range conns {
		_ = c.sendNotification(method, params)
	}
}

// createSession allocates a new session and its default chat.
func (h *AHPHost) createSession(params ahptypes.CreateSessionParams) (*ahpSession, *ahpChat, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sessID := uuid.New().String()
	if params.Channel != "" && params.Channel != ahptypes.RootResourceURI {
		cand := canonicalURI(params.Channel)
		if strings.HasPrefix(cand, "ahp-session:/") {
			idPart := strings.TrimPrefix(cand, "ahp-session:/")
			if idPart != "" {
				sessID = idPart
			}
		}
	}
	sessURI := "ahp-session:/" + sessID
	if _, exists := h.sessions[sessURI]; exists {
		return nil, nil, fmt.Errorf("session already exists: %s", sessURI)
	}

	workspace := h.srv.cfg.Workspace
	if len(params.WorkingDirectories) > 0 {
		ws := params.WorkingDirectories[0]
		if strings.HasPrefix(ws, "file://") {
			ws = strings.TrimPrefix(ws, "file://")
		}
		if ws != "" {
			workspace = ws
		}
	}

	model := h.srv.cfg.Model
	if params.Config != nil {
		if raw, ok := params.Config["model"]; ok {
			var m string
			if err := json.Unmarshal(raw, &m); err == nil && m != "" {
				model = m
			}
		}
	}

	provider := "antigravity"
	if params.Provider != nil && *params.Provider != "" {
		if *params.Provider != "antigravity" {
			return nil, nil, fmt.Errorf("provider not found: %s", *params.Provider)
		}
		provider = *params.Provider
	}

	// Initialize underlying pigasus Session
	underlyingSession, err := h.srv.sessionFor(workspace, model)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	chatID := uuid.New().String()
	chatURI := "ahp-chat:/" + chatID

	chatSummary := ahptypes.ChatSummary{
		Resource:   chatURI,
		Title:      "Default Chat",
		Status:     ahptypes.SessionStatusIdle,
		ModifiedAt: now,
	}

	chatState := ahptypes.ChatState{
		Resource:   chatURI,
		Title:      "Default Chat",
		Status:     ahptypes.SessionStatusIdle,
		ModifiedAt: now,
		Turns:      []ahptypes.Turn{},
	}

	workingDirs := []ahptypes.URI{"file://" + underlyingSession.Workspace}

	var serverTools []ahptypes.ToolDefinition
	for _, d := range underlyingSession.Tools {
		title := d.Name
		desc := d.Description
		serverTools = append(serverTools, ahptypes.ToolDefinition{
			Name:        d.Name,
			Title:       &title,
			Description: &desc,
		})
	}

	sessState := ahptypes.SessionState{
		Provider:           provider,
		Title:              "Session " + sessID[:8],
		Status:             ahptypes.SessionStatusIdle,
		WorkingDirectories: workingDirs,
		Lifecycle:          ahptypes.SessionLifecycleReady,
		ActiveClients:      []ahptypes.SessionActiveClient{},
		ServerTools:        serverTools,
		Chats:              []ahptypes.ChatSummary{chatSummary},
		DefaultChat:        &chatURI,
	}

	sess := &ahpSession{
		id:          sessID,
		resource:    sessURI,
		state:       sessState,
		session:     underlyingSession,
		defaultChat: chatURI,
		createdAt:   now,
		modifiedAt:  now,
	}

	engine := agentturn.NewLocal(underlyingSession.env)
	chat := &ahpChat{
		id:            chatID,
		resource:      chatURI,
		sessionURI:    sessURI,
		parentSession: sess,
		state:         chatState,
		engine:        engine,
		history:       make([]agentlink.Message, 0),
	}

	h.sessions[sessURI] = sess
	h.chats[chatURI] = chat

	// Update active sessions counter
	activeCount := int64(len(h.sessions))
	h.root.ActiveSessions = &activeCount

	return sess, chat, nil
}

// createChat allocates a new chat channel within a session.
func (h *AHPHost) createChat(params ahptypes.CreateChatParams) (*ahpChat, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sessURI := canonicalURI(params.Channel)
	sess, ok := h.sessions[sessURI]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessURI)
	}

	chatID := uuid.New().String()
	if params.Chat != "" {
		cand := canonicalURI(params.Chat)
		if strings.HasPrefix(cand, "ahp-chat:/") {
			idPart := strings.TrimPrefix(cand, "ahp-chat:/")
			if idPart != "" {
				chatID = idPart
			}
		}
	}
	chatURI := "ahp-chat:/" + chatID
	if _, exists := h.chats[chatURI]; exists {
		return nil, fmt.Errorf("chat already exists: %s", chatURI)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	title := fmt.Sprintf("Chat %s", chatID[:8])
	chatSummary := ahptypes.ChatSummary{
		Resource:   chatURI,
		Title:      title,
		Status:     ahptypes.SessionStatusIdle,
		ModifiedAt: now,
	}
	chatState := ahptypes.ChatState{
		Resource:   chatURI,
		Title:      title,
		Status:     ahptypes.SessionStatusIdle,
		ModifiedAt: now,
		Turns:      []ahptypes.Turn{},
	}

	engine := agentturn.NewLocal(sess.session.env)
	chat := &ahpChat{
		id:            chatID,
		resource:      chatURI,
		sessionURI:    sessURI,
		parentSession: sess,
		state:         chatState,
		engine:        engine,
		history:       make([]agentlink.Message, 0),
	}

	sess.state.Chats = append(sess.state.Chats, chatSummary)
	h.chats[chatURI] = chat

	return chat, nil
}

// disposeChat tears down a specific chat channel.
func (h *AHPHost) disposeChat(chatURI string) bool {
	h.mu.Lock()
	chatURI = canonicalURI(chatURI)
	chat, ok := h.chats[chatURI]
	if !ok {
		h.mu.Unlock()
		return false
	}

	chat.cancelTurn()
	if chat.engine != nil {
		_ = chat.engine.Close()
	}
	sess := chat.parentSession
	if sess != nil {
		for i, c := range sess.state.Chats {
			if c.Resource == chatURI {
				sess.state.Chats = append(sess.state.Chats[:i], sess.state.Chats[i+1:]...)
				break
			}
		}
	}
	delete(h.chats, chatURI)
	delete(h.subs, chatURI)
	h.mu.Unlock()

	if sess != nil {
		h.broadcastAction(sess.resource, ahptypes.StateAction{
			Value: &ahptypes.SessionChatRemovedAction{
				Type: ahptypes.ActionTypeSessionChatRemoved,
				Chat: chatURI,
			},
		})
	}
	return true
}

// disposeSession tears down a session and its chats.
func (h *AHPHost) disposeSession(sessionURI string) bool {
	h.mu.Lock()
	sessionURI = canonicalURI(sessionURI)
	sess, ok := h.sessions[sessionURI]
	if !ok {
		h.mu.Unlock()
		return false
	}

	// Remove and close chats
	for _, chSummary := range sess.state.Chats {
		if ch, found := h.chats[chSummary.Resource]; found {
			ch.cancelTurn()
			if ch.engine != nil {
				_ = ch.engine.Close()
			}
			delete(h.chats, chSummary.Resource)
			delete(h.subs, chSummary.Resource)
		}
	}

	delete(h.sessions, sessionURI)
	delete(h.subs, sessionURI)

	activeCount := int64(len(h.sessions))
	h.root.ActiveSessions = &activeCount
	h.mu.Unlock()

	// Broadcast notifications
	h.broadcastNotification(ahptypes.RootResourceURI, "root/sessionRemoved", ahptypes.SessionRemovedParams{
		Channel: ahptypes.RootResourceURI,
		Session: sessionURI,
	})
	h.broadcastAction(ahptypes.RootResourceURI, ahptypes.StateAction{
		Value: &ahptypes.RootActiveSessionsChangedAction{
			Type:           ahptypes.ActionTypeRootActiveSessionsChanged,
			ActiveSessions: activeCount,
		},
	})

	return true
}

// listSessions returns summaries of all sessions.
func (h *AHPHost) listSessions() []ahptypes.SessionSummary {
	h.mu.RLock()
	defer h.mu.RUnlock()

	res := make([]ahptypes.SessionSummary, 0, len(h.sessions))
	for _, s := range h.sessions {
		res = append(res, s.summary())
	}
	return res
}

// startTurn executes a turn on the specified chat.
func (h *AHPHost) startTurn(chat *ahpChat, action *ahptypes.ChatTurnStartedAction, origin ...*ahptypes.ActionOrigin) {
	chat.turnMu.Lock()
	if chat.turnCancel != nil {
		chat.turnMu.Unlock()
		return
	}
	chat.turnMu.Unlock()

	turnID := action.TurnId
	if turnID == "" {
		turnID = fmt.Sprintf("turn_%d", time.Now().UnixNano())
		action.TurnId = turnID
	}
	startedAt := action.StartedAt
	if startedAt == "" {
		startedAt = time.Now().UTC().Format(time.RFC3339Nano)
		action.StartedAt = startedAt
	}
	if action.Message.Origin.Kind == "" {
		action.Message.Origin.Kind = ahptypes.MessageKindUser
	}

	// Emit and broadcast chat/turnStarted (preserving client ActionOrigin if present)
	h.broadcastAction(chat.resource, ahptypes.StateAction{Value: action}, origin...)

	go func() {
		turnCtx, cancel := context.WithCancel(context.Background())
		chat.setCancel(cancel)
		defer func() {
			chat.cancelTurn()
		}()

		userText := action.Message.Text
		chat.turnMu.Lock()
		chat.history = append(chat.history, agentlink.Message{
			Role:    "user",
			Content: userText,
		})
		hist := make([]agentlink.Message, len(chat.history))
		copy(hist, chat.history)
		chat.turnMu.Unlock()

		model := chat.parentSession.session.Model
		if action.Message.Model != nil && action.Message.Model.Id != "" {
			model = action.Message.Model.Id
		}
		frames, err := chat.engine.Turn(turnCtx, agentturn.Turn{
			Model:   model,
			History: hist,
		})
		if err != nil {
			h.broadcastAction(chat.resource, ahptypes.StateAction{
				Value: &ahptypes.ChatErrorAction{
					Type:     ahptypes.ActionTypeChatError,
					TurnId:   turnID,
					Duration: 0,
					Part: ahptypes.ErrorResponsePart{
						Kind: ahptypes.ResponsePartKindError,
						Error: ahptypes.ErrorInfo{
							ErrorType: "error",
							Message:   err.Error(),
						},
					},
				},
			})
			return
		}

		var (
			assistantText   strings.Builder
			startTime       = time.Now()
			partCounter     = 0
			reasoningPartID string
			markdownPartID  string
			activeToolCalls = make(map[string][]string) // toolName -> []toolID
			turnFailed      bool
		)

		for frame := range frames {
			if turnCtx.Err() != nil {
				return
			}
			switch frame.Kind {
			case agentlink.KindThought:
				if reasoningPartID == "" {
					partCounter++
					reasoningPartID = fmt.Sprintf("reasoning-%d", partCounter)
					h.broadcastAction(chat.resource, ahptypes.StateAction{
						Value: &ahptypes.ChatResponsePartAction{
							Type:   ahptypes.ActionTypeChatResponsePart,
							TurnId: turnID,
							Part: ahptypes.ResponsePart{
								Value: &ahptypes.ReasoningResponsePart{
									Kind:    ahptypes.ResponsePartKindReasoning,
									Id:      reasoningPartID,
									Content: "",
								},
							},
						},
					})
				}
				h.broadcastAction(chat.resource, ahptypes.StateAction{
					Value: &ahptypes.ChatReasoningAction{
						Type:    ahptypes.ActionTypeChatReasoning,
						TurnId:  turnID,
						PartId:  reasoningPartID,
						Content: frame.Text,
					},
				})

			case agentlink.KindText:
				assistantText.WriteString(frame.Text)
				if markdownPartID == "" {
					partCounter++
					markdownPartID = fmt.Sprintf("part-%d", partCounter)
					h.broadcastAction(chat.resource, ahptypes.StateAction{
						Value: &ahptypes.ChatResponsePartAction{
							Type:   ahptypes.ActionTypeChatResponsePart,
							TurnId: turnID,
							Part: ahptypes.ResponsePart{
								Value: &ahptypes.MarkdownResponsePart{
									Kind:    ahptypes.ResponsePartKindMarkdown,
									Id:      markdownPartID,
									Content: "",
								},
							},
						},
					})
				}
				h.broadcastAction(chat.resource, ahptypes.StateAction{
					Value: &ahptypes.ChatDeltaAction{
						Type:    ahptypes.ActionTypeChatDelta,
						TurnId:  turnID,
						PartId:  markdownPartID,
						Content: frame.Text,
					},
				})

			case agentlink.KindTool:
				if frame.Tool != nil {
					toolName := frame.Tool.Name
					if frame.Tool.Phase == agentlink.ToolStart {
						toolID := fmt.Sprintf("tool_%s_%d", toolName, time.Now().UnixNano())
						activeToolCalls[toolName] = append(activeToolCalls[toolName], toolID)

						// 1. Emit ChatToolCallStartAction (streaming state)
						h.broadcastAction(chat.resource, ahptypes.StateAction{
							Value: &ahptypes.ChatToolCallStartAction{
								Type:        ahptypes.ActionTypeChatToolCallStart,
								TurnId:      turnID,
								ToolCallId:  toolID,
								ToolName:    toolName,
								DisplayName: toolName,
							},
						})

						// 2. Emit ChatToolCallReadyAction (transitions to running state)
						confirmed := ahptypes.ToolCallConfirmationReasonNotNeeded
						var toolInput *ahptypes.ToolInput
						if frame.Tool.Args != "" {
							rawArgs := frame.Tool.Args
							toolInput = &ahptypes.ToolInput{Inline: &rawArgs}
						}
						h.broadcastAction(chat.resource, ahptypes.StateAction{
							Value: &ahptypes.ChatToolCallReadyAction{
								Type:       ahptypes.ActionTypeChatToolCallReady,
								TurnId:     turnID,
								ToolCallId: toolID,
								ToolInput:  toolInput,
								Confirmed:  &confirmed,
							},
						})

					} else if frame.Tool.Phase == agentlink.ToolEnd {
						var toolID string
						if queue := activeToolCalls[toolName]; len(queue) > 0 {
							toolID = queue[0]
							activeToolCalls[toolName] = queue[1:]
						} else {
							toolID = fmt.Sprintf("tool_%s_%d", toolName, time.Now().UnixNano())
						}

						// 3. Emit ChatToolCallCompleteAction (transitions to completed state)
						h.broadcastAction(chat.resource, ahptypes.StateAction{
							Value: &ahptypes.ChatToolCallCompleteAction{
								Type:       ahptypes.ActionTypeChatToolCallComplete,
								TurnId:     turnID,
								ToolCallId: toolID,
								Result: ahptypes.ToolCallResult{
									Success:          !frame.Tool.Err,
									PastTenseMessage: ahptypes.NewStringOrMarkdownPlain(frame.Tool.Result),
								},
							},
						})
					}
				}

			case agentlink.KindResult:
				if frame.Error != "" {
					turnFailed = true
					h.broadcastAction(chat.resource, ahptypes.StateAction{
						Value: &ahptypes.ChatErrorAction{
							Type:     ahptypes.ActionTypeChatError,
							TurnId:   turnID,
							Duration: time.Since(startTime).Milliseconds(),
							Part: ahptypes.ErrorResponsePart{
								Kind: ahptypes.ResponsePartKindError,
								Error: ahptypes.ErrorInfo{
									ErrorType: "turn_error",
									Message:   frame.Error,
								},
							},
						},
					})
					return
				}
			}
		}

		if turnCtx.Err() != nil || turnFailed {
			return
		}

		duration := time.Since(startTime).Milliseconds()
		h.broadcastAction(chat.resource, ahptypes.StateAction{
			Value: &ahptypes.ChatTurnCompleteAction{
				Type:     ahptypes.ActionTypeChatTurnComplete,
				TurnId:   turnID,
				Duration: duration,
			},
		})

		chat.turnMu.Lock()
		chat.history = append(chat.history, agentlink.Message{
			Role:    "assistant",
			Content: assistantText.String(),
		})
		chat.turnMu.Unlock()

		// Notify parent session of chat update
		if chat.parentSession != nil {
			h.broadcastAction(chat.sessionURI, ahptypes.StateAction{
				Value: &ahptypes.SessionChatUpdatedAction{
					Type: ahptypes.ActionTypeSessionChatUpdated,
					Chat: chat.resource,
					Changes: ahptypes.PartialChatSummary{
						ModifiedAt: &chat.state.ModifiedAt,
					},
				},
			})
		}

		// Notify root of session summary change
		h.broadcastNotification(ahptypes.RootResourceURI, "root/sessionSummaryChanged", ahptypes.SessionSummaryChangedParams{
			Channel: ahptypes.RootResourceURI,
			Session: chat.parentSession.resource,
			Changes: ahptypes.PartialSessionSummary{
				ModifiedAt: &chat.state.ModifiedAt,
			},
		})
	}()
}

// handleMessage processes a single inbound JSON-RPC message.
func (h *AHPHost) handleMessage(ctx context.Context, conn *ahpConn, data []byte) {
	var msg ahptypes.JsonRpcMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		_ = conn.sendError(0, ahptypes.ErrorCodeParseError, "parse error: "+err.Error())
		return
	}

	if msg.Request != nil {
		h.handleRequest(ctx, conn, *msg.Request)
	} else if msg.Notification != nil {
		h.handleNotification(ctx, conn, *msg.Notification)
	}
}

func (h *AHPHost) handleRequest(ctx context.Context, conn *ahpConn, req ahptypes.JsonRpcRequest) {
	switch req.Method {
	case "initialize":
		var params ahptypes.InitializeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid initialize params")
			return
		}
		ver, err := ahptypes.NegotiateProtocolVersion(params.ProtocolVersions)
		if err != nil || ver == "" {
			_ = conn.sendErrorWithData(req.ID, ahptypes.ErrorCodeUnsupportedProtocolVersion, "unsupported protocol version", ahptypes.UnsupportedProtocolVersionErrorData{
				SupportedVersions: ahptypes.SupportedProtocolVersions(),
			})
			return
		}

		conn.clientID = params.ClientId
		conn.clientInfo = params.ClientInfo
		if conn.clientID != "" {
			h.mu.Lock()
			h.clients[conn.clientID] = conn
			h.mu.Unlock()
		}

		var snapshots []any
		if h.srv.cfg.Token == "" || conn.authenticated {
			for _, uri := range params.InitialSubscriptions {
				h.addSubscription(conn, uri)
				if snap := h.snapshotFor(uri); snap != nil {
					snapshots = append(snapshots, snap)
				}
			}
		}
		if snapshots == nil {
			snapshots = []any{}
		}

		ws := h.srv.cfg.Workspace
		defaultDir := &ws
		if ws == "" {
			defaultDir = nil
		}
		cmdPrefix := "!"

		res := map[string]any{
			"protocolVersion": ver,
			"serverSeq":       h.nextSeq(),
			"serverInfo": map[string]any{
				"name":    "pigasus-ahp",
				"title":   "Pigasus Agent Host",
				"version": Version,
			},
			"snapshots":             snapshots,
			"defaultDirectory":      defaultDir,
			"terminalCommandPrefix": &cmdPrefix,
		}
		_ = conn.sendResult(req.ID, res)

	case "ping":
		_ = conn.sendResult(req.ID, map[string]any{})

	case "authenticate":
		var params ahptypes.AuthenticateParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid authenticate params")
			return
		}
		if h.srv.cfg.Token != "" {
			tok := strings.TrimPrefix(params.Token, "Bearer ")
			expected := strings.TrimPrefix(h.srv.cfg.Token, "Bearer ")
			if subtle.ConstantTimeCompare([]byte(tok), []byte(expected)) != 1 {
				_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "invalid authentication token")
				return
			}
		}
		conn.authenticated = true
		_ = conn.sendResult(req.ID, ahptypes.AuthenticateResult{})

	case "subscribe":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.SubscribeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid subscribe params")
			return
		}
		ch := canonicalURI(params.Channel)
		h.addSubscription(conn, ch)
		snap := h.snapshotFor(ch)
		_ = conn.sendResult(req.ID, map[string]any{"snapshot": snap})

	case "unsubscribe":
		var params ahptypes.UnsubscribeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid unsubscribe params")
			return
		}
		h.removeSubscription(conn, params.Channel)
		_ = conn.sendResult(req.ID, map[string]any{})

	case "session.list", "listSessions":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.ListSessionsParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		summaries := h.listSessions()
		sort.Slice(summaries, func(i, j int) bool {
			return summaries[i].ModifiedAt > summaries[j].ModifiedAt
		})
		if params.Limit != nil && *params.Limit > 0 && int64(len(summaries)) > *params.Limit {
			summaries = summaries[:*params.Limit]
		}
		_ = conn.sendResult(req.ID, ahptypes.ListSessionsResult{Items: summaries})

	case "session.create", "createSession":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.CreateSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid createSession params")
			return
		}
		sess, chat, err := h.createSession(params)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				_ = conn.sendError(req.ID, ahptypes.ErrorCodeSessionAlreadyExists, err.Error())
			} else if strings.Contains(err.Error(), "provider not found") {
				_ = conn.sendError(req.ID, ahptypes.ErrorCodeProviderNotFound, err.Error())
			} else {
				_ = conn.sendError(req.ID, ahptypes.ErrorCodeInternalError, err.Error())
			}
			return
		}

		// Broadcast session added to root subscribers
		h.broadcastNotification(ahptypes.RootResourceURI, "root/sessionAdded", ahptypes.SessionAddedParams{
			Channel: ahptypes.RootResourceURI,
			Summary: sess.summary(),
		})
		h.broadcastAction(ahptypes.RootResourceURI, ahptypes.StateAction{
			Value: &ahptypes.RootActiveSessionsChangedAction{
				Type:           ahptypes.ActionTypeRootActiveSessionsChanged,
				ActiveSessions: *h.root.ActiveSessions,
			},
		})

		res := map[string]any{
			"session":  sess.resource,
			"resource": sess.resource,
			"chat":     chat.resource,
			"summary":  sess.summary(),
		}
		_ = conn.sendResult(req.ID, res)

	case "session.dispose", "disposeSession":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.DisposeSessionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid disposeSession params")
			return
		}
		if !h.disposeSession(params.Channel) {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeSessionNotFound, "session not found")
			return
		}
		_ = conn.sendResult(req.ID, map[string]any{})

	case "createChat", "chat.create":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.CreateChatParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid createChat params")
			return
		}
		chat, err := h.createChat(params)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				_ = conn.sendError(req.ID, ahptypes.ErrorCodeSessionNotFound, err.Error())
			} else if strings.Contains(err.Error(), "already exists") {
				_ = conn.sendError(req.ID, ahptypes.ErrorCodeAlreadyExists, err.Error())
			} else {
				_ = conn.sendError(req.ID, ahptypes.ErrorCodeInternalError, err.Error())
			}
			return
		}

		h.broadcastAction(chat.sessionURI, ahptypes.StateAction{
			Value: &ahptypes.SessionChatAddedAction{
				Type:    ahptypes.ActionTypeSessionChatAdded,
				Summary: chat.summary(),
			},
		})

		if params.InitialMessage != nil {
			turnAction := &ahptypes.ChatTurnStartedAction{
				Type:      ahptypes.ActionTypeChatTurnStarted,
				TurnId:    fmt.Sprintf("turn_%d", time.Now().UnixNano()),
				StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Message:   *params.InitialMessage,
			}
			h.startTurn(chat, turnAction)
		}

		_ = conn.sendResult(req.ID, map[string]any{
			"chat":    chat.resource,
			"summary": chat.summary(),
		})

	case "disposeChat", "chat.dispose":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.DisposeChatParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid disposeChat params")
			return
		}
		if !h.disposeChat(params.Channel) {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeNotFound, "chat not found")
			return
		}
		_ = conn.sendResult(req.ID, map[string]any{})

	case "fetchTurns":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.FetchTurnsParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid fetchTurns params")
			return
		}
		chURI := canonicalURI(params.Channel)
		h.mu.RLock()
		_, ok := h.chats[chURI]
		h.mu.RUnlock()
		if !ok {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeNotFound, "chat channel not found")
			return
		}
		_ = conn.sendResult(req.ID, ahptypes.FetchTurnsResult{})

	case "dispatchAction", "dispatch":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.DispatchActionParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid dispatchAction params")
			return
		}
		h.handleDispatchAction(conn, params)
		_ = conn.sendResult(req.ID, map[string]any{})

	case "createTerminal", "terminal.create", "terminal/create":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var rawParams struct {
			Channel string                  `json:"channel"`
			Claim   *ahptypes.TerminalClaim `json:"claim"`
			Name    *string                 `json:"name"`
			Cwd     *ahptypes.URI           `json:"cwd"`
			Cols    *int64                  `json:"cols"`
			Rows    *int64                  `json:"rows"`
		}
		if err := json.Unmarshal(req.Params, &rawParams); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, fmt.Sprintf("invalid createTerminal params: %v", err))
			return
		}
		params := ahptypes.CreateTerminalParams{
			Channel: ahptypes.URI(rawParams.Channel),
			Name:    rawParams.Name,
			Cwd:     rawParams.Cwd,
			Cols:    rawParams.Cols,
			Rows:    rawParams.Rows,
		}
		if rawParams.Claim != nil {
			params.Claim = *rawParams.Claim
		}
		term, err := h.createTerminal(conn, params)
		if err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInternalError, err.Error())
			return
		}
		_ = conn.sendResult(req.ID, map[string]any{
			"channel":  term.channel,
			"resource": term.channel,
		})

	case "disposeTerminal", "terminal.dispose", "terminal/dispose":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.DisposeTerminalParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid disposeTerminal params")
			return
		}
		if err := h.disposeTerminal(string(params.Channel)); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, err.Error())
			return
		}
		_ = conn.sendResult(req.ID, map[string]any{})

	case "terminals", "terminal.list", "terminal/list":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		h.mu.RLock()
		terms := append([]ahptypes.TerminalInfo(nil), h.root.Terminals...)
		h.mu.RUnlock()
		_ = conn.sendResult(req.ID, terms)

	case "reconnect":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var params ahptypes.ReconnectParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid reconnect params")
			return
		}
		conn.clientID = params.ClientId
		h.mu.Lock()
		h.clients[conn.clientID] = conn

		var (
			validSubs   []string
			missingSubs []ahptypes.URI
		)
		for _, ch := range params.Subscriptions {
			cCh := canonicalURI(ch)
			if cCh == ahptypes.RootResourceURI || h.sessions[cCh] != nil || h.chats[cCh] != nil || h.terminals[cCh] != nil {
				validSubs = append(validSubs, cCh)
				if h.subs[cCh] == nil {
					h.subs[cCh] = make(map[*ahpConn]bool)
				}
				h.subs[cCh][conn] = true
				conn.subsMu.Lock()
				conn.subs[cCh] = true
				conn.subsMu.Unlock()
			} else {
				missingSubs = append(missingSubs, ch)
			}
		}

		lastSeen := params.LastSeenServerSeq
		currSeq := h.seq
		canReplay := false
		var missed []ahptypes.ActionEnvelope
		if lastSeen == currSeq {
			canReplay = true
			missed = []ahptypes.ActionEnvelope{}
		} else if len(h.replay) > 0 && lastSeen >= h.replay[0].ServerSeq-1 && lastSeen < currSeq {
			canReplay = true
			for _, a := range h.replay {
				if a.ServerSeq > lastSeen {
					missed = append(missed, a)
				}
			}
		}
		h.mu.Unlock()

		if canReplay {
			if missed == nil {
				missed = []ahptypes.ActionEnvelope{}
			}
			if missingSubs == nil {
				missingSubs = []ahptypes.URI{}
			}
			_ = conn.sendResult(req.ID, map[string]any{
				"type":    "replay",
				"actions": missed,
				"missing": missingSubs,
			})
		} else {
			var snaps []any
			for _, ch := range validSubs {
				if s := h.snapshotFor(ch); s != nil {
					snaps = append(snaps, s)
				}
			}
			if snaps == nil {
				snaps = []any{}
			}
			_ = conn.sendResult(req.ID, map[string]any{
				"type":      "snapshot",
				"snapshots": snaps,
			})
		}

	case "prompt", "chat/prompt":
		// Direct prompt convenience RPC
		if h.srv.cfg.Token != "" && !conn.authenticated {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeAuthRequired, "authentication required")
			return
		}
		var p struct {
			Channel string `json:"channel"`
			Prompt  string `json:"prompt"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeInvalidParams, "invalid prompt params")
			return
		}
		ch := canonicalURI(p.Channel)
		h.mu.RLock()
		chat, ok := h.chats[ch]
		if !ok {
			if sess, sessOk := h.sessions[ch]; sessOk && sess.defaultChat != "" {
				chat = h.chats[sess.defaultChat]
			}
		}
		h.mu.RUnlock()
		if chat == nil {
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeNotFound, "chat channel not found")
			return
		}

		chat.turnMu.Lock()
		if chat.turnCancel != nil {
			chat.turnMu.Unlock()
			_ = conn.sendError(req.ID, ahptypes.ErrorCodeTurnInProgress, "turn already in progress")
			return
		}
		chat.turnMu.Unlock()

		turnAction := &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    fmt.Sprintf("turn_%d", time.Now().UnixNano()),
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: p.Prompt,
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		}
		h.startTurn(chat, turnAction)
		_ = conn.sendResult(req.ID, map[string]any{"turnId": turnAction.TurnId})

	default:
		_ = conn.sendError(req.ID, ahptypes.ErrorCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (h *AHPHost) handleNotification(ctx context.Context, conn *ahpConn, notif ahptypes.JsonRpcNotification) {
	switch notif.Method {
	case "dispatchAction", "dispatch":
		if h.srv.cfg.Token != "" && !conn.authenticated {
			return
		}
		var params ahptypes.DispatchActionParams
		if err := json.Unmarshal(notif.Params, &params); err != nil {
			return
		}
		h.handleDispatchAction(conn, params)
	case "unsubscribe":
		var params ahptypes.UnsubscribeParams
		if err := json.Unmarshal(notif.Params, &params); err != nil {
			return
		}
		h.removeSubscription(conn, params.Channel)
	}
}

func (h *AHPHost) handleDispatchAction(conn *ahpConn, params ahptypes.DispatchActionParams) {
	chURI := canonicalURI(params.Channel)

	h.mu.RLock()
	chat, isChat := h.chats[chURI]
	if !isChat {
		if sess, isSess := h.sessions[chURI]; isSess && sess.defaultChat != "" {
			chat = h.chats[sess.defaultChat]
			isChat = true
			chURI = sess.defaultChat
		}
	}
	term, isTerm := h.terminals[chURI]
	h.mu.RUnlock()

	origin := &ahptypes.ActionOrigin{
		ClientId:  conn.clientID,
		ClientSeq: params.ClientSeq,
	}

	if isChat && chat != nil {
		if turnStarted, ok := params.Action.Value.(*ahptypes.ChatTurnStartedAction); ok {
			h.startTurn(chat, turnStarted, origin)
			return
		}
		if _, ok := params.Action.Value.(*ahptypes.ChatTurnCancelledAction); ok {
			chat.cancelTurn()
		}
	}

	if isTerm && term != nil {
		switch act := params.Action.Value.(type) {
		case *ahptypes.TerminalInputAction:
			_ = term.writeInput(act.Data)
			return
		case *ahptypes.TerminalResizedAction:
			_ = term.resize(act.Cols, act.Rows)
		case *ahptypes.TerminalClearedAction:
			term.clear()
		}
	}

	h.broadcastAction(chURI, params.Action, origin)
}

// ServeWebSocket upgrades and handles an incoming AHP WebSocket connection.
func (h *AHPHost) ServeWebSocket(ctx context.Context, wsConn *websocket.Conn, authenticated bool) error {
	conn := &ahpConn{
		host:          h,
		authenticated: authenticated,
		subs:          make(map[string]bool),
		sendFn: func(msg ahptypes.JsonRpcMessage) error {
			data, err := json.Marshal(msg)
			if err != nil {
				return err
			}
			return wsConn.Write(ctx, websocket.MessageText, data)
		},
		closeFn: func() error {
			return wsConn.Close(websocket.StatusNormalClosure, "closed")
		},
	}
	defer h.removeConn(conn)

	for {
		typ, data, err := wsConn.Read(ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageText {
			h.handleMessage(ctx, conn, data)
		}
	}
}

// ServeReadWriter serves the AHP protocol over raw reader/writer streams.
func (h *AHPHost) ServeReadWriter(ctx context.Context, peerInput io.Writer, peerOutput io.Reader, authenticated bool) error {
	var writeMu sync.Mutex
	conn := &ahpConn{
		host:          h,
		authenticated: authenticated,
		subs:          make(map[string]bool),
		sendFn: func(msg ahptypes.JsonRpcMessage) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			data, err := json.Marshal(msg)
			if err != nil {
				return err
			}
			_, err = peerInput.Write(append(data, '\n'))
			return err
		},
		closeFn: func() error {
			return nil
		},
	}
	defer h.removeConn(conn)

	scanner := bufio.NewScanner(peerOutput)
	// Buffer up to 32MB frames
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 32*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		h.handleMessage(ctx, conn, line)
	}
	return scanner.Err()
}

// ServeAHPWebSocket upgrades an incoming HTTP request to a WebSocket and serves AHP.
func (s *Server) ServeAHPWebSocket(w http.ResponseWriter, r *http.Request) error {
	authenticated := s.authorized(r)
	if !authenticated && s.cfg.Token != "" {
		qToken := r.URL.Query().Get("token")
		if qToken == "" {
			qToken = r.URL.Query().Get("tkn")
		}
		if qToken == s.cfg.Token {
			authenticated = true
		}
	}

	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return err
	}
	defer wsConn.Close(websocket.StatusNormalClosure, "closed")

	host := s.AHPHost()
	return host.ServeWebSocket(r.Context(), wsConn, authenticated)
}

// ServeAHP streams the AHP protocol over arbitrary reader and writer streams.
func (s *Server) ServeAHP(ctx context.Context, peerInput io.Writer, peerOutput io.Reader) error {
	host := s.AHPHost()
	return host.ServeReadWriter(ctx, peerInput, peerOutput, true)
}

// ServeAHPConn streams the AHP protocol over a raw network connection.
func (s *Server) ServeAHPConn(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	return s.ServeAHP(ctx, conn, conn)
}

package lsproto

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ACPBridge bridges between the LanguageServerService protocol (lsproto)
// and the Agent Client Protocol (ACP).
//
// It provides a standardized, minimal translation layer for IDEs and
// agent clients (e.g. Cursor, Zed, VS Code) to initialize sessions,
// exchange messages, and subscribe to streaming turn events using
// standard ACP data models backed by the LanguageServer descriptor schema.
type ACPBridge struct {
	codec *Codec

	mu       sync.RWMutex
	sessions map[string]*ACPSession
}

// ACPImplementation identifies a participant in the ACP protocol.
type ACPImplementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// ACPClientCapabilities describes capabilities offered by the ACP client.
type ACPClientCapabilities struct {
	Streaming     bool `json:"streaming,omitempty"`
	ToolApprovals bool `json:"toolApprovals,omitempty"`
}

// ACPAgentCapabilities describes capabilities supported by this ACP agent bridge.
type ACPAgentCapabilities struct {
	PromptSession   bool     `json:"promptSession"`
	CancelSession   bool     `json:"cancelSession"`
	StreamingEvents bool     `json:"streamingEvents"`
	LSPMethods      []string `json:"lspMethods,omitempty"`
}

// ACPAuthMethod describes an authentication method advertised during initialization.
type ACPAuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ACPInitializeRequest is the standard initialization handshake payload.
type ACPInitializeRequest struct {
	ProtocolVersion int                   `json:"protocolVersion"`
	ClientInfo      ACPImplementation     `json:"clientInfo"`
	Capabilities    ACPClientCapabilities `json:"capabilities,omitempty"`
}

// ACPInitializeResponse is the agent's initialization answer.
type ACPInitializeResponse struct {
	ProtocolVersion   int                  `json:"protocolVersion"`
	AgentInfo         ACPImplementation    `json:"agentInfo"`
	AgentCapabilities ACPAgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []ACPAuthMethod      `json:"authMethods,omitempty"`
}

// ACPEventType names an event kind in the ACP streaming channel.
type ACPEventType string

const (
	ACPEventTextDelta    ACPEventType = "text_delta"
	ACPEventThoughtDelta ACPEventType = "thought_delta"
	ACPEventToolCall     ACPEventType = "tool_call"
	ACPEventStateChange  ACPEventType = "state_change"
	ACPEventDone         ACPEventType = "done"
	ACPEventError        ACPEventType = "error"
)

// ACPToolCall represents a tool call reported over the ACP stream.
type ACPToolCall struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Args   string `json:"args,omitempty"`
	Result string `json:"result,omitempty"`
	Status string `json:"status,omitempty"` // "started", "completed", "error"
}

// ACPEvent is one streaming event frame emitted during a turn.
type ACPEvent struct {
	SessionID string       `json:"sessionId"`
	TurnID    string       `json:"turnId,omitempty"`
	Type      ACPEventType `json:"type"`
	Text      string       `json:"text,omitempty"`
	ToolCall  *ACPToolCall `json:"toolCall,omitempty"`
	State     string       `json:"state,omitempty"`
	Error     string       `json:"error,omitempty"`
	Timestamp int64        `json:"timestamp"`
}

// ACPMessage is one turn message in session history.
type ACPMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ACPSession holds the state and subscribers for one conversation.
type ACPSession struct {
	ID        string       `json:"id"`
	Workspace string       `json:"workspace"`
	Model     string       `json:"model"`
	CreatedAt time.Time    `json:"createdAt"`
	History   []ACPMessage `json:"history"`

	mu   sync.RWMutex
	subs map[chan ACPEvent]struct{}
}

// ACPNewSessionRequest requests creating an isolated session.
type ACPNewSessionRequest struct {
	SessionID string `json:"sessionId,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Model     string `json:"model,omitempty"`
}

// ACPNewSessionResponse reports the created session properties.
type ACPNewSessionResponse struct {
	SessionID string `json:"sessionId"`
	Workspace string `json:"workspace"`
	Model     string `json:"model"`
}

// ACPPromptRequest submits a prompt to a session.
type ACPPromptRequest struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model,omitempty"`
}

// ACPPromptResponse reports the prompt outcome.
type ACPPromptResponse struct {
	SessionID string `json:"sessionId"`
	TurnID    string `json:"turnId"`
	Status    string `json:"status"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
}

// NewACPBridge creates a new ACPBridge backed by the given Codec.
// If codec is nil, the process-wide shared Codec is used.
func NewACPBridge(codec *Codec) (*ACPBridge, error) {
	if codec == nil {
		c, err := Shared()
		if err != nil {
			return nil, fmt.Errorf("lsproto: init codec for acp bridge: %w", err)
		}
		codec = c
	}
	return &ACPBridge{
		codec:    codec,
		sessions: make(map[string]*ACPSession),
	}, nil
}

// Initialize performs the standard ACP initialization handshake,
// verifying available language-server methods and advertising capabilities.
func (b *ACPBridge) Initialize(ctx context.Context, req ACPInitializeRequest) (*ACPInitializeResponse, error) {
	if req.ProtocolVersion < 1 {
		return nil, errors.New("lsproto: acp initialize: protocol version must be >= 1")
	}

	streamingMethods := b.codec.ServerStreamingMethods()
	// Filter prominent agent methods
	var keyMethods []string
	for _, m := range streamingMethods {
		switch m {
		case "StreamAgentStateUpdates", "SetupJetskiChat", "HandleStreamingCommand", "ProjectUpdatesStream":
			keyMethods = append(keyMethods, m)
		}
	}

	return &ACPInitializeResponse{
		ProtocolVersion: req.ProtocolVersion,
		AgentInfo: ACPImplementation{
			Name:    "pigasus-lsproto-acp",
			Title:   "Pigasus ACP Bridge",
			Version: "1.5.14",
		},
		AgentCapabilities: ACPAgentCapabilities{
			PromptSession:   true,
			CancelSession:   true,
			StreamingEvents: true,
			LSPMethods:      keyMethods,
		},
		AuthMethods: []ACPAuthMethod{
			{
				ID:          "token",
				Name:        "Bearer Token",
				Description: "Authorization via Bearer token",
			},
		},
	}, nil
}

func randomID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

// NewSession creates or registers an isolated ACP session.
func (b *ACPBridge) NewSession(ctx context.Context, req ACPNewSessionRequest) (*ACPNewSessionResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := req.SessionID
	if id == "" {
		id = randomID("acp-sess")
	}

	model := req.Model
	if model == "" {
		model = "gemini-3.8-flash-high"
	}

	sess := &ACPSession{
		ID:        id,
		Workspace: req.Workspace,
		Model:     model,
		CreatedAt: time.Now().UTC(),
		subs:      make(map[chan ACPEvent]struct{}),
	}
	b.sessions[id] = sess

	return &ACPNewSessionResponse{
		SessionID: id,
		Workspace: sess.Workspace,
		Model:     sess.Model,
	}, nil
}

// GetSession returns an active session by ID.
func (b *ACPBridge) GetSession(sessionID string) (*ACPSession, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	sess, ok := b.sessions[sessionID]
	return sess, ok
}

// Subscribe opens a non-interactive event streaming subscription channel for sessionID.
// Slow consumers are protected with non-blocking buffer drops.
func (b *ACPBridge) Subscribe(sessionID string) (<-chan ACPEvent, func(), error) {
	b.mu.Lock()
	sess, ok := b.sessions[sessionID]
	if !ok {
		// Automatically allocate session if not yet present
		sess = &ACPSession{
			ID:        sessionID,
			CreatedAt: time.Now().UTC(),
			subs:      make(map[chan ACPEvent]struct{}),
		}
		b.sessions[sessionID] = sess
	}
	b.mu.Unlock()

	sess.mu.Lock()
	ch := make(chan ACPEvent, 128)
	sess.subs[ch] = struct{}{}
	sess.mu.Unlock()

	cleanup := func() {
		sess.mu.Lock()
		delete(sess.subs, ch)
		close(ch)
		sess.mu.Unlock()
	}

	return ch, cleanup, nil
}

// EmitEvent broadcasts an ACP event to all active subscribers of sessionID.
func (b *ACPBridge) EmitEvent(sessionID string, ev ACPEvent) {
	b.mu.RLock()
	sess, ok := b.sessions[sessionID]
	b.mu.RUnlock()
	if !ok || sess == nil {
		return
	}

	if ev.Timestamp == 0 {
		ev.Timestamp = time.Now().UnixMilli()
	}
	if ev.SessionID == "" {
		ev.SessionID = sessionID
	}

	sess.mu.RLock()
	defer sess.mu.RUnlock()
	for ch := range sess.subs {
		select {
		case ch <- ev:
		default:
			// Non-blocking drop on subscriber backpressure
		}
	}
}

// Prompt processes a prompt submission, appending to history and emitting state/text events.
func (b *ACPBridge) Prompt(ctx context.Context, req ACPPromptRequest) (*ACPPromptResponse, error) {
	if req.SessionID == "" {
		return nil, errors.New("lsproto: acp prompt: sessionId is required")
	}

	b.mu.Lock()
	sess, ok := b.sessions[req.SessionID]
	if !ok {
		sess = &ACPSession{
			ID:        req.SessionID,
			CreatedAt: time.Now().UTC(),
			Model:     req.Model,
			subs:      make(map[chan ACPEvent]struct{}),
		}
		b.sessions[req.SessionID] = sess
	}
	b.mu.Unlock()

	turnID := randomID("turn")
	now := time.Now().UnixMilli()

	// 1. Record prompt in history
	sess.mu.Lock()
	sess.History = append(sess.History, ACPMessage{Role: "user", Content: req.Prompt})
	sess.mu.Unlock()

	// 2. Emit state change to working
	b.EmitEvent(req.SessionID, ACPEvent{
		SessionID: req.SessionID,
		TurnID:    turnID,
		Type:      ACPEventStateChange,
		State:     "working",
		Timestamp: now,
	})

	return &ACPPromptResponse{
		SessionID: req.SessionID,
		TurnID:    turnID,
		Status:    "started",
	}, nil
}

// FormatSessionUpdateProtoJSON converts an ACPEvent to standard proto-JSON
// compatible with LanguageServerService StreamAgentStateUpdates.
func (b *ACPBridge) FormatSessionUpdateProtoJSON(ev ACPEvent) ([]byte, error) {
	stepPayload := map[string]any{
		"sessionId": ev.SessionID,
		"turnId":    ev.TurnID,
		"type":      string(ev.Type),
		"text":      ev.Text,
		"timestamp": ev.Timestamp,
	}
	if ev.ToolCall != nil {
		stepPayload["toolCall"] = ev.ToolCall
	}
	if ev.State != "" {
		stepPayload["state"] = ev.State
	}
	if ev.Error != "" {
		stepPayload["error"] = ev.Error
	}

	wrapper := map[string]any{
		"agentStateUpdates": []map[string]any{
			{
				"conversationId": ev.SessionID,
				"rawEvent":       stepPayload,
			},
		},
	}
	return json.Marshal(wrapper)
}

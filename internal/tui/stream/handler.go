package stream

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// TurnState represents the streaming cycle status of an agent turn.
type TurnState int

const (
	StateIdle TurnState = iota
	StatePrompting
	StateStreaming
	StateCompacting
	StateInterrupted
)

// StreamEvent carries progress and token events from the turn execution.
type StreamEvent struct {
	TurnID    string
	Role      string
	Content   string
	Delta     string
	Done      bool
	Error     error
	Timestamp time.Time
}

// Notification represents a live notification emitted during turn execution.
type Notification struct {
	ID        string
	Category  string
	Title     string
	Message   string
	Severity  string
	CreatedAt time.Time
}

// Host encapsulates the callbacks and state queries required by stream handlers.
type Host interface {
	ActiveModel() string
	IsStreaming() bool
	AppendStreamDelta(delta string)
	OnTurnComplete(reply string, err error) tea.Cmd
	Notify(n Notification)
}

// Handler orchestrates turn stream lifecycle and event delivery.
type Handler struct {
	host Host
}

// NewHandler returns a new stream Handler bound to the given host.
func NewHandler(host Host) *Handler {
	return &Handler{host: host}
}

// HandleEvent processes an incoming StreamEvent.
func (h *Handler) HandleEvent(ev StreamEvent) tea.Cmd {
	if ev.Error != nil {
		return h.host.OnTurnComplete("", ev.Error)
	}
	if ev.Delta != "" {
		h.host.AppendStreamDelta(ev.Delta)
	}
	if ev.Done {
		return h.host.OnTurnComplete(ev.Content, nil)
	}
	return nil
}

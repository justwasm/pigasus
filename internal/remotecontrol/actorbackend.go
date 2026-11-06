// Package remotecontrol — actorbackend.go: the production
// MessageBackend that replaces StubBackend at the cascade seam.
package remotecontrol

import (
	"context"
	"errors"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// SessionEventType classifies observable occurrences emitted during a session turn.
type SessionEventType string

const (
	SessionEventStart     SessionEventType = "start"
	SessionEventTextDelta SessionEventType = "text_delta"
	SessionEventToolCall  SessionEventType = "tool_call"
	SessionEventDone      SessionEventType = "done"
	SessionEventError     SessionEventType = "error"
)

// SessionEvent represents an event frame emitted by a Session.
type SessionEvent struct {
	Type       SessionEventType
	Delta      string
	ToolCall   cascades.ToolCall
	StopReason string
	Error      error
}

// SessionMessage is an assistant turn outcome message.
type SessionMessage struct {
	Role         string
	Content      string
	ToolCalls    []cascades.ToolCall
	StopReason   string
	ErrorMessage string
}

// Session is the abstract seam ActorBackend.SessionFactory produces.
type Session interface {
	Send(ctx context.Context, prompt string, events chan<- SessionEvent) ([]SessionMessage, error)
	Close() error
}

// SessionFactory is the seam ActorBackend uses to mint a fresh Session per SendStream call.
type SessionFactory func(ctx context.Context, prompt cascades.Prompt, opts cascades.SendOptions) (Session, error)

// ActorBackend is the production MessageBackend + cascades.StreamingBackend.
type ActorBackend struct {
	SessionFactory SessionFactory
}

// PiagentBackend is an alias for ActorBackend for backward compatibility.
type PiagentBackend = ActorBackend

var (
	_ cascades.Backend          = (*ActorBackend)(nil)
	_ cascades.StreamingBackend = (*ActorBackend)(nil)
)

// Send implements MessageBackend.
func (b *ActorBackend) Send(ctx context.Context, prompt cascades.Prompt) (cascades.Result, error) {
	return b.SendStream(ctx, prompt, nil, cascades.SendOptions{})
}

var ErrNilSessionFactory = errors.New("actor: SessionFactory is nil")
var ErrSessionClosed = errors.New("actor: session event channel closed without done")

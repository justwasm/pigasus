package agentturn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/justwasm/pigasus/agentlink"
)

// cancelTimeout bounds the cancel frame's write.
const cancelTimeout = 2 * time.Second

// Remote runs turns on a pigasus server.
//
// The remote engine is the mirror of the local one: it sends the
// transcript the turn replays and reports the server's frames as they
// arrive. The frames are the server's own — the remote engine does not
// rewrite them — so a turn renders the same whether it ran here or
// there.
//
// The connection carries the whole session, not one turn: a cancelled
// turn stops the turn, not the connection. The frames are read by a
// reader of their own for exactly that reason — a read that a turn's
// cancellation interrupts would take the session's next turn with it.
type Remote struct {
	client *agentlink.Client

	mu sync.Mutex
	// busy is true while a turn is in flight. The protocol carries one
	// turn at a time per connection, so a second one would read the
	// first's frames.
	busy   bool
	closed bool

	// frames carries the frames the reader read. It is closed when the
	// connection ends.
	frames chan agentlink.Frame

	// readErr is the error the connection ended with.
	readErr error
}

// NewRemote returns an engine that runs the turns on a server.
//
// The engine starts reading the connection right away: the frames a
// server sends outside a turn — a notice, a state change — belong to
// the session, not to the turn that happens to be running.
func NewRemote(client *agentlink.Client) *Remote {
	r := &Remote{client: client, frames: make(chan agentlink.Frame, frameBuffer)}
	go r.readLoop()
	return r
}

// readLoop reads the connection's frames until it ends.
func (r *Remote) readLoop() {
	defer close(r.frames)
	for {
		f, err := r.client.Next(context.Background())
		if err != nil {
			r.mu.Lock()
			r.readErr = err
			r.mu.Unlock()
			return
		}
		r.frames <- f
	}
}

// Turn asks the server to run one turn and reports its frames on the
// returned channel.
//
// The channel is closed when the turn ends. A cancelled turn sends the
// server a cancel frame first, so the server stops the work rather than
// running it to the end.
func (r *Remote) Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("agentturn: the engine is closed")
	}
	if r.client == nil {
		r.mu.Unlock()
		return nil, errors.New("agentturn: the engine has no connection")
	}
	if r.busy {
		r.mu.Unlock()
		return nil, errors.New("agentturn: a turn is already running")
	}
	r.busy = true
	r.mu.Unlock()

	id, err := turnID()
	if err != nil {
		r.mu.Lock()
		r.busy = false
		r.mu.Unlock()
		return nil, err
	}

	prompt := agentlink.Frame{
		Kind:    agentlink.KindPrompt,
		ID:      id,
		Model:   t.Model,
		History: t.History,
	}
	if n := len(t.History); n > 0 {
		prompt.Text = t.History[n-1].Content
	}
	if err := r.client.Send(ctx, prompt); err != nil {
		r.mu.Lock()
		r.busy = false
		r.mu.Unlock()
		return nil, err
	}

	ch := make(chan agentlink.Frame, frameBuffer)
	go func() {
		defer close(ch)
		defer func() {
			r.mu.Lock()
			r.busy = false
			r.mu.Unlock()
		}()
		r.read(ctx, id, ch)
	}()
	return ch, nil
}

// Close releases the engine. The connection is closed with it: the
// reader waits on it, and a session that is over has nothing left to
// read.
func (r *Remote) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}

// read forwards the frames of one turn.
//
// The frames come from the connection's reader, so a turn that is
// cancelled stops reading and the connection stays open for the next
// one.
func (r *Remote) read(ctx context.Context, id string, out chan<- agentlink.Frame) {
	for {
		select {
		case <-ctx.Done():
			// The turn was cancelled: ask the server to stop
			// rather than let it run to the end.
			r.cancel(id)
			return
		case f, ok := <-r.frames:
			if !ok {
				out <- agentlink.Frame{Kind: agentlink.KindResult, Error: r.connectionError()}
				return
			}
			if f.ID != "" && f.ID != id {
				// A frame for another turn. The protocol carries one
				// turn at a time, so this is a late frame of a
				// turn that was cancelled; dropping it keeps the
				// turn's own stream intact.
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

// cancel asks the server to stop the turn.
//
// The frame is best effort: the connection may already be gone, and a
// turn that is over needs no stop. The write is bounded, so a server
// that stopped reading cannot hold the client.
func (r *Remote) cancel(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	_ = r.client.Send(ctx, agentlink.Frame{Kind: agentlink.KindCancel, ID: id})
}

// connectionError is the failure the connection ended with.
func (r *Remote) connectionError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readErr == nil {
		return "agentturn: the connection closed"
	}
	return r.readErr.Error()
}

// turnID mints the identifier that correlates a turn's frames.
func turnID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// EngineName identifies this engine as the native pigasus engine.
func (r *Remote) EngineName() string {
	return "pigasus"
}

// Capabilities returns all capabilities supported by the remote pigasus engine.
func (r *Remote) Capabilities() Capability {
	return CapAll
}

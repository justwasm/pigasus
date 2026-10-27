package agentbrowser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// defaultRequestTimeout is the reference's base timeout for one
	// browser action.
	//
	// 【反编译】 2026-10-27, `agentbrowser.requestTimeoutForAction`:
	// the base timeout is 30s and only a batch widens it.
	defaultRequestTimeout = 30 * time.Second

	// batchStepBudget is what one step of a batch may take.
	batchStepBudget = 20 * time.Second
	// batchMinBudget and batchMaxBudget clamp the batch's own budget.
	batchMinBudget = 20 * time.Second
	batchMaxBudget = 120 * time.Second
	// batchSlack is what the timeout adds on top of the batch budget.
	batchSlack = 10 * time.Second
)

// The reference's own error strings. They are part of the contract: the
// IDE half reads them, and the clone's browser subagent prompt quotes
// them.
//
// 【反编译】 2026-10-27, `agentbrowser` package strings.
var (
	// errBrowserInUse is the fence's base error: another browser
	// subagent owns the browser of this conversation.
	errBrowserInUse = errors.New("browser is currently in use by another browser subagent in this conversation")
	// errNoWindows is what the broker answers when no IDE window can
	// serve the request.
	errNoWindows = errors.New("no active AGY Hub webview browser windows connected to Language Server")
	// errWindowGone is what a request sees when its window closed
	// mid-flight.
	errWindowGone = errors.New("agent browser window disconnected")
)

// Broker is the reference's agentbrowser.StreamBroker: the rendezvous
// between the agent half (the `webview_browser` tool) and the IDE half
// (the frontend's AgentBrowserListener).
//
// The broker owns three tables:
//
//   - the connected windows, by window id;
//   - the pending requests, by request id;
//   - the browser owners, by root conversation id.
//
// 【反编译】 2026-10-27, `agentbrowser.StreamBroker` carries the
// windows, the pending frames and the per-root owner fence; the
// reference's method names are Subscribe, SubscriberCount, Store, Take,
// Resolve, Fulfill, Request, SupportsIsolatedWorld,
// WaitForSupportsIsolatedWorld, UpdateWindowDisplayedConversation,
// RecordUserMessageWindow and DeleteRootConversation.
type Broker struct {
	mu      sync.Mutex
	windows map[string]*window
	pending map[string]*pending
	owners  map[string]*owner
	// senderWindow remembers which window took the user's last message
	// for a conversation. The reference prefers that window when it
	// picks the window that runs an action.
	senderWindow map[string]string
	seq          uint64
	timeout      time.Duration
	now          func() time.Time
}

// window is one connected IDE window.
type window struct {
	sub      SubscribeRequest
	requests chan *Request
	closed   chan struct{}
	once     sync.Once
}

// pending is one request waiting for the IDE half's answer.
type pending struct {
	request  *Request
	windowID string
	response chan *Response
}

// owner is the browser subagent that owns one conversation's browser.
type owner struct {
	subagentID string
	since      time.Time
	windowID   string
}

// Subscription is one connected IDE window.
type Subscription struct {
	// WindowID is the window's own identifier.
	WindowID string
	// Requests carries the browser actions the agent asked for.
	Requests <-chan *Request
	// Close unregisters the window.
	Close func()
}

// NewBroker returns an empty broker.
func NewBroker() *Broker {
	return &Broker{
		windows:      map[string]*window{},
		pending:      map[string]*pending{},
		owners:       map[string]*owner{},
		senderWindow: map[string]string{},
		timeout:      defaultRequestTimeout,
		now:          time.Now,
	}
}

// Subscribe registers one IDE window and returns the stream its
// browser actions arrive on.
//
// A window that subscribes twice replaces its own earlier
// registration: the reference keeps one entry per window id.
func (b *Broker) Subscribe(ctx context.Context, sub SubscribeRequest) *Subscription {
	w := &window{
		sub:      sub,
		requests: make(chan *Request, 16),
		closed:   make(chan struct{}),
	}
	b.mu.Lock()
	if old, ok := b.windows[sub.WindowID]; ok {
		old.close()
	}
	b.windows[sub.WindowID] = w
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.unregister(sub.WindowID, w)
	}()
	return &Subscription{
		WindowID: sub.WindowID,
		Requests: w.requests,
		Close:    func() { b.unregister(sub.WindowID, w) },
	}
}

// SubscriberCount returns the number of connected windows.
func (b *Broker) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.windows)
}

// Request runs one browser action: it selects the window that must run
// it, hands the action over and waits for the IDE half's answer.
//
// The reference's own error strings are reproduced verbatim.
func (b *Broker) Request(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, errors.New("agentbrowser.Request cannot be nil")
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		return nil, errors.New("webview_browser requires a non-empty 'action'")
	}
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = b.nextRequestID()
	}
	w, err := b.selectWindow(req)
	if err != nil {
		return nil, err
	}
	p := b.register(req, w)
	defer b.dropPending(req.RequestID)
	timeout := b.requestTimeoutForAction(action, req.ParamsJSON)
	deadline := time.Now().Add(timeout)
	if err := b.send(ctx, w, req, deadline, timeout); err != nil {
		return nil, err
	}
	return b.await(ctx, p, timeout, deadline)
}

// Fulfill hands the IDE half's answer to the waiting agent half.
//
// 【反编译】 2026-10-27, `agentbrowser.StreamBroker.Fulfill`: a nil
// response, a response without a request id, a request id that is not
// pending and a window id that does not match the pending request's
// window are four distinct failures.
func (b *Broker) Fulfill(resp *Response) error {
	if resp == nil {
		return errors.New("agentbrowser.Response cannot be nil")
	}
	id := strings.TrimSpace(resp.RequestID)
	if id == "" {
		return errors.New("agentbrowser.Response.RequestID is required")
	}
	b.mu.Lock()
	p, ok := b.pending[id]
	if !ok {
		b.mu.Unlock()
		return fmt.Errorf("no pending agent browser request found for request_id=%q (may have timed out or already been fulfilled)", id)
	}
	if p.windowID != "" && resp.WindowID != "" && p.windowID != resp.WindowID {
		b.mu.Unlock()
		return fmt.Errorf("window_id mismatch for request_id=%q: got %q, want %q", id, resp.WindowID, p.windowID)
	}
	delete(b.pending, id)
	b.mu.Unlock()
	select {
	case p.response <- resp:
	default:
	}
	return nil
}

// UpdateWindow records the window's new state.
func (b *Broker) UpdateWindow(req UpdateSubscriptionRequest) error {
	id := strings.TrimSpace(req.WindowID)
	if id == "" {
		return errors.New("UpdateAgentBrowserSubscription requires window_id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.windows[id]
	if !ok {
		return fmt.Errorf("agent browser window %q is not connected to the Language Server", id)
	}
	w.sub.DisplayedRootConversationID = req.DisplayedRootConversationID
	w.sub.SupportsIsolatedWorld = req.SupportsIsolatedWorld
	return nil
}

// SupportsIsolatedWorld reports whether any connected window runs its
// webview in an isolated world.
func (b *Broker) SupportsIsolatedWorld() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, w := range b.windows {
		if w.sub.SupportsIsolatedWorld {
			return true
		}
	}
	return false
}

// RecordUserMessageWindow remembers the window that took the user's
// last message of a conversation. The broker prefers that window when
// it picks the one that runs an action.
func (b *Broker) RecordUserMessageWindow(rootConversationID, windowID string) {
	root := strings.TrimSpace(rootConversationID)
	id := strings.TrimSpace(windowID)
	if root == "" || id == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.senderWindow[root] = id
}

// DeleteRootConversation drops everything the broker remembers about a
// finished conversation.
func (b *Broker) DeleteRootConversation(rootConversationID string) {
	root := strings.TrimSpace(rootConversationID)
	if root == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.owners, root)
	delete(b.senderWindow, root)
}

// ClaimOwner records that ownerID owns the browser of one conversation.
//
// The fence is the reference's: a second browser subagent that reaches
// for a browser another subagent owns is refused with the reference's
// own message.
func (b *Broker) ClaimOwner(rootConversationID, ownerID, windowID string) error {
	root := strings.TrimSpace(rootConversationID)
	holder := strings.TrimSpace(ownerID)
	if root == "" || holder == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.owners[root]; ok && cur.subagentID != holder {
		if windowID != "" && cur.windowID == windowID {
			return fmt.Errorf("%w: owned by subagent %s", errBrowserInUse, cur.subagentID)
		}
		return fmt.Errorf("%w: subagent %s has owned the browser since %s", errBrowserInUse, cur.subagentID, cur.since.UTC().Format(time.RFC3339))
	}
	b.owners[root] = &owner{subagentID: holder, since: b.now(), windowID: windowID}
	return nil
}

// ReleaseOwner drops the browser ownership of one conversation.
func (b *Broker) ReleaseOwner(rootConversationID, ownerID string) {
	root := strings.TrimSpace(rootConversationID)
	holder := strings.TrimSpace(ownerID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.owners[root]; ok && cur.subagentID == holder {
		delete(b.owners, root)
	}
}

// nextRequestID mints the id of one request.
func (b *Broker) nextRequestID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	return fmt.Sprintf("agent-browser-%d", b.seq)
}

// register parks the request until the IDE half answers it.
func (b *Broker) register(req *Request, w *window) *pending {
	p := &pending{
		request:  req,
		windowID: w.sub.WindowID,
		response: make(chan *Response, 1),
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending[req.RequestID] = p
	return p
}

// dropPending forgets a request that timed out or was cancelled.
func (b *Broker) dropPending(requestID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, requestID)
}

// send hands the action to the window that must run it.
func (b *Broker) send(ctx context.Context, w *window, req *Request, deadline time.Time, timeout time.Duration) error {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case w.requests <- req:
		return nil
	case <-w.closed:
		return fmt.Errorf("agent browser request %q (%s) cancelled: %w", req.RequestID, req.Action, errWindowGone)
	case <-ctx.Done():
		return fmt.Errorf("agent browser request %q (%s) cancelled: %w", req.RequestID, req.Action, ctx.Err())
	case <-timer.C:
		return fmt.Errorf("timed out after %s waiting for AUIT webview to fulfill browser action %q (request_id=%s)",
			timeout, req.Action, req.RequestID)
	}
}

// await waits for the IDE half's answer.
func (b *Broker) await(ctx context.Context, p *pending, timeout time.Duration, deadline time.Time) (*Response, error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case resp := <-p.response:
		return resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("agent browser request %q (%s) cancelled: %w", p.request.RequestID, p.request.Action, ctx.Err())
	case <-timer.C:
		return nil, fmt.Errorf("timed out after %s waiting for AUIT webview to fulfill browser action %q (request_id=%s)",
			timeout, p.request.Action, p.request.RequestID)
	}
}

// unregister drops a window that closed.
func (b *Broker) unregister(id string, w *window) {
	b.mu.Lock()
	if cur, ok := b.windows[id]; ok && cur == w {
		delete(b.windows, id)
	}
	b.mu.Unlock()
	w.close()
}

// close releases the window's request channel.
func (w *window) close() {
	w.once.Do(func() { close(w.closed) })
}

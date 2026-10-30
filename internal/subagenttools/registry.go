package subagenttools

import (
	"context"
	"sort"
	"sync"
	"time"
)

// The states a live subagent can be in.
//
// 【反编译】 agy 1.4.4's `manage_subagents` list entry carries the
// reference's own state enum:
//
//	enum=running,idle,waiting_for_input,waiting_for_dependents,waiting_for_message,canceling,errored,unspecified
//
// The clone uses the ones a delegated turn can reach: a turn in flight, a
// turn that ended, a subagent that has gone quiet and can be woken by a
// message, one that is being killed, and one whose turn failed.
const (
	// StateRunning is a subagent whose turn is in flight.
	StateRunning = "running"
	// StateIdle is a subagent whose turn has ended.
	StateIdle = "idle"
	// StateWaitingForMessage is a subagent that has gone quiet and can be
	// woken by a message.
	StateWaitingForMessage = "waiting_for_message"
	// StateCanceling is a subagent that is being killed.
	StateCanceling = "canceling"
	// StateErrored is a subagent whose turn failed.
	StateErrored = "errored"
	// StateStopped is a subagent that was stopped or paused and can be resumed.
	StateStopped = "stopped"
)

// Message is one agent-to-agent message.
//
// The reference's own message carries a sender, a content, a priority and a
// timestamp (【descriptor】 exa.cortex_pb's AgentMessage); the clone keeps
// the three the delivery envelope renders.
type Message struct {
	// From is the conversation id of the sender.
	From string
	// Content is the message text.
	Content string
	// At is the moment the message was written.
	At time.Time
}

// Child is one live subagent.
//
// A child is a conversation of its own: it runs in the background, it is
// reachable by its conversation id, and it stays reachable until it is
// killed.
type Child struct {
	// ConversationID is the id the parent and the child know each other
	// by. It is what invoke_subagent's acknowledgement reports and what
	// send_message and manage_subagents take.
	ConversationID string
	// TypeName is the subagent type the child was launched as ("self",
	// "research", or a definition's own name).
	TypeName string
	// Role is the role the call asked for, or the type's name when the
	// call named none — the reference's own rule for the list entry.
	Role string
	// Worktree is the directory the child's session is anchored to.
	Worktree string

	mu        sync.Mutex
	state     string
	cancel    context.CancelFunc
	abortTurn func()
	inbox     chan Message
	reported  bool
	launchFn  func(ctx context.Context, initialMsg *Message)
	subDir    string
}

// NewChild returns a child in the running state.
//
// The cancel function ends the child's goroutine; the inbox carries the
// messages the parent sends it.
func NewChild(conversationID, typeName, role, worktree string, cancel context.CancelFunc) *Child {
	return &Child{
		ConversationID: conversationID,
		TypeName:       typeName,
		Role:           role,
		Worktree:       worktree,
		state:          StateRunning,
		cancel:         cancel,
		inbox:          make(chan Message, 8),
	}
}

// State returns the child's current state.
func (c *Child) State() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// setState moves the child to a new state.
func (c *Child) setState(state string) {
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
}

// Reported reports whether the child has sent something to its parent.
func (c *Child) Reported() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reported
}

// MarkReported records that the child sent something to its parent.
func (c *Child) MarkReported() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.reported = true
	c.mu.Unlock()
}

// Deliver hands a message to the child.
//
// It answers false when the child is gone: a killed subagent has no inbox
// left to read.
func (c *Child) Deliver(msg Message) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	inbox := c.inbox
	c.mu.Unlock()
	if inbox == nil {
		return false
	}
	if msg.At.IsZero() {
		msg.At = time.Now()
	}
	select {
	case inbox <- msg:
		return true
	default:
		return false
	}
}

// Wait blocks for the next message the parent sends.
//
// It answers false when the context is done or the child is killed.
func (c *Child) Wait(ctx context.Context) (Message, bool) {
	if c == nil {
		return Message{}, false
	}
	c.mu.Lock()
	inbox := c.inbox
	c.mu.Unlock()
	if inbox == nil {
		return Message{}, false
	}
	select {
	case <-ctx.Done():
		return Message{}, false
	case msg, ok := <-inbox:
		return msg, ok
	}
}

// Kill ends the child's turn and puts it in the canceling state.
func (c *Child) Kill() {
	if c == nil {
		return
	}
	c.setState(StateCanceling)
	c.mu.Lock()
	cancel := c.cancel
	abort := c.abortTurn
	c.mu.Unlock()
	if abort != nil {
		abort()
	}
	if cancel != nil {
		cancel()
	}
}

// Stop pauses/halts the child subagent and sets its state to StateStopped.
// The in-flight turn context is canceled, any abort callback is invoked,
// and the child remains registered and can be resumed/revived later without losing progress.
func (c *Child) Stop() {
	if c == nil {
		return
	}
	c.setState(StateStopped)
	c.mu.Lock()
	cancel := c.cancel
	abort := c.abortTurn
	c.mu.Unlock()
	if abort != nil {
		abort()
	}
	if cancel != nil {
		cancel()
	}
}

// Revive restarts or unpauses a stopped child subagent without losing progress.
// If initialMsg is provided, it is sent as the revival prompt.
func (c *Child) Revive(ctx context.Context, initialMsg *Message) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	if c.state == StateRunning {
		c.mu.Unlock()
		if initialMsg != nil {
			return c.Deliver(*initialMsg)
		}
		return false
	}
	launch := c.launchFn
	c.mu.Unlock()
	if launch == nil {
		return false
	}
	launch(ctx, initialMsg)
	return true
}

// SetLaunch installs the callback to restart/revive the child.
func (c *Child) SetLaunch(fn func(ctx context.Context, initialMsg *Message)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.launchFn = fn
}

// SetAbort installs the callback to abort the active session turn.
func (c *Child) SetAbort(fn func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.abortTurn = fn
}

// SetCancel sets the context cancel function for the current turn.
func (c *Child) SetCancel(cancel context.CancelFunc) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancel = cancel
}

// IsRunning reports whether the child is currently running.
func (c *Child) IsRunning() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state == StateRunning
}

// SubDir returns the persisted subagents directory for this child.
func (c *Child) SubDir() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subDir
}

// SetSubDir sets the persisted subagents directory for this child.
func (c *Child) SetSubDir(dir string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subDir = dir
}

// Registry is the conversation's set of live subagents.
//
// It belongs to the conversation, not to a turn: the parent's tools and the
// children's tools share it, so a subagent can be reached by its id from
// either side, and a child's own message reaches the parent through it.
//
// The reference scopes the same thing to the conversation —
// `manage_subagents` "Manage the direct subagents of this conversation" —
// and its `list` action reports "the direct subagents", so the registry
// holds the direct children only.
type Registry struct {
	mu       sync.Mutex
	parentID string
	children map[string]*Child
	notify   func(sender, content string)
	closed   bool
}

// NewRegistry returns an empty registry for a conversation.
func NewRegistry(parentID string) *Registry {
	return &Registry{
		parentID: parentID,
		children: map[string]*Child{},
	}
}

// ParentID returns the conversation id of the agent that owns the
// registry — the parent.
func (r *Registry) ParentID() string {
	if r == nil {
		return ""
	}
	return r.parentID
}

// SetNotify installs the function that delivers a message to the parent.
//
// The parent's own transport owns it: the TUI puts the message in the
// session's inbox (the same inbox a background task's notification uses),
// and the print mode hands it to the next turn.
func (r *Registry) SetNotify(fn func(sender, content string)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.notify = fn
	r.mu.Unlock()
}

// NotifyParent delivers a message to the parent conversation.
//
// It answers false when no transport is installed or the registry is closed.
func (r *Registry) NotifyParent(sender, content string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	notify := r.notify
	closed := r.closed
	r.mu.Unlock()
	if closed || notify == nil {
		return false
	}
	notify(sender, content)
	return true
}

// Add registers a live child.
func (r *Registry) Add(c *Child) {
	if r == nil || c == nil || c.ConversationID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.children == nil {
		r.children = map[string]*Child{}
	}
	r.children[c.ConversationID] = c
}

// Get returns the live child with that conversation id.
func (r *Registry) Get(conversationID string) (*Child, bool) {
	if r == nil || conversationID == "" {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.children[conversationID]
	return c, ok
}

// List returns the live children, ordered by conversation id.
//
// The order is the registry's own, not the launch order: the reference's
// list is a marshalled array of the states, and its order is not recovered.
func (r *Registry) List() []*Child {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Child, 0, len(r.children))
	for _, c := range r.children {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ConversationID < out[j].ConversationID
	})
	return out
}

// Busy reports whether a subagent is still working.
//
// A child is busy while its turn is in flight. The parent waits for a
// report only while that is true: once every child has finished its turn,
// there is nothing left to wait for — a child that has reported stays
// alive so that the parent can message it again, but it will not speak
// on its own.
func (r *Registry) Busy() bool {
	for _, c := range r.List() {
		if c.State() == StateRunning {
			return true
		}
	}
	return false
}

// Remove drops a child from the registry.
func (r *Registry) Remove(conversationID string) {
	if r == nil || conversationID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.children, conversationID)
}

// Stop halts the child with conversationID and puts it into StateStopped.
func (r *Registry) Stop(conversationID string) bool {
	if c, ok := r.Get(conversationID); ok {
		c.Stop()
		return true
	}
	return false
}

// Revive restarts or unpauses the child with conversationID without losing progress.
func (r *Registry) Revive(ctx context.Context, conversationID string, initialMsg *Message) bool {
	if c, ok := r.Get(conversationID); ok {
		return c.Revive(ctx, initialMsg)
	}
	return false
}

// Close kills every child and refuses further deliveries.
//
// The registry is closed when the conversation that owns it ends: the
// print run tears its runtime down, and the TUI's session ends.
func (r *Registry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closed = true
	children := make([]*Child, 0, len(r.children))
	for _, c := range r.children {
		children = append(children, c)
	}
	r.mu.Unlock()
	for _, c := range children {
		c.Kill()
	}
}

// StateEntry is one entry of the reference's `manage_subagents` list.
//
// 【反编译】 agy 1.4.4's list entry carries four fields — the type, the
// role, the conversation id and the state — and the field order is the
// struct's own.
type StateEntry struct {
	Type           string `json:"type"`
	Role           string `json:"role"`
	ConversationID string `json:"conversationId"`
	State          string `json:"state"`
}

// States renders the registry's children as the reference's list entries.
//
// The role falls back to the type's name when the call named none — the
// reference's own rule ("The subagent's role, or its type name if no role
// was set").
func (r *Registry) States() []StateEntry {
	children := r.List()
	out := make([]StateEntry, 0, len(children))
	for _, c := range children {
		role := c.Role
		if role == "" {
			role = c.TypeName
		}
		out = append(out, StateEntry{
			Type:           c.TypeName,
			Role:           role,
			ConversationID: c.ConversationID,
			State:          c.State(),
		})
	}
	return out
}

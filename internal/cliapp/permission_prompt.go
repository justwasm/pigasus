// permission_prompt.go — host wiring for interactive ACP / agent tool approval prompts.
//
// When an external ACP agent or tool runner needs user consent for sensitive actions
// (e.g. executing bash commands, writing outside workspace), this host prompt is raised.
// It supports:
// - [y] Approve once
// - [a] Always approve this tool
// - [n] Reject
// - [d] Reject with feedback / explanation (micro-acp pattern)

package cliapp

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
)

// Prompt represents an active or queued interactive tool approval prompt in the host.
type Prompt struct {
	mu       sync.Mutex
	once     sync.Once
	model    *permissionprompt.Model
	req      permissionprompt.Request
	done     chan permissionprompt.Result
	result   permissionprompt.Result
	resolved bool
	onClose  func(*Prompt)
}

// NewPrompt constructs and opens a new interactive permission approval prompt.
func NewPrompt(req permissionprompt.Request) *Prompt {
	p := &Prompt{
		req:  req,
		done: make(chan permissionprompt.Result, 1),
	}
	p.model = permissionprompt.New(req, permissionprompt.WithOnDecision(func(res permissionprompt.Result) {
		p.Resolve(res)
	}))
	return p
}

// Request returns the request payload for this prompt.
func (p *Prompt) Request() permissionprompt.Request {
	if p == nil {
		return permissionprompt.Request{}
	}
	return p.req
}

// Model returns the underlying Bubbletea view model.
func (p *Prompt) Model() *permissionprompt.Model {
	if p == nil {
		return nil
	}
	return p.model
}

// IsOpen reports whether the prompt is open and awaiting a decision.
func (p *Prompt) IsOpen() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.resolved && p.model.IsOpen()
}

// Done returns a receive-only channel that delivers the finalized Result.
func (p *Prompt) Done() <-chan permissionprompt.Result {
	if p == nil {
		ch := make(chan permissionprompt.Result, 1)
		ch <- permissionprompt.Result{Decision: permissionprompt.DecisionDeny}
		return ch
	}
	return p.done
}

// Wait blocks until the prompt is resolved by the user or the context is cancelled.
// If the context is cancelled, the prompt is automatically resolved with DecisionDeny.
func (p *Prompt) Wait(ctx context.Context) (permissionprompt.Result, error) {
	if p == nil {
		return permissionprompt.Result{Decision: permissionprompt.DecisionDeny}, nil
	}

	select {
	case res := <-p.done:
		return res, nil
	case <-ctx.Done():
		p.Resolve(permissionprompt.Result{
			Decision: permissionprompt.DecisionDeny,
			OptionID: p.req.MatchOptionID(permissionprompt.DecisionDeny),
		})
		return p.result, ctx.Err()
	}
}

// Resolve finalizes the prompt with the given Result.
func (p *Prompt) Resolve(res permissionprompt.Result) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.result = res
		p.resolved = true
		if p.model != nil {
			p.model.Close()
		}
		p.mu.Unlock()

		select {
		case p.done <- res:
		default:
		}

		if p.onClose != nil {
			p.onClose(p)
		}
	})
}

// HandleKey passes a key event to the prompt's underlying UI component.
// If the key resolves the decision, HandleKey automatically completes the prompt.
func (p *Prompt) HandleKey(key tea.Key) (permissionprompt.Result, bool) {
	if p == nil || !p.IsOpen() {
		return permissionprompt.Result{}, false
	}
	res, handled := p.model.HandleKey(key)
	if handled && res.Decision != permissionprompt.DecisionNone {
		p.Resolve(res)
		return res, true
	}
	return res, handled
}

// Update forwards tea.Msg events to the prompt.
func (p *Prompt) Update(msg tea.Msg) (*Prompt, tea.Cmd) {
	if p == nil || !p.IsOpen() {
		return p, nil
	}
	_, cmd := p.model.Update(msg)
	if res, has := p.model.Result(); has {
		p.Resolve(res)
	}
	return p, cmd
}

// View returns the rendered prompt string.
func (p *Prompt) View() string {
	if p == nil || !p.IsOpen() {
		return ""
	}
	return p.model.ViewString()
}

// Lines returns the rendered prompt lines for terminal display or overlay.
func (p *Prompt) Lines(width int) []string {
	if p == nil || !p.IsOpen() {
		return nil
	}
	return p.model.Lines(width)
}

// PermissionPromptState manages active and queued permission prompts in the host.
type PermissionPromptState struct {
	mu     sync.Mutex
	active *Prompt
	queue  []*Prompt
}

// NewPermissionPromptState creates a new host permission prompt state manager.
func NewPermissionPromptState() *PermissionPromptState {
	return &PermissionPromptState{}
}

// Raise creates and enqueues or activates a permission prompt for the session.
func (s *PermissionPromptState) Raise(req permissionprompt.Request) *Prompt {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := NewPrompt(req)
	p.onClose = func(closed *Prompt) {
		s.onPromptClosed(closed)
	}

	if s.active == nil {
		s.active = p
	} else {
		s.queue = append(s.queue, p)
	}
	return p
}

func (s *PermissionPromptState) onPromptClosed(closed *Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active == closed {
		if len(s.queue) > 0 {
			s.active = s.queue[0]
			s.queue = s.queue[1:]
		} else {
			s.active = nil
		}
	}
}

// Active returns the currently open permission prompt, or nil if none.
func (s *PermissionPromptState) Active() *Prompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// HasActive reports whether a permission prompt is currently active.
func (s *PermissionPromptState) HasActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active != nil && s.active.IsOpen()
}

// QueueLen returns the number of prompts waiting in the queue behind the active one.
func (s *PermissionPromptState) QueueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// DismissActive dismisses the currently active prompt with Deny.
func (s *PermissionPromptState) DismissActive(decisions ...permissionprompt.Decision) {
	dec := permissionprompt.DecisionDeny
	if len(decisions) > 0 {
		dec = decisions[0]
	}

	s.mu.Lock()
	active := s.active
	s.mu.Unlock()

	if active != nil {
		active.Resolve(permissionprompt.Result{
			Decision: dec,
			OptionID: active.req.MatchOptionID(dec),
		})
	}
}

// HandleKey routes key events to the active prompt.
func (s *PermissionPromptState) HandleKey(key tea.Key) (permissionprompt.Result, bool) {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()

	if active == nil || !active.IsOpen() {
		return permissionprompt.Result{}, false
	}
	return active.HandleKey(key)
}

// View returns the rendered string of the currently active prompt.
func (s *PermissionPromptState) View() string {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()

	if active == nil {
		return ""
	}
	return active.View()
}

// Lines returns the rendered lines of the currently active prompt.
func (s *PermissionPromptState) Lines(width int) []string {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()

	if active == nil {
		return nil
	}
	return active.Lines(width)
}

// Reset clears any active or queued prompts.
func (s *PermissionPromptState) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active != nil {
		s.active.Resolve(permissionprompt.Result{
			Decision: permissionprompt.DecisionDeny,
		})
		s.active = nil
	}
	for _, p := range s.queue {
		p.Resolve(permissionprompt.Result{
			Decision: permissionprompt.DecisionDeny,
		})
	}
	s.queue = nil
}

// DefaultPermissionPrompts is the shared session state manager for tool approvals.
var DefaultPermissionPrompts = NewPermissionPromptState()

// RaisePermissionPrompt raises an interactive permission prompt on the shared state.
func RaisePermissionPrompt(req permissionprompt.Request) *Prompt {
	return DefaultPermissionPrompts.Raise(req)
}

// ActivePermissionPrompt returns the currently active permission prompt, if any.
func ActivePermissionPrompt() *Prompt {
	return DefaultPermissionPrompts.Active()
}

// HandlePermissionPromptKey routes key events to the shared active permission prompt.
func HandlePermissionPromptKey(key tea.Key) (permissionprompt.Result, bool) {
	return DefaultPermissionPrompts.HandleKey(key)
}

// Host seams on runMode (without modifying runMode struct fields):

// raisePermissionPrompt raises a permission prompt for the session.
func (m *runMode) raisePermissionPrompt(req permissionprompt.Request) *Prompt {
	return DefaultPermissionPrompts.Raise(req)
}

// activePermissionPrompt returns the currently active permission prompt, if any.
func (m *runMode) activePermissionPrompt() *Prompt {
	return DefaultPermissionPrompts.Active()
}

// hasActivePermissionPrompt reports whether a permission prompt is awaiting input.
func (m *runMode) hasActivePermissionPrompt() bool {
	return DefaultPermissionPrompts.HasActive()
}

// handlePermissionKey handles a key for the active permission prompt, if any.
func (m *runMode) handlePermissionKey(key tea.Key) (permissionprompt.Result, bool) {
	return DefaultPermissionPrompts.HandleKey(key)
}

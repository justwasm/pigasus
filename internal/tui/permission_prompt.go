package tui

import (
	"context"
	"fmt"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
)

// ---- permission_prompt.go ----

// permission_prompt.go — host wiring for interactive ACP / agent tool approval prompts.
//
// When an external ACP agent or tool runner needs user consent for sensitive actions
// (e.g. executing bash commands, writing outside workspace), this host prompt is raised.
// It supports:
// - [y] Approve once
// - [a] Always approve this tool
// - [n] Reject
// - [d] Reject with feedback / explanation (micro-acp pattern)

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

// Host seams on RootModel (without modifying RootModel struct fields):

// deadcode: staged (host seam tested in permission_prompt_test.go)
func (m *RootModel) raisePermissionPrompt(req permissionprompt.Request) *Prompt {
	return DefaultPermissionPrompts.Raise(req)
}

// deadcode: staged (host seam tested in permission_prompt_test.go)
func (m *RootModel) activePermissionPrompt() *Prompt {
	return DefaultPermissionPrompts.Active()
}

// deadcode: staged (host seam tested in permission_prompt_test.go)
func (m *RootModel) hasActivePermissionPrompt() bool {
	return DefaultPermissionPrompts.HasActive()
}

// permissionPromptLines renders the active modal for the body. The prompt
// is raised by a tool goroutine (and, for --remote-agent, by the turn's
// own goroutine), so the view polls it instead of waiting for a message.
func (m *RootModel) permissionPromptLines(width int) []string {
	active := DefaultPermissionPrompts.Active()
	if active == nil || !active.IsOpen() {
		return nil
	}
	if width <= 0 {
		width = 80
	}
	return active.Lines(width)
}

// deadcode: staged (host seam tested in permission_prompt_test.go)
func (m *RootModel) handlePermissionKey(key tea.Key) (permissionprompt.Result, bool) {
	return DefaultPermissionPrompts.HandleKey(key)
}

// ---- elig_inline.go ----

// elig_inline.go — cycle 262: the eligibility notice's host wiring.
//
// Changelog 1.1.3 (internal/cli/testdata/changelog_reference.txt:686):
//
//	"Improved eligibility error handling by showing errors with a
//	 verification URL inline in the input loop instead of stacking
//	 them above the screen."
//
// The notice itself — its state (announce.Notice) and its row
// (announce.RenderNoticeRow) — lives in
// internal/ui/views/announce: a UI row does not belong in a features
// package, and the eligibility *facts* (the typed error, the Reason,
// the VerificationURL, the tier gates) stay in
// internal/features/auth/eligibility (tier 2). Cycle 381, tranche 53.
//
// What stays here is the wiring the host owns:
//
//   - eligibilityNoticeMsg is the tea.Msg the streamPrompt chunk
//     handler posts to the program when chunk.Error carries an
//     *EligibilityError. The Update() handler routes it into
//     setEligibilityNotice, which writes the notice onto
//     m.overlays.eligibilityNotice and emits the diagnostic to stderr.
//
//   - setEligibilityNotice stores the notice AND logs it for
//     diagnostics. The diagnostic-log path is the only place the
//     user-facing message reaches stderr; the inline notice is what
//     the user sees on the screen. The split mirrors the
//     `error: <msg>` / `AGY_ERROR: {...}` print-mode contract:
//     the diagnostic line is operator-facing, the inline row is
//     user-facing, neither leaks into the conversation transcript.
//
//   - clearEligibilityNotice wipes the inline notice when the user
//     starts typing or submits a new prompt. Called from the
//     tea.KeyMsg handler (the cycle 262 "user starts typing"
//     signal) and from submitPrompt (a new prompt implicitly
//     acknowledges the prior notice).
//
// The root-cause analysis of cycle 262 — why the chunk handler used
// to drop the error, and why the notice renders inline instead of
// stacking above the transcript — moved with the row into
// internal/ui/views/announce/notice.go.

// eligibilityNoticeMsg is the tea.Msg the streamPrompt chunk
// handler posts to the program when chunk.Error carries an
// *EligibilityError. The Update() handler routes it into
// setEligibilityNotice, which writes the inline notice onto
// m.overlays.eligibilityNotice and emits the diagnostic to stderr.
//
// The struct is a value type (Reason / VerificationURL) rather
// than a pointer-to-EligibilityError so the post path is a
// simple copy — chunk handlers fire from a streaming goroutine
// and we don't want to share the source error's lifecycle with
// the program goroutine.
type eligibilityNoticeMsg struct {
	Reason          string
	VerificationURL string
}

// setEligibilityNotice stores a notice on the RootModel AND logs
// it for diagnostics.
//
// reason is the user-facing message (falls back to a generic
// message when empty). url is the verification URL (empty when
// the upstream did not supply one — the inline row still
// renders in that case).
//
// The function does NOT clear the notice first; if a caller
// wants to overwrite an active notice with a new one, they
// should call clearEligibilityNotice first. Cycle 262's call
// site (the Update() handler for eligibilityNoticeMsg) is the
// only producer, and each eligibility error from the chunk
// handler produces a fresh message — overwrites are an
// emergent property, not an explicit branch.
func (m *RootModel) setEligibilityNotice(reason, url string) {
	m.overlays.eligibilityNotice = announce.Notice{
		Reason:          reason,
		VerificationURL: url,
	}
	if reason == "" {
		reason = "your account is not eligible for this request"
	}
	if url != "" {
		fmt.Fprintf(os.Stderr, "diagnostic: eligibility error: %s (verify at %s)\n", reason, url)
	} else {
		fmt.Fprintf(os.Stderr, "diagnostic: eligibility error: %s\n", reason)
	}
}

// clearEligibilityNotice wipes the inline notice when the user
// starts typing or submits a new prompt. Called from the
// tea.KeyMsg handler (the cycle 262 "user starts typing"
// signal) and from submitPrompt (a new prompt implicitly
// acknowledges the prior notice).
//
// The function is idempotent — clearing an already-empty
// notice is a no-op so the call sites don't have to guard.
func (m *RootModel) clearEligibilityNotice() {
	m.overlays.eligibilityNotice = announce.Notice{}
}

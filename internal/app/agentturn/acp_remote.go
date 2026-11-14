package agentturn

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/ironpark/acp-go/router"
	schemav1 "github.com/ironpark/acp-go/schema/v1"
	schemav2 "github.com/ironpark/acp-go/schema/v2"
	"github.com/justwasm/pigasus/agentlink"
	coreversion "github.com/justwasm/pigasus/internal/core/version"
)

// ACPRemote runs turns against an ACP (Agent Client Protocol) server,
// supporting both stable ACP v1 and draft ACP v2 via router negotiation.
type ACPRemote struct {
	agent     *router.Agent
	sessionV1 *acp1.ClientSession
	sessionV2 *acp2.ClientSession
	closer    io.Closer
	cwd       string

	mu                sync.Mutex
	busy              bool
	closed            bool
	frameCh           chan agentlink.Frame
	availableCmds     []CommandInfo
	plan              []PlanEntry
	lastUsage         *agentlink.Usage
	permissionHandler PermissionHandler

	termMu    sync.Mutex
	terminals map[string]*acpTerminalState
	nextTerm  int64
}

type acpTerminalState struct {
	cmd  *exec.Cmd
	done chan struct{}

	mu        sync.Mutex
	output    []byte
	limit     int // maximum output bytes to retain; 0 keeps everything
	truncated bool
	exit      *acp1.TerminalExitStatus
}

func (t *acpTerminalState) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.output = append(t.output, p...)
	if t.limit > 0 && len(t.output) > t.limit {
		cut := len(t.output) - t.limit
		for cut < len(t.output) && !utf8.RuneStart(t.output[cut]) {
			cut++
		}
		t.output = t.output[cut:]
		t.truncated = true
	}
	return len(p), nil
}

func (t *acpTerminalState) snapshot() (string, bool, *acp1.TerminalExitStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.output), t.truncated, t.exit
}

var _ Engine = (*ACPRemote)(nil)
var _ Steerer = (*ACPRemote)(nil)
var _ CommandLister = (*ACPRemote)(nil)
var _ PlanLister = (*ACPRemote)(nil)
var _ CapabilityProvider = (*ACPRemote)(nil)
var _ ActionHandler = (*ACPRemote)(nil)

// PlanEntry represents a single task or goal in an execution plan.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// PermissionRequest represents an incoming authorization request from an ACP agent.
type PermissionRequest struct {
	SessionID  string             `json:"sessionId"`
	Title      string             `json:"title"`
	Options    []PermissionOption `json:"options"`
	ToolCallID string             `json:"toolCallId,omitempty"`
	RawInput   string             `json:"rawInput,omitempty"`
}

// PermissionOption describes a choice offered to authorize an action.
type PermissionOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// PermissionDecision contains the outcome of a permission check.
type PermissionDecision struct {
	Approved bool   `json:"approved"`
	OptionID string `json:"optionId,omitempty"` // Specific option ID to select; if empty, chosen automatically
	Feedback string `json:"feedback,omitempty"` // Rejection explanation or guidance
}

// PermissionHandler handles permission requests from ACP agents.
type PermissionHandler func(ctx context.Context, req PermissionRequest) (PermissionDecision, error)

// Plan returns a copy of the current execution plan tasks reported by the ACP agent.
func (r *ACPRemote) Plan() []PlanEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.plan) == 0 {
		return nil
	}
	out := make([]PlanEntry, len(r.plan))
	copy(out, r.plan)
	return out
}

func (r *ACPRemote) setPlanV1(entries []schemav1.PlanEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PlanEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, PlanEntry{
			Content:  e.Content,
			Priority: string(e.Priority),
			Status:   string(e.Status),
		})
	}
	r.plan = out
}

func (r *ACPRemote) setPlanV2(entries []schemav2.PlanEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PlanEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, PlanEntry{
			Content:  e.Content,
			Priority: string(e.Priority),
			Status:   string(e.Status),
		})
	}
	r.plan = out
}

func parseMarkdownPlan(content string) []PlanEntry {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	var entries []PlanEntry
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		var isChecklist bool
		var completed bool
		var taskText string

		switch {
		case strings.HasPrefix(lower, "- [x]") || strings.HasPrefix(lower, "* [x]"):
			isChecklist = true
			completed = true
			taskText = strings.TrimSpace(line[5:])
		case strings.HasPrefix(lower, "- [ ]") || strings.HasPrefix(lower, "* [ ]"):
			isChecklist = true
			completed = false
			taskText = strings.TrimSpace(line[5:])
		case strings.HasPrefix(lower, "[x]"):
			isChecklist = true
			completed = true
			taskText = strings.TrimSpace(line[3:])
		case strings.HasPrefix(lower, "[ ]"):
			isChecklist = true
			completed = false
			taskText = strings.TrimSpace(line[3:])
		default:
			if idx := strings.Index(lower, ". [x]"); idx > 0 && idx < 5 {
				isChecklist = true
				completed = true
				taskText = strings.TrimSpace(line[idx+6:])
			} else if idx := strings.Index(lower, ". [ ]"); idx > 0 && idx < 5 {
				isChecklist = true
				completed = false
				taskText = strings.TrimSpace(line[idx+6:])
			}
		}

		if isChecklist {
			status := "pending"
			if completed {
				status = "completed"
			}
			entries = append(entries, PlanEntry{
				Content: taskText,
				Status:  status,
			})
		}
	}

	if len(entries) > 0 {
		return entries
	}
	return []PlanEntry{{Content: trimmed}}
}

func (r *ACPRemote) setPlanMarkdown(content string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plan = parseMarkdownPlan(content)
}

// SetPlanMarkdown sets the execution plan from markdown checklist text.
func (r *ACPRemote) SetPlanMarkdown(content string) {
	r.setPlanMarkdown(content)
}

// SetPlanMarkdownForTest sets the execution plan from markdown checklist text for tests.
func (r *ACPRemote) SetPlanMarkdownForTest(content string) {
	r.setPlanMarkdown(content)
}

func (r *ACPRemote) clearPlan() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plan = nil
}

func (r *ACPRemote) setUsage(u *agentlink.Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastUsage = u
}

func (r *ACPRemote) emitUsage(u *agentlink.Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frameCh != nil {
		select {
		case r.frameCh <- agentlink.Frame{Usage: u}:
		default:
		}
	}
}

// SetPermissionHandler configures a custom callback for permission requests.
// When nil, ACPRemote defaults to auto-approval.
func (r *ACPRemote) SetPermissionHandler(h PermissionHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissionHandler = h
}

func (r *ACPRemote) emitNotice(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frameCh != nil {
		select {
		case r.frameCh <- agentlink.Frame{
			Kind:   agentlink.KindNotice,
			Notice: &agentlink.Notice{Text: text},
		}:
		default:
		}
	}
}

func (r *ACPRemote) emitFrame(f agentlink.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frameCh != nil {
		select {
		case r.frameCh <- f:
		default:
		}
	}
}

// AvailableCommands returns the dynamic slash commands advertised by the ACP agent.
func (r *ACPRemote) AvailableCommands() []CommandInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.availableCmds) == 0 {
		return nil
	}
	out := make([]CommandInfo, len(r.availableCmds))
	copy(out, r.availableCmds)
	return out
}

// EngineName identifies this engine as acp.
func (r *ACPRemote) EngineName() string {
	return "acp"
}

// Capabilities returns capabilities supported by the ACP agent.
func (r *ACPRemote) Capabilities() Capability {
	r.mu.Lock()
	defer r.mu.Unlock()
	caps := CapNone
	for _, cmd := range r.availableCmds {
		clean := strings.TrimPrefix(cmd.Name, "/")
		if strings.EqualFold(clean, "tasks") {
			caps |= CapTasks
		}
		if strings.EqualFold(clean, "agents") {
			caps |= CapAgents
		}
		if strings.EqualFold(clean, "model") {
			caps |= CapModelSwitch
		}
	}
	return caps
}

// ExecuteAction executes a backend action against the ACP agent.
func (r *ACPRemote) ExecuteAction(ctx context.Context, action string, args string) (ActionResult, error) {
	// Generic ACP agents currently pass dynamic actions via prompt or session update.
	return ActionResult{
		Success: true,
		Message: "Action processed by ACP agent",
	}, nil
}

func (r *ACPRemote) setAvailableCommandsV1(cmds []schemav1.AvailableCommand) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		hint := ""
		if c.Input != nil {
			hint = c.Input.Hint
		}
		out = append(out, CommandInfo{
			Name:        c.Name,
			Description: c.Description,
			InputHint:   hint,
		})
	}
	r.availableCmds = out
}

func (r *ACPRemote) setAvailableCommandsV2(cmds []schemav2.AvailableCommand) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		hint := ""
		if txt, ok := c.Input.As[schemav2.AvailableCommandInputText](); ok {
			hint = txt.Hint
		}
		out = append(out, CommandInfo{
			Name:        c.Name,
			Description: c.Description,
			InputHint:   hint,
		})
	}
	r.availableCmds = out
}

// DialACPRemote connects to an ACP server over WebSocket and initializes the ACPRemote engine.
// acpVersion specifies the protocol version ("auto", "1", "2").
func DialACPRemote(ctx context.Context, rawURL, token, cwd string, acpVersion ...string) (*ACPRemote, error) {
	u := strings.TrimSpace(rawURL)
	if !strings.Contains(u, "://") {
		u = "ws://" + u
	}
	if !strings.HasSuffix(strings.TrimRight(u, "/"), "/acp") {
		u = strings.TrimRight(u, "/") + "/acp"
	}

	opts := &websocket.DialOptions{}
	if strings.TrimSpace(token) != "" {
		opts.HTTPHeader = http.Header{
			"Authorization": []string{"Bearer " + strings.TrimSpace(token)},
		}
	}

	ver := "auto"
	if len(acpVersion) > 0 && strings.TrimSpace(acpVersion[0]) != "" {
		ver = strings.ToLower(strings.TrimSpace(acpVersion[0]))
	}

	dial := func(dialCtx context.Context) (acp.Transport, error) {
		wsConn, _, err := websocket.Dial(dialCtx, u, opts)
		if err != nil {
			return nil, fmt.Errorf("acp: dial %s: %w", u, err)
		}
		netConn := websocket.NetConn(context.Background(), wsConn, websocket.MessageText)
		return acp.NewStdioTransport(netConn, netConn), nil
	}

	return connectACPRemote(ctx, dial, nil, cwd, ver)
}

// NewACPRemote initializes an ACP connection over static reader/writer and creates a session.
func NewACPRemote(ctx context.Context, peerInput io.Writer, peerOutput io.Reader, closer io.Closer, cwd string, acpVersion ...string) (*ACPRemote, error) {
	ver := "auto"
	if len(acpVersion) > 0 && strings.TrimSpace(acpVersion[0]) != "" {
		ver = strings.ToLower(strings.TrimSpace(acpVersion[0]))
	}

	// For in-memory or pipe connections where dialing cannot be re-invoked,
	// wrap in single-use transport.
	transport := acp.NewStdioTransport(peerOutput, peerInput)
	dial := func(dialCtx context.Context) (acp.Transport, error) {
		return transport, nil
	}

	return connectACPRemote(ctx, dial, closer, cwd, ver)
}

func connectACPRemote(ctx context.Context, dial func(context.Context) (acp.Transport, error), closer io.Closer, cwd, version string) (*ACPRemote, error) {
	if cwd == "" {
		cwd = "."
	}
	absCwd, err := filepath.Abs(cwd)
	if err == nil {
		cwd = absCwd
	}

	r := &ACPRemote{
		closer:    closer,
		cwd:       cwd,
		terminals: make(map[string]*acpTerminalState),
	}

	connector := router.NewClient()
	clientInfo := &acp1.Implementation{
		Name:    "pigasus-tui",
		Version: coreversion.Reference,
	}

	v1Client := &acpRemoteV1Client{r: r}
	v1Init := &acp1.InitializeRequest{
		ClientInfo:         clientInfo,
		ClientCapabilities: acp1.ClientCapabilitiesOf(v1Client),
	}

	switch version {
	case "1", "v1":
		connector.WithV1(func(c *acp1.ClientSideConnection) acp1.Client {
			return v1Client
		}, v1Init)
	case "2", "v2":
		connector.WithV2(func(c *acp2.ClientSideConnection) acp2.Client {
			return &acpRemoteV2Client{r: r}
		}, &acp2.InitializeRequest{
			Info: acp2.Implementation{Name: clientInfo.Name, Version: clientInfo.Version},
		})
	default: // "auto", "dual", ""
		connector.WithV1(func(c *acp1.ClientSideConnection) acp1.Client {
			return v1Client
		}, v1Init).
			WithV2(func(c *acp2.ClientSideConnection) acp2.Client {
				return &acpRemoteV2Client{r: r}
			}, &acp2.InitializeRequest{
				Info: acp2.Implementation{Name: clientInfo.Name, Version: clientInfo.Version},
			})
	}

	agent, err := connector.Connect(ctx, dial)
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, fmt.Errorf("acp: connect failed: %w", err)
	}
	r.agent = agent

	// Initialize session according to negotiated protocol version
	if agent.V2 != nil {
		newResp, err := agent.V2.NewSession(ctx, &acp2.NewSessionRequest{
			Cwd: acp2.AbsolutePath(cwd),
		})
		if err != nil {
			_ = agent.Close()
			if closer != nil {
				_ = closer.Close()
			}
			return nil, fmt.Errorf("acp v2: new session failed: %w", err)
		}
		r.sessionV2 = agent.V2.Session(newResp.SessionID)
		if len(newResp.AvailableCommands) > 0 {
			r.setAvailableCommandsV2(newResp.AvailableCommands)
		}
	} else if agent.V1 != nil {
		newResp, err := agent.V1.NewSession(ctx, &acp1.NewSessionRequest{
			Cwd: cwd,
		})
		if err != nil {
			_ = agent.Close()
			if closer != nil {
				_ = closer.Close()
			}
			return nil, fmt.Errorf("acp v1: new session failed: %w", err)
		}
		r.sessionV1 = agent.V1.Session(newResp.SessionID)
	} else {
		_ = agent.Close()
		if closer != nil {
			_ = closer.Close()
		}
		return nil, errors.New("acp: no compatible protocol version negotiated")
	}

	return r, nil
}

// Steer submits mid-turn guidance to an active ACP v2 session.
func (r *ACPRemote) Steer(instruction string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("agentturn: the engine is closed")
	}
	if !r.busy {
		return errors.New("agentturn: no turn is active to steer")
	}
	if r.sessionV2 == nil {
		return errors.New("acp: steering is only supported on ACP v2 sessions")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.sessionV2.Prompt(ctx, acp2.TextBlock(instruction))
	return err
}

// Turn asks the ACP server to run one turn and reports its frames on the returned channel.
func (r *ACPRemote) Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("agentturn: the engine is closed")
	}
	if r.busy {
		r.mu.Unlock()
		return nil, errors.New("agentturn: a turn is already running")
	}
	r.busy = true
	out := make(chan agentlink.Frame, frameBuffer)
	r.frameCh = out
	r.mu.Unlock()

	var promptText string
	for _, m := range t.History {
		if m.Role == "user" {
			promptText = m.Content
		}
	}

	cancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			detachedCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if r.sessionV2 != nil {
				_ = r.sessionV2.Cancel(detachedCtx)
			} else if r.sessionV1 != nil {
				_ = r.sessionV1.Cancel(detachedCtx)
			}
		case <-cancelDone:
		}
	}()

	go func() {
		defer func() {
			close(cancelDone)
			r.mu.Lock()
			r.busy = false
			ch := r.frameCh
			r.frameCh = nil
			if ch != nil {
				close(ch)
			}
			r.mu.Unlock()
		}()

		if r.sessionV2 != nil {
			r.runTurnV2(ctx, promptText, out)
		} else if r.sessionV1 != nil {
			r.runTurnV1(ctx, promptText, out)
		}
	}()

	return out, nil
}

func (r *ACPRemote) runTurnV2(ctx context.Context, prompt string, out chan<- agentlink.Frame) {
	if _, err := r.sessionV2.Prompt(ctx, acp2.TextBlock(prompt)); err != nil {
		out <- agentlink.Frame{Kind: agentlink.KindResult, Error: err.Error()}
		return
	}

	var lastUsage *agentlink.Usage
	toolNames := make(map[string]string)
	toolArgs := make(map[string]string)
	uTurn := r.sessionV2.UntilIdle(ctx)
	for update := range uTurn.Updates() {
		switch u := update.Variant().(type) {
		case schemav2.SessionUpdateAgentMessageChunk:
			if text, ok := acp2.TextOf(u.Content); ok {
				out <- agentlink.Frame{Kind: agentlink.KindText, Text: text}
			}
		case schemav2.SessionUpdateAgentThoughtChunk:
			if text, ok := acp2.TextOf(u.Content); ok {
				out <- agentlink.Frame{Kind: agentlink.KindThought, Text: text}
			}
		case schemav2.SessionUpdateAvailableCommandsUpdate:
			r.setAvailableCommandsV2(u.AvailableCommands)
		case schemav2.SessionUpdatePlanUpdate:
			if items, ok := u.Plan.As[schemav2.PlanUpdateContentItems](); ok {
				r.setPlanV2(items.Entries)
			} else if md, ok := u.Plan.As[schemav2.PlanUpdateContentMarkdown](); ok {
				r.setPlanMarkdown(md.Content)
			}
		case schemav2.SessionUpdatePlanRemoved:
			r.clearPlan()
		case schemav2.SessionUpdateUsageUpdate:
			lastUsage = &agentlink.Usage{PromptTokens: int(u.Used), TotalTokens: int(u.Used)}
			r.setUsage(lastUsage)
			out <- agentlink.Frame{Usage: lastUsage}
		case schemav2.SessionUpdateToolCallUpdate:
			callID := string(u.ToolCallID)
			phase := agentlink.ToolStart
			isErr := false
			if status, ok := u.Status.Get(); ok {
				if status == schemav2.ToolCallStatusCompleted || status == schemav2.ToolCallStatusFailed {
					phase = agentlink.ToolEnd
				}
				if status == schemav2.ToolCallStatusFailed {
					isErr = true
				}
			}
			title, _ := u.Title.Get()
			if title == "" {
				if n, ok := u.Name.Get(); ok {
					title = n
				}
			}
			if title != "" {
				toolNames[callID] = title
			} else {
				title = toolNames[callID]
			}

			args := string(u.RawInput)
			if args != "" {
				toolArgs[callID] = args
			} else {
				args = toolArgs[callID]
			}

			result := string(u.RawOutput)
			if contents, ok := u.Content.Get(); ok && len(contents) > 0 {
				var b strings.Builder
				for _, c := range contents {
					if cc, ok := c.As[schemav2.ToolCallContentContent](); ok {
						if text, ok := acp2.TextOf(cc.Content); ok {
							b.WriteString(text)
						}
					}
				}
				if b.Len() > 0 {
					result = b.String()
				}
			} else if len(result) > 0 {
				var unquoted string
				if err := json.Unmarshal([]byte(result), &unquoted); err == nil {
					result = unquoted
				}
			}

			out <- agentlink.Frame{
				Kind: agentlink.KindTool,
				ID:   callID,
				Tool: &agentlink.Tool{
					Name:   title,
					Phase:  phase,
					Args:   args,
					Result: result,
					Err:    isErr,
				},
			}
		}
	}

	reason, err := uTurn.Wait()
	result := agentlink.Frame{Kind: agentlink.KindResult}
	if err != nil {
		result.Error = err.Error()
	} else if reason == acp2.StopReasonRefusal {
		result.Error = "turn refused"
	} else if ctx.Err() != nil {
		result.Error = "turn cancelled"
	}
	if lastUsage == nil {
		r.mu.Lock()
		lastUsage = r.lastUsage
		r.mu.Unlock()
	}
	if result.Usage == nil && lastUsage != nil {
		result.Usage = lastUsage
	}
	out <- result
}

func (r *ACPRemote) runTurnV1(ctx context.Context, prompt string, out chan<- agentlink.Frame) {
	turn, err := r.sessionV1.Prompt(ctx, acp1.TextBlock(prompt))
	if err != nil {
		out <- agentlink.Frame{Kind: agentlink.KindResult, Error: err.Error()}
		return
	}

	var lastUsage *agentlink.Usage
	toolNames := make(map[string]string)
	toolArgs := make(map[string]string)
	for update := range turn.Updates() {
		switch u := update.Variant().(type) {
		case schemav1.SessionUpdateAgentMessageChunk:
			if text, ok := acp1.TextOf(u.Content); ok {
				out <- agentlink.Frame{Kind: agentlink.KindText, Text: text}
			}
		case schemav1.SessionUpdateAgentThoughtChunk:
			if text, ok := acp1.TextOf(u.Content); ok {
				out <- agentlink.Frame{Kind: agentlink.KindThought, Text: text}
			}
		case schemav1.SessionUpdateAvailableCommandsUpdate:
			r.setAvailableCommandsV1(u.AvailableCommands)
		case schemav1.SessionUpdatePlan:
			r.setPlanV1(u.Entries)
		case schemav1.SessionUpdatePlanUpdate:
			if items, ok := u.Plan.As[schemav1.PlanUpdateContentItems](); ok {
				r.setPlanV1(items.Entries)
			} else if md, ok := u.Plan.As[schemav1.PlanUpdateContentMarkdown](); ok {
				r.setPlanMarkdown(md.Content)
			}
		case schemav1.SessionUpdatePlanRemoved:
			r.clearPlan()
		case schemav1.SessionUpdateUsageUpdate:
			lastUsage = &agentlink.Usage{PromptTokens: int(u.Used), TotalTokens: int(u.Used)}
			r.setUsage(lastUsage)
			out <- agentlink.Frame{Usage: lastUsage}
		case schemav1.SessionUpdateToolCall:
			callID := string(u.ToolCallID)
			if u.Title != "" {
				toolNames[callID] = u.Title
			}
			args := string(u.RawInput)
			if args != "" {
				toolArgs[callID] = args
			}
			out <- agentlink.Frame{
				Kind: agentlink.KindTool,
				ID:   callID,
				Tool: &agentlink.Tool{
					Name:  u.Title,
					Phase: agentlink.ToolStart,
					Args:  args,
				},
			}
		case schemav1.SessionUpdateToolCallUpdate:
			callID := string(u.ToolCallID)
			phase := agentlink.ToolStart
			isErr := false
			if u.Status != nil {
				if *u.Status == schemav1.ToolCallStatusCompleted || *u.Status == schemav1.ToolCallStatusFailed {
					phase = agentlink.ToolEnd
				}
				if *u.Status == schemav1.ToolCallStatusFailed {
					isErr = true
				}
			}
			title := ""
			if u.Title != nil {
				title = *u.Title
			}
			if title != "" {
				toolNames[callID] = title
			} else {
				title = toolNames[callID]
			}

			args := string(u.RawInput)
			if args != "" {
				toolArgs[callID] = args
			} else {
				args = toolArgs[callID]
			}

			result := string(u.RawOutput)
			if len(u.Content) > 0 {
				var b strings.Builder
				for _, c := range u.Content {
					if cc, ok := c.As[schemav1.ToolCallContentContent](); ok {
						if text, ok := acp1.TextOf(cc.Content); ok {
							b.WriteString(text)
						}
					}
				}
				if b.Len() > 0 {
					result = b.String()
				}
			} else if len(result) > 0 {
				var unquoted string
				if err := json.Unmarshal([]byte(result), &unquoted); err == nil {
					result = unquoted
				}
			}

			out <- agentlink.Frame{
				Kind: agentlink.KindTool,
				ID:   callID,
				Tool: &agentlink.Tool{
					Name:   title,
					Phase:  phase,
					Args:   args,
					Result: result,
					Err:    isErr,
				},
			}
		}
	}

	resp, err := turn.Wait()
	result := agentlink.Frame{Kind: agentlink.KindResult}
	if err != nil {
		result.Error = err.Error()
	} else if resp != nil {
		if resp.StopReason == acp1.StopReasonRefusal {
			result.Error = "turn refused"
		}
		if resp.Usage != nil {
			result.Usage = &agentlink.Usage{
				PromptTokens:     int(resp.Usage.InputTokens),
				CompletionTokens: int(resp.Usage.OutputTokens),
				TotalTokens:      int(resp.Usage.TotalTokens),
			}
		}
	} else if ctx.Err() != nil {
		result.Error = "turn cancelled"
	}
	if lastUsage == nil {
		r.mu.Lock()
		lastUsage = r.lastUsage
		r.mu.Unlock()
	}
	if result.Usage == nil && lastUsage != nil {
		result.Usage = lastUsage
	}
	out <- result
}

func (r *ACPRemote) handlePermissionRequestV1(ctx context.Context, params *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	if params == nil {
		return acp1.PermissionCancelled(), nil
	}

	req := PermissionRequest{
		SessionID:  string(params.SessionID),
		ToolCallID: string(params.ToolCall.ToolCallID),
		RawInput:   string(params.ToolCall.RawInput),
		Title:      string(params.ToolCall.ToolCallID),
	}
	if params.ToolCall.Title != nil {
		req.Title = *params.ToolCall.Title
	}
	for _, opt := range params.Options {
		req.Options = append(req.Options, PermissionOption{
			ID:   string(opt.OptionID),
			Name: opt.Name,
			Kind: string(opt.Kind),
		})
	}

	r.mu.Lock()
	handler := r.permissionHandler
	r.mu.Unlock()

	var decision PermissionDecision
	var err error
	if handler != nil {
		decision, err = handler(ctx, req)
		if err != nil {
			return nil, err
		}
	} else {
		decision = PermissionDecision{Approved: true}
	}

	if decision.Approved {
		selectedID := schemav1.PermissionOptionID(decision.OptionID)
		if selectedID == "" {
			for _, opt := range params.Options {
				if opt.Kind == schemav1.PermissionOptionKindAllowOnce || opt.Kind == schemav1.PermissionOptionKindAllowAlways {
					selectedID = opt.OptionID
					break
				}
			}
			if selectedID == "" && len(params.Options) > 0 {
				selectedID = params.Options[0].OptionID
			}
		}
		resp := acp1.PermissionSelected(selectedID)
		if decision.Feedback != "" {
			fbJSON, _ := json.Marshal(decision.Feedback)
			resp.Meta = schemav1.Meta{"feedback": jsontext.Value(fbJSON)}
		}
		return resp, nil
	}

	rejectID := schemav1.PermissionOptionID(decision.OptionID)
	if rejectID == "" {
		for _, opt := range params.Options {
			if opt.Kind == schemav1.PermissionOptionKindRejectOnce || opt.Kind == schemav1.PermissionOptionKindRejectAlways {
				rejectID = opt.OptionID
				break
			}
		}
	}

	var resp *acp1.RequestPermissionResponse
	if rejectID != "" {
		resp = acp1.PermissionSelected(rejectID)
	} else {
		resp = acp1.PermissionCancelled()
	}
	if decision.Feedback != "" {
		fbJSON, _ := json.Marshal(decision.Feedback)
		resp.Meta = schemav1.Meta{"feedback": jsontext.Value(fbJSON)}
		r.emitNotice("Permission rejected: " + decision.Feedback)
	}
	return resp, nil
}

func (r *ACPRemote) handlePermissionRequestV2(ctx context.Context, params *acp2.RequestPermissionRequest) (*acp2.RequestPermissionResponse, error) {
	if params == nil {
		return acp2.PermissionCancelled(), nil
	}

	req := PermissionRequest{
		SessionID: string(params.SessionID),
		Title:     params.Title,
	}
	if tc, ok := params.Subject.As[schemav2.RequestPermissionSubjectToolCall](); ok {
		req.ToolCallID = string(tc.ToolCall.ToolCallID)
		req.RawInput = string(tc.ToolCall.RawInput)
		if title, ok := tc.ToolCall.Title.Get(); ok && req.Title == "" {
			req.Title = title
		}
	} else if cmd, ok := params.Subject.As[schemav2.RequestPermissionSubjectCommand](); ok {
		req.RawInput = cmd.Command
		if cmd.ToolCallID != nil {
			req.ToolCallID = string(*cmd.ToolCallID)
		}
	}
	for _, opt := range params.Options {
		req.Options = append(req.Options, PermissionOption{
			ID:   string(opt.OptionID),
			Name: opt.Name,
			Kind: string(opt.Kind),
		})
	}

	r.mu.Lock()
	handler := r.permissionHandler
	r.mu.Unlock()

	var decision PermissionDecision
	var err error
	if handler != nil {
		decision, err = handler(ctx, req)
		if err != nil {
			return nil, err
		}
	} else {
		decision = PermissionDecision{Approved: true}
	}

	if decision.Approved {
		selectedID := schemav2.PermissionOptionID(decision.OptionID)
		if selectedID == "" {
			for _, opt := range params.Options {
				if opt.Kind == schemav2.PermissionOptionKindAllowOnce || opt.Kind == schemav2.PermissionOptionKindAllowAlways {
					selectedID = opt.OptionID
					break
				}
			}
			if selectedID == "" && len(params.Options) > 0 {
				selectedID = params.Options[0].OptionID
			}
		}
		resp := acp2.PermissionSelected(selectedID)
		if decision.Feedback != "" {
			fbJSON, _ := json.Marshal(decision.Feedback)
			resp.Meta = schemav2.Meta{"feedback": jsontext.Value(fbJSON)}
		}
		return resp, nil
	}

	rejectID := schemav2.PermissionOptionID(decision.OptionID)
	if rejectID == "" {
		for _, opt := range params.Options {
			if opt.Kind == schemav2.PermissionOptionKindRejectOnce || opt.Kind == schemav2.PermissionOptionKindRejectAlways {
				rejectID = opt.OptionID
				break
			}
		}
	}

	var resp *acp2.RequestPermissionResponse
	if rejectID != "" {
		resp = acp2.PermissionSelected(rejectID)
	} else {
		resp = acp2.PermissionCancelled()
	}
	if decision.Feedback != "" {
		fbJSON, _ := json.Marshal(decision.Feedback)
		resp.Meta = schemav2.Meta{"feedback": jsontext.Value(fbJSON)}
		r.emitNotice("Permission rejected: " + decision.Feedback)
	}
	return resp, nil
}

// Close releases the underlying ACP connection.
func (r *ACPRemote) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	r.termMu.Lock()
	for _, t := range r.terminals {
		select {
		case <-t.done:
		default:
			if t.cmd != nil && t.cmd.Process != nil {
				_ = t.cmd.Process.Kill()
			}
		}
	}
	r.terminals = nil
	r.termMu.Unlock()

	var errs []error
	if r.agent != nil {
		if err := r.agent.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.closer != nil {
		if err := r.closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

var _ acp1.Client = (*acpRemoteV1Client)(nil)
var _ acp1.FileReader = (*acpRemoteV1Client)(nil)
var _ acp1.FileWriter = (*acpRemoteV1Client)(nil)
var _ acp1.TerminalHandler = (*acpRemoteV1Client)(nil)

type acpRemoteV1Client struct {
	acp1.UnimplementedClient
	r *ACPRemote
}

func (c *acpRemoteV1Client) SessionUpdate(_ context.Context, params *acp1.SessionNotification) error {
	if params == nil || c.r == nil {
		return nil
	}
	if u, ok := params.Update.As[schemav1.SessionUpdateAvailableCommandsUpdate](); ok {
		c.r.setAvailableCommandsV1(u.AvailableCommands)
	}
	if u, ok := params.Update.As[schemav1.SessionUpdatePlan](); ok {
		c.r.setPlanV1(u.Entries)
	}
	if u, ok := params.Update.As[schemav1.SessionUpdatePlanUpdate](); ok {
		if items, ok := u.Plan.As[schemav1.PlanUpdateContentItems](); ok {
			c.r.setPlanV1(items.Entries)
		} else if md, ok := u.Plan.As[schemav1.PlanUpdateContentMarkdown](); ok {
			c.r.setPlanMarkdown(md.Content)
		}
	}
	if _, ok := params.Update.As[schemav1.SessionUpdatePlanRemoved](); ok {
		c.r.clearPlan()
	}
	if u, ok := params.Update.As[schemav1.SessionUpdateUsageUpdate](); ok {
		usage := &agentlink.Usage{PromptTokens: int(u.Used), TotalTokens: int(u.Used)}
		c.r.setUsage(usage)
		c.r.emitUsage(usage)
	}
	return nil
}

func (c *acpRemoteV1Client) RequestPermission(ctx context.Context, params *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	return c.r.handlePermissionRequestV1(ctx, params)
}

func (c *acpRemoteV1Client) ReadTextFile(ctx context.Context, params *acp1.ReadTextFileRequest) (*acp1.ReadTextFileResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	if params == nil {
		return nil, errors.New("acp: read text file params is nil")
	}
	filePath := params.Path
	if !filepath.IsAbs(filePath) {
		base := c.r.cwd
		if base == "" {
			base = "."
		}
		filePath = filepath.Join(base, filePath)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	content := string(data)
	if params.Line != nil || params.Limit != nil {
		lines := strings.Split(content, "\n")
		start := 1
		if params.Line != nil && *params.Line > 0 {
			start = int(*params.Line)
		}
		if start > len(lines) {
			content = ""
		} else {
			lines = lines[start-1:]
			if params.Limit != nil && int(*params.Limit) < len(lines) {
				lines = lines[:int(*params.Limit)]
			}
			content = strings.Join(lines, "\n")
		}
	}
	return &acp1.ReadTextFileResponse{Content: content}, nil
}

func (c *acpRemoteV1Client) WriteTextFile(ctx context.Context, params *acp1.WriteTextFileRequest) (*acp1.WriteTextFileResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	if params == nil {
		return nil, errors.New("acp: write text file params is nil")
	}
	filePath := params.Path
	if !filepath.IsAbs(filePath) {
		base := c.r.cwd
		if base == "" {
			base = "."
		}
		filePath = filepath.Join(base, filePath)
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filePath, []byte(params.Content), 0644); err != nil {
		return nil, err
	}
	return &acp1.WriteTextFileResponse{}, nil
}

func (c *acpRemoteV1Client) CreateTerminal(ctx context.Context, params *acp1.CreateTerminalRequest) (*acp1.CreateTerminalResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	if params == nil {
		return nil, errors.New("acp: create terminal params is nil")
	}

	shellName := "sh"
	if runtime.GOOS == "darwin" {
		shellName = "zsh"
	}

	fullCmd := params.Command
	if len(params.Args) > 0 {
		fullCmd = fullCmd + " " + strings.Join(params.Args, " ")
	}
	cmd := exec.Command(shellName, "-c", fullCmd)

	workDir := c.r.cwd
	if params.Cwd != nil && strings.TrimSpace(*params.Cwd) != "" {
		workDir = *params.Cwd
		if !filepath.IsAbs(workDir) && c.r.cwd != "" {
			workDir = filepath.Join(c.r.cwd, workDir)
		}
	}
	if workDir != "" {
		cmd.Dir = workDir
	}

	cmd.Env = os.Environ()
	for _, v := range params.Env {
		cmd.Env = append(cmd.Env, v.Name+"="+v.Value)
	}

	t := &acpTerminalState{
		cmd:  cmd,
		done: make(chan struct{}),
	}
	if params.OutputByteLimit != nil {
		t.limit = int(*params.OutputByteLimit)
	}
	cmd.Stdout = t
	cmd.Stderr = t

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start terminal: %w", err)
	}

	// Emit tool execution start event to active frame channel if present
	cmdArgsJSON, _ := json.Marshal(map[string]any{"command": fullCmd, "cwd": workDir})
	c.r.emitFrame(agentlink.Frame{
		Kind: agentlink.KindTool,
		ID:   fmt.Sprintf("term_%d", c.r.nextTerm+1),
		Tool: &agentlink.Tool{
			Name:  "terminal",
			Phase: agentlink.ToolStart,
			Args:  string(cmdArgsJSON),
		},
	})

	termIDNum := c.r.nextTerm + 1
	go func() {
		_ = cmd.Wait()
		exit := &acp1.TerminalExitStatus{}
		var exitCode int
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
			if exitCode >= 0 {
				uCode := uint32(exitCode)
				exit.ExitCode = &uCode
			} else {
				sig := "killed"
				exit.Signal = &sig
			}
		} else {
			uZero := uint32(0)
			exit.ExitCode = &uZero
		}
		t.mu.Lock()
		t.exit = exit
		outputStr := string(t.output)
		t.mu.Unlock()
		close(t.done)

		// Emit tool execution end event to active frame channel
		isErr := exitCode != 0
		c.r.emitFrame(agentlink.Frame{
			Kind: agentlink.KindTool,
			ID:   fmt.Sprintf("term_%d", termIDNum),
			Tool: &agentlink.Tool{
				Name:   "terminal",
				Phase:  agentlink.ToolEnd,
				Args:   string(cmdArgsJSON),
				Result: outputStr,
				Err:    isErr,
			},
		})
	}()

	c.r.termMu.Lock()
	if c.r.terminals == nil {
		c.r.terminals = make(map[string]*acpTerminalState)
	}
	c.r.nextTerm++
	id := acp1.TerminalID(fmt.Sprintf("term_%d", c.r.nextTerm))
	c.r.terminals[string(id)] = t
	c.r.termMu.Unlock()

	return &acp1.CreateTerminalResponse{TerminalID: id}, nil
}

func (c *acpRemoteV1Client) TerminalOutput(ctx context.Context, params *acp1.TerminalOutputRequest) (*acp1.TerminalOutputResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	if params == nil {
		return nil, errors.New("acp: terminal output params is nil")
	}

	c.r.termMu.Lock()
	t, ok := c.r.terminals[string(params.TerminalID)]
	c.r.termMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("acp: terminal %q not found", params.TerminalID)
	}

	output, truncated, exit := t.snapshot()
	return &acp1.TerminalOutputResponse{
		Output:     output,
		Truncated:  truncated,
		ExitStatus: exit,
	}, nil
}

func (c *acpRemoteV1Client) WaitForTerminalExit(ctx context.Context, params *acp1.WaitForTerminalExitRequest) (*acp1.WaitForTerminalExitResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	if params == nil {
		return nil, errors.New("acp: wait for terminal exit params is nil")
	}

	c.r.termMu.Lock()
	t, ok := c.r.terminals[string(params.TerminalID)]
	c.r.termMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("acp: terminal %q not found", params.TerminalID)
	}

	select {
	case <-t.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	_, _, exit := t.snapshot()
	resp := &acp1.WaitForTerminalExitResponse{}
	if exit != nil {
		resp.ExitCode = exit.ExitCode
		resp.Signal = exit.Signal
	}
	return resp, nil
}

func (c *acpRemoteV1Client) KillTerminal(ctx context.Context, params *acp1.KillTerminalRequest) (*acp1.KillTerminalResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	if params == nil {
		return nil, errors.New("acp: kill terminal params is nil")
	}

	c.r.termMu.Lock()
	t, ok := c.r.terminals[string(params.TerminalID)]
	c.r.termMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("acp: terminal %q not found", params.TerminalID)
	}

	select {
	case <-t.done:
	default:
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	}
	return &acp1.KillTerminalResponse{}, nil
}

func (c *acpRemoteV1Client) ReleaseTerminal(ctx context.Context, params *acp1.ReleaseTerminalRequest) (*acp1.ReleaseTerminalResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	if params == nil {
		return nil, errors.New("acp: release terminal params is nil")
	}

	c.r.termMu.Lock()
	t, ok := c.r.terminals[string(params.TerminalID)]
	delete(c.r.terminals, string(params.TerminalID))
	c.r.termMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("acp: terminal %q not found", params.TerminalID)
	}

	select {
	case <-t.done:
	default:
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	}
	return &acp1.ReleaseTerminalResponse{}, nil
}

type acpRemoteV2Client struct {
	acp2.UnimplementedClient
	r *ACPRemote
}

func (c *acpRemoteV2Client) SessionUpdate(_ context.Context, params *acp2.UpdateSessionNotification) error {
	if params == nil || c.r == nil {
		return nil
	}
	if u, ok := params.Update.As[schemav2.SessionUpdateAvailableCommandsUpdate](); ok {
		c.r.setAvailableCommandsV2(u.AvailableCommands)
	}
	if u, ok := params.Update.As[schemav2.SessionUpdatePlanUpdate](); ok {
		if items, ok := u.Plan.As[schemav2.PlanUpdateContentItems](); ok {
			c.r.setPlanV2(items.Entries)
		} else if md, ok := u.Plan.As[schemav2.PlanUpdateContentMarkdown](); ok {
			c.r.setPlanMarkdown(md.Content)
		}
	}
	if _, ok := params.Update.As[schemav2.SessionUpdatePlanRemoved](); ok {
		c.r.clearPlan()
	}
	if u, ok := params.Update.As[schemav2.SessionUpdateUsageUpdate](); ok {
		usage := &agentlink.Usage{PromptTokens: int(u.Used), TotalTokens: int(u.Used)}
		c.r.setUsage(usage)
		c.r.emitUsage(usage)
	}
	return nil
}

func (c *acpRemoteV2Client) RequestPermission(ctx context.Context, params *acp2.RequestPermissionRequest) (*acp2.RequestPermissionResponse, error) {
	if c.r == nil {
		return nil, errors.New("acp: client remote is nil")
	}
	return c.r.handlePermissionRequestV2(ctx, params)
}

// Package codex provides an agentturn.Engine implementation for OpenAI Codex app-server via stdio JSON-RPC.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func init() {
	agentturn.Register(agentturn.Driver{
		Name:         "codex",
		Aliases:      []string{"codex-app-server"},
		Description:  "Codex app-server via stdio JSON-RPC",
		DefaultModel: "gpt-6.1-sol",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnCodexRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model)
		},
	})
}

// Remote is an alias for CodexRemote.
type Remote = CodexRemote

// CodexRemote runs turns against an OpenAI Codex app-server over JSON-RPC 2.0 (stdio or pipe).
type CodexRemote struct {
	in      io.Writer
	closer  io.Closer
	writeMu sync.Mutex

	mu          sync.Mutex
	reqSeq      uint64
	threadID    string
	activeTurn  string
	pendingReqs map[uint64]chan codexRPCResponse

	busy    bool
	closed  bool
	frameCh chan agentlink.Frame
	doneCh  chan struct{}

	defaultModel string
	cachedModels []agentturn.ModelInfo
}

var _ agentturn.Engine = (*CodexRemote)(nil)
var _ agentturn.ModelLister = (*CodexRemote)(nil)
var _ agentturn.DefaultModeler = (*CodexRemote)(nil)
var _ agentturn.CommandLister = (*CodexRemote)(nil)
var _ agentturn.CapabilityProvider = (*CodexRemote)(nil)

type codexRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type codexRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *codexRPCError  `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type codexRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// SpawnCodexRemote spawns `codex app-server --listen stdio://` and returns an active CodexRemote engine.
func SpawnCodexRemote(ctx context.Context, codexBin, cwd, model string) (*CodexRemote, error) {
	if codexBin == "" {
		if p, err := exec.LookPath("codex"); err == nil {
			codexBin = p
		} else if _, err := os.Stat("/opt/homebrew/bin/codex"); err == nil {
			codexBin = "/opt/homebrew/bin/codex"
		} else if _, err := os.Stat("/usr/local/bin/codex"); err == nil {
			codexBin = "/usr/local/bin/codex"
		} else {
			return nil, errors.New("codex binary not found on PATH or common locations")
		}
	}

	cmd := exec.Command(codexBin, "app-server", "--listen", "stdio://")
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("codex: start app-server: %w", err)
	}

	closer := &procCloser{cmd: cmd, stdin: stdin}
	remote, err := NewCodexRemote(ctx, stdin, stdout, closer, cwd, model)
	if err != nil {
		return nil, err
	}
	// Best-effort warm-up of models catalogue to discover the default model
	warmCtx, warmCancel := context.WithTimeout(ctx, 2*time.Second)
	_, _ = remote.ListModels(warmCtx)
	warmCancel()
	return remote, nil
}

type procCloser struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func (p *procCloser) Close() error {
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
	}
	return nil
}

// NewCodexRemote connects to a Codex app-server via reader/writer pipes and initializes the session.
func NewCodexRemote(ctx context.Context, in io.Writer, out io.Reader, closer io.Closer, cwd, model string) (*CodexRemote, error) {
	r := &CodexRemote{
		in:          in,
		closer:      closer,
		pendingReqs: make(map[uint64]chan codexRPCResponse),
		doneCh:      make(chan struct{}),
	}

	go r.readLoop(out)

	// 1. Initialize
	type clientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	type initParams struct {
		ClientInfo clientInfo `json:"clientInfo"`
	}
	var initResp struct {
		UserAgent string `json:"userAgent"`
		CodexHome string `json:"codexHome"`
	}
	if err := r.call(ctx, "initialize", initParams{
		ClientInfo: clientInfo{Name: "pigasus-tui", Version: "1.4.12"},
	}, &initResp); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("codex initialize failed: %w", err)
	}

	// 2. Thread start
	type threadParams struct {
		Cwd            string `json:"cwd,omitempty"`
		Model          string `json:"model,omitempty"`
		ApprovalPolicy string `json:"approvalPolicy,omitempty"`
	}
	var threadResp struct {
		Model           string `json:"model"`
		ModelProvider   string `json:"modelProvider"`
		ReasoningEffort string `json:"reasoningEffort"`
		Thread          struct {
			ID              string `json:"id"`
			Model           string `json:"model"`
			ModelProvider   string `json:"modelProvider"`
			ReasoningEffort string `json:"reasoningEffort"`
		} `json:"thread"`
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	threadModel := model
	if strings.EqualFold(threadModel, "codex") {
		threadModel = ""
	} else if threadModel != "" {
		for _, tier := range []string{"ultra", "max", "xhigh", "high", "medium", "low"} {
			if strings.HasSuffix(threadModel, "-"+tier) {
				threadModel = strings.TrimSuffix(threadModel, "-"+tier)
				break
			}
		}
	}
	if err := r.call(ctx, "thread/start", threadParams{
		Cwd:            cwd,
		Model:          threadModel,
		ApprovalPolicy: "never",
	}, &threadResp); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("codex thread/start failed: %w", err)
	}

	r.threadID = threadResp.Thread.ID

	// Extract active model dynamically from the ndjson response of thread/start.
	// Codex app-server resolves its complete configuration hierarchy and reports
	// the active model and reasoning effort directly over stdio JSON-RPC.
	activeModel := threadResp.Model
	if activeModel == "" {
		activeModel = threadResp.Thread.Model
	}
	if activeModel != "" {
		r.defaultModel = activeModel
	} else if model != "" {
		r.defaultModel = model
	} else {
		r.defaultModel = "gemini-3.8-flash-high"
	}
	return r, nil
}

// DefaultModel returns the default model advertised by Codex app-server, if known.
func (r *CodexRemote) DefaultModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defaultModel
}

// ThreadID returns the active thread ID for this connection.
func (r *CodexRemote) ThreadID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.threadID
}

func (r *CodexRemote) nextID() uint64 {
	return atomic.AddUint64(&r.reqSeq, 1)
}

func (r *CodexRemote) call(ctx context.Context, method string, params any, result any) error {
	id := r.nextID()
	respCh := make(chan codexRPCResponse, 1)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("codex: client closed")
	}
	r.pendingReqs[id] = respCh
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pendingReqs, id)
		r.mu.Unlock()
	}()

	req := codexRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	r.writeMu.Lock()
	_, err = r.in.Write(data)
	r.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("codex: write request %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.doneCh:
		return errors.New("codex: connection closed")
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("codex jsonrpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("codex: unmarshal %s result: %w", method, err)
			}
		}
		return nil
	}
}

func (r *CodexRemote) reply(id uint64, result any) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, err := json.Marshal(resp)
	if err == nil {
		data = append(data, '\n')
		r.writeMu.Lock()
		_, _ = r.in.Write(data)
		r.writeMu.Unlock()
	}
}

func (r *CodexRemote) readLoop(rd io.Reader) {
	scanner := bufio.NewScanner(rd)
	// Allow larger tokens for big diffs/patches
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp codexRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		// Handle Response to a Client Request
		if resp.ID != nil && resp.Method == "" {
			r.mu.Lock()
			ch, ok := r.pendingReqs[*resp.ID]
			r.mu.Unlock()
			if ok {
				ch <- resp
			}
			continue
		}

		// Handle Server Request to Client (e.g. approval requests)
		if resp.ID != nil && resp.Method != "" {
			go r.handleServerRequest(*resp.ID, resp.Method, resp.Params)
			continue
		}

		// Handle Server Notifications
		if resp.Method != "" {
			r.handleNotification(resp.Method, resp.Params)
		}
	}

	r.mu.Lock()
	r.closed = true
	close(r.doneCh)
	if r.frameCh != nil {
		close(r.frameCh)
		r.frameCh = nil
	}
	for id, ch := range r.pendingReqs {
		ch <- codexRPCResponse{Error: &codexRPCError{Message: "connection closed"}}
		delete(r.pendingReqs, id)
	}
	r.mu.Unlock()
}

func (r *CodexRemote) handleServerRequest(id uint64, method string, params json.RawMessage) {
	switch method {
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/permissions/requestApproval",
		"applyPatchApproval",
		"execCommandApproval":
		// Auto-approve approval prompts in headless remote mode
		r.reply(id, map[string]any{"decision": "approved"})
	default:
		r.reply(id, map[string]any{"status": "ok"})
	}
}

func (r *CodexRemote) handleNotification(method string, paramsRaw json.RawMessage) {
	r.mu.Lock()
	out := r.frameCh
	r.mu.Unlock()
	if out == nil {
		return
	}

	switch method {
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		var p struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil && p.Delta != "" {
			out <- agentlink.Frame{Kind: agentlink.KindThought, Text: p.Delta}
		}

	case "item/agentMessage/delta":
		var p struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil && p.Delta != "" {
			out <- agentlink.Frame{Kind: agentlink.KindText, Text: p.Delta}
		}

	case "item/started":
		var p struct {
			Item struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"item"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil {
			if p.Item.Type == "commandExecution" {
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					ID:   p.Item.ID,
					Tool: &agentlink.Tool{
						Name:  "command",
						Phase: agentlink.ToolStart,
						Args:  p.Item.Command,
					},
				}
			}
		}

	case "item/commandExecution/outputDelta":
		var p struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil && p.Delta != "" {
			out <- agentlink.Frame{
				Kind: agentlink.KindNotice,
				Text: p.Delta,
			}
		}

	case "item/completed":
		var p struct {
			Item struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				ExitCode *int   `json:"exitCode"`
				Output   string `json:"output"`
			} `json:"item"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil {
			if p.Item.Type == "commandExecution" {
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					ID:   p.Item.ID,
					Tool: &agentlink.Tool{
						Name:   "command",
						Phase:  agentlink.ToolEnd,
						Result: p.Item.Output,
					},
				}
			}
		}

	case "item/fileChange/patchUpdated":
		var p struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil && p.Patch != "" {
			out <- agentlink.Frame{
				Kind: agentlink.KindTool,
				Tool: &agentlink.Tool{
					Name:   "fileChange",
					Phase:  agentlink.ToolEnd,
					Result: p.Patch,
				},
			}
		}

	case "thread/tokenUsage/updated":
		var p struct {
			TokenUsage struct {
				PromptTokens     int `json:"promptTokens"`
				CompletionTokens int `json:"completionTokens"`
				TotalTokens      int `json:"totalTokens"`
			} `json:"tokenUsage"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil {
			out <- agentlink.Frame{
				Kind: agentlink.KindResult,
				Usage: &agentlink.Usage{
					PromptTokens:     p.TokenUsage.PromptTokens,
					CompletionTokens: p.TokenUsage.CompletionTokens,
					TotalTokens:      p.TokenUsage.TotalTokens,
				},
			}
		}

	case "warning":
		var p struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil && p.Message != "" {
			out <- agentlink.Frame{Kind: agentlink.KindNotice, Text: "Codex warning: " + p.Message}
		}

	case "item/plan/delta":
		var p struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil && p.Delta != "" {
			out <- agentlink.Frame{Kind: agentlink.KindThought, Text: p.Delta}
		}

	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message           string `json:"message"`
					AdditionalDetails string `json:"additionalDetails,omitempty"`
				} `json:"error,omitempty"`
				RawError string `json:"raw_error,omitempty"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(paramsRaw, &p)
		errMsg := ""
		if p.Turn.Error != nil && p.Turn.Error.Message != "" {
			errMsg = p.Turn.Error.Message
			if p.Turn.Error.AdditionalDetails != "" {
				errMsg += " (" + p.Turn.Error.AdditionalDetails + ")"
			}
		} else if p.Turn.RawError != "" {
			errMsg = p.Turn.RawError
		} else if p.Turn.Status == "failed" {
			errMsg = "turn execution failed"
		}

		if errMsg != "" {
			out <- agentlink.Frame{Kind: agentlink.KindResult, Error: errMsg}
		}

		r.mu.Lock()
		r.busy = false
		r.activeTurn = ""
		if r.frameCh != nil {
			close(r.frameCh)
			r.frameCh = nil
		}
		r.mu.Unlock()

	case "error":
		var p struct {
			Error *struct {
				Message           string `json:"message"`
				AdditionalDetails string `json:"additionalDetails,omitempty"`
			} `json:"error"`
			Message   string `json:"message,omitempty"`
			WillRetry bool   `json:"willRetry"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil {
			msg := ""
			if p.Error != nil && p.Error.Message != "" {
				msg = p.Error.Message
				if p.Error.AdditionalDetails != "" {
					msg += " (" + p.Error.AdditionalDetails + ")"
				}
			} else if p.Message != "" {
				msg = p.Message
			}
			if msg != "" {
				if p.WillRetry {
					out <- agentlink.Frame{Kind: agentlink.KindNotice, Text: "Codex: " + msg}
				} else {
					out <- agentlink.Frame{Kind: agentlink.KindResult, Error: msg}
					r.mu.Lock()
					r.busy = false
					r.activeTurn = ""
					if r.frameCh != nil {
						close(r.frameCh)
						r.frameCh = nil
					}
					r.mu.Unlock()
				}
			}
		}
	}
}

// Turn starts a new turn on Codex app-server and streams its events.
func (r *CodexRemote) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("codex: client closed")
	}
	if r.busy {
		r.mu.Unlock()
		return nil, errors.New("agentturn: a turn is already running")
	}
	r.busy = true
	out := make(chan agentlink.Frame, 64)
	r.frameCh = out
	threadID := r.threadID
	r.mu.Unlock()

	var promptText string
	for _, m := range t.History {
		if m.Role == "user" {
			promptText = m.Content
		}
	}

	model := t.Model
	effort := t.Effort
	if model == "" {
		r.mu.Lock()
		model = r.defaultModel
		r.mu.Unlock()
	}
	// Strip parenthetical tier if present (e.g. "Gemini 3.8 Flash (High)")
	for _, tier := range []string{"ultra", "max", "xhigh", "high", "medium", "low"} {
		suf := "(" + strings.ToLower(tier) + ")"
		if strings.HasSuffix(strings.ToLower(model), suf) {
			model = strings.TrimSpace(model[:len(model)-len(suf)])
			if effort == "" {
				effort = tier
			}
			break
		}
	}
	// Strip dash tier if present (e.g. "gemini-3.8-flash-high")
	for _, tier := range []string{"ultra", "max", "xhigh", "high", "medium", "low"} {
		if strings.HasSuffix(model, "-"+tier) {
			model = strings.TrimSuffix(model, "-"+tier)
			if effort == "" {
				effort = tier
			}
			break
		}
	}
	// Normalize common Gemini display names to wire slugs
	lowM := strings.ToLower(model)
	if strings.Contains(lowM, "3.8-flash") || strings.Contains(lowM, "3.8 flash") {
		model = "gemini-3.8-flash"
	} else if strings.Contains(lowM, "3.1-pro") || strings.Contains(lowM, "3.1 pro") {
		model = "gemini-3.1-pro"
	} else if strings.Contains(lowM, "2.5-pro") || strings.Contains(lowM, "2.5 pro") {
		model = "gemini-2.5-pro"
	} else if strings.Contains(lowM, "2.5-flash") || strings.Contains(lowM, "2.5 flash") {
		model = "gemini-2.5-flash"
	}

	// If the model is the generic "codex" placeholder or empty, omit it so Codex
	// app-server defaults to the thread's configured model without warnings.
	if strings.EqualFold(model, "codex") {
		model = ""
	}
	if strings.HasPrefix(model, "gemini-") && effort == "" {
		effort = "high"
	}

	type userInput struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type turnStartParams struct {
		ThreadID string      `json:"threadId"`
		Input    []userInput `json:"input"`
		Model    string      `json:"model,omitempty"`
		Effort   string      `json:"effort,omitempty"`
	}

	params := turnStartParams{
		ThreadID: threadID,
		Input: []userInput{
			{Type: "text", Text: promptText},
		},
		Model:  model,
		Effort: effort,
	}

	var turnResp struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}

	go func() {
		err := r.call(ctx, "turn/start", params, &turnResp)
		if err != nil {
			out <- agentlink.Frame{Kind: agentlink.KindResult, Error: err.Error()}
			r.mu.Lock()
			r.busy = false
			if r.frameCh != nil {
				close(r.frameCh)
				r.frameCh = nil
			}
			r.mu.Unlock()
			return
		}

		r.mu.Lock()
		r.activeTurn = turnResp.Turn.ID
		r.mu.Unlock()

		// If context cancels before completion, send turn/interrupt and reset state
		go func(turnID string, threadID string) {
			select {
			case <-ctx.Done():
				if turnID != "" && threadID != "" {
					_ = r.call(context.Background(), "turn/interrupt", map[string]any{
						"threadId": threadID,
						"turnId":   turnID,
					}, nil)
				}
				r.mu.Lock()
				r.busy = false
				r.activeTurn = ""
				if r.frameCh != nil {
					close(r.frameCh)
					r.frameCh = nil
				}
				r.mu.Unlock()
			case <-r.doneCh:
			}
		}(turnResp.Turn.ID, threadID)
	}()

	return out, nil
}

// Close terminates the Codex connection.
func (r *CodexRemote) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// ListModels queries the Codex app-server for available models and their reasoning tiers.
func (r *CodexRemote) ListModels(ctx context.Context) ([]agentturn.ModelInfo, error) {
	r.mu.Lock()
	if len(r.cachedModels) > 0 {
		models := make([]agentturn.ModelInfo, len(r.cachedModels))
		copy(models, r.cachedModels)
		r.mu.Unlock()
		return models, nil
	}
	r.mu.Unlock()

	type codexModelItem struct {
		ID                        string `json:"id"`
		DisplayName               string `json:"displayName"`
		Description               string `json:"description"`
		SupportedReasoningEfforts []struct {
			ReasoningEffort string `json:"reasoningEffort"`
		} `json:"supportedReasoningEfforts"`
		DefaultReasoningEffort string `json:"defaultReasoningEffort"`
		IsDefault              bool   `json:"isDefault"`
		Hidden                 bool   `json:"hidden"`
	}
	var resp struct {
		Data []codexModelItem `json:"data"`
	}
	_ = r.call(ctx, "model/list", map[string]any{}, &resp)

	out := []agentturn.ModelInfo{
		{
			ID:          "gemini-3.8-flash",
			DisplayName: "Gemini 3.8 Flash",
			Description: "Google multimodal model with reasoning tiers",
			Tiers:       []string{"low", "medium", "high"},
			DefaultTier: "high",
		},
		{
			ID:          "gemini-3.1-pro",
			DisplayName: "Gemini 3.1 Pro",
			Description: "Google multimodal reasoning model",
			Tiers:       []string{"low", "high"},
			DefaultTier: "high",
		},
		{
			ID:          "gemini-2.5-pro",
			DisplayName: "Gemini 2.5 Pro",
			Description: "Google reasoning model for complex tasks",
		},
		{
			ID:          "gemini-2.5-flash",
			DisplayName: "Gemini 2.5 Flash",
			Description: "Fast and lightweight multimodal model",
		},
	}
	seen := make(map[string]bool)
	for _, m := range out {
		seen[m.ID] = true
	}

	var defModel string
	for _, m := range resp.Data {
		if m.Hidden || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		var tiers []string
		for _, e := range m.SupportedReasoningEfforts {
			if e.ReasoningEffort != "" {
				tiers = append(tiers, e.ReasoningEffort)
			}
		}
		disp := m.DisplayName
		if disp == "" {
			disp = m.ID
		}
		if m.IsDefault && defModel == "" {
			defModel = m.ID
		}
		out = append(out, agentturn.ModelInfo{
			ID:          m.ID,
			DisplayName: disp,
			Description: m.Description,
			Tiers:       tiers,
			DefaultTier: m.DefaultReasoningEffort,
		})
	}
	if defModel == "" && len(out) > 0 {
		defModel = out[0].ID
	}

	r.mu.Lock()
	r.cachedModels = out
	if r.defaultModel == "" {
		if defModel != "" {
			r.defaultModel = defModel
		} else {
			r.defaultModel = "gemini-3.8-flash-high"
		}
	}
	r.mu.Unlock()

	return out, nil
}

// AvailableCommands returns the slash commands supported by Codex.
func (r *CodexRemote) AvailableCommands() []agentturn.CommandInfo {
	return []agentturn.CommandInfo{
		{Name: "compact", Description: "Summarize conversation to prevent hitting context limit"},
		{Name: "review", Description: "Review recent changes or specified files", InputHint: "[files]"},
	}
}

// EngineName identifies this engine as codex.
func (r *CodexRemote) EngineName() string {
	return "codex"
}

// Capabilities returns capabilities supported by Codex.
func (r *CodexRemote) Capabilities() agentturn.Capability {
	return agentturn.CapModelSwitch | agentturn.CapReasoningTier | agentturn.CapContextInspect
}

// Package crush provides an agentturn.Engine implementation for Crush app-server via stdio JSON-RPC.
package crush

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	coreversion "github.com/justwasm/pigasus/internal/core/version"
)

func init() {
	agentturn.Register(agentturn.Driver{
		Name:         "crush",
		Aliases:      []string{"crush-app-server"},
		Description:  "Crush app-server via stdio JSON-RPC",
		DefaultModel: "default",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnCrushRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model)
		},
	})
}

// CrushRemote runs turns against a Crush app-server over JSON-RPC 2.0 (stdio or pipe).
type CrushRemote struct {
	in      io.Writer
	closer  io.Closer
	writeMu sync.Mutex

	mu          sync.Mutex
	reqSeq      uint64
	threadID    string
	activeTurn  string
	pendingReqs map[uint64]chan crushRPCResponse

	busy    bool
	closed  bool
	frameCh chan agentlink.Frame
	doneCh  chan struct{}

	defaultModel string
	cachedModels []agentturn.ModelInfo
}

var _ agentturn.Engine = (*CrushRemote)(nil)
var _ agentturn.ModelLister = (*CrushRemote)(nil)
var _ agentturn.DefaultModeler = (*CrushRemote)(nil)
var _ agentturn.CommandLister = (*CrushRemote)(nil)
var _ agentturn.CapabilityProvider = (*CrushRemote)(nil)

type crushRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type crushRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *crushRPCError  `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type crushRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// SpawnCrushRemote spawns `crush app-server --listen stdio://` and returns an active CrushRemote engine.
func SpawnCrushRemote(ctx context.Context, crushBin, cwd, model string) (*CrushRemote, error) {
	if crushBin == "" {
		if p, err := exec.LookPath("crush"); err == nil {
			crushBin = p
		} else if _, err := os.Stat("/tmp/crush-bin"); err == nil {
			crushBin = "/tmp/crush-bin"
		} else if _, err := os.Stat("/opt/homebrew/bin/crush"); err == nil {
			crushBin = "/opt/homebrew/bin/crush"
		} else if _, err := os.Stat("/usr/local/bin/crush"); err == nil {
			crushBin = "/usr/local/bin/crush"
		} else {
			return nil, errors.New("crush binary not found on PATH, /tmp/crush-bin, or common locations")
		}
	}

	cmd := exec.Command(crushBin, "app-server", "--listen", "stdio://")
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("crush: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("crush: stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("crush: start app-server: %w", err)
	}

	closer := &procCloser{cmd: cmd, stdin: stdin}
	remote, err := NewCrushRemote(ctx, stdin, stdout, closer, cwd, model)
	if err != nil {
		return nil, err
	}
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

// NewCrushRemote connects to a Crush app-server via reader/writer pipes and initializes the session.
func NewCrushRemote(ctx context.Context, in io.Writer, out io.Reader, closer io.Closer, cwd, model string) (*CrushRemote, error) {
	r := &CrushRemote{
		in:          in,
		closer:      closer,
		pendingReqs: make(map[uint64]chan crushRPCResponse),
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
		ClientInfo: clientInfo{Name: "pigasus-tui", Version: coreversion.Reference},
	}, &initResp); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("crush initialize failed: %w", err)
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
	if err := r.call(ctx, "thread/start", threadParams{
		Cwd:            cwd,
		Model:          model,
		ApprovalPolicy: "never",
	}, &threadResp); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("crush thread/start failed: %w", err)
	}

	r.threadID = threadResp.Thread.ID

	activeModel := threadResp.Model
	if activeModel == "" {
		activeModel = threadResp.Thread.Model
	}
	if activeModel != "" {
		r.defaultModel = activeModel
	} else if model != "" {
		r.defaultModel = model
	} else {
		r.defaultModel = "default"
	}
	return r, nil
}

// DefaultModel returns the default model advertised by Crush app-server, if known.
func (r *CrushRemote) DefaultModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defaultModel
}

// ThreadID returns the active thread ID for this connection.
func (r *CrushRemote) ThreadID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.threadID
}

func (r *CrushRemote) nextID() uint64 {
	return atomic.AddUint64(&r.reqSeq, 1)
}

func (r *CrushRemote) call(ctx context.Context, method string, params any, result any) error {
	id := r.nextID()
	respCh := make(chan crushRPCResponse, 1)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("crush: client closed")
	}
	r.pendingReqs[id] = respCh
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.pendingReqs, id)
		r.mu.Unlock()
	}()

	req := crushRPCRequest{
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
		return fmt.Errorf("crush: write request %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.doneCh:
		return errors.New("crush: connection closed")
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("crush jsonrpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("crush: unmarshal %s result: %w", method, err)
			}
		}
		return nil
	}
}

func (r *CrushRemote) reply(id uint64, result any) {
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

func (r *CrushRemote) readLoop(rd io.Reader) {
	scanner := bufio.NewScanner(rd)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp crushRPCResponse
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

		// Handle Server Request to Client (approvals)
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
		ch <- crushRPCResponse{Error: &crushRPCError{Message: "connection closed"}}
		delete(r.pendingReqs, id)
	}
	r.mu.Unlock()
}

func (r *CrushRemote) handleServerRequest(id uint64, method string, params json.RawMessage) {
	switch method {
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/permissions/requestApproval",
		"applyPatchApproval",
		"execCommandApproval":
		r.reply(id, map[string]any{"decision": "approved"})
	default:
		r.reply(id, map[string]any{"status": "ok"})
	}
}

func (r *CrushRemote) handleNotification(method string, paramsRaw json.RawMessage) {
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

	case "turn/completed":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error,omitempty"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(paramsRaw, &p)
		errMsg := ""
		if p.Turn.Error != nil && p.Turn.Error.Message != "" {
			errMsg = p.Turn.Error.Message
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
			Message string `json:"message"`
		}
		if err := json.Unmarshal(paramsRaw, &p); err == nil && p.Message != "" {
			out <- agentlink.Frame{Kind: agentlink.KindResult, Error: p.Message}
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

// Turn starts a new turn on Crush app-server and streams its events.
func (r *CrushRemote) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("crush: client closed")
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
	if model == "" {
		r.mu.Lock()
		model = r.defaultModel
		r.mu.Unlock()
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
		Effort: t.Effort,
	}

	var turnResp struct {
		TurnID string `json:"turnId"`
		Turn   struct {
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

		turnID := turnResp.Turn.ID
		if turnID == "" {
			turnID = turnResp.TurnID
		}

		r.mu.Lock()
		r.activeTurn = turnID
		r.mu.Unlock()

		go func(tid, thid string) {
			select {
			case <-ctx.Done():
				_ = r.call(context.Background(), "turn/cancel", map[string]any{
					"threadId": thid,
					"turnId":   tid,
				}, nil)
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
		}(turnID, threadID)
	}()

	return out, nil
}

// Close terminates the Crush connection.
func (r *CrushRemote) Close() error {
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

// ListModels queries the Crush app-server for available models.
func (r *CrushRemote) ListModels(ctx context.Context) ([]agentturn.ModelInfo, error) {
	r.mu.Lock()
	if len(r.cachedModels) > 0 {
		models := make([]agentturn.ModelInfo, len(r.cachedModels))
		copy(models, r.cachedModels)
		r.mu.Unlock()
		return models, nil
	}
	r.mu.Unlock()

	type crushModelItem struct {
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
		Data   []crushModelItem `json:"data"`
		Models []crushModelItem `json:"models"`
	}
	_ = r.call(ctx, "model/list", map[string]any{}, &resp)

	items := resp.Data
	if len(items) == 0 {
		items = resp.Models
	}

	var out []agentturn.ModelInfo
	var defModel string
	seen := make(map[string]bool)

	for _, m := range items {
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

	if len(out) == 0 {
		out = []agentturn.ModelInfo{
			{ID: "default", DisplayName: "Crush Default Model"},
		}
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
			r.defaultModel = "default"
		}
	}
	r.mu.Unlock()

	return out, nil
}

// AvailableCommands returns the slash commands supported by Crush.
func (r *CrushRemote) AvailableCommands() []agentturn.CommandInfo {
	return []agentturn.CommandInfo{
		{Name: "compact", Description: "Summarize conversation to prevent hitting context limit"},
		{Name: "review", Description: "Review recent changes or specified files", InputHint: "[files]"},
	}
}

// EngineName identifies this engine as crush.
func (r *CrushRemote) EngineName() string {
	return "crush"
}

// Capabilities returns capabilities supported by Crush.
func (r *CrushRemote) Capabilities() agentturn.Capability {
	return agentturn.CapModelSwitch | agentturn.CapReasoningTier | agentturn.CapContextInspect
}

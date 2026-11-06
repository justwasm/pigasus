// Package pi provides an agentturn.Engine implementation for Mario Zechner's Pi coding agent CLI over NDJSON.
package pi

import (
	"bufio"
	"bytes"
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
	"uuid"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func init() {
	agentturn.Register(agentturn.Driver{
		Name:         "pi",
		Aliases:      []string{"pi-coding-agent", "pi-agent"},
		Description:  "Pi coding agent CLI via NDJSON stream",
		DefaultModel: "claude-sonnet-4-6",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnPiRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model, opts.Effort, opts.SessionID, "pi")
		},
	})
	agentturn.Register(agentturn.Driver{
		Name:         "pig",
		Aliases:      []string{"pig-coding-agent", "pig-agent"},
		Description:  "PiG (Pi-in-Go) coding agent CLI via NDJSON stream",
		DefaultModel: "claude-sonnet-4-6",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnPiRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model, opts.Effort, opts.SessionID, "pig")
		},
	})
}

// Remote is an alias for PiRemote.
type Remote = PiRemote

// PiRemote runs turns against a Pi or PiG coding agent CLI process over NDJSON.
type PiRemote struct {
	in         io.Writer
	closer     io.Closer
	cmd        *exec.Cmd
	piBin      string
	cwd        string
	model      string
	effort     string
	engineName string

	mu         sync.Mutex
	sessionID  string
	busy       bool
	closed     bool
	frameCh    chan agentlink.Frame
	doneCh     chan struct{}
	lastToolID string
	lastToolNm string
	usage      agentlink.Usage
	commands   []string
	reqCounter uint64
}

var _ agentturn.Engine = (*PiRemote)(nil)
var _ agentturn.ModelLister = (*PiRemote)(nil)
var _ agentturn.CommandLister = (*PiRemote)(nil)
var _ agentturn.CapabilityProvider = (*PiRemote)(nil)
var _ agentturn.DefaultModeler = (*PiRemote)(nil)

// SpawnPiRemote initializes an active PiRemote engine connected to the Pi or PiG CLI.
func SpawnPiRemote(ctx context.Context, piBin, cwd, model, effort, sessionID, target string) (*PiRemote, error) {
	if target == "" {
		target = "pi"
	}
	if piBin == "" {
		if target == "pig" {
			if p, err := exec.LookPath("pig"); err == nil {
				piBin = p
			} else if p, err := exec.LookPath("pig-coding-agent"); err == nil {
				piBin = p
			} else {
				home, _ := os.UserHomeDir()
				candidates := []string{
					home + "/.local/bin/pig",
					"/opt/homebrew/bin/pig",
					"/usr/local/bin/pig",
					cwd + "/../PiG/cmd/pig/pig",
				}
				for _, cand := range candidates {
					if _, err := os.Stat(cand); err == nil {
						piBin = cand
						break
					}
				}
			}
		} else {
			if p, err := exec.LookPath("pi"); err == nil {
				piBin = p
			} else if p, err := exec.LookPath("pi-coding-agent"); err == nil {
				piBin = p
			} else {
				home, _ := os.UserHomeDir()
				candidates := []string{
					home + "/.local/bin/pi",
					"/opt/homebrew/bin/pi",
					"/usr/local/bin/pi",
					home + "/.npm-global/bin/pi",
					home + "/.yarn/bin/pi",
					home + "/.local/bin/pi-coding-agent",
					"/opt/homebrew/bin/pi-coding-agent",
					"/usr/local/bin/pi-coding-agent",
					// fallback to pig if pi is not found
					home + "/.local/bin/pig",
					"/opt/homebrew/bin/pig",
					"/usr/local/bin/pig",
				}
				for _, cand := range candidates {
					if _, err := os.Stat(cand); err == nil {
						piBin = cand
						break
					}
				}
			}
		}
	}
	if piBin == "" {
		return nil, fmt.Errorf("%s binary not found on PATH or common locations (~/.local/bin/%s, /opt/homebrew/bin/%s, /usr/local/bin/%s)", target, target, target, target)
	}

	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	r := &PiRemote{
		piBin:      piBin,
		cwd:        cwd,
		model:      model,
		effort:     effort,
		sessionID:  sessionID,
		engineName: target,
		doneCh:     make(chan struct{}),
	}

	// Try spawning a long-lived RPC daemon process for low-latency turns
	args := []string{"--mode", "rpc"}
	if sessionID != "" {
		args = append(args, "--session-id", sessionID)
	} else {
		args = append(args, "--no-session")
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--thinking", effort)
	}

	cmd := exec.CommandContext(ctx, piBin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdin, err := cmd.StdinPipe()
	if err == nil {
		stdout, err := cmd.StdoutPipe()
		if err == nil {
			if err := cmd.Start(); err == nil {
				r.in = stdin
				r.closer = stdin
				r.cmd = cmd
				go r.readLoop(stdout)
				return r, nil
			}
		}
	}

	// Fallback to per-prompt headless mode if RPC daemon fails to start
	return r, nil
}

// NewPiRemote wraps stream reader/writer pipes connected to Pi.
func NewPiRemote(in io.Writer, out io.Reader, closer io.Closer, cmd *exec.Cmd, sessionID string) (*PiRemote, error) {
	r := &PiRemote{
		in:        in,
		closer:    closer,
		cmd:       cmd,
		sessionID: sessionID,
		doneCh:    make(chan struct{}),
	}
	if out != nil {
		go r.readLoop(out)
	}
	return r, nil
}

// SessionID returns the current Pi session ID.
func (r *PiRemote) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

// DefaultModel returns the configured default model.
func (r *PiRemote) DefaultModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.model != "" {
		return r.model
	}
	return "claude-sonnet-4-6"
}

func (r *PiRemote) nextReqID() string {
	id := atomic.AddUint64(&r.reqCounter, 1)
	return fmt.Sprintf("req-%d", id)
}

func (r *PiRemote) readLoop(rd io.Reader) {
	scanner := bufio.NewScanner(rd)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		r.handleLine(line)
	}

	r.finishTurn()
}

type piBaseMsg struct {
	Type                  string          `json:"type,omitempty"`
	ID                    string          `json:"id,omitempty"`
	Command               string          `json:"command,omitempty"`
	Success               *bool           `json:"success,omitempty"`
	Error                 string          `json:"error,omitempty"`
	AssistantMessageEvent json.RawMessage `json:"assistantMessageEvent,omitempty"`
	Usage                 *piUsage        `json:"usage,omitempty"`
	ToolCallID            string          `json:"toolCallId,omitempty"`
	ToolName              string          `json:"toolName,omitempty"`
	Args                  any             `json:"args,omitempty"`
	Result                any             `json:"result,omitempty"`
	IsError               bool            `json:"isError,omitempty"`
	Aborted               bool            `json:"aborted,omitempty"`
	Reason                string          `json:"reason,omitempty"`
	SessionID             string          `json:"sessionId,omitempty"`
}

type piUsage struct {
	Input       int `json:"input,omitempty"`
	Output      int `json:"output,omitempty"`
	CacheRead   int `json:"cacheRead,omitempty"`
	CacheWrite  int `json:"cacheWrite,omitempty"`
	TotalTokens int `json:"totalTokens,omitempty"`
}

func (r *PiRemote) handleLine(line []byte) {
	if len(line) == 0 {
		return
	}
	var base piBaseMsg
	if err := json.Unmarshal(line, &base); err != nil {
		return
	}

	eventType := strings.ToLower(strings.TrimSpace(base.Type))

	r.mu.Lock()
	out := r.frameCh
	if base.Usage != nil {
		r.usage = agentlink.Usage{
			PromptTokens:     base.Usage.Input,
			CompletionTokens: base.Usage.Output,
			TotalTokens:      base.Usage.TotalTokens,
		}
	}
	if base.SessionID != "" {
		r.sessionID = base.SessionID
	}
	r.mu.Unlock()

	switch eventType {
	case "message_update":
		if len(base.AssistantMessageEvent) > 0 {
			var sub struct {
				Type  string `json:"type,omitempty"`
				Delta string `json:"delta,omitempty"`
			}
			if err := json.Unmarshal(base.AssistantMessageEvent, &sub); err == nil && out != nil {
				subType := strings.ToLower(strings.TrimSpace(sub.Type))
				switch subType {
				case "text_delta":
					if sub.Delta != "" {
						out <- agentlink.Frame{Kind: agentlink.KindText, Text: sub.Delta}
					}
				case "thinking_delta":
					if sub.Delta != "" {
						out <- agentlink.Frame{Kind: agentlink.KindThought, Text: sub.Delta}
					}
				}
			}
		}

	case "tool_execution_start":
		if out != nil {
			callID := base.ToolCallID
			toolNm := base.ToolName
			r.mu.Lock()
			r.lastToolID = callID
			r.lastToolNm = toolNm
			r.mu.Unlock()

			argsStr := ""
			if base.Args != nil {
				if b, err := json.Marshal(base.Args); err == nil {
					argsStr = string(b)
				}
			}

			out <- agentlink.Frame{
				Kind: agentlink.KindTool,
				ID:   callID,
				Tool: &agentlink.Tool{
					Name:  toolNm,
					Phase: agentlink.ToolStart,
					Args:  argsStr,
				},
			}
		}

	case "tool_execution_update":
		if out != nil {
			var update struct {
				PartialResult any `json:"partialResult,omitempty"`
			}
			_ = json.Unmarshal(line, &update)
			text := extractResultText(update.PartialResult)
			if text != "" {
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					ID:   base.ToolCallID,
					Tool: &agentlink.Tool{
						Name:   base.ToolName,
						Phase:  agentlink.ToolStart,
						Result: text,
					},
				}
			}
		}

	case "tool_execution_end":
		if out != nil {
			callID := base.ToolCallID
			toolNm := base.ToolName
			if callID == "" {
				r.mu.Lock()
				callID = r.lastToolID
				toolNm = r.lastToolNm
				r.mu.Unlock()
			}
			text := extractResultText(base.Result)

			out <- agentlink.Frame{
				Kind: agentlink.KindTool,
				ID:   callID,
				Tool: &agentlink.Tool{
					Name:   toolNm,
					Phase:  agentlink.ToolEnd,
					Result: text,
					Err:    base.IsError,
				},
			}
		}

	case "compaction_start", "compaction_end":
		if out != nil {
			phase := agentlink.CompactStart
			if eventType == "compaction_end" {
				phase = agentlink.CompactEnd
			}
			out <- agentlink.Frame{
				Kind: agentlink.KindCompact,
				Compact: &agentlink.Compact{
					Phase: phase,
				},
			}
		}

	case "turn_end":
		// assistant turn and tool execution finished

	case "agent_end", "agent_settled":
		if out != nil {
			r.mu.Lock()
			usage := r.usage
			r.mu.Unlock()

			out <- agentlink.Frame{
				Kind:  agentlink.KindResult,
				Usage: &usage,
			}
		}
		r.finishTurn()

	case "response":
		if base.Success != nil && !*base.Success && base.Error != "" {
			if out != nil {
				out <- agentlink.Frame{
					Kind:  agentlink.KindResult,
					Error: base.Error,
				}
			}
			r.finishTurn()
		}
	}
}

func extractResultText(raw any) string {
	if raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	if m, ok := raw.(map[string]any); ok {
		if content, ok := m["content"].([]any); ok {
			var b strings.Builder
			for _, item := range content {
				if itemMap, ok := item.(map[string]any); ok {
					if txt, ok := itemMap["text"].(string); ok {
						b.WriteString(txt)
					}
				}
			}
			if b.Len() > 0 {
				return b.String()
			}
		}
		if txt, ok := m["text"].(string); ok {
			return txt
		}
	}
	data, _ := json.Marshal(raw)
	return string(data)
}

// Turn starts a new turn by running Pi or writing to its Stdio input.
func (r *PiRemote) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("pi: process closed")
	}
	if r.busy {
		r.mu.Unlock()
		return nil, errors.New("agentturn: a turn is already running")
	}

	r.busy = true
	r.usage = agentlink.Usage{}
	out := make(chan agentlink.Frame, 64)
	r.frameCh = out
	r.mu.Unlock()

	var promptText string
	for i := len(t.History) - 1; i >= 0; i-- {
		if t.History[i].Role == "user" {
			promptText = t.History[i].Content
			break
		}
	}
	if strings.TrimSpace(promptText) == "" {
		r.finishTurn()
		return nil, errors.New("pi: prompt is empty")
	}

	if t.Model != "" {
		r.mu.Lock()
		r.model = t.Model
		r.mu.Unlock()
	}

	// 1. Wrapped Stdio / RPC daemon mode
	if r.in != nil {
		reqID := r.nextReqID()
		payload := map[string]any{
			"id":      reqID,
			"type":    "prompt",
			"message": promptText,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			r.finishTurn()
			return nil, err
		}
		data = append(data, '\n')
		if _, err := r.in.Write(data); err != nil {
			r.finishTurn()
			return nil, fmt.Errorf("pi: write prompt: %w", err)
		}
		return out, nil
	}

	// 2. Headless per-prompt execution mode
	if r.piBin == "" {
		r.finishTurn()
		return nil, errors.New("pi: no binary available to run turn")
	}

	args := []string{
		"--mode", "json",
		"-p",
	}
	r.mu.Lock()
	if r.sessionID != "" {
		args = append(args, "--session-id", r.sessionID)
	} else {
		args = append(args, "--no-session")
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	if r.effort != "" {
		args = append(args, "--thinking", r.effort)
	}
	cwd := r.cwd
	r.mu.Unlock()

	args = append(args, promptText)

	cmd := exec.CommandContext(ctx, r.piBin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.finishTurn()
		return nil, fmt.Errorf("pi: stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		r.finishTurn()
		return nil, fmt.Errorf("pi: start process: %w", err)
	}

	r.mu.Lock()
	r.cmd = cmd
	r.mu.Unlock()

	go func() {
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 16*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			r.handleLine(line)
		}
		err := cmd.Wait()
		if err != nil && stderrBuf.Len() > 0 {
			errStr := strings.TrimSpace(stderrBuf.String())
			r.mu.Lock()
			outCh := r.frameCh
			r.mu.Unlock()
			if outCh != nil {
				outCh <- agentlink.Frame{
					Kind:  agentlink.KindResult,
					Error: fmt.Sprintf("pi error (%v): %s", err, errStr),
				}
			}
		}
		r.finishTurn()
	}()

	return out, nil
}

func (r *PiRemote) finishTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.busy = false
	if r.frameCh != nil {
		close(r.frameCh)
		r.frameCh = nil
	}
}

// Interrupt interrupts the active turn if a process is running.
func (r *PiRemote) Interrupt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.in != nil {
		// In RPC mode, send abort command
		reqID := r.nextReqID()
		cmdData, _ := json.Marshal(map[string]any{
			"id":   reqID,
			"type": "abort",
		})
		_, _ = r.in.Write(append(cmdData, '\n'))
	} else if r.cmd != nil && r.cmd.Process != nil && r.busy {
		_ = r.cmd.Process.Signal(os.Interrupt)
	}
}

// Close terminates the Pi process.
func (r *PiRemote) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	if r.closer != nil {
		_ = r.closer.Close()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	return nil
}

// ListModels returns the available models for Pi CLI.
func (r *PiRemote) ListModels(ctx context.Context) ([]agentturn.ModelInfo, error) {
	return []agentturn.ModelInfo{
		{
			ID:          "claude-sonnet-4-6",
			DisplayName: "Claude Sonnet 4.6 (Pi Default)",
			Description: "High-reasoning coding assistant model",
			Tiers:       []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"},
			DefaultTier: "high",
		},
		{
			ID:          "anthropic/claude-3-7-sonnet",
			DisplayName: "Claude 3.7 Sonnet Thinking",
			Description: "Hybrid reasoning model with extended thinking",
			Tiers:       []string{"off", "minimal", "low", "medium", "high", "max"},
			DefaultTier: "high",
		},
		{
			ID:          "gemini-3.8-flash-high",
			DisplayName: "Gemini 3.8 Flash High",
			Description: "High-speed multimodal reasoning model",
			Tiers:       []string{"low", "medium", "high"},
			DefaultTier: "high",
		},
		{
			ID:          "openai/gpt-4o",
			DisplayName: "GPT-4o",
			Description: "OpenAI flagship multimodal coding model",
		},
		{
			ID:          "deepseek/deepseek-reasoner",
			DisplayName: "DeepSeek R1 / Reasoner",
			Description: "Frontier open-weights reasoning model",
		},
		{
			ID:          "xai/grok-4.7",
			DisplayName: "Grok 4.7",
			Description: "xAI high-throughput reasoning model",
		},
	}, nil
}

// AvailableCommands returns the slash commands supported by Pi CLI.
func (r *PiRemote) AvailableCommands() []agentturn.CommandInfo {
	return []agentturn.CommandInfo{
		{Name: "compact", Description: "Compact chat context in Pi session"},
		{Name: "session", Description: "Manage Pi session state and bookmarks"},
		{Name: "stats", Description: "Display session token usage and estimated cost"},
		{Name: "tools", Description: "List and configure tool allowlist/denylist"},
	}
}

// EngineName identifies this engine as pi or pig.
func (r *PiRemote) EngineName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engineName != "" {
		return r.engineName
	}
	return "pi"
}

// Capabilities returns capabilities supported by Pi CLI.
func (r *PiRemote) Capabilities() agentturn.Capability {
	return agentturn.CapModelSwitch | agentturn.CapReasoningTier | agentturn.CapContextInspect
}

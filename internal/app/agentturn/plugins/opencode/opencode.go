// Package opencode provides an agentturn.Engine implementation for OpenCode CLI over NDJSON.
package opencode

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

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func init() {
	agentturn.Register(agentturn.Driver{
		Name:         "opencode",
		Aliases:      []string{"open-code"},
		Description:  "OpenCode CLI via NDJSON stream",
		DefaultModel: "local/gemini-3.8-flash-high",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnOpencodeRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model, opts.SessionID)
		},
	})
}

// Remote is an alias for OpencodeRemote.
type Remote = OpencodeRemote

// OpencodeRemote runs turns against an OpenCode CLI process over NDJSON.
type OpencodeRemote struct {
	in          io.Writer
	closer      io.Closer
	cmd         *exec.Cmd
	opencodeBin string
	cwd         string
	model       string

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
}

var _ agentturn.Engine = (*OpencodeRemote)(nil)
var _ agentturn.ModelLister = (*OpencodeRemote)(nil)
var _ agentturn.CommandLister = (*OpencodeRemote)(nil)
var _ agentturn.CapabilityProvider = (*OpencodeRemote)(nil)

// SpawnOpencodeRemote initializes an active OpencodeRemote engine connected to the OpenCode CLI.
func SpawnOpencodeRemote(ctx context.Context, opencodeBin, cwd, model, sessionID string) (*OpencodeRemote, error) {
	if opencodeBin == "" {
		if p, err := exec.LookPath("opencode"); err == nil {
			opencodeBin = p
		} else {
			home, _ := os.UserHomeDir()
			candidates := []string{
				"/opt/homebrew/bin/opencode",
				"/usr/local/bin/opencode",
				home + "/.local/bin/opencode",
				home + "/.opencode/bin/opencode",
			}
			for _, cand := range candidates {
				if _, err := os.Stat(cand); err == nil {
					opencodeBin = cand
					break
				}
			}
		}
	}
	if opencodeBin == "" {
		return nil, errors.New("opencode binary not found on PATH or common locations (/opt/homebrew/bin, /usr/local/bin, ~/.local/bin)")
	}

	if sessionID != "" && !strings.HasPrefix(sessionID, "ses_") {
		sessionID = "ses_" + sessionID
	}

	r := &OpencodeRemote{
		opencodeBin: opencodeBin,
		cwd:         cwd,
		model:       model,
		sessionID:   sessionID,
		doneCh:      make(chan struct{}),
	}
	return r, nil
}

// NewOpencodeRemote wraps stream reader/writer pipes connected to OpenCode.
func NewOpencodeRemote(in io.Writer, out io.Reader, closer io.Closer, cmd *exec.Cmd, sessionID string) (*OpencodeRemote, error) {
	r := &OpencodeRemote{
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

// SessionID returns the current OpenCode session ID.
func (r *OpencodeRemote) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

func (r *OpencodeRemote) readLoop(rd io.Reader) {
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

type opencodeWireEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Error     *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	Part struct {
		ID     string `json:"id"`
		PartID string `json:"partID"`
		Type   string `json:"type"`
		Text   string `json:"text"`
		Tool   string `json:"tool"`
		Reason string `json:"reason"`
		State  *struct {
			Status string `json:"status"`
			Input  any    `json:"input"`
			Output string `json:"output"`
		} `json:"state"`
		Tokens *struct {
			Input     int `json:"input"`
			Output    int `json:"output"`
			Reasoning int `json:"reasoning"`
		} `json:"tokens"`
	} `json:"part"`
}

func (r *OpencodeRemote) handleLine(line []byte) {
	if len(line) == 0 {
		return
	}
	var ev opencodeWireEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}

	if ev.SessionID != "" {
		r.mu.Lock()
		r.sessionID = ev.SessionID
		r.mu.Unlock()
	}

	r.mu.Lock()
	out := r.frameCh
	r.mu.Unlock()

	eventType := strings.ToLower(strings.TrimSpace(ev.Type))
	partType := strings.ToLower(strings.TrimSpace(ev.Part.Type))

	switch {
	case eventType == "reasoning" || partType == "reasoning" || eventType == "thought":
		t := ev.Part.Text
		if t != "" && out != nil {
			out <- agentlink.Frame{Kind: agentlink.KindThought, Text: t}
		}

	case eventType == "text" || partType == "text":
		t := ev.Part.Text
		if t != "" && out != nil {
			out <- agentlink.Frame{Kind: agentlink.KindText, Text: t}
		}

	case eventType == "tool_use" || partType == "tool":
		callID := ev.Part.ID
		if callID == "" {
			callID = ev.Part.PartID
		}
		toolName := ev.Part.Tool
		if toolName == "" {
			toolName = "tool"
		}
		status := ""
		argsStr := ""
		outputStr := ""
		if ev.Part.State != nil {
			status = ev.Part.State.Status
			outputStr = ev.Part.State.Output
			if ev.Part.State.Input != nil {
				if b, err := json.Marshal(ev.Part.State.Input); err == nil {
					argsStr = string(b)
				}
			}
		}

		r.mu.Lock()
		r.lastToolID = callID
		r.lastToolNm = toolName
		r.mu.Unlock()

		if out != nil {
			// If completed directly in single event
			if status == "completed" || status == "error" || outputStr != "" {
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					ID:   callID,
					Tool: &agentlink.Tool{
						Name:  toolName,
						Phase: agentlink.ToolStart,
						Args:  argsStr,
					},
				}
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					ID:   callID,
					Tool: &agentlink.Tool{
						Name:   toolName,
						Phase:  agentlink.ToolEnd,
						Result: outputStr,
					},
				}
			} else {
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					ID:   callID,
					Tool: &agentlink.Tool{
						Name:  toolName,
						Phase: agentlink.ToolStart,
						Args:  argsStr,
					},
				}
			}
		}

	case eventType == "step_finish" || partType == "step-finish":
		r.mu.Lock()
		if ev.Part.Tokens != nil {
			r.usage.PromptTokens = ev.Part.Tokens.Input
			r.usage.CompletionTokens = ev.Part.Tokens.Output
			r.usage.TotalTokens = ev.Part.Tokens.Input + ev.Part.Tokens.Output
		}
		usageCopy := r.usage
		r.mu.Unlock()

		if ev.Part.Reason == "stop" && out != nil {
			resFrame := agentlink.Frame{
				Kind: agentlink.KindResult,
			}
			if usageCopy.TotalTokens > 0 || usageCopy.PromptTokens > 0 {
				resFrame.Usage = &agentlink.Usage{
					PromptTokens:     usageCopy.PromptTokens,
					CompletionTokens: usageCopy.CompletionTokens,
					TotalTokens:      usageCopy.TotalTokens,
				}
			}
			out <- resFrame
		}

	case eventType == "error":
		errMsg := "opencode error"
		if ev.Error != nil && ev.Error.Message != "" {
			errMsg = ev.Error.Message
		}
		if out != nil {
			out <- agentlink.Frame{
				Kind:  agentlink.KindResult,
				Error: errMsg,
			}
		}
	}
}

// Turn starts a new turn by running OpenCode or writing to its Stdio input.
func (r *OpencodeRemote) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("opencode: process closed")
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
		return nil, errors.New("opencode: prompt is empty")
	}

	if t.Model != "" {
		r.mu.Lock()
		r.model = t.Model
		r.mu.Unlock()
	}

	// 1. Wrapped Stdio pipe mode (mock / continuous daemon)
	if r.in != nil {
		payload := map[string]any{
			"type": "user",
			"text": promptText,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			r.finishTurn()
			return nil, err
		}
		data = append(data, '\n')
		if _, err := r.in.Write(data); err != nil {
			r.finishTurn()
			return nil, fmt.Errorf("opencode: write prompt: %w", err)
		}
		return out, nil
	}

	// 2. Subprocess headless execution mode
	if r.opencodeBin == "" {
		r.finishTurn()
		return nil, errors.New("opencode: no binary available to run turn")
	}

	args := []string{
		"run",
		"--format", "json",
		"--thinking",
		"--auto",
	}
	r.mu.Lock()
	if r.sessionID != "" {
		args = append(args, "--session", r.sessionID)
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	cwd := r.cwd
	r.mu.Unlock()

	args = append(args, promptText)

	cmd := exec.CommandContext(ctx, r.opencodeBin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.finishTurn()
		return nil, fmt.Errorf("opencode: stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		r.finishTurn()
		return nil, fmt.Errorf("opencode: start process: %w", err)
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
					Error: fmt.Sprintf("opencode error (%v): %s", err, errStr),
				}
			}
		}
		r.finishTurn()
	}()

	return out, nil
}

func (r *OpencodeRemote) finishTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.busy = false
	if r.frameCh != nil {
		close(r.frameCh)
		r.frameCh = nil
	}
}

// Interrupt interrupts the active turn if a process is running.
func (r *OpencodeRemote) Interrupt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.cmd.Process != nil && r.busy {
		_ = r.cmd.Process.Signal(os.Interrupt)
	}
}

// Close terminates the OpenCode process.
func (r *OpencodeRemote) Close() error {
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

// ListModels returns the available models for OpenCode CLI.
func (r *OpencodeRemote) ListModels(ctx context.Context) ([]agentturn.ModelInfo, error) {
	return []agentturn.ModelInfo{
		{
			ID:          "local/gemini-3.8-flash-high",
			DisplayName: "Gemini 3.8 Flash (Local High)",
			Description: "Fast local Gemini model configured in OpenCode",
			Tiers:       []string{"low", "medium", "high"},
			DefaultTier: "high",
		},
		{
			ID:          "local/claude-sonnet-4-6-thinking",
			DisplayName: "Claude Sonnet 4.6 Thinking (Local)",
			Description: "Local Claude Sonnet with thinking enabled",
			Tiers:       []string{"low", "medium", "high"},
			DefaultTier: "high",
		},
		{
			ID:          "opencode/claude-sonnet-5-5",
			DisplayName: "Claude Sonnet 5.5",
			Description: "Anthropic flagship reasoning model via OpenCode",
		},
		{
			ID:          "opencode/gpt-5.5",
			DisplayName: "GPT 5.5",
			Description: "OpenAI frontier model via OpenCode",
		},
		{
			ID:          "opencode/deepseek-v4.1-flash",
			DisplayName: "DeepSeek V4.1 Flash",
			Description: "High-speed reasoning model via OpenCode",
		},
		{
			ID:          "opencode/grok-4.7",
			DisplayName: "Grok 4.7",
			Description: "xAI reasoning model via OpenCode",
		},
	}, nil
}

// AvailableCommands returns the slash commands supported by OpenCode CLI.
func (r *OpencodeRemote) AvailableCommands() []agentturn.CommandInfo {
	return []agentturn.CommandInfo{
		{Name: "compact", Description: "Compact chat context in OpenCode session"},
		{Name: "models", Description: "List OpenCode available models"},
		{Name: "stats", Description: "Show shareable usage statistics"},
		{Name: "session", Description: "Manage OpenCode session state"},
	}
}

// EngineName identifies this engine as opencode.
func (r *OpencodeRemote) EngineName() string {
	return "opencode"
}

// Capabilities returns capabilities supported by OpenCode CLI.
func (r *OpencodeRemote) Capabilities() agentturn.Capability {
	return agentturn.CapModelSwitch | agentturn.CapReasoningTier | agentturn.CapContextInspect
}

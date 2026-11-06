// Package grok provides an agentturn.Engine implementation for xAI Grok CLI over streaming-json NDJSON.
package grok

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
	"uuid"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func init() {
	agentturn.Register(agentturn.Driver{
		Name:         "grok",
		Aliases:      []string{"grok-build"},
		Description:  "Grok CLI via streaming-json NDJSON",
		DefaultModel: "grok-4.7",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnGrokRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model, opts.Effort, opts.SessionID)
		},
	})
}

// Remote is an alias for GrokRemote.
type Remote = GrokRemote

// GrokRemote runs turns against an xAI Grok CLI process over streaming-json NDJSON.
type GrokRemote struct {
	in      io.Writer
	closer  io.Closer
	cmd     *exec.Cmd
	grokBin string
	cwd     string
	model   string
	effort  string

	mu             sync.Mutex
	sessionID      string
	sessionStarted bool
	busy           bool
	closed         bool
	frameCh        chan agentlink.Frame
	doneCh         chan struct{}
	lastToolID     string
	lastToolNm     string
	usage          agentlink.Usage
	commands       []string
}

var _ agentturn.Engine = (*GrokRemote)(nil)
var _ agentturn.ModelLister = (*GrokRemote)(nil)
var _ agentturn.CommandLister = (*GrokRemote)(nil)
var _ agentturn.CapabilityProvider = (*GrokRemote)(nil)

// SpawnGrokRemote initializes an active GrokRemote engine connected to the Grok CLI.
func SpawnGrokRemote(ctx context.Context, grokBin, cwd, model, effort, sessionID string) (*GrokRemote, error) {
	if grokBin == "" {
		if p, err := exec.LookPath("grok"); err == nil {
			grokBin = p
		} else if p, err := exec.LookPath("grok-build"); err == nil {
			grokBin = p
		} else {
			home, _ := os.UserHomeDir()
			candidates := []string{
				home + "/.grok/bin/grok",
				home + "/.grok/bin/grok-build",
				home + "/.local/bin/grok",
				"/opt/homebrew/bin/grok",
				"/usr/local/bin/grok",
				home + "/.local/bin/grok-build",
				"/opt/homebrew/bin/grok-build",
				"/usr/local/bin/grok-build",
			}
			for _, cand := range candidates {
				if _, err := os.Stat(cand); err == nil {
					grokBin = cand
					break
				}
			}
		}
	}
	if grokBin == "" {
		return nil, errors.New("grok binary not found on PATH or common locations (~/.grok/bin, ~/.local/bin, /opt/homebrew/bin, /usr/local/bin)")
	}

	if sessionID == "" {
		sessionID = uuid.New().String()
	} else if _, err := uuid.Parse(sessionID); err != nil {
		sessionID = uuid.New().String()
	}

	r := &GrokRemote{
		grokBin:   grokBin,
		cwd:       cwd,
		model:     model,
		effort:    effort,
		sessionID: sessionID,
		doneCh:    make(chan struct{}),
	}
	return r, nil
}

// NewGrokRemote wraps stream reader/writer pipes connected to Grok.
func NewGrokRemote(in io.Writer, out io.Reader, closer io.Closer, cmd *exec.Cmd, sessionID string) (*GrokRemote, error) {
	r := &GrokRemote{
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

// SessionID returns the current Grok session ID.
func (r *GrokRemote) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

func (r *GrokRemote) readLoop(rd io.Reader) {
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

type grokBaseMsg struct {
	Type      string `json:"type,omitempty"`
	Event     string `json:"event,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	MessageID string `json:"messageId,omitempty"`
}

func (r *GrokRemote) handleLine(line []byte) {
	if len(line) == 0 {
		return
	}
	var base grokBaseMsg
	if err := json.Unmarshal(line, &base); err != nil {
		return
	}

	eventType := base.Type
	if eventType == "" {
		eventType = base.Event
	}
	eventType = strings.ToLower(strings.TrimSpace(eventType))

	if base.SessionID != "" {
		r.mu.Lock()
		r.sessionID = base.SessionID
		r.mu.Unlock()
	}

	r.mu.Lock()
	out := r.frameCh
	r.mu.Unlock()

	switch eventType {
	case "thought", "thinking":
		var ev struct {
			Text     string `json:"text,omitempty"`
			Thinking string `json:"thinking,omitempty"`
			Delta    string `json:"delta,omitempty"`
			Data     string `json:"data,omitempty"`
		}
		if err := json.Unmarshal(line, &ev); err == nil && out != nil {
			t := ev.Text
			if t == "" {
				t = ev.Thinking
			}
			if t == "" {
				t = ev.Delta
			}
			if t == "" {
				t = ev.Data
			}
			if t != "" {
				out <- agentlink.Frame{Kind: agentlink.KindThought, Text: t}
			}
		}

	case "text", "content", "message":
		var ev struct {
			Text    string `json:"text,omitempty"`
			Delta   string `json:"delta,omitempty"`
			Content string `json:"content,omitempty"`
			Data    string `json:"data,omitempty"`
		}
		if err := json.Unmarshal(line, &ev); err == nil && out != nil {
			t := ev.Text
			if t == "" {
				t = ev.Delta
			}
			if t == "" {
				t = ev.Content
			}
			if t == "" {
				t = ev.Data
			}
			if t != "" {
				out <- agentlink.Frame{Kind: agentlink.KindText, Text: t}
			}
		}

	case "tool_call", "tool_use", "tool":
		var ev struct {
			ID         string          `json:"id,omitempty"`
			ToolCallID string          `json:"toolCallId,omitempty"`
			Tool       string          `json:"tool,omitempty"`
			ToolName   string          `json:"toolName,omitempty"`
			Name       string          `json:"name,omitempty"`
			Command    string          `json:"command,omitempty"`
			RawInput   any             `json:"rawInput,omitempty"`
			Input      any             `json:"input,omitempty"`
			Parameters json.RawMessage `json:"parameters,omitempty"`
			Args       json.RawMessage `json:"args,omitempty"`
		}
		if err := json.Unmarshal(line, &ev); err == nil && out != nil {
			callID := ev.ToolCallID
			if callID == "" {
				callID = ev.ID
			}
			toolName := ev.ToolName
			if toolName == "" {
				toolName = ev.Tool
			}
			if toolName == "" {
				toolName = ev.Name
			}
			if toolName == "" {
				toolName = "tool"
			}
			argsStr := ev.Command
			if argsStr == "" && ev.RawInput != nil {
				if b, err := json.Marshal(ev.RawInput); err == nil {
					argsStr = string(b)
				}
			}
			if argsStr == "" && ev.Input != nil {
				if b, err := json.Marshal(ev.Input); err == nil {
					argsStr = string(b)
				}
			}
			if argsStr == "" && len(ev.Parameters) > 0 {
				argsStr = string(ev.Parameters)
			}
			if argsStr == "" && len(ev.Args) > 0 {
				argsStr = string(ev.Args)
			}
			r.mu.Lock()
			r.lastToolID = callID
			r.lastToolNm = toolName
			r.mu.Unlock()

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

	case "tool_call_update", "tool_result", "tool_update", "tool_output":
		var ev struct {
			ID         string `json:"id,omitempty"`
			ToolCallID string `json:"toolCallId,omitempty"`
			ToolUseID  string `json:"tool_use_id,omitempty"`
			Status     string `json:"status,omitempty"`
			Content    []struct {
				Type    string `json:"type,omitempty"`
				Content struct {
					Type string `json:"type,omitempty"`
					Text string `json:"text,omitempty"`
				} `json:"content,omitempty"`
			} `json:"content,omitempty"`
			RawOutput json.RawMessage `json:"rawOutput,omitempty"`
			Output    string          `json:"output,omitempty"`
			Result    string          `json:"result,omitempty"`
			Error     string          `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &ev); err == nil && out != nil {
			callID := ev.ToolCallID
			if callID == "" {
				callID = ev.ToolUseID
			}
			if callID == "" {
				callID = ev.ID
			}
			toolName := "tool"
			r.mu.Lock()
			if r.lastToolNm != "" {
				toolName = r.lastToolNm
			}
			if callID == "" {
				callID = r.lastToolID
			}
			r.mu.Unlock()

			var resStr string
			for _, c := range ev.Content {
				if c.Content.Text != "" {
					if resStr != "" {
						resStr += "\n"
					}
					resStr += c.Content.Text
				}
			}
			if resStr == "" {
				resStr = ev.Output
			}
			if resStr == "" {
				resStr = ev.Result
			}
			if resStr == "" && len(ev.RawOutput) > 0 {
				var rawObj map[string]any
				if err := json.Unmarshal(ev.RawOutput, &rawObj); err == nil {
					if p, ok := rawObj["output_for_prompt"].(string); ok && p != "" {
						resStr = p
					} else if errStr, ok := rawObj["error"].(string); ok && errStr != "" {
						resStr = errStr
					}
				}
				if resStr == "" {
					resStr = string(ev.RawOutput)
				}
			}
			if ev.Error != "" {
				if resStr != "" {
					resStr += "\nError: " + ev.Error
				} else {
					resStr = "Error: " + ev.Error
				}
			}

			if ev.Status == "completed" || ev.Error != "" || (ev.Status == "" && resStr != "") {
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					ID:   callID,
					Tool: &agentlink.Tool{
						Name:   toolName,
						Phase:  agentlink.ToolEnd,
						Result: resStr,
					},
				}
			}
		}

	case "plan":
		var ev struct {
			Entries []struct {
				Step   string `json:"step,omitempty"`
				Status string `json:"status,omitempty"`
			} `json:"entries,omitempty"`
		}
		if err := json.Unmarshal(line, &ev); err == nil && out != nil && len(ev.Entries) > 0 {
			var sb strings.Builder
			sb.WriteString("Plan update:\n")
			for _, e := range ev.Entries {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", e.Status, e.Step))
			}
			out <- agentlink.Frame{Kind: agentlink.KindNotice, Text: sb.String()}
		}

	case "available_commands":
		var ev struct {
			Tools    []string `json:"tools,omitempty"`
			Commands []string `json:"commands,omitempty"`
		}
		if err := json.Unmarshal(line, &ev); err == nil {
			r.mu.Lock()
			if len(ev.Commands) > 0 {
				r.commands = ev.Commands
			}
			r.mu.Unlock()
		}

	case "usage":
		var ev struct {
			Usage struct {
				PromptTokens     int `json:"prompt_tokens,omitempty"`
				CompletionTokens int `json:"completion_tokens,omitempty"`
				InputTokens      int `json:"input_tokens,omitempty"`
				OutputTokens     int `json:"output_tokens,omitempty"`
				TotalTokens      int `json:"total_tokens,omitempty"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(line, &ev); err == nil {
			r.mu.Lock()
			inp := ev.Usage.InputTokens
			if inp == 0 {
				inp = ev.Usage.PromptTokens
			}
			outp := ev.Usage.OutputTokens
			if outp == 0 {
				outp = ev.Usage.CompletionTokens
			}
			r.usage.PromptTokens = inp
			r.usage.CompletionTokens = outp
			if ev.Usage.TotalTokens > 0 {
				r.usage.TotalTokens = ev.Usage.TotalTokens
			} else {
				r.usage.TotalTokens = inp + outp
			}
			r.mu.Unlock()
		}

	case "end", "result", "complete", "finish":
		var ev struct {
			StopReason string `json:"stopReason,omitempty"`
			Status     string `json:"status,omitempty"`
			Error      string `json:"error,omitempty"`
			Spend      struct {
				Cost     float64 `json:"cost,omitempty"`
				Currency string  `json:"currency,omitempty"`
			} `json:"spend,omitempty"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens,omitempty"`
				CompletionTokens int `json:"completion_tokens,omitempty"`
				InputTokens      int `json:"input_tokens,omitempty"`
				OutputTokens     int `json:"output_tokens,omitempty"`
				TotalTokens      int `json:"total_tokens,omitempty"`
			} `json:"usage,omitempty"`
		}
		_ = json.Unmarshal(line, &ev)

		r.mu.Lock()
		inp := ev.Usage.InputTokens
		if inp == 0 {
			inp = ev.Usage.PromptTokens
		}
		outp := ev.Usage.OutputTokens
		if outp == 0 {
			outp = ev.Usage.CompletionTokens
		}
		if inp > 0 {
			r.usage.PromptTokens = inp
		}
		if outp > 0 {
			r.usage.CompletionTokens = outp
		}
		if ev.Usage.TotalTokens > 0 {
			r.usage.TotalTokens = ev.Usage.TotalTokens
		} else if r.usage.PromptTokens+r.usage.CompletionTokens > 0 {
			r.usage.TotalTokens = r.usage.PromptTokens + r.usage.CompletionTokens
		}
		usageCopy := r.usage
		r.mu.Unlock()

		if out != nil {
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
			if ev.Error != "" {
				resFrame.Error = ev.Error
			}
			out <- resFrame
		}

		// Only close immediately if in continuous wrapped pipe mode.
		// In subprocess mode, cmd.Wait() must finish first before closing the channel.
		if r.in != nil {
			r.mu.Lock()
			r.busy = false
			if r.frameCh != nil {
				close(r.frameCh)
				r.frameCh = nil
			}
			r.mu.Unlock()
		}
	}
}

// Turn starts a new turn by running Grok or writing to its Stdio input.
func (r *GrokRemote) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("grok: process closed")
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
		return nil, errors.New("grok: prompt is empty")
	}

	if t.Model != "" {
		r.mu.Lock()
		r.model = t.Model
		r.mu.Unlock()
	}
	if t.Effort != "" {
		r.mu.Lock()
		r.effort = t.Effort
		r.mu.Unlock()
	}

	// 1. Wrapped Stdio pipe mode (testing / continuous daemon)
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
			return nil, fmt.Errorf("grok: write prompt: %w", err)
		}
		return out, nil
	}

	// 2. Subprocess headless execution mode
	if r.grokBin == "" {
		r.finishTurn()
		return nil, errors.New("grok: no binary available to run turn")
	}

	args := []string{
		"-p", promptText,
		"--output-format", "streaming-json",
		"--always-approve",
	}
	r.mu.Lock()
	if !r.sessionStarted {
		args = append(args, "--session-id", r.sessionID)
		r.sessionStarted = true
	} else {
		args = append(args, "--resume", r.sessionID)
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}
	if r.effort != "" {
		args = append(args, "--reasoning-effort", r.effort)
	}
	cwd := r.cwd
	r.mu.Unlock()

	cmd := exec.CommandContext(ctx, r.grokBin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.finishTurn()
		return nil, fmt.Errorf("grok: stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		r.finishTurn()
		return nil, fmt.Errorf("grok: start process: %w", err)
	}

	r.mu.Lock()
	r.cmd = cmd
	r.mu.Unlock()

	go func() {
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 16*1024*1024)
		lineCount := 0
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			lineCount++
			r.handleLine(line)
		}
		err := cmd.Wait()
		if err != nil {
			errStr := strings.TrimSpace(stderrBuf.String())
			r.mu.Lock()
			outCh := r.frameCh
			r.mu.Unlock()
			if outCh != nil {
				outCh <- agentlink.Frame{
					Kind:  agentlink.KindResult,
					Error: fmt.Sprintf("grok error (%v): %s", err, errStr),
				}
			}
		}
		r.finishTurn()
	}()

	return out, nil
}

func (r *GrokRemote) finishTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.busy = false
	if r.frameCh != nil {
		close(r.frameCh)
		r.frameCh = nil
	}
}

// Interrupt interrupts the active turn if a process is running.
func (r *GrokRemote) Interrupt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.cmd.Process != nil && r.busy {
		_ = r.cmd.Process.Signal(os.Interrupt)
	}
}

// Close terminates the Grok process.
func (r *GrokRemote) Close() error {
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

// ListModels returns the available models for Grok CLI.
func (r *GrokRemote) ListModels(ctx context.Context) ([]agentturn.ModelInfo, error) {
	return []agentturn.ModelInfo{
		{
			ID:          "grok-4.7",
			DisplayName: "Grok 4.7 (default)",
			Description: "xAI frontier reasoning model (Grok Build default)",
			Tiers:       []string{"low", "medium", "high"},
			DefaultTier: "high",
		},
		{
			ID:          "grok-4.7-build-fast",
			DisplayName: "Grok 4.7 Build Fast",
			Description: "xAI fast-tier reasoning and coding model",
			Tiers:       []string{"low", "medium", "high"},
			DefaultTier: "medium",
		},
		{
			ID:          "grok-4.6",
			DisplayName: "Grok 4.6",
			Description: "xAI previous generation model",
		},
		{
			ID:          "grok-4.5",
			DisplayName: "Grok 4.5",
			Description: "xAI earlier generation model",
		},
	}, nil
}

// AvailableCommands returns the slash commands supported by Grok CLI.
func (r *GrokRemote) AvailableCommands() []agentturn.CommandInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.commands) > 0 {
		out := make([]agentturn.CommandInfo, 0, len(r.commands))
		for _, cmd := range r.commands {
			out = append(out, agentturn.CommandInfo{Name: cmd, Description: fmt.Sprintf("Grok command: /%s", cmd)})
		}
		return out
	}
	return []agentturn.CommandInfo{
		{Name: "compact", Description: "Compact chat context"},
		{Name: "always-approve", Description: "Toggle auto-approval of tool executions"},
		{Name: "context", Description: "Show context breakdown"},
		{Name: "plan", Description: "Show Grok's task planning breakdown"},
		{Name: "spend", Description: "View session token spend and cost metrics"},
	}
}

// EngineName identifies this engine as grok.
func (r *GrokRemote) EngineName() string {
	return "grok"
}

// Capabilities returns capabilities supported by Grok CLI.
func (r *GrokRemote) Capabilities() agentturn.Capability {
	return agentturn.CapModelSwitch | agentturn.CapReasoningTier | agentturn.CapContextInspect
}

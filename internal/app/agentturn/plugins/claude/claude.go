// Package claude provides an agentturn.Engine implementation for Anthropic Claude Code via stdio stream-json.
package claude

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

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func init() {
	agentturn.Register(agentturn.Driver{
		Name:         "claude",
		Aliases:      []string{"claude-code"},
		Description:  "Anthropic Claude Code via stdio stream-json",
		DefaultModel: "claude",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnClaudeRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model, opts.SessionID)
		},
	})
}

// Remote is an alias for ClaudeRemote.
type Remote = ClaudeRemote

// ClaudeRemote runs turns against an Anthropic Claude Code process over bidirectional Stdio stream-json.
type ClaudeRemote struct {
	in        io.Writer
	closer    io.Closer
	cmd       *exec.Cmd
	claudeBin string
	cwd       string
	model     string

	mu         sync.Mutex
	sessionID  string
	busy       bool
	closed     bool
	frameCh    chan agentlink.Frame
	doneCh     chan struct{}
	lastToolID string
	lastToolNm string
}

var _ agentturn.Engine = (*ClaudeRemote)(nil)
var _ agentturn.ModelLister = (*ClaudeRemote)(nil)
var _ agentturn.CommandLister = (*ClaudeRemote)(nil)
var _ agentturn.CapabilityProvider = (*ClaudeRemote)(nil)

// SpawnClaudeRemote spawns `claude --print --verbose --input-format stream-json --output-format stream-json`
// and returns an active ClaudeRemote engine.
func SpawnClaudeRemote(ctx context.Context, claudeBin, cwd, model, sessionID string) (*ClaudeRemote, error) {
	if claudeBin == "" {
		if p, err := exec.LookPath("claude"); err == nil {
			claudeBin = p
		} else {
			home, _ := os.UserHomeDir()
			candidates := []string{
				home + "/.local/bin/claude",
				"/opt/homebrew/bin/claude",
				"/usr/local/bin/claude",
			}
			for _, cand := range candidates {
				if _, err := os.Stat(cand); err == nil {
					claudeBin = cand
					break
				}
			}
		}
	}
	if claudeBin == "" {
		return nil, errors.New("claude binary not found on PATH or common locations (~/.local/bin, /opt/homebrew/bin, /usr/local/bin)")
	}

	args := []string{
		"--print",
		"--verbose",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--dangerously-skip-permissions",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if sessionID != "" {
		args = append(args, "--session-id", sessionID)
	}

	cmd := exec.Command(claudeBin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("claude: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("claude: start process: %w", err)
	}

	closer := &agentturn.ProcCloser{Cmd: cmd, Stdin: stdin}
	r, err := NewClaudeRemote(stdin, stdout, closer, cmd, sessionID)
	if err != nil {
		return nil, err
	}
	r.claudeBin = claudeBin
	r.cwd = cwd
	r.model = model
	return r, nil
}

// NewClaudeRemote wraps stream reader/writer pipes connected to Claude Code.
func NewClaudeRemote(in io.Writer, out io.Reader, closer io.Closer, cmd *exec.Cmd, sessionID string) (*ClaudeRemote, error) {
	r := &ClaudeRemote{
		in:        in,
		closer:    closer,
		cmd:       cmd,
		sessionID: sessionID,
		doneCh:    make(chan struct{}),
	}

	go r.readLoop(out)
	return r, nil
}

// SessionID returns the current Claude session ID.
func (r *ClaudeRemote) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

func (r *ClaudeRemote) readLoop(rd io.Reader) {
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

	r.mu.Lock()
	r.closed = true
	close(r.doneCh)
	if r.frameCh != nil {
		close(r.frameCh)
		r.frameCh = nil
	}
	r.mu.Unlock()
}

type claudeBaseMsg struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

func (r *ClaudeRemote) handleLine(line []byte) {
	var base claudeBaseMsg
	if err := json.Unmarshal(line, &base); err != nil {
		return
	}

	if base.SessionID != "" {
		r.mu.Lock()
		r.sessionID = base.SessionID
		r.mu.Unlock()
	}

	r.mu.Lock()
	out := r.frameCh
	r.mu.Unlock()

	switch base.Type {
	case "stream_event":
		if out == nil {
			return
		}
		var se struct {
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type     string `json:"type"`
					Text     string `json:"text,omitempty"`
					Thinking string `json:"thinking,omitempty"`
				} `json:"delta"`
			} `json:"event"`
		}
		if err := json.Unmarshal(line, &se); err == nil {
			delta := se.Event.Delta
			switch delta.Type {
			case "text_delta":
				if delta.Text != "" {
					out <- agentlink.Frame{Kind: agentlink.KindText, Text: delta.Text}
				}
			case "thinking_delta":
				if delta.Thinking != "" {
					out <- agentlink.Frame{Kind: agentlink.KindThought, Text: delta.Thinking}
				}
			}
		}

	case "assistant":
		if out == nil {
			return
		}
		var asMsg struct {
			Message struct {
				Content []struct {
					Type     string          `json:"type"`
					Text     string          `json:"text,omitempty"`
					Thinking string          `json:"thinking,omitempty"`
					ID       string          `json:"id,omitempty"`
					Name     string          `json:"name,omitempty"`
					Input    json.RawMessage `json:"input,omitempty"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &asMsg); err == nil {
			for _, c := range asMsg.Message.Content {
				switch c.Type {
				case "thinking":
					if c.Thinking != "" {
						out <- agentlink.Frame{Kind: agentlink.KindThought, Text: c.Thinking}
					}
				case "text":
					if c.Text != "" {
						out <- agentlink.Frame{Kind: agentlink.KindText, Text: c.Text}
					}
				case "tool_use":
					r.mu.Lock()
					r.lastToolID = c.ID
					r.lastToolNm = c.Name
					r.mu.Unlock()

					out <- agentlink.Frame{
						Kind: agentlink.KindTool,
						ID:   c.ID,
						Tool: &agentlink.Tool{
							Name:  c.Name,
							Phase: agentlink.ToolStart,
							Args:  string(c.Input),
						},
					}
				}
			}
		}

	case "user":
		// Tool results or user acknowledgments
		if out == nil {
			return
		}
		var uMsg struct {
			Message struct {
				Content []struct {
					Type      string `json:"type"`
					ToolUseID string `json:"tool_use_id,omitempty"`
					Content   string `json:"content,omitempty"`
					IsError   bool   `json:"is_error,omitempty"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &uMsg); err == nil {
			for _, c := range uMsg.Message.Content {
				if c.Type == "tool_result" {
					toolName := "tool"
					r.mu.Lock()
					if c.ToolUseID == r.lastToolID && r.lastToolNm != "" {
						toolName = r.lastToolNm
					}
					r.mu.Unlock()

					out <- agentlink.Frame{
						Kind: agentlink.KindTool,
						ID:   c.ToolUseID,
						Tool: &agentlink.Tool{
							Name:   toolName,
							Phase:  agentlink.ToolEnd,
							Result: c.Content,
						},
					}
				}
			}
		}

	case "result":
		var res struct {
			IsError bool   `json:"is_error"`
			Result  string `json:"result,omitempty"`
			Usage   struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			TerminalReason string `json:"terminal_reason,omitempty"`
		}
		_ = json.Unmarshal(line, &res)

		if out != nil {
			if res.Usage.InputTokens > 0 || res.Usage.OutputTokens > 0 {
				out <- agentlink.Frame{
					Kind: agentlink.KindResult,
					Usage: &agentlink.Usage{
						PromptTokens:     res.Usage.InputTokens,
						CompletionTokens: res.Usage.OutputTokens,
						TotalTokens:      res.Usage.InputTokens + res.Usage.OutputTokens,
					},
				}
			}
			if res.IsError && res.Result != "" {
				out <- agentlink.Frame{
					Kind:  agentlink.KindResult,
					Error: res.Result,
				}
			}
		}

		r.mu.Lock()
		r.busy = false
		if r.frameCh != nil {
			close(r.frameCh)
			r.frameCh = nil
		}
		r.mu.Unlock()
	}
}

// Turn starts a new turn by writing a user message to Claude's stdin and streaming events.
func (r *ClaudeRemote) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("claude: process closed")
	}
	if r.busy {
		r.mu.Unlock()
		return nil, errors.New("agentturn: a turn is already running")
	}

	// Model switch support via process restart on persistent session ID
	if t.Model != "" && r.model != "" && t.Model != r.model && r.sessionID != "" && r.claudeBin != "" {
		_ = r.closer.Close()
		r.model = t.Model
		args := []string{
			"-p",
			"--verbose",
			"--input-format=stream-json",
			"--output-format=stream-json",
			"--dangerously-skip-permissions",
			"--resume=" + r.sessionID,
			"--model=" + r.model,
		}
		cmd := exec.Command(r.claudeBin, args...)
		if r.cwd != "" {
			cmd.Dir = r.cwd
		}
		stdin, sErr := cmd.StdinPipe()
		stdout, oErr := cmd.StdoutPipe()
		if sErr == nil && oErr == nil && cmd.Start() == nil {
			r.cmd = cmd
			r.in = stdin
			r.closer = &agentturn.ProcCloser{Cmd: cmd, Stdin: stdin}
			r.doneCh = make(chan struct{})
			go r.readLoop(stdout)
		}
	}

	r.busy = true
	out := make(chan agentlink.Frame, 64)
	r.frameCh = out
	r.mu.Unlock()

	var promptText string
	for _, m := range t.History {
		if m.Role == "user" {
			promptText = m.Content
		}
	}

	type textContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type userPayload struct {
		Type    string `json:"type"`
		Message struct {
			Role    string        `json:"role"`
			Content []textContent `json:"content"`
		} `json:"message"`
	}

	payload := userPayload{
		Type: "user",
	}
	payload.Message.Role = "user"
	payload.Message.Content = []textContent{
		{Type: "text", Text: promptText},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		r.mu.Lock()
		r.busy = false
		close(out)
		r.frameCh = nil
		r.mu.Unlock()
		return nil, err
	}
	data = append(data, '\n')

	r.mu.Lock()
	_, err = r.in.Write(data)
	r.mu.Unlock()
	if err != nil {
		r.mu.Lock()
		r.busy = false
		close(out)
		r.frameCh = nil
		r.mu.Unlock()
		return nil, fmt.Errorf("claude: write prompt: %w", err)
	}

	// Handle turn cancellation / interruption
	go func() {
		select {
		case <-ctx.Done():
			r.Interrupt()
		case <-r.doneCh:
		}
	}()

	return out, nil
}

// Interrupt interrupts the currently running turn by sending SIGINT to the Claude child process.
func (r *ClaudeRemote) Interrupt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.cmd.Process != nil && r.busy {
		_ = r.cmd.Process.Signal(os.Interrupt)
	}
}

// Close terminates the Claude process.
func (r *ClaudeRemote) Close() error {
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

// ListModels returns the available models for Claude Code.
func (r *ClaudeRemote) ListModels(ctx context.Context) ([]agentturn.ModelInfo, error) {
	return []agentturn.ModelInfo{
		{
			ID:          "gemini-3.8-flash-high",
			DisplayName: "Gemini 3.8 Flash (High - Local Gateway)",
			Description: "Google Gemini 3.8 Flash through local OpenAGY proxy",
		},
		{
			ID:          "gemini-3.8-flash-medium",
			DisplayName: "Gemini 3.8 Flash (Medium - Local Gateway)",
		},
		{
			ID:          "claude-sonnet-4-6",
			DisplayName: "Claude Sonnet 4.6 (Thinking - Local Gateway)",
		},
		{
			ID:          "claude-opus-4-6-thinking",
			DisplayName: "Claude Opus 4.6 (Thinking - Local Gateway)",
		},
		{
			ID:          "gpt-oss-120b-medium",
			DisplayName: "GPT-OSS 120B (Medium - Local Gateway)",
		},
		{
			ID:          "sonnet",
			DisplayName: "Claude 3.7 Sonnet (Official)",
			Description: "Anthropic official Sonnet model",
		},
		{
			ID:          "opus",
			DisplayName: "Claude 3 Opus (Official)",
			Description: "Anthropic official Opus model",
		},
		{
			ID:          "haiku",
			DisplayName: "Claude 3.5 Haiku (Official)",
			Description: "Anthropic official Haiku model",
		},
	}, nil
}

// AvailableCommands returns the slash commands supported by Claude Code.
func (r *ClaudeRemote) AvailableCommands() []agentturn.CommandInfo {
	return []agentturn.CommandInfo{
		{Name: "compact", Description: "Compact conversation history to clear context"},
		{Name: "cost", Description: "Show token usage and estimated cost for this session"},
		{Name: "doctor", Description: "Healthcheck Claude Code environment"},
		{Name: "init", Description: "Initialize a CLAUDE.md project guide"},
		{Name: "review", Description: "Review git diff against base branch", InputHint: "[branch]"},
		{Name: "summary", Description: "Summarize recent work in this session"},
	}
}

// EngineName identifies this engine as claude.
func (r *ClaudeRemote) EngineName() string {
	return "claude"
}

// Capabilities returns capabilities supported by Claude Code.
func (r *ClaudeRemote) Capabilities() agentturn.Capability {
	return agentturn.CapModelSwitch | agentturn.CapContextInspect
}

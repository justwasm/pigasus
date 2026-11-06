// Package agy provides an agentturn.Engine implementation for Google Antigravity (agy)
// and Pigasus subprocesses over bidirectional stdio stream-json.
package agy

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
	"github.com/justwasm/pigasus/internal/features/models/catalog"
)

func init() {
	agentturn.Register(agentturn.Driver{
		Name:         "agy",
		Aliases:      []string{"antigravity"},
		Description:  "Google Antigravity CLI via stdio stream-json",
		DefaultModel: "agy",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnAgyRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model, opts.Effort, opts.SessionID)
		},
	})
	agentturn.Register(agentturn.Driver{
		Name:         "pigasus",
		Description:  "Pigasus agent via stdio stream-json",
		DefaultModel: "pigasus",
		Spawn: func(ctx context.Context, opts agentturn.SpawnOptions) (agentturn.Engine, error) {
			return SpawnPigasusRemote(ctx, opts.BinaryPath, opts.CWD, opts.Model, opts.Effort, opts.SessionID)
		},
	})
}

// Remote is an alias for AgyRemote.
type Remote = AgyRemote

// AgyRemote runs turns against a Google Antigravity (agy) CLI process over bidirectional Stdio stream-json.
type AgyRemote struct {
	in     io.Writer
	closer io.Closer
	cmd    *exec.Cmd

	engineName string
	agyBin     string
	cwd        string
	model      string
	effort     string

	mu             sync.Mutex
	conversationID string
	busy           bool
	closed         bool
	frameCh        chan agentlink.Frame
	doneCh         chan struct{}
}

var _ agentturn.Engine = (*AgyRemote)(nil)
var _ agentturn.ModelLister = (*AgyRemote)(nil)
var _ agentturn.CapabilityProvider = (*AgyRemote)(nil)

// SpawnAgyRemote spawns `agy --input-format=stream-json --output-format=stream-json`
// and returns an active AgyRemote engine.
func SpawnAgyRemote(ctx context.Context, agyBin, cwd, model, effort, conversationID string) (*AgyRemote, error) {
	if agyBin == "" {
		if p, err := exec.LookPath("agy"); err == nil {
			agyBin = p
		} else {
			home, _ := os.UserHomeDir()
			candidates := []string{
				home + "/.local/bin/agy",
				"/opt/homebrew/bin/agy",
				"/usr/local/bin/agy",
			}
			for _, cand := range candidates {
				if _, err := os.Stat(cand); err == nil {
					agyBin = cand
					break
				}
			}
		}
	}
	if agyBin == "" {
		return nil, errors.New("agy binary not found on PATH or common locations (~/.local/bin, /opt/homebrew/bin, /usr/local/bin)")
	}

	return spawnStreamJSONProcess(ctx, "agy", agyBin, cwd, model, effort, conversationID)
}

// SpawnPigasusRemote spawns `pigasus --input-format=stream-json --output-format=stream-json`
// and returns an active AgyRemote engine driving the pigasus child process.
func SpawnPigasusRemote(ctx context.Context, pigasusBin, cwd, model, effort, conversationID string) (*AgyRemote, error) {
	if pigasusBin == "" {
		if exe, err := os.Executable(); err == nil && exe != "" {
			pigasusBin = exe
		} else if p, err := exec.LookPath("pigasus"); err == nil {
			pigasusBin = p
		} else {
			home, _ := os.UserHomeDir()
			candidates := []string{
				home + "/go/bin/pigasus",
				"/opt/homebrew/bin/pigasus",
				"/usr/local/bin/pigasus",
			}
			for _, cand := range candidates {
				if _, err := os.Stat(cand); err == nil {
					pigasusBin = cand
					break
				}
			}
		}
	}
	if pigasusBin == "" {
		return nil, errors.New("pigasus binary not found (neither os.Executable nor on PATH)")
	}

	return spawnStreamJSONProcess(ctx, "pigasus", pigasusBin, cwd, model, effort, conversationID)
}

func spawnStreamJSONProcess(ctx context.Context, engineName, binPath, cwd, model, effort, conversationID string) (*AgyRemote, error) {
	args := []string{
		"--input-format=stream-json",
		"--output-format=stream-json",
		"--dangerously-skip-permissions",
	}
	if model != "" {
		args = append(args, "--model="+model)
	}
	if effort != "" {
		args = append(args, "--effort="+effort)
	}
	if conversationID != "" {
		args = append(args, "--conversation="+conversationID)
	}

	cmd := exec.Command(binPath, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: stdin pipe: %w", engineName, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("%s: stdout pipe: %w", engineName, err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("%s: start process: %w", engineName, err)
	}

	closer := &agentturn.ProcCloser{Cmd: cmd, Stdin: stdin}
	r, err := NewAgyRemote(stdin, stdout, closer, cmd, conversationID)
	if err != nil {
		return nil, err
	}
	r.agyBin = binPath
	r.cwd = cwd
	r.model = model
	r.effort = effort
	r.engineName = engineName
	return r, nil
}

// NewAgyRemote wraps stream reader/writer pipes connected to agy.
func NewAgyRemote(in io.Writer, out io.Reader, closer io.Closer, cmd *exec.Cmd, conversationID string) (*AgyRemote, error) {
	r := &AgyRemote{
		in:             in,
		closer:         closer,
		cmd:            cmd,
		conversationID: conversationID,
		doneCh:         make(chan struct{}),
	}

	go r.readLoop(out)
	return r, nil
}

// ConversationID returns the active agy conversation ID.
func (r *AgyRemote) ConversationID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conversationID
}

func (r *AgyRemote) readLoop(rd io.Reader) {
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

type agyStreamEvent struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id,omitempty"`
	Init           *struct {
		CWD            string   `json:"cwd"`
		Tools          []string `json:"tools"`
		PermissionMode string   `json:"permission_mode"`
	} `json:"init,omitempty"`
	StepUpdate *struct {
		ConversationID  string  `json:"conversation_id"`
		StepIndex       int     `json:"step_index"`
		State           string  `json:"state"`
		StepType        string  `json:"step_type"`
		ToolName        string  `json:"tool_name,omitempty"`
		TextDelta       string  `json:"text_delta,omitempty"`
		DurationSeconds float64 `json:"duration_seconds,omitempty"`
		ToolInfo        *struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters,omitempty"`
			Output     string          `json:"output,omitempty"`
			Error      *struct {
				Type    string `json:"type,omitempty"`
				Message string `json:"message,omitempty"`
			} `json:"error,omitempty"`
		} `json:"tool_info,omitempty"`
		Usage *struct {
			InputTokens    int `json:"input_tokens"`
			OutputTokens   int `json:"output_tokens"`
			ThinkingTokens int `json:"thinking_tokens"`
			TotalTokens    int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	} `json:"step_update,omitempty"`
	Result *struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error,omitempty"`
		Usage          struct {
			InputTokens    int `json:"input_tokens"`
			OutputTokens   int `json:"output_tokens"`
			ThinkingTokens int `json:"thinking_tokens"`
			TotalTokens    int `json:"total_tokens"`
		} `json:"usage"`
		FinishedReason string `json:"finished_reason,omitempty"`
	} `json:"result,omitempty"`
}

func (r *AgyRemote) handleLine(line []byte) {
	var ev agyStreamEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}

	if ev.ConversationID != "" {
		r.mu.Lock()
		r.conversationID = ev.ConversationID
		r.mu.Unlock()
	}

	r.mu.Lock()
	out := r.frameCh
	r.mu.Unlock()

	switch ev.Event {
	case "init":
		if ev.ConversationID != "" {
			r.mu.Lock()
			r.conversationID = ev.ConversationID
			r.mu.Unlock()
		}

	case "step_update":
		if out == nil || ev.StepUpdate == nil {
			return
		}
		su := ev.StepUpdate
		if su.ConversationID != "" {
			r.mu.Lock()
			r.conversationID = su.ConversationID
			r.mu.Unlock()
		}

		switch su.StepType {
		case "agent_response":
			if su.TextDelta != "" {
				out <- agentlink.Frame{Kind: agentlink.KindText, Text: su.TextDelta}
			}

		case "tool":
			toolName := su.ToolName
			if su.ToolInfo != nil && su.ToolInfo.Name != "" {
				toolName = su.ToolInfo.Name
			}
			switch su.State {
			case "ACTIVE":
				var argsStr string
				if su.ToolInfo != nil && len(su.ToolInfo.Parameters) > 0 {
					argsStr = string(su.ToolInfo.Parameters)
				}
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					Tool: &agentlink.Tool{
						Name:  toolName,
						Phase: agentlink.ToolStart,
						Args:  argsStr,
					},
				}
			case "DONE":
				var resultStr string
				if su.ToolInfo != nil {
					resultStr = su.ToolInfo.Output
					if su.ToolInfo.Error != nil && su.ToolInfo.Error.Message != "" {
						resultStr += "\nError: " + su.ToolInfo.Error.Message
					}
				}
				out <- agentlink.Frame{
					Kind: agentlink.KindTool,
					Tool: &agentlink.Tool{
						Name:   toolName,
						Phase:  agentlink.ToolEnd,
						Result: resultStr,
					},
				}
			}

		case "error_message":
			out <- agentlink.Frame{Kind: agentlink.KindNotice, Text: su.TextDelta}
		}

	case "result":
		if ev.Result != nil {
			if ev.Result.ConversationID != "" {
				r.mu.Lock()
				r.conversationID = ev.Result.ConversationID
				r.mu.Unlock()
			}
			if out != nil {
				res := ev.Result
				if res.Usage.InputTokens > 0 || res.Usage.OutputTokens > 0 {
					out <- agentlink.Frame{
						Kind: agentlink.KindResult,
						Usage: &agentlink.Usage{
							PromptTokens:     res.Usage.InputTokens,
							CompletionTokens: res.Usage.OutputTokens,
							TotalTokens:      res.Usage.TotalTokens,
						},
					}
				}
				if res.Status == "ERROR" && res.Error != "" {
					out <- agentlink.Frame{
						Kind:  agentlink.KindResult,
						Error: res.Error,
					}
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

// Turn starts a new turn by writing a user message to agy's stdin and streaming events.
func (r *AgyRemote) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("agy: process closed")
	}
	if r.busy {
		r.mu.Unlock()
		return nil, errors.New("agentturn: a turn is already running")
	}

	// Model switch support via process restart on persistent conversation ID
	if t.Model != "" && r.model != "" && t.Model != r.model && r.conversationID != "" && r.agyBin != "" {
		_ = r.closer.Close()
		r.model = t.Model
		args := []string{
			"--input-format=stream-json",
			"--output-format=stream-json",
			"--dangerously-skip-permissions",
			"--conversation=" + r.conversationID,
			"--model=" + r.model,
		}
		if r.effort != "" {
			args = append(args, "--effort="+r.effort)
		}
		cmd := exec.Command(r.agyBin, args...)
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

	payload := map[string]any{
		"event": "user",
		"message": map[string]any{
			"content": promptText,
		},
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
		return nil, fmt.Errorf("agy: write prompt: %w", err)
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

// Interrupt interrupts the currently running turn by sending SIGINT to the agy child process.
func (r *AgyRemote) Interrupt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.cmd.Process != nil && r.busy {
		_ = r.cmd.Process.Signal(os.Interrupt)
	}
}

// Close terminates the agy process.
func (r *AgyRemote) Close() error {
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

// ListModels returns the available models for Google agy.
func (r *AgyRemote) ListModels(ctx context.Context) ([]agentturn.ModelInfo, error) {
	list := []agentturn.ModelInfo{
		{
			ID:          "gemini-3.8-flash",
			DisplayName: "Gemini 3.8 Flash",
			Description: "Google multimodal model with reasoning tiers",
			Tiers:       []string{"low", "medium", "high"},
			DefaultTier: "high",
		},
		{
			ID:          "claude-sonnet-4-6",
			DisplayName: "Claude Sonnet 4.6 (Thinking)",
			Description: "Anthropic Claude Sonnet with extended thinking",
		},
		{
			ID:          "claude-opus-4-6-thinking",
			DisplayName: "Claude Opus 4.6 (Thinking)",
			Description: "Anthropic Claude Opus with extended thinking",
		},
		{
			ID:          "gpt-oss-120b-medium",
			DisplayName: "GPT-OSS 120B (Medium)",
			Description: "Open-weight model with medium reasoning effort",
		},
	}
	if catalog.Claude55Available() {
		list = append(list,
			agentturn.ModelInfo{
				ID:          "claude-opus-5-5",
				DisplayName: "Claude Opus 5.5",
				Description: "Anthropic Claude Opus 5.5 (ADC gated)",
			},
			agentturn.ModelInfo{
				ID:          "claude-sonnet-5-5",
				DisplayName: "Claude Sonnet 5.5",
				Description: "Anthropic Claude Sonnet 5.5 (ADC gated)",
			},
		)
	}
	for _, m := range catalog.ConfiguredModelEntries() {
		list = append(list, agentturn.ModelInfo{
			ID:          m.ID,
			DisplayName: m.Label,
			Tiers:       m.Tiers,
			DefaultTier: m.DefaultTier,
		})
	}
	return list, nil
}

// EngineName identifies this engine as agy or pigasus.
func (r *AgyRemote) EngineName() string {
	if r.engineName != "" {
		return r.engineName
	}
	return "agy"
}

// Capabilities returns all capabilities supported by reference agy.
func (r *AgyRemote) Capabilities() agentturn.Capability {
	return agentturn.CapAll
}

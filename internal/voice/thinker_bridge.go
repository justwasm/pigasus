package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/actor"
)

// DefaultNoteInterval is the default minimum delay between non-critical progress notes.
const DefaultNoteInterval = 2 * time.Second

// NoteThrottler prevents rapid-fire tool events from overwhelming the voice talker.
type NoteThrottler struct {
	mu           sync.Mutex
	minInterval  time.Duration
	lastNoteTime time.Time
	lastNote     string
}

// NewNoteThrottler creates a NoteThrottler with the given minimum interval.
func NewNoteThrottler(minInterval time.Duration) *NoteThrottler {
	if minInterval <= 0 {
		minInterval = DefaultNoteInterval
	}
	return &NoteThrottler{
		minInterval: minInterval,
	}
}

// ShouldSend reports whether a progress note should be emitted.
// If force is true (e.g. error, cancellation, milestone), throttling is bypassed.
func (t *NoteThrottler) ShouldSend(now time.Time, note string, force bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if force {
		t.lastNoteTime = now
		t.lastNote = note
		return true
	}
	if note == t.lastNote {
		return false
	}
	if !t.lastNoteTime.IsZero() && now.Sub(t.lastNoteTime) < t.minInterval {
		return false
	}
	t.lastNoteTime = now
	t.lastNote = note
	return true
}

// HumanizeTool converts a tool name and raw input JSON into a concise, canonical English semantic description.
// The upstream multimodal live model (e.g. Gemini 3.8 Live) dynamically translates and verbalizes this
// into whatever language the user is currently conversing in (Chinese, French, Japanese, Spanish, etc.).
func HumanizeTool(name string, input string) string {
	if strings.TrimSpace(name) == "" {
		return "Processing background task"
	}

	var args map[string]any
	if strings.TrimSpace(input) != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}

	getString := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}

	switch strings.ToLower(name) {
	case "read_file":
		path := getString("path", "Path", "file_path", "file")
		if path != "" {
			return fmt.Sprintf("Reading file %s", filepath.Base(path))
		}
		return "Reading code file"
	case "edit_file", "write_file":
		path := getString("path", "Path", "file_path", "file")
		if path != "" {
			return fmt.Sprintf("Editing file %s", filepath.Base(path))
		}
		return "Updating code file"
	case "run_command", "bash", "sh", "terminal":
		cmd := getString("CommandLine", "command", "cmd")
		if cmd != "" {
			trimmed := strings.TrimSpace(cmd)
			switch {
			case strings.HasPrefix(trimmed, "go test"):
				return "Running unit tests"
			case strings.HasPrefix(trimmed, "go build") || strings.HasPrefix(trimmed, "make build"):
				return "Building project"
			case strings.HasPrefix(trimmed, "git diff"):
				return "Checking git diff"
			case strings.HasPrefix(trimmed, "git status"):
				return "Checking git status"
			case strings.HasPrefix(trimmed, "git"):
				return "Executing git operation"
			case strings.HasPrefix(trimmed, "gh"):
				return "Checking GitHub repository"
			case strings.HasPrefix(trimmed, "grep") || strings.HasPrefix(trimmed, "rg"):
				return "Searching codebase"
			default:
				if len(trimmed) > 40 {
					trimmed = trimmed[:37] + "..."
				}
				return fmt.Sprintf("Running command: %s", trimmed)
			}
		}
		return "Executing terminal command"
	case "search_web", "web_search", "google_search":
		q := getString("query", "q")
		if q != "" {
			if len(q) > 30 {
				q = q[:27] + "..."
			}
			return fmt.Sprintf("Searching web for: %s", q)
		}
		return "Searching the web"
	case "list_dir", "ls":
		return "Listing directory contents"
	case "invoke_subagent", "subagent":
		return "Dispatching specialized subagent"
	default:
		return fmt.Sprintf("Calling tool: %s", name)
	}
}

// HumanizeToolCall converts a raw actor.ToolCall into a concise, canonical English semantic description.
func HumanizeToolCall(call *actor.ToolCall) string {
	if call == nil {
		return "Processing background task"
	}
	return HumanizeTool(call.Name, call.Input)
}

// ThinkerBridgeOption configures a ThinkerBridge.
type ThinkerBridgeOption func(*ThinkerBridge)

// WithNoteInterval sets the throttle interval between consecutive progress notes.
func WithNoteInterval(interval time.Duration) ThinkerBridgeOption {
	return func(b *ThinkerBridge) {
		b.throttler = NewNoteThrottler(interval)
	}
}

// ThinkerBridge delegates complex reasoning or autonomous execution tasks triggered
// by the real-time Talker (via `call_thinker_agent` or `delegate_to_thinker`) to the
// background Actor loop (Thinker). It pipes progress notes back to the live session
// so the Talker can speak progress updates, and returns the final answer upon completion.
type ThinkerBridge struct {
	mu           sync.Mutex
	act          *actor.Actor
	activeTurn   *actor.TurnHandle
	activeCancel context.CancelFunc
	running      atomic.Bool
	throttler    *NoteThrottler
}

// NewThinkerBridge creates a ThinkerBridge backed by an Actor instance.
func NewThinkerBridge(act *actor.Actor, opts ...ThinkerBridgeOption) *ThinkerBridge {
	b := &ThinkerBridge{
		act:       act,
		throttler: NewNoteThrottler(DefaultNoteInterval),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// SetActor updates the background actor instance used for execution.
func (b *ThinkerBridge) SetActor(act *actor.Actor) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.act = act
}

// IsRunning reports whether a thinker task is currently executing.
func (b *ThinkerBridge) IsRunning() bool {
	return b.running.Load()
}

// CancelActive cancels the currently executing Thinker task, if any.
func (b *ThinkerBridge) CancelActive() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running.Load() {
		return nil
	}
	if b.activeCancel != nil {
		b.activeCancel()
	}
	if b.activeTurn != nil {
		_ = b.activeTurn.Cancel("cancelled by voice talker")
	}
	b.running.Store(false)
	return nil
}

// Dispatch executes the delegated query on the Actor, streaming progress notes and
// the final answer to the provided LiveSession. It runs asynchronously in the background
// so it does not block the real-time audio pipeline.
func (b *ThinkerBridge) Dispatch(
	ctx context.Context,
	sess LiveSession,
	invocationID string,
	query string,
) error {
	b.mu.Lock()
	if b.act == nil {
		b.mu.Unlock()
		return errors.New("thinker bridge: actor is nil")
	}

	// Cancel any prior active task
	if b.running.Load() {
		if b.activeCancel != nil {
			b.activeCancel()
		}
		if b.activeTurn != nil {
			_ = b.activeTurn.Cancel("superseded by new thinker handoff")
		}
	}

	turnCtx, cancel := context.WithCancel(ctx)
	b.activeCancel = cancel

	handle, err := b.act.Prompt(turnCtx, query)
	if err != nil {
		b.mu.Unlock()
		cancel()
		return fmt.Errorf("thinker bridge: prompt actor: %w", err)
	}

	b.activeTurn = handle
	b.running.Store(true)
	b.mu.Unlock()

	// Execute event observation in background goroutine
	go func() {
		defer func() {
			b.mu.Lock()
			if b.activeTurn == handle {
				b.running.Store(false)
				b.activeTurn = nil
				b.activeCancel = nil
			}
			b.mu.Unlock()
			cancel()
		}()

		var answerBuilder strings.Builder
		toolCount := 0

		for ev := range handle.Events() {
			switch ev.Type {
			case actor.EventDelta:
				answerBuilder.WriteString(ev.Delta)
			case actor.EventToolCallStarted:
				toolCount++
				if sess != nil && ev.ToolCall != nil {
					note := HumanizeTool(ev.ToolCall.Name, ev.ToolCall.Input)
					if b.throttler == nil || b.throttler.ShouldSend(time.Now(), note, false) {
						_ = sess.SendThinkerNote(note)
					}
				}
			case actor.EventCheckpoint:
				if sess != nil {
					note := "Compacting task context memory"
					if b.throttler == nil || b.throttler.ShouldSend(time.Now(), note, true) {
						_ = sess.SendThinkerNote(note)
					}
				}
			}
		}

		if turnCtx.Err() != nil {
			if sess != nil {
				_ = sess.SendThinkerNote("Task was cancelled by voice.")
				_ = sess.SendToolResponse(invocationID, "call_thinker_agent", map[string]any{
					"error": "task was cancelled",
				})
			}
			return
		}

		if err := handle.Err(); err != nil {
			if sess != nil {
				_ = sess.SendThinkerNote(fmt.Sprintf("Task encountered an error: %v", err))
				_ = sess.SendToolResponse(invocationID, "call_thinker_agent", map[string]any{
					"error": err.Error(),
				})
			}
			return
		}

		answer := strings.TrimSpace(answerBuilder.String())
		if answer == "" {
			answer = "Task completed successfully."
		}

		if sess != nil {
			_ = sess.SendThinkerAnswer(answer)
			_ = sess.SendToolResponse(invocationID, "call_thinker_agent", map[string]any{
				"answer": answer,
			})
		}
	}()

	return nil
}

package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/justwasm/pigasus/actor"
)

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
}

// NewThinkerBridge creates a ThinkerBridge backed by an Actor instance.
func NewThinkerBridge(act *actor.Actor) *ThinkerBridge {
	return &ThinkerBridge{
		act: act,
	}
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
					_ = sess.SendThinkerNote(fmt.Sprintf("Running tool: %s", ev.ToolCall.Name))
				}
			case actor.EventCheckpoint:
				if sess != nil {
					_ = sess.SendThinkerNote("Compacting task context checkpoint")
				}
			}
		}

		if turnCtx.Err() != nil {
			if sess != nil {
				_ = sess.SendThinkerNote("Task was cancelled.")
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

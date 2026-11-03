package backend

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// Sink is the recovered printmode emitter abstraction. The three
// concrete emitters in 1.3.5 (textEmitter / jsonEmitter /
// streamJSONEmitter) all implement the same 4-method contract:
//
//	EmitInit(ctx, model string) error
//	BeginTurn(turnID int64) error
//	Emit(text string) error
//	EmitFinal() error
//	EmitError(err error) error
//
// The default impl is textEmitter (writes to w directly, the way
// `antigravity --print` does). jsonEmitter / streamJSONEmitter live in
// the cmd/wagyu package because their wire shape depends on the
// output destination (stdout vs HTTP).
type Sink interface {
	// EmitInit signals the start of a new conversation. Mirrors
	// `(*textEmitter).EmitInit` at 0x8d063e0.
	EmitInit(ctx context.Context, model string) error
	// BeginTurn signals the start of a new turn. Mirrors
	// `(*textEmitter).BeginTurn` at 0x8d06400.
	BeginTurn(turnID int64) error
	// Emit forwards one streamed chunk. The recovered code path calls
	// this on every StreamChunk.Text (cycle 33+40 mock path, and the
	// gateway SSE parser).
	Emit(text string) error
	// EmitFinal signals the end of a successful turn. Mirrors
	// `(*textEmitter).EmitFinal` at 0x8d06600.
	EmitFinal() error
	// EmitError signals an error. Mirrors `(*textEmitter).EmitError`
	// at 0x8d06560. May be called from any state.
	EmitError(err error) error
}

// textSink is the default Sink that mirrors the decompiled
// (*textEmitter) at 0x8d063e0..0x8d06600. It writes a banner on
// EmitInit, a turn-marker on BeginTurn, the text directly on Emit, a
// newline on EmitFinal, and a `[error] ...` line on EmitError.
//
// This is what the `cmd/wagyu --print` path needs; the JSON
// variant lives in cmd/wagyu (out of scope for this file).
type textSink struct {
	w  io.Writer
	mu sync.Mutex // writer is concurrent-safe per Chunk callback
}

// NewTextSink returns a Sink that writes to w. Returns a usable
// Sink even if w is nil (writes go to /dev/null in that case so the
// caller can still drive the lifecycle in tests).
func NewTextSink(w io.Writer) Sink {
	if w == nil {
		w = io.Discard
	}
	return &textSink{w: w}
}

// EmitInit prints a one-line banner identifying the model. The shape
// matches what `antigravity --print` prints today; the cycle 41
// refactor just adds it as a lifecycle event instead of a special
// case in runPrintMode.
func (t *textSink) EmitInit(_ context.Context, model string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := fmt.Fprintf(t.w, "[init] model=%s\n", model)
	return err
}

// BeginTurn prints the turn number on its own line. The 1.3.5 binary
// uses this to drive the TUI's `step=5/5 turn=N` counter; the cycle 41
// textSink just echoes it for the `--print` path.
func (t *textSink) BeginTurn(turnID int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := fmt.Fprintf(t.w, "[turn %d] ", turnID)
	return err
}

// Emit writes the chunk text directly to the writer.
func (t *textSink) Emit(text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := io.WriteString(t.w, text)
	return err
}

// EmitFinal writes a trailing newline and a separator so the next
// turn's [turn N] is on its own line.
func (t *textSink) EmitFinal() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := io.WriteString(t.w, "\n")
	return err
}

// EmitError writes `[error] ...` followed by a newline. Mirrors the
// printmode emitter's error framing.
func (t *textSink) EmitError(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err == nil {
		return nil
	}
	_, werr := fmt.Fprintf(t.w, "[error] %v\n", err)
	return werr
}

// Compile-time guarantee textSink implements Sink.
var _ Sink = (*textSink)(nil)

// Emit implements Backend.Emit. Useful for callers that want to push
// banner text or error messages through the same sink without going
// through the full Run path. After the first successful Emit the
// state machine is in LStateStream.
func (l *Lifecycle) Emit(text string) error {
	if text == "" {
		return nil
	}
	if err := l.sink.Emit(text); err != nil {
		l.transition(fmt.Sprintf("sink.Emit failed: %s", err), LStateError)
		return err
	}
	l.mu.Lock()
	l.appendTranscriptLocked(text)
	l.mu.Unlock()
	l.transition("explicit Emit call", LStateStream)
	return nil
}

func (l *Lifecycle) handleStreamChunk(chunk StreamChunk) (bool, error) {
	if chunk.Error != nil {
		l.transition(fmt.Sprintf("chunk error: %s", chunk.Error), LStateError)
		_ = l.sink.EmitError(chunk.Error)
		return false, chunk.Error
	}
	if chunk.Text != "" {
		l.transition(fmt.Sprintf("chunk %d bytes", len(chunk.Text)), LStateStream)
		if err := l.sink.Emit(chunk.Text); err != nil {
			l.transition(fmt.Sprintf("sink.Emit failed: %s", err), LStateError)
			return false, fmt.Errorf("backend.Lifecycle: sink.Emit: %w", err)
		}
		l.appendTranscriptLocked(chunk.Text)
	}
	if chunk.Done {
		// Cycle 45 (Issue #7 fix) — capture terminal
		// chunk usage into the Lifecycle's UsageTracker.
		// The v1.3.5 binary populates chunk.Meta on
		// the terminal frame (the OpenAI usage block
		// is mapped 1:1 by client.go); the recovered
		// gateway SSE parser does the same. Add
		// updates the running totals the UsageBattle
		// panel reads via Lifecycle.Usage().
		if chunk.Meta != nil && l.usage != nil {
			l.usage.Add(
				chunk.Meta.PromptTokens,
				chunk.Meta.CompletionTokens,
				chunk.Meta.TotalTokens,
			)
		}
		l.transition("received done marker", LStateSave)
		_ = l.sink.EmitFinal()
		l.transition("turn complete", LStateDone)
		return true, nil
	}
	return false, nil
}

func (l *Lifecycle) consumeStream(ctx context.Context, stream <-chan StreamChunk) error {
	for {
		select {
		case <-ctx.Done():
			l.transition(fmt.Sprintf("ctx canceled: %s", ctx.Err()), LStateError)
			_ = l.sink.EmitError(ctx.Err())
			return ctx.Err()
		case chunk, ok := <-stream:
			if !ok {
				// Channel closed without a Done marker; synthesize one.
				l.transition("channel closed without done marker", LStateSave)
				_ = l.sink.EmitFinal()
				l.transition("turn complete (synthesized done)", LStateDone)
				return nil
			}
			done, err := l.handleStreamChunk(chunk)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

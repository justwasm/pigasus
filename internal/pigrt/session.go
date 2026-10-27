package pigrt

import (
	"context"
	"errors"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/justwasm/pigasus/retrybudget"
)

// Session exposes the underlying PiG session for callers that need more than
// Run (commands, steering, model swaps).
func (r *Runtime) Session() *coding.Session { return r.session }

// AbortRetry cancels the in-flight auto-retry backoff timer without
// cancelling the session itself.
func (r *Runtime) AbortRetry() {
	if r == nil || r.session == nil {
		return
	}
	r.session.AbortRetry()
}

// RetryBudget returns the per-turn retry budget (or nil when the cap is disabled).
func (r *Runtime) RetryBudget() *retrybudget.Budget { return r.retryBudget }

// Run sends one prompt and returns the messages the turn produced.
func (r *Runtime) Run(ctx context.Context, prompt string, onText func(string)) ([]agent.AgentMessage, error) {
	return r.RunWith(ctx, prompt, onText, nil)
}

// setupTurnObservers attaches the text and event callbacks for a turn and returns a teardown function.
func (r *Runtime) setupTurnObservers(onText func(string), onEvent EventFunc) func() {
	if r.retryBudget != nil {
		r.retryBudget.Reset()
	}
	chained := onEvent
	if r.retryBudget != nil {
		chained = chainEventFuncs(onEvent, retrybudget.Observer(r.retryBudget, r))
	}
	r.mu.Lock()
	r.onText, r.onEvent = onText, chained
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.onText, r.onEvent = nil, nil
		r.mu.Unlock()
	}
}

// classifyTurnResult recovers typed provider errors or folded message failures from turn messages.
func (r *Runtime) classifyTurnResult(msgs []agent.AgentMessage, err error) ([]agent.AgentMessage, error) {
	if failure := turnFailure(msgs); failure != nil {
		if r.opts.ErrorSlot != nil {
			if typed := r.opts.ErrorSlot.Err(); typed != nil {
				return msgs, typed
			}
		}
		return msgs, failure
	}
	if err != nil && r.opts.ErrorSlot != nil {
		if typed := r.opts.ErrorSlot.Err(); typed != nil {
			return msgs, typed
		}
	}
	return msgs, err
}

// RunWith is Run with an event observer attached for the duration of the turn.
func (r *Runtime) RunWith(ctx context.Context, prompt string, onText func(string), onEvent EventFunc) ([]agent.AgentMessage, error) {
	return r.runTurn(ctx, func(ctx context.Context) ([]agent.AgentMessage, error) {
		return r.session.Send(ctx, prompt)
	}, onText, onEvent)
}

// RunWithContent is RunWith for a turn whose user message carries
// images.
//
// The content blocks are the user message's: a leading text block, then
// one block per image — the shape PiG's own BuildUserContent builds
// (coding/session.go). A turn with no image goes through the plain
// Send path, so the text-only behaviour is byte for byte the one it
// had before.
func (r *Runtime) RunWithContent(ctx context.Context, content []ai.UserContentBlock, onText func(string), onEvent EventFunc) ([]agent.AgentMessage, error) {
	return r.runTurn(ctx, func(ctx context.Context) ([]agent.AgentMessage, error) {
		return r.session.SendContent(ctx, content)
	}, onText, onEvent)
}

// runTurn is the turn body both RunWith and RunWithContent share.
func (r *Runtime) runTurn(ctx context.Context, send func(context.Context) ([]agent.AgentMessage, error), onText func(string), onEvent EventFunc) ([]agent.AgentMessage, error) {
	cleanup := r.setupTurnObservers(onText, onEvent)
	defer cleanup()

	msgs, err := send(ctx)
	r.flushEvents(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return msgs, ctxErr
	}
	if r.opts.AutoCont {
		recovered, rcErr := r.maybeAutoContinue(ctx, msgs, err, 0)
		if rcErr != nil {
			return recovered, rcErr
		}
		msgs, err = recovered, nil
	}
	return r.classifyTurnResult(msgs, err)
}

// eventFlushTimeout bounds the end-of-turn event barrier.
const eventFlushTimeout = 5 * time.Second

// flushEvents waits until every event of the turn has been forwarded.
//
// PiG's barrier is the only thing that says "the drain goroutine is
// done with this turn". The turn's own context cannot carry it: a
// cancelled turn skips the barrier, and the drain goroutine — which
// outlives the turn — is then still forwarding events when the caller
// tears the event channel down. That late write panicked with "send
// on closed channel" and, running in the daemon's tunnel goroutine,
// took the whole process down.
//
// A context that is already dead gets a fresh one, bounded by a
// timeout, so the barrier is still enqueued.
func (r *Runtime) flushEvents(ctx context.Context) {
	if r.session == nil {
		return
	}
	flushCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		var fresh context.Context
		fresh, cancel = context.WithTimeout(context.WithoutCancel(ctx), eventFlushTimeout)
		flushCtx = fresh
	}
	defer cancel()
	_ = r.session.FlushEvents(flushCtx)
}

// turnFailure reports the failure PiG encoded in the turn's messages: the last
// assistant message ending in error or abort.
func turnFailure(msgs []agent.AgentMessage) error {
	for i := len(msgs) - 1; i >= 0; i-- {
		message := msgs[i].Assistant
		if message == nil {
			continue
		}
		switch message.StopReason {
		case ai.StopReasonError, ai.StopReasonAborted:
			text := message.ErrorMessage
			if text == "" {
				text = "agent: upstream stream failed"
			}
			return errors.New(text)
		default:
			return nil
		}
	}
	return nil
}

// UsageOf sums the token usage of the assistant messages a turn produced.
func UsageOf(msgs []agent.AgentMessage) (ai.Usage, bool) {
	var total ai.Usage
	found := false
	for _, m := range msgs {
		if m.Assistant == nil || m.Assistant.Usage == nil {
			continue
		}
		found = true
		u := *m.Assistant.Usage
		total.Input += u.Input
		total.Output += u.Output
		total.CacheRead += u.CacheRead
		total.CacheWrite += u.CacheWrite
		total.TotalTokens += u.TotalTokens
	}
	return total, found
}

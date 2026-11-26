package streampipe

// pipeline_resume.go — resuming a retried stream without replaying what the
// caller already saw.
//
// Context. AGY 1.5.17 changelog:
//
//	"Fixed long, multi-part streaming model responses discarding their
//	 progress and restarting or failing the turn when a continuation request
//	 hit a temporary model capacity (`503` / `MODEL_CAPACITY_EXHAUSTED`)
//	 error; the CLI now retries and resumes from where the response left
//	 off."
//
// The provider offers no continuation offset: a retry is a fresh request
// carrying the same body, and the model streams its answer from the first
// token again. "Resumes from where the response left off" therefore cannot
// mean "ask the server for byte N onward". What it can mean, and what this
// file implements, is that the *caller* neither loses nor repeats what it has
// already been handed: Run keeps a record of the assistant text it has
// delivered for this turn and, on a retry, swallows the replayed prefix so
// every token reaches the caller exactly once.
//
// Two cases worth stating up front:
//
//   - A reader may emit its own terminal chunk for the attempt that just failed
//     (cloudcode's finishScan and the gateway's SSE readers both do, for
//     non-connection-drop failures). A terminal chunk handed to the caller for
//     a stream that then resumes is exactly the "discarding their progress"
//     this fix removes, so attemptTap holds terminal chunks — and everything
//     after them — until Run knows whether the attempt is final.
//   - The retry may produce *different* text than the failed attempt did. That
//     is a genuine ambiguity with no correct answer. The choice here is to end
//     suppression at the first chunk that stops matching and deliver that chunk
//     in full, letting every later chunk straight through. Rationale: silently
//     dropping model output hides an entire answer behind a stream that looks
//     complete, which is worse than a visibly repeated fragment at the seam.
//     "Hello " followed by a retry of "Hi there" gives the caller
//     "Hello Hi there"; trimming the mismatch would instead swallow the
//     differing byte(s) at the seam and could drop output the user never learns
//     exists.

import (
	"context"
	"errors"
	"net/http"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// tapMode is the per-attempt forwarding mode of an attemptTap.
type tapMode int

const (
	// tapPassthrough hands every chunk to the caller unchanged. This is the
	// first attempt, and any attempt that follows a turn where nothing has been
	// delivered yet (there is nothing to suppress).
	tapPassthrough tapMode = iota
	// tapSuppress swallows text that repeats the already-delivered prefix.
	tapSuppress
)

// attemptTap is the seam between Pipeline.Run and the caller's channel.
//
// Run gives ReadBody this tap's input channel and forwards every chunk to the
// caller through the methods below, so the pipeline — not the dialect reader —
// owns what the caller sees. All methods are called from the goroutine running
// Run; nothing here is safe for concurrent use.
type attemptTap struct {
	out chan<- backend.StreamChunk

	// delivered is every byte of assistant text written to out so far in this
	// Run, in order. It is the prefix a retry must not repeat.
	delivered []byte

	mode tapMode
	// matched is how many bytes of delivered the current (suppressing) attempt
	// has replayed. Comparing per chunk keeps the check O(chunk) rather than
	// O(delivered) for a response of several megabytes.
	matched int
	// deferred holds the non-text payload (tool calls, thinking blocks, meta)
	// of chunks whose text was swallowed, so it can be emitted in the position
	// it originally occupied once suppression resolves.
	deferred []backend.StreamChunk
	// held holds a terminal chunk — Done or Error — and everything after it,
	// until the attempt's fate is known.
	held    []backend.StreamChunk
	holding bool
}

func newAttemptTap(out chan<- backend.StreamChunk) *attemptTap {
	return &attemptTap{out: out}
}

// arm puts the tap in the mode the next attempt needs: suppression when earlier
// attempts already handed the caller some text, passthrough otherwise.
func (t *attemptTap) arm() {
	t.held = nil
	t.deferred = nil
	t.holding = false
	t.matched = 0
	if len(t.delivered) == 0 {
		t.mode = tapPassthrough
		return
	}
	t.mode = tapSuppress
}

// discard throws away everything the failed attempt produced that the caller
// has not seen. Called when Run decides to retry: the next attempt re-produces
// the whole answer, so nothing queued here is needed.
func (t *attemptTap) discard() {
	t.held = nil
	t.deferred = nil
	t.holding = false
}

// accept forwards one chunk of the current attempt. It reports whether the
// caller is still accepting chunks.
func (t *attemptTap) accept(ctx context.Context, c backend.StreamChunk) bool {
	if t.holding {
		t.held = append(t.held, c)
		return true
	}
	if c.Error != nil || c.Done {
		t.holding = true
		t.held = append(t.held, c)
		return true
	}
	if t.mode == tapPassthrough {
		return t.write(ctx, c)
	}
	return t.suppress(ctx, c)
}

// suppress compares one chunk of a retry against the already-delivered prefix.
func (t *attemptTap) suppress(ctx context.Context, c backend.StreamChunk) bool {
	text := c.Text

	// How much of this chunk the delivered prefix can possibly cover.
	cover := len(t.delivered) - t.matched
	if cover < 0 {
		cover = 0
	}
	n := len(text)
	if n > cover {
		n = cover
	}
	i := 0
	for ; i < n; i++ {
		if t.delivered[t.matched+i] != text[i] {
			break
		}
	}
	t.matched += i

	switch {
	case i == n && n < len(text):
		// The whole delivered prefix matched and this chunk continues past it:
		// the clean continuation this fix exists for. Deliver the tail.
		return t.resume(ctx, c, text[n:])
	case i < n:
		// Divergence at byte i. Suppression ends here and this chunk is
		// delivered in full: the caller may see a repeated leading fragment at
		// the seam, but no model output is ever dropped (see the file comment).
		return t.resume(ctx, c, text)
	}

	// Still inside the delivered prefix: swallow the text, keep any structural
	// payload for the flush that happens when suppression resolves.
	if c.Text = ""; len(c.ToolCalls) > 0 || len(c.ThinkingBlocks) > 0 || c.Meta != nil {
		t.deferred = append(t.deferred, c)
	}
	return true
}

// resume ends suppression and delivers fresh: first the structural payload of
// the earlier chunks whose text was swallowed, then the new text itself.
func (t *attemptTap) resume(ctx context.Context, c backend.StreamChunk, fresh string) bool {
	t.mode = tapPassthrough
	t.matched = 0
	if !t.flushDeferred(ctx) {
		return false
	}
	c.Text = fresh
	return t.write(ctx, c)
}

func (t *attemptTap) flushDeferred(ctx context.Context) bool {
	rest := t.deferred
	t.deferred = nil
	for _, c := range rest {
		if !t.write(ctx, c) {
			return false
		}
	}
	return true
}

// finish flushes what the current attempt produced and the caller has not seen
// yet — the deferred structural chunks, then the held terminal chunks in order
// — and reports whether a terminal chunk was among them, so Run can avoid
// reporting the same ending twice.
func (t *attemptTap) finish(ctx context.Context) (wroteTerminal bool) {
	if !t.flushDeferred(ctx) {
		t.held = nil
		t.holding = false
		return true
	}
	for _, c := range t.held {
		wroteTerminal = true
		if !t.write(ctx, c) {
			break
		}
	}
	t.held = nil
	t.holding = false
	return wroteTerminal
}

// write hands one chunk to the caller and records any text it carried.
func (t *attemptTap) write(ctx context.Context, c backend.StreamChunk) bool {
	select {
	case <-ctx.Done():
		return false
	case t.out <- c:
	}
	if c.Text != "" {
		t.delivered = append(t.delivered, c.Text...)
	}
	return true
}

// readThrough runs one ReadBody attempt while forwarding its chunks through the
// tap. The reader runs on its own goroutine so Run — which owns the forwarding
// decision — can observe each chunk as it lands; the attempt is not over until
// ReadBody returns, so the goroutine is always waited for.
func (p Pipeline[Req]) readThrough(ctx context.Context, resp *http.Response, tap *attemptTap) error {
	ch := make(chan backend.StreamChunk, 64)
	errc := make(chan error, 1)
	go func() {
		defer close(ch)
		errc <- p.ReadBody(ctx, resp, ch)
	}()

	// Once the caller stops taking chunks (cancelled context) the reader still
	// has to be drained, or it blocks on its own send forever and Run never
	// returns.
	drained := false
	for c := range ch {
		if drained {
			continue
		}
		if !tap.accept(ctx, c) {
			drained = true
		}
	}
	return <-errc
}

// isModelCapacityErr reports whether err is the transient model-capacity
// failure the 1.5.17 changelog names: a 503 / MODEL_CAPACITY_EXHAUSTED answer
// for a request the model cannot serve right now, which is worth re-issuing
// once capacity frees up.
//
// Only 503 qualifies. A 500 is a server fault on this answer, a 400 is a
// malformed request and a 429 is rate limiting; each keeps exactly the
// behaviour it had before this fix. A quota or prepaid-balance body is excluded
// through IsRetryableStatus, the same way the open-path classifier excludes it.
func isModelCapacityErr(err error) bool {
	if err == nil {
		return false
	}
	var he *apierror.HTTPError
	if !errors.As(err, &he) {
		return false
	}
	if he.StatusCode != http.StatusServiceUnavailable {
		return false
	}
	return he.IsRetryableStatus()
}

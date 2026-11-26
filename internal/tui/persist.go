// persist.go — the composer's persistence off the Update goroutine.
//
// Every composer submit ends in m.saveSession(). It used to be a synchronous
// rewrite of the conversation into SQLite on the Update goroutine — the same
// goroutine Bubble Tea renders from — so the write was directly the user's
// Enter-to-send latency. Measured on a real 3,903-step conversation: 3.30 s
// before the write path was made incremental, and still several hundred
// milliseconds afterwards because the summaries index rebuilds the
// conversation's full-text body on every save.
//
// The write cannot simply move to a tea.Cmd. It has two halves with
// different owners:
//
//   - Preparing (supervisor.PrepareSave) reads the live trajectory and
//     advances the durable boundary. It must happen in order and on the
//     Update goroutine, because the supervisor's state is the UI's state.
//   - Writing (SavePlan.Write) only touches the store. It is the expensive
//     half and needs nothing from the UI.
//
// So the Update goroutine prepares, and a single background writer performs
// the writes. Ordering and coalescing are what make that safe:
//
//   - One goroutine, one queue: plans are written in the order they were
//     prepared, so a later state never loses to an earlier one.
//   - Coalescing: while the writer is busy, a new plan replaces the pending
//     one. The store converges on the newest state, and a burst of submits
//     costs one write instead of one write each.
//   - Close: drains the queue, so quitting persists the last state.
package tui

import (
	"sync"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
)

// sessionWriter serializes conversation persistence off the Update
// goroutine.
type sessionWriter struct {
	mu      sync.Mutex
	cond    *sync.Cond
	pending *persistJob
	closed  bool

	// busy is true while the writer is applying a job. drain waits for the
	// queue to empty and for the last write to finish.
	busy bool

	// done is closed when the writer goroutine exits, so a shutdown can
	// wait for the last write.
	done chan struct{}
}

// persistJob is one queued save: a prepared plan, plus the side writes that
// have to happen beside it.
type persistJob struct {
	plan    supervisor.SavePlan
	hasPlan bool
	// convID and queued describe the queue file, which is not part of the
	// plan.
	convID string
	queued []string
	// steps/workspace/title are the fallback path for a session with no
	// supervisor: it has no plan and builds the step list itself.
	steps     []convstore.Step
	hasSteps  bool
	workspace string
	title     string
	// next is the save queued behind this one. A job about a different
	// conversation cannot be coalesced, so it waits its turn instead.
	next *persistJob
}

// newSessionWriter starts the writer goroutine.
func newSessionWriter() *sessionWriter {
	w := &sessionWriter{done: make(chan struct{})}
	w.cond = sync.NewCond(&w.mu)
	go w.run()
	return w
}

// enqueue hands a save to the writer, coalescing it with the queued one when
// that is safe.
//
// Coalescing replaces the queued job, so everything only the dropped job
// carried must be folded into the survivor:
//
//   - the steps. A delta plan's Prev is the previous plan's Next, so the
//     survivor's diff would step over the dropped plan's steps and they would
//     never reach the store. The survivor's diff is widened back to where the
//     dropped one started.
//   - the conversation. A tenant switch (/resume, /fork, /new) makes the
//     queued job describe a different conversation, whose steps have nothing
//     to do with the survivor's. Those are kept, not merged.
func (w *sessionWriter) enqueue(job *persistJob) {
	if w == nil || job == nil {
		return
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.pending = coalesceJobs(w.pending, job)
	w.cond.Signal()
	w.mu.Unlock()
}

// coalesceJobs folds job into the queue's last save when doing so cannot lose
// a write.
//
// They are merged only when they describe the same conversation and the same
// kind of save — two plans, or two fallback step lists. A job of either kind
// carries the live session's full state, so the newer one subsumes the older
// one; a job of the other kind does not, and neither does one about another
// conversation.
//
// The chain is walked, not recursed: enqueue runs on the Update goroutine and
// the chain grows with every conversation switched away from before the writer
// catches up. Only the chain's last link can be superseded, because the links
// before it are earlier conversations that must still be written in order.
func coalesceJobs(head, job *persistJob) *persistJob {
	if head == nil {
		return job
	}
	prev, tail := (*persistJob)(nil), head
	for tail.next != nil {
		prev, tail = tail, tail.next
	}
	if tail.convID != job.convID || tail.hasPlan != job.hasPlan {
		// Not the same subject. Keep both, in order.
		tail.next = job
		return head
	}
	if tail.hasPlan {
		// The survivor's diff base is advanced to where the dropped plan
		// started, so its steps are written by the survivor.
		job.plan.Prev = tail.plan.Prev
	}
	if prev == nil {
		// The queue held only this job, which the new one supersedes.
		return job
	}
	prev.next = job
	return head
}

// run applies queued saves until the writer is closed and the queue is
// empty.
func (w *sessionWriter) run() {
	defer close(w.done)
	for {
		w.mu.Lock()
		for w.pending == nil && !w.closed {
			w.cond.Wait()
		}
		if w.pending == nil && w.closed {
			// Nothing left to write: done carries the news to whoever is
			// waiting for the writer to stop.
			w.mu.Unlock()
			return
		}
		job := w.pending
		w.pending = nil
		w.busy = true
		w.mu.Unlock()

		applyPersistJob(job)

		w.mu.Lock()
		w.busy = false
		w.cond.Broadcast() // a drain, if one is waiting
		w.mu.Unlock()
	}
}

// drain waits until everything queued so far has been written, without
// stopping the writer.
//
// It is for the rare site that has to read what it just wrote: /resume and
// /fork both save the conversation they are leaving and then list or copy the
// store. Those are user-initiated commands with nothing on the keystroke
// path, so waiting for the write there is correct — and the alternative is a
// picker that shows the conversation as it was one save ago.
//
// It waits for the queue to empty and for the write in flight to finish. That
// is the state a reader needs — the store holds everything the session has
// produced — and it cannot hang on a job queued meanwhile: a waiting drain is
// woken by the writer's next finish, and by then the queue is empty again.
func (w *sessionWriter) drain() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.closed {
		// The writer is stopping. It applies what it still holds and
		// then exits, so waiting for it is waiting for the queue — but it
		// has to take the mutex to notice, so the wait happens unlocked.
		w.mu.Unlock()
		<-w.done
		return
	}
	for w.pending != nil || w.busy {
		w.cond.Wait()
	}
	w.mu.Unlock()
}

// Close drains the queue and stops the writer. It is safe to call twice.
func (w *sessionWriter) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		<-w.done
		return
	}
	w.closed = true
	w.cond.Signal()
	w.mu.Unlock()
	<-w.done
}

// applyPersistJob performs one save, then the saves queued behind it.
//
// The chain is walked rather than recursed: it is as long as the number of
// conversations that were switched away from before the writer caught up, and
// that is not a depth to risk on the writer goroutine's stack.
func applyPersistJob(job *persistJob) {
	for j := job; j != nil; j = j.next {
		applyOnePersistJob(j)
	}
}

// applyOnePersistJob performs one save.
func applyOnePersistJob(job *persistJob) {
	if job.convID != "" {
		_ = convstore.SaveQueuedPrompts(job.convID, job.queued)
	}
	if job.hasPlan {
		_ = job.plan.Write()
		return
	}
	if job.convID == "" || !job.hasSteps {
		return
	}
	if err := convstore.SaveConversation(job.convID, job.workspace, job.steps); err != nil {
		return
	}
	if job.title != "" {
		_ = convstore.WriteSummariesTitle(job.convID, job.title)
		_ = convstore.WriteCacheTitle(job.convID, job.title)
	}
}

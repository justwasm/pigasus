package tui

// conversation_model.go — the conversation sub-model.
//
// ConversationModel owns the rendering transcript and the live turn: the
// rows the viewport scrolls, the block indexes that let a repaint find a
// turn's tool group or an assistant reply, and the in-flight reply buffers
// the stream writes into. The domain session still owns the messages
// themselves (sessionState.domain is the only copy); this model owns their
// presentation.
//
// Upstream: google3/third_party/jetski/cli/model/model.ConversationModel,
// which owns the reference's message list, tool calls and turn lifecycle.
// The clone keeps the session as the message owner and this model as the
// render/turn owner.

import (
	"fmt"
	"math"
	"strings"
	"time"

	sp "charm.land/bubbles/v2/spinner"

	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
)

// ConversationModel owns the rendering transcript and the live turn's
// streaming state.
type ConversationModel struct {
	// response is the rendered transcript: one entry per display row.
	response []string
	// blocks records where each turn's step block lives in response so
	// reflow and live colour changes can re-render it.
	blocks []transcript.Block
	// userBlocks records where each user prompt lives in response.
	userBlocks []transcript.UserBlock
	// assistants records where each completed assistant markdown reply
	// lives in response.
	assistants []transcript.AssistantBlock
	// discardTurn is set when a mid-stream local command (/clear, /new,
	// /fork) makes the in-flight turn's rows obsolete.
	discardTurn bool

	// streaming is true while a turn's stream is being applied.
	streaming bool
	// compactHappenedThisTurn records whether a compaction was performed during this turn.
	compactHappenedThisTurn bool
	// compactRowIdx is the transcript row the running compaction's spinner
	// occupies. It is only meaningful while compactRowLive is set: row 0 is a
	// legal row (the "> /compact" echo), so the index cannot say "none" by
	// itself.
	//
	// The row is repainted in place on every tick: the reference shows one
	// live line where "Generating…" and "Working…" appear, so a compaction
	// that appended a row per frame would scroll the conversation.
	compactRowIdx  int
	compactRowLive bool

	// compactedToolSteps is how many tool steps the latest compaction took
	// out of the active context: the sum of every tool step the summary
	// stands in for.
	//
	// It is a *step* count, not a block index, because a block does not
	// carry a compaction boundary: the transcript finalises a whole turn's
	// steps into blocks when the turn ends, so the compaction point can fall
	// inside a block, and a compaction that lands mid-turn sits between two
	// blocks that only exist after that turn is finalised. Counting steps
	// survives both, and survives DeduplicateBlocks re-sorting the blocks.
	//
	// The panel counts the tool results of the steps *after* this prefix, so
	// the tools that the summary replaced do not inflate the usage figure
	// while the tools the turn ran after compacting still count.
	compactedToolSteps int
	// turnInterrupted is true when the user cancelled the latest turn with
	// Esc; the composer hint and the absence of an error row follow it.
	turnInterrupted bool

	// liveSteps accumulates the steps of the turn in flight.
	liveSteps []toolsteps.Step
	// liveStart marks the first response row of the live turn.
	liveStart int
	// activeTool is the call currently running, or "".
	activeTool string
	// liveStepRuns mirrors liveSteps with transcript coordinates.
	liveStepRuns []transcript.LiveStepRun
	// replyStart marks the response row where the model's reply begins.
	replyStart int
	// liveTextLines is the row count of the streaming text.
	liveTextLines int
	// liveReplyBuf is the uncommitted tail of the streamed reply.
	liveReplyBuf string
	// steerReplyPrefix accumulates reply text streamed before the most
	// recent applied steer.
	steerReplyPrefix string
	// lastStreamSave throttles synchronous persistence during streaming.
	lastStreamSave time.Time
	// streamedText records whether this turn rendered any text live.
	streamedText bool
	// liveBatch is the batch id of the current turn's calls.
	liveBatch int
	// batchBreak forces the next call into a new batch.
	batchBreak bool
	// spinFrame drives the animated spinner label.
	spinFrame int
	// anyToolRun flips once a call has run in this turn.
	anyToolRun bool
	// liveReplyIdx is the index in the session where the live reply lives.
	liveReplyIdx int

	// thinking and its snippet drive the live thought row.
	thinking            bool
	thinkingSince       time.Time
	thinkingElapsed     time.Duration
	liveThinking        string
	liveThinkingSnippet string
	liveThoughtRunIdx   int

	// phaseTimer tracks elapsed time for the active phase (thinking, tool, generating/working).
	phaseTimer activityPhaseTimer
	toolSeq    int

	// Mid-turn quota refresh tracking (reference behaviour since 1.5.16, unchanged
	// in 1.5.17).
	turnStartedAt        time.Time
	lastQuotaRefreshAt   time.Time
	quotaRefreshInFlight bool

	// stepTray tracks the live checklist from update_steps during a turn.
	stepTray stepTray

	// spinner is the streaming spinner.
	spinner sp.Model

	// modelRetry tracks in-progress unavailable model retry countdown.
	modelRetry *ModelRetry

	// voiceReplyStart marks the response row where live voice agent speech begins (-1 when inactive).
	voiceReplyStart int
	// voiceReplyBuf accumulates streamed voice speech deltas.
	voiceReplyBuf string
	// voiceTextLines tracks how many rows the current live voice speech occupies.
	voiceTextLines int
}

// ModelRetry tracks model unavailable retry countdown.
type ModelRetry struct {
	Attempt     int
	MaxAttempts int
	RetryAt     time.Time
	Reason      string
}

func (c *ConversationModel) SetModelRetry(attempt, maxAttempts int, retryAt time.Time) {
	c.SetModelRetryWithReason(attempt, maxAttempts, retryAt, "")
}

func (c *ConversationModel) SetModelRetryWithReason(attempt, maxAttempts int, retryAt time.Time, reason string) {
	c.modelRetry = &ModelRetry{
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		RetryAt:     retryAt,
		Reason:      strings.TrimSpace(reason),
	}
}

func (c *ConversationModel) ClearModelRetry() {
	c.modelRetry = nil
}

// ModelRetryStatus formats the current model retry countdown line according
// to reference parity:
//
//	when remaining > 0: "Model unavailable, retrying in 12s (attempt 2/10)"
//	when remaining <= 0: "Model unavailable, retrying (attempt 2/10)"
//
// When a non-empty Reason is attached, it is appended concisely so the user
// can see why the request is retrying (e.g. HTTP 503 / 429 / capacity).
func (c *ConversationModel) ModelRetryStatus() (string, bool) {
	if c.modelRetry == nil {
		return "", false
	}
	attempt := c.modelRetry.Attempt
	maxAttempts := c.modelRetry.MaxAttempts
	var attemptStr string
	if attempt > 0 && maxAttempts > 0 {
		attemptStr = fmt.Sprintf(" (attempt %d/%d)", attempt, maxAttempts)
	}
	rem := 0
	if !c.modelRetry.RetryAt.IsZero() {
		rem = int(math.Ceil(time.Until(c.modelRetry.RetryAt).Seconds()))
		if rem < 0 {
			rem = 0
		}
	}
	var base string
	if rem > 0 {
		base = fmt.Sprintf("Model unavailable, retrying in %ds%s", rem, attemptStr)
	} else {
		base = fmt.Sprintf("Model unavailable, retrying%s", attemptStr)
	}
	if c.modelRetry.Reason != "" {
		return base + " — " + c.modelRetry.Reason + " (/debug)", true
	}
	return base, true
}

// ---- turn lifecycle ----
//
// These transitions own the conversation's stream flags. The host still
// performs the I/O they imply (session appends, persistence, transcript
// document edits); the model decides what the turn's state is.

// activityPhaseTimer tracks elapsed wall-clock time for the current live
// activity phase (thinking, a specific running tool, or generating/working),
// resetting whenever the phase or tool changes.
type activityPhaseTimer struct {
	key       string
	startedAt time.Time
	frozen    time.Duration
	active    bool
}

func (t *activityPhaseTimer) reset() {
	*t = activityPhaseTimer{}
}

func (t *activityPhaseTimer) track(key string, now time.Time) {
	if key == "" {
		t.reset()
		return
	}
	if t.active && t.key == key {
		return
	}
	t.key = key
	t.startedAt = now
	t.frozen = 0
	t.active = true
}

func (t *activityPhaseTimer) freeze(now time.Time) {
	if !t.active || t.startedAt.IsZero() {
		return
	}
	d := now.Sub(t.startedAt)
	if d < 0 {
		d = 0
	}
	t.frozen = d
	t.active = false
}

func (t *activityPhaseTimer) elapsed(now time.Time) time.Duration {
	if t.active && !t.startedAt.IsZero() {
		d := now.Sub(t.startedAt)
		if d < 0 {
			return 0
		}
		return d
	}
	return t.frozen
}

// BeginThinking starts a thought block. It reports whether this call opened
// the block, so the host only paints the first thought once.
func (c *ConversationModel) BeginThinking(now time.Time) bool {
	if c.thinking {
		c.streaming = true
		return false
	}
	c.thinking = true
	c.thinkingSince = now
	c.liveThinking = ""
	c.liveThinkingSnippet = ""
	c.liveThoughtRunIdx = -1
	c.streaming = true
	c.phaseTimer.track("thinking", now)
	return true
}

// AppendThought adds a delta to the live thought and refreshes its snippet.
// A delta that arrives before the start message opens the block.
func (c *ConversationModel) AppendThought(delta string, now time.Time) {
	if !c.thinking {
		c.thinking = true
		c.thinkingSince = now
		c.phaseTimer.track("thinking", now)
	}
	c.streaming = true
	c.liveThinking += delta
	c.liveThinkingSnippet = toolsteps.ThinkingLiveSnippet(c.liveThinking)
}

// ClearThinking drops the live thought buffers once the block is finished.
// The finished rows are painted by the host before it calls this.
func (c *ConversationModel) ClearThinking() {
	if !c.thinkingSince.IsZero() {
		if d := time.Since(c.thinkingSince); d > 0 {
			c.thinkingElapsed += d
		}
	}
	c.thinking = false
	c.thinkingSince = time.Time{}
	c.liveThinking = ""
	c.liveThinkingSnippet = ""
}

// BeginReply opens the reply region at the given response index.
func (c *ConversationModel) BeginReply(start int) {
	c.replyStart = start
	c.liveTextLines = 0
	c.liveReplyBuf = ""
}

// AppendReplyChunk adds streamed text to the reply and marks the turn live.
func (c *ConversationModel) AppendReplyChunk(chunk string) {
	c.liveReplyBuf += chunk
	c.streamedText = true
	c.batchBreak = true
	c.streaming = true
	if c.activeTool == "" && !c.thinking {
		key := "generating"
		if c.anyToolRun {
			key = "working"
		}
		c.phaseTimer.track(key, time.Now())
	}
}

// BeginToolRun marks a call as running and opens the tool phase.
func (c *ConversationModel) BeginToolRun(name string) {
	c.activeTool = name
	c.anyToolRun = true
	c.streaming = true
	c.toolSeq++
	c.phaseTimer.track(fmt.Sprintf("tool:%d:%s", c.toolSeq, name), time.Now())
}

// BeginToolResult closes the running call before its finished row is drawn.
func (c *ConversationModel) BeginToolResult() {
	c.activeTool = ""
	c.replyStart = -1
	c.liveTextLines = 0
	c.liveReplyBuf = ""
	c.phaseTimer.track("working", time.Now())
}

const midTurnQuotaRefreshInterval = 60 * time.Second

func (c *ConversationModel) shouldRefreshQuotaWhileStreaming(now time.Time) bool {
	if !c.streaming || c.turnStartedAt.IsZero() || c.quotaRefreshInFlight {
		return false
	}
	if c.lastQuotaRefreshAt.IsZero() {
		return now.Sub(c.turnStartedAt) >= midTurnQuotaRefreshInterval
	}
	return now.Sub(c.lastQuotaRefreshAt) >= midTurnQuotaRefreshInterval
}

// EndStream closes a streamed turn: no reply region, no live reply index and
// neither the streaming nor thinking flag stays set.
func (c *ConversationModel) EndStream() {
	c.replyStart = -1
	c.liveReplyIdx = -1
	c.streaming = false
	c.thinking = false
	c.thinkingSince = time.Time{}
	c.thinkingElapsed = 0
	c.toolSeq = 0
	c.phaseTimer.reset()
	c.turnStartedAt = time.Time{}
	c.lastQuotaRefreshAt = time.Time{}
	c.quotaRefreshInFlight = false
	c.stepTray.reset()
	c.modelRetry = nil
}

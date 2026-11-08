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
	liveThinking        string
	liveThinkingSnippet string
	liveThoughtRunIdx   int

	// spinner is the streaming spinner.
	spinner sp.Model
}

// ---- turn lifecycle ----
//
// These transitions own the conversation's stream flags. The host still
// performs the I/O they imply (session appends, persistence, transcript
// document edits); the model decides what the turn's state is.

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
	return true
}

// AppendThought adds a delta to the live thought and refreshes its snippet.
// A delta that arrives before the start message opens the block.
func (c *ConversationModel) AppendThought(delta string, now time.Time) {
	if !c.thinking {
		c.thinking = true
		c.thinkingSince = now
	}
	c.streaming = true
	c.liveThinking += delta
	c.liveThinkingSnippet = toolsteps.ThinkingLiveSnippet(c.liveThinking)
}

// ClearThinking drops the live thought buffers once the block is finished.
// The finished rows are painted by the host before it calls this.
func (c *ConversationModel) ClearThinking() {
	c.thinking = false
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
}

// BeginToolRun marks a call as running and opens the tool phase.
func (c *ConversationModel) BeginToolRun(name string) {
	c.activeTool = name
	c.anyToolRun = true
	c.streaming = true
}

// BeginToolResult closes the running call before its finished row is drawn.
func (c *ConversationModel) BeginToolResult() {
	c.activeTool = ""
	c.replyStart = -1
	c.liveTextLines = 0
	c.liveReplyBuf = ""
}

// EndStream closes a streamed turn: no reply region, no live reply index and
// neither the streaming nor thinking flag stays set.
func (c *ConversationModel) EndStream() {
	c.replyStart = -1
	c.liveReplyIdx = -1
	c.streaming = false
	c.thinking = false
}

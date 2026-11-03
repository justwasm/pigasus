package cliapp

import (
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/palette"
)

// transcriptDocument hands the host's transcript state to the ui tier.
//
// The host owns the rows: the document is the shape the transcript's
// mechanics work on, and the host takes the result back through
// applyTranscriptDocument.
func (m *runMode) transcriptDocument() transcript.Document {
	return transcript.Document{
		Lines:             m.response,
		Blocks:            m.blocks,
		Assistants:        m.assistants,
		UserBlocks:        m.userBlocks,
		Runs:              m.liveStepRuns,
		LiveStart:         m.liveStart,
		ReplyStart:        m.replyStart,
		LiveThoughtRunIdx: m.liveThoughtRunIdx,
		LiveTextLines:     m.liveTextLines,
		LiveReplyBuf:      m.liveReplyBuf,
		StreamedText:      m.streamedText,
	}
}

// applyTranscriptDocument takes the transcript's state back.
func (m *runMode) applyTranscriptDocument(doc transcript.Document) {
	m.response = doc.Lines
	m.blocks = doc.Blocks
	m.assistants = doc.Assistants
	m.userBlocks = doc.UserBlocks
	m.liveStepRuns = doc.Runs
	m.liveStart = doc.LiveStart
	m.replyStart = doc.ReplyStart
	m.liveThoughtRunIdx = doc.LiveThoughtRunIdx
	m.liveTextLines = doc.LiveTextLines
	m.liveReplyBuf = doc.LiveReplyBuf
	m.streamedText = doc.StreamedText
}

// transcriptOptions are the host's own render facts, as the transcript's
// mechanics see them.
func (m *runMode) transcriptOptions(msg toolsteps.TurnDoneMsg) transcript.Options {
	opts := m.repaintOptions()
	opts.EmptyTurn = agentturn.IsEmptyAssistantTurn(msg.Reply, nil, msg.Reasoning)
	return opts
}

// repaintOptions are the host's own render facts, as the transcript's
// repaint engine sees them.
//
// The viewport's height is handed over as a lookup rather than as a
// number: the engine asks for it only when it has to decide how many of
// the blocks it repaints, and the host's answer is a cached
// measurement.
func (m *runMode) repaintOptions() transcript.Options {
	verb := m.currentVerbosity()
	return transcript.Options{
		Width:          m.width,
		Height:         m.height,
		Verbosity:      verb,
		Expanded:       m.effectiveExpanded(verb),
		ViewportHeight: m.viewportHeight,
		Streaming:      m.streaming,
		Thought: transcript.LiveThought{
			Active: m.thinking,
			Text:   m.liveThinking,
			Since:  m.thinkingSince,
		},
	}
}

// toggleTrajectoryExpanded flips the trajectory-wide expanded flag,
// clears the scrollback by snapping the viewport to the active turn,
// and repaints the tool-step blocks under the active verbosity.
func (m *runMode) toggleTrajectoryExpanded() {
	m.expandedUserToggled = true
	m.expanded = !m.isExpanded()
	m.scroll = 0
	m.autoScroll = true
	m.rowsCacheDirty = true
	m.viewCacheValid = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

// rerenderBlocks repaints the transcript's tool-step blocks under the
// active verbosity and expansion.
func (m *runMode) rerenderBlocks() {
	m.rowsCacheDirty = true
	doc := m.transcriptDocument()
	doc.RerenderBlocks(m.repaintOptions())
	m.applyTranscriptDocument(doc)
}

// ensureVisibleBlocksExpanded repaints the blocks inside the visible
// window, so a block that scrolls into view is drawn under the current
// verbosity and expansion.
func (m *runMode) ensureVisibleBlocksExpanded(avail int) {
	doc := m.transcriptDocument()
	if doc.EnsureVisibleBlocksExpanded(m.repaintOptions(), avail, m.scroll, m.autoScroll) {
		m.rowsCacheDirty = true
		m.viewCacheValid = false
	}
	m.applyTranscriptDocument(doc)
}

// rerenderTranscriptForTheme repaints all user prompts, assistant replies,
// and tool step blocks across m.response under the active colour scheme.
//
// 【反编译】 agy 1.4.8 model.(*ConversationModel).handleSettingsChanges
// calls store.(*Manager).RerenderAll when the colour scheme changes,
// repainting the entire transcript with the new scheme's colours.
func (m *runMode) rerenderTranscriptForTheme() {
	if m == nil {
		return
	}
	m.rowsCacheDirty = true

	doc := m.transcriptDocument()
	doc.RerenderForTheme(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate view caches so viewport redraws completely
	m.rowsCacheDirty = true
	m.rowsCache = wrapSlot{}
	m.rowsView = composedRows{}
}

// reflowTranscript re-renders and re-wraps the entire transcript (markdown replies,
// tool step blocks, turn separators, in-flight streaming replies) when the terminal width changes.
//
// 【反编译】 agy 1.4.12 ConversationModel.handleResizeRedraw (0x102bcbb70)
// calls buildRenderedItems (0x102bbc2d0) on terminal resize, recalculating and
// re-rendering all message items to reflow properly for the new column width.
func (m *runMode) reflowTranscript(width int) {
	if m == nil || width <= 0 {
		return
	}
	m.width = width
	mdrender.SetCurrentWidth(width)
	m.rowsCacheDirty = true
	m.viewCacheValid = false
	m.extraRowsValid = false

	doc := m.transcriptDocument()
	doc.Reflow(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate viewport caches and rebuild wraps for the new width.
	m.rowsCacheDirty = true
	m.rowsCache = wrapSlot{}
	m.rowsCacheN1 = wrapSlot{}
	m.rowsView = composedRows{}
	m.viewCacheValid = false
	m.cachedHeaderKey = headerCacheKey{}
}

// syncLiveThoughtStep paints the turn's live thought row.
func (m *runMode) syncLiveThoughtStep() {
	doc := m.transcriptDocument()
	if doc.SyncLiveThoughtStep(m.repaintOptions()) {
		m.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// finishLiveThinking closes the turn's live thought: the running row
// becomes the finished thought.
func (m *runMode) finishLiveThinking() {
	if !m.thinking {
		return
	}
	// The options are taken while the thought is still live: the
	// finished thought is painted from the thought that was running.
	opts := m.repaintOptions()
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""

	doc := m.transcriptDocument()
	if doc.FinishLiveThinking(opts) {
		m.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// rerenderLiveSteps repaints the in-flight step rows: the live thought
// and the tool batches the model is still running.
func (m *runMode) rerenderLiveSteps() {
	doc := m.transcriptDocument()
	if doc.RerenderLiveSteps(m.repaintOptions()) {
		m.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// appendCompactResult renders the completion of a PiG-driven
// compaction. The reference (agy 1.4.5) draws a centred
// "Conversation compacted" rule plus a centred stats line instead of a
// transcript bullet; the same two rows are used here.
//
// PiG's end event only carries the token counts, so the stats line
// omits the step count and the summary size the reference reports
// (both are passed as "absent"). See
// docs/cycles/auto-compact-parity/captures.md.
func (m *runMode) appendCompactResult(msg compactDoneMsg) {
	if msg.Err != nil {
		m.response = append(m.response,
			uirows.NoticeCompact("Compact failed: "+msg.Err.Error()),
			"",
		)
		return
	}
	stats := compaction.StatsRow(m.width, 0, msg.TokensBefore, msg.TokensAfter, -1, -1)
	if msg.Aborted {
		stats += " (aborted)"
	}
	m.response = append(m.response,
		compaction.DividerRow(m.width),
		stats,
		"",
	)
}

var separator = palette.Rule(separatorWidth)

// separator returns the horizontal rule drawn between the banner and the
// prompt. Cycle 119: when the terminal width is known the rule is drawn at
// that width rather than the fixed 220 columns, so it never wraps.
func (m *runMode) separator() string {
	if m.width > 0 {
		return palette.Rule(m.width)
	}
	return separator
}

// promptRule returns the rule that frames the prompt box.
//
// 【一手】 agy 1.4.7: with a seasonal event switched on the prompt box's
// two rules become the event's own — a gradient rule with face motifs in
// it (out/changelog-147/ref/seasonal-prompt.txt) — while the rule that
// separates the queued messages from the transcript stays plain.
func (m *runMode) promptRule(top bool) string {
	if m.seasonal == nil {
		return m.separator()
	}
	return m.seasonal.Border.Rule(m.width, top)
}

const inputPrompt = "> "

// inputWidth is the textarea render width.
const inputWidth = 219

// statusRow is the status area's single row: the custom script's output
// when one is active, the built-in template otherwise.
func (m *runMode) statusRow() string {
	if out, _ := m.externalRenderedForRow(); out != "" {
		return out
	}
	return statusrow.Render(m.statusRowState())
}

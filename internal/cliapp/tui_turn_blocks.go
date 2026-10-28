package cliapp

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/compaction"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/palette"
)

type compactSpinnerTickMsg struct{}

func (m *runMode) replaceRange(start, end int, lines []string) {
	if start < 0 || start > len(m.response) {
		return
	}
	if end < start || end > len(m.response) {
		end = len(m.response)
	}
	tail := append([]string(nil), m.response[end:]...)
	m.response = append(m.response[:start], lines...)
	m.response = append(m.response, tail...)
}

func (m *runMode) rerenderBlocks() {
	m.rowsCacheDirty = true
	for i := range m.blocks {
		b := &m.blocks[i]
		var lines []string
		if b.bash != nil {
			lines = bashBlockLines(b.bash, m.expanded, m.height)
		} else {
			lines = toolBlockLines(b.steps, m.expanded, m.width)
		}
		delta := len(lines) - b.lines
		oldEnd := b.start + b.lines
		if delta == 0 {
			m.replaceRange(b.start, oldEnd, lines)
			continue
		}
		m.replaceRange(b.start, oldEnd, lines)
		b.lines = len(lines)
		for j := range m.blocks {
			if m.blocks[j].start > b.start {
				m.blocks[j].start += delta
			}
		}
		for j := range m.assistants {
			if m.assistants[j].start >= oldEnd {
				m.assistants[j].start += delta
			}
		}
		if m.liveStart >= oldEnd {
			m.liveStart += delta
		}
		if m.replyStart >= oldEnd {
			m.replyStart += delta
		}
	}
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

	// 1. Re-render all user prompt rows and slash echo blocks in m.response in-place
	for i, line := range m.response {
		inBlock := false
		for _, b := range m.blocks {
			if i >= b.start && i < b.start+b.lines {
				inBlock = true
				break
			}
		}
		if !inBlock {
			for _, a := range m.assistants {
				if i >= a.start && i < a.start+a.lines {
					inBlock = true
					break
				}
			}
		}
		if inBlock {
			continue
		}

		if isUserPromptRow(line) {
			plain := stripANSI(line)
			if idx := strings.Index(plain, "> "); idx >= 0 {
				prompt := plain[idx+2:]
				if strings.HasPrefix(prompt, "/") {
					m.response[i] = palette.WarningBlueOpen + "> " + prompt + palette.WarningBlueClose
				} else {
					m.response[i] = userPromptRow(prompt)
				}
			}
		} else if strings.Contains(line, "\u23bf") || strings.Contains(line, "⎿") {
			plain := stripANSI(line)
			var hookGlyph string
			var msg string
			if idx := strings.Index(plain, "\u23bf"); idx >= 0 {
				hookGlyph = "\u23bf"
				msg = strings.TrimSpace(plain[idx+len("\u23bf"):])
			} else if idx := strings.Index(plain, "⎿"); idx >= 0 {
				hookGlyph = "⎿"
				msg = strings.TrimSpace(plain[idx+len("⎿"):])
			}
			if hookGlyph != "" {
				m.response[i] = palette.WarningBlueOpen + "  " + hookGlyph + "  " + palette.WarningBlueClose + palette.SubtleOpen + msg + "\x1b[39m"
			}
		}
	}

	// 2. Re-render all assistant markdown replies
	for k := range m.assistants {
		a := &m.assistants[k]
		if a.start < 0 || a.start > len(m.response) {
			continue
		}
		oldEnd := a.start + a.lines
		if oldEnd > len(m.response) {
			oldEnd = len(m.response)
		}
		styled := renderMarkdown(a.raw, m.width)
		lines := strings.Split(styled, "\n")
		delta := len(lines) - a.lines
		m.replaceRange(a.start, oldEnd, lines)
		a.lines = len(lines)
		if delta != 0 {
			for j := k + 1; j < len(m.assistants); j++ {
				if m.assistants[j].start >= oldEnd {
					m.assistants[j].start += delta
				}
			}
			for j := range m.blocks {
				if m.blocks[j].start >= oldEnd {
					m.blocks[j].start += delta
				}
			}
			if m.liveStart >= oldEnd {
				m.liveStart += delta
			}
			if m.replyStart >= oldEnd {
				m.replyStart += delta
			}
		}
	}

	// 3. Re-render all tool step blocks (using new palette colours)
	m.rerenderBlocks()

	// 4. Invalidate view caches so viewport redraws completely
	m.rowsCacheDirty = true
	m.rowsCache = wrapSlot{}
	m.rowsView = composedRows{}
}

func (m *runMode) blankBefore() {
	if len(m.response) == 0 {
		return
	}
	if strings.TrimSpace(stripANSI(m.response[len(m.response)-1])) != "" {
		m.response = append(m.response, "")
	}
}

type liveStepBatch struct {
	last  int
	start int
	steps []toolStep
}

func (m *runMode) finaliseLeadingThought(msg turnDoneMsg) {
	extra := len(msg.steps) - len(m.liveStepRuns)
	if extra > 0 && msg.steps[0].kind == stepKindThought {
		thought := msg.steps[:extra]
		lines := toolBlockLines(thought, m.expanded, m.width)
		at := m.liveStart
		m.replaceRange(at, at, lines)
		for j := range m.liveStepRuns {
			m.liveStepRuns[j].start += len(lines)
		}
		if m.replyStart >= at {
			m.replyStart += len(lines)
		}
		m.blocks = append(m.blocks, turnBlock{start: at, lines: len(lines), steps: thought})
	}
}

func (m *runMode) collectLiveStepBatches() []liveStepBatch {
	var batches []liveStepBatch
	for i, r := range m.liveStepRuns {
		if n := len(batches); n > 0 && batches[n-1].steps[0].batch == r.step.batch {
			batches[n-1].last = i
			batches[n-1].steps = append(batches[n-1].steps, r.step)
			continue
		}
		batches = append(batches, liveStepBatch{last: i, start: r.start, steps: []toolStep{r.step}})
	}
	return batches
}

func (m *runMode) replaceLiveStepBatches(batches []liveStepBatch) {
	for bi := len(batches) - 1; bi >= 0; bi-- {
		b := batches[bi]
		end := m.liveStepRuns[b.last].start + m.liveStepRuns[b.last].lines
		if b.start < 0 || end > len(m.response) {
			continue
		}
		lines := toolBlockLines(b.steps, m.expanded, m.width)
		delta := len(lines) - (end - b.start)
		m.replaceRange(b.start, end, lines)
		m.blocks = append(m.blocks, turnBlock{start: b.start, lines: len(lines), steps: b.steps})
		if delta != 0 {
			for j := range m.liveStepRuns {
				if m.liveStepRuns[j].start >= end {
					m.liveStepRuns[j].start += delta
				}
			}
			if m.replyStart >= end {
				m.replyStart += delta
			}
		}
	}
}

func (m *runMode) finaliseLiveTurn(msg turnDoneMsg) bool {
	if len(m.liveStepRuns) == 0 {
		return false
	}
	m.rowsCacheDirty = true
	m.finaliseLeadingThought(msg)
	batches := m.collectLiveStepBatches()
	m.replaceLiveStepBatches(batches)
	sort.SliceStable(m.blocks, func(i, j int) bool { return m.blocks[i].start < m.blocks[j].start })
	return true
}

func (m *runMode) rerenderLiveSteps() {
	if len(m.liveStepRuns) == 0 {
		return
	}
	m.rowsCacheDirty = true
	for i := len(m.liveStepRuns) - 1; i >= 0; i-- {
		r := &m.liveStepRuns[i]
		oldEnd := r.start + r.lines
		if r.start < 0 || oldEnd > len(m.response) {
			m.liveStepRuns = append(m.liveStepRuns[:i], m.liveStepRuns[i+1:]...)
			continue
		}
		lines := toolStepLines(r.step, m.expanded, m.width)
		delta := len(lines) - r.lines
		m.replaceRange(r.start, oldEnd, lines)
		if delta == 0 {
			continue
		}
		r.lines = len(lines)
		for j := i + 1; j < len(m.liveStepRuns); j++ {
			m.liveStepRuns[j].start += delta
		}
		if m.replyStart >= oldEnd {
			m.replyStart += delta
		}
	}
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
			cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+"Compact failed: "+msg.Err.Error()+panelGray238C,
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

// turnErrorRow renders the transcript row for a failed turn. Delegated to agent.TurnErrorRow.
func turnErrorRow(err error) string {
	return agent.TurnErrorRow(err)
}

var separator = cRule(separatorWidth)

// separator returns the horizontal rule drawn between the banner and the
// prompt. Cycle 119: when the terminal width is known the rule is drawn at
// that width rather than the fixed 220 columns, so it never wraps.
func (m *runMode) separator() string {
	if m.width > 0 {
		return cRule(m.width)
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

// statusWidth is the canonical screen width used by the status row pad math.
const statusWidth = separatorWidth

// statusRightPrefix is the literal right-side label of the status row.
const statusRightPrefix = " \u00b7 high" // " · high" (7 runes; 8 bytes UTF-8)

// staticStatusRow is the static variant of statusRow used by the non-tty render path.
func staticStatusRow(model string) string {
	leftPlain := "? for shortcuts"
	left := "\033[38;2;154;160;166m" + leftPlain + "\033[39m"
	right := "\033[38;2;154;160;166m" + model + statusRightPrefix + "\033[39m"
	pad := strings.Repeat(" ", statusWidth-utf8.RuneCountInString(leftPlain)-utf8.RuneCountInString(model)-utf8.RuneCountInString(statusRightPrefix))
	return left + pad + right
}

// staticStatusRowWithTasks is the cycle-tasks-panel variant of staticStatusRow.
func staticStatusRowWithTasks(model string, n int) string {
	taskSuffix := ""
	if n == 1 {
		taskSuffix = " \u00b7 1 task"
	} else if n > 1 {
		taskSuffix = fmt.Sprintf(" \u00b7 %d tasks", n)
	}
	leftPlain := "? for shortcuts"
	left := "\033[38;2;154;160;166m" + leftPlain + "\033[39m"
	right := "\033[38;2;154;160;166m" + model + statusRightPrefix + taskSuffix + "\033[39m"
	pad := strings.Repeat(" ", statusWidth-utf8.RuneCountInString(leftPlain)-utf8.RuneCountInString(model)-utf8.RuneCountInString(statusRightPrefix)-utf8.RuneCountInString(taskSuffix))
	return left + pad + right
}

func (m *runMode) statusCompactLabel() string {
	frame := usageSpinnerFrame(m.compactSpinnerFrame)
	if m.compactEnd.IsZero() {
		if m.compactTokensAfter > 0 {
			return frame + "  Compacting context…  " +
				printmode.TokenK(m.compactTokensBefore) + " → " +
				printmode.TokenK(m.compactTokensAfter) + " (" +
				compactDuration(m.compactStart, time.Now()) + ")"
		}
		return frame + "  Compacting context… (" +
			compactDuration(m.compactStart, time.Now()) + ")"
	}
	return "Compacted " +
		printmode.TokenK(m.compactTokensBefore) + " → " +
		printmode.TokenK(m.compactTokensAfter) + " (" +
		compactDuration(m.compactStart, m.compactEnd) + ")"
}

func (m *runMode) statusLeftSegment() (string, string) {
	leftPlain := m.statusLeftHint()
	left := ""
	if leftPlain != "" {
		left = cDimGrayOpen + leftPlain + cDimGrayClose
	}

	if badge := m.vimBadge(); badge != "" {
		badgeStyled := cDimGrayOpen + badge + cDimGrayClose
		if leftPlain != "" {
			leftPlain = badge + " " + leftPlain
			left = badgeStyled + " " + left
		} else {
			leftPlain = badge
			left = badgeStyled
		}
	}
	if m.helpMode || (m.panel.kind != panelNone && !m.panel.kind.keepStatusHint()) {
		leftPlain, left = "", ""
	}
	if m.exitConfirm {
		leftPlain = "press ctrl+c again to exit"
		left = cDimGrayOpen + leftPlain + cDimGrayClose
	}
	if !m.compactStart.IsZero() {
		label := m.statusCompactLabel()
		leftPlain = label
		left = cDimGrayOpen + label + cDimGrayClose
	}
	return left, leftPlain
}

// statusLeftHint is the plain text of the status row's left segment.
//
// The rules, in order: a visible picker says "esc to cancel" (except the
// /model popup, which drops the hint); a streaming turn says the same;
// queued messages ask for "up"; a non-empty buffer clears the hint
// entirely; bash mode replaces it with its own.
func (m *runMode) statusLeftHint() string {
	leftPlain := "? for shortcuts"
	bufferEmpty := strings.TrimSpace(m.input.Value()) == ""

	if m.picker.Visible() {
		leftPlain = "esc to cancel"
		// The reference's "/model <query>" popup drops the cancel hint:
		// its status row keeps the left segment empty.
		// 【一手】 agy 1.4.6, live.
		if m.picker.Kind() == tuilib.PickerKindModel {
			leftPlain = ""
		}
	}
	if m.streaming && !m.picker.Visible() && leftPlain != "" {
		leftPlain = "esc to cancel"
	}
	if (len(m.streamQueue) > 0 || len(m.bashQueue) > 0) && !m.picker.Visible() {
		leftPlain = "  Press up to edit queued messages"
	}
	if (!bufferEmpty || m.resumeMode) && !m.picker.Visible() {
		leftPlain = ""
	}
	if m.bashMode && !m.picker.Visible() && !m.helpMode {
		leftPlain = " activated bash mode · esc to cancel"
	}
	return leftPlain
}

type statusRightInfo struct {
	styled        string
	modePlain     string
	rightPlainLen int
}

func (m *runMode) buildRightStyled(statusModel, suffix, taskSuffix string) (string, string) {
	agentPartPlain := ""
	right := ""
	if m.agentName != "" {
		agentPartPlain = m.agentName + " (" + statusModel + suffix + taskSuffix + ")  /agents"
		if m.queueMode {
			agentPartPlain += " · Queue mode"
		}
		right = cDimGrayOpen + agentPartPlain + cDimGrayClose
	} else {
		right = cDimGrayOpen + statusModel + suffix + taskSuffix
		if m.queueMode {
			right += " · Queue mode"
		}
		right += cDimGrayClose
	}
	return right, agentPartPlain
}

func (m *runMode) statusRightSegment() statusRightInfo {
	statusModel := m.statusModel
	if statusModel == "" {
		statusModel = m.model
	}
	modePlain, modeStyled := "", ""
	if m.mode != "" {
		modePlain = m.mode + " · "
		modeStyled = statusModeColor(m.mode) + m.mode + cDimGrayOpen + " · "
	}
	suffix := m.statusEffortSuffix()
	taskSuffix := m.tasksStatusSuffix()

	right, agentPartPlain := m.buildRightStyled(statusModel, suffix, taskSuffix)

	rightPlainLen := utf8.RuneCountInString(statusModel) + utf8.RuneCountInString(suffix) + utf8.RuneCountInString(taskSuffix)
	if m.agentName != "" {
		rightPlainLen = utf8.RuneCountInString(agentPartPlain)
	}

	return statusRightInfo{
		styled:        modeStyled + right,
		modePlain:     modePlain,
		rightPlainLen: rightPlainLen,
	}
}

// statusRow is the status area's single row: the custom script's output
// when one is active, the built-in template otherwise.
func (m *runMode) statusRow() string {
	if out, _ := m.externalRenderedForRow(); out != "" {
		return out
	}
	return m.builtinStatusRow()
}

// builtinStatusRow is the reference's built-in status line.
func (m *runMode) builtinStatusRow() string {
	left, leftPlain := m.statusLeftSegment()
	rightInfo := m.statusRightSegment()

	width := statusWidth
	if m.width > 0 {
		width = m.width
	}
	padLen := width - utf8.RuneCountInString(leftPlain) - utf8.RuneCountInString(rightInfo.modePlain) - rightInfo.rightPlainLen
	if padLen < 1 {
		padLen = 1
	}
	pad := strings.Repeat(" ", padLen)
	return left + pad + rightInfo.styled
}

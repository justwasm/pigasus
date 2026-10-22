package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

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
				tokenKMid(m.compactTokensBefore) + " → " +
				tokenKMid(m.compactTokensAfter) + " (" +
				compactDuration(m.compactStart, time.Now()) + ")"
		}
		return frame + "  Compacting context… (" +
			compactDuration(m.compactStart, time.Now()) + ")"
	}
	return "Compacted " +
		tokenKMid(m.compactTokensBefore) + " → " +
		tokenKMid(m.compactTokensAfter) + " (" +
		compactDuration(m.compactStart, m.compactEnd) + ")"
}

func (m *runMode) statusLeftSegment() (string, string) {
	leftPlain := "? for shortcuts"
	leftOpen, leftClose := cDimGrayOpen, cDimGrayClose
	bufferEmpty := strings.TrimSpace(m.input.Value()) == ""

	if m.picker.Visible() {
		leftPlain = "esc to cancel"
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
		leftPlain = " activated bash mode \u00b7 esc to cancel"
	}

	left := ""
	if leftPlain != "" {
		left = leftOpen + leftPlain + leftClose
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
		left = leftOpen + leftPlain + leftClose
	}
	if !m.compactStart.IsZero() {
		label := m.statusCompactLabel()
		leftPlain = label
		left = leftOpen + label + leftClose
	}
	return left, leftPlain
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

func (m *runMode) statusRow() string {
	if out, _ := m.externalRenderedForRow(); out != "" {
		return out
	}
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

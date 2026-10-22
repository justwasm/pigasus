package main

import (
	"fmt"
	"sort"
	"strings"
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
		lines := toolBlockLines(b.steps, m.expanded, m.width)
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
		if m.liveStart >= oldEnd {
			m.liveStart += delta
		}
		if m.replyStart >= oldEnd {
			m.replyStart += delta
		}
	}
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

func (m *runMode) appendCompactResult(msg compactDoneMsg) {
	var body string
	switch {
	case msg.Err != nil:
		body = "Compact failed: " + msg.Err.Error()
	default:
		var tokensLine string
		if msg.TokensBefore > 0 || msg.TokensAfter > 0 {
			tokensLine = fmt.Sprintf("%d \u2192 %d tokens", msg.TokensBefore, msg.TokensAfter)
			if msg.TokensBefore > 0 {
				pct := (1 - float64(msg.TokensAfter)/float64(msg.TokensBefore)) * 100
				if pct < 0 {
					pct = 0
				}
				tokensLine += fmt.Sprintf(" (%.1f%% reduction)", pct)
			}
		} else {
			tokensLine = "0 \u2192 0 tokens"
		}
		body = "Compacted: " + tokensLine
		if msg.Aborted {
			body += " (aborted)"
		}
	}
	m.response = append(m.response,
		cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+body+panelGray238C,
		"",
	)
}

// repaint.go — the transcript's repaint engine.
//
// The host owns the response *buffer* and decides *when* the transcript
// is repainted: the width changed, the colour scheme changed, the
// verbosity changed, the user expanded the blocks. This file owns the
// arithmetic of the repaint itself — the in-place rewriting of the row
// buffer and the shifting of every index into it.
//
// The host hands over a Document and takes it back. Nothing here knows
// about the TUI host: the render facts arrive as Options and the turn's
// live thought arrives as LiveThought.
//
// Stdlib-only plus internal/ui/mdrender, internal/ui/toolsteps, the
// palette and the diff action's ANSI stripper. No dependency on the TUI
// host.
package transcript

import (
	"strings"
	"time"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/palette"
)

// blockUpdate is one block's repaint: the rows it now occupies and the
// height it gained or lost.
type blockUpdate struct {
	// idx is the block's index in the document's block list.
	idx int
	// origEnd is the row the block's old rows ended at.
	origEnd int
	// lines are the block's new rows.
	lines []string
	// delta is the height the block gained (or lost).
	delta int
}

// applyBlockUpdates rewrites the row buffer in place.
//
// Every updated block's rows are replaced by its new ones, and every
// index into the buffer — the user prompts, the assistant replies, the
// live step runs, the live row and the streaming reply — is shifted by
// the height the blocks gained or lost.
func (d *Document) applyBlockUpdates(updates []blockUpdate, totalDelta int, exp bool) {
	if len(updates) == 0 {
		return
	}
	newResp := make([]string, 0, len(d.Lines)+totalDelta)
	curOld := 0

	updateMap := make(map[int]blockUpdate, len(updates))
	for _, u := range updates {
		updateMap[u.idx] = u
	}

	for i := range d.Blocks {
		b := &d.Blocks[i]
		origStart := b.Start
		origEnd := origStart + b.Lines

		// Prevent overlap or backwards traversal
		if origStart < curOld {
			origStart = curOld
		}
		if origEnd < curOld {
			origEnd = curOld
		}

		// Copy unmodified lines between last position and this block
		if origStart > curOld {
			limit := origStart
			if limit > len(d.Lines) {
				limit = len(d.Lines)
			}
			if curOld < limit {
				newResp = append(newResp, d.Lines[curOld:limit]...)
			}
		}

		if u, ok := updateMap[i]; ok {
			newStart := len(newResp)
			newResp = append(newResp, u.lines...)
			b.Start = newStart
			b.Lines = len(u.lines)
			b.Expanded = exp
			if origEnd > curOld {
				curOld = origEnd
			}
		} else {
			newStart := len(newResp)
			if origStart < len(d.Lines) {
				limit := origEnd
				if limit > len(d.Lines) {
					limit = len(d.Lines)
				}
				if origStart < limit {
					newResp = append(newResp, d.Lines[origStart:limit]...)
				}
			}
			b.Start = newStart
			if origEnd > curOld {
				curOld = origEnd
			}
		}
	}

	// Copy any remaining lines after the last block
	if curOld < len(d.Lines) {
		newResp = append(newResp, d.Lines[curOld:]...)
	}
	d.Lines = newResp

	shiftPos := func(oldPos int) int {
		shift := 0
		for _, u := range updates {
			if u.origEnd <= oldPos {
				shift += u.delta
			}
		}
		return oldPos + shift
	}

	for j := range d.UserBlocks {
		d.UserBlocks[j].Start = shiftPos(d.UserBlocks[j].Start)
	}
	for j := range d.Assistants {
		d.Assistants[j].Start = shiftPos(d.Assistants[j].Start)
	}
	for j := range d.Runs {
		d.Runs[j].Start = shiftPos(d.Runs[j].Start)
	}
	if d.LiveStart > 0 {
		d.LiveStart = shiftPos(d.LiveStart)
	}
	if d.ReplyStart > 0 {
		d.ReplyStart = shiftPos(d.ReplyStart)
	}
}

// RerenderBlocks repaints the transcript's tool-step blocks under the
// active verbosity and expansion.
//
// A block that needs no repaint is left alone; the rows of the blocks
// that do are rewritten in place, and every index into the buffer is
// shifted by the height the blocks gained or lost.
func (d *Document) RerenderBlocks(opts Options) {
	verb := opts.Verbosity
	exp := opts.Expanded

	if len(d.Blocks) == 0 {
		return
	}

	const fastThreshold = 30
	var shouldRender func(i int, b *Block) bool

	if len(d.Blocks) <= fastThreshold {
		shouldRender = func(i int, b *Block) bool { return true }
	} else {
		avail := 0
		if opts.ViewportHeight != nil {
			avail = opts.ViewportHeight()
		}
		if avail <= 0 {
			avail = opts.Height
		}
		if avail <= 0 {
			avail = 40
		}
		budget := avail + 120
		visibleIdx := make(map[int]bool)
		linesAcc := 0
		for i := len(d.Blocks) - 1; i >= 0; i-- {
			visibleIdx[i] = true
			linesAcc += d.Blocks[i].Lines
			if linesAcc >= budget {
				break
			}
		}
		shouldRender = func(i int, b *Block) bool {
			return visibleIdx[i]
		}
	}

	var updates []blockUpdate
	totalDelta := 0

	for i := range d.Blocks {
		b := &d.Blocks[i]
		if !b.NeedsRender(exp, opts.Width, verb) {
			continue
		}
		if !shouldRender(i, b) {
			continue
		}
		lines := b.Render(exp, opts.Width, opts.Height, verb)
		delta := len(lines) - b.Lines
		updates = append(updates, blockUpdate{
			idx:     i,
			origEnd: b.Start + b.Lines,
			lines:   lines,
			delta:   delta,
		})
		totalDelta += delta
	}

	if len(updates) > 0 {
		d.applyBlockUpdates(updates, totalDelta, exp)
	}
}

// EnsureVisibleBlocksExpanded repaints the blocks inside the visible
// window, so a block that scrolls into view is drawn under the current
// verbosity and expansion.
//
// It reports whether it repainted anything.
func (d *Document) EnsureVisibleBlocksExpanded(opts Options, avail int, scroll int, autoScroll bool) bool {
	if len(d.Blocks) == 0 {
		return false
	}
	verb := opts.Verbosity
	exp := opts.Expanded

	anyNeed := false
	for i := range d.Blocks {
		if d.Blocks[i].NeedsRender(exp, opts.Width, verb) {
			anyNeed = true
			break
		}
	}
	if !anyNeed {
		return false
	}

	totalRows := len(d.Lines)
	var winStart, winEnd int
	if autoScroll || scroll >= totalRows-avail {
		winEnd = totalRows
		winStart = totalRows - avail
	} else {
		winStart = scroll
		winEnd = scroll + avail
	}
	if winStart < 0 {
		winStart = 0
	}
	winStart -= 80
	if winStart < 0 {
		winStart = 0
	}
	winEnd += 80
	if winEnd > totalRows {
		winEnd = totalRows
	}

	var updates []blockUpdate
	totalDelta := 0

	for i := range d.Blocks {
		b := &d.Blocks[i]
		if !b.NeedsRender(exp, opts.Width, verb) {
			continue
		}
		bEnd := b.Start + b.Lines
		if bEnd < winStart || b.Start > winEnd {
			continue
		}
		lines := b.Render(exp, opts.Width, opts.Height, verb)
		delta := len(lines) - b.Lines
		updates = append(updates, blockUpdate{
			idx:     i,
			origEnd: bEnd,
			lines:   lines,
			delta:   delta,
		})
		totalDelta += delta
	}

	if len(updates) > 0 {
		d.applyBlockUpdates(updates, totalDelta, exp)
		return true
	}
	return false
}

// RerenderForTheme repaints all user prompts, assistant replies, and
// tool step blocks across the buffer under the active colour scheme.
//
// 【反编译】 agy 1.4.8 model.(*ConversationModel).handleSettingsChanges
// calls store.(*Manager).RerenderAll when the colour scheme changes,
// repainting the entire transcript with the new scheme's colours.
func (d *Document) RerenderForTheme(opts Options) {
	// 1. Re-render all user prompt rows and slash echo blocks in place
	for i, line := range d.Lines {
		inBlock := false
		for _, b := range d.Blocks {
			if i >= b.Start && i < b.Start+b.Lines {
				inBlock = true
				break
			}
		}
		if !inBlock {
			for _, a := range d.Assistants {
				if i >= a.Start && i < a.Start+a.Lines {
					inBlock = true
					break
				}
			}
		}
		if !inBlock {
			for _, u := range d.UserBlocks {
				if i >= u.Start && i < u.Start+u.Lines {
					inBlock = true
					break
				}
			}
		}
		if inBlock {
			continue
		}

		if tuiviewport.IsUserPromptRow(line) {
			plain := diffaction.StripANSI(line)
			if idx := strings.Index(plain, "> "); idx >= 0 {
				prompt := plain[idx+2:]
				if strings.HasPrefix(prompt, "/") {
					d.Lines[i] = palette.WarningBlueOpen + "> " + prompt + palette.WarningBlueClose
				} else {
					d.Lines[i] = UserPromptRow(prompt)
				}
			}
		} else if strings.Contains(line, "\u23bf") || strings.Contains(line, "⎿") {
			plain := diffaction.StripANSI(line)
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
				d.Lines[i] = palette.WarningBlueOpen + "  " + hookGlyph + "  " + palette.WarningBlueClose + palette.SubtleOpen + msg + "\x1b[39m"
			}
		}
	}

	// 1b. Re-render all user prompt blocks
	for k := range d.UserBlocks {
		u := &d.UserBlocks[k]
		if u.Start < 0 || u.Start > len(d.Lines) {
			continue
		}
		oldEnd := u.Start + u.Lines
		if oldEnd > len(d.Lines) {
			oldEnd = len(d.Lines)
		}
		lines := RenderUserPrompt(u.Raw, opts.Width)
		d.Lines = ReplaceRange(d.Lines, u.Start, oldEnd, lines)
	}

	// 2. Re-render all assistant markdown replies
	for k := range d.Assistants {
		a := &d.Assistants[k]
		if a.Start < 0 || a.Start > len(d.Lines) {
			continue
		}
		oldEnd := a.Start + a.Lines
		if oldEnd > len(d.Lines) {
			oldEnd = len(d.Lines)
		}
		styled := mdrender.RenderMarkdown(a.Raw, opts.Width)
		lines := strings.Split(styled, "\n")
		delta := len(lines) - a.Lines
		d.Lines = ReplaceRange(d.Lines, a.Start, oldEnd, lines)
		a.Lines = len(lines)
		if delta != 0 {
			for j := k + 1; j < len(d.Assistants); j++ {
				if d.Assistants[j].Start >= oldEnd {
					d.Assistants[j].Start += delta
				}
			}
			for j := range d.Blocks {
				if d.Blocks[j].Start >= oldEnd {
					d.Blocks[j].Start += delta
				}
			}
			for j := range d.Runs {
				if d.Runs[j].Start >= oldEnd {
					d.Runs[j].Start += delta
				}
			}
			if d.LiveStart >= oldEnd {
				d.LiveStart += delta
			}
			if d.ReplyStart >= oldEnd {
				d.ReplyStart += delta
			}
		}
	}

	// 3. Re-render all tool step blocks (using new palette colours)
	d.RerenderBlocks(opts)
}

// Reflow re-renders and re-wraps the entire transcript (markdown
// replies, tool step blocks, turn separators, in-flight streaming
// replies) for a new terminal width.
//
// 【反编译】 agy 1.4.12 ConversationModel.handleResizeRedraw (0x102bcbb70)
// calls buildRenderedItems (0x102bbc2d0) on terminal resize, recalculating and
// re-rendering all message items to reflow properly for the new column width.
func (d *Document) Reflow(opts Options) {
	width := opts.Width

	// 1. Re-render turn separators for the new width.
	for i, line := range d.Lines {
		inBlock := false
		for _, b := range d.Blocks {
			if i >= b.Start && i < b.Start+b.Lines {
				inBlock = true
				break
			}
		}
		if !inBlock {
			for _, a := range d.Assistants {
				if i >= a.Start && i < a.Start+a.Lines {
					inBlock = true
					break
				}
			}
		}
		if !inBlock {
			for _, u := range d.UserBlocks {
				if i >= u.Start && i < u.Start+u.Lines {
					inBlock = true
					break
				}
			}
		}
		if inBlock {
			continue
		}
		plain := diffaction.StripANSI(line)
		trimmed := strings.TrimSpace(plain)
		if len(trimmed) > 0 && strings.Trim(trimmed, "─") == "" {
			d.Lines[i] = TurnSeparator(width)
		}
	}

	// 2. Re-render all user prompt blocks with the new width.
	for k := range d.UserBlocks {
		u := &d.UserBlocks[k]
		if u.Start < 0 || u.Start > len(d.Lines) {
			continue
		}
		oldEnd := u.Start + u.Lines
		if oldEnd > len(d.Lines) {
			oldEnd = len(d.Lines)
		}
		lines := RenderUserPrompt(u.Raw, width)
		delta := len(lines) - u.Lines
		d.Lines = ReplaceRange(d.Lines, u.Start, oldEnd, lines)
		u.Lines = len(lines)
		if delta != 0 {
			for j := range d.UserBlocks {
				if j != k && d.UserBlocks[j].Start >= oldEnd {
					d.UserBlocks[j].Start += delta
				}
			}
			for j := range d.Assistants {
				if d.Assistants[j].Start >= oldEnd {
					d.Assistants[j].Start += delta
				}
			}
			for j := range d.Blocks {
				if d.Blocks[j].Start >= oldEnd {
					d.Blocks[j].Start += delta
				}
			}
			for j := range d.Runs {
				if d.Runs[j].Start >= oldEnd {
					d.Runs[j].Start += delta
				}
			}
			if d.LiveStart >= oldEnd {
				d.LiveStart += delta
			}
			if d.ReplyStart >= oldEnd {
				d.ReplyStart += delta
			}
		}
	}

	// 3. Re-render all completed assistant markdown replies with the new width.
	for k := range d.Assistants {
		a := &d.Assistants[k]
		if a.Start < 0 || a.Start > len(d.Lines) {
			continue
		}
		oldEnd := a.Start + a.Lines
		if oldEnd > len(d.Lines) {
			oldEnd = len(d.Lines)
		}
		styled := mdrender.RenderMarkdown(a.Raw, width)
		lines := strings.Split(styled, "\n")
		delta := len(lines) - a.Lines
		d.Lines = ReplaceRange(d.Lines, a.Start, oldEnd, lines)
		a.Lines = len(lines)
		if delta != 0 {
			for j := range d.UserBlocks {
				if d.UserBlocks[j].Start >= oldEnd {
					d.UserBlocks[j].Start += delta
				}
			}
			for j := range d.Assistants {
				if j != k && d.Assistants[j].Start >= oldEnd {
					d.Assistants[j].Start += delta
				}
			}
			for j := range d.Blocks {
				if d.Blocks[j].Start >= oldEnd {
					d.Blocks[j].Start += delta
				}
			}
			for j := range d.Runs {
				if d.Runs[j].Start >= oldEnd {
					d.Runs[j].Start += delta
				}
			}
			if d.LiveStart >= oldEnd {
				d.LiveStart += delta
			}
			if d.ReplyStart >= oldEnd {
				d.ReplyStart += delta
			}
		}
	}

	// 3. Re-render all tool step blocks for the new width.
	d.RerenderBlocks(opts)

	// 4. Re-render in-flight live step runs (if any).
	d.RerenderLiveSteps(opts)

	// 5. If currently streaming a model reply, re-render it for the new width.
	if opts.Streaming && d.ReplyStart >= 0 && d.LiveReplyBuf != "" {
		styled := mdrender.RenderMarkdown(d.LiveReplyBuf, width)
		lines := strings.Split(styled, "\n")
		d.Lines = ReplaceRange(d.Lines, d.ReplyStart, d.ReplyStart+d.LiveTextLines, lines)
		d.LiveTextLines = len(lines)
	}
}

// SyncLiveThoughtStep paints the turn's live thought row.
//
// A collapsed transcript keeps no thought row: the thought is dropped
// from the buffer instead. It reports whether it painted anything.
func (d *Document) SyncLiveThoughtStep(opts Options) bool {
	thought := opts.Thought
	if !thought.Active || thought.Text == "" {
		return false
	}
	s := toolsteps.Step{
		Kind:    toolsteps.KindThought,
		Dur:     time.Since(thought.Since),
		Detail:  thought.Text,
		Running: true,
	}
	lines := toolsteps.SingleLines(s, true, opts.Width)
	if opts.Expanded {
		if d.LiveThoughtRunIdx >= 0 && d.LiveThoughtRunIdx < len(d.Runs) {
			r := &d.Runs[d.LiveThoughtRunIdx]
			oldEnd := r.Start + r.Lines
			if r.Start >= 0 && oldEnd <= len(d.Lines) {
				delta := len(lines) - r.Lines
				d.Lines = ReplaceRange(d.Lines, r.Start, oldEnd, lines)
				r.Lines = len(lines)
				r.Step = s
				if delta != 0 {
					for j := d.LiveThoughtRunIdx + 1; j < len(d.Runs); j++ {
						d.Runs[j].Start += delta
					}
					for j := range d.UserBlocks {
						if d.UserBlocks[j].Start >= oldEnd {
							d.UserBlocks[j].Start += delta
						}
					}
					if d.ReplyStart >= oldEnd {
						d.ReplyStart += delta
					}
				}
				return true
			}
		}
		start := len(d.Lines)
		d.Lines = append(d.Lines, lines...)
		d.Runs = append(d.Runs, LiveStepRun{
			Start: start,
			Lines: len(lines),
			Step:  s,
		})
		d.LiveThoughtRunIdx = len(d.Runs) - 1
		return true
	}
	if d.LiveThoughtRunIdx >= 0 && d.LiveThoughtRunIdx < len(d.Runs) {
		r := &d.Runs[d.LiveThoughtRunIdx]
		oldEnd := r.Start + r.Lines
		if r.Start >= 0 && oldEnd <= len(d.Lines) {
			delta := -r.Lines
			d.Lines = ReplaceRange(d.Lines, r.Start, oldEnd, nil)
			idx := d.LiveThoughtRunIdx
			d.Runs = append(d.Runs[:idx], d.Runs[idx+1:]...)
			d.LiveThoughtRunIdx = -1
			if delta != 0 {
				for j := idx; j < len(d.Runs); j++ {
					d.Runs[j].Start += delta
				}
				for j := range d.UserBlocks {
					if d.UserBlocks[j].Start >= oldEnd {
						d.UserBlocks[j].Start += delta
					}
				}
				if d.ReplyStart >= oldEnd {
					d.ReplyStart += delta
				}
			}
			return true
		}
	}
	return false
}

// FinishLiveThinking closes the turn's live thought: the running row
// becomes the finished thought.
//
// The options carry the thought that was *running*: the caller takes
// them before the turn's thought ends. A thought with no text closes
// the run without painting one.
//
// It reports whether it painted anything.
func (d *Document) FinishLiveThinking(opts Options) bool {
	thought := opts.Thought
	if !thought.Active {
		return false
	}
	body := thought.Text
	if body == "" {
		d.LiveThoughtRunIdx = -1
		return false
	}
	s := toolsteps.Step{
		Kind:    toolsteps.KindThought,
		Dur:     time.Since(thought.Since),
		Detail:  body,
		Running: false,
	}
	exp := opts.Expanded
	lines := toolsteps.SingleLines(s, exp, opts.Width)
	if d.LiveThoughtRunIdx >= 0 && d.LiveThoughtRunIdx < len(d.Runs) {
		r := &d.Runs[d.LiveThoughtRunIdx]
		oldEnd := r.Start + r.Lines
		if r.Start >= 0 && oldEnd <= len(d.Lines) {
			delta := len(lines) - r.Lines
			d.Lines = ReplaceRange(d.Lines, r.Start, oldEnd, lines)
			r.Lines = len(lines)
			r.Step = s
			if delta != 0 {
				for j := d.LiveThoughtRunIdx + 1; j < len(d.Runs); j++ {
					d.Runs[j].Start += delta
				}
				for j := range d.UserBlocks {
					if d.UserBlocks[j].Start >= oldEnd {
						d.UserBlocks[j].Start += delta
					}
				}
				if d.ReplyStart >= oldEnd {
					d.ReplyStart += delta
				}
			}
			return true
		}
	}
	start := len(d.Lines)
	d.Lines = append(d.Lines, lines...)
	d.Runs = append(d.Runs, LiveStepRun{
		Start: start,
		Lines: len(lines),
		Step:  s,
	})
	d.LiveThoughtRunIdx = len(d.Runs) - 1
	return true
}

// RerenderLiveSteps repaints the in-flight step rows: the live thought
// and the tool batches the model is still running.
//
// It reports whether the buffer's live rows need a re-wrap.
func (d *Document) RerenderLiveSteps(opts Options) bool {
	changed := false
	if opts.Thought.Active {
		changed = d.SyncLiveThoughtStep(opts)
	} else if d.LiveThoughtRunIdx >= 0 && d.LiveThoughtRunIdx < len(d.Runs) {
		r := &d.Runs[d.LiveThoughtRunIdx]
		if r.Step.Kind == toolsteps.KindThought && r.Start >= 0 && r.Start+r.Lines <= len(d.Lines) {
			exp := opts.Expanded
			lines := toolsteps.SingleLines(r.Step, exp, opts.Width)
			delta := len(lines) - r.Lines
			d.Lines = ReplaceRange(d.Lines, r.Start, r.Start+r.Lines, lines)
			r.Lines = len(lines)
			if delta != 0 {
				for j := d.LiveThoughtRunIdx + 1; j < len(d.Runs); j++ {
					d.Runs[j].Start += delta
				}
				for j := range d.UserBlocks {
					if d.UserBlocks[j].Start >= r.Start+r.Lines {
						d.UserBlocks[j].Start += delta
					}
				}
				if d.ReplyStart >= r.Start+r.Lines {
					d.ReplyStart += delta
				}
			}
		}
	}
	if len(d.Runs) == 0 {
		return changed
	}
	verb := opts.Verbosity
	exp := opts.Expanded
	batches := CollectLiveStepBatches(d.Runs)
	for bi := len(batches) - 1; bi >= 0; bi-- {
		b := batches[bi]
		end := d.Runs[b.Last].Start + d.Runs[b.Last].Lines
		if b.Start < 0 || end > len(d.Lines) {
			continue
		}
		lines := toolsteps.RenderBlock(b.Steps, toolsteps.RenderOptions{
			Expanded:  exp,
			Width:     opts.Width,
			Verbosity: verb,
		})
		delta := len(lines) - (end - b.Start)
		d.Lines = ReplaceRange(d.Lines, b.Start, end, lines)
		if delta != 0 {
			for j := b.Last + 1; j < len(d.Runs); j++ {
				d.Runs[j].Start += delta
			}
			for j := range d.UserBlocks {
				if d.UserBlocks[j].Start >= end {
					d.UserBlocks[j].Start += delta
				}
			}
			if d.ReplyStart >= end {
				d.ReplyStart += delta
			}
		}
		for j := range b.Steps {
			idx := b.Last - len(b.Steps) + 1 + j
			if idx >= 0 && idx < len(d.Runs) {
				d.Runs[idx].Lines = len(lines)
			}
		}
	}
	return true
}

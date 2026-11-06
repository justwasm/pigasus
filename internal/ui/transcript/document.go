// document.go — the transcript's own mechanics.
//
// The host (internal/cliapp) owns the response *buffer*: it decides
// when to re-render, when to reflow and when to seed a conversation.
// This file owns the mechanics that *change* that buffer: the range
// replacement, the blank row between two rows, the block index, the
// live step batches and the turn's finalisation.
//
// The host keeps its rows in its own field and hands them over: a
// Document is the shape the mechanics work on, and the host takes the
// result back. The host's facts — the width, the verbosity, whether
// the blocks are expanded and whether the turn said anything at all —
// travel in Options.
//
// Stdlib-only plus internal/ui/mdrender, internal/ui/toolsteps and the
// diff action's ANSI stripper. No dependency on the TUI host.
package transcript

import (
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
)

// Options are the render facts the transcript's mechanics need.
//
// The host owns them: the width and the height are the terminal's, the
// verbosity and the expansion are the user's, the emptiness of the turn
// is the turn engine's verdict, and the live thought is the turn's.
type Options struct {
	// Width is the terminal width the rows are rendered for.
	Width int
	// Height is the terminal height the rows are rendered for.
	Height int
	// Verbosity is the active verbosity level.
	Verbosity toolsteps.Verbosity
	// Expanded reports whether the tool-step blocks are drawn
	// expanded.
	Expanded bool
	// EmptyTurn reports that the turn produced no assistant text
	// worth showing, so the reply block is left out.
	EmptyTurn bool
	// Streaming reports that the turn is streaming a reply.
	Streaming bool
	// ViewportHeight reports the height of the transcript's visible
	// window, in rows.
	//
	// The repaint engine asks for it only when it has to decide how
	// many of the blocks it repaints, so the host may answer it with
	// a lazy lookup. A nil answer means "the whole transcript counts
	// as visible".
	ViewportHeight func() int
	// Thought is the turn's live thought.
	Thought LiveThought
}

// LiveThought is the turn's live thought: the text the model is
// reasoning with, and the moment the reasoning started.
type LiveThought struct {
	// Active reports that the turn is thinking.
	Active bool
	// Text is the thought's own text.
	Text string
	// Since is the moment the thought started.
	Since time.Time
}

// Document is the transcript's own state: the rows it draws and the
// indexes into them.
//
// The host owns the rows; the document is the shape the mechanics work
// on. A host that keeps its rows in its own field builds a document
// around them, hands it to a mechanic, and takes the result back.
type Document struct {
	// Lines is the row buffer the transcript draws.
	Lines []string
	// Blocks indexes the tool-step blocks inside Lines.
	Blocks []Block
	// Assistants indexes the completed assistant replies inside Lines.
	Assistants []AssistantBlock
	// UserBlocks indexes the user prompts inside Lines.
	UserBlocks []UserBlock
	// Runs indexes the in-flight step rows inside Lines.
	Runs []LiveStepRun
	// LiveStart is the row the turn's live content starts at.
	LiveStart int
	// ReplyStart is the row the streaming reply starts at, or -1 when
	// there is none.
	ReplyStart int
	// LiveThoughtRunIdx is the index of the live thought run, or -1
	// when there is none.
	LiveThoughtRunIdx int
	// LiveTextLines is the height of the streaming reply.
	LiveTextLines int
	// LiveReplyBuf is the streaming reply's own text.
	LiveReplyBuf string
	// StreamedText reports whether the turn streamed any text.
	StreamedText bool
}

// ReplaceRange replaces the rows [start, end) of lines with rows and
// returns the new buffer.
//
// A start outside the buffer leaves the buffer alone; an end before
// the start or past the buffer's end is clamped to the buffer's
// length.
func ReplaceRange(lines []string, start, end int, rows []string) []string {
	if start < 0 || start > len(lines) {
		return lines
	}
	if end < start || end > len(lines) {
		end = len(lines)
	}
	tail := append([]string(nil), lines[end:]...)
	lines = append(lines[:start], rows...)
	lines = append(lines, tail...)
	return lines
}

// BlankBefore appends a blank row when the last row of lines carries
// content, and returns the buffer.
//
// The blank row is the gap the reference puts between two rows of the
// conversation; a row that is already blank needs no second one.
func BlankBefore(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	if strings.TrimSpace(diffaction.StripANSI(lines[len(lines)-1])) != "" {
		return append(lines, "")
	}
	return lines
}

// DeduplicateBlocks sorts the blocks by the row they start at and drops
// the duplicates: two blocks that start at the same row are the same
// block, and the one that carries the steps and the token metrics wins.
func DeduplicateBlocks(blocks []Block) []Block {
	if len(blocks) <= 1 {
		return blocks
	}
	sort.SliceStable(blocks, func(i, j int) bool {
		return blocks[i].Start < blocks[j].Start
	})
	out := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		if n := len(out); n > 0 && out[n-1].Start == b.Start {
			if len(b.Steps) >= len(out[n-1].Steps) && b.Metrics.HasTokens() {
				out[n-1] = b
			}
			continue
		}
		out = append(out, b)
	}
	return out
}

// LiveStepBatch is a group of live step runs that belong to one tool
// batch: the calls the model emitted in the same assistant message.
type LiveStepBatch struct {
	// Last is the index of the run the batch ends at.
	Last int
	// Start is the row the batch starts at.
	Start int
	// Steps are the batch's steps, in paint order.
	Steps []toolsteps.Step
}

// CollectLiveStepBatches groups the live step runs by their tool
// batch. A thought run belongs to no batch, so it is left out.
func CollectLiveStepBatches(runs []LiveStepRun) []LiveStepBatch {
	var batches []LiveStepBatch
	for i, r := range runs {
		if r.Step.Kind == toolsteps.KindThought {
			continue
		}
		if n := len(batches); n > 0 && batches[n-1].Steps[0].Batch == r.Step.Batch {
			batches[n-1].Last = i
			batches[n-1].Steps = append(batches[n-1].Steps, r.Step)
			continue
		}
		batches = append(batches, LiveStepBatch{Last: i, Start: r.Start, Steps: []toolsteps.Step{r.Step}})
	}
	return batches
}

// ReplaceLiveStepBatches replaces the rows of every batch with the
// rendered block, and records a block for each.
//
// The batches are replaced back to front: a batch's rows end where the
// next one starts, so the later ones are repainted first.
func (d *Document) ReplaceLiveStepBatches(batches []LiveStepBatch, metrics toolsteps.TokenMetrics, opts Options) {
	for bi := len(batches) - 1; bi >= 0; bi-- {
		b := batches[bi]
		end := d.Runs[b.Last].Start + d.Runs[b.Last].Lines
		if b.Start < 0 || end > len(d.Lines) {
			continue
		}
		lines := toolsteps.RenderBlock(b.Steps, toolsteps.RenderOptions{
			Expanded:     opts.Expanded,
			Width:        opts.Width,
			Verbosity:    opts.Verbosity,
			TokenMetrics: metrics,
		})
		delta := len(lines) - (end - b.Start)
		d.Lines = ReplaceRange(d.Lines, b.Start, end, lines)
		d.Blocks = append(d.Blocks, NewBlock(Block{
			Start:    b.Start,
			Lines:    len(lines),
			Steps:    b.Steps,
			Metrics:  metrics,
			Expanded: opts.Expanded,
		}, lines, opts.Width, opts.Verbosity))
		if delta != 0 {
			for j := range d.Runs {
				if d.Runs[j].Start >= end {
					d.Runs[j].Start += delta
				}
			}
			for j := range d.UserBlocks {
				if d.UserBlocks[j].Start >= end {
					d.UserBlocks[j].Start += delta
				}
			}
			if d.ReplyStart >= end {
				d.ReplyStart += delta
			}
			if d.LiveStart >= end {
				d.LiveStart += delta
			}
		}
	}
}

// FinaliseLiveTurn turns the turn's in-flight step rows into the
// transcript's blocks.
//
// It reports whether the turn's live rows were finalised into blocks:
// a turn that only thought has nothing to finalise, and a turn that
// never ran leaves the transcript alone.
func (d *Document) FinaliseLiveTurn(msg toolsteps.TurnDoneMsg, opts Options) bool {
	hasTools := false
	for _, r := range d.Runs {
		if r.Step.Kind != toolsteps.KindThought {
			hasTools = true
			break
		}
	}
	if !hasTools && len(d.Runs) == 0 {
		return false
	}
	if !hasTools {
		d.Runs = nil
		d.LiveThoughtRunIdx = -1
		return false
	}
	d.finaliseLeadingThought(msg, opts)
	batches := CollectLiveStepBatches(d.Runs)
	d.ReplaceLiveStepBatches(batches, msg.TokenMetrics, opts)
	d.Blocks = DeduplicateBlocks(d.Blocks)
	d.Runs = nil
	d.LiveThoughtRunIdx = -1
	return true
}

// finaliseLeadingThought turns the turn's live thought row into the
// turn's first block.
//
// The reference shows one thought row per turn: the thought the model
// streamed is the first row of the turn's block, not a row of its own.
func (d *Document) finaliseLeadingThought(msg toolsteps.TurnDoneMsg, opts Options) {
	hasThoughtStep := len(msg.Steps) > 0 && msg.Steps[0].Kind == toolsteps.KindThought
	if !hasThoughtStep {
		return
	}
	thought := msg.Steps[:1]
	lines := toolsteps.RenderBlock(thought, toolsteps.RenderOptions{
		Expanded:     opts.Expanded,
		Width:        opts.Width,
		Verbosity:    opts.Verbosity,
		TokenMetrics: msg.TokenMetrics,
	})
	at := d.LiveStart
	if d.LiveThoughtRunIdx >= 0 && d.LiveThoughtRunIdx < len(d.Runs) {
		r := d.Runs[d.LiveThoughtRunIdx]
		d.Lines = ReplaceRange(d.Lines, r.Start, r.Start+r.Lines, lines)
		delta := len(lines) - r.Lines
		d.Runs = append(d.Runs[:d.LiveThoughtRunIdx], d.Runs[d.LiveThoughtRunIdx+1:]...)
		d.LiveThoughtRunIdx = -1
		if delta != 0 {
			for j := range d.Runs {
				d.Runs[j].Start += delta
			}
			if d.ReplyStart >= at {
				d.ReplyStart += delta
			}
		}
	} else {
		d.Lines = ReplaceRange(d.Lines, at, at, lines)
		for j := range d.Runs {
			d.Runs[j].Start += len(lines)
		}
		if d.ReplyStart >= at {
			d.ReplyStart += len(lines)
		}
	}
	d.Blocks = append(d.Blocks, NewBlock(Block{
		Start:    at,
		Lines:    len(lines),
		Steps:    thought,
		Metrics:  msg.TokenMetrics,
		Expanded: opts.Expanded,
	}, lines, opts.Width, opts.Verbosity))
}

// FinaliseTurnReply renders the finished turn into the transcript.
//
// It reports whether the turn's live rows were finalised into blocks:
// the caller repaints the transcript when they were.
func (d *Document) FinaliseTurnReply(msg toolsteps.TurnDoneMsg, opts Options) bool {
	if !d.FinaliseLiveTurn(msg, opts) {
		var replyLines []string
		if !opts.EmptyTurn {
			styled := mdrender.RenderMarkdown(msg.Reply, opts.Width)
			replyLines = strings.Split(styled, "\n")
		}
		block := toolsteps.RenderBlock(msg.Steps, toolsteps.RenderOptions{
			Expanded:     opts.Expanded,
			Width:        opts.Width,
			Verbosity:    opts.Verbosity,
			TokenMetrics: msg.TokenMetrics,
		})
		final := make([]string, 0, len(block)+len(replyLines)+1)
		pad := d.LiveStart > 0 && strings.TrimSpace(diffaction.StripANSI(d.Lines[d.LiveStart-1])) != ""
		if pad {
			final = append(final, "")
		}
		final = append(final, block...)
		replyStart := d.LiveStart + len(final)
		final = append(final, replyLines...)
		d.Lines = ReplaceRange(d.Lines, d.LiveStart, len(d.Lines), final)
		if len(msg.Steps) > 0 {
			start := d.LiveStart
			if pad {
				start++
			}
			d.Blocks = append(d.Blocks, NewBlock(Block{
				Start:    start,
				Lines:    len(block),
				Steps:    msg.Steps,
				Metrics:  msg.TokenMetrics,
				Expanded: opts.Expanded,
			}, block, opts.Width, opts.Verbosity))
			d.Blocks = DeduplicateBlocks(d.Blocks)
		}
		if len(replyLines) > 0 {
			d.Assistants = append(d.Assistants, AssistantBlock{
				Start: replyStart,
				Lines: len(replyLines),
				Raw:   msg.Reply,
			})
		}
		return false
	}
	if !d.StreamedText && !opts.EmptyTurn {
		styled := mdrender.RenderMarkdown(msg.Reply, opts.Width)
		lines := strings.Split(styled, "\n")
		start := len(d.Lines)
		d.Lines = append(d.Lines, lines...)
		d.Assistants = append(d.Assistants, AssistantBlock{
			Start: start,
			Lines: len(lines),
			Raw:   msg.Reply,
		})
	} else if d.StreamedText && !opts.EmptyTurn {
		if d.ReplyStart >= 0 && d.LiveTextLines > 0 {
			raw := msg.Reply
			if raw == "" {
				raw = d.LiveReplyBuf
			}
			styled := mdrender.RenderMarkdown(raw, opts.Width)
			lines := strings.Split(styled, "\n")
			d.Lines = ReplaceRange(d.Lines, d.ReplyStart, d.ReplyStart+d.LiveTextLines, lines)
			d.Assistants = append(d.Assistants, AssistantBlock{
				Start: d.ReplyStart,
				Lines: len(lines),
				Raw:   raw,
			})
		}
	}
	return true
}

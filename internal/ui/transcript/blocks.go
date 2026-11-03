// Package transcript owns the transcript's row vocabulary.
//
// The transcript is the conversation's own record: the user's own
// prompt rows, the rule between two turns, and the blocks that hold a
// turn's tool steps. This package owns that vocabulary — where a block
// lives inside the response buffer, how the block re-renders itself
// when the verbosity, the width or the expansion changes, and what a
// user prompt row looks like.
//
// The host (internal/cliapp) owns the response *buffer*: it decides
// when to re-render, when to reflow and when to seed a conversation.
// What a row is belongs here.
//
// The block types answer a question the step model does not: "the
// block that starts at row N and is M rows tall — re-render it". That
// is a fact about the transcript, not about what a step *is*, so it
// lives here while the step itself lives in internal/ui/toolsteps.
//
// Stdlib-only plus internal/ui/toolsteps, internal/ui/tuilib and the
// palette. No dependency on the TUI host.
package transcript

import (
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

// BashBlock is a bash-mode block. Its rows are re-rendered from the
// full body so the truncated tail can be expanded and collapsed the
// same way the tool blocks do, so the transcript asks for the rows
// through this interface rather than knowing the host's block shape.
type BashBlock interface {
	// Lines renders the block's rows.
	Lines(expanded bool, height int) []string
}

// Block records where a turn's tool-step block lives inside the
// response buffer, so a re-render can repaint it in place. Start and
// Lines delimit the block; Steps is the data needed to render it at
// either verbosity.
type Block struct {
	// Start is the block's first row in the response buffer.
	Start int
	// Lines is the block's height, in rows.
	Lines int
	// Steps is the data the block renders from.
	Steps []toolsteps.Step
	// Bash is set for a bash-mode block: its rows are re-rendered from
	// the full body so the truncated tail can be expanded and
	// collapsed, the same way the tool blocks do.
	Bash BashBlock
	// Metrics records the turn's token metrics for the low-verbosity
	// rendering.
	Metrics toolsteps.TokenMetrics

	// Expanded records whether the rows currently in the buffer reflect
	// the expanded view.
	Expanded bool

	// cachedCol and cachedExp cache the rendered rows for the collapsed
	// and the expanded views. They are the block's own state: the
	// caller hands the rows it already painted to NewBlock and the
	// block keeps them until the width or the verbosity moves.
	cachedCol  []string
	cachedExp  []string
	cachedColW int
	cachedExpW int
	cachedColV toolsteps.Verbosity
	cachedExpV toolsteps.Verbosity
}

// NewBlock returns b with its render cache seeded with the rows the
// caller already painted.
//
// The cache slot is picked by b.Expanded: the collapsed rows go into
// the collapsed slot and the expanded rows into the expanded one, the
// way the host's three populating sites filled them.
func NewBlock(b Block, rendered []string, width int, verb toolsteps.Verbosity) Block {
	if b.Expanded {
		b.cachedExp = rendered
		b.cachedExpW = width
		b.cachedExpV = verb
		return b
	}
	b.cachedCol = rendered
	b.cachedColW = width
	b.cachedColV = verb
	return b
}

// NeedsRender reports whether the block needs to be re-rendered
// because its expansion, width, or verbosity changed.
func (b *Block) NeedsRender(expanded bool, width int, verb toolsteps.Verbosity) bool {
	if b.Expanded != expanded {
		return true
	}
	if expanded {
		return b.cachedExp == nil || b.cachedExpW != width || b.cachedExpV != verb
	}
	return b.cachedCol == nil || b.cachedColW != width || b.cachedColV != verb
}

// Render returns the block's rows under the given expansion, width,
// height and verbosity.
//
// A bash-mode block renders from its own body; a tool-step block
// renders from its steps, through the cache the block keeps for the
// width and the verbosity it was last painted at.
func (b *Block) Render(expanded bool, width, height int, verb toolsteps.Verbosity) []string {
	if b.Bash != nil {
		return b.Bash.Lines(expanded, height)
	}
	if expanded {
		if b.cachedExp != nil && b.cachedExpW == width && b.cachedExpV == verb {
			return b.cachedExp
		}
		lines := toolsteps.RenderBlock(b.Steps, toolsteps.RenderOptions{
			Expanded:     true,
			Width:        width,
			Verbosity:    verb,
			TokenMetrics: b.Metrics,
		})
		b.cachedExp = lines
		b.cachedExpW = width
		b.cachedExpV = verb
		return lines
	}
	if b.cachedCol != nil && b.cachedColW == width && b.cachedColV == verb {
		return b.cachedCol
	}
	lines := toolsteps.RenderBlock(b.Steps, toolsteps.RenderOptions{
		Expanded:     false,
		Width:        width,
		Verbosity:    verb,
		TokenMetrics: b.Metrics,
	})
	b.cachedCol = lines
	b.cachedColW = width
	b.cachedColV = verb
	return lines
}

// UserBlock records where a user prompt lives inside the response
// buffer, so a window reflow and a live colour scheme change can
// re-render it in place.
type UserBlock struct {
	// Start is the prompt's first row in the response buffer.
	Start int
	// Lines is the prompt's height, in rows.
	Lines int
	// Raw is the prompt's own text, before it was wrapped and styled.
	Raw string
}

// AssistantBlock records where a completed assistant markdown reply
// lives inside the response buffer, so a live colour scheme change can
// re-render it in place.
type AssistantBlock struct {
	// Start is the reply's first row in the response buffer.
	Start int
	// Lines is the reply's height, in rows.
	Lines int
	// Raw is the reply's own markdown, before it was styled.
	Raw string
}

// LiveStepRun records where a single in-flight step was painted in the
// response buffer. Until the turn is done, a running turn's steps live
// only as rows in the transcript (they are not a Block yet), so a
// re-render needs its own index to repaint them in place: one run per
// tool step, in paint order. Cleared on prompt submit and when the turn
// finalises into a block.
type LiveStepRun struct {
	// Start is the step's first row in the response buffer.
	Start int
	// Lines is the step's height, in rows.
	Lines int
	// Step is the step itself.
	Step toolsteps.Step
}

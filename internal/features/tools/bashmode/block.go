// block.go — the bash-mode ("! mode") block: the transcript's row holder.
//
// 【一手】 agy 1.4.4, byte-level pty captures 2026-10-22
// (docs/cycles/bash-mode/). The rows the reference prints for a bash-mode
// command:
//
//	● ! echo hello
//	  ⎿  hello
//
//	● ! sh -c 'echo out; echo err >&2; exit 3'
//	  ⎿  out
//	err
//
//	● ! true                ← empty output prints no detail row at all
//
// Measured branches:
//
//   - Success paints a green ● with the body in #586E75; failure paints a
//     red ● and a #DC322F body. The "  ⎿  " prefix is blue bold either way.
//   - An interrupted command keeps its hollow `○` head: the preemption path
//     never repaints it (【一手】 agy 1.4.5, `!top` then `!pwd`).
//   - An empty body prints no detail row at all: the block is the head row
//     and the trailing blank row.
//
// The block owns its rows; the host owns the process that fills it — the
// exec, the queue, the preemption and the cancel.
package bashmode

// Block is one bash-mode block: the command the reference printed, the
// body it produced, and the two flags that pick the bullet and the body
// colour.
type Block struct {
	// Cmd is the command, without the leading "!".
	Cmd string
	// Body is the command's merged stdout/stderr, already trimmed.
	Body string
	// Failed marks a command that exited non-zero.
	Failed bool
	// Running marks a block whose command was still running when the
	// block was written down — an interrupted head keeps its hollow ○.
	Running bool
}

// Lines renders the block's rows: the head row, the body rows, and the
// block's trailing blank row.
//
// The transcript re-renders the block from its own body when the
// expansion or the height changes, so the rows are computed here and not
// kept as painted text.
func (b *Block) Lines(expanded bool, height int) []string {
	return BlockLines(b.Cmd, b.Body, b.Failed, b.Running, expanded, height)
}

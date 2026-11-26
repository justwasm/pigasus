package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/features/tools/bashmode"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/procgroup"
)

// tui_bash_mode.go — the reference's bash mode ("! mode"): the process.
//
// 【一手】 agy 1.4.4, byte-level pty captures 2026-10-22
// (docs/cycles/bash-mode/). What the reference does:
//
//	> !                     ← typing `!` on an empty composer arms the mode
//	 activated bash mode · esc to cancel
//
// The command never reaches the model. The block's own rows — the head
// row, the body, the failed-command shape, the hollow `○` of an
// interrupted head — belong to internal/features/tools/bashmode, which
// renders them.
//
// Measured branches:
//
//   - `!` arms the mode only when the buffer is already empty. ` !echo hi`
//     (leading space) is an ordinary prompt and goes to the model.
//   - The `!` is consumed by the mode; the buffer holds the command alone.
//     The composer draws a bold blue `!` + space as the prompt glyph.
//   - Enter with an empty command just leaves the mode (nothing is printed).
//   - Esc leaves the mode and clears the buffer.
//   - The command runs through the platform's default shell in the
//     workspace (zsh -c on darwin — the same DefaultShellArgv the
//     run_command tool uses).
//   - stdout and stderr are merged; trailing newlines are dropped.
//   - A failed command with no output prints the Go error text
//     ("exit status 3"); a failed command that *did* print something
//     prints only that output — there is no status line.
//   - The command behaves like a local turn: the spinner reads
//     "Generating...", the status row reads "esc to cancel", and Esc
//     cancels it — leaving the head row and no body.
//   - A command submitted while a *model* turn streams is queued and shown
//     as "▸ <cmd>" above the input (no "(alt+m …)" suffix, unlike queued
//     prompts); it runs when the turn ends.
//   - A command submitted while another *shell* command runs preempts it:
//     the old process tree is stopped, its partial output is kept as the
//     block's body, and the new command starts at once (【一手】 agy 1.4.5,
//     `!top` then `!pwd`).
//   - Esc and Ctrl+C cancel a running command. The block keeps only the head
//     row (no partial body); Ctrl+C additionally arms the two-step exit
//     confirmation on the same press.

// bashDoneMsg is the host's message: it carries the host's generation
// counter (the stale-completion guard) and the exit status the implicit
// trajectory records, so it stays with the host's pump. The block the
// message fills belongs to internal/features/tools/bashmode.
type bashDoneMsg struct {
	gen       int
	cmd       string
	output    string
	failed    bool
	cancelled bool
	// code is the command's exit status. It is only meaningful when
	// hasCode is set: a canceled command has no exit status, and the
	// reference's record leaves the field unset for one.
	code    int
	hasCode bool
}

// The bash block is the transcript's bash block: the transcript asks it
// for its rows through the interface, and the block itself — its state
// and its rows — belongs to internal/features/tools/bashmode.
var _ transcript.BashBlock = (*bashmode.Block)(nil)

// startBashCommand runs the command as a local turn: it appends the
// in-flight head row, flips the TUI into the "streaming" state (spinner +
// "esc to cancel"), and returns the tea.Cmd that performs the exec and the
// spinner tick. The caller must have already cleared the composer and the
// bash-mode flag.
func (m *RootModel) startBashCommand(cmd string) tea.Cmd {
	m.session.store.BashGen++
	gen := m.session.store.BashGen
	m.session.store.BashCmd = cmd
	m.conversation.response = transcript.BlankBefore(m.conversation.response)
	m.session.store.BashRowIdx = len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, bashmode.HeadRow(cmd, false, true))
	m.conversation.streaming = true
	m.conversation.thinking = false
	m.conversation.activeTool = ""
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.clearLastTurnOutcome()
	out := &tools.SafeBuffer{}
	m.session.store.BashRun = out
	ctx, cancel := context.WithCancel(context.Background())
	m.session.store.SetBashCancel(cancel)
	dir := m.session.cwd
	run := func() tea.Msg {
		return execBashCommand(ctx, dir, cmd, out, gen)
	}
	tick := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(run, tick)
}

// execBashCommand runs one command through the platform shell with merged
// stdout/stderr. The reference prints "exit status N" only when a failed
// command produced no output, which is exactly Go's *exec.ExitError text.
//
// Cancel kills the whole process group, not just the shell: `sh -c 'sleep 30'`
// leaves an orphan holding the stdout pipe, and killing only the shell makes
// exec.Cmd.Wait block until the grandchild exits on its own — the turn (and the
// spinner) would outlive the cancel by however long the orphan runs.
func execBashCommand(ctx context.Context, dir, command string, out *tools.SafeBuffer, gen int) bashDoneMsg {
	argv := shell.DefaultShellArgv(command)
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.Stdout = out
	c.Stderr = out
	procgroup.Set(c)
	c.Cancel = func() error { return procgroup.Kill(c) }
	c.WaitDelay = 800 * time.Millisecond
	err := c.Run()
	if ctx.Err() != nil {
		// Esc / Ctrl+C: the reference finalises the block with no body and
		// never prints an "Interrupted" row. The preemption path snapshots the
		// buffer before cancelling, so dropping it here is correct.
		return bashDoneMsg{gen: gen, cmd: command, cancelled: true}
	}
	text := bashmode.TrimOutput(out.String())
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		code, hasCode := bashExitCode(err)
		return bashDoneMsg{gen: gen, cmd: command, output: text, failed: true, code: code, hasCode: hasCode}
	}
	return bashDoneMsg{gen: gen, cmd: command, output: text, hasCode: true}
}

// bashExitCode returns the command's exit status. The second result is false
// when the command has no exit status at all — a command that never ran, or
// one that was canceled.
func bashExitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

// finishBashCommand applies a bashDoneMsg: repaint the head row in place,
// append the body (if any) plus the block's trailing blank row. A cancelled
// command keeps only the head row — the reference never shows partial output
// for Esc / Ctrl+C.
func (m *RootModel) finishBashCommand(msg bashDoneMsg) {
	m.session.store.ClearInFlightBash()
	m.conversation.streaming = false
	b := &bashmode.Block{Cmd: msg.cmd, Failed: msg.failed}
	if !msg.cancelled {
		b.Body = msg.output
	}
	m.placeBashBlock(b)
	m.session.store.BashRowIdx = -1
	// The reference records the command in its implicit trajectory: this is
	// the only place a `!` command is written down.
	m.recordImplicitStep(msg.cmd, msg.output, msg.code, msg.hasCode, msg.cancelled)
}

// placeBashBlock paints a finished block. When an in-flight head row exists it
// is rebuilt in place and the rest appended after it; otherwise the block is
// appended to the transcript. Every block is recorded in m.conversation.blocks so ctrl+o
// can re-render it from the full body.
func (m *RootModel) placeBashBlock(b *bashmode.Block) {
	rows := b.Lines(m.isExpanded(), m.viewport.height)
	if m.session.store.BashRowIdx >= 0 && m.session.store.BashRowIdx < len(m.conversation.response) {
		m.conversation.response[m.session.store.BashRowIdx] = rows[0]
		m.conversation.response = append(m.conversation.response, rows[1:]...)
		m.conversation.blocks = append(m.conversation.blocks, transcript.Block{Start: m.session.store.BashRowIdx, Lines: len(rows), Bash: b})
		return
	}
	m.conversation.response = transcript.BlankBefore(m.conversation.response)
	start := len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, rows...)
	m.conversation.blocks = append(m.conversation.blocks, transcript.Block{Start: start, Lines: len(rows), Bash: b})
}

// preemptBashCommand interrupts the running command the way the reference does
// when a *new* shell command is submitted while one is still running: the old
// process tree is stopped, whatever it had already written becomes its body,
// and the new command starts immediately instead of queueing.
//
// 【一手】 agy 1.4.5, `!sh -c 'echo first; sleep 30'` then `!echo queued2`: the
// first block keeps its hollow amber `○` head (never repainted) and gains
// `  ⎿  first`, then the second block runs to completion — all before any Esc.
func (m *RootModel) preemptBashCommand(next string) tea.Cmd {
	partial := ""
	if m.session.store.BashRun != nil {
		partial = bashmode.TrimOutput(m.session.store.BashRun.String())
	}
	cmd := m.session.store.BashCmd
	m.session.store.CancelBash()
	m.session.store.BashRun = nil
	m.placeBashBlock(&bashmode.Block{Cmd: cmd, Body: partial, Running: true})
	m.session.store.BashRowIdx = -1
	// The preempted command is recorded as canceled: the reference's
	// tracker finishes it when the process tree is stopped, and the step it
	// writes carries CORTEX_STEP_STATUS_CANCELED.
	m.recordImplicitStep(cmd, partial, 0, false, true)
	return m.startBashCommand(next)
}

// cancelBashCommand runs the Esc-cancel path for an in-flight command.
func (m *RootModel) cancelBashCommand() {
	m.session.store.CancelBash()
}

// applyHistoryEntry loads a recalled entry into the composer. Entries are
// stored with the leading "!" for shell commands (submitPrompt pushes
// "!"+cmd), so a recall re-arms the mode and strips the marker — the
// reference recalls "!echo hist" as "! echo hist" in bash mode.
// If the recalled entry meets the folding threshold (>= 16 lines or > 1000 runes),
// it is dynamically folded into [Pasted text #1 ...] matching reference agy.
func (m *RootModel) applyHistoryEntry(entry string) {
	m.session.store.BashMode = false
	if strings.HasPrefix(entry, "!") {
		m.session.store.BashMode = true
		entry = strings.TrimPrefix(entry, "!")
		entry = strings.TrimPrefix(entry, " ")
	}
	m.prompt.pastedBlocks = nil
	m.prompt.nextPasteID = 0

	lines := strings.Count(entry, "\n") + 1
	charCount := len([]rune(entry))
	if lines >= 16 || charCount > 1000 {
		m.prompt.pastedBlocks = make(map[string]string)
		m.prompt.nextPasteID = 1
		var placeholder string
		if lines >= 16 {
			placeholder = fmt.Sprintf("[Pasted text #1 +%d lines]", lines)
		} else {
			placeholder = fmt.Sprintf("[Pasted text #1 %d chars]", charCount)
		}
		m.prompt.pastedBlocks[placeholder] = entry
		entry = placeholder
	}

	m.prompt.input.SetValue(entry)
}

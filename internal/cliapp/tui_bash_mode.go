// tui_bash_mode.go — the reference's bash mode ("! mode").
//
// 【一手】 agy 1.4.4, byte-level pty captures 2026-10-22
// (docs/cycles/bash-mode/). What the reference does:
//
//	> !                     ← typing `!` on an empty composer arms the mode
//	 activated bash mode · esc to cancel
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
// The command never reaches the model. Measured branches:
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
//   - Success paints a green ● with the body in #586E75; failure paints a
//     red ● and a #DC322F body. The "  ⎿  " prefix is blue bold either way.
//   - The command behaves like a local turn: the spinner reads
//     "Generating...", the status row reads "esc to cancel", and Esc
//     cancels it — leaving the head row and no body.
//   - A command submitted while a *model* turn streams is queued and shown
//     as "▸ <cmd>" above the input (no "(alt+m …)" suffix, unlike queued
//     prompts); it runs when the turn ends.
//   - A command submitted while another *shell* command runs preempts it:
//     the old process tree is stopped, its partial output is kept as the
//     block's body, and the new command starts at once (【一手】 agy 1.4.5,
//     `!top` then `!pwd`). The interrupted head keeps its hollow `○`.
//   - Esc and Ctrl+C cancel a running command. The block keeps only the head
//     row (no partial body); Ctrl+C additionally arms the two-step exit
//     confirmation on the same press.
package cliapp

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/bashmode"
	"github.com/justwasm/pigasus/internal/procgroup"
	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/palette"
)

var (
	// The bash block's body colours follow the selected colour scheme;
	// refreshPaletteAliases re-reads them when the scheme changes.
	bashBodyOKOpen   = palette.WarningBodyOpen
	bashBodyFailOpen = palette.ToolRemoveBodyOpen
)

type bashOutput = tools.SafeBuffer

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

// bashHeadRow renders the transcript head row.
func bashHeadRow(cmd string, failed, running bool) string {
	return bashmode.HeadRow(cmd, failed, running)
}

// bashBodyRows renders the "  ⎿  " body block.
func bashBodyRows(lines []string, failed bool) []string {
	return bashmode.BodyRows(lines, failed)
}

func bashOutputRows(out string, failed bool) []string {
	return bashmode.OutputRows(out, failed)
}

func bashContRows(lines []string, failed bool) []string {
	return bashmode.ContRows(lines, failed)
}

func bashBodyCap(height int) int {
	return bashmode.BodyCap(height)
}

// bashBlock is one finished bash-mode block.
type bashBlock struct {
	cmd     string
	body    string
	failed  bool
	running bool
}

// bashBlockLines renders a finished bash block plus its trailing blank row.
func bashBlockLines(b *bashBlock, expanded bool, height int) []string {
	return bashmode.BlockLines(b.cmd, b.body, b.failed, b.running, expanded, height)
}

func bashMarkerColour(failed bool) string {
	return bashmode.MarkerColour(failed)
}

func bashPromptGlyph(inputView string) string {
	return bashmode.PromptGlyph(inputView, cBlueOpen)
}

// startBashCommand runs the command as a local turn: it appends the
// in-flight head row, flips the TUI into the "streaming" state (spinner +
// "esc to cancel"), and returns the tea.Cmd that performs the exec and the
// spinner tick. The caller must have already cleared the composer and the
// bash-mode flag.
func (m *runMode) startBashCommand(cmd string) tea.Cmd {
	m.bashGen++
	gen := m.bashGen
	m.bashCmd = cmd
	m.blankBefore()
	m.bashRowIdx = len(m.response)
	m.response = append(m.response, bashHeadRow(cmd, false, true))
	m.streaming = true
	m.thinking = false
	m.activeTool = ""
	m.anyToolRun = false
	m.spinFrame = 0
	m.clearLastTurnOutcome()
	out := &bashOutput{}
	m.bashRun = out
	ctx, cancel := context.WithCancel(context.Background())
	m.bashCancel = cancel
	dir := m.cwd
	run := func() tea.Msg {
		return execBashCommand(ctx, dir, cmd, out, gen)
	}
	tick := func() tea.Msg { return m.spinner.Tick() }
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
func execBashCommand(ctx context.Context, dir, command string, out *bashOutput, gen int) bashDoneMsg {
	argv := tools.DefaultShellArgv(command)
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.Stdout = out
	c.Stderr = out
	procgroup.Set(c)
	c.Cancel = func() error { return procgroup.Kill(c) }
	c.WaitDelay = 2 * time.Second
	err := c.Run()
	if ctx.Err() != nil {
		// Esc / Ctrl+C: the reference finalises the block with no body and
		// never prints an "Interrupted" row. The preemption path snapshots the
		// buffer before cancelling, so dropping it here is correct.
		return bashDoneMsg{gen: gen, cmd: command, cancelled: true}
	}
	text := trimBashOutput(out.String())
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

func trimBashOutput(s string) string { return bashmode.TrimOutput(s) }

// finishBashCommand applies a bashDoneMsg: repaint the head row in place,
// append the body (if any) plus the block's trailing blank row. A cancelled
// command keeps only the head row — the reference never shows partial output
// for Esc / Ctrl+C.
func (m *runMode) finishBashCommand(msg bashDoneMsg) {
	m.bashCancel = nil
	m.bashRun = nil
	m.bashCmd = ""
	m.streaming = false
	b := &bashBlock{cmd: msg.cmd, failed: msg.failed}
	if !msg.cancelled {
		b.body = msg.output
	}
	m.placeBashBlock(b)
	m.bashRowIdx = -1
	// The reference records the command in its implicit trajectory: this is
	// the only place a `!` command is written down.
	m.recordImplicitStep(msg.cmd, msg.output, msg.code, msg.hasCode, msg.cancelled)
}

// placeBashBlock paints a finished block. When an in-flight head row exists it
// is rebuilt in place and the rest appended after it; otherwise the block is
// appended to the transcript. Every block is recorded in m.blocks so ctrl+o
// can re-render it from the full body.
func (m *runMode) placeBashBlock(b *bashBlock) {
	rows := bashBlockLines(b, m.expanded, m.height)
	if m.bashRowIdx >= 0 && m.bashRowIdx < len(m.response) {
		m.response[m.bashRowIdx] = rows[0]
		m.response = append(m.response, rows[1:]...)
		m.blocks = append(m.blocks, turnBlock{start: m.bashRowIdx, lines: len(rows), bash: b})
		return
	}
	m.blankBefore()
	start := len(m.response)
	m.response = append(m.response, rows...)
	m.blocks = append(m.blocks, turnBlock{start: start, lines: len(rows), bash: b})
}

// preemptBashCommand interrupts the running command the way the reference does
// when a *new* shell command is submitted while one is still running: the old
// process tree is stopped, whatever it had already written becomes its body,
// and the new command starts immediately instead of queueing.
//
// 【一手】 agy 1.4.5, `!sh -c 'echo first; sleep 30'` then `!echo queued2`: the
// first block keeps its hollow amber `○` head (never repainted) and gains
// `  ⎿  first`, then the second block runs to completion — all before any Esc.
func (m *runMode) preemptBashCommand(next string) tea.Cmd {
	partial := ""
	if m.bashRun != nil {
		partial = trimBashOutput(m.bashRun.String())
	}
	cmd := m.bashCmd
	if m.bashCancel != nil {
		m.bashCancel()
		m.bashCancel = nil
	}
	m.bashRun = nil
	m.placeBashBlock(&bashBlock{cmd: cmd, body: partial, running: true})
	m.bashRowIdx = -1
	// The preempted command is recorded as canceled: the reference's
	// tracker finishes it when the process tree is stopped, and the step it
	// writes carries CORTEX_STEP_STATUS_CANCELED.
	m.recordImplicitStep(cmd, partial, 0, false, true)
	return m.startBashCommand(next)
}

// cancelBashCommand runs the Esc-cancel path for an in-flight command.
func (m *runMode) cancelBashCommand() {
	if m.bashCancel == nil {
		return
	}
	m.bashCancel()
	m.bashCancel = nil
}

// applyHistoryEntry loads a recalled entry into the composer. Entries are
// stored with the leading "!" for shell commands (submitPrompt pushes
// "!"+cmd), so a recall re-arms the mode and strips the marker — the
// reference recalls "!echo hist" as "! echo hist" in bash mode.
func (m *runMode) applyHistoryEntry(entry string) {
	m.bashMode = false
	if strings.HasPrefix(entry, "!") {
		m.bashMode = true
		entry = strings.TrimPrefix(entry, "!")
		entry = strings.TrimPrefix(entry, " ")
	}
	m.input.SetValue(entry)
}

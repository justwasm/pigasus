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
package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tools"
)

// bashBodyOKOpen / bashBodyFailOpen are the first-line body colours. The
// reference appends ";22" (normal intensity) to the first row only:
//
//	\x1b[38;2;88;110;117;22mhello\x1b[m     (success)
//	\x1b[38;2;220;50;47;22mout\x1b[m        (failure)
//
// Continuation rows drop the ;22 and reuse cSubtleOpen / cToolRemoveOpen.
const (
	bashBodyOKOpen   = "\x1b[38;2;88;110;117;22m"
	bashBodyFailOpen = "\x1b[38;2;220;50;47;22m"
)

// bashOutput is the mutex-guarded sink the child writes into. It exists so a
// preemption can snapshot what the command produced *before* it is killed: the
// reference keeps that partial output as the interrupted block's body (【一手】
// `!sh -c 'echo first; sleep 30'` then `!echo queued2` on agy 1.4.5 keeps
// `  ⎿  first` on the first block).
type bashOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *bashOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *bashOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// bashDoneMsg is delivered when a bash-mode command finishes (or is cancelled
// by Esc / Ctrl+C). cmd is carried so the in-flight head row can be repainted
// in place; gen lets a preempted command's late completion be ignored.
type bashDoneMsg struct {
	gen       int
	cmd       string
	output    string
	failed    bool
	cancelled bool
}

// bashHeadRow renders the transcript head row.
//
//	\x1b[38;2;181;137;0m○\x1b[m \x1b[38;2;38;139;210;1m! cmd\x1b[m   (running)
//	\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! cmd\x1b[m   (success)
//	\x1b[38;2;220;50;47m●\x1b[m \x1b[38;2;38;139;210;1m! cmd\x1b[m   (failure)
func bashHeadRow(cmd string, failed, running bool) string {
	var bullet string
	switch {
	case running:
		bullet = cToolBulletRunningOpen + "○" + "\x1b[m"
	case failed:
		bullet = cToolBulletErrOpen + "●" + "\x1b[m"
	default:
		bullet = cToolBulletOpen + "●" + "\x1b[m"
	}
	return bullet + " " + cWarningBlueOpen + "! " + cmd + "\x1b[m"
}

// bashOutputRows renders the "  ⎿  " body block. The reference indents only
// the first line — continuation lines start at column 0:
//
//	  ⎿  out
//	err
func bashBodyRows(lines []string, failed bool) []string {
	bodyOpen, bodyCont := bashBodyOKOpen, cSubtleOpen
	if failed {
		bodyOpen, bodyCont = bashBodyFailOpen, cToolRemoveOpen
	}
	rows := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			rows = append(rows, cWarningBlueOpen+"  ⎿  "+bodyOpen+line+"\x1b[m")
			continue
		}
		rows = append(rows, bodyCont+line+"\x1b[m")
	}
	return rows
}

func bashOutputRows(out string, failed bool) []string {
	return bashBodyRows(strings.Split(out, "\n"), failed)
}

// bashContRows renders continuation rows only (no "  ⎿  " prefix). The
// truncated form uses it for the kept tail: only the "<output +N lines>"
// marker carries the prefix.
func bashContRows(lines []string, failed bool) []string {
	bodyCont := cSubtleOpen
	if failed {
		bodyCont = cToolRemoveOpen
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, bodyCont+line+"\x1b[m")
	}
	return rows
}

// bashBodyCap is how many trailing body rows a collapsed bash block shows.
// 【一手】 the reference caps at half the terminal height: 20 rows at a 40-row
// pty, 25 at 50, 30 at 60 (`!seq 1 40` measured at all three heights).
func bashBodyCap(height int) int {
	if height <= 0 {
		height = 40
	}
	if n := height / 2; n > 0 {
		return n
	}
	return 1
}

// bashHintColour is the dim colour of both "(ctrl+o to expand)" and
// "(ctrl+o to collapse)"; each is a separate span after the last body row.
const bashHintColour = "\x1b[38;2;88;110;117m"

// bashBlock is one finished bash-mode block, kept so ctrl+o can re-render it
// with the full body or collapse it back to the truncated tail.
//
// 【一手】 collapsed, `!seq 1 200` at 120×40:
//
//	● ! seq 1 200
//	  ⎿  <output +180 lines>
//	181
//	…
//	200 (ctrl+o to expand)
//
// Expanded the marker disappears and the hint becomes "(ctrl+o to collapse)".
// A body of `height/2` lines or fewer never truncates and never shows a hint.
type bashBlock struct {
	cmd  string
	body string
	// failed paints the head red and the body with the failure palette.
	failed bool
	// running is set for a block interrupted by preemption: the reference
	// leaves its head as the hollow amber "○" and never repaints it.
	running bool
}

// bashBlockLines renders a finished bash block plus its trailing blank row.
func bashBlockLines(b *bashBlock, expanded bool, height int) []string {
	out := []string{bashHeadRow(b.cmd, b.failed, b.running)}
	if b.body == "" {
		return append(out, "")
	}
	lines := strings.Split(b.body, "\n")
	cap := bashBodyCap(height)
	truncated := len(lines) > cap
	hint := ""
	switch {
	case truncated && expanded:
		hint = "(ctrl+o to collapse)"
		out = append(out, bashBodyRows(lines, b.failed)...)
	case truncated:
		hint = "(ctrl+o to expand)"
		elided := len(lines) - cap
		out = append(out, cWarningBlueOpen+"  ⎿  "+bashMarkerColour(b.failed)+
			fmt.Sprintf("<output +%d lines>", elided)+"\x1b[m")
		out = append(out, bashContRows(lines[len(lines)-cap:], b.failed)...)
	default:
		out = append(out, bashBodyRows(lines, b.failed)...)
	}
	if hint != "" {
		out[len(out)-1] += " " + bashHintColour + hint + "\x1b[m"
	}
	return append(out, "")
}

func bashMarkerColour(failed bool) string {
	if failed {
		return bashBodyFailOpen
	}
	return bashBodyOKOpen
}

// bashPromptGlyph is the composer prompt while the mode is armed. The `!`
// is bold blue and the space after it is default-coloured, matching the
// reference's "\x1b[38;2;38;139;210;1m!\x1b[m ".
func bashPromptGlyph(inputView string) string {
	// The textarea paints the whole prompt string with the focused prompt
	// style (#268BD2, no bold): `\x1b[38;2;38;139;210m! \x1b[m`. Move the
	// reset one cell left and add the bold attribute, the same trick the
	// `>` prompt uses for its trailing space.
	styled := cBlueOpen + "! \x1b[m"
	if !strings.Contains(inputView, styled) {
		return inputView
	}
	return strings.Replace(inputView, styled, cWarningBlueOpen+"!\x1b[m ", 1)
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
	bashSetProcessGroup(c)
	c.Cancel = func() error { return bashKillProcessGroup(c) }
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
		return bashDoneMsg{gen: gen, cmd: command, output: text, failed: true}
	}
	return bashDoneMsg{gen: gen, cmd: command, output: text}
}

func trimBashOutput(s string) string { return strings.TrimRight(s, "\r\n") }

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

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
//   - A command submitted while a model turn streams is queued and shown
//     as "▸ <cmd>" above the input (no "(alt+m …)" suffix, unlike queued
//     prompts); it runs when the turn ends.
package main

import (
	"context"
	"os/exec"
	"strings"

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

// bashDoneMsg is delivered when a bash-mode command finishes (or is
// cancelled by Esc). cmd is carried so the in-flight head row can be
// repainted in place.
type bashDoneMsg struct {
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
func bashOutputRows(out string, failed bool) []string {
	bodyOpen, bodyCont := bashBodyOKOpen, cSubtleOpen
	if failed {
		bodyOpen, bodyCont = bashBodyFailOpen, cToolRemoveOpen
	}
	lines := strings.Split(out, "\n")
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
	m.blankBefore()
	m.bashRowIdx = len(m.response)
	m.response = append(m.response, bashHeadRow(cmd, false, true))
	m.streaming = true
	m.thinking = false
	m.activeTool = ""
	m.anyToolRun = false
	m.spinFrame = 0
	m.clearLastTurnOutcome()
	ctx, cancel := context.WithCancel(context.Background())
	m.bashCancel = cancel
	dir := m.cwd
	run := func() tea.Msg {
		output, failed, cancelled := execBashCommand(ctx, dir, cmd)
		return bashDoneMsg{cmd: cmd, output: output, failed: failed, cancelled: cancelled}
	}
	tick := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(run, tick)
}

// execBashCommand runs one command through the platform shell with merged
// stdout/stderr. The reference prints "exit status N" only when a failed
// command produced no output, which is exactly Go's *exec.ExitError text.
func execBashCommand(ctx context.Context, dir, command string) (output string, failed, cancelled bool) {
	argv := tools.DefaultShellArgv(command)
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	raw, err := c.CombinedOutput()
	if ctx.Err() != nil {
		// Esc cancelled the turn: the reference leaves the block head with
		// no body and a green bullet, never an "Interrupted" row.
		return "", false, true
	}
	text := strings.TrimRight(string(raw), "\r\n")
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, true, false
	}
	return text, false, false
}

// finishBashCommand applies a bashDoneMsg: repaint the head row in place,
// append the body (if any) plus the block's trailing blank row.
func (m *runMode) finishBashCommand(msg bashDoneMsg) {
	m.bashCancel = nil
	m.streaming = false
	if m.bashRowIdx >= 0 && m.bashRowIdx < len(m.response) {
		m.response[m.bashRowIdx] = bashHeadRow(msg.cmd, msg.failed, false)
	}
	m.bashRowIdx = -1
	if msg.output != "" {
		m.response = append(m.response, bashOutputRows(msg.output, msg.failed)...)
	}
	m.response = append(m.response, "")
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

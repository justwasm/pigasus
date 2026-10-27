// tui_interrupted.go — the composer hint for a turn the user cancelled.
package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/branding"
)

// interruptedHintRow renders the reference's hint for a turn the user
// cancelled with Esc.
//
// 【反编译】 agy 1.4.8 renders the line in the *composer* —
// `cli/model/model.(*PromptModel).View` (0x102bb49f0) asks
// `PromptModel.IsInterrupted` (0x102bb4800) and, when the latest turn was
// interrupted, renders `(*PromptModel).interruptedLine` (0x102bb48b0).
// The transcript keeps no error row for a cancelled turn.
//
// The reference's own literal is
//
//	  ⎿  Interrupted · What should %s do instead?
//
// with the CLI's own name as the argument. The split below — the hook in
// the blue the other "⎿" rows use, the sentence in dim gray — is the
// clone's own styling; the reference paints the whole line with one
// lipgloss style that the decompilation does not name.
func (m *runMode) interruptedHintRow() string {
	if !m.turnInterrupted {
		return ""
	}
	line := agent.InterruptedHintLine(branding.Current().CLIName)
	const hook = "  ⎿  "
	return cWarningBlueOpen + hook + cWarningBlueClose +
		cDimGrayOpen + strings.TrimPrefix(line, hook) + cDimGrayClose
}

// tui_compact_spinner.go — the auto-compaction spinner's tick.
//
// # Why the message and the command live together
//
// A tick is one fact: the message the command produces and the message
// the host's update switch handles have to be the *same type*. They were
// not, for two years.
//
// Commit 64e71d67 moved the tick commands into the shared viewer package
// and aliased the /usage panel's message type (`usageQuotaTickMsg`),
// but left the compaction spinner's message behind as a second,
// distinct type in the host. The command produced the shared package's
// type; the host's switch named the host's own. Nothing matched, the
// spinner's frame never advanced, and the compaction status row sat on
// its first Braille glyph for the whole compaction.
//
// The fix is structural rather than a patch: the message and the
// command that arms it live in one file, so a second type cannot be
// introduced beside them. The test in tui_compact_spinner_test.go pins
// the pair.
//
// # Why the host owns it
//
// The compaction spinner is a host-level turn fact: the host owns the
// compaction events themselves (compactStartMsg, compactEndMsg) and
// draws the status row that carries the spinner. The ui tier has no
// owner for it, and the lower tiers do not import the terminal
// framework at all.

package cliapp

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// compactSpinnerTickMsg fires every 100 ms while auto-compaction runs.
type compactSpinnerTickMsg struct{}

// compactSpinnerTick arms the next compactSpinnerTickMsg.
func compactSpinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return compactSpinnerTickMsg{} })
}

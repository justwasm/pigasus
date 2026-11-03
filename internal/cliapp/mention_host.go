// mention_host.go — the host's side of the "@" file-mention dropdown.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/mentionpicker: it owns the
// candidate list, the highlight, the mention token's span, the rows it
// writes and the keys it handles.
//
// The host keeps the facts the panel cannot know — what the composer
// holds, the workspace root the candidates are listed from, the
// terminal's width — and hands them to the panel as seams.
//
// The host's job is to route: it re-derives the dropdown's state from
// the composer after every keystroke, it asks the panel for the rows
// when it paints a frame, and it asks the panel before it routes a key
// the panel owns.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
)

// newMentionPicker builds the "@" file-mention dropdown with the
// host's seams.
func (m *runMode) newMentionPicker() *mentionpicker.Panel {
	return mentionpicker.New(mentionpicker.Seams{
		Buffer:    func() string { return m.input.Value() },
		SetBuffer: func(s string) { m.input.SetValue(s) },
		Cwd:       func() string { return m.cwd },
		Width:     func() int { return m.width },
	})
}

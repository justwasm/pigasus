package panels

import (
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
)

// SlashPickerHost defines host seams for the slash command popup.
type SlashPickerHost interface {
	AltScreenActive() bool
	TerminalWidth() int
	IsInputY(y int) bool
	InputValue() string
	SetInputValue(string)
	EngineSlashCommands() []slashcommands.Command
	SlashScopeContext() slashcommands.ScopeContext
}

// NewSlashPicker builds the slash command popup with the host's seams.
func NewSlashPicker(h SlashPickerHost) *slashpicker.Panel {
	return slashpicker.New(slashpicker.Seams{
		AltScreen:      h.AltScreenActive,
		Width:          h.TerminalWidth,
		IsInputY:       h.IsInputY,
		Buffer:         h.InputValue,
		SetBuffer:      h.SetInputValue,
		CustomCommands: h.EngineSlashCommands,
		ScopeContext:   h.SlashScopeContext,
	})
}

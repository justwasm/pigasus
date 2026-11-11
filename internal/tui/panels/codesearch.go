package panels

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/features/tools/codesearch"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
)

// CodesearchHost defines the host operations needed by the /codesearch panel.
type CodesearchHost interface {
	SearchRoot() string
	TerminalSize() (width int, height int)
	SearchStreaming() bool
	CodesearchReadFile(path string) ([]string, string, bool)
	CodesearchOpenEditor(path string, line int) tea.Cmd
	CodesearchSendComments(text string) tea.Cmd
	ClosePanel() tea.Cmd
}

// NewCodesearchPanel builds the /codesearch panel with the given host seams.
func NewCodesearchPanel(h CodesearchHost) *codesearchpanel.Panel {
	return codesearchpanel.New(codesearchpanel.Seams{
		Root:         h.SearchRoot,
		Size:         h.TerminalSize,
		Streaming:    h.SearchStreaming,
		NewRunner:    codesearch.NewRunner,
		ReadFile:     h.CodesearchReadFile,
		OpenEditor:   h.CodesearchOpenEditor,
		SendComments: h.CodesearchSendComments,
		Close:        h.ClosePanel,
	})
}

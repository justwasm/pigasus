package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
)

// DiffHost defines the host operations needed by the /diff panel.
type DiffHost interface {
	SessionCwd() string
	TerminalSize() (width int, height int)
	DiffTurnCount() int
	VimEnabled() bool
	ClosePanel() tea.Cmd
}

// DiffRemote is the optional remote-session seam: the workspace and its
// history live on the proxy, so the panel asks the host for the turn diff
// instead of reading local git.
type DiffRemote interface {
	RemoteDiffChanges() (tracked, untracked []diffpanel.Change, ok bool)
	RemoteDiffBody(target, kind string) (lines []string, ok bool)
	RemoteDiffVCS() string
}

// NewDiffPanel builds the /diff panel with the given host seams.
func NewDiffPanel(h DiffHost) *diffpanel.Panel {
	seams := diffpanel.Seams{
		Cwd:        h.SessionCwd,
		Size:       h.TerminalSize,
		Turns:      h.DiffTurnCount,
		VimEnabled: h.VimEnabled,
		Close:      h.ClosePanel,
	}
	if remote, ok := h.(DiffRemote); ok {
		seams.Changes = remote.RemoteDiffChanges
		seams.Body = remote.RemoteDiffBody
		seams.VCS = remote.RemoteDiffVCS
	}
	return diffpanel.New(seams)
}

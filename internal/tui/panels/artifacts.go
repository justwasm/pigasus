package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
)

// ArtifactsHost is the host environment the /artifact panel's seams need.
type ArtifactsHost interface {
	// ArtifactRefs is the conversation's artifacts.
	ArtifactRefs() []artifactpanel.Artifact
	// TerminalSize is the terminal's size.
	TerminalSize() (width, height int)
	// ReadArtifact returns an artifact's text.
	ReadArtifact(path string) (string, bool)
	// OpenArtifactInEditor hands the artifact to the configured editor.
	OpenArtifactInEditor(path string) tea.Cmd
	// ClosePanel leaves the panel.
	ClosePanel() tea.Cmd
}

// NewArtifactPanel builds the /artifact panel against the host's seams.
func NewArtifactPanel(h ArtifactsHost) *artifactpanel.Panel {
	return artifactpanel.New(artifactpanel.Seams{
		Refs:         h.ArtifactRefs,
		Size:         h.TerminalSize,
		Read:         h.ReadArtifact,
		OpenInEditor: h.OpenArtifactInEditor,
		Close:        h.ClosePanel,
	})
}

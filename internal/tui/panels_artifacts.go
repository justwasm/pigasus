package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
)

// newArtifactPanel builds the /artifact panel against the host's seams.
//
// The panel reads the conversation's artifacts on every draw (m.Artifacts, the
// same source the checkpoint's resume block reads), so it always shows the
// current conversation rather than the one it opened on.
func (m *RootModel) newArtifactPanel() *artifactpanel.Panel {
	return panels.NewArtifactPanel(m)
}

// artifactPanel returns the panel, building it on first use.
func (m *RootModel) artifactPanel() *artifactpanel.Panel {
	if m.overlays.panel.art == nil {
		m.overlays.panel.art = m.newArtifactPanel()
	}
	return m.overlays.panel.art
}

// ArtifactRefs is the panel's view of the conversation's artifacts.
//
// artifacts.List returns compaction.ArtifactRef, whose Path is a file:// URL;
// the panel needs the filesystem path, so the scheme is stripped here — the
// host is where the two conventions meet.
func (m *RootModel) ArtifactRefs() []artifactpanel.Artifact {
	refs := m.Artifacts()
	out := make([]artifactpanel.Artifact, 0, len(refs))
	for _, ref := range refs {
		out = append(out, artifactpanel.Artifact{
			Name: ref.Name,
			Path: strings.TrimPrefix(ref.Path, "file://"),
		})
	}
	return out
}

// ReadArtifact returns an artifact's text.
//
// The second value is false when the file cannot be read; the panel then shows
// the reason instead of an empty document.
func (m *RootModel) ReadArtifact(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// OpenArtifactInEditor hands an artifact to the configured editor.
//
// The editor is resolved the same way /restore-prompt's editor is
// (editorkit.ResolveEditor), so /artifact's ctrl+g and the prompt's ctrl+g open
// the same program. The command runs through tea.ExecProcess, which suspends
// the TUI while the editor owns the terminal.
func (m *RootModel) OpenArtifactInEditor(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	return m.editFileInEditorCmd(path)
}

// editFileInEditorCmd opens an existing file in the configured editor.
//
// It is editPromptCmd's sibling: the same editor resolution
// (editorkit.ResolveEditor), the same lookpath check and the same
// tea.ExecProcess suspension, but over a file that already exists rather than a
// scratch file the prompt is copied into.
func (m *RootModel) editFileInEditorCmd(path string) tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.conversation.response = append(m.conversation.response, notice)
	}
	c := exec.Command(name, append(args, path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

// ArtifactsPanelKind is the panel kind /artifact opens.
const ArtifactsPanelKind = panelregistry.Artifact

// remote_upload.go — /upload in a `--remote-agent` session.
//
// The proxy's uploadProjectFile turns local bytes into a project resource.
// The CLI has no reference slash command for it, so the host exposes a
// host-only /upload <path> that reads the file and reports the resource.
package tui

import (
	"context"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/slashparse"
)

// slashUpload uploads one local file to the remote project.
func (m *RootModel) slashUpload(args string) (bool, tea.Cmd) {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok || !m.session.remoteAgent {
		m.slashRespond("/upload", "Remote uploads are only available in a --remote-agent session.")
		return true, nil
	}
	path := slashparse.Trim(args)
	if path == "" {
		m.slashRespond("/upload", "Usage: /upload <path>")
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resource, err := engine.UploadProjectFile(ctx, path)
	if err != nil {
		m.slashRespond("/upload", "error: "+err.Error())
		return true, nil
	}
	name := resource.DisplayName
	if name == "" {
		name = resource.ResourceID
	}
	m.slashRespond("/upload", "Uploaded "+name+" as "+resource.ResourceID+
		" ("+strconv.FormatInt(resource.SizeBytes, 10)+" bytes).")
	return true, nil
}

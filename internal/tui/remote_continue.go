// remote_continue.go — `/continue` in a `--remote-agent` session.
//
// The remote cascade id is the conversation id, exactly as in the
// reference client: the server mints the cascade, and the local store keeps
// the transcript under that id. Once the first turn has started, the host
// adopts the id, so `/continue` resolves the newest conversation in the
// workspace and asks the proxy for it with GetConversation.
package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// adoptRemoteCascadeID names the saved conversation after the remote
// cascade. The turn's frames were recorded under the session's temporary
// id; the save at turn end uses the new one.
func (m *RootModel) adoptRemoteCascadeID() {
	if m == nil || !m.session.remoteAgent {
		return
	}
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok {
		return
	}
	id := engine.CascadeID()
	if id == "" || id == m.session.conversationID {
		return
	}
	m.session.conversationID = id
}

// slashContinueRemote resumes the newest remote conversation in the
// workspace. The engine attaches to the cascade and the stored transcript
// is replayed, mirroring the local /continue.
func (m *RootModel) slashContinueRemote() (bool, tea.Cmd) {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok {
		m.slashRespond("/continue", "No previous conversation to continue.")
		return true, nil
	}

	id, _, err := convstore.ResolveContinue(m.session.cwd)
	if err != nil {
		m.slashRespond("/continue", "error: "+err.Error())
		return true, nil
	}
	if id == "" {
		m.slashRespond("/continue", "No previous conversation to continue.")
		return true, nil
	}

	ctx, cancel := remoteagentclient.WithUnaryTimeout(context.Background())
	defer cancel()
	if _, err := engine.ResumeCascade(ctx, id); err != nil {
		if err == agentturn.ErrNoRemoteConversation {
			m.slashRespond("/continue", "No previous remote conversation to continue.")
		} else {
			m.slashRespond("/continue", "Failed to load conversation "+id+": "+err.Error())
		}
		return true, nil
	}

	m.session.conversationID = id
	m.conversation.response = nil
	m.session.bumpEpoch()
	if sup := m.session.sup(); sup != nil {
		sup.Clear()
	}
	m.conversation.blocks = nil
	m.conversation.liveSteps = nil
	m.conversation.activeTool = ""
	m.session.autoTitle = ""

	if sel := convstore.LoadSelection(id, "continue", convstore.ConversationPath(id)); sel.Store != nil {
		m.seedConversation(sel.Store)
	}
	m.conversation.response = append(m.conversation.response, "  ⎿  Resumed conversation "+id)
	return true, nil
}

// remoteStartupConversation resolves `--conversation` / `--continue` for a
// remote session at startup: the engine attaches before the first prompt.
func (m *RootModel) remoteStartupConversation() {
	if m == nil || !m.session.remoteAgent || m.session.conversationID == "" {
		return
	}
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = engine.ResumeCascade(ctx, m.session.conversationID)
}

// interaction_host.go — the remote-agent server's user seam.
//
// The agent runtime's ask_question / ask_permission / ask_custom_permission
// tools park on an interaction.Host. In a `pigasus agent-server` turn the
// user is behind the remote protocol, so the host publishes a WAITING step
// on the cascade and wakes when HandleCascadeUserInteraction answers it.
package agentserver

import (
	"context"

	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/infra/mockremoteagent"
)

// sessionInteractionHost publishes interactions on one cascade.
type sessionInteractionHost struct {
	session *mockremoteagent.CascadeSession
}

// Ask publishes the waiting step and blocks for the client's answer.
func (h *sessionInteractionHost) Ask(ctx context.Context, req interaction.Request) (interaction.Answer, error) {
	if h == nil || h.session == nil {
		return interaction.Answer{Cancelled: true}, nil
	}
	return h.session.AskUser(ctx, req)
}

var _ interaction.Host = (*sessionInteractionHost)(nil)

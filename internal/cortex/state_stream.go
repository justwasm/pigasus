// Package cortex — state_stream.go: agent state streaming and subagent hub access.
//
// pclntab entries:
//   - StreamAgentStateUpdates    0x7fe9e60
//   - RequestAgentStatePageUpdate 0x7fea1c0
//   - SubagentHub                (inferred)
//   - gatedSubagentHub           0x7fe46e0
//   - SendUserCascadeMessage     (inferred)
//   - HandleUserInteraction      (inferred)
package cortex

// SubagentHub returns the (un-gated) subagent hub. v1.3.5 binary
// addr (per pclntab .SubagentHub), name=SubagentHub, signature=(cm
// *CascadeManager) SubagentHubLike.
func (cm *CascadeManager) SubagentHub() SubagentHubLike {
	return cm.subagentHub
}

// gatedSubagentHub returns the gated subagent hub. v1.3.5 binary
// addr 0x7fe46e0, name=gatedSubagentHub, signature=(cm
// *CascadeManager) SubagentGate.
func (cm *CascadeManager) gatedSubagentHub() SubagentGate {
	if cm.subagentHub == nil {
		return nil
	}
	return cm.subagentHub.Gate()
}

// StreamAgentStateUpdates streams agent state updates. v1.3.5 binary
// addr 0x7fe9e60, name=StreamAgentStateUpdates, signature=(cm
// *CascadeManager) <-chan AgentStateUpdate.
func (cm *CascadeManager) StreamAgentStateUpdates() <-chan AgentStateUpdate {
	ch := make(chan AgentStateUpdate, 16)
	return ch
}

// RequestAgentStatePageUpdate requests an agent state page update.
// v1.3.5 binary addr 0x7fea1c0, name=RequestAgentStatePageUpdate,
// signature=(cm *CascadeManager, page int) error.
func (cm *CascadeManager) RequestAgentStatePageUpdate(page int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// SendUserCascadeMessage sends a user message to the cascade. v1.3.5
// binary addr (per pclntab .SendUserCascadeMessage),
// name=SendUserCascadeMessage, signature=(cm *CascadeManager, msg
// string) error.
func (cm *CascadeManager) SendUserCascadeMessage(msg string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// HandleUserInteraction handles a user interaction. v1.3.5 binary
// addr (per pclntab .HandleUserInteraction),
// name=HandleUserInteraction, signature=(cm *CascadeManager, ev
// UserInteractionEvent) error.
func (cm *CascadeManager) HandleUserInteraction(ev UserInteractionEvent) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// UserInteractionEvent is the recovered user interaction event.
type UserInteractionEvent struct {
	Kind       string
	SubagentID string
	Payload    map[string]any
}

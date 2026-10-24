// Package main — remote_control_stores.go: store constructors for remote control daemon.
package rchost

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// buildHostOriginPolicyStore returns the production OriginPolicyStore the
// single SetRemoteControlOriginPatterns method (cycle rc-auth-tokens) calls
// into. Wired here, in cmd/wagyu/, because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a dispatcher package
// can't import cmd/wagyu/ (an import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one helper would be heavier
// than the helper itself.
func buildHostOriginPolicyStore() remotecontrol.OriginPolicyStore {
	root := filepath.Join(auth.AntigravityHome(), "origin_policy.json")
	return remotecontrol.NewFSOriginPolicyStore(root, nil)
}

// buildHostResourceStore returns the production ResourceStore the three
// JSON-RPC methods (ListRemoteAgentResourceProviders /
// ListRemoteAgentAvailableResources / UpdateRemoteAgentProjectResources,
// cycle rc-resources) call into. Wired here, in cmd/wagyu/, because the
// constructor needs to read auth.AntigravityHome() (for the resources.json
// path), build a `$ANTIGRAVITY_HOME/secrets/` discovery closure (the
// `configured` flag), and reach into the bound ProjectStore (so the Bind path
// can write the project's `Resources` field back).
func buildHostResourceStore(projects remotecontrol.ProjectStore) remotecontrol.ResourceStore {
	root := filepath.Join(auth.AntigravityHome(), "resources.json")
	secrets := filepath.Join(auth.AntigravityHome(), "secrets")
	check := func(providerID string) bool {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			return false
		}
		path := filepath.Join(secrets, providerID)
		st, err := os.Stat(path)
		if err != nil {
			return false
		}
		return !st.IsDir() && st.Size() > 0
	}
	return remotecontrol.NewFSResourceStore(root, projects, check, nil)
}

// buildHostConversationStore returns the production ConversationStore the
// three destructive methods (cycle rc-conversation-deletions) call into.
// Wired here, in cmd/wagyu/, because the constructor's ProjectLookup closure
// needs to reach into the ProjectStore to map workspaces back to project ids
// (a dispatcher package shouldn't depend on the project-store shape).
func buildHostConversationStore(projects remotecontrol.ProjectStore) remotecontrol.ConversationStore {
	lookup := func(workspace string) string {
		if workspace == "" || projects == nil {
			return ""
		}
		rows, err := projects.List(nil, "", 0)
		if err != nil {
			return ""
		}
		wantBase := filepath.Base(workspace)
		for _, p := range rows {
			if filepath.Base(p.Workspace) == wantBase {
				return p.ID
			}
		}
		return ""
	}
	return remotecontrol.NewFSConversationStore(lookup)
}

// buildHostConversationConfigStore returns the production
// ConversationConfigStore the single UpsertRemoteAgentConversationConfig
// method (cycle rc-conversation-config) calls into. Wired here, in cmd/wagyu/,
// because the constructor needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be heavier than the helper itself.
func buildHostConversationConfigStore() remotecontrol.ConversationConfigStore {
	root := filepath.Join(auth.AntigravityHome(), "conversation_config.json")
	return remotecontrol.NewFSConversationConfigStore(root, nil)
}

// buildHostTerminalsRegistry returns the in-memory TerminalsRegistry the
// rc-terminals five RemoteAgentTerminal* methods route through. Cycle rc-terminals.
func buildHostTerminalsRegistry() *remotecontrol.TerminalsRegistry {
	terminalsRegistryOnce.Do(func() {
		terminalsRegistrySingleton = remotecontrol.NewTerminalsRegistry()
	})
	return terminalsRegistrySingleton
}

var (
	terminalsRegistryOnce      sync.Once
	terminalsRegistrySingleton *remotecontrol.TerminalsRegistry
)

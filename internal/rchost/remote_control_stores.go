// Package main — remote_control_stores.go: store constructors for remote control daemon.
package rchost

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/rcaudio"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotecontrol/convconfig"
	"github.com/justwasm/pigasus/internal/remotecontrol/origins"
	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
	"github.com/justwasm/pigasus/internal/remotecontrol/resources"
	"github.com/justwasm/pigasus/internal/remotecontrol/terminals"
	"github.com/justwasm/pigasus/rcterminal"
)

// buildHostOriginPolicyStore returns the production origins.Store the
// single SetRemoteControlOriginPatterns method (cycle rc-auth-tokens) calls
// into. Wired here, in cmd/wagyu/, because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a dispatcher package
// can't import cmd/wagyu/ (an import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one helper would be heavier
// than the helper itself.
func buildHostOriginPolicyStore() origins.Store {
	root := filepath.Join(auth.AntigravityHome(), "origin_policy.json")
	return origins.NewFSStore(root, nil)
}

// buildHostResourceStore returns the production resources.Store the three
// JSON-RPC methods (ListRemoteAgentResourceProviders /
// ListRemoteAgentAvailableResources / UpdateRemoteAgentProjectResources,
// cycle rc-resources) call into. Wired here, in cmd/wagyu/, because the
// constructor needs to read auth.AntigravityHome() (for the resources.json
// path), build a `$ANTIGRAVITY_HOME/secrets/` discovery closure (the
// `configured` flag), and reach into the bound projects.Store (so the Bind path
// can write the project's `Resources` field back).
func buildHostResourceStore(projectStore projects.Store) resources.Store {
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
	return resources.NewFSStore(root, projectStore, check, nil)
}

// buildHostConversationStore returns the production ConversationStore the
// three destructive methods (cycle rc-conversation-deletions) call into.
// Wired here, in cmd/wagyu/, because the constructor's ProjectLookup closure
// needs to reach into the projects.Store to map workspaces back to project ids
// (a dispatcher package shouldn't depend on the project-store shape).
func buildHostConversationStore(projectStore projects.Store) remotecontrol.ConversationStore {
	lookup := func(workspace string) string {
		if workspace == "" || projectStore == nil {
			return ""
		}
		rows, err := projectStore.List(nil, "", 0)
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
// convconfig.Store the single UpsertRemoteAgentConversationConfig
// method (cycle rc-conversation-config) calls into. Wired here, in cmd/wagyu/,
// because the constructor needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be heavier than the helper itself.
func buildHostConversationConfigStore() convconfig.Store {
	root := filepath.Join(auth.AntigravityHome(), "conversation_config.json")
	return convconfig.NewFSStore(root, nil)
}

// buildHostTerminalsRegistry returns the in-memory registry the
// rc-terminals five RemoteAgentTerminal* methods route through. Cycle rc-terminals.
func buildHostTerminalsRegistry() *terminals.Registry {
	terminalsRegistryOnce.Do(func() {
		terminalsRegistrySingleton = terminals.NewRegistry()
	})
	return terminalsRegistrySingleton
}

var (
	terminalsRegistryOnce      sync.Once
	terminalsRegistrySingleton *terminals.Registry
)

// buildHostWebTerminalsRegistry returns the registry the Web UI's
// terminal methods (CreateTerminal, ListTerminals,
// SendTerminalInput, StreamTerminalOutput, CloseTerminal) call
// into. The terminals it owns are live shells on a pty — the
// reference's own shape.
//
// The registry is a process-wide singleton so every dashboard
// connection on the dispatcher sees the same terminals, the way the
// cascade registry works.
//
// The registry's own fallback — the directory a terminal starts in
// when its request names no workspace — is the user's home
// directory, not the server's working directory.
//
// 【一手】 2026-10-27, the reference's own process table: the
// Antigravity IDE's language_server runs with the working directory
// "/" while the shell it spawns for a terminal runs in "/Users/gear".
//
// Cycle rc-web-terminals.
func buildHostWebTerminalsRegistry(cwd ...string) *rcterminal.Registry {
	webTerminalsRegistryOnce.Do(func() {
		webTerminalsRegistrySingleton = rcterminal.NewRegistry()
	})
	if len(cwd) > 0 && cwd[0] != "" {
		webTerminalsRegistrySingleton.SetWorkspace(cwd[0])
	}
	return webTerminalsRegistrySingleton
}

var (
	webTerminalsRegistryOnce      sync.Once
	webTerminalsRegistrySingleton *rcterminal.Registry
)

// buildHostWebAudioRegistry returns the registry the Web UI's voice
// methods (StreamAudioTranscription, SendAudioChunk,
// EndAudioSession) call into.
//
// The sessions transcribe with the Gemini transport the clone already
// speaks: the recording rides in an `inlineData` part, the way an
// image attachment rides. The reference transcribes with a local
// engine; the clone has none (see internal/rcaudio).
//
// The registry is a process-wide singleton so every dashboard
// connection on the dispatcher sees the same sessions.
//
// Cycle rc-web-terminals.
func buildHostWebAudioRegistry() *rcaudio.Registry {
	webAudioRegistryOnce.Do(func() {
		webAudioRegistrySingleton = rcaudio.NewRegistry(rcaudio.NewCloudCodeTranscriber())
	})
	return webAudioRegistrySingleton
}

var (
	webAudioRegistryOnce      sync.Once
	webAudioRegistrySingleton *rcaudio.Registry
)

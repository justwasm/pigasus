// Package main — remote_control_builders.go: builders for remote control filesystem, projects, permissions, plugins, triggers, and fastpush stores.
package rchost

import (
	"flag"
	"path/filepath"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/remotecontrol/fastpush"
	"github.com/justwasm/pigasus/internal/remotecontrol/files"
	"github.com/justwasm/pigasus/internal/remotecontrol/permissions"
	"github.com/justwasm/pigasus/internal/remotecontrol/plugins"
	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
	"github.com/justwasm/pigasus/internal/remotecontrol/triggers"
)

// buildHostFSProvider returns the production files.Provider the two read-only
// filesystem methods (rc-file-ops) call into. Wired here, in cmd/wagyu/,
// because the scope reads from the same flag set the rest of the host binary
// uses (--add-dir, --sandbox).
func buildHostFSProvider(fs *flag.FlagSet, cwd string) files.Provider {
	return &files.WorkdirScope{
		Workspace: cwd,
		Allow:     flagStringValues(fs, "add-dir"),
		Sandbox:   flagBoolValue(fs, "sandbox"),
	}
}

// buildHostProjectStore returns the production projects.Store the three
// project-CRUD methods (cycle rc-projects) call into. Wired here, in cmd/wagyu/,
// because the constructor needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be heavier than the helper itself.
func buildHostProjectStore(fs files.Provider) projects.Store {
	root := filepath.Join(auth.AntigravityHome(), "projects")
	return projects.NewFSStore(
		root,
		fs,
		convstore.ConversationBoundToProject, // fail-if-bound check (cycle rc-conversation-deletions)
		nil,                                  // time.Now (production)
	)
}

// buildHostPermissionStore returns the production permissions.Store the four
// permission-mutation methods (cycle rc-permissions-grants) call into.
// Wired here, in cmd/wagyu/, because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a dispatcher package
// can't import cmd/wagyu/ (an import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one helper would be heavier
// than the helper itself.
func buildHostPermissionStore() permissions.Store {
	root := filepath.Join(auth.AntigravityHome(), "permissions.json")
	return permissions.NewFSStore(root, nil)
}

// buildHostPluginStore returns the production plugins.Store the two plugin
// methods (cycle rc-plugins) call into. Wired here, in cmd/wagyu/, because the
// constructor needs to read auth.AntigravityHome() and stdlib path/filepath —
// a dispatcher package can't import cmd/wagyu/ (an import-direction
// reversal), and pulling internal/auth into internal/remotecontrol/ for one
// helper would be heavier than the helper itself.
func buildHostPluginStore() plugins.Store {
	root := filepath.Join(auth.AntigravityHome(), "plugins.json")
	return plugins.NewFSStore(root, nil)
}

// buildHostTriggerStore returns the production triggers.Store the five
// trigger methods (cycle rc-triggers) call into. Wired here, in cmd/wagyu/,
// because the constructor needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be heavier than the helper itself.
func buildHostTriggerStore() triggers.Store {
	root := filepath.Join(auth.AntigravityHome(), "triggers.json")
	return triggers.NewFSStore(root, nil)
}

// buildHostFastpushStore returns the production FastpushStore the single
// SetRemoteAgentFastpushEnabled method (cycle rc-fastpush-toggle) calls into.
// Wired here, in cmd/wagyu/, because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a dispatcher package
// can't import cmd/wagyu/ (an import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one helper would be heavier
// than the helper itself.
func buildHostFastpushStore() fastpush.Store {
	root := filepath.Join(auth.AntigravityHome(), "fastpush.json")
	return fastpush.NewFSStore(root, nil)
}

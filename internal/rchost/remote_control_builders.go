// Package main — remote_control_builders.go: builders for remote control filesystem, projects, permissions, plugins, triggers, and fastpush stores.
package rchost

import (
	"flag"
	"path/filepath"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// buildHostFSProvider returns the production FSProvider the two read-only
// filesystem methods (rc-file-ops) call into. Wired here, in cmd/wagyu/,
// because the scope reads from the same flag set the rest of the host binary
// uses (--add-dir, --sandbox).
func buildHostFSProvider(fs *flag.FlagSet, cwd string) remotecontrol.FSProvider {
	return &remotecontrol.WorkdirScope{
		Workspace: cwd,
		Allow:     flagStringValues(fs, "add-dir"),
		Sandbox:   flagBoolValue(fs, "sandbox"),
	}
}

// buildHostProjectStore returns the production ProjectStore the three
// project-CRUD methods (cycle rc-projects) call into. Wired here, in cmd/wagyu/,
// because the constructor needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be heavier than the helper itself.
func buildHostProjectStore(fs remotecontrol.FSProvider) remotecontrol.ProjectStore {
	root := filepath.Join(auth.AntigravityHome(), "projects")
	return remotecontrol.NewFSProjectStore(
		root,
		fs,
		convstore.ConversationBoundToProject, // fail-if-bound check (cycle rc-conversation-deletions)
		nil,                                  // time.Now (production)
	)
}

// buildHostPermissionStore returns the production PermissionStore the four
// permission-mutation methods (cycle rc-permissions-grants) call into.
// Wired here, in cmd/wagyu/, because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a dispatcher package
// can't import cmd/wagyu/ (an import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one helper would be heavier
// than the helper itself.
func buildHostPermissionStore() remotecontrol.PermissionStore {
	root := filepath.Join(auth.AntigravityHome(), "permissions.json")
	return remotecontrol.NewFSPermissionStore(root, nil)
}

// buildHostPluginStore returns the production PluginStore the two plugin
// methods (cycle rc-plugins) call into. Wired here, in cmd/wagyu/, because the
// constructor needs to read auth.AntigravityHome() and stdlib path/filepath —
// a dispatcher package can't import cmd/wagyu/ (an import-direction
// reversal), and pulling internal/auth into internal/remotecontrol/ for one
// helper would be heavier than the helper itself.
func buildHostPluginStore() remotecontrol.PluginStore {
	root := filepath.Join(auth.AntigravityHome(), "plugins.json")
	return remotecontrol.NewFSPluginStore(root, nil)
}

// buildHostTriggerStore returns the production TriggerStore the five
// trigger methods (cycle rc-triggers) call into. Wired here, in cmd/wagyu/,
// because the constructor needs to read auth.AntigravityHome() and stdlib
// path/filepath — a dispatcher package can't import cmd/wagyu/ (an
// import-direction reversal), and pulling internal/auth into
// internal/remotecontrol/ for one helper would be heavier than the helper itself.
func buildHostTriggerStore() remotecontrol.TriggerStore {
	root := filepath.Join(auth.AntigravityHome(), "triggers.json")
	return remotecontrol.NewFSTriggerStore(root, nil)
}

// buildHostFastpushStore returns the production FastpushStore the single
// SetRemoteAgentFastpushEnabled method (cycle rc-fastpush-toggle) calls into.
// Wired here, in cmd/wagyu/, because the constructor needs to read
// auth.AntigravityHome() and stdlib path/filepath — a dispatcher package
// can't import cmd/wagyu/ (an import-direction reversal), and pulling
// internal/auth into internal/remotecontrol/ for one helper would be heavier
// than the helper itself.
func buildHostFastpushStore() remotecontrol.FastpushStore {
	root := filepath.Join(auth.AntigravityHome(), "fastpush.json")
	return remotecontrol.NewFSFastpushStore(root, nil)
}

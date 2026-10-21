// Package remotecontrol — plugins.go: the plugin-state types and
// the PluginStore interface the two JSON-RPC methods introduced
// in cycle rc-plugins call into.
//
// Why split: the two JSON-RPC methods
// (ListRemoteAgentPlugins / DisconnectRemoteAgentPlugin) share a
// small CRUD-shaped seam they both talk to; the dispatcher does
// NOT know about the on-disk format. Putting the types in their
// own file keeps the per-handler bodies in
// plugins_dispatcher.go focused on the JSON-RPC envelope shape,
// and lets the storage layer (plugins_store.go) keep talking to
// a stable interface without re-touching the dispatcher.
//
// Storage shape:
//
//	$ANTIGRAVITY_HOME/plugins.json
//
// One file, versioned ({"version":1, "plugins":[...], "updated_at":...}).
// The file format is intentionally forward-compatible so future
// cycles can add fields without rewriting history (the version
// stamp bumps only on breaking changes).
//
// Cycle rc-plugins adds two JSON-RPC methods and one storage
// seam. The seam is shaped so a future cycle (e.g.
// ConnectRemoteAgentPlugin or UpdateRemoteAgentPlugin — not in
// this brief) can extend it without re-touching the dispatcher.
//
// What "plugin" means here:
//
// The decompiled agy binary's `RemoteAgentPlugin` records are
// external integrations (claude-code-bridge, mcp-bridge,
// github-copilot-bridge, slack-bridge, custom companions, etc.).
// For the clone we don't have real bridges, so this cycle ships
// a `StubPluginRegistry` that reads entries from
// $ANTIGRAVITY_HOME/plugins.json if it exists (and is
// well-formed), else seeds three fixture entries:
// `claude-code-bridge@1.0.0`, `mcp-bridge@1.0.0`,
// `github-copilot-bridge@1.0.0` with `connected_at = today`.
// Disconnect removes the entry from the JSON file and broadcasts
// `plugins/changed`.
//
// This is a "fixture that reads from disk" pattern: production
// deployments will plug in real plugin lifecycle later. The gap
// closes in cycle `rc-plugins-real-bridge` (not yet scheduled;
// this cycle documents the deferral in the deliverable's
// 硬约束 section).
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Plugin is one row of the plugin registry the dashboard sees on
// ListRemoteAgentPlugins. The fields mirror what the decompiled
// agy `RemoteAgentPlugin` records carry:
//
//   - PluginID: the stable id (e.g. "claude-code-bridge"),
//     unique within the registry. Wire-mapped to "plugin_id".
//
//   - Name: the human-facing display name; usually the same as
//     PluginID today, but the field is separate so a future
//     plugin can carry a friendlier label.
//
//   - Version: the plugin-reported semver (e.g. "1.0.0"). The
//     clone's fixtures pin "1.0.0"; a real bridge that wires up
//     later will surface its own version.
//
//   - ConnectedAt: RFC3339Nano UTC stamp the store stamped at
//     record-creation time.
//
//   - Capabilities: the free-form capability list the plugin
//     advertised (e.g. ["conversation.send",
//     "tool.execute"]). Today the fixtures seed a small set
//     per bridge; production wiring will surface whatever
//     the real plugin lifecycle reports.
//
//   - Origin: how the plugin was discovered — "config" (an
//     operator seeded it via the JSON file), "auto-discovered"
//     (a future cycle that scans the host filesystem), or ""
//     (the legacy fixture-seeding path). omitempty so the
//     wire drops the field when there's no provenance note.
type Plugin struct {
	// PluginID is the stable id dashboards use to address a
	// plugin. Required and unique within a registry.
	PluginID string `json:"plugin_id"`
	// Name is the human-facing display name.
	Name string `json:"name"`
	// Version is the plugin-reported semver.
	Version string `json:"version"`
	// ConnectedAt is when the registry recorded the plugin
	// as connected. RFC3339Nano UTC.
	ConnectedAt time.Time `json:"connected_at"`
	// Capabilities is the list of capabilities the plugin
	// advertised.
	Capabilities []string `json:"capabilities"`
	// Origin is the provenance note ("config" / "auto-discovered").
	// omitempty so the wire drops "" entries.
	Origin string `json:"origin,omitempty"`
}

// PluginState is the on-disk / wire shape the JSON file holds
// and the dispatcher hands back. One instance per
// $ANTIGRAVITY_HOME/plugins.json; the store is single-document
// by design — there is no "list of states" because there is
// exactly one registry per host.
//
// The struct is intentionally JSON-serialised with lowercase
// keys so the on-disk format matches the wire format the
// dashboard reads. The Version field is bumped on breaking
// changes (the brief pins v1).
type PluginState struct {
	// Version is the on-disk format version. Bumped on
	// breaking changes (renamed keys, removed fields);
	// non-breaking additions (new optional fields with
	// omitempty) leave the version alone.
	Version int `json:"version"`
	// Plugins is the live plugin slice. nil → empty
	// ([]Plugin{}) so the wire always sees "[]".
	Plugins []Plugin `json:"plugins"`
	// UpdatedAt is the wall-clock stamp the store bumped
	// at the most-recent mutation. RFC3339Nano UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// PluginStore is the seam the two JSON-RPC methods call into.
// Cycle rc-plugins adds Snapshot / Disconnect; future cycles
// (rc-plugins-real-bridge) will extend it with
// Connect / Register / Update as the real bridge lifecycle
// replaces today's fixture-seeding stub.
//
// Snapshot returns the current state. The dispatcher never
// checks for a "not-found" envelope — a fresh host with no
// plugins.json yet returns the seeded fixture state (three
// well-known bridges) so the dashboard always has a
// coherent answer.
//
// Disconnect removes the entry identified by pluginID. The
// store refuses an unknown id with ErrPluginNotFound (the
// dispatcher maps that to JSON-RPC -32015 "Plugin not found")
// so dashboards can distinguish "you tried to disconnect
// something that wasn't there" from "you successfully removed
// a plugin". Returns the post-mutation state so the
// dispatcher can echo the new plugin list in the response
// envelope.
//
// Both methods take a context the dispatcher already passes
// through; neither method blocks on I/O today (the FS store
// uses an in-process mutex + synchronous write), but the
// signature is future-proof for a cycle that promotes the
// write path to async / batched.
type PluginStore interface {
	Snapshot(ctx context.Context) (PluginState, error)
	Disconnect(ctx context.Context, pluginID string) (PluginState, error)
}

// ErrPluginNotFound is the sentinel PluginStore.Disconnect
// returns when the caller tries to remove a plugin_id that
// is not in the registry. The dispatcher translates to
// JSON-RPC -32015 "Plugin not found".
//
// Cycle rc-plugins adds this code; prior cycles parked the
// destructive method without defining the sentinel.
//
// The -32015 code is brand-new this cycle. -32602 ("Invalid
// params") is shared with every other method's "bad input"
// envelope (the brief explicitly says so). The "empty
// plugin_id" case maps to -32602 because it's a dashboard
// bug; the "unknown id" case maps to -32015 because it's a
// plausible retry-after-disconnect race that a dashboard
// might want to surface distinctly.
var ErrPluginNotFound = errors.New("rc-plugins: plugin not found")

// PluginStateVersion is the on-disk format version the
// current store writes. Bumped on breaking changes (renamed
// keys, removed fields); non-breaking additions (new
// optional fields with omitempty) leave the version alone so
// older readers still parse the file.
//
// v1 (cycle rc-plugins): the initial layout. See
// PluginState for the field set.
const PluginStateVersion = 1

// DefaultPluginFixtures is the fresh-host fixture set the
// store seeds when no plugins.json exists yet. Three well-
// known bridges the recovered agy builds surface:
//
//   - claude-code-bridge (1.0.0): the Claude Code ↔
//     Antigravity bridge. Capabilities:
//     conversation.send, conversation.read,
//     tool.execute.
//
//   - mcp-bridge (1.0.0): the Model Context Protocol
//     bridge. Capabilities: mcp.connect, mcp.list_tools,
//     mcp.invoke_tool.
//
//   - github-copilot-bridge (1.0.0): the GitHub Copilot ↔
//     Antigravity bridge. Capabilities: conversation.send,
//     completion.generate, pr.review.
//
// All three carry Origin = "config" so a dashboard can tell
// fixture-seeded plugins from operator-supplied ones (a
// future cycle can grow that distinction).
//
// This is the canonical fixture set: tests that want a
// deterministic answer to Snapshot() inject their own
// MemPluginStore; production (cmd/wagyu/) is happy to use
// the default shape but typically installs a MemPluginStore
// directly into the dispatcher's Config so the fixture
// semantics stay explicit at the seam.
var DefaultPluginFixtures = []Plugin{
	{
		PluginID:     "claude-code-bridge",
		Name:         "Claude Code Bridge",
		Version:      "1.0.0",
		Capabilities: []string{"conversation.send", "conversation.read", "tool.execute"},
		Origin:       "config",
	},
	{
		PluginID:     "mcp-bridge",
		Name:         "MCP Bridge",
		Version:      "1.0.0",
		Capabilities: []string{"mcp.connect", "mcp.list_tools", "mcp.invoke_tool"},
		Origin:       "config",
	},
	{
		PluginID:     "github-copilot-bridge",
		Name:         "GitHub Copilot Bridge",
		Version:      "1.0.0",
		Capabilities: []string{"conversation.send", "completion.generate", "pr.review"},
		Origin:       "config",
	},
}

// normalisePluginID trims whitespace from the user-supplied
// plugin_id. Empty after trim → "". Used by both the
// dispatcher layer (before calling Disconnect) and the store
// layer (when comparing for membership). Returns "" when the
// trimmed result is empty so the same check trips
// everywhere.
//
// Today the store does not auto-normalise (Disconnects that
// pass "  claude-code-bridge  " and "claude-code-bridge"
// are considered distinct), but the helper is here so a
// future cycle that wants server-side canonicalisation
// (typical for id-shaped fields) only has to swap the
// lookup line in plugins_store.go for normalisePluginID(...).
// The dispatcher path is the only validation site today
// (it surfaces the trimmed result in error envelopes), so
// the helper is intentionally not part of the public
// PluginStore interface.
func normalisePluginID(id string) string {
	return strings.TrimSpace(id)
}

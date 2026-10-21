// Package remotecontrol — top_level_status.go:
// topLevelStatus + deferredFeaturesSnapshot +
// serverFingerprint. Pulled out of top_level.go in
// cycle rc-filesize-internal-remotecontrol so the
// parent file stays focused on the wire envelope
// types + canonical sorted slices.
package remotecontrol

import (
	"strings"
	"time"
)

// topLevelStatus builds the TopLevelStatus snapshot
// the RemoteControlStatus handler returns. Pulls:
//
//   - Version: Session.Version (or
//     cli.ReferenceVersion's mirror when Session
//     is nil)
//   - UptimeSeconds: time.Since(SessionState.StartedAt)
//     rounded to seconds
//   - StartedAt: RFC3339Nano UTC
//   - ConnectedClients: SessionState.Connected()
//   - OpenCascades: (*CascadeRegistry).Len()
//   - DispatchMethods: sortedDispatchMethods() copy
//   - ServerFingerprint: serverFingerprint(d, version)
//
// Nil-safe: a dispatcher with nil Session /
// Cascades yields the zero fields (0 uptime,
// "unknown" fingerprint, empty started_at) instead
// of panicking. A dispatcher with no Config.Commit
// falls back to "unknown" so dashboards never have
// to nil-check.
func (d *Dispatcher) topLevelStatus() TopLevelStatus {
	out := TopLevelStatus{
		DispatchMethods: sortedDispatchMethods(),
	}

	// Default version to the package-level
	// ServerVersion constant so a dashboard
	// renderer always sees a non-empty string
	// (ServerVersion is the MCP-protocol-level
	// "0.1.0" — the brief's "1.4.2" lives on
	// Session.Version when the host installed one).
	out.Version = ServerVersion

	out = applySessionDerivedFields(d, out)

	// Server fingerprint always derived from the
	// resolved version (Session if present, else
	// ServerVersion). nil d.cfg.Session yields a
	// fingerprint that still mentions the version
	// constant.
	out.ServerFingerprint = serverFingerprint(d, out.Version)

	out = applyCascadeState(d, out)
	out.DeferredFeatures = deferredFeaturesSnapshot(d)

	return out
}

// applySessionDerivedFields reads Session.Version /
// Connected / StartedAt under the Session mu and
// stamps the matching TopLevelStatus fields. Pulled
// out of topLevelStatus so the lock-held section is
// auditable on its own.
func applySessionDerivedFields(d *Dispatcher, out TopLevelStatus) TopLevelStatus {
	if d == nil || d.cfg.Session == nil {
		return out
	}
	d.cfg.Session.mu.RLock()
	version := d.cfg.Session.Version
	startedAt := d.cfg.Session.StartedAt
	d.cfg.Session.mu.RUnlock()
	if version != "" {
		out.Version = version
	}
	out.ConnectedClients = int(d.cfg.Session.Connected())
	if startedAt.IsZero() {
		return out
	}
	out.StartedAt = startedAt.UTC().Format(time.RFC3339Nano)
	out.UptimeSeconds = int64(time.Since(startedAt).Round(time.Second).Seconds())
	if out.UptimeSeconds < 0 {
		// Defensive: a clock skew or a hand-set
		// StartedAt in the future would yield a
		// negative uptime. Clamp to 0 so the
		// dashboard renderer never sees a
		// nonsensical value.
		out.UptimeSeconds = 0
	}
	return out
}

// applyCascadeState reads the cascade registry's
// length and stamps TopLevelStatus.OpenCascades.
// Pulled out so the nil-d / nil-cascades guards
// stay visually compact.
func applyCascadeState(d *Dispatcher, out TopLevelStatus) TopLevelStatus {
	if d == nil || d.cascades == nil {
		return out
	}
	out.OpenCascades = d.cascades.Len()
	return out
}

// defaultDeferredFeatures is the canonical four-key
// baseline every dispatcher emits on the wire when the
// host didn't install any custom overrides. Adding a key
// to the canonical set is a deliberate wire-shape change
// (dashboards iterate the four expected entries); a future
// cycle that adds a new deferred item appends a key here
// AND bumps a "deferred_features version" field on the
// envelope (parked for that cycle). Today's keys:
//
//   - "webrtc_mesh"        — WebRTC peer-to-peer mesh
//   - "antigravity_hub"    — second Hub process
//   - "systemd_user"       — Linux systemd-user svc-mgr
//   - "enterprise_tunnel"  — enterprise session tunnel
//
// Sorted alphabetically so the rendered list is
// deterministic.
var defaultDeferredFeatures = []string{
	"antigravity_hub",
	"enterprise_tunnel",
	"systemd_user",
	"webrtc_mesh",
}

// deferredFeaturesSnapshot returns the four-key
// `deferred_features` map that every RemoteControlStatus
// response carries. nil-safe: a dispatcher with no
// Config.DeferredFeatures field returns the all-false
// baseline. A dispatcher with a partial map has the
// canonical default applied first (so missing keys show
// as `false`), then any host-installed overrides applied
// on top.
//
// A defensive copy is returned so a host that mutates
// the source map after construction cannot race the
// dispatcher's response. The map keys are the canonical
// four (sorted ascending); values default to `false`
// unless the host installed an override.
func deferredFeaturesSnapshot(d *Dispatcher) map[string]bool {
	out := make(map[string]bool, len(defaultDeferredFeatures))
	for _, k := range defaultDeferredFeatures {
		out[k] = false
	}
	if d == nil || d.cfg.DeferredFeatures == nil {
		return out
	}
	for _, k := range defaultDeferredFeatures {
		if v, ok := d.cfg.DeferredFeatures[k]; ok {
			out[k] = v
		}
	}
	return out
}

// serverFingerprint derives the "wagyu-rc/<version>/<short-hash>"
// string the TopLevelStatus response carries. The
// short-hash is the first 8 chars of the build-time
// git SHA (Config.Commit) — long enough to be unique
// across local builds, short enough to render in a
// dashboard's "build id" badge without truncating
// manually. Empty / whitespace Commit falls back to
// "unknown" so dashboards never see an empty badge.
func serverFingerprint(d *Dispatcher, version string) string {
	if version == "" {
		version = "unknown"
	}
	commit := ""
	if d != nil {
		commit = strings.TrimSpace(d.cfg.Commit)
	}
	if commit == "" {
		return "wagyu-rc/" + version + "/unknown"
	}
	if len(commit) > 8 {
		commit = commit[:8]
	}
	return "wagyu-rc/" + version + "/" + commit
}

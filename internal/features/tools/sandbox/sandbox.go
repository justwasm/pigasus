// Package sandbox implements the recovered agy terminal-sandbox model
// from google3/third_party/jetski/cli. Cycle 228.
//
// The terminal sandbox constrains which paths a `run_command` tool call
// is allowed to read or write. When the sandbox is enabled (settings
// key `enableTerminalSandbox`) and a shell command touches a path
// outside the allow-list, the agent surfaces a prompt: "do you want to
// bypass the sandbox for this command?" — the recovered string
// `tool call rejected` / `BypassSandbox: true` / `Ran in the sandbox`
// evidence in `out/local-macos/strings-resolved.txt` (agy 1.3.4 binary,
// SHA-256 verified).
//
// Two paths are always in the allow-list so the agent does not need to
// ask for them:
//
//  1. the artifact directory — the agent's own output (codesearch
//     snapshots, the per-conversation review pane);
//  2. the per-conversation scratch directory — where the agent stores
//     intermediate files between turns.
//
// Without this carve-out, every `run_command` that touches one of
// those directories would prompt the user, which is the gap AGY 1.2.10
// closes: "Improved terminal sandbox behavior so the agent asks to
// bypass the sandbox less often: it now knows sandboxed commands can
// read and write its own artifact and scratch directories, and it tries
// the sandbox first even after an earlier command needed a bypass."
//
// The "tries the sandbox first even after an earlier command needed a
// bypass" half is the second contract this package enforces: a bypass
// is a per-call decision. Each Run() invocation re-evaluates the
// sandbox from scratch — there is no sticky `BypassSandbox=true` flag
// carried between calls. The previous-cycle implementation persisted
// a "yes, bypass" decision, so the first bypass turned the rest of the
// session into a permission-less run_command; cycle 228 makes that
// regression impossible by keeping the bypass a single-shot input.
package sandbox

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Sandbox is the resolved state of the terminal sandbox for one
// permission gate. Zero value is "disabled" — every path is allowed —
// which matches the reference's default (`enableTerminalSandbox`
// defaults to false, recovered from the 1.3.4 `setConfig` recovery at
// `out/local-macos/strings-resolved.txt`).
type Sandbox struct {
	// Enabled switches the sandbox on. When false the gate ignores
	// every other field and every command is allowed.
	Enabled bool
	// Cwd is the workspace cwd; sandboxed commands may always read
	// and write the cwd and its descendants.
	Cwd string
	// ArtifactDir is the agent's artifact directory. The path itself
	// may be empty when the artifact layer is disabled — empty paths
	// are skipped by Allowed.
	ArtifactDir string
	// ScratchDir is the per-conversation scratch directory. Empty
	// when no conversation is active.
	ScratchDir string
	// ExtraPaths are further directories a sandboxed command may read or
	// write. They are not part of the reference's own three roots
	// (workspace, artifact dir, scratch dir); a subagent's worktree is
	// the reason they exist — the child runs in its own directory, and
	// the gate that guards its commands has to allow that directory.
	ExtraPaths []string
}

// Resolve builds the sandbox for the cwd + antigravity home. The home
// is the v4 binary's canonical config home (auth.AntigravityConfigHome),
// so the artifact dir is always underneath it. conversationID is the
// per-conversation brain id; an empty id disables the scratch path
// rather than using a top-level scratch dir.
//
// enabled is read straight from settings.json's
// `enableTerminalSandbox` key. The boolean-vs-string form the TUI /
// /config panel accepts is collapsed here.
func Resolve(enabled bool, cwd, antigravityHome, conversationID string) Sandbox {
	s := Sandbox{Enabled: enabled, Cwd: cwd}
	if antigravityHome != "" {
		// Artifact dir lives under the antigravity home — the v4
		// binary's recovered `AntigravityArtifactsPath` is the same
		// join. Pinning to "artifacts" matches the recovered path
		// observed by `find ~/.gemini/antigravity-cli -type d
		// -name scratch` (out/local-macos/FINDINGS.md §"scratch").
		s.ArtifactDir = filepath.Join(antigravityHome, "artifacts")
	}
	if antigravityHome != "" && conversationID != "" {
		// Per-conversation scratch lives one level under
		// `brain/<conv-id>/scratch` — the recovered layout is
		// `~/.gemini/antigravity-cli/brain/<uuid>/scratch`, observed
		// on the live machine during cycle 228 verification.
		s.ScratchDir = filepath.Join(antigravityHome, "brain", conversationID, "scratch")
	}
	return s
}

// AllowedPaths returns the list of paths that are always allowed in
// the sandbox. Empty paths (an unset artifact or scratch dir) are
// skipped so the slice carries only real on-disk locations. Order
// matches the canonical order: cwd, artifact, scratch.
func (s Sandbox) AllowedPaths() []string {
	if !s.Enabled {
		return nil
	}
	var out []string
	for _, p := range []string{s.Cwd, s.ArtifactDir, s.ScratchDir} {
		if p != "" {
			out = append(out, p)
		}
	}
	for _, p := range s.ExtraPaths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Allowed reports whether path is inside one of the allow-listed
// directories. Comparison is `filepath.HasPrefix` after
// `filepath.Clean`, so trailing slashes and `./` prefixes do not
// produce false negatives. An empty path matches when one of the
// allowed dirs is empty, which never happens in practice because
// empty entries are stripped by AllowedPaths.
//
// When the sandbox is disabled Allowed is true for every input, so a
// caller can short-circuit with `if s.Allowed(p) { ... }`.
func (s Sandbox) Allowed(p string) bool {
	if !s.Enabled {
		return true
	}
	p = filepath.Clean(p)
	if p == "" || p == "." {
		return true
	}
	for _, root := range s.AllowedPaths() {
		root = filepath.Clean(root)
		if root == "" {
			continue
		}
		if p == root {
			return true
		}
		// HasPrefix requires a trailing separator, otherwise
		// /foo/bar would match /foo/barbaz. Clean + the explicit
		// separator check covers that case.
		if strings.HasPrefix(p, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// tokenPathRegexp matches a path-shaped token in a shell command
// string. It is intentionally conservative — capturing the literal
// after a quoted segment or a redirection target — so we do not
// mis-classify `echo "$HOME"` as a path. The captured group is the
// first path-shaped token; full extraction would need the mvdan.cc/sh
// AST, which is overkill for the sandbox check (a false negative only
// makes the user see one extra prompt).
var tokenPathRegexp = regexp.MustCompile(`(?:^|[\s|;<>"'\x60\(\{])(/[^\s|;<>"'\x60\(\)\}]+|~/[^\s|;<>"'\x60\(\)\}]+|\./[^\s|;<>"'\x60\(\)\}]+|[A-Za-z0-9._-]+/[^\s|;<>"'\x60\(\)\}]+)`)

// CommandResult is the outcome of CheckCommand. Outside means at least
// one extracted path resolves to something the sandbox does not
// allow. Touched lists the absolute paths the command reads or
// writes; the caller decides how to surface Outside to the user.
type CommandResult struct {
	// Outside is true when at least one path the command touches
	// falls outside the allow-list. Disabled sandboxes always
	// report (Outside=false, Touched=nil).
	Outside bool
	// Touched is the list of paths extracted from the command,
	// before ~ / $HOME / relative resolution, so the caller can
	// show the user the offending token.
	Touched []string
}

// CheckCommand inspects a shell command string and reports whether
// every path-shaped token lives inside the allow-list. The check is
// best-effort — it does not parse the shell — so a token like `~/..`
// or `$TMPDIR/x` is left as-is and compared against the allowed
// prefix. The cycle 228 deliverable documents this as a known gap:
//
//	[A] tokenPathRegexp does not substitute shell variables; a
//	command that uses `$HOME/foo` always lands in the touch list and
//	is treated as outside the sandbox. AGY 1.2.10 ships the same
//	limitation — the binary's regex-driven extraction cannot expand
//	$HOME either.
//
// RunCommand / RunShellCommand in cmd/wagyu/internal/tools execute
// after the gate, so the gate's outside-call is the agent-visible
// signal; a bypass through RunBypassSandbox is a one-shot path that
// does not persist (see package doc).
func (s Sandbox) CheckCommand(cmd string) CommandResult {
	if !s.Enabled {
		return CommandResult{}
	}
	matches := tokenPathRegexp.FindAllStringSubmatch(cmd, -1)
	if len(matches) == 0 {
		return CommandResult{Touched: nil, Outside: false}
	}
	touched := make([]string, 0, len(matches))
	outside := false
	for _, m := range matches {
		tok := m[1]
		touched = append(touched, tok)
		if !s.Allowed(tok) {
			outside = true
		}
	}
	return CommandResult{Touched: touched, Outside: outside}
}

// Bypassed is the per-call bypass input. It is intentionally NOT a
// field on Sandbox so the gate cannot accidentally persist a "yes"
// between calls: the cycle 228 contract is that every call is
// sandboxed fresh, and only the caller's explicit `Bypassed: true`
// argument on that one call is honoured.
//
// The recovered binary's `BypassSandbox: true` option (out/local-macos/
// strings-resolved.txt) is wired the same way — a single-shot input
// that lifts the sandbox for the duration of one Run().
func (s Sandbox) CheckCommandWithBypass(cmd string, bypassed bool) CommandResult {
	if bypassed {
		return CommandResult{}
	}
	return s.CheckCommand(cmd)
}

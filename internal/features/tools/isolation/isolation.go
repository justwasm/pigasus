// Package isolation implements the /branch and /share workspace sandbox.
//
// The reference's /branch and /share commands fork the live workspace
// into an isolated copy, run there, and let the parent session continue
// as if nothing changed. The sandbox here is the in-process analogue of
// that subprocess: snapshot parent state on entry, hand the work a
// sandbox-scoped view (cwd + env keys + a registration set), capture
// anything the work writes, and restore parent state on exit so the
// parent's transcript, CWD, env, and skill/agent registrations are
// exactly what they were before the slash command ran.
//
// The contract is:
//
//	iw := isolation.New()
//	iw.Capture(parentCwd, parentEnvKeys)   // one-time parent snapshot
//	iw.Enter()                              // initialize sandbox state
//	defer iw.Exit()                         // restore parent state
//	// ... work that uses iw.Cwd(), iw.Getenv(), iw.RegisterSkill(...), ...
//	iw.CaptureOutput(...)                   // append to transcript buffer
//	// On Exit, parent state is restored and Output() is drained into
//	// the caller's transcript.
//
// Everything in this package is pure session-state bookkeeping: it
// never touches the process's own cwd or environment, so a caller can
// run several sandboxes side by side.
package isolation

import (
	"fmt"
	"sort"
	"strings"
)

type Workspace struct {
	// parentCwd is the cwd captured at Capture time.
	parentCwd string
	// parentEnvKeys is the list of environment-variable names whose
	// values were snapshotted at Capture time. Only these keys are
	// touched by Setenv / Unsetenv during the sandbox run, so the
	// parent's wider env (PATH, locale, agent-specific vars the
	// sandbox does not know about) is invisible to the work.
	parentEnvKeys []string
	// parentEnv is the captured value for each parentEnvKeys entry
	// (empty string for unset keys; the parent's nil-vs-empty
	// distinction is lost on purpose — the slice the struct hands
	// the work is a plain string map).
	parentEnv map[string]string

	// cwd is the sandbox-scoped working directory the work sees.
	// Defaults to parentCwd on Enter; settable via SetCwd so the
	// /branch command can land the worker in a fresh temp dir
	// without leaking the parent's path.
	cwd string
	// env is the sandbox-scoped environment. Only keys listed in
	// parentEnvKeys appear here; reading an unknown key returns ""
	// to keep the sandbox's view of the world narrow.
	env map[string]string

	// registeredSkills is the set of slash-command skill names
	// registered inside the sandbox. The parent's skill catalogue
	// is the file-loaded one in slash_skills.go; the sandbox-local
	// registration table is what /branch / /share add to during a
	// run. The parent never sees these names.
	registeredSkills map[string]bool
	// registeredAgents mirrors the above for subagent names.
	registeredAgents map[string]bool

	// output is the captured transcript. The /branch and /share
	// handlers feed their result lines in here; on Exit the
	// caller drains it into the parent runMode's response buffer.
	output []string

	// active reports whether the sandbox is currently entered.
	// Enter sets it true; Exit sets it false. Operations against
	// a non-active sandbox return zero values rather than
	// panicking, so a deferred Exit that races with a panic-recover
	// in the work is harmless.
	active bool
}

// EnvKeys is the canonical list of environment
// variable names the /branch and /share sandbox snapshots and scopes.
// The list is intentionally short: every key the runtime and the
// clone's helper commands read at execution time (CWD-aware tooling,
// TMPDIR for child processes, HOME for config-file lookup, AGY_* for
// clone-side settings). Keys outside this list remain invisible to
// the sandbox work and are never modified.
var EnvKeys = []string{
	"HOME",
	"TMPDIR",
	"PWD",
	"AGY_HOME",
	"ANTIGRAVITY_HOME",
	"AGY_CONFIG_DIR",
}

// New returns a zero-value sandbox. Capture must be
// called before Enter; calling Enter without Capture is a no-op so
// unit tests can construct a workspace without a parent session.
func New() *Workspace {
	return &Workspace{
		env:              make(map[string]string),
		registeredSkills: make(map[string]bool),
		registeredAgents: make(map[string]bool),
	}
}

// Capture snapshots the parent session's CWD and the listed env keys.
// The map argument is the full parent environment; only the keys
// listed in EnvKeys are recorded. Call once per
// workspace, before Enter.
func (iw *Workspace) Capture(parentCwd string, parentEnv map[string]string) {
	if iw == nil {
		return
	}
	iw.parentCwd = parentCwd
	iw.parentEnvKeys = append([]string(nil), EnvKeys...)
	iw.parentEnv = make(map[string]string, len(iw.parentEnvKeys))
	for _, k := range iw.parentEnvKeys {
		iw.parentEnv[k] = parentEnv[k]
	}
	// Seed the sandbox env with the captured values so the work
	// starts with the parent's view of the world. Getenv later
	// returns from this map; Setenv mutates it; on Exit the
	// captured parent values are reapplied to the parent session.
	for k, v := range iw.parentEnv {
		iw.env[k] = v
	}
}

// Enter initializes the sandbox view of the world. It MUST be called
// after Capture; without a snapshot, the sandbox starts blank and
// Exit cannot restore anything (which is fine for unit tests but
// never what production callers want).
//
// After Enter:
//   - iw.Cwd() returns parentCwd
//   - iw.Getenv(k) returns the captured parent value (or "" if
//     parentEnv never had k)
//   - iw.RegisterSkill / iw.RegisterAgent accept new names
//   - iw.CaptureOutput appends to the transcript buffer
func (iw *Workspace) Enter() {
	if iw == nil {
		return
	}
	if iw.parentCwd != "" {
		iw.cwd = iw.parentCwd
	} else if wd, ok := iw.env["PWD"]; ok && wd != "" {
		iw.cwd = wd
	} else {
		iw.cwd = "."
	}
	iw.active = true
}

// Exit restores the parent snapshot and clears the sandbox-local
// state. The returned slice is the captured transcript, ready to be
// drained into the parent session's response buffer. Exit is
// idempotent — calling it twice returns the same output and never
// double-restores.
func (iw *Workspace) Exit() []string {
	if iw == nil {
		return nil
	}
	if !iw.active {
		// Already exited; return whatever was captured (may be
		// nil for a workspace that never ran anything).
		return append([]string(nil), iw.output...)
	}
	// Restore the parent env values for every captured key,
	// including any the sandbox Unsetenv'd. Iterating iw.env
	// would skip those keys because Unsetenv already deleted
	// them; iterating parentEnvKeys guarantees we re-establish
	// the parent's view for the whole allow-list, regardless of
	// how the sandbox mutated it.
	for _, k := range iw.parentEnvKeys {
		if v, ok := iw.parentEnv[k]; ok {
			iw.env[k] = v
		} else {
			delete(iw.env, k)
		}
	}
	// Restore cwd.
	iw.cwd = iw.parentCwd
	// Clear sandbox-local registration. The parent's catalogue
	// is file-loaded and therefore not in the workspace; we just
	// drop the local additions.
	iw.registeredSkills = make(map[string]bool)
	iw.registeredAgents = make(map[string]bool)
	// Reset the captured transcript buffer so the next Enter()
	// gets a fresh sandbox-local view. Exit returns the
	// pre-reset snapshot so callers can drain it into the parent
	// transcript.
	out := append([]string(nil), iw.output...)
	iw.output = nil
	iw.active = false
	return out
}

// Cwd returns the sandbox-scoped working directory. Callers that
// resolve relative paths in the sandbox MUST go through this — never
// os.Getwd, which sees the process-global view and would defeat the
// isolation.
func (iw *Workspace) Cwd() string {
	if iw == nil {
		return ""
	}
	return iw.cwd
}

// SetCwd updates the sandbox working directory. It does NOT mutate
// the parent's cwd; on Exit the parentCwd is reapplied regardless of
// what SetCwd set. Used by /branch to land the sandbox work in a
// fresh temp directory.
func (iw *Workspace) SetCwd(cwd string) {
	if iw == nil {
		return
	}
	iw.cwd = cwd
}

// Getenv reads the sandbox-scoped environment. Keys not in the
// captured parentEnvKeys list return "" — the sandbox's view of the
// world is intentionally narrow.
func (iw *Workspace) Getenv(key string) string {
	if iw == nil {
		return ""
	}
	return iw.env[key]
}

// Setenv writes a value into the sandbox-scoped environment. Only
// captured keys (those listed in EnvKeys) are
// writable; other keys are silently dropped so the work cannot
// inject, e.g., PATH entries the parent never authorised.
func (iw *Workspace) Setenv(key, value string) bool {
	if iw == nil {
		return false
	}
	if _, ok := iw.parentEnv[key]; !ok {
		return false
	}
	iw.env[key] = value
	return true
}

// Unsetenv removes a key from the sandbox-scoped environment. Same
// key-allow-list restriction as Setenv.
func (iw *Workspace) Unsetenv(key string) bool {
	if iw == nil {
		return false
	}
	if _, ok := iw.parentEnv[key]; !ok {
		return false
	}
	delete(iw.env, key)
	return true
}

// RegisterSkill adds a sandbox-local skill name. The parent runMode's
// skill catalogue is unaffected — this map is purely the workspace's
// view of "skills added during this run". Returns true if the name
// was new, false if it was already registered.
func (iw *Workspace) RegisterSkill(name string) bool {
	if iw == nil {
		return false
	}
	if iw.registeredSkills[name] {
		return false
	}
	iw.registeredSkills[name] = true
	return true
}

// RegisterAgent is the agent-side counterpart of RegisterSkill.
func (iw *Workspace) RegisterAgent(name string) bool {
	if iw == nil {
		return false
	}
	if iw.registeredAgents[name] {
		return false
	}
	iw.registeredAgents[name] = true
	return true
}

// RegisteredSkills returns a sorted copy of the sandbox's registered
// skill names. Sorted so tests get deterministic ordering; callers
// that want the raw map should iterate it directly.
func (iw *Workspace) RegisteredSkills() []string {
	if iw == nil {
		return nil
	}
	out := make([]string, 0, len(iw.registeredSkills))
	for k := range iw.registeredSkills {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RegisteredAgents is the agent-side counterpart of RegisteredSkills.
func (iw *Workspace) RegisteredAgents() []string {
	if iw == nil {
		return nil
	}
	out := make([]string, 0, len(iw.registeredAgents))
	for k := range iw.registeredAgents {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CaptureOutput appends a line to the sandbox transcript buffer. The
// line is stored verbatim (no styling), and is drained into the
// parent runMode's response buffer by the caller after Exit.
func (iw *Workspace) CaptureOutput(lines ...string) {
	if iw == nil {
		return
	}
	iw.output = append(iw.output, lines...)
}

// Capturef formats with fmt.Sprintf and appends in one call, so
// slash handlers don't have to format-and-then-append on every line.
func (iw *Workspace) Capturef(format string, args ...any) {
	if iw == nil {
		return
	}
	iw.output = append(iw.output, fmt.Sprintf(format, args...))
}

// Output returns the captured transcript lines. The slice is a copy
// — callers can mutate it freely without disturbing future Enter()
// runs against the same struct.
func (iw *Workspace) Output() []string {
	if iw == nil {
		return nil
	}
	return append([]string(nil), iw.output...)
}

// Run wraps Enter + the callback + Exit in a single call. The
// returned slice is whatever Exit captured. Run is a convenience —
// callers that need to do work between Enter and Exit (e.g. decide
// whether to run based on a captured env value) call them directly.
func (iw *Workspace) Run(fn func(iw *Workspace)) []string {
	if iw == nil || fn == nil {
		return nil
	}
	iw.Enter()
	fn(iw)
	return iw.Exit()
}

// Active reports whether the sandbox is currently entered. Useful
// for assertion code that checks isolation lifecycle.
func (iw *Workspace) Active() bool {
	if iw == nil {
		return false
	}
	return iw.active
}

// EnvSnapshot is a helper that returns the parent environment
// as a plain map. We can't import os into every test file just to
// build a map from os.Environ(); this thin wrapper keeps the
// conversion in one place. Empty entries (KEY= with no value) are
// preserved as empty strings so the snapshot/restore cycle is
// symmetric.
func EnvSnapshot(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}

// BranchFeedback builds the user-facing lines /branch writes
// into the captured transcript.
func BranchFeedback(args, parentCwd string) []string {
	branchName := strings.TrimSpace(args)
	if branchName == "" {
		branchName = "branch-isolate"
	}
	return []string{
		fmt.Sprintf("Branched workspace: %s", parentCwd),
		fmt.Sprintf("Sandbox branch: %s", branchName),
		"Isolated copy: would run in a fresh git worktree",
		"State changes (cwd, env, registrations) will not affect parent session.",
	}
}

// ShareFeedback is the share-side counterpart of BranchFeedback.
func ShareFeedback(args, parentCwd string) []string {
	shareName := strings.TrimSpace(args)
	if shareName == "" {
		shareName = "share-isolate"
	}
	return []string{
		fmt.Sprintf("Shared workspace: %s", parentCwd),
		fmt.Sprintf("Sandbox share: %s", shareName),
		"Isolated copy: read-only view of parent env, no side-effects.",
		"State changes (cwd, env, registrations) will not affect parent session.",
	}
}

// FeedbackFn is the constructor signature for feedback lines.
type FeedbackFn func(args, parentCwd string) []string

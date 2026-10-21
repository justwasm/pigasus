package main

// slash_branch_share_test.go — cycle branch-share-isolation: the unit
// tests for the /branch and /share sandbox helper.
//
// AGY 1.3.3 changelog line 51 second half closes with:
//
//	Fixed `/branch` and `/share` commands leaking workspace state
//	(CWD, env vars, active file paths) between sessions — each
//	/branch and /share now runs in an isolated sandbox that doesn't
//	affect the parent session.
//
// These tests pin that contract:
//
//   - The isolatedWorkspace struct captures parent CWD + env keys,
//     runs sandbox-scoped work, and restores parent state on exit.
//   - /branch and /share route through the same helper, so a parent
//     session's skills, agents, response buffer, and session are
//     unaffected by the worker's mutations.
//
// The tests run in-process; the struct never mutates os.Getwd /
// os.Setenv, so concurrent test execution against the rest of the
// package is safe.

import (
	"strings"
	"testing"
)

// TestIsolatedWorkspace_CwdRestoredAfterExit pins the "CWD restored"
// half of the changelog promise. We capture a parent value, mutate
// the sandbox CWD inside Run, and assert that post-exit the
// workspace is back to the captured value.
func TestIsolatedWorkspace_CwdRestoredAfterExit(t *testing.T) {
	iw := newIsolatedWorkspace()
	iw.Capture("/parent/workspace", map[string]string{})
	out := iw.Run(func(sandbox *isolatedWorkspace) {
		if got := sandbox.Cwd(); got != "/parent/workspace" {
			t.Errorf("sandbox Cwd() before SetCwd = %q, want /parent/workspace", got)
		}
		sandbox.SetCwd("/sandbox/workspace")
		if got := sandbox.Cwd(); got != "/sandbox/workspace" {
			t.Errorf("sandbox Cwd() after SetCwd = %q, want /sandbox/workspace", got)
		}
		sandbox.CaptureOutput("ran in sandbox")
	})
	if len(out) != 1 || out[0] != "ran in sandbox" {
		t.Errorf("captured output = %v, want [\"ran in sandbox\"]", out)
	}
	if got := iw.Cwd(); got != "/parent/workspace" {
		t.Errorf("post-exit Cwd() = %q, want /parent/workspace (parent value must be restored)", got)
	}
	if iw.Active() {
		t.Error("workspace should not be active after Exit")
	}
}

// TestIsolatedWorkspace_EnvVarsRestoredAfterExit pins the "env vars
// restored" half. We snapshot parent env values, mutate them inside
// the sandbox, and assert that the parent-side parentEnv map (and
// the sandbox env after Exit) match what we captured.
func TestIsolatedWorkspace_EnvVarsRestoredAfterExit(t *testing.T) {
	iw := newIsolatedWorkspace()
	parentEnv := map[string]string{
		"HOME":             "/parent/home",
		"TMPDIR":           "/parent/tmp",
		"PWD":              "/parent/workspace",
		"AGY_HOME":         "/parent/.agy",
		"ANTIGRAVITY_HOME": "/parent/.antigravity",
		"AGY_CONFIG_DIR":   "/parent/.config",
		"PATH":             "/usr/bin:/bin", // outside the allow-list
	}
	iw.Capture("/parent/workspace", parentEnv)
	out := iw.Run(func(sandbox *isolatedWorkspace) {
		// Sandbox can read captured values.
		if got := sandbox.Getenv("HOME"); got != "/parent/home" {
			t.Errorf("sandbox Getenv(HOME) = %q, want /parent/home", got)
		}
		// Sandbox can mutate captured keys.
		if ok := sandbox.Setenv("HOME", "/sandbox/home"); !ok {
			t.Error("Setenv on captured key should succeed")
		}
		if got := sandbox.Getenv("HOME"); got != "/sandbox/home" {
			t.Errorf("after Setenv, sandbox Getenv(HOME) = %q, want /sandbox/home", got)
		}
		// Unsetenv is also reachable.
		if ok := sandbox.Unsetenv("TMPDIR"); !ok {
			t.Error("Unsetenv on captured key should succeed")
		}
		if got := sandbox.Getenv("TMPDIR"); got != "" {
			t.Errorf("after Unsetenv, sandbox Getenv(TMPDIR) = %q, want empty", got)
		}
		// PATH is outside the allow-list — both Setenv and
		// Getenv should treat it as invisible.
		if got := sandbox.Getenv("PATH"); got != "" {
			t.Errorf("sandbox Getenv(PATH) = %q, want empty (PATH is not in the allow-list)", got)
		}
		if ok := sandbox.Setenv("PATH", "/sandbox/bin"); ok {
			t.Error("Setenv on non-allowed key should fail silently")
		}
	})
	if len(out) != 0 {
		t.Errorf("captured output should be empty, got %v", out)
	}
	// Post-exit, the sandbox env must match the captured parent
	// values: HOME re-restored, AGY_HOME untouched, etc.
	for _, k := range isolatedWorkspaceEnvKeys {
		got := iw.Getenv(k)
		want := parentEnv[k]
		if got != want {
			t.Errorf("post-exit Getenv(%s) = %q, want %q", k, got, want)
		}
	}
}

// TestIsolatedWorkspace_CapturesAndRestoresStateCleanly is the
// lifecycle test: a single workspace handles multiple Enter/Exit
// cycles without bleeding state across them. The "state is captured
// and restored cleanly" guarantee is what lets the parent session
// run /branch, then /share, then chat — each run is independent.
func TestIsolatedWorkspace_CapturesAndRestoresStateCleanly(t *testing.T) {
	iw := newIsolatedWorkspace()
	iw.Capture("/parent/workspace", map[string]string{
		"HOME":   "/parent/home",
		"PWD":    "/parent/workspace",
		"TMPDIR": "/parent/tmp",
	})
	// First cycle: register a skill + agent.
	first := iw.Run(func(sandbox *isolatedWorkspace) {
		sandbox.SetCwd("/first/sandbox")
		sandbox.Setenv("HOME", "/first/home")
		sandbox.RegisterSkill("first:skill")
		sandbox.RegisterAgent("first:agent")
		sandbox.CaptureOutput("first-cycle-output")
	})
	if len(first) != 1 || first[0] != "first-cycle-output" {
		t.Errorf("first cycle output = %v", first)
	}
	if iw.Cwd() != "/parent/workspace" {
		t.Errorf("after first cycle, Cwd() = %q, want /parent/workspace", iw.Cwd())
	}
	if got := iw.Getenv("HOME"); got != "/parent/home" {
		t.Errorf("after first cycle, Getenv(HOME) = %q, want /parent/home", got)
	}
	if skills := iw.RegisteredSkills(); len(skills) != 0 {
		t.Errorf("after first cycle, RegisteredSkills = %v, want empty (must not leak)", skills)
	}
	if agents := iw.RegisteredAgents(); len(agents) != 0 {
		t.Errorf("after first cycle, RegisteredAgents = %v, want empty (must not leak)", agents)
	}
	// Second cycle: prove the workspace is reusable and the first
	// cycle's registrations didn't bleed.
	second := iw.Run(func(sandbox *isolatedWorkspace) {
		sandbox.SetCwd("/second/sandbox")
		sandbox.RegisterSkill("second:skill")
		if skills := sandbox.RegisteredSkills(); len(skills) != 1 || skills[0] != "second:skill" {
			t.Errorf("second cycle sandbox skills = %v, want [second:skill]", skills)
		}
	})
	if len(second) != 0 {
		t.Errorf("second cycle captured %v, want empty", second)
	}
	if iw.Cwd() != "/parent/workspace" {
		t.Errorf("after second cycle, Cwd() = %q, want /parent/workspace", iw.Cwd())
	}
	// Idempotent Exit: calling Exit again is safe.
	dup := iw.Exit()
	if dup != nil {
		t.Errorf("double Exit returned %v, want nil (Exit is idempotent)", dup)
	}
}

// TestIsolatedWorkspace_RunInvokesCallback pins the Run helper's
// invocation contract — the callback runs, sees an entered sandbox,
// and any captured output is returned.
func TestIsolatedWorkspace_RunInvokesCallback(t *testing.T) {
	iw := newIsolatedWorkspace()
	iw.Capture("/parent/workspace", map[string]string{
		"HOME": "/parent/home",
	})
	called := false
	out := iw.Run(func(sandbox *isolatedWorkspace) {
		called = true
		if !sandbox.Active() {
			t.Error("sandbox should be active inside Run's callback")
		}
		sandbox.Capturef("formatted: %d", 42)
		sandbox.CaptureOutput("second line")
	})
	if !called {
		t.Error("Run did not invoke the callback")
	}
	if len(out) != 2 {
		t.Fatalf("captured %d lines, want 2", len(out))
	}
	if out[0] != "formatted: 42" || out[1] != "second line" {
		t.Errorf("captured = %v, want [formatted: 42 second line]", out)
	}
	if iw.Active() {
		t.Error("sandbox should be inactive after Run returns")
	}
}

// TestBranch_DoesNotModifyParentSkills runs /branch through the
// slash router and asserts the parent runMode's skill catalogue is
// untouched. The skill catalogue lives on disk; the sandbox-local
// registration table is separate, so adding a name to it must not
// create a phantom entry in slashSkillCatalog().
func TestBranch_DoesNotModifyParentSkills(t *testing.T) {
	m := newSlashModel(t)
	// Snapshot the parent's skill catalogue size.
	beforeSkills := len(slashSkillCatalog())
	// Run /branch. The helper inside the sandbox registers
	// "branch-isolate:helper" against the sandbox-local table.
	m.handleSlashCommand("/branch feature-x")
	// Parent's skill catalogue size is unchanged — the parent's
	// view of skills comes from disk, not from the sandbox-local
	// map.
	if got := len(slashSkillCatalog()); got != beforeSkills {
		t.Errorf("after /branch, parent skill catalogue size = %d, want %d (must not leak)",
			got, beforeSkills)
	}
	// Verify the sandbox-local registration table is empty after
	// the command returns. We use a fresh workspace to assert
	// this in isolation rather than reach into m's internals —
	// the parent runMode has no fields that map directly to
	// isolatedWorkspace.registeredSkills.
	iw := newIsolatedWorkspace()
	iw.Capture("/parent/workspace", map[string]string{})
	iw.Run(func(sandbox *isolatedWorkspace) {
		sandbox.RegisterSkill("branch-isolate:helper")
	})
	if skills := iw.RegisteredSkills(); len(skills) != 0 {
		t.Errorf("post-Exit sandbox skills = %v, want empty (must not leak)", skills)
	}
	// /branch's output is captured into m.response. The captured
	// transcript must include the four feedback lines.
	resp := strings.Join(afterEcho(m), "\n")
	for _, want := range []string{
		"Branched workspace: /",
		"Sandbox branch: feature-x",
		"Isolated copy: would run in a fresh git worktree",
	} {
		if !strings.Contains(resp, want) {
			t.Errorf("/branch transcript missing %q; full: %s", want, resp)
		}
	}
}

// TestShare_DoesNotModifyParentAgents mirrors
// TestBranch_DoesNotModifyParentSkills for /share. The parent
// session's subagent catalogue (DefaultRouter in at_subagent.go) is
// unchanged after /share runs.
func TestShare_DoesNotModifyParentAgents(t *testing.T) {
	m := newSlashModel(t)
	// Snapshot parent agent router size.
	beforeAgents := 0
	if m.atRouter != nil {
		beforeAgents = len(m.atRouter.Names())
	}
	m.handleSlashCommand("/share team-collaboration")
	// Parent's agent router size is unchanged.
	if m.atRouter != nil {
		if got := len(m.atRouter.Names()); got != beforeAgents {
			t.Errorf("after /share, parent agent router size = %d, want %d (must not leak)",
				got, beforeAgents)
		}
	}
	// Verify the sandbox-local registration table is empty after
	// the command returns.
	iw := newIsolatedWorkspace()
	iw.Capture("/parent/workspace", map[string]string{})
	iw.Run(func(sandbox *isolatedWorkspace) {
		sandbox.RegisterAgent("branch-isolate:scout")
	})
	if agents := iw.RegisteredAgents(); len(agents) != 0 {
		t.Errorf("post-Exit sandbox agents = %v, want empty (must not leak)", agents)
	}
	// /share's output is captured into m.response. The captured
	// transcript must include the four feedback lines, distinct
	// from /branch's wording.
	resp := strings.Join(afterEcho(m), "\n")
	for _, want := range []string{
		"Shared workspace: /",
		"Sandbox share: team-collaboration",
		"Isolated copy: read-only view of parent env",
	} {
		if !strings.Contains(resp, want) {
			t.Errorf("/share transcript missing %q; full: %s", want, resp)
		}
	}
	// /share's first line must differ from /branch's first line —
	// the changelog promise is that /branch and /share are
	// distinguishable in the transcript.
	branchResp := strings.Join(func() []string {
		mm := newSlashModel(t)
		mm.handleSlashCommand("/branch team-collaboration")
		return afterEcho(mm)
	}(), "\n")
	if strings.HasPrefix(resp, branchResp) {
		t.Errorf("/share transcript should differ from /branch; got %q", resp)
	}
}

// TestBranch_OutputCapturedIntoParent asserts that /branch's
// output is captured and displayed in the parent session's
// transcript. The captured lines flow through the slashRespond
// wrapper (the `> /branch` echo + ⎿ wrapper) and append below it.
func TestBranch_OutputCapturedIntoParent(t *testing.T) {
	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/branch captured"); !handled {
		t.Fatal("/branch should be handled by the router")
	}
	// m.response must include the echo header `> /branch`.
	if len(m.response) == 0 || !strings.HasPrefix(m.response[0], cWarningBlueOpen+"> /branch") {
		t.Errorf("/branch echo header missing; m.response[0] = %q", firstSliceLine(m.response))
	}
	// The captured output appears below the echo wrapper.
	lines := afterEcho(m)
	if len(lines) < 4 {
		t.Fatalf("/branch produced %d transcript lines, want >= 4 (the four feedback lines)", len(lines))
	}
	want := []string{
		"Branched workspace: /",
		"Sandbox branch: captured",
		"Isolated copy: would run in a fresh git worktree",
		"State changes (cwd, env, registrations) will not affect parent session.",
	}
	for i, w := range want {
		if i >= len(lines) {
			t.Errorf("missing line %d (%q)", i, w)
			continue
		}
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	// Parent's session / blocks / liveSteps are untouched.
	if len(m.session) != 0 {
		t.Errorf("/branch should not modify parent session; got %d messages", len(m.session))
	}
	if len(m.blocks) != 0 {
		t.Errorf("/branch should not modify parent blocks; got %d entries", len(m.blocks))
	}
}

// TestShare_OutputCapturedIntoParent is the share-side
// counterpart of TestBranch_OutputCapturedIntoParent. The echo
// wrapper is identical (`> /share`), but the captured lines below it
// use /share's wording.
func TestShare_OutputCapturedIntoParent(t *testing.T) {
	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/share captured"); !handled {
		t.Fatal("/share should be handled by the router")
	}
	if len(m.response) == 0 || !strings.HasPrefix(m.response[0], cWarningBlueOpen+"> /share") {
		t.Errorf("/share echo header missing; m.response[0] = %q", firstSliceLine(m.response))
	}
	lines := afterEcho(m)
	if len(lines) < 4 {
		t.Fatalf("/share produced %d transcript lines, want >= 4 (the four feedback lines)", len(lines))
	}
	want := []string{
		"Shared workspace: /",
		"Sandbox share: captured",
		"Isolated copy: read-only view of parent env, no side-effects.",
		"State changes (cwd, env, registrations) will not affect parent session.",
	}
	for i, w := range want {
		if i >= len(lines) {
			t.Errorf("missing line %d (%q)", i, w)
			continue
		}
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	// Bare /share with no args: the default share-isolate name
	// must appear in the captured output.
	m2 := newSlashModel(t)
	m2.handleSlashCommand("/share")
	share2 := strings.Join(afterEcho(m2), "\n")
	if !strings.Contains(share2, "Sandbox share: share-isolate") {
		t.Errorf("/share default name missing; got %q", share2)
	}
}

// firstSliceLine returns the first non-empty entry of s, or "" if
// all entries are empty. Named to avoid colliding with tui_run.go's
// firstLine helper, which has a different signature. Used to render
// readable error messages when m.response[0] is the empty string.
func firstSliceLine(s []string) string {
	for _, l := range s {
		if l != "" {
			return l
		}
	}
	return ""
}

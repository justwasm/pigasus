package isolation

import (
	"testing"
)

// TestIsolatedWorkspace_CwdRestoredAfterExit pins the "CWD restored"
// half of the changelog promise. We capture a parent value, mutate
// the sandbox CWD inside Run, and assert that post-exit the
// workspace is back to the captured value.
func TestIsolatedWorkspace_CwdRestoredAfterExit(t *testing.T) {
	iw := New()
	iw.Capture("/parent/workspace", map[string]string{})
	out := iw.Run(func(sandbox *Workspace) {
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
	iw := New()
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
	out := iw.Run(func(sandbox *Workspace) {
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
	for _, k := range EnvKeys {
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
	iw := New()
	iw.Capture("/parent/workspace", map[string]string{
		"HOME":   "/parent/home",
		"PWD":    "/parent/workspace",
		"TMPDIR": "/parent/tmp",
	})
	// First cycle: register a skill + agent.
	first := iw.Run(func(sandbox *Workspace) {
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
	second := iw.Run(func(sandbox *Workspace) {
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
	iw := New()
	iw.Capture("/parent/workspace", map[string]string{
		"HOME": "/parent/home",
	})
	called := false
	out := iw.Run(func(sandbox *Workspace) {
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

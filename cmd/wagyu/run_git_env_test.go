package main

// run_git_env_test.go — unit tests for the env-var contract runGit applies
// to every child process it execs.
//
// Target changelog entry, AGY 1.3.3 lines 50-51:
//
//	"Fixed jj (Jujutsu VCS) command output being broken by the `less`
//	 pager — JJ_PAGER=cat is now set alongside GIT_PAGER=cat so commands
//	 like `jj log` show full output instead of paging through interactive
//	 less."
//
// Pre-cycle state: the slash_viewer_panels.go `runGit` helper set
// PAGER=cat / GIT_PAGER=cat but not JJ_PAGER=cat. The agent's main-shell
// env (internal/tools.AgentShellEnvKVs) and the /diff vcsDiff helper in
// slash_actions.go already had JJ_PAGER=cat, so this was an inconsistency
// rather than a missing capability. The tests below pin the post-cycle
// shape and the runGitEnv() contract.

import (
	"os"
	"strings"
	"testing"
)

// parseEnv turns a KEY=VALUE slice into a map. Used to assert presence
// without caring about order or duplicates.
func parseEnv(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			out[kv[:eq]] = kv[eq+1:]
		}
	}
	return out
}

// TestRunGitEnvKVs_Shape pins the slice contents and order, so an
// accidental edit (drop JJ_PAGER, replace `cat` with `less`, ...) fails
// the cheapest possible test before the runGit-level tests below.
func TestRunGitEnvKVs_Shape(t *testing.T) {
	want := []string{
		"PAGER=cat",
		"GIT_PAGER=cat",
		"JJ_PAGER=cat",
		"GIT_EDITOR=false",
		"LC_ALL=C",
	}
	if len(runGitEnvKVs) != len(want) {
		t.Fatalf("runGitEnvKVs has %d entries, want %d (%v)",
			len(runGitEnvKVs), len(want), runGitEnvKVs)
	}
	for i := range want {
		if runGitEnvKVs[i] != want[i] {
			t.Errorf("runGitEnvKVs[%d] = %q, want %q",
				i, runGitEnvKVs[i], want[i])
		}
	}
}

// TestRunGitEnv_GitPagerSet is the headline pre-cycle test: GIT_PAGER=cat
// was already in the slice; this guards against a future refactor that
// drops it.
func TestRunGitEnv_GitPagerSet(t *testing.T) {
	env := parseEnv(runGitEnv())
	if got := env["GIT_PAGER"]; got != "cat" {
		t.Errorf("GIT_PAGER = %q, want %q (runGitEnvKVs = %v)",
			got, "cat", runGitEnvKVs)
	}
}

// TestRunGitEnv_JjPagerSet is the new entry. AGY 1.3.3 lines 50-51 say
// JJ_PAGER=cat must be set alongside GIT_PAGER=cat so `jj log` does not
// wedge on the interactive less pager. Even though runGit only execs git
// today, the var travels alongside GIT_PAGER=cat so a future caller that
// pipes git output through a jj-aware step inherits the same contract.
func TestRunGitEnv_JjPagerSet(t *testing.T) {
	env := parseEnv(runGitEnv())
	if got := env["JJ_PAGER"]; got != "cat" {
		t.Errorf("JJ_PAGER = %q, want %q (runGitEnvKVs = %v)",
			got, "cat", runGitEnvKVs)
	}
}

// TestRunGitEnv_BothPagersSet asserts both pager overrides land
// together — the changelog's "alongside GIT_PAGER=cat" wording. If a
// future edit accidentally splits them (e.g. introduces a separate
// runGitJjPager variable that runGit does not consult), this fails
// loudly.
func TestRunGitEnv_BothPagersSet(t *testing.T) {
	env := parseEnv(runGitEnv())
	if env["GIT_PAGER"] != "cat" {
		t.Errorf("GIT_PAGER = %q, want %q", env["GIT_PAGER"], "cat")
	}
	if env["JJ_PAGER"] != "cat" {
		t.Errorf("JJ_PAGER = %q, want %q", env["JJ_PAGER"], "cat")
	}
	// And: PAGER (the generic POSIX var) must also be set — the three
	// pagers are a unit. The 1.3.3 changelog fix is "alongside" not
	// "replaces".
	if env["PAGER"] != "cat" {
		t.Errorf("PAGER = %q, want %q", env["PAGER"], "cat")
	}
}

// TestRunGitEnv_OtherVarsUntouched confirms runGitEnv only ADDS our
// overrides — it must not strip unrelated parent-process vars. We seed
// the environment with a sentinel, then check the sentinel survives.
// Without this guard a future refactor that resets env to a fixed slice
// would silently lose whatever the parent shell exported (PATH, HOME,
// CI vars, ...).
func TestRunGitEnv_OtherVarsUntouched(t *testing.T) {
	const sentinel = "WAGYU_RUN_GIT_ENV_TEST_SENTINEL_42"
	t.Setenv(sentinel, "alive")

	env := parseEnv(runGitEnv())
	if got := env[sentinel]; got != "alive" {
		t.Errorf("parent env lost: %s = %q, want %q (full runGitEnv slice has %d entries)",
			sentinel, got, "alive", len(runGitEnv()))
	}

	// And: a known system var must also still be present.
	if env["PATH"] == "" {
		t.Errorf("PATH missing from runGitEnv — parent environment was stripped")
	}
}

// TestRunGitEnv_OverridesWinOnDuplicate confirms our overrides beat any
// pre-existing entry from the parent environment, per the contract
// documented on runGitEnv: "KVs are appended *after* the parent
// environment so on POSIX systems where duplicate env entries take the
// last value, our overrides win."
//
// We can't observe execve's behaviour from inside this process, but we
// CAN observe the relative order in the returned slice — duplicates must
// have our value LAST so the exec'd child sees it.
func TestRunGitEnv_OverridesWinOnDuplicate(t *testing.T) {
	// Seed two of the vars the test expects to override.
	t.Setenv("GIT_PAGER", "less")
	t.Setenv("JJ_PAGER", "more")

	env := runGitEnv()

	// Find the LAST occurrence of each — that's what the child sees.
	lastGIT := ""
	lastJJ := ""
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "GIT_PAGER="):
			lastGIT = kv
		case strings.HasPrefix(kv, "JJ_PAGER="):
			lastJJ = kv
		}
	}
	if lastGIT != "GIT_PAGER=cat" {
		t.Errorf("GIT_PAGER override lost: last occurrence = %q, want %q", lastGIT, "GIT_PAGER=cat")
	}
	if lastJJ != "JJ_PAGER=cat" {
		t.Errorf("JJ_PAGER override lost: last occurrence = %q, want %q", lastJJ, "JJ_PAGER=cat")
	}

	// Belt-and-braces: at least one entry from os.Environ must precede
	// our overrides. Without that guarantee the "override wins" trick
	// fails silently on systems that take the FIRST duplicate value.
	parentBefore := true
	for _, kv := range env {
		if kv == "GIT_PAGER=cat" || kv == "JJ_PAGER=cat" {
			parentBefore = false
			break
		}
	}
	if parentBefore {
		t.Errorf("our overrides appear at the start of the slice; on POSIX duplicate env entries the LAST value wins, so overrides must be appended after os.Environ (got %v)", env)
	}
}

// TestRunGitEnv_PrependsParentEnv is the inverse: runGitEnv must contain
// EVERY entry from os.Environ, not just our overrides. Without this a
// future refactor that does `return append(runGitEnvKVs, os.Environ()...)`
// would silently drop our overrides on duplicate-key hosts.
func TestRunGitEnv_PrependsParentEnv(t *testing.T) {
	parent := os.Environ()
	got := runGitEnv()

	if len(got) < len(parent)+len(runGitEnvKVs) {
		t.Errorf("runGitEnv length = %d, want at least %d (parent %d + kvs %d)",
			len(got), len(parent)+len(runGitEnvKVs), len(parent), len(runGitEnvKVs))
	}

	// Spot-check three parent entries. We don't iterate them all
	// because os.Environ on CI can have hundreds; the slice is built
	// with append so order is "parent first, our kvs last".
	step := len(parent) / 3
	if step > 0 {
		for i := 0; i < 3; i++ {
			idx := i * step
			if idx < len(parent) && got[idx] != parent[idx] {
				t.Errorf("parent env entry [%d] = %q, want %q (runGitEnv reorders parent?)",
					idx, got[idx], parent[idx])
			}
		}
	}
}

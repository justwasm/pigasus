// run_git_test.go — the git runner's environment contract.
//
// The /diff surfaces shell out to git, and they must see the same
// environment every time: no pager, no editor, and a parseable
// locale. The contract lives in run_git.go, and it is pinned here —
// the tests belong with the rule they pin, not with the panel that
// happens to read it.

package action

import (
	"os"
	"strings"
	"testing"
)

// parseEnv turns a KEY=VALUE slice into a map.
func parseEnv(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			out[kv[:eq]] = kv[eq+1:]
		}
	}
	return out
}

func TestRunGitEnvKVs_Shape(t *testing.T) {
	want := []string{
		"PAGER=cat",
		"GIT_PAGER=cat",
		"JJ_PAGER=cat",
		"GIT_EDITOR=false",
		"LC_ALL=C",
	}
	if len(RunGitEnvKVs) != len(want) {
		t.Fatalf("RunGitEnvKVs has %d entries, want %d (%v)",
			len(RunGitEnvKVs), len(want), RunGitEnvKVs)
	}
	for i := range want {
		if RunGitEnvKVs[i] != want[i] {
			t.Errorf("RunGitEnvKVs[%d] = %q, want %q",
				i, RunGitEnvKVs[i], want[i])
		}
	}
}

func TestRunGitEnv_GitPagerSet(t *testing.T) {
	env := parseEnv(RunGitEnv())
	if got := env["GIT_PAGER"]; got != "cat" {
		t.Errorf("GIT_PAGER = %q, want %q (RunGitEnvKVs = %v)",
			got, "cat", RunGitEnvKVs)
	}
}

func TestRunGitEnv_JjPagerSet(t *testing.T) {
	env := parseEnv(RunGitEnv())
	if got := env["JJ_PAGER"]; got != "cat" {
		t.Errorf("JJ_PAGER = %q, want %q (RunGitEnvKVs = %v)",
			got, "cat", RunGitEnvKVs)
	}
}

func TestRunGitEnv_BothPagersSet(t *testing.T) {
	env := parseEnv(RunGitEnv())
	if env["GIT_PAGER"] != "cat" {
		t.Errorf("GIT_PAGER = %q, want %q", env["GIT_PAGER"], "cat")
	}
	if env["JJ_PAGER"] != "cat" {
		t.Errorf("JJ_PAGER = %q, want %q", env["JJ_PAGER"], "cat")
	}
	if env["PAGER"] != "cat" {
		t.Errorf("PAGER = %q, want %q", env["PAGER"], "cat")
	}
}

func TestRunGitEnv_OtherVarsUntouched(t *testing.T) {
	const sentinel = "WAGYU_RUN_GIT_ENV_TEST_SENTINEL_42"
	t.Setenv(sentinel, "alive")

	env := parseEnv(RunGitEnv())
	if got := env[sentinel]; got != "alive" {
		t.Errorf("parent env lost: %s = %q, want %q (full RunGitEnv slice has %d entries)",
			sentinel, got, "alive", len(RunGitEnv()))
	}
	if env["PATH"] == "" {
		t.Errorf("PATH missing from RunGitEnv — parent environment was stripped")
	}
}

func TestRunGitEnv_OverridesWinOnDuplicate(t *testing.T) {
	t.Setenv("GIT_PAGER", "less")
	t.Setenv("JJ_PAGER", "more")

	env := RunGitEnv()
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

func TestRunGitEnv_PrependsParentEnv(t *testing.T) {
	parent := os.Environ()
	got := RunGitEnv()

	if len(got) < len(parent)+len(RunGitEnvKVs) {
		t.Errorf("RunGitEnv length = %d, want at least %d (parent %d + kvs %d)",
			len(got), len(parent)+len(RunGitEnvKVs), len(parent), len(RunGitEnvKVs))
	}

	step := len(parent) / 3
	if step > 0 {
		for i := 0; i < 3; i++ {
			idx := i * step
			if idx < len(parent) && got[idx] != parent[idx] {
				t.Errorf("parent env entry [%d] = %q, want %q (RunGitEnv reorders parent?)",
					idx, got[idx], parent[idx])
			}
		}
	}
}

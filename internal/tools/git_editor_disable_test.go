// Tests for AGY 1.3.6 changelog line 7:
//
//	"Fixed agent shell commands hanging on an invisible editor when a
//	 version control command wants one, such as `git commit` without `-m`
//	 or `jj describe`: the CLI now sets `GIT_EDITOR`, `HGEDITOR`, and
//	 `JJ_EDITOR` to `false` for agent commands, so they fail right away
//	 instead."
//
// The fix lives in internal/tools/command.go (AgentShellEnv /
// AgentShellEnvKVs) and is applied in five places that hand work off to
// os/exec:
//
//   - RunCommand.Run       (the `run_command` tool)
//   - RunShellCommand.Run   (the `run_shell_command` tool, mvdan/sh)
//   - ExecCommand.Run       (the `exec_command` tool, explicit env)
//   - bgTable.start         (background-mode run_command)
//
// Each is covered below. The `git commit` no-`-m` test is the
// behavioural one — without the env override it would block forever on
// $EDITOR; with GIT_EDITOR=false git exits non-zero immediately with a
// "there was a problem with the editor 'false'" message on stderr.
//
// Cycle 346: the `git_*` tools (a fifth, now-deleted call site of
// AgentShellEnv) are gone with their two tests. The reference's 1.4.7
// surface does not advertise them.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// gitEditorEnvVars are the three vars the AGY 1.3.6 changelog line 7
// pins. PAGER=cat / GIT_PAGER=cat / JJ_PAGER=cat come along for the ride
// — they're the 1.3.4 setting the cycle-107 /diff work introduced, and
// AgentShellEnv keeps them together because they share the same
// "don't let a tool wedge on a hidden prompt" intent.
var gitEditorEnvVars = []string{
	"GIT_EDITOR=false",
	"HGEDITOR=false",
	"JJ_EDITOR=false",
	"PAGER=cat",
	"GIT_PAGER=cat",
	"JJ_PAGER=cat",
}

// TestAgentShellEnv_HasAllVars is the contract test for AgentShellEnv:
// every var from AgentShellEnvKVs must appear (with our value) in the
// returned slice. We assert by scanning the slice because order is
// defined as "os.Environ first, AgentShellEnvKVs second", not as any
// particular order between the two halves.
func TestAgentShellEnv_HasAllVars(t *testing.T) {
	env := AgentShellEnv()
	got := make(map[string]string)
	for _, kv := range env {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			got[kv[:eq]] = kv[eq+1:]
		}
	}
	for _, want := range gitEditorEnvVars {
		eq := strings.IndexByte(want, '=')
		k, v := want[:eq], want[eq+1:]
		if got[k] != v {
			t.Errorf("env[%q] = %q, want %q (full AgentShellEnvKVs = %v)",
				k, got[k], v, gitEditorEnvVars)
		}
	}
}

// TestAgentShellEnvKVs_Shape pins the var list itself, so an accidental
// edit ("drop JJ_EDITOR", "set GIT_EDITOR=true", etc.) fails CI before
// the slower tests below.
func TestAgentShellEnvKVs_Shape(t *testing.T) {
	want := []string{
		"GIT_EDITOR=false",
		"HGEDITOR=false",
		"JJ_EDITOR=false",
		"PAGER=cat",
		"GIT_PAGER=cat",
		"JJ_PAGER=cat",
	}
	if len(AgentShellEnvKVs) != len(want) {
		t.Fatalf("AgentShellEnvKVs has %d entries, want %d (%v)",
			len(AgentShellEnvKVs), len(want), AgentShellEnvKVs)
	}
	for i := range want {
		if AgentShellEnvKVs[i] != want[i] {
			t.Errorf("AgentShellEnvKVs[%d] = %q, want %q",
				i, AgentShellEnvKVs[i], want[i])
		}
	}
}

// TestRunCommand_GitEditorSet spawns a child via run_command that
// prints KEY=VALUE pairs (we use `env` because run_command
// tokenises argv with shlex — a pipe is a shell feature and would
// hit a "no such file" path here). The child must see
// GIT_EDITOR=false (and friends) alongside whatever else the parent
// process inherits.
func TestRunCommand_GitEditorSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only: env / grep are not portable to Windows cmd")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"env"}`))
	if err != nil {
		t.Fatalf("RunCommand: %v\n%s", err, out)
	}
	got := out.(string)
	for _, want := range []string{"GIT_EDITOR=false", "HGEDITOR=false", "JJ_EDITOR=false",
		"PAGER=cat", "GIT_PAGER=cat", "JJ_PAGER=cat"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output (truncated): %q", want, truncate(got, 4096))
		}
	}
}

// truncate is a small helper for readable failure output.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// TestRunShellCommand_GitEditorSet does the same through mvdan/sh.
// This is the harder path: the runner needs `interp.Env(...)` wired
// up so subprocesses forked by the shell interpreter inherit the env.
func TestRunShellCommand_GitEditorSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	got, err := runSH(t, t.TempDir(),
		`env | grep -E '^(GIT_EDITOR|HGEDITOR|JJ_EDITOR|PAGER|GIT_PAGER|JJ_PAGER)'`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	for _, want := range []string{"GIT_EDITOR=false", "HGEDITOR=false", "JJ_EDITOR=false",
		"PAGER=cat", "GIT_PAGER=cat", "JJ_PAGER=cat"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output: %q", want, got)
		}
	}
}

// TestExecCommand_UserEnvPreserved confirms ExecCommand does NOT clobber
// caller-supplied env: AGY_ADC_AUTH=true (the 1.3.6 changelog line 1
// sign-in flow) and our GIT_EDITOR=false both end up in the child.
// The spec point: "User-provided env (AGY_ADC_AUTH=true) is preserved".
func TestExecCommand_UserEnvPreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	e := &ExecCommand{Workspace: t.TempDir()}
	raw := json.RawMessage(`{
		"command":"sh",
		"args":["-c","env | grep -E '^(AGY_ADC_AUTH|GIT_EDITOR|HGEDITOR|JJ_EDITOR)'"],
		"env":{"AGY_ADC_AUTH":"true"}
	}`)
	out, err := e.Run(context.Background(), raw)
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T, want map[string]any", out)
	}
	stdout, _ := m["stdout"].(string)
	stderr, _ := m["stderr"].(string)
	combined := stdout + stderr
	for _, want := range []string{
		"AGY_ADC_AUTH=true", // user-provided, must survive
		"GIT_EDITOR=false",  // AgentShellEnv injection
		"HGEDITOR=false",
		"JJ_EDITOR=false",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("output missing %q\nstdout=%q stderr=%q", want, stdout, stderr)
		}
	}
	if rc, _ := m["exit"].(int); rc != 0 {
		t.Errorf("exit = %d, want 0 (err = %v)", rc, m["err"])
	}
}

// TestExecCommand_NoEnv_StillHasAgentEnv confirms AgentShellEnv is the
// *baseline* for ExecCommand even when no caller env is supplied — the
// GIT_EDITOR=false trio must still be set. This is the regression
// guard for the change at command.go:204 (env := AgentShellEnv()).
func TestExecCommand_NoEnv_StillHasAgentEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	e := &ExecCommand{Workspace: t.TempDir()}
	out, err := e.Run(context.Background(), json.RawMessage(
		`{"command":"sh","args":["-c","env | grep -E '^(GIT_EDITOR|HGEDITOR|JJ_EDITOR)'"]}`))
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	m := out.(map[string]any)
	combined, _ := m["stdout"].(string)
	for _, want := range []string{"GIT_EDITOR=false", "HGEDITOR=false", "JJ_EDITOR=false"} {
		if !strings.Contains(combined, want) {
			t.Errorf("output missing %q\nfull output: %q", want, combined)
		}
	}
}

// initGitRepoForTest creates a temporary git repository, configures a
// safe user.email / user.name, and stages a single tracked file so
// `git commit` has something to commit.
func initGitRepoForTest(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.test"},
		{"config", "user.name", "test"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, out)
		}
	}
	// A tracked file so `git commit` is not a no-op.
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked.txt: %v", err)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, out)
	}
	return dir
}

// TestRunCommand_GitCommitNoMessage_FailsFast is the headline test:
// `git commit` (no `-m`) invoked through RunCommand in a fresh repo
// must NOT hang on an editor. With GIT_EDITOR=false git exits
// non-zero with `error: there was a problem with the editor 'false'`
// within a fraction of a second.
//
// A timeout guards against the regression: if the env fix is reverted
// and git falls back to $EDITOR (often vim) on the test machine, the
// call blocks forever and the test fails with the timeout message
// instead of silently passing.
func TestRunCommand_GitCommitNoMessage_FailsFast(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git commit hang test is POSIX-only")
	}
	dir := initGitRepoForTest(t)

	// Belt-and-braces: clear any $EDITOR / $VISUAL / $GIT_EDITOR the
	// developer's shell might have set. Without our env injection this
	// would fall back to $EDITOR; with it, GIT_EDITOR=false wins.
	for _, v := range []string{"EDITOR", "VISUAL", "GIT_EDITOR", "HGEDITOR", "JJ_EDITOR"} {
		t.Setenv(v, "")
	}

	r := &RunCommand{Workspace: dir}

	// Bound the call so a regression (the env fix is lost, git
	// invokes vim) cannot hang the test forever. 5 s is generous —
	// git's own "no editor available" path returns in <100 ms.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	out, err := r.Run(ctx, json.RawMessage(`{"command":"git commit"}`))
	elapsed := time.Since(start)

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("git commit hung (no editor was disabled): timed out after %s", elapsed)
	}
	// A non-zero exit is DATA, not a Go error — the reference returns
	// "Command exited with code N." inside the result text (see
	// RunCommand.renderResult). So "fails fast" is asserted on that
	// line, not on err.
	if err != nil {
		t.Fatalf("git commit without -m: unexpected runner error: %v", err)
	}
	combined, _ := out.(string)
	if strings.Contains(combined, "Command exited with code 0.") {
		t.Fatalf("git commit without -m succeeded; want non-zero exit (output: %q)", combined)
	}
	// git's diagnostic is exact: "there was a problem with the editor
	// 'false'". We assert substrings so a future git message tweak
	// doesn't break us, but we DO assert "false" appears (proving
	// GIT_EDITOR=false is what git is invoking).
	if !strings.Contains(combined, "false") {
		t.Errorf("git output does not mention the editor 'false' (full output: %q)", combined)
	}
	// And: this should be near-instant. If it's >2s the env fix is
	// almost certainly bypassed (e.g. GIT_EDITOR was unset and git
	// fell back to /usr/bin/editor which on some systems is vi).
	if elapsed > 2*time.Second {
		t.Errorf("git commit took %s; expected sub-second (the env fix should make this fail immediately)", elapsed)
	}
}

// TestBackgroundCommand_GitEditorSet confirms background-mode
// run_command also receives the env. We start a background `env`
// job, wait briefly for it to finish, and check the captured output
// contains GIT_EDITOR=false. (bgTable.start's exec.Cmd waits in a
// goroutine; we wait until t.done.)
func TestBackgroundCommand_GitEditorSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	// Snapshot the singleton's task map BEFORE starting so we can
	// find the one we just added (background commands live on a
	// package-level singleton; we don't want to mistake a stray
	// previous-run task for ours).
	before := make(map[string]bool, len(backgroundTasks.tasks))
	for k := range backgroundTasks.tasks {
		before[k] = true
	}

	dir := t.TempDir()
	r := &RunCommand{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"env","background":true}`))
	if err != nil {
		t.Fatalf("RunCommand(background): %v\n%s", err, out)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "Tool is running as a background task with task id: ") {
		t.Fatalf("background launch did not return the started marker; got: %q", s)
	}

	// Find the new task and wait for it to finish.
	var bg *bgTask
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for id, t := range backgroundTasks.tasks {
			if before[id] {
				continue
			}
			t.mu.Lock()
			done := t.done
			t.mu.Unlock()
			if done {
				bg = t
				break
			}
		}
		if bg != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if bg == nil {
		t.Fatalf("background command never finished within 2 s (registered tasks: %d)", len(backgroundTasks.tasks))
	}
	combined := bg.out.String()
	for _, want := range []string{"GIT_EDITOR=false", "HGEDITOR=false", "JJ_EDITOR=false"} {
		if !strings.Contains(combined, want) {
			t.Errorf("background output missing %q (full: %q)", want, want)
		}
	}
}

// TestGitBase_GitEditorSet was removed in cycle 346 together with the
// `git_*` tools it covered: the reference's 1.4.7 surface does not
// advertise any of them, so the catalogue never registered them and
// the tools themselves are gone. The env fix they exercised —
// AgentShellEnv — is still covered by the run_command / run_shell_command
// / exec_command / background-command tests above.

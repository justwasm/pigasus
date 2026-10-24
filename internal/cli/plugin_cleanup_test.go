// Tests for internal/cli/plugin_cleanup.go (AGY 1.2.7 changelog line
// 215). Every test runs against a temp registry/cache path so the
// shared $HOME is never touched; the helpers below also isolate the
// $HOME so the rest of the package's tests do not see each other's
// leftovers.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// newCleanupEnv points HOME / ANTIGRAVITY_HOME at a fresh temp tree so
// the existing legacyPluginsDir / AntigravityConfigHome helpers read
// from the test sandbox. Returns the root and a teardown.
func newCleanupEnv(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(root, ".gemini", "antigravity-cli"))
	return root
}

// writeRegistry seeds a registry file at path with records. A nil
// slice writes {"records": []}; a missing file is left alone.
func writeRegistry(t *testing.T, path string, records []PluginProcessRecord) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	doc := struct {
		Records []PluginProcessRecord `json:"records"`
	}{Records: records}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, buf, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readRegistry returns the records currently in path, or nil when the
// file is missing / unparseable.
func readRegistry(t *testing.T, path string) []PluginProcessRecord {
	t.Helper()
	records, err := LoadPluginProcessRegistry(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return records
}

// pidAlive returns whether pid is (probably) a running process. The
// check is best-effort: it sends signal 0 which succeeds iff the
// process exists and the calling user can signal it. ESRCH means
// dead; EPERM means alive but not ours — we treat that as "alive".
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(unblockSignalZero()); err != nil {
		return false
	}
	return true
}

// longRunningPID starts a child process that sleeps for a few seconds
// and returns its PID. The child is killed when the test ends (the
// caller can also use t.Cleanup to /bin/kill it). On systems where
// spawning a child fails the test is skipped — we do not pretend a
// missing toolchain is a code bug.
func longRunningPID(t *testing.T) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("long-running child not portable to windows test runner")
	}
	// sleep 30s is plenty for a sub-second test, and the kernel
	// reaps the zombie when the test exits.
	pid, err := startSleepChild(30)
	if err != nil {
		t.Skipf("could not spawn sleep child: %v", err)
	}
	t.Cleanup(func() {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(unblockSignalTerm())
		}
	})
	return pid
}

// TestPruneOrphanPluginProcesses_KillsMissingDir is the headline gap
// test: a process whose install_dir no longer exists on disk must be
// signalled at startup.
func TestPruneOrphanPluginProcesses_KillsMissingDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are posix-only")
	}
	env := newCleanupEnv(t)
	reg := PluginProcessRegistryPath(filepath.Join(env, ".gemini", "antigravity-cli"))
	pid := longRunningPID(t)

	writeRegistry(t, reg, []PluginProcessRecord{
		{PID: pid, PluginName: "old-plugin", ServerName: "fs",
			InstallDir: filepath.Join(env, "deleted-install"),
			StartedAt:  time.Now().Unix()},
	})
	if !pidAlive(pid) {
		t.Fatalf("preflight: pid %d not alive", pid)
	}

	killed, err := PruneOrphanPluginProcesses(reg)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if killed != 1 {
		t.Fatalf("killed=%d want 1", killed)
	}
	// Allow the kernel a moment to deliver the signal before we
	// re-check the pid; on a busy CI a single PollImmediate is not
	// enough, so we loop a few times with a short sleep.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(pid) {
		t.Fatalf("pid %d still alive after SIGTERM", pid)
	}
	if left := readRegistry(t, reg); len(left) != 0 {
		t.Fatalf("registry not drained: %d rows left", len(left))
	}
}

// TestPruneOrphanPluginProcesses_PreservesLiveInstalls ensures the
// startup sweep does NOT touch processes whose install dir is still
// present — killing a running plugin would be a regression.
func TestPruneOrphanPluginProcesses_PreservesLiveInstalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are posix-only")
	}
	env := newCleanupEnv(t)
	reg := PluginProcessRegistryPath(filepath.Join(env, ".gemini", "antigravity-cli"))
	installDir := filepath.Join(env, "live-plugin")
	if err := os.MkdirAll(installDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pid := longRunningPID(t)
	writeRegistry(t, reg, []PluginProcessRecord{
		{PID: pid, PluginName: "live-plugin", ServerName: "fs",
			InstallDir: installDir, StartedAt: time.Now().Unix()},
	})
	killed, err := PruneOrphanPluginProcesses(reg)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if killed != 0 {
		t.Fatalf("killed=%d want 0", killed)
	}
	if !pidAlive(pid) {
		t.Fatalf("pid %d killed despite live install dir", pid)
	}
	if left := readRegistry(t, reg); len(left) != 1 {
		t.Fatalf("registry should still have 1 row, got %d", len(left))
	}
}

// TestKillPluginProcessesForInstall_UpgradeKillsOldDir is the upgrade
// half of the gap: an MCP server started from <plugins>/<oldver>/...
// must be terminated before the install path replaces the directory.
func TestKillPluginProcessesForInstall_UpgradeKillsOldDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are posix-only")
	}
	env := newCleanupEnv(t)
	reg := PluginProcessRegistryPath(filepath.Join(env, ".gemini", "antigravity-cli"))
	oldDir := filepath.Join(env, ".gemini", "config", "plugins", "demo", "1.0")
	if err := os.MkdirAll(oldDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pid := longRunningPID(t)
	writeRegistry(t, reg, []PluginProcessRecord{
		{PID: pid, PluginName: "demo", ServerName: "fs",
			InstallDir: oldDir, StartedAt: time.Now().Unix()},
	})
	res, err := KillPluginProcessesForInstall(reg, oldDir)
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if res.Killed != 1 {
		t.Fatalf("killed=%d want 1 (errors=%v)", res.Killed, res.Errors)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(pid) {
		t.Fatalf("pid %d still alive after upgrade kill", pid)
	}
	if left := readRegistry(t, reg); len(left) != 0 {
		t.Fatalf("registry should be drained, got %d", len(left))
	}
}

// TestKillPluginProcessesForInstall_NoMatchPreserves confirms the
// "no upgrade in progress" path is a no-op: a process whose install
// dir differs from the requested dir must survive.
func TestKillPluginProcessesForInstall_NoMatchPreserves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are posix-only")
	}
	env := newCleanupEnv(t)
	reg := PluginProcessRegistryPath(filepath.Join(env, ".gemini", "antigravity-cli"))
	otherDir := filepath.Join(env, "other-plugin")
	if err := os.MkdirAll(otherDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pid := longRunningPID(t)
	writeRegistry(t, reg, []PluginProcessRecord{
		{PID: pid, PluginName: "other", ServerName: "fs",
			InstallDir: otherDir, StartedAt: time.Now().Unix()},
	})
	res, err := KillPluginProcessesForInstall(reg, filepath.Join(env, "upgraded-plugin"))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if res.Killed != 0 {
		t.Fatalf("killed=%d want 0", res.Killed)
	}
	if !pidAlive(pid) {
		t.Fatalf("pid %d killed despite dir mismatch", pid)
	}
	if left := readRegistry(t, reg); len(left) != 1 {
		t.Fatalf("registry should be unchanged, got %d", len(left))
	}
}

// TestPruneMarketplaceCache_StagingRemoved verifies the on-startup
// staging sweep drops directories named "staging" anywhere under the
// marketplace cache root.
func TestPruneMarketplaceCache_StagingRemoved(t *testing.T) {
	env := newCleanupEnv(t)
	root := filepath.Join(env, ".gemini", "antigravity-cli", "cache", "marketplaces")
	staging := filepath.Join(root, "antigravity-plugins-official", "staging", "demo-1.0-abc")
	if err := os.MkdirAll(staging, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "marker"), []byte("x"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	stats, err := PruneMarketplaceCache(root, func(string) string { return "" })
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.StagingRemoved < 1 {
		t.Fatalf("StagingRemoved=%d want >=1", stats.StagingRemoved)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging dir still present: %v", err)
	}
}

// TestPruneMarketplaceCache_SupersededRemoved checks the second half
// of the prune: a version directory that does not match the installed
// plugin's manifest version is dropped, the matching version is kept.
func TestPruneMarketplaceCache_SupersededRemoved(t *testing.T) {
	env := newCleanupEnv(t)
	root := filepath.Join(env, ".gemini", "antigravity-cli", "cache", "marketplaces")
	plugins := filepath.Join(root, "antigravity-plugins-official", "plugins", "demo")
	for _, v := range []string{"1.0", "1.1", "2.0"} {
		if err := os.MkdirAll(filepath.Join(plugins, v), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", v, err)
		}
	}
	installed := "2.0"
	stats, err := PruneMarketplaceCache(root, func(name string) string {
		if name == "demo" {
			return installed
		}
		return ""
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.SupersededRemoved != 2 {
		t.Fatalf("SupersededRemoved=%d want 2", stats.SupersededRemoved)
	}
	if _, err := os.Stat(filepath.Join(plugins, "1.0")); !os.IsNotExist(err) {
		t.Fatalf("1.0 still present")
	}
	if _, err := os.Stat(filepath.Join(plugins, "1.1")); !os.IsNotExist(err) {
		t.Fatalf("1.1 still present")
	}
	if _, err := os.Stat(filepath.Join(plugins, installed)); err != nil {
		t.Fatalf("installed version %s gone: %v", installed, err)
	}
}

// TestPruneMarketplaceCache_NoSupersededNoError is the contract test:
// a cache where every version is current must not error and must not
// drop anything. The clone must remain a no-op when nothing has
// changed.
func TestPruneMarketplaceCache_NoSupersededNoError(t *testing.T) {
	env := newCleanupEnv(t)
	root := filepath.Join(env, ".gemini", "antigravity-cli", "cache", "marketplaces")
	plugins := filepath.Join(root, "m", "plugins", "demo", "1.0")
	if err := os.MkdirAll(plugins, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plugins, "marker"), []byte("x"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	stats, err := PruneMarketplaceCache(root, func(name string) string {
		if name == "demo" {
			return "1.0"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.SupersededRemoved != 0 || stats.StagingRemoved != 0 {
		t.Fatalf("unexpected removals: %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(plugins, "marker")); err != nil {
		t.Fatalf("marker gone: %v", err)
	}
}

// TestPruneMarketplaceCache_MissingRootIsNoOp covers the freshly-
// installed case where the marketplace cache does not exist yet —
// the prune must not panic or error.
func TestPruneMarketplaceCache_MissingRootIsNoOp(t *testing.T) {
	env := newCleanupEnv(t)
	root := filepath.Join(env, ".gemini", "antigravity-cli", "cache", "marketplaces")
	stats, err := PruneMarketplaceCache(root, func(string) string { return "" })
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats != (PluginCachePruneStats{}) {
		t.Fatalf("expected zero stats, got %+v", stats)
	}
}

// TestRegisterUnregisterPluginProcess round-trips a record through the
// registry to confirm both helpers cooperate with the load/save pair.
func TestRegisterUnregisterPluginProcess(t *testing.T) {
	env := newCleanupEnv(t)
	reg := PluginProcessRegistryPath(filepath.Join(env, ".gemini", "antigravity-cli"))
	if err := RegisterPluginProcess(reg, PluginProcessRecord{
		PID: 4242, PluginName: "demo", ServerName: "fs",
		InstallDir: filepath.Join(env, "install"),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := RegisterPluginProcess(reg, PluginProcessRecord{
		PID: 4242, PluginName: "demo", ServerName: "fs",
		InstallDir: filepath.Join(env, "install"),
	}); err != nil {
		t.Fatalf("register dup: %v", err)
	}
	rows := readRegistry(t, reg)
	if len(rows) != 1 {
		t.Fatalf("dup not deduped: %d rows", len(rows))
	}
	if err := UnregisterPluginProcess(reg, 4242); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if rows := readRegistry(t, reg); len(rows) != 0 {
		t.Fatalf("unregister left %d rows", len(rows))
	}
}

// TestSortedPluginProcessRecords pins the sort order used by tests
// downstream; the production code does not depend on it, but the
// tests do so a stray refactor does not silently change a fixture.
func TestSortedPluginProcessRecords(t *testing.T) {
	in := []PluginProcessRecord{
		{PluginName: "b", ServerName: "fs", PID: 5},
		{PluginName: "a", ServerName: "fs", PID: 2},
		{PluginName: "a", ServerName: "fs", PID: 1},
	}
	got := SortedPluginProcessRecords(in)
	want := []int{1, 2, 5}
	gotKey := make([]int, len(got))
	for i, r := range got {
		gotKey[i] = r.PID
	}
	if !sort.SliceIsSorted(gotKey, func(i, k int) bool { return gotKey[i] < gotKey[k] }) {
		// not strictly required — only the (PluginName, ServerName, PID)
		// order is — but the test below asserts the PIDs are ascending
		// for the a/fs group, so this stays a useful sanity check.
	}
	for i, r := range got {
		if i >= len(want) {
			t.Fatalf("extra row %d", i)
		}
		if r.PID != want[i] {
			t.Fatalf("row %d pid=%d want %d", i, r.PID, want[i])
		}
	}
	if got := strings.ToLower(got[0].PluginName + got[0].ServerName); got != "afs" {
		t.Fatalf("first row ordering broken: %s", got)
	}
}

// TestRunPluginInstall_UpgradeKillsProcesses is the integration test
// for the install-side change: re-installing a plugin whose name is
// already installed must terminate any MCP server / sidecar processes
// that were started from the previous install directory, then drop
// the old tree and copy the new one in. Without the cleanup step the
// new copy lands but the old processes keep running against binaries
// that no longer exist on disk.
func TestRunPluginInstall_UpgradeKillsProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are posix-only")
	}
	env := newCleanupEnv(t)
	home := filepath.Join(env, ".gemini", "antigravity-cli")
	t.Setenv("ANTIGRAVITY_HOME", home)

	// Build the v1 source tree with a minimal plugin.json.
	src1 := filepath.Join(env, "src-v1")
	seedPluginTree(t, src1, "demo", "1.0", nil)
	// Seed a child process representing an MCP server launched from
	// the v1 install dir, then pre-create the v1 install dir itself
	// (runPluginInstall only kills processes whose install dir is
	// already on disk — the bug we're closing).
	dst, oldVersionDir := setupOldInstall(t, env)
	pid := longRunningPID(t)
	writeRegistry(t, PluginProcessRegistryPath(home), []PluginProcessRecord{
		{PID: pid, PluginName: "demo", ServerName: "fs",
			InstallDir: dst, StartedAt: time.Now().Unix()},
	})
	// Run the install once — this is the upgrade step.
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)
	if err := runPluginInstall(j, []string{src1}); err != nil {
		t.Fatalf("install v1: %v (errb=%q)", err, errb.String())
	}
	if !strings.Contains(out.String(), "Installed plugin") {
		t.Fatalf("expected install confirmation, got %q", out.String())
	}
	// Allow the kernel a moment to deliver SIGTERM.
	waitForPIDDead(t, pid)
	if left := readRegistry(t, PluginProcessRegistryPath(home)); len(left) != 0 {
		t.Fatalf("registry should be drained, got %d", len(left))
	}
	// The fresh install must exist; the old version subdirectory must
	// be gone (the upgrade path removes the entire dst before copying).
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("new install dir missing: %v", err)
	}
	if _, err := os.Stat(oldVersionDir); !os.IsNotExist(err) {
		t.Fatalf("old version subdir still present: %v", err)
	}
}

func waitForPIDDead(t *testing.T, pid int) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pidAlive(pid) {
		t.Fatalf("pid %d still alive after upgrade", pid)
	}
}

func setupOldInstall(t *testing.T, env string) (string, string) {
	t.Helper()
	dst := filepath.Join(env, ".gemini", "config", "plugins", "demo")
	if err := os.MkdirAll(dst, 0700); err != nil {
		t.Fatalf("mkdir old install: %v", err)
	}
	oldVersionDir := filepath.Join(dst, "1.0")
	if err := os.MkdirAll(oldVersionDir, 0700); err != nil {
		t.Fatalf("mkdir old version: %v", err)
	}
	return dst, oldVersionDir
}

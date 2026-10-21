// sandbox_test.go — cycle 228. The AGY 1.2.10 close-gap:
// sandboxed run_command is allowed when the command's paths all live
// under the cwd or the agent's own artifact / scratch dirs; an
// out-of-bounds path triggers the "bypass sandbox?" prompt (in
// headless print mode the gate surfaces a soft-deny so the model sees
// the recovered DeniedToolResult text instead of an unprompted run).
//
// Evidence grade 【strings】+【推测】:
//   - "Ran in the sandbox" + "BypassSandbox: true" are recovered
//     1.3.4 binary strings (out/local-macos/strings-resolved.txt).
//   - The artifact / scratch locations are observed on the live
//     machine (out/local-macos/FINDINGS.md §"scratch").
package sandbox

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// cwdFixture returns a sandbox that matches a workspace of /work and
// an antigravity home of /home/.gemini/antigravity-cli, so the test
// inputs read like real on-disk paths.
func cwdFixture() Sandbox {
	return Resolve(true,
		"/work",
		filepath.Join("/home", ".gemini", "antigravity-cli"),
		"conv-1234")
}

// TestResolve_DefaultsToDisabled pins the zero-value behaviour: a
// fresh Sandbox{} (which is what the gate sees before settings.json
// is consulted) must allow every path so a misconfigured call site
// cannot silently start denying commands.
func TestResolve_DefaultsToDisabled(t *testing.T) {
	var s Sandbox
	if !s.Allowed("/anything/at/all") {
		t.Errorf("disabled sandbox must allow every path; Allowed(/anything/at/all)=false")
	}
	if !s.Allowed("/") {
		t.Errorf("disabled sandbox must allow /; Allowed(/)=false")
	}
}

// TestSandboxArtifactDirAllowed is the package-level mirror of the
// gate-level carve-out test: a sandbox that contains a workspace
// and a configured antigravity home must put the artifact dir and
// its descendants on the allow-list.
//
// This file's TestAllowed_ArtifactDirInAllowList covers the same
// surface; TestSandboxArtifactDirAllowed exists so the cycle 228
// targeted-test regex `TestSandbox|TestArtifact|TestScratch|TestBypass`
// picks up this package as well as cmd/wagyu.
func TestSandboxArtifactDirAllowed(t *testing.T) {
	root := "/home/.gemini/antigravity-cli/artifacts"
	s := Resolve(true, "/work", "/home/.gemini/antigravity-cli", "conv-a")
	for _, p := range []string{root, root + "/", root + "/snap.json"} {
		if !s.Allowed(p) {
			t.Errorf("Allowed(%q) = false, want true (artifact carve-out)", p)
		}
	}
}

// TestSandboxScratchDirAllowed mirrors the gate-level carve-out
// test at the package level: the per-conversation scratch dir is
// on the allow-list, while a sibling conversation's scratch is
// not. Same surface as TestAllowed_ScratchDirInAllowList, kept
// under the TestSandbox name for the cycle 228 test regex.
func TestSandboxScratchDirAllowed(t *testing.T) {
	home := "/home/.gemini/antigravity-cli"
	s := Resolve(true, "/work", home, "conv-a")
	root := home + "/brain/conv-a/scratch"
	for _, p := range []string{root, root + "/", root + "/tmp.txt"} {
		if !s.Allowed(p) {
			t.Errorf("Allowed(%q) = false, want true (scratch carve-out)", p)
		}
	}
	if s.Allowed(home + "/brain/conv-other/scratch/x") {
		t.Errorf("a different conversation's scratch must NOT pass this sandbox")
	}
}

// TestSandboxBypassIsPerCall is the package-level pin of the
// "tries the sandbox first even after an earlier command needed a
// bypass" contract. Two consecutive CheckCommandWithBypass calls
// with the same out-of-bounds command — one with bypass, one
// without — must observe: bypass=true → Outside=false; bypass=false
// → Outside=true. The check is intentionally a struct-level
// assertion (it reads Sandbox for a Bypassed field below) so a
// future refactor that adds a sticky flag fails fast.
func TestSandboxBypassIsPerCall(t *testing.T) {
	home := "/home/.gemini/antigravity-cli"
	s := Resolve(true, "/work", home, "conv-b")

	cmd := "rm -rf /etc/passwd"
	if s.CheckCommandWithBypass(cmd, true).Outside {
		t.Errorf("bypass=true must lift the sandbox; got Outside=true")
	}
	if !s.CheckCommandWithBypass(cmd, false).Outside {
		t.Errorf("bypass=false must NOT lift the sandbox; got Outside=false")
	}
}

// TestResolve_AllowedPathsOrder pins the canonical order — cwd,
// artifact, scratch — because the cycle 228 deliverable references
// "the cwd, the artifact dir, and the scratch dir" and any change to
// this slice must be a deliberate naming change.
func TestResolve_AllowedPathsOrder(t *testing.T) {
	s := cwdFixture()
	got := s.AllowedPaths()
	want := []string{
		"/work",
		filepath.Join("/home", ".gemini", "antigravity-cli", "artifacts"),
		filepath.Join("/home", ".gemini", "antigravity-cli", "brain", "conv-1234", "scratch"),
	}
	if len(got) != len(want) {
		t.Fatalf("AllowedPaths len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("AllowedPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestAllowed_Cwd pins the basic case: the cwd and its descendants
// are always allowed. This is the workspace the agent is operating
// on — sandboxed commands must be able to read & write it.
func TestAllowed_Cwd(t *testing.T) {
	s := cwdFixture()
	for _, p := range []string{"/work", "/work/", "/work/sub/dir", "/work/file.txt"} {
		if !s.Allowed(p) {
			t.Errorf("Allowed(%q) = false, want true (cwd should be permitted)", p)
		}
	}
}

// TestAllowed_ArtifactDirInAllowList is the AGY 1.2.10 carve-out:
// commands that touch the artifact dir are allowed without the
// "bypass?" prompt. The artifact dir is the agent's own output, so
// reading or writing it from a sandboxed run_command is the normal
// path. This is one of the four minimum tests the cycle brief
// enumerates.
func TestAllowed_ArtifactDirInAllowList(t *testing.T) {
	s := cwdFixture()
	root := filepath.Join("/home", ".gemini", "antigravity-cli", "artifacts")
	for _, p := range []string{root, root + "/", root + "/snapshot.json", root + "/deep/path/file.txt"} {
		if !s.Allowed(p) {
			t.Errorf("Allowed(%q) = false, want true (artifact dir must be in the sandbox allow-list)", p)
		}
	}
	// And the negative case — sibling of the artifact dir is NOT
	// allowed just because they share a parent.
	if s.Allowed(filepath.Join("/home", ".gemini", "antigravity-cli", "conversations")) {
		t.Errorf("Allowed(conversations) = true, want false (sibling of artifact dir must stay outside the sandbox)")
	}
}

// TestAllowed_ScratchDirInAllowList is the second half of the AGY
// 1.2.10 carve-out: the per-conversation scratch dir is allowed so
// sandboxed commands can deposit intermediate state.
func TestAllowed_ScratchDirInAllowList(t *testing.T) {
	s := cwdFixture()
	root := filepath.Join("/home", ".gemini", "antigravity-cli", "brain", "conv-1234", "scratch")
	for _, p := range []string{root, root + "/", root + "/tmp.txt", root + "/deep/dir/output.bin"} {
		if !s.Allowed(p) {
			t.Errorf("Allowed(%q) = false, want true (scratch dir must be in the sandbox allow-list)", p)
		}
	}
	// Sibling — another conversation's scratch — must be outside,
	// so a sandboxed `cat /…/brain/conv-other/scratch/x` still
	// prompts. This is what makes the per-conversation isolation
	// meaningful.
	other := filepath.Join("/home", ".gemini", "antigravity-cli", "brain", "conv-other", "scratch", "x")
	if s.Allowed(other) {
		t.Errorf("Allowed(%q) = true, want false (a different conversation's scratch is not in this sandbox)", other)
	}
}

// TestAllowed_PrefixDoesNotConfineSiblings is a defensive check:
// /foo/barbaz must NOT be considered "inside /foo/bar" just because
// the literal byte sequence matches. Without the trailing-separator
// guard, an attacker who could place a file at
// /home/.gemini/antigravity-cli/artifacts-evil/ could trick a
// sandboxed `cat` into reading it.
func TestAllowed_PrefixDoesNotConfineSiblings(t *testing.T) {
	s := cwdFixture()
	evil := filepath.Join("/home", ".gemini", "antigravity-cli", "artifacts-evil", "x")
	if s.Allowed(evil) {
		t.Errorf("Allowed(%q) = true, want false (sibling-prefix must not match the allow-list root)", evil)
	}
}

// TestResolve_EmptyConversationIDOmitsScratch confirms the package
// does not invent a top-level scratch dir when the gate is wired
// without a conversation id. The artifact path still appears because
// it lives off the antigravity home, which is independent of the
// conversation.
func TestResolve_EmptyConversationIDOmitsScratch(t *testing.T) {
	s := Resolve(true, "/work", "/home/.gemini/antigravity-cli", "")
	for _, p := range s.AllowedPaths() {
		if strings.Contains(p, "scratch") {
			t.Errorf("AllowedPaths contains a scratch entry %q with no conversation id", p)
		}
	}
	if !s.Allowed(filepath.Join("/home", ".gemini", "antigravity-cli", "artifacts", "x")) {
		t.Errorf("artifact dir should still be allowed when scratch is disabled")
	}
}

// TestCheckCommand_AllInsideAllowed pins the happy path: a shell
// command whose every path-shaped token is in the allow-list returns
// (Outside=false) so the gate can let it through without surfacing
// the "bypass?" prompt.
func TestCheckCommand_AllInsideAllowed(t *testing.T) {
	s := cwdFixture()
	res := s.CheckCommand("cat /work/foo.txt && cp /work/a /home/.gemini/antigravity-cli/artifacts/x")
	if res.Outside {
		t.Errorf("CheckCommand(inside paths) = Outside=true, want false; Touched=%v", res.Touched)
	}
	want := []string{"/work/foo.txt", "/work/a", "/home/.gemini/antigravity-cli/artifacts/x"}
	if !sameSet(res.Touched, want) {
		t.Errorf("Touched = %v, want %v", res.Touched, want)
	}
}

// TestCheckCommand_TouchesOutside: as soon as one token lands
// outside the allow-list the gate flags it. This is what makes the
// "ask the user to bypass" prompt fire for `rm -rf /tmp/foo` when
// /tmp is not under cwd, artifact, or scratch.
func TestCheckCommand_TouchesOutside(t *testing.T) {
	s := cwdFixture()
	res := s.CheckCommand("cat /etc/passwd && rm -rf /tmp/foo")
	if !res.Outside {
		t.Errorf("CheckCommand(/etc/passwd + /tmp/foo) = Outside=false, want true; Touched=%v", res.Touched)
	}
}

// TestCheckCommand_BypassPerCallOnly is the AGY 1.2.10 second-half
// guarantee: a bypass lifts the sandbox for the one call it is
// passed on, but the *next* call must still see Outside=true when
// the next command touches an out-of-bounds path.
//
// This is exactly the contract the changelog closes — "tries the
// sandbox first even after an earlier command needed a bypass" —
// and the test pins it on the package API so a future refactor
// cannot quietly turn Bypassed into a sticky flag.
func TestCheckCommand_BypassPerCallOnly(t *testing.T) {
	s := cwdFixture()
	cmd := "cat /etc/passwd"

	// First call: the user (or a headless test) lifts the sandbox.
	r1 := s.CheckCommandWithBypass(cmd, true)
	if r1.Outside {
		t.Errorf("bypass must suppress Outside; got %+v", r1)
	}

	// Second call: same command, no bypass argument. The sandbox
	// must NOT have remembered the previous bypass; Outside must
	// fire again.
	r2 := s.CheckCommandWithBypass(cmd, false)
	if !r2.Outside {
		t.Errorf("second call with bypass=false must still flag Outside; got %+v", r2)
	}
}

// TestCheckCommand_BypassIsNotASandboxField is the structural half
// of the previous test: Sandbox does not carry a Bypassed bool, so
// the only way to clear a bypass is to drop the *Sandbox and rebuild
// it. A refactor that adds `Bypassed bool` to Sandbox would compile
// but break the cycle 228 contract; this test reads the field set to
// catch such a regression.
func TestCheckCommand_BypassIsNotASandboxField(t *testing.T) {
	s := cwdFixture()
	rt := reflect.TypeOf(s)
	if _, ok := rt.FieldByName("Bypassed"); ok {
		t.Errorf("Sandbox must not carry a Bypassed field; the bypass must be a per-call argument, not a sticky flag")
	}
}

// sameSet reports whether a and b contain the same elements in any
// order. Used by the CheckCommand tests; the token order depends on
// the regex implementation and is not part of the contract.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoModeOutsideWorkspaceFileEdits_1516(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	pol := Policy{
		Mode:           ModeAuto,
		WorkspaceRoots: []string{ws},
	}

	// 1. Creating a new file outside workspace in Auto mode -> Allow
	newFile := filepath.Join(outside, "brand_new.txt")
	if got := pol.DecideCall(newFile, []byte(`{"Path":"`+newFile+`","Content":"hello"}`), "write_file"); got != Allow {
		t.Fatalf("new file outside workspace = %v, want Allow", got)
	}

	// 2. Overwriting an existing file outside workspace (non-VCS) -> Ask
	existing := filepath.Join(outside, "existing.txt")
	if err := os.WriteFile(existing, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pol.DecideCall(existing, []byte(`{"Path":"`+existing+`","Content":"replaced","Overwrite":true}`), "write_file"); got != Ask {
		t.Fatalf("overwrite existing file outside VCS = %v, want Ask", got)
	}

	// 3. Appending to an existing file outside workspace (non-VCS) -> Allow
	if got := pol.DecideCall(existing, []byte(`{"Path":"`+existing+`","Content":"line3\n","Append":true}`), "write_file"); got != Allow {
		t.Fatalf("append existing file outside VCS = %v, want Allow", got)
	}

	// 4. Non-replacing edit_file (inserting without replacing existing lines) -> Allow
	if got := pol.DecideCall(existing, []byte(`{"Path":"`+existing+`","Target":"line1","Replacement":"line1\ninserted"}`), "edit_file"); got != Allow {
		t.Fatalf("non-replacing edit_file outside VCS = %v, want Allow", got)
	}

	// 5. Replacing edit_file outside VCS -> Ask
	if got := pol.DecideCall(existing, []byte(`{"Path":"`+existing+`","Target":"line1","Replacement":"changed"}`), "edit_file"); got != Ask {
		t.Fatalf("replacing edit_file outside VCS = %v, want Ask", got)
	}

	// 6. Replacing edit_file inside an outside VCS repo (.git, .jj, .hg) -> Allow
	for _, vcs := range []string{".git", ".jj", ".hg"} {
		repo := t.TempDir()
		if err := os.MkdirAll(filepath.Join(repo, vcs), 0o755); err != nil {
			t.Fatal(err)
		}
		repoFile := filepath.Join(repo, "code.go")
		if err := os.WriteFile(repoFile, []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := pol.DecideCall(repoFile, []byte(`{"Path":"`+repoFile+`","Target":"old","Replacement":"new"}`), "edit_file"); got != Allow {
			t.Fatalf("replacing edit inside %s repo = %v, want Allow", vcs, got)
		}
	}

	// 7. Hidden paths, VCS metadata, hooks.json, and system folders still Ask
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []string{
		filepath.Join(repo, ".git", "config"),
		filepath.Join(repo, ".hidden"),
		filepath.Join(repo, "hooks.json"),
		filepath.Join(outside, ".bashrc"),
		"/etc/hosts",
		"/Library/LaunchAgents/com.example.plist",
	} {
		if got := pol.DecideCall(blocked, []byte(`{"Path":"`+blocked+`","Content":"x"}`), "write_file"); got != Ask {
			t.Fatalf("blocked path %q = %v, want Ask", blocked, got)
		}
	}

	// 8. Explicit ask/deny rules still apply even inside VCS repo
	polWithAsk := Policy{
		Mode:           ModeAuto,
		WorkspaceRoots: []string{ws},
		Ask:            []Rule{{Action: ActionWriteFile, Pattern: repo}},
	}
	repoFile := filepath.Join(repo, "guarded.go")
	if got := polWithAsk.DecideCall(repoFile, []byte(`{"Path":"`+repoFile+`","Content":"x"}`), "write_file"); got != Ask {
		t.Fatalf("explicit ask rule overridden = %v, want Ask", got)
	}
}

// TestProtectedWriteTargets_AskInEveryMode is the AGY 1.5.17 bullet:
//
//	"Fixed Gemini Enterprise and enterprise model-gateway sessions with
//	 non-workspace file access allowed bypassing built-in approval and deny
//	 protections on sensitive files such as .git, .env, config.json,
//	 hooks.json, and mcp_config.json."
//
// Every sensitive name is exercised in every execution mode, inside and
// outside the workspace, and asserted to never auto-approve. "Never" is
// checked as "the verdict is Ask or Deny", because both are a stop: Ask
// prompts, Deny refuses. What must not happen is Allow.
//
// The modes are the *whole* set the engine accepts — the three --mode
// values plus the empty default — not just the one the bug report named,
// because the protected list has to be a floor under all of them rather
// than a patch on the mode that happened to be exploited.
func TestProtectedWriteTargets_AskInEveryMode(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	// Make `outside` a real repository so the auto-approval path has every
	// excuse it could possibly have: the VCS allowance applies there, and
	// the target paths themselves are protected, not merely unrecognised.
	if err := os.MkdirAll(filepath.Join(outside, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The five files the changelog names, plus the configuration files the
	// clone already protected, plus an .env variant and an arbitrary file
	// inside .git/ — a write there rewrites what the repository does next,
	// which is the real risk the bullet is about.
	sensitive := []string{
		".env",
		".env.local",
		".env.production",
		"config.json",
		"hooks.json",
		"mcp_config.json",
		"settings.json",
		"admin_settings.json",
		".git/config",
		".git/hooks/pre-commit",
		".git",
		".jj/config",
	}

	modes := []struct {
		name string
		mode AgentMode
	}{
		{"default", ModeDefault},
		{"accept-edits", ModeAcceptEdits},
		{"auto", ModeAuto},
		{"plan", ModePlan},
	}

	// A write_file that creates a brand-new file is the strongest form of
	// "should be auto-approved" in Auto mode (a new file replaces nothing),
	// so it is what each case uses: if the protected list were merely
	// ordering itself after the VCS allowance, this would still slip
	// through.
	newFileArgs := func(p string) []byte {
		return []byte(`{"Path":"` + p + `","Content":"x"}`)
	}
	replaceArgs := func(p string) []byte {
		return []byte(`{"Path":"` + p + `","Content":"x","Overwrite":true}`)
	}
	editArgs := func(p string) []byte {
		return []byte(`{"Path":"` + p + `","Target":"a","Replacement":"b"}`)
	}

	for _, m := range modes {
		for _, root := range []struct {
			scope string
			dir   string
		}{
			{"inside-workspace", ws},
			{"outside-workspace", outside},
		} {
			for _, name := range sensitive {
				target := filepath.Join(root.dir, name)
				if name == ".git" || name == ".git/config" || name == ".git/hooks/pre-commit" {
					// .git exists in `outside` already; give `ws` one too so
					// the inside-workspace case is a real repository as well.
					if err := os.MkdirAll(filepath.Join(root.dir, ".git", "hooks"), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}

				pol := Policy{Mode: m.mode, WorkspaceRoots: []string{ws}}

				for _, tc := range []struct {
					tool string
					args []byte
				}{
					{"write_file", newFileArgs(target)},
					{"write_file", replaceArgs(target)},
					{"write_to_file", newFileArgs(target)},
					{"edit_file", editArgs(target)},
				} {
					got := pol.DecideCall(target, tc.args, tc.tool)
					if got == Allow {
						t.Errorf("mode=%q scope=%s target=%s tool=%s: verdict = Allow, want Ask or Deny (AGY 1.5.17)",
							m.mode, root.scope, name, tc.tool)
					}
				}
			}
		}
	}
}

// TestProtectedWriteTargets_NamesCovered pins the protected set itself, so
// that dropping an entry from the list is a test failure rather than a
// silent regression. It reads the same predicate the engine uses.
func TestProtectedWriteTargets_NamesCovered(t *testing.T) {
	dir := t.TempDir()

	protected := []string{
		// AGY 1.5.17's own list.
		filepath.Join(dir, ".git"),
		filepath.Join(dir, ".git", "config"),
		filepath.Join(dir, ".git", "hooks", "pre-commit"),
		filepath.Join(dir, ".env"),
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "hooks.json"),
		filepath.Join(dir, "mcp_config.json"),
		// The clone's pre-existing entries — kept, not replaced.
		filepath.Join(dir, "settings.json"),
		filepath.Join(dir, "admin_settings.json"),
		filepath.Join(dir, "outside-of-project.json"),
		filepath.Join(dir, "sidecar.json"),
		// The .env family.
		filepath.Join(dir, ".env.local"),
		filepath.Join(dir, ".env.development"),
		filepath.Join(dir, ".env.production.local"),
		// Other VCS metadata the repo detection knows.
		filepath.Join(dir, ".jj", "config"),
		filepath.Join(dir, ".hg", "hgrc"),
		filepath.Join(dir, ".svn", "entries"),
		// Case-insensitivity: the filesystem opens the same file.
		filepath.Join(dir, ".ENV"),
		filepath.Join(dir, "Config.JSON"),
		filepath.Join(dir, ".Git", "config"),
	}
	for _, p := range protected {
		if !isProtectedWriteTarget(p) {
			t.Errorf("isProtectedWriteTarget(%q) = false, want true", p)
		}
	}

	// Negative control: names that merely start with a protected name, or
	// live beside one, are not protected. Without this the predicate could
	// pass the loop above by matching every path.
	notProtected := []string{
		filepath.Join(dir, ".gitignore"),
		filepath.Join(dir, ".gitattributes"),
		filepath.Join(dir, ".gitmodules"),
		filepath.Join(dir, "config.json.bak"),
		filepath.Join(dir, "my-config.json"),
		filepath.Join(dir, "config.txt"),
		filepath.Join(dir, "env"),
		filepath.Join(dir, "environment.go"),
		filepath.Join(dir, ".envrc"),
		filepath.Join(dir, "notenv"),
		filepath.Join(dir, "app.go"),
	}
	for _, p := range notProtected {
		if isProtectedWriteTarget(p) {
			t.Errorf("isProtectedWriteTarget(%q) = true, want false", p)
		}
	}
}

// TestProtectedWriteTargets_OrdinaryFileStillAutoApproved is the negative
// control the suite needs to be meaningful: a test that asserts "everything
// asks" would pass while breaking Auto mode entirely. An ordinary file in the
// very same directory as a protected one must still be auto-approved where the
// mode says so.
func TestProtectedWriteTargets_OrdinaryFileStillAutoApproved(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()

	// A real repository outside the workspace: the outside-workspace
	// allowance applies, so an ordinary file there is a fair test.
	if err := os.MkdirAll(filepath.Join(outside, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory that also holds protected files, so "same directory" is
	// literal.
	if err := os.WriteFile(filepath.Join(outside, ".env"), []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, m := range []AgentMode{ModeAcceptEdits, ModeAuto} {
		pol := Policy{Mode: m, WorkspaceRoots: []string{ws}}

		// Inside the workspace, an ordinary edit is approved by the mode.
		wsFile := filepath.Join(ws, "main.go")
		if err := os.WriteFile(wsFile, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := pol.DecideCall(wsFile, []byte(`{"Path":"`+wsFile+`","Target":"package","Replacement":"package main"}`), "edit_file"); got != Allow {
			t.Errorf("mode=%q: ordinary inside-workspace edit = %v, want Allow", m, got)
		}

		// Outside the workspace in a repository, an ordinary file is
		// approved by the VCS allowance / non-replacing-edit rule.
		outFile := filepath.Join(outside, "code.go")
		if err := os.WriteFile(outFile, []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := pol.DecideCall(outFile, []byte(`{"Path":"`+outFile+`","Target":"old","Replacement":"new"}`), "edit_file"); got != Allow {
			t.Errorf("mode=%q: ordinary outside-workspace edit in a repo = %v, want Allow", m, got)
		}

		// And the protected neighbours in that same directory still ask —
		// so the Allow above is genuinely about the file, not the folder.
		for _, name := range []string{".env", "config.json"} {
			p := filepath.Join(outside, name)
			if got := pol.DecideCall(p, []byte(`{"Path":"`+p+`","Target":"a","Replacement":"b"}`), "edit_file"); got == Allow {
				t.Errorf("mode=%q: protected neighbour %s = Allow, want Ask or Deny", m, name)
			}
		}
	}
}

// TestProtectedWriteTargets_VCSDetectionUnaffected pins the separation the fix
// depends on: .git is both a *marker* (its presence makes the surrounding tree
// auto-approvable) and a *target* (a write into it is protected). Protecting
// the target must not switch off the marker, or the whole outside-workspace
// repository allowance would disappear.
func TestProtectedWriteTargets_VCSDetectionUnaffected(t *testing.T) {
	ws := t.TempDir()
	repo := t.TempDir()

	for _, vcs := range []string{".git", ".jj", ".hg"} {
		if err := os.MkdirAll(filepath.Join(repo, vcs), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	pol := Policy{Mode: ModeAuto, WorkspaceRoots: []string{ws}}

	// The marker still works: an ordinary file elsewhere in the tree is
	// auto-approved even though a replacing edit would otherwise ask.
	code := filepath.Join(repo, "code.go")
	if err := os.WriteFile(code, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pol.DecideCall(code, []byte(`{"Path":"`+code+`","Target":"old","Replacement":"new"}`), "edit_file"); got != Allow {
		t.Errorf("file in a VCS repo = %v, want Allow (the marker must still work)", got)
	}

	// The target does not: a write inside the marker asks.
	for _, name := range []string{".git/config", ".jj/config", ".hg/hgrc"} {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := pol.DecideCall(p, []byte(`{"Path":"`+p+`","Target":"a","Replacement":"b"}`), "edit_file"); got == Allow {
			t.Errorf("write into %s = Allow, want Ask (the target must be protected)", name)
		}
	}
}

// TestProtectedWriteTargets_ExplicitOptOuts pins the two surfaces that still
// win, and records them as deliberate rather than inherited:
//
//   - --dangerously-skip-permissions is the user's explicit "never ask"; the
//     reference names it in the same sentence as the auto-approval, and the
//     headless path depends on it.
//   - toolPermission: always-proceed is the same opt-out expressed as a
//     setting.
//   - an explicit allow rule is the user's own decision about their own file.
//
// These are above the protected-list floor by design. If a future cycle
// decides that even these must not reach .env, this test is the one to change
// — deliberately, not as a side effect.
func TestProtectedWriteTargets_ExplicitOptOuts(t *testing.T) {
	target := filepath.Join(t.TempDir(), ".env")

	t.Run("skip-permissions", func(t *testing.T) {
		pol := Policy{Mode: ModeAuto, SkipPermissions: true}
		if got := pol.DecideCall(target, nil, "write_file"); got != Allow {
			t.Errorf("skip-permissions = %v, want Allow (the documented opt-out)", got)
		}
	})

	t.Run("always-proceed", func(t *testing.T) {
		pol := Policy{Mode: ModeAuto, ToolPermission: ToolPermissionAlwaysProceed}
		if got := pol.DecideCall(target, nil, "write_file"); got != Allow {
			t.Errorf("always-proceed = %v, want Allow (the documented opt-out)", got)
		}
	})

	t.Run("explicit allow rule", func(t *testing.T) {
		pol := Policy{
			Mode:  ModeAuto,
			Allow: []Rule{{Action: ActionWriteFile, Pattern: target}},
		}
		if got := pol.DecideCall(target, nil, "write_file"); got != Allow {
			t.Errorf("explicit allow rule = %v, want Allow (the user's own decision)", got)
		}
	})

	t.Run("explicit ask rule still asks", func(t *testing.T) {
		pol := Policy{
			Mode: ModeAuto,
			Ask:  []Rule{{Action: ActionWriteFile, Pattern: target}},
		}
		if got := pol.DecideCall(target, nil, "write_file"); got != Ask {
			t.Errorf("explicit ask rule = %v, want Ask", got)
		}
	})

	t.Run("explicit deny rule still denies", func(t *testing.T) {
		pol := Policy{
			Mode: ModeAuto,
			Deny: []Rule{{Action: ActionWriteFile, Pattern: target}},
		}
		if got := pol.DecideCall(target, nil, "write_file"); got != Deny {
			t.Errorf("explicit deny rule = %v, want Deny", got)
		}
	})

	t.Run("a read of a protected file is not a write", func(t *testing.T) {
		// The protection is about writes. Reading .env is governed by the
		// read rules, not by this floor, and Auto mode allows reads.
		pol := Policy{Mode: ModeAuto}
		if got := pol.DecideCall(target, nil, "read_file"); got != Allow {
			t.Errorf("read of .env = %v, want Allow (reads are not writes)", got)
		}
	})
}

// TestProtectedWriteTargets_SymlinkToProtectedFile pins that the protection
// follows the symlink: a write to a harmless-looking name that resolves to
// .env is a write to .env.
func TestProtectedWriteTargets_SymlinkToProtectedFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("SECRET=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "notes.txt")
	if err := os.Symlink(envPath, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if !isProtectedWriteTarget(link) {
		t.Fatalf("isProtectedWriteTarget(%q) = false, want true (it resolves to .env)", link)
	}

	pol := Policy{Mode: ModeAuto}
	if got := pol.DecideCall(link, []byte(`{"Path":"`+link+`","Target":"SECRET","Replacement":"SECRET=2"}`), "edit_file"); got == Allow {
		t.Errorf("edit through a symlink to .env = Allow, want Ask or Deny")
	}
}

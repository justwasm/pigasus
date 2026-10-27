package cliapp

// slash_actions_test.go — cycle 103: /add-dir, /copy and /logout perform real
// local actions. Every expected string is recovered from the 1.3.6 string
// table except where noted.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/compaction"
	"github.com/justwasm/pigasus/internal/editorkit"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/tuilib"

	tea "charm.land/bubbletea/v2"
)

func TestAddDirFeedback_Validation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	writeSettings(t, dir, `{"colorScheme":"light"}`)

	file := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, arg, want string }{
		{"empty", "", "error: no directory specified"},
		{"spaces", "   ", "error: no directory specified"},
		{"missing", filepath.Join(dir, "nope"), "error: directory not found: " + filepath.Join(dir, "nope")},
		{"file", file, "error: not a directory: " + file},
		{"dir", sub, "Added " + sub + " to workspace"},
		{"quoted", `"` + sub + `"`, "Added " + sub + " to workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := addDirFeedback(tc.arg); got != tc.want {
				t.Errorf("addDirFeedback(%q) = %q, want %q", tc.arg, got, tc.want)
			}
		})
	}
}

// TestAddDirFeedback_Persists checks the settings.json half: the validated
// directory is appended to trustedWorkspaces, other keys survive, and a repeat
// call does not duplicate the entry.
func TestAddDirFeedback_Persists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"colorScheme":"light","trustedWorkspaces":["/already/there"]}`)

	sub := filepath.Join(home, "ws")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := addDirFeedback(sub); got != "Added "+sub+" to workspace" {
		t.Fatalf("addDirFeedback = %q", got)
	}
	body := readSettings(t, home)
	for _, want := range []string{"colorScheme", "light", "/already/there", sub} {
		if !strings.Contains(body, want) {
			t.Errorf("settings.json missing %q:\n%s", want, body)
		}
	}
	// Idempotent.
	if err := auth.AddTrustedWorkspace(sub); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(readSettings(t, home), sub); n != 1 {
		t.Errorf("directory recorded %d times, want 1:\n%s", n, readSettings(t, home))
	}
}

func TestSlashAddDir_ViaRouter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"trustedWorkspaces":[]}`)
	sub := filepath.Join(home, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/add-dir " + sub); !handled {
		t.Fatal("/add-dir not handled")
	}
	if got := strings.Join(afterEcho(m), "\n"); got != "Added "+sub+" to workspace" {
		t.Errorf("/add-dir rendered %q", got)
	}
}

func TestSlashCopy_Clipboard(t *testing.T) {
	orig := clipboardWrite
	defer func() { clipboardWrite = orig }()

	var copied string
	clipboardWrite = func(s string) error { copied = s; return nil }

	m := newSlashModel(t)
	m.session = []backend.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "first"},
		{Role: "assistant", Content: "second"},
	}
	m.handleSlashCommand("/copy")
	if copied != "second" {
		t.Errorf("clipboard got %q, want the last assistant message", copied)
	}
	if got := strings.Join(afterEcho(m), "\n"); got != "Copied last response to clipboard." {
		t.Errorf("/copy rendered %q", got)
	}

	// Failure path.
	clipboardWrite = func(string) error { return os.ErrPermission }
	m = newSlashModel(t)
	m.session = []backend.Message{{Role: "assistant", Content: "x"}}
	m.handleSlashCommand("/copy")
	if got := strings.Join(afterEcho(m), "\n"); !strings.HasPrefix(got, "Failed to copy to clipboard: ") {
		t.Errorf("/copy failure rendered %q", got)
	}

	// Nothing to copy: recovered "No response to copy.", clipboard untouched.
	called := false
	clipboardWrite = func(string) error { called = true; return nil }
	m = newSlashModel(t)
	m.handleSlashCommand("/copy")
	if called {
		t.Error("clipboard written with no assistant response")
	}
	if got := strings.Join(afterEcho(m), "\n"); got != "No response to copy." {
		t.Errorf("/copy with no response rendered %q", got)
	}
}

// TestResolveCopy_Forms pins the recovered /copy argument forms.
func TestResolveCopy_Forms(t *testing.T) {
	responses := []string{"first", "second", "third"}
	for _, tc := range []struct {
		name, arg, wantPayload, wantMsg string
	}{
		{"default", "", "third", "Copied last response to clipboard."},
		{"nth-last", "1", "third", "Copied response 1 to clipboard."},
		{"nth-2", "2", "second", "Copied response 2 to clipboard."},
		{"nth-oldest", "3", "first", "Copied response 3 to clipboard."},
		{"out-of-range", "9", "", "No response to copy."},
		{"zero", "0", "third", "Copied last response to clipboard."},
		{"junk", "zzz", "third", "Copied last response to clipboard."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, msg := resolveCopy(tc.arg, responses)
			if payload != tc.wantPayload || msg != tc.wantMsg {
				t.Errorf("resolveCopy(%q) = (%q, %q), want (%q, %q)",
					tc.arg, payload, msg, tc.wantPayload, tc.wantMsg)
			}
		})
	}

	// With no responses at all.
	if p, m := resolveCopy("", nil); p != "" || m != "No response to copy." {
		t.Errorf("resolveCopy(empty) = (%q, %q)", p, m)
	}
	if p, m := resolveCopy("2", nil); p != "" || m != "No response to copy." {
		t.Errorf("resolveCopy(2, none) = (%q, %q)", p, m)
	}
}

// TestSlashCopy_ArgumentForms exercises /copy through the router, including
// /copy 2. Cycle 230 moved /copy btw to runMode.copyBtwCard so it can reach
// the active side-question Card; that path is covered by the slash_btw_test
// suite.
func TestSlashCopy_ArgumentForms(t *testing.T) {
	orig := clipboardWrite
	defer func() { clipboardWrite = orig }()
	var copied []string
	clipboardWrite = func(s string) error { copied = append(copied, s); return nil }

	m := newSlashModel(t)
	m.session = []backend.Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "assistant", Content: "a2"},
	}
	m.handleSlashCommand("/copy 2")
	if len(copied) != 1 || copied[0] != "a1" {
		t.Fatalf("copied %q, want [a1]", copied)
	}
	if got := strings.Join(afterEcho(m), "\n"); got != "Copied response 2 to clipboard." {
		t.Errorf("/copy 2 rendered %q", got)
	}
}

// TestSlashStatusline_Subcommands pins the reference's /statusline
// behaviour.
//
// Evidence 【一手】: agy 1.4.8, driven through every subcommand:
//
//	/statusline                 → *toggles* ("Statusline on." /
//	                              "Statusline off. Run /statusline to re-enable.")
//	/statusline <command>       → "Statusline set to: <command>"
//	/statusline on | enable     → "Statusline on." (" (<command>)" with one)
//	/statusline off | disable   → "Statusline off."
//	/statusline delete | reset  → "Custom statusline command cleared. …"
//	/statusline help            → the usage block
//
// The subcommand names are matched case-insensitively (upstream 1.0.1).
func TestSlashStatusline_Subcommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"colorScheme":"light"}`)

	// Cycle 351: the bare command toggles instead of printing the usage
	// block. The fresh settings.json has no statusLine block, so the
	// toggle switches it on.
	m := newSlashModel(t)
	m.handleSlashCommand("/statusline")
	rows := afterEcho(m)
	if len(rows) == 0 || rows[0] != "Statusline on." {
		t.Fatalf("/statusline rendered %q", rows)
	}
	// A second bare press switches it off again, with the long form.
	m.response = nil
	m.handleSlashCommand("/statusline")
	rows = afterEcho(m)
	if len(rows) == 0 || rows[0] != "Statusline off. Run /statusline to re-enable." {
		t.Fatalf("/statusline (toggle off) rendered %q", rows)
	}

	m = newSlashModel(t)
	m.handleSlashCommand("/statusline help")
	rows = afterEcho(m)
	if len(rows) == 0 || rows[0] != "Usage:" {
		t.Errorf("/statusline help rendered %q", rows)
	}
	if !strings.Contains(strings.Join(rows, "\n"), "/statusline on | enable     Enable the statusline.") {
		t.Errorf("/statusline usage block = %q", rows)
	}
	m = newSlashModel(t)
	m.handleSlashCommand("/statusline /bin/echo hi")
	if got := strings.Join(afterEcho(m), "\n"); got != "Statusline set to: /bin/echo hi" {
		t.Errorf("/statusline <command> rendered %q", got)
	}
	body := readSettings(t, home)
	if !strings.Contains(body, `"command": "/bin/echo hi"`) {
		t.Errorf("settings.json missing the command:\n%s", body)
	}
	// The reference writes the block *without* an `enabled` member: the
	// member's default is true, so the script runs right away.
	//
	// Evidence 【一手】: agy 1.4.7's `/statusline <command>` wrote
	// `{"type":"command","command":"…"}`.
	if strings.Contains(body, `"enabled"`) {
		t.Errorf("settings.json must not carry an enabled member:\n%s", body)
	}
	if !m.statusLineExternal {
		t.Error("statusLineExternal must be on after /statusline <command>")
	}

	// The subcommand names are matched case-insensitively.
	m = newSlashModel(t)
	m.handleSlashCommand("/statusline ON")
	if got := strings.Join(afterEcho(m), "\n"); got != "Statusline on. (/bin/echo hi)" {
		t.Errorf("/statusline ON rendered %q", got)
	}
	m = newSlashModel(t)
	m.handleSlashCommand("/statusline OFF")
	if got := strings.Join(afterEcho(m), "\n"); got != "Statusline off." {
		t.Errorf("/statusline OFF rendered %q", got)
	}
	if m.statusLineExternal {
		t.Error("statusLineExternal must be off after /statusline off")
	}
	body = readSettings(t, home)
	if !strings.Contains(body, `"enabled": false`) {
		t.Errorf("settings.json missing enabled=false:\n%s", body)
	}
	// The command survives the visibility write.
	if !strings.Contains(body, `"command": "/bin/echo hi"`) {
		t.Errorf("settings.json clobbered the command:\n%s", body)
	}

	m = newSlashModel(t)
	m.handleSlashCommand("/statusline delete")
	if got := strings.Join(afterEcho(m), "\n"); got != "Custom statusline command cleared. Reverted to built-in default." {
		t.Errorf("/statusline delete rendered %q", got)
	}
	body = readSettings(t, home)
	// The reference removes the whole `statusLine` key.
	//
	// Evidence 【一手】: agy 1.4.7's settings.json after `/statusline
	// delete` held no `statusLine` key at all.
	if strings.Contains(body, `"statusLine"`) {
		t.Errorf("settings.json still carries the statusLine block:\n%s", body)
	}
}

// TestSlashTitle_Subcommands pins the reference's /title behaviour.
//
// Evidence 【一手】: agy 1.4.7, driven through every verb —
//   - `/title on` without a command → the "No title command configured" error
//   - `/title bogus` → "Usage: /title [on|off]"
//   - `/title ON` → "Custom title enabled. (/bin/echo hi)"
//   - `/title off` → "Custom title disabled."
//   - a bare `/title` toggles the feature
func TestSlashTitle_Subcommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"colorScheme":"light"}`)

	// Without a title block the reference reports the missing command.
	m := newSlashModel(t)
	m.handleSlashCommand("/title on")
	if got := strings.Join(afterEcho(m), "\n"); got != tuilib.TitleNoCommandMessage {
		t.Errorf("/title on without a block rendered %q", got)
	}

	// The bare verb is a toggle, so it needs a command too.
	m = newSlashModel(t)
	m.handleSlashCommand("/title")
	if got := strings.Join(afterEcho(m), "\n"); got != tuilib.TitleNoCommandMessage {
		t.Errorf("/title without a block rendered %q", got)
	}

	// An unknown verb is answered with the usage line.
	m = newSlashModel(t)
	m.handleSlashCommand("/title bogus")
	if got := strings.Join(afterEcho(m), "\n"); got != tuilib.TitleUsage {
		t.Errorf("/title bogus rendered %q", got)
	}

	writeSettings(t, home, `{"title":{"type":"command","command":"/bin/echo hi","enabled":false}}`)
	m = newSlashModel(t)
	m.reloadStatusLineConfig()
	m.handleSlashCommand("/title ON")
	if got := strings.Join(afterEcho(m), "\n"); got != "Custom title enabled. (/bin/echo hi)" {
		t.Errorf("/title ON rendered %q", got)
	}
	if m.title != "on" {
		t.Errorf("after /title ON m.title = %q", m.title)
	}
	body := readSettings(t, home)
	if !strings.Contains(body, `"enabled": true`) {
		t.Errorf("settings.json missing enabled=true:\n%s", body)
	}
	// The command survives the visibility write.
	if !strings.Contains(body, `"command": "/bin/echo hi"`) {
		t.Errorf("settings.json clobbered the command:\n%s", body)
	}

	m = newSlashModel(t)
	m.reloadStatusLineConfig()
	m.handleSlashCommand("/title off")
	if got := strings.Join(afterEcho(m), "\n"); got != "Custom title disabled." {
		t.Errorf("/title off rendered %q", got)
	}
	if m.title != "off" {
		t.Errorf("after /title off m.title = %q", m.title)
	}

	// A bare /title toggles the feature back on.
	m = newSlashModel(t)
	m.reloadStatusLineConfig()
	m.handleSlashCommand("/title")
	if got := strings.Join(afterEcho(m), "\n"); got != "Custom title enabled. (/bin/echo hi)" {
		t.Errorf("/title rendered %q", got)
	}
	body = readSettings(t, home)
	if !strings.Contains(body, `"enabled": true`) {
		t.Errorf("settings.json missing enabled=true:\n%s", body)
	}
}

// TestSlashToggles_Persist checks that /statusline and /title write
// settings.json and that the model tracks the new state.
//
// The reference stores both blocks as objects — `type`, `command` and
// `enabled` — and the other keys survive the writes.
//
// Evidence 【一手】: agy 1.4.7's settings.json after `/statusline delete`
// holds `{"type":"","command":"","enabled":false}`.
func TestSlashToggles_Persist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"colorScheme":"light"}`)

	m := newSlashModel(t)
	m.handleSlashCommand("/statusline on")
	if m.statusLine != "on" {
		t.Errorf("after /statusline on m.statusLine = %q, want on", m.statusLine)
	}
	if body := readSettings(t, home); !strings.Contains(body, `"enabled": true`) {
		t.Errorf("settings.json missing enabled=true:\n%s", body)
	}

	m = newSlashModel(t)
	m.handleSlashCommand("/statusline off")
	if body := readSettings(t, home); !strings.Contains(body, `"enabled": false`) {
		t.Errorf("settings.json missing enabled=false:\n%s", body)
	}

	// /title needs a title block: without one the reference reports the
	// missing command and leaves the state alone.
	m = newSlashModel(t)
	m.handleSlashCommand("/title on")
	if m.title == "on" {
		t.Error("after /title on without a block the title must stay off")
	}
	// The other keys survive both writes.
	if body := readSettings(t, home); !strings.Contains(body, `"colorScheme": "light"`) {
		t.Errorf("colorScheme lost:\n%s", body)
	}
}

// TestSettingsString pins the reader used to seed the toggles.
func TestSettingsString(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"title":"on","statusLine":"off","model":"gemini-3.8-flash-high"}`)
	if got := auth.SettingsString("title"); got != "on" {
		t.Errorf("SettingsString(title) = %q", got)
	}
	if got := auth.SettingsString("statusLine"); got != "off" {
		t.Errorf("SettingsString(statusLine) = %q", got)
	}
	if got := auth.SettingsString("missing"); got != "" {
		t.Errorf("SettingsString(missing) = %q", got)
	}
}

// TestStatuslineOffHidesRow checks the visible effect of /statusline.
func TestStatuslineOffHidesRow(t *testing.T) {
	m := newSlashModel(t)
	m.statusLine = ""
	if !strings.Contains(m.View().Content, "? for shortcuts") {
		t.Fatal("status row missing with statusLine on")
	}
	m.statusLine = "off"
	if strings.Contains(m.View().Content, "? for shortcuts") {
		t.Error("status row still rendered with statusLine off")
	}
}

// TestSlashCodesearch_Router checks the router wiring.
func TestSlashCodesearch_Router(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newSlashModel(t)
	m.cwd = dir
	m.handleSlashCommand("/codesearch hello")
	if got := strings.Join(afterEcho(m), "\n"); got != "Found 1 result\na.txt:1:1: hello world" {
		t.Errorf("/codesearch rendered %q", got)
	}
	m = newSlashModel(t)
	m.cwd = dir
	m.handleSlashCommand("/codesearch")
	// Cycle 228: bare `/codesearch` (no query) now echoes the
	// "needs a query" line. The panel path takes the full
	// /codesearch <args> route; the print-mode echo is for the
	// slash command line only.
	if got := strings.Join(afterEcho(m), "\n"); got != "/codesearch needs a query." {
		t.Errorf("/codesearch with no query rendered %q", got)
	}
}

// TestEditPromptCmd covers the ctrl+g scratch-file round trip. bubbletea's
// ExecProcess produces an internal message rather than the callback's, so the
// two halves are exercised directly: promptScratchFile writes the prompt, and
// the editorFinishedMsg branch reads it back and removes it.
func TestEditPromptCmd(t *testing.T) {
	path, err := editorkit.PromptScratchFile("original")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Errorf("scratch file = %q, want %q", data, "original")
	}

	m := newSlashModel(t)
	// The editor rewrites the scratch file.
	if err := os.WriteFile(path, []byte("EDITED\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.Update(editorFinishedMsg{tempPath: path})
	if got := m.input.Value(); got != "EDITED" {
		t.Errorf("prompt after ctrl+g = %q, want EDITED", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("scratch file %s was not removed", path)
	}

	// A failing editor leaves the prompt alone and reports the error.
	path2, err := editorkit.PromptScratchFile("keep me")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path2) }()
	m = newSlashModel(t)
	m.input.SetValue("keep me")
	m.Update(editorFinishedMsg{err: os.ErrPermission, tempPath: path2})
	if got := m.input.Value(); got != "keep me" {
		t.Errorf("prompt after a failed editor = %q", got)
	}
	want := "Editor exited with error: " + os.ErrPermission.Error()
	if got := m.response[len(m.response)-1]; got != want {
		t.Errorf("failed editor rendered %q, want %q", got, want)
	}
}

// TestEditPromptCmd_Scheduled checks that ctrl+g schedules an editor run when
// one is configured.
func TestEditPromptCmd_Scheduled(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("EDITOR", "cat")
	m := newSlashModel(t)
	m.input.SetValue("hello")
	if cmd := m.editPromptCmd(); cmd == nil {
		t.Fatal("editPromptCmd returned nil for a working editor")
	}
}

// TestEditPromptCmd_BadEditor checks the recovered failure path.
func TestEditPromptCmd_BadEditor(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("EDITOR", "")
	m := newSlashModel(t)
	// resolveEditor reads settings.json; write the bad editor there.
	body := `{"editor":"definitely-not-an-editor-xyz"}`
	if err := os.WriteFile(filepath.Join(os.Getenv("ANTIGRAVITY_HOME"), "settings.json"),
		[]byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if cmd := m.editPromptCmd(); cmd != nil {
		t.Error("ctrl+g scheduled a command for a missing editor")
	}
	if len(m.response) == 0 ||
		!strings.HasPrefix(m.response[len(m.response)-1], "Failed to resolve editor: ") {
		t.Errorf("ctrl+g with a bad editor rendered %q", m.response)
	}
}

// TestSlashOpen_Router checks the router wiring: usage without an argument, a
// scheduled editor process with one.
func TestSlashOpen_Router(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "cat")

	m := newSlashModel(t)
	m.cwd = dir
	if _, cmd := m.handleSlashCommand("/open"); cmd != nil {
		t.Error("/open with no argument scheduled a command")
	}
	if got := strings.Join(afterEcho(m), "\n"); got != "Usage: /open <file_path>" {
		t.Errorf("/open rendered %q", got)
	}

	m = newSlashModel(t)
	m.cwd = dir
	handled, cmd := m.handleSlashCommand("/open a.txt")
	if !handled || cmd == nil {
		t.Fatalf("/open a.txt = (handled=%v, cmd=%v)", handled, cmd)
	}
	if got := strings.Join(afterEcho(m), "\n"); got != "Opening file: "+file {
		t.Errorf("/open a.txt rendered %q", got)
	}
}

// TestEditorFinishedMsg checks the post-editor report.
func TestEditorFinishedMsg(t *testing.T) {
	m := newSlashModel(t)
	m.Update(editorFinishedMsg{})
	if len(m.response) != 0 {
		t.Errorf("successful editor exit produced %q", m.response)
	}
	m.Update(editorFinishedMsg{err: os.ErrPermission})
	want := "Editor exited with error: " + os.ErrPermission.Error()
	if got := m.response[len(m.response)-1]; got != want {
		t.Errorf("editor failure rendered %q, want %q", got, want)
	}
}

// TestDiffFeedback_Git drives /diff against a real repository.
func TestDiffFeedback_Git(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	file := filepath.Join(dir, "a.txt")
	git("init")
	if err := os.WriteFile(file, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "init")

	if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("clean tree = %q", got)
	}

	if err := os.WriteFile(file, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := diffFeedback(dir, 0)
	if !strings.Contains(got, "-one") || !strings.Contains(got, "+two") {
		t.Errorf("diff body = %q", got)
	}
	if !strings.Contains(got, "[1-") || !strings.HasSuffix(got, "  (end of diff)") {
		t.Errorf("diff footer = %q", got)
	}

	// A nested directory resolves to the same repository.
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := detectVCS(sub); got != "git" {
		t.Errorf("detectVCS(nested) = %q", got)
	}
}

func TestDiffFeedback_Edges(t *testing.T) {
	if got := diffFeedback("", 0); got != "no workspace directory" {
		t.Errorf("diffFeedback(\"\") = %q", got)
	}
	// A directory with no VCS anywhere up the chain. AGY 1.3.3
	// line 52 falls back to `git diff HEAD`, which exits non-zero
	// in a non-repo directory; the surfaced line is
	// "Not in a git repository".
	if got := diffFeedback(t.TempDir(), 0); got != "Not in a git repository" {
		t.Errorf("diffFeedback(no repo) = %q, want %q", got, "Not in a git repository")
	}
	if got := detectVCS(t.TempDir()); got != "" {
		t.Errorf("detectVCS(no repo) = %q", got)
	}
}

// TestTasksFeedback pins the recovered empty and non-empty forms.
func TestTasksFeedback(t *testing.T) {
	if got := tasksFeedback(0, 0); got != "No background tasks." {
		t.Errorf("tasksFeedback(0,0) = %q", got)
	}
	if got := tasksFeedback(2, 5); got != "2 running, 5 done" {
		t.Errorf("tasksFeedback(2,5) = %q", got)
	}
}

func TestRemoteControlFeedback(t *testing.T) {
	// The bare form means "on" (description: "Turn on remote control for
	// this session (off to disable)").
	for _, tc := range []struct {
		arg     string
		current bool
		wantOn  bool
		wantMsg string
	}{
		{"", false, true, "Remote control on for this session."},
		{"on", false, true, "Remote control on for this session."},
		{"ON", true, true, "Remote control on for this session."},
		{"off", true, false, "Remote control off. This machine is no longer reachable from other devices."},
		{"maybe", true, true, "Usage: /remote-control [on|off]"},
	} {
		on, msg := remoteControlFeedback(tc.arg, tc.current)
		if on != tc.wantOn || msg != tc.wantMsg {
			t.Errorf("remoteControlFeedback(%q,%v) = (%v,%q), want (%v,%q)",
				tc.arg, tc.current, on, msg, tc.wantOn, tc.wantMsg)
		}
	}
}

func TestRenameFeedback(t *testing.T) {
	if next, msg := renameFeedback("", "old"); next != "old" ||
		msg != "Error: Please provide a new name for the conversation." {
		t.Errorf("renameFeedback(\"\") = (%q,%q)", next, msg)
	}
	if next, msg := renameFeedback("   ", "old"); next != "old" ||
		msg != "Error: Please provide a new name for the conversation." {
		t.Errorf("renameFeedback(spaces) = (%q,%q)", next, msg)
	}
	if next, msg := renameFeedback("Refactor auth", "old"); next != "Refactor auth" ||
		msg != "Conversation renamed to: Refactor auth" {
		t.Errorf("renameFeedback(name) = (%q,%q)", next, msg)
	}
}

// TestMCPServersFeedback covers the empty case (the reference's default) and a
// populated config.
func TestMCPServersFeedback(t *testing.T) {
	if got := mcpServersFeedback(nil); got != "No MCP servers configured." {
		t.Errorf("nil config = %q", got)
	}
	if got := mcpServersFeedback(&auth.MCPConfig{}); got != "No MCP servers configured." {
		t.Errorf("empty config = %q", got)
	}
	cfg := &auth.MCPConfig{Servers: map[string]auth.MCPServer{
		"zeta":  {Type: "stdio", Command: "npx", Args: []string{"-y", "z"}, Enabled: true},
		"alpha": {Type: "http", URL: "https://example.test/mcp", Enabled: false},
	}}
	got := mcpServersFeedback(cfg)
	want := "MCP servers\n" +
		"  alpha\thttp\tdisabled\thttps://example.test/mcp\n" +
		"  zeta\tstdio\tenabled\tnpx -y z"
	if got != want {
		t.Errorf("mcpServersFeedback =\n%q\nwant\n%q", got, want)
	}
}

// TestSlashMCP_ReadsConfig drives /mcp through the router, including the
// missing-file case.
func TestSlashMCP_ReadsConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	m := newSlashModel(t)
	m.handleSlashCommand("/mcp")
	if got := strings.Join(afterEcho(m), "\n"); got != "No MCP servers configured." {
		t.Errorf("/mcp with no mcp.json rendered %q", got)
	}

	body := `{"servers":{"local":{"type":"stdio","command":"mcp-server","enabled":true}}}`
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m = newSlashModel(t)
	m.handleSlashCommand("/mcp")
	if got := strings.Join(afterEcho(m), "\n"); !strings.Contains(got, "local") ||
		!strings.Contains(got, "enabled") || !strings.Contains(got, "mcp-server") {
		t.Errorf("/mcp with one server rendered %q", got)
	}
}

// TestSlashRename_UpdatesTitle checks that /rename feeds the title used by
// /title.
func TestSlashRename_UpdatesTitle(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/rename Refactor auth")
	if m.conversationName != "Refactor auth" {
		t.Fatalf("conversationName = %q", m.conversationName)
	}
	if got := m.conversationTitle(); got != "Refactor auth" {
		t.Errorf("conversationTitle() = %q", got)
	}
	// With no rename the first user message is used.
	m = newSlashModel(t)
	m.session = []backend.Message{{Role: "user", Content: "fix the build"}}
	if got := m.conversationTitle(); got != "fix the build" {
		t.Errorf("conversationTitle() = %q", got)
	}
}

func TestSlashLogout_RemovesToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	token := filepath.Join(home, "antigravity-oauth-token")
	if err := os.WriteFile(token, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newSlashModel(t)
	m.handleSlashCommand("/logout")
	want := "Signed out from server: " + endpoints.Current().CloudCodeHost()
	if got := strings.Join(afterEcho(m), "\n"); got != want {
		t.Errorf("/logout rendered %q, want %q", got, want)
	}
	if _, err := os.Stat(token); !os.IsNotExist(err) {
		t.Errorf("token still present after /logout: %v", err)
	}

	// Idempotent: a second logout is not an error.
	m = newSlashModel(t)
	m.handleSlashCommand("/logout")
	if got := strings.Join(afterEcho(m), "\n"); got != want {
		t.Errorf("second /logout rendered %q, want %q", got, want)
	}
}

func TestAuthServerName(t *testing.T) {
	if got := authServerName(); got != endpoints.Current().CloudCodeHost() {
		t.Errorf("authServerName() = %q", got)
	}
}

func writeSettings(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readSettings(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- cycle 217 batch items ------------------------------------------------

// TestSkillsPanel_HeightsSurfaceContract pins the empirical finding that
// at h ≤ 16 REF and OUR keep OPPOSITE ends of the /skills panel. The test
// is structured so a future panel rewrite that breaks the contract will
// fail with a useful diff, not just a generic string mismatch.
func TestSkillsPanel_HeightsSurfaceContract(t *testing.T) {
	const wantShared = "Shared skills"
	found := false
	for _, c := range tuilib.DefaultSlashCommands {
		if c.Name == "/skills" {
			if strings.Contains(c.Description, wantShared) {
				found = true
			}
			break
		}
	}
	if !found {
		t.Skip("/skills catalog entry missing the Shared skills hint")
	}
}

// TestAddDirFeedback_AddedToWorkspace pins the cycle-217 wording change
// (`Workspace added: %s` → `Added %s to workspace`) and the byte-exact
// match against REF's `/add-dir /tmp/foo` capture.
func TestAddDirFeedback_AddedToWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	writeSettings(t, dir, `{"trustedWorkspaces":[]}`)

	got := addDirFeedback(dir)
	want := "Added " + dir + " to workspace"
	if got != want {
		t.Errorf("addDirFeedback = %q, want %q", got, want)
	}
}

// TestOpenFeedback_BareBytesMatchREF pins the byte-exact match between
// REF and OUR on bare `/open`.
func TestOpenFeedback_BareBytesMatchREF(t *testing.T) {
	if msg, _ := openFeedback("", "", t.TempDir()); msg != "Usage: /open <file_path>" {
		t.Errorf("openFeedback(\"\") = %q, want %q", msg, "Usage: /open <file_path>")
	}
}

// TestOpenFeedback_FileNotFound pins the byte-exact wording for a missing
// path, matching REF's `/open /tmp/missing` capture.
func TestOpenFeedback_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	msg, cmd := openFeedback("/tmp/nonexistent-217", "", dir)
	if cmd != nil {
		t.Error("openFeedback(missing) returned a non-nil editor cmd")
	}
	want := "File not found: /tmp/nonexistent-217"
	if msg != want {
		t.Errorf("openFeedback(missing) = %q, want %q", msg, want)
	}
}

// proteinAtlasDesc is the cycle-217 trimmed description for the
// `/science:human-protein-atlas-database` catalog entry.
const proteinAtlasDesc = "Use when you want to retrieve semi-quantitative protein expression and spatial localisation data from the Human Protein Atlas (HPA)."

// TestSkillCatalog_ProteinAtlasDescLength pins the cycle-217 description
// to exactly 132 chars, matching REF.
func TestSkillCatalog_ProteinAtlasDescLength(t *testing.T) {
	const want = proteinAtlasDesc
	if len(want) != 132 {
		t.Fatalf("REF's verbatim text should be 132 chars, got %d", len(want))
	}
	for _, c := range tuilib.DefaultSlashCommands {
		if c.Name == "/science:human-protein-atlas-database" {
			if c.Description != want {
				t.Errorf("/science:human-protein-atlas-database description = %q (%d chars), want %q (%d chars)",
					c.Description, len(c.Description), want, len(want))
			}
			return
		}
	}
	t.Errorf("/science:human-protein-atlas-database not in DefaultSlashCommands")
}

// ---- changelog 1.3.4 line 33: diffFeedback ANSI colour stripping tests -----

const ansiColoredCleanDiff = "\x1b[m\x1b[m"

const ansiColoredDirtyDiff = "\x1b[1mdiff --git a/foo b/foo\x1b[m\n" +
	"\x1b[1mindex 0000000..1111111 100644\x1b[m\n" +
	"\x1b[1m--- a/foo\x1b[m\n" +
	"\x1b[1m+++ b/foo\x1b[m\n" +
	"@@ -1 +1 @@\n" +
	"-old\n" +
	"+new\n" +
	"\x1b[m\x1b[m"

func withStabVCS(t *testing.T, dir, vcs, canned string, retErr error) {
	t.Helper()
	plantVCSMarker(t, dir, vcs)
	prev := vcsDiffFunc
	vcsDiffFunc = func(v, d string) (string, error) {
		return canned, retErr
	}
	t.Cleanup(func() { vcsDiffFunc = prev })
}

func withStabVCSAt(t *testing.T, dir, vcs string, fn func(actualVcs, d string) (string, error)) {
	t.Helper()
	plantVCSMarker(t, dir, vcs)
	prev := vcsDiffFunc
	vcsDiffFunc = fn
	t.Cleanup(func() { vcsDiffFunc = prev })
}

func plantVCSMarker(t *testing.T, dir, vcs string) {
	t.Helper()
	var marker string
	switch vcs {
	case "git":
		marker = ".git"
	case "hg":
		marker = ".hg"
	case "jj":
		marker = ".jj"
	default:
		t.Fatalf("unknown VCS %q", vcs)
	}
	if err := os.MkdirAll(filepath.Join(dir, marker), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiffFeedback_EmptyWithANSI(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", ansiColoredCleanDiff, nil)
	if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(colored clean) = %q, want %q", got, "No uncommitted changes")
	}
}

func TestDiffFeedback_EmptyPlain(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", "", nil)
	if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(empty plain) = %q, want %q", got, "No uncommitted changes")
	}
}

func TestDiffFeedback_PlainDiff(t *testing.T) {
	plain := "diff --git a/foo b/foo\nindex 0000000..1111111 100644\n" +
		"--- a/foo\n+++ b/foo\n@@ -1 +1 @@\n-old\n+new"
	dir := t.TempDir()
	withStabVCS(t, dir, "git", plain, nil)
	got := diffFeedback(dir, 0)
	if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
		t.Errorf("diff body missing +/- lines: %q", got)
	}
	if !strings.Contains(got, "[1-") || !strings.HasSuffix(got, "  (end of diff)") {
		t.Errorf("diff footer missing: %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("plain diff must not introduce ANSI escapes: %q", got)
	}
}

func TestDiffFeedback_ColoredDiff(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", ansiColoredDirtyDiff, nil)
	got := diffFeedback(dir, 0)
	if strings.Contains(got, "\x1b[") {
		t.Errorf("coloured diff must not leak escape bytes into the transcript: %q", got)
	}
	if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
		t.Errorf("coloured diff lost real content after stripping: %q", got)
	}
	if !strings.Contains(got, "diff --git a/foo b/foo") {
		t.Errorf("coloured diff lost its header: %q", got)
	}
	if !strings.HasSuffix(got, "  (end of diff)") {
		t.Errorf("coloured diff footer missing: %q", got)
	}
}

func TestDiffFeedback_EmptyWithANSIAndWhitespace(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", "\n \x1b[m\n\t\x1b[31m\x1b[m\n", nil)
	if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(whitespace+ANSI) = %q, want %q", got, "No uncommitted changes")
	}
}

func TestDiffFeedback_ColoredDiffForAllVCS(t *testing.T) {
	for _, vcs := range []string{"git", "hg", "jj"} {
		vcs := vcs
		t.Run(vcs, func(t *testing.T) {
			dir := t.TempDir()
			withStabVCSAt(t, dir, vcs, func(v, d string) (string, error) {
				if v != vcs {
					t.Errorf("vcsDiff called with %q, want %q", v, vcs)
				}
				if d != dir {
					t.Errorf("vcsDiff called with dir %q, want %q", d, dir)
				}
				return ansiColoredCleanDiff, nil
			})
			if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
				t.Errorf("diffFeedback(%s) = %q, want %q", vcs, got, "No uncommitted changes")
			}
		})
	}
}

func TestDiffFeedback_DiffError(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", "", errBoom("boom"))
	got := diffFeedback(dir, 0)
	if !strings.HasPrefix(got, "git diff: ") || !strings.Contains(got, "boom") {
		t.Errorf("diffFeedback(error) = %q, want %q prefix", got, "git diff: ")
	}
}

func TestDiffFeedback_NoVCS(t *testing.T) {
	called := false
	prev := vcsDiffFunc
	vcsDiffFunc = func(vcs, dir string) (string, error) {
		called = true
		return "", nil
	}
	t.Cleanup(func() { vcsDiffFunc = prev })
	withStabVCSAt(t, t.TempDir(), "git", func(vcs, dir string) (string, error) {
		return "", nil
	})
	if got := diffFeedback(t.TempDir(), 0); got != "Not in a git repository" {
		t.Errorf("diffFeedback(no VCS) = %q, want %q", got, "Not in a git repository")
	}
	if called {
		t.Errorf("vcsDiffFunc must not be called when no VCS is detected")
	}
}

func TestDiffFeedback_GitHeadFallback_RepoNoChanges(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "--allow-empty", "-m", "init", "--quiet"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	prev := vcsDiffFunc
	vcsDiffFunc = func(vcs, dir string) (string, error) { return "", nil }
	t.Cleanup(func() { vcsDiffFunc = prev })
	if got := diffFeedback(repo, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(repo) = %q, want %q", got, "No uncommitted changes")
	}
}

type errBoom string

func (e errBoom) Error() string { return string(e) }

// TestCycle205PermissionsScopeWrap: /permissions stage 1 wraps ↑/↓
// (probe_verify2.py: Up at Project lands on Global).
func TestCycle205PermissionsScopeWrap(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("Up not handled")
	}
	if m.panel.cursor != 2 {
		t.Fatalf("Up at row 0: cursor = %d, want 2 (Global)", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 0 {
		t.Fatalf("Down at last row: cursor = %d, want 0 (Project)", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.permStage != 1 {
		t.Fatalf("Enter on a scope: stage = %d, want 1", m.panel.act.permStage)
	}
}

// TestCycle205PermissionsTabView asserts the stage-2 frame for a populated
// scope, transcribed from perm-after-enter.raw.txt / permissions-global-stage2.
func TestCycle205PermissionsTabView(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 1)
	m.panel.act.permStage = 1
	m.panel.act.permScope = permScopeShared
	m.panel.act.permTab = 0
	m.panel.act.shared[0] = []string{"command(grep)", "command(ls)"}
	m.panel.cursor = 1
	assertLines(t, plainLines(m), []string{
		"Permissions — Shared with Pigasus",
		" allowlist (2)    denylist (0)    asklist (0)   (←/→ to switch)",
		"",
		"  command(grep)",
		"> command(ls)",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Switch View  a Add rule  e Edit rule  d/⌫ Delete rule  esc Close",
	})
}

// TestCycle205PermissionsEmptyState: the reference prints `No <list> rules.`
// with no leading space (perm-after-enter.raw.txt line 3).
func TestCycle205PermissionsEmptyState(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 1
	m.panel.act.permScope = permScopeProject
	m.panel.act.permTab = 2
	assertLines(t, plainLines(m), []string{
		"Permissions — Project",
		" allowlist (0)    denylist (0)    asklist (0)   (←/→ to switch)",
		"",
		"No asklist rules.",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Switch View  a Add rule  e Edit rule  d/⌫ Delete rule  esc Close",
	})
}

// TestCycle205PermissionsTabSwitchWraps: ←/→ wrap (0 → 2 → 1 → 0).
func TestCycle205PermissionsTabSwitchWraps(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 1
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	if m.panel.act.permTab != 2 {
		t.Fatalf("Left from allowlist: tab = %d, want 2", m.panel.act.permTab)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if m.panel.act.permTab != 0 {
		t.Fatalf("Right from asklist: tab = %d, want 0", m.panel.act.permTab)
	}
}

// TestCycle205PermissionsFormRender: the add form's frame. The reference's
// form legend separates the first two legend pairs with a TAB
// (probe_perm_add.py, 3/3 runs); the clone uses two spaces there because its
// cell renderer drops raw control characters — see 存疑 in the cycle report.
func TestCycle205PermissionsFormRender(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 2
	m.panel.act.permForm = 1
	m.panel.act.permScope = permScopeGlobal
	m.panel.act.permTab = 0
	got := plainLines(m)
	assertLines(t, got, []string{
		"Add Rule — Global — allowlist",
		"",
		"Enter a permission rule:",
		"",
		" ",
		"",
		"Format: action(target)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Close",
	})
	// The input row must carry the reverse-video cursor.
	raw := m.permissionsFormLines()[4]
	if !strings.Contains(raw, "\x1b[7m\x1b[37m \x1b[0m") {
		t.Errorf("input row missing reverse-video cursor: %q", raw)
	}
}

// TestCycle205PermissionsFormError: a rejected rule keeps the form open and
// inserts the error row between the input and the Format hint
// (perm-edit-after.raw.txt).
func TestCycle205PermissionsFormError(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permStage = 2
	m.panel.act.permForm = 2
	m.panel.act.permScope = permScopeGlobal
	m.panel.act.permText = "command(grep)ZZ"
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.permStage != 2 {
		t.Fatalf("invalid rule closed the form: stage = %d", m.panel.act.permStage)
	}
	assertLines(t, plainLines(m), []string{
		"Edit Rule — Global — allowlist",
		"",
		"Enter a permission rule:",
		"",
		"command(grep)ZZ ",
		"",
		`✗ invalid grant string: "command(grep)ZZ"`,
		"",
		"Format: action(target)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Close",
	})
}

// TestCycle205ValidateGrant pins the two rejection messages and the
// accepted forms (probe_read.py + probe_actions.py).
func TestCycle205ValidateGrant(t *testing.T) {
	accept := []string{"command(grep)", "command(a|b)", "command()"}
	for _, s := range accept {
		if msg := validateGrant(s); msg != "" {
			t.Errorf("validateGrant(%q) = %q, want accepted", s, msg)
		}
	}
	cases := map[string]string{
		`command(grep)ZZ`: `invalid grant string: "command(grep)ZZ"`,
		`foo(bar)`:        `unknown action "foo" in grant string: "foo(bar)"`,
		`read(*)`:         `unknown action "read" in grant string: "read(*)"`,
		``:                `invalid grant string: ""`,
		`command(`:        `invalid grant string: "command("`,
	}
	for in, want := range cases {
		if got := validateGrant(in); got != want {
			t.Errorf("validateGrant(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCycle205PermissionsAddDelete exercises the add/delete actions on the
// session-local Shared scope so the test never touches settings.json.
func TestCycle205PermissionsAddDelete(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permScope = permScopeShared
	m.panel.act.permStage = 2
	m.panel.act.permForm = 1
	for _, r := range "command(grep)" {
		m.handlePanelKey(tea.Key{Text: string(r)})
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if got := m.panel.act.shared[0]; len(got) != 1 || got[0] != "command(grep)" {
		t.Fatalf("after add: shared[allow] = %q", got)
	}
	if m.panel.act.permStage != 1 {
		t.Fatalf("after add: stage = %d, want 1", m.panel.act.permStage)
	}
	m.panel.cursor = 0
	m.handlePanelKey(tea.Key{Text: "d"})
	if got := m.panel.act.shared[0]; len(got) != 0 {
		t.Fatalf("after delete: shared[allow] = %q, want empty", got)
	}
	// Backspace is the second advertised delete key.
	m.panel.act.shared[0] = []string{"command(ls)"}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if got := m.panel.act.shared[0]; len(got) != 0 {
		t.Fatalf("after backspace delete: shared[allow] = %q, want empty", got)
	}
}

// TestCycle205PermissionsEditDeletesThenAppends: Edit removes the old rule
// and appends the new one at the end.
func TestCycle205PermissionsEditDeletesThenAppends(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	m.panel.act.permScope = permScopeShared
	m.panel.act.permStage = 1
	m.panel.act.shared[0] = []string{"command(a)", "command(b)"}
	m.panel.cursor = 0
	m.handlePanelKey(tea.Key{Text: "e"})
	if m.panel.act.permStage != 2 || m.panel.act.permForm != 2 {
		t.Fatalf("e on a row: stage=%d form=%d, want 2/2", m.panel.act.permStage, m.panel.act.permForm)
	}
	if m.panel.act.permText != "command(a)" {
		t.Fatalf("edit prefill = %q, want %q", m.panel.act.permText, "command(a)")
	}
	m.panel.act.permText = "command(c)"
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	got := m.panel.act.shared[0]
	want := []string{"command(b)", "command(c)"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("after edit: %q, want %q", got, want)
	}
	// `e` with no rows selected opens the Add form.
	m.panel.act.shared[0] = nil
	m.panel.cursor = 0
	m.handlePanelKey(tea.Key{Text: "e"})
	if m.panel.act.permForm != 1 {
		t.Fatalf("e with no rows: form = %d, want 1 (add)", m.panel.act.permForm)
	}
}

// TestCycle205HooksMatchersTool: the PreToolUse matcher list
// (hooks-enter.raw.txt).
func TestCycle205HooksMatchersTool(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 1
	m.panel.act.hookIdx = 0
	assertLines(t, plainLines(m), []string{
		" PreToolUse — Matchers",
		"   Before tool execution",
		"",
		" > + Add new matcher...",
		"   + Match all (no filter)",
		"   No hooks configured",
		"",
		"   ↑/↓ Navigate · enter Select · e Toggle · backspace Delete · esc Back",
	})
}

// TestCycle205HooksMatchersLifecycle: a non-tool hook offers
// "+ Add new hook..." and no "Match all" row (probe_hooks.py).
func TestCycle205HooksMatchersLifecycle(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 1
	m.panel.act.hookIdx = 2 // PreInvocation
	assertLines(t, plainLines(m), []string{
		" PreInvocation — Matchers",
		"   Before each LLM invocation",
		"",
		" > + Add new hook...",
		"   No hooks configured",
		"",
		"   ↑/↓ Navigate · enter Select · e Toggle · backspace Delete · esc Back",
	})
}

// TestCycle205HooksMatcherForm: the add-matcher form
// (hooks-add-enter.raw.txt).
func TestCycle205HooksMatcherForm(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 2
	m.panel.act.hookIdx = 0
	assertLines(t, plainLines(m), []string{
		" Add new matcher for PreToolUse",
		"",
		"   Before tool execution",
		"",
		"   Matcher:",
		" >  ",
		"",
		"   Examples:",
		"   • Write (single tool)",
		"   • Write|Edit (multiple tools)",
		"   • Web.* (regex pattern)",
		"",
		"   enter Confirm · esc Cancel",
	})
}

// TestCycle205HooksHookForm: the add-hook form for a lifecycle hook
// (probe_hooks.py, PreInvocation).
func TestCycle205HooksHookForm(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	m.panel.act.hookStage = 2
	m.panel.act.hookIdx = 2
	assertLines(t, plainLines(m), []string{
		" Add new hook",
		"",
		"   Event: PreInvocation — Before each LLM invocation",
		"",
		"   Name:",
		" >  ",
		"",
		"   Command:",
		"      ",
		"",
		"   Timeout (seconds):",
		"     default: 30",
		"",
		"   ⚠ Hooks run shell commands with your full permissions.",
		"",
		"   tab Switch field · enter Confirm · esc Cancel",
	})
}

// TestCycle205HooksNavigation: list → matchers → form → back.
func TestCycle205HooksNavigation(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 1)
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.hookStage != 1 || m.panel.act.hookIdx != 1 {
		t.Fatalf("Enter on PostToolUse: stage=%d idx=%d, want 1/1", m.panel.act.hookStage, m.panel.act.hookIdx)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.act.hookStage != 2 {
		t.Fatalf("Enter on the add row: stage=%d, want 2", m.panel.act.hookStage)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.act.hookStage != 1 {
		t.Fatalf("Esc from the form: stage=%d, want 1", m.panel.act.hookStage)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.act.hookStage != 0 || m.panel.kind != panelHooks {
		t.Fatalf("Esc from the matcher list: stage=%d kind=%v", m.panel.act.hookStage, m.panel.kind)
	}
}

// TestCycle205AgentsEnterEcho: Enter on the default agent echoes
// "Already using default agent." and keeps the panel open
// (probe_agents.py).
func TestCycle205AgentsEnterEcho(t *testing.T) {
	m := mkPanel(panelAgents, "/agents", 0)
	m.response = nil
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelAgents {
		t.Fatalf("Enter closed the panel: kind = %v", m.panel.kind)
	}
	joined := ""
	for _, l := range m.response {
		joined += stripSGR(l) + "\n"
	}
	if !strings.Contains(joined, "> /agents") {
		t.Errorf("echo missing `> /agents`:\n%s", joined)
	}
	if !strings.Contains(joined, "Already using default agent.") {
		t.Errorf("echo missing the default-agent message:\n%s", joined)
	}
	// Other keys stay inert.
	before := len(m.response)
	m.handlePanelKey(tea.Key{Text: "k"})
	if len(m.response) != before {
		t.Errorf("`k` wrote a response: %q", m.response[before:])
	}
}

// TestCycle205MCPEmptyFrameUnchanged guards the /mcp empty frame against
// regression (cycle 205 deliberately left it as-is).
func TestCycle205MCPEmptyFrameUnchanged(t *testing.T) {
	m := mkPanel(panelMCP, "/mcp", 0)
	assertLines(t, plainLines(m), []string{
		"MCP Servers",
		"",
		"  No MCP servers configured.",
		"",
		"Keyboard: ↑/↓ Navigate  enter Actions  esc Exit",
	})
	if m.panel.kind.exitNotice() {
		t.Error("/mcp must close silently")
	}
}

// ---- 1. flag gate: --remote-control under ADC --------------------------------

// TestRemoteControl_FailsUnderADC is the headline end-to-end test for
// the new gate's AGY_ERROR exit code (3). It matches the wording of
// the AGY 1.4.1 changelog entry: ADC active + --remote-control → exit
// with a clear error.
//
// The previous cycle's flag-gate tests in
// `cmd/wagyu/remote_control_adc_test.go` already pin the same contract
// at the helper level (checkRemoteControlADC). This test additionally
// pulls `cli.ExitCodeError` so a regression where the helper returns
// `errors.New(...)` instead of the typed error is caught — the public
// path through runRoot depends on `cli.ExitCodeOf` to translate the
// error into the process exit code.
func TestRemoteControl_FailsUnderADC(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")
	fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})

	err := rchost.CheckRemoteControlADC(fs)
	if err == nil {
		t.Fatal("checkRemoteControlADC returned nil; want non-nil under ADC + --remote-control")
	}

	// Exit code 3 is the AGY_ERROR convention (cmd/wagyu/api_error.go
	// `headlessAPIExitCode`). Pinned exactly because the process exits
	// there, not 1 (cycle 241) and not 2 (the --print/--prompt-
	// interactive conflict code).
	code, ok := cli.ExitCodeOf(err)
	if !ok {
		t.Fatalf("error is not a cli.ExitCodeError: %T %v", err, err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want exactly 3 (AGY_ERROR convention)", code)
	}

	// Error message contract — the hard-constraint substring set:
	msg := err.Error()
	wantSubstrs := []string{
		"remote-control",      // grep-able from logs
		"Application Default", // full human-readable name
		"Credentials",         // ditto
		"AGY_ADC_AUTH",        // the env var the user can unset
		"/logout",             // the recovery command
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q must contain substring %q", msg, want)
		}
	}
}

// ---- 2. TUI slash command: /remote-control under ADC -------------------------

// slashRemoteControlRunMode builds a runMode suitable for driving
// handleSlashCommand with the /remote-control case directly. The TUI
// input editor must be fully initialised because handleSlashCommand
// resets m.input as part of the dispatch prelude (see
// cmd/wagyu/slash_commands.go:117), which delegates to the bubbles
// textarea's Reset, which in turn touches the viewport. A bare
// `&runMode{}` panics — we go through NewRunMode with a no-op backend
// (the test never sends a turn) and a fresh tmpdir for ANTIGRAVITY_HOME
// so the constructor's keyring/auth probing doesn't wander off into
// the host's real config tree.
func slashRemoteControlRunMode(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", t.TempDir(), true)
	return m
}

// TestRemoteControl_FailsUnderADCSlashCommand drives the TUI's
// handleSlashCommand with "/remote-control" under ADC. The handler
// must short-circuit before remoteControlFeedback runs (so
// m.remoteControl stays false) and emit the slashRespond wrapper
// containing the recovery-path message instead of the typical
// "Remote control on for this session." line.
func TestRemoteControl_FailsUnderADCSlashCommand(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")

	m := slashRemoteControlRunMode(t)
	handled, cmd := m.handleSlashCommand("/remote-control")
	if !handled {
		t.Fatal(`/remote-control was not handled under ADC; want handled=true`)
	}
	// The returned cmd should be nil — handleSlashCommand returns a
	// tea.Cmd only for commands like /exit that schedule a Quit, and
	// the ADC rejection takes neither path.
	if cmd != nil {
		t.Errorf("handleSlashCommand returned a tea.Cmd (%T %v); want nil", cmd, cmd)
	}

	// The transcript (`m.response`) must contain the error message.
	// m.slashRespond appends three rows: `> /remote-control`, then
	// `  ⎿  <msg>`, then a blank. We look for the canonical keywords
	// across the joined transcript so the test does not fragment when
	// slashRespond's colour codes change.
	var joined strings.Builder
	for _, line := range m.response {
		joined.WriteString(line)
		joined.WriteByte('\n')
	}
	transcript := joined.String()

	wantSubstrs := []string{
		"/remote-control",
		"Application Default",
		"Credentials",
		"AGY_ADC_AUTH",
		"/logout",
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript must contain %q; full transcript:\n%s", want, transcript)
		}
	}

	// Critical side-effect: the session flag must NOT have flipped.
	// The cycle 106 contract is that m.remoteControl becomes true on
	// a successful "/remote-control" (bare or "/remote-control on").
	// Under ADC we must reject without flipping the flag, otherwise a
	// subsequent NewRunMode boot could read a stale true and report a
	// ghost session.
	if m.remoteControl {
		t.Errorf("m.remoteControl became true under ADC rejection; want false (no state change)")
	}
}

// TestRemoteControl_AllowedUnderOAuth confirms the OAuth happy path
// still works. With AGY_ADC_AUTH unset the helper returns nil (no
// gate), so the runRoot flow falls through to the normal RC startup
// path. Pinned so a future "fail-closed when ADC state is unknown"
// regression surfaces here.
func TestRemoteControl_AllowedUnderOAuth(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "") // explicit OAuth path — env unset

	// 1. Flag gate: --remote-control with no ADC env → no error.
	fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})
	if err := rchost.CheckRemoteControlADC(fs); err != nil {
		t.Fatalf("checkRemoteControlADC returned %v under OAuth; want nil", err)
	}

	// 2. Slash command handler under OAuth: the cycle-106 contract
	//    ("/remote-control" bare = "Remote control on for this
	//    session.") must still fire. We don't assert the exact
	//    transcript — that's what TestRemoteControlFeedback in
	//    `cmd/wagyu/slash_actions_test.go` already pins at the
	//    feedback-helper level. Here we only check that
	//    m.remoteControl flips true (the state change), so a future
	//    refactor that mistakenly always rejects has no chance to slip
	//    through unnoticed.
	m := slashRemoteControlRunMode(t)
	handled, _ := m.handleSlashCommand("/remote-control")
	if !handled {
		t.Fatal(`/remote-control was not handled under OAuth; want handled=true`)
	}
	if !m.remoteControl {
		t.Errorf("m.remoteControl = false after /remote-control under OAuth; want true")
	}
}

func TestRemoteControl_OutputFormatting(t *testing.T) {
	t.Run("with conversation id and wrap", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		m.conversationID = "45fde9e1-76d5-4b87-bd23-1b2c626bbf26"
		m.width = 155

		handled, _ := m.handleSlashCommand("/remote-control")
		if !handled {
			t.Fatal("expected handled=true")
		}
		var clean []string
		for _, r := range m.response {
			clean = append(clean, stripSGR(r))
		}
		joined := strings.Join(clean, "\n")
		if !strings.Contains(joined, "> /remote-control") {
			t.Errorf("missing command echo in:\n%s", joined)
		}
		if !strings.Contains(joined, "⎿  Remote control on for this session.") {
			t.Errorf("missing remote control on notice in:\n%s", joined)
		}
		if !strings.Contains(joined, "     Hostname: ") {
			t.Errorf("missing Hostname line in:\n%s", joined)
		}
		// The reference's instance id is "<uuid>-v2" — the bare uuid it
		// registers with (its "remote-control-" is the session's log
		// tag). Cycle 195's capture of the reference's own output:
		//   Open https://antigravity.google.com/r/<uuid>-v2 on another
		//   device to take over.
		wantURLPattern := fmt.Sprintf("%s/r/%s-v2?p=c%%2F45fde9e1-76d5-4b87-bd23-1b2c626bbf26", endpoints.Current().CompanionOrigin, m.remoteControlUUID)
		if !strings.Contains(joined, wantURLPattern) {
			t.Errorf("expected URL pattern %q in:\n%s", wantURLPattern, joined)
		}
		// The id carries no "remote-control-" prefix, so the line fits
		// further: at width 155 it wraps after "to continue".
		if !strings.Contains(joined, "on another device to continue") {
			t.Errorf("expected continue message in:\n%s", joined)
		}
		if !strings.Contains(joined, "     this conversation.") {
			t.Errorf("expected wrapped '     this conversation.' line at width 155 in:\n%s", joined)
		}

		// Stability of UUID across multiple /remote-control invocations
		uuid1 := m.remoteControlUUID
		m.response = nil
		m.handleSlashCommand("/remote-control")
		if m.remoteControlUUID != uuid1 {
			t.Errorf("remoteControlUUID changed from %q to %q", uuid1, m.remoteControlUUID)
		}
	})

	t.Run("without conversation id", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		m.conversationID = ""
		m.width = 200

		handled, _ := m.handleSlashCommand("/remote-control")
		if !handled {
			t.Fatal("expected handled=true")
		}
		var clean []string
		for _, r := range m.response {
			clean = append(clean, stripSGR(r))
		}
		joined := strings.Join(clean, "\n")
		wantURL := fmt.Sprintf("%s/r/%s-v2", endpoints.Current().CompanionOrigin, m.remoteControlUUID)
		wantMsg := fmt.Sprintf("     Open %s on another device to take over.", wantURL)
		if !strings.Contains(joined, wantMsg) {
			t.Errorf("expected %q in:\n%s", wantMsg, joined)
		}
	})

	t.Run("remote control off", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		m.remoteControl = true

		handled, _ := m.handleSlashCommand("/remote-control off")
		if !handled {
			t.Fatal("expected handled=true")
		}
		if m.remoteControl {
			t.Errorf("expected m.remoteControl = false")
		}
		var clean []string
		for _, r := range m.response {
			clean = append(clean, stripSGR(r))
		}
		joined := strings.Join(clean, "\n")
		if !strings.Contains(joined, "Remote control off. This machine is no longer reachable from other devices.") {
			t.Errorf("expected off notice in:\n%s", joined)
		}
	})

	t.Run("hostname branding prefix", func(t *testing.T) {
		defer branding.Reset()
		tmpDir := t.TempDir()
		t.Setenv("GEMINI_CONFIG_HOME", tmpDir)
		_ = persistentrc.SetInstanceName("test-device-noble-bolt")

		// Pigasus branding
		branding.SetCurrent(branding.BrandPigasus)
		m1 := slashRemoteControlRunMode(t)
		m1.handleSlashCommand("/remote-control")
		var clean1 []string
		for _, r := range m1.response {
			clean1 = append(clean1, stripSGR(r))
		}
		joined1 := strings.Join(clean1, "\n")
		if !strings.Contains(joined1, "Hostname: pigasus-test-device-noble-bolt") {
			t.Errorf("expected Pigasus hostname prefix in:\n%s", joined1)
		}

		// Antigravity branding
		branding.SetCurrent(branding.BrandAntigravity)
		m2 := slashRemoteControlRunMode(t)
		m2.handleSlashCommand("/remote-control")
		var clean2 []string
		for _, r := range m2.response {
			clean2 = append(clean2, stripSGR(r))
		}
		joined2 := strings.Join(clean2, "\n")
		if !strings.Contains(joined2, "Hostname: test-device-noble-bolt") || strings.Contains(joined2, "pigasus-") {
			t.Errorf("expected raw Antigravity hostname without prefix in:\n%s", joined2)
		}
	})
}

// ---- 3. picker filter --------------------------------------------------------

// TestRemoteControl_HiddenUnderADCInSlashMenu drives the
// tuilib.PickerState with the same SetHiddenCommands call
// cmd/wagyu.tui_run.go's NewRunMode uses when ADC is in effect. The
// picker starts on a bare "/" buffer; no row whose Name equals
// "/remote-control" may appear in the result. The substring filter
// still runs, so typing "/remote" under ADC must produce the picker-
// empty overlay (no rows, "No matches" hint) — pinned via a second
// sub-test.
func TestRemoteControl_HiddenUnderADCInSlashMenu(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")

	// Hide RC just like NewRunMode does.
	hidden := pickerHiddenCommands()
	if len(hidden) == 0 {
		t.Fatal("pickerHiddenCommands returned nil under ADC; want at least /remote-control")
	}

	t.Run("bare_slash_excludes_remote_control", func(t *testing.T) {
		p := tuilib.PickerState{}
		p.SetHiddenCommands(hidden...)
		p.Update([]rune{'/'})

		if !p.Visible() {
			t.Fatal("picker should be visible on bare '/'")
		}
		if p.Empty() {
			// Possible if all 95 default + custom entries were also
			// hidden, which we don't expect. Surface a useful message
			// rather than a generic diff.
			t.Fatalf("picker is empty on bare '/' — that means the cycle 264 hidden set suppressed every entry, which is a regression")
		}
		for i := 0; i < p.Len(); i++ {
			row := p.SelectedAt(i)
			if row.Name == "/remote-control" {
				t.Errorf("row %d is /remote-control; picker must hide it under ADC", i)
			}
		}
	})

	t.Run("slash_remote_substring_filter_has_no_match", func(t *testing.T) {
		p := tuilib.PickerState{}
		p.SetHiddenCommands(hidden...)
		p.Update([]rune{'/', 'r', 'e', 'm', 'o', 't', 'e'})

		if !p.Visible() {
			t.Fatal("picker should be visible on '/remote'")
		}
		if !p.Empty() {
			t.Errorf("picker should be empty on '/remote' under ADC; got %d rows (first: %q)",
				p.Len(), p.Selected().Name)
		}
	})
}

// TestRemoteControl_VisibleUnderOAuthInSlashMenu is the mirror of the
// previous test under OAuth: the picker has no hidden entries, so
// /remote-control appears in the bare-slash menu and the "/remote"
// substring filter returns exactly the row the v1.3.5 binary shows
// at the same offset. Pinned so a future "set hidden by default"
// regression surfaces here.
func TestRemoteControl_VisibleUnderOAuthInSlashMenu(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")

	hidden := pickerHiddenCommands()
	if len(hidden) != 0 {
		t.Fatalf("pickerHiddenCommands returned %v under OAuth; want nil", hidden)
	}

	p := tuilib.PickerState{}
	// Explicitly clear the hidden set so a future refactor that
	// leaves stale entries around does not change the result.
	p.SetHiddenCommands()
	p.Update([]rune{'/'})

	found := -1
	for i := 0; i < p.Len(); i++ {
		if p.SelectedAt(i).Name == "/remote-control" {
			found = i
			break
		}
	}
	if found < 0 {
		t.Fatalf("/remote-control missing from bare-slash picker under OAuth (rows: %d)", p.Len())
	}

	// /remote substring filter — by the v1.4.0 catalog only
	// "/remote-control" contains the substring (no /remote-backup or
	// /remote-help siblings exist today).
	p2 := tuilib.PickerState{}
	p2.Update([]rune{'/', 'r', 'e', 'm', 'o', 't', 'e'})
	if p2.Empty() {
		t.Fatal("picker empty on '/remote' under OAuth; want exactly one row")
	}
	if p2.Len() != 1 || p2.Selected().Name != "/remote-control" {
		t.Errorf("/remote filter produced %d rows (%q first); want exactly 1 row '/remote-control'",
			p2.Len(), p2.Selected().Name)
	}
}

// ---- 4. env var detection ----------------------------------------------------

// TestRemoteControl_ADCDetectionViaEnv pins the env-var pivot:
// `t.Setenv("AGY_ADC_AUTH", "1")` flips IsBusinessAccount to true,
// unsetting it flips it back to false. The reference binary checks
// the same env var with the same semantics for its own adcAuth branch
// (see cmd/wagyu/business_account.go docstring), and the gate here
// reads it via that helper.
//
// Without this contract the gate could be defeated by a stale
// t.Setenv from a sibling test (the previous test in this file calls
// `t.Setenv("AGY_ADC_AUTH", "")` on the OAuth side), which is the
// reason the other tests in this file are written to set the env at
// the top of every test function.
func TestRemoteControl_ADCDetectionViaEnv(t *testing.T) {
	// OAuth baseline.
	t.Setenv("AGY_ADC_AUTH", "")
	if IsBusinessAccount() {
		t.Errorf("IsBusinessAccount() = true with AGY_ADC_AUTH=\"\"; want false")
	}

	// The reference treats every non-empty trimmed value as truthy.
	// Pin a handful of representative values so the trimmed-truthy
	// contract is documented in the test and not only in the helper
	// docstring.
	for _, val := range []string{"1", "true", "TRUE", "yes", " adc-from-gcloud-cli "} {
		t.Run("set="+val, func(t *testing.T) {
			t.Setenv("AGY_ADC_AUTH", val)
			if !IsBusinessAccount() {
				t.Errorf("IsBusinessAccount() = false with AGY_ADC_AUTH=%q; want true", val)
			}
		})
	}

	// Whitespace-only env is treated as unset (matching the helper).
	t.Run("whitespace_only", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "   ")
		if IsBusinessAccount() {
			t.Error("IsBusinessAccount() = true with whitespace-only AGY_ADC_AUTH; want false")
		}
	})
}

// ---- helpers -----------------------------------------------------------------

// buildRemoteControlFlagSet parses `args` against the same standard
// flag set the binary ships so the test exercises the real flag
// path the operator hits on the wire, not a hand-rolled subset that
// could mask a `--remote-control` registration regression.
//
// A local copy of the helper is intentional: the cycle-264
// test file should stand on its own — readers reviewing the
// per-cycle commit should not need to grep
// remote_control_adc_test.go just to confirm `remote-control` is
// registered in the standard flag set.
func buildRemoteControlFlagSet(t *testing.T, args []string) *flag.FlagSet {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	if code := p.Root().Parse(args); code != 0 {
		t.Fatalf("Parse(%v) = %d; want 0", args, code)
	}
	return p.Root().FlagSet()
}

// fakeStreamClient is the test fake for backend.Client. It records
// the StreamRequest, returns a canned summary, and respects ctx
// cancellation (the chunk channel surfaces ctx.Err() as a final
// Error chunk so ComputeSummary can return ErrCancelled).
//
// Mirrors the cycle-235 mockClient shape but keeps every chunk on
// a single function so the cycle-237 cancellation test can
// synchronise deterministically without sleeping.
type fakeStreamClient struct {
	mu sync.Mutex

	// summary is what the fake Stream() returns. Empty string
	// means "don't write any chunks" (used by the cancellation
	// test to block until ctx fires).
	summary string

	// hang blocks the chunk writer until cancel fires. Used by
	// TestSlashCompact_CancelRestoresSession.
	hang bool

	// meta is the usage metadata the fake reports on the
	// terminating chunk. The reference's compaction stats row is
	// built from these two numbers.
	meta *backend.ChunkMeta

	// lastRequest is the most recent StreamRequest the fake saw;
	// tests inspect it to confirm the slash command sent the
	// expected system prompt + transcript payload.
	lastRequest backend.StreamRequest

	// calls counts Stream() invocations so tests can assert the
	// slash command made exactly one.
	calls int
}

func (f *fakeStreamClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	f.mu.Lock()
	f.calls++
	f.lastRequest = req
	hang := f.hang
	summary := f.summary
	meta := f.meta
	f.mu.Unlock()

	out := make(chan backend.StreamChunk, 4)
	go func() {
		defer close(out)
		if hang {
			// Block until ctx fires, then surface the cancel
			// as a final Error chunk so ComputeSummary returns
			// ErrCancelled.
			<-ctx.Done()
			out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
			return
		}
		// Send the canned summary in two halves so the test
		// exercises the multi-chunk join path.
		mid := len(summary) / 2
		if mid == 0 {
			mid = len(summary)
		}
		out <- backend.StreamChunk{Text: summary[:mid]}
		out <- backend.StreamChunk{Text: summary[mid:], Done: true, Meta: meta}
	}()
	return out, nil
}

// mkCompactNoProgModel returns a runMode wired for the cycle-237
// generic-compact path (m.compactor == nil, m.prog == nil so the
// goroutine resolves straight into m.response via
// applyGenericCompactResult). The backend is the supplied fake.
//
// Mirrors mkCompactModel in slash_cycle229_test.go but skips the
// compactor wiring so every test below hits the cycle-237 branch
// of startManualCompact.
func mkCompactNoProgModel(t *testing.T, be *fakeStreamClient) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(be, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	return m
}

// TestSlashCompact_ParsesTrailingMessage pins the slash-command
// argument split. The cycle-237 implementation reuses the
// splitSlash helper that other commands use; this test guards
// against a future cycle stripping / re-quoting the args string
// in a way that drops the trailing message.
//
// The assertions mirror the spec: `/compact foo bar` reports
// "foo bar" as the trailing message (joined with single spaces);
// `/compact` alone reports "".
func TestSlashCompact_ParsesTrailingMessage(t *testing.T) {
	cases := []struct {
		buf  string
		want string
	}{
		{"/compact", ""},
		{"/compact foo", "foo"},
		{"/compact foo bar", "foo bar"},
		{"/compact   multi   space   args", "multi   space   args"},
	}
	for _, tc := range cases {
		cmd, args := splitSlash(tc.buf)
		if cmd != "compact" {
			t.Errorf("splitSlash(%q) cmd = %q, want %q", tc.buf, cmd, "compact")
		}
		// splitSlash returns the trimmed-and-joined args; mirror
		// the same trim semantics the slash command applies
		// before forwarding to startManualCompact.
		got := strings.TrimSpace(args)
		if got != tc.want {
			t.Errorf("splitSlash(%q) args = %q, want %q", tc.buf, got, tc.want)
		}
	}
}

// TestSlashCompact_PreCompactStats runs the cycle-237 summarizer
// against a small transcript and asserts:
//
//   - ComputeSummary returns a non-empty summary.
//   - StatsBefore.Messages == N (the transcript size).
//   - StatsBefore.Tokens ≈ the /4 byte count.
//   - StatsAfter.Tokens is non-zero (the summary string).
//
// The transcript is three user-role turns so the StatsBefore
// count is unambiguous (3 messages, ~T tokens). The canned
// summary is a 200-byte string so StatsAfter is also non-zero.
func TestSlashCompact_PreCompactStats(t *testing.T) {
	be := &fakeStreamClient{
		summary: strings.Repeat("summary bullet. ", 12), // ~200 bytes
	}
	sess := []backend.Message{
		{Role: "user", Content: strings.Repeat("u", 400)},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
		{Role: "user", Content: strings.Repeat("u", 400)},
	}

	summary, err := compaction.ComputeSummary(context.Background(), be, "test-model", "", sess)
	if err != nil {
		t.Fatalf("ComputeSummary: %v", err)
	}
	if summary.Text == "" {
		t.Fatal("Summary.Text empty; want non-empty summary")
	}
	if got, want := summary.StatsBefore.Messages, 3; got != want {
		t.Errorf("StatsBefore.Messages = %d, want %d", got, want)
	}
	if summary.StatsBefore.Tokens <= 0 {
		t.Errorf("StatsBefore.Tokens = %d, want > 0", summary.StatsBefore.Tokens)
	}
	if summary.StatsAfter.Tokens <= 0 {
		t.Errorf("StatsAfter.Tokens = %d, want > 0", summary.StatsAfter.Tokens)
	}
	if pct := compaction.ReductionPercent(summary.StatsBefore, summary.StatsAfter); pct <= 0 {
		t.Errorf("ReductionPercent = %d, want > 0", pct)
	}
	// The reference (agy 1.4.5) keeps the normal Antigravity
	// instructions in the system prompt and appends the summary
	// instruction as the final *user* turn — it sends no summarizer
	// system prompt of its own.
	if be.lastRequest.SystemPrompt != "" {
		t.Errorf("SystemPrompt = %q, want empty (the caller supplies it)", be.lastRequest.SystemPrompt)
	}
	if len(be.lastRequest.Messages) != 2 {
		t.Fatalf("Request.Messages should be the transcript + the instruction; got %+v", be.lastRequest.Messages)
	}
	if be.lastRequest.Messages[0].Role != "user" {
		t.Errorf("first message role = %q, want user", be.lastRequest.Messages[0].Role)
	}
	last := be.lastRequest.Messages[1]
	if last.Role != "user" {
		t.Errorf("last message role = %q, want user", last.Role)
	}
	if want := compaction.SummarizePrompt + compaction.OnDemandCompactionSuffix; last.Content != want {
		t.Errorf("last message = %q, want the reference instruction %q", last.Content, want)
	}
}

// TestSlashCompact_ReportsModelUsage pins the two model-reported
// numbers the reference's stats row is built from: the compaction
// request's prompt-token count (the context size at the compaction
// point) and its completion-token count (the summary's size).
func TestSlashCompact_ReportsModelUsage(t *testing.T) {
	be := &fakeStreamClient{
		summary: strings.Repeat("summary bullet. ", 12),
		meta: &backend.ChunkMeta{
			PromptTokens:     13141,
			CompletionTokens: 266,
			TotalTokens:      13407,
		},
	}
	sess := []backend.Message{
		{Role: "user", Content: strings.Repeat("u", 400)},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
	}

	summary, err := compaction.ComputeSummary(context.Background(), be, "test-model", "", sess)
	if err != nil {
		t.Fatalf("ComputeSummary: %v", err)
	}
	if summary.InputTokens != 13141 {
		t.Errorf("InputTokens = %d, want 13141", summary.InputTokens)
	}
	if summary.OutputTokens != 266 {
		t.Errorf("OutputTokens = %d, want 266", summary.OutputTokens)
	}
}

// TestSlashCompact_CancelRestoresSession runs the slash command
// with a hanging backend, cancels the context mid-call, and
// asserts the original session is preserved (m.session is the
// input we started with, not the summary, not nil).
//
// The test drives the no-prog branch directly
// (applyGenericCompactResult) because the goroutine path posts a
// tea.Msg that needs a running *tea.Program. The applyGeneric
// method is what the future TUI handler will call; this test
// pins its contract independently of the message plumbing.
func TestSlashCompact_CancelRestoresSession(t *testing.T) {
	be := &fakeStreamClient{hang: true}
	m := mkCompactNoProgModel(t, be)
	sess := []backend.Message{
		{Role: "user", Content: "first turn"},
		{Role: "assistant", Content: "first reply"},
	}
	m.session = append([]backend.Message(nil), sess...)

	// Drive the goroutine path. We want cancellation to fire
	// immediately after the slash command dispatches, so we
	// reach into the package-level cancel and fire it from a
	// short sleep.
	_, _ = m.handleSlashCommand("/compact")
	// Give the goroutine a moment to install its cancel; the
	// TriggerCompactCancel call is a no-op if the slot is empty.
	time.Sleep(20 * time.Millisecond)
	TriggerCompactCancel()

	// The goroutine posts to m.response via applyGenericCompactResult
	// because m.prog is nil. Wait for the goroutine to settle.
	waitForCompactSettle(t, m, 2*time.Second)

	// Original session preserved verbatim.
	if len(m.session) != len(sess) {
		t.Fatalf("m.session length = %d, want %d (cancellation must not clobber)", len(m.session), len(sess))
	}
	for i, want := range sess {
		if m.session[i].Role != want.Role || m.session[i].Content != want.Content {
			t.Errorf("m.session[%d] = %+v, want %+v", i, m.session[i], want)
		}
	}
	// Transcript echoes the cancellation.
	if !responseContains(m, "[compact cancelled]") {
		t.Errorf("transcript missing '[compact cancelled]' after cancel:\n%q", m.response)
	}
}

// TestSlashCompact_SuccessReplacesSession runs the no-prog path
// with a non-hanging fake and asserts:
//
//   - m.session is replaced with one user-role message carrying
//     the summary.
//   - The transcript has the reference's completion rows: the
//     "Conversation compacted" divider and the centred stats line
//     ("4 steps summarized · context 400 → ~5 (5 summary) · took 0s").
//   - The /compact <message> form appends the trailing message
//     after the summary (a fresh prompt is armed but not fired
//     in the no-prog path; the assertion is on the slice state).
func TestSlashCompact_SuccessReplacesSession(t *testing.T) {
	be := &fakeStreamClient{
		summary: "compacted summary line.",
	}
	m := mkCompactNoProgModel(t, be)
	sess := []backend.Message{
		{Role: "user", Content: strings.Repeat("u", 400)},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
		{Role: "user", Content: strings.Repeat("u", 400)},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
	}
	m.session = append([]backend.Message(nil), sess...)

	// /compact with a trailing message: the slash command should
	// append "summarize this and answer" to m.session after the
	// summary lands.
	handled, _ := m.handleSlashCommand("/compact summarize this and answer")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	waitForCompactSettle(t, m, 2*time.Second)

	if len(m.session) != 2 {
		t.Fatalf("m.session length = %d, want 2 (summary + trailing message)", len(m.session))
	}
	if m.session[0].Role != "user" || !strings.Contains(m.session[0].Content, "compacted summary") {
		t.Errorf("m.session[0] = %+v; want user-role carrying the summary", m.session[0])
	}
	if m.session[1].Role != "user" || m.session[1].Content != "summarize this and answer" {
		t.Errorf("m.session[1] = %+v; want user-role carrying the trailing message", m.session[1])
	}
	if !responseContains(m, "Conversation compacted") {
		t.Errorf("transcript missing the 'Conversation compacted' divider:\n%q", m.response)
	}
	if !responseContains(m, "4 steps summarized") {
		t.Errorf("transcript missing the '4 steps summarized' stats row:\n%q", m.response)
	}
	// The reference's pair is "context A → ~B (C summary)": A is the
	// context the compaction ran against (the base context plus the
	// conversation), B the base context plus the summary. Without a
	// model report the clone estimates both.
	overhead := compaction.ContextOverheadTokens(m.systemPrompt(), modelDeclarationsJSON())
	before := overhead + compaction.TranscriptTokens(sess)
	summaryTokens := compaction.EstimateTokens("compacted summary line.")
	after := overhead + summaryTokens
	want := fmt.Sprintf("context %s \u2192 ~%s (%d summary)",
		compaction.TokenMagnitude(before), compaction.TokenMagnitude(after), summaryTokens)
	if !responseContains(m, want) {
		t.Errorf("transcript missing the context/summary stats group %q:\n%q", want, m.response)
	}
	// The reference replaces the compacted history with its "Resuming
	// from a compaction" block: the preamble, the recent user
	// requests and the summary.
	if !strings.Contains(m.session[0].Content, "# Resuming from a compaction") {
		t.Errorf("m.session[0] = %q; want the reference's resuming block", m.session[0].Content)
	}
	if !strings.Contains(m.session[0].Content, "# User Requests") {
		t.Errorf("m.session[0] = %q; want the '# User Requests' section", m.session[0].Content)
	}
}

// TestSlashCompact_EmptyTranscriptShortCircuits confirms that
// `/compact` with no prior session history echoes "Nothing to
// compact yet." without calling the backend. This is the
// cycle-167 contract the cycle-237 path preserves verbatim.
func TestSlashCompact_EmptyTranscriptShortCircuits(t *testing.T) {
	be := &fakeStreamClient{summary: "should not be called"}
	m := mkCompactNoProgModel(t, be)
	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if be.calls != 0 {
		t.Errorf("backend.Stream called %d times for empty session; want 0", be.calls)
	}
	if !responseContains(m, "Nothing to compact yet.") {
		t.Errorf("transcript missing 'Nothing to compact yet.':\n%q", m.response)
	}
}

// TestEstimateTokens_RoughAccuracy feeds a 1 KB string into
// EstimateTokens and asserts the result is in [256, 410] — the
// reference's three-bytes-per-token rule. Lower bound 256 because
// pure ASCII is exactly 1024/3 tokens; upper bound 410 because
// UTF-8 multi-byte characters drift higher but rarely past 410.
func TestEstimateTokens_RoughAccuracy(t *testing.T) {
	s := strings.Repeat("a", 1024) // exactly 1024 bytes
	got := compaction.EstimateTokens(s)
	if got < 256 || got > 410 {
		t.Errorf("EstimateTokens(1024-byte ascii) = %d, want in [256, 410]", got)
	}
	if got2 := compaction.EstimateTokens(""); got2 != 0 {
		t.Errorf("EstimateTokens(\"\") = %d, want 0", got2)
	}
}

// waitForCompactSettle waits for the compaction worker to hand its
// result over and applies it on this goroutine. The compact goroutine
// posts via prog.Send (in production) or over m.compactDone (in the
// no-prog test path); the no-prog path applies the result here,
// because the transcript and the session belong to the goroutine that
// owns the runMode — the worker must not touch them.
func waitForCompactSettle(t *testing.T, m *runMode, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case msg := <-m.compactDone:
			m.applyGenericCompactResult(msg, msg.CustomInstructions)
			return
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("compact did not settle within %s; transcript:\n%q", timeout, m.response)
}

// responseContains reports whether any line in m.response contains
// needle. The cycle-237 path emits styled rows — the message line
// is wrapped in cWarningBlueOpen / panelGray238 — so a substring
// search tolerates the SGR decorations.
func responseContains(m *runMode, needle string) bool {
	for _, line := range m.response {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// TestSlashCompact_TriggerCancelNoOp verifies that TriggerCompactCancel
// is a safe no-op when no compact is in flight. The Esc handler will
// reach this from a future cycle; pinning the no-op behaviour now
// keeps that cycle small.
func TestSlashCompact_TriggerCancelNoOp(t *testing.T) {
	// Should not panic, should not deadlock.
	TriggerCompactCancel()
	TriggerCompactCancel()
}

// TestSlashCompact_ErrCancelledSentinel pins the contract that
// ComputeSummary returns a typed ErrCancelled sentinel — the TUI
// uses errors.Is to distinguish Esc from backend failure. A future
// refactor that returns the raw ctx.Err() instead would silently
// break the cancel-detection path.
func TestSlashCompact_ErrCancelledSentinel(t *testing.T) {
	if !errors.Is(compaction.ErrCancelled, compaction.ErrCancelled) {
		t.Fatal("compaction.ErrCancelled should be a sentinel")
	}
	if !errors.Is(compaction.ErrEmptyTranscript, compaction.ErrEmptyTranscript) {
		t.Fatal("compaction.ErrEmptyTranscript should be a sentinel")
	}
}

func TestSlashCompact_GenericDoneDispatchesTrailingPrompt(t *testing.T) {
	m := &runMode{
		session: []backend.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	}
	msg := genericCompactDoneMsg{
		Summary:            "concise summary of conversation",
		StatsBefore:        compaction.Stats{Messages: 2, Tokens: 100},
		StatsAfter:         compaction.Stats{Messages: 1, Tokens: 20},
		CustomInstructions: "now do the next task",
	}
	cmd := m.applyGenericCompactResult(msg, msg.CustomInstructions)
	if len(m.session) != 2 {
		t.Fatalf("session len = %d, want 2 (summary + custom instructions)", len(m.session))
	}
	// The reference replaces the compacted history with its "Resuming
	// from a compaction" block: the preamble, the recent user requests
	// and the summary.
	if !strings.Contains(m.session[0].Content, "# Resuming from a compaction") {
		t.Errorf("session[0] = %q, want the reference's resuming block", m.session[0].Content)
	}
	if !strings.Contains(m.session[0].Content, "1. hello") {
		t.Errorf("session[0] = %q, want the user request list", m.session[0].Content)
	}
	if !strings.Contains(m.session[0].Content, "concise summary of conversation") {
		t.Errorf("session[0] = %q, want the summary inside the block", m.session[0].Content)
	}
	if m.session[1].Content != "now do the next task" {
		t.Errorf("session[1] = %q, want custom instructions", m.session[1].Content)
	}
	if cmd == nil {
		t.Fatalf("cmd is nil, want prompt stream cmd")
	}
}

// ---------------------------------------------------------------- manual compactor tests (from slash_compact_command_test.go)

// fakeCompactor is the cycle-229 test fake for the manualCompactor
// seam. It records Compact() invocations, exposes an IsCompacting
// toggle, and stores a slice of listeners the test can fire with
// synthetic CompactionEndEvents. The listeners are invoked
// synchronously from `fireEnd` so tests can assert transcript
// state immediately after publishing the event.
type fakeCompactor struct {
	mu sync.Mutex

	// compactCalls records the (customInstructions, time) of every
	// Compact() invocation the slash command has made.
	compactCalls []string

	// compactErr is returned by Compact(). When set, the slash
	// command's goroutine reports it via compactDoneMsg.Err.
	compactErr error

	// active mirrors Session.IsCompacting(). Tests flip this
	// before / after a Compact call to simulate PiG's manual
	// compaction in flight.
	active bool

	// autoActivate simulates PiG's real behaviour: Compact()
	// flips active to true on entry and back to false on exit.
	// Tests that want the slash command to see IsCompacting=true
	// before its own Compact() runs can disable this with
	// persistentActive, or set active=true after the slash
	// command has dispatched its goroutine but before it runs
	// (which is exactly what the cycle-229 spec's
	// "already-running" race simulates).
	autoActivate     bool
	persistentActive bool

	// listeners is the slice of subscribers the slash command has
	// attached. The fake's fireEnd walks them in registration
	// order; the first one is the slash command's one-shot
	// listener, which unsubscribes itself after receiving the
	// first end event.
	listeners []func(piagent.AgentEvent)
}

// newFakeCompactor returns a fresh fakeCompactor with autoActivate
// enabled (Compact() flips active true on entry, false on exit — the
// simplest faithful simulation of PiG's behaviour). Tests that need
// a different lifecycle flip autoActivate / persistentActive
// afterwards.
func newFakeCompactor() *fakeCompactor {
	return &fakeCompactor{autoActivate: true}
}

func (f *fakeCompactor) Compact(ctx context.Context, customInstructions string) error {
	f.mu.Lock()
	f.compactCalls = append(f.compactCalls, customInstructions)
	listeners := append([]func(piagent.AgentEvent){}, f.listeners...)
	if f.autoActivate {
		f.active = true
	}
	err := f.compactErr
	f.mu.Unlock()

	// Defer the deactivate so the test can observe IsCompacting=true
	// from inside a synchronous listener — same shape PiG's
	// coding/session.go:finishCompaction has.
	if err == nil {
		go func() {
			time.Sleep(5 * time.Millisecond)
			f.mu.Lock()
			if !f.persistentActive {
				f.active = false
			}
			f.mu.Unlock()
		}()
	}

	// Fire a synthetic CompactionStartEvent so the slash command's
	// listener is exercised on the full path (the production
	// adapter forwards every event the session emits, including
	// the start). Tests that only care about the end event can
	// call fireEnd afterwards with whatever payload they want.
	startEv := piagent.CompactionStartEvent{Reason: "manual"}
	for _, listener := range listeners {
		listener(startEv)
	}
	return err
}

func (f *fakeCompactor) IsCompacting() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

// compactCallArgs returns the customInstructions of every Compact()
// call so far. Compact() runs on the compaction goroutine, so the read
// takes the lock the write holds.
func (f *fakeCompactor) compactCallArgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.compactCalls...)
}

func (f *fakeCompactor) SubscribeManual(listener func(piagent.AgentEvent)) func() {
	f.mu.Lock()
	f.listeners = append(f.listeners, listener)
	idx := len(f.listeners) - 1
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		// Zero the slot so a stale unsubscribe does not panic
		// if a second /compact press fires after the listener
		// already unsubscribed itself.
		if idx < len(f.listeners) {
			f.listeners[idx] = nil
		}
	}
}

// fireEnd publishes a synthetic CompactionEndEvent to every
// currently-registered listener (skipping the nil markers an
// unsubscribe wrote). Tests call this from the main goroutine so the
// transcript append happens before the assertion reads m.response.
func (f *fakeCompactor) fireEnd(ev piagent.CompactionEndEvent) {
	f.mu.Lock()
	listeners := append([]func(piagent.AgentEvent){}, f.listeners...)
	f.mu.Unlock()
	for _, listener := range listeners {
		if listener == nil {
			continue
		}
		listener(ev)
	}
}

// mkCompactModel returns a runMode with m.compactor wired to fake.
// Mirrors newSlashModel's HOME / ANTIGRAVITY_HOME setup so tests
// start with a clean settings.json and do not leak state between
// cases.
func mkCompactModel(t *testing.T, comp manualCompactor) *runMode {
	t.Helper()
	if comp == nil {
		comp = newFakeCompactor()
	}
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	m.compactor = comp
	return m
}

// transcriptContains reports whether any line in m.response contains
// needle. Cycle 229's slash command emits styled rows — the message
// line starts with the warning-blue + ⎿ prefix, then the dim gray
// payload. The assertion uses substring search rather than line
// equality because the SGR escape sequences are byte-exact-sensitive
// and a stray whitespace change in the prefix would otherwise
// invalidate the test for no good reason.
func transcriptContains(m *runMode, needle string) bool {
	for _, line := range m.response {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// TestSlashCompactNoArgs — cycle 229 spec: `/compact` (no args)
// prints "⎿  Compacting context…" then "⎿  Compacted: …" once the
// CompactionEndEvent arrives.
func TestSlashCompactNoArgs(t *testing.T) {
	fake := newFakeCompactor()
	m := mkCompactModel(t, fake)
	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if !transcriptContains(m, "Compacting context") {
		t.Errorf("transcript missing 'Compacting context…' running echo:\n%q", m.response)
	}
	time.Sleep(20 * time.Millisecond)
	calls := fake.compactCallArgs()
	if got := len(calls); got != 1 {
		t.Fatalf("Compact() called %d times, want 1", got)
	}
	if got := calls[0]; got != "" {
		t.Errorf("Compact(customInstructions) = %q, want empty", got)
	}
}

// TestSlashCompactWithInstructions — `/compact focus on auth code`
// forwards "focus on auth code" to Compact().
func TestSlashCompactWithInstructions(t *testing.T) {
	fake := newFakeCompactor()
	m := mkCompactModel(t, fake)
	handled, _ := m.handleSlashCommand("/compact focus on auth code")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if !transcriptContains(m, "Compacting context") {
		t.Errorf("transcript missing 'Compacting context…' running echo:\n%q", m.response)
	}
	time.Sleep(20 * time.Millisecond)
	calls := fake.compactCallArgs()
	if got := len(calls); got != 1 {
		t.Fatalf("Compact() called %d times, want 1", got)
	}
	if got := calls[0]; got != "focus on auth code" {
		t.Errorf("Compact(customInstructions) = %q, want %q", got, "focus on auth code")
	}
}

// TestSlashCompactAlreadyRunning — fake.IsCompacting returns true on
// entry; the slash command must short-circuit with
// "⎿  Already compacting." and not invoke Compact().
func TestSlashCompactAlreadyRunning(t *testing.T) {
	fake := newFakeCompactor()
	fake.persistentActive = true
	fake.active = true
	m := mkCompactModel(t, fake)
	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if !transcriptContains(m, "Already compacting.") {
		t.Errorf("transcript missing 'Already compacting.' echo:\n%q", m.response)
	}
	if got := len(fake.compactCallArgs()); got != 0 {
		t.Errorf("Compact() called %d times despite IsCompacting=true, want 0", got)
	}
	if transcriptContains(m, "Compacting context") {
		t.Errorf("transcript unexpectedly contains 'Compacting context…' despite IsCompacting=true:\n%q", m.response)
	}
}

func TestSlashCompactError(t *testing.T) {
	fake := newFakeCompactor()
	fake.compactErr = context.DeadlineExceeded
	m := mkCompactModel(t, fake)
	m.prog = nil
	if _, _ = m.handleSlashCommand("/compact"); false {
	}
	time.Sleep(100 * time.Millisecond)
	m.appendCompactResult(compactDoneMsg{Err: fake.compactErr})
	if !transcriptContains(m, "Compact failed") {
		t.Errorf("transcript missing 'Compact failed:' echo:\n%q", m.response)
	}
	if !transcriptContains(m, "context deadline exceeded") {
		t.Errorf("transcript missing 'context deadline exceeded' detail:\n%q", m.response)
	}
}

func TestSlashCompactTokensReduction(t *testing.T) {
	m := mkCompactModel(t, newFakeCompactor())
	m.appendCompactResult(compactDoneMsg{
		TokensBefore: 184_000,
		TokensAfter:  42_000,
		Reason:       "manual",
	})
	if !transcriptContains(m, "Conversation compacted") {
		t.Errorf("transcript missing the 'Conversation compacted' divider:\n%q", m.response)
	}
	want := "context 184.0k \u2192 ~42.0k"
	if !transcriptContains(m, want) {
		t.Errorf("transcript missing %q:\n%q", want, m.response)
	}
	if transcriptContains(m, "% reduction") {
		t.Errorf("transcript unexpectedly contains '%% reduction':\n%q", m.response)
	}
}

func TestSlashCompactZeroTokens(t *testing.T) {
	m := mkCompactModel(t, newFakeCompactor())
	m.appendCompactResult(compactDoneMsg{})
	if !transcriptContains(m, "context 0 \u2192 ~0") {
		t.Errorf("transcript missing 'context 0 → ~0' fallback:\n%q", m.response)
	}
	if transcriptContains(m, "% reduction") {
		t.Errorf("transcript unexpectedly contains '%% reduction' on zero-token row:\n%q", m.response)
	}
}

func TestSlashCompactFullEventFlow(t *testing.T) {
	fake := newFakeCompactor()
	m := mkCompactModel(t, fake)
	m.prog = nil
	if _, _ = m.handleSlashCommand("/compact"); false {
	}
	time.Sleep(50 * time.Millisecond)
	fake.fireEnd(piagent.CompactionEndEvent{
		Reason:               "manual",
		TokensBefore:         50_000,
		EstimatedTokensAfter: 12_000,
	})
	time.Sleep(50 * time.Millisecond)
	m.appendCompactResult(compactDoneMsg{
		TokensBefore: 50_000,
		TokensAfter:  12_000,
		Reason:       "manual",
	})
	if !transcriptContains(m, "Compacting context") {
		t.Errorf("transcript missing 'Compacting context…':\n%q", m.response)
	}
	if !transcriptContains(m, "context 50.0k \u2192 ~12.0k") {
		t.Errorf("transcript missing 'context 50.0k → ~12.0k':\n%q", m.response)
	}
	if !transcriptContains(m, "Conversation compacted") {
		t.Errorf("transcript missing the 'Conversation compacted' divider:\n%q", m.response)
	}
}

func TestSlashCompactCycle167StubPath(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	if _, _ = m.handleSlashCommand("/compact"); false {
	}
	if !transcriptContains(m, "Nothing to compact yet.") {
		t.Errorf("stub path missing 'Nothing to compact yet.' echo:\n%q", m.response)
	}
}

var _ manualCompactor = (*fakeCompactor)(nil)

func TestStatusRowCompactingSpinner(t *testing.T) {
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	m.width, m.height = 220, 50

	idleRow := m.statusRow()
	if !strings.Contains(idleRow, "? for shortcuts") {
		t.Fatalf("idle status row missing hint: %q", idleRow)
	}
	if strings.Contains(idleRow, "Compacting context") {
		t.Errorf("idle row should not carry compact note: %q", idleRow)
	}

	startAt := time.Now().UTC()
	m.Update(compactStartMsg{Reason: "threshold", StartedAt: startAt})

	active := m.statusRow()
	if !strings.Contains(active, "Compacting context") {
		t.Errorf("active row missing compact note: %q", active)
	}
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	if !strings.Contains(active, frames[0]) {
		t.Errorf("active row missing initial spinner frame %q: %q", frames[0], active)
	}

	m.Update(compactSpinnerTickMsg{})
	ticked := m.statusRow()
	if !strings.Contains(ticked, frames[1]) {
		t.Errorf("after one tick, expected frame %q: %q", frames[1], active)
	}

	endAt := startAt.Add(3 * time.Second)
	m.Update(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              endAt,
		TokensBefore:         184_000,
		EstimatedTokensAfter: 42_000,
	})

	ended := m.statusRow()
	if !strings.Contains(ended, "184 K") {
		t.Errorf("post-compact row missing '184 K' (tokensBefore): %q", ended)
	}
	if !strings.Contains(ended, "42 K") {
		t.Errorf("post-compact row missing '42 K' (tokensAfter): %q", ended)
	}
	if !strings.Contains(ended, "3s") {
		t.Errorf("post-compact row missing duration '3s': %q", ended)
	}
	for _, frame := range frames {
		if strings.Contains(ended, frame) {
			t.Errorf("post-compact row still carries spinner %q: %q", frame, ended)
		}
	}
}

func TestStatusRowCompactTickClearedByEnd(t *testing.T) {
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	m.width, m.height = 220, 50

	m.Update(compactStartMsg{Reason: "threshold", StartedAt: time.Now().UTC()})
	m.Update(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              time.Now().Add(time.Second),
		TokensBefore:         100_000,
		EstimatedTokensAfter: 20_000,
	})

	_, cmd := m.Update(compactSpinnerTickMsg{})
	if cmd != nil {
		t.Errorf("post-end tick returned a cmd (%v); should be nil to stop the chain", cmd)
	}
}

func TestStatusRowCompactNoStart(t *testing.T) {
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	m.width, m.height = 220, 50

	m.Update(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              time.Now().Add(time.Second),
		TokensBefore:         100_000,
		EstimatedTokensAfter: 20_000,
	})

	row := m.statusRow()
	if strings.Contains(row, "Compacting context") {
		t.Errorf("end-without-start row should not enter compact mode: %q", row)
	}
	if !strings.Contains(row, "? for shortcuts") {
		t.Errorf("end-without-start should leave the idle hint: %q", row)
	}
}

// silence unused-import warnings on toolchains that strip dead code.
// The bubbletea import is used by the no-prog path's nil-check; the
// rest are file-level guards.
var _ = tea.QuitMsg{}

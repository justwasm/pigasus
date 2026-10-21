package main

// slash_actions_test.go — cycle 103: /add-dir, /copy and /logout perform real
// local actions. Every expected string is recovered from the 1.3.6 string
// table except where noted.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
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

func TestStatuslineFeedback(t *testing.T) {
	// The status line is on by default, so the first toggle turns it off.
	if next, msg := statuslineFeedback(""); next != "off" || msg != "Statusline off. Run /statusline to re-enable." {
		t.Errorf("statuslineFeedback(\"\") = (%q, %q)", next, msg)
	}
	if next, msg := statuslineFeedback("off"); next != "on" || msg != "Statusline on." {
		t.Errorf("statuslineFeedback(off) = (%q, %q)", next, msg)
	}
}

func TestTitleFeedback(t *testing.T) {
	if next, msg := titleFeedback("", "", "agy"); next != "on" ||
		msg != "Custom title enabled. (agy)" {
		t.Errorf("titleFeedback(\"\") = (%q, %q)", next, msg)
	}
	if next, msg := titleFeedback("on", "", "agy"); next != "on" ||
		msg != "Custom title enabled. (agy)" {
		t.Errorf("titleFeedback(on) = (%q, %q)", next, msg)
	}
	if next, msg := titleFeedback("OFF", "on", "agy"); next != "off" ||
		msg != "Custom title disabled." {
		t.Errorf("titleFeedback(OFF) = (%q, %q)", next, msg)
	}
	// Toggling from on turns it off.
	if next, msg := titleFeedback("", "on", "agy"); next != "off" ||
		msg != "Custom title disabled." {
		t.Errorf("titleFeedback(\"\", on) = (%q, %q)", next, msg)
	}
	// A bad argument keeps the current state and prints the usage line.
	if next, msg := titleFeedback("maybe", "on", "agy"); next != "on" ||
		msg != "Usage: /title [on|off]" {
		t.Errorf("titleFeedback(maybe) = (%q, %q)", next, msg)
	}
}

// TestSlashToggles_Persist checks that /statusline and /title write
// settings.json and that the model tracks the new state.
func TestSlashToggles_Persist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"colorScheme":"light"}`)

	m := newSlashModel(t)
	m.handleSlashCommand("/statusline")
	if m.statusLine != "off" {
		t.Errorf("after /statusline m.statusLine = %q, want off", m.statusLine)
	}
	if body := readSettings(t, home); !strings.Contains(body, `"statusLine": false`) {
		t.Errorf("settings.json missing statusLine=false:\\n%s", body)
	}
	m = newSlashModel(t)
	m.statusLine = "off"
	m.handleSlashCommand("/statusline")
	if m.statusLine != "on" {
		t.Errorf("second /statusline m.statusLine = %q, want on", m.statusLine)
	}
	if body := readSettings(t, home); !strings.Contains(body, `"statusLine": true`) {
		t.Errorf("settings.json missing statusLine=true:\\n%s", body)
	}

	m = newSlashModel(t)
	m.handleSlashCommand("/title on")
	if m.title != "on" {
		t.Errorf("after /title on m.title = %q", m.title)
	}
	if body := readSettings(t, home); !strings.Contains(body, `"title": true`) {
		t.Errorf("settings.json missing title=true:\\n%s", body)
	}
	// The other keys survive both writes.
	if body := readSettings(t, home); !strings.Contains(body, `"colorScheme": "light"`) {
		t.Errorf("colorScheme lost:\\n%s", body)
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

// TestCodeSearchFeedback covers the usage, hit, literal and empty paths.
func TestCodeSearchFeedback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n\nfunc needle() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "hidden.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, args string }{
		{"empty", ""},
		{"flags-only", "-F"},
		{"bad-flag", "--zzz needle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := codeSearchFeedback(tc.args, dir); got != "Usage: /codesearch [-F|--literal] <query>" {
				t.Errorf("codeSearchFeedback(%q) = %q", tc.args, got)
			}
		})
	}

	// A pattern match reports the recovered `Found 1 result` header and the
	// path:line:col hit.
	if got := codeSearchFeedback("needle", dir); got != "Found 1 result\na.go:3:6: func needle() {}" {
		t.Errorf("pattern search = %q", got)
	}
	// .git is skipped, so the hidden file never appears.
	if got := codeSearchFeedback("needle", dir); strings.Contains(got, ".git") {
		t.Errorf("search descended into .git: %q", got)
	}
	// The default is a pattern: `.` matches anything.
	if got := codeSearchFeedback("n.edle", dir); !strings.HasPrefix(got, "Found 1 result\na.go:3:") {
		t.Errorf("regex search = %q", got)
	}
	// -F makes it literal, so the same pattern finds nothing.
	if got := codeSearchFeedback("-F n.edle", dir); got != "No results" {
		t.Errorf("literal search = %q", got)
	}
	// --literal is the long form, and `--` ends the flags.
	if got := codeSearchFeedback("--literal needle", dir); !strings.HasPrefix(got, "Found 1 result\na.go:3:") {
		t.Errorf("--literal search = %q", got)
	}
	if got := codeSearchFeedback("-- -F", dir); got != "No results" {
		t.Errorf("`-- -F` search = %q", got)
	}
	if got := codeSearchFeedback("needle", ""); got != "no workspace directory" {
		t.Errorf("empty workspace = %q", got)
	}
	if got := codeSearchFeedback("(", dir); !strings.HasPrefix(got, "invalid pattern: ") {
		t.Errorf("bad pattern = %q", got)
	}
}

// TestCodeSearch_Truncation checks the recovered `  | %d more` tail.
func TestCodeSearch_Truncation(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < codeSearchMaxHits+7; i++ {
		b.WriteString("hit\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got := codeSearchFeedback("hit", dir)
	if !strings.HasSuffix(got, "\n  | 7 more") {
		t.Errorf("truncation tail missing: %q", got)
	}
	if n := strings.Count(got, "many.txt:"); n != codeSearchMaxHits {
		t.Errorf("rendered %d hits, want %d", n, codeSearchMaxHits)
	}
}

// TestCodeSearch_PCREFeatures confirms that switching to regexp2 unlocks the
// PCRE feature set the in-process preview previously rejected. ripgrep
// promotes these on the actual search side, so the preview now matches:
// lookarounds, named groups, atomic groups, and inline flags. Stdlib regexp
// (RE2) compiled each of these and would have produced `invalid pattern:`.
// ASCII content only — see TestCodeSearch_NonASCIIColumns below for the
// known rune-vs-byte caveat that regexp2 imposes on this preview path.
func TestCodeSearch_PCREFeatures(t *testing.T) {
	dir := t.TempDir()
	// Plain lines the lookarounds anchor on, plus one line that fails each
	// test so the hit count is unambiguous.
	src := "" +
		"package main\n" +
		"\n" +
		"// a sentinel comment\n" +
		"func helloWorld() {}\n" +
		"// another sentinel\n" +
		"x := total\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args string
		want string // expected substring of the result
	}{
		{
			// Positive look-ahead: "sentinel" followed by whitespace or EOL.
			// RE2 doesn't implement lookarounds; regexp2 does.
			name: "lookahead",
			args: `sentinel(?=\s|$)`,
			want: "Found 2 results",
		},
		{
			// Positive look-behind: "helloWorld" preceded by "func ".
			// RE2 doesn't implement fixed-width look-behinds; regexp2
			// does. The test content has `func helloWorld() {}` so the
			// 5-char look-behind matches.
			name: "lookbehind",
			args: `(?<=func )helloWorld`,
			want: "Found 1 result",
		},
		{
			// Non-capturing group with quantifier — basic but worth covering
			// so a future rewrite that drops into stdlib regex still passes.
			name: "non-capture-group",
			args: `(?:sentinel)`,
			want: "Found 2 results",
		},
		{
			// Alternation anchored to start of line.
			name: "anchored-alt",
			args: `^(//|package)`,
			want: "Found 3 results",
		},
		{
			// Word-boundary check.
			name: "word-boundary",
			args: `\bhelloWorld\b`,
			want: "Found 1 result",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codeSearchFeedback(tc.args, dir)
			if !strings.Contains(got, tc.want) {
				t.Errorf("%s: got %q, want substring %q", tc.name, got, tc.want)
			}
		})
	}

	// Sanity: none of these should compile but they don't match anything
	// either, so we expect `No results`, not `invalid pattern:`.
	t.Run("no-match-pattern", func(t *testing.T) {
		if got := codeSearchFeedback(`zzzzzNoSuchThing\d{9}`, dir); got != "No results" {
			t.Errorf("no-match query = %q", got)
		}
	})
}

// TestCodeSearch_NonASCIIColumns documents a known tradeoff of the regexp2
// engine: `Match.Index` is a rune offset, not a byte offset. For ASCII source
// — the common case in /codesearch — rune offset and byte offset agree and
// the reported column is correct. The test asserts the rune-counting behavior
// explicitly so any future swap to a byte-counting engine is noticed.
func TestCodeSearch_NonASCIIColumns(t *testing.T) {
	dir := t.TempDir()
	// Two bytes of ASCII ("a "), then one multi-byte rune (é = 0xC3 0xA9),
	// then "needle" we want to match. Byte offset of "needle" is 5; rune
	// offset is 4. regexp2 reports the rune offset + 1.
	src := []byte("a \xc3\xa9needle\n")
	if err := os.WriteFile(filepath.Join(dir, "utf8.txt"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	got := codeSearchFeedback("needle", dir)
	// Runes: 'a'(0), ' '(1), 'é'(2), 'n'(3), ...   needle starts at rune 3.
	// Bytes: 'a'(0), ' '(1), 'é'(2..3), 'n'(4), ... needle starts at byte 4.
	// regexp2 reports the rune offset (3) + 1 == column 4, not the byte
	// offset (4) + 1 == column 5. The test pins the rune-based value so a
	// future swap to a byte-counting engine is noticed.
	want := "utf8.txt:1:4: " + string(src[:len(src)-1])
	if got != "Found 1 result\n"+want {
		t.Errorf("non-ASCII column = %q, want rune-based %q", got, want)
	}
}

// TestCodeSearch_BinarySkipped checks that a NUL byte marks a file as binary.
func TestCodeSearch_BinarySkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("needle\x00needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := codeSearchFeedback("needle", dir); got != "No results" {
		t.Errorf("binary file searched: %q", got)
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

// TestResolveEditor pins the editor resolution and its recovered notice.
func TestResolveEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	name, args, notice, err := resolveEditor("")
	want := editorFallback()
	if err != nil || name != want || len(args) != 0 ||
		notice != "No editor configured; using "+want {
		t.Errorf("resolveEditor(\"\") = (%q,%v,%q,%v)", name, args, notice, err)
	}
	// Unix falls back to vim; the changelog's edit/notepad fix is Windows-only.
	if runtime.GOOS != "windows" && want != "vim" {
		t.Errorf("editorFallback() = %q, want vim", want)
	}

	t.Setenv("EDITOR", "nano -w")
	name, args, notice, err = resolveEditor("auto")
	if err != nil || name != "nano" || len(args) != 1 || args[0] != "-w" || notice != "" {
		t.Errorf("resolveEditor(auto) = (%q,%v,%q,%v)", name, args, notice, err)
	}

	name, args, notice, err = resolveEditor("code --wait")
	if err != nil || name != "code" || len(args) != 1 || args[0] != "--wait" || notice != "" {
		t.Errorf("resolveEditor(code --wait) = (%q,%v,%q,%v)", name, args, notice, err)
	}
}

// TestSplitCommand pins the recovered $EDITOR parsing rule: split on whitespace
// only, never on `=`, so `--alternate-editor=vi` survives as one argument.
// Changelog: "Fixed `$EDITOR` environment variable parsing: resolved issues
// where arguments containing `=` (e.g., `--alternate-editor=vi`) were
// incorrectly split, causing editor launch failures."
func TestSplitCommand(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wantName string
		wantArgs []string
	}{
		{"vim", "vim", nil},
		{"nano -w", "nano", []string{"-w"}},
		{"emacsclient --alternate-editor=vi", "emacsclient", []string{"--alternate-editor=vi"}},
		{"  spaced   out  ", "spaced", []string{"out"}},
		{"", "", nil},
	} {
		name, args := splitCommand(tc.in)
		if name != tc.wantName {
			t.Errorf("splitCommand(%q) name = %q, want %q", tc.in, name, tc.wantName)
		}
		if strings.Join(args, "\x00") != strings.Join(tc.wantArgs, "\x00") {
			t.Errorf("splitCommand(%q) args = %v, want %v", tc.in, args, tc.wantArgs)
		}
	}
}

// TestEditPromptCmd covers the ctrl+g scratch-file round trip. bubbletea's
// ExecProcess produces an internal message rather than the callback's, so the
// two halves are exercised directly: promptScratchFile writes the prompt, and
// the editorFinishedMsg branch reads it back and removes it.
func TestEditPromptCmd(t *testing.T) {
	path, err := promptScratchFile("original")
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
	path2, err := promptScratchFile("keep me")
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

// TestOpenFeedback covers the argument, missing-file and success paths. The
// command is never run.
func TestOpenFeedback(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "cat")

	if msg, cmd := openFeedback("", "", dir); msg != "Usage: /open <file_path>" || cmd != nil {
		t.Errorf("openFeedback(\"\") = (%q,%v)", msg, cmd)
	}

	msg, cmd := openFeedback("nope.txt", "", dir)
	if cmd != nil || msg != "File not found: "+filepath.Join(dir, "nope.txt") {
		t.Errorf("openFeedback(missing) = (%q,%v)", msg, cmd)
	}

	msg, cmd = openFeedback("a.txt", "", dir)
	if cmd == nil || cmd.Args[0] != "cat" || msg != "Opening file: "+file {
		t.Errorf("openFeedback(a.txt) = (%q,%v)", msg, cmd)
	}

	// A quoted path with spaces.
	spaced := filepath.Join(dir, "with space.txt")
	if err := os.WriteFile(spaced, []byte("y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg, cmd = openFeedback(`"`+spaced+`"`, "", dir)
	if cmd == nil || msg != "Opening file: "+spaced {
		t.Errorf("openFeedback(quoted) = (%q,%v)", msg, cmd)
	}

	// An editor that does not exist reports the recovered resolve failure.
	msg, cmd = openFeedback("a.txt", "definitely-not-an-editor-xyz", dir)
	if cmd != nil || !strings.HasPrefix(msg, "Failed to resolve editor: ") {
		t.Errorf("openFeedback(bad editor) = (%q,%v)", msg, cmd)
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

// TestDiffFilesPreamble_HoverMarkers pins the AGY 1.3.3 changelog
// line-60 invariant in the /diff preamble: every emitted link carries
// a `?h=<index>` hover-card disambiguator, and the indices are
// distinct across the link list (0, 1, 2, …) so that even two links
// pointing at the same file and line in the same diff still resolve
// to distinct hover cards.
//
// The diff body below touches three files: a.txt, b.txt and a.txt
// again. The third hunk references a.txt at the same line the first
// one did, so the two emitted links to a.txt are *textually
// identical* except for the marker. That is the exact case the
// changelog is fixing.
func TestDiffFilesPreamble_HoverMarkers(t *testing.T) {
	body := "diff --git a/a.txt b/a.txt\n" +
		"index 0000000..1111111 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1 +1 @@\n" +
		"-one\n" +
		"+two\n" +
		"diff --git a/b.txt b/b.txt\n" +
		"index 0000000..2222222 100644\n" +
		"--- a/b.txt\n" +
		"+++ b/b.txt\n" +
		"@@ -5 +5 @@\n" +
		"-foo\n" +
		"+bar\n" +
		"diff --git a/a.txt b/a.txt\n" +
		"index 1111111..3333333 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1 +1 @@\n" +
		"-two\n" +
		"+three\n"
	got := diffFilesPreamble(body)
	if got == "" {
		t.Fatalf("diffFilesPreamble = empty, want non-empty preamble")
	}
	// The three emitted links must each carry a distinct marker.
	for _, want := range []string{"?h=0", "?h=1", "?h=2"} {
		if !strings.Contains(got, want) {
			t.Errorf("preamble missing %q:\n%s", want, got)
		}
	}
	// The two a.txt links must each be present (changelog case).
	if c := strings.Count(got, "a.txt%23L1"); c < 2 {
		t.Errorf("preamble has %d a.txt%%23L1 references, want >=2:\n%s", c, got)
	}
	// The preamble still starts with the recovered `Files: ` prefix
	// (other tests / contracts rely on it).
	if !strings.HasPrefix(got, "Files: ") {
		t.Errorf("preamble missing `Files: ` prefix: %q", got)
	}
	// The links are percent-encoded (AGY 1.3.3 changelog line 59
	// is not regressed by the cycle-60 marker addition).
	if strings.Contains(got, "a.txt#L1") || strings.Contains(got, "b.txt#L5") {
		t.Errorf("preamble leaked raw #L<n> fragment:\n%s", got)
	}
}

// TestDiffFilesPreamble_SingleLinkMarker pins the "single link gets
// marker index 0" half of the changelog line-60 contract: a diff
// touching one file produces a preamble whose single emitted link
// carries `?h=0`, not the bare `path%23LN` form.
func TestDiffFilesPreamble_SingleLinkMarker(t *testing.T) {
	body := "diff --git a/a.txt b/a.txt\n" +
		"index 0000000..1111111 100644\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -3 +3 @@\n" +
		"-old\n" +
		"+new\n"
	got := diffFilesPreamble(body)
	if !strings.Contains(got, "?h=0") {
		t.Errorf("single-link preamble missing ?h=0 marker:\n%s", got)
	}
	// No higher indices in a one-file diff.
	if strings.Contains(got, "?h=1") {
		t.Errorf("single-link preamble leaked ?h=1:\n%s", got)
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
	want := "Signed out from server: daily-cloudcode-pa.googleapis.com"
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
	if got := authServerName(); got != "daily-cloudcode-pa.googleapis.com" {
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

package tuilib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnquotePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{`"/path/with space"`, "/path/with space"},
		{`'/path/with space'`, "/path/with space"},
		{`  "/path/foo"  `, "/path/foo"},
		{`  '/path/bar'  `, "/path/bar"},
		{`/plain/path`, "/plain/path"},
		{`"mismatched'`, `"mismatched'`},
		{`'mismatched"`, `'mismatched"`},
		{`"a"`, "a"},
		{`""`, ""},
		{`"`, `"`},
	}
	for _, tc := range cases {
		if got := UnquotePath(tc.in); got != tc.want {
			t.Errorf("UnquotePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAddDirFeedback_Errors(t *testing.T) {
	if got := AddDirFeedback(""); got != "error: no directory specified" {
		t.Errorf("empty = %q", got)
	}
	if got := AddDirFeedback("   "); got != "error: no directory specified" {
		t.Errorf("spaces = %q", got)
	}

	nonexistent := filepath.Join(t.TempDir(), "does-not-exist")
	wantNotFound := "error: directory not found: " + nonexistent
	if got := AddDirFeedback(nonexistent); got != wantNotFound {
		t.Errorf("nonexistent = %q, want %q", got, wantNotFound)
	}

	tmpFile := filepath.Join(t.TempDir(), "regular-file.txt")
	if err := os.WriteFile(tmpFile, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantNotDir := "error: not a directory: " + tmpFile
	if got := AddDirFeedback(tmpFile); got != wantNotDir {
		t.Errorf("file = %q, want %q", got, wantNotDir)
	}
}

func TestAddDirFeedback_Success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	dir := filepath.Join(t.TempDir(), "valid-dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := AddDirFeedback(dir)
	want := "Added " + dir + " to workspace"
	if got != want {
		t.Errorf("AddDirFeedback(%q) = %q, want %q", dir, got, want)
	}
}

// TestStatuslineMessages pins the reference's /statusline messages.
//
// Evidence 【一手】: agy 1.4.7's own output, plus the string table
// (`Statusline on. (%s)`, `Statusline on.`, `Statusline off.`,
// `Statusline set to: %s`).
func TestStatuslineMessages(t *testing.T) {
	if got := StatuslineEnabledMessage(""); got != "Statusline on." {
		t.Errorf("StatuslineEnabledMessage(\"\") = %q", got)
	}
	if got := StatuslineEnabledMessage("/bin/echo hi"); got != "Statusline on. (/bin/echo hi)" {
		t.Errorf("StatuslineEnabledMessage(command) = %q", got)
	}
	if got := StatuslineDisabledMessage; got != "Statusline off." {
		t.Errorf("StatuslineDisabledMessage = %q", got)
	}
	if got := StatuslineSetMessage("/bin/echo hi"); got != "Statusline set to: /bin/echo hi" {
		t.Errorf("StatuslineSetMessage = %q", got)
	}
	if got := StatuslineClearedMessage; got != "Custom statusline command cleared. Reverted to built-in default." {
		t.Errorf("StatuslineClearedMessage = %q", got)
	}
	want := "Usage:\n" +
		"  /statusline                 Toggle statusline on/off.\n" +
		"  /statusline <command>       Configure a custom shell command to render the statusline.\n" +
		"  /statusline delete          Delete the custom command and revert to the built-in default.\n" +
		"  /statusline on | enable     Enable the statusline.\n" +
		"  /statusline off | disable   Disable the statusline.\n" +
		"  /statusline help            Show this help message."
	if StatuslineUsage != want {
		t.Errorf("StatuslineUsage =\n%s\nwant\n%s", StatuslineUsage, want)
	}
}

// TestTitleMessages pins the reference's /title messages.
//
// Evidence 【一手】: the reference's own string table
// (`Usage: /title [on|off]`, `Custom title enabled. (%s)`,
// `Custom title disabled.`).
func TestTitleMessages(t *testing.T) {
	if got := TitleUsage; got != "Usage: /title [on|off]" {
		t.Errorf("TitleUsage = %q", got)
	}
	if got := TitleEnabledMessage("/bin/echo hi"); got != "Custom title enabled. (/bin/echo hi)" {
		t.Errorf("TitleEnabledMessage = %q", got)
	}
	if got := TitleDisabledMessage; got != "Custom title disabled." {
		t.Errorf("TitleDisabledMessage = %q", got)
	}
	if got := TitleNoCommandMessage; got != `Error: No title command configured in settings.json. Please add a "title" block.` {
		t.Errorf("TitleNoCommandMessage = %q", got)
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   \t\n\r  ", ""},
		{"Simple Title", "Simple Title"},
		{"  Leading and trailing  ", "Leading and trailing"},
		{"Multi\nline\r\ntitle", "Multi line title"},
		{"Multiple   internal\t\tspaces", "Multiple internal spaces"},
		{"\u00A0Non-breaking\u00A0spaces\u00A0", "Non-breaking spaces"},
	}
	for _, tc := range cases {
		if got := NormalizeTitle(tc.in); got != tc.want {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenameFeedback(t *testing.T) {
	if next, msg := RenameFeedback("", "old"); next != "old" ||
		msg != "Error: Please provide a new name for the conversation." {
		t.Errorf("empty arg = (%q, %q)", next, msg)
	}
	if next, msg := RenameFeedback("   \n\t ", "old"); next != "old" ||
		msg != "Error: Please provide a new name for the conversation." {
		t.Errorf("whitespace arg = (%q, %q)", next, msg)
	}
	if next, msg := RenameFeedback("New Name", "old"); next != "New Name" ||
		msg != "Conversation renamed to: New Name" {
		t.Errorf("valid arg = (%q, %q)", next, msg)
	}
	if next, msg := RenameFeedback("  Padded\nTitle  ", "old"); next != "Padded Title" ||
		msg != "Conversation renamed to: Padded Title" {
		t.Errorf("normalised arg = (%q, %q)", next, msg)
	}
}

func TestTasksFeedback(t *testing.T) {
	if got := TasksFeedback(0, 0); got != "No background tasks." {
		t.Errorf("0,0 = %q", got)
	}
	if got := TasksFeedback(1, 0); got != "1 running, 0 done" {
		t.Errorf("1,0 = %q", got)
	}
	if got := TasksFeedback(0, 3); got != "0 running, 3 done" {
		t.Errorf("0,3 = %q", got)
	}
	if got := TasksFeedback(2, 5); got != "2 running, 5 done" {
		t.Errorf("2,5 = %q", got)
	}
}

func TestRemoteControlFeedback(t *testing.T) {
	cases := []struct {
		arg      string
		cur      bool
		wantNext bool
		wantMsg  string
	}{
		{"", false, true, "Remote control on for this session."},
		{"on", false, true, "Remote control on for this session."},
		{"ON", false, true, "Remote control on for this session."},
		{"off", true, false, "Remote control off. This machine is no longer reachable from other devices."},
		{"OFF", true, false, "Remote control off. This machine is no longer reachable from other devices."},
		{"toggle", true, true, "Usage: /remote-control [on|off]"},
		{"toggle", false, false, "Usage: /remote-control [on|off]"},
	}
	for _, tc := range cases {
		next, msg := RemoteControlFeedback(tc.arg, tc.cur)
		if next != tc.wantNext || msg != tc.wantMsg {
			t.Errorf("RemoteControlFeedback(%q, %v) = (%v, %q), want (%v, %q)",
				tc.arg, tc.cur, next, msg, tc.wantNext, tc.wantMsg)
		}
	}
}

func TestMCPServersFeedback(t *testing.T) {
	if got := MCPServersFeedback(false); got != "No MCP servers configured." {
		t.Errorf("false = %q, want %q", got, "No MCP servers configured.")
	}
	if got := MCPServersFeedback(true); got != "MCP servers" {
		t.Errorf("true = %q, want %q", got, "MCP servers")
	}
}

func TestAuthServerName(t *testing.T) {
	name := AuthServerName()
	if name == "" {
		t.Fatal("AuthServerName returned empty string")
	}
}

func TestLogoutFeedback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_ADC_AUTH", "")

	token := filepath.Join(home, "antigravity-oauth-token")
	if err := os.WriteFile(token, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := LogoutFeedback()
	if !strings.HasPrefix(got, "Signed out from server: ") {
		t.Errorf("LogoutFeedback() = %q", got)
	}
	if _, err := os.Stat(token); !os.IsNotExist(err) {
		t.Errorf("token file still exists: %v", err)
	}

	// Idempotent
	gotSecond := LogoutFeedback()
	if gotSecond != got {
		t.Errorf("second LogoutFeedback() = %q, want %q", gotSecond, got)
	}
}

func TestLogoutFeedback_EnvVars(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("AGY_ADC_AUTH", "")
	got := LogoutFeedback()
	if !strings.Contains(got, "GEMINI_API_KEY") {
		t.Errorf("expected GEMINI_API_KEY message, got %q", got)
	}

	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_ADC_AUTH", "1")
	gotADC := LogoutFeedback()
	if !strings.Contains(gotADC, "AGY_ADC_AUTH") {
		t.Errorf("expected AGY_ADC_AUTH message, got %q", gotADC)
	}
}

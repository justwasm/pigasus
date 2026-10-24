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

func TestStatuslineFeedback(t *testing.T) {
	if next, msg := StatuslineFeedback(""); next != "off" || msg != "Statusline off. Run /statusline to re-enable." {
		t.Errorf("empty current = (%q, %q)", next, msg)
	}
	if next, msg := StatuslineFeedback("on"); next != "off" || msg != "Statusline off. Run /statusline to re-enable." {
		t.Errorf("on current = (%q, %q)", next, msg)
	}
	if next, msg := StatuslineFeedback("off"); next != "on" || msg != "Statusline on." {
		t.Errorf("off current = (%q, %q)", next, msg)
	}
}

func TestTitleFeedback(t *testing.T) {
	cases := []struct {
		arg       string
		cur       string
		titleText string
		wantNext  string
		wantMsg   string
	}{
		{"", "", "agy", "on", "Custom title enabled. (agy)"},
		{"", "on", "agy", "off", "Custom title disabled."},
		{"on", "", "agy", "on", "Custom title enabled. (agy)"},
		{"ON", "off", "agy", "on", "Custom title enabled. (agy)"},
		{"off", "on", "agy", "off", "Custom title disabled."},
		{"OFF", "on", "agy", "off", "Custom title disabled."},
		{"invalid", "cur", "agy", "cur", "Usage: /title [on|off]"},
	}
	for _, tc := range cases {
		next, msg := TitleFeedback(tc.arg, tc.cur, tc.titleText)
		if next != tc.wantNext || msg != tc.wantMsg {
			t.Errorf("TitleFeedback(%q, %q, %q) = (%q, %q), want (%q, %q)",
				tc.arg, tc.cur, tc.titleText, next, msg, tc.wantNext, tc.wantMsg)
		}
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

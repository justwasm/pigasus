// script_test.go — how the CLI runs a custom status-line / window-title
// script.
//
// Evidence:
//   - 【一手】 agy 1.4.7 ran the command through a shell: a command of
//     `~/bin/probe.sh | cat` rendered, which only works when the command
//     reaches a shell.
//   - 【一手】 the reference's title script printed
//     "TITLE-A\nTITLE-B\t\x1b[31mCOLOR\x07\n" and the window title became
//     "TITLE-A TITLE-B[31mCOLOR": the tab, the escape and the bell
//     characters are gone, the "[31m" survives as literal text, and the two
//     lines are joined by one space.
package statusline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/statusline"
)

// writeScript drops an executable script into a fresh directory.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.sh")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunScript_PayloadOnStdin checks that the payload reaches the script's
// stdin.
func TestRunScript_PayloadOnStdin(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\nread -r line\necho \"got: $line\"\n")
	res := statusline.RunScript(script, []byte(`{"cwd":"/tmp"}`))
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Output != `got: {"cwd":"/tmp"}` {
		t.Errorf("output = %q", res.Output)
	}
}

// TestRunScript_ThroughShell checks that the command reaches a shell, so
// pipes and redirections work.
//
// Evidence 【一手】: agy 1.4.7 rendered a command of `~/bin/probe.sh | cat`.
func TestRunScript_ThroughShell(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\ncat > /dev/null\necho piped\n")
	res := statusline.RunScript(script+` | cat`, []byte("payload"))
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Output != "piped" {
		t.Errorf("output = %q, want %q", res.Output, "piped")
	}
}

// TestRunScript_Failure pins the error text the reference's error card
// shows: "command failed: <err> (stderr: <text>)".
func TestRunScript_Failure(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\ncat > /dev/null\necho boom >&2\nexit 3\n")
	res := statusline.RunScript(script, []byte("payload"))
	if res.Err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(res.Err.Error(), "command failed:") {
		t.Errorf("error = %q", res.Err)
	}
	if !strings.Contains(res.Err.Error(), "exit status 3") {
		t.Errorf("error = %q", res.Err)
	}
	if !strings.Contains(res.Err.Error(), "stderr: boom") {
		t.Errorf("error = %q", res.Err)
	}
	if res.Output != "" {
		t.Errorf("output = %q, want empty", res.Output)
	}
}

// TestRunScript_NoOutput reports a script that printed nothing as an
// error: an empty row would leave the user with a blank status line.
func TestRunScript_NoOutput(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\ncat > /dev/null\n")
	res := statusline.RunScript(script, []byte("payload"))
	if res.Err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(res.Err.Error(), "no output") {
		t.Errorf("error = %q", res.Err)
	}
}

// TestRunScript_EmptyCommand reports an empty command as an error.
func TestRunScript_EmptyCommand(t *testing.T) {
	res := statusline.RunScript("   ", []byte("payload"))
	if res.Err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(res.Err.Error(), "empty") {
		t.Errorf("error = %q", res.Err)
	}
}

// TestRunScript_Timeout gives up on a script that runs too long.
//
// Evidence: the reference's own status line documentation — "stops any run
// that takes longer than 5 seconds".
func TestRunScript_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("timeout test is slow")
	}
	script := writeScript(t, "#!/bin/sh\ncat > /dev/null\nsleep 30\n")
	start := time.Now()
	res := statusline.RunScript(script, []byte("payload"))
	elapsed := time.Since(start)
	if res.Err == nil {
		t.Fatal("expected an error")
	}
	if elapsed > statusline.ScriptTimeout+3*time.Second {
		t.Errorf("run took %v, want about %v", elapsed, statusline.ScriptTimeout)
	}
}

// TestShellCommand pins the shell invocation.
func TestShellCommand(t *testing.T) {
	name, args := statusline.ShellCommand("/bin/echo hi")
	if name != "sh" {
		t.Errorf("name = %q, want sh", name)
	}
	if len(args) != 2 || args[0] != "-c" || args[1] != "/bin/echo hi" {
		t.Errorf("args = %v", args)
	}
	if name, args := statusline.ShellCommand("  "); name != "" || args != nil {
		t.Errorf("empty command = (%q, %v)", name, args)
	}
}

// TestExpandHome expands a leading "~".
func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := statusline.ExpandHome("~/bin/probe.sh"); got != filepath.Join(home, "bin/probe.sh") {
		t.Errorf("ExpandHome = %q", got)
	}
	if got := statusline.ExpandHome("/bin/echo"); got != "/bin/echo" {
		t.Errorf("ExpandHome = %q", got)
	}
	// A different user's home is not expanded.
	if got := statusline.ExpandHome("~other/bin/probe.sh"); got != "~other/bin/probe.sh" {
		t.Errorf("ExpandHome = %q", got)
	}
}

// TestSanitizeTitle pins the reference's title semantics.
//
// Evidence 【一手】: agy 1.4.7's title script printed
// "TITLE-A\nTITLE-B\t\x1b[31mCOLOR\x07\n" and the window title became
// "TITLE-A TITLE-B[31mCOLOR".
func TestSanitizeTitle(t *testing.T) {
	got := statusline.SanitizeTitle("TITLE-A\nTITLE-B\t\x1b[31mCOLOR\x07\n")
	want := "TITLE-A TITLE-B[31mCOLOR"
	if got != want {
		t.Errorf("SanitizeTitle = %q, want %q", got, want)
	}
	if got := statusline.SanitizeTitle("  spaced  \n\n  title  "); got != "spaced title" {
		t.Errorf("SanitizeTitle = %q", got)
	}
	if got := statusline.SanitizeTitle(""); got != "" {
		t.Errorf("SanitizeTitle = %q, want empty", got)
	}
}

// TestConstants pins the reference's timing and failure budget.
func TestConstants(t *testing.T) {
	if statusline.ScriptInterval != 300*time.Millisecond {
		t.Errorf("ScriptInterval = %v", statusline.ScriptInterval)
	}
	if statusline.ScriptTimeout != 5*time.Second {
		t.Errorf("ScriptTimeout = %v", statusline.ScriptTimeout)
	}
	if statusline.MaxConsecutiveFailures != 30 {
		t.Errorf("MaxConsecutiveFailures = %d", statusline.MaxConsecutiveFailures)
	}
	if statusline.DisabledMessage != "⚠ Statusline disabled after 30 consecutive failures." {
		t.Errorf("DisabledMessage = %q", statusline.DisabledMessage)
	}
}

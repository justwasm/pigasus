package statusline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// ScriptTimeout is how long a single script run may take before the
// reference gives up on it.
//
// Evidence: the reference's own status line documentation — "The CLI runs
// your script at most once every 300 ms and stops any run that takes
// longer than 5 seconds."
const ScriptTimeout = 5 * time.Second

// ScriptInterval is the shortest gap between two script runs.
//
// Evidence: the reference's own status line documentation — "The CLI runs
// your script at most once every 300 ms".
const ScriptInterval = 300 * time.Millisecond

// MaxConsecutiveFailures is how many failed runs in a row disable the
// status line for the rest of the session.
//
// Evidence: the reference's own status line documentation — "After 30
// failed runs in a row, it stops running the script for the rest of the
// session and shows `⚠ Statusline disabled after 30 consecutive
// failures.`"
const MaxConsecutiveFailures = 30

// ScriptWaitDelay is how long the runner waits for a killed script's pipes
// to close before it gives up on them.
//
// The command runs through a shell, so the process that owns the stdout
// pipe may be a grandchild of the process that was killed. Without this
// bound the wait would block for as long as the grandchild lives.
const ScriptWaitDelay = 500 * time.Millisecond

// DisabledMessage is what the reference shows once the status line has
// been switched off by too many consecutive failures.
const DisabledMessage = "⚠ Statusline disabled after 30 consecutive failures."

// Result is what RunScript returns.
type Result struct {
	// Output is the script's stdout, with the trailing newline removed.
	Output string
	// Err is set when the script failed, timed out or produced nothing.
	Err error
}

// SplitCommand splits the script command on whitespace.
//
// The reference runs the command through the shell, so this is only the
// token split the clone uses for its own bookkeeping — the command itself
// is handed to `sh -c` verbatim.
func SplitCommand(command string) (string, []string) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

// ExpandHome expands a leading "~" in the command to the user's home
// directory. A "~" that is not the whole first path element ("~user/...")
// is left alone.
//
// Evidence: the reference's own status line documentation — "a leading `~`
// in `command` expands to your home directory".
func ExpandHome(command string) string {
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, "~") {
		return command
	}
	if len(trimmed) > 1 && trimmed[1] != '/' && trimmed[1] != ' ' {
		// "~user/..." — a different user's home, which the reference does
		// not expand.
		return command
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return command
	}
	return filepath.Join(home, strings.TrimPrefix(trimmed[1:], "/"))
}

// ShellCommand returns the argv that runs command through the platform
// shell: `sh -c` on unix, `cmd /c` on Windows.
//
// Evidence: the reference's own status line documentation — "the CLI runs
// your command with `sh -c` (`cmd /c` on Windows)". Confirmed 【一手】
// against agy 1.4.7: a command of `~/bin/probe.sh | cat` renders, which
// only works when the command reaches a shell.
func ShellCommand(command string) (string, []string) {
	expanded := ExpandHome(command)
	if strings.TrimSpace(expanded) == "" {
		return "", nil
	}
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", expanded}
	}
	return "sh", []string{"-c", expanded}
}

// RunScript shells out the configured command through the shell, writes the
// payload to stdin, and returns the rendered stdout or an error.
//
// A failing script reports "command failed: <err> (stderr: <text>)",
// matching the reference's status line error card.
func RunScript(command string, payload []byte) Result {
	name, args := ShellCommand(command)
	if name == "" {
		return Result{Err: fmt.Errorf("status-line command is empty")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), ScriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	// The command runs through a shell, so killing the shell does not kill
	// its children — and a child that holds the stdout pipe open would
	// keep Wait blocked long past the timeout. WaitDelay bounds that: the
	// wait ends shortly after the context is cancelled even when the pipes
	// are still open.
	//
	// Evidence: the reference's own status line documentation — "stops any
	// run that takes longer than 5 seconds".
	cmd.WaitDelay = ScriptWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{Err: fmt.Errorf("stdin pipe: %w", err)}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{Err: fmt.Errorf("spawn %q: %w", name, err)}
	}

	writeErr := make(chan error, 1)
	go func() {
		_, werr := stdin.Write(payload)
		writeErr <- werr
		_ = stdin.Close()
	}()

	waitErr := cmd.Wait()
	// A failed payload write is not fatal: a script that never reads its
	// stdin closes the pipe early, and the command itself still succeeded.
	<-writeErr
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return Result{Err: fmt.Errorf("command failed: %s", waitErr.Error())}
		}
		return Result{Err: fmt.Errorf("command failed: %s (stderr: %s)", waitErr.Error(), msg)}
	}
	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		return Result{Err: fmt.Errorf("status-line script produced no output")}
	}
	return Result{Output: out}
}

// SanitizeTitle turns a title script's stdout into the window title.
//
// The reference removes control characters (tabs and the escape character
// included) without interpreting ANSI colour codes, and joins several
// lines with spaces.
//
// Evidence: the reference's own terminal title documentation. Confirmed
// 【一手】 against agy 1.4.7: a script printing "TITLE-A\nTITLE-B\t\x1b[31mCOLOR\x07\n"
// set the window title to "TITLE-A TITLE-B[31mCOLOR" — the tab and the
// escape and bell characters are gone, the "[31m" survives as literal
// text, and the two lines are joined by one space.
func SanitizeTitle(output string) string {
	lines := strings.Split(output, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned := stripControl(line)
		cleaned = strings.TrimSpace(cleaned)
		if cleaned == "" {
			continue
		}
		kept = append(kept, cleaned)
	}
	return strings.Join(kept, " ")
}

// stripControl removes every control character from s.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

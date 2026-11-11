package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCommand(t *testing.T) {
	dir := t.TempDir()
	r := &RunCommand{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.(string), "hello") {
		t.Errorf("got %q, want contains 'hello'", out)
	}
}

// Cycle 166: tokens are produced by POSIX word-splitting, not
// strings.Fields. A quoted argument must stay a single argv entry — the
// bug the old split had was that `ls "/tmp/My Documents"` became three
// args. `touch` makes the difference observable on the filesystem: with
// quoting, `touch "my file.txt"` creates exactly one file (one arg with
// an embedded space); with the old whitespace split, two files ("my"
// and "file.txt") would land.
//
// Cycle 276: the quoting is now done by the platform shell itself
// (`zsh -c` / `bash -c`), not by the clone's tokeniser — which is
// exactly what the reference does.
func TestRunCommandTokeniseQuotedArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("touch is not portable")
	}
	dir := t.TempDir()
	r := &RunCommand{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"touch \"my file.txt\""}`))
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "my file.txt")); err != nil {
		t.Errorf("expected \"my file.txt\" (single arg with embedded space), got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "my")); err == nil {
		t.Errorf("\"my\" should not exist — it would land only with strings.Fields")
	}
}

// Cycle 276: an unclosed quote is now the *shell's* error, returned as
// ordinary result text with a non-zero exit — not a clone-side parse
// error. That is what the reference does (一手, live 1.4.2 turn):
//
//	Command exited with code 1.
//
//	Command output:
//	zsh:1: unmatched "
//
// The wording is shell-specific (zsh says `unmatched "`, bash says
// `unexpected EOF while looking for matching '"'`), so only the exit
// code, the nil error and the presence of the quote are asserted.
func TestRunCommandUnclosedQuoteIsShellError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell quoting differs")
	}
	dir := t.TempDir()
	r := &RunCommand{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"echo \"unclosed"}`))
	if err != nil {
		t.Fatalf("a shell error must be data, not a Go error: %v", err)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "Command exited with code 1.") && !strings.Contains(s, "Command exited with code 2.") {
		t.Errorf("want exit code 1 (zsh) or 2 (bash) in result, got %q", s)
	}
	if !strings.Contains(s, `"`) {
		t.Errorf("want the shell's quote diagnostic in Output, got %q", s)
	}
}

// Cycle 166: empty / whitespace-only inputs still surface the
// documented "empty command" error. Behaviour matches the prior
// strings.Fields path so callers don't need to change.
func TestRunCommandEmpty(t *testing.T) {
	dir := t.TempDir()
	r := &RunCommand{Workspace: dir}
	for _, in := range []string{`""`, `"   "`, `"\t\n  "`} {
		_, err := r.Run(context.Background(), json.RawMessage(
			`{"command":`+in+`}`))
		if err == nil {
			t.Errorf("input %s: expected error, got nil", in)
		}
		if !strings.Contains(err.Error(), "empty command") {
			t.Errorf("input %s: err = %v, want it to mention 'empty command'", in, err)
		}
	}
}

// Cycle 276: run_command is a shell, so a pipeline must reach each stage.
// Before cycle 275 this was the exact `(Failed)` repro: `ls | head` handed
// `|` to ls as a literal argument and exited 2 with no output.
//
// Cycle 277: the Output section is CRLF-terminated, matching the reference.
func TestRunCommandPipeline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pipeline syntax")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"printf 'b\\na\\n' | sort | head -1"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "Command exited with code 0.") {
		t.Errorf("pipeline should succeed, got %q", s)
	}
	if !strings.Contains(s, "\nCommand output:\na\n") {
		t.Errorf("pipeline output should be `a` + LF, got %q", s)
	}
}

// Cycle 276: a non-zero exit is data. `sh -c 'exit 3'` must produce the
// reference's result text with code 3 and a nil Go error, so Dispatch keeps
// the output instead of replacing the row with `(Failed)`.
func TestRunCommandNonZeroExitIsData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX exit builtin")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"echo before; exit 3"}`))
	if err != nil {
		t.Fatalf("non-zero exit must not be a Go error: %v", err)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "Command exited with code 3.") {
		t.Errorf("want exit code 3 in result, got %q", s)
	}
	if !strings.Contains(s, "before") {
		t.Errorf("output before the exit must be kept, got %q", s)
	}
}

// Cycle 277: stdin is a pty (stdout/stderr stay pipes), matching the
// reference's split — `TTY0=/dev/ttys005`, `TTY1=not a tty`. Asserted through
// the shell itself so the check is what a real command would see.
func TestRunCommandStdinIsTTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"tty; [ -t 0 ] && echo ISTTY; [ -t 1 ] || echo PIPE1"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "/dev/tty") && !strings.Contains(s, "/dev/pts/") {
		t.Errorf("stdin should be a tty device, got %q", s)
	}
	if !strings.Contains(s, "ISTTY") {
		t.Errorf("[ -t 0 ] should be true, got %q", s)
	}
	if !strings.Contains(s, "PIPE1") {
		t.Errorf("[ -t 1 ] should be false (stdout is a pipe), got %q", s)
	}
}

// The result keeps the command's own line endings, and a CR is a line break
// (一手, live 1.4.4: `printf "a\r\nb\n"` -> `"a\n\nb\n"`). Cycle 277 had
// recorded a naive LF -> CRLF rewrite here; that string is byte-identical to
// the clone's own formatter, not to what the reference sends.
func TestRunCommandCRBecomesLF(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("printf / POSIX quoting")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"printf 'a\\r\\nb\\n'"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "\nCommand output:\na\n\nb\n") {
		t.Errorf("want `a`, a blank line, then `b`, got %q", s)
	}
}

// ANSI colour escapes are stripped before the result is built — exebox wraps
// the command's stdout/stderr in its (*stripANSI) writer. Live 1.4.4 probe:
// `printf "\033[31mRED\033[0m\n"` comes back as `RED\n`.
func TestRunCommandStripsANSI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("printf / POSIX quoting")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	out, err := r.Run(context.Background(), json.RawMessage(
		`{"command":"printf '\\033[31mRED\\033[0m\\n'"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s, _ := out.(string)
	if strings.ContainsRune(s, 0x1b) {
		t.Errorf("escape byte survived into the result: %q", s)
	}
	if !strings.Contains(s, "\nCommand output:\nRED\n") {
		t.Errorf("want the SGR-wrapped RED as bare RED, got %q", s)
	}
}

// Cycle 277: a command that reads stdin must not wedge the turn. The
// reference's pty belongs to a persistent terminal, so `read x` blocks there
// until WaitMsBeforeAsync hands the command to the background; the live probe
// ends at `DONE:eof` (x unset), exit 0. The clone is synchronous and ignores
// WaitMsBeforeAsync, so RunWithPTYStdin writes a VEOF instead — same result,
// no hang. See docs/cycles/cycle-277 for why that is a deliberate deviation.
func TestRunCommandStdinReadGetsEOFInsteadOfBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	type res struct {
		out any
		err error
	}
	done := make(chan res, 1)
	go func() {
		out, err := r.Run(context.Background(), json.RawMessage(
			`{"command":"read x; echo DONE:${x:-eof}"}`))
		done <- res{out, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Run: %v", got.err)
		}
		s, _ := got.out.(string)
		if !strings.Contains(s, "DONE:eof") {
			t.Errorf("want the reference's DONE:eof, got %q", s)
		}
		if !strings.Contains(s, "Command exited with code 0.") {
			t.Errorf("want exit 0, got %q", s)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("run_command blocked on stdin — the pty VEOF is not being delivered")
	}
}

// Cycle 277 regression guard for the pty's echo back-pressure. The pty echoes
// the clone's own VEOF back up to the master; if nothing reads the master,
// Darwin holds the child's exit for a fixed ~600 ms waiting for that output to
// drain — `sleep 1` measured 1.62 s undrained against 1.02 s drained, and
// `echo hello` 0.61 s against 0.01 s. RunWithPTYStdin drains the master; if
// that goroutine is ever removed, every command slower than a few ms silently
// gains half a second.
func TestRunCommandDoesNotStallOnPTYEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	r := &RunCommand{Workspace: t.TempDir()}
	start := time.Now()
	out, err := r.Run(context.Background(), json.RawMessage(`{"command":"sleep 1"}`))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.(string), "Command exited with code 0.") {
		t.Fatalf("want exit 0, got %q", out)
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("sleep 1 took %v; the ~600 ms pty echo stall is back (expected ~1.0s)", elapsed)
	}
}

// Cycle 276: the shell is the platform default, not $SHELL. The reference
// reports what terminal.GetDefaultShellName returns; on a host whose $SHELL
// is bash it still says "Shell: zsh." on macOS.
func TestDefaultShellNameIgnoresDollarSHELL(t *testing.T) {
	t.Setenv("SHELL", "/opt/homebrew/bin/bash")
	got := DefaultShellName()
	d := RunCommandDescription()
	if !strings.Contains(d, "Shell: "+got+".") {
		t.Errorf("description must advertise the executing shell %q, got %q", got, d)
	}
	if runtime.GOOS == "darwin" && got != "zsh" {
		t.Errorf("darwin DefaultShellName = %q, want zsh", got)
	}
	if runtime.GOOS == "linux" && got != "bash" {
		t.Errorf("linux DefaultShellName = %q, want bash", got)
	}
}

// Cycle 276: the argv the clone spawns is `zsh -c` / `bash -c` — not an
// in-process interpreter — so $0 and $- match the reference.
func TestDefaultShellArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX invocation shape")
	}
	argv := DefaultShellArgv("echo hi")
	if len(argv) != 3 || argv[1] != "-c" || argv[2] != "echo hi" {
		t.Fatalf("argv = %q, want [shell -c src]", argv)
	}
	if argv[0] != DefaultShellName() {
		t.Errorf("argv[0] = %q, want %q", argv[0], DefaultShellName())
	}
}

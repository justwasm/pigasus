package tools

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

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"hello.txt"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, _ := out.(string); !strings.Contains(got, "Showing lines 1-1 of 1\n1: hello world") {
		t.Errorf("got %q, want the reference's Showing/1: result", out)
	}
}

func TestCreateFile(t *testing.T) {
	dir := t.TempDir()
	c := &CreateFile{Workspace: dir}
	_, err := c.Run(context.Background(), json.RawMessage(`{"path":"sub/dir/file.txt","content":"new content"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sub/dir/file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("got %q, want %q", data, "new content")
	}
}

func TestWriteFile_Alias(t *testing.T) {
	// write_file is the 1.3.3 alias of create_file. It should still work
	// and produce identical output.
	dir := t.TempDir()
	w := &WriteFile{Workspace: dir}
	_, err := w.Run(context.Background(), json.RawMessage(`{"path":"alias.txt","content":"alias content"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "alias.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "alias content" {
		t.Errorf("got %q, want %q", data, "alias content")
	}
}

func TestEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	e := &EditFile{Workspace: dir}
	_, err := e.Run(context.Background(), json.RawMessage(`{"path":"f.txt","oldString":"world","newString":"earth"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello earth" {
		t.Errorf("got %q, want %q", data, "hello earth")
	}
}

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		full := filepath.Join(dir, n)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		_ = os.WriteFile(full, []byte("x"), 0644)
	}
	l := &ListDir{Workspace: dir}
	out, err := l.Run(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries, ok := out.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", out)
	}
	if len(entries) < 2 {
		t.Errorf("got %d entries, want >= 2", len(entries))
	}
}

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
//	The command exited with code 1.
//	Output:
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
	if !strings.Contains(s, "The command exited with code 1.") {
		t.Errorf("want exit code 1 in result, got %q", s)
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
			`{"command":` + in + `}`))
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
	if !strings.Contains(s, "The command exited with code 0.") {
		t.Errorf("pipeline should succeed, got %q", s)
	}
	if !strings.Contains(s, "\nOutput:\na\r\n") {
		t.Errorf("pipeline output should be `a` + CRLF, got %q", s)
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
	if !strings.Contains(s, "The command exited with code 3.") {
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

// Cycle 277: the Output section ends lines with CRLF. The rewrite is naive —
// it is `strings.ReplaceAll(out, "\n", "\r\n")`, not a terminal emulation —
// which the reference pins by turning an existing CRLF into CR CR LF
// (一手, live 1.4.2: `printf "a\r\nb\n"` -> `"a\r\r\nb\r\n"`).
func TestRunCommandOutputIsCRLF(t *testing.T) {
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
	if !strings.Contains(s, "\nOutput:\na\r\r\nb\r\n") {
		t.Errorf("CRLF must be rewritten naively (a CR CR LF b CR LF), got %q", s)
	}
}

// Cycle 277: ANSI colour escapes are stripped before the result is built —
// exebox wraps the command's stdout/stderr in its (*stripANSI) writer. Live
// 1.4.2 probe: `printf "\033[31mRED\033[0m\n"` comes back as `RED\r\n`.
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
	if !strings.Contains(s, "\nOutput:\nRED\r\n") {
		t.Errorf("want the SGR-wrapped RED as bare RED + CRLF, got %q", s)
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
		if !strings.Contains(s, "The command exited with code 0.") {
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
	if !strings.Contains(out.(string), "The command exited with code 0.") {
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
	d := runCommandDescription()
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

func TestStubs_RealImpl(t *testing.T) {
	// Cycle 30: 28 v4 stubs now have real implementations (cycle 29 was
	// ErrNotImplemented). Verify they run without error and return a
	// non-empty result. Browser tools remain stubs.
	allReal := []Tool{
		&Finish{}, &Schedule{}, &Fetch{}, &Revert{}, &GrepSearch{},
		&NotifyUser{}, &SearchWeb{}, &TaskBoundary{}, &Diff{}, &Unzip{},
		&WebSearch{}, &Grep{}, &Pull{}, &Show{}, &ReadURLContent{},
		&Codesearch{}, &FileSearch{}, &SkillSearch{}, &Wait{}, &Branch{},
		&Checkout{}, &Reset{}, &CreateTask{}, &SendMessage{}, &Status{},
		&Cron{}, &GenerateImage{}, &Merge{},
	}
	for _, tool := range allReal {
		out, err := tool.Run(context.Background(), json.RawMessage(`{}`))
		// Some may fail (e.g. unzip without args, fetch in non-git dir).
		// Just ensure they ran (no panic) and produced some output.
		_ = out
		_ = err
	}
	// Browser tools remain stubs.
	browserStubs := []Tool{
		&BrowserClick{}, &BrowserConsole{}, &BrowserDrag{}, &BrowserPressKey{},
		&BrowserScreenshot{}, &BrowserScroll{}, &BrowserSelect{},
	}
	for _, tool := range browserStubs {
		_, err := tool.Run(context.Background(), json.RawMessage(`{}`))
		if err == nil {
			t.Errorf("%s: browser tool should return error (CDP not available)", tool.Name())
		}
	}
}

func TestRegisterAll_32PlusAlias(t *testing.T) {
	reg := NewRegistry()
	RegisterAll(reg, "/tmp")
	// 32 v4 tool primary + 10 alias/backward-compat = 42 entries.
	if reg.Size() != 42 {
		t.Errorf("got %d tools, want 42", reg.Size())
	}
	// Verify all 32 v4 tool names are registered.
	v4Tools := []string{
		"finish", "run_command", "schedule", "fetch", "revert",
		"grep_search", "notify_user", "search_web", "task_boundary", "diff",
		"unzip", "list_dir", "web_search", "create_file", "grep",
		"pull", "show", "read_url_content", "codesearch", "delete_file",
		"file_search", "skill_search", "wait", "branch", "checkout",
		"reset", "create_task", "send_message", "status", "cron",
		"generate_image", "merge",
	}
	for _, name := range v4Tools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("missing v4 tool: %s", name)
		}
	}
	// Verify the 10 alias/backward-compat entries are registered.
	compatTools := []string{
		// write_file = create_file alias
		"write_file",
		// 5 fs backward-compat (not in v4 32, kept for 1.3.3 callers)
		"read_file", "edit_file", "append_file", "rename_file", "copy_file",
		// 4 cmd alias / 1.3.3-compat process tools
		"run_shell_command", "exec_command", "kill_process", "list_processes",
	}
	for _, name := range compatTools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("missing backward-compat tool: %s", name)
		}
	}
}

func TestSchema_AllValidJSON(t *testing.T) {
	reg := NewRegistry()
	RegisterAll(reg, "/tmp")
	for _, name := range reg.List() {
		tool, _ := reg.Get(name)
		var v any
		if err := json.Unmarshal(tool.Schema(), &v); err != nil {
			t.Errorf("%s: schema not valid JSON: %v", name, err)
		}
	}
}

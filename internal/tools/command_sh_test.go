// Tests for RunShellCommand, which after cycle 169 runs commands through
// github.com/mvdan.cc/sh/v3 (the same pure-Go shell interpreter the
// recovered binary embeds per out/local-macos/strings-mvdan.txt).
//
// The tests live in their own file (command_sh_test.go) because they
// need to spawn real sub-processes via the shell's exec handler; the
// tools_test.go file is mostly pure logic and would slow down with shell
// fixtures if they were bundled together.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runSH is a small helper that constructs a RunShellCommand with a
// workspace and invokes Run with the given shell source.
func runSH(t *testing.T, workspace, src string) (string, error) {
	t.Helper()
	r := &RunShellCommand{Workspace: workspace}
	raw, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: src})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := r.Run(context.Background(), raw)
	if err != nil {
		return "", err
	}
	s, _ := out.(string)
	return s, nil
}

// TestRunShellCommand_SimpleEcho confirms a single command runs and its
// stdout is captured verbatim.
func TestRunShellCommand_SimpleEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows shell fixtures are cmd-syntax specific; this test targets POSIX sh semantics")
	}
	got, err := runSH(t, t.TempDir(), `printf hello`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if got != "hello" {
		t.Errorf("output = %q, want %q", got, "hello")
	}
}

// TestRunShellCommand_StdoutAndStderrCombined confirms stderr is
// concatenated onto the returned string the same way CombinedOutput did
// under /bin/sh -c.
func TestRunShellCommand_StdoutAndStderrCombined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	//  printf writes to stdout; echo to stderr.
	got, err := runSH(t, t.TempDir(), `printf 'OUT'; printf 'ERR' 1>&2`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if got != "OUTERR" {
		t.Errorf("combined output = %q, want %q", got, "OUTERR")
	}
}

// TestRunShellCommand_Pipe confirms a shell pipeline runs end-to-end.
func TestRunShellCommand_Pipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	got, err := runSH(t, t.TempDir(), `printf 'banana' | tr 'a' 'X'`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if got != "bXnXnX" {
		t.Errorf("pipe output = %q, want %q", got, "bXnXnX")
	}
}

// TestRunShellCommand_Redirect confirms output redirection lands in
// a file in the workspace and the command's stdout is empty.
func TestRunShellCommand_Redirect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	dir := t.TempDir()
	got, err := runSH(t, dir, `printf 'hello-file' > out.txt`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if got != "" {
		t.Errorf("redirect stdout = %q, want empty", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("ReadFile(out.txt): %v", err)
	}
	if string(b) != "hello-file" {
		t.Errorf("file contents = %q, want %q", string(b), "hello-file")
	}
}

// TestRunShellCommand_ForLoop confirms a control-flow construct runs.
func TestRunShellCommand_ForLoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	got, err := runSH(t, t.TempDir(), `
for i in 1 2 3; do
  printf "%s " "$i"
done
`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if strings.TrimSpace(got) != "1 2 3" {
		t.Errorf("for-loop output = %q, want %q", got, "1 2 3")
	}
}

// TestRunShellCommand_VariableExpansion confirms $FOO is expanded.
func TestRunShellCommand_VariableExpansion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	got, err := runSH(t, t.TempDir(), `FOO=bar; printf "%s" "$FOO"`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if got != "bar" {
		t.Errorf("expansion = %q, want %q", got, "bar")
	}
}

// TestRunShellCommand_NonZeroExit confirms a failing command surfaces
// a non-nil error and the error message preserves the exit status.
func TestRunShellCommand_NonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	_, err := runSH(t, t.TempDir(), `exit 7`)
	if err == nil {
		t.Fatal("expected non-nil error from `exit 7`")
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("error message = %q, want it to mention status 7", err.Error())
	}
}

// TestRunShellCommand_ParseError confirms a syntax error in the source
// is reported with a parse error (mvdan/sh returns one from the parser
// stage) rather than being passed to a sub-shell that may silently
// reinterpret it.
func TestRunShellCommand_ParseError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	r := &RunShellCommand{Workspace: t.TempDir()}
	raw, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: `if then else fi fi`}) // mismatched fi
	_, err := r.Run(context.Background(), raw)
	if err == nil {
		t.Fatal("expected parse error for mismatched fi/fi")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error = %q, want substring 'parse'", err.Error())
	}
	// Belt-and-braces: ensure the error is not a context.Canceled.
	if errors.Is(err, context.Canceled) {
		t.Errorf("parse error should not be a cancellation: %v", err)
	}
}

// TestRunShellCommand_WorkspaceIsCwd confirms the runner honours
// interp.Dir and that file-relative operations land in the workspace.
func TestRunShellCommand_WorkspaceIsCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	dir := t.TempDir()
	// Create a known file inside the workspace.
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("present"), 0o644); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	got, err := runSH(t, dir, `cat marker`)
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if got != "present" {
		t.Errorf("workspace cwd = %q, want %q", got, "present")
	}
}

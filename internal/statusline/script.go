package statusline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Result is what RunScript returns.
type Result struct {
	Output string
	Err    error
}

// SplitCommand splits the script command on whitespace.
func SplitCommand(command string) (string, []string) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

// RunScript shells out the configured command, writes payload to stdin,
// and returns rendered stdout or error.
func RunScript(command string, payload []byte) Result {
	name, args := SplitCommand(command)
	if name == "" {
		return Result{Err: fmt.Errorf("status-line command is empty")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
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
	if werr := <-writeErr; werr != nil && waitErr == nil {
		waitErr = nil
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return Result{Err: fmt.Errorf("status-line script failed: %s", msg)}
	}
	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		return Result{Err: fmt.Errorf("status-line script produced no output")}
	}
	return Result{Output: out}
}

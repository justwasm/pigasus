// notebook.go — the notebook runner.
//
// Cycle 379: it lived in internal/app/tools/support.go next to the
// background-command table, although it executes a document and not a
// command. It belongs with the file tools that call it.

package filesystem

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ---- notebooks -------------------------------------------------------------

// RunNotebook executes a notebook with nbconvert and returns the executed
// notebook on stdout. Errors name the missing binary explicitly so the model
// can report it.
func RunNotebook(ctx context.Context, ws, absPath string) (any, error) {
	bin, err := exec.LookPath("jupyter")
	if err != nil {
		return nil, fmt.Errorf("notebook_execution: jupyter is not installed (%v)", err)
	}
	cmd := exec.CommandContext(ctx, bin, "nbconvert", "--to", "notebook", "--execute", "--stdout", absPath)
	cmd.Dir = ws
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("notebook_execution: %s", msg)
	}
	return out.String(), nil
}

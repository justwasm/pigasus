package agystep_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/agystep"
)

func TestAgystepFacade(t *testing.T) {
	res, ok := agystep.ParseRunCommandResult("Command exited with code 0.\n\nCommand output:\nhello\n")
	if !ok || res.ExitCode != 0 || res.Output != "hello\n" {
		t.Fatalf("unexpected ParseRunCommandResult: ok=%v res=%+v", ok, res)
	}
	if agystep.StatusDone.String() != "CORTEX_STEP_STATUS_DONE" {
		t.Fatalf("unexpected Status: %v", agystep.StatusDone)
	}
}

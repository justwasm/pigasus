package procgroup_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/procgroup"
)

func TestProcessGroup_Lifecycle(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	procgroup.Set(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := procgroup.Kill(cmd); err != nil {
		t.Errorf("procgroup.Kill: %v", err)
	}
	_ = cmd.Wait()
}

func TestProcessGroup_NilSafe(t *testing.T) {
	if err := procgroup.Kill(nil); err != nil {
		t.Errorf("Kill(nil): %v", err)
	}
	cmd := exec.Command("sleep", "1")
	if err := procgroup.Kill(cmd); err != nil {
		t.Errorf("Kill(unstarted): %v", err)
	}
}

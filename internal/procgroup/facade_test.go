package procgroup_test

import (
	"os/exec"
	"testing"

	"github.com/justwasm/pigasus/internal/procgroup"
)

func TestProcGroupFacade(t *testing.T) {
	cmd := exec.Command("true")
	procgroup.Set(cmd)
}

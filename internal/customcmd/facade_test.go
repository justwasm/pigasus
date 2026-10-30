package customcmd_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/customcmd"
)

func TestCustomCmdFacade(t *testing.T) {
	catalog, conflicts := customcmd.Load(customcmd.LoadRoots{})
	if catalog == nil && conflicts == nil {
		// ok
	}
}

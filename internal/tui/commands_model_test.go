package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/tui/commands"
)

// TestCommandsModel_OwnsDispatcher pins the ownership move: the one slash
// command table is held by CommandsModel, not the engine bucket.
func TestCommandsModel_OwnsDispatcher(t *testing.T) {
	var c CommandsModel
	if c.Dispatcher() != nil {
		t.Fatal("zero model has a dispatcher")
	}
	d := &commands.Dispatcher{}
	c.SetDispatcher(d)
	if c.Dispatcher() != d {
		t.Fatal("SetDispatcher did not stick")
	}
}

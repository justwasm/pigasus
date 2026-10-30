package customagent_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/customagent"
)

func TestCustomAgentFacade(t *testing.T) {
	agents, bad, err := customagent.LoadAgentDir(t.TempDir())
	if err != nil || len(agents) != 0 || len(bad) != 0 {
		t.Fatalf("unexpected load: agents=%v bad=%v err=%v", agents, bad, err)
	}
}

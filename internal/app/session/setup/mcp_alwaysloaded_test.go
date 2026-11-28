// mcp_alwaysloaded_test.go — the install path's always-loaded sweep.
//
// The sweep is the one place the clone reads `tools/list`, and it runs on
// the startup path (the TUI and print mode both call InstallMCPProvider
// before the first turn). The assertion here is the one that keeps a
// misbehaving server from turning a fast start into a five-minute wait:
// the sweep must return within its budget even when a server never answers.
package setup

import (
	"context"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/justwasm/pigasus/mcp"
)

// hangingTransport is a transport whose connection never completes the
// handshake, standing in for a server that is unreachable or wedged.
type hangingTransport struct{}

func (hangingTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (hangingTransport) String() string { return "hanging" }

// TestAlwaysLoadedSweepIsBounded pins that the startup sweep gives up
// rather than blocking on a server that never answers.
//
// A wedged server used to be free: the pool is lazy, so nothing connected
// until a tool call named it. The always-loaded sweep connects eagerly, so
// it needs a bound — and this test is what keeps the bound there.
func TestAlwaysLoadedSweepIsBounded(t *testing.T) {
	if alwaysLoadSweepBudget <= 0 {
		t.Fatalf("alwaysLoadSweepBudget = %v, want a positive bound", alwaysLoadSweepBudget)
	}
	if alwaysLoadSweepBudget > 30*time.Second {
		t.Errorf("alwaysLoadSweepBudget = %v; a startup path cannot wait that long", alwaysLoadSweepBudget)
	}

	pool := mcp.NewPool([]mcp.ServerSpec{{Name: "wedged", Type: "stdio", Enabled: true}})
	pool.SetTransportFactory(func(mcp.ServerSpec) (sdk.Transport, error) {
		return hangingTransport{}, nil
	})
	t.Cleanup(func() { _ = pool.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := pool.AlwaysLoadedTools(ctx)
	elapsed := time.Since(start)

	if len(got) != 0 {
		t.Errorf("AlwaysLoadedTools = %+v, want none from a wedged server", got)
	}
	if elapsed > 2*time.Second {
		t.Errorf("the sweep took %v; a wedged server must not hold it", elapsed)
	}
}

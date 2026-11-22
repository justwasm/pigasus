package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/tools"
)

type dummyTool struct {
	name   string
	desc   string
	output string
}

func (d *dummyTool) Name() string            { return d.name }
func (d *dummyTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (d *dummyTool) Description() string     { return d.desc }
func (d *dummyTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return d.output, nil
}

func TestRegistry_ConcurrentHotReload(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&dummyTool{name: "base_tool", output: "base_ok"})

	var stop atomicBool
	var wg sync.WaitGroup

	// Reader goroutines executing tool calls
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.get() {
				_, _ = reg.Dispatch(context.Background(), "base_tool", nil)
				_ = reg.List()
				_ = reg.Size()
				time.Sleep(100 * time.Microsecond)
			}
		}()
	}

	// Writer goroutine performing in-place hot reload
	for i := 0; i < 20; i++ {
		newReg := tools.NewRegistry()
		newReg.Register(&dummyTool{name: "base_tool", output: "base_ok"})
		for j := 0; j < 5; j++ {
			newReg.Register(&dummyTool{
				name:   fmt.Sprintf("plugin_tool_%d_%d", i, j),
				output: "plugin_ok",
			})
		}
		reg.ReplaceWith(newReg)
		time.Sleep(500 * time.Microsecond)
	}

	stop.set(true)
	wg.Wait()

	if reg.Size() != 6 {
		t.Fatalf("expected 6 tools after final reload, got %d", reg.Size())
	}
	out, err := reg.Dispatch(context.Background(), "plugin_tool_19_0", nil)
	if err != nil {
		t.Fatalf("dispatch newly reloaded tool failed: %v", err)
	}
	if out != "plugin_ok" {
		t.Errorf("expected 'plugin_ok', got %q", out)
	}
}

type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (b *atomicBool) get() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.v
}

func (b *atomicBool) set(val bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.v = val
}

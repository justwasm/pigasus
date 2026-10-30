package registry

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

type dummyEngine struct {
	name string
}

func (d *dummyEngine) Name() string { return d.name }
func (d *dummyEngine) Send(ctx context.Context, input TurnInput) (*TurnResult, error) {
	return &TurnResult{Response: "dummy"}, nil
}

type dummyTool struct {
	name string
}

func (d *dummyTool) Name() string { return d.name }
func (d *dummyTool) Decl() backend.ToolDecl {
	return backend.ToolDecl{Name: d.name, Description: "dummy"}
}
func (d *dummyTool) Run(ctx context.Context, args []byte) (string, error) {
	return "ok", nil
}

func TestRegistry(t *testing.T) {
	ClearRegistry()

	RegisterEngine("eng1", &dummyEngine{name: "eng1"})
	eng, ok := GetEngine("eng1")
	if !ok || eng.Name() != "eng1" {
		t.Fatalf("failed to get eng1")
	}

	def, err := DefaultEngine()
	if err != nil || def.Name() != "eng1" {
		t.Fatalf("unexpected default engine: %v, err: %v", def, err)
	}

	RegisterTool(&dummyTool{name: "tool1"})
	decls := AllToolDeclarations()
	if len(decls) != 1 || decls[0].Name != "tool1" {
		t.Fatalf("unexpected decls: %v", decls)
	}

	res, err := DispatchToolCall(context.Background(), backend.ToolCall{Name: "tool1"})
	if err != nil || res != "ok" {
		t.Fatalf("dispatch failed: res=%v, err=%v", res, err)
	}

	_, err = DispatchToolCall(context.Background(), backend.ToolCall{Name: "unknown"})
	if err == nil {
		t.Fatalf("expected unknown tool error")
	}
}

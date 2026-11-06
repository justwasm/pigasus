package agentturn

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/agentlink"
)

type mockDriverEngine struct{}

func (m *mockDriverEngine) Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error) {
	return nil, nil
}

func (m *mockDriverEngine) Close() error {
	return nil
}

func TestDriverRegistry(t *testing.T) {
	testDriver := Driver{
		Name:         "test-dummy-backend",
		Aliases:      []string{"dummy-alias"},
		Description:  "Dummy Test Backend",
		DefaultModel: "dummy-model",
		Spawn: func(ctx context.Context, opts SpawnOptions) (Engine, error) {
			return &mockDriverEngine{}, nil
		},
	}

	Register(testDriver)

	d, ok := Lookup("test-dummy-backend")
	if !ok {
		t.Fatal("expected test-dummy-backend to be found")
	}
	if d.Name != "test-dummy-backend" {
		t.Errorf("expected driver name test-dummy-backend, got %s", d.Name)
	}

	// Lookup via alias
	dAlias, ok := Lookup("dummy-alias")
	if !ok || dAlias.Name != "test-dummy-backend" {
		t.Fatal("expected dummy-alias to resolve to test-dummy-backend")
	}

	// Spawn
	eng, err := Spawn(context.Background(), "dummy-alias", SpawnOptions{})
	if err != nil || eng == nil {
		t.Fatalf("expected Spawn to succeed, got %v", err)
	}

	// Unknown backend
	_, err = Spawn(context.Background(), "non-existent-backend", SpawnOptions{})
	if err == nil {
		t.Fatal("expected error for non-existent backend")
	}
}

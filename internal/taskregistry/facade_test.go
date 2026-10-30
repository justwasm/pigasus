package taskregistry_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/taskregistry"
)

func TestTaskRegistryFacade(t *testing.T) {
	reg := taskregistry.New()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}

	id := reg.Register(&taskregistry.Task{
		Kind: taskregistry.KindShell,
		Name: "test task",
	})
	if id == "" {
		t.Fatal("expected non-empty task id")
	}

	task := reg.Get(id)
	if task == nil || task.Kind != taskregistry.KindShell {
		t.Fatalf("unexpected task: %v", task)
	}
}

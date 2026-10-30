package hooks

import (
	"context"
	"testing"
)

func TestFacadeHooks(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("expected non-nil Manager")
	}
	m.Register(PreSideQuestion, func(ctx context.Context, p Payload) (Decision, string, error) {
		return DecisionAllow, "", nil
	})
	reg := m.Registered()
	if len(reg) != 1 || reg[0] != PreSideQuestion {
		t.Fatalf("expected [PreSideQuestion], got %v", reg)
	}
	blocked, _, _ := m.Fire(context.Background(), PreSideQuestion, Payload{})
	if blocked {
		t.Fatal("expected blocked=false")
	}
	if m.Count(PreSideQuestion) != 1 {
		t.Fatalf("expected count 1, got %d", m.Count(PreSideQuestion))
	}
}

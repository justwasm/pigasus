package seam

import (
	"context"
	"testing"
)

type dummyHost struct{}

func (d *dummyHost) Ask(ctx context.Context, req Request) (Answer, error) {
	return Answer{Allowed: true}, nil
}

func TestHostContext(t *testing.T) {
	ctx := context.Background()
	if HostFrom(ctx) != nil {
		t.Fatal("expected nil host from empty context")
	}

	h := &dummyHost{}
	ctx2 := WithHost(ctx, h)
	if got := HostFrom(ctx2); got != h {
		t.Fatalf("expected %v, got %v", h, got)
	}

	ans, err := HostFrom(ctx2).Ask(ctx2, Request{Kind: KindPermission})
	if err != nil || !ans.Allowed {
		t.Fatalf("unexpected answer: %v, err: %v", ans, err)
	}
}

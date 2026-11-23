package backend

import (
	"context"
	"testing"
)

func TestContextWithMaxRetries(t *testing.T) {
	if n, ok := MaxRetriesFromContext(context.Background()); ok || n != 0 {
		t.Fatalf("expected unset context to return (0, false), got (%d, %v)", n, ok)
	}

	ctxZero := ContextWithMaxRetries(context.Background(), 0)
	if n, ok := MaxRetriesFromContext(ctxZero); !ok || n != 0 {
		t.Fatalf("expected zero override to return (0, true), got (%d, %v)", n, ok)
	}

	ctxTwo := ContextWithMaxRetries(context.Background(), 2)
	if n, ok := MaxRetriesFromContext(ctxTwo); !ok || n != 2 {
		t.Fatalf("expected override 2 to return (2, true), got (%d, %v)", n, ok)
	}

	ctxNeg := ContextWithMaxRetries(context.Background(), -5)
	if n, ok := MaxRetriesFromContext(ctxNeg); !ok || n != 0 {
		t.Fatalf("expected negative override to clamp to (0, true), got (%d, %v)", n, ok)
	}
}

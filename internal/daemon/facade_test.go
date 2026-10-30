package daemon

import (
	"testing"
	"time"
)

func TestFacadeDaemon(t *testing.T) {
	if DefaultGracePeriod != 5*time.Second {
		t.Fatalf("unexpected DefaultGracePeriod: %v", DefaultGracePeriod)
	}
	tr := New()
	if tr == nil {
		t.Fatal("expected non-nil Tracker")
	}
	g := Global()
	if g == nil {
		t.Fatal("expected non-nil Global Tracker")
	}
}

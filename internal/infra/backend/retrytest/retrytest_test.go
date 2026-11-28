package retrytest

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

func TestShortRestoresTheProductionSchedule(t *testing.T) {
	initial, maximum := retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval
	restore := Short()

	// The values Short installs are what make an unexpected retry cheap: two
	// attempts cost a few milliseconds, not the production 3s.
	if got := retry.InitialConnectionRetryInterval; got > 10*time.Millisecond {
		t.Errorf("InitialConnectionRetryInterval = %v, want a millisecond-scale value", got)
	}
	if got := retry.MaxConnectionRetryInterval; got > 50*time.Millisecond {
		t.Errorf("MaxConnectionRetryInterval = %v, want a millisecond-scale value", got)
	}
	if got := retry.NextConnectionRetryDelay(0); got > 10*time.Millisecond {
		t.Errorf("NextConnectionRetryDelay(0) = %v, want a millisecond-scale value", got)
	}

	restore()
	if retry.InitialConnectionRetryInterval != initial || retry.MaxConnectionRetryInterval != maximum {
		t.Fatalf("restore left %v/%v, want %v/%v",
			retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval, initial, maximum)
	}
}

func TestShortLeavesTheBudgetAlone(t *testing.T) {
	budget := retry.MaxConnectionRetries
	restore := Short()
	defer restore()
	if retry.MaxConnectionRetries != budget {
		t.Fatalf("MaxConnectionRetries = %d, want %d (the budget is the caller's to pin)", retry.MaxConnectionRetries, budget)
	}
}

func TestFastRestoresWhenTheTestEnds(t *testing.T) {
	initial, maximum := retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval

	t.Run("inner", func(t *testing.T) {
		Fast(t)
		if got := retry.InitialConnectionRetryInterval; got >= initial {
			t.Fatalf("InitialConnectionRetryInterval = %v, want it short", got)
		}
	})

	if retry.InitialConnectionRetryInterval != initial || retry.MaxConnectionRetryInterval != maximum {
		t.Fatalf("after the subtest %v/%v, want %v/%v",
			retry.InitialConnectionRetryInterval, retry.MaxConnectionRetryInterval, initial, maximum)
	}
}

func TestNoRetriesPinsTheBudgetToZero(t *testing.T) {
	n, ok := backend.MaxRetriesFromContext(NoRetries(context.Background()))
	if !ok || n != 0 {
		t.Fatalf("MaxRetriesFromContext = (%d, %v), want (0, true)", n, ok)
	}
}

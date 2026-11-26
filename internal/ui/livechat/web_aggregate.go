package livechat

import (
	"context"

	"github.com/justwasm/pigasus/accountpool"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/infra/livevoice"
)

// resetAggregateRouterForTest replaces or clears the shared aggregate router in unit tests.
func resetAggregateRouterForTest(r *accountrouting.Router) {
	livevoice.ResetSharedRouterForTest(r)
}

// effectiveAggregatePolicy returns an account-pool policy with routing,
// aggregate mode, and quota-error failover enabled.
func effectiveAggregatePolicy() *accountpool.Config {
	return livevoice.EffectivePolicy()
}

// aggregatePoolStatus summarizes the backend pool availability without exposing
// individual account credentials to the frontend.
func aggregatePoolStatus(ctx context.Context) (enabled bool, total int, healthy int) {
	return livevoice.PoolStatus(ctx, currentGeminiAPIKeyFn)
}

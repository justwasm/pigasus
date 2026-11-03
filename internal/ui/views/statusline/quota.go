// quota.go — the payload's quota buckets.
//
// The reference keys the map by the bucket id ("gemini-5h", "3p-weekly",
// …) and reports the remaining fraction, the reset time and the seconds
// until the reset.
//
// The map is the payload's own shape, so it is the package's: the host
// hands over the account's quota summary and the clock the seconds are
// counted against.
package statusline

import (
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
)

// QuotaBuckets maps the account's quota buckets onto the payload's shape.
//
// A summary without buckets produces no map at all: the payload's
// `quota` member is omitted when it is empty.
//
// The seconds until a reset are counted against now, so the caller
// decides which clock they are counted against.
func QuotaBuckets(summary *auth.QuotaSummary, now time.Time) map[string]QuotaBucket {
	if summary == nil {
		return nil
	}
	out := map[string]QuotaBucket{}
	for _, group := range summary.Groups {
		for _, bucket := range group.Buckets {
			entry := QuotaBucket{RemainingFraction: bucket.RemainingFraction}
			if !bucket.ResetTime.IsZero() {
				reset := bucket.ResetTime
				entry.ResetTime = &reset
				entry.ResetInSeconds = int(reset.Sub(now).Seconds())
			}
			out[bucket.ID] = entry
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

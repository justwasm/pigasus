// duration.go — the seconds-to-duration conversion the timeout-carrying
// tools share.
//
// Cycle 379: the helper lived with the command tools and was reached
// from the notebook tool as well. A conversion every timeout-carrying
// tool needs belongs to the contract.

package tools

import "time"

// SecondsToDuration converts a whole number of seconds into a duration.
func SecondsToDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}

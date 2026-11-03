package tuistore

import "sync/atomic"

// Tokens holds accumulated session token totals and last turn token counts.
type Tokens struct {
	SessionPromptTokens      int64
	SessionCompletionTokens  int64
	LastTurnCompletionTokens int64
	LastTurnPromptTokens     int64
	StatusLineTickArmed      bool
}

// ResetSessionTokens zeroes session token counters.
func (s *Store) ResetSessionTokens() {
	atomic.StoreInt64(&s.SessionPromptTokens, 0)
	atomic.StoreInt64(&s.SessionCompletionTokens, 0)
	atomic.StoreInt64(&s.LastTurnCompletionTokens, 0)
	atomic.StoreInt64(&s.LastTurnPromptTokens, 0)
}

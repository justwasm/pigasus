// observer.go — wire Budget into retry event stream.
package retrybudget

import (
	"time"
)

// AutoRetryStartEvent represents the start of an automated retry wait.
type AutoRetryStartEvent struct {
	DelayMs int
}

// AutoRetryEndEvent represents the end of an automated retry sequence.
type AutoRetryEndEvent struct {
	Success    bool
	Attempt    int
	FinalError string
}

// Aborter is the seam that decouples this package from the turn engine.
// AbortRetry() cancels the in-flight retry delay timer without cancelling the session itself.
type Aborter interface {
	AbortRetry()
}

// Observer returns an event observer that consults budget for every
// AutoRetryStartEvent and calls aborter.AbortRetry() when the proposed
// delay would push the cumulative wait past the budget cap.
func Observer(budget *Budget, aborter Aborter) func(any) {
	return func(ev any) {
		start, ok := ev.(AutoRetryStartEvent)
		if !ok {
			return
		}
		if budget == nil {
			return
		}
		if aborter == nil {
			panic("retrybudget: Observer wired with nil Aborter and non-nil Budget")
		}
		delay := time.Duration(start.DelayMs) * time.Millisecond
		if !budget.TryConsume(delay) {
			aborter.AbortRetry()
		}
	}
}

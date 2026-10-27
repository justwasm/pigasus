// observer.go — wire Budget into PiG's AutoRetryStartEvent stream.
//
// The Observer is the EventFunc (agent.AgentEvent consumer) the print path
// passes alongside the existing printCompactionObserver when calling
// pigrt.Runtime.RunWith. It recognises AutoRetryStartEvent, calls
// Budget.TryConsume(delayMs), and invokes Aborter.AbortRetry() when
// consumption would overflow the cap. All other event types pass through
// untouched so chaining with the compaction observer (and any future
// observers) works without coordination.
//
// The Observer is stateless apart from the borrowed Budget and Aborter
// references, so multiple Observer instances can share one Budget for
// read-only access; mutating the cap or the budget itself between turns
// is the caller's responsibility (Reset on Budget at the top of each turn).
package retrybudget

import (
	"time"

	"github.com/MichaelKinsy/PiG/agent"
)

// Aborter is the seam that decouples this package from PiG's concrete
// *coding.Session. PiG's Session.AbortRetry() cancels the in-flight
// retry delay timer without cancelling the session itself, which is
// exactly the half-step the cycle needs: the upcoming attempt does
// not start, but the error that triggered the retry propagates as
// the turn's final error — the AGY_ERROR envelope and exit code 3
// (cmd/wagyu/api_error.go) still fire.
//
// In production the Aborter is *pigrt.Runtime (which delegates to its
// embedded *coding.Session.AbortRetry). In tests it is a tiny stub
// that records the call — see observer_test.go.
type Aborter interface {
	AbortRetry()
}

// Observer returns an EventFunc that consults budget for every
// AutoRetryStartEvent and calls aborter.AbortRetry() when the proposed
// delay would push the cumulative wait past the budget cap.
//
// Passing a nil budget is permitted: the returned EventFunc is a
// no-op for AutoRetryStartEvent (and for every other event type) —
// callers that want to disable the cap for a specific runtime
// instance can leave Budget unset.
//
// Passing nil aborter alongside a non-nil budget causes the function to
// panic the first time it sees AutoRetryStartEvent. The intent is
// loud failure: a half-wired retry cap that silently swallows the
// overflow is worse than the bug it pretends to fix.
func Observer(budget *Budget, aborter Aborter) func(agent.AgentEvent) {
	return func(ev agent.AgentEvent) {
		start, ok := ev.(agent.AutoRetryStartEvent)
		if !ok {
			return
		}
		if budget == nil {
			return
		}
		if aborter == nil {
			// Loud failure: a half-wired retry cap is worse than the
			// bug it pretends to fix. The cycle retry-30s-cap-stays
			// spec rules out a silent no-op here.
			panic("retrybudget: Observer wired with nil Aborter and non-nil Budget")
		}
		delay := time.Duration(start.DelayMs) * time.Millisecond
		if !budget.TryConsume(delay) {
			aborter.AbortRetry()
		}
	}
}

package agentturn

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/core/apierror"
)

var (
	// ErrMaxTurnsReached is returned when the loop hits the maximum tool iterations cap.
	ErrMaxTurnsReached = actor.ErrMaxStepsReached
)

// ErrAutoContNetworkFailed reports that auto-continue was abandoned because the
// transport failed on every attempt.
type ErrAutoContNetworkFailed struct {
	Attempts int
	Last     error
}

func (e *ErrAutoContNetworkFailed) Error() string {
	return fmt.Sprintf("agent: network error after %d retries (last error: %v)", e.Attempts, e.Last)
}

func (e *ErrAutoContNetworkFailed) Unwrap() error { return e.Last }

// ErrAutoContMaxDepth reports that auto-continue was abandoned because
// AutoContMaxDepth was reached.
type ErrAutoContMaxDepth struct {
	Depth int
	Last  error
}

func (e *ErrAutoContMaxDepth) Error() string {
	return fmt.Sprintf("agent: auto-continue reached max depth (%d); user input required (last error: %v)", e.Depth, e.Last)
}

func (e *ErrAutoContMaxDepth) Unwrap() error { return e.Last }

// TurnErrorMessage classifies a failed turn into the message the
// transcript row carries.
//
// The text comes from classifyAPIError when the failure is a model-API error,
// so the TUI and the headless `error:` line agree on the reference's copy
// (including the auth-expired / OAuth-scope replacements). Everything else —
// a stream interrupt, a tool-budget exhaustion, a stall — keeps the loop's own
// message, which is the only description of what happened.
//
// The second value is false when there is no message at all — a nil
// error. The caller then renders no row: the row itself is the ui
// tier's business (toolsteps.ErrorRow), and this host only knows what
// the turn failed with.
func TurnErrorMessage(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	message := ""
	if api, ok := apierror.ClassifyAPIError(err); ok {
		message = api.Message
	} else if message = err.Error(); message == "" {
		message = "the turn failed"
	}
	// Auto-continue give-up wraps the last real cause; showing the wrapper
	// alone ("auto-continue reached max depth (3)") buries the reason the
	// user can act on. Keep the wrapper's sentence and append the cause.
	var network *ErrAutoContNetworkFailed
	var maxDepth *ErrAutoContMaxDepth
	switch {
	case errors.As(err, &network) && network.Last != nil:
		message = "Network error after " + strconv.Itoa(network.Attempts) + " retries: " + causeText(network.Last)
	case errors.As(err, &maxDepth) && maxDepth.Last != nil:
		message = "The turn stopped before it finished: " + causeText(maxDepth.Last)
	}
	return message, true
}

// causeText is TurnErrorMessage's helper for a wrapped cause: it prefers
// the reference's API copy over the raw transport string.
func causeText(err error) string {
	if api, ok := apierror.ClassifyAPIError(err); ok {
		return api.Message
	}
	if text := err.Error(); text != "" {
		return text
	}
	return "unknown error"
}

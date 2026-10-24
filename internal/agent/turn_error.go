package agent

import (
	"errors"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/apierror"
)

// TurnErrorRow renders the transcript row for a failed turn.
//
// The text comes from classifyAPIError when the failure is a model-API error,
// so the TUI and the headless `error:` line agree on the reference's copy
// (including the auth-expired / OAuth-scope replacements). Everything else —
// a stream interrupt, a tool-budget exhaustion, a stall — keeps the loop's own
// message, which is the only description of what happened.
func TurnErrorRow(err error) string {
	if err == nil {
		return ""
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
	return "Error: " + strings.TrimSpace(message)
}

// causeText is TurnErrorRow's helper for a wrapped cause: it prefers the
// reference's API copy over the raw transport string.
func causeText(err error) string {
	if api, ok := apierror.ClassifyAPIError(err); ok {
		return api.Message
	}
	if text := err.Error(); text != "" {
		return text
	}
	return "unknown error"
}

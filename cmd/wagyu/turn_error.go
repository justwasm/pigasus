// turn_error.go — cycle 279: make a failed TUI turn visible.
//
// Root cause it closes: cmd/wagyu/tui_run.go's streamPrompt called
// `_ = loop.Send(...)` and its turnDoneMsg carried no error, so any turn that
// did not complete (transport failure, stream interrupt after auto-continue
// gave up, iteration cap, stalled stream) ended with:
//
//   - the spinner cleared,
//   - the tool block re-rendered over the streamed range (m.replaceRange),
//   - no reply, no error line, no exit code — nothing.
//
// That is the reported "on a long task it suddenly can't proceed and ends up
// printing nothing". The reference always says something: it renders the API
// error with its own copy (`error: …` in headless, an inline error row in the
// TUI) and keeps the partial assistant text.
package main

import (
	"errors"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/agent"
)

// turnErrorRow renders the transcript row for a failed turn.
//
// The text comes from classifyAPIError when the failure is a model-API error,
// so the TUI and the headless `error:` line agree on the reference's copy
// (including the auth-expired / OAuth-scope replacements). Everything else —
// a stream interrupt, a tool-budget exhaustion, a stall — keeps the loop's own
// message, which is the only description of what happened.
func turnErrorRow(err error) string {
	if err == nil {
		return ""
	}
	message := ""
	if api, ok := classifyAPIError(err); ok {
		message = api.Message
	} else if message = err.Error(); message == "" {
		message = "the turn failed"
	}
	// Auto-continue give-up wraps the last real cause; showing the wrapper
	// alone ("auto-continue reached max depth (3)") buries the reason the
	// user can act on. Keep the wrapper's sentence and append the cause.
	var network *agent.ErrAutoContNetworkFailed
	var maxDepth *agent.ErrAutoContMaxDepth
	switch {
	case errors.As(err, &network) && network.Last != nil:
		message = "Network error after " + strconv.Itoa(network.Attempts) + " retries: " + causeText(network.Last)
	case errors.As(err, &maxDepth) && maxDepth.Last != nil:
		message = "The turn stopped before it finished: " + causeText(maxDepth.Last)
	}
	return "Error: " + strings.TrimSpace(message)
}

// causeText is turnErrorRow's helper for a wrapped cause: it prefers the
// reference's API copy over the raw transport string.
func causeText(err error) string {
	if api, ok := classifyAPIError(err); ok {
		return api.Message
	}
	if text := err.Error(); text != "" {
		return text
	}
	return "unknown error"
}

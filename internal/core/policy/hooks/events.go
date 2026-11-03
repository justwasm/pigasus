// Package hooks is the clone's hooks.json runtime.
//
// The Antigravity CLI loads hook documents from two places — the global
// one at ~/.gemini/config/hooks.json and the workspace one at
// <workspace>/.agents/hooks.json — and fires six events out of them:
// SessionStart, PreToolUse, PostToolUse, PreInvocation, PostInvocation
// and Stop.
//
// This package owns the vocabulary and the mechanics of that runtime:
//
//   - the six event names (events.go),
//   - the document shape and the loader (document.go, paths.go),
//   - the decision vocabulary a PreToolUse hook answers with
//     (decision.go),
//   - the execution of a handler's command and the aggregation of the
//     handlers' answers (runtime.go).
//
// The package is in the core tier: it imports nothing but the standard
// library, so the tiers above it — the plugin bus, the TUI, the hosts —
// can all reach it.
//
// Provenance: the official documentation (official-docs/hooks.md) names
// the two paths, the six events, the document shape, the handler fields
// (type, command, timeout), the 30-second default timeout, the JSON
// stdin/stdout contract and the six PreToolUse decisions.
package hooks

import "strings"

// Event is one of the six documented hook events.
//
// The names are the reference's own: they are the keys of a hook
// definition and the values a hook answers with.
type Event string

const (
	// EventSessionStart fires once when a new conversation session
	// starts, before the first model invocation.
	EventSessionStart Event = "SessionStart"
	// EventPreToolUse fires before a tool is executed. Its matcher
	// selects the tool.
	EventPreToolUse Event = "PreToolUse"
	// EventPostToolUse fires after a tool completes. Its matcher selects
	// the tool.
	EventPostToolUse Event = "PostToolUse"
	// EventPreInvocation fires before the model is called.
	EventPreInvocation Event = "PreInvocation"
	// EventPostInvocation fires immediately after each model invocation
	// completes.
	EventPostInvocation Event = "PostInvocation"
	// EventStop fires when the execution loop terminates.
	EventStop Event = "Stop"
)

// Events returns the six documented events, in the order the
// documentation lists them.
func Events() []Event {
	return []Event{
		EventSessionStart,
		EventPreToolUse,
		EventPostToolUse,
		EventPreInvocation,
		EventPostInvocation,
		EventStop,
	}
}

// EventNames returns the six event names as plain strings.
func EventNames() []string {
	events := Events()
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e))
	}
	return out
}

// ParseEvent reads an event name. The second value is false when the name
// is not one of the six.
func ParseEvent(s string) (Event, bool) {
	s = strings.TrimSpace(s)
	for _, e := range Events() {
		if string(e) == s {
			return e, true
		}
	}
	return "", false
}

// UsesMatcher reports whether the event's matcher selects a tool.
//
// The two tool events match on the tool's name; the four lifecycle
// events ignore their matcher.
func (e Event) UsesMatcher() bool {
	return e == EventPreToolUse || e == EventPostToolUse
}

// String returns the event's name.
func (e Event) String() string { return string(e) }

// at_subagent.go — cycle 231: `@<subagent> <message>` prompt routing.
//
// AGY 1.2.9 changelog line 177:
//
//	Added `@<subagent> <message>` prompt syntax to send a message
//	directly to a subagent conversation, with autocomplete listing
//	running and completed subagents.
//
// The clone's prior prompt-input layer (cycle 71 textarea + cycle 77
// slash router + cycle 230 /btw side-question card) treated every
// buffer that wasn't a slash command as a model prompt. The new
// routing here detects `@<name> <message>` *before* the chat-stream
// path and dispatches the message body to the named subagent's
// registered Sender (see internal/subagentrouter).
//
// State machine (per submit invocation):
//
//	input    ""                              -> chat path (no routing)
//	input    "hello"                         -> chat path
//	input    "/help"                         -> slash router (unchanged)
//	input    "@"                             -> chat path (no name yet)
//	input    "@explore-bot"                  -> chat path (no message yet)
//	input    "@explore-bot "                 -> error: empty message body
//	input    "@explore-bot find the auth"    -> route; name=explore-bot, msg=...
//	input    "@Explore-Bot ..."              -> route via case-insensitive lookup
//	input    "@ghost ..."                    -> error: no subagent "ghost" is running
//
// The parser is deliberately strict: any character outside
// letters / digits / `_` / `-` / `.` in the name slot disqualifies the
// buffer. Concretely `@file/foo.txt` does NOT match — `@file` +
// `@/` (slash) + `foo.txt` (name slots) — `@` is a chat affordance
// the reference introduced in 1.2.9; file-mentions remain a slash
// command (`/add-dir` etc.). The test in this cycle asserts that
// behaviour.
//
// Wiring:
//
//   - newRouter / DefaultRouter are the package-level state. NewRunMode
//     uses DefaultRouter so the same registry is shared across the
//     TUI session;
//   - parseAtSubagentPrompt is the pure parser exercised by unit
//     tests;
//   - routeAtSubagent runs on the runMode after Enter: it parses,
//     looks the name up, dispatches, and echoes the routed prompt + a
//     status line into m.response.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/subagentrouter"
)

// DefaultRouter is the named-subagent registry every new runMode
// shares. It is package-scoped (mirroring how `internal/tools` exposes
// `SharedTaskRegistry` / `SharedSubagentManager`) so a `@<name>`
// prompt reaches the same Sender regardless of which runMode was
// last constructed. Production wires adapters onto it in a future
// cycle; today the tests register a fake directly.
var DefaultRouter = subagentrouter.NewManager()

// atNameMaxLen caps the parser's name slot. A 64-rune upper bound is
// comfortable for human-supplied identifiers (e.g. "explore-bot-2")
// while still rejecting pathological buffers (`@xxxxxxxxxxxxxxxxxx
// <msg>` is a chat affordance, not a name).
const atNameMaxLen = 64

// parseAtSubagentPrompt recognises `@<name><whitespace><message>`.
//
// Returns (name, message, true) when buf is a routed prompt and
// ("", "", false) otherwise. The empty-name case is deliberately
// reported as !ok so a bare `@` falls through to the chat path while
// the user is still typing — they may yet type a name.
//
// Rules:
//
//   - buf must be at least 2 bytes and start with `@`
//   - the second byte onward is the name slot, which is the longest
//     run of letters / digits / `_` / `-` / `.`.
//   - the name slot terminates at the first whitespace (space,
//     tab, or newline) or end-of-buffer.
//   - the message slot is what follows the first whitespace,
//     trimmed of leading/trailing spaces and tabs.
//   - the parser only acknowledges the syntax when both a
//     whitespace AND a non-empty trimmed message are present;
//     "still typing" returns !ok, the chat path handles it.
//
// The name-character whitelist excludes `/` (so `@file/foo` does not
// match), `:` / `@` (so `@foo@bar` doesn't either), and any
// control rune. The reference uses the same set.
func parseAtSubagentPrompt(buf string) (name, message string, ok bool) {
	if len(buf) < 2 || buf[0] != '@' {
		return "", "", false
	}
	// Scan the name slot.
	end := -1
	for i := 1; i < len(buf); i++ {
		c := buf[i]
		if c == ' ' || c == '\t' || c == '\n' {
			end = i
			break
		}
		if !isAtNameRune(c) {
			// Anything other than letter/digit/_-/. is a hard
			// disqualifier. Lets `@file/foo` (file mention?) fall
			// through to the chat path unchanged.
			return "", "", false
		}
	}
	if end < 0 {
		// No whitespace yet — the user is still typing the message.
		return "", "", false
	}
	name = buf[1:end]
	if name == "" {
		return "", "", false
	}
	if len(name) > atNameMaxLen {
		return "", "", false
	}
	// Require a non-empty message body. The reference's `@<name>` with
	// nothing after it is "still typing"; with only whitespace after it
	// the reference prompts the user to add a message.
	rest := buf[end+1:]
	trimmed := trimSpace(rest)
	if trimmed == "" {
		// Caller distinguishes "still typing" from "empty message".
		// Returning the name with empty message lets the TUI render
		// the proper "empty message" line.
		return name, "", true
	}
	return name, trimmed, true
}

// isAtNameRune reports whether c may appear in the name slot of an
// `@<name> <message>` parser invocation. Whitespace terminates the
// name; control runes disqualify the buffer; everything else here is
// permitted.
func isAtNameRune(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-' || c == '.':
		return true
	}
	return false
}

// namedSubagentEmptyMessage is the user-visible error when the
// buffer is `@<name>` with no message body. The reference uses a
// similar "send a message to follow" hint; we keep ours short and
// factual so the TUI's slashRespond helper stamps it consistently.
func namedSubagentEmptyMessage(name string) string {
	return fmt.Sprintf("@%s has no message body. Type a message after the name.", name)
}

// namedSubagentUnknownMessage is the clear error the prompt layer
// echoes when `@<name>` does not resolve against the router. The
// reference surfaces this with the literal name and a hint to
// register the subagent first.
func namedSubagentUnknownMessage(name string) string {
	return fmt.Sprintf("No subagent named %q is running. Start one or check the name.", name)
}

// routeAtSubagent parses buf and either dispatches the message or
// emits one of two clear errors. Returns:
//
//   - (routed, true, nil) when the parse succeeded and dispatch
//     returned no error. routed is the resolved subagent entry;
//   - (nil, false, <typed error>) when the parse/dispatch path is
//     not taken — empty buffer, chat-style buffer, parse mismatch
//     (caller falls through to the standard chat submit);
//   - (nil, true, <typed error>) when the parse matched but
//     routing failed (unknown name or empty body). The caller
//     echoes namedSubagentEmptyMessage / namedSubagentUnknownMessage
//     and resets the input.
//
// The third return collapses both routing-side errors so the runMode
// can decide whether to surface "no message" / "no subagent" inline.
// handlePromptAtPrefix inspects it with errors.Is.
func routeAtSubagent(ctx context.Context, buf string, m *runMode) (*subagentrouter.Entry, bool, error) {
	name, message, ok := parseAtSubagentPrompt(buf)
	if !ok {
		return nil, false, nil
	}
	if message == "" {
		return nil, true, errAtEmptyBody
	}
	if m.atRouter == nil {
		m.atRouter = DefaultRouter
	}
	entry, lookupOK := m.atRouter.Lookup(name)
	if !lookupOK {
		return nil, true, errAtUnknownName
	}
	if err := entry.Sender.Send(ctx, message); err != nil {
		return nil, true, errAtDispatchFailed{cause: err}
	}
	return entry, true, nil
}

// errAtEmptyBody is the typed sentinel routeAtSubagent returns when
// the buffer is `@<name>` with no message body.
var errAtEmptyBody = errors.New("at-subagent: empty message body")

// errAtUnknownName is the typed sentinel routeAtSubagent returns
// when no subagent is registered under the parsed name.
var errAtUnknownName = errors.New("at-subagent: unknown name")

// errAtDispatchFailed is the typed wrapper returned when the
// underlying Sender.Send call fails. The wrapped error carries the
// original cause for tests and structured logs.
type errAtDispatchFailed struct{ cause error }

func (e errAtDispatchFailed) Error() string {
	return "at-subagent: dispatch failed: " + e.cause.Error()
}
func (e errAtDispatchFailed) Unwrap() error { return e.cause }

// handlePromptAtPrefix is the submitPrompt hook. It runs the parser
// against the buffer; on a routed prompt it dispatches, echoes the
// user message, and returns (true, nil). On parse mismatch it
// returns (false, nil) so the caller falls through to the existing
// slash / chat path. On routing-side errors (empty body, unknown
// name, dispatch failure) it appends an error row to m.response and
// returns (true, nil) — the prompt was understood, it just couldn't
// run.
//
// Returning a tea.Cmd is rare: dispatch is synchronous today
// (subagenttools's Invoke is), but a future PiG-backed wiring may go
// async, in which case the slot below hosts the tea.Cmd.
func (m *runMode) handlePromptAtPrefix(buf string) (bool, tea.Cmd) {
	if len(buf) == 0 || buf[0] != '@' {
		return false, nil
	}
	name, message, ok := parseAtSubagentPrompt(buf)
	if !ok {
		// bare "@" or "@<name>" with no whitespace yet — chat path
		return false, nil
	}
	// Common setup: reset input + history, echo the prompt, then
	// dispatch. Mirrors slashCommand flow but without the slash-
	// command registry.
	m.pushHistory(buf)
	m.input.Reset()
	m.streaming = false
	m.historyIdx = 0
	m.historyEdit = ""
	m.picker.Reset()

	if message == "" {
		// Parse matched, body is empty. Echo the parser-style row
		// the reference emits and reset the input.
		m.response = append(m.response, "", "> "+buf, "", namedSubagentEmptyMessage(name))
		m.saveSession()
		return true, nil
	}

	if m.atRouter == nil {
		m.atRouter = DefaultRouter
	}
	entry, ok := m.atRouter.Lookup(name)
	if !ok {
		m.response = append(m.response, "", "> "+buf, "", namedSubagentUnknownMessage(name))
		m.saveSession()
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), atDispatchTimeout)
	defer cancel()
	if err := entry.Sender.Send(ctx, message); err != nil {
		m.response = append(m.response,
			"",
			"> "+buf,
			"",
			strings.TrimSpace("at-subagent: failed to deliver: "+err.Error()),
		)
		m.saveSession()
		return true, nil
	}

	// Success: echo the routed prompt + a short "delivered" note so
	// the user can see what they sent and to which subagent. The
	// reply text, when the future PiG-backed Sender returns one,
	// will be appended by the Sender itself via the TUI's update
	// loop; today (fire-and-forget Senders) the row below is the
	// only visible feedback.
	m.response = append(m.response,
		"",
		cWarningBlueOpen+"> @"+entry.Name+" "+message+cWarningBlueClose,
		"",
		panelGray238+"delivered to \""+entry.Name+"\""+panelGray238C,
		"",
	)
	m.saveSession()
	return true, nil
}

// atDispatchTimeout caps one dispatch. Mirrors the print path's
// 2-minute default for unrelated turns but stays small so a stuck
// Sender does not freeze the prompt for long. A future cycle can
// make this a flag.
const atDispatchTimeout = 30 * 1000 * 1000 * 1000 // 30 seconds

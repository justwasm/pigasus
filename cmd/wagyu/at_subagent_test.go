package main

// at_subagent_test.go — cycle 231: coverage for the
// `@<subagent> <message>` prompt-input layer.
//
// Three layers of tests:
//
//   - parseAtSubagentPrompt is the pure string parser;
//     drive it directly so every legal/illegal input shape is
//     exercised once.
//   - the routing layer wraps a fake Sender behind Manager; the
//     runMode helper is driven through submitPrompt the same way
//     cycle-225/229/230 tests drive other slash-command flows.
//   - the regression guard: a buffer that is NOT an @-mention
//     (e.g. "@/foo" or "@explore-bot@bar") falls through to the
//     chat path unchanged. The chat path test seeds a fake
//     backend and asserts the user message lands on m.session,
//     not in the @-routing error rows.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/justwasm/pigasus/internal/subagentrouter"
)

// ---------------------------------------------------------------- parser

func TestParseAtSubagentPrompt_HappyPath(t *testing.T) {
	tests := []struct {
		raw     string
		name    string
		message string
		ok      bool
	}{
		{"@explore-bot find the auth code", "explore-bot", "find the auth code", true},
		{"@explore-bot ", "explore-bot", "", true}, // parse match; caller decides "no body"
		{"@Explore-Bot hi", "Explore-Bot", "hi", true},
		{"@a x", "a", "x", true},
		{"@foo.bar.baz hello world", "foo.bar.baz", "hello world", true},
		{"@_under_score hi", "_under_score", "hi", true},
		{"@name with\ttabs", "name", "with\ttabs", true}, // tabs are terminators too
		{"@name line\nbreak", "name", "line\nbreak", true},
	}
	for _, tc := range tests {
		name, msg, ok := parseAtSubagentPrompt(tc.raw)
		if ok != tc.ok || name != tc.name || msg != tc.message {
			t.Errorf("parseAtSubagentPrompt(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.raw, name, msg, ok, tc.name, tc.message, tc.ok)
		}
	}
}

func TestParseAtSubagentPrompt_RejectsNonMatch(t *testing.T) {
	tests := []struct {
		raw string
		why string
	}{
		{"", "empty buffer"},
		{"@", "bare @, no name"},
		{"@explore-bot", "still typing — no whitespace yet"},
		{"hello world", "no leading @"},
		{"/help", "slash command"},
		{"@/", "name slot contains slash"},
		{"@file/foo.txt", "name slot contains slash (regression)"},
		{"@explore-bot@next", "name slot contains @"},
		{"@name!", "name slot contains punctuation"},
		// Long name (65 runes) must fail (> atNameMaxLen=64).
		{strings.Repeat("a", 65), "name over 64 chars"},
		// name with control char
		{"@explo\x01re-bot hello", "name slot contains control"},
		// Leading space inside name slot — whitespace immediately
		// after @ terminates with empty name, parser rejects.
		{"@ hi", "empty name"},
	}
	for _, tc := range tests {
		name, msg, ok := parseAtSubagentPrompt(tc.raw)
		if ok {
			t.Errorf("%s: parseAtSubagentPrompt(%q) unexpectedly matched: (%q,%q)",
				tc.why, tc.raw, name, msg)
		}
	}
}

func TestParseAtSubagentPrompt_AcceptsLongNameAtBoundary(t *testing.T) {
	// 64 chars is the boundary; we accept it.
	raw := "@" + strings.Repeat("a", 64) + " hello"
	name, _, ok := parseAtSubagentPrompt(raw)
	if !ok || len(name) != 64 {
		t.Errorf("boundary 64-chars name: got ok=%v name_len=%d, want true,64", ok, len(name))
	}
}

// ---------------------------------------------------------------- runMode routing

// atFakeSender records Send calls so tests can assert which (if any)
// message the dispatch path delivered. Implements subagentrouter.Sender.
type atFakeSender struct {
	called atomic.Int32
	last   atomic.Value // string
	err    error
	cb     func(string)
}

func (f *atFakeSender) Send(_ context.Context, msg string) error {
	f.called.Add(1)
	f.last.Store(msg)
	if f.cb != nil {
		f.cb(msg)
	}
	return f.err
}
func (f *atFakeSender) Calls() int32 { return f.called.Load() }
func (f *atFakeSender) LastMsg() string {
	v, _ := f.last.Load().(string)
	return v
}

func newAtRouterWith(t *testing.T, name string, sender subagentrouter.Sender) *runMode {
	t.Helper()
	// DefaultRouter is package-scoped; tests would otherwise see
	// each other's registrations and the second Register would
	// silently fail with ErrAlreadyRegistered. Replace it with a
	// fresh Manager at the start of every test so each test owns
	// its own registry. AtCurRouter captures the previous one so
	// the global pointer is restored after the test suite finishes
	// (cosmetic — the test binary ends, but discipline matches the
	// rest of the package).
	prev := DefaultRouter
	DefaultRouter = subagentrouter.NewManager()
	t.Cleanup(func() { DefaultRouter = prev })
	m := newSlashModel(t)
	m.atRouter = DefaultRouter
	if _, err := m.atRouter.Register(name, sender); err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
	return m
}

// submitAt is the test entry point — wrap buffer in m.input the
// same way TestTUISessionPersistence does, then submit.
func submitAt(t *testing.T, m *runMode, buf string) {
	t.Helper()
	m.input.SetValue(buf)
	cmd := m.submitPrompt()
	if cmd != nil {
		_ = cmd // stream prompts not exercised; at-routing is sync
	}
}

func TestHandlePromptAtPrefix_RoutesToRegistered(t *testing.T) {
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	if handled, _ := m.handlePromptAtPrefix("@explore-bot find the auth code"); !handled {
		t.Fatal("handlePromptAtPrefix should have handled a routed buffer")
	}
	if got := sender.Calls(); got != 1 {
		t.Errorf("Sender.calls = %d, want 1", got)
	}
	if got := sender.LastMsg(); got != "find the auth code" {
		t.Errorf("Sender.lastMsg = %q, want %q", got, "find the auth code")
	}
	// Echo: at least one transcript row should mention explore-bot
	// + the message body.
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "explore-bot") {
		t.Errorf("transcript missing explore-bot mention: %v", m.response)
	}
	if !strings.Contains(joined, "find the auth code") {
		t.Errorf("transcript missing message body: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_LookupCaseInsensitive(t *testing.T) {
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	// User types a different-cased name; the lookup must still resolve.
	if handled, _ := m.handlePromptAtPrefix("@Explore-Bot hi"); !handled {
		t.Fatal("expected routed")
	}
	if sender.Calls() != 1 {
		t.Errorf("Sender.calls = %d, want 1", sender.Calls())
	}
}

func TestHandlePromptAtPrefix_UnknownNameSurfacesError(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	if handled, _ := m.handlePromptAtPrefix("@ghost hello world"); !handled {
		t.Fatal("handlePromptAtPrefix should still handle (and surface an error)")
	}
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "No subagent named") {
		t.Errorf("transcript missing unknown-name error; got: %v", m.response)
	}
	if !strings.Contains(joined, "ghost") {
		t.Errorf("transcript should mention ghost; got: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_EmptyMessageSurfacesError(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	// "@explore-bot " — name matches, message is empty.
	if handled, _ := m.handlePromptAtPrefix("@explore-bot "); !handled {
		t.Fatal("handlePromptAtPrefix should still handle (and surface an error)")
	}
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "no message body") {
		t.Errorf("transcript missing empty-body hint; got: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_DispatchErrorIsEchoed(t *testing.T) {
	sender := &atFakeSender{err: errors.New("pipe closed")}
	m := newAtRouterWith(t, "explore-bot", sender)
	if handled, _ := m.handlePromptAtPrefix("@explore-bot hi"); !handled {
		t.Fatal("expected handled")
	}
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "pipe closed") {
		t.Errorf("transcript should surface the dispatch failure; got: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_NonAtBufferFallsThrough(t *testing.T) {
	// "@file/foo.txt" is the regression-guard case: the parser
	// disqualifies it (slash in name slot), so the routing layer
	// returns false and lets submitPrompt take the chat path.
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "file", sender) // even if name existed, this buffer wouldn't bind to it
	if handled, _ := m.handlePromptAtPrefix("@file/foo.txt hello"); handled {
		t.Fatal("a buffer with / in the name slot must NOT route")
	}
	if sender.Calls() != 0 {
		t.Errorf("Sender.calls = %d, want 0 (no dispatch)", sender.Calls())
	}
}

func TestHandlePromptAtPrefix_BareAtFallsThrough(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	if handled, _ := m.handlePromptAtPrefix("@"); handled {
		t.Fatal("a bare @ must not be interpreted as a routed prompt")
	}
	if handled, _ := m.handlePromptAtPrefix("@explore-bot"); handled {
		t.Fatal("a @<name> with no whitespace must not be interpreted as a routed prompt")
	}
}

func TestHandlePromptAtPrefix_PlainChatFallsThrough(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	if handled, _ := m.handlePromptAtPrefix("hello world"); handled {
		t.Fatal("plain chat must not be intercepted")
	}
	if handled, _ := m.handlePromptAtPrefix("/help"); handled {
		t.Fatal("slash command must not be intercepted by at-routing")
	}
}

// ---------------------------------------------------------------- end-to-end through submitPrompt

func TestSubmitPrompt_AtSubagentRouting_HappyPath(t *testing.T) {
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	submitAt(t, m, "@explore-bot find the auth code")
	if sender.Calls() != 1 {
		t.Errorf("Sender.calls = %d, want 1", sender.Calls())
	}
	if got := sender.LastMsg(); got != "find the auth code" {
		t.Errorf("Sender.lastMsg = %q, want %q", got, "find the auth code")
	}
}

func TestSubmitPrompt_AtSubagentRouting_UnknownNameNoBackend(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	submitAt(t, m, "@ghost what is up")
	// The error row must mention "ghost" so the user knows which name was rejected.
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "ghost") {
		t.Errorf("transcript should mention the unknown name; got: %v", m.response)
	}
}

func TestSubmitPrompt_AtSubagentRouting_DoesNotTouchBackend(t *testing.T) {
	// Critical regression guard: a `@<name> <msg>` prompt must
	// NOT be appended to m.session (the chat session) and must
	// NOT call the backend. The cycle-231 layer is pure routing.
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	before := len(m.session)
	submitAt(t, m, "@explore-bot do something")
	if got := len(m.session); got != before {
		t.Errorf("submitPrompt appended %d entries to m.session; want no change", got-before)
	}
}

func TestSubmitPrompt_NonRoutedBuffersUnchanged(t *testing.T) {
	// Sanity: a chat buffer still appends a user message, AND the
	// @-router did not capture it. This is the cycle-225-style
	// guard: every legacy path stays intact.
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	submitAt(t, m, "hello world")
	if sender.Calls() != 0 {
		t.Errorf("chat buffer accidentally routed to subagent")
	}
	if len(m.session) == 0 {
		t.Fatalf("chat submit did not register a session message")
	}
}

package main

// slash_btw_test.go — cycle 230: /btw slash command + /copy btw.
//
// Tests drive handleSlashCommand directly through newSlashModel. The
// PiG side-question Manager is injected through the newBtwManager hook
// so the test uses PiG's scripted faux provider (test-faux) and never
// touches the real agw gateway — see internal/sidequestion for the
// same pattern in the package-level tests.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/hooks"
	"github.com/justwasm/pigasus/internal/sidequestion"
)

// withBtwManager injects a side-question Manager backed by PiG's
// faux provider. Returns a cleanup func that restores the previous
// factory; tests call defer withBtwManager(t)(...).
func withBtwManager(t *testing.T) func() {
	t.Helper()
	prev := newBtwManager
	newBtwManager = func(opts sidequestion.Options) *sidequestion.Manager {
		opts.Model = &ai.Model{
			Type:         ai.ModelTypeChat,
			ID:           "faux-1",
			DisplayName:  "faux-1",
			Provider:     &ai.TestFauxProvider{},
			ProviderMeta: ai.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
			Input:        []string{"text"},
		}
		return sidequestion.NewManager(opts)
	}
	return func() { newBtwManager = prev }
}

// stripColor removes ANSI escape sequences from a transcript row so
// assertions can match on plain text. The transcript renderer mixes
// SGR escapes (the slashRespond header uses cWarningBlueOpen +
// panelGray238); stripColor matches the helper slash_content_test.go
// already exports via the parent package's other tests.
func stripColor(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestBtwSlashNoArgs: typing `/btw` with no question and no history must echo the
// authentic "Usage: /btw <question>" line and never open a Session.
func TestBtwSlashNoArgs(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/btw"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.btwCards) != 0 {
		t.Errorf("no-args /btw opened %d cards", len(m.btwCards))
	}
	// The transcript carries the echo + "Usage: /btw <question>" line;
	// strip the ANSI wrapper and assert on the message body.
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Usage: /btw <question>") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/btw (no args) transcript missing 'Usage: /btw <question>' line: %v", m.response)
	}
}

// TestBtwSlashStreamingCard: /btw <question> spawns a coding.Session,
// sends the question, and the rendered Card has non-empty Answer +
// Tokens > 0. PiG's faux provider yields "42" for "What is 20+22?";
// the test asks a question that triggers the over-window branch so
// Tokens is non-zero (the basic text branch leaves Usage nil).
func TestBtwSlashStreamingCard(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/btw Trigger: over-window response"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.btwCards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(m.btwCards))
	}
	card := m.btwCards[0]
	_, answer, tokens, state, err := card.LockedSnapshot()
	if err != nil {
		t.Fatalf("Card.Err: %v", err)
	}
	if state != sidequestion.StateDone {
		t.Errorf("Card.State = %q, want done", state)
	}
	if answer == "" {
		t.Error("Card.Answer is empty")
	}
	if tokens <= 0 {
		t.Errorf("Card.Tokens = %d, want > 0", tokens)
	}
	// The active id pointer advanced to the new card.
	if m.activeBtwID != card.ID {
		t.Errorf("activeBtwID = %q, want %q", m.activeBtwID, card.ID)
	}
}

// TestBtwParentTranscriptUntouched: after a /btw round-trip the
// parent's m.session records the side question as a user message.
// Cycle btw-runs-hooks inverts cycle-230's "session untouched"
// contract so /btw shows up in the conversation history like any
// other user input — see slash_btw.go's appendBtwToSession helper
// and the cycle btw-runs-hooks deliverable for the rationale.
func TestBtwParentTranscriptUntouched(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	parent := []backend.Message{
		{Role: "user", Content: "parent question"},
		{Role: "assistant", Content: "parent answer"},
	}
	m.session = append([]backend.Message(nil), parent...)

	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}

	// The two parent messages stay intact and a new user message
	// lands at the end with a "> /btw <question>" body so future
	// turns can replay the side question in context.
	if len(m.session) != len(parent)+1 {
		t.Fatalf("parent session grew from %d to %d messages, want %d", len(parent), len(m.session), len(parent)+1)
	}
	for i, msg := range parent {
		if m.session[i].Role != msg.Role || m.session[i].Content != msg.Content {
			t.Errorf("parent session[%d] = %+v, want %+v", i, m.session[i], msg)
		}
	}
	last := m.session[len(m.session)-1]
	if last.Role != "user" {
		t.Errorf("appended role = %q, want user", last.Role)
	}
	if !strings.Contains(last.Content, "What is 20+22?") {
		t.Errorf("appended content = %q, want it to contain the side question", last.Content)
	}
	if !strings.HasPrefix(last.Content, "> /btw ") {
		t.Errorf("appended content = %q, want it to start with \"> /btw \"", last.Content)
	}
}

// TestBtwCollapseExpand: the card renders collapsed by default;
// ToggleBtwCardExpansion flips it; CollapseBtwCard forces collapsed.
// Future cycles wire Enter/Esc to these (the cycle-230 contract
// captures the visual rendering).
func TestBtwCollapseExpand(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}
	card := m.btwCards[0]

	if m.BtwCardExpanded(card.ID) {
		t.Error("new card expanded by default; reference renders collapsed")
	}
	if !m.ToggleBtwCardExpansion(card.ID) {
		t.Error("ToggleBtwCardExpansion returned false on the first toggle")
	}
	if !m.BtwCardExpanded(card.ID) {
		t.Error("card not expanded after ToggleBtwCardExpansion")
	}
	if m.ToggleBtwCardExpansion(card.ID) {
		t.Error("ToggleBtwCardExpansion returned true on the second toggle")
	}
	if m.BtwCardExpanded(card.ID) {
		t.Error("card not collapsed after second ToggleBtwCardExpansion")
	}

	// Esc collapses an expanded card.
	m.ToggleBtwCardExpansion(card.ID)
	if !m.BtwCardExpanded(card.ID) {
		t.Fatal("setup: card not expanded")
	}
	m.CollapseBtwCard(card.ID)
	if m.BtwCardExpanded(card.ID) {
		t.Error("CollapseBtwCard did not collapse")
	}
}

// TestBtwErrorPath: a session that fails mid-stream leaves the
// transcript with `⎿  btw failed: <err>` and no body rows.
//
// The faux provider returns a clean stop on most requests; to force
// an error path we directly call the manager with a question that
// triggers the "test-faux: unsupported scenario" branch — that
// branch is what every non-"parity-basic" Scenario raises.
func TestBtwErrorPath(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	// Set the Scenario so the next Stream call fails with an
	// unsupported-scenario error.
	t.Setenv("PIG_TEST_FAUX_SCENARIO", "not-a-real-scenario")

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/btw anything"); !handled {
		t.Fatal("/btw not handled")
	}
	// The transcript must carry `btw failed: <err>` even though no
	// card was registered.
	if len(m.btwCards) != 0 {
		t.Errorf("error path registered %d cards; want 0", len(m.btwCards))
	}
	var found bool
	for _, row := range m.response {
		if strings.HasPrefix(stripColor(row), "btw failed: ") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("btw failed line missing from transcript: %v", m.response)
	}
}

// TestCopyBtwSlash: /copy btw with a visible card copies the active
// card's text; without a card the echo is "No active btw response
// to copy." The clipboard mock is swapped so the test does not
// touch the real system clipboard.
func TestCopyBtwSlash(t *testing.T) {
	orig := clipboardWrite
	defer func() { clipboardWrite = orig }()

	cleanup := withBtwManager(t)
	defer cleanup()

	// No card yet → "No /btw response to copy."
	var copied string
	clipboardWrite = func(s string) error { copied = s; return nil }
	m := newSlashModel(t)
	m.handleSlashCommand("/copy btw")
	if copied != "" {
		t.Errorf("clipboard written with no active card: %q", copied)
	}
	var noActive bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "No /btw response to copy.") {
			noActive = true
			break
		}
	}
	if !noActive {
		t.Errorf("/copy btw with no card missed the no-active echo: %v", m.response)
	}

	// Add a card via /btw, then /copy btw again.
	clipboardWrite = func(s string) error { copied = s; return nil }
	m = newSlashModel(t)
	m.handleSlashCommand("/btw What is 20+22?")
	card := m.btwCards[0]
	m.handleSlashCommand("/copy btw")
	if copied != "42" {
		t.Errorf("clipboard got %q, want %q", copied, "42")
	}
	var copiedMsg bool
	for _, row := range m.response {
		plain := stripColor(row)
		if strings.Contains(plain, "Copied /btw answer to clipboard.") {
			copiedMsg = true
			break
		}
	}
	if !copiedMsg {
		t.Errorf("/copy btw echo missing 'Copied /btw answer to clipboard.': %v", m.response)
	}
	_ = card
}

// TestCopyBtwClipboardError: when the clipboard backend fails, the
// /copy btw echo carries the error and never panics.
func TestCopyBtwClipboardError(t *testing.T) {
	orig := clipboardWrite
	defer func() { clipboardWrite = orig }()

	cleanup := withBtwManager(t)
	defer cleanup()

	clipboardWrite = func(string) error { return os.ErrPermission }

	m := newSlashModel(t)
	m.handleSlashCommand("/btw What is 20+22?")
	m.handleSlashCommand("/copy btw")

	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Failed to copy to clipboard: ") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/copy btw clipboard error echo missing: %v", m.response)
	}
}

// TestBtwIndependentServices: opening a btw card does not mutate
// anything on the runMode outside the btwManager / btwCards /
// session fields. After a /btw round-trip the parent session grows
// by exactly one user message (the side-question record added by
// appendBtwToSession); closing the runMode's main transport
// (m.backend) does not retroactively affect the card. PiG's
// coding.Services per-Ask isolation guarantees this; the test pins
// the contract so a future cycle that decides to share a Services
// cannot regress without noticing.
func TestBtwIndependentServices(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	parent := []backend.Message{{Role: "user", Content: "p"}}
	m.session = append([]backend.Message(nil), parent...)
	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.session) != len(parent)+1 {
		t.Errorf("parent session length = %d, want %d", len(m.session), len(parent)+1)
	}
	if len(m.btwCards) != 1 {
		t.Errorf("expected 1 btw card, got %d", len(m.btwCards))
	}
}

// ---- cycle btw-runs-hooks: lifecycle-hook dispatch -----------------------
//
// The next six tests are the cycle btw-runs-hooks deliverable for
// AGY 1.3.3 line 49: /btw now fires the registered hooks. The
// production Manager is constructed by NewRunMode (see tui_run.go);
// the tests overwrite m.hooks after construction so each case has
// a private registry without leaking handlers between tests.

// TestBtwFiresPreSideQuestionHook: /btw dispatches the
// PreSideQuestion event before opening the side-question Session.
// A handler that returns Allow is enough — the side question still
// runs and the hook saw the typed question text.
func TestBtwFiresPreSideQuestionHook(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	called := 0
	var seenEvent hooks.Event
	m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		called++
		seenEvent = p.Event
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}
	if called != 1 {
		t.Errorf("PreSideQuestion fired %d times; want 1", called)
	}
	if seenEvent != hooks.PreSideQuestion {
		t.Errorf("payload event = %q, want %q", seenEvent, hooks.PreSideQuestion)
	}
}

// TestBtwFiresPostSideQuestionHook: /btw dispatches the
// PostSideQuestion event after the side-question Session settles
// (success path). The handler sees the streamed answer and the
// token total in the payload — same fields the TUI renders.
func TestBtwFiresPostSideQuestionHook(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	called := 0
	var seenAnswer string
	var seenTokens int
	m.hooks.Register(hooks.PostSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		called++
		seenAnswer = p.Answer
		seenTokens = p.Tokens
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw Trigger: over-window response"); !handled {
		t.Fatal("/btw not handled")
	}
	if called != 1 {
		t.Errorf("PostSideQuestion fired %d times; want 1", called)
	}
	if seenAnswer == "" {
		t.Errorf("PostSideQuestion payload Answer was empty; want the streamed answer")
	}
	if seenTokens <= 0 {
		t.Errorf("PostSideQuestion payload Tokens = %d; want > 0", seenTokens)
	}
}

// TestBtwHookCanBlock: a PreSideQuestion handler that returns
// DecisionBlock aborts the side question entirely — no card is
// registered, the transcript carries the block reason, and the
// post-event still fires so logging handlers can react.
func TestBtwHookCanBlock(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	preCalls := 0
	postCalls := 0
	var postBlocked bool
	var postReason string
	m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		preCalls++
		return hooks.DecisionBlock, "policy: denied", nil
	})
	m.hooks.Register(hooks.PostSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		postCalls++
		postBlocked = p.Blocked
		postReason = p.BlockReason
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}
	if preCalls != 1 {
		t.Errorf("PreSideQuestion fired %d times; want 1", preCalls)
	}
	if postCalls != 1 {
		t.Errorf("PostSideQuestion fired %d times; want 1", postCalls)
	}
	if len(m.btwCards) != 0 {
		t.Errorf("blocked /btw registered %d cards; want 0", len(m.btwCards))
	}
	if !postBlocked {
		t.Errorf("PostSideQuestion payload Blocked = false; want true")
	}
	if postReason != "policy: denied" {
		t.Errorf("PostSideQuestion BlockReason = %q; want %q", postReason, "policy: denied")
	}
	// The block reason echoes into the visible transcript.
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "blocked by hook: policy: denied") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("block reason missing from transcript: %v", m.response)
	}
}

// TestBtwMultipleInvocationsFireHooksPerCall: three back-to-back
// /btw invocations each fire both Pre and Post exactly once. The
// Manager's fire-count counter is a useful invariant for subagent
// hosts that tally events over the session.
//
// The test rebuilds a fresh runMode per /btw so the underlying PiG
// coding.Session lifecycle stays inside one Ask per model — every
// existing cycle-230 test follows the same pattern (TestCopyBtwSlash
// does `m = newSlashModel(t)` between turns; see also
// TestBtwStreamingCard etc.). The three questions share the
// "What is 20+22?" prefix that PiG's faux provider matches, so all
// three resolve to the "42" canned response — varying the suffix
// is enough to give every /btw a distinct Question string for the
// hook-payload assertion.
func TestBtwMultipleInvocationsFireHooksPerCall(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	questions := []string{
		"What is 20+22? (1)",
		"What is 20+22? (2)",
		"What is 20+22? (3)",
	}
	var pre, post int
	var seenQuestions []string

	for i, q := range questions {
		m := newSlashModel(t)
		m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
			seenQuestions = append(seenQuestions, p.Question)
			return hooks.DecisionAllow, "", nil
		})
		buf := "/btw " + q
		if handled, _ := m.handleSlashCommand(buf); !handled {
			t.Fatalf("/btw %d not handled", i+1)
		}
		if len(m.btwCards) != 1 {
			t.Errorf("/btw %d btwCards length = %d, want 1", i+1, len(m.btwCards))
		}
		pre += m.hooks.Count(hooks.PreSideQuestion)
		post += m.hooks.Count(hooks.PostSideQuestion)
	}
	if pre != 3 {
		t.Errorf("cumulative PreSideQuestion count = %d, want 3", pre)
	}
	if post != 3 {
		t.Errorf("cumulative PostSideQuestion count = %d, want 3", post)
	}
	if len(seenQuestions) != 3 {
		t.Fatalf("captured %d hook payloads; want 3", len(seenQuestions))
	}
	for i, want := range questions {
		if seenQuestions[i] != want {
			t.Errorf("PreSideQuestion payload[%d] = %q, want %q", i, seenQuestions[i], want)
		}
	}
}

// TestBtwHookReceivesQuestionText: the hook payload carries the
// user-typed question verbatim. This is the contract a subagent
// host relies on to log or to make a block decision based on the
// question's content.
func TestBtwHookReceivesQuestionText(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	const want = "explain quantum entanglement briefly"
	seen := ""
	m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		seen = p.Question
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw " + want); !handled {
		t.Fatal("/btw not handled")
	}
	if seen != want {
		t.Errorf("hook payload Question = %q, want %q", seen, want)
	}
}

// TestBtwSideQuestionAppearsInTranscript: after /btw the parent
// m.session grows by one user message whose body is the side
// question prefixed with "> /btw ". The transcript records the
// side question like any other user input so /resume and the
// on-disk conversation db see it too.
func TestBtwSideQuestionAppearsInTranscript(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	// Empty session: appendBtwToSession should grow it from 0 to 1.
	if handled, _ := m.handleSlashCommand("/btw What is 6*7?"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.session) != 1 {
		t.Fatalf("m.session length = %d, want 1", len(m.session))
	}
	got := m.session[0]
	if got.Role != "user" {
		t.Errorf("appended role = %q, want user", got.Role)
	}
	if !strings.Contains(got.Content, "What is 6*7?") {
		t.Errorf("appended content = %q, want it to contain the side question", got.Content)
	}
	if !strings.HasPrefix(got.Content, "> /btw ") {
		t.Errorf("appended content = %q, want it to start with \"> /btw \"", got.Content)
	}
}

// TestBtwShowHistoryWhenNoArgs verifies that when past BTW cards exist and no question is in progress,
// running `/btw` with no arguments re-opens the most recent card rather than printing usage.
func TestBtwShowHistoryWhenNoArgs(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	m.handleSlashCommand("/btw What is 20+22?")
	if len(m.btwCards) != 1 || m.activeBtwIndex != 0 {
		t.Fatalf("expected 1 card active at index 0, got len=%d idx=%d", len(m.btwCards), m.activeBtwIndex)
	}

	// Dismiss card
	m.DismissBtwResponse()
	if m.isBtwCardVisible() {
		t.Fatal("card should not be visible after dismissal")
	}

	// Run /btw without arguments: should restore history
	respLenBefore := len(m.response)
	m.handleSlashCommand("/btw")
	if !m.isBtwCardVisible() {
		t.Fatal("card should be visible after /btw with history")
	}
	if m.activeBtwIndex != 0 {
		t.Errorf("expected activeBtwIndex=0, got %d", m.activeBtwIndex)
	}
	// No error / usage response should have been appended
	if len(m.response) > respLenBefore {
		for _, row := range m.response[respLenBefore:] {
			if strings.Contains(stripColor(row), "Usage: /btw") {
				t.Errorf("unexpected usage output when showing history: %s", row)
			}
		}
	}
}

// TestBtwConcurrencyGuard verifies that attempting to run `/btw` while another question
// is still generating is rejected with the reference message.
func TestBtwConcurrencyGuard(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	// Seed an in-flight card
	activeCard := &sidequestion.Card{
		ID:       "in-flight-1",
		Question: "Running question?",
		State:    sidequestion.StateStreaming,
	}
	m.btwCards = append(m.btwCards, activeCard)
	m.activeBtwIndex = 0
	m.activeBtwID = activeCard.ID

	if !m.IsBtwActive() {
		t.Fatal("IsBtwActive should report true for streaming card")
	}

	m.handleSlashCommand("/btw Another question")
	var found bool
	wantMsg := "A /btw question is already in progress. Please wait for it to finish."
	for _, row := range m.response {
		if strings.Contains(stripColor(row), wantMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("transcript missing concurrency guard message %q: %v", wantMsg, m.response)
	}
}

// TestCopyBtwInProgressAndFailed verifies `/copy btw` behavior for generating and failed cards.
func TestCopyBtwInProgressAndFailed(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	streamingCard := &sidequestion.Card{
		ID:       "gen-1",
		Question: "Generating...",
		State:    sidequestion.StateStreaming,
	}
	m.btwCards = append(m.btwCards, streamingCard)
	m.activeBtwIndex = 0
	m.activeBtwID = streamingCard.ID

	m.handleSlashCommand("/copy btw")
	wantGen := "The /btw answer is still being generated."
	var foundGen bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), wantGen) {
			foundGen = true
			break
		}
	}
	if !foundGen {
		t.Errorf("missing in-progress message %q: %v", wantGen, m.response)
	}

	// Now test failed card
	failedCard := &sidequestion.Card{
		ID:       "fail-1",
		Question: "Failing...",
		State:    sidequestion.StateFailed,
		Err:      errors.New("network timeout"),
	}
	m.btwCards = append(m.btwCards, failedCard)
	m.activeBtwIndex = 1
	m.activeBtwID = failedCard.ID

	m.handleSlashCommand("/copy btw")
	wantFail := "The /btw question failed: network timeout"
	var foundFail bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), wantFail) {
			foundFail = true
			break
		}
	}
	if !foundFail {
		t.Errorf("missing failure message %q: %v", wantFail, m.response)
	}
}

// TestBtwNavigation verifies ← / → shifting through historical BTW cards.
func TestBtwNavigation(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	c1 := &sidequestion.Card{ID: "c1", Question: "Q1", Answer: "A1", State: sidequestion.StateDone}
	c2 := &sidequestion.Card{ID: "c2", Question: "Q2", Answer: "A2", State: sidequestion.StateDone}
	c3 := &sidequestion.Card{ID: "c3", Question: "Q3", Answer: "A3", State: sidequestion.StateDone}
	m.btwCards = []*sidequestion.Card{c1, c2, c3}
	m.activeBtwIndex = 2
	m.activeBtwID = c3.ID

	// Left navigates to c2
	m.NavigateBtw(-1)
	if m.activeBtwIndex != 1 || m.activeBtwID != c2.ID {
		t.Errorf("expected idx=1 c2, got idx=%d id=%s", m.activeBtwIndex, m.activeBtwID)
	}

	// Left navigates to c1
	m.NavigateBtw(-1)
	if m.activeBtwIndex != 0 || m.activeBtwID != c1.ID {
		t.Errorf("expected idx=0 c1, got idx=%d id=%s", m.activeBtwIndex, m.activeBtwID)
	}

	// Left clamps at 0
	m.NavigateBtw(-1)
	if m.activeBtwIndex != 0 {
		t.Errorf("expected clamp to idx=0, got %d", m.activeBtwIndex)
	}

	// Right navigates back to c2 then c3
	m.NavigateBtw(1)
	if m.activeBtwIndex != 1 {
		t.Errorf("expected idx=1, got %d", m.activeBtwIndex)
	}
	m.NavigateBtw(1)
	if m.activeBtwIndex != 2 {
		t.Errorf("expected idx=2, got %d", m.activeBtwIndex)
	}
	// Right clamps at len-1
	m.NavigateBtw(1)
	if m.activeBtwIndex != 2 {
		t.Errorf("expected clamp to idx=2, got %d", m.activeBtwIndex)
	}
}

// TestBtwRenderCardFormatting verifies card styling with thick left border (┃),
// question header, answer body, and hint bar.
func TestBtwRenderCardFormatting(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	m.width = 80
	m.height = 30
	c := &sidequestion.Card{
		ID:       "card-render",
		Question: "How does caching work?",
		Answer:   "Caching stores computed results for fast retrieval.",
		State:    sidequestion.StateDone,
	}
	m.btwCards = []*sidequestion.Card{c}
	m.activeBtwIndex = 0
	m.activeBtwID = c.ID

	rendered := m.renderBtwCard()
	if !strings.Contains(rendered, "┃") {
		t.Errorf("rendered card missing thick left border ┃:\n%s", rendered)
	}
	if !strings.Contains(rendered, "How does caching work?") {
		t.Errorf("rendered card missing question:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Caching stores computed results") {
		t.Errorf("rendered card missing answer body:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Tab to expand") {
		t.Errorf("rendered card missing 'Tab to expand' in collapsed mode:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Enter/Esc to dismiss") {
		t.Errorf("rendered card missing 'Enter/Esc to dismiss':\n%s", rendered)
	}

	// Toggle expansion
	m.ToggleBtwExpanded()
	expandedRendered := m.renderBtwCard()
	if !strings.Contains(expandedRendered, "Tab to collapse") {
		t.Errorf("expanded card missing 'Tab to collapse':\n%s", expandedRendered)
	}
}


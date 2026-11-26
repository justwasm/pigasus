package tui

// credential_test.go — the shared credential's cached read.
//
// The cache exists so the composer's submit path does not pay for a keychain
// syscall on every Enter. What has to hold, and is tested here:
//
//   - one read answers both questions (presence, identity);
//   - the answer is reused, so a second question costs nothing;
//   - a credential the session changes itself invalidates the cache, so the
//     TUI never answers with a credential it has already replaced;
//   - a read issued before that change is dropped rather than adopted, so an
//     in-flight read cannot undo a sign-in.

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// TestCredentialStateAnswersPresenceAndIdentityFromOneRead pins the shape of
// the cache: one read fills both answers, and the second question-behind it
// is free.
func TestCredentialStateAnswersPresenceAndIdentityFromOneRead(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")

	m := &RootModel{session: sessionState{email: "a@example.com"}}
	present, email := m.credentialState()
	if !present {
		t.Fatal("credentialState reports no credential with one registered")
	}
	if email != "a@example.com" {
		t.Errorf("credentialState email = %q, want a@example.com", email)
	}
	if !m.session.credential.loaded {
		t.Fatal("the read was not cached")
	}
}

// TestCredentialStateDoesNotOverwriteTheSessionIdentity pins the field split.
//
// session.email is the identity the session adopted; the cache's email is what
// the credential says. syncAccountIdentity compares the two to notice a
// switch, so a cache that wrote session.email would make every switch look
// like no switch.
func TestCredentialStateDoesNotOverwriteTheSessionIdentity(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com", "b@example.com")

	// The session is still talking as an account the credential no longer
	// names — the state a switch made outside the TUI leaves behind.
	const adopted = "stale@example.com"
	m := &RootModel{session: sessionState{email: adopted}}

	_, email := m.credentialState()
	if email != "a@example.com" {
		t.Fatalf("credential email = %q, want a@example.com", email)
	}
	// Adopting an account is what changes session.email, not the read. If
	// the read wrote it, the comparison in syncAccountIdentity would find
	// the two equal and never rebuild the transport.
	if m.session.email != adopted {
		t.Errorf("the read changed session.email to %q, want the adopted %q", m.session.email, adopted)
	}
}

// TestForgetCredentialMakesTheNextReadFresh pins invalidation: a sign-in or a
// switch has to be visible to the very next question.
func TestForgetCredentialMakesTheNextReadFresh(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")

	m := &RootModel{session: sessionState{email: "a@example.com"}}
	if _, email := m.credentialState(); email != "a@example.com" {
		t.Fatalf("first read = %q, want a@example.com", email)
	}

	// The account changes outside this write path (the registry is the
	// source of truth).
	if err := auth.SaveTokenUnified(accountsFakeToken(t, "b@example.com", "at-b")); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}
	if _, email := m.credentialState(); email != "a@example.com" {
		t.Fatalf("the cache answered %q after the credential changed; it should be reused until forgotten", email)
	}

	m.forgetCredential()
	if _, email := m.credentialState(); email != "b@example.com" {
		t.Errorf("after forgetCredential the read = %q, want b@example.com", email)
	}
}

// TestSignOutInvalidatesTheCredentialCache pins the sign-out path: /logout
// must not leave the composer answering "signed in" from a stale read.
func TestSignOutInvalidatesTheCredentialCache(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")

	m := &RootModel{session: sessionState{email: "a@example.com"}}
	if !m.hasCredential() {
		t.Fatal("the session does not see the credential it was built with")
	}
	if _, cmd := m.slashLogout(""); cmd != nil {
		t.Fatal("logout returned a command; it has no work for one")
	}
	if m.hasCredential() {
		t.Error("the composer still reports a credential after signing out")
	}
}

// TestStaleCredentialReadIsDropped pins the generation: a read issued before
// the session replaced its own credential answers about the old one, and
// adopting it would undo the replacement.
func TestStaleCredentialReadIsDropped(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com", "b@example.com")

	m := &RootModel{session: sessionState{email: "b@example.com"}}
	m.forgetCredential()

	// A read that started now...
	cmd := m.readCredentialCmd()
	// ...and the session signs in as b@example.com before it answers.
	m.forgetCredential()

	msg, ok := cmd().(credentialReadMsg)
	if !ok {
		t.Fatal("the read did not produce a credentialReadMsg")
	}
	if cmd := m.applyCredentialRead(msg); cmd != nil {
		_ = cmd
	}
	if m.session.credential.loaded {
		t.Error("a read from before the invalidation was adopted")
	}
}

// TestCredentialReadAdoptsASwitchMadeOutsideTheTUI pins the feature the cache
// must not break: the account can be switched by the `agy accounts` command in
// another terminal, and the running session follows it.
func TestCredentialReadAdoptsASwitchMadeOutsideTheTUI(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")

	m := &RootModel{session: sessionState{email: "a@example.com"}}
	old := &accountsTestBackend{Client: backend.NewMock(), id: 0}
	m.turn.backend = old
	m.turn.backendFor = func(string) backend.Client {
		return &accountsTestBackend{Client: backend.NewMock(), id: 1}
	}

	// The shared credential now names another account.
	if err := auth.SaveTokenUnified(accountsFakeToken(t, "b@example.com", "at-b@example.com")); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}
	m.forgetCredential()

	cmd := m.readCredentialCmd()
	msg, ok := cmd().(credentialReadMsg)
	if !ok {
		t.Fatal("the read did not produce a credentialReadMsg")
	}
	_ = m.applyCredentialRead(msg)
	if m.session.email != "b@example.com" {
		t.Fatalf("session.email = %q, want b@example.com", m.session.email)
	}
	if m.turn.backend == old {
		t.Error("the transport still belongs to the previous account")
	}
}

// TestWarmCredentialCmdSkipsAReadThatAlreadyHappened pins the start-up warm:
// Init arms it once, and the first submit then has nothing to wait for.
func TestWarmCredentialCmdSkipsAReadThatAlreadyHappened(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")

	m := &RootModel{session: sessionState{email: "a@example.com"}}
	if m.warmCredentialCmd() == nil {
		t.Fatal("warmCredentialCmd has nothing to warm on a cold session")
	}
	m.hasCredential()
	if cmd := m.warmCredentialCmd(); cmd != nil {
		t.Error("warmCredentialCmd issued a read for a session whose credential is already known")
	}
}

// TestRefreshCredentialCmdOnlyAges is the TTL rule: a fresh read costs nothing
// and an aged one is re-read on a command goroutine.
func TestRefreshCredentialCmdOnlyAges(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")

	m := &RootModel{session: sessionState{email: "a@example.com"}}
	m.hasCredential()
	if cmd := m.refreshCredentialCmd(); cmd != nil {
		t.Error("refreshCredentialCmd issued a read for a fresh cache")
	}
	m.session.credential.at = m.session.credential.at.Add(-2 * credentialTTL)
	if cmd := m.refreshCredentialCmd(); cmd == nil {
		t.Error("refreshCredentialCmd issued no read for an aged cache")
	}
}

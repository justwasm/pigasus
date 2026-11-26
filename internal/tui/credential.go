// credential.go — the shared credential's state, read once per turn and
// re-read off the Update goroutine.
//
// Two questions are asked of the shared credential on the composer's submit
// path, in this order:
//
//  1. Is there one at all? (dispatchPromptPrefix sends the user to the login
//     screen when there is not.)
//  2. Whose is it? (syncAccountIdentity rebuilds the transport when the
//     account changed outside the TUI.)
//
// Both answers cost a read of the shared credential, and on macOS that read
// is a keychain lookup — a `security` spawn, measured at 11-15 ms here. The
// submit path used to ask twice per Enter: once through HasOAuthToken, which
// probed the keychain and then loaded the token (reading it again), and once
// through syncAccountIdentity. On the goroutine Bubble Tea renders from.
//
// Now the two answers come from one read — accounts.Current returns the token
// and the identity together — and the read is reused for a moment, so a
// keystroke never pays for it. The TTL is what keeps the "switched the
// account outside the TUI" behaviour (the reason the credential is re-read at
// all): the fresh read happens on a command goroutine, not on Update.
//
// A switch made *inside* the TUI is noticed immediately, because the sites
// that change the credential forget the cache (see forgetCredential) and
// update the session's identity directly.
package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/infra/backend/backendselect"
)

// credentialTTL bounds how long one read of the shared credential is reused
// before a command is asked to re-read it.
const credentialTTL = 5 * time.Second

// credentialCache is the shared credential's presence and identity as last
// read, with the time it was read.
//
// It lives in sessionState so a RootModel carries it the way it carries the
// rest of its session state; the zero value means "never read".
type credentialCache struct {
	at      time.Time
	loaded  bool
	present bool
	email   string
	// gen is bumped whenever the cache is invalidated. A read that started
	// before the bump answers about a credential the session has already
	// replaced, so its answer must be dropped rather than adopted.
	gen uint64
}

// credentialReadMsg carries a re-read of the shared credential.
//
// The read is performed on the command's goroutine: it is the keychain
// syscall the composer must not pay for. gen is the cache generation the
// read was issued under.
type credentialReadMsg struct {
	present bool
	email   string
	gen     uint64
}

// credentialState returns whether a usable credential exists and the account
// it names.
//
// The first call in a session reads the credential, because a session cannot
// answer "is the user signed in" without asking once. Every later call
// answers from the cached read even when it has aged past credentialTTL: the
// refresh is refreshCredentialCmd's job, and it runs off this goroutine.
// Answering from the stale read rather than blocking here is the whole point
// — an account switch made outside the TUI is noticed one message later, and
// the alternative is every Enter paying ~15 ms.
//
// The email is *not* stored into session.email: that field is the identity
// the session has adopted, and syncAccountIdentity compares the credential's
// answer against it to decide whether the account moved. Writing it here
// would make the comparison see "unchanged" and skip the transport rebuild.
func (m *RootModel) credentialState() (bool, string) {
	c := &m.session.credential
	if c.loaded {
		return c.present, c.email
	}
	present, email := readCredential()
	c.at = time.Now()
	c.loaded = true
	c.present = present
	c.email = email
	return present, email
}

// readCredential performs the credential read itself.
func readCredential() (bool, string) {
	tok, email := accounts.Current()
	present := tok != nil && (tok.Token.AccessToken != "" || tok.Token.RefreshToken != "")
	if !present {
		// The cascade found nothing usable. Ask the dispatch's own question
		// rather than answering "absent": it also accepts a token file this
		// package cannot parse, which is the state that sends the user to
		// the login screen — and that is the dispatch's decision to make.
		//
		// This second read only happens with no usable credential, i.e.
		// right before a login prompt, never on a healthy turn.
		present = backendselect.HasOAuthToken()
	}
	return present, email
}

// hasCredential reports whether the session can talk to the server as a
// signed-in account.
func (m *RootModel) hasCredential() bool {
	present, _ := m.credentialState()
	return present
}

// warmCredentialCmd reads the shared credential once at start-up, on a
// command goroutine, so the session's first submit does not have to.
//
// Init is the natural place: a session needs the answer eventually (the
// banner shows the account, the first turn needs the transport), and warming
// it there means the first Enter costs what every later one costs.
func (m *RootModel) warmCredentialCmd() tea.Cmd {
	if m == nil || m.session.credential.loaded {
		return nil
	}
	return m.readCredentialCmd()
}

// readCredentialCmd issues one read under the cache's current generation.
func (m *RootModel) readCredentialCmd() tea.Cmd {
	gen := m.session.credential.gen
	return func() tea.Msg {
		present, email := readCredential()
		return credentialReadMsg{present: present, email: email, gen: gen}
	}
}

// refreshCredentialCmd re-reads the shared credential on a command goroutine
// when the cached read has aged out.
//
// It returns nil while the cache is fresh, so an idle composer costs nothing
// and a burst of submits costs one read.
func (m *RootModel) refreshCredentialCmd() tea.Cmd {
	if m == nil || !m.session.credential.loaded {
		return nil
	}
	if time.Since(m.session.credential.at) < credentialTTL {
		return nil
	}
	return m.readCredentialCmd()
}

// applyCredentialRead adopts a re-read credential.
//
// A different account means the transport has to be rebuilt before the next
// turn: the transport caches the keychain token it read, so it keeps talking
// as the previous account until it is rebuilt.
func (m *RootModel) applyCredentialRead(msg credentialReadMsg) tea.Cmd {
	c := &m.session.credential
	if msg.gen != c.gen {
		// The read was issued before the session changed the credential
		// itself, so it answers about a credential that is already gone.
		// Dropping it keeps a sign-in from being undone by an in-flight
		// read that started a moment earlier.
		return nil
	}
	c.at = time.Now()
	c.loaded = true
	c.present = msg.present
	c.email = msg.email
	if msg.email == "" || msg.email == m.session.email {
		return nil
	}
	return m.adoptCredentialAccount(msg.email)
}

// adoptCredentialAccount makes the session talk as email.
func (m *RootModel) adoptCredentialAccount(email string) tea.Cmd {
	m.session.email = email
	// The session's tier belonged to the previous account.
	m.forgetSessionTier()
	if m.turn.backendFor != nil {
		if b := m.turn.backendFor(m.session.model); b != nil {
			m.turn.backend = b
		}
	}
	return m.fetchPlanTierCmd()
}

// forgetCredential drops the cached read, so the next question re-reads the
// shared credential.
//
// The sites that change the credential call it: a sign-in, a sign-out and an
// account switch. Without it the cache would answer with a credential the
// session has already replaced.
func (m *RootModel) forgetCredential() {
	gen := m.session.credential.gen + 1
	m.session.credential = credentialCache{gen: gen}
}

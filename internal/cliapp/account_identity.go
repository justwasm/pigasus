// account_identity.go — cycle multi-account-switch: keeping the
// session's account identity in step with the shared credential.
//
// # Why this exists
//
// The credential is shared: the clone, the `agy accounts` command and
// the installed `agy` all read the same macOS keychain entry (and the
// same on-disk copies). A switch made *outside* the TUI — the CLI
// command, a sign-in in another terminal — must still reach the running
// session, so the session re-reads the credential when it can matter:
// before a model turn and when a slash command opens a panel.
//
// The reference caches its credential in memory for the whole session,
// so an outside switch only reaches it on its next token refresh (or on
// the next start-up). The clone re-reads it instead — the superset the
// /accounts panel promises.
package cliapp

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

// syncAccountIdentity re-reads the shared credential and updates the
// session's account identity when it changed outside the TUI.
//
// It returns a command that refreshes the plan tier when the account
// changed, and nil when nothing changed. The credential read is the
// same one auth.LoadTokenUnified does — the login keychain first, the
// on-disk copies after it.
func (m *runMode) syncAccountIdentity() tea.Cmd {
	_, email := accounts.Current()
	if email == "" || email == m.email {
		return nil
	}
	m.email = email
	// The session's tier belonged to the previous account.
	m.forgetSessionTier()
	// The transport caches the keychain token it read, so it has to be
	// rebuilt for the session to talk as the new account.
	if m.backendFor != nil {
		if b := m.backendFor(m.model); b != nil {
			m.backend = b
		}
	}
	return m.fetchPlanTierCmd()
}

// registerCurrentAccount adds the credential the session starts on to the
// registry.
//
// The email comes from the caller: the userinfo probe resolves it when the
// token's id_token carries no email claim any more — a token the
// reference refreshed loses its id_token. A missing credential is not an
// error; the session simply starts signed out.
func registerCurrentAccount(email string) {
	accounts.RegisterCurrent(email)
}

// ---- merged from account_quota.go ----
// account_quota.go — cycle accounts-tier-usage: the per-account quota
// and tier lookups the /accounts panel shows.
//
// # What is queried
//
// Two answers, both per *account* and both read from the server:
//
//   - the plan tier (loadCodeAssist's paidTier.name — "Google AI Pro",
//     "Antigravity Starter Quota");
//   - the quota summary (retrieveUserQuotaSummary — the model groups
//     with their weekly and five-hour buckets).
//
// The tier is queried for the *active* account only: it is what the
// session's banner shows, so one query per session is enough. The
// answer is cached in the registry, so every account that has been
// active once keeps its tier.
//
// The quota is queried for every account, because that is what the
// panel shows. It is fetched one account at a time — a registry with
// five accounts must not fire five refresh-token exchanges at once —
// and the answer is cached, so opening the panel twice in a row costs
// one fetch.
//
// # The credential each query uses
//
// The quota belongs to the account behind a credential, so each account
// is queried with its own access token. The active account uses the
// shared credential (the one the session talks with); an inactive
// account uses its own stored token, refreshed quietly — a refresh must
// not write the shared credential (that would switch the active
// account) and must not clear it (that would sign the user out).

// accountsQuotaMsg carries one account's quota fetch result.
type accountsQuotaMsg struct {
	email   string
	summary *auth.QuotaSummary
	err     error
}

// fetchAccountQuotaCmd fetches one account's quota summary with that
// account's own credential and caches the result in the registry.
//
// The cache write happens here, in the goroutine, so the UI thread
// never blocks on the registry file.
//
// The budget is 45 seconds because the call can be two round trips: a
// token refresh (the OAuth endpoint measured 9 s from this machine)
// and the quota call itself (6.9 s). A shorter budget turned a slow
// but successful fetch into a timeout.
func fetchAccountQuotaCmd(email string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		summary, err := fetchAccountQuota(ctx, email)
		return accountsQuotaMsg{email: email, summary: summary, err: err}
	}
}

// fetchAccountQuota queries one account's quota and caches it.
func fetchAccountQuota(ctx context.Context, email string) (*auth.QuotaSummary, error) {
	return accounts.FetchQuota(ctx, email)
}

// updateAccountsQuotaMsg handles one account's quota fetch result.
//
// The panel may have been closed while the request was in flight, so
// the panel's own state is only touched while it is the open panel.
// The next account in the queue is started either way — the queue is
// the panel's, but a fetch that is already running must not be
// orphaned.
func (m *runMode) updateAccountsQuotaMsg(msg accountsQuotaMsg) tea.Cmd {
	// The finished account leaves the queue, whether the panel is still
	// open or not: a queue that keeps a finished entry would stall.
	if len(m.panel.acct.quotaQueue) > 0 &&
		strings.EqualFold(m.panel.acct.quotaQueue[0], msg.email) {
		m.panel.acct.quotaQueue = m.panel.acct.quotaQueue[1:]
	}
	if m.panel.kind == panelAccounts {
		if msg.err != nil {
			m.panel.acct.quotaErr = msg.err.Error()
		} else if msg.summary != nil {
			m.cacheQuota(msg.email, msg.summary)
			m.panel.acct.quotaErr = ""
		}
		// Pick up the snapshot the fetch cached in the registry.
		m.reloadAccountsRows()
	}
	return m.nextAccountsQuotaCmd()
}

// accountAccessToken returns the credential to query an account's quota
// with.
//
// The active account is queried with the shared credential: that is the
// credential the session talks with, and a refresh of it belongs in the
// shared credential. An inactive account is queried with its own stored
// token, refreshed quietly.
//
// The shared credential is read *once*: the token that answers the
// request and the decision "is this the active account?" have to come
// from the same read, or a switch in between would query one account
// and file the answer under another.
func accountAccessToken(ctx context.Context, email string, acc accounts.Account) (*auth.AntigravityToken, error) {
	return accounts.AccessToken(ctx, email, acc)
}

// ---- the session's quota cache -----------------------------------------

// quotaCacheEntry is one account's cached quota summary.
type quotaCacheEntry struct {
	summary   *auth.QuotaSummary
	fetchedAt time.Time
}

// quotaCacheFresh is how long a cached summary is reused instead of
// being queried again.
//
// The quota moves in hours, not in seconds: the five-hour window
// creeps and the weekly window creeps. Two minutes is short enough
// that the numbers are current and long enough that opening /usage
// right after /accounts does not query the same account twice.
const quotaCacheFresh = 2 * time.Minute

// cachedQuota returns the cached summary for an account when it is
// still fresh enough to render.
func (m *runMode) cachedQuota(email string) *auth.QuotaSummary {
	if m == nil || email == "" || m.quotaCache == nil {
		return nil
	}
	entry, ok := m.quotaCache[email]
	if !ok || entry.summary == nil {
		return nil
	}
	if time.Since(entry.fetchedAt) > quotaCacheFresh {
		return nil
	}
	return entry.summary
}

// cacheQuota remembers an account's summary for the session.
func (m *runMode) cacheQuota(email string, summary *auth.QuotaSummary) {
	if m == nil || email == "" || summary == nil {
		return
	}
	if m.quotaCache == nil {
		m.quotaCache = map[string]quotaCacheEntry{}
	}
	m.quotaCache[email] = quotaCacheEntry{summary: summary, fetchedAt: time.Now()}
}

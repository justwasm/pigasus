// Package accountrouting — the runtime half of the account pool.
//
// # What it is
//
// The clone keeps a registry of the Google accounts the user signed in to
// (`internal/accounts`). A turn is served by one of them. The *policy* —
// which account a turn runs on, and how the pool rotates — lives in the
// `accountpool` package and is read from `~/.config/openagy/config.json`.
//
// This package is the machinery around that policy:
//
//   - it gathers the pool's state (who is registered, what quota each
//     account has left, who is in a cooldown);
//   - it asks the policy which account the turn should use;
//   - it hands the transport that account's credential;
//   - it moves the pool on when the server says a quota is spent.
//
// # The credential
//
// The routed account's credential is handed to the transport as a token
// function, so the decision is taken *per turn* and not once per session.
// The shared credential — the macOS keychain entry and the three on-disk
// copies — is **not** rewritten: the routed account is the account this
// process talks to, and the account the user picked stays the one the
// installed `agy` and the `/accounts` panel show. The two are separate on
// purpose: a load balancer that rewrote the keychain would change the
// identity of every other program on the machine.
//
// An account that is *not* the active one is refreshed quietly: a refresh
// that wrote the shared credential would sign the user in as that account
// behind the user's back.
//
// # The quota figures
//
// A quota figure is read from the server (`retrieveUserQuotaSummary`, the
// same call the `/usage` panel makes), per account, with that account's
// own credential. The read happens in the background: a turn is never
// held up by a quota lookup, and a figure that is not there yet ranks the
// account last instead of excluding it.
package accountrouting

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
)

// DebugEnvVar turns the routing's log lines on. The lines go to the
// transport's log writer — the TUI's own log, or stderr in the headless
// paths — and this variable adds them to stderr as well.
const DebugEnvVar = "OPENAGY_ACCOUNT_POOL_DEBUG"

// Router picks the account a turn runs on.
//
// The router is safe for concurrent use: the turn loop and the quota
// refresher touch it at the same time.
type Router struct {
	mu sync.Mutex

	// cursor is the round-robin rotation's position.
	cursor int

	// remaining holds the last quota figure per account.
	remaining map[string]quota

	// excluded holds the accounts that are out of quota, with the time
	// the server said so.
	excluded map[string]time.Time

	// lastUsed holds when an account last served a turn.
	lastUsed map[string]time.Time

	// refreshing marks the accounts whose quota is being read right now,
	// so a second turn does not start a second read.
	refreshing map[string]bool

	// shared is the credential of the account the session is signed in
	// with, sharedEmail is the account it belongs to, and sharedActive
	// is the registry's active account at the time it was read.
	shared       *auth.AntigravityToken
	sharedEmail  string
	sharedActive string

	// loadShared reads the shared credential. It is a field so a test
	// can answer without the machine's keychain.
	loadShared func() (*auth.AntigravityToken, string, error)

	// fallback is the transport's own token source. It answers when the
	// routing is off, so the credential the clone uses without a policy
	// is the one the transport would have used by itself — the same
	// cascade, not a second implementation of it.
	fallback func(context.Context) (string, error)

	// lastRouted is the account the last turn was routed to.
	lastRouted string

	// log is where the routing's lines go.
	log io.Writer
}

// quota is one account's last known quota figure.
type quota struct {
	// remaining is the fraction of the window that is left.
	remaining float64

	// known reports whether the figure was measured.
	known bool

	// fetchedAt is when the figure was read.
	fetchedAt time.Time
}

// shared is the process-wide router.
var shared = sync.OnceValue(func() *Router {
	r := &Router{
		remaining:  map[string]quota{},
		excluded:   map[string]time.Time{},
		lastUsed:   map[string]time.Time{},
		refreshing: map[string]bool{},
		log:        defaultLogWriter(),
	}
	r.loadShared = readShared
	r.fallback = backend.DefaultTokenSource()
	return r
})

// Shared returns the process-wide router.
func Shared() *Router { return shared() }

// Policy returns the account-routing policy the configuration file
// carries, or nil when it carries none.
//
// The file is re-read when it changes on disk, so a policy the user edits
// mid-session takes effect on the next turn.
func Policy() *accountpool.Config { return endpoints.CurrentAccountPool() }

// Enabled reports whether the routing is on.
//
// The transport installs the router whether it is on or not — the policy
// is read per turn, so a policy the user turns on takes effect on the
// next turn. This is the question the panels ask: "is the pool routing
// the turns right now?"
func Enabled() bool {
	p := Policy()
	return p != nil && p.Enabled
}

// SetLogWriter sets where the routing's lines go. The transport's own log
// writer is the natural one.
func (r *Router) SetLogWriter(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	r.mu.Lock()
	r.log = w
	r.mu.Unlock()
}

// logf writes one routing line.
func (r *Router) logf(format string, args ...any) {
	r.mu.Lock()
	w := r.log
	r.mu.Unlock()
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", args...)
}

// defaultLogWriter is where the routing's lines go when nobody said
// otherwise: stderr, but only when the debug variable is set. A quiet
// default matters — the lines are about the clone's own plumbing, not
// about the user's turn.
func defaultLogWriter() io.Writer {
	if strings.TrimSpace(os.Getenv(DebugEnvVar)) == "" {
		return io.Discard
	}
	return os.Stderr
}

// TokenFunc returns the token source the CloudCode transport runs on.
//
// It is called once per turn, so the pool rotates per turn. When the
// routing is off it answers with the credential the transport's own token
// source would answer with — the same cascade, not a copy of it.
func (r *Router) TokenFunc(ctx context.Context) (string, error) {
	policy := Policy()
	if policy == nil || !policy.Enabled {
		return r.defaultToken(ctx)
	}
	decision, err := r.Decide(ctx)
	if err != nil {
		return "", err
	}
	if decision.Email == "" {
		// The pool holds no account: the transport's own credential
		// is the only one there is.
		return r.defaultToken(ctx)
	}
	tok, err := r.tokenFor(ctx, decision.Email)
	if err != nil {
		return "", err
	}
	r.logf("[account-pool] %s — %s", decision.Email, decision.Reason)
	return tok.Token.AccessToken, nil
}

// SetFallback installs the transport's own token source as the router's
// off-policy answer.
//
// The transport installs it when it is built. A transport that is rebuilt
// — the TUI does that when the model changes — installs a fresh one, so
// the credential the clone uses without a policy is the one the transport
// would have used by itself, at that moment.
func (r *Router) SetFallback(src func(context.Context) (string, error)) {
	if src == nil {
		return
	}
	r.mu.Lock()
	r.fallback = src
	r.mu.Unlock()
}

// defaultToken answers with the transport's own credential.
//
// The source is created once and kept: it caches the credential in
// memory, and a second cache would be a second answer to the same
// question.
func (r *Router) defaultToken(ctx context.Context) (string, error) {
	r.mu.Lock()
	fallback := r.fallback
	r.mu.Unlock()
	if fallback == nil {
		fallback = backend.DefaultTokenSource()
		r.mu.Lock()
		r.fallback = fallback
		r.mu.Unlock()
	}
	return fallback(ctx)
}

// Decide asks the policy which account the next turn should use.
//
// It is the decision the token function makes, without the credential:
// what is left is the account's own business.
func (r *Router) Decide(ctx context.Context) (accountpool.Decision, error) {
	policy := Policy()
	if policy == nil || !policy.Enabled {
		return accountpool.Decision{}, errors.New("accountpool: the routing is off")
	}
	cfg := policy.Normalised()

	states, err := r.states(ctx, cfg)
	if err != nil {
		return accountpool.Decision{}, err
	}

	r.mu.Lock()
	cursor := r.cursor
	r.mu.Unlock()

	decision := accountpool.Choose(cfg, states, cursor)

	r.mu.Lock()
	r.cursor = decision.NextCursor
	if decision.Email != "" {
		r.lastUsed[decision.Email] = time.Now()
		r.lastRouted = decision.Email
	}
	r.mu.Unlock()
	return decision, nil
}

// LastRouted returns the account the last turn was routed to.
func (r *Router) LastRouted() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRouted
}

// Exclude puts an account out of the pool for the policy's cooldown.
//
// The runtime calls it when the server says the account's quota is spent:
// the next turn goes to another account, and this one comes back when its
// cooldown passes.
func (r *Router) Exclude(email string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	r.mu.Lock()
	r.excluded[email] = time.Now()
	r.mu.Unlock()
	r.logf("[account-pool] %s is out of quota; cooling down", email)
}

// states gathers the pool's state for the policy to choose from.
func (r *Router) states(ctx context.Context, cfg accountpool.Config) ([]accountpool.State, error) {
	st, err := accounts.Load()
	if err != nil {
		return nil, err
	}
	current := strings.TrimSpace(st.Active)
	if current == "" {
		// The registry names no active account: the shared credential
		// does. It is only read when it is needed, so a turn does not
		// pay for a keychain lookup it does not need.
		if _, email, err := r.sharedToken(ctx); err == nil {
			current = email
		}
	}
	now := time.Now()
	out := make([]accountpool.State, 0, len(st.Accounts))
	for _, acc := range st.Accounts {
		email := strings.TrimSpace(acc.Email)
		if email == "" {
			continue
		}
		remaining, known := r.quotaFor(ctx, cfg, acc)
		r.mu.Lock()
		excluded := false
		if until, ok := r.excluded[email]; ok {
			if now.Before(until.Add(cfg.CooldownDuration())) {
				excluded = true
			} else {
				delete(r.excluded, email)
			}
		}
		lastUsed := r.lastUsed[email]
		r.mu.Unlock()
		out = append(out, accountpool.State{
			Email:     email,
			Remaining: remaining,
			Known:     known,
			Current:   strings.EqualFold(email, current),
			Excluded:  excluded,
			LastUsed:  lastUsed,
		})
	}
	return out, nil
}

// quotaFor returns an account's remaining quota.
//
// The figure comes from the router's own cache, seeded from the
// registry's snapshot (the `/accounts` panel keeps that fresh). A figure
// that is missing or stale schedules a background read; the turn goes on
// with what is known.
func (r *Router) quotaFor(ctx context.Context, cfg accountpool.Config, acc accounts.Account) (float64, bool) {
	email := acc.Email

	r.mu.Lock()
	cached, haveCached := r.remaining[email]
	r.mu.Unlock()

	if haveCached && time.Since(cached.fetchedAt) < cfg.RefreshDuration() {
		return cached.remaining, cached.known
	}

	// The registry's own snapshot is the seed: it costs nothing to read
	// and the panel keeps it fresh.
	if acc.Quota != nil {
		if rem, known := remainingOf(acc.Quota); known {
			r.mu.Lock()
			if !haveCached || acc.Quota.FetchedAt.After(cached.fetchedAt) {
				r.remaining[email] = quota{remaining: rem, known: true, fetchedAt: acc.Quota.FetchedAt}
				cached = r.remaining[email]
				haveCached = true
			}
			r.mu.Unlock()
		}
	}

	// Ask the server in the background — but only when the figure at hand
	// is stale. The turn is never held up by a quota lookup.
	if !haveCached || time.Since(cached.fetchedAt) >= cfg.RefreshDuration() {
		r.refreshQuota(email)
	}

	if haveCached {
		return cached.remaining, cached.known
	}
	return 0, false
}

// refreshQuota reads one account's quota in the background.
//
// One read per account at a time: a second turn does not start a second
// read.
func (r *Router) refreshQuota(email string) {
	r.mu.Lock()
	if r.refreshing[email] {
		r.mu.Unlock()
		return
	}
	r.refreshing[email] = true
	r.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		summary, err := accounts.FetchQuota(ctx, email)
		r.mu.Lock()
		delete(r.refreshing, email)
		r.mu.Unlock()
		if err != nil {
			r.logf("[account-pool] the quota of %s could not be read: %v", email, err)
			return
		}
		snap := summary.Snapshot()
		rem, known := remainingOf(snap)
		r.mu.Lock()
		r.remaining[email] = quota{remaining: rem, known: known, fetchedAt: time.Now()}
		r.mu.Unlock()
		if known {
			r.logf("[account-pool] %s has %.1f%% left", email, rem*100)
		}
	}()
}

// tokenFor returns the credential of the named account, refreshed when it
// is about to expire.
//
// The active account's credential is the shared one — the keychain entry
// and the three on-disk copies. Every other account has its own stored
// credential, and it is refreshed *quietly*: a refresh that wrote the
// shared credential would sign the user in as that account behind the
// user's back.
func (r *Router) tokenFor(ctx context.Context, email string) (*auth.AntigravityToken, error) {
	// The active account's credential is the shared one. It is only
	// looked up for that account: an account the user did not pick has
	// its own stored credential, and the keychain has nothing to say
	// about it.
	if active := r.activeAccount(); active != "" && strings.EqualFold(active, email) {
		if tok, _, err := r.sharedToken(ctx); err == nil && tok != nil {
			return tok, nil
		}
	}
	acc, ok, err := accounts.Get(email)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("accountpool: unknown account %q", email)
	}
	if acc.Token == nil {
		return nil, fmt.Errorf("accountpool: %s has no usable credential", email)
	}
	updated, _, err := acc.Token.RefreshQuiet(ctx)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		// A refreshed credential goes back into the registry, or the
		// next turn would refresh it again.
		_ = accounts.SetToken(email, updated)
		return updated, nil
	}
	return acc.Token, nil
}

// activeAccount returns the account the session is signed in with, as the
// registry records it.
func (r *Router) activeAccount() string {
	st, err := accounts.Load()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(st.Active)
}

// sharedToken returns the credential of the account the session is signed
// in with, and the account it belongs to.
//
// The credential is cached: the keychain is read once, not once per turn.
// The cache is dropped when the registry's active account changes — that
// is how a switch made in the `/accounts` panel reaches the transport.
//
// The cascade is the transport's own: on macOS the keychain entry wins
// over the on-disk copies, because that is where the live token is.
func (r *Router) sharedToken(ctx context.Context) (*auth.AntigravityToken, string, error) {
	active := r.activeAccount()

	r.mu.Lock()
	cached := r.shared
	cachedEmail := r.sharedEmail
	cachedActive := r.sharedActive
	r.mu.Unlock()

	if cached == nil || (active != "" && !strings.EqualFold(active, cachedActive)) {
		load := r.loadShared
		if load == nil {
			load = readShared
		}
		tok, email, err := load()
		if err != nil {
			return nil, "", err
		}
		r.mu.Lock()
		r.shared = tok
		r.sharedEmail = email
		r.sharedActive = active
		r.mu.Unlock()
		cached = tok
		cachedEmail = email
	}

	updated, _, err := cached.RefreshIfNeeded(ctx)
	if err != nil {
		return nil, "", err
	}
	if updated != nil {
		r.mu.Lock()
		r.shared = updated
		r.mu.Unlock()
		cached = updated
	}
	return cached, cachedEmail, nil
}

// readShared reads the shared credential once.
//
// The macOS keychain entry is preferred, exactly as the transport's own
// token source prefers it: the live token lives there, and the on-disk
// copies are the fallback.
func readShared() (*auth.AntigravityToken, string, error) {
	if runtime.GOOS == "darwin" && auth.HasKeychainToken() {
		tok, err := auth.LoadKeychainToken()
		if err == nil && tok != nil && tok.Token.AccessToken != "" {
			return tok, emailOf(tok), nil
		}
	}
	tok, email := accounts.Current()
	if tok == nil {
		return nil, "", auth.ErrNoToken
	}
	return tok, email, nil
}

// emailOf names the account a credential belongs to.
//
// The id_token's email claim is the first answer; a credential the
// reference refreshed carries no id_token any more, so the refresh token
// identifies it instead.
func emailOf(tok *auth.AntigravityToken) string {
	if tok == nil {
		return ""
	}
	if email := strings.TrimSpace(tok.UserEmail()); email != "" {
		return email
	}
	if st, err := accounts.Load(); err == nil {
		for _, a := range st.Accounts {
			if a.Token != nil && a.Token.Token.RefreshToken != "" &&
				a.Token.Token.RefreshToken == tok.Token.RefreshToken {
				return a.Email
			}
		}
	}
	return ""
}

// remainingOf returns the lowest remaining fraction across a snapshot's
// buckets.
//
// The lowest figure is the one that decides whether the account can serve
// a turn: the five-hour window runs out long before the weekly one.
func remainingOf(snap *auth.QuotaSnapshot) (float64, bool) {
	if snap == nil {
		return 0, false
	}
	found := false
	lowest := 0.0
	for _, g := range snap.Groups {
		for _, b := range g.Buckets {
			if !found || b.RemainingFraction < lowest {
				lowest = b.RemainingFraction
				found = true
			}
		}
	}
	return lowest, found
}

// Wrap returns a transport that runs its turns through the pool.
//
// The wrapper is installed whether the policy is on or not: it reads the
// policy per turn, so a policy the user turns on or off takes effect on
// the next turn, without a restart and without a new transport.
func Wrap(inner backend.Client, logWriter io.Writer) backend.Client {
	if inner == nil {
		return nil
	}
	r := Shared()
	if logWriter != nil {
		r.SetLogWriter(logWriter)
	}
	return &routedClient{inner: inner, router: r}
}

// Unwrap returns the transport behind a routed one.
//
// The pool's wrapper is transparent: a caller that wants to know which
// transport it got — a test, mostly — asks for the inner one.
func Unwrap(client backend.Client) backend.Client {
	if routed, ok := client.(*routedClient); ok {
		return routed.inner
	}
	return client
}

// routedClient is the transport the pool drives.
//
// It answers the transport's own credential with the routed account's,
// and it moves the pool on when the server says a quota is spent.
type routedClient struct {
	inner  backend.Client
	router *Router
}

// Unwrap returns the transport the pool drives.
func (c *routedClient) Unwrap() backend.Client { return c.inner }

// Stream runs one turn on the account the policy picked.
//
// A quota error is the one failure the pool acts on: the account is put
// out of the pool and the turn is tried again on another one, as far as
// the policy's attempt count allows. Any other failure is the transport's
// own — the runtime's retry decides what happens next.
func (c *routedClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	policy := Policy()
	if policy == nil || !policy.Enabled {
		// The routing is off: the transport answers for itself.
		return c.inner.Stream(ctx, req)
	}
	cfg := policy.Normalised()

	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		out, err := c.inner.Stream(ctx, req)
		if err == nil {
			return c.watch(ctx, out), nil
		}
		lastErr = err
		if !backend.IsQuotaReachedErr(err) {
			return nil, err
		}
		if !cfg.SwitchOnQuotaErrorEnabled() {
			return nil, err
		}
		email := c.router.LastRouted()
		c.router.Exclude(email)
		c.router.logf("[account-pool] %s is out of quota; trying another account", email)
	}
	return nil, lastErr
}

// watch forwards a turn's chunks and watches them for a quota error.
//
// A quota error that arrives mid-turn cannot be retried — the turn has
// already produced output — but it does put the account out of the pool,
// so the *next* turn goes to another account.
func (c *routedClient) watch(ctx context.Context, in <-chan backend.StreamChunk) <-chan backend.StreamChunk {
	out := make(chan backend.StreamChunk)
	go func() {
		defer close(out)
		for chunk := range in {
			if chunk.Error != nil && backend.IsQuotaReachedErr(chunk.Error) {
				c.router.Exclude(c.router.LastRouted())
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

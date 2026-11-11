// Package accountrouting — the runtime half of the account pool.
//
// # What it is
//
// The clone keeps a registry of the Google accounts the user signed in to
// (`internal/features/accounts/core`). A turn is served by one of them. The *policy* —
// which account a turn runs on, and how the pool rotates — lives in the
// `accountpool` package and is read from `~/.openagy/config.json`.
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
package routing

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

	"golang.org/x/sync/singleflight"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/auth/credential"
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

	// quarantined holds accounts that are permanently disabled (e.g. Terms
	// of Service violation), mapped to the quarantine reason.
	quarantined map[string]string

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

	// fallbackDefault reports that fallback is a default credential
	// source (not a test/caller-supplied one), and fallbackActive is the
	// registry's active account when it was built. The source caches the
	// credential it read, so an in-session switch has to rebuild it —
	// otherwise a long-lived router (the proxy's, the daemon's) keeps
	// talking as the account it started with until the process restarts.
	fallbackDefault bool
	fallbackActive  string

	// newFallback builds the default credential source. It is a field so
	// a test can answer without the machine's keychain.
	newFallback func() func(context.Context) (string, error)

	// policyOverride overrides the global Policy() call.
	policyOverride func() *accountpool.Config

	// candidatesOverride overrides CandidateAccounts for testing.
	candidatesOverride func(ctx context.Context) ([]accounts.Account, error)

	// lastRouted is the account the last turn was routed to.
	lastRouted string

	// log is where the routing's lines go.
	log io.Writer

	// tokenFlight deduplicates concurrent token refreshes per account.
	tokenFlight singleflight.Group

	// quotaFlight deduplicates concurrent quota fetches per account.
	quotaFlight singleflight.Group

	// prewarmCh receives account emails whose tokens need background refresh.
	prewarmCh chan string

	// prewarmOnce starts the prewarm background pipeline worker.
	prewarmOnce sync.Once

	// lastPrewarm is the last time candidate accounts were prewarmed.
	lastPrewarm time.Time

	// ctx controls the lifecycle of background workers (quota refresh, candidate prewarming).
	ctx context.Context

	// cancel terminates background workers when Close is called.
	cancel context.CancelFunc

	// workers tracks background goroutines so Close can drain them cleanly.
	workers sync.WaitGroup
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
	return NewRouter()
})

// Shared returns the process-wide router.
func Shared() *Router { return shared() }

// NewRouter returns a fresh Router instance.
func NewRouter() *Router {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Router{
		remaining:   map[string]quota{},
		excluded:    map[string]time.Time{},
		quarantined: map[string]string{},
		lastUsed:    map[string]time.Time{},
		refreshing:  map[string]bool{},
		log:         defaultLogWriter(),
		ctx:         ctx,
		cancel:      cancel,
	}
	if st, err := accounts.Load(); err == nil {
		for _, acc := range st.Accounts {
			if acc.Disabled {
				reason := acc.DisabledReason
				if reason == "" {
					reason = "account disabled / Terms of Service violation"
				}
				r.quarantined[strings.ToLower(strings.TrimSpace(acc.Email))] = reason
			}
		}
	}
	r.loadShared = readShared
	r.newFallback = credential.DefaultTokenSource
	r.fallback = r.newFallback()
	r.fallbackDefault = true
	r.fallbackActive = r.activeAccount()
	return r
}

func (r *Router) context() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx == nil {
		r.ctx, r.cancel = context.WithCancel(context.Background())
	}
	return r.ctx
}

// Close cancels background quota refresh and prewarming workers.
func (r *Router) Close() {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.workers.Wait()
}

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

// RouteResult represents an atomic routing decision and its ready-to-use access token.
type RouteResult struct {
	Email       string
	AccessToken string
	Decision    accountpool.Decision
}

// RouteTurn selects an account, ensures its credential is valid, and returns
// both the account identity and token atomically. This eliminates race conditions
// between selecting an account and querying LastRouted().
func (r *Router) RouteTurn(ctx context.Context, modelFamily ...string) (RouteResult, error) {
	policy := r.currentPolicy()
	if policy == nil || !policy.Enabled {
		tok, err := r.defaultToken(ctx)
		if err != nil {
			return RouteResult{}, err
		}
		active := r.currentAccountEmail()
		r.mu.Lock()
		r.lastRouted = active
		r.mu.Unlock()
		if slot, ok := ctx.Value(routeSlotKey{}).(*routeSlot); ok && slot != nil {
			slot.email = active
		}
		return RouteResult{Email: active, AccessToken: tok}, nil
	}

	family := ""
	if len(modelFamily) > 0 && modelFamily[0] != "" {
		family = modelFamily[0]
	} else {
		family = ModelFamilyFromContext(ctx)
	}

	decision, err := r.DecideForModel(ctx, family)
	if err != nil {
		return RouteResult{}, err
	}
	if decision.Email == "" {
		tok, err := r.defaultToken(ctx)
		if err != nil {
			return RouteResult{}, err
		}
		return RouteResult{Email: r.currentAccountEmail(), Decision: decision, AccessToken: tok}, nil
	}

	if slot, ok := ctx.Value(routeSlotKey{}).(*routeSlot); ok && slot != nil {
		slot.email = decision.Email
	}

	tok, err := r.tokenFor(ctx, decision.Email)
	if err != nil {
		if apierror.IsAccountDisabledErr(err) {
			r.Quarantine(decision.Email, apierror.AccountDisabledReason(err))
		} else if apierror.IsAuthFailureErr(err) {
			r.Exclude(decision.Email)
		}
		return RouteResult{}, err
	}
	r.logf("[account-pool] %s — %s", decision.Email, decision.Reason)

	r.mu.Lock()
	r.lastRouted = decision.Email
	r.mu.Unlock()

	r.mu.Lock()
	shouldPrewarm := time.Since(r.lastPrewarm) > 3*time.Minute
	if shouldPrewarm {
		r.lastPrewarm = time.Now()
	}
	r.mu.Unlock()
	if shouldPrewarm {
		go r.PrewarmCandidates(context.Background())
	}

	return RouteResult{
		Email:       decision.Email,
		AccessToken: tok.Token.AccessToken,
		Decision:    decision,
	}, nil
}

// TokenFunc returns the token source the CloudCode transport runs on.
//
// It is called once per turn, so the pool rotates per turn. When the
// routing is off it answers with the credential the transport's own token
// source would answer with — the same cascade, not a copy of it.
func (r *Router) TokenFunc(ctx context.Context) (string, error) {
	res, err := r.RouteTurn(ctx)
	if err != nil {
		return "", err
	}
	return res.AccessToken, nil
}

// TokenFuncForModel returns a token function tailored for a specific model family.
func (r *Router) TokenFuncForModel(model string) func(context.Context) (string, error) {
	family := ModelFamily(model)
	return func(ctx context.Context) (string, error) {
		res, err := r.RouteTurn(ctx, family)
		if err != nil {
			return "", err
		}
		return res.AccessToken, nil
	}
}

// SetPolicy installs a policy resolver that overrides the global Policy() read.
// A proxy or test uses this to supply a custom or enabled policy.
func (r *Router) SetPolicy(fn func() *accountpool.Config) {
	r.mu.Lock()
	r.policyOverride = fn
	r.mu.Unlock()
	if fn != nil {
		if cfg := fn(); cfg != nil && cfg.Enabled {
			go r.PrewarmCandidates(context.Background())
		}
	}
}

// currentPolicy returns the effective policy for this router.
func (r *Router) currentPolicy() *accountpool.Config {
	r.mu.Lock()
	fn := r.policyOverride
	r.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return Policy()
}

// SetLoadShared sets the function that reads the shared credential.
// Tests use this so they do not read the host machine's keychain.
func (r *Router) SetLoadShared(fn func() (*auth.AntigravityToken, string, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadShared = fn
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
	// Read the account before taking the lock: the registry read is I/O,
	// and the lock guards the router's own state only.
	active := r.activeAccount()
	r.mu.Lock()
	r.fallback = src
	// The source was built for the account the registry names right now.
	// Recording it lets defaultToken tell "still current" from "the user
	// switched accounts without rebuilding the transport".
	r.fallbackActive = active
	r.mu.Unlock()
}

// defaultToken answers with the transport's own credential.
//
// The source is cached like the transport caches it: a source reads the
// keychain once and keeps the result. The cache is dropped when the
// registry's active account changes, which is how an in-session
// `/accounts` switch reaches a long-lived router that was never rebuilt
// (the proxy's, a daemon's) — without this, only a process restart
// would pick the new credential up.
func (r *Router) defaultToken(ctx context.Context) (string, error) {
	active := r.activeAccount()

	r.mu.Lock()
	fallback := r.fallback
	if r.fallbackDefault && active != "" && !strings.EqualFold(active, r.fallbackActive) {
		fallback = r.newFallback()
		r.fallback = fallback
		r.fallbackActive = active
	}
	if fallback == nil {
		fallback = r.newFallback()
		r.fallback = fallback
		if r.fallbackDefault {
			r.fallbackActive = active
		}
	}
	r.mu.Unlock()
	return fallback(ctx)
}

// Decide asks the policy which account the next turn should use.
//
// It is the decision the token function makes, without the credential:
// what is left is the account's own business.
func (r *Router) Decide(ctx context.Context) (accountpool.Decision, error) {
	return r.DecideForModel(ctx, "")
}

// DecideForModel asks the policy which account the next turn should use for a specific model family.
func (r *Router) DecideForModel(ctx context.Context, modelFamily string) (accountpool.Decision, error) {
	policy := r.currentPolicy()
	if policy == nil || !policy.Enabled {
		return accountpool.Decision{}, errors.New("accountpool: the routing is off")
	}
	cfg := policy.Normalised()

	states, err := r.statesForModel(ctx, cfg, modelFamily)
	if err != nil {
		return accountpool.Decision{}, err
	}

	r.mu.Lock()
	cursor := r.cursor
	r.mu.Unlock()

	decision := accountpool.Choose(cfg, states, cursor)

	r.mu.Lock()
	r.cursor = decision.NextCursor
	r.lastRouted = decision.Email
	if decision.Email != "" {
		r.lastUsed[decision.Email] = time.Now()
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

// Exclude puts an account out of the pool globally for the policy's cooldown.
//
// The runtime calls it when the server says the account's quota is spent:
// the next turn goes to another account, and this one comes back when its
// cooldown passes.
func (r *Router) Exclude(email string) {
	r.ExcludeForModel(email, "")
}

// ExcludeForModel puts an account out of the pool for a specific model family for the policy's cooldown.
// If modelFamily is empty, the account is excluded globally across all model families.
func (r *Router) ExcludeForModel(email string, modelFamily string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	family := strings.ToLower(strings.TrimSpace(modelFamily))
	key := exclusionKey(email, family)
	now := time.Now()
	r.mu.Lock()
	r.excluded[key] = now
	r.remaining[quotaKey(email, family)] = quota{remaining: 0, known: true, fetchedAt: now}
	r.mu.Unlock()
	if family != "" {
		r.logf("[account-pool] %s is out of %s quota; cooling down", email, family)
	} else {
		r.logf("[account-pool] %s is out of quota; cooling down", email)
	}
}

// Quarantine permanently marks an account as disabled (e.g. Terms of Service
// violation) and removes it from active rotation in the pool.
func (r *Router) Quarantine(email, reason string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	norm := strings.ToLower(email)
	if reason == "" {
		reason = "Terms of Service violation / account service disabled"
	}
	now := time.Now()
	r.mu.Lock()
	r.quarantined[norm] = reason
	r.excluded[norm] = now.Add(100 * 365 * 24 * time.Hour)
	r.mu.Unlock()

	r.logf("[account-pool] %s is quarantined: %s", email, reason)
	_ = accounts.MarkDisabled(email, reason)
}

// IsQuarantined reports whether an account has been quarantined, and the reason.
func (r *Router) IsQuarantined(email string) (bool, string) {
	norm := strings.ToLower(strings.TrimSpace(email))
	if norm == "" {
		return false, ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	reason, ok := r.quarantined[norm]
	return ok, reason
}

// Unquarantine clears the quarantine status of an account.
func (r *Router) Unquarantine(email string) {
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	norm := strings.ToLower(email)
	r.mu.Lock()
	delete(r.quarantined, norm)
	delete(r.excluded, norm)
	r.mu.Unlock()
	_ = accounts.ClearDisabled(email)
}

// IsExcluded reports whether an account is currently in cooldown or quarantined.
// If modelFamily is supplied, it checks both global cooldown and model-family cooldown.
func (r *Router) IsExcluded(email string, modelFamily ...string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	norm := strings.ToLower(email)
	r.mu.Lock()
	if _, isQ := r.quarantined[norm]; isQ {
		r.mu.Unlock()
		return true
	}
	r.mu.Unlock()
	family := ""
	if len(modelFamily) > 0 {
		family = strings.ToLower(strings.TrimSpace(modelFamily[0]))
	}
	cooldown := accountpool.DefaultCooldown
	if r.policyOverride != nil {
		if cfg := r.policyOverride(); cfg != nil {
			cooldown = cfg.CooldownDuration()
		}
	} else if cfg := Policy(); cfg != nil {
		cooldown = cfg.CooldownDuration()
	}

	r.mu.Lock()
	isExc, expired := r.isExcludedLocked(email, family, cooldown, time.Now())
	r.mu.Unlock()

	for _, exp := range expired {
		r.refreshQuota(exp)
	}
	return isExc
}

func (r *Router) isExcludedLocked(email, family string, cooldown time.Duration, now time.Time) (bool, []string) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	fam := strings.ToLower(strings.TrimSpace(family))
	var expired []string
	isExc := false

	// Check global exclusion
	globalKey := normEmail
	if until, ok := r.excluded[globalKey]; ok {
		if now.Before(until.Add(cooldown)) {
			isExc = true
		} else {
			delete(r.excluded, globalKey)
			expired = append(expired, normEmail)
		}
	}

	// Check family-specific exclusion if family is specified
	if fam != "" {
		famKey := normEmail + "#" + fam
		if until, ok := r.excluded[famKey]; ok {
			if now.Before(until.Add(cooldown)) {
				isExc = true
			} else {
				delete(r.excluded, famKey)
				expired = append(expired, normEmail)
			}
		}
	}

	return isExc, expired
}

func exclusionKey(email, family string) string {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	fam := strings.ToLower(strings.TrimSpace(family))
	if fam == "" {
		return normEmail
	}
	return normEmail + "#" + fam
}

// SetCandidateAccounts overrides CandidateAccounts retrieval for testing.
func (r *Router) SetCandidateAccounts(fn func(ctx context.Context) ([]accounts.Account, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidatesOverride = fn
}

// CandidateAccounts returns registered accounts that are permitted by the current policy.
func (r *Router) CandidateAccounts(ctx context.Context) ([]accounts.Account, error) {
	r.mu.Lock()
	fn := r.candidatesOverride
	r.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	st, err := accounts.Load()
	if err != nil {
		return nil, err
	}
	policy := r.currentPolicy()
	if policy == nil {
		return st.Accounts, nil
	}
	cfg := policy.Normalised()
	var out []accounts.Account
	for _, acc := range st.Accounts {
		if cfg.Allows(acc.Email) {
			out = append(out, acc)
		}
	}
	return out, nil
}

// PrewarmCandidates proactively refreshes credentials for all accounts eligible under the current policy,
// warming OAuth tokens in the background so turn transitions avoid refresh latency.
func (r *Router) PrewarmCandidates(ctx context.Context) {
	policy := r.currentPolicy()
	if policy == nil || !policy.Enabled {
		return
	}
	candidates, err := r.CandidateAccounts(ctx)
	if err != nil || len(candidates) == 0 {
		return
	}
	for _, acc := range candidates {
		email := strings.TrimSpace(acc.Email)
		if email == "" {
			continue
		}
		if isQ, _ := r.IsQuarantined(email); isQ || acc.Disabled {
			continue
		}
		r.enqueuePrewarm(email)
	}
}

// states gathers the pool's state for the policy to choose from.
func (r *Router) states(ctx context.Context, cfg accountpool.Config) ([]accountpool.State, error) {
	return r.statesForModel(ctx, cfg, "")
}

// statesForModel gathers the pool's state tailored for a specific model family.
func (r *Router) statesForModel(ctx context.Context, cfg accountpool.Config, modelFamily string) ([]accountpool.State, error) {
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
	var toRefresh []string
	out := make([]accountpool.State, 0, len(st.Accounts))
	for _, acc := range st.Accounts {
		email := strings.TrimSpace(acc.Email)
		if email == "" {
			continue
		}
		remaining, known := r.quotaForModel(ctx, cfg, acc, modelFamily)
		r.mu.Lock()
		excluded, expired := r.isExcludedLocked(email, modelFamily, cfg.CooldownDuration(), now)
		qReason, isQuarantined := r.quarantined[strings.ToLower(email)]
		if !isQuarantined && acc.Disabled {
			isQuarantined = true
			qReason = acc.DisabledReason
		}
		lastUsed := r.lastUsed[email]
		r.mu.Unlock()
		if len(expired) > 0 {
			toRefresh = append(toRefresh, expired...)
		}
		out = append(out, accountpool.State{
			Email:            email,
			Remaining:        remaining,
			Known:            known,
			Current:          strings.EqualFold(email, current),
			Excluded:         excluded,
			Quarantined:      isQuarantined,
			QuarantineReason: qReason,
			LastUsed:         lastUsed,
		})
	}
	for _, exp := range toRefresh {
		r.refreshQuota(exp)
	}
	return out, nil
}

func quotaKey(email, family string) string {
	return strings.ToLower(strings.TrimSpace(email)) + "#" + strings.ToLower(strings.TrimSpace(family))
}

// quotaFor returns an account's remaining quota across all models.
func (r *Router) quotaFor(ctx context.Context, cfg accountpool.Config, acc accounts.Account) (float64, bool) {
	return r.quotaForModel(ctx, cfg, acc, "")
}

// quotaForModel returns an account's remaining quota for a given model family.
func (r *Router) quotaForModel(ctx context.Context, cfg accountpool.Config, acc accounts.Account, modelFamily string) (float64, bool) {
	email := acc.Email
	key := quotaKey(email, modelFamily)

	r.mu.Lock()
	cached, haveCached := r.remaining[key]
	r.mu.Unlock()

	if haveCached && time.Since(cached.fetchedAt) < cfg.RefreshDuration() {
		return cached.remaining, cached.known
	}

	// The registry's own snapshot is the seed: it costs nothing to read
	// and the panel keeps it fresh.
	if acc.Quota != nil {
		if rem, known := remainingForModelFamily(acc.Quota, modelFamily); known {
			r.mu.Lock()
			if !haveCached || acc.Quota.FetchedAt.After(cached.fetchedAt) {
				r.remaining[key] = quota{remaining: rem, known: true, fetchedAt: acc.Quota.FetchedAt}
				cached = r.remaining[key]
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

// refreshQuota reads one account's quota in the background, deduplicated with quotaFlight.
func (r *Router) refreshQuota(email string) {
	if r.context().Err() != nil {
		return
	}
	r.mu.Lock()
	if r.refreshing[email] {
		r.mu.Unlock()
		return
	}
	r.refreshing[email] = true
	r.mu.Unlock()

	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		_, _, _ = r.quotaFlight.Do(email, func() (any, error) {
			defer func() {
				r.mu.Lock()
				delete(r.refreshing, email)
				r.mu.Unlock()
			}()

			ctx, cancel := context.WithTimeout(r.context(), 20*time.Second)
			defer cancel()
			summary, err := accounts.FetchQuota(ctx, email)
			if err != nil {
				r.logf("[account-pool] the quota of %s could not be read: %v", email, err)
				return nil, err
			}
			snap := summary.Snapshot()
			remAll, knownAll := remainingForModelFamily(snap, "")
			remGemini, knownGemini := remainingForModelFamily(snap, "gemini")
			remClaude, knownClaude := remainingForModelFamily(snap, "claude")
			now := time.Now()
			r.mu.Lock()
			r.remaining[quotaKey(email, "")] = quota{remaining: remAll, known: knownAll, fetchedAt: now}
			r.remaining[quotaKey(email, "gemini")] = quota{remaining: remGemini, known: knownGemini, fetchedAt: now}
			r.remaining[quotaKey(email, "claude")] = quota{remaining: remClaude, known: knownClaude, fetchedAt: now}
			r.mu.Unlock()
			if knownAll {
				r.logf("[account-pool] %s has %.1f%% left", email, remAll*100)
			}
			return remAll, nil
		})
	}()
}

func (r *Router) startPrewarmPipeline() {
	r.prewarmOnce.Do(func() {
		r.prewarmCh = make(chan string, 32)
		r.workers.Add(1)
		go func() {
			defer r.workers.Done()
			for {
				select {
				case <-r.context().Done():
					return
				case email, ok := <-r.prewarmCh:
					if !ok {
						return
					}
					ctx, cancel := context.WithTimeout(r.context(), 25*time.Second)
					_, _ = r.tokenForDirect(ctx, email)
					cancel()
				}
			}
		}()
	})
}

func (r *Router) enqueuePrewarm(email string) {
	if email == "" || r.context().Err() != nil {
		return
	}
	r.startPrewarmPipeline()
	select {
	case r.prewarmCh <- email:
	default:
	}
}

// tokenFor returns the credential of the named account, refreshed when it
// is about to expire, deduplicated with singleflight.
func (r *Router) tokenFor(ctx context.Context, email string) (*auth.AntigravityToken, error) {
	val, err, _ := r.tokenFlight.Do(email, func() (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return r.tokenForDirect(opCtx, email)
	})
	if err != nil {
		return nil, err
	}
	tok := val.(*auth.AntigravityToken)
	if exp := tok.ExpiryTime(); !exp.IsZero() {
		rem := time.Until(exp)
		// If expiring soon (between 1 and 10 minutes left), enqueue background prewarm
		if rem < 10*time.Minute && rem > 1*time.Minute {
			r.enqueuePrewarm(email)
		}
	}
	return tok, nil
}

// tokenForDirect executes token acquisition and quiet refresh.
func (r *Router) tokenForDirect(ctx context.Context, email string) (*auth.AntigravityToken, error) {
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

// currentAccountEmail names the account the shared credential belongs to.
// The registry's Active field is the first answer; when it is empty the
// credential itself is consulted, the same cascade accounts.Current uses.
func (r *Router) currentAccountEmail() string {
	if active := r.activeAccount(); active != "" {
		return active
	}
	if _, email := accounts.Current(); email != "" {
		return email
	}
	return ""
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

// ModelFamily classifies a model name into its quota family ("claude" vs "gemini").
// If the model is empty or unrecognized, it returns an empty string (meaning global quota).
func ModelFamily(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "claude") || strings.Contains(m, "gpt"):
		return "claude"
	case strings.Contains(m, "gemini"):
		return "gemini"
	default:
		return ""
	}
}

// remainingForModelFamily returns the lowest remaining fraction for a specific model family.
// If family is empty, it defaults to Gemini models. Claude and GPT groups are ignored for
// Gemini routing.
func remainingForModelFamily(snap *auth.QuotaSnapshot, family string) (float64, bool) {
	if snap == nil {
		return 0, false
	}
	target := strings.ToLower(strings.TrimSpace(family))
	if target == "" {
		target = "gemini"
	}
	found := false
	lowest := 0.0
	for _, g := range snap.Groups {
		gName := strings.ToLower(g.Name)
		if target == "claude" && !strings.Contains(gName, "claude") && !strings.Contains(gName, "gpt") {
			continue
		}
		if target == "gemini" && !strings.Contains(gName, "gemini") {
			continue
		}
		for _, b := range g.Buckets {
			if !found || b.RemainingFraction < lowest {
				lowest = b.RemainingFraction
				found = true
			}
		}
	}
	if !found {
		// Fallback for snapshots where group names do not explicitly mention the model family
		// (e.g. generic single-group test fixtures), skipping claude/gpt if targeting gemini.
		for _, g := range snap.Groups {
			gName := strings.ToLower(g.Name)
			if target == "gemini" && (strings.Contains(gName, "claude") || strings.Contains(gName, "gpt")) {
				continue
			}
			for _, b := range g.Buckets {
				if !found || b.RemainingFraction < lowest {
					lowest = b.RemainingFraction
					found = true
				}
			}
		}
	}
	return lowest, found
}

// remainingOf returns the lowest remaining fraction across a snapshot's
// buckets.
//
// The lowest figure is the one that decides whether the account can serve
// a turn: the five-hour window runs out long before the weekly one.
func remainingOf(snap *auth.QuotaSnapshot) (float64, bool) {
	return remainingForModelFamily(snap, "")
}

type modelFamilyKey struct{}
type routeSlotKey struct{}

type routeSlot struct {
	email string
}

// WithModel returns a context carrying the target model family for routing.
func WithModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, modelFamilyKey{}, ModelFamily(model))
}

// ModelFamilyFromContext extracts the model family if attached to ctx.
func ModelFamilyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(modelFamilyKey{}).(string); ok {
		return v
	}
	return ""
}

func withRouteSlot(ctx context.Context, slot *routeSlot) context.Context {
	return context.WithValue(ctx, routeSlotKey{}, slot)
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
// Failover policy:
//   - When an account hits a Terms of Service violation / disabled service, it
//     is permanently Quarantined and excluded from future turns. If attempts remain,
//     the turn seamlessly fails over to the next healthy account in the pool.
//   - When an account hits an authentication failure (401, expired/revoked token),
//     it is excluded and the turn fails over to the next healthy account.
//   - When an account runs out of quota and SwitchOnQuotaError is enabled, it is
//     excluded for the model and the turn fails over to the next healthy account.
//   - When an in-stream error frame arrives on chunk 0 before user-visible content
//     has been emitted, transparent failover switches to the next healthy account
//     without erroring or interrupting the turn.
func (c *routedClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	policy := Policy()
	if policy == nil || !policy.Enabled {
		// The routing is off: the transport answers for itself.
		// However, if the active account hits a permanent ToS violation, quarantine it immediately.
		var slot routeSlot
		callCtx := withRouteSlot(ctx, &slot)
		out, err := c.inner.Stream(callCtx, req)
		activeEmail := slot.email
		if activeEmail == "" {
			activeEmail = c.router.currentAccountEmail()
		}
		if err != nil {
			if apierror.IsAccountDisabledErr(err) {
				c.router.Quarantine(activeEmail, apierror.AccountDisabledReason(err))
			}
			return nil, err
		}
		return c.watchPrefixedList(ctx, nil, backend.StreamChunk{}, out, activeEmail, ""), nil
	}
	cfg := policy.Normalised()

	family := ModelFamily(req.Model)
	maxAttempts := cfg.MaxAttempts
	if cands, err := c.router.CandidateAccounts(ctx); err == nil && len(cands) > maxAttempts {
		maxAttempts = len(cands)
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var slot routeSlot
		callCtx := withRouteSlot(WithModel(ctx, req.Model), &slot)
		out, err := c.inner.Stream(callCtx, req)
		if err != nil {
			lastErr = err
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			email := slot.email
			if email == "" {
				email = c.router.LastRouted()
			}
			if apierror.IsAccountDisabledErr(err) {
				reason := apierror.AccountDisabledReason(err)
				c.router.Quarantine(email, reason)
				c.router.logf("[account-pool] %s is quarantined: %s; trying another account", email, reason)
				if attempt+1 < maxAttempts {
					continue
				}
				return nil, err
			}
			if apierror.IsAuthFailureErr(err) {
				c.router.Exclude(email)
				c.router.logf("[account-pool] %s authentication failed; trying another account", email)
				if attempt+1 < maxAttempts {
					continue
				}
				return nil, err
			}
			if apierror.IsQuotaReachedErr(err) && cfg.SwitchOnQuotaErrorEnabled() {
				c.router.ExcludeForModel(email, family)
				if family != "" {
					c.router.logf("[account-pool] %s is out of %s quota; trying another account", email, family)
				} else {
					c.router.logf("[account-pool] %s is out of quota; trying another account", email)
				}
				if attempt+1 < maxAttempts {
					continue
				}
				return nil, err
			}
			return nil, err
		}

		// Peek stream until first non-empty content chunk or error chunk is received.
		var buffered []backend.StreamChunk
		var firstNonEmpty backend.StreamChunk
		hasContent := false
		gotError := false

		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case chunk, ok := <-out:
				if !ok {
					return replayBuffered(buffered), nil
				}
				if chunk.Error != nil {
					gotError = true
					firstNonEmpty = chunk
					break
				}
				// Empty preamble chunks without content/meta/tool-calls/done
				if chunk.Text == "" && len(chunk.ToolCalls) == 0 && len(chunk.ThinkingBlocks) == 0 && !chunk.Done {
					buffered = append(buffered, chunk)
					continue
				}
				hasContent = true
				firstNonEmpty = chunk
				break
			}
			if gotError || hasContent {
				break
			}
		}

		if gotError {
			// Drain remaining in background to avoid leaking upstream goroutine
			go func(ch <-chan backend.StreamChunk) {
				for range ch {
				}
			}(out)

			email := slot.email
			if email == "" {
				email = c.router.LastRouted()
			}
			lastErr = firstNonEmpty.Error

			if apierror.IsAccountDisabledErr(firstNonEmpty.Error) {
				reason := apierror.AccountDisabledReason(firstNonEmpty.Error)
				c.router.Quarantine(email, reason)
				c.router.logf("[account-pool] %s stream quarantined: %s; switching account (attempt %d/%d)", email, reason, attempt+1, maxAttempts)
				if attempt+1 < maxAttempts {
					continue
				}
				return errorChunkChan(firstNonEmpty), nil
			}
			if apierror.IsAuthFailureErr(firstNonEmpty.Error) {
				c.router.Exclude(email)
				c.router.logf("[account-pool] %s stream authentication failed; switching account (attempt %d/%d)", email, attempt+1, maxAttempts)
				if attempt+1 < maxAttempts {
					continue
				}
				return errorChunkChan(firstNonEmpty), nil
			}
			if apierror.IsQuotaReachedErr(firstNonEmpty.Error) && cfg.SwitchOnQuotaErrorEnabled() {
				c.router.ExcludeForModel(email, family)
				if family != "" {
					c.router.logf("[account-pool] %s stream is out of %s quota; switching account (attempt %d/%d)", email, family, attempt+1, maxAttempts)
				} else {
					c.router.logf("[account-pool] %s stream is out of quota; switching account (attempt %d/%d)", email, attempt+1, maxAttempts)
				}
				if attempt+1 < maxAttempts {
					continue
				}
				return errorChunkChan(firstNonEmpty), nil
			}
			return errorChunkChan(firstNonEmpty), nil
		}

		if hasContent {
			return c.watchPrefixedList(ctx, buffered, firstNonEmpty, out, slot.email, family), nil
		}
	}
	return nil, lastErr
}

// replayBuffered yields buffered chunks and closes.
func replayBuffered(buffered []backend.StreamChunk) <-chan backend.StreamChunk {
	ch := make(chan backend.StreamChunk, len(buffered))
	for _, b := range buffered {
		ch <- b
	}
	close(ch)
	return ch
}

// errorChunkChan emits a single error chunk and closes.
func errorChunkChan(chunk backend.StreamChunk) <-chan backend.StreamChunk {
	ch := make(chan backend.StreamChunk, 1)
	ch <- chunk
	close(ch)
	return ch
}

// watchPrefixedList forwards the buffered preamble, head chunk, and subsequent stream chunks,
// watching for mid-stream account errors to quarantine or exclude for subsequent turns.
func (c *routedClient) watchPrefixedList(ctx context.Context, prefix []backend.StreamChunk, head backend.StreamChunk, in <-chan backend.StreamChunk, email, family string) <-chan backend.StreamChunk {
	out := make(chan backend.StreamChunk)
	go func() {
		defer close(out)
		for _, p := range prefix {
			select {
			case out <- p:
			case <-ctx.Done():
				return
			}
		}
		select {
		case out <- head:
		case <-ctx.Done():
			return
		}
		if head.Done {
			go func() {
				for range in {
				}
			}()
			return
		}
		for chunk := range in {
			if chunk.Error != nil {
				acc := email
				if acc == "" {
					acc = c.router.LastRouted()
				}
				if apierror.IsAccountDisabledErr(chunk.Error) {
					c.router.Quarantine(acc, apierror.AccountDisabledReason(chunk.Error))
				} else if apierror.IsAuthFailureErr(chunk.Error) {
					c.router.Exclude(acc)
				} else if apierror.IsQuotaReachedErr(chunk.Error) {
					c.router.ExcludeForModel(acc, family)
				}
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				go func() {
					for range in {
					}
				}()
				return
			}
		}
	}()
	return out
}

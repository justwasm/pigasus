// Package accountpool — the account-routing policy: the file format and
// the decision rules.
//
// # What it is
//
// The clone keeps a *registry* of the Google accounts the user signed in
// to (`internal/accounts`, `~/.config/openagy/accounts.json`). A turn is
// served by one of them. This package answers the question "which one?" —
// it holds the *policy* (the configuration file's section) and the *rules*
// (which account a turn picks, given the state of the pool).
//
// It is a pure package: it reads no file, calls no service and knows
// nothing about the registry. The runtime that gathers the state and
// applies the decision lives in `internal/accountrouting`; the file the
// policy is read from is `~/.config/openagy/config.json`, whose only
// reader is the `endpoints` package.
//
// # The configuration section
//
// The policy lives beside the endpoint overrides, under "account_pool":
//
//	{
//	  "endpoints": { ... },
//	  "account_pool": {
//	    "enabled": true,
//	    "strategy": "quota_aware",
//	    "threshold": 0.15,
//	    "refresh_interval": "2m",
//	    "cooldown": "5m",
//	    "accounts": ["a@example.com", "b@example.com"],
//	    "max_attempts": 3,
//	    "switch_on_quota_error": true
//	  }
//	}
//
// The section is **off by default**: a pool with one account and no
// policy behaves exactly as it did before this package existed.
//
// # The strategies
//
// | strategy | what it does |
// |---|---|
// | `sticky` | the account the session is signed in with, until its quota drops below the threshold |
// | `round_robin` | the next account of the pool, in turn |
// | `quota_aware` | the account with the most quota left |
// | `failover` | the account the session is signed in with, until a quota error comes back |
//
// The difference between `sticky` and `failover` is *what moves the turn*:
// the threshold moves it in `sticky`, the server's quota error moves it in
// `failover`.
//
// # The threshold
//
// One number — the fraction of a quota window an account must still have
// — is the eligibility floor. It is a *preference*, not a wall: when every
// account in the pool is below it, the one with the most left is used,
// because a turn with no account cannot be served at all.
package accountpool

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Strategy names the way a turn picks its account.
type Strategy string

// The strategies the policy knows.
const (
	// StrategySticky keeps the account the session is signed in with
	// and leaves it when its quota drops below the threshold.
	StrategySticky Strategy = "sticky"

	// StrategyRoundRobin hands the turns to the pool in turn.
	StrategyRoundRobin Strategy = "round_robin"

	// StrategyQuotaAware hands every turn to the account with the most
	// quota left.
	StrategyQuotaAware Strategy = "quota_aware"

	// StrategyFailover keeps the account the session is signed in with
	// and leaves it when the server says the quota is spent.
	StrategyFailover Strategy = "failover"

	// StrategyAggregate treats all accounts in the pool as a single unified
	// virtual account, routing each turn to the account with the most quota
	// left while presenting aggregate quota metrics to callers.
	StrategyAggregate Strategy = "aggregate"
)

// The defaults the policy falls back to.
const (
	// DefaultStrategy is the strategy a policy without one uses.
	DefaultStrategy = StrategySticky

	// DefaultRefreshInterval is how often a quota figure is read again.
	DefaultRefreshInterval = time.Minute

	// DefaultCooldown is how long an account stays out of the pool
	// after the server said its quota is spent.
	DefaultCooldown = 5 * time.Minute

	// DefaultMaxAttempts is how many accounts one turn may try. One
	// attempt is the behaviour without the policy: the turn fails, and
	// the runtime's own retry decides what happens next.
	DefaultMaxAttempts = 1
)

// Strategies returns the known strategies, in the order the documentation
// lists them.
func Strategies() []Strategy {
	return []Strategy{
		StrategySticky,
		StrategyRoundRobin,
		StrategyQuotaAware,
		StrategyFailover,
		StrategyAggregate,
	}
}

// ParseStrategy maps a configured name onto a strategy.
//
// The name is matched case-insensitively, and a few spellings are
// accepted for the ones a user is likely to type ("rr", "roundrobin",
// "quota", "least_used"). An unknown name falls back to
// DefaultStrategy and reports false, so the caller can warn without
// refusing to start.
func ParseStrategy(name string) (Strategy, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "sticky", "default", "current", "active":
		return DefaultStrategy, true
	case "round_robin", "roundrobin", "round-robin", "rr", "rotate":
		return StrategyRoundRobin, true
	case "quota_aware", "quotaaware", "quota-aware", "quota", "least_used",
		"least-used", "most_quota":
		return StrategyQuotaAware, true
	case "failover", "fail_over", "fail-over":
		return StrategyFailover, true
	case "aggregate", "agg", "unified", "super", "pool_aggregate":
		return StrategyAggregate, true
	}
	return DefaultStrategy, false
}

// Config is the "account_pool" section of the configuration file.
//
// Every field has a default, so an empty section is a valid one: it
// describes a pool that is switched off.
type Config struct {
	// Enabled turns the routing on. It is off by default: the routing
	// changes which credential a turn runs on, so it is the user's
	// explicit choice.
	Enabled bool `json:"enabled,omitempty"`

	// Strategy is the way a turn picks its account. See Strategies.
	Strategy Strategy `json:"strategy,omitempty"`

	// Threshold is the fraction of a quota window an account must still
	// have to be eligible. It is clamped to 0..1; zero means "every
	// account is eligible".
	Threshold float64 `json:"threshold,omitempty"`

	// RefreshInterval is how often a quota figure is read again. It
	// accepts Go's duration syntax ("30s", "2m", "1h").
	RefreshInterval string `json:"refresh_interval,omitempty"`

	// Cooldown is how long an account stays out of the pool after the
	// server said its quota is spent.
	Cooldown string `json:"cooldown,omitempty"`

	// Accounts restricts the pool to the named accounts. An empty list
	// means every account in the registry.
	Accounts []string `json:"accounts,omitempty"`

	// MaxAttempts is how many accounts one turn may try when the server
	// answers with a quota error.
	MaxAttempts int `json:"max_attempts,omitempty"`

	// SwitchOnQuotaError moves the next turn to another account after a
	// quota error. It is on by default; set it to false to keep the turn
	// on the account that failed.
	SwitchOnQuotaError *bool `json:"switch_on_quota_error,omitempty"`

	// ProxyBalance enables multi-account load balancing in agyproxy (pigasus agy).
	ProxyBalance bool `json:"proxy_balance,omitempty"`

	// Aggregate enables virtual unified account aggregation in agyproxy.
	// When enabled, proxy intercepts quota and codeassist queries and presents
	// a unified view across all pool accounts, transparently rotating turns.
	Aggregate bool `json:"aggregate,omitempty"`
}

// Normalised returns the policy with its defaults filled in.
//
// A malformed value never keeps the policy from being used: an
// unparsable duration falls back to the default, an unknown strategy to
// DefaultStrategy, a threshold outside 0..1 is clamped into it, and a
// negative attempt count becomes one.
func (c Config) Normalised() Config {
	out := c
	if s, ok := ParseStrategy(string(c.Strategy)); ok {
		out.Strategy = s
	} else {
		out.Strategy = DefaultStrategy
	}
	if out.Strategy == StrategyAggregate {
		out.Aggregate = true
	}
	if out.Threshold < 0 {
		out.Threshold = 0
	}
	if out.Threshold > 1 {
		out.Threshold = 1
	}
	if out.MaxAttempts < 1 {
		out.MaxAttempts = DefaultMaxAttempts
	}
	out.RefreshInterval = normaliseDuration(c.RefreshInterval, DefaultRefreshInterval)
	out.Cooldown = normaliseDuration(c.Cooldown, DefaultCooldown)
	allow := make([]string, 0, len(c.Accounts))
	seen := map[string]bool{}
	for _, a := range c.Accounts {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		key := strings.ToLower(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		allow = append(allow, a)
	}
	out.Accounts = allow
	return out
}

// RefreshDuration is how often a quota figure is read again.
func (c Config) RefreshDuration() time.Duration {
	return parseDuration(c.RefreshInterval, DefaultRefreshInterval)
}

// CooldownDuration is how long an account stays out of the pool after a
// quota error.
func (c Config) CooldownDuration() time.Duration {
	return parseDuration(c.Cooldown, DefaultCooldown)
}

// SwitchOnQuotaErrorEnabled reports whether a quota error moves the next
// turn to another account. It is on unless the policy says otherwise.
func (c Config) SwitchOnQuotaErrorEnabled() bool {
	if c.SwitchOnQuotaError == nil {
		return true
	}
	return *c.SwitchOnQuotaError
}

// Allows reports whether the policy puts the named account in the pool.
// An empty allow-list allows every account.
func (c Config) Allows(email string) bool {
	email = strings.TrimSpace(email)
	if len(c.Accounts) == 0 {
		return email != ""
	}
	for _, a := range c.Accounts {
		if strings.EqualFold(a, email) {
			return true
		}
	}
	return false
}

// State is one account's state in the pool.
type State struct {
	// Email identifies the account.
	Email string

	// Remaining is the account's remaining quota as a fraction of the
	// window it is measured in — 0..1. It is the *lowest* figure across
	// the account's quota windows: the one that decides whether the
	// account can serve a turn right now.
	Remaining float64

	// Known reports whether Remaining is a measured figure. An account
	// whose quota has never been read has no figure, and the threshold
	// does not exclude it: a pool does not punish an account for not
	// having been asked.
	Known bool

	// Current marks the account the session is signed in with.
	Current bool

	// Excluded marks an account that must not be used: the server said
	// its quota is spent and its cooldown has not passed.
	Excluded bool

	// LastUsed is when the account last served a turn.
	LastUsed time.Time
}

// Skip records why an account was not eligible.
type Skip struct {
	// Email is the account that was passed over.
	Email string

	// Reason says why, in the words the log line uses.
	Reason string
}

// Decision is the outcome of a routing decision.
type Decision struct {
	// Email is the account the turn should use. It is empty only when
	// the pool itself is empty.
	Email string

	// Reason says why that account, in the words the log line uses.
	Reason string

	// NextCursor is where the round-robin rotation continues. It is the
	// index of the chosen account in the eligible list, so the next
	// decision starts after it.
	NextCursor int

	// Skipped lists the accounts that were passed over, with the reason.
	Skipped []Skip
}

// Choose applies the policy to the state of the pool and returns the
// account the turn should use.
//
// The policy never leaves the session without an account: when every
// account is ineligible the one with the most quota left is used anyway.
func Choose(cfg Config, states []State, cursor int) Decision {
	cfg = cfg.Normalised()
	decision := Decision{NextCursor: cursor}

	allowed := make([]State, 0, len(states))
	for _, s := range states {
		if strings.TrimSpace(s.Email) == "" {
			continue
		}
		if !cfg.Allows(s.Email) {
			decision.Skipped = append(decision.Skipped, Skip{Email: s.Email, Reason: "not in the pool's allow-list"})
			continue
		}
		allowed = append(allowed, s)
	}
	if len(allowed) == 0 {
		decision.Reason = "the pool is empty"
		return decision
	}

	eligible := make([]State, 0, len(allowed))
	for _, s := range allowed {
		switch {
		case s.Excluded:
			decision.Skipped = append(decision.Skipped, Skip{Email: s.Email, Reason: "in cooldown after a quota error"})
		case cfg.Strategy != StrategyFailover && s.Known && s.Remaining < cfg.Threshold:
			decision.Skipped = append(decision.Skipped, Skip{
				Email:  s.Email,
				Reason: fmt.Sprintf("below the threshold (%.1f%% left, %.1f%% needed)", s.Remaining*100, cfg.Threshold*100),
			})
		default:
			eligible = append(eligible, s)
		}
	}

	// Every account is out: use the one with the most left. A turn with
	// no account cannot be served at all, so the threshold is a
	// preference and not a wall.
	if len(eligible) == 0 {
		fallback := mostRemaining(allowed)
		decision.Email = fallback.Email
		decision.Reason = fmt.Sprintf("every account is out of quota; using the one with the most left (%.1f%%)", fallback.Remaining*100)
		return decision
	}

	switch cfg.Strategy {
	case StrategyRoundRobin:
		idx := cursor % len(eligible)
		if idx < 0 {
			idx = 0
		}
		chosen := eligible[idx]
		decision.Email = chosen.Email
		decision.NextCursor = (idx + 1) % len(eligible)
		decision.Reason = "round robin"
		return decision

	case StrategyQuotaAware:
		chosen := mostRemaining(eligible)
		decision.Email = chosen.Email
		decision.Reason = fmt.Sprintf("the most quota left (%.1f%%)", chosen.Remaining*100)
		return decision

	case StrategyAggregate:
		chosen := mostRemaining(eligible)
		decision.Email = chosen.Email
		decision.Reason = fmt.Sprintf("unified pool: most quota left (%.1f%%)", chosen.Remaining*100)
		return decision

	case StrategyFailover:
		if cur, ok := currentOf(eligible); ok {
			decision.Email = cur.Email
			decision.Reason = "the account the session is signed in with"
			return decision
		}
		chosen := mostRemaining(eligible)
		decision.Email = chosen.Email
		decision.Reason = fmt.Sprintf("the account the session is signed in with is out; using the one with the most left (%.1f%%)", chosen.Remaining*100)
		return decision

	default: // StrategySticky
		if cur, ok := currentOf(eligible); ok {
			decision.Email = cur.Email
			decision.Reason = "the account the session is signed in with"
			return decision
		}
		chosen := mostRemaining(eligible)
		decision.Email = chosen.Email
		decision.Reason = fmt.Sprintf("the account the session is signed in with is out; using the one with the most left (%.1f%%)", chosen.Remaining*100)
		return decision
	}
}

// mostRemaining returns the account with the most quota left.
//
// An account whose quota is unknown ranks behind every account that has a
// figure: the pool prefers what it knows. A tie goes to the account that
// served a turn longest ago, so a tie does not keep hammering the same
// account.
func mostRemaining(states []State) State {
	best := states[0]
	for _, s := range states[1:] {
		switch {
		case s.Known && !best.Known:
			best = s
		case s.Known == best.Known && s.Remaining > best.Remaining:
			best = s
		case s.Known == best.Known && s.Remaining == best.Remaining && s.LastUsed.Before(best.LastUsed):
			best = s
		}
	}
	return best
}

// currentOf returns the account the session is signed in with, if it is in
// the given list.
func currentOf(states []State) (State, bool) {
	for _, s := range states {
		if s.Current {
			return s, true
		}
	}
	return State{}, false
}

// normaliseDuration renders a duration the way the policy stores it: the
// canonical form of the parsed value, or the default when the text does
// not parse.
func normaliseDuration(raw string, fallback time.Duration) string {
	d := parseDuration(raw, fallback)
	return d.String()
}

// parseDuration parses a Go duration string, falling back to the default
// when the text does not parse.
func parseDuration(raw string, fallback time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// SortEmails sorts a list of account emails case-insensitively — the
// order the pool's accounts are listed in.
func SortEmails(emails []string) {
	sort.SliceStable(emails, func(i, j int) bool {
		return strings.ToLower(emails[i]) < strings.ToLower(emails[j])
	})
}

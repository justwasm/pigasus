// Package accountpool — the account-routing policy: the file format and
// the decision rules.
//
// # What it is
//
// The clone keeps a *registry* of the Google accounts the user signed in
// to (`internal/accounts`, `~/.openagy/accounts.json`). A turn is
// served by one of them. This package answers the question "which one?" —
// it holds the *policy* (the configuration file's section) and the *rules*
// (which account a turn picks, given the state of the pool).
//
// It is a pure package: it reads no file, calls no service and knows
// nothing about the registry. The runtime that gathers the state and
// applies the decision lives in `internal/accountrouting`; the file the
// policy is read from is `~/.openagy/config.json`, whose only
// reader is the `endpoints` package.
//
// # The configuration section
//
// The policy lives beside the endpoint overrides, under "account_pool":
//
//	{
//	  "account_pool": {
//	    "enabled": true,
//	    "threshold_5h": 0.05,
//	    "threshold_weekly": 0.10,
//	    "refresh_interval": "2m",
//	    "cooldown": "5m",
//	    "accounts": ["a@example.com", "b@example.com"],
//	    "max_attempts": 3,
//	    "switch_on_quota_error": true,
//	    "aggregate": { "enabled": true }
//	  }
//	}
//
// The section is **off by default** in the clone's own runtime: a pool with
// one account and no policy behaves exactly as it did before this package
// existed. The proxy is the exception — `pigasus agy` starts an aggregate
// pool, because a pool is the reason to start it.
//
// # The rule: one account, until its window runs down
//
// There is one way a turn picks its account, and no knob to change it.
//
// The pool stays on the account it is pinned to for as long as that account
// can serve a turn — its five-hour window is above the threshold, it is not
// in cooldown after a quota error, and it is not quarantined. Only then does
// it move, and it moves to the eligible account with the most of that window
// left. From that moment the new account is the pin.
//
// A pool with no pin yet — the first turn after the process started — starts
// on the account the session is signed in with when that account can serve a
// turn, and on the fullest one otherwise. That is the account the user is
// looking at, so the first turn is not a surprise.
//
// # Why there is no rotation choice
//
// A rotation is a way to *deliberately* spread turns across accounts:
// round-robin, or "the fullest account, every turn". Both answer two turns of
// one conversation with two different accounts, and a conversation that
// changes account mid-flight changes the plan, the project and the caches
// behind it. Worse, neither respects the resource that actually runs out: the
// five-hour window is per account, so spreading turns *faster* spends several
// accounts' windows at once instead of spending one and keeping the rest in
// reserve.
//
// The useful behaviour here is therefore not a strategy but a *sticky
// session*, and it is what the pool does. A configuration that still names
// one — `"strategy"` at the top level, or `"aggregate": {"strategy": …}` — is
// read and ignored; see the note in Config.
//
// # The two thresholds
//
// An account has two quota windows, and they are not interchangeable. The
// **five-hour** one refills several times a day: an account that spends it is
// usable again within hours. The **weekly** one refills once, and it is the
// scarce thing in a pool — spending the last of a week leaves an account out
// until the reset, however much of its day is left.
//
// So there are two floors, one per window:
//
//   - `threshold_5h` — how much of the five-hour window an account must still
//     have to serve the *next* turn.
//   - `threshold_weekly` — how much of the week it must still have to be worth
//     holding for the turns after that.
//
// An account is **eligible** only while both hold. A weekly window below its
// floor takes an account out even though its day looks fine: that is the case
// this pair exists for, because a pool that only watched the day would run an
// account into its weekly wall with no time to move.
//
// The floors are a *preference*, not a wall: when every account is below them,
// the one with the most left is used, because a turn with no account cannot be
// served at all.
//
// # Which account a move picks
//
// Turns are not spread; one account serves until it falls out (see above).
// When it does fall out, the pool picks the eligible account that is best for
// the days ahead, in this order:
//
//  1. a measured figure over an unmeasured one — the pool prefers what it
//     knows;
//  2. **more of the five-hour window** — that is what serves the next turn;
//  3. **more of the week** — of two accounts that are equally healthy today,
//     the one with more of the week left is the one to spend, because it can
//     carry the session longer before the next move.
//
// Step 3 only decides when step 2 is close, not when it is exact: two figures
// within `fiveHourTieBand` of each other count as equally healthy. Reading it
// strictly lexicographically would make the week almost never matter, since
// two accounts rarely have byte-identical five-hour figures.
//
// # The choice outlives a turn
//
// `ChooseWithPin` takes the account the previous turn used; `Choose` is the
// same rule with no memory, for a caller that has none to give.
package accountpool

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Aggregate is the pool's aggregate mode: the switch that makes the pool
// answer quota and plan-tier calls as one virtual account.
//
// **Writing the section is enough** — a section with no "enabled" is the mode
// on, because the only reason to write the section is to run the pool:
//
//	"account_pool": {
//	  "enabled": true,
//	  "threshold_5h": 0.05,
//	  "threshold_weekly": 0.10,
//	  "aggregate": {}
//	}
//
// The section used to carry a `strategy` — which account serves a turn, and
// when the pool moves. There is one such rule now (see the package doc), so
// the field is gone; a document that still names one parses, and the key is
// ignored, because `json.Unmarshal` drops the keys the struct does not have.
type Aggregate struct {
	// Enabled is the mode's switch. It is nil when the section does not say
	// — which means "on" for a caller that runs a pool (the proxy) and
	// "off" for one that does not (the clone's own runtime). Say false to
	// turn the mode off while keeping the pool's other settings on file.
	Enabled *bool `json:"enabled,omitempty"`
}

// Bool returns a pointer to b. It exists for the two optional booleans in the
// policy — the aggregate switch and "switch_on_quota_error" — where "absent"
// and "false" are different answers.
func Bool(b bool) *bool { return &b }

// The defaults the policy falls back to.
const (
	// DefaultRefreshInterval is how often a quota figure is read again.
	DefaultRefreshInterval = time.Minute

	// DefaultCooldown is how long an account stays out of the pool
	// after the server said its quota is spent.
	DefaultCooldown = 5 * time.Minute

	// DefaultMaxAttempts is how many accounts one turn may try. One
	// attempt is the behaviour without the policy: the turn fails, and
	// the runtime's own retry decides what happens next.
	DefaultMaxAttempts = 1

	// DefaultThresholdStep is the default delta (1%) applied when stepping
	// quota threshold values with arrow keys or interactive controls.
	DefaultThresholdStep = 0.01
)

// Config is the "account_pool" section of the configuration file.
//
// Every field has a default, so an empty section is a valid one: it
// describes a pool that is switched off.
type Config struct {
	// Enabled turns the routing on. It is off by default: the routing
	// changes which credential a turn runs on, so it is the user's
	// explicit choice.
	//
	// A section that exists but does not name it counts as on — see
	// RoutingEnabled. Writing a pool's threshold is asking for a pool.
	//
	// The field is always written, because "enabled": false has to survive
	// the round trip: omitting it would read back as "said nothing", which
	// is "on".
	Enabled bool `json:"enabled"`

	// enabledStated records whether the document named "enabled". It is what
	// tells "false" apart from "said nothing"; it is set by UnmarshalJSON and
	// never written back.
	enabledStated bool

	// ThresholdFiveHour is the fraction of the five-hour window an account
	// must still have to be eligible. It is clamped to 0..1; zero means "no
	// floor in this window".
	ThresholdFiveHour float64 `json:"threshold_5h,omitempty"`

	// ThresholdWeekly is the fraction of the **weekly** window an account
	// must still have. It is what takes an account out of the pool in time:
	// a week spent down to a few percent will not survive the days the
	// five-hour figure still looks fine for.
	ThresholdWeekly float64 `json:"threshold_weekly,omitempty"`

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

	// ProxyBalance enables multi-account load balancing in agyproxy
	// (pigasus agy). It is the old name of Enabled and is implied by
	// Aggregate: a unified pool that could not move a turn between accounts
	// would answer with numbers it cannot spend.
	ProxyBalance bool `json:"proxy_balance,omitempty"`

	// Aggregate is the pool's aggregate mode: the switch that answers the
	// quota and the plan tier as the pool's merged figures.
	//
	// A nil section means "not stated": the pool's own runtime treats it
	// as off, and the proxy treats it as on — the proxy *is* an aggregate
	// pool unless something turns it off.
	Aggregate *Aggregate `json:"aggregate,omitempty"`
}

// UnmarshalJSON reads the section, ignoring the keys this build no longer has.
//
// Two of them used to mean something. `strategy` — either the pool's own or a
// sub-field of `aggregate` — named a rotation; there is one rule now, so the
// key is read and dropped rather than refused. `enabled` is read through a
// pointer so that "false" and "said nothing" stay different answers.
func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	var aux struct {
		*plain
		Enabled *bool `json:"enabled,omitempty"`
	}
	aux.plain = (*plain)(c)
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.Enabled != nil {
		c.Enabled = *aux.Enabled
		c.enabledStated = true
	}
	return nil
}

// MarshalJSON writes the section, naming "enabled" only when the document
// named it.
//
// # Why the key is not always written
//
// `enabled` has three states, not two: on, off, and *not said* — and the third
// one means on (see RoutingEnabled), because writing a threshold or an allow-list
// is asking for a pool. A plain `bool` cannot carry that: marshalling a policy
// whose document never mentioned the key would write `"enabled": false`, and the
// next read would see an explicit off. That is not hypothetical — it is what a
// quota refresh, which saves the registry back after caching a token, did to a
// live pool: the pool turned itself off on the first refresh.
//
// So the key is written only when it was read. A policy built in code (a fresh
// Config, an admin command that means "on") states it explicitly with
// SetEnabled, or leaves it unstated and gets the section's default.
func (c Config) MarshalJSON() ([]byte, error) {
	type section struct {
		Enabled            *bool      `json:"enabled,omitempty"`
		ThresholdFiveHour  float64    `json:"threshold_5h,omitempty"`
		ThresholdWeekly    float64    `json:"threshold_weekly,omitempty"`
		RefreshInterval    string     `json:"refresh_interval,omitempty"`
		Cooldown           string     `json:"cooldown,omitempty"`
		Accounts           []string   `json:"accounts,omitempty"`
		MaxAttempts        int        `json:"max_attempts,omitempty"`
		SwitchOnQuotaError *bool      `json:"switch_on_quota_error,omitempty"`
		ProxyBalance       bool       `json:"proxy_balance,omitempty"`
		Aggregate          *Aggregate `json:"aggregate,omitempty"`
	}
	out := section{
		ThresholdFiveHour:  c.ThresholdFiveHour,
		ThresholdWeekly:    c.ThresholdWeekly,
		RefreshInterval:    c.RefreshInterval,
		Cooldown:           c.Cooldown,
		Accounts:           c.Accounts,
		MaxAttempts:        c.MaxAttempts,
		SwitchOnQuotaError: c.SwitchOnQuotaError,
		ProxyBalance:       c.ProxyBalance,
		Aggregate:          c.Aggregate,
	}
	if c.enabledStated {
		enabled := c.Enabled
		out.Enabled = &enabled
	}
	return json.Marshal(out)
}

// SetEnabled states the switch, so the value survives a save.
func (c *Config) SetEnabled(on bool) {
	c.Enabled = on
	c.enabledStated = true
}

// RoutingEnabled reports whether the pool routes a turn: whether a turn may
// run on an account other than the one the credential names.
//
// A section that exists and does not mention "enabled" counts as **on**. That
// is the whole point of the field: a user who writes a threshold or an
// allow-list has asked for a pool, and should not have to write
// `"enabled": true` to get what they asked for. Say `"enabled": false` to stop
// it.
func (c Config) RoutingEnabled() bool {
	return c.Enabled || !c.enabledStated
}

// RoutingStated reports whether the document named "enabled" at all.
//
// It is what a migration needs: a value that was written can be carried over,
// and one that was implied by the section's existence cannot.
func (c Config) RoutingStated() bool {
	return c.enabledStated
}

// Normalised returns the policy with its defaults filled in.
//
// A malformed value never keeps the policy from being used: an unparsable
// duration falls back to the default, a threshold outside 0..1 is clamped into
// it, and a negative attempt count becomes one.
func (c Config) Normalised() Config {
	out := c
	out.Aggregate = c.normalisedAggregate()
	out.ThresholdFiveHour = clampFraction(out.ThresholdFiveHour)
	out.ThresholdWeekly = clampFraction(out.ThresholdWeekly)
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

// normalisedAggregate returns a copy of the aggregate section. A nil section
// stays nil — "not stated" is a state a caller may want to see — and the
// accessors answer the defaults for it.
func (c Config) normalisedAggregate() *Aggregate {
	if c.Aggregate == nil {
		return nil
	}
	out := *c.Aggregate
	return &out
}

// AggregateEnabled reports whether the pool is *explicitly* the unified
// virtual account.
//
// It is on only when the section exists and its switch says so. A caller that
// runs a pool by default — the proxy — asks AggregateStated instead, which
// also counts a section that exists without naming the switch.
func (c Config) AggregateEnabled() bool {
	return c.Aggregate != nil && c.Aggregate.Enabled != nil && *c.Aggregate.Enabled
}

// AggregateStated reports the section's own answer and whether it gave one.
//
//	section absent        -> (false, false)
//	{}                    -> (true,  false)   the section exists, so the mode is meant
//	{ "enabled": true }   -> (true,  true)
//	{ "enabled": false }  -> (false, true)
//
// The middle case is the point: "on, and I am not going to spell it out" has
// to be distinguishable from "said nothing", so a caller that runs a pool by
// default can honour an explicit `false` without overriding a silence.
func (c Config) AggregateStated() (on bool, stated bool) {
	if c.Aggregate == nil {
		return false, false
	}
	if c.Aggregate.Enabled != nil {
		return *c.Aggregate.Enabled, true
	}
	return true, false
}

// SetAggregate turns the unified pool on or off, allocating the section when
// it is absent.
func (c *Config) SetAggregate(on bool) {
	if c.Aggregate == nil {
		c.Aggregate = &Aggregate{}
	}
	c.Aggregate.Enabled = &on
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

	// Remaining is the account's remaining **five-hour** quota, 0..1.
	// That is the window that decides whether the account can serve a turn
	// right now, because it refills within hours.
	Remaining float64

	// Known reports whether Remaining is a measured figure. An account
	// whose quota has never been read has no figure, and the threshold
	// does not exclude it: a pool does not punish an account for not
	// having been asked.
	Known bool

	// WeeklyRemaining is the account's remaining **weekly** quota, 0..1.
	// It is the slow-moving figure: an account can look healthy today and
	// still be one turn from its weekly wall, so it has a floor of its own
	// (Config.ThresholdWeekly) and breaks a tie when the pool moves.
	WeeklyRemaining float64

	// WeeklyKnown reports whether WeeklyRemaining is a measured figure.
	WeeklyKnown bool

	// Current marks the account the session is signed in with.
	Current bool

	// Excluded marks an account that must not be used: the server said
	// its quota is spent and its cooldown has not passed.
	Excluded bool

	// Quarantined marks an account that is permanently disabled (e.g. Terms of
	// Service violation) and must not be chosen, even as a fallback.
	Quarantined bool

	// QuarantineReason explains why the account is quarantined.
	QuarantineReason string
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

	// Skipped lists the accounts that were passed over, with the reason.
	Skipped []Skip
}

// Choose applies the policy to the state of the pool and returns the
// account the turn should use.
//
// The policy never leaves the session without an account: when every
// account is ineligible the one with the most quota left is used anyway.
//
// Choose has no memory, so it starts on the account the session is signed in
// with. A caller that keeps the pool on one account across turns — which is
// what the pool is for — calls ChooseWithPin with the account the previous
// turn used.
func Choose(cfg Config, states []State) Decision {
	return ChooseWithPin(cfg, states, "")
}

// ChooseWithPin is Choose with the pool's memory spelled out: pinned is the
// account the previous turn used, and the pool stays on it until it is no
// longer eligible.
func ChooseWithPin(cfg Config, states []State, pinned string) Decision {
	cfg = cfg.Normalised()
	decision := Decision{}

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
		case s.Quarantined:
			reason := s.QuarantineReason
			if reason == "" {
				reason = "account disabled / Terms of Service violation"
			}
			decision.Skipped = append(decision.Skipped, Skip{Email: s.Email, Reason: "quarantined: " + reason})
		case s.Excluded:
			decision.Skipped = append(decision.Skipped, Skip{Email: s.Email, Reason: "in cooldown after a quota error"})
		case s.Known && s.Remaining <= 0:
			// A spent five-hour window cannot serve a turn, whatever the
			// floors say: the resource itself is gone.
			decision.Skipped = append(decision.Skipped, Skip{Email: s.Email, Reason: "the five-hour window is spent"})
		case s.WeeklyKnown && s.WeeklyRemaining <= 0:
			decision.Skipped = append(decision.Skipped, Skip{Email: s.Email, Reason: "the weekly window is spent"})
		case s.Known && s.Remaining < cfg.ThresholdFiveHour:
			decision.Skipped = append(decision.Skipped, Skip{
				Email:  s.Email,
				Reason: fmt.Sprintf("below threshold_5h (%.1f%% of the five-hour window left, %.1f%% needed)", s.Remaining*100, cfg.ThresholdFiveHour*100),
			})
		case s.WeeklyKnown && s.WeeklyRemaining < cfg.ThresholdWeekly:
			decision.Skipped = append(decision.Skipped, Skip{
				Email:  s.Email,
				Reason: fmt.Sprintf("below threshold_weekly (%.1f%% of the week left, %.1f%% needed)", s.WeeklyRemaining*100, cfg.ThresholdWeekly*100),
			})
		default:
			eligible = append(eligible, s)
		}
	}

	// Every eligible account is out: use the one with the most left among
	// non-quarantined accounts. A turn with no account cannot be served at all,
	// so the threshold is a preference and not a wall. But quarantined accounts
	// are never used.
	if len(eligible) == 0 {
		var nonQuarantined []State
		for _, s := range allowed {
			if !s.Quarantined {
				nonQuarantined = append(nonQuarantined, s)
			}
		}
		if len(nonQuarantined) > 0 {
			fallback := mostRemaining(nonQuarantined)
			decision.Email = fallback.Email
			decision.Reason = fmt.Sprintf("every account is below its floors; using the one with the most five-hour quota left (%.1f%%)", fallback.Remaining*100)
			return decision
		}
		decision.Reason = "all accounts in pool are quarantined or disabled"
		return decision
	}

	// One rule: stay on the pinned account while it can serve a turn, even
	// when a neighbour happens to have more left, and move only once it falls
	// out. A per-turn "who has the most?" would hand two turns of one
	// conversation to two accounts.
	if pinned != "" {
		for _, s := range eligible {
			if strings.EqualFold(s.Email, pinned) {
				decision.Email = s.Email
				decision.Reason = fmt.Sprintf("staying on the pool's account (%.1f%% of the five-hour window, %.1f%% of the week left)", s.Remaining*100, s.WeeklyRemaining*100)
				return decision
			}
		}
	}
	chosen := mostRemaining(eligible)
	if cur, ok := currentOf(eligible); ok {
		// A pool with no pin yet starts on the account the session is signed
		// in with, not on the fullest one: that is the account the user is
		// looking at, and the one whose project and tier the CLI already
		// knows. It keeps the turn until its window runs down, and only then
		// does the pool move.
		chosen = cur
	}
	decision.Email = chosen.Email
	if pinned == "" {
		decision.Reason = fmt.Sprintf("first turn of the pool: starting on %s (%.1f%% of the five-hour window, %.1f%% of the week left)", chosen.Email, chosen.Remaining*100, chosen.WeeklyRemaining*100)
	} else {
		decision.Reason = fmt.Sprintf("%s is out of the pool; moving to %s (%.1f%% of the five-hour window, %.1f%% of the week left)", pinned, chosen.Email, chosen.Remaining*100, chosen.WeeklyRemaining*100)
	}
	return decision
}

// fiveHourTieBand is how close two five-hour figures have to be before the
// weekly one decides.
//
// Five percentage points: two accounts within that of each other are equally
// healthy *today*, so the pool breaks the tie on the figure that lasts — the
// week. Comparing the five-hour figures strictly would make the weekly rule
// almost never fire, since exact equality is vanishingly rare.
const fiveHourTieBand = 0.05

// mostRemaining returns the account the pool should spend next.
//
// The preference, in order:
//
//  1. a measured figure over an unmeasured one — the pool prefers what it
//     knows;
//  2. more of the five-hour window, by more than fiveHourTieBand — that is
//     what serves the next turn;
//  3. more of the week — of two accounts equally healthy today, the one with
//     more of the week left can carry the session longer before the next move.
//
// A full tie goes to the account earlier in the pool's list, so the choice is a
// function of the pool — not of a timestamp the last turn left behind, which
// would make the same state answer two different accounts on two consecutive
// reads.
func mostRemaining(states []State) State {
	best := states[0]
	for _, s := range states[1:] {
		if preferAccount(s, best) {
			best = s
		}
	}
	return best
}

// preferAccount reports whether a is the better account to spend than b.
func preferAccount(a, b State) bool {
	// 1. What is measured beats what is not.
	if a.Known != b.Known {
		return a.Known
	}
	if a.Known {
		// 2. The five-hour window decides when the gap is real.
		switch {
		case a.Remaining > b.Remaining+fiveHourTieBand:
			return true
		case b.Remaining > a.Remaining+fiveHourTieBand:
			return false
		}
		// 3. Otherwise the week does.
		if a.WeeklyKnown != b.WeeklyKnown {
			return a.WeeklyKnown
		}
		if a.WeeklyKnown && a.WeeklyRemaining != b.WeeklyRemaining {
			return a.WeeklyRemaining > b.WeeklyRemaining
		}
	}
	return false
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

// clampFraction pins a fraction into 0..1. A floor outside the range is a
// typo, not a policy: 15 means 15%, and above one there is nothing left to
// say.
func clampFraction(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
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

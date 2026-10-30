package accountpool

import (
	"testing"
	"time"
)

// ptr returns a pointer to a bool — the "switch_on_quota_error" field is
// a *bool so that "not set" and "false" are different.
func ptr(b bool) *bool { return &b }

func TestParseStrategy(t *testing.T) {
	cases := map[string]Strategy{
		"":            DefaultStrategy,
		"sticky":      StrategySticky,
		"round_robin": StrategyRoundRobin,
		"roundrobin":  StrategyRoundRobin,
		"rr":          StrategyRoundRobin,
		"quota_aware": StrategyQuotaAware,
		"quota":       StrategyQuotaAware,
		"least_used":  StrategyQuotaAware,
		"failover":    StrategyFailover,
		"  STICKY  ":  StrategySticky,
		"fail-over":   StrategyFailover,
		"aggregate":   StrategyAggregate,
		"unified":     StrategyAggregate,
		"agg":         StrategyAggregate,
	}
	for name, want := range cases {
		got, ok := ParseStrategy(name)
		if !ok {
			t.Errorf("ParseStrategy(%q) reported an unknown strategy", name)
		}
		if got != want {
			t.Errorf("ParseStrategy(%q) = %q, want %q", name, got, want)
		}
	}
	if got, ok := ParseStrategy("nonsense"); ok || got != DefaultStrategy {
		t.Errorf("ParseStrategy(\"nonsense\") = %q, %v; want the default and false", got, ok)
	}
}

func TestConfigNormalised(t *testing.T) {
	c := Config{}.Normalised()
	if c.Strategy != DefaultStrategy {
		t.Errorf("strategy = %q, want %q", c.Strategy, DefaultStrategy)
	}
	if c.RefreshDuration() != DefaultRefreshInterval {
		t.Errorf("refresh interval = %v, want %v", c.RefreshDuration(), DefaultRefreshInterval)
	}
	if c.CooldownDuration() != DefaultCooldown {
		t.Errorf("cooldown = %v, want %v", c.CooldownDuration(), DefaultCooldown)
	}
	if c.MaxAttempts != DefaultMaxAttempts {
		t.Errorf("max attempts = %d, want %d", c.MaxAttempts, DefaultMaxAttempts)
	}
	if !c.SwitchOnQuotaErrorEnabled() {
		t.Error("switch_on_quota_error should default to on")
	}

	c = Config{
		Strategy:           "nonsense",
		Threshold:          1.7,
		RefreshInterval:    "not a duration",
		Cooldown:           "",
		MaxAttempts:        -3,
		SwitchOnQuotaError: ptr(false),
		Accounts:           []string{" a@example.com ", "A@EXAMPLE.COM", "", "b@example.com"},
	}.Normalised()
	if c.Strategy != DefaultStrategy {
		t.Errorf("an unknown strategy should fall back, got %q", c.Strategy)
	}
	if c.Threshold != 1 {
		t.Errorf("threshold = %v, want it clamped to 1", c.Threshold)
	}
	if c.RefreshDuration() != DefaultRefreshInterval {
		t.Errorf("an unparsable refresh interval should fall back, got %v", c.RefreshDuration())
	}
	if c.MaxAttempts != DefaultMaxAttempts {
		t.Errorf("max attempts = %d, want %d", c.MaxAttempts, DefaultMaxAttempts)
	}
	if c.SwitchOnQuotaErrorEnabled() {
		t.Error("switch_on_quota_error=false should be honoured")
	}
	if len(c.Accounts) != 2 {
		t.Errorf("the allow-list = %v, want the two distinct addresses", c.Accounts)
	}

	c = Config{Threshold: -1}.Normalised()
	if c.Threshold != 0 {
		t.Errorf("threshold = %v, want it clamped to 0", c.Threshold)
	}
}

func TestConfigAllows(t *testing.T) {
	c := Config{}
	if !c.Allows("a@example.com") {
		t.Error("an empty allow-list should allow every account")
	}
	if c.Allows("  ") {
		t.Error("an empty address is not an account")
	}
	c = Config{Accounts: []string{"a@example.com"}}
	if !c.Allows("A@Example.com") {
		t.Error("the allow-list should match case-insensitively")
	}
	if c.Allows("b@example.com") {
		t.Error("an address outside the allow-list must not be allowed")
	}
}

func state(email string, remaining float64, known bool, current bool) State {
	return State{Email: email, Remaining: remaining, Known: known, Current: current}
}

func TestChooseSticky(t *testing.T) {
	cfg := Config{Strategy: StrategySticky}.Normalised()
	states := []State{
		state("a@example.com", 0.9, true, true),
		state("b@example.com", 0.5, true, false),
	}
	d := Choose(cfg, states, 0)
	if d.Email != "a@example.com" {
		t.Errorf("sticky chose %q, want the account the session is signed in with", d.Email)
	}

	// Below the threshold: the turn moves to the account with the most
	// left.
	states = []State{
		state("a@example.com", 0.2, true, true),
		state("b@example.com", 0.8, true, false),
	}
	cfg = Config{Strategy: StrategySticky, Threshold: 0.5}.Normalised()
	d = Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("sticky chose %q, want the account with the most left", d.Email)
	}
	if len(d.Skipped) != 1 || d.Skipped[0].Email != "a@example.com" {
		t.Errorf("the skipped list = %+v, want the account below the threshold", d.Skipped)
	}

	// Excluded: the account the server said is out of quota.
	states[0].Excluded = true
	d = Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("sticky chose %q, want the remaining account", d.Email)
	}
}

func TestChooseFailoverKeepsTheCurrentBelowTheThreshold(t *testing.T) {
	cfg := Config{Strategy: StrategyFailover, Threshold: 0.9}.Normalised()
	states := []State{
		state("a@example.com", 0.2, true, true),
		state("b@example.com", 0.8, true, false),
	}
	d := Choose(cfg, states, 0)
	if d.Email != "a@example.com" {
		t.Errorf("failover chose %q; the threshold does not move a failover", d.Email)
	}

	// A quota error does move it.
	states[0].Excluded = true
	d = Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("failover chose %q, want the other account", d.Email)
	}
}

func TestChooseRoundRobin(t *testing.T) {
	cfg := Config{Strategy: StrategyRoundRobin}.Normalised()
	states := []State{
		state("a@example.com", 0.9, true, true),
		state("b@example.com", 0.5, true, false),
		state("c@example.com", 0.1, true, false),
	}
	cursor := 0
	seen := []string{}
	for i := 0; i < 6; i++ {
		d := Choose(cfg, states, cursor)
		seen = append(seen, d.Email)
		cursor = d.NextCursor
	}
	want := []string{"a@example.com", "b@example.com", "c@example.com", "a@example.com", "b@example.com", "c@example.com"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("the rotation was %v, want %v", seen, want)
		}
	}
}

func TestChooseRoundRobinSkipsTheAccountsBelowTheThreshold(t *testing.T) {
	cfg := Config{Strategy: StrategyRoundRobin, Threshold: 0.3}.Normalised()
	states := []State{
		state("a@example.com", 0.1, true, true),
		state("b@example.com", 0.9, true, false),
	}
	cursor := 0
	for i := 0; i < 4; i++ {
		d := Choose(cfg, states, cursor)
		if d.Email != "b@example.com" {
			t.Fatalf("the rotation chose %q, want the account above the threshold", d.Email)
		}
		cursor = d.NextCursor
	}
}

func TestChooseQuotaAware(t *testing.T) {
	cfg := Config{Strategy: StrategyQuotaAware}.Normalised()
	states := []State{
		state("a@example.com", 0.2, true, true),
		state("b@example.com", 0.8, true, false),
	}
	d := Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("quota_aware chose %q, want the account with the most left", d.Email)
	}

	// An account whose quota is unknown ranks behind the ones that have a
	// figure: the pool prefers what it knows.
	states = append(states, state("c@example.com", 0, false, false))
	d = Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("quota_aware chose %q, want the account with a measured figure", d.Email)
	}

	// With nothing measured, the least recently used wins.
	states = []State{
		{Email: "a@example.com", Known: false, LastUsed: time.Now().Add(-time.Hour)},
		{Email: "b@example.com", Known: false, LastUsed: time.Now()},
	}
	d = Choose(cfg, states, 0)
	if d.Email != "a@example.com" {
		t.Errorf("quota_aware chose %q, want the least recently used", d.Email)
	}
}

func TestChooseAllowList(t *testing.T) {
	cfg := Config{Strategy: StrategyQuotaAware, Accounts: []string{"b@example.com"}}.Normalised()
	states := []State{
		state("a@example.com", 0.9, true, true),
		state("b@example.com", 0.1, true, false),
	}
	d := Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want the only account in the allow-list", d.Email)
	}
	if len(d.Skipped) != 1 || d.Skipped[0].Email != "a@example.com" {
		t.Errorf("the skipped list = %+v, want the account outside the allow-list", d.Skipped)
	}
}

func TestChooseEveryAccountOut(t *testing.T) {
	cfg := Config{Strategy: StrategyQuotaAware, Threshold: 0.9}.Normalised()
	states := []State{
		state("a@example.com", 0.1, true, true),
		state("b@example.com", 0.4, true, false),
	}
	d := Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want the account with the most left; the threshold is a preference, not a wall", d.Email)
	}
}

func TestChooseEmptyPool(t *testing.T) {
	d := Choose(Config{}.Normalised(), nil, 0)
	if d.Email != "" {
		t.Errorf("chose %q, want nothing", d.Email)
	}
	if d.Reason == "" {
		t.Error("an empty pool should say why")
	}
}

func TestChooseAggregate(t *testing.T) {
	cfg := Config{Strategy: StrategyAggregate, Threshold: 0.15}.Normalised()
	if !cfg.Aggregate {
		t.Errorf("StrategyAggregate should automatically set Aggregate=true")
	}
	states := []State{
		state("a@example.com", 0.3, true, true),
		state("b@example.com", 0.8, true, false),
		state("c@example.com", 0.05, true, false), // below threshold
	}
	d := Choose(cfg, states, 0)
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (most quota left in aggregate pool)", d.Email)
	}
}

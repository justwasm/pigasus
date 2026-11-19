package accountpool

import "testing"

// ptr returns a pointer to a bool — the "switch_on_quota_error" field is
// a *bool so that "not set" and "false" are different.
func ptr(b bool) *bool { return &b }

// pooled is a policy whose unified pool is on.
func pooled() Config {
	return Config{Aggregate: &Aggregate{Enabled: Bool(true)}}
}

// pooledCfg is pooled plus a threshold.
func pooledCfg(threshold float64) Config {
	return Config{Aggregate: &Aggregate{Enabled: Bool(true)}, ThresholdFiveHour: threshold}
}

func TestConfigNormalised(t *testing.T) {
	c := Config{}.Normalised()
	if c.AggregateEnabled() {
		t.Error("an empty policy should not report the unified pool on")
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
		Aggregate:          &Aggregate{Enabled: Bool(true)},
		ThresholdFiveHour:  1.7,
		RefreshInterval:    "not a duration",
		Cooldown:           "",
		MaxAttempts:        -3,
		SwitchOnQuotaError: ptr(false),
		Accounts:           []string{" a@example.com ", "A@EXAMPLE.COM", "", "b@example.com"},
	}.Normalised()
	if !c.AggregateEnabled() {
		t.Error("aggregate.enabled should survive normalisation")
	}
	if c.ThresholdFiveHour != 1 {
		t.Errorf("threshold_5h = %v, want it clamped to 1", c.ThresholdFiveHour)
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

	c = Config{ThresholdFiveHour: -1, ThresholdWeekly: 3}.Normalised()
	if c.ThresholdFiveHour != 0 {
		t.Errorf("threshold_5h = %v, want it clamped to 0", c.ThresholdFiveHour)
	}
	if c.ThresholdWeekly != 1 {
		t.Errorf("threshold_weekly = %v, want it clamped to 1", c.ThresholdWeekly)
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

// The pool starts on the account the session is signed in with, not on the
// fullest one: that is the account the user is looking at.
func TestChooseStartsOnTheSignedInAccount(t *testing.T) {
	cfg := pooled().Normalised()
	states := []State{
		state("a@example.com", 0.9, true, true),
		state("b@example.com", 0.5, true, false),
	}
	d := Choose(cfg, states)
	if d.Email != "a@example.com" {
		t.Errorf("chose %q, want the account the session is signed in with", d.Email)
	}
}

// With no pin and no signed-in account in the pool, the fullest serves.
func TestChooseWithoutACurrentTakesTheFullest(t *testing.T) {
	cfg := pooled().Normalised()
	states := []State{
		state("a@example.com", 0.2, true, false),
		state("b@example.com", 0.8, true, false),
	}
	if d := Choose(cfg, states); d.Email != "b@example.com" {
		t.Errorf("chose %q, want the account with the most left", d.Email)
	}
}

// The single rule: the pin keeps the turn until it can no longer serve one.
func TestPinnedAccountKeepsTheTurn(t *testing.T) {
	cfg := pooledCfg(0.5).Normalised()
	states := []State{
		state("a@example.com", 0.6, true, false),
		state("b@example.com", 0.9, true, false),
	}
	if d := ChooseWithPin(cfg, states, "a@example.com"); d.Email != "a@example.com" {
		t.Errorf("chose %q, want the pinned account even though b has more", d.Email)
	}
}

// Below the threshold, the pin falls out and the turn moves to the fullest.
func TestPinMovesBelowTheThreshold(t *testing.T) {
	cfg := pooledCfg(0.5).Normalised()
	states := []State{
		state("a@example.com", 0.2, true, false),
		state("b@example.com", 0.8, true, false),
	}
	d := ChooseWithPin(cfg, states, "a@example.com")
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want the account with the most left", d.Email)
	}
	if len(d.Skipped) != 1 || d.Skipped[0].Email != "a@example.com" {
		t.Errorf("the skipped list = %+v, want the account below the threshold", d.Skipped)
	}
}

// A quota error outranks the pin: the account the server said is out cannot
// keep the turn, whatever its last figure said.
func TestPinMovesOnACooldown(t *testing.T) {
	cfg := pooledCfg(0.1).Normalised()
	a := state("a@example.com", 0.9, true, false)
	a.Excluded = true
	d := ChooseWithPin(cfg, []State{a, state("b@example.com", 0.5, true, false)}, "a@example.com")
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want the remaining account", d.Email)
	}
}

// A quarantine outranks everything.
func TestQuarantinedIsNeverChosen(t *testing.T) {
	cfg := pooledCfg(0.1).Normalised()
	a := state("a@example.com", 0.9, true, true)
	a.Quarantined = true
	a.QuarantineReason = "terms of service"
	if d := ChooseWithPin(cfg, []State{a, state("b@example.com", 0.5, true, false)}, "a@example.com"); d.Email != "b@example.com" {
		t.Errorf("chose %q, want the non-quarantined account", d.Email)
	}
}

// An account whose quota is unknown ranks behind the ones that have a figure:
// the pool prefers what it knows.
func TestUnknownQuotaRanksLast(t *testing.T) {
	cfg := pooled().Normalised()
	states := []State{
		state("a@example.com", 0.2, true, false),
		state("b@example.com", 0.8, true, false),
		state("c@example.com", 0, false, false),
	}
	if d := Choose(cfg, states); d.Email != "b@example.com" {
		t.Errorf("chose %q, want the account with a measured figure", d.Email)
	}
}

// A tie goes to the account earlier in the pool's list, so the same state
// answers the same account on every read — no timestamp, no drift.
func TestTieGoesToTheEarlierAccount(t *testing.T) {
	cfg := pooled().Normalised()
	states := []State{
		state("a@example.com", 0.5, true, false),
		state("b@example.com", 0.5, true, false),
	}
	for i := 0; i < 3; i++ {
		if d := Choose(cfg, states); d.Email != "a@example.com" {
			t.Fatalf("read %d chose %q, want a@example.com every time", i, d.Email)
		}
	}
	// Reversing the input reverses the answer: the choice is a function of
	// the list, not of a hidden clock.
	reversed := []State{states[1], states[0]}
	if d := Choose(cfg, reversed); d.Email != "b@example.com" {
		t.Errorf("reversed list chose %q, want b@example.com", d.Email)
	}
}

func TestChooseAllowList(t *testing.T) {
	cfg := Config{Aggregate: &Aggregate{Enabled: Bool(true)}, Accounts: []string{"b@example.com"}}.Normalised()
	states := []State{
		state("a@example.com", 0.9, true, true),
		state("b@example.com", 0.1, true, false),
	}
	d := Choose(cfg, states)
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want the only account in the allow-list", d.Email)
	}
	if len(d.Skipped) != 1 || d.Skipped[0].Email != "a@example.com" {
		t.Errorf("the skipped list = %+v, want the account outside the allow-list", d.Skipped)
	}
}

func TestChooseEveryAccountOut(t *testing.T) {
	cfg := pooledCfg(0.9).Normalised()
	states := []State{
		state("a@example.com", 0.1, true, true),
		state("b@example.com", 0.4, true, false),
	}
	d := Choose(cfg, states)
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want the account with the most left; the threshold is a preference, not a wall", d.Email)
	}
}

func TestChooseEmptyPool(t *testing.T) {
	d := Choose(Config{}.Normalised(), nil)
	if d.Email != "" {
		t.Errorf("chose %q, want nothing", d.Email)
	}
	if d.Reason == "" {
		t.Error("an empty pool should say why")
	}
}

func TestChooseQuarantined(t *testing.T) {
	cfg := pooled().Normalised()
	states := []State{
		{Email: "banned@example.com", Remaining: 0.9, Known: true, Quarantined: true, QuarantineReason: "Terms of Service violation"},
		{Email: "healthy@example.com", Remaining: 0.2, Known: true},
	}
	d := Choose(cfg, states)
	if d.Email != "healthy@example.com" {
		t.Errorf("chose %q, want healthy@example.com (banned account must be skipped)", d.Email)
	}

	// Even if all accounts are out of quota or banned, quarantined accounts are never picked
	statesAllOut := []State{
		{Email: "banned@example.com", Remaining: 0.9, Known: true, Quarantined: true, QuarantineReason: "ToS Banned"},
		{Email: "exhausted@example.com", Remaining: 0.05, Known: true, Excluded: true},
	}
	d2 := Choose(pooledCfg(0.5).Normalised(), statesAllOut)
	if d2.Email != "exhausted@example.com" {
		t.Errorf("chose %q, want exhausted@example.com (quarantined account must never be fallback)", d2.Email)
	}
}

func TestChooseAggregate(t *testing.T) {
	cfg := pooledCfg(0.15).Normalised()
	if !cfg.AggregateEnabled() {
		t.Error("a rotation named inside the aggregate section should report the pool on")
	}
	states := []State{
		state("a@example.com", 0.3, true, false),
		state("b@example.com", 0.8, true, false),
		state("c@example.com", 0.05, true, false), // below threshold
	}
	d := Choose(cfg, states)
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (most quota left on the pool's first turn)", d.Email)
	}
	// The signed-in account wins the first turn when it can serve one: the
	// pool starts where the session already is, then stays there.
	states[0].Current = true
	if d := Choose(cfg, states); d.Email != "a@example.com" {
		t.Errorf("chose %q, want the signed-in account a@example.com on the pool's first turn", d.Email)
	}
}

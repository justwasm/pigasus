package accountpool

import "testing"

// The pool is sticky: it stays on the pinned account even when a neighbour has
// more left. Switching per turn would split one conversation across accounts.
func TestAggregateStaysOnThePinnedAccount(t *testing.T) {
	cfg := pooledCfg(0.15).Normalised()
	states := []State{
		state("a@example.com", 0.40, true, false),
		state("b@example.com", 0.90, true, false),
	}
	d := ChooseWithPin(cfg, states, "a@example.com")
	if d.Email != "a@example.com" {
		t.Errorf("chose %q, want a@example.com (the pinned account keeps the turn even with less quota left)", d.Email)
	}
}

// The pin is left when its five-hour window drops to the floor.
func TestPinMovesWhenFiveHourReachesItsFloor(t *testing.T) {
	cfg := pooledCfg(0.15).Normalised()
	states := []State{
		state("a@example.com", 0.10, true, false),
		state("b@example.com", 0.90, true, false),
		state("c@example.com", 0.50, true, false),
	}
	d := ChooseWithPin(cfg, states, "a@example.com")
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (the fullest account once the pin runs down)", d.Email)
	}
}

// The weekly floor is the point of having two: an account whose *day* looks
// fine but whose week is nearly spent is taken out in time, before it hits the
// wall mid-session.
func TestWeeklyFloorMovesThePool(t *testing.T) {
	cfg := Config{
		Aggregate:         &Aggregate{Enabled: Bool(true)},
		ThresholdFiveHour: 0.05,
		ThresholdWeekly:   0.25,
	}.Normalised()
	a := state("a@example.com", 0.90, true, false)
	a.WeeklyRemaining = 0.10 // plenty of today, almost no week
	a.WeeklyKnown = true
	b := state("b@example.com", 0.60, true, false)
	b.WeeklyRemaining = 0.80
	b.WeeklyKnown = true

	d := ChooseWithPin(cfg, []State{a, b}, "a@example.com")
	if d.Email != "b@example.com" {
		t.Fatalf("chose %q, want b@example.com (the pin's week is below its floor)", d.Email)
	}
	if len(d.Skipped) != 1 || d.Skipped[0].Email != "a@example.com" {
		t.Fatalf("skipped = %+v, want a@example.com", d.Skipped)
	}
	if got := d.Skipped[0].Reason; got != "below threshold_weekly (10.0% of the week left, 25.0% needed)" {
		t.Errorf("reason = %q", got)
	}
}

// A week that is *spent* is not a floor opinion: the account has nothing left
// to serve with, whatever its day says.
func TestSpentWeekIsSkipped(t *testing.T) {
	cfg := pooledCfg(0.15).Normalised()
	a := state("a@example.com", 0.90, true, false)
	a.WeeklyRemaining = 0
	a.WeeklyKnown = true
	d := ChooseWithPin(cfg, []State{a, state("b@example.com", 0.40, true, false)}, "")
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (an account whose week is spent cannot serve)", d.Email)
	}
}

// A spent five-hour window is the same kind of fact.
func TestSpentFiveHourIsSkipped(t *testing.T) {
	cfg := pooledCfg(0).Normalised() // no floors at all
	a := state("a@example.com", 0, true, false)
	d := ChooseWithPin(cfg, []State{a, state("b@example.com", 0.30, true, false)}, "")
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (a spent five-hour window cannot serve)", d.Email)
	}
}

func TestAggregateMovesWhenThePinnedAccountIsInCooldown(t *testing.T) {
	cfg := pooledCfg(0.15).Normalised()
	a := state("a@example.com", 0.90, true, false)
	a.Excluded = true
	states := []State{a, state("b@example.com", 0.60, true, false)}
	d := ChooseWithPin(cfg, states, "a@example.com")
	if d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (a quota error moves the pool off the pin)", d.Email)
	}
}

// A pool with no pin yet answers with one account, and the caller keeps it:
// this is the first turn, not a rotation.
func TestAggregateWithoutAPinPicksTheFullest(t *testing.T) {
	cfg := pooledCfg(0.15).Normalised()
	states := []State{
		state("a@example.com", 0.40, true, false),
		state("b@example.com", 0.90, true, false),
	}
	if d := ChooseWithPin(cfg, states, ""); d.Email != "b@example.com" {
		t.Errorf("first turn chose %q, want b@example.com (the most left)", d.Email)
	}
}

// Which account a move picks, in order: the five-hour window first, by more
// than the tie band; then the week.
func TestMovePrefersFiveHourThenWeekly(t *testing.T) {
	cfg := pooledCfg(0).Normalised()
	weekly := func(email string, five, week float64) State {
		s := state(email, five, true, false)
		s.WeeklyRemaining, s.WeeklyKnown = week, true
		return s
	}

	// A clear five-hour gap decides, whatever the weeks say.
	states := []State{
		weekly("a@example.com", 0.50, 0.95),
		weekly("b@example.com", 0.90, 0.10),
	}
	if d := ChooseWithPin(cfg, states, ""); d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (the clear five-hour lead wins over the week)", d.Email)
	}

	// Inside the band, the week decides.
	states = []State{
		weekly("a@example.com", 0.60, 0.20),
		weekly("b@example.com", 0.62, 0.90),
	}
	if d := ChooseWithPin(cfg, states, ""); d.Email != "b@example.com" {
		t.Errorf("chose %q, want b@example.com (comparable five-hour figures, more week left)", d.Email)
	}

	// Outside the band, the five-hour figure wins again: the band is a band,
	// not a licence to ignore the day.
	states = []State{
		weekly("a@example.com", 0.70, 0.10),
		weekly("b@example.com", 0.63, 0.95),
	}
	if d := ChooseWithPin(cfg, states, ""); d.Email != "a@example.com" {
		t.Errorf("chose %q, want a@example.com (a 7-point five-hour lead is real)", d.Email)
	}
}

// An account with no measured figure ranks behind one that has a figure.
func TestUnmeasuredRanksBehindMeasured(t *testing.T) {
	cfg := pooledCfg(0).Normalised()
	unknown := State{Email: "a@example.com"}
	known := state("b@example.com", 0.01, true, false)
	if d := ChooseWithPin(cfg, []State{unknown, known}, ""); d.Email != "b@example.com" {
		t.Errorf("chose %q, want the measured account", d.Email)
	}
}

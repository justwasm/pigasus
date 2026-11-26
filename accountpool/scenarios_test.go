package accountpool

import (
	"strings"
	"testing"
)

// This file is a Storybook for the routing policy.
//
// Storybook philosophy, applied to a pure domain package:
//
//   - a scenario is *independently constructed* — it builds its own Config
//     and its own []State from scratch, so it shares nothing with its
//     neighbours and depends on no file, network, clock or environment;
//   - it has a *stable id* (Scenario.ID) and a human name;
//   - its *inputs are explicit* (the Config and the states it is built from);
//   - its *output is observable and asserted*: the account chosen, the reason
//     the log line would carry, the accounts passed over and why, and the
//     side-effects that must NOT happen.
//
// Each scenario runs through the same harness (runScenario): it re-normalises
// the config, replays the optional action sequence turn by turn (carrying the
// previous choice forward as the next pin, exactly as the runtime does), and
// finally hands the last Decision to the scenario's own assertions.
//
// The scenarios below pin the rules the package exists for: sticky pin,
// the five-hour floor, the weekly floor, the below-floor fallback, cooldown,
// quarantine, unknown quota, and deterministic tie-breaks.

// scenario is one independently-constructed routing story.
type scenario struct {
	// ID is the stable identifier of the scenario.
	ID string
	// Name is a short human description of what the scenario pins.
	Name string
	// cfg is the explicit policy input.
	cfg Config
	// states is the explicit pool state input.
	states []State
	// pin is the account the first turn is pinned to ("" = no pin yet).
	pin string
	// actions is an optional sequence of follow-up turns. Turn i is served
	// with the account chosen by turn i-1 as its pin, so a scenario can show
	// a pool moving over several turns without hand-feeding each pin.
	actions [][]State
	// check asserts the observable output of the final turn and the
	// side-effects that must not occur.
	check func(t *testing.T, d Decision)
}

// runScenario constructs and runs one scenario through the real entry point.
func runScenario(t *testing.T, sc scenario) {
	t.Helper()
	// Decide the first turn.
	d := ChooseWithPin(sc.cfg, sc.states, sc.pin)
	// Replay any follow-up turns, carrying the choice forward as the pin.
	for _, action := range sc.actions {
		d = ChooseWithPin(sc.cfg, action, d.Email)
	}
	sc.check(t, d)
}

// requireChosen asserts the chosen account.
func requireChosen(t *testing.T, d Decision, want string) {
	t.Helper()
	if d.Email != want {
		t.Fatalf("chose %q, want %q (reason: %q)", d.Email, want, d.Reason)
	}
}

// requireReasonContains asserts the decision's reason carries want.
func requireReasonContains(t *testing.T, d Decision, want string) {
	t.Helper()
	if !strings.Contains(d.Reason, want) {
		t.Fatalf("reason = %q, want it to contain %q", d.Reason, want)
	}
}

// requireNoReasonContains asserts the decision's reason does not carry bad —
// the guard against a wrong-but-plausible explanation.
func requireNoReasonContains(t *testing.T, d Decision, bad string) {
	t.Helper()
	if strings.Contains(d.Reason, bad) {
		t.Fatalf("reason = %q, must not contain %q", d.Reason, bad)
	}
}

// skippedEmails returns the addresses the decision passed over, in order.
func skippedEmails(d Decision) []string {
	out := make([]string, 0, len(d.Skipped))
	for _, s := range d.Skipped {
		out = append(out, s.Email)
	}
	return out
}

// requireSkipped asserts the exact set of skipped accounts (order-sensitive:
// the order is part of the observable output).
func requireSkipped(t *testing.T, d Decision, want ...string) {
	t.Helper()
	got := skippedEmails(d)
	if len(got) != len(want) {
		t.Fatalf("skipped = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("skipped = %v, want %v", got, want)
		}
	}
}

// skipReason returns the recorded reason for the named skipped account.
func skipReason(d Decision, email string) (string, bool) {
	for _, s := range d.Skipped {
		if s.Email == email {
			return s.Reason, true
		}
	}
	return "", false
}

// weekly builds a State carrying both a five-hour and a weekly figure.
func weekly(email string, five, week float64, known bool) State {
	s := state(email, five, known, false)
	s.WeeklyRemaining = week
	s.WeeklyKnown = known
	return s
}

// TestRoutingScenarios is the Storybook table: every entry is an independent,
// named, pure scenario with explicit inputs and observable assertions.
func TestRoutingScenarios(t *testing.T) {
	scenarios := []scenario{
		{
			// No pin yet, no signed-in account in the pool: the first turn
			// starts on the fullest account. The pin is a preference, not a
			// starting requirement.
			ID:   "first-turn-no-pin-takes-fullest",
			Name: "首次无 pin 且无登录账号时选剩余最多者",
			cfg:  pooledCfg(0.05).Normalised(),
			states: []State{
				state("a@example.com", 0.30, true, false),
				state("b@example.com", 0.90, true, false),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "b@example.com")
				requireReasonContains(t, d, "first turn of the pool")
				requireSkipped(t, d) // nothing is passed over on a clean first turn
			},
		},
		{
			// No pin, but the session is signed in with an account that can
			// serve: the first turn starts there, not on a fuller neighbour.
			// That is the account the user is looking at.
			ID:   "first-turn-no-pin-starts-on-signed-in",
			Name: "首次无 pin 时从登录账号开始而非最满账号",
			cfg:  pooledCfg(0.05).Normalised(),
			states: []State{
				state("a@example.com", 0.40, true, true),
				state("b@example.com", 0.90, true, false),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "a@example.com")
				requireReasonContains(t, d, "first turn of the pool")
			},
		},
		{
			// Sticky pin: the pinned account keeps the turn even though a
			// neighbour has more left. Switching per turn would split one
			// conversation across accounts.
			ID:   "sticky-pin-keeps-the-turn",
			Name: "粘性 pin 保持在当前账号即便邻居额度更高",
			cfg:  pooledCfg(0.05).Normalised(),
			states: []State{
				state("a@example.com", 0.40, true, false),
				state("b@example.com", 0.95, true, false),
			},
			pin: "a@example.com",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "a@example.com")
				requireReasonContains(t, d, "staying on the pool's account")
				requireSkipped(t, d) // staying is not skipping
			},
		},
		{
			// The five-hour window drops below its floor: the pin falls out
			// and the turn moves to the fullest eligible account.
			ID:   "five-hour-floor-moves-the-pool",
			Name: "5h 额度跌破阈值触发切换到最健康账号",
			cfg:  pooledCfg(0.5).Normalised(),
			states: []State{
				state("a@example.com", 0.20, true, false),
				state("b@example.com", 0.80, true, false),
			},
			pin: "a@example.com",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "b@example.com")
				requireReasonContains(t, d, "a@example.com is out of the pool; moving to b@example.com")
				requireSkipped(t, d, "a@example.com")
				if reason, ok := skipReason(d, "a@example.com"); !ok || !strings.Contains(reason, "below threshold_5h") {
					t.Fatalf("skip reason = %q, want a below threshold_5h note", reason)
				}
			},
		},
		{
			// The weekly window is below its floor even though the day looks
			// fine: the account is taken out in time. This is the whole reason
			// there are two floors.
			ID:   "weekly-floor-moves-the-pool",
			Name: "周额度跌破阈值即使 5h 充足也切换",
			cfg: Config{
				Aggregate:         &Aggregate{Enabled: Bool(true)},
				ThresholdFiveHour: 0.05,
				ThresholdWeekly:   0.25,
			}.Normalised(),
			states: []State{
				weekly("a@example.com", 0.90, 0.10, true), // plenty of today, almost no week
				weekly("b@example.com", 0.60, 0.80, true),
			},
			pin: "a@example.com",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "b@example.com")
				requireSkipped(t, d, "a@example.com")
				if reason, ok := skipReason(d, "a@example.com"); !ok || !strings.Contains(reason, "below threshold_weekly") {
					t.Fatalf("skip reason = %q, want a below threshold_weekly note", reason)
				}
			},
		},
		{
			// Every account is below the floors: the threshold is a
			// preference, not a wall. The pool still serves a turn, choosing
			// the non-quarantined account with the most five-hour left.
			ID:   "all-below-floor-falls-back-to-most-left",
			Name: "全部低于阈值时兜底选剩余最多者",
			cfg:  pooledCfg(0.9).Normalised(),
			states: []State{
				state("a@example.com", 0.10, true, true),
				state("b@example.com", 0.40, true, false),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "b@example.com")
				requireReasonContains(t, d, "every account is below its floors")
				// Both accounts fell below the five-hour floor before the
				// fallback ran, so both are recorded as passed over — the
				// fallback is what still serves a turn anyway.
				requireSkipped(t, d, "a@example.com", "b@example.com")
			},
		},
		{
			// An account in cooldown after a quota error is skipped, and the
			// pin moves off it. Cooldown outranks the pin.
			ID:   "cooldown-account-is-skipped",
			Name: "冷却中账号被跳过且 pin 移走",
			cfg:  pooledCfg(0.15).Normalised(),
			states: func() []State {
				a := state("a@example.com", 0.90, true, false)
				a.Excluded = true
				return []State{a, state("b@example.com", 0.60, true, false)}
			}(),
			pin: "a@example.com",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "b@example.com")
				requireSkipped(t, d, "a@example.com")
				if reason, ok := skipReason(d, "a@example.com"); !ok || !strings.Contains(reason, "cooldown") {
					t.Fatalf("skip reason = %q, want a cooldown note", reason)
				}
			},
		},
		{
			// A quarantined account is never chosen and never a fallback,
			// even when every other account is spent.
			ID:   "quarantined-account-is-skipped",
			Name: "隔离账号被跳过且永不作为兜底",
			cfg:  pooledCfg(0.5).Normalised(),
			states: []State{
				{Email: "banned@example.com", Remaining: 0.99, Known: true, Quarantined: true, QuarantineReason: "Terms of Service violation"},
				{Email: "exhausted@example.com", Remaining: 0.05, Known: true, Excluded: true},
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "exhausted@example.com")
				requireReasonContains(t, d, "every account is below its floors")
				if reason, ok := skipReason(d, "banned@example.com"); !ok || !strings.Contains(reason, "quarantined") {
					t.Fatalf("skip reason = %q, want a quarantine note", reason)
				}
			},
		},
		{
			// A spent five-hour window is a hard fact, not a floor opinion: the
			// account cannot serve whatever the floors say.
			ID:   "spent-five-hour-window-is-skipped",
			Name: "5h 窗口耗尽被跳过无论阈值如何",
			cfg:  pooledCfg(0).Normalised(), // no floors at all
			states: []State{
				state("a@example.com", 0, true, true), // signed in, but spent
				state("b@example.com", 0.30, true, false),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "b@example.com")
				requireSkipped(t, d, "a@example.com")
				if reason, ok := skipReason(d, "a@example.com"); !ok || !strings.Contains(reason, "five-hour window is spent") {
					t.Fatalf("skip reason = %q, want a spent-window note", reason)
				}
			},
		},
		{
			// An account whose quota has never been read has no figure, and
			// the threshold does not exclude it — but a measured account is
			// preferred over an unmeasured one.
			ID:   "unknown-quota-ranks-behind-measured",
			Name: "未知额度排在有测量值之后但不被阈值排除",
			cfg:  pooledCfg(0.05).Normalised(),
			states: []State{
				state("a@example.com", 0.60, true, false),
				state("b@example.com", 0, false, false), // never read
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "a@example.com")
				requireSkipped(t, d) // unknown is eligible, just not preferred
			},
		},
		{
			// An unmeasured account still serves when it is the only one and
			// is not excluded by a floor: the pool does not punish an account
			// for not having been asked.
			ID:   "only-unknown-account-still-serves",
			Name: "唯一且未知的账号仍然服务不被阈值惩罚",
			cfg:  pooledCfg(0.5).Normalised(),
			states: []State{
				state("a@example.com", 0, false, true),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "a@example.com")
				requireSkipped(t, d)
			},
		},
		{
			// A full tie goes to the account earlier in the pool's list, and
			// the same state answers the same account on every read — no
			// timestamp, no drift.
			ID:   "tie-break-is-deterministic-earlier-account",
			Name: "完全并列时取列表中靠前者且多次读取一致",
			cfg:  pooledCfg(0.05).Normalised(),
			states: []State{
				state("a@example.com", 0.50, true, false),
				state("b@example.com", 0.50, true, false),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				for i := 0; i < 3; i++ {
					got := ChooseWithPin(pooledCfg(0.05).Normalised(), []State{
						state("a@example.com", 0.50, true, false),
						state("b@example.com", 0.50, true, false),
					}, "")
					if got.Email != "a@example.com" {
						t.Fatalf("read %d chose %q, want a@example.com every time", i, got.Email)
					}
				}
				requireChosen(t, d, "a@example.com")
			},
		},
		{
			// Inside the five-hour tie band, the weekly figure breaks the tie:
			// of two accounts equally healthy today, spend the one with more
			// week left.
			ID:   "tie-band-weekly-breaks-the-tie",
			Name: "5h 在并列带内时由周额度决出",
			cfg:  pooledCfg(0).Normalised(),
			states: []State{
				weekly("a@example.com", 0.60, 0.20, true),
				weekly("b@example.com", 0.62, 0.90, true),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "b@example.com")
			},
		},
		{
			// A five-hour lead larger than the tie band is real and wins over
			// the week: the band is a band, not a licence to ignore the day.
			ID:   "five-hour-lead-outside-band-wins",
			Name: "5h 领先超出并列带时无视周额度",
			cfg:  pooledCfg(0).Normalised(),
			states: []State{
				weekly("a@example.com", 0.70, 0.10, true),
				weekly("b@example.com", 0.63, 0.95, true),
			},
			pin: "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "a@example.com")
			},
		},
		{
			// A multi-turn story: the pool starts on the signed-in account,
			// keeps it while it is healthy, and only moves once its five-hour
			// window runs below the floor — carrying the pin forward each turn
			// exactly as the runtime does.
			ID:   "multi-turn-sticky-then-move",
			Name: "多轮故事：先粘住再随 5h 跌破阈值切换",
			cfg:  pooledCfg(0.5).Normalised(),
			states: []State{
				state("a@example.com", 0.80, true, true),
				state("b@example.com", 0.95, true, false),
			},
			pin: "",
			actions: [][]State{
				// turn 2: a is still healthy (0.80 >= 0.5) -> stay on a.
				{
					state("a@example.com", 0.80, true, true),
					state("b@example.com", 0.95, true, false),
				},
				// turn 3: a's five-hour window has dropped below the floor
				// -> move to b.
				{
					state("a@example.com", 0.20, true, true),
					state("b@example.com", 0.95, true, false),
				},
			},
			check: func(t *testing.T, d Decision) {
				// The final turn's decision is the move to b.
				requireChosen(t, d, "b@example.com")
				requireReasonContains(t, d, "a@example.com is out of the pool; moving to b@example.com")
				requireSkipped(t, d, "a@example.com")
			},
		},
		{
			// An empty pool answers with no account and says why — the policy
			// never invents an account that is not there.
			ID:   "empty-pool-answers-nothing",
			Name: "空池不产出账号并说明原因",
			cfg:  pooledCfg(0.5).Normalised(),
			states: nil,
			pin:    "",
			check: func(t *testing.T, d Decision) {
				requireChosen(t, d, "")
				requireReasonContains(t, d, "the pool is empty")
				requireSkipped(t, d)
			},
		},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			runScenario(t, sc)
		})
	}
}
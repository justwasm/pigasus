package accountpool

import (
	"encoding/json"
	"testing"
)

// TestUnstatedEnabledSurvivesASave pins the three-state field.
//
// `enabled` means on, off, or "not said" — and "not said" is on, because
// writing a threshold or an allow-list is asking for a pool. A plain bool
// could not carry the third state: marshalling a section that never mentioned
// the key wrote `"enabled": false`, and the next read saw an explicit off. A
// quota refresh that saved the registry back was enough to switch a live pool
// off, so the key must be written only when it was read.
func TestUnstatedEnabledSurvivesASave(t *testing.T) {
	const document = `{"threshold_5h": 0.05, "accounts": ["a@example.com"]}`
	var cfg Config
	if err := json.Unmarshal([]byte(document), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !cfg.RoutingEnabled() {
		t.Fatal("a section that does not name enabled reads as off, want on")
	}
	if cfg.RoutingStated() {
		t.Fatal("RoutingStated = true for a section that does not name enabled")
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(raw); containsKey(got, "enabled") {
		t.Fatalf("a save wrote %s, want no \"enabled\" key", got)
	}

	var again Config
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatalf("Unmarshal after save: %v", err)
	}
	if !again.RoutingEnabled() {
		t.Fatal("the pool switched itself off across a save")
	}
	if again.ThresholdFiveHour != 0.05 || len(again.Accounts) != 1 {
		t.Fatalf("the save lost the policy: %+v", again)
	}
}

// TestStatedEnabledSurvivesASave is the other half: an explicit off is a
// decision and has to be written, or every save would quietly turn the pool
// back on.
func TestStatedEnabledSurvivesASave(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"enabled": false, "threshold_5h": 0.2}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.RoutingEnabled() {
		t.Fatal("RoutingEnabled = true for \"enabled\": false")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !containsKey(string(raw), "enabled") {
		t.Fatalf("a save dropped the explicit off: %s", raw)
	}
	var again Config
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatalf("Unmarshal after save: %v", err)
	}
	if again.RoutingEnabled() {
		t.Fatal("the explicit off was lost across a save")
	}
}

// TestSetEnabledStatesTheSwitch pins the writer's way in: a policy built in
// code that means "on" must say so, because an unstated section is only on by
// virtue of its own existence.
func TestSetEnabledStatesTheSwitch(t *testing.T) {
	cfg := Config{}
	cfg.SetEnabled(true)
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !containsKey(string(raw), "enabled") {
		t.Fatalf("SetEnabled(true) did not state the switch: %s", raw)
	}
	var again Config
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !again.RoutingEnabled() || !again.RoutingStated() {
		t.Fatalf("round trip lost the switch: %+v", again)
	}

	cfg.SetEnabled(false)
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	again = Config{}
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if again.RoutingEnabled() {
		t.Fatal("SetEnabled(false) round-tripped as on")
	}
}

// TestMarshalKeepsEveryField is the guard on the hand-written marshaler: a
// field that is not copied into the section struct would be dropped on every
// save, silently.
func TestMarshalKeepsEveryField(t *testing.T) {
	off := false
	cfg := Config{
		ThresholdFiveHour:  0.15,
		ThresholdWeekly:    0.1,
		RefreshInterval:    "2m",
		Cooldown:           "5m",
		Accounts:           []string{"a@example.com"},
		MaxAttempts:        3,
		SwitchOnQuotaError: &off,
		ProxyBalance:       true,
		Aggregate:          &Aggregate{Enabled: boolPtr(true)},
	}
	cfg.SetEnabled(true)
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back Config
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !back.Enabled || !back.RoutingStated() {
		t.Errorf("enabled: %+v", back)
	}
	if back.ThresholdFiveHour != cfg.ThresholdFiveHour || back.ThresholdWeekly != cfg.ThresholdWeekly {
		t.Errorf("thresholds: %v / %v", back.ThresholdFiveHour, back.ThresholdWeekly)
	}
	if back.RefreshInterval != cfg.RefreshInterval || back.Cooldown != cfg.Cooldown {
		t.Errorf("durations: %q / %q", back.RefreshInterval, back.Cooldown)
	}
	if len(back.Accounts) != 1 || back.Accounts[0] != "a@example.com" {
		t.Errorf("accounts: %v", back.Accounts)
	}
	if back.MaxAttempts != 3 {
		t.Errorf("max_attempts: %d", back.MaxAttempts)
	}
	if back.SwitchOnQuotaError == nil || *back.SwitchOnQuotaError {
		t.Errorf("switch_on_quota_error: %v", back.SwitchOnQuotaError)
	}
	if !back.ProxyBalance {
		t.Error("proxy_balance was dropped")
	}
	if back.Aggregate == nil || back.Aggregate.Enabled == nil || !*back.Aggregate.Enabled {
		t.Errorf("aggregate: %+v", back.Aggregate)
	}
}

// containsKey reports whether the JSON object names key at its top level.
func containsKey(document, key string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(document), &fields); err != nil {
		return false
	}
	_, ok := fields[key]
	return ok
}

// boolPtr is the pointer the Aggregate switch needs.
func boolPtr(v bool) *bool { return &v }

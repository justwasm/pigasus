package accountpool

import (
	"encoding/json"
	"strings"
	"testing"
)

// The aggregate section is a switch and nothing else: it says whether the pool
// answers the quota and the tier as one virtual account.
func TestAggregateSectionMarshalsAsASwitch(t *testing.T) {
	cfg := Config{Enabled: true, ThresholdFiveHour: 0.15}
	cfg.SetAggregate(true)

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"aggregate":{"enabled":true}`) {
		t.Errorf("the document = %s, want the aggregate section to be one switch", text)
	}
	if strings.Contains(text, "strategy") {
		t.Errorf("the document carries a strategy: %s", text)
	}
}

// There is one rule, so there is no rotation key anywhere in the type. A
// document that still names one parses; the key is dropped.
func TestRetiredRotationKeysAreIgnored(t *testing.T) {
	docs := []string{
		`{"enabled":true,"threshold_5h":0.2,"strategy":"quota_aware"}`,
		`{"enabled":true,"threshold_5h":0.2,"aggregate":{"strategy":"round_robin"}}`,
	}
	for _, doc := range docs {
		var cfg Config
		if err := json.Unmarshal([]byte(doc), &cfg); err != nil {
			t.Fatalf("Unmarshal(%s): %v", doc, err)
		}
		if !cfg.RoutingEnabled() {
			t.Errorf("%s: the pool should route", doc)
		}
		if cfg.ThresholdFiveHour != 0.2 {
			t.Errorf("%s: threshold_5h = %v, want 0.2", doc, cfg.ThresholdFiveHour)
		}
		// The `aggregate` object still says the mode is meant, even when the
		// only key it had was a retired rotation.
		if on, _ := cfg.AggregateStated(); on != strings.Contains(doc, `"aggregate"`) {
			t.Errorf("%s: AggregateStated() = %v", doc, on)
		}
	}
}

// An empty policy says nothing about the mode: the caller decides, and the
// proxy decides "on".
func TestAggregateSectionIsOptional(t *testing.T) {
	cfg := Config{}.Normalised()
	if cfg.Aggregate != nil {
		t.Errorf("an empty policy invented an aggregate section: %+v", cfg.Aggregate)
	}
	if cfg.AggregateEnabled() {
		t.Error("an empty policy should not report the mode on")
	}
	if on, stated := cfg.AggregateStated(); on || stated {
		t.Errorf("AggregateStated() = (%v, %v), want (false, false)", on, stated)
	}
}

// "The pool is on, but do not merge" has to be distinguishable from "said
// nothing", or a proxy that runs a pool by default cannot honour it.
func TestExplicitFalseIsNotSilence(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"enabled":true,"aggregate":{"enabled":false}}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	on, stated := cfg.AggregateStated()
	if on {
		t.Error("the section said false")
	}
	if !stated {
		t.Error("the section gave an answer; it must not read as silence")
	}
}

// Tests for the Web UI's picker rows of a model that declares reasoning
// tiers.
//
// The picker shows the label it is handed and sends the row's id back, so
// the tiers reach the picker without a frontend change: one row per tier,
// the tier in the id and in the label.
package rchost

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// TestHostListModelsExpandsTheTiers pins the picker's rows.
func TestHostListModelsExpandsTheTiers(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
	  "type": "openai",
	  "api_url": "https://gw.example/v1/chat/completions",
	  "models": [
	    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
	     "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
	     "reasoning_effort": "high"},
	    {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}
	  ]}}}`)
	rows, err := hostListModels("", "", "")
	if err != nil {
		t.Fatalf("hostListModels: %v", err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.ID] = r.Label
	}
	want := map[string]string{
		"minimax-m3.1-flash-low":    "MiniMax M3.1 Flash (Low)",
		"minimax-m3.1-flash-medium": "MiniMax M3.1 Flash (Medium)",
		"minimax-m3.1-flash-high":   "MiniMax M3.1 Flash (High)",
		"minimax-m3.1-flash-xhigh":  "MiniMax M3.1 Flash (Xhigh)",
		"minimax-m3.1-flash-max":    "MiniMax M3.1 Flash (Max)",
		"minimax-m3":                "MiniMax M3",
	}
	for id, label := range want {
		if got[id] != label {
			t.Errorf("the picker row %q = %q, want %q", id, got[id], label)
		}
	}
	// The bare id is not a row of its own: the tiers replaced it.
	if _, ok := got["minimax-m3.1-flash"]; ok {
		t.Errorf("the bare id is still a picker row: %q", got["minimax-m3.1-flash"])
	}
}

// TestHostListModelsKeepsTheTierRowUpstreamID pins that the tier is the
// clone's word: the row still asks the endpoint for the model's own id.
func TestHostListModelsKeepsTheTierRowUpstreamID(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
	  "type": "openai",
	  "api_url": "https://gw.example/v1/chat/completions",
	  "models": [
	    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
	     "reasoning_efforts": ["high", "xhigh"]}
	  ]}}}`)
	rows, err := hostListModels("", "", "")
	if err != nil {
		t.Fatalf("hostListModels: %v", err)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "minimax-m3.1-flash-") {
			continue
		}
		if r.ModelID != "MiniMax-M3.1-Flash" {
			t.Errorf("the row %q asks for %q, want MiniMax-M3.1-Flash", r.ID, r.ModelID)
		}
	}
}

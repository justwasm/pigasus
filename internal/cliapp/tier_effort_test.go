// Tests for the TUI's effort word on a model that declares reasoning tiers.
//
// The reference's effort slider has three places, so a model that offers
// five levels cannot ride it: the tier is read from the model's own label
// ("MiniMax M3.1 Flash (Xhigh)") and reaches the runtime as that word.
package cliapp

import (
	"flag"
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// TestCurrentEffortNameReadsTheTier pins the tier-aware word: the model's
// label carries the tier, and the flag still wins over it.
func TestCurrentEffortNameReadsTheTier(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
     "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
     "reasoning_effort": "high"}
  ]}}}`)
	newMode := func(model, effort string) *runMode {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("effort", effort, "")
		return &runMode{model: model, flags: fs}
	}
	cases := []struct {
		model  string
		effort string
		want   string
	}{
		{"MiniMax M3.1 Flash (Xhigh)", "", "xhigh"},
		{"MiniMax M3.1 Flash (Max)", "", "max"},
		{"MiniMax M3.1 Flash (Low)", "", "low"},
		{"MiniMax M3.1 Flash (Medium)", "", "medium"},
		// The flag is the user's own word and still wins.
		{"MiniMax M3.1 Flash (Xhigh)", "low", "low"},
		{"MiniMax M3.1 Flash (Low)", "max", "max"},
		// A model without tiers keeps the slider's word.
		{"MiniMax M3", "", "high"},
		// The reference's own rows are untouched.
		{"Gemini 3.8 Flash (High)", "", "high"},
		{"Gemini 3.8 Flash (Medium)", "", "medium"},
	}
	for _, c := range cases {
		m := newMode(c.model, c.effort)
		if got := m.currentEffortName(); got != c.want {
			t.Errorf("currentEffortName(model=%q, effort=%q) = %q, want %q", c.model, c.effort, got, c.want)
		}
	}
}

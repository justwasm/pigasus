// modelctx_test.go — the captured per-model context budget.
//
// 【一手】 The numbers come from the server's own catalogue, captured at
// agy 1.4.4 (docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json).
// Every entry carries `maxTokens` (the input limit) and a per-model
// checkpoint experiment naming `max_token_limit` and `max_output_tokens`;
// the effective limit is the smaller of the two, and the reserve is the
// checkpoint's output cap.
//
// The test pins the numbers for the models the reference lets the CLI
// select, so a table edit that drifts from the capture fails here.
//
// A model the user configured is not in that capture at all: its numbers
// come from the user's own configuration, and the second half of this file
// pins that path.

package modelctx

import (
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// TestCatalogueLimits pins the effective window and the reserve for every
// model the reference's own validator lets the CLI select.
func TestCatalogueLimits(t *testing.T) {
	configtest.Isolate(t)
	cases := []struct {
		id      string
		window  int
		reserve int
	}{
		// MODEL_PLACEHOLDER_M318/M319/M320 — maxTokens 1048576, checkpoint
		// limit 256000, checkpoint output 16384.
		{"gemini-3.8-flash-high", 256_000, 16_384},
		{"gemini-3.8-flash-medium", 256_000, 16_384},
		{"gemini-3.8-flash-low", 256_000, 16_384},
		// MODEL_PLACEHOLDER_M35/M26 — maxTokens 250000, checkpoint limit
		// 160000, checkpoint output 16384.
		{"claude-sonnet-4-6", 160_000, 16_384},
		{"claude-opus-4-6-thinking", 160_000, 16_384},
		// MODEL_OPENAI_GPT_OSS_120B_MEDIUM — maxTokens 131072, checkpoint
		// limit 80000, checkpoint output 8192.
		{"gpt-oss-120b-medium", 80_000, 8_192},
	}
	for _, c := range cases {
		got := For(c.id)
		if got.ContextWindow != c.window {
			t.Errorf("For(%q).ContextWindow = %d, want %d", c.id, got.ContextWindow, c.window)
		}
		if got.ReserveTokens != c.reserve {
			t.Errorf("For(%q).ReserveTokens = %d, want %d", c.id, got.ReserveTokens, c.reserve)
		}
	}
}

// TestForIsCaseInsensitive pins the lookup's tolerance: the table is keyed by
// the lower-case catalogue id.
func TestForIsCaseInsensitive(t *testing.T) {
	configtest.Isolate(t)
	want := For("gpt-oss-120b-medium")
	for _, id := range []string{"gpt-oss-120b-medium", "  gpt-oss-120b-medium  ", "GPT-OSS-120B-MEDIUM"} {
		if got := For(id); got != want {
			t.Errorf("For(%q) = %+v, want %+v", id, got, want)
		}
	}
}

// TestForResolvesTheDisplayLabel pins that a display label finds its
// catalogue entry. The TUI shows the label and PiG records it as the model
// id, so a lookup that only knew the slug would fall back to the 200 K
// window — a fifth of the real budget.
func TestForResolvesTheDisplayLabel(t *testing.T) {
	configtest.Isolate(t)
	want := For("gpt-oss-120b-medium")
	for _, id := range []string{"GPT-OSS 120B (Medium)", "gpt-oss 120b medium", "  GPT-OSS 120B (Medium)  "} {
		if got := For(id); got != want {
			t.Errorf("For(%q) = %+v, want %+v", id, got, want)
		}
	}
	if got := For("GPT-OSS 120B (Medium)"); got.ContextWindow != 80_000 {
		t.Errorf("window for the display label = %d, want %d", got.ContextWindow, 80_000)
	}
}

// TestConfiguredModelUsesItsOwnNumbers pins that a model the user
// configured is budgeted from the configuration: context_window is its input
// limit and default_max_tokens its output cap — the same pair the reference's
// catalogue carries.
//
// 【一手】 The numbers are measured against the provider, not read off a
// catalogue: a prompt of 1048203 tokens is accepted and one of about
// 1100000 is refused with "invalid params, context window exceeds limit",
// so 1048576 (1 Mi) is the limit that fits.
func TestConfiguredModelUsesItsOwnNumbers(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://host/v1",
		"models": [{"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash",
		            "model": "Upstream-Flash", "context_window": 1048576,
		            "default_max_tokens": 8192}]}}}`)

	got := For("minimax-m3.1-flash")
	if got.ContextWindow != 1_048_576 {
		t.Errorf("window = %d, want %d", got.ContextWindow, 1_048_576)
	}
	if got.ReserveTokens != 8_192 {
		t.Errorf("reserve = %d, want %d", got.ReserveTokens, 8_192)
	}
	// The display label is what the TUI records as the model id, so the
	// lookup has to find the entry by that name too.
	if got := For("MiniMax M3.1 Flash"); got.ContextWindow != 1_048_576 {
		t.Errorf("window for the display label = %d, want %d", got.ContextWindow, 1_048_576)
	}
}

// TestConfiguredModelWithoutNumbersFallsBack pins that a configured model
// that states no window gets the package defaults rather than zero.
func TestConfiguredModelWithoutNumbersFallsBack(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://host/v1",
		"models": [{"id": "bare", "name": "Bare"}]}}}`)
	got := For("bare")
	if got.ContextWindow != DefaultContextWindow {
		t.Errorf("window = %d, want %d", got.ContextWindow, DefaultContextWindow)
	}
	if got.ReserveTokens != DefaultReserveTokens {
		t.Errorf("reserve = %d, want %d", got.ReserveTokens, DefaultReserveTokens)
	}
}

// TestForUnknownModelFallsBack pins the unknown-model fallback.
func TestForUnknownModelFallsBack(t *testing.T) {
	configtest.Isolate(t)
	got := For("no-such-model")
	if got.ContextWindow != DefaultContextWindow {
		t.Errorf("unknown model window = %d, want %d", got.ContextWindow, DefaultContextWindow)
	}
	if got.ReserveTokens != DefaultReserveTokens {
		t.Errorf("unknown model reserve = %d, want %d", got.ReserveTokens, DefaultReserveTokens)
	}
}

// TestEveryLimitLeavesRoomForTheReserve guards the one property PiG's
// compaction rule needs: `contextWindow - reserveTokens` must stay
// positive, or every turn would cross the threshold.
func TestEveryLimitLeavesRoomForTheReserve(t *testing.T) {
	for id, m := range catalogue {
		l := m.limit()
		if l.ContextWindow <= 0 {
			t.Errorf("%s: window = %d, want > 0", id, l.ContextWindow)
		}
		if l.ReserveTokens >= l.ContextWindow {
			t.Errorf("%s: reserve %d leaves no room in a window of %d", id, l.ReserveTokens, l.ContextWindow)
		}
	}
}

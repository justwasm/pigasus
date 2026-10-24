// context_window_test.go — the context window every model handle
// advertises to PiG.
//
// PiG's auto-compaction rule is `contextTokens > contextWindow -
// reserveTokens` (internal/codingagent/compaction's ShouldCompact, called
// from coding/session.go's checkCompactionDecision). A handle without a
// window makes that rule degenerate — with a window of 0 every turn
// crosses the threshold — and it also disables Session.ContextUsage.
//
// 【一手】 The reference's window is per model: the server's catalogue
// carries `maxTokens` per model and a per-model checkpoint experiment
// naming `max_token_limit`, and the effective limit is the smaller of the
// two (internal/modelctx). The three model builders are the only places
// the clone produces a PiG model handle, so the per-model window is pinned
// on all three.

package piagentbridge

import (
	"testing"

	"github.com/justwasm/pigasus/internal/modelctx"
	"github.com/justwasm/pigasus/internal/pigrt"
)

// TestModelHandlesAdvertiseContextWindow pins the window on every model
// handle the clone builds. The three builders must agree for the same
// catalogue id — a handle that drifts from the table would compact at a
// different point than the reference.
func TestModelHandlesAdvertiseContextWindow(t *testing.T) {
	const gemini = "gemini-3.8-flash-medium"
	want := modelctx.ContextWindowFor(gemini)
	if want != 256_000 {
		t.Fatalf("modelctx.ContextWindowFor(%q) = %d, want 256000", gemini, want)
	}

	handles := map[string]func() int{
		"bridge.Model": func() int {
			return Model(gemini).Capabilities.ContextWindow
		},
		"BackendProvider.Model": func() int {
			p := NewBackendProvider(nil, gemini)
			return p.Model(gemini).Capabilities.ContextWindow
		},
		"Provider.Model": func() int {
			p := NewProvider(ProviderConfig{Model: "MiniMax-M3"})
			return p.Model(gemini).Capabilities.ContextWindow
		},
	}
	for name, window := range handles {
		if got := window(); got != want {
			t.Errorf("%s context window = %d, want %d", name, got, want)
		}
	}
}

// TestModelHandlesAdvertiseFallbackWindow pins the unknown-model fallback:
// a handle for a model the clone has no captured limit for still carries a
// window, so PiG's compaction rule does not degenerate.
func TestModelHandlesAdvertiseFallbackWindow(t *testing.T) {
	if pigrt.ContextWindow != modelctx.DefaultContextWindow {
		t.Fatalf("pigrt.ContextWindow = %d, want %d", pigrt.ContextWindow, modelctx.DefaultContextWindow)
	}
	if got := Model("no-such-model").Capabilities.ContextWindow; got != pigrt.ContextWindow {
		t.Errorf("unknown model context window = %d, want %d", got, pigrt.ContextWindow)
	}
}

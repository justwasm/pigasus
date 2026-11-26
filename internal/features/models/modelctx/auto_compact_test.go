package modelctx

import "testing"

func TestAutoCompactOverride(t *testing.T) {
	cases := []struct {
		name      string
		env       string
		maxTokens int
		wantTok   int
		wantSpec  string
		wantOK    bool
	}{
		{name: "empty", env: "", maxTokens: 1_000_000, wantOK: false},
		{name: "plain integer", env: "700000", maxTokens: 1_000_000, wantTok: 700_000, wantSpec: "700000", wantOK: true},
		{name: "trimmed integer", env: "  700000  ", maxTokens: 1_000_000, wantTok: 700_000, wantSpec: "700000", wantOK: true},
		{name: "K suffix", env: "800K", maxTokens: 1_000_000, wantTok: 800_000, wantSpec: "800K", wantOK: true},
		{name: "k suffix decimal", env: "12.5k", maxTokens: 1_000_000, wantTok: 12_500, wantSpec: "12.5k", wantOK: true},
		{name: "M suffix decimal", env: "0.5M", maxTokens: 1_000_000, wantTok: 500_000, wantSpec: "0.5M", wantOK: true},
		{name: "m suffix", env: "1m", maxTokens: 1_048_576, wantTok: 1_000_000, wantSpec: "1m", wantOK: true},
		{name: "percent", env: "80%", maxTokens: 250_000, wantTok: 200_000, wantSpec: "80%", wantOK: true},
		{name: "percent decimal floor", env: "33.3%", maxTokens: 1_000, wantTok: 333, wantSpec: "33.3%", wantOK: true},
		{name: "percent 100", env: "100%", maxTokens: 200_000, wantTok: 200_000, wantSpec: "100%", wantOK: true},
		{name: "percent over 100 rejected", env: "101%", maxTokens: 200_000, wantOK: false},
		{name: "percent zero rejected", env: "0%", maxTokens: 200_000, wantOK: false},
		{name: "percent without maxTokens rejected", env: "80%", maxTokens: 0, wantOK: false},
		{name: "exceeds maxTokens rejected", env: "800K", maxTokens: 250_000, wantOK: false},
		{name: "zero rejected", env: "0", maxTokens: 1_000_000, wantOK: false},
		{name: "negative rejected", env: "-100", maxTokens: 1_000_000, wantOK: false},
		{name: "leading dot rejected", env: ".5M", maxTokens: 1_000_000, wantOK: false},
		{name: "trailing dot rejected", env: "1.M", maxTokens: 1_000_000, wantOK: false},
		{name: "garbage rejected", env: "abc", maxTokens: 1_000_000, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvAutoCompactOverride, tc.env)
			gotTok, gotSpec, gotOK := AutoCompactOverride(tc.maxTokens)
			if gotOK != tc.wantOK || gotTok != tc.wantTok || gotSpec != tc.wantSpec {
				t.Fatalf("AutoCompactOverride(%d) with %q = (%d, %q, %v), want (%d, %q, %v)",
					tc.maxTokens, tc.env, gotTok, gotSpec, gotOK, tc.wantTok, tc.wantSpec, tc.wantOK)
			}
		})
	}
}

func TestAutoCompactWindow(t *testing.T) {
	t.Setenv(EnvAutoCompactOverride, "")
	if tok, spec := AutoCompactWindow("gemini-3.8-flash-high"); tok != 0 || spec != "" {
		t.Fatalf("default AutoCompactWindow = (%d, %q), want (0, \"\")", tok, spec)
	}

	t.Setenv(EnvAutoCompactOverride, "800K")
	if tok, spec := AutoCompactWindow("gemini-3.8-flash-high"); tok != 800_000 || spec != "800K" {
		t.Fatalf("gemini 800K AutoCompactWindow = (%d, %q), want (800000, \"800K\")", tok, spec)
	}
	// 800K exceeds Claude's 250k window, so it falls back to server default.
	if tok, spec := AutoCompactWindow("claude-sonnet-4-6"); tok != 0 || spec != "" {
		t.Fatalf("claude 800K AutoCompactWindow = (%d, %q), want (0, \"\")", tok, spec)
	}

	t.Setenv(EnvAutoCompactOverride, "80%")
	if tok, spec := AutoCompactWindow("claude-sonnet-4-6"); tok != 200_000 || spec != "80%" {
		t.Fatalf("claude 80%% AutoCompactWindow = (%d, %q), want (200000, \"80%%\")", tok, spec)
	}
}

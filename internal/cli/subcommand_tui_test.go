package cli

import (
	"testing"
)

func TestParseTUIFlags_PigasusAndBackends(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantBackend string
	}{
		{
			name:        "pigasus long flag",
			args:        []string{"--pigasus"},
			wantBackend: "pigasus",
		},
		{
			name:        "pigasus short flag",
			args:        []string{"-pigasus"},
			wantBackend: "pigasus",
		},
		{
			name:        "backend pigasus space",
			args:        []string{"--backend", "pigasus"},
			wantBackend: "pigasus",
		},
		{
			name:        "backend pigasus equals",
			args:        []string{"--backend=pigasus"},
			wantBackend: "pigasus",
		},
		{
			name:        "agy flag",
			args:        []string{"--agy"},
			wantBackend: "agy",
		},
		{
			name:        "codex flag",
			args:        []string{"--codex"},
			wantBackend: "codex",
		},
		{
			name:        "claude flag",
			args:        []string{"--claude"},
			wantBackend: "claude",
		},
		{
			name:        "default no backend",
			args:        []string{},
			wantBackend: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseTUIFlags(nil, tc.args, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if opts.backend != tc.wantBackend {
				t.Errorf("got backend %q, want %q", opts.backend, tc.wantBackend)
			}
		})
	}
}

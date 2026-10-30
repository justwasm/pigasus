package cel

import (
	"testing"
)

func TestPolicyEngine_Eval(t *testing.T) {
	tests := []struct {
		name       string
		rule       string
		vars       map[string]any
		wantResult bool
		wantErr    bool
	}{
		{
			name: "command starts with git",
			rule: `command.startsWith("git ")`,
			vars: map[string]any{
				"command": "git status",
			},
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "forbidden tool",
			rule: `tool == "rm" || tool == "dd"`,
			vars: map[string]any{
				"tool": "ls",
			},
			wantResult: false,
			wantErr:    false,
		},
		{
			name: "path contains sensitive dir",
			rule: `path.contains("/etc/")`,
			vars: map[string]any{
				"path": "/etc/passwd",
			},
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "env check",
			rule: `env["ENV"] == "production"`,
			vars: map[string]any{
				"env": map[string]string{"ENV": "production"},
			},
			wantResult: true,
			wantErr:    false,
		},
		{
			name:       "invalid expression non bool output",
			rule:       `command + " suffix"`,
			vars:       map[string]any{},
			wantResult: false,
			wantErr:    true,
		},
		{
			name:       "syntax error",
			rule:       `command === "invalid"`,
			vars:       map[string]any{},
			wantResult: false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalRule(tt.rule, tt.vars)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EvalRule() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantResult {
				t.Fatalf("EvalRule() got = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

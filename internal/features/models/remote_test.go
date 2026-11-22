package models_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/features/models"
)

func TestIsRemoteAgentSupported(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"Gemini 3.8 Flash", true},
		{"gemini-3.8-flash-high", true},
		{"Gemini 3.8 Pro (High)", true},
		{"Claude Sonnet 4.6 (Thinking)", true},
		{"claude-sonnet-4-6", true},
		{"Claude Opus 4.6 (Thinking)", true},
		{"claude-opus-4-6-thinking", true},
		{"Claude Opus 5.5", true},
		{"Claude Sonnet 5.5", true},
		{"GPT-OSS 120B (Medium)", false},
		{"gpt-oss-120b-medium", false},
		{"Gemini 4 Carbon", false},
		{"Gemini 4 Argon", false},
		{"custom-local-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := models.IsRemoteAgentSupported(tt.model)
			if got != tt.want {
				t.Errorf("IsRemoteAgentSupported(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestRemoteAgentUnsupportedMessage(t *testing.T) {
	msg := models.RemoteAgentUnsupportedMessage("GPT-OSS 120B (Medium)")
	want := "GPT-OSS 120B (Medium) isn't available for remote conversations yet. Choose another model and try again."
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

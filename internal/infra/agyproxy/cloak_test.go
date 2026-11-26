package agyproxy

import (
	"strings"
	"testing"
)

func TestCloakSystemPrompt(t *testing.T) {
	// Normal prompt untouched
	normal := "You are a helpful coding assistant."
	if got := cloakSystemPrompt(normal); got != normal {
		t.Errorf("got %q, want %q", got, normal)
	}

	// Claude Code billing & identity triggers
	claudePrompt := "You are running with X-Anthropic-Billing-Header: test and Built on Anthropic's Claude Agent SDK."
	cloaked := cloakSystemPrompt(claudePrompt)
	if strings.Contains(strings.ToLower(cloaked), "built on anthropic's claude agent sdk") {
		t.Errorf("cloaked prompt still contains exact blacklisted phrase: %q", cloaked)
	}
	if !strings.Contains(cloaked, "\u200b") {
		t.Errorf("cloaked prompt should contain zero-width space: %q", cloaked)
	}

	// Codex trigger
	codexPrompt := "You are Codex, an agent based on GPT-5."
	cloakedCodex := cloakSystemPrompt(codexPrompt)
	if strings.Contains(strings.ToLower(cloakedCodex), "you are codex, an agent based on gpt-5") {
		t.Errorf("cloaked codex prompt still contains exact blacklisted phrase: %q", cloakedCodex)
	}

	// Empty prompt
	if got := cloakSystemPrompt(""); got != "" {
		t.Errorf("empty prompt = %q, want empty", got)
	}
}
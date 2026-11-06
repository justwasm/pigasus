package slashcommands

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func TestFilterScoped_PigasusNative(t *testing.T) {
	ctx := ScopeContext{
		EngineName:   "pigasus",
		Capabilities: agentturn.CapAll,
	}
	all := FilterScoped("", ctx)
	if len(all) != len(Default) {
		t.Fatalf("expected all %d default commands for native pigasus, got %d", len(Default), len(all))
	}
}

func TestFilterScoped_CodexBackend(t *testing.T) {
	ctx := ScopeContext{
		EngineName:   "codex",
		Capabilities: agentturn.CapModelSwitch | agentturn.CapReasoningTier | agentturn.CapContextInspect,
		Dynamic: []Command{
			{Name: "review", Description: "Review changes", Badge: "codex"},
		},
	}
	cmds := FilterScoped("", ctx)

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}

	// 1. Host commands MUST be present
	for _, want := range []string{"/help", "/clear", "/exit", "/copy", "/codesearch"} {
		if !names[want] {
			t.Errorf("host command %s should be visible in codex mode", want)
		}
	}

	// 2. Capabilities supported by Codex MUST be present
	for _, want := range []string{"/model", "/effort", "/compact"} {
		if !names[want] {
			t.Errorf("capability command %s should be visible in codex mode", want)
		}
	}

	// 3. Pigasus-exclusive commands MUST NOT be present
	for _, forbidden := range []string{"/accounts", "/pool", "/credits", "/feature-diff", "/hooks", "/skills", "/plugin"} {
		if names[forbidden] {
			t.Errorf("pigasus-exclusive command %s must NOT be visible in codex mode", forbidden)
		}
	}

	// 4. Dynamic commands from Codex MUST be present
	if !names["/review"] {
		t.Errorf("dynamic command /review should be visible")
	}
}

func TestFilterScoped_ACPBackend(t *testing.T) {
	ctx := ScopeContext{
		EngineName:   "acp",
		Capabilities: agentturn.CapNone,
		Dynamic: []Command{
			{Name: "explain", Description: "Explain code", Badge: "acp"},
		},
	}
	cmds := FilterScoped("", ctx)

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}

	// 1. Host commands present
	if !names["/help"] || !names["/exit"] {
		t.Errorf("host commands should be present in ACP mode")
	}

	// 2. Capability command /model, /tasks and /agents must NOT be present when capability flags are absent
	if names["/model"] {
		t.Errorf("/model should not be visible when CapModelSwitch is missing")
	}
	if names["/tasks"] {
		t.Errorf("/tasks should not be visible when CapTasks is missing")
	}
	if names["/agents"] {
		t.Errorf("/agents should not be visible when CapAgents is missing")
	}

	// 3. Dynamic command /explain present
	if !names["/explain"] {
		t.Errorf("dynamic ACP command /explain should be present")
	}
}

func TestFilterScoped_ACPBackend_WithTasks(t *testing.T) {
	ctx := ScopeContext{
		EngineName:   "acp",
		Capabilities: agentturn.CapTasks,
	}
	cmds := FilterScoped("", ctx)
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}
	if !names["/tasks"] {
		t.Errorf("/tasks should be visible when CapTasks capability is provided")
	}
}

func TestFilterScoped_ACPBackend_WithAgents(t *testing.T) {
	ctx := ScopeContext{
		EngineName:   "acp",
		Capabilities: agentturn.CapAgents,
	}
	cmds := FilterScoped("", ctx)
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}
	if !names["/agents"] {
		t.Errorf("/agents should be visible when CapAgents capability is provided")
	}
}

func TestFilterScoped_AgyBackend(t *testing.T) {
	ctx := ScopeContext{
		EngineName:   "agy",
		Capabilities: agentturn.CapAll,
	}
	cmds := FilterScoped("", ctx)

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}

	// 1. Reference commands MUST be visible for agy
	for _, want := range []string{"/help", "/clear", "/exit", "/model", "/effort", "/plugin", "/skills", "/mcp", "/accounts", "/pool", "/credits"} {
		if !names[want] {
			t.Errorf("reference command %s should be visible in agy mode", want)
		}
	}

	// 2. Pigasus-only extensions MUST NOT be visible for official agy
	for _, forbidden := range []string{"/provider", "/feature-diff"} {
		if names[forbidden] {
			t.Errorf("pigasus-only command %s must NOT be visible in official agy mode", forbidden)
		}
	}
}

func TestFilterScoped_GrokBackend(t *testing.T) {
	ctx := ScopeContext{
		EngineName:   "grok",
		Capabilities: agentturn.CapModelSwitch | agentturn.CapReasoningTier | agentturn.CapContextInspect,
	}
	cmds := FilterScoped("", ctx)

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}

	// 1. Host commands MUST be visible in Grok mode
	for _, want := range []string{"/help", "/clear", "/exit", "/copy", "/diff"} {
		if !names[want] {
			t.Errorf("host command %s should be visible in grok mode", want)
		}
	}

	// 2. Capabilities supported by Grok MUST be visible
	for _, want := range []string{"/model", "/effort", "/compact", "/context"} {
		if !names[want] {
			t.Errorf("capability command %s should be visible in grok mode", want)
		}
	}

	// 3. Pigasus-only and Google-account commands MUST NOT be visible
	for _, forbidden := range []string{"/provider", "/feature-diff", "/accounts", "/pool", "/credits", "/plugin", "/skills"} {
		if names[forbidden] {
			t.Errorf("command %s must NOT be visible in grok mode", forbidden)
		}
	}
}

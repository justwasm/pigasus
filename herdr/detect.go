package herdr

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Recognized canonical agent identifiers matching upstream Herdr detect manifests.
const (
	AgentAgy       = "agy"
	AgentClaude    = "claude"
	AgentCodex     = "codex"
	AgentCrush     = "crush"
	AgentGemini    = "gemini"
	AgentCursor    = "cursor"
	AgentDevin     = "devin"
	AgentCopilot   = "copilot"
	AgentKimi      = "kimi"
	AgentQwen      = "qwen"
	AgentPi        = "pi"
	AgentHermes    = "hermes"
	AgentCollie    = "collie"
	AgentOpenCode  = "opencode"
	AgentAmp       = "amp"
	AgentDroid     = "droid"
)

// LookupAgent maps any process name, alias, command, or label to its canonical Herdr agent ID.
// Supports agy/antigravity, claude/claude-code, codex, crush, gemini, cursor, devin, copilot, etc.
func LookupAgent(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", false
	}

	// Strip path prefix and extensions (e.g. "/usr/local/bin/agy" -> "agy", "claude.exe" -> "claude")
	base := filepath.Base(name)
	base = strings.TrimSuffix(base, ".exe")
	lower := strings.ToLower(base)

	// Direct match table matching upstream Herdr lookup_agent
	switch lower {
	case "agy", "antigravity", "antigravity-cli", "openagy", "pigasus":
		return AgentAgy, true
	case "claude", "claude-code":
		return AgentClaude, true
	case "codex":
		return AgentCodex, true
	case "crush":
		return AgentCrush, true
	case "gemini", "gemini-cli":
		return AgentGemini, true
	case "cursor", "cursor-agent":
		return AgentCursor, true
	case "devin", "devin-cli":
		return AgentDevin, true
	case "copilot", "github-copilot", "ghcs":
		return AgentCopilot, true
	case "kimi", "kimi-code":
		return AgentKimi, true
	case "qwen", "qwen-code":
		return AgentQwen, true
	case "pi":
		return AgentPi, true
	case "hermes", "hermes-agent":
		return AgentHermes, true
	case "collie":
		return AgentCollie, true
	case "opencode", "open-code":
		return AgentOpenCode, true
	case "amp", "amp-local":
		return AgentAmp, true
	case "droid":
		return AgentDroid, true
	}

	// Prefix / compound match (e.g. "agy-worker", "claude-3-5", "codex-runner")
	for _, prefix := range []string{"agy", "claude", "codex", "crush", "gemini", "collie"} {
		if strings.HasPrefix(lower, prefix+"-") || strings.HasPrefix(lower, prefix+"_") {
			return prefix, true
		}
	}

	return "", false
}

// Regex patterns for terminal screen content matching based on Herdr manifests.
var (
	// Antigravity (agy) patterns: braille spinner, esc to cancel, permission prompt
	agyBlockedRe = regexp.MustCompile(`(?i)(navigate.*tab amend|requesting permission for:|do you trust the contents of this project\?|do you want to proceed\?)`)
	agyWorkingRe = regexp.MustCompile(`(?i)(esc to cancel|[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]|· \d+ task\(s\)|thinking|executing)`)
	agyIdleRe    = regexp.MustCompile(`(?i)(\? for shortcuts|❯|sh-\d|\$ )`)

	// Claude Code patterns: esc to interrupt, waiting for permission, background agents
	claudeBlockedRe = regexp.MustCompile(`(?i)(do you want to proceed\?|waiting for permission|do you want to allow this connection\?)`)
	claudeWorkingRe = regexp.MustCompile(`(?i)(esc to interrupt|waiting for \d+ background agent|[⏸⏵]|· \d+ MCP task)`)
	claudeIdleRe    = regexp.MustCompile(`(?i)(claude code|❯|bypass permissions)`)

	// Codex patterns: Action Required, Trust this folder
	codexBlockedRe = regexp.MustCompile(`(?i)(action required|trust this folder\?|trust and continue)`)
	codexWorkingRe = regexp.MustCompile(`(?i)(thinking|[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏])`)
	codexIdleRe    = regexp.MustCompile(`(?i)(codex|enter continue)`)

	// Crush patterns
	crushBlockedRe = regexp.MustCompile(`(?i)(\[security\].*confirmation|blocked pending)`)
	crushWorkingRe = regexp.MustCompile(`(?i)(\[crush\].*(analyzing|reading|step|generating|refactoring))`)
)

// DetectAgentFromPane analyzes a pane's explicit state, process title, or recent terminal buffer lines.
// It returns the canonical agent name, state ("working", "blocked", "idle"), subtitle message, and whether an agent was detected.
func DetectAgentFromPane(p *Pane) (string, string, string, bool) {
	if p == nil {
		return "", "", "", false
	}

	// 1. Check if agent was explicitly reported (via socket API or SetAgentState)
	name, state, msg := p.AgentState()
	if name != "" {
		if canonical, ok := LookupAgent(name); ok {
			if state == "" {
				state = "idle"
			}
			return canonical, state, msg, true
		}
	}

	// 2. Check title / process name / dynamic terminal title (OSC 0/2)
	if canonical, ok := LookupAgent(p.Title); ok {
		state, msg := inferAgentStateFromOutput(canonical, p.PlainText())
		return canonical, state, msg, true
	}
	if termTitle := p.TermTitle(); termTitle != "" {
		if canonical, ok := LookupAgent(termTitle); ok {
			state, msg := inferAgentStateFromOutput(canonical, p.PlainText())
			return canonical, state, msg, true
		}
	}

	// 3. Check live foreground process / command line
	if agent, ok := p.ForegroundAgent(); ok {
		state, msg := inferAgentStateFromOutput(agent, p.PlainText())
		return agent, state, msg, true
	}

	// 4. Check screen plain text output ONLY for definitive, unmistakable agent startup banners
	lines := p.PlainText()
	joined := strings.Join(lines, "\n")

	// Check Antigravity / Pigasus Agent Runtime banner
	// NOTE: NEVER match bare directory strings like "openagy" or "pigasus" to avoid false positives on normal shells!
	if strings.Contains(joined, "Pigasus Agent Runtime") || strings.Contains(joined, "[TURN]") ||
		strings.Contains(joined, "Antigravity Agent") || strings.Contains(joined, "Google Deepmind") {
		state, msg := inferAgentStateFromOutput(AgentAgy, lines)
		return AgentAgy, state, msg, true
	}

	// Check Claude Code definitive header
	if strings.Contains(joined, "Claude Code") || strings.Contains(joined, "claude-code") || strings.Contains(joined, "Welcome to Claude Code") {
		state, msg := inferAgentStateFromOutput(AgentClaude, lines)
		return AgentClaude, state, msg, true
	}

	// Check OpenAI Codex banner
	if strings.Contains(joined, "OpenAI Codex") || strings.Contains(joined, "› Ask Codex") || strings.Contains(joined, "Trust this folder?") {
		state, msg := inferAgentStateFromOutput(AgentCodex, lines)
		return AgentCodex, state, msg, true
	}

	// Check Crush banner
	if strings.Contains(joined, "[crush]") || strings.Contains(joined, "crush run") {
		state, msg := inferAgentStateFromOutput(AgentCrush, lines)
		return AgentCrush, state, msg, true
	}

	return "", "", "", false
}

func inferAgentStateFromOutput(agent string, lines []string) (state, msg string) {
	joined := strings.Join(lines, "\n")

	switch agent {
	case AgentAgy:
		if agyBlockedRe.MatchString(joined) {
			return "blocked", "Awaiting tool permission"
		}
		if agyWorkingRe.MatchString(joined) {
			return "working", "Executing turn"
		}
		return "idle", "Ready for prompt"

	case AgentClaude:
		if claudeBlockedRe.MatchString(joined) {
			return "blocked", "Awaiting confirmation"
		}
		if claudeWorkingRe.MatchString(joined) {
			return "working", "Generating response"
		}
		return "idle", "Ready for prompt"

	case AgentCodex:
		if codexBlockedRe.MatchString(joined) {
			return "blocked", "Action required"
		}
		if codexWorkingRe.MatchString(joined) {
			return "working", "Thinking..."
		}
		return "idle", "Ready"

	case AgentCrush:
		if crushBlockedRe.MatchString(joined) {
			return "blocked", "Awaiting tool permission"
		}
		if crushWorkingRe.MatchString(joined) {
			return "working", "Executing task"
		}
		return "idle", "Ready"
	}

	return "idle", "Active"
}

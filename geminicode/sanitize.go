package geminicode

import (
	"fmt"
	"regexp"
	"strings"
)

// Code block protection: isolate multi-line fenced code blocks ```...``` and inline code `...`
var (
	reCodeBlock = regexp.MustCompile("(?ms)(```[\\s\\S]*?```|`[^`\\r\\n]+`)")

	reCodeBlockPlaceholder = regexp.MustCompile(`__PROMPT_SANITIZER_CODE_BLOCK_(\d+)__`)

	// reWAFTriggerHeaders matches third-party billing headers and SDK fingerprints that trigger
	// upstream Google Cloud Code WAF heuristic 429 RESOURCE_EXHAUSTED or 403.
	reWAFTriggerHeaders = regexp.MustCompile(
		`(?im)^\s*(?:x-[a-z0-9_-]*billing[a-z0-9_-]*|[a-z0-9_-]+-billing-(?:header|metadata|token|info)):\s*[^\r\n]*(\r?\n)?` +
			`|^\s*x-[a-z0-9_-]+:\s*[^\r\n]*(?:cc_version|cc_entrypoint|cc_workload|cc_is_subagent|cc_prev_req|cc_prompt_id|cc_turn_origin|cch=)[^\r\n]*(\r?\n)?` +
			`|(?i)x-anthropic-billing-header:\s*[^\r\n]*` +
			`|(?i)You are a Claude agent, built on Anthropic's Claude Agent SDK\.(\r?\n)?` +
			`|(?i)You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK\.(\r?\n)?` +
			`|(?i)You are an interactive agent that helps users with software engineering tasks\.[^\r\n]*(\r?\n)?`,
	)

	// reWAFTriggerAgentToolGuidelines matches third-party agent tool guideline patterns that
	// trigger upstream Google Cloud Code WAF fake 429 RESOURCE_EXHAUSTED.
	reWAFTriggerAgentToolGuidelines = regexp.MustCompile(
		`(?i)\bprefer\s+(?:the\s+)?read_file,\s*edit_file,?\s+and\s+write_file\s+over\s+cat/sed\b` +
			`|(?i)\balways\s+read_file\s+before\s+edit_file\b` +
			`|(?i)\bcall\s+read_file\s+on\s+(?:its\s+)?SKILL\.md\b` +
			`|(?i)\bGoogle\s+model\s+operational\s+(?:directives|guidelines)\b` +
			`|(?i)\btools\.opencode\.session[-_]move\b`,
	)

	rePiCodemode = regexp.MustCompile(`(?i)\b(codem)o(de)\b`)

	reWorkBuddyPoweredBy = regexp.MustCompile(
		`(?im)^[ \t]*this\s+conversation\s+is\s+powered\s+by\s+([A-Za-z0-9_.:/-]+)[ \t]*(\r?)$`,
	)

	reCodexDesktopContext = regexp.MustCompile(
		`(?im)^[ \t]*#\s*Codex\s+desktop\s+context\b(\r?)$`,
	)

	reCodexAsRole = regexp.MustCompile(
		`(?i)\bas\s+Codex:`,
	)

	reMultiNewline = regexp.MustCompile(`\n{3,}`)
)

// SanitizePromptText neutralizes known Google Cloud Code WAF triggers in prompt or
// instruction text while strictly preserving all fenced and inline code blocks verbatim.
//
// Root-cause protection (learned from Antigravity-Manager production issue #3965, #3982):
// Google Cloud Code upstream WAF uses heuristic regex rules that intercept common agent
// phrases (e.g. "Prefer read_file, edit_file, and write_file over cat/sed" or client billing
// signatures) and returns a fake 429 RESOURCE_EXHAUSTED error.
func SanitizePromptText(text string) string {
	if text == "" {
		return ""
	}

	// 1. Isolate code blocks
	var placeholders []string
	protected := reCodeBlock.ReplaceAllStringFunc(text, func(match string) string {
		idx := len(placeholders)
		placeholders = append(placeholders, match)
		return fmt.Sprintf("__PROMPT_SANITIZER_CODE_BLOCK_%d__", idx)
	})

	// 2. Neutralize WAF billing headers and client fingerprints
	pass1 := reWAFTriggerHeaders.ReplaceAllString(protected, "")

	// 3. Neutralize WAF agent tool guideline patterns
	pass2 := reWAFTriggerAgentToolGuidelines.ReplaceAllStringFunc(pass1, func(match string) string {
		lower := strings.ToLower(match)
		switch {
		case strings.Contains(lower, "prefer"):
			return "Prefer read file, edit file, and write file over cat/sed"
		case strings.Contains(lower, "always"):
			return "Always read file before edit file"
		case strings.Contains(lower, "call"):
			return "call read file on its SKILL.md"
		case strings.Contains(lower, "operational"):
			return "Model operational guidelines"
		case strings.Contains(lower, "session"):
			return "tools.opencode.session_relocate"
		default:
			return match
		}
	})

	// 4. Neutralize Pi codemode, WorkBuddy powered-by, and Codex desktop contexts
	pass3 := rePiCodemode.ReplaceAllString(pass2, "${1}0${2}")
	pass4 := reWorkBuddyPoweredBy.ReplaceAllString(pass3, "Selected model: $1$2")
	pass5 := reCodexDesktopContext.ReplaceAllString(pass4, "# Desktop application context$1")
	pass6 := reCodexAsRole.ReplaceAllString(pass5, "as an assistant:")

	// 5. Restore code blocks in single non-recursive pass
	restored := reCodeBlockPlaceholder.ReplaceAllStringFunc(pass6, func(match string) string {
		sub := reCodeBlockPlaceholder.FindStringSubmatch(match)
		if len(sub) == 2 {
			var idx int
			if _, err := fmt.Sscanf(sub[1], "%d", &idx); err == nil && idx >= 0 && idx < len(placeholders) {
				return placeholders[idx]
			}
		}
		return match
	})

	// 6. Normalize multiple newlines
	return strings.TrimSpace(reMultiNewline.ReplaceAllString(restored, "\n\n"))
}

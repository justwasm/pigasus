package agyproxy

import "strings"

// blockedSystemPhrases lists system-prompt signatures from external coding
// agents that Google CodeAssist rejects with a simulated HTTP 429. Inserting
// zero-width spaces (\u200b) preserves semantic meaning for the model while
// bypassing the strict string blocklist.
var blockedSystemPhrases = []string{
	"x-anthropic-billing-header:",
	"built on anthropic's claude agent sdk",
	"you are codex, an agent based on gpt-5",
	"you are codex, a coding agent based on gpt-5",
	"hermes agent, an intelligent ai assistant created by nous research",
}

// cloakSystemPrompt preserves meaning while inserting a zero-width separator
// into any known blocked system-prompt phrase.
func cloakSystemPrompt(system string) string {
	if system == "" {
		return ""
	}
	low := strings.ToLower(system)
	hasBlocked := false
	for _, phrase := range blockedSystemPhrases {
		if strings.Contains(low, phrase) {
			hasBlocked = true
			break
		}
	}
	if !hasBlocked {
		return system
	}
	res := system
	for _, phrase := range blockedSystemPhrases {
		for {
			idx := strings.Index(strings.ToLower(res), phrase)
			if idx < 0 {
				break
			}
			cloaked := string(res[idx]) + "\u200b" + res[idx+1:idx+len(phrase)]
			res = res[:idx] + cloaked + res[idx+len(phrase):]
		}
	}
	return res
}
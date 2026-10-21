package main

import (
	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/pigrt"
)

// systemPrompt builds the Antigravity CLI system prompt for this run mode.
//
// Before cycle agy-system-prompt the TUI passed no SystemPrompt at all, so PiG
// sent the conversation with no instructions while the reference sent its
// 34 KB prompt. This restores the reference's prompt, including the skill list
// the model can actually read (the same agy roots pigrt discovers).
//
// It is rebuilt per prompt: skill discovery is a handful of small file reads
// and the run mode's cwd/conversation id can change between turns.
func (m *runMode) systemPrompt() string {
	opts := m.promptOptions()
	if m.agentBody != "" {
		return agyprompt.BuildAgent(m.agentBody, opts)
	}
	return agyprompt.Build(opts)
}

// promptOptions assembles the reference prompt inputs. It is shared by
// systemPrompt and the /context token breakdown so the panel measures
// exactly the text the next request will send.
func (m *runMode) promptOptions() agyprompt.Options {
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	conversationID := m.conversationID
	if conversationID == "" {
		conversationID = newConversationID()
	}
	agentDir := auth.AntigravityConfigHome()
	opts := agyprompt.DefaultOptions(workspace, agentDir, conversationID)
	opts.UserRules = agyprompt.DiscoverUserRules(workspace, agentDir)
	opts.Skills = agyprompt.DiscoverSkills(pigrt.AgySkillPaths(agentDir, m.cwd))
	if m.agentBody == "" {
		opts.Subagents = append(opts.Subagents, customSubagents()...)
	}
	return opts
}

// promptSectionTokens splits the reference prompt into the three
// categories 1.4.3's /context panel reports separately. The reference
// tokenizes prompt text at exactly three bytes per token
// (EstimateTokenCounter's divisor is 3.0), and its "System prompt"
// row excludes the <skills> and <subagents> bodies because those rows
// are printed again on their own. That exclusion is why the live 1.4.3
// panel shows 11.5k for a 58.8 KB instruction block: the 22.3 KB skills
// section and 2.1 KB subagents section are removed first.
func (m *runMode) promptSectionTokens() (base, skills, subagents int) {
	opts := m.promptOptions()
	if m.agentBody != "" {
		full := agyprompt.BuildAgent(m.agentBody, opts)
		return len(full) / 3, 0, 0
	}
	full := agyprompt.Build(opts)
	skills = promptSectionTokenCount("skills", agyprompt.SectionBody(opts, "skills"))
	subagents = promptSectionTokenCount("subagents", agyprompt.SectionBody(opts, "subagents"))
	base = (len(full) - skills*3 - subagents*3) / 3
	if base < 0 {
		base = 0
	}
	return base, skills, subagents
}

// promptSectionTokenCount measures one complete XML section the way the
// reference's step tokenizer does: the body plus its opening/closing tags and
// the newline that follows it. The +1 is load-bearing for <subagents>: its
// 2,083-byte body is 701 tokens on its own but the reference prints 702.
func promptSectionTokenCount(name, body string) int {
	if body == "" {
		return 0
	}
	return (len(body) + 2*len(name) + 2 + 1) / 3
}

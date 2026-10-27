// prompt.go — the system instruction the harness sends to the model.
//
// # Who owns the prompt
//
// The harness owns it, but the client may override it. The SDK exposes
// two forms, and the protocol carries both:
//
//	SystemInstructions.custom     a full replacement
//	SystemInstructions.appended   the default plus an identity override
//	                             and extra sections
//
// The SDK maps its own types onto those:
//
//	CustomSystemInstructions(text)      → custom with one text part
//	TemplatedSystemInstructions(...)    → appended
//	a plain string                      → appended with one section
//
// The reference renders a `custom` text part as a `<System>` block and a
// `custom` builtin section as that section. An unknown section name is an
// error, and the reference reports it as
// `unknown builtin prompt section: "<name>"`.
//
// # The default
//
// With no instructions at all the harness sends the reference's own
// default prompt — the one `harnessprompt` reproduces byte for
// byte from a capture of the reference binary.
package main

import (
	"strings"

	"github.com/justwasm/pigasus/harnessprompt"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// systemPrompt renders the session's system instruction.
func (c *conversation) systemPrompt() string {
	opts := c.promptOptions()
	instructions := c.cfg.GetSystemInstructions()
	if instructions == nil {
		return harnessprompt.Build(opts)
	}

	if custom := instructions.GetCustom(); custom != nil {
		prompt, err := harnessprompt.Custom(opts, customParts(custom))
		if err != nil {
			// The reference refuses the session's instruction and
			// reports the error. The clone keeps the default prompt so
			// the session still runs, and says why.
			c.logger.Error("harness: system instructions rejected", "error", err)
			return harnessprompt.Build(opts)
		}
		return prompt
	}

	if appended := instructions.GetAppended(); appended != nil {
		opts.Identity = strings.TrimSpace(appended.GetCustomIdentity())
		return harnessprompt.Appended(opts, appendedSections(appended))
	}

	return harnessprompt.Build(opts)
}

// promptOptions renders the session's prompt options.
func (c *conversation) promptOptions() harnessprompt.Options {
	return harnessprompt.Options{
		Behavior:       c.settings.Behavior,
		Workspaces:     c.settings.Workspaces,
		AppDataDir:     c.settings.AppDataDir,
		ConversationID: c.settings.ConversationID,
		OSName:         c.settings.OSName,
		// The subagent tool decides two things in the prompt: whether
		// the `<subagents>` section is there, and whether the
		// messaging section counts agents among the message sources.
		SubagentTool: c.sideToolSubset()["invoke_subagent"],
		// The user-queued message feature is part of every session the
		// reference opens; nothing in the config turns it off.
		MessageTool: true,
		Subagents:   c.promptSubagents(),
	}
}

// promptSubagents lists the subagents the prompt names.
//
// The reference's own two come first, then the client's own — the
// protocol's `HarnessConfig.custom_subagents`.
func (c *conversation) promptSubagents() []harnessprompt.Subagent {
	out := harnessprompt.BuiltinSubagents()
	for _, agent := range c.cfg.GetCustomSubagents() {
		if agent == nil || strings.TrimSpace(agent.GetName()) == "" {
			continue
		}
		out = append(out, harnessprompt.Subagent{
			Name:        agent.GetName(),
			Description: agent.GetDescription(),
		})
	}
	return out
}

// customParts converts the protocol's custom parts into the package's
// own shape.
func customParts(custom *pb.CustomSystemInstructions) []harnessprompt.CustomPart {
	parts := custom.GetPart()
	out := make([]harnessprompt.CustomPart, 0, len(parts))
	for _, part := range parts {
		if part == nil {
			continue
		}
		switch {
		case part.GetText() != "":
			out = append(out, harnessprompt.CustomPart{Text: part.GetText()})
		case part.GetBuiltinSection() != "":
			out = append(out, harnessprompt.CustomPart{BuiltinSection: part.GetBuiltinSection()})
		case part.GetTemplate() != nil:
			out = append(out, harnessprompt.CustomPart{Template: part.GetTemplate().GetName()})
		}
	}
	return out
}

// appendedSections converts the protocol's appended sections into the
// package's own shape.
func appendedSections(appended *pb.AppendedSystemInstructions) []harnessprompt.AppendedSection {
	sections := appended.GetAppendedSections()
	out := make([]harnessprompt.AppendedSection, 0, len(sections))
	for _, section := range sections {
		if section == nil {
			continue
		}
		title := strings.TrimSpace(section.GetTitle())
		if title == "" {
			title = "user_system_instructions"
		}
		out = append(out, harnessprompt.AppendedSection{
			Title:   title,
			Content: section.GetContent(),
		})
	}
	return out
}

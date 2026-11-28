package main

// slashprompts.go — the wire shape of the prompt-sending slash commands.
//
// # Why this is a separate axis from the scenarios
//
// The scenario suite in scenarios.go answers "does the outbound request's
// systemInstruction match the templates?". A prompt-sending command
// (`/learn`, `/goal`, `/plan`, …) is not part of that request at all: it is a
// *user turn*, and its whole content is the string the command contributes.
// So the parity question is narrower and harder: is that string the
// reference's string, byte for byte?
//
// # Where the reference string comes from
//
// 【一手】 The agy 1.5.18 binary carries each of these prompts as one literal.
// `/learn`, for instance, sits at file offset 0x3ee0d64 as a 1,965 B block
// wrapped in `<LEARN>` … `</LEARN>`, with a single `%s` filled from a second
// literal (`Check the `%s` skill in the skills section …`, `%s` =
// `agy-customizations`). The body below is that block with the wrapper
// stripped and the `%s` filled.
//
// # How the check works
//
// The clone side is the live function (`slashactions.LearnPrompt()`), not a
// copy, so this gate fails the moment the two disagree. The reference side is
// the recovered literal, kept here as the pin. That direction matters: a test
// that compared the clone's function to the clone's own testdata would pass
// forever while the shipped prompt drifted from the reference.
//
// Six commands are pinned, in three different senses. Four are *sent* as an
// expansion (/learn, /goal, /grill-me, /schedule); two are recovered but
// *never sent* (/plan and /teamwork — see the NotSent flag and each Why);
// and one, /boost, is *not sent on this account* for a reason that lives
// outside the binary.
//
// The distinction matters most for /boost. The reference DOES ship a /boost
// body — the `<ORCHESTRATOR>` wrapper and the owl.OrchestratorPrompt text —
// and the clone reproduces that text faithfully. What the reference does not
// do, on this account, is send it: a server-issued admin control
// (MultiagentControls.boost_command_disabled) switches the command off, and
// the same message's teamwork_preview_command_disabled explains why
// /teamwork-preview behaves the same way. So the /boost pin asserts the
// disabled behaviour, and it is marked contingent: it is a statement about
// the account, not about the binary.
//
// `/schedule` was once in the same "cannot be pinned" bucket as /boost, and
// that was wrong for a different reason: its body is one plain line
// ("Please use the schedule tool to create a schedule as specified by the
// user"), it is only the picker description that differs, and a live capture
// confirmed the command expands. It is pinned as MATCH.
//
// # What a live capture added, and what it corrected
//
// The binary literals alone left one question open for `/teamwork`: its text
// carries two indexed verbs (`%[1]s` the tool, `%[2]s` the team's root type),
// and a pin cannot be written until both are known. Rather than guess, the
// reference was driven through this repo's own capture proxy
// (`wirealign server` + `CLOUD_CODE_URL`) — the TUI in tmux and `-p`/`--mode`
// printmode — and the outbound requests were read back. That settled the
// names, and it corrected two things this file had assumed:
//
//	1. The reference does NOT send a slash command's body as the user turn.
//	   It sends the command text verbatim (`/learn always run make arch`)
//	   and puts the body in <ADDITIONAL_METADATA> as an @-mention expansion:
//
//	     The user has mentioned some items in the form @[ITEM]. Here is extra
//	     information about the items that were mentioned by the user, in the
//	     order that they appear:
//
//	     /learn is a [Slash Command]:
//	     <LEARN>
//	     …body…
//	     </LEARN>
//
//	   /goal expands the same way with a bare body; /grill-me carries
//	   <GRILL_ME>. The clone now sends that shape too — the envelope is
//	   pinned against three live captures in
//	   internal/core/backend/testdata/.
//
//	2. /plan is an agent mode, not a prompt-sending command, and the
//	   reference never sends <PLAN> on any path: 20 live captures (TUI,
//	   `--mode plan`, `-p`) contain zero occurrences of it, the command is
//	   absent from the reference's picker, and plan mode's system
//	   instruction is byte-identical to the normal one. Pinning its body as
//	   a sent prompt — which this file did before the captures — asserted a
//	   behaviour the reference does not have. The clone now sends the turn
//	   verbatim; the entry below pins the absence instead.
//
//	3. /teamwork is the same story as /plan. Three captures (/teamwork and
//	   /teamwork-preview in the TUI, and `-p`) all went out verbatim with no
//	   expansion, and the system instruction for those turns names teamwork
//	   nowhere. A first pass recorded this as "unverified" because two
//	   earlier attempts failed to reach the model; the retry that succeeded
//	   showed there was nothing to verify — the command never expands.

import (
	"strings"

	"github.com/justwasm/pigasus/internal/app/session/slashactions"
)

// slashPromptSpec is one command's expected user-turn body.
type slashPromptSpec struct {
	// Command is the slash command, without the leading slash.
	Command string
	// Want is the reference's own text.
	Want string
	// Why records where Want came from and what it replaced, so a future
	// reader can re-evidence it instead of trusting it.
	Why string
	// NotSent marks a command whose literal exists in the reference but is
	// never put on the wire. The check then asserts the absence instead of
	// equality: the clone must send the turn verbatim and no body at all.
	//
	// /plan is the one such command. It is an agent mode, not a
	// prompt-sending command: twenty live 1.5.18 captures contain zero
	// occurrences of <PLAN>, the command is absent from the reference's
	// picker, and plan mode's system instruction is byte-identical to the
	// normal one. Pinning its body as if it were sent — which this file did
	// before the captures — asserts a behaviour the reference does not have.
	NotSent bool
}

// LearnPromptReference is the agy 1.5.18 `/learn` body.
//
// Recovered from the binary's `<LEARN>` literal (file offset 0x3ee0d64,
// 1,965 B including the wrapper), with the wrapper removed and the `%s` in
// step 1 of "Mandatory Proposal Workflow" filled from the sibling literal
// `Check the `%s` skill in the skills section (including its relevant guide)
// and inspect existing workspace rules/skills to follow their exact schema,
// frontmatter, directory conventions, and size/budget limits.` (the
// reference passes `agy-customizations`).
//
// This is the instruction behind the 1.5.8 changelog entry: `/learn` must
// check the documented rule size limits and, when an update would approach
// them, propose condensing, splitting into a scoped rule, or moving the
// guidance into a skill — before touching a rule file.
const LearnPromptReference = `The user invoked /learn to persist reusable behaviors from recent interactions, corrections, or successes. Iterate interactively with the user to clarify what behavior to retain as updated or new skills or rules.

## Identify What to Learn
1. **Analyze User Messages**: Prioritize analyzing recent user messages for explicit corrections, constraints, overrides, or pointers (e.g., "no", "instead", "that failed").
2. **Identify the Fix**: Compare failed attempts with the successful resolution to isolate the pivotal change.
3. **Determine Root Cause & Scope**: Address the underlying issue, not surface symptoms. Determine if it's universal or domain-specific.
4. **Verify if learning is needed**: If the interaction did not reveal any new reusable behaviors or constraints, explain this to the user and exit without proposing changes.

## Classify Rules vs. Skills
1. **Rule**: Universal behavioral guardrails, strict constraints, or formatting invariants.
2. **Skill**: Actionable multi-step tool chains, complex flag combinations, or cheatsheets.

## Create vs. Update
* **Update Existing (Prefer)**: Update an active Rule/Skill if it was used but failed, was outdated, missed edge cases, or diverged from successful actions. If updating a target file would approach or exceed its documented size/budget limits (or it is already truncated in context), propose condensing it or splitting/extracting into a scoped rule or skill instead of appending.
* **Create New**: When the behavior covers an entirely new domain or guardrail not covered by existing rules or skills, or when splitting an oversized file.

## Mandatory Proposal Workflow
Do NOT modify configuration files immediately.
1. Check the ` + "`agy-customizations`" + ` skill in the skills section (including its relevant guide) and inspect existing workspace rules/skills to follow their exact schema, frontmatter, directory conventions, and size/budget limits.
2. Create/update a learning_proposal.md artifact outlining your classification, rationale, and precise text additions/diffs.
3. Set request_feedback = true in ArtifactMetadata for user review.
4. Only execute file/tool modifications after explicit user approval.
`

// AllSlashPrompts lists the pinned commands.
func AllSlashPrompts() []slashPromptSpec {
	return []slashPromptSpec{
		{
			Command: "learn",
			Want:    LearnPromptReference,
			Why: "agy 1.5.18, the <LEARN> literal at file offset 0x3ee0d64 (1,965 B, " +
				"wrapper stripped, %s filled with `agy-customizations`). The clone used " +
				"to send a one-sentence summary of the 1.5.8 size-limit fix, which dropped " +
				"the rule-vs-skill classification, the update-before-create preference, the " +
				"four-step proposal workflow and the learning_proposal.md artifact contract.",
		},
		{
			Command: "grill-me",
			Want:    GrillMePromptReference,
			Why: "agy 1.5.18, the <GRILL_ME> literal at file offset 0x3e73ac4 (491 B, " +
				"wrapper stripped; no %s). The clone used to send an invented sentence " +
				"(\"Interrogate my reasoning on the topic I just raised…\") that is not in " +
				"the binary at all, and that asked for adversarial interrogation rather " +
				"than the reference's structured design interview with its three " +
				"guidelines (one question at a time, explore the codebase first, ask " +
				"through the ask_question tool).",
		},
		{
			Command: "plan",
			Want:    PlanPromptReference,
			NotSent: true,
			Why: "NOT SENT. /plan is an agent mode, not a prompt-sending command: the " +
				"reference sets the mode and sends the turn verbatim. Twenty live 1.5.18 " +
				"captures (TUI, `--mode plan`, `-p`) contain zero occurrences of <PLAN>; " +
				"the command is absent from the reference's picker (typed bare it answers " +
				"\"Unknown command\"); and plan mode's system instruction is byte-identical " +
				"to the normal one. The <PLAN> literal does exist (file offset 0x3f7245b, " +
				"3,722 B, wrapper stripped, no %s) but this path never reaches it. The " +
				"clone used to prepend it, sending the model a block the reference never " +
				"sends; it now sends the verbatim turn. This entry pins the absence.",
		},
		{
			Command: "goal",
			Want:    GoalPromptReference,
			Why: "agy 1.5.18, the bare literal at file offset 0x3e758f4 (535 B, no " +
				"wrapper — /goal is the one prompt-sending command shipped bare), and " +
				"confirmed byte-for-byte against a live capture: this is the body the " +
				"reference puts in <ADDITIONAL_METADATA> for `/goal ship the release`. " +
				"The clone used to send an invented sentence that named none of the " +
				"markers; the goal loop keys off <!-- GOAL_COMPLETE --> and " +
				"<!-- GOAL_CANCELLED -->, so a prompt omitting them leaves it waiting " +
				"for a signal the model was never told to send.",
		},
		{
			Command: "schedule",
			Want:    SchedulePromptReference,
			Why: "agy 1.5.18, the bare literal at file offset 0x3df6776 (74 B, no " +
				"wrapper), confirmed on the wire: `/schedule run the tests every " +
				"morning` produces a 437 B turn whose expansion body is exactly this " +
				"line. The clone used to send an invented three-clause sentence " +
				"(\"Register this instruction to run later… Tell me when it is " +
				"registered…\") that the reference never sends; the reference's whole " +
				"instruction is one line naming the tool to call.",
		},
		{
			Command: "boost",
			Want:    "",
			NotSent: true,
			Why: "NOT SENT, and the pin is CONTINGENT rather than structural. " +
				"Captured from agy 1.5.18: `/boost analyze the retry layer` produces a " +
				"160 B turn with nothing in <ADDITIONAL_METADATA> but the timestamp. " +
				"That is not because the reference has no /boost body — it does. The " +
				"binary carries the `<ORCHESTRATOR>\\n%s\\n</ORCHESTRATOR>` format " +
				"string (0x3d25678) and the full owl.OrchestratorPrompt body, and the " +
				"clone's builtin.OrchestratorPrompt reproduces it line for line (26 of " +
				"26 paragraphs, every line found in the reference). The gate is " +
				"server-side: AdminControls.FeatureControls.MultiagentControls carries " +
				"boost_command_disabled (proto field 1), set on this account. The same " +
				"message carries teamwork_preview_command_disabled (field 2), which is " +
				"why /teamwork-preview behaves identically. When the flag is clear the " +
				"reference expands /boost into <ORCHESTRATOR>; we reproduce the " +
				"disabled behaviour because it is the only one observable here. If " +
				"that flag ever flips, this pin becomes wrong, not the code.",
		},
		{
			Command: "teamwork",
			Want:    TeamworkPromptReference,
			NotSent: true,
			Why: "NOT SENT. Captured three ways in agy 1.5.18 — `/teamwork-preview " +
				"build a parser` in the TUI, `/teamwork build a parser` in the TUI " +
				"(the alias), and the same through `-p` — and every turn went out " +
				"verbatim with no expansion, exactly like /plan. The command is not in " +
				"the reference's picker, and the system instruction for those turns " +
				"(11,140 B) mentions teamwork nowhere. The <TEAMWORK> literal does " +
				"exist (file offset 0x3e9fef2, 1,072 B body, 1,103 B once %[1]s -> " +
				"`invoke_subagent` and %[2]s -> `sentinel` are filled) but no captured " +
				"path reaches it. This entry pins the absence; the clone sends the turn " +
				"verbatim like /plan. Like /boost, this pin is CONTINGENT: the same " +
				"server-issued admin control message that carries boost_command_disabled " +
				"also carries teamwork_preview_command_disabled " +
				"(AdminControls.FeatureControls.MultiagentControls, proto field 2), so " +
				"the command is switched off upstream rather than absent from the " +
				"binary. See internal/tui/slash_teamwork_test.go.",
		},
	}
}

// GrillMePromptReference is the agy 1.5.18 `/grill-me` body.
//
// Recovered from the binary's `<GRILL_ME>` literal (file offset 0x3e73ac4,
// 491 B including the wrapper), wrapper removed. Nothing is substituted: the
// literal has no `%s`.
//
// The instruction is a design interview, not an argument: walk down each
// branch of the design tree, resolve dependencies between decisions one at a
// time, recommend an answer for every question, and ask through the
// `ask_question` tool rather than in prose.
const GrillMePromptReference = `The user has requested that you interview them about every aspect of their task until you've reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one-by-one. For each question, provide your recommended answer.

Guidelines:
- Ask the questions one at a time.
- If a question can be answered by exploring the codebase, explore the codebase instead.
- Use the ask_question tool for asking questions to the user.
`

// checkSlashPrompt compares the live function with the pin.
//
// The clone's whole body is checked, not a prefix or a few markers: the failure
// mode this guards against is a rewrite (a summary standing in for the
// instruction), and a summary passes every substring check while saying
// something different.
func checkSlashPrompt(spec slashPromptSpec) []string {
	if spec.NotSent {
		return checkSlashPromptNotSent(spec)
	}
	var got string
	switch spec.Command {
	case "learn":
		got = slashactions.LearnPrompt()
	case "grill-me":
		got = slashactions.GrillMePrompt()
	case "schedule":
		got = slashactions.SchedulePrompt()
	case "goal":
		got = slashactions.GoalPrompt()
	case "teamwork":
		got = slashactions.TeamworkPrompt()
	default:
		return []string{"no live function registered for /" + spec.Command}
	}

	if got == spec.Want {
		return nil
	}

	var errs []string
	if len(got) != len(spec.Want) {
		errs = append(errs, "length differs: clone "+itoa(len(got))+" B, reference "+itoa(len(spec.Want))+" B")
	}
	first := firstDifference(got, spec.Want)
	if first >= 0 {
		errs = append(errs, "first difference at byte "+itoa(first)+": clone "+quoteNear(got, first)+
			", reference "+quoteNear(spec.Want, first))
	}
	return errs
}

// checkSlashPromptNotSent handles a command whose literal exists but is never
// put on the wire, which needs the opposite assertion: the recovery must still
// be intact (so the text is not silently lost), and the path must not send it.
//
// The second half cannot be checked from here without compiling the TUI into
// this harness. It lives in internal/tui/slash_plan_test.go
// (TestSlashPlan_SendsNoInstructionBody), which asserts the <PLAN> body is
// absent from the request the TUI composes. Both halves run under `make test`;
// this one keeps the literal from rotting, that one keeps it off the wire.
func checkSlashPromptNotSent(spec slashPromptSpec) []string {
	// A command with no body at all (Want empty) has nothing to compare: the
	// only claim is that the clone does not send one, which the TUI test
	// asserts. Return cleanly rather than comparing against "".
	if spec.Want == "" {
		return nil
	}
	var got string
	switch spec.Command {
	case "plan":
		got = slashactions.PlanPrompt()
	case "teamwork":
		got = slashactions.TeamworkPrompt()
	default:
		return []string{"no recovered literal registered for /" + spec.Command}
	}
	if got != spec.Want {
		return []string{
			"the reference literal is no longer recovered intact: clone " +
				itoa(len(got)) + " B, reference " + itoa(len(spec.Want)) + " B",
		}
	}
	return nil
}

// firstDifference returns the byte offset where two strings diverge, or -1.
func firstDifference(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) == len(b) {
		return -1
	}
	return n
}

// quoteNear renders a short window around off for the report.
func quoteNear(s string, off int) string {
	start := off - 20
	if start < 0 {
		start = 0
	}
	end := off + 40
	if end > len(s) {
		end = len(s)
	}
	return "…" + strings.ReplaceAll(strings.ReplaceAll(s[start:end], "\n", "\\n"), "\t", "\\t") + "…"
}

// itoa avoids pulling strconv into a file whose job is prose comparison.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

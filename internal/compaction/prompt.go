// prompt.go — the summarisation instruction the /compact command sends.
//
// Cycle `auto-compact-parity` replaced the cycle-237 reconstruction
// ("Summarize the prior conversation in 3-6 bullet points …") with the
// reference's *verbatim* instruction, captured from a live agy 1.4.5
// compaction request: the CloudCode request body the proxy
// (internal/agyproxy) recorded for the compaction turn carries it as the
// last user turn
// (docs/cycles/auto-compact-parity/captures.md).
//
// Two structural facts the capture settles, both of which the
// reconstruction got wrong:
//
//   - The reference sends NO summariser system prompt. It keeps the
//     normal Antigravity instructions as the system prompt and
//     appends this instruction as the final *user* turn, so the
//     summary model sees the live conversation as its context window.
//   - The instruction asks for a <summary></summary> block with fixed
//     sections (Task Overview / Progress / Key Findings / …). The
//     reference embeds that block verbatim in the post-compaction
//     context, so the model resumes with the shape it asked for.
//
// # The instruction is versioned, and the clone now carries 1.4.7's
//
// 【一手】 agy 1.4.7 carries the instruction as a *string literal* in its
// rodata (file offset 0x3785081, 3 937 bytes) and repeats it verbatim in
// the `session_summary_prompt_override` of the two profiles that
// override it (MODEL_PLACEHOLDER_M124 and MODEL_PLACEHOLDER_M408 — see
// internal/modelctx/profiles_1.4.7.go). The 1.4.4 catalogue the clone
// used to read carried the *older* revision: the same six sections with
// shorter bullets.
//
// The two revisions are not cosmetic — 1.4.7 asks for **seven** sections
// (it splits "Errors & Fixes" out of "Key Findings" and numbers the rest
// up) and adds the sentences that keep a resumed agent from repeating
// work: "test or verification commands run and their outcomes",
// "Distinguish completed edits from files that were only read or
// partially edited", "Preserve specific uncompleted items rather than
// collapsing checklists into high-level summaries".
//
// The 1.4.4 text is kept next to the new one as
// testdata/session_summary_prompt_override_1.4.4.txt; the live text is
// testdata/session_summary_prompt_1.4.7.txt.
//
// The instruction ends with a trigger-dependent tail. The captured
// (on-demand) tail is reproduced in OnDemandCompactionSuffix; the
// reference has no "AUTO COMPACTION" literal in the binary, so the
// automatic trigger's tail is still unknown (see the report's 存疑).
package compaction

// SummarizePrompt is the reference's continuation-summary instruction
// (agy 1.4.7, verbatim). It is sent as the final user turn of the
// compaction request, not as a system prompt.
//
// 3937 bytes (3931 characters — the body carries three em-dashes).
// The binary carries the same text as a string literal at file offset
// 0x3785081, and the captured fixture is the ground truth for the
// surrounding whitespace.
const SummarizePrompt = `You have been working on the task described above but have not yet completed
it. Write a continuation summary that will allow you (or another instance of
yourself) to resume work efficiently in a future context window where the
full conversation history will NOT be available—only this summary.

This summary is all that will be available to you going forward in the future
context window. Do not call any tools, simply just provide the summary based on
the information available in the current context window.

Your summary must be structured, concise, and actionable. Optimize for enabling immediate resumption with zero redundant work.

Include the following sections:

1. **Task Overview**
   - The user's core request, success criteria, and requested deliverables. Record any target IDs, URLs, or reporting recipients from the request.
   - Scope boundaries or task-specific constraints
   - Any ambiguities that were resolved (and how)

2. **Progress**
   - What has been completed, with concrete references (file paths,
     test or verification commands run and their outcomes, resource identifiers, tool outputs, URLs, etc.)
   - Key plans, design docs, or artifacts produced and their current state
   - Skills, documentation, or reference files that were read or inspected
   - What is in progress or only inspected/planned, and its current state. Distinguish completed edits from files that were only read or partially edited, and do not mark unverified test commands as completed or passed.

3. **Key Findings**
   - Technical constraints, requirements, or domain details uncovered
   - Decisions made and their rationale

4. **Errors & Fixes**
   This list should be exhaustive across the entire session so that no resolved or unresolved failure is omitted:
   - All errors, failed commands, or test/build failures encountered during the session. Include both errors that were resolved or worked around and any unresolved failures, with specific error messages and root causes.
   - How each error was fixed or worked around, and any user feedback on the error
   - Approaches that were tried and abandoned (and why—this prevents
     the successor from repeating them)

5. **Active Context**
   - State of any external resources, sessions, environments, or multi-agent context, such as parent or child agents in use.
   - Relevant intermediate results, hypotheses, or working assumptions
   - Dependencies between components or steps

6. **Next Steps**
   The next steps should provide all information required to continue working on the task. This includes any remaining features, bugs, points of investigation, and verification items. This list should be exhaustive, so that an agent working with this list can complete the full original task end-to-end. For example:
   - Remaining steps from any agreed plan, design doc, or active workflows and instructions, in priority order. Preserve specific uncompleted items rather than collapsing checklists into high-level summaries.
   - Remaining files to create, modify, move, copy, or delete, with the key changes needed in each
   - Remaining verification steps, test or build commands, or deliverables needed to complete the task
   - Any required cleanup, such as removing temporary files, debug code, or temporary configuration changes.
   - Known blockers, open questions, prerequisites, or risks for each step

7. **Commitments & Constraints**
   - Standing rules, personas, or response formatting requirements from user instructions, rules files, or reference documents.
   - Promises made to the user (e.g., "I said I would do X before Y")
   - User preferences or style requirements
   - Any boundaries the user set on approach, tools, or scope

Be concise but complete—err on the side of including concrete details needed to
prevent duplicate work, omitted plan steps, repeated mistakes, or broken promises.

Wrap your response in <summary></summary> tags.
`

// OnDemandCompactionSuffix is the reference's tail for a compaction the
// user triggered with /compact:
//
//	ON-DEMAND COMPACTION:
//	The user triggered this compaction. Follow any instructions in their
//	latest message, if present.
//
// It follows SummarizePrompt with two blank lines, so
// SummarizePrompt+OnDemandCompactionSuffix is byte-identical to the
// user turn the reference sent (2409 bytes).
const OnDemandCompactionSuffix = "\n\nON-DEMAND COMPACTION:\nThe user triggered this compaction. Follow any instructions in their latest message, if present."

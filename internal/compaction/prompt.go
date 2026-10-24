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
//   - The instruction asks for a <summary></summary> block with six
//     fixed sections (Task Overview / Progress / Key Findings / Active
//     Context / Next Steps / Commitments & Constraints). The reference
//     embeds that block verbatim in the post-compaction context, so
//     the model resumes with the shape it asked for.
//
// The instruction ends with a trigger-dependent tail. The captured
// (on-demand) tail is reproduced in OnDemandCompactionSuffix; the
// reference has no "AUTO COMPACTION" literal in the binary, so the
// automatic trigger's tail is still unknown (see the report's 存疑).
package compaction

// SummarizePrompt is the reference's continuation-summary instruction
// (agy 1.4.5, verbatim). It is sent as the final user turn of the
// compaction request, not as a system prompt.
//
// 2289 bytes (2283 characters — the body carries three em-dashes).
// The binary carries the same text as a string literal; the capture is
// the ground truth for the surrounding whitespace.
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
   - The user's core request and success criteria
   - Constraints, preferences, or scope boundaries they specified
   - Any ambiguities that were resolved (and how)

2. **Progress**
   - What has been completed, with concrete references (file paths,
     resource identifiers, tool outputs, URLs, etc.)
   - Key artifacts produced and their current state
   - What is in progress but incomplete, and its current state

3. **Key Findings**
   - Technical constraints, requirements, or domain details uncovered
   - Decisions made and their rationale
   - Errors encountered and their resolutions
   - Approaches that were tried and abandoned (and why—this prevents
     the successor from repeating them)

4. **Active Context**
   - State of any external resources, sessions, or environments in use
   - Relevant intermediate results, hypotheses, or working assumptions
   - Dependencies between components or steps

5. **Next Steps**
   - Specific actions needed to complete the task, in priority order
   - Known blockers or open questions that must be resolved
   - For each step, note any prerequisites or risks

6. **Commitments & Constraints**
   - Promises made to the user (e.g., "I said I would do X before Y")
   - User preferences or style requirements
   - Any boundaries the user set on approach, tools, or scope

Be concise but complete—err on the side of including anything that would
prevent duplicate work, repeated mistakes, or broken promises. Do not include
information that is obvious from the task description itself.

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

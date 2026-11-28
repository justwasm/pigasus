package slashactions

// prompts_reference.go — the reference's own prompt text, byte for byte.
//
// Every literal here was recovered from the agy 1.5.18 binary and then
// confirmed against a live wire capture: the binary's literal is what the
// reference actually puts in front of the model, not just what it ships.
//
// # Why the bodies live here and not beside the functions
//
// These are three of the longest literals in the tree (3.7 KB, 1.1 KB and
// 535 B). Keeping them in their own file means the functions that return
// them read as one line each, and a diff that touches the text is obviously
// a text change rather than a behaviour change. devutils/wirealign pins all
// three (see slashprompts.go), so drift fails a test rather than shipping.
//
// # How the wrapper tags were treated
//
// `/plan`, `/teamwork` and `/grill-me` ship wrapped (`<PLAN>…</PLAN>`,
// `<TEAMWORK>…</TEAMWORK>`, `<GRILL_ME>…</GRILL_ME>`); `/goal` ships bare.
// The clone sends the body without the wrapper, matching what `/learn` and
// `/grill-me` already do — the wrapper is the reference's own private
// framing, and the live capture confirms the model sees the body.

// planPromptReference is the agy 1.5.18 `/plan` body.
//
// 【一手】 the `<PLAN>` literal at file offset 0x3f7245b, 3,735 B including
// the wrapper; 3,722 B with it stripped. No format verb: nothing is filled
// at send time.
//
// It is an instruction to plan, not a request for a plan: research first,
// state the ambiguities out loud, write an implementation-plan artifact with
// request_feedback and user_facing set, wait for explicit approval, verify by
// running the tests, then write a walkthrough. The clone used to send a
// one-sentence request ("Help me create a plan for the work I just
// described…") that carried none of that: no artifact contract, no approval
// gate, no verification step.
const planPromptReference = `The user is requesting that you think and plan carefully before executing the upcoming task.
Carefully research the task, make sure that you and the user are aligned on the goals and requirements,
create a detailed implementation plan artifact, and get user approval on the plan before making any code changes (besides artifacts)
or running any modifying commands.

# Guidelines
- Establish a shared understanding of the task with the user. If there are any ambiguities, underspecified requirements,
or implicit assumptions, clarify them with the user before proceeding.
- Thoroughly research the codebase to establish a solid understanding of the relevant components, systems, dependencies, and architecture.
As you research, provide verbal updates of your research steps and thought process with the user, so they can follow along.
- Create an implementation plan artifact that outlines your proposed execution strategy.
Set request_feedback = true and user_facing = true in the ArtifactMetadata. The user will automatically
see any new and modified plans you create, so DO NOT re-summarize the plan.
- Only after the user explicitly approves the plan should you proceed to execution.
- Verify that your changes have the desired effects e.g. run unit tests, make sure code builds, etc. before claiming that the task is complete.
- After you've completed your task and verified that your solution works, create a walkthrough artifact to summarize your work.

# Planning Mode Artifacts
When in planning mode, you should create two special artifacts.

# Implementation Plan
Path: <Artifact Directory>/<plan_name>.md

**Purpose**: A technical design document to present your implementation plan to the user for feedback and approval.
After reading the document, the user should understand the key technical details of your plan, and be able to make an informed decision on whether to approve it.
This document should be very detailed, including code snippets, diffs, mermaid diagrams, verification strategies, and background information.

**Format**: Use the following format, omitting any irrelevant sections:

## [Goal Description]
Provide a brief description of the problem, any background context, and what the change accomplishes.

## User Review Required
Document anything that requires user review or feedback, for example, breaking changes or significant design decisions. Use GitHub alerts (IMPORTANT/WARNING/CAUTION) to highlight critical items.

## Open Questions
Any clarifying or design questions for the user that will impact the implementation plan. Use GitHub alerts (IMPORTANT/WARNING/CAUTION) to highlight critical items.

## Proposed Changes
Group files by component (e.g., package, feature area, dependency layer) and order logically (dependencies first). Separate components with horizontal rules for visual clarity.

### [Component Name]
Summary of what will change in this component with explicit code snippets and diffs. For specific files, Use [NEW] and [DELETE] to demarcate new and deleted files, for example:
#### [MODIFY] file basename
#### [NEW] file basename
#### [DELETE] file basename

## Verification Plan
Summary of how you will verify that your changes have the desired effects.

### Automated Tests
Exact commands to run automated tests

### Manual Verification
Instructions for what the user should manually verify.

# Walkthrough
Path: <Artifact Directory>/walkthrough.md

**Purpose**: After completing work, summarize what you accomplished. Update an existing walkthrough for related follow-up work rather than creating a new one.

**Document**:
- Changes made
- What was tested
- Validation results

Embed screenshots and recordings to visually demonstrate UI changes and user flows.`

// teamworkPromptReference is the agy 1.5.18 `/teamwork` body.
//
// 【一手】 the `<TEAMWORK>` literal at file offset 0x3e9fef2, 1,085 B
// including the wrapper; 1,072 B of body, and 1,103 B once every indexed verb
// is filled (`%[1]s` once, `%[2]s` seven times). Nothing here is invented:
//
//   - the first verb is the tool the reference names, `invoke_subagent`;
//   - the second is the team's root type, `sentinel` — the same name the
//     reference's own `project_sentinel` role registers as an alias, and the
//     one its routing description calls "Sentinel with task routing"
//     (file offset 0x3e60f6e).
//
// The instruction hands the request over rather than attempting it, keeps
// any leading `[skip-interview]` bracket exactly as typed, and tells the
// model to ask the team for status instead of guessing. The clone used to
// send a one-sentence summary ("Coordinate a team of autonomous agents…")
// that named no tool and no type, so nothing could be dispatched.
const teamworkPromptReference = `
The user wants the sentinel multi-agent team to take on their request. Hand it
over as it is—do not attempt the task yourself. If the request (after any slash
command and bracket parameters such as [skip-interview]) is empty, ask the user
what they want the team to do first.

Call the invoke_subagent tool with TypeName: sentinel (sentinel is hidden from the subagents
list, but can be invoked). If sentinel cannot be invoked, do not attempt to define
a subagent and invoke that instead; just tell the user that sentinel is
temporarily not available. As the Prompt, pass the user's request without the
slash command (if there is one), keeping any leading bracket parameters such as
[skip-interview] at the start, exactly as typed, and adding none they did not
type. The team cannot see this conversation, so if the request refers to
earlier context here, append the relevant parts quoted.

When sentinel or its interviewer messages you with updates, questions, or drafts
for the user, follow the instructions in each message. If the user asks how it
is going, ask the sentinel subagent you started.
`

// goalPromptReference is the agy 1.5.18 `/goal` body.
//
// 【一手】 the literal at file offset 0x3e758f4, 535 B, with no wrapper tag
// around it — `/goal` is the one prompt-sending command the reference ships
// bare. Verified byte-for-byte against a live capture: the body the
// reference put in `<ADDITIONAL_METADATA>` for `/goal ship the release` is
// this exact 535-byte string.
//
// It names the two markers the clone already parses
// (`convstore.GoalCompleteToken` / `GoalCancelledToken`), which is why the
// text has to be the reference's: the goal loop keys off those tokens, so a
// prompt that omits them leaves the loop waiting for a signal the model was
// never told to send.
const goalPromptReference = `The user has marked this task with /goal, indicating that this task is intended to run for a long time without user input, e.g. overnight. You should be extra thorough and only stop when you are confident the goal has been completely fulfilled. The system will force you to continue execution, prompting you to audit your work until completion. Once complete, include <!-- GOAL_COMPLETE --> in your response. If the user explicitly asked to stop or cancel this goal, include <!-- GOAL_CANCELLED --> in your response to cancel the goal.`

package main

// slashprompts_reference.go — the reference's own body text for /plan, /goal
// and /teamwork, recovered from the agy 1.5.18 binary.
//
// These copies exist so checkSlashPrompt compares the clone's function against
// the *reference*, not against itself. Calling the clone's own accessor would
// make the check a tautology that passes no matter how far the shipped text
// drifts — the exact false confidence this package was written to prevent. The
// /learn and /grill-me pins live inline in slashprompts.go for the same reason.
//
// Recovery, per literal:
//
//   - plan:     <PLAN> at 0x3f7245b, 3,722 B of body, wrapper stripped, no %s.
//   - goal:     bare at 0x3e758f4, 535 B, no wrapper (the one prompt-sending
//               command shipped bare); confirmed against a live capture.
//   - teamwork: <TEAMWORK> at 0x3e9fef2, 1,072 B of body, 1,103 B once
//               %[1]s -> invoke_subagent (x1) and %[2]s -> sentinel (x7) are
//               filled; both names settled from a live capture.

// PlanPromptReference is the agy 1.5.18 body (see the file header).
const PlanPromptReference = `The user is requesting that you think and plan carefully before executing the upcoming task.
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

// GoalPromptReference is the agy 1.5.18 body (see the file header).
const GoalPromptReference = `The user has marked this task with /goal, indicating that this task is intended to run for a long time without user input, e.g. overnight. You should be extra thorough and only stop when you are confident the goal has been completely fulfilled. The system will force you to continue execution, prompting you to audit your work until completion. Once complete, include <!-- GOAL_COMPLETE --> in your response. If the user explicitly asked to stop or cancel this goal, include <!-- GOAL_CANCELLED --> in your response to cancel the goal.`

// TeamworkPromptReference is the agy 1.5.18 body (see the file header).
const TeamworkPromptReference = `
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

// SchedulePromptReference is the agy 1.5.18 /schedule expansion body.
//
// The literal at file offset 0x3df6776, 74 B, no wrapper tag. Confirmed on the
// wire: `/schedule run the tests every morning` produces a 437 B turn whose
// expansion body is exactly this line.
const SchedulePromptReference = "Please use the schedule tool to create a schedule as specified by the user"

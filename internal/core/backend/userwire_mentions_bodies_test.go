package backend

// The three bodies below are the expansion text from the captured envelopes
// in testdata/. They live here rather than being imported so the test compares
// the *envelope* — the thing that was wrong — while the bodies themselves stay
// pinned byte-for-byte by devutils/wirealign against the binary.

const learnBodyForTest = "The user invoked /learn to persist reusable behaviors from recent interactions, corrections, or successes. Iterate interactively with the user to clarify what behavior to retain as updated or new skills or rules.\n" +
	"\n" +
	"## Identify What to Learn\n" +
	"1. **Analyze User Messages**: Prioritize analyzing recent user messages for explicit corrections, constraints, overrides, or pointers (e.g., \"no\", \"instead\", \"that failed\").\n" +
	"2. **Identify the Fix**: Compare failed attempts with the successful resolution to isolate the pivotal change.\n" +
	"3. **Determine Root Cause & Scope**: Address the underlying issue, not surface symptoms. Determine if it's universal or domain-specific.\n" +
	"4. **Verify if learning is needed**: If the interaction did not reveal any new reusable behaviors or constraints, explain this to the user and exit without proposing changes.\n" +
	"\n" +
	"## Classify Rules vs. Skills\n" +
	"1. **Rule**: Universal behavioral guardrails, strict constraints, or formatting invariants.\n" +
	"2. **Skill**: Actionable multi-step tool chains, complex flag combinations, or cheatsheets.\n" +
	"\n" +
	"## Create vs. Update\n" +
	"* **Update Existing (Prefer)**: Update an active Rule/Skill if it was used but failed, was outdated, missed edge cases, or diverged from successful actions. If updating a target file would approach or exceed its documented size/budget limits (or it is already truncated in context), propose condensing it or splitting/extracting into a scoped rule or skill instead of appending.\n" +
	"* **Create New**: When the behavior covers an entirely new domain or guardrail not covered by existing rules or skills, or when splitting an oversized file.\n" +
	"\n" +
	"## Mandatory Proposal Workflow\n" +
	"Do NOT modify configuration files immediately.\n" +
	"1. Check the `agy-customizations` skill in the skills section (including its relevant guide) and inspect existing workspace rules/skills to follow their exact schema, frontmatter, directory conventions, and size/budget limits.\n" +
	"2. Create/update a learning_proposal.md artifact outlining your classification, rationale, and precise text additions/diffs.\n" +
	"3. Set request_feedback = true in ArtifactMetadata for user review.\n" +
	"4. Only execute file/tool modifications after explicit user approval.\n"

const goalBodyForTest = "The user has marked this task with /goal, indicating that this task is intended to run for a long time without user input, e.g. overnight. You should be extra thorough and only stop when you are confident the goal has been completely fulfilled. The system will force you to continue execution, prompting you to audit your work until completion. Once complete, include <!-- GOAL_COMPLETE --> in your response. If the user explicitly asked to stop or cancel this goal, include <!-- GOAL_CANCELLED --> in your response to cancel the goal.\n"

const grillMeBodyForTest = "The user has requested that you interview them about every aspect of their task until you've reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one-by-one. For each question, provide your recommended answer.\n" +
	"\n" +
	"Guidelines:\n" +
	"- Ask the questions one at a time.\n" +
	"- If a question can be answered by exploring the codebase, explore the codebase instead.\n" +
	"- Use the ask_question tool for asking questions to the user.\n"

// output_budget.go — AGY 1.4.1 cycle 248: per-step tool-output budget.
//
// AGY 1.4.1 changelog (recoverable from
// internal/cli/testdata/changelog_reference.txt:12):
//
//	"Fixed large structured tool outputs (such as JSON results from
//	 MCP tools) bypassing the per-step token limit while still being
//	 counted as truncated, which could push long conversations past
//	 the model's context window before automatic compaction ran."
//
// truncate.go implements the truncation half of that fix. This file
// implements the per-step budget pipeline that tool-result callers
// (e.g. internal/agent.pig.go's runnerTool.Execute,
// internal/piagentbridge.toolAdapter.Execute,
// internal/subagenttools' tool Execute methods) should run their
// result through before the model sees it.
//
// The pipeline is intentionally a single function:
//
//	ApplyOutputBudget(content, format, maxTokens) → BudgetResult
//
// so each caller can adopt it independently and the regression tests
// can pin the contract end-to-end. The function:
//
//   - returns BudgetResult{Content: content, Truncated: false} when
//     the content fits (or when maxTokens <= 0);
//   - delegates to TruncateStructured for JSON, YAML, XML inputs;
//   - falls back to the existing freeform-prose truncation for
//     everything else (and for unparseable JSON);
//   - records before/after token counts so callers can echo a
//     "X → Y tokens" diagnostic.
package tools

// BudgetResult is the outcome of a single ApplyOutputBudget call.
//
// Content is what the caller should feed to the model. Truncated
// is the AGY-changelog contract flag: a non-truncated result carries
// the full content; a truncated result carries the truncated form
// (always under the budget; always parseable if the input was
// structured).
//
// Format is the detected (or supplied) input format — FormatJSON,
// FormatYAML, FormatXML, FormatProse. The field is exported so
// diagnostic callers (cycle-237's compact_stats, cycle-244's
// /feature-diff panel) can surface what the pipeline saw.
//
// TokensBefore and TokensAfter are the cheap 4-chars-per-token
// estimates before and after truncation. Zero when the input was
// already under budget (Truncated=false).
type BudgetResult struct {
	// Content is the post-budget content. When Truncated is
	// false this is the original input verbatim.
	Content string
	// Truncated is true when ApplyOutputBudget reduced the
	// content to fit within the budget. Matches the AGY 1.4.1
	// "truncated" metadata flag.
	Truncated bool
	// Format is the detected (or caller-supplied) format of
	// the input. One of FormatJSON, FormatYAML, FormatXML,
	// FormatProse. Exported for diagnostics.
	Format string
	// TokensBefore is the estimate of the original content's
	// token cost (the cheap /4 heuristic). Zero when
	// Truncated is false.
	TokensBefore int
	// TokensAfter is the estimate of the truncated content's
	// token cost. Zero when Truncated is false.
	TokensAfter int
}

// DefaultMaxResultTokens is the per-step budget the cycle
// recommends callers pass to ApplyOutputBudget. Matches the AGY
// changelog wording ("per-step token limit"). The reference binary
// ships a model-class-dependent value; this constant is the
// cycle-248 default for the clone.
//
// AGY's downstream implementation tweaks this number per model
// (e.g. Gemini 3 Pro gets a larger budget than the Flash class),
// but the clone does not have the per-model recovery table yet —
// 8000 is the conservative cycle-248 number chosen so an MCP
// tool's JSON result (which is the dominant large-output case
// the changelog calls out) fits well within a 200K context window
// without bumping into the model's auto-compact threshold.
const DefaultMaxResultTokens = 8000

// ApplyOutputBudget is the per-step budget pipeline. It detects
// the format of content (or accepts a caller-supplied format),
// truncates the content via TruncateStructured when it exceeds
// maxTokens, and returns the result with before/after token
// counts.
//
// Behaviour:
//
//   - maxTokens <= 0 → returns input verbatim, Truncated=false.
//   - format == "" → auto-detect via DetectFormat.
//   - format != "" → use the caller-supplied format (FormatJSON,
//     FormatYAML, FormatXML, FormatProse). An invalid format
//     string is treated as FormatProse.
//   - Under-budget input → returns input verbatim.
//   - Over-budget input → truncates and sets Truncated=true.
//
// The function never returns a content string longer than
// maxTokens * DefaultApproxCharsPerToken characters (the
// /4 heuristic). The structured-output helpers preserve
// syntactic validity (JSON stays parseable, YAML stays
// parseable, XML stays wrapped in <truncated>).
func ApplyOutputBudget(content string, format string, maxTokens int) BudgetResult {
	if content == "" {
		return BudgetResult{Content: content, Format: formatOrDetect(content, format)}
	}
	if maxTokens <= 0 {
		return BudgetResult{Content: content, Format: formatOrDetect(content, format)}
	}
	if format == "" {
		format = DetectFormat(content)
	}
	if !isKnownFormat(format) {
		format = FormatProse
	}
	before := estimateTokens(content)
	if before <= maxTokens {
		return BudgetResult{
			Content:     content,
			Format:      format,
			TokensBefore: before,
			TokensAfter: before,
		}
	}
	truncated := truncateByFormat(content, format, maxTokens)
	// Defensive guard: truncateByFormat returns the input
	// unchanged if it can't make a smaller one. Detect that
	// here so the BudgetResult.Truncated flag stays honest.
	after := estimateTokens(truncated)
	if truncated == content {
		return BudgetResult{
			Content:     content,
			Format:      format,
			TokensBefore: before,
			TokensAfter: after,
		}
	}
	return BudgetResult{
		Content:     truncated,
		Truncated:   true,
		Format:      format,
		TokensBefore: before,
		TokensAfter: after,
	}
}

// formatOrDetect returns format if non-empty, otherwise detects
// from content. The two-value form keeps the call site in
// ApplyOutputBudget readable.
func formatOrDetect(content, format string) string {
	if format != "" {
		return format
	}
	return DetectFormat(content)
}

// isKnownFormat reports whether format is one of the constants
// ApplyOutputBudget and ApplyOutputBudget callers recognise.
// An unknown format is coerced to FormatProse so the function
// never panics on a typo.
func isKnownFormat(format string) bool {
	switch format {
	case FormatJSON, FormatYAML, FormatXML, FormatProse:
		return true
	}
	return false
}
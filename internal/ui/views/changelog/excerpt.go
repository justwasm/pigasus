// excerpt.go — the /changelog panel's own facts: the excerpt it opens
// with, the window's height and the denominator in its counter.
//
// # Where the facts live
//
// The panel's document has three shapes: the loaded reference file
// (see source.go), the embedded markdown (see wrap.go) and the excerpt
// below — the first screen the panel shows when neither is available.
// All three belong to this package, so the excerpt lives here and not
// in a generic viewer package that held it for three different panels.
//
// # The excerpt's body
//
// The 1.3.6 bullet points are transcribed from the reference's own
// release notes. BuildRestRows skips the first `##` header of the
// embedded markdown because the excerpt already carries that section,
// so the two concatenate without overlap.

package changelog

// TotalLines is the denominator in the panel's "(a–b of N lines)"
// counter. It is the reference's own figure for the document's length,
// not the length of the rows this clone renders.
const TotalLines = 2014

// WindowRows is how many document rows the panel shows at once when the
// terminal's height is unknown.
func WindowRows() int { return 18 }

// Body is the opening set of bullet points under ## 1.3.6.
var Body = []string{
	"",
	"  \x1b[1m\x1b[38;5;27m## 1.3.6\x1b[0m",
	"",
	"  \x1b[38;5;235m• Added Claude Opus 5.5 and Claude Sonnet 5.5 for Application Default Credentials (AGY_ADC_AUTH) sign-in. Claude models now appear in /model only if your Google Cloud project can use them.\x1b[39m",
	"  \x1b[38;5;235m• Improved Auto mode's review of commands that would leave the terminal sandbox: the review is faster and no longer times out without a decision, it allows the workspace's own format, lint, and fix commands instead of asking about them, and it blocks commands that would upload sensitive data.\x1b[39m",
	"  \x1b[38;5;235m• Improved the /diff Commit Tree view with colors: commit hashes and change IDs are highlighted, descriptions are dimmed, the Working Copy (Uncommitted Changes) row stands out, and @ is bold, so hashes, timestamps, and descriptions no longer run together.\x1b[39m",
	"  \x1b[38;5;235m• Fixed the agent hanging indefinitely when the model connection stalled: if no data arrives for 5 minutes the request is now canceled and retried automatically, and a stalled request no longer leaves an empty response step behind.\x1b[39m",
	"  \x1b[38;5;235m• Fixed questions asked by subagents and workflow agents being lost, which left the subagent waiting forever: the alert now reads <agent> has a question and shows it, and Ctrl+K or a in the subagent's view opens a question picker that sends your answer to that agent. Headless -p runs skip these questions so the subagent can continue.\x1b[39m",
	"  \x1b[38;5;235m• Fixed agent shell commands hanging on an invisible editor when a version control command wants one, such as git commit without -m or jj describe: the CLI now sets GIT_EDITOR, HGEDITOR, and JJ_EDITOR to false for agent commands, so they fail right away instead.\x1b[39m",
	"  \x1b[38;5;235m• Fixed skills, rules, workflows, and Markdown agents failing to load with mapping values are not allowed in this context when an unquoted description: contains a colon, such as description: Runs checks: formatters and linters.\x1b[39m",
	"  \x1b[38;5;235m• Fixed Mermaid diagrams that start with YAML frontmatter, a %%{init: ...}%% directive, or %% comments, or that use Mermaid v11 syntax such as id@{ shape: ..., label: ... } nodes, showing as raw source instead of rendering in agent responses and the artifact viewer.\x1b[39m",
	"  \x1b[38;5;235m• Fixed the @ file-mention dropdown staying open after a newline in a multi-line prompt, where it took over Up/Down and made Enter pick a file instead of inserting the newline.\x1b[39m",
	"  \x1b[38;5;235m• Fixed / search in the file and artifact viewer highlighting the wrong characters, or crashing, on lines with letters whose lowercase form has a different length such as İ; n and N now stop only on lines that show a highlight.\x1b[39m",
	"  \x1b[38;5;235m• Fixed long lines and file paths containing CJK or other multi-byte text turning into characters in /codesearch results; they are now cut cleanly with an ellipsis.\x1b[39m",
	"  \x1b[38;5;235m• Fixed agy plugin validate, the summary agy plugin install prints, and marketplace previews in the /plugin Discover tab leaving out subagents a plugin declares in its root agents.json.\x1b[39m",
}

// ExcerptRows returns the visible first screen of the /changelog panel.
//
// It is the panel's title, its tagline and the 1.3.6 bullet points —
// the shape the panel shows when it has neither the reference's release
// notes file nor a terminal size to measure.
func ExcerptRows() []string {
	out := []string{TitleRow, "", Tagline}
	return append(out, Body...)
}

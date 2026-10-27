package tuiviewer

import (
	"strings"

	"github.com/justwasm/pigasus/palette"
)

// ChangelogTotalLines is the denominator in the viewer's "(a–b of N lines)" counter.
const ChangelogTotalLines = 2014

// ChangelogWindowRows is how many document rows the viewer shows at once.
func ChangelogWindowRows() int { return 18 }

// ChangelogBody is the opening set of bullet points under ## 1.3.6.
var ChangelogBody = []string{
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

// ChangelogExcerptRows returns the visible first screen of the /changelog viewer.
func ChangelogExcerptRows() []string {
	out := []string{
		"  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m",
		"",
		"  \x1b[38;5;235mThe terminal-first surface to interact with Pigasus agents. Stay in your flow without context switching.\x1b[39m",
	}
	return append(out, ChangelogBody...)
}

// BuildChangelogRestRows parses the markdown blob into styled bullet points.
func BuildChangelogRestRows(rawMarkdown string) []string {
	var out []string
	for _, l := range strings.Split(rawMarkdown, "\n") {
		switch {
		case strings.HasPrefix(l, "## "):
			out = append(out, "", "  \x1b[1m\x1b[38;5;27m"+l+"\x1b[0m", "")
		case strings.HasPrefix(l, "- "):
			out = append(out, "  \x1b[38;5;235m• "+strings.TrimPrefix(l, "- ")+"\x1b[39m")
		}
	}
	return out
}

// ChangelogLegend returns the standard changelog footer hint.
func ChangelogLegend() string {
	return palette.DimGrayOpen + "  ↑/↓ Navigate  esc Cancel" + palette.DimGrayClose
}

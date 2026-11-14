package storybook

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/branding"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/canvas"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchaction"
	diffrow "github.com/justwasm/pigasus/internal/ui/views/diff/row"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/render"
)

// RegisterAtomicStories registers the 8 atomic, viewport, and queue component stories.
func RegisterAtomicStories(c *Catalog) {
	c.Register(newSessionBannerStory())
	c.Register(newStickyPromptStory())
	c.Register(newCanvasHighlightsStory())
	c.Register(newTabAlignedStory())
	c.Register(newPromptQueuesStory())
	c.Register(newDiffRowStory())
	c.Register(newCodeSearchActionStory())
	c.Register(newEligibilityNoticeStory())
}

// ---------------------------------------------------------------- 1. Session Banner Story

type sessionBannerStory struct {
	state  int
	width  int
	height int
}

func newSessionBannerStory() Story {
	return Story{
		ID:          "foundations-session-banner",
		Name:        "Session Welcome Banner",
		Level:       LevelMolecule,
		Category:    CategoryFoundations,
		UpstreamSym: "jetski/cli.RenderBanner",
		Description: "Top-of-session greeting banner with Unicode logo, active user email/tier, model, shortened CWD, and optional security/fallback warning callouts.",
		States: []State{
			{ID: "standard-pro", Name: "Standard Pro Tier", Description: "Welcome banner with full 5-row logo, Pro tier badge, and CWD"},
			{ID: "agent-warning", Name: "Agent Warning Callout", Description: "Banner with security/untrusted skills warning bar"},
			{ID: "model-fallback", Name: "Model Fallback Callout", Description: "Banner with model degraded fallback warning"},
			{ID: "compact-no-logo", Name: "Compact (No Logo)", Description: "Compact mode with text details only"},
		},
		Factory: func() StoryInstance {
			return &sessionBannerStory{width: 80, height: 24}
		},
	}
}

func (s *sessionBannerStory) SetState(idx int) {
	s.state = idx
}

func (s *sessionBannerStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *sessionBannerStory) View() string {
	var b strings.Builder
	b.WriteString("\n")

	artRows := tuilib.RenderLogoLines()
	emailLine := "developer@example.com (PRO)"
	model := "Gemini 3.8 Flash (Preview)"
	cwd := tuilib.ShortenCwd("/Users/developer/workspace/pigasus-core")

	if s.state == 3 {
		// Compact no logo
		b.WriteString(palette.BlueBoldOpen + branding.Current().CLIName + " 1.5.5" + palette.BlueBoldClose + "\n")
		b.WriteString(palette.DimGrayOpen + emailLine + palette.DimGrayClose + "\n")
		b.WriteString(palette.DimGrayOpen + model + palette.DimGrayClose + "\n")
		b.WriteString(palette.DimGrayOpen + cwd + palette.DimGrayClose + "\n")
	} else {
		b.WriteString(fmt.Sprintf("%s  %s%s 1.5.5%s\n", artRows[0], palette.BlueBoldOpen, branding.Current().CLIName, palette.BlueBoldClose))
		b.WriteString(fmt.Sprintf("%s  %s%s%s\n", artRows[1], palette.DimGrayOpen, emailLine, palette.DimGrayClose))
		b.WriteString(fmt.Sprintf("%s  %s%s%s\n", artRows[2], palette.DimGrayOpen, model, palette.DimGrayClose))
		b.WriteString(fmt.Sprintf("%s  %s%s%s\n", artRows[3], palette.DimGrayOpen, cwd, palette.DimGrayClose))
		b.WriteString(artRows[4] + "\n")
	}

	switch s.state {
	case 1:
		b.WriteString("\n" + tuilib.RenderAgentWarning("Agent 'code-researcher' loads external unverified tools") + "\n")
	case 2:
		b.WriteString("\n" + tuilib.RenderWarning("Claude 3.5 Sonnet", "Gemini 3.8 Flash") + "\n")
	}

	return b.String()
}

func (s *sessionBannerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- 2. Sticky Prompt Story

type stickyPromptStory struct {
	state  int
	width  int
	height int
}

func newStickyPromptStory() Story {
	return Story{
		ID:          "foundations-sticky-prompt",
		Name:        "Sticky Pinned Prompt",
		Level:       LevelMolecule,
		Category:    CategoryFoundations,
		UpstreamSym: "jetski/cli/viewport.FormatVisibleRows",
		Description: "Sticky user prompt that adheres to the top of the terminal viewport when scrolling upward through lengthy turn transcripts.",
		States: []State{
			{ID: "pinned-prompt", Name: "Pinned User Prompt", Description: "Top prompt pinned with horizontal rule separator"},
			{ID: "scrolled-history", Name: "Deep Transcript Scroll", Description: "Pinned prompt above historical turn rows with rule"},
			{ID: "truncated-narrow", Name: "Narrow Truncated Prompt", Description: "Long pinned prompt clamped to viewport width"},
		},
		Factory: func() StoryInstance {
			return &stickyPromptStory{width: 80, height: 24}
		},
	}
}

func (s *stickyPromptStory) SetState(idx int) {
	s.state = idx
}

func (s *stickyPromptStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *stickyPromptStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}

	visibleRows := []string{
		palette.DimGrayOpen + "Assistant response: Examining repository architecture..." + palette.DimGrayClose,
		"1. Analyzed internal/ui/panelregistry/registry.go (all 26 kinds verified)",
		"2. Checked internal/ui/toolsteps/steps.go data types",
		"3. Verified bubbletea/v2 event loop integration",
		"",
		palette.DimGrayOpen + "Run `make quick` to verify changes." + palette.DimGrayClose,
	}

	pinnedText := "> Can you explain the full TUI Storybook component coverage and layout?"
	if s.state == 2 {
		pinnedText = "> Please provide an extremely comprehensive breakdown of every single visual component across the entire repository with detailed decompilation parity comparisons"
	}

	out := tuiviewport.FormatVisibleRows(visibleRows, w, pinnedText, "", true)
	return strings.Join(out, "\n")
}

func (s *stickyPromptStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- 3. Canvas Highlights Story

type canvasHighlightsStory struct {
	state  int
	width  int
	height int
}

func newCanvasHighlightsStory() Story {
	return Story{
		ID:          "foundations-canvas-highlights",
		Name:        "Canvas Search Highlights",
		Level:       LevelAtom,
		Category:    CategoryFoundations,
		UpstreamSym: "vito/midterm.Canvas",
		Description: "Cell-level partial repaints and query search highlights using vito/midterm Canvas over ANSI-styled transcript text.",
		States: []State{
			{ID: "single-keyword", Name: "Single Keyword Match", Description: "Highlight single occurrence of 'func main()'"},
			{ID: "multi-regions", Name: "Multiple Match Regions", Description: "Highlight multiple search tokens across line"},
			{ID: "cjk-boundary", Name: "CJK Rune Boundary", Description: "Accurate cell highlight over double-width East Asian glyphs"},
		},
		Factory: func() StoryInstance {
			return &canvasHighlightsStory{width: 80, height: 24}
		},
	}
}

func (s *canvasHighlightsStory) SetState(idx int) {
	s.state = idx
}

func (s *canvasHighlightsStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *canvasHighlightsStory) View() string {
	var lines []canvas.Line

	switch s.state {
	case 1:
		lines = []canvas.Line{
			{
				Content: "func SearchWorkspace(dir string, re *regexp2.Regexp) (hits []string, extra int) {",
				Highlights: []canvas.Range{
					{Start: 5, End: 20},  // "SearchWorkspace"
					{Start: 53, End: 57}, // "hits"
				},
			},
			{
				Content: "    for _, path := range files {",
				Highlights: []canvas.Range{
					{Start: 11, End: 15}, // "path"
					{Start: 25, End: 30}, // "files"
				},
			},
		}
	case 2:
		lines = []canvas.Line{
			{
				Content: "在深思熟虑之后，我们决定构建一个专用的前端 Storybook 预览系统。",
				Highlights: []canvas.Range{
					{Start: 0, End: 18},   // "在深思熟虑"
					{Start: 45, End: 54},  // "Storybook"
				},
			},
			{
				Content: "精确计算多字节 Rune 宽度，确保 CJK 输入与光标对齐完全一致。",
				Highlights: []canvas.Range{
					{Start: 0, End: 12},  // "精确计算"
					{Start: 25, End: 29}, // "Rune"
					{Start: 41, End: 44}, // "CJK"
				},
			},
		}
	default:
		lines = []canvas.Line{
			{
				Content: "package main\n\nfunc main() {\n    runLiveTUI(os.Stdout, flag.CommandLine)\n}",
				Highlights: []canvas.Range{
					{Start: 19, End: 30}, // "func main()"
				},
			},
		}
	}

	var b strings.Builder
	b.WriteString(bold("Midterm Canvas Cell Highlights Preview:") + "\n\n")
	for _, l := range lines {
		b.WriteString(l.Render() + "\n")
	}
	b.WriteString("\n" + dim("▶ Highlights painted via vito/midterm bold+reverse cell attributes") + "\n")
	return b.String()
}

func (s *canvasHighlightsStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- 4. Tab Aligned Story

type tabAlignedStory struct {
	state  int
	width  int
	height int
}

func newTabAlignedStory() Story {
	return Story{
		ID:          "foundations-tab-aligned",
		Name:        "Tab Aligned Formatter",
		Level:       LevelAtom,
		Category:    CategoryFoundations,
		UpstreamSym: "jetski/cli.TabAlignedRows",
		Description: "Tab-separated column tabular formatter with dynamic Unicode rune width measurement and automatic trailing space trimming.",
		States: []State{
			{ID: "keybindings", Name: "Keybindings Cheatsheet", Description: "Aligned 3-column shortcut table"},
			{ID: "cjk-columns", Name: "Mixed CJK Multi-Byte", Description: "Aligned columns containing Chinese and English"},
			{ID: "ragged-rows", Name: "Ragged Column Counts", Description: "Uneven column counts gracefully padded"},
		},
		Factory: func() StoryInstance {
			return &tabAlignedStory{width: 80, height: 24}
		},
	}
}

func (s *tabAlignedStory) SetState(idx int) {
	s.state = idx
}

func (s *tabAlignedStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *tabAlignedStory) View() string {
	var input string

	switch s.state {
	case 1:
		input = "快捷键\t操作名称\t功能说明\n" +
			"Ctrl+O\t切换折叠\t折叠或展开全局历史 Trajectory 工具调用\n" +
			"Tab\t切换变体\t在当前 Story 的不同状态预设间循环切换\n" +
			"Esc\t返回导航\t退出当前交互模式并回到侧边栏分类树\n" +
			"/\t搜索过滤\t按关键字实时过滤 Story 组件名称"
	case 2:
		input = "ID\tCategory\tStatus\tNotes\n" +
			"1\tFoundations\tPASS\n" +
			"2\tComposer\tPASS\tCJK IME anchor verified\n" +
			"3\tToolSteps\tPASS\tParallel batch supported\n" +
			"4\tPanels\tPASS\t26 official kinds"
	default:
		input = "Key\tAction\tDescription\n" +
			"Enter\tPlayground\tFocus component for direct keyboard interaction\n" +
			"Tab\tNext State\tCycle forward through component state variations\n" +
			"1..9\tJump State\tDirectly jump to state preset index\n" +
			"t\tToggle Theme\tCycle color scheme across 4 canonical palettes\n" +
			"q\tQuit\tExit the Storybook environment"
	}

	rows := uirows.TabAlignedRows(input)
	var b strings.Builder
	b.WriteString(bold("Tab-Aligned Tabular Output:") + "\n\n")
	for _, r := range rows {
		b.WriteString(r + "\n")
	}
	return b.String()
}

func (s *tabAlignedStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- 5. Prompt Queues Story

type promptQueuesStory struct {
	state  int
	width  int
	height int
}

func newPromptQueuesStory() Story {
	return Story{
		ID:          "composer-prompt-queues",
		Name:        "Prompt Queues & Notices",
		Level:       LevelMolecule,
		Category:    CategoryComposer,
		UpstreamSym: "jetski/cli.PromptQueues",
		Description: "Input queue rows displayed immediately above the composer: sending indicator, queued user prompts with execution hints, queued shell commands, and cancelled turn notices.",
		States: []State{
			{ID: "sending-prompt", Name: "In-Flight Sending Prompt", Description: "Prompt dispatched while waiting for model to acknowledge"},
			{ID: "queued-prompts", Name: "Queued Prompts with Hint", Description: "Multiple queued prompts with (alt+m to send immediately)"},
			{ID: "queued-commands", Name: "Queued Bash Commands", Description: "Queued bash execution items waiting for turn completion"},
			{ID: "interrupted-hint", Name: "Turn Interrupted Notice", Description: "Dim hint displayed when user cancels turn with Esc"},
		},
		Factory: func() StoryInstance {
			return &promptQueuesStory{width: 80, height: 24}
		},
	}
}

func (s *promptQueuesStory) SetState(idx int) {
	s.state = idx
}

func (s *promptQueuesStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *promptQueuesStory) View() string {
	var b strings.Builder
	b.WriteString(bold("Composer Input Queue & Notice Rows Demonstration:") + "\n\n")

	switch s.state {
	case 0:
		b.WriteString(uirows.SendingMessageRow("Please check the test coverage of all components") + "\n")
	case 1:
		b.WriteString(uirows.QueuedPromptRowWithHint("First queued prompt for execution", false) + "\n")
		b.WriteString(uirows.QueuedPromptRowWithHint("Second prompt waiting in queue", true) + "\n")
	case 2:
		b.WriteString(uirows.QueuedCommandRow("go test -v ./internal/ui/storybook/...") + "\n")
		b.WriteString(uirows.QueuedCommandRow("make quick") + "\n")
	case 3:
		b.WriteString(palette.DimGrayOpen + "Turn cancelled by user" + palette.DimGrayClose + "\n")
	}

	b.WriteString(palette.Rule(s.width) + "\n")
	b.WriteString("> _\n")
	b.WriteString(palette.Rule(s.width) + "\n")

	return b.String()
}

func (s *promptQueuesStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- 6. Diff Row Story

type diffRowStory struct {
	state  int
	width  int
	height int
}

func newDiffRowStory() Story {
	return Story{
		ID:          "toolsteps-diff-row",
		Name:        "Atomic Diff Row",
		Level:       LevelAtom,
		Category:    CategoryToolSteps,
		UpstreamSym: "jetski/cortex/diff.Row",
		Description: "Atomic unified diff line renderer with line numbers, 4-column marker gutters, added/deleted color tints, and hunk header formatting.",
		States: []State{
			{ID: "hunk-and-changes", Name: "Unified Diff Hunk", Description: "Hunk header, deleted lines (-), added lines (+), and context"},
			{ID: "wide-gutter", Name: "5-Digit Line Numbers", Description: "Wide gutter alignment for large source files"},
			{ID: "more-marker", Name: "Truncation More Marker", Description: "Diff lines truncated with '... more lines' indicator"},
		},
		Factory: func() StoryInstance {
			return &diffRowStory{width: 80, height: 24}
		},
	}
}

func (s *diffRowStory) SetState(idx int) {
	s.state = idx
}

func (s *diffRowStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *diffRowStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}

	maxLine := 45
	if s.state == 1 {
		maxLine = 12500
	}
	layout := diffrow.NewLayout(w, maxLine)
	style := diffrow.TranscriptStyle()

	var b strings.Builder
	b.WriteString(bold("Atomic Unified Diff Lines:") + "\n\n")

	header, ok := diffrow.ParseHunkHeader("@@ -12,5 +12,7 @@ func RegisterStories(c *Catalog) {")
	if ok {
		b.WriteString(palette.DimGrayOpen + fmt.Sprintf("@@ -%d,%d +%d,%d @@", header.OldStart, header.OldCount, header.NewStart, header.NewCount) + palette.DimGrayClose + "\n")
	}

	rows := []diffrow.Row{
		{Kind: diffrow.Context, Num: 12, Text: "func RegisterStories(c *Catalog) {"},
		{Kind: diffrow.Remove, Num: 13, Text: "\tc.Register(oldLegacyComponent())"},
		{Kind: diffrow.Add, Num: 13, Text: "\tc.Register(newAtomicDiffRowStory())"},
		{Kind: diffrow.Add, Num: 14, Text: "\tc.Register(newCodeSearchActionStory())"},
		{Kind: diffrow.Context, Num: 15, Text: "}"},
	}

	for _, r := range rows {
		b.WriteString(diffrow.Render(layout, style, r) + "\n")
	}

	if s.state == 2 {
		b.WriteString(diffrow.More(2, 48) + "\n")
	}

	b.WriteString("\n" + dim("Summary: ") + diffrow.Summary(2, 1) + "\n")
	return b.String()
}

func (s *diffRowStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- 7. CodeSearch Action Story

type codeSearchActionStory struct {
	state  int
	width  int
	height int
}

func newCodeSearchActionStory() Story {
	return Story{
		ID:          "toolsteps-codesearch-action",
		Name:        "CodeSearch One-Shot Feedback",
		Level:       LevelMolecule,
		Category:    CategoryToolSteps,
		UpstreamSym: "jetski/cli/codesearch.Feedback",
		Description: "One-shot workspace regex search feedback output printed prior to opening the interactive panel, including match highlights and overflow count.",
		States: []State{
			{ID: "matches-found", Name: "Found Matches Tree", Description: "Rendered match rows with file paths and line numbers"},
			{ID: "overflow-tail", Name: "MaxHits Truncation", Description: "Tail indicator with withheld match count"},
			{ID: "no-results", Name: "Zero Matches", Description: "Informative message when query yields no hits"},
		},
		Factory: func() StoryInstance {
			return &codeSearchActionStory{width: 80, height: 24}
		},
	}
}

func (s *codeSearchActionStory) SetState(idx int) {
	s.state = idx
}

func (s *codeSearchActionStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *codeSearchActionStory) View() string {
	var b strings.Builder
	b.WriteString(bold("CodeSearch Immediate Terminal Feedback (/codesearch):") + "\n\n")

	switch s.state {
	case 1:
		b.WriteString("Found 245 results\n")
		b.WriteString("cmd/storybook/main.go:12:1: func main() {\n")
		b.WriteString("internal/ui/storybook/catalog.go:45:6: func (c *Catalog) Register(s Story) {\n")
		b.WriteString("internal/ui/storybook/sidebar.go:88:2: func renderTree(c *Catalog) string {\n")
		b.WriteString(fmt.Sprintf("  │ %d more", 242) + "\n")
	case 2:
		b.WriteString("No matches found for pattern: nonexistent_query_token\n")
		b.WriteString(dim(codesearchaction.Usage) + "\n")
	default:
		b.WriteString("Found 3 results\n")
		b.WriteString("internal/ui/panelregistry/registry.go:10:1: type Kind int\n")
		b.WriteString("internal/ui/panelregistry/registry.go:35:2: FeatDiff\n")
		b.WriteString("internal/ui/panelregistry/registry.go:58:1: func (k Kind) IsViewer() bool {\n")
	}

	return b.String()
}

func (s *codeSearchActionStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- 8. Eligibility Notice Story

type eligibilityNoticeStory struct {
	state  int
	width  int
	height int
}

func newEligibilityNoticeStory() Story {
	return Story{
		ID:          "status-eligibility-notice",
		Name:        "Eligibility Notice Card",
		Level:       LevelMolecule,
		Category:    CategoryStatus,
		UpstreamSym: "jetski/cli/notice.RenderNoticeRow",
		Description: "Inline eligibility and tier verification notice displayed in the input loop with optional OSC 8 clickable URL support.",
		States: []State{
			{ID: "phone-verification-osc8", Name: "Phone Verification (OSC 8)", Description: "Notice with clickable terminal hyperlink"},
			{ID: "plain-fallback", Name: "Plain Text Fallback", Description: "Notice without OSC 8 terminal capabilities"},
			{ID: "tier-ineligible", Name: "Tier Ineligible Notice", Description: "Notice prompting tier subscription upgrade"},
		},
		Factory: func() StoryInstance {
			return &eligibilityNoticeStory{width: 80, height: 24}
		},
	}
}

func (s *eligibilityNoticeStory) SetState(idx int) {
	s.state = idx
}

func (s *eligibilityNoticeStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *eligibilityNoticeStory) View() string {
	var reason string
	var url string
	var caps render.HyperlinkCaps

	switch s.state {
	case 1:
		reason = "Phone verification required to unlock high-reasoning models"
		url = "https://antigravity.google/verify?account=test"
		caps = render.HyperlinkCaps{OSC8: false}
	case 2:
		reason = "Your current tier does not support multi-agent swarm orchestration"
		url = "https://antigravity.google/upgrade"
		caps = render.HyperlinkCaps{OSC8: true}
	default:
		reason = "Phone verification required to unlock high-reasoning models"
		url = "https://antigravity.google/verify?account=test"
		caps = render.HyperlinkCaps{OSC8: true}
	}

	row := announce.RenderNoticeRow(reason, url, caps)

	var b strings.Builder
	b.WriteString(bold("Inline Eligibility Notice Row:") + "\n\n")
	b.WriteString(row + "\n\n")
	b.WriteString(palette.Rule(s.width) + "\n")
	b.WriteString("> [Composer input is ready here]\n")
	b.WriteString(palette.Rule(s.width) + "\n\n")
	b.WriteString(dim("▶ Keystroke in composer clears this inline notice automatically") + "\n")
	return b.String()
}

func (s *eligibilityNoticeStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

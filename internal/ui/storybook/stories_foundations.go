package storybook

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/onboarding"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/palette"
)

// RegisterFoundationsStories registers typography, themes, and rule stories.
func RegisterFoundationsStories(c *Catalog) {
	// 1. ThemePreview
	c.Register(Story{
		ID:          "foundations-theme-preview",
		Name:        "ThemePreview (Color Schemes)",
		Category:    CategoryFoundations,
		Description: "The 63-column diff card shown beside the /config Color Scheme menu.",
		UpstreamSym: "jetski/cli.ThemePreview",
		States: []State{
			{ID: "solarized-light", Name: "Solarized Light", Description: "Standard reference solarized light theme"},
			{ID: "dark", Name: "Dark", Description: "High-contrast dark theme"},
			{ID: "tokyo-night", Name: "Tokyo Night", Description: "Tokyo night modern dark palette"},
			{ID: "terminal", Name: "Terminal", Description: "Standard ANSI 16-color terminal scheme"},
		},
		Factory: func() StoryInstance {
			return &themePreviewStory{state: 0}
		},
	})

	// 2. MarkdownRenderer
	c.Register(Story{
		ID:          "foundations-markdown",
		Name:        "MarkdownRenderer (Typography)",
		Category:    CategoryFoundations,
		Description: "Styled terminal markdown with code blocks, tables, lists, links, alerts, LaTeX math, and Mermaid ASCII diagrams.",
		UpstreamSym: "jetski/cli.Markdown",
		States: []State{
			{ID: "headings-code", Name: "Code & Headings", Description: "Fenced code blocks and heading scales"},
			{ID: "lists-tables", Name: "Lists & Tables", Description: "Bulleted lists and GFM tables"},
			{ID: "quotes-links", Name: "Quotes & Links", Description: "Blockquotes and terminal OSC 8 hyperlinks"},
			{ID: "mermaid-flowchart", Name: "Mermaid Flowchart", Description: "Terminal ASCII flowchart box-and-arrow rendering"},
			{ID: "mermaid-sequence", Name: "Mermaid Sequence", Description: "Sequence diagram participant interaction flows"},
			{ID: "latex-math", Name: "LaTeX Math Notation", Description: "Inline and display mathematical formulas rendered in Unicode"},
			{ID: "github-alerts", Name: "GitHub Alerts", Description: "Callout alerts: NOTE, TIP, IMPORTANT, WARNING, CAUTION"},
		},
		Factory: func() StoryInstance {
			return &markdownStory{state: 0, width: 80}
		},
	})

	// 3. PromptGlyphs & Rules
	c.Register(Story{
		ID:          "foundations-rules",
		Name:        "PromptGlyphs & Rules (Dividers)",
		Category:    CategoryFoundations,
		Description: "Top/bottom prompt rules, bash glyph '!' vs normal '>', and section dividers.",
		UpstreamSym: "jetski/cli/model.PromptModel",
		States: []State{
			{ID: "normal-rule", Name: "Normal Rules", Description: "Double-line composer boundaries"},
			{ID: "bash-mode", Name: "Bash Mode Glyph", Description: "Red/blue '!' bash prompt prefix"},
			{ID: "separators", Name: "Section Dividers", Description: "Dim horizontal dividers and status separators"},
		},
		Factory: func() StoryInstance {
			return &rulesStory{state: 0, width: 80}
		},
	})

	// 4. LogoTransforms
	c.Register(Story{
		ID:          "foundations-logo-transforms",
		Name:        "LogoTransforms (Delta & Blocks)",
		Category:    CategoryFoundations,
		Description: "Brand delta rotations, Apple Terminal 5x6 color blocks, and sub-pixel color inversion transforms.",
		UpstreamSym: "jetski/cli.Logo",
		States: []State{
			{ID: "pigasus-default", Name: "Pigasus Inverted Delta", Description: "180° rotated winged delta (FlipBoth)"},
			{ID: "reference-agy", Name: "Reference AgY Upward Delta", Description: "Upstream Google Antigravity upward delta"},
			{ID: "flip-vert", Name: "Vertical Flip", Description: "Reflected across horizontal axis"},
			{ID: "flip-horiz", Name: "Horizontal Flip", Description: "Mirrored across vertical axis"},
			{ID: "invert-colors", Name: "Sub-pixel Color Invert", Description: "Top and bottom half-block sub-pixels swapped"},
			{ID: "simple-blocks-5x6", Name: "Apple Terminal 5x6 Blocks", Description: "Full-cell background block raster for Apple Terminal"},
			{ID: "all-gallery", Name: "All Transforms Gallery", Description: "Side-by-side comparison of all 6 variants"},
		},
		Factory: func() StoryInstance {
			return &logoTransformsStory{state: 0, width: 80}
		},
	})

	// 5. BoxBorders (Frames & Glyphs)
	c.Register(Story{
		ID:          "foundations-box-borders",
		Name:        "BoxBorders (Frames & Glyphs)",
		Category:    CategoryFoundations,
		Description: "Box-drawing character styles (Normal, Rounded, Thick, Double, Dotted, Left-Accent), tagged header framing, and dialogue box palettes.",
		UpstreamSym: "jetski/cli.BorderStyles",
		States: []State{
			{ID: "normal-sharp", Name: "Normal / Sharp (┌─┐)", Description: "Standard single-line box drawing for PermissionPrompt, AnnouncementCard, and forms"},
			{ID: "rounded-curved", Name: "Rounded Curved (╭─╮)", Description: "Modern curved border for notification popovers, floating cards, and menus"},
			{ID: "thick-heavy", Name: "Thick / Heavy (┏━┓)", Description: "High-emphasis bold framing and active focus boundaries"},
			{ID: "double-line", Name: "Double Line (╔═╗)", Description: "Classical high-importance modal overlays and security boundary alerts"},
			{ID: "dotted-dashed", Name: "Dashed & Dotted (┌┈┐)", Description: "Dotted & dashed dividers for scratchpads, drafting turns, and secondary drawers"},
			{ID: "left-accent", Name: "Left Accent Callout (┃ / ▌)", Description: "Thick left callout bar for /btw side questions, blockquotes, and thought streams"},
			{ID: "all-gallery", Name: "All Border Styles Matrix", Description: "Side-by-side gallery comparing all 6 framing styles"},
		},
		Factory: func() StoryInstance {
			return &boxBordersStory{state: 0, width: 80, boxWidth: 60, showTitle: true}
		},
	})
}

type themePreviewStory struct {
	state int
	w, h  int
}

func (s *themePreviewStory) SetState(idx int)            { s.state = idx }
func (s *themePreviewStory) SetSize(w, h int)           { s.w, s.h = w, h }
func (s *themePreviewStory) HandleKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }

func (s *themePreviewStory) View() string {
	var scheme onboarding.ColorScheme
	switch s.state {
	case 0:
		scheme = onboarding.SchemeSolarizedLight
	case 1:
		scheme = onboarding.SchemeDark
	case 2:
		scheme = onboarding.SchemeTokyoNight
	case 3:
		scheme = onboarding.SchemeTerminal
	default:
		scheme = onboarding.SchemeSolarizedLight
	}

	lines := themepreview.Render(scheme)
	return strings.Join(lines, "\n")
}

type markdownStory struct {
	state int
	width int
	h     int
}

func (s *markdownStory) SetState(idx int)            { s.state = idx }
func (s *markdownStory) SetSize(w, h int)           { s.width, s.h = w, h }
func (s *markdownStory) HandleKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }

func (s *markdownStory) View() string {
	var src string
	switch s.state {
	case 0:
		src = `# Architecture Overview

This is **Antigravity CLI** (*openagy/pigasus*).

## Implementation Sample

` + "```go" + `
package main

import "fmt"

func main() {
    fmt.Println("Clean-room parity agent!")
}
` + "```" + `
`
	case 1:
		src = `### Task Matrix

| Component | Status | Upstream Alignment |
| :--- | :---: | :--- |
| PromptModel | **Ready** | 100% 1:1 Parity |
| SlashPicker | **Ready** | 105 Canonical commands |
| Storybook | **New** | Native TUI Catalog |

* Bullet item one
* Bullet item two with *emphasis*
  * Nested sub-item
`
	case 2:
		src = `> [!NOTE]
> The reference colours the command token in the composer brand blue.

Visit [Google Deepmind](https://deepmind.google) or read the local documentation.
`
	case 3: // mermaid-flowchart
		src = "### Mermaid ASCII Flowchart\n\n```mermaid\ngraph TD\n    A[User Request] --> B{Parse Input}\n    B -->|Slash Command| C[Slash Dispatcher]\n    B -->|Agent Prompt| D[Turn Engine]\n    C --> E[Modal View / Storybook]\n    D --> F[Tool Execution & LLM Response]\n```\n"
	case 4: // mermaid-sequence
		src = "### Mermaid Sequence Interaction\n\n```mermaid\nsequenceDiagram\n    autonumber\n    participant U as User\n    participant T as TUI Host\n    participant E as EngineBridge\n    participant S as Server\n    U->>T: KeyPress (Enter)\n    T->>E: SendTurnPrompt()\n    E->>S: StreamRequest()\n    S-->>E: StreamChunk\n    E-->>T: DeltaMsg\n    T-->>U: Redraw View\n```\n"
	case 5: // latex-math
		src = `### Mathematical Formulations & LaTeX

Inline formula: The attention weight is $\alpha_{i,j} = \frac{\exp(e_{i,j})}{\sum_{k=1}^n \exp(e_{i,k})}$.

Display formula:

$$\sigma(z)_j = \frac{e^{z_j}}{\sum_{k=1}^K e^{z_k}} \quad \text{for } j = 1, \dots, K$$

And polynomial evaluation:

$$f(x) = \sum_{i=0}^n a_i x^i$$
`
	case 6: // github-alerts
		src = `### GitHub Alerts & Callouts

> [!NOTE]
> Reference implementation compatibility is verified against version 1.5.5.

> [!TIP]
> Press 'Tab' to cycle states and 'p' to enter full interactive playground mode!

> [!IMPORTANT]
> Tier 5 UI components must never import entry hosts (R2 layering rule).

> [!WARNING]
> Esc Esc double-tap is required to clear composer input; solitary Esc cancels turns.

> [!CAUTION]
> Deleting workspaces or killing agents permanently removes ephemeral branched states.
`
	}

	w := s.width - 4
	if w < 20 {
		w = 60
	}
	return mdrender.RenderMarkdown(src, w)
}

type rulesStory struct {
	state int
	width int
	h     int
}

func (s *rulesStory) SetState(idx int)            { s.state = idx }
func (s *rulesStory) SetSize(w, h int)           { s.width, s.h = w, h }
func (s *rulesStory) HandleKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }

func (s *rulesStory) View() string {
	w := s.width - 4
	if w < 10 {
		w = 60
	}
	var b strings.Builder
	switch s.state {
	case 0:
		b.WriteString(dim("Top Rule (Composer Upper Frame):") + "\n")
		b.WriteString(dim(strings.Repeat("─", w)) + "\n\n")
		b.WriteString("  " + blue("> ") + "Ask anything or type / for commands\n\n")
		b.WriteString(dim("Bottom Rule (Composer Lower Frame):") + "\n")
		b.WriteString(dim(strings.Repeat("─", w)) + "\n")
	case 1:
		b.WriteString(bold("Normal Interaction Glyph:") + "\n")
		b.WriteString("  " + blue("> ") + "explore the codebase\n\n")
		b.WriteString(bold("Vim Bash Mode Glyph ('!'):") + "\n")
		b.WriteString("  " + red("! ") + "git status -s\n")
	case 2:
		b.WriteString("Section A: Transcript Row\n")
		b.WriteString(dim(strings.Repeat("┈", w)) + "\n")
		b.WriteString("Section B: Active Items Drawer\n")
		b.WriteString(dim(strings.Repeat("─", w)) + "\n")
		b.WriteString("Section C: Terminal Bottom Status\n")
	}
	return fmt.Sprintf("\n%s", b.String())
}

type logoTransformsStory struct {
	state int
	width int
	h     int
}

func (s *logoTransformsStory) SetState(idx int)            { s.state = idx }
func (s *logoTransformsStory) SetSize(w, h int)           { s.width, s.h = w, h }
func (s *logoTransformsStory) HandleKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }

func (s *logoTransformsStory) View() string {
	switch s.state {
	case 0: // pigasus-default
		lines := tuilib.RenderGridLines(tuilib.PigasusLogoPixels)
		return formatLogoCard("Pigasus Inverted Delta (180° / FlipBoth)", "Canonical brand logo: inverted winged delta representing open freedom", lines[:])
	case 1: // reference-agy
		lines := tuilib.RenderGridLines(tuilib.RefLogoPixels)
		return formatLogoCard("Reference Antigravity Upward Delta", "Upstream Google Antigravity reference 10×16 sub-pixel raster", lines[:])
	case 2: // flip-vert
		lines := tuilib.RenderGridLines(tuilib.FlipVert(tuilib.RefLogoPixels))
		return formatLogoCard("Vertical Flip (FlipVert)", "Raster reflected vertically across horizontal axis", lines[:])
	case 3: // flip-horiz
		lines := tuilib.RenderGridLines(tuilib.FlipHoriz(tuilib.RefLogoPixels))
		return formatLogoCard("Horizontal Flip (FlipHoriz)", "Raster mirrored horizontally across vertical axis", lines[:])
	case 4: // invert-colors
		lines := tuilib.RenderGridLines(tuilib.InvertColors(tuilib.PigasusLogoPixels))
		return formatLogoCard("Sub-pixel Color Inversion (InvertColors)", "Top (▀) and bottom (▄) sub-pixel bytes inverted within each cell", lines[:])
	case 5: // simple-blocks-5x6
		refBlocks := tuilib.RenderBlockGridLines(tuilib.RefLogoBlockGrid)
		pigasusBlocks := tuilib.RenderBlockGridLines(tuilib.PigasusLogoBlockGrid)
		var b strings.Builder
		b.WriteString(bold("Apple Terminal 5×6 Color Block Style (TERM_PROGRAM=Apple_Terminal / iTerm.app)") + "\n")
		b.WriteString(dim("Uses full-cell ANSI background colors for terminals with half-block alignment quirks.") + "\n\n")
		b.WriteString(cyan("  Reference Upward Blocks:         Pigasus Inverted Blocks:") + "\n")
		for r := 0; r < 5; r++ {
			b.WriteString("  " + refBlocks[r] + "       " + pigasusBlocks[r] + "\n")
		}
		return b.String()
	case 6: // all-gallery
		pigasus := tuilib.RenderGridLines(tuilib.PigasusLogoPixels)
		ref := tuilib.RenderGridLines(tuilib.RefLogoPixels)
		vert := tuilib.RenderGridLines(tuilib.FlipVert(tuilib.RefLogoPixels))
		horiz := tuilib.RenderGridLines(tuilib.FlipHoriz(tuilib.RefLogoPixels))
		invert := tuilib.RenderGridLines(tuilib.InvertColors(tuilib.PigasusLogoPixels))
		pigasusBlocks := tuilib.RenderBlockGridLines(tuilib.PigasusLogoBlockGrid)

		var b strings.Builder
		b.WriteString(bold("✦ Pigasus & Antigravity Logo Transformation Gallery ✦") + "\n\n")

		b.WriteString(cyan("1. Pigasus Inverted (Default)      2. Reference AgY (Upstream)\n"))
		for r := 0; r < 5; r++ {
			b.WriteString("   " + pigasus[r] + "          " + ref[r] + "\n")
		}
		b.WriteString("\n")
		b.WriteString(cyan("3. Vertical Flip                   4. Horizontal Flip\n"))
		for r := 0; r < 5; r++ {
			b.WriteString("   " + vert[r] + "          " + horiz[r] + "\n")
		}
		b.WriteString("\n")
		b.WriteString(cyan("5. Color Inverted Sub-pixels       6. 5×6 Full-Block Mode\n"))
		for r := 0; r < 5; r++ {
			b.WriteString("   " + invert[r] + "          " + pigasusBlocks[r] + "\n")
		}
		return b.String()
	}
	return ""
}

func formatLogoCard(title, subtitle string, lines []string) string {
	var b strings.Builder
	b.WriteString(bold(title) + "\n")
	b.WriteString(dim(subtitle) + "\n\n")
	for _, l := range lines {
		b.WriteString("    " + l + "\n")
	}
	return b.String()
}

// ---------------------------------------------------------------- BoxBorders Story

type BorderDefinition struct {
	Name        string
	Category    string
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	Horizontal  string
	Vertical    string
	GlyphSpec   string
	Role        string
}

var allBorders = []BorderDefinition{
	{
		Name:        "Normal / Sharp",
		Category:    "Single-Line Box",
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "└",
		BottomRight: "┘",
		Horizontal:  "─",
		Vertical:    "│",
		GlyphSpec:   "┌ U+250C  ·  ┐ U+2510  ·  └ U+2514  ·  ┘ U+2518  ·  ─ U+2500  ·  │ U+2502",
		Role:        "Standard dialog framing for PermissionPrompt, AnnouncementCard, and form popups.",
	},
	{
		Name:        "Rounded Curved",
		Category:    "Modern Arc Box",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "╰",
		BottomRight: "╯",
		Horizontal:  "─",
		Vertical:    "│",
		GlyphSpec:   "╭ U+256D  ·  ╮ U+256E  ·  ╰ U+2570  ·  ╯ U+256F  ·  ─ U+2500  ·  │ U+2502",
		Role:        "Modern friendly aesthetics for toast popovers, informational tooltips, and floating cards.",
	},
	{
		Name:        "Thick / Heavy",
		Category:    "High-Emphasis Box",
		TopLeft:     "┏",
		TopRight:    "┓",
		BottomLeft:  "┗",
		BottomRight: "┛",
		Horizontal:  "━",
		Vertical:    "┃",
		GlyphSpec:   "┏ U+250F  ·  ┓ U+2513  ·  ┗ U+2517  ·  ┛ U+251B  ·  ━ U+2501  ·  ┃ U+2503",
		Role:        "High-priority focus rings, primary action dialogs, and active tool containers.",
	},
	{
		Name:        "Double Line",
		Category:    "Classical Ruled Box",
		TopLeft:     "╔",
		TopRight:    "╗",
		BottomLeft:  "╚",
		BottomRight: "╝",
		Horizontal:  "═",
		Vertical:    "║",
		GlyphSpec:   "╔ U+2554  ·  ╗ U+2557  ·  ╚ U+255A  ·  ╝ U+255D  ·  ═ U+2550  ·  ║ U+2551",
		Role:        "Security boundary prompts, critical modal confirmation, and classical system barriers.",
	},
	{
		Name:        "Dashed & Dotted",
		Category:    "Subordinate / Scratch Box",
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "└",
		BottomRight: "┘",
		Horizontal:  "┈",
		Vertical:    "┊",
		GlyphSpec:   "┌ U+250C  ·  ┐ U+2510  ·  └ U+2514  ·  ┘ U+2518  ·  ┈ U+2548  ·  ┊ U+250A",
		Role:        "Subagent scratchpad drawers, ephemeral thought steps, and secondary preview panes.",
	},
	{
		Name:        "Left Accent Callout",
		Category:    "Open Side Callout",
		TopLeft:     "",
		TopRight:    "",
		BottomLeft:  "",
		BottomRight: "",
		Horizontal:  "",
		Vertical:    "┃",
		GlyphSpec:   "┃ U+2503 (Thick Left Border)  /  ▌ U+258C (Left Half Block)",
		Role:        "Side-question /btw floating cards, markdown blockquotes (> ), and streaming reasoning snippets.",
	},
}

type boxBordersStory struct {
	state     int
	width     int
	h         int
	boxWidth  int
	showTitle bool
}

func (s *boxBordersStory) SetState(idx int) { s.state = idx }
func (s *boxBordersStory) SetSize(w, h int) {
	s.width, s.h = w, h
	if s.boxWidth <= 0 {
		s.boxWidth = 60
	}
}

func (s *boxBordersStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	k := msg.Key()
	switch {
	case k.Code == '+' || k.Code == '=' || k.Code == tea.KeyRight || k.Code == 'l':
		maxW := s.width - 4
		if maxW < 30 {
			maxW = 80
		}
		if s.boxWidth < maxW {
			s.boxWidth += 2
			return nil, true
		}
	case k.Code == '-' || k.Code == '_' || k.Code == tea.KeyLeft || k.Code == 'h':
		if s.boxWidth > 32 {
			s.boxWidth -= 2
			return nil, true
		}
	case k.Code == ' ' || k.Code == 'c' || k.Code == 'C':
		s.showTitle = !s.showTitle
		return nil, true
	case k.Code == '0':
		s.boxWidth = 60
		return nil, true
	}
	return nil, false
}

func (s *boxBordersStory) View() string {
	bw := s.boxWidth
	if bw <= 0 {
		bw = 60
	}
	maxW := s.width - 6
	if maxW > 20 && bw > maxW {
		bw = maxW
	}

	var sb strings.Builder

	if s.state >= 0 && s.state < len(allBorders) {
		bDef := allBorders[s.state]
		sb.WriteString(bold("❖ Border Style: "+bDef.Name+" ("+bDef.Category+")") + "\n")
		sb.WriteString(dim("  Glyphs: ") + cyan(bDef.GlyphSpec) + "\n")
		sb.WriteString(dim("  Usage:  ") + bDef.Role + "\n\n")

		if s.state == 5 { // Left Accent Callout
			sb.WriteString(renderLeftAccentCard(bw, s.showTitle))
		} else {
			sb.WriteString(renderCustomBox(bDef, "Permission Approval: execute_command",
				"Tool:    run_command\n"+
					"Command: go test -v ./...\n"+
					"Reason:  verifying all box border rendering invariants",
				"[y] Approve once  [a] Always allow  [n] Deny",
				bw, s.showTitle))
		}

		sb.WriteString("\n" + dim("Interactive Controls: [+/-] Resize width ("+fmt.Sprintf("%d", bw)+")  ·  [Space/c] Toggle tagged header  ·  [Tab] Cycle styles"))
		return sb.String()
	}

	// State 6: All gallery
	sb.WriteString(bold("✦ Terminal Box-Drawing & Border Style Matrix ✦") + "\n")
	sb.WriteString(dim("Comprehensive typography and framing styles used across Antigravity CLI & Pigasus components:") + "\n\n")

	// Table comparing styles
	sb.WriteString(cyan(fmt.Sprintf("  %-18s %-12s %-12s %s\n", "Style Name", "Corners", "Lines", "Upstream agy Component Role")))
	sb.WriteString(dim("  "+strings.Repeat("─", 74)) + "\n")
	for _, b := range allBorders {
		corners := b.TopLeft + " " + b.TopRight + " " + b.BottomLeft + " " + b.BottomRight
		if b.TopLeft == "" {
			corners = " (none) "
		}
		lines := b.Horizontal + " " + b.Vertical
		if b.Horizontal == "" {
			lines = "  " + b.Vertical + "  "
		}
		sb.WriteString(fmt.Sprintf("  %-18s %-12s %-12s %s\n", bold(b.Name), corners, lines, dim(b.Role)))
	}
	sb.WriteString("\n")

	// Side-by-side or stacked demo cards
	sb.WriteString(cyan("1. Sharp (NormalBorder)             2. Rounded (RoundedBorder)\n"))
	card1 := renderMiniBox(allBorders[0], "Standard Dialog", "PermissionPrompt / Notices", 34)
	card2 := renderMiniBox(allBorders[1], "Modern Card", "Toasts & floating popovers", 34)
	sb.WriteString(joinCardColumns(card1, card2) + "\n\n")

	sb.WriteString(cyan("3. Thick (HeavyBorder)              4. Double (ClassicalRuled)\n"))
	card3 := renderMiniBox(allBorders[2], "High Priority", "Active focus boundaries", 34)
	card4 := renderMiniBox(allBorders[3], "Security Boundary", "Critical system alert", 34)
	sb.WriteString(joinCardColumns(card3, card4) + "\n\n")

	sb.WriteString(cyan("5. Dotted / Dashed                  6. Left Accent Callout\n"))
	card5 := renderMiniBox(allBorders[4], "Scratch Drawer", "Subagent thinking steps", 34)
	card6 := renderMiniCallout("Side Question", "/btw floating card answer", 34)
	sb.WriteString(joinCardColumns(card5, card6) + "\n")

	return sb.String()
}

func renderCustomBox(border BorderDefinition, title, body, hint string, width int, showTitle bool) string {
	if width < 30 {
		width = 30
	}
	innerWidth := width - 6
	if innerWidth < 10 {
		innerWidth = 10
	}

	var sb strings.Builder

	// Top border
	sb.WriteString("  " + palette.DimGrayOpen + border.TopLeft)
	if showTitle && title != "" {
		tag := " " + title + " "
		tagLen := ansi.StringWidth(tag)
		topDashes := width - (tagLen + 5)
		if topDashes < 1 {
			topDashes = 1
		}
		sb.WriteString(border.Horizontal + palette.BlueOpen + bold(tag) + palette.DimGrayOpen + strings.Repeat(border.Horizontal, topDashes))
	} else {
		sb.WriteString(strings.Repeat(border.Horizontal, width-4))
	}
	sb.WriteString(border.TopRight + palette.DimGrayClose + "\n")

	// Body content
	bodyLines := strings.Split(body, "\n")
	for _, line := range bodyLines {
		sb.WriteString("  " + palette.DimGrayOpen + border.Vertical + palette.DimGrayClose + " ")
		vis := ansi.StringWidth(line)
		pad := innerWidth - vis
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(line + strings.Repeat(" ", pad))
		sb.WriteString(" " + palette.DimGrayOpen + border.Vertical + palette.DimGrayClose + "\n")
	}

	// Hint line (if present)
	if hint != "" {
		sb.WriteString("  " + palette.DimGrayOpen + border.Vertical + palette.DimGrayClose + " ")
		hintVis := ansi.StringWidth(hint)
		pad := innerWidth - hintVis
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(dim(hint) + strings.Repeat(" ", pad))
		sb.WriteString(" " + palette.DimGrayOpen + border.Vertical + palette.DimGrayClose + "\n")
	}

	// Bottom border
	sb.WriteString("  " + palette.DimGrayOpen + border.BottomLeft + strings.Repeat(border.Horizontal, width-4) + border.BottomRight + palette.DimGrayClose + "\n")

	return sb.String()
}

func renderLeftAccentCard(width int, showTitle bool) string {
	var sb strings.Builder
	bar := palette.BlueOpen + "┃" + palette.BlueClose
	if showTitle {
		sb.WriteString("  " + bar + "  " + palette.BlueOpen + bold("Side Question (/btw) · Gemini 3.8 Flash") + palette.BlueClose + "\n")
		sb.WriteString("  " + bar + "  " + bold(`"How does box-drawing character rendering work?"`) + "\n")
		sb.WriteString("  " + bar + "\n")
	}
	sb.WriteString("  " + bar + "  Terminal box drawing characters reside in Unicode block U+2500–U+257F.\n")
	sb.WriteString("  " + bar + "  They allow full-terminal framing without graphics APIs.\n")
	sb.WriteString("  " + bar + "\n")
	sb.WriteString("  " + bar + "  " + dim("Tab to collapse · Enter/Esc to dismiss") + "\n")
	return sb.String()
}

func renderMiniBox(border BorderDefinition, title, desc string, width int) []string {
	var lines []string
	tag := " " + title + " "
	topDashes := width - (ansi.StringWidth(tag) + 3)
	if topDashes < 1 {
		topDashes = 1
	}
	lines = append(lines, palette.DimGrayOpen+border.TopLeft+border.Horizontal+palette.BlueOpen+tag+palette.DimGrayOpen+strings.Repeat(border.Horizontal, topDashes)+border.TopRight+palette.DimGrayClose)

	bodyPad := width - ansi.StringWidth(desc) - 4
	if bodyPad < 0 {
		bodyPad = 0
	}
	lines = append(lines, palette.DimGrayOpen+border.Vertical+palette.DimGrayClose+" "+desc+strings.Repeat(" ", bodyPad)+" "+palette.DimGrayOpen+border.Vertical+palette.DimGrayClose)

	lines = append(lines, palette.DimGrayOpen+border.BottomLeft+strings.Repeat(border.Horizontal, width-2)+border.BottomRight+palette.DimGrayClose)
	return lines
}

func renderMiniCallout(title, desc string, width int) []string {
	var lines []string
	bar := palette.BlueOpen + "┃" + palette.BlueClose
	lines = append(lines, bar+" "+bold(title))
	lines = append(lines, bar+" "+dim(desc))
	lines = append(lines, bar+" "+strings.Repeat("┄", width-3))
	return lines
}

func joinCardColumns(c1, c2 []string) string {
	maxRows := max(len(c1), len(c2))
	var out []string
	for r := 0; r < maxRows; r++ {
		l1, l2 := "", ""
		if r < len(c1) {
			l1 = c1[r]
		}
		if r < len(c2) {
			l2 = c2[r]
		}
		out = append(out, "  "+l1+"    "+l2)
	}
	return strings.Join(out, "\n")
}

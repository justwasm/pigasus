package storybook

import (
	"fmt"
	"strings"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// RegisterComposerStories registers prompt, textarea, CJK cursor, and attachment stories.
func RegisterComposerStories(c *Catalog) {
	// 1. PromptModel / Composer
	c.Register(Story{
		ID:          "composer-prompt-model",
		Name:        "PromptModel / Composer",
		Category:    CategoryComposer,
		Description: "Prompt textarea with mode banners, token highlighting, and undo/esc mechanics.",
		UpstreamSym: "jetski/cli/model.PromptModel",
		States: []State{
			{ID: "empty-plan-mode", Name: "Plan Mode Banner", Description: "Empty input with permissions.ModeBanner(ModePlan)"},
			{ID: "single-line-token", Name: "Command Token Styling", Description: "Coloured slash command '/model [model]' with argument hint"},
			{ID: "multiline-input", Name: "Multi-line Input", Description: "Multi-line input with wrapped rows"},
			{ID: "esc-clear-armed", Name: "Esc-Esc Clear Hint", Description: "Armed state for double-Esc prompt wipe"},
		},
		Factory: func() StoryInstance {
			inst := &composerStory{state: 0, width: 80}
			inst.initTextarea()
			return inst
		},
	})

	// 2. CJK & East Asian Cursor
	c.Register(Story{
		ID:          "composer-cjk-cursor",
		Name:        "CJKCursor (IME Anchoring)",
		Category:    CategoryComposer,
		Description: "Demonstrates East Asian double-width rune calculations and physical hardware cursor docking.",
		UpstreamSym: "jetski/cli.CursorTracker",
		States: []State{
			{ID: "chinese-sentence", Name: "Chinese Pinyin", Description: "Full sentence with double-width punctuation"},
			{ID: "mixed-cjk-ascii", Name: "Mixed CJK & English", Description: "Alternating single-width and double-width runes"},
			{ID: "cjk-multiline", Name: "Multi-line CJK", Description: "CJK text soft-wrapped across lines"},
		},
		Factory: func() StoryInstance {
			inst := &cjkCursorStory{state: 0, width: 80}
			inst.initTextarea()
			return inst
		},
	})

	// 3. AttachmentCards
	c.Register(Story{
		ID:          "composer-attachments",
		Name:        "AttachmentCards (Media Preview)",
		Category:    CategoryComposer,
		Description: "Pasted or dropped image attachment cards shown above the composer prompt.",
		UpstreamSym: "jetski/cli.AttachmentRows",
		States: []State{
			{ID: "collapsed", Name: "Collapsed (Single Row)", Description: "Badge showing number of attached images"},
			{ID: "expanded", Name: "Expanded (Detail Preview)", Description: "Full cards with file names and image metadata"},
		},
		Factory: func() StoryInstance {
			return &attachmentsStory{state: 0, width: 80}
		},
	})
}

type composerStory struct {
	state int
	width int
	h     int
	input tas.Model
}

func (s *composerStory) initTextarea() {
	s.input = tas.New()
	s.input.SetVirtualCursor(false)
	s.input.Prompt = "> "
	s.input.SetWidth(s.width - 6)
	s.input.SetHeight(1)
	s.input.Focus()
}

func (s *composerStory) SetState(idx int) {
	s.state = idx
	switch idx {
	case 0:
		s.input.SetValue("")
		s.input.Placeholder = permissions.ModeBanner(permissions.ModePlan)
	case 1:
		s.input.SetValue("/model claude-3-7-sonnet")
		s.input.Placeholder = ""
	case 2:
		s.input.SetValue("Please review the following changes:\n1. Refactor the turn engine\n2. Add TUI Storybook")
		s.input.Placeholder = ""
	case 3:
		s.input.SetValue("Draft text that can be cleared with double-Esc")
		s.input.Placeholder = ""
	}
}

func (s *composerStory) SetSize(w, h int) {
	s.width, s.h = w, h
	s.input.SetWidth(w - 6)
}

func (s *composerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(key)
	return cmd, true
}

func (s *composerStory) View() string {
	var b strings.Builder
	b.WriteString(dim("─ Composer Frame ─────────────────────────────────────") + "\n")

	val := s.input.Value()
	firstLine := tuilib.FirstLine(val)
	view := s.input.View()

	// Token coloring
	view = tuilib.StyleComposerCommandToken(view, firstLine)
	if hint, ok := tuilib.ComposerArgumentHint(firstLine); ok {
		view = tuilib.AppendComposerHint(view, dim(hint), firstLine)
	}

	b.WriteString(view + "\n")

	if s.state == 3 {
		b.WriteString(dim("  [Esc again to clear input]") + "\n")
	}

	b.WriteString(dim("──────────────────────────────────────────────────────") + "\n\n")

	// Diagnostics box
	cur := s.input.Cursor()
	curX := 0
	curY := 0
	if cur != nil {
		curX = cur.X
		curY = cur.Y
	}
	b.WriteString(bold("Cursor & Widget Metrics:") + "\n")
	b.WriteString(fmt.Sprintf("  • Value Length: %d chars\n", len(val)))
	b.WriteString(fmt.Sprintf("  • Line: %d, Column: %d\n", s.input.Line(), s.input.Column()))
	b.WriteString(fmt.Sprintf("  • Hardware Cursor: (X=%d, Y=%d)\n", curX, curY))
	b.WriteString(fmt.Sprintf("  • Virtual Cursor Disabled: %v (Hardware Cursor active)\n", !s.input.VirtualCursor()))

	return b.String()
}

type cjkCursorStory struct {
	state int
	width int
	h     int
	input tas.Model
}

func (s *cjkCursorStory) initTextarea() {
	s.input = tas.New()
	s.input.SetVirtualCursor(false)
	s.input.Prompt = "> "
	s.input.SetWidth(s.width - 6)
	s.input.Focus()
}

func (s *cjkCursorStory) SetState(idx int) {
	s.state = idx
	switch idx {
	case 0:
		s.input.SetValue("你好，世界！这是一条全角中文测试消息。")
	case 1:
		s.input.SetValue("Hello 世界, Antigravity 架构演进与 Clean-room 实现！")
	case 2:
		s.input.SetValue("第一行中文输入验证：光标位置随字符宽度移动\n第二行包含标点符号：【一手】官方逆向还原")
	}
	s.input.CursorEnd()
}

func (s *cjkCursorStory) SetSize(w, h int) {
	s.width, s.h = w, h
	s.input.SetWidth(w - 6)
}

func (s *cjkCursorStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(key)
	return cmd, true
}

func (s *cjkCursorStory) View() string {
	var b strings.Builder
	b.WriteString(dim("─ CJK Terminal IME Docking Frame ─────────────────────") + "\n")
	b.WriteString(s.input.View() + "\n")
	b.WriteString(dim("──────────────────────────────────────────────────────") + "\n\n")

	val := s.input.Value()
	cur := s.input.Cursor()
	curX := 0
	if cur != nil {
		curX = cur.X
	}

	visWidth := ansi.StringWidth(val)
	b.WriteString(bold("East Asian Width & IME Verification:") + "\n")
	b.WriteString(fmt.Sprintf("  • Input Text: %q\n", val))
	b.WriteString(fmt.Sprintf("  • Rune Count: %d runes\n", len([]rune(val))))
	b.WriteString(fmt.Sprintf("  • Visual Display Width: %d terminal cells\n", visWidth))
	b.WriteString(fmt.Sprintf("  • Expected Cursor X: %d cells ('> ' width 2 + text width %d)\n", 2+visWidth, visWidth))
	b.WriteString(fmt.Sprintf("  • Actual Cursor Position: X=%d\n", curX))
	if curX == 2+visWidth {
		b.WriteString(green("  ✓ PASS: Hardware cursor perfectly docks after the CJK text for OS IME.") + "\n")
	} else {
		b.WriteString(red("  ✗ DRIFT DETECTED: Hardware cursor does not match string cell width.") + "\n")
	}
	return b.String()
}

type attachmentsStory struct {
	state int
	width int
	h     int
}

func (s *attachmentsStory) SetState(idx int)  { s.state = idx }
func (s *attachmentsStory) SetSize(w, h int) { s.width, s.h = w, h }
func (s *attachmentsStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if isCtrlO(key) {
		if s.state == 0 {
			s.state = 1
		} else {
			s.state = 0
		}
		return nil, true
	}
	return nil, false
}

func (s *attachmentsStory) View() string {
	mediaList := []clipboard.Media{
		{
			Filename: "architecture_diagram.png",
			Path:     "/tmp/architecture_diagram.png",
			Size:     450210,
		},
		{
			Filename: "ui_mockup.jpeg",
			Path:     "/tmp/ui_mockup.jpeg",
			Size:     210400,
		},
	}

	expanded := s.state == 1
	rows := uirows.AttachmentRows(mediaList, expanded)

	var b strings.Builder
	b.WriteString(bold("Attached Media Preview ("+fmt.Sprintf("%d items", len(mediaList))+"):") + "\n\n")
	for _, r := range rows {
		b.WriteString(r + "\n")
	}
	b.WriteString("\n" + dim("(Press Ctrl+O or Tab / '1'/'2' to toggle collapsed / expanded)") + "\n")
	return b.String()
}

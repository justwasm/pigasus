package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
)

func TestTranscriptReflowOnWindowSizeMsg(t *testing.T) {
	initialWidth := 50
	m := &RootModel{
		viewport: uiState{
			width:  initialWidth,
			height: 30,
		},
		prompt: PromptModel{input: tas.New()},
	}
	m.prompt.input.Focus()

	// 1. Build initial transcript with turn separator + assistant markdown
	rawMarkdown := "This is a long line of markdown text that will definitely wrap across multiple lines when rendered at 50 columns, but when rendered at 100 columns it will wrap to fewer lines or even fit on a single line."
	rendered50 := mdrender.RenderMarkdown(rawMarkdown, initialWidth)
	lines50 := strings.Split(rendered50, "\n")

	m.conversation.response = append(m.conversation.response, transcript.TurnSeparator(initialWidth))
	startOffset := len(m.conversation.response)
	m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{
		Start: startOffset,
		Lines: len(lines50),
		Raw:   rawMarkdown,
	})
	m.conversation.response = append(m.conversation.response, lines50...)

	totalLines50 := len(m.conversation.response)
	if len(lines50) <= 1 {
		t.Fatalf("expected raw text to wrap to >1 lines at 50 cols, got %d", len(lines50))
	}

	// 2. Expand width to 100: SIGWINCH / WindowSizeMsg
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if m.viewport.width != 100 {
		t.Fatalf("expected m.viewport.width == 100, got %d", m.viewport.width)
	}

	// Separator should have resized to 100 columns
	sepPlain := diffaction.StripANSI(m.conversation.response[0])
	if len(sepPlain) < 95 {
		t.Errorf("expected separator width >= 95, got %d (plain: %q)", len(sepPlain), sepPlain)
	}

	// Assistant response should have reflowed into fewer lines
	lines100Count := m.conversation.assistants[0].Lines
	if lines100Count >= len(lines50) {
		t.Errorf("expected reflowed lines at 100 cols (%d) < lines at 50 cols (%d)", lines100Count, len(lines50))
	}
	if len(m.conversation.response) >= totalLines50 {
		t.Errorf("expected total transcript lines (%d) < original (%d)", len(m.conversation.response), totalLines50)
	}

	// 3. Narrow width to 35 cols
	m.Update(tea.WindowSizeMsg{Width: 35, Height: 30})

	if m.viewport.width != 35 {
		t.Fatalf("expected m.viewport.width == 35, got %d", m.viewport.width)
	}
	lines35Count := m.conversation.assistants[0].Lines
	if lines35Count <= len(lines50) {
		t.Errorf("expected reflowed lines at 35 cols (%d) > lines at 50 cols (%d)", lines35Count, len(lines50))
	}

	// 4. Test tool block start offset updates correctly when an assistant reply above it reflows
	// Reset transcript with assistant reply followed by a tool block
	m.conversation.response = nil
	m.conversation.assistants = nil
	m.conversation.blocks = nil

	m.conversation.response = append(m.conversation.response, lines50...)
	m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{
		Start: 0,
		Lines: len(lines50),
		Raw:   rawMarkdown,
	})

	blockStart := len(m.conversation.response)
	step := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(git status)",
		Summary: "Ran (git status)",
	}
	m.conversation.blocks = append(m.conversation.blocks, transcript.Block{
		Start: blockStart,
		Lines: 1,
		Steps: []toolsteps.Step{step},
	})
	m.conversation.response = append(m.conversation.response, "● Ran (git status)")

	// Trigger reflow to 100 cols
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	expectedBlockStart := m.conversation.assistants[0].Lines
	if m.conversation.blocks[0].Start != expectedBlockStart {
		t.Errorf("expected block start shifted to %d after assistant reflow, got %d", expectedBlockStart, m.conversation.blocks[0].Start)
	}
}

func TestStreamingReplyReflowOnWindowSizeMsg(t *testing.T) {
	m := &RootModel{
		viewport: uiState{
			width:  60,
			height: 30,
		},
		conversation: ConversationModel{streaming: true},
		prompt:       PromptModel{input: tas.New()},
	}
	m.prompt.input.Focus()

	rawStream := "Streaming token batch with multiple paragraphs.\n\nHere is a second paragraph that extends far enough to test reflow behavior on resize events."
	rendered60 := mdrender.RenderMarkdown(rawStream, 60)
	lines60 := strings.Split(rendered60, "\n")

	m.conversation.replyStart = 0
	m.conversation.liveReplyBuf = rawStream
	m.conversation.liveTextLines = len(lines60)
	m.conversation.response = append(m.conversation.response, lines60...)

	// Resize to 120
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	if m.viewport.width != 120 {
		t.Fatalf("expected m.viewport.width == 120, got %d", m.viewport.width)
	}
	if m.conversation.liveTextLines >= len(lines60) {
		t.Errorf("expected liveTextLines at 120 cols (%d) < at 60 cols (%d)", m.conversation.liveTextLines, len(lines60))
	}
	if len(m.conversation.response) != m.conversation.liveTextLines {
		t.Errorf("expected len(m.conversation.response) == liveTextLines (%d), got %d", m.conversation.liveTextLines, len(m.conversation.response))
	}
}

func TestNarrowWidthTurnSeparatorPreservation(t *testing.T) {
	m := &RootModel{
		viewport: uiState{
			width:  80,
			height: 30,
		},
		prompt: PromptModel{input: tas.New()},
	}
	m.prompt.input.Focus()

	// Initial separator at 80 cols
	m.conversation.response = append(m.conversation.response, transcript.TurnSeparator(80))

	// Resize to narrow width (e.g. 4 cols)
	m.Update(tea.WindowSizeMsg{Width: 4, Height: 30})
	sep4 := diffaction.StripANSI(m.conversation.response[0])
	if len(sep4) != 4*len("─") { // 4 runes
		t.Fatalf("expected 4-column rule, got len %d: %q", len(sep4), sep4)
	}

	// Now resize back to 80 cols: separator must re-expand to 80 cols, NOT be lost!
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	sep80 := diffaction.StripANSI(m.conversation.response[0])
	if len(sep80) < 60 {
		t.Errorf("expected separator to re-expand to >= 60, got len %d: %q", len(sep80), sep80)
	}
}

func TestLiveStepRunsShiftOnReflow(t *testing.T) {
	m := &RootModel{
		viewport: uiState{
			width:  50,
			height: 30,
		},
		conversation: ConversationModel{streaming: true},
		prompt:       PromptModel{input: tas.New()},
	}
	m.prompt.input.Focus()

	// Assistant response at top (takes multiple lines at 50 cols)
	raw := "First line of assistant reply that wraps across several rows when rendered narrowly."
	rendered50 := mdrender.RenderMarkdown(raw, 50)
	lines50 := strings.Split(rendered50, "\n")
	m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{
		Start: 0,
		Lines: len(lines50),
		Raw:   raw,
	})
	m.conversation.response = append(m.conversation.response, lines50...)

	// In-flight live step run directly below assistant block
	stepRunStart := len(m.conversation.response)
	m.conversation.liveStepRuns = append(m.conversation.liveStepRuns, transcript.LiveStepRun{
		Start: stepRunStart,
		Lines: 1,
		Step: toolsteps.Step{
			Kind:    toolsteps.KindTool,
			Name:    "run_command",
			Title:   "Bash(echo test)",
			Running: true,
		},
	})
	m.conversation.response = append(m.conversation.response, "● Running (echo test)")

	// Resize to 100 cols (assistant reply shrinks in line count)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	newAssistantLines := m.conversation.assistants[0].Lines
	if newAssistantLines >= len(lines50) {
		t.Fatalf("expected assistant lines to shrink at 100 cols")
	}

	// liveStepRuns start MUST shift to newAssistantLines!
	if m.conversation.liveStepRuns[0].Start != newAssistantLines {
		t.Errorf("expected liveStepRuns[0].start to shift to %d, got %d", newAssistantLines, m.conversation.liveStepRuns[0].Start)
	}
}

func TestUserPromptReflowOnWindowSizeMsg(t *testing.T) {
	initialWidth := 40
	m := &RootModel{
		viewport: uiState{
			width:  initialWidth,
			height: 30,
		},
		prompt: PromptModel{input: tas.New()},
	}
	m.prompt.input.Focus()

	rawPrompt := "This is a long user prompt that will wrap across multiple lines at 40 columns and should re-wrap cleanly to fewer lines at 80 columns without truncation."
	promptLines40 := transcript.RenderUserPrompt(rawPrompt, initialWidth)
	if len(promptLines40) <= 1 {
		t.Fatalf("expected prompt to wrap to >1 lines at 40 cols, got %d", len(promptLines40))
	}

	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
		Start: 0,
		Lines: len(promptLines40),
		Raw:   rawPrompt,
	})
	m.conversation.response = append(m.conversation.response, promptLines40...)

	// Followed by an assistant reply
	rawAssistant := "Assistant reply following the user prompt."
	assistLines40 := strings.Split(mdrender.RenderMarkdown(rawAssistant, initialWidth), "\n")
	assistStart40 := len(m.conversation.response)
	m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{
		Start: assistStart40,
		Lines: len(assistLines40),
		Raw:   rawAssistant,
	})
	m.conversation.response = append(m.conversation.response, assistLines40...)

	// Resize to 80 cols
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	if m.viewport.width != 80 {
		t.Fatalf("expected m.viewport.width == 80, got %d", m.viewport.width)
	}

	// User prompt lines should have shrunk at 80 cols
	newPromptLines := m.conversation.userBlocks[0].Lines
	if newPromptLines >= len(promptLines40) {
		t.Errorf("expected user prompt lines at 80 cols (%d) < at 40 cols (%d)", newPromptLines, len(promptLines40))
	}

	// Assistant block offset must have shifted correctly
	if m.conversation.assistants[0].Start != newPromptLines {
		t.Errorf("expected assistant block start shifted to %d, got %d", newPromptLines, m.conversation.assistants[0].Start)
	}

	// Resize down to 25 cols
	m.Update(tea.WindowSizeMsg{Width: 25, Height: 30})
	lines25Count := m.conversation.userBlocks[0].Lines
	if lines25Count <= newPromptLines {
		t.Errorf("expected user prompt lines at 25 cols (%d) > at 80 cols (%d)", lines25Count, newPromptLines)
	}
}

func TestResumeConversation_ClearsStaleStateAndReflows(t *testing.T) {
	dir := storeFixture(t, fixtureID)
	dbPath := filepath.Join(dir, "conversations", fixtureID+".db")
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.viewport.width, m.viewport.height = 80, 24
	m.viewport.altScreen = true

	// Populate stale assistants and userBlocks from a "prior" conversation
	m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{Start: 99, Lines: 10, Raw: "stale assistant"})
	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{Start: 88, Lines: 5, Raw: "stale user"})
	m.conversation.replyStart = 42
	m.conversation.liveReplyBuf = "stale buffer"
	m.conversation.liveTextLines = 3

	// Resume the conversation
	m.resumeConversation(convstore.ConversationMeta{ID: fixtureID, Path: dbPath})

	// Verify stale streaming state was wiped
	if m.conversation.replyStart != -1 {
		t.Errorf("expected replyStart reset to -1, got %d", m.conversation.replyStart)
	}
	if m.conversation.liveReplyBuf != "" {
		t.Errorf("expected liveReplyBuf reset to empty, got %q", m.conversation.liveReplyBuf)
	}
	if m.conversation.liveTextLines != 0 {
		t.Errorf("expected liveTextLines reset to 0, got %d", m.conversation.liveTextLines)
	}

	// Verify old stale blocks were not retained; new blocks must reflect resumed conversation
	for _, a := range m.conversation.assistants {
		if a.Raw == "stale assistant" {
			t.Errorf("found stale assistant in resumed conversation")
		}
	}
	for _, u := range m.conversation.userBlocks {
		if u.Raw == "stale user" {
			t.Errorf("found stale user block in resumed conversation")
		}
	}

	// Verify transcript was reflowed to m.viewport.width (80)
	if len(m.conversation.response) == 0 {
		t.Fatalf("expected non-empty response after resume")
	}
}

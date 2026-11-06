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
	m := &runMode{
		viewport: uiState{
			width:  initialWidth,
			height: 30,
		},
		composer: composerState{input: tas.New()},
	}
	m.composer.input.Focus()

	// 1. Build initial transcript with turn separator + assistant markdown
	rawMarkdown := "This is a long line of markdown text that will definitely wrap across multiple lines when rendered at 50 columns, but when rendered at 100 columns it will wrap to fewer lines or even fit on a single line."
	rendered50 := mdrender.RenderMarkdown(rawMarkdown, initialWidth)
	lines50 := strings.Split(rendered50, "\n")

	m.engine.response = append(m.engine.response, transcript.TurnSeparator(initialWidth))
	startOffset := len(m.engine.response)
	m.engine.assistants = append(m.engine.assistants, transcript.AssistantBlock{
		Start: startOffset,
		Lines: len(lines50),
		Raw:   rawMarkdown,
	})
	m.engine.response = append(m.engine.response, lines50...)

	totalLines50 := len(m.engine.response)
	if len(lines50) <= 1 {
		t.Fatalf("expected raw text to wrap to >1 lines at 50 cols, got %d", len(lines50))
	}

	// 2. Expand width to 100: SIGWINCH / WindowSizeMsg
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if m.viewport.width != 100 {
		t.Fatalf("expected m.viewport.width == 100, got %d", m.viewport.width)
	}

	// Separator should have resized to 100 columns
	sepPlain := diffaction.StripANSI(m.engine.response[0])
	if len(sepPlain) < 95 {
		t.Errorf("expected separator width >= 95, got %d (plain: %q)", len(sepPlain), sepPlain)
	}

	// Assistant response should have reflowed into fewer lines
	lines100Count := m.engine.assistants[0].Lines
	if lines100Count >= len(lines50) {
		t.Errorf("expected reflowed lines at 100 cols (%d) < lines at 50 cols (%d)", lines100Count, len(lines50))
	}
	if len(m.engine.response) >= totalLines50 {
		t.Errorf("expected total transcript lines (%d) < original (%d)", len(m.engine.response), totalLines50)
	}

	// 3. Narrow width to 35 cols
	m.Update(tea.WindowSizeMsg{Width: 35, Height: 30})

	if m.viewport.width != 35 {
		t.Fatalf("expected m.viewport.width == 35, got %d", m.viewport.width)
	}
	lines35Count := m.engine.assistants[0].Lines
	if lines35Count <= len(lines50) {
		t.Errorf("expected reflowed lines at 35 cols (%d) > lines at 50 cols (%d)", lines35Count, len(lines50))
	}

	// 4. Test tool block start offset updates correctly when an assistant reply above it reflows
	// Reset transcript with assistant reply followed by a tool block
	m.engine.response = nil
	m.engine.assistants = nil
	m.engine.blocks = nil

	m.engine.response = append(m.engine.response, lines50...)
	m.engine.assistants = append(m.engine.assistants, transcript.AssistantBlock{
		Start: 0,
		Lines: len(lines50),
		Raw:   rawMarkdown,
	})

	blockStart := len(m.engine.response)
	step := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(git status)",
		Summary: "Ran (git status)",
	}
	m.engine.blocks = append(m.engine.blocks, transcript.Block{
		Start: blockStart,
		Lines: 1,
		Steps: []toolsteps.Step{step},
	})
	m.engine.response = append(m.engine.response, "● Ran (git status)")

	// Trigger reflow to 100 cols
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	expectedBlockStart := m.engine.assistants[0].Lines
	if m.engine.blocks[0].Start != expectedBlockStart {
		t.Errorf("expected block start shifted to %d after assistant reflow, got %d", expectedBlockStart, m.engine.blocks[0].Start)
	}
}

func TestStreamingReplyReflowOnWindowSizeMsg(t *testing.T) {
	m := &runMode{
		viewport: uiState{
			width:  60,
			height: 30,
		},
		engine:   engineState{streaming: true},
		composer: composerState{input: tas.New()},
	}
	m.composer.input.Focus()

	rawStream := "Streaming token batch with multiple paragraphs.\n\nHere is a second paragraph that extends far enough to test reflow behavior on resize events."
	rendered60 := mdrender.RenderMarkdown(rawStream, 60)
	lines60 := strings.Split(rendered60, "\n")

	m.engine.replyStart = 0
	m.engine.liveReplyBuf = rawStream
	m.engine.liveTextLines = len(lines60)
	m.engine.response = append(m.engine.response, lines60...)

	// Resize to 120
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	if m.viewport.width != 120 {
		t.Fatalf("expected m.viewport.width == 120, got %d", m.viewport.width)
	}
	if m.engine.liveTextLines >= len(lines60) {
		t.Errorf("expected liveTextLines at 120 cols (%d) < at 60 cols (%d)", m.engine.liveTextLines, len(lines60))
	}
	if len(m.engine.response) != m.engine.liveTextLines {
		t.Errorf("expected len(m.engine.response) == liveTextLines (%d), got %d", m.engine.liveTextLines, len(m.engine.response))
	}
}

func TestNarrowWidthTurnSeparatorPreservation(t *testing.T) {
	m := &runMode{
		viewport: uiState{
			width:  80,
			height: 30,
		},
		composer: composerState{input: tas.New()},
	}
	m.composer.input.Focus()

	// Initial separator at 80 cols
	m.engine.response = append(m.engine.response, transcript.TurnSeparator(80))

	// Resize to narrow width (e.g. 4 cols)
	m.Update(tea.WindowSizeMsg{Width: 4, Height: 30})
	sep4 := diffaction.StripANSI(m.engine.response[0])
	if len(sep4) != 4*len("─") { // 4 runes
		t.Fatalf("expected 4-column rule, got len %d: %q", len(sep4), sep4)
	}

	// Now resize back to 80 cols: separator must re-expand to 80 cols, NOT be lost!
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	sep80 := diffaction.StripANSI(m.engine.response[0])
	if len(sep80) < 60 {
		t.Errorf("expected separator to re-expand to >= 60, got len %d: %q", len(sep80), sep80)
	}
}

func TestLiveStepRunsShiftOnReflow(t *testing.T) {
	m := &runMode{
		viewport: uiState{
			width:  50,
			height: 30,
		},
		engine:   engineState{streaming: true},
		composer: composerState{input: tas.New()},
	}
	m.composer.input.Focus()

	// Assistant response at top (takes multiple lines at 50 cols)
	raw := "First line of assistant reply that wraps across several rows when rendered narrowly."
	rendered50 := mdrender.RenderMarkdown(raw, 50)
	lines50 := strings.Split(rendered50, "\n")
	m.engine.assistants = append(m.engine.assistants, transcript.AssistantBlock{
		Start: 0,
		Lines: len(lines50),
		Raw:   raw,
	})
	m.engine.response = append(m.engine.response, lines50...)

	// In-flight live step run directly below assistant block
	stepRunStart := len(m.engine.response)
	m.engine.liveStepRuns = append(m.engine.liveStepRuns, transcript.LiveStepRun{
		Start: stepRunStart,
		Lines: 1,
		Step: toolsteps.Step{
			Kind:    toolsteps.KindTool,
			Name:    "run_command",
			Title:   "Bash(echo test)",
			Running: true,
		},
	})
	m.engine.response = append(m.engine.response, "● Running (echo test)")

	// Resize to 100 cols (assistant reply shrinks in line count)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	newAssistantLines := m.engine.assistants[0].Lines
	if newAssistantLines >= len(lines50) {
		t.Fatalf("expected assistant lines to shrink at 100 cols")
	}

	// liveStepRuns start MUST shift to newAssistantLines!
	if m.engine.liveStepRuns[0].Start != newAssistantLines {
		t.Errorf("expected liveStepRuns[0].start to shift to %d, got %d", newAssistantLines, m.engine.liveStepRuns[0].Start)
	}
}

func TestUserPromptReflowOnWindowSizeMsg(t *testing.T) {
	initialWidth := 40
	m := &runMode{
		viewport: uiState{
			width:  initialWidth,
			height: 30,
		},
		composer: composerState{input: tas.New()},
	}
	m.composer.input.Focus()

	rawPrompt := "This is a long user prompt that will wrap across multiple lines at 40 columns and should re-wrap cleanly to fewer lines at 80 columns without truncation."
	promptLines40 := transcript.RenderUserPrompt(rawPrompt, initialWidth)
	if len(promptLines40) <= 1 {
		t.Fatalf("expected prompt to wrap to >1 lines at 40 cols, got %d", len(promptLines40))
	}

	m.engine.userBlocks = append(m.engine.userBlocks, transcript.UserBlock{
		Start: 0,
		Lines: len(promptLines40),
		Raw:   rawPrompt,
	})
	m.engine.response = append(m.engine.response, promptLines40...)

	// Followed by an assistant reply
	rawAssistant := "Assistant reply following the user prompt."
	assistLines40 := strings.Split(mdrender.RenderMarkdown(rawAssistant, initialWidth), "\n")
	assistStart40 := len(m.engine.response)
	m.engine.assistants = append(m.engine.assistants, transcript.AssistantBlock{
		Start: assistStart40,
		Lines: len(assistLines40),
		Raw:   rawAssistant,
	})
	m.engine.response = append(m.engine.response, assistLines40...)

	// Resize to 80 cols
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	if m.viewport.width != 80 {
		t.Fatalf("expected m.viewport.width == 80, got %d", m.viewport.width)
	}

	// User prompt lines should have shrunk at 80 cols
	newPromptLines := m.engine.userBlocks[0].Lines
	if newPromptLines >= len(promptLines40) {
		t.Errorf("expected user prompt lines at 80 cols (%d) < at 40 cols (%d)", newPromptLines, len(promptLines40))
	}

	// Assistant block offset must have shifted correctly
	if m.engine.assistants[0].Start != newPromptLines {
		t.Errorf("expected assistant block start shifted to %d, got %d", newPromptLines, m.engine.assistants[0].Start)
	}

	// Resize down to 25 cols
	m.Update(tea.WindowSizeMsg{Width: 25, Height: 30})
	lines25Count := m.engine.userBlocks[0].Lines
	if lines25Count <= newPromptLines {
		t.Errorf("expected user prompt lines at 25 cols (%d) > at 80 cols (%d)", lines25Count, newPromptLines)
	}
}

func TestResumeConversation_ClearsStaleStateAndReflows(t *testing.T) {
	dir := storeFixture(t, fixtureID)
	dbPath := filepath.Join(dir, "conversations", fixtureID+".db")
	m := newRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.viewport.width, m.viewport.height = 80, 24
	m.viewport.altScreen = true

	// Populate stale assistants and userBlocks from a "prior" conversation
	m.engine.assistants = append(m.engine.assistants, transcript.AssistantBlock{Start: 99, Lines: 10, Raw: "stale assistant"})
	m.engine.userBlocks = append(m.engine.userBlocks, transcript.UserBlock{Start: 88, Lines: 5, Raw: "stale user"})
	m.engine.replyStart = 42
	m.engine.liveReplyBuf = "stale buffer"
	m.engine.liveTextLines = 3

	// Resume the conversation
	m.resumeConversation(convstore.ConversationMeta{ID: fixtureID, Path: dbPath})

	// Verify stale streaming state was wiped
	if m.engine.replyStart != -1 {
		t.Errorf("expected replyStart reset to -1, got %d", m.engine.replyStart)
	}
	if m.engine.liveReplyBuf != "" {
		t.Errorf("expected liveReplyBuf reset to empty, got %q", m.engine.liveReplyBuf)
	}
	if m.engine.liveTextLines != 0 {
		t.Errorf("expected liveTextLines reset to 0, got %d", m.engine.liveTextLines)
	}

	// Verify old stale blocks were not retained; new blocks must reflect resumed conversation
	for _, a := range m.engine.assistants {
		if a.Raw == "stale assistant" {
			t.Errorf("found stale assistant in resumed conversation")
		}
	}
	for _, u := range m.engine.userBlocks {
		if u.Raw == "stale user" {
			t.Errorf("found stale user block in resumed conversation")
		}
	}

	// Verify transcript was reflowed to m.viewport.width (80)
	if len(m.engine.response) == 0 {
		t.Fatalf("expected non-empty response after resume")
	}
}

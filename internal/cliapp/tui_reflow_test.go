package cliapp

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
		width:  initialWidth,
		height: 30,
		input:  tas.New(),
	}
	m.input.Focus()

	// 1. Build initial transcript with turn separator + assistant markdown
	rawMarkdown := "This is a long line of markdown text that will definitely wrap across multiple lines when rendered at 50 columns, but when rendered at 100 columns it will wrap to fewer lines or even fit on a single line."
	rendered50 := mdrender.RenderMarkdown(rawMarkdown, initialWidth)
	lines50 := strings.Split(rendered50, "\n")

	m.response = append(m.response, transcript.TurnSeparator(initialWidth))
	startOffset := len(m.response)
	m.assistants = append(m.assistants, transcript.AssistantBlock{
		Start: startOffset,
		Lines: len(lines50),
		Raw:   rawMarkdown,
	})
	m.response = append(m.response, lines50...)

	totalLines50 := len(m.response)
	if len(lines50) <= 1 {
		t.Fatalf("expected raw text to wrap to >1 lines at 50 cols, got %d", len(lines50))
	}

	// 2. Expand width to 100: SIGWINCH / WindowSizeMsg
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if m.width != 100 {
		t.Fatalf("expected m.width == 100, got %d", m.width)
	}

	// Separator should have resized to 100 columns
	sepPlain := diffaction.StripANSI(m.response[0])
	if len(sepPlain) < 95 {
		t.Errorf("expected separator width >= 95, got %d (plain: %q)", len(sepPlain), sepPlain)
	}

	// Assistant response should have reflowed into fewer lines
	lines100Count := m.assistants[0].Lines
	if lines100Count >= len(lines50) {
		t.Errorf("expected reflowed lines at 100 cols (%d) < lines at 50 cols (%d)", lines100Count, len(lines50))
	}
	if len(m.response) >= totalLines50 {
		t.Errorf("expected total transcript lines (%d) < original (%d)", len(m.response), totalLines50)
	}

	// 3. Narrow width to 35 cols
	m.Update(tea.WindowSizeMsg{Width: 35, Height: 30})

	if m.width != 35 {
		t.Fatalf("expected m.width == 35, got %d", m.width)
	}
	lines35Count := m.assistants[0].Lines
	if lines35Count <= len(lines50) {
		t.Errorf("expected reflowed lines at 35 cols (%d) > lines at 50 cols (%d)", lines35Count, len(lines50))
	}

	// 4. Test tool block start offset updates correctly when an assistant reply above it reflows
	// Reset transcript with assistant reply followed by a tool block
	m.response = nil
	m.assistants = nil
	m.blocks = nil

	m.response = append(m.response, lines50...)
	m.assistants = append(m.assistants, transcript.AssistantBlock{
		Start: 0,
		Lines: len(lines50),
		Raw:   rawMarkdown,
	})

	blockStart := len(m.response)
	step := toolsteps.Step{
		Kind:    toolsteps.KindTool,
		Name:    "run_command",
		Title:   "Bash(git status)",
		Summary: "Ran (git status)",
	}
	m.blocks = append(m.blocks, transcript.Block{
		Start: blockStart,
		Lines: 1,
		Steps: []toolsteps.Step{step},
	})
	m.response = append(m.response, "● Ran (git status)")

	// Trigger reflow to 100 cols
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	expectedBlockStart := m.assistants[0].Lines
	if m.blocks[0].Start != expectedBlockStart {
		t.Errorf("expected block start shifted to %d after assistant reflow, got %d", expectedBlockStart, m.blocks[0].Start)
	}
}

func TestStreamingReplyReflowOnWindowSizeMsg(t *testing.T) {
	m := &runMode{
		width:     60,
		height:    30,
		streaming: true,
		input:     tas.New(),
	}
	m.input.Focus()

	rawStream := "Streaming token batch with multiple paragraphs.\n\nHere is a second paragraph that extends far enough to test reflow behavior on resize events."
	rendered60 := mdrender.RenderMarkdown(rawStream, 60)
	lines60 := strings.Split(rendered60, "\n")

	m.replyStart = 0
	m.liveReplyBuf = rawStream
	m.liveTextLines = len(lines60)
	m.response = append(m.response, lines60...)

	// Resize to 120
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	if m.width != 120 {
		t.Fatalf("expected m.width == 120, got %d", m.width)
	}
	if m.liveTextLines >= len(lines60) {
		t.Errorf("expected liveTextLines at 120 cols (%d) < at 60 cols (%d)", m.liveTextLines, len(lines60))
	}
	if len(m.response) != m.liveTextLines {
		t.Errorf("expected len(m.response) == liveTextLines (%d), got %d", m.liveTextLines, len(m.response))
	}
}

func TestNarrowWidthTurnSeparatorPreservation(t *testing.T) {
	m := &runMode{
		width:  80,
		height: 30,
		input:  tas.New(),
	}
	m.input.Focus()

	// Initial separator at 80 cols
	m.response = append(m.response, transcript.TurnSeparator(80))

	// Resize to narrow width (e.g. 4 cols)
	m.Update(tea.WindowSizeMsg{Width: 4, Height: 30})
	sep4 := diffaction.StripANSI(m.response[0])
	if len(sep4) != 4*len("─") { // 4 runes
		t.Fatalf("expected 4-column rule, got len %d: %q", len(sep4), sep4)
	}

	// Now resize back to 80 cols: separator must re-expand to 80 cols, NOT be lost!
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	sep80 := diffaction.StripANSI(m.response[0])
	if len(sep80) < 60 {
		t.Errorf("expected separator to re-expand to >= 60, got len %d: %q", len(sep80), sep80)
	}
}

func TestLiveStepRunsShiftOnReflow(t *testing.T) {
	m := &runMode{
		width:     50,
		height:    30,
		streaming: true,
		input:     tas.New(),
	}
	m.input.Focus()

	// Assistant response at top (takes multiple lines at 50 cols)
	raw := "First line of assistant reply that wraps across several rows when rendered narrowly."
	rendered50 := mdrender.RenderMarkdown(raw, 50)
	lines50 := strings.Split(rendered50, "\n")
	m.assistants = append(m.assistants, transcript.AssistantBlock{
		Start: 0,
		Lines: len(lines50),
		Raw:   raw,
	})
	m.response = append(m.response, lines50...)

	// In-flight live step run directly below assistant block
	stepRunStart := len(m.response)
	m.liveStepRuns = append(m.liveStepRuns, transcript.LiveStepRun{
		Start: stepRunStart,
		Lines: 1,
		Step: toolsteps.Step{
			Kind:    toolsteps.KindTool,
			Name:    "run_command",
			Title:   "Bash(echo test)",
			Running: true,
		},
	})
	m.response = append(m.response, "● Running (echo test)")

	// Resize to 100 cols (assistant reply shrinks in line count)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	newAssistantLines := m.assistants[0].Lines
	if newAssistantLines >= len(lines50) {
		t.Fatalf("expected assistant lines to shrink at 100 cols")
	}

	// liveStepRuns start MUST shift to newAssistantLines!
	if m.liveStepRuns[0].Start != newAssistantLines {
		t.Errorf("expected liveStepRuns[0].start to shift to %d, got %d", newAssistantLines, m.liveStepRuns[0].Start)
	}
}

func TestUserPromptReflowOnWindowSizeMsg(t *testing.T) {
	initialWidth := 40
	m := &runMode{
		width:  initialWidth,
		height: 30,
		input:  tas.New(),
	}
	m.input.Focus()

	rawPrompt := "This is a long user prompt that will wrap across multiple lines at 40 columns and should re-wrap cleanly to fewer lines at 80 columns without truncation."
	promptLines40 := transcript.RenderUserPrompt(rawPrompt, initialWidth)
	if len(promptLines40) <= 1 {
		t.Fatalf("expected prompt to wrap to >1 lines at 40 cols, got %d", len(promptLines40))
	}

	m.userBlocks = append(m.userBlocks, transcript.UserBlock{
		Start: 0,
		Lines: len(promptLines40),
		Raw:   rawPrompt,
	})
	m.response = append(m.response, promptLines40...)

	// Followed by an assistant reply
	rawAssistant := "Assistant reply following the user prompt."
	assistLines40 := strings.Split(mdrender.RenderMarkdown(rawAssistant, initialWidth), "\n")
	assistStart40 := len(m.response)
	m.assistants = append(m.assistants, transcript.AssistantBlock{
		Start: assistStart40,
		Lines: len(assistLines40),
		Raw:   rawAssistant,
	})
	m.response = append(m.response, assistLines40...)

	// Resize to 80 cols
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	if m.width != 80 {
		t.Fatalf("expected m.width == 80, got %d", m.width)
	}

	// User prompt lines should have shrunk at 80 cols
	newPromptLines := m.userBlocks[0].Lines
	if newPromptLines >= len(promptLines40) {
		t.Errorf("expected user prompt lines at 80 cols (%d) < at 40 cols (%d)", newPromptLines, len(promptLines40))
	}

	// Assistant block offset must have shifted correctly
	if m.assistants[0].Start != newPromptLines {
		t.Errorf("expected assistant block start shifted to %d, got %d", newPromptLines, m.assistants[0].Start)
	}

	// Resize down to 25 cols
	m.Update(tea.WindowSizeMsg{Width: 25, Height: 30})
	lines25Count := m.userBlocks[0].Lines
	if lines25Count <= newPromptLines {
		t.Errorf("expected user prompt lines at 25 cols (%d) > at 80 cols (%d)", lines25Count, newPromptLines)
	}
}

func TestResumeConversation_ClearsStaleStateAndReflows(t *testing.T) {
	dir := storeFixture(t, fixtureID)
	dbPath := filepath.Join(dir, "conversations", fixtureID+".db")
	m := newRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.width, m.height = 80, 24
	m.altScreen = true

	// Populate stale assistants and userBlocks from a "prior" conversation
	m.assistants = append(m.assistants, transcript.AssistantBlock{Start: 99, Lines: 10, Raw: "stale assistant"})
	m.userBlocks = append(m.userBlocks, transcript.UserBlock{Start: 88, Lines: 5, Raw: "stale user"})
	m.replyStart = 42
	m.liveReplyBuf = "stale buffer"
	m.liveTextLines = 3

	// Resume the conversation
	m.resumeConversation(convstore.ConversationMeta{ID: fixtureID, Path: dbPath})

	// Verify stale streaming state was wiped
	if m.replyStart != -1 {
		t.Errorf("expected replyStart reset to -1, got %d", m.replyStart)
	}
	if m.liveReplyBuf != "" {
		t.Errorf("expected liveReplyBuf reset to empty, got %q", m.liveReplyBuf)
	}
	if m.liveTextLines != 0 {
		t.Errorf("expected liveTextLines reset to 0, got %d", m.liveTextLines)
	}

	// Verify old stale blocks were not retained; new blocks must reflect resumed conversation
	for _, a := range m.assistants {
		if a.Raw == "stale assistant" {
			t.Errorf("found stale assistant in resumed conversation")
		}
	}
	for _, u := range m.userBlocks {
		if u.Raw == "stale user" {
			t.Errorf("found stale user block in resumed conversation")
		}
	}

	// Verify transcript was reflowed to m.width (80)
	if len(m.response) == 0 {
		t.Fatalf("expected non-empty response after resume")
	}
}

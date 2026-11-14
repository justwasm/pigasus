package storybook

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/modelargpopup"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
)

// -----------------------------------------------------------------------------
// 1. ModelArgPopup Story
// -----------------------------------------------------------------------------

type modelArgPopupStory struct {
	width, height int
	stateIdx      int
	buf           string
	panel         *slashpicker.Panel
}

func newModelArgPopupStory() Story {
	return Story{
		ID:          "popups-model-arg",
		Name:        "Model Argument Popup (/model <query>)",
		Category:    CategoryPopups,
		Description: "30-column model completion popup triggered by /model argument matching against model catalogue.",
		UpstreamSym: "jetski/cli/popup.ModelArgPopup",
		States: []State{
			{ID: "all-models", Name: "All Models Catalogue", Description: "Full catalogue popup with labels and catalogue IDs."},
			{ID: "filtered-claude", Name: "Filtered: 'claude'", Description: "Subsequence filtered models matching 'claude'."},
		},
		Factory: func() StoryInstance {
			s := &modelArgPopupStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *modelArgPopupStory) rebuild() {
	s.panel = slashpicker.New(slashpicker.Seams{
		AltScreen: func() bool { return true },
		Width:     func() int { return s.width },
		Buffer:    func() string { return s.buf },
		SetBuffer: func(newBuf string) { s.buf = newBuf },
	})
	s.panel.SetModelCatalogueSource(func() []slashpicker.ModelRow {
		return modelargpopup.Rows()
	})

	if s.stateIdx == 0 {
		s.buf = "/model "
	} else {
		s.buf = "/model claude"
	}
	s.panel.Update([]rune(s.buf))
}

func (s *modelArgPopupStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *modelArgPopupStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *modelArgPopupStory) View() string {
	if s.panel == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(bold("Model Completion Popup Simulation:") + "\n")
	b.WriteString("  Input Buffer: " + blue(s.buf) + "\n\n")
	b.WriteString(s.panel.Rows())
	return b.String()
}

func (s *modelArgPopupStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	switch msg.Key().Code {
	case tea.KeyUp, 'k':
		s.panel.HandleKey(tea.Key{Code: tea.KeyUp})
		return nil, true
	case tea.KeyDown, 'j':
		s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
		return nil, true
	case tea.KeyBackspace:
		if len(s.buf) > 7 {
			s.buf = s.buf[:len(s.buf)-1]
			s.panel.Update([]rune(s.buf))
			return nil, true
		}
	default:
		if msg.Key().Text != "" {
			s.buf += msg.Key().Text
			s.panel.Update([]rune(s.buf))
			return nil, true
		}
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 2. BatchSteps Story
// -----------------------------------------------------------------------------

type batchStepsStory struct {
	width, height int
	stateIdx      int
	steps         []toolsteps.Step
}

func newBatchStepsStory() Story {
	return Story{
		ID:          "toolsteps-batch",
		Name:        "Parallel Batch Tool Steps (BatchStep)",
		Category:    CategoryToolSteps,
		Description: "Multi-tool parallel execution block grouping concurrent function calls within one assistant turn.",
		UpstreamSym: "jetski/cortex/step.BatchStep",
		States: []State{
			{ID: "batch-expanded", Name: "Batch Expanded", Description: "Detailed multi-step cards showing command arguments and status."},
			{ID: "batch-collapsed", Name: "Batch Collapsed Summary", Description: "Compact summary badge showing count of batched executions."},
		},
		Factory: func() StoryInstance {
			s := &batchStepsStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *batchStepsStory) rebuild() {
	s.steps = []toolsteps.Step{
		{
			Kind:    toolsteps.KindTool,
			Name:    "read_file",
			Title:   "Read(internal/ui/storybook/types.go)",
			Summary: "Read 1 file (types.go)",
			Detail:  "package storybook\n...",
			Result:  "package storybook\n...",
			Batch:   1,
		},
		{
			Kind:    toolsteps.KindTool,
			Name:    "read_file",
			Title:   "Read(internal/ui/storybook/catalog.go)",
			Summary: "Read 1 file (catalog.go)",
			Detail:  "package storybook\n...",
			Result:  "package storybook\n...",
			Batch:   1,
		},
		{
			Kind:    toolsteps.KindTool,
			Name:    "run_command",
			Title:   "Bash(go test -v ./internal/ui/storybook/...)",
			Summary: "Ran (go test -v ./internal/ui/storybook/...)",
			Detail:  "PASS\nok github.com/justwasm/pigasus/internal/ui/storybook 0.05s",
			Result:  "PASS\nok github.com/justwasm/pigasus/internal/ui/storybook 0.05s",
			Batch:   1,
		},
	}
}

func (s *batchStepsStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *batchStepsStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *batchStepsStory) View() string {
	batched := toolsteps.BatchSteps(s.steps, 1)
	expanded := s.stateIdx == 0
	lines := toolsteps.BlockLines(batched, expanded, s.width)
	return strings.Join(lines, "\n")
}

func (s *batchStepsStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if isCtrlO(msg) {
		if s.stateIdx == 0 {
			s.stateIdx = 1
		} else {
			s.stateIdx = 0
		}
		return nil, true
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 3. ErrorRow Story
// -----------------------------------------------------------------------------

type errorRowStory struct {
	width, height int
	stateIdx      int
}

func newErrorRowStory() Story {
	return Story{
		ID:          "toolsteps-error-row",
		Name:        "Tool Error Row & Badge (ErrorRow)",
		Category:    CategoryToolSteps,
		Description: "Diagnostic error rows and failure callouts rendered in the transcript.",
		UpstreamSym: "jetski/cortex/step.ErrorRow",
		States: []State{
			{ID: "command-failure", Name: "Command Execution Failure", Description: "Non-zero exit code error line formatted for transcript."},
			{ID: "permission-denied", Name: "Security Permission Block", Description: "Security boundary rejection error message."},
		},
		Factory: func() StoryInstance {
			return &errorRowStory{width: 80, height: 24}
		},
	}
}

func (s *errorRowStory) SetState(idx int) {
	s.stateIdx = idx
}

func (s *errorRowStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *errorRowStory) View() string {
	var errText string
	if s.stateIdx == 0 {
		errText = "run_command: exit status 1: undefined: NewSessionConfig in package auth"
	} else {
		errText = "policy violation: write_file outside of active workspace is denied"
	}

	row := toolsteps.ErrorRow(errText)
	var b strings.Builder
	b.WriteString(bold("Transcript Error Row Presentation:") + "\n")
	b.WriteString(dim("────────────────────────────────────────────────────────────") + "\n\n")
	b.WriteString("  " + red(row) + "\n\n")
	b.WriteString(dim("Raw representation: ") + row + "\n")
	return b.String()
}

func (s *errorRowStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// -----------------------------------------------------------------------------
// 4. Notifications Story
// -----------------------------------------------------------------------------

type notificationsStory struct {
	width, height int
	stateIdx      int
}

func newNotificationsStory() Story {
	return Story{
		ID:          "status-notifications",
		Name:        "Task Notifications (Toast Rows)",
		Category:    CategoryStatus,
		Description: "Transcript notification rows for background task events, completions, and alerts.",
		UpstreamSym: "jetski/cli/notice.Notifications",
		States: []State{
			{ID: "timeout-warning", Name: "Task Timeout Notification", Description: "High-priority warning banner when background task times out."},
			{ID: "task-finished", Name: "Task Finished Notification", Description: "Success banner when background task completes with zero exit code."},
		},
		Factory: func() StoryInstance {
			return &notificationsStory{width: 80, height: 24}
		},
	}
}

func (s *notificationsStory) SetState(idx int) {
	s.stateIdx = idx
}

func (s *notificationsStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *notificationsStory) View() string {
	var notif shell.TaskNotification
	if s.stateIdx == 0 {
		notif = shell.TaskNotification{
			TaskID:   "session-42/task-1",
			Title:    "Notification timeout reached",
			Body:     "Background task 'npm run build:prod' exceeded execution wait threshold of 30s.",
			Priority: "MESSAGE_PRIORITY_HIGH",
		}
	} else {
		notif = shell.TaskNotification{
			TaskID: "session-42/task-2",
			Title:  "Background task completed",
			Body:   "Container build finished successfully in 14.2s (Exit code 0).",
		}
	}

	lines := rows.NotificationRows(notif)
	return strings.Join(lines, "\n")
}

func (s *notificationsStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// -----------------------------------------------------------------------------
// 5. TranscriptDocument Story
// -----------------------------------------------------------------------------

type transcriptDocumentStory struct {
	width, height int
	stateIdx      int
	expanded      bool
}

func newTranscriptDocumentStory() Story {
	return Story{
		ID:          "foundations-transcript-flow",
		Name:        "Transcript Document Flow",
		Category:    CategoryFoundations,
		Description: "Multi-turn conversation transcript layout combining user prompt, separator, tool steps, and markdown reply. Press Ctrl+O to collapse/expand trajectory.",
		UpstreamSym: "jetski/cli/transcript.Document",
		States: []State{
			{ID: "standard-turn", Name: "Standard Assistant Turn", Description: "User prompt bubble, divider, tool execution step, and markdown response."},
			{ID: "slash-command-turn", Name: "Slash Command Turn", Description: "Highlighted warning user prompt for slash command with immediate reply."},
		},
		Factory: func() StoryInstance {
			return &transcriptDocumentStory{width: 80, height: 24, expanded: true}
		},
	}
}

func (s *transcriptDocumentStory) SetState(idx int) {
	s.stateIdx = idx
	s.expanded = true
}

func (s *transcriptDocumentStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *transcriptDocumentStory) View() string {
	var b strings.Builder

	if s.stateIdx == 0 {
		// User prompt
		userPrompt := "Verify architecture tier compliance and run unit tests for Storybook."
		userLines := transcript.RenderUserPrompt(userPrompt, s.width)
		for _, l := range userLines {
			b.WriteString(l + "\n")
		}
		b.WriteString(transcript.TurnSeparator(s.width) + "\n\n")

		// Tool step
		step := toolsteps.Step{
			Kind:    toolsteps.KindTool,
			Name:    "run_command",
			Title:   "Bash(make arch && make quick)",
			Summary: "Ran (make arch && make quick)",
			Detail:  "321 production packages checked — all rules hold.",
			Result:  "321 production packages checked — all rules hold.",
		}
		stepLines := toolsteps.BlockLines([]toolsteps.Step{step}, s.expanded, s.width)
		for _, l := range stepLines {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")

		// Assistant reply
		b.WriteString(blue("All architecture layering checks have passed with 0 violations.") + "\n")
		b.WriteString("The test suite completed in 1.1s across all core and UI packages.\n")
	} else {
		// Slash command prompt
		slashPrompt := "/model claude-sonnet-4-6"
		userLines := transcript.RenderUserPrompt(slashPrompt, s.width)
		for _, l := range userLines {
			b.WriteString(l + "\n")
		}
		b.WriteString(transcript.TurnSeparator(s.width) + "\n\n")

		b.WriteString(green("Model set to Claude Sonnet 4.6 (Thinking)") + "\n")
	}

	return b.String()
}

func (s *transcriptDocumentStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if isCtrlO(msg) {
		s.expanded = !s.expanded
		return nil, true
	}
	return nil, false
}

// RegisterAdvancedStepsStories mounts all model autocomplete, batch tool steps, notifications, and transcript flow stories into the catalogue.
func RegisterAdvancedStepsStories(c *Catalog) {
	c.Register(newModelArgPopupStory())
	c.Register(newBatchStepsStory())
	c.Register(newErrorRowStory())
	c.Register(newNotificationsStory())
	c.Register(newTranscriptDocumentStory())
}

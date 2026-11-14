package storybook

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

// RegisterToolStepsStories registers tool execution, thinking, and file diff stories.
func RegisterToolStepsStories(c *Catalog) {
	// 1. BashStep
	c.Register(Story{
		ID:          "toolsteps-bash-step",
		Name:        "BashStep (Command Execution)",
		Category:    CategoryToolSteps,
		Description: "Tool execution cards for shell commands: running, zero exit, and nonzero failure. Press Ctrl+O to expand/collapse.",
		UpstreamSym: "jetski/cortex/step.BashStep",
		States: []State{
			{ID: "running", Name: "Running (Spinner)", Description: "Command executing with active progress spinner"},
			{ID: "success", Name: "Success (Exit 0)", Description: "Completed command with stdout output"},
			{ID: "failure", Name: "Failure (Exit 1)", Description: "Nonzero exit status with highlighted stderr"},
			{ID: "overflow-tail", Name: "Dynamic Tail Overflow (<output +70 lines>)", Description: "Dynamic folding: head overflow marker (<output +70 lines>) with tail output window and (ctrl+o to collapse)"},
		},
		Factory: func() StoryInstance {
			return &bashStepStory{state: 0, width: 80, expanded: true}
		},
	})

	// 2. ThoughtStep
	c.Register(Story{
		ID:          "toolsteps-thought-step",
		Name:        "ThoughtStep (Model Reasoning)",
		Category:    CategoryToolSteps,
		Description: "Thinking blocks with elapsed duration, live streaming snippet, and expanded reasoning. Press Ctrl+O to toggle.",
		UpstreamSym: "jetski/cortex/step.ThoughtStep",
		States: []State{
			{ID: "live-snippet", Name: "Live Streaming Thought", Description: "Live reasoning snippet while token generation is in flight"},
			{ID: "collapsed-thought", Name: "Collapsed Thought", Description: "Compact bullet showing thinking duration and token count (e.g. '▸ Thought for 4.2s, 280 tokens')"},
			{ID: "expanded-thought", Name: "Expanded Thought", Description: "Full multi-paragraph reasoning view indented cleanly with 2 spaces"},
		},
		Factory: func() StoryInstance {
			return &thoughtStepStory{state: 0, width: 80}
		},
	})

	// 3. FileDiffStep
	c.Register(Story{
		ID:          "toolsteps-file-diff",
		Name:        "FileDiffStep (File Operations)",
		Category:    CategoryToolSteps,
		Description: "Read file, write file, and patch diff tool execution representations.",
		UpstreamSym: "jetski/cortex/step.FileStep",
		States: []State{
			{ID: "create-file", Name: "Write New File", Description: "New file creation preview with additions"},
			{ID: "edit-file-diff", Name: "Edit File Diff", Description: "Unified diff hunk with additions and deletions"},
		},
		Factory: func() StoryInstance {
			return &fileDiffStepStory{state: 0, width: 80, expanded: true}
		},
	})

	// 4. TrajectoryToggle (ctrl+o expand / collapse showcase)
	c.Register(Story{
		ID:          "toolsteps-trajectory-toggle",
		Name:        "Trajectory Toggle (Ctrl+O Collapse/Expand)",
		Category:    CategoryToolSteps,
		Description: "Interactive global Ctrl+O shortcut toggling between collapsed tool summary and expanded trajectory output.",
		UpstreamSym: "jetski/cli.ToggleTrajectory",
		States: []State{
			{ID: "collapsed-hint", Name: "Collapsed (ctrl+o to expand)", Description: "Compact tool summary bullet with '(ctrl+o to expand)' hint"},
			{ID: "expanded-hint", Name: "Expanded (ctrl+o to collapse)", Description: "Full stdout execution lines with '(ctrl+o to collapse)' hint"},
		},
		Factory: func() StoryInstance {
			return &trajectoryToggleStory{state: 0, width: 80}
		},
	})
}

// -----------------------------------------------------------------------------
// BashStep Story
// -----------------------------------------------------------------------------

type bashStepStory struct {
	state    int
	width    int
	h        int
	expanded bool
}

func (s *bashStepStory) SetState(idx int) {
	s.state = idx
	s.expanded = true
}

func (s *bashStepStory) SetSize(w, h int) {
	s.width, s.h = w, h
}

func (s *bashStepStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if isCtrlO(key) {
		s.expanded = !s.expanded
		return nil, true
	}
	return nil, false
}

func (s *bashStepStory) View() string {
	w := s.width - 4
	if w < 20 {
		w = 60
	}

	var step toolsteps.Step
	args, _ := json.Marshal(map[string]any{
		"CommandLine": "go test -v ./internal/tui -run TestCursor",
	})

	switch s.state {
	case 0:
		// Running
		step = toolsteps.NewRunningStep("run_command", args)
	case 1:
		// Success
		step = toolsteps.NewToolStep("run_command", args, "=== RUN TestCursor_VisibleDuringStreamingAndThinking\n--- PASS: TestCursor_VisibleDuringStreamingAndThinking (0.01s)\nPASS\nok github.com/justwasm/pigasus/internal/tui 1.450s", false)
	case 2:
		// Failure
		step = toolsteps.NewToolStep("run_command", args, "=== RUN TestCursor\n--- FAIL: TestCursor (0.02s)\npanic: unexpected nil cursor in input box\nFAIL github.com/justwasm/pigasus/internal/tui 0.050s", true)
	case 3:
		// Dynamic overflow with tail window (make build && make quick && make arch)
		args, _ = json.Marshal(map[string]any{
			"CommandLine": "make build && make quick && make arch",
		})
		var outLines []string
		for i := 1; i <= 69; i++ {
			outLines = append(outLines, fmt.Sprintf("building package %d/70...", i))
		}
		for i := 1; i <= 10; i++ {
			outLines = append(outLines, fmt.Sprintf("linking binary step %d/10...", i))
		}
		outLines = append(outLines,
			"=== RUN   TestIsEngineHost",
			"--- PASS: TestIsEngineHost (0.00s)",
			"=== RUN   TestIsEntryHost",
			"--- PASS: TestIsEntryHost (0.00s)",
			"=== RUN   TestIsPublic",
			"--- PASS: TestIsPublic (0.00s)",
			"=== RUN   TestFindModuleRoot",
			"--- PASS: TestFindModuleRoot (0.00s)",
			"PASS",
			"ok      github.com/justwasm/pigasus/internal/core/architecture    0.837s",
		)
		step = toolsteps.NewToolStep("run_command", args, strings.Join(outLines, "\n"), false)
	}

	lines := toolsteps.SingleLines(step, s.expanded, w)
	var b strings.Builder
	b.WriteString(bold("Tool Execution Step Rendering (Press Ctrl+O to toggle expansion):") + "\n\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// -----------------------------------------------------------------------------
// ThoughtStep Story
// -----------------------------------------------------------------------------

type thoughtStepStory struct {
	state    int
	width    int
	h        int
	expanded bool
}

func (s *thoughtStepStory) SetState(idx int) {
	s.state = idx
	s.expanded = (idx == 2)
}

func (s *thoughtStepStory) SetSize(w, h int) {
	s.width, s.h = w, h
}

func (s *thoughtStepStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if isCtrlO(key) {
		if s.state == 1 {
			s.state = 2
			s.expanded = true
		} else if s.state == 2 {
			s.state = 1
			s.expanded = false
		}
		return nil, true
	}
	return nil, false
}

func (s *thoughtStepStory) View() string {
	w := s.width - 4
	if w < 20 {
		w = 60
	}

	var b strings.Builder
	switch s.state {
	case 0:
		b.WriteString(bold("Live Thinking Snippet (Streaming Turn):") + "\n\n")
		b.WriteString(toolsteps.ThinkingLiveLine(3400 * time.Millisecond))
		b.WriteString("\n")
		b.WriteString(toolsteps.ThinkingLiveSnippet("Analyzing cursor positioning in bubbles/v2/textarea and East Asian runes..."))
		b.WriteString("\n")
	case 1:
		b.WriteString(bold("Collapsed Thinking Step (Press Ctrl+O to expand):") + "\n\n")
		steps := toolsteps.AppendThoughtStep(nil, "The user reported that the cursor drifts to the spinner...", 4200*time.Millisecond, 280)
		lines := toolsteps.BlockLines(steps, false, w)
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	case 2:
		b.WriteString(bold("Expanded Thinking Step (Press Ctrl+O to collapse):") + "\n\n")
		fullThought := "1. Inspect internal/tui/tui_view.go.\n2. In shouldShowInputCursor, remove the streaming/thinking suppression.\n3. Recalculate inputCursorX for CJK double-width rune alignment."
		steps := toolsteps.AppendThoughtStep(nil, fullThought, 4200*time.Millisecond, 280)
		lines := toolsteps.BlockLines(steps, true, w)
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// -----------------------------------------------------------------------------
// FileDiffStep Story
// -----------------------------------------------------------------------------

type fileDiffStepStory struct {
	state    int
	width    int
	h        int
	expanded bool
}

func (s *fileDiffStepStory) SetState(idx int) {
	s.state = idx
	s.expanded = true
}

func (s *fileDiffStepStory) SetSize(w, h int) {
	s.width, s.h = w, h
}

func (s *fileDiffStepStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if isCtrlO(key) {
		s.expanded = !s.expanded
		return nil, true
	}
	return nil, false
}

func (s *fileDiffStepStory) View() string {
	w := s.width - 4
	if w < 20 {
		w = 60
	}

	var step toolsteps.Step
	if s.state == 0 {
		args, _ := json.Marshal(map[string]any{
			"Path": "/Users/gear/GitHub/openagy/internal/ui/storybook/types.go",
		})
		diffText := "package storybook\n\ntype Story struct {\n    ID string\n    Name string\n}"
		step = toolsteps.NewToolStep("write_file", args, diffText, false)
	} else {
		args, _ := json.Marshal(map[string]any{
			"Path": "/Users/gear/GitHub/openagy/internal/tui/tui_view.go",
		})
		diffText := "@@ -179,6 +179,0 @@\n-	if m.conversation.streaming || m.conversation.thinking {\n-		return false\n-	}\n-	if m.session.store.BashRunning() {\n-		return false\n-	}"
		step = toolsteps.NewToolStep("edit_file", args, diffText, false)
	}

	lines := toolsteps.SingleLines(step, s.expanded, w)
	var b strings.Builder
	b.WriteString(bold("File Diff Step Rendering (Press Ctrl+O to toggle expansion):") + "\n\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// -----------------------------------------------------------------------------
// TrajectoryToggle Story (Ctrl+O Showcase)
// -----------------------------------------------------------------------------

type trajectoryToggleStory struct {
	state int
	width int
	h     int
}

func (s *trajectoryToggleStory) SetState(idx int) { s.state = idx }
func (s *trajectoryToggleStory) SetSize(w, h int) { s.width, s.h = w, h }

func (s *trajectoryToggleStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
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

func (s *trajectoryToggleStory) View() string {
	w := s.width - 4
	if w < 20 {
		w = 60
	}

	steps := []toolsteps.Step{
		{
			Kind:     toolsteps.KindTool,
			Name:     "run_command",
			Title:    "Bash(echo one)",
			Summary:  "Ran 2 commands (echo one · echo two)",
			Detail:   "one",
		},
		{
			Kind:     toolsteps.KindTool,
			Name:     "run_command",
			Title:    "Bash(echo two)",
			Summary:  "Ran 2 commands (echo one · echo two)",
			Detail:   "two",
		},
	}

	expanded := s.state == 1
	lines := toolsteps.BlockLines(steps, expanded, w)

	var b strings.Builder
	b.WriteString(bold("Global Trajectory Toggle Demonstration (Ctrl+O):") + "\n\n")
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n" + dim("▶ Press Ctrl+O in Playground mode (Enter to focus, Ctrl+O to toggle expand / collapse)") + "\n")
	return b.String()
}

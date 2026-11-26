package storybook

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/editor/selection"
)

func newInputSelectionStory() Story {
	return Story{
		ID:          "composer-input-selection",
		Name:        "Input Selection (Stateful Module)",
		Category:    CategoryComposer,
		Description: "Real selection module: drag ranges, reverse-video highlight, clear and select-all transitions.",
		UpstreamSym: "jetski/cli/model.PromptModel.InputSelection",
		States: []State{
			{ID: "initial", Name: "Initial", Description: "No selection; editor value is unchanged."},
			{ID: "forward-selection", Name: "Forward Drag", Description: "Select the first word with a forward drag."},
			{ID: "reverse-selection", Name: "Reverse Drag", Description: "Select the last word with a reverse drag."},
			{ID: "cleared", Name: "Cancelled Drag", Description: "Reset a drag without changing the value."},
		},
		Factory: func() StoryInstance {
			inst := &selectionStory{}
			inst.SetState(0)
			return inst
		},
	}
}

// selectionStory is a Storybook adapter over the real composer selection
// module. It intentionally keeps a tiny in-memory editor so its states are
// deterministic and do not need RootModel, a terminal, or clipboard I/O.
type selectionStory struct {
	state   int
	width   int
	input   selectionStoryEditor
	selects selection.Model
}

type selectionStoryEditor struct {
	value string
	line  int
	col   int
}

func (e *selectionStoryEditor) Value() string              { return e.value }
func (e *selectionStoryEditor) SetValue(value string)      { e.value = value }
func (e *selectionStoryEditor) MoveCursorTo(line, col int) { e.line, e.col = line, col }

func (s *selectionStory) SetState(idx int) {
	s.state = idx
	s.selects.ResetInputSelection()
	s.input = selectionStoryEditor{value: "hello world"}
	switch idx {
	case 1: // forward drag
		s.selects.Begin(0, &s.input)
		s.selects.Drag(5, &s.input)
	case 2: // reverse drag
		s.selects.Begin(11, &s.input)
		s.selects.Drag(6, &s.input)
	case 3: // cleared/cancelled
		s.selects.Begin(2, &s.input)
		s.selects.ResetInputSelection()
	}
}

func (s *selectionStory) SetSize(w, h int) { s.width = w }

func (s *selectionStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	switch key.Key().Code {
	case tea.KeyEsc:
		s.selects.ResetInputSelection()
		return nil, true
	case 'a':
		if key.Key().Mod.Contains(tea.ModCtrl) || key.Key().Mod.Contains(tea.ModSuper) {
			s.selects.SelectAll(&s.input)
			return nil, true
		}
	case tea.KeyBackspace, tea.KeyDelete:
		if s.selects.HasInputSelection() {
			s.selects.DeleteInputSelection(&s.input)
			return nil, true
		}
	}
	return nil, false
}

func (s *selectionStory) View() string {
	view := "> " + s.input.value
	view = s.selects.ApplyHighlight(view, s.input.value)
	start, end := s.selects.InputSelectionRange()
	return fmt.Sprintf("Input selection module\n%s\nrange=[%d,%d) dragging=%v cursor=(%d,%d)\n",
		view, start, end, s.selects.InputSelectionDragging(), s.input.line, s.input.col)
}

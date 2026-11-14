package storybook

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
)

// RegisterPopupsStories registers slash-command and @ file-mention popups.
func RegisterPopupsStories(c *Catalog) {
	// 1. SlashPicker
	c.Register(Story{
		ID:          "popups-slash-picker",
		Name:        "SlashPicker (Command Dropdown)",
		Category:    CategoryPopups,
		Description: "The 5-row completion popup showing the canonical commands, category tags, and tails.",
		UpstreamSym: "jetski/cli.SlashPicker",
		States: []State{
			{ID: "all-commands", Name: "All Commands ('/')", Description: "Initial 5 rows with '↓ 100 more' indicator"},
			{ID: "filtered-m", Name: "Filtered ('/m')", Description: "Matches /model, /mcp, /models"},
			{ID: "filtered-t", Name: "Filtered ('/t')", Description: "Matches /tasks"},
			{ID: "no-matches", Name: "No Matches ('/xyz')", Description: "Empty listing showing 'No matches'"},
		},
		Factory: func() StoryInstance {
			inst := &slashPickerStory{state: 0, width: 80}
			inst.initPicker()
			return inst
		},
	})

	// 2. MentionPicker
	c.Register(Story{
		ID:          "popups-mention-picker",
		Name:        "MentionPicker (@-File Dropdown)",
		Category:    CategoryPopups,
		Description: "Workspace file mention popup showing file path suggestions.",
		UpstreamSym: "jetski/cli.AtFilePicker",
		States: []State{
			{ID: "initial", Name: "All Files ('@')", Description: "Workspace file suggestions list"},
			{ID: "filtered-in", Name: "Filtered ('@in')", Description: "Filtered by prefix 'internal/...'"},
		},
		Factory: func() StoryInstance {
			inst := &mentionPickerStory{state: 0, width: 80}
			inst.initPicker()
			return inst
		},
	})
}

type slashPickerStory struct {
	state  int
	width  int
	h      int
	buf    string
	picker *slashpicker.Panel
}

func (s *slashPickerStory) initPicker() {
	s.picker = slashpicker.New(slashpicker.Seams{
		AltScreen: func() bool { return true },
		Width:     func() int { return s.width },
		Buffer:    func() string { return s.buf },
		SetBuffer: func(newBuf string) { s.buf = newBuf },
	})
}

func (s *slashPickerStory) SetState(idx int) {
	s.state = idx
	switch idx {
	case 0:
		s.buf = "/"
	case 1:
		s.buf = "/m"
	case 2:
		s.buf = "/t"
	case 3:
		s.buf = "/xyz"
	}
	s.picker.Update([]rune(s.buf))
}

func (s *slashPickerStory) SetSize(w, h int) {
	s.width, s.h = w, h
}

func (s *slashPickerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	switch key.Key().Code {
	case tea.KeyUp, 'k':
		s.picker.HandleKey(tea.Key{Code: tea.KeyUp})
		return nil, true
	case tea.KeyDown, 'j':
		s.picker.HandleKey(tea.Key{Code: tea.KeyDown})
		return nil, true
	case tea.KeyBackspace:
		if len(s.buf) > 1 {
			s.buf = s.buf[:len(s.buf)-1]
			s.picker.Update([]rune(s.buf))
			return nil, true
		}
	default:
		if key.Key().Text != "" {
			s.buf += key.Key().Text
			s.picker.Update([]rune(s.buf))
			return nil, true
		}
	}
	return nil, false
}

func (s *slashPickerStory) View() string {
	var b strings.Builder
	b.WriteString(bold("Slash Command Picker Simulation:") + "\n")
	b.WriteString("  Input Buffer: " + blue(s.buf) + "\n\n")

	rows := s.picker.Rows()
	if rows == "" {
		b.WriteString(dim("  (Popup closed or no output)") + "\n")
	} else {
		b.WriteString(rows)
		if !strings.HasSuffix(rows, "\n") {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n" + dim(fmt.Sprintf("Catalogue stats: %d total registered commands", len(slashcommands.Default))) + "\n")
	return b.String()
}

type mentionPickerStory struct {
	state  int
	width  int
	h      int
	buf    string
	picker *mentionpicker.Panel
}

func (s *mentionPickerStory) initPicker() {
	s.picker = mentionpicker.New(mentionpicker.Seams{
		Width:     func() int { return s.width },
		Buffer:    func() string { return s.buf },
		SetBuffer: func(newBuf string) { s.buf = newBuf },
		Cwd:       func() string { return "." },
	})
}

func (s *mentionPickerStory) SetState(idx int) {
	s.state = idx
	switch idx {
	case 0:
		s.buf = "look at @"
	case 1:
		s.buf = "look at @internal"
	}
	s.picker.Update()
}

func (s *mentionPickerStory) SetSize(w, h int) {
	s.width, s.h = w, h
}

func (s *mentionPickerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	switch key.Key().Code {
	case tea.KeyUp, 'k':
		s.picker.HandleKey(tea.Key{Code: tea.KeyUp})
		return nil, true
	case tea.KeyDown, 'j':
		s.picker.HandleKey(tea.Key{Code: tea.KeyDown})
		return nil, true
	case tea.KeyBackspace:
		if len(s.buf) > 1 {
			s.buf = s.buf[:len(s.buf)-1]
			s.picker.Update()
			return nil, true
		}
	default:
		if key.Key().Text != "" {
			s.buf += key.Key().Text
			s.picker.Update()
			return nil, true
		}
	}
	return nil, false
}

func (s *mentionPickerStory) View() string {
	var b strings.Builder
	b.WriteString(bold("File Mention Dropdown Simulation:") + "\n")
	b.WriteString("  Input Buffer: " + blue(s.buf) + "\n\n")

	rows := s.picker.Rows()
	if rows == "" {
		b.WriteString(dim("  (Dropdown closed or no file candidates)") + "\n")
	} else {
		b.WriteString(rows)
		if !strings.HasSuffix(rows, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

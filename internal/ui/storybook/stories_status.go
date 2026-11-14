package storybook

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/internal/ui/views/swarmboard"
)

// RegisterStatusStories registers statusline, permission approval prompt, /btw side question, and swarm board stories.
func RegisterStatusStories(c *Catalog) {
	// 1. StatusLine
	c.Register(Story{
		ID:          "status-statusline",
		Name:        "StatusLine (Bottom Segment Bar)",
		Category:    CategoryStatus,
		Description: "Canonical bottom status bar with hint, active tasks, mode, and model tokens.",
		UpstreamSym: "jetski/cli/model.StatusLineModel",
		States: []State{
			{ID: "idle", Name: "Default Idle", Description: "Shortcuts hint with model and reasoning tier"},
			{ID: "narrow", Name: "Narrow Terminal", Description: "Graceful segment truncation on 50 cols"},
			{ID: "streaming", Name: "Streaming Turn", Description: "Cancel hint with active background tasks"},
			{ID: "compacting", Name: "Context Compacting", Description: "Live animated context window compaction"},
			{ID: "bash-mode", Name: "Bash Mode Armed", Description: "Exclamation prompt with bash cancellation hint"},
		},
		Factory: func() StoryInstance {
			inst := &statusLineStory{width: 80}
			inst.SetState(0)
			return inst
		},
	})

	// 2. PermissionPrompt
	c.Register(Story{
		ID:          "status-permission-prompt",
		Name:        "PermissionPrompt (Tool Approval Box)",
		Category:    CategoryStatus,
		Description: "Modal tool execution authorization box for command, read_file, and write_file actions.",
		UpstreamSym: "jetski/cli.PermissionPrompt",
		States: []State{
			{ID: "command", Name: "Command Approval", Description: "Authorization prompt for running a shell command"},
			{ID: "write-file", Name: "File Write Approval", Description: "Authorization prompt for file modifications"},
			{ID: "feedback", Name: "Feedback Mode", Description: "Interactive user feedback entry on rejection"},
		},
		Factory: func() StoryInstance {
			inst := &permissionPromptStory{width: 66}
			inst.SetState(0)
			return inst
		},
	})

	// 3. SideQuestionCard
	c.Register(Story{
		ID:          "status-side-question",
		Name:        "SideQuestionCard (/btw Card)",
		Category:    CategoryStatus,
		Description: "Inline side-question card with thick blue border, Braille spinner, and collapsible markdown body.",
		UpstreamSym: "jetski/cli.BtwCard",
		States: []State{
			{ID: "thinking", Name: "Thinking (Pending)", Description: "Single Braille spinner thinking before first token/tool"},
			{ID: "tool-call", Name: "Calling Tool", Description: "Single Braille spinner executing a tool with cancel prompt"},
			{ID: "generating", Name: "Generating Answer", Description: "Single Braille spinner streaming answer tokens"},
			{ID: "collapsed", Name: "Collapsed Card", Description: "Compact 30% view with 'Tab to expand' hint"},
			{ID: "expanded", Name: "Expanded Card", Description: "Full markdown viewport with scroll indicators"},
		},
		Factory: func() StoryInstance {
			inst := &sideQuestionStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 4. SwarmBoard
	c.Register(Story{
		ID:          "status-swarm-board",
		Name:        "SwarmBoard (Multi-Agent Roster & Posts)",
		Category:    CategoryStatus,
		Description: "3-pane collaborative swarm message board showing roster, threads, and post details.",
		UpstreamSym: "jetski/cortex/native_swarm.Board",
		States: []State{
			{ID: "3-pane", Name: "3-Pane Overview", Description: "Roster, posts, and message panes active"},
			{ID: "filtered", Name: "Filtered Member", Description: "Thread posts filtered to researcher"},
			{ID: "zoomed", Name: "Zoomed Pane", Description: "Full-width message detail view"},
		},
		Factory: func() StoryInstance {
			inst := &swarmBoardStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})
}

// -----------------------------------------------------------------------------
// 1. StatusLine Story
// -----------------------------------------------------------------------------

type statusLineStory struct {
	state int
	width int
}

func (s *statusLineStory) SetState(idx int) {
	s.state = idx
}

func (s *statusLineStory) SetSize(w, _ int) {
	s.width = w
}

func (s *statusLineStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}

	st := statusrow.State{
		Width:          w,
		CanonicalWidth: 80,
		Model:          "Gemini 3.8 Flash",
		EffortSuffix:   " · high",
		Mode:           "auto",
		BufferEmpty:    true,
	}

	switch s.state {
	case 0:
		// Default idle
	case 1:
		// Narrow terminal
		st.Width = 50
	case 2:
		// Streaming turn with running tasks
		st.Streaming = true
		st.Model = "MiniMax M3"
		st.TaskSuffix = " · 2 tasks"
	case 3:
		// Compacting context
		started := time.Now().Add(-12 * time.Second)
		st.Compact = &statusrow.Compact{
			Start: started,
		}
	case 4:
		// Bash mode armed
		st.BashMode = true
	}

	var b strings.Builder
	b.WriteString(bold("StatusLine Row View:") + "\n\n")
	b.WriteString(dim("─ Status Frame (Width="+fmt.Sprintf("%d cols", st.Width)+") ───────────────────────────") + "\n")
	b.WriteString(statusrow.Render(st) + "\n")
	b.WriteString(dim("──────────────────────────────────────────────────────") + "\n\n")

	b.WriteString(bold("Segment Breakdown:") + "\n")
	b.WriteString(fmt.Sprintf("  • Model: %s%s\n", st.Model, st.EffortSuffix))
	b.WriteString(fmt.Sprintf("  • Mode: %s\n", st.Mode))
	if st.TaskSuffix != "" {
		b.WriteString(fmt.Sprintf("  • Background Tasks: %s\n", strings.TrimPrefix(st.TaskSuffix, " · ")))
	}
	if st.Streaming {
		b.WriteString("  • Turn State: Streaming active (hint: 'esc to cancel')\n")
	}
	if st.Compact != nil {
		b.WriteString("  • Context Operation: Compacting context in background\n")
	}
	return b.String()
}

func (s *statusLineStory) HandleKey(tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// -----------------------------------------------------------------------------
// 2. PermissionPrompt Story
// -----------------------------------------------------------------------------

type permissionPromptStory struct {
	state int
	width int
	model *permissionprompt.Model
}

func (s *permissionPromptStory) SetState(idx int) {
	s.state = idx

	w := s.width
	if w < permissionprompt.MinBoxWidth {
		w = permissionprompt.MinBoxWidth
	}

	var req permissionprompt.Request
	switch idx {
	case 0:
		req = permissionprompt.Request{
			Tool:    "run_command",
			Command: "rm -rf dist && npm run build:prod",
		}
	case 1:
		req = permissionprompt.Request{
			Tool:        "write_file",
			Path:        "/Users/openagy/workspace/internal/tui/bridge.go",
			Description: "Patch engine bridge to support Storybook playground event routing",
		}
	case 2:
		req = permissionprompt.Request{
			Tool:    "run_command",
			Command: "curl -fsSL https://get.example.com/install.sh | bash",
		}
	}

	s.model = permissionprompt.New(req, permissionprompt.WithWidth(w))
	if idx == 2 {
		// Switch to feedback mode
		s.model.HandleKey(tea.Key{Code: 'd'})
	}
}

func (s *permissionPromptStory) SetSize(w, _ int) {
	s.width = w
}

func (s *permissionPromptStory) View() string {
	if s.model == nil {
		return ""
	}
	w := s.width
	if w < permissionprompt.MinBoxWidth {
		w = permissionprompt.MinBoxWidth
	}

	var b strings.Builder
	b.WriteString(bold("Modal Authorization Prompt:") + "\n\n")
	b.WriteString(s.model.Render(w) + "\n\n")
	b.WriteString(dim("Shortcuts: [y] Approve once  [a] Always allow  [n] Deny  [d] Feedback") + "\n")
	return b.String()
}

func (s *permissionPromptStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.model != nil {
		_, handled := s.model.HandleKey(key.Key())
		return nil, handled
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 3. SideQuestionCard Story
// -----------------------------------------------------------------------------

type sideQuestionStory struct {
	state  int
	width  int
	height int
	card   *sidequestion.Card
}

func (s *sideQuestionStory) SetState(idx int) {
	s.state = idx
	s.card = &sidequestion.Card{
		ID:       "sq-01",
		Question: "What is the difference between Elm architecture and standard Bubbletea?",
	}

	switch idx {
	case 0: // Thinking
		s.card.State = sidequestion.StatePending
	case 1: // Calling tool
		s.card.State = sidequestion.StateStreaming
		s.card.CurrentTool = "codebase_search"
	case 2: // Generating answer
		s.card.State = sidequestion.StateStreaming
		s.card.Answer = "Bubbletea is Go's pure implementation of the Elm architecture (Model-View-Update)."
	case 3: // Collapsed
		s.card.State = sidequestion.StateDone
		s.card.Answer = "Bubbletea is Go's pure implementation of the Elm architecture (Model-View-Update). State is never mutated across goroutines; instead, every interaction produces a message dispatched through Update()."
		s.card.Expanded = false
	case 4: // Expanded
		s.card.State = sidequestion.StateDone
		s.card.Answer = `### The Elm Architecture in Bubbletea

In standard object-oriented UI frameworks, components mutate their own state directly. In Bubbletea:
1. **Model**: Pure representation of component data and view state.
2. **View**: Pure projection of Model into a string representation.
3. **Update**: Pure reducer handling incoming messages and returning the updated Model with optional asynchronous ` + "`tea.Cmd`" + `.

#### Key Principles:
- Single source of truth.
- Decoupled asynchronous side-effects.
- Deterministic, easily testable UI transitions.`
		s.card.Expanded = true
	}
}

func (s *sideQuestionStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *sideQuestionStory) View() string {
	if s.card == nil {
		return ""
	}

	opts := sidequestion.CardRenderOptions{
		Width:     s.width,
		Height:    s.height,
		CardCount: 1,
	}

	var b strings.Builder
	b.WriteString(bold("Side Question (/btw) Card:") + "\n\n")
	b.WriteString(sidequestion.RenderBtwCard(s.card, opts) + "\n\n")
	b.WriteString(dim("Storybook tip: Press Tab to toggle card expansion interactively") + "\n")
	return b.String()
}

func (s *sideQuestionStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.card != nil {
		if key.Key().Code == tea.KeyTab {
			s.card.SetExpanded(!s.card.IsExpanded())
			return nil, true
		}
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 4. SwarmBoard Story
// -----------------------------------------------------------------------------

type swarmBoardStory struct {
	state  int
	width  int
	height int
	board  *swarmboard.Board
}

func (s *swarmBoardStory) SetState(idx int) {
	s.state = idx
	s.board = swarmboard.NewBoard(nil)
	s.board.Open()
	s.board.SetSize(s.width, s.height)

	now := time.Now()
	snap := &swarmboard.SwarmSnapshot{
		Title: "Refactor: OpenAGY TUI Storybook",
		Members: []swarmboard.Member{
			{Name: "coordinator", Role: "Swarm Orchestrator", State: "working", Model: "claude-sonnet-4.6", IsCoordinator: true},
			{Name: "researcher", Role: "Architecture Auditor", State: "waiting", Model: "gemini-3.8-flash"},
			{Name: "tester", Role: "Test & Parity Verifier", State: "waiting", Model: "gpt-oss-120b"},
		},
		Posts: []swarmboard.Post{
			{
				Seq:       1,
				Author:    "coordinator",
				Channel:   "board",
				Body:      "@researcher please survey all internal/ui/views packages and extract their seam interfaces.",
				CreatedAt: now.Add(-10 * time.Minute),
				Mentions:  []string{"researcher"},
			},
			{
				Seq:       2,
				Author:    "researcher",
				Channel:   "board",
				Body:      "@coordinator Completed! Found 10 canonical views including modelpicker, diffpanel, taskspanel, and statusrow.",
				CreatedAt: now.Add(-5 * time.Minute),
				Mentions:  []string{"coordinator"},
			},
			{
				Seq:       3,
				Author:    "coordinator",
				Channel:   "board",
				Body:      "@tester Please verify architecture boundaries and make sure `make arch` passes cleanly.",
				CreatedAt: now.Add(-1 * time.Minute),
				Mentions:  []string{"tester"},
			},
		},
	}

	s.board.ApplySnapshot(snap)

	switch idx {
	case 1:
		// Filter by researcher
		s.board.HandleKey(tea.Key{Code: 'f'})
	case 2:
		// Zoomed message
		s.board.HandleKey(tea.Key{Code: 'z'})
	}
}

func (s *swarmBoardStory) SetSize(w, h int) {
	s.width, s.height = w, h
	if s.board != nil {
		s.board.SetSize(w, h)
	}
}

func (s *swarmBoardStory) View() string {
	if s.board == nil {
		return ""
	}
	return s.board.View()
}

func (s *swarmBoardStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.board != nil {
		handled, cmd := s.board.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

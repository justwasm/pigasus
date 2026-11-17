package agentspanel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

func TestAgentsPanel_SubagentDetailView(t *testing.T) {
	sa := &subagent.Subagent{
		ID:     "sub-1",
		Name:   "worker-1",
		Title:  "Analyze Repository",
		State:  "running",
		Model:  "Gemini 3.8 Flash (High)",
		Effort: "high",
	}

	p := New(Seams{
		Subagents: func() []*subagent.Subagent {
			return []*subagent.Subagent{sa}
		},
		HasSession: func() bool { return false },
	}, nil)

	// Expand subagents accordion and position on subagent row (index 1).
	p.expandedSubagents = true
	p.SetCursor(1)

	// Press Enter to open Subagent Detail View
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || !p.DetailMode() {
		t.Fatalf("expected Enter to open DetailMode")
	}

	lines := strings.Join(p.Lines(), "\n")
	if !strings.Contains(lines, "main \xe2\x80\xba worker-1") {
		t.Errorf("expected detail title path, got:\n%s", lines)
	}
	if !strings.Contains(lines, "ID: sub-1") {
		t.Errorf("expected ID in detail view, got:\n%s", lines)
	}
	// AGY 1.5.9: the detail view carries the model and effort in the status line.
	if !strings.Contains(lines, "Gemini 3.8 Flash (High)") {
		t.Errorf("expected model in detail view, got:\n%s", lines)
	}
	if !strings.Contains(lines, "Prompt") {
		t.Errorf("expected Prompt section in detail view, got:\n%s", lines)
	}

	// AGY 1.5.9: Trajectory expand/collapse via Ctrl+O
	if p.ExpandedDetail() {
		t.Errorf("expected initial detail view to be collapsed")
	}
	handledCtrlO, _ := p.HandleKey(tea.Key{Code: 'o', Mod: tea.ModCtrl})
	if !handledCtrlO || !p.ExpandedDetail() {
		t.Fatalf("expected Ctrl+O to expand detail trajectory")
	}
	handledCtrlO2, _ := p.HandleKey(tea.Key{Code: 'o', Mod: tea.ModCtrl})
	if !handledCtrlO2 || p.ExpandedDetail() {
		t.Fatalf("expected Ctrl+O to collapse detail trajectory")
	}

	// Home / End navigation
	p.HandleKey(tea.Key{Code: tea.KeyEnd})
	p.HandleKey(tea.Key{Code: tea.KeyHome})
	if p.window != 0 {
		t.Errorf("expected Home to reset scroll window to 0, got %d", p.window)
	}

	// Press Esc to exit detail mode and return to list
	handledEsc, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handledEsc || p.DetailMode() {
		t.Fatalf("expected Esc to close DetailMode")
	}
}

func TestAgentsPanel_SubagentDetailView_NavigationAlignedWithMainChat(t *testing.T) {
	// Setup 200 lines of output
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "step output line")
	}
	sa := &subagent.Subagent{
		ID:     "sub-nav",
		Name:   "worker-nav",
		State:  "running",
		Result: strings.Join(lines, "\n"),
	}

	height := 30
	p := New(Seams{
		Subagents: func() []*subagent.Subagent { return []*subagent.Subagent{sa} },
		Size:      func() (int, int) { return 80, height },
	}, nil)

	p.OpenDetail(sa)
	p.HandleKey(tea.Key{Code: 'o', Mod: tea.ModCtrl}) // expand detail

	pageStep := p.pageStep()
	halfStep := p.halfPageStep()
	if pageStep < 5 || halfStep < 2 {
		t.Fatalf("expected meaningful page/half-page steps, got page=%d, half=%d", pageStep, halfStep)
	}

	// 1. Shift+Down (half page down)
	p.HandleKey(tea.Key{Code: tea.KeyDown, Mod: tea.ModShift})
	if p.window != halfStep {
		t.Errorf("after Shift+Down: window = %d, want %d", p.window, halfStep)
	}

	// 2. Ctrl+D (half page down)
	p.HandleKey(tea.Key{Code: 'd', Mod: tea.ModCtrl})
	if p.window != halfStep*2 {
		t.Errorf("after Ctrl+D: window = %d, want %d", p.window, halfStep*2)
	}

	// 3. Shift+Up (half page up)
	p.HandleKey(tea.Key{Code: tea.KeyUp, Mod: tea.ModShift})
	if p.window != halfStep {
		t.Errorf("after Shift+Up: window = %d, want %d", p.window, halfStep)
	}

	// 4. Ctrl+U (half page up)
	p.HandleKey(tea.Key{Code: 'u', Mod: tea.ModCtrl})
	if p.window != 0 {
		t.Errorf("after Ctrl+U: window = %d, want 0", p.window)
	}

	// 5. PgDown / Ctrl+F (page down)
	p.HandleKey(tea.Key{Code: tea.KeyPgDown})
	if p.window != pageStep {
		t.Errorf("after PgDown: window = %d, want %d", p.window, pageStep)
	}
	p.HandleKey(tea.Key{Code: 'f', Mod: tea.ModCtrl})
	if p.window != pageStep*2 {
		t.Errorf("after Ctrl+F: window = %d, want %d", p.window, pageStep*2)
	}

	// 6. PgUp / Ctrl+B (page up)
	p.HandleKey(tea.Key{Code: tea.KeyPgUp})
	if p.window != pageStep {
		t.Errorf("after PgUp: window = %d, want %d", p.window, pageStep)
	}
	p.HandleKey(tea.Key{Code: 'b', Mod: tea.ModCtrl})
	if p.window != 0 {
		t.Errorf("after Ctrl+B: window = %d, want 0", p.window)
	}

	// 7. Ctrl+End (jump to bottom)
	p.HandleKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if p.window <= pageStep*2 {
		t.Errorf("after Ctrl+End: window = %d, expected near bottom", p.window)
	}

	// 8. Ctrl+Home (jump to top)
	p.HandleKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if p.window != 0 {
		t.Errorf("after Ctrl+Home: window = %d, want 0", p.window)
	}
}

func TestAgentsPanel_SwitchConversationNotice(t *testing.T) {
	// 1. Without active session -> "create a new conversation"
	pNoSession := New(Seams{
		HasSession: func() bool { return false },
	}, nil)
	pNoSession.SetCursor(0)
	pNoSession.HandleKey(tea.Key{Code: tea.KeyEnter})
	pNoSession.prepared = true
	linesNoSession := strings.Join(pNoSession.Lines(), "\n")
	if !strings.Contains(linesNoSession, "[ Switch will create a new conversation on exit ]") {
		t.Errorf("expected new conversation notice, got:\n%s", linesNoSession)
	}

	// 2. With active session -> "fork the current conversation"
	pWithSession := New(Seams{
		HasSession: func() bool { return true },
	}, nil)
	pWithSession.SetCursor(0)
	pWithSession.HandleKey(tea.Key{Code: tea.KeyEnter})
	pWithSession.prepared = true
	linesWithSession := strings.Join(pWithSession.Lines(), "\n")
	if !strings.Contains(linesWithSession, "[ Switch will fork the current conversation on exit ]") {
		t.Errorf("expected fork conversation notice, got:\n%s", linesWithSession)
	}
}

func TestAgentsPanel_SubagentDetailView_LineWrapping(t *testing.T) {
	longOutput := "\tfunc SomeLongMethodWithTabIndent() {\n\t\tfmt.Println(\"a very long line that easily exceeds thirty columns by a lot\")\n\t}"
	sa := &subagent.Subagent{
		ID:     "sub-wrap",
		Name:   "worker-wrap",
		Title:  "Task with very long title that also exceeds thirty columns",
		State:  "running",
		Result: longOutput,
	}

	width := 35
	p := New(Seams{
		Subagents: func() []*subagent.Subagent {
			return []*subagent.Subagent{sa}
		},
		Size: func() (int, int) {
			return width, 24
		},
	}, nil)

	p.expandedSubagents = true
	p.SetCursor(1)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.DetailMode() {
		t.Fatal("expected DetailMode")
	}

	lines := p.Lines()
	// Exclude the bottom blank row and keyboard footer hint
	contentLines := lines[:len(lines)-2]
	for i, l := range contentLines {
		plain := ansi.Strip(l)
		if n := len([]rune(plain)); n > width {
			t.Errorf("line %d exceeds width %d (len %d): %q", i, width, n, plain)
		}
		if strings.Contains(l, "\t") {
			t.Errorf("line %d contains unexpanded tab: %q", i, l)
		}
	}
}

// TestAgentsPanel_SubagentRowsShowModelAndOneTip pins the AGY 1.5.3
// list changes: the model sits next to the subagent's name and the
// panel prints one tip line instead of a per-row "[view details]".
func TestAgentsPanel_SubagentRowsShowModelAndOneTip(t *testing.T) {
	sa := &subagent.Subagent{
		ID:    "sub-1",
		Name:  "worker-1",
		State: "running",
		Model: "Gemini 3.8 Flash (High)",
	}
	p := New(Seams{
		Size:      func() (int, int) { return 120, 40 },
		Subagents: func() []*subagent.Subagent { return []*subagent.Subagent{sa} },
	}, nil)
	p.expandedSubagents = true
	plain := ansi.Strip(strings.Join(p.Lines(), "\n"))
	if !strings.Contains(plain, "worker-1") || !strings.Contains(plain, "Gemini 3.8 Flash") {
		t.Errorf("subagent row does not carry the model:\n%s", plain)
	}
	if got := strings.Count(plain, "Tip: Press enter on a subagent to view details."); got != 1 {
		t.Errorf("tip line count = %d, want 1:\n%s", got, plain)
	}
	if strings.Contains(plain, "[view details]") {
		t.Errorf("the panel repeats a per-row [view details]:\n%s", plain)
	}
}

func TestAgentsPanel_SubagentDetailTrajectorySingleHint(t *testing.T) {
	sa := &subagent.Subagent{
		ID:     "sub-multi",
		Name:   "worker-multi",
		Title:  "Multi-step task",
		State:  "running",
		Model:  "Gemini 3.8 Flash (High)",
		Effort: "high",
	}

	task := &taskregistry.Task{
		ID:    "sub-multi",
		Name:  "worker-multi",
		Kind:  taskregistry.KindSubagent,
		State: taskregistry.StateRunning,
		Steps: []string{
			"Thought: Check weather for Shanghai",
			"read_file(/tmp/weather.json)",
			"Thought: Format the weather output",
			"write_file(/tmp/report.md)",
		},
	}

	p := New(Seams{
		Subagents: func() []*subagent.Subagent {
			return []*subagent.Subagent{sa}
		},
		TaskLookup: func(id string) *taskregistry.Task {
			if id == "sub-multi" {
				return task
			}
			return nil
		},
		HasSession: func() bool { return false },
		Size: func() (int, int) {
			return 100, 40
		},
	}, nil)

	p.OpenDetail(sa)
	if !p.DetailMode() {
		t.Fatalf("expected OpenDetail to open DetailMode")
	}

	// In collapsed mode:
	plainCollapsed := ansi.Strip(strings.Join(p.Lines(), "\n"))
	expandCount := strings.Count(plainCollapsed, "(ctrl+o to expand)")
	collapseCount := strings.Count(plainCollapsed, "(ctrl+o to collapse)")
	if expandCount != 1 {
		t.Errorf("expected exactly 1 '(ctrl+o to expand)' in collapsed mode, got %d:\n%s", expandCount, plainCollapsed)
	}
	if collapseCount != 0 {
		t.Errorf("expected 0 '(ctrl+o to collapse)' in collapsed mode, got %d", collapseCount)
	}
	if !strings.Contains(plainCollapsed, "ctrl+o expand") {
		t.Errorf("expected footer to show 'ctrl+o expand', got:\n%s", plainCollapsed)
	}

	// Toggle expand with ctrl+o key
	handled, _ := p.HandleKey(tea.Key{Code: 'o', Mod: tea.ModCtrl})
	if !handled || !p.ExpandedDetail() {
		t.Fatalf("expected ctrl+o to toggle expanded detail")
	}

	plainExpanded := ansi.Strip(strings.Join(p.Lines(), "\n"))
	expandCountExp := strings.Count(plainExpanded, "(ctrl+o to expand)")
	collapseCountExp := strings.Count(plainExpanded, "(ctrl+o to collapse)")
	if expandCountExp != 0 {
		t.Errorf("expected 0 '(ctrl+o to expand)' in expanded mode, got %d", expandCountExp)
	}
	if collapseCountExp != 1 {
		t.Errorf("expected exactly 1 '(ctrl+o to collapse)' in expanded mode, got %d:\n%s", collapseCountExp, plainExpanded)
	}
	if !strings.Contains(plainExpanded, "ctrl+o collapse") {
		t.Errorf("expected footer to show 'ctrl+o collapse', got:\n%s", plainExpanded)
	}
}

func TestAgentsPanel_SubagentDetailView_TranscriptWithOutput(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempDir)

	subID := "sub-live-weather"
	logDir := filepath.Join(tempDir, "brain", subID, ".system_generated", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("failed to create log dir: %v", err)
	}

	jsonlContent := strings.Join([]string{
		`{"step_index":0,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE","created_at":"2026-11-15T12:00:00Z","content":"<SYSTEM_MESSAGE>Query weather</SYSTEM_MESSAGE>"}`,
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-15T12:00:01Z","input_tokens":100,"output_tokens":42,"thinking":"Checking the weather in Shanghai.\nNeed to run curl.","tool_calls":[{"name":"run_command","args":{"CommandLine":"curl -s wttr.in/Shanghai?format=j1"}}]}`,
		`{"step_index":2,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-11-15T12:00:03Z","content":"Created At: 2026-11-15T12:00:01Z\nCompleted At: 2026-11-15T12:00:03Z\nCommand exited with code 0.\n\nCommand output:\nTemp: 22C\nCondition: Sunny"}`,
		`{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-15T12:00:04Z","input_tokens":150,"output_tokens":20,"content":"Shanghai is sunny and 22C."}`,
	}, "\n") + "\n"

	if err := os.WriteFile(filepath.Join(logDir, "transcript.jsonl"), []byte(jsonlContent), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	task := &taskregistry.Task{
		ID:        subID,
		Name:      "weather researcher",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateDone,
		Prompt:    "Query weather for Shanghai",
		StartedAt: time.Now().Add(-5 * time.Second),
		EndedAt:   time.Now(),
	}

	// 1. In collapsed mode:
	_, bodyCollapsed := BuildSubagentDetail(task, 100, false)
	joinedCollapsed := ansi.Strip(strings.Join(bodyCollapsed, "\n"))

	if strings.Contains(joinedCollapsed, "1. ") || strings.Contains(joinedCollapsed, "2. ") {
		t.Errorf("did not expect numbered markdown list in trajectory:\n%s", joinedCollapsed)
	}
	if !strings.Contains(joinedCollapsed, "● Bash(curl -s wttr.in/Shanghai?format=j1)") {
		t.Errorf("expected tool call '● Bash(...)', got:\n%s", joinedCollapsed)
	}
	if !strings.Contains(joinedCollapsed, "▸ Thought for") {
		t.Errorf("expected thought bullet '▸ Thought for', got:\n%s", joinedCollapsed)
	}
	if strings.Contains(joinedCollapsed, "Condition: Sunny") {
		t.Errorf("did not expect tool execution output visible in collapsed mode:\n%s", joinedCollapsed)
	}
	if strings.Count(joinedCollapsed, "(ctrl+o to expand)") != 1 {
		t.Errorf("expected exactly 1 '(ctrl+o to expand)' in collapsed mode, got:\n%s", joinedCollapsed)
	}

	// 2. In expanded mode:
	_, bodyExpanded := BuildSubagentDetail(task, 100, true)
	joinedExpanded := ansi.Strip(strings.Join(bodyExpanded, "\n"))

	if !strings.Contains(joinedExpanded, "▾ Thought for") {
		t.Errorf("expected expanded thought bullet '▾ Thought for', got:\n%s", joinedExpanded)
	}
	if !strings.Contains(joinedExpanded, "Checking the weather in Shanghai.") {
		t.Errorf("expected full thought reasoning text in expanded mode:\n%s", joinedExpanded)
	}
	if !strings.Contains(joinedExpanded, "Command output:") || !strings.Contains(joinedExpanded, "Condition: Sunny") {
		t.Errorf("expected tool execution output visible in expanded mode:\n%s", joinedExpanded)
	}
	if strings.Count(joinedExpanded, "(ctrl+o to collapse)") != 1 {
		t.Errorf("expected exactly 1 '(ctrl+o to collapse)' in expanded mode, got:\n%s", joinedExpanded)
	}
}

func TestAgentsPanel_SubagentDetailView_FormatB_SubagentsDir(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempDir)

	subID := "48e453d0-weather-format-b"
	subDir := filepath.Join(tempDir, "subagents")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subagents dir: %v", err)
	}

	jsonlContent := strings.Join([]string{
		`{"index":0,"role":"user","subagent":"` + subID + `","message":{"role":"user","content":"<SYSTEM_MESSAGE>Query weather</SYSTEM_MESSAGE>"}}`,
		`{"index":1,"role":"assistant","subagent":"` + subID + `","message":{"role":"assistant","tool_name":"run_command","tool_call_id":"call_123","content":"{\"CommandLine\": \"curl -s \\\"wttr.in/Shanghai?format=j1\\\"\"}"}}`,
		`{"index":2,"role":"toolResult","subagent":"` + subID + `","message":{"role":"toolResult","tool_call_id":"call_123","content":"Command exited with code 0.\n\nCommand output:\nTemp: 18C\nCondition: Cloudy"}}`,
		`{"index":3,"role":"assistant","subagent":"` + subID + `","message":{"role":"assistant","content":"# Shanghai Weather Report"}}`,
	}, "\n") + "\n"

	if err := os.WriteFile(filepath.Join(subDir, subID+".jsonl"), []byte(jsonlContent), 0644); err != nil {
		t.Fatalf("failed to write subagents jsonl: %v", err)
	}

	steps, total := LoadTrajectoryFromTranscript(subID)
	if total != 4 {
		t.Fatalf("expected total = 4, got %d", total)
	}
	if len(steps) != 1 { // 1 tool step (final message omitted from trajectory as it belongs to answer)
		t.Fatalf("expected 1 trajectory step, got %d", len(steps))
	}
	if steps[0].ToolName != "Bash" {
		t.Errorf("expected ToolName 'Bash', got %q", steps[0].ToolName)
	}
	if steps[0].Args != `curl -s "wttr.in/Shanghai?format=j1"` {
		t.Errorf("expected clean args with quotes unescaped, got %q", steps[0].Args)
	}
	if !strings.Contains(steps[0].Output, "Temp: 18C") {
		t.Errorf("expected output to be populated from toolResult, got %q", steps[0].Output)
	}

	task := &taskregistry.Task{
		ID:        subID,
		Name:      "weather researcher",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateDone,
		Prompt:    "Query weather for Shanghai",
		StartedAt: time.Now().Add(-5 * time.Second),
		EndedAt:   time.Now(),
	}

	// 1. Collapsed mode:
	_, bodyCollapsed := BuildSubagentDetail(task, 100, false)
	joinedCollapsed := ansi.Strip(strings.Join(bodyCollapsed, "\n"))

	expectedLine := `● Bash(curl -s "wttr.in/Shanghai?format=j1") (ctrl+o to expand)`
	if !strings.Contains(joinedCollapsed, expectedLine) {
		t.Errorf("expected collapsed trajectory line %q, got:\n%s", expectedLine, joinedCollapsed)
	}

	// 2. Expanded mode:
	_, bodyExpanded := BuildSubagentDetail(task, 100, true)
	joinedExpanded := ansi.Strip(strings.Join(bodyExpanded, "\n"))

	if !strings.Contains(joinedExpanded, `● Bash(curl -s "wttr.in/Shanghai?format=j1")`) {
		t.Errorf("expected expanded tool header, got:\n%s", joinedExpanded)
	}
	if !strings.Contains(joinedExpanded, "Command output:") || !strings.Contains(joinedExpanded, "Temp: 18C") {
		t.Errorf("expected tool output visible in expanded mode:\n%s", joinedExpanded)
	}
	if !strings.Contains(joinedExpanded, "(ctrl+o to collapse)") {
		t.Errorf("expected '(ctrl+o to collapse)' in expanded mode, got:\n%s", joinedExpanded)
	}

	// 3. Test panel key handling with both modifier and string formats
	sa := &subagent.Subagent{
		ID:    subID,
		Name:  "weather researcher",
		State: "running",
	}
	p := New(Seams{
		Subagents: func() []*subagent.Subagent { return []*subagent.Subagent{sa} },
	}, nil)
	p.OpenDetail(sa)
	if !p.DetailMode() {
		t.Fatal("expected DetailMode")
	}

	// ctrl+o via key.Mod
	handled, _ := p.HandleKey(tea.Key{Code: 'o', Mod: tea.ModCtrl})
	if !handled || !p.ExpandedDetail() {
		t.Fatalf("expected ctrl+o with ModCtrl to expand detail")
	}

	// ctrl+o via key.Text / key.String()
	handled2, _ := p.HandleKey(tea.Key{Text: "ctrl+o"})
	if !handled2 || p.ExpandedDetail() {
		t.Fatalf("expected 'ctrl+o' string to collapse detail")
	}
}

func TestAgentsPanel_SubagentDetailView_1000LinesScrollPerformance(t *testing.T) {
	// Build a 1000-line output
	var longLines []string
	for i := 1; i <= 1000; i++ {
		longLines = append(longLines, "line "+string(rune('0'+(i/1000)%10))+string(rune('0'+(i/100)%10))+string(rune('0'+(i/10)%10))+string(rune('0'+i%10))+": output content of the subagent turn")
	}
	longOutput := strings.Join(longLines, "\n")

	sa := &subagent.Subagent{
		ID:     "sub-perf-1000",
		Name:   "perf-worker",
		Title:  "Large output test",
		State:  "running",
		Result: longOutput,
	}

	p := New(Seams{
		Subagents: func() []*subagent.Subagent { return []*subagent.Subagent{sa} },
		Size:      func() (int, int) { return 80, 24 },
	}, nil)

	p.OpenDetail(sa)
	if !p.DetailMode() {
		t.Fatal("expected DetailMode")
	}

	// Expand detail with Ctrl+O
	p.HandleKey(tea.Key{Code: 'o', Mod: tea.ModCtrl})
	if !p.ExpandedDetail() {
		t.Fatal("expected ExpandedDetail")
	}

	// Warm-up initial render
	initialLines := p.Lines()
	if len(initialLines) == 0 {
		t.Fatal("expected non-empty lines")
	}

	// Scroll 1000 times and measure execution time
	start := time.Now()
	for i := 0; i < 1000; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
		lines := p.Lines()
		if len(lines) == 0 {
			t.Fatalf("empty lines at scroll %d", i)
		}
	}
	elapsed := time.Since(start)

	// 1000 scroll frames must take well under 1 second (typically < 30ms with cache)
	if elapsed > 2*time.Second {
		t.Fatalf("1000 scroll frames took %v, expected under 2s (previously took 10 minutes)", elapsed)
	}
	t.Logf("1000 scroll frames completed in %v", elapsed)

	// Verify line counter shows scrolled position
	joined := strings.Join(p.Lines(), "\n")
	if !strings.Contains(joined, "lines") {
		t.Errorf("expected line counter indicator in scrolled view:\n%s", joined)
	}
}

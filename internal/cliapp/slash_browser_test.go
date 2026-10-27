package cliapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/tools"
)

// slash_browser_test.go — the /browser and /schedule slash commands.
//
// Cycle 342: both commands used to print a hard-coded quota block
// ("⚠ Individual quota reached …" + "Error ID: <uuid>-1"), because the
// reference's capture of them was taken while the reference account's
// quota was exhausted. The tests below pin the behaviour that replaced
// it: /browser runs the browser agent, /schedule sends the instruction
// to the model, and neither of them ever claims a quota failure.

// fakeBrowserAgent is a browser agent that answers with a canned text.
type fakeBrowserAgent struct {
	answer string
	err    error
	task   string
}

// RunBrowserTask records the task and answers with the canned text.
func (f *fakeBrowserAgent) RunBrowserTask(_ context.Context, task string) (string, error) {
	f.task = task
	return f.answer, f.err
}

// withFakeBrowserAgent installs a fake browser agent for the duration of
// the test.
func withFakeBrowserAgent(t *testing.T, agent *fakeBrowserAgent) {
	t.Helper()
	prev := browserAgentFactory
	browserAgentFactory = func(*runMode) (tools.BrowserSubagentProvider, func(), error) {
		return agent, nil, nil
	}
	t.Cleanup(func() { browserAgentFactory = prev })
}

// strippedRows returns the transcript rows without their SGR styling.
func strippedRows(m *runMode) []string {
	out := make([]string, len(m.response))
	for i, row := range m.response {
		out[i] = stripSGR(row)
	}
	return out
}

// withFakeBrowserProbe replaces the browser probe for the duration of
// the test: the real one attaches to (or launches) a Chrome, and a unit
// test must do neither.
func withFakeBrowserProbe(t *testing.T, err error) {
	t.Helper()
	prev := browserProbe
	browserProbe = func(context.Context) error { return err }
	t.Cleanup(func() { browserProbe = prev })
}

// TestNewBrowserAgent_BuildsARunner pins that the command can build its
// runner from the TUI's own state: the session's model, the session's
// catalogue, and a browser agent of its own (the tool's runner belongs
// to the turn that installed it, and that turn is over).
func TestNewBrowserAgent_BuildsARunner(t *testing.T) {
	m := newSlashModel(t)
	m.catalog = tools.RuntimeCatalog(t.TempDir())
	withFakeBrowserProbe(t, nil)
	runner, closeRunner, err := m.newBrowserAgent()
	if err != nil {
		t.Fatalf("newBrowserAgent: %v", err)
	}
	if closeRunner != nil {
		defer closeRunner()
	}
	if runner == nil {
		t.Fatal("newBrowserAgent returned no runner")
	}
}

// TestNewBrowserAgent_NoBrowser_SaysWhy pins the precondition: without a
// browser there is no browser agent, and the command must say so instead
// of starting a run whose every tool call is doomed.
//
// The agent itself would keep trying new tools after each failure — it
// only knows that a tool failed — so the run would burn the model's
// quota on a dozen calls that cannot work.
func TestNewBrowserAgent_NoBrowser_SaysWhy(t *testing.T) {
	m := newSlashModel(t)
	m.catalog = tools.RuntimeCatalog(t.TempDir())
	withFakeBrowserProbe(t, browsercdp.ErrBrowserUnavailable)

	runner, closeRunner, err := m.newBrowserAgent()
	if err == nil {
		if closeRunner != nil {
			closeRunner()
		}
		t.Fatal("newBrowserAgent built a runner without a browser")
	}
	if runner != nil {
		t.Error("newBrowserAgent returned a runner without a browser")
	}
	if !errors.Is(err, browsercdp.ErrBrowserUnavailable) {
		t.Errorf("newBrowserAgent error = %v, want the browser-unavailable reason", err)
	}
	if !strings.Contains(err.Error(), "CHROME_BIN") {
		t.Errorf("the reason does not name the overrides: %v", err)
	}
}

// TestSlashBrowser_NoTask_ShowsUsage pins the empty-argument case: the
// command says how it is used instead of pretending to have run.
func TestSlashBrowser_NoTask_ShowsUsage(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/browser")
	if cmd != nil {
		t.Error("/browser without a task should not schedule a tea.Cmd")
	}
	rows := afterEcho(m)
	if len(rows) == 0 || rows[0] != "Usage: /browser <task>" {
		t.Errorf("/browser without a task = %q, want the usage line", rows)
	}
}

// TestSlashBrowser_RunsTheBrowserAgent pins the command's whole path:
// the task reaches the browser agent and the agent's answer lands in the
// transcript.
func TestSlashBrowser_RunsTheBrowserAgent(t *testing.T) {
	m := newSlashModel(t)
	agent := &fakeBrowserAgent{answer: "the price is 42"}
	withFakeBrowserAgent(t, agent)

	_, cmd := m.handleSlashCommand("/browser what is the price?")
	if cmd == nil {
		t.Fatal("/browser with a task should return a tea.Cmd")
	}
	if agent.task != "" {
		t.Error("the browser agent ran before the command was executed")
	}

	msg := cmd()
	done, ok := msg.(browserAgentDoneMsg)
	if !ok {
		t.Fatalf("/browser command produced %T, want browserAgentDoneMsg", msg)
	}
	if done.err != nil {
		t.Fatalf("/browser command failed: %v", done.err)
	}
	m.Update(done)

	if agent.task != "what is the price?" {
		t.Errorf("the browser agent got the task %q, want %q", agent.task, "what is the price?")
	}
	joined := strings.Join(strippedRows(m), "\n")
	if !strings.Contains(joined, "the price is 42") {
		t.Errorf("the browser agent's answer is not in the transcript: %q", joined)
	}
}

// TestSlashBrowser_AgentUnavailable pins the case where the session has
// no browser agent: the command says so, and says nothing about quota.
func TestSlashBrowser_AgentUnavailable(t *testing.T) {
	m := newSlashModel(t)
	prev := browserAgentFactory
	browserAgentFactory = func(*runMode) (tools.BrowserSubagentProvider, func(), error) {
		return nil, nil, errors.New("the browser tools are not available in this session")
	}
	t.Cleanup(func() { browserAgentFactory = prev })

	_, cmd := m.handleSlashCommand("/browser something")
	if cmd != nil {
		t.Error("/browser without a browser agent should not schedule a tea.Cmd")
	}
	rows := afterEcho(m)
	if len(rows) == 0 || !strings.Contains(rows[0], "not available") {
		t.Errorf("/browser without a browser agent = %q, want the reason", rows)
	}
}

// TestSlashBrowser_AgentFailure pins the failure path: the error is
// reported in the transcript, in the reference's warning colour.
func TestSlashBrowser_AgentFailure(t *testing.T) {
	m := newSlashModel(t)
	agent := &fakeBrowserAgent{err: errors.New("chrome did not start")}
	withFakeBrowserAgent(t, agent)

	_, cmd := m.handleSlashCommand("/browser something")
	if cmd == nil {
		t.Fatal("/browser with a task should return a tea.Cmd")
	}
	msg := cmd()
	done, ok := msg.(browserAgentDoneMsg)
	if !ok {
		t.Fatalf("/browser command produced %T, want browserAgentDoneMsg", msg)
	}
	m.Update(done)

	joined := strings.Join(strippedRows(m), "\n")
	if !strings.Contains(joined, "chrome did not start") {
		t.Errorf("the browser agent's failure is not in the transcript: %q", joined)
	}
}

// TestSlashCommands_NeverClaimAQuota is the regression guard: neither
// command may print the cycle-194 quota block again. The block claimed
// a quota state the clone never checked, and it read as "this CLI cannot
// reach any model".
func TestSlashCommands_NeverClaimAQuota(t *testing.T) {
	m := newSlashModel(t)
	withFakeBrowserAgent(t, &fakeBrowserAgent{answer: "done"})

	m.handleSlashCommand("/browser")
	m.handleSlashCommand("/browser a task")
	m.handleSlashCommand("/schedule")
	m.handleSlashCommand("/schedule an instruction")

	for _, row := range strippedRows(m) {
		if strings.Contains(row, "quota reached") || strings.Contains(row, "Error ID:") {
			t.Errorf("a slash command claimed a quota failure: %q", row)
		}
	}
}

// TestSlashSchedule_NoInstruction_ShowsUsage pins the empty-argument
// case for /schedule.
func TestSlashSchedule_NoInstruction_ShowsUsage(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/schedule")
	if cmd != nil {
		t.Error("/schedule without an instruction should not schedule a tea.Cmd")
	}
	if m.streaming {
		t.Error("/schedule without an instruction should leave m.streaming=false")
	}
	rows := afterEcho(m)
	if len(rows) == 0 || rows[0] != "Usage: /schedule <instruction>" {
		t.Errorf("/schedule without an instruction = %q, want the usage line", rows)
	}
}

// TestSlashSchedule_SendsTheInstruction pins the command's behaviour:
// the reference's capture of /schedule shows model output under the
// command, so the command sends a prompt — like /plan and /goal do.
func TestSlashSchedule_SendsTheInstruction(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/schedule check the build every hour")
	if cmd == nil {
		t.Fatal("/schedule with an instruction should return a tea.Cmd")
	}
	if !m.streaming {
		t.Error("/schedule should set m.streaming=true")
	}
	if m.response[0] != "> /schedule" {
		t.Errorf("/schedule echo = %q, want %q", m.response[0], "> /schedule")
	}
	got := lastUserMessage(m)
	if !strings.Contains(got, "check the build every hour") {
		t.Errorf("/schedule session message lost the instruction: %q", got)
	}
	if !strings.Contains(got, "schedule") {
		t.Errorf("/schedule session message lost the scheduling intent: %q", got)
	}
}

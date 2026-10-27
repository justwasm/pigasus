package cliapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/browsersubagent"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// slash_browser.go — the /browser slash command.
//
// The reference describes the command as "Invoke a browser agent for web
// tasks." — that is the description in the reference's own slash menu
// (internal/tuilib/commands.go, the catalogue recovered from the binary
// 【一手】). The clone has that agent: internal/browsersubagent, wired
// into the browser_subagent tool (cycle 311).
//
// What cycle 194 left behind
// --------------------------
// Cycle 194 made /browser print a hard-coded block:
//
//	⚠ Individual quota reached. Please upgrade your subscription to
//	increase your limits. Resets in 0h0m0s.
//	Error ID: <uuid>-1
//
// The reference's capture of the command was taken while *the reference
// account's* quota was exhausted, so what the capture holds is the
// failure, not the behaviour. The clone reproduced the failure and
// printed it for every user, on every account, forever — a false
// statement about the reader's own quota. It reads as "this CLI cannot
// reach any model", which is the opposite of what the clone does: the
// model path is the one thing the clone always had.
//
// This cycle runs the browser agent instead. The command answers with
// the agent's final text — the same text the browser_subagent tool
// returns.
//
// Evidence grades:
//   【一手】 the command's description (the reference's slash menu).
//   【反编译】 the reference's browser_subagent step carries task /
//     result, so the agent's answer is what the command reports.
//   【推测】 that the reference's /browser starts that agent itself: the
//     capture shows model output under the command, and a browser agent
//     is a model.

// browserAgentDoneMsg carries the browser agent's answer back into the
// TUI.
type browserAgentDoneMsg struct {
	answer string
	err    error
}

// browserAgentFactory builds the browser agent for one /browser
// invocation.
//
// It is a var so a test can substitute a fake runner: the real one
// needs a model, a session and a browser.
var browserAgentFactory = func(m *runMode) (tools.BrowserSubagentProvider, func(), error) {
	return m.newBrowserAgent()
}

// browserProbe answers whether the browser tools can run at all.
//
// It is a var so a test can replace it: the real probe looks for a
// browser, and a unit test must not start one.
//
// The probe is what keeps the command honest. Without a browser every
// browser tool fails, and the agent — which only knows that its tools
// failed — keeps trying new ones: the run then burns the model's quota
// on a dozen doomed calls. The reference's own cap on that is not
// recoverable; the clone's rule is to answer before the model is asked.
//
// The probe starts nothing: the browser starts on the first tool call,
// the reference's rule.
//
// The live surface probes the bridge instead of the CDP client: it asks
// the extension for the session's tabs, which opens nothing and moves
// nothing, and it is the only way to know that the extension is attached
// to the browser the user is actually using.
var browserProbe = func(ctx context.Context) error {
	_, err := browsersubagent.Probe(ctx)
	return err
}

// browserAgentTimeout bounds one /browser run.
//
// The clone's own policy, for the same reason: a slash command that never
// returns is a defect in the TUI.
const browserAgentTimeout = 10 * time.Minute

// runBrowserCommand is the /browser slash command's body.
//
// The task is the command's argument: "/browser <task>". Without one
// there is nothing to browse, so the command says how it is used
// instead of pretending to have run.
func (m *runMode) runBrowserCommand(args string) (bool, tea.Cmd) {
	task := strings.TrimSpace(args)
	if task == "" {
		m.slashRespond("/browser", "Usage: /browser <task>")
		return true, nil
	}
	runner, closeRunner, err := browserAgentFactory(m)
	if err != nil {
		m.slashRespond("/browser", "The browser agent is not available: "+err.Error())
		return true, nil
	}
	m.slashRespond("/browser", "Running the browser agent…")
	return true, m.runBrowserTask(runner, closeRunner, task)
}

// runBrowserTask runs the task in the browser agent and answers with
// the agent's final text.
//
// The run happens off the UI goroutine: a browsing task can take
// minutes, and the TUI has to stay responsive while it does.
func (m *runMode) runBrowserTask(runner tools.BrowserSubagentProvider, closeRunner func(), task string) tea.Cmd {
	return func() tea.Msg {
		if closeRunner != nil {
			defer closeRunner()
		}
		ctx, cancel := context.WithTimeout(context.Background(), browserAgentTimeout)
		defer cancel()
		answer, err := runner.RunBrowserTask(ctx, task)
		if err != nil && errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("the browser agent did not finish within %s", browserAgentTimeout)
		}
		return browserAgentDoneMsg{answer: answer, err: err}
	}
}

// updateBrowserAgentDoneMsg puts the browser agent's answer into the
// transcript.
//
// The answer is plain text, the way a model's answer is: the reference's
// /browser streamed the agent's output under the command, not a wrapped
// command reply.
func (m *runMode) updateBrowserAgentDoneMsg(msg browserAgentDoneMsg) {
	if msg.err != nil {
		m.response = append(m.response,
			panelGray166+msg.err.Error()+panelGray166C,
			"",
		)
		return
	}
	answer := strings.TrimRight(strings.TrimSpace(msg.answer), "\n")
	if answer == "" {
		m.response = append(m.response,
			panelGray238+"(the browser agent returned no answer)"+panelGray238C,
			"",
		)
		return
	}
	for _, line := range strings.Split(answer, "\n") {
		m.response = append(m.response, line)
	}
	m.response = append(m.response, "")
}

// newBrowserAgent builds a browser agent for one /browser invocation.
//
// The browser_subagent *tool* gets its runner per turn
// (internal/cliapp/tui_runmode_agent.go), and that runner's session
// services are closed when the turn ends — so the slash command cannot
// borrow it. It builds its own, on the session's own model and the
// session's own browser tools, and closes it when the task is done.
func (m *runMode) newBrowserAgent() (tools.BrowserSubagentProvider, func(), error) {
	if m.catalog == nil {
		return nil, nil, errors.New("the browser tools are not available in this session")
	}
	if m.agentBody != "" {
		return nil, nil, errors.New("this session runs an agent body, which has no browser agent")
	}
	if m.backend == nil {
		return nil, nil, errors.New("no model is selected")
	}
	model := pigPrintModel(m.backend, m.model)
	if model == nil {
		return nil, nil, errors.New("no model is selected")
	}
	// The browser comes first: without one every tool fails, and the
	// agent would retry new tools until the run is over. The probe
	// starts nothing — the browser starts on the first tool call, the
	// reference's rule — so this only finds out whether a browser could
	// work at all.
	if err := browserProbe(context.Background()); err != nil {
		return nil, nil, err
	}
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	agentDir := auth.AntigravityConfigHome()
	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      workspace,
		AgentDir: agentDir,
	})
	if err != nil {
		return nil, nil, err
	}
	runner := browsersubagent.New(subagenttools.Deps{
		Services: services,
		Model:    model,
		CWD:      workspace,
		AgentDir: agentDir,
	}, browsersubagent.Tools(m.catalog))
	return runner, func() { services.Close() }, nil
}

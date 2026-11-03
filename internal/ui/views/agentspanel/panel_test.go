// panel_test.go — the /agents panel's own half.
//
// Cycle 381 moved the panel out of internal/cliapp: the agent list, the
// running-agent section, the cursor, the keys and the prepared switch
// are the package's, and the host keeps only the seams. The behaviour is
// the clone's own from cycle 205 on, so the tests move with the code
// they exercise.
//
// The tests build the panel directly — there is no runMode behind it —
// so a seam is a closure over a test value.

package agentspanel

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	"github.com/justwasm/pigasus/internal/features/agents"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
)

// ---------------------------------------------------------------- helpers

// stripSGR removes every ANSI SGR sequence.
func stripSGR(s string) string {
	return termcolor.StripSGR(s)
}

// plainLines strips the SGR spans off a rendered row slice.
func plainLines(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, stripSGR(r))
	}
	return out
}

// assertLines compares a rendered row slice with the expected rows.
func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

// containsRow is a tiny helper so the test reads cleanly.
func containsRow(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// markFailed mutates a Subagent into the failed state with a known
// reason. We do this directly because the bash-driven Wait() path takes
// a real exec.Cmd, and the test only cares about the StatusLine mapping
// — the wire shape (State="failed", LastError=<reason>) is what the
// panel reads.
func markFailed(s *subagent.Subagent, reason string) {
	// Inject by sending it through Wait's natural exit path:
	// run a sub-process that exits non-zero.
	cmd := exec.Command("sh", "-c", "exit 1")
	cmd.Stdin, _ = os.Open(os.DevNull)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	s.Cmd = cmd
	if err := s.Start(); err != nil {
		// If sh isn't available we still set the fields by
		// hand so the test runs on minimal CI images.
		s.SetStateForTest("failed")
		s.SetLastErrorForTest(reason)
		return
	}
	if err := s.Wait(context.Background()); err == nil {
		s.SetStateForTest("failed")
		s.SetLastErrorForTest(reason)
		return
	}
	// Override the captured reason so we test against the
	// exact "quota exceeded" string the changelog mentions.
	s.SetLastErrorForTest(reason)
}

// ---------------------------------------------------------------- tests

// TestPanelAgentsLayout pins the /agents frame: the title, the two path
// rows, the built-in default row and the keyboard legend.
func TestPanelAgentsLayout(t *testing.T) {
	p := New(Seams{}, nil)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, plainLines(p.Lines()), []string{
		"Create New Agents",
		"  Workspace: " + filepath.Join(wd, ".agents", "agents", "{agent_name}", "agent.md"),
		"  Global: " + filepath.Join(home, ".gemini", "config", "agents", "{agent_name}", "agent.md"),
		"",
		"",
		"Available Agents",
		"> ● default  Default agent",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select / Toggle  k Kill Active Subagent  esc Go Back",
	})
	// The ● glyph is green even though the row itself is blue.
	if got := p.Lines()[6]; !strings.Contains(got, panelGreen34) {
		t.Errorf("agent bullet should be green: %q", got)
	}
}

// TestPanelAgentsRunningAgents — AGY 1.3.1: when the session has
// registered subagents, the /agents panel renders a subagent section
// that lists each by name with its StatusLine. An errored subagent
// shows "Error: <reason>" instead of "Done".
//
// We exercise three contracts:
//  1. The group header appears when there are subagents, and it is
//     the reference's own: `▸ Subagents (N running, M done)`
//     (`official-docs/cli/commands/agents.md:80`, and the reference's
//     own `%d running, %d done`, indexed in
//     out/local-macos-1.4.11/strings-index.txt — the full dump is retired).
//  2. A subagent that stopped on error renders as "Error: <reason>"
//     (not "Done") — the bug AGY 1.3.1 closed.
//  3. A successfully-completed subagent renders as "Done".
func TestPanelAgentsRunningAgents(t *testing.T) {
	mgr := subagent.NewManager()

	// Failed subagent: simulate a quota-style failure. We
	// don't need to actually exec a failing process — the
	// StatusLine mapping is the contract we're pinning down.
	failed := subagent.NewSubagent("explore-bot", "ls")
	failed.Wait(context.Background()) // no Start — error path
	// Force the errored state and reason directly so we don't
	// depend on the shell machinery in the unit test.
	markFailed(failed, "quota exceeded")
	mgr.Add(failed)

	// Healthy subagent: a successful exit leaves State="done"
	// with an empty LastError, which renders as "Done".
	done := subagent.NewSubagent("true")
	if err := done.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := done.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	mgr.Add(done)

	p := New(Seams{Subagents: func() []*subagent.Subagent { return mgr.List() }}, nil)
	plain := plainLines(p.Lines())

	// Header row: the reference's own group header, with the two
	// counts the reference's `%d running, %d done` carries. Both
	// subagents here have finished, so none of them is running.
	hasHeader := false
	for _, l := range plain {
		if l == "▸ Subagents (0 running, 2 done)" {
			hasHeader = true
			break
		}
	}
	if !hasHeader {
		t.Fatalf("expected the reference's group header, got: %v", plain)
	}

	// Failed row carries "Error:" and the reason.
	if !containsRow(plain, "explore-bot  Error: quota exceeded") {
		t.Fatalf("expected errored row with reason, got: %v", plain)
	}

	// Successful row carries "Done".
	if !containsRow(plain, "true  Done") {
		t.Fatalf("expected successful row with \"Done\", got: %v", plain)
	}

	// The errored row must NOT be displayed as "Done" — this is
	// the bug AGY 1.3.1 closed.
	for _, l := range plain {
		if strings.Contains(l, "explore-bot") && strings.HasSuffix(l, "  Done") {
			t.Fatalf("errored subagent rendered as Done: %q", l)
		}
	}
}

// TestCycle205AgentsEnterEcho: Enter on the default agent echoes
// "Already using default agent." and keeps the panel open
// (probe_agents.py).
func TestCycle205AgentsEnterEcho(t *testing.T) {
	var echoed []string
	switched := false
	p := New(Seams{
		Respond: func(msg string) { echoed = append(echoed, msg) },
		Switch:  func(name, body string) { switched = true },
	}, nil)
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if switched {
		t.Error("Enter on the active agent must not prepare a switch")
	}
	if p.Prepared() {
		t.Error("Enter on the active agent must not prepare a switch")
	}
	if len(echoed) != 1 || echoed[0] != "Already using default agent." {
		t.Errorf("echo = %q, want the default-agent message", echoed)
	}
	// Other keys stay inert.
	before := len(echoed)
	p.HandleKey(tea.Key{Text: "k"})
	if len(echoed) != before {
		t.Errorf("`k` wrote a response: %q", echoed[before:])
	}
}

// TestAgentsPrepareAndSwitch pins the reference's two-step switch: the
// Enter prepares it, the Esc applies it.
func TestAgentsPrepareAndSwitch(t *testing.T) {
	var echoed []string
	var gotName, gotBody string
	switched := false
	p := New(Seams{
		Respond: func(msg string) { echoed = append(echoed, msg) },
		Switch: func(name, body string) {
			gotName, gotBody = name, body
			switched = true
		},
	}, []agents.Agent{{Name: "reviewer", Description: "reviews", Body: "you review"}})

	// Down to the custom agent, then Enter.
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.Prepared() || p.PreparedName() != "reviewer" {
		t.Fatalf("prepared = %v, name = %q", p.Prepared(), p.PreparedName())
	}
	if len(echoed) != 1 || !strings.Contains(echoed[0], "Prepared selection: reviewer") {
		t.Errorf("echo = %q, want the prepared-selection message", echoed)
	}
	// Without a conversation the switch creates a new one.
	if !strings.Contains(echoed[0], "create a new conversation") {
		t.Errorf("echo = %q, want the new-conversation wording", echoed[0])
	}

	// Esc applies the prepared switch.
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !switched {
		t.Fatal("Esc did not apply the prepared switch")
	}
	if gotName != "reviewer" || gotBody != "you review" {
		t.Errorf("switch = (%q, %q), want the prepared agent", gotName, gotBody)
	}
}

// TestAgentsPrepareForkWording pins the other half of the prepared
// message: with a conversation in the session the switch forks it.
func TestAgentsPrepareForkWording(t *testing.T) {
	var echoed []string
	p := New(Seams{
		ActiveAgent: func() string { return "reviewer" },
		HasSession:  func() bool { return true },
		Respond:     func(msg string) { echoed = append(echoed, msg) },
	}, []agents.Agent{{Name: "reviewer", Description: "reviews", Body: "you review"}})
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if len(echoed) != 1 || !strings.Contains(echoed[0], "fork the current conversation") {
		t.Errorf("echo = %q, want the fork wording", echoed)
	}
	if !strings.Contains(echoed[0], "Prepared switch to default agent") {
		t.Errorf("echo = %q, want the default-switch message", echoed)
	}
}

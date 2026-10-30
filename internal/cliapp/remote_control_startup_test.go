package cliapp

import (
	"bytes"
	"context"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/rclink"
)

// TestRemoteControlStartup_InteractiveFlagOn verifies that starting the TUI
// with --remote-control populates the transcript with the remote-control card
// and enables remote control for the session.
func TestRemoteControlStartup_InteractiveFlagOn(t *testing.T) {
	m := slashRemoteControlRunMode(t)
	m.width = 120

	fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})
	setupTUIRemoteControl(m, fs)

	if !m.remoteControl {
		t.Fatal("expected m.remoteControl=true after setupTUIRemoteControl with --remote-control")
	}

	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "> /remote-control") {
		t.Errorf("expected command echo '> /remote-control' in transcript:\n%s", joined)
	}
	if !strings.Contains(joined, "Remote control on for this session.") {
		t.Errorf("expected 'Remote control on for this session.' in transcript:\n%s", joined)
	}
	if !strings.Contains(joined, "Hostname: ") {
		t.Errorf("expected 'Hostname: ' in transcript:\n%s", joined)
	}
	if !strings.Contains(joined, "Open on another device to take over:") {
		t.Errorf("expected take-over lead line in transcript:\n%s", joined)
	}

	instanceID := persistentrc.InstanceIDPrefix + m.remoteControlUUID + persistentrc.InstanceIDSuffix
	wantURL := rclink.BrowserURL(instanceID, "")
	if !strings.Contains(joined, wantURL) {
		t.Errorf("expected account chooser URL %q in transcript:\n%s", wantURL, joined)
	}

	viewText := m.View().Content
	if !strings.Contains(viewText, "> /remote-control") {
		t.Errorf("expected View().Content to contain '> /remote-control':\n%s", viewText)
	}
	if !strings.Contains(viewText, "Remote control on for this session.") {
		t.Errorf("expected View().Content to contain 'Remote control on for this session.':\n%s", viewText)
	}
}

// TestRemoteControlStartup_InteractiveFlagWithConversation verifies that when
// --remote-control and --conversation are present, the card uses the continuation
// prose and includes the conversation query in the URL.
func TestRemoteControlStartup_InteractiveFlagWithConversation(t *testing.T) {
	m := slashRemoteControlRunMode(t)
	m.width = 140
	m.conversationID = "01234567-89ab-cdef-0123-456789abcdef"

	fs := buildRemoteControlFlagSet(t, []string{"--remote-control", "--conversation", m.conversationID})
	setupTUIRemoteControl(m, fs)

	if !m.remoteControl {
		t.Fatal("expected m.remoteControl=true")
	}

	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "Open on another device to continue this conversation:") {
		t.Errorf("expected continuation lead line in transcript:\n%s", joined)
	}

	instanceID := persistentrc.InstanceIDPrefix + m.remoteControlUUID + persistentrc.InstanceIDSuffix
	wantURL := rclink.BrowserURL(instanceID, "p=c%2F"+m.conversationID)
	if !strings.Contains(joined, wantURL) {
		t.Errorf("expected URL %q with conversation query in transcript:\n%s", wantURL, joined)
	}
}

// TestRemoteControlStartup_InteractiveFlagWithContinue verifies that when
// --remote-control and --continue (or -c) are present, it resolves the prior
// conversation in the workspace and continues it in the remote-control card.
func TestRemoteControlStartup_InteractiveFlagWithContinue(t *testing.T) {
	wsDir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(wsDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	convID := "prior-conversation-uuid-99"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "hello"},
	}

	for _, flagArg := range []string{"--continue", "-c"} {
		t.Run(flagArg, func(t *testing.T) {
			m := slashRemoteControlRunMode(t)
			m.width = 140
			m.cwd = wsDir

			if err := convstore.SaveConversation(convID, wsDir, steps); err != nil {
				t.Fatalf("SaveConversation: %v", err)
			}

			fs := buildRemoteControlFlagSet(t, []string{"--remote-control", flagArg})
			setupTUIConversation(m, fs, wsDir)
			setupTUIRemoteControl(m, fs)

			if !m.remoteControl {
				t.Fatal("expected m.remoteControl=true")
			}
			if m.conversationID != convID {
				t.Fatalf("expected m.conversationID=%q, got %q", convID, m.conversationID)
			}

			joined := strings.Join(m.response, "\n")
			if !strings.Contains(joined, "Open on another device to continue this conversation:") {
				t.Errorf("expected continuation lead line in transcript:\n%s", joined)
			}

			instanceID := persistentrc.InstanceIDPrefix + m.remoteControlUUID + persistentrc.InstanceIDSuffix
			wantURL := rclink.BrowserURL(instanceID, "p=c%2F"+convID)
			if !strings.Contains(joined, wantURL) {
				t.Errorf("expected URL %q with conversation query in transcript:\n%s", wantURL, joined)
			}
		})
	}
}

// TestRemoteControlStartup_InteractiveFlagWithContinueEmptyWorkspace verifies that
// when --remote-control --continue is run in an empty workspace with no conversations,
// it falls back cleanly to the take-over lead without a conversation query.
func TestRemoteControlStartup_InteractiveFlagWithContinueEmptyWorkspace(t *testing.T) {
	wsDir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(wsDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	m := slashRemoteControlRunMode(t)
	m.width = 140
	m.cwd = wsDir

	fs := buildRemoteControlFlagSet(t, []string{"--remote-control", "--continue"})
	setupTUIConversation(m, fs, wsDir)
	setupTUIRemoteControl(m, fs)

	if !m.remoteControl {
		t.Fatal("expected m.remoteControl=true")
	}

	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "Open on another device to take over:") {
		t.Errorf("expected take-over lead line in transcript on empty workspace:\n%s", joined)
	}
	instanceID := persistentrc.InstanceIDPrefix + m.remoteControlUUID + persistentrc.InstanceIDSuffix
	wantURL := rclink.BrowserURL(instanceID, "")
	if !strings.Contains(joined, wantURL) {
		t.Errorf("expected account chooser URL %q without conversation query in transcript:\n%s", wantURL, joined)
	}
}

// TestRemoteControlStartup_InteractiveFlagOff verifies that when --remote-control
// is omitted or explicitly set to false, setupTUIRemoteControl is a no-op.
func TestRemoteControlStartup_InteractiveFlagOff(t *testing.T) {
	t.Run("omitted", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		fs := buildRemoteControlFlagSet(t, []string{})
		setupTUIRemoteControl(m, fs)

		if m.remoteControl {
			t.Error("expected m.remoteControl=false when flag is omitted")
		}
		if len(m.response) != 0 {
			t.Errorf("expected empty m.response, got: %v", m.response)
		}
	})

	t.Run("explicitly false", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		fs := buildRemoteControlFlagSet(t, []string{"--remote-control=false"})
		setupTUIRemoteControl(m, fs)

		if m.remoteControl {
			t.Error("expected m.remoteControl=false when --remote-control=false")
		}
		if len(m.response) != 0 {
			t.Errorf("expected empty m.response, got: %v", m.response)
		}
	})

	t.Run("nil args", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		setupTUIRemoteControl(m, nil)
		setupTUIRemoteControl(nil, &flag.FlagSet{})
		if m.remoteControl || len(m.response) != 0 {
			t.Error("expected no-op on nil arguments")
		}
	})
}

// TestRemoteControlStartup_InteractiveFlagUnderADC verifies that under ADC
// auth, setupTUIRemoteControl writes the ADC rejection message without enabling RC.
func TestRemoteControlStartup_InteractiveFlagUnderADC(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")

	m := slashRemoteControlRunMode(t)
	fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})
	setupTUIRemoteControl(m, fs)

	if m.remoteControl {
		t.Error("expected m.remoteControl=false under ADC")
	}

	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "Application Default Credentials") {
		t.Errorf("expected ADC rejection message in transcript:\n%s", joined)
	}
}

// TestRemoteControlStartup_NonInteractiveSnapshot verifies that renderTUIWithFlags
// includes the remote control card when --remote-control is passed.
func TestRemoteControlStartup_NonInteractiveSnapshot(t *testing.T) {
	t.Run("with remote-control", func(t *testing.T) {
		fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})
		var buf bytes.Buffer
		if err := renderTUIWithFlags(&buf, fs); err != nil {
			t.Fatalf("renderTUIWithFlags returned error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "> /remote-control") {
			t.Errorf("expected '> /remote-control' in snapshot:\n%s", out)
		}
		if !strings.Contains(out, "Remote control on for this session.") {
			t.Errorf("expected 'Remote control on for this session.' in snapshot:\n%s", out)
		}
		if !strings.Contains(out, "Hostname: ") {
			t.Errorf("expected 'Hostname: ' in snapshot:\n%s", out)
		}
		if !strings.Contains(out, "Open on another device to take over:") {
			t.Errorf("expected take over line in snapshot:\n%s", out)
		}
	})

	t.Run("with remote-control and conversation", func(t *testing.T) {
		fs := buildRemoteControlFlagSet(t, []string{"--remote-control", "--conversation", "snap-conv-42"})
		var buf bytes.Buffer
		if err := renderTUIWithFlags(&buf, fs); err != nil {
			t.Fatalf("renderTUIWithFlags returned error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "Open on another device to continue this conversation:") {
			t.Errorf("expected continue conversation in snapshot:\n%s", out)
		}
		if !strings.Contains(out, "snap-conv-42") {
			t.Errorf("expected conversation id in snapshot URL:\n%s", out)
		}
	})

	t.Run("with remote-control and continue flag", func(t *testing.T) {
		wsDir := t.TempDir()
		origWd, _ := os.Getwd()
		if err := os.Chdir(wsDir); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer func() { _ = os.Chdir(origWd) }()

		convID := "snap-continue-prior-conv-88"
		steps := []convstore.Step{
			{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "hi"},
		}
		if err := convstore.SaveConversation(convID, wsDir, steps); err != nil {
			t.Fatalf("SaveConversation: %v", err)
		}

		for _, flagArg := range []string{"--continue", "-c"} {
			fs := buildRemoteControlFlagSet(t, []string{"--remote-control", flagArg})
			var buf bytes.Buffer
			if err := renderTUIWithFlags(&buf, fs); err != nil {
				t.Fatalf("renderTUIWithFlags returned error: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, "Open on another device to continue this conversation:") {
				t.Errorf("%s: expected continue conversation in snapshot:\n%s", flagArg, out)
			}
			if !strings.Contains(out, convID) {
				t.Errorf("%s: expected %q in snapshot URL:\n%s", flagArg, convID, out)
			}
		}
	})

	t.Run("without remote-control", func(t *testing.T) {
		fs := buildRemoteControlFlagSet(t, []string{})
		var buf bytes.Buffer
		if err := renderTUIWithFlags(&buf, fs); err != nil {
			t.Fatalf("renderTUIWithFlags returned error: %v", err)
		}
		out := buf.String()
		if strings.Contains(out, "> /remote-control") {
			t.Errorf("unexpected '> /remote-control' in snapshot when flag omitted:\n%s", out)
		}
	})

	t.Run("under ADC", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "1")
		fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})
		var buf bytes.Buffer
		if err := renderTUIWithFlags(&buf, fs); err != nil {
			t.Fatalf("renderTUIWithFlags returned error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "> /remote-control") {
			t.Errorf("expected '> /remote-control' error banner in snapshot under ADC:\n%s", out)
		}
		if !strings.Contains(out, "Application Default Credentials") {
			t.Errorf("expected ADC message in snapshot:\n%s", out)
		}
	})
}

// TestRemoteControlStartup_InteractiveFlagDesktop verifies that starting the TUI
// with --remote-control and --desktop enables remote control and passes the desktop
// switch to StartOfficialDaemon so the service registers in desktop mode.
func TestRemoteControlStartup_InteractiveFlagDesktop(t *testing.T) {
	var daemonCalled bool
	var gotDesktop bool
	origStart := persistentrc.StartOfficialDaemon
	persistentrc.StartOfficialDaemon = func(ctx context.Context, name string, session, desktop bool) error {
		daemonCalled = true
		gotDesktop = desktop
		return nil
	}
	defer func() { persistentrc.StartOfficialDaemon = origStart }()

	m := slashRemoteControlRunMode(t)
	m.width = 120

	fs := buildRemoteControlFlagSet(t, []string{"--remote-control", "--desktop"})
	setupTUIRemoteControl(m, fs)

	if !m.remoteControl {
		t.Fatal("expected m.remoteControl=true after setupTUIRemoteControl with --remote-control --desktop")
	}
	if !m.remoteControlDesktop {
		t.Fatal("expected m.remoteControlDesktop=true")
	}

	for i := 0; i < 50; i++ {
		if daemonCalled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !daemonCalled {
		t.Fatal("expected StartOfficialDaemon to be called")
	}
	if !gotDesktop {
		t.Fatal("expected StartOfficialDaemon to be called with desktop=true")
	}
}

// TestRemoteControlStartup_DesktopFlagRetainedForSlashCommand verifies that starting
// with --desktop (without --remote-control) retains the desktop preference when the
// user later issues /remote-control interactively.
func TestRemoteControlStartup_DesktopFlagRetainedForSlashCommand(t *testing.T) {
	var daemonCalled bool
	var gotDesktop bool
	origStart := persistentrc.StartOfficialDaemon
	persistentrc.StartOfficialDaemon = func(ctx context.Context, name string, session, desktop bool) error {
		daemonCalled = true
		gotDesktop = desktop
		return nil
	}
	defer func() { persistentrc.StartOfficialDaemon = origStart }()

	m := slashRemoteControlRunMode(t)
	m.width = 120

	fs := buildRemoteControlFlagSet(t, []string{"--desktop"})
	setupTUIRemoteControl(m, fs)

	if m.remoteControl {
		t.Fatal("expected m.remoteControl=false when --remote-control flag was omitted")
	}
	if !m.remoteControlDesktop {
		t.Fatal("expected m.remoteControlDesktop=true when --desktop was provided")
	}

	handled, _ := m.handleSlashCommand("/remote-control")
	if !handled {
		t.Fatal("expected /remote-control to be handled")
	}
	if !m.remoteControl {
		t.Fatal("expected m.remoteControl=true after /remote-control")
	}

	for i := 0; i < 50; i++ {
		if daemonCalled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !daemonCalled {
		t.Fatal("expected StartOfficialDaemon to be called")
	}
	if !gotDesktop {
		t.Fatal("expected StartOfficialDaemon to be called with desktop=true from retained flag")
	}
}

// TestRemoteControlStartup_RunRootIntegration verifies that top-level CLI execution
// with --remote-control behaves identically to reference agy.
func TestRemoteControlStartup_RunRootIntegration(t *testing.T) {
	t.Run("ADC flag validation exits with code 3", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "1")
		p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
			"pigasus", "recovered", "recovered", cli.ExtraFlags()...)
		p.Root().RootRun = func(args []string) error { return runRoot(args, p) }

		code := p.Root().Parse([]string{"--remote-control"})
		if code != 3 {
			t.Fatalf("Parse = %d; want 3 (AGY_ERROR under ADC)", code)
		}
	})

	t.Run("non-interactive runRoot prints remote-control card", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		var out bytes.Buffer
		p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
			"pigasus", "recovered", "recovered", cli.ExtraFlags()...)
		p.Root().Out = &out
		p.Root().RootRun = func(args []string) error { return runRoot(args, p) }

		if code := p.Root().Parse([]string{"--remote-control"}); code != 0 {
			t.Fatalf("Parse = %d; want 0", code)
		}
		if err := runRoot(nil, p); err != nil {
			t.Fatalf("runRoot error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "> /remote-control") {
			t.Errorf("expected '> /remote-control' in output:\n%s", got)
		}
		if !strings.Contains(got, "Remote control on for this session.") {
			t.Errorf("expected 'Remote control on for this session.' in output:\n%s", got)
		}
		if !strings.Contains(got, "Open on another device to take over:") {
			t.Errorf("expected take over prose in output:\n%s", got)
		}
	})

	t.Run("non-interactive runRoot accepts --remote-control with --desktop", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		var out bytes.Buffer
		p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
			"pigasus", "recovered", "recovered", cli.ExtraFlags()...)
		p.Root().Out = &out
		p.Root().RootRun = func(args []string) error { return runRoot(args, p) }

		if code := p.Root().Parse([]string{"--remote-control", "--desktop"}); code != 0 {
			t.Fatalf("Parse = %d; want 0", code)
		}
		if err := runRoot(nil, p); err != nil {
			t.Fatalf("runRoot error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "> /remote-control") {
			t.Errorf("expected '> /remote-control' in output:\n%s", got)
		}
		if !strings.Contains(got, "Remote control on for this session.") {
			t.Errorf("expected 'Remote control on for this session.' in output:\n%s", got)
		}
	})
}

// TestRemoteControlStartupAndSlashParity pins parity between starting with
// --remote-control and invoking /remote-control in the session.
func TestRemoteControlStartupAndSlashParity(t *testing.T) {
	t.Run("fresh session without prior conversation", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		m.width = 140

		// 1. Startup with --remote-control
		fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})
		setupTUIRemoteControl(m, fs)
		startupLines := append([]string(nil), m.response...)
		startupJoined := strings.Join(startupLines, "\n")

		if !strings.Contains(startupJoined, "Open on another device to take over:") {
			t.Fatalf("startup expected 'take over':\n%s", startupJoined)
		}
		instanceID := persistentrc.InstanceIDPrefix + m.remoteControlUUID + persistentrc.InstanceIDSuffix
		wantURL := rclink.BrowserURL(instanceID, "")
		if !strings.Contains(startupJoined, wantURL) {
			t.Fatalf("startup expected URL %q:\n%s", wantURL, startupJoined)
		}

		// 2. Clear response and run /remote-control before any user prompt
		m.response = nil
		handled, _ := m.handleSlashCommand("/remote-control")
		if !handled {
			t.Fatal("expected /remote-control handled=true")
		}
		slashLines := append([]string(nil), m.response...)
		slashJoined := strings.Join(slashLines, "\n")

		if !strings.Contains(slashJoined, "Open on another device to take over:") {
			t.Fatalf("slash expected 'take over':\n%s", slashJoined)
		}
		if !strings.Contains(slashJoined, wantURL) {
			t.Fatalf("slash expected URL %q:\n%s", wantURL, slashJoined)
		}

		// 3. Both outputs should have identical cards
		if startupJoined != slashJoined {
			t.Errorf("URL/prose mismatch between startup and slash command:\n--- startup ---\n%s\n--- slash ---\n%s", startupJoined, slashJoined)
		}

		// 4. After user submits a prompt, /remote-control should continue the conversation
		m.startModelTurn("hello")
		m.response = nil
		handled, _ = m.handleSlashCommand("/remote-control")
		if !handled {
			t.Fatal("expected /remote-control handled=true")
		}
		activeSlashJoined := strings.Join(m.response, "\n")
		if !strings.Contains(activeSlashJoined, "Open on another device to continue this conversation:") {
			t.Errorf("expected 'continue this conversation' after turn started:\n%s", activeSlashJoined)
		}
		wantActiveURL := rclink.BrowserURL(instanceID, "p=c%2F"+m.conversationID)
		if !strings.Contains(activeSlashJoined, wantActiveURL) {
			t.Errorf("expected active conversation URL %q in:\n%s", wantActiveURL, activeSlashJoined)
		}
	})

	t.Run("session started with conversation flag", func(t *testing.T) {
		m := slashRemoteControlRunMode(t)
		m.width = 140
		convID := "explicit-conv-uuid-1234"
		m.conversationID = convID

		fs := buildRemoteControlFlagSet(t, []string{"--remote-control", "--conversation", convID})
		setupTUIRemoteControl(m, fs)
		startupJoined := strings.Join(m.response, "\n")

		m.response = nil
		m.handleSlashCommand("/remote-control")
		slashJoined := strings.Join(m.response, "\n")

		if startupJoined != slashJoined {
			t.Errorf("URL/prose mismatch with --conversation:\n--- startup ---\n%s\n--- slash ---\n%s", startupJoined, slashJoined)
		}
		if !strings.Contains(slashJoined, "Open on another device to continue this conversation:") {
			t.Errorf("expected 'continue this conversation' in:\n%s", slashJoined)
		}
	})
}


package harnessprompt

import (
	"os"
	"strings"
	"testing"
)

// The goldens are the system instructions the reference `localharness`
// binary sent when it was pointed at a local fake Gemini endpoint. They
// are byte-for-byte captures; see `out/localharness/captures/`.
const (
	goldenAppDataDir   = "/Users/gear/.gemini/antigravity"
	goldenConversation = "cascade-conformance-000000000000000"
	goldenOSName       = "darwin"
)

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(data)
}

func TestBuildMatchesDefaultGolden(t *testing.T) {
	got := Build(Options{
		AppDataDir:     goldenAppDataDir,
		ConversationID: goldenConversation,
		OSName:         goldenOSName,
		MessageTool:    true,
	})
	want := readGolden(t, "system_instruction_default.txt")
	if got != want {
		t.Fatalf("default prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildMatchesWorkspaceGolden(t *testing.T) {
	got := Build(Options{
		Workspaces:     []string{"/tmp/wsA", "/tmp/wsB"},
		AppDataDir:     goldenAppDataDir,
		ConversationID: goldenConversation,
		OSName:         goldenOSName,
		MessageTool:    true,
	})
	want := readGolden(t, "system_instruction_workspace.txt")
	if got != want {
		t.Fatalf("workspace prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildMatchesInteractiveGolden(t *testing.T) {
	got := Build(Options{
		Behavior:       BehaviorInteractive,
		Workspaces:     []string{"/tmp/wsA"},
		AppDataDir:     "/tmp/appdata",
		ConversationID: goldenConversation,
		OSName:         goldenOSName,
		MessageTool:    true,
	})
	want := readGolden(t, "system_instruction_interactive.txt")
	if got != want {
		t.Fatalf("interactive prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The subagent golden is the reference's own prompt with the subagent
// tool on: it carries the `<subagents>` section and the messaging
// section counts agents among the message sources.
func TestBuildMatchesSubagentGolden(t *testing.T) {
	got := Build(Options{
		AppDataDir:     goldenAppDataDir,
		ConversationID: goldenConversation,
		OSName:         goldenOSName,
		SubagentTool:   true,
		MessageTool:    true,
		Subagents:      BuiltinSubagents(),
	})
	want := readGolden(t, "system_instruction_subagents.txt")
	if got != want {
		t.Fatalf("subagent prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The messaging section is the reference's own template: the two flags
// decide which sources it lists.
func TestMessagingSourcesFollowTheFlags(t *testing.T) {
	cases := []struct {
		subagent bool
		message  bool
		want     string
	}{
		{false, false, "messages from: background tasks."},
		{true, false, "messages from: agents, background tasks."},
		{false, true, "messages from: background tasks, user-queued messages."},
		{true, true, "messages from: agents, background tasks, user-queued messages."},
	}
	for _, tc := range cases {
		got := Build(Options{
			AppDataDir:     goldenAppDataDir,
			ConversationID: goldenConversation,
			OSName:         goldenOSName,
			SubagentTool:   tc.subagent,
			MessageTool:    tc.message,
			Subagents:      BuiltinSubagents(),
		})
		if !strings.Contains(got, tc.want) {
			t.Fatalf("subagent=%v message=%v: prompt does not say %q:\n%s", tc.subagent, tc.message, tc.want, got)
		}
	}
}

func TestAppendedMatchesGolden(t *testing.T) {
	got := Appended(
		Options{
			Identity:       "I AM A ROBOT",
			AppDataDir:     goldenAppDataDir,
			ConversationID: goldenConversation,
			OSName:         goldenOSName,
			MessageTool:    true,
		},
		[]AppendedSection{{Title: "my_section", Content: "SECTION BODY"}},
	)
	want := readGolden(t, "system_instruction_appended.txt")
	if got != want {
		t.Fatalf("appended prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCustomMatchesGolden(t *testing.T) {
	got, err := Custom(
		Options{
			AppDataDir:     goldenAppDataDir,
			ConversationID: goldenConversation,
			OSName:         goldenOSName,
		},
		[]CustomPart{{Text: "CUSTOM TEXT ONE"}, {BuiltinSection: SectionIdentity}},
	)
	if err != nil {
		t.Fatalf("custom: %v", err)
	}
	want := readGolden(t, "system_instruction_custom.txt")
	if got != want {
		t.Fatalf("custom prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCustomUnknownSection(t *testing.T) {
	_, err := Custom(Options{}, []CustomPart{{BuiltinSection: "no_such_section"}})
	if err == nil {
		t.Fatalf("expected an error for an unknown builtin section")
	}
	if !strings.Contains(err.Error(), "unknown builtin prompt section") {
		t.Fatalf("error = %v", err)
	}
}

func TestIdentityOverride(t *testing.T) {
	got := Build(Options{
		Identity:       "I AM A ROBOT",
		AppDataDir:     goldenAppDataDir,
		ConversationID: goldenConversation,
		OSName:         goldenOSName,
	})
	if !strings.Contains(got, "<identity>\nI AM A ROBOT\n</identity>") {
		t.Fatalf("identity override missing:\n%s", got)
	}
}

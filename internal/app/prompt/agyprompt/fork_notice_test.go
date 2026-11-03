package agyprompt

import (
	"strings"
	"testing"
)

func TestRenderForkNotice_AllFields(t *testing.T) {
	data := ForkNoticeData{
		SourceCascadeID:  "source-uuid-1234",
		OldArtifactDir:   "/old/artifact/path",
		NewArtifactDir:   "/new/artifact/path",
		ArtifactNote:     "Note: All existing files were migrated.",
		Notes:            []string{"Custom note 1", "Custom note 2"},
		DroppedSubagents: true,
		CanceledTasks:    true,
	}

	out, err := RenderForkNotice(data)
	if err != nil {
		t.Fatalf("RenderForkNotice failed: %v", err)
	}

	expectedSubstrings := []string{
		"<fork_notice>",
		"</fork_notice>",
		"This conversation was branched from a previous conversation (ID: source-uuid-1234).",
		"- Old artifact directory (referenced in past steps): /old/artifact/path",
		"- New artifact directory (use for all new work): /new/artifact/path",
		"Note: All existing files were migrated.",
		"Custom note 1",
		"Custom note 2",
		"The subagents invoked in the history above are NOT yours",
		"The background tasks in the history above are NOT yours",
		"IMPORTANT: For all future file edits, artifact creation, and run_command invocations, you MUST use the new artifact directory.",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(out, sub) {
			t.Errorf("expected output to contain %q, but got:\n%s", sub, out)
		}
	}
}

func TestRenderForkNotice_NoFlags(t *testing.T) {
	data := ForkNoticeData{
		SourceCascadeID:  "source-uuid-5678",
		OldArtifactDir:   "/old/dir",
		NewArtifactDir:   "/new/dir",
		DroppedSubagents: false,
		CanceledTasks:    false,
	}

	out, err := RenderForkNotice(data)
	if err != nil {
		t.Fatalf("RenderForkNotice failed: %v", err)
	}

	if strings.Contains(out, "The subagents invoked in the history above are NOT yours") {
		t.Errorf("did not expect subagents note when DroppedSubagents is false")
	}
	if strings.Contains(out, "The background tasks in the history above are NOT yours") {
		t.Errorf("did not expect tasks note when CanceledTasks is false")
	}
	if !strings.Contains(out, "source-uuid-5678") {
		t.Errorf("expected source cascade ID to be present")
	}
}

func TestRenderForkNotice_OnlyDroppedSubagents(t *testing.T) {
	data := ForkNoticeData{
		SourceCascadeID:  "src-subagents-only",
		OldArtifactDir:   "/old",
		NewArtifactDir:   "/new",
		DroppedSubagents: true,
		CanceledTasks:    false,
	}

	out, err := RenderForkNotice(data)
	if err != nil {
		t.Fatalf("RenderForkNotice failed: %v", err)
	}

	if !strings.Contains(out, "The subagents invoked in the history above are NOT yours") {
		t.Errorf("expected subagents note when DroppedSubagents is true")
	}
	if strings.Contains(out, "The background tasks in the history above are NOT yours") {
		t.Errorf("did not expect tasks note when CanceledTasks is false")
	}
}

func TestRenderForkNotice_OnlyCanceledTasks(t *testing.T) {
	data := ForkNoticeData{
		SourceCascadeID:  "src-tasks-only",
		OldArtifactDir:   "/old",
		NewArtifactDir:   "/new",
		DroppedSubagents: false,
		CanceledTasks:    true,
	}

	out, err := RenderForkNotice(data)
	if err != nil {
		t.Fatalf("RenderForkNotice failed: %v", err)
	}

	if strings.Contains(out, "The subagents invoked in the history above are NOT yours") {
		t.Errorf("did not expect subagents note when DroppedSubagents is false")
	}
	if !strings.Contains(out, "The background tasks in the history above are NOT yours") {
		t.Errorf("expected tasks note when CanceledTasks is true")
	}
}

func TestRenderForkNotice_EmptyNotes(t *testing.T) {
	data := ForkNoticeData{
		SourceCascadeID: "src-empty",
		OldArtifactDir:  "/old",
		NewArtifactDir:  "/new",
	}

	out, err := RenderForkNotice(data)
	if err != nil {
		t.Fatalf("RenderForkNotice failed: %v", err)
	}

	if !strings.HasPrefix(out, "<fork_notice>\n") {
		t.Errorf("expected output to start with '<fork_notice>\\n'")
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "</fork_notice>") {
		t.Errorf("expected output to end with '</fork_notice>'")
	}
}

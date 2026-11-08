package subagenttools

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestSnapshotSection_MatchesTheReferenceEntry pins the subagent section the
// 1.5.3 checkpoint at step 7414 carried: spec.typeName/role, the result's
// conversation id, log URI and workspace list, and the spawning step.
func TestSnapshotSection_MatchesTheReferenceEntry(t *testing.T) {
	reg := NewRegistry("parent-conv")
	child := NewChild("b1923c8b-d44e-4b8a-a368-29b54434bee4", "DeepCoder",
		"Subagent & Task TUI Alignment Engineer", "/Users/gear/GitHub/openagy", nil)
	child.LogAbsoluteURI = "file:///brain/b1923c8b/.system_generated/logs/transcript.jsonl"
	child.SpawnStepIndex = 7363
	child.StartedAt = time.Date(2026, 11, 3, 4, 5, 44, 0, time.UTC)
	reg.Add(child)

	section := reg.SnapshotSection(DefaultMaxSubagentSnapshots)
	if section == nil {
		t.Fatal("SnapshotSection = nil")
	}
	if section.Total != 1 || section.Max != 20 {
		t.Errorf("section counts = total %d max %d, want 1/20", section.Total, section.Max)
	}
	if len(section.Entries) != 1 {
		t.Fatalf("entries = %+v, want one", section.Entries)
	}
	ref := section.Entries[0]
	if ref.TypeName != "DeepCoder" || ref.Role != "Subagent & Task TUI Alignment Engineer" {
		t.Errorf("spec = %+v", ref)
	}
	if ref.ConversationID != "b1923c8b-d44e-4b8a-a368-29b54434bee4" {
		t.Errorf("conversationId = %q", ref.ConversationID)
	}
	if ref.LogAbsoluteURI != "file:///brain/b1923c8b/.system_generated/logs/transcript.jsonl" {
		t.Errorf("logAbsoluteUri = %q", ref.LogAbsoluteURI)
	}
	if len(ref.WorkspaceURIs) != 1 || ref.WorkspaceURIs[0] != "file:///Users/gear/GitHub/openagy" {
		t.Errorf("workspaceUris = %+v", ref.WorkspaceURIs)
	}
	if ref.SpawnStepIndex != 7363 {
		t.Errorf("spawnStepIndex = %d", ref.SpawnStepIndex)
	}
	if ref.Killed {
		t.Error("killed = true, want false")
	}
}

// TestSnapshotSection_CapsToTheMostRecent pins the 20-snapshot cap: the total
// stays the real spawn count and only the newest children are listed.
func TestSnapshotSection_CapsToTheMostRecent(t *testing.T) {
	reg := NewRegistry("parent-conv")
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("child-%02d", i)
		child := NewChild(id, "DeepCoder", "worker", "/tmp/wt", nil)
		child.SpawnStepIndex = int32(i + 1)
		child.StartedAt = time.Date(2026, 11, 3, 4, 5, i, 0, time.UTC)
		reg.Add(child)
	}
	section := reg.SnapshotSection(DefaultMaxSubagentSnapshots)
	if section == nil {
		t.Fatal("SnapshotSection = nil")
	}
	if section.Total != 25 {
		t.Errorf("total = %d, want 25", section.Total)
	}
	if len(section.Entries) != 20 {
		t.Fatalf("entries = %d, want 20", len(section.Entries))
	}
	if section.Entries[0].ConversationID != "child-05" {
		t.Errorf("first kept = %q, want child-05", section.Entries[0].ConversationID)
	}
	if section.Entries[19].ConversationID != "child-24" {
		t.Errorf("last kept = %q, want child-24", section.Entries[19].ConversationID)
	}
}

// TestSnapshotSection_EmptyRegistry keeps an empty conversation from carrying
// an empty section.
func TestSnapshotSection_EmptyRegistry(t *testing.T) {
	if got := NewRegistry("parent").SnapshotSection(20); got != nil {
		t.Fatalf("SnapshotSection = %+v, want nil", got)
	}
	var reg *Registry
	if got := reg.SnapshotSection(20); got != nil {
		t.Fatalf("nil registry SnapshotSection = %+v, want nil", got)
	}
}

// TestParentContextLines pins the parent section's entries: the task first,
// then the later parent messages, all in the reference's one-line form.
func TestParentContextLines(t *testing.T) {
	p := &ParentContext{
		ID:     "53dc2d4d-166f-4082-9dce-7ca95818e00d",
		Task:   "<original_task>\nTask: hello",
		TaskAt: time.Date(2026, 11, 3, 21, 30, 22, 0, time.UTC),
	}
	p.Record("more context", time.Date(2026, 11, 3, 21, 31, 0, 0, time.UTC))
	// A repeated task must not be listed twice.
	p.Record("<original_task>\nTask: hello", time.Date(2026, 11, 3, 21, 32, 0, 0, time.UTC))

	lines, mostRecent := p.Lines()
	if mostRecent {
		t.Error("mostRecent = true for two lines")
	}
	want := []string{
		"[Message] timestamp=2026-11-03T21:30:22Z sender=53dc2d4d-166f-4082-9dce-7ca95818e00d priority=MESSAGE_PRIORITY_HIGH content=<original_task>\nTask: hello",
		"[Message] timestamp=2026-11-03T21:31:00Z sender=53dc2d4d-166f-4082-9dce-7ca95818e00d priority=MESSAGE_PRIORITY_HIGH content=more context",
	}
	if len(lines) != len(want) {
		t.Fatalf("Lines = %+v, want %d lines", lines, len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("Lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

// TestParentContextLinesCap pins the ten-entry cap and the "most recent"
// signal.
func TestParentContextLinesCap(t *testing.T) {
	p := &ParentContext{ID: "parent"}
	for i := 0; i < 12; i++ {
		p.Record(fmt.Sprintf("m%d", i), time.Date(2026, 11, 3, 21, 30, i, 0, time.UTC))
	}
	lines, mostRecent := p.Lines()
	if !mostRecent {
		t.Error("mostRecent = false, want true")
	}
	if len(lines) != MaxParentMessages {
		t.Fatalf("len(Lines) = %d, want %d", len(lines), MaxParentMessages)
	}
	if !strings.Contains(lines[len(lines)-1], "content=m11") {
		t.Errorf("last line = %q, want the newest message", lines[len(lines)-1])
	}
}

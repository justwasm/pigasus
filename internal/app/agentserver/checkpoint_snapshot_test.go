// checkpoint_snapshot_test.go — the server host supplies the same live
// sections the TUI host does.
package agentserver

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/subagenttools"
)

// TestCheckpointSnapshotCarriesTheSubagentRoster pins that a checkpoint
// written by the server remembers the subagents the turn spawned.
//
// The server and the TUI are two hosts for the same engine, and a checkpoint
// is the only thing that survives a compaction. A host that forgot the roster
// would lose every live subagent's type name, role and transcript URI at the
// moment the transcript is truncated — while the other host kept them.
func TestCheckpointSnapshotCarriesTheSubagentRoster(t *testing.T) {
	const convID = "server-host-conv"
	reg := subagenttools.NewRegistry(convID)
	child := subagenttools.NewChild("child-conv-id", "DeepCoder", "the worker role", "/tmp/wt", nil)
	child.LogAbsoluteURI = "file:///brain/child-conv-id/transcript.jsonl"
	child.SpawnStepIndex = 12
	reg.Add(child)

	s := &Session{ConversationID: convID, SubagentRegistry: reg}
	snap := s.checkpointSnapshot()

	if snap.Subagents == nil {
		t.Fatal("Subagents = nil: the server dropped the subagent roster")
	}
	if snap.Subagents.Total != 1 || len(snap.Subagents.Entries) != 1 {
		t.Fatalf("roster = total %d entries %d, want 1/1", snap.Subagents.Total, len(snap.Subagents.Entries))
	}
	entry := snap.Subagents.Entries[0]
	if entry.TypeName != "DeepCoder" || entry.Role != "the worker role" {
		t.Errorf("entry spec = %+v", entry)
	}
	if entry.ConversationID != "child-conv-id" {
		t.Errorf("conversationId = %q", entry.ConversationID)
	}
	if entry.LogAbsoluteURI != "file:///brain/child-conv-id/transcript.jsonl" {
		t.Errorf("logAbsoluteUri = %q", entry.LogAbsoluteURI)
	}
}

// TestCheckpointSnapshotIgnoresAForeignRegistry pins that a registry owned by
// another conversation cannot leak its roster into this one's checkpoint.
//
// The TUI host applies the same guard: a checkpoint may only describe
// subagents spawned by the conversation it belongs to.
func TestCheckpointSnapshotIgnoresAForeignRegistry(t *testing.T) {
	reg := subagenttools.NewRegistry("someone-elses-conv")
	reg.Add(subagenttools.NewChild("other-child", "DeepCoder", "not mine", "/tmp/wt", nil))

	s := &Session{ConversationID: "my-conv", SubagentRegistry: reg}
	if got := s.checkpointSnapshot(); got.Subagents != nil {
		t.Fatalf("Subagents = %+v, want nil for a foreign registry", got.Subagents)
	}
}

// TestCheckpointSnapshotWithoutARegistry pins the nil-safe path: a session
// with no subagent registry still produces a usable snapshot.
func TestCheckpointSnapshotWithoutARegistry(t *testing.T) {
	s := &Session{ConversationID: "no-subagents-conv"}
	snap := s.checkpointSnapshot()
	if snap.Subagents != nil {
		t.Fatalf("Subagents = %+v, want nil", snap.Subagents)
	}
}

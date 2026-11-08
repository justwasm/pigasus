// Subagent snapshots for the checkpoint's resume block.
//
// 【一手】 agy 1.5.3, conversation 43966145-…, checkpoint at step 7414: the
// "# Subagents" section lists every subagent spawned during the conversation
// as a pretty-printed protojson SubagentSnapshot. Only spec.typeName,
// spec.role, result.conversationId, result.logAbsoluteUri,
// result.workspaceUris and spawnStepIndex appeared; the reference omits the
// initial prompt and every zero value.

package subagenttools

import (
	"sort"

	"github.com/justwasm/pigasus/internal/core/compaction"
)

// DefaultMaxSubagentSnapshots is the reference's cap on the section.
//
// 【反编译】 GetDefaultCheckpointConfigForModel sets max_subagent_snapshots
// to 20.
const DefaultMaxSubagentSnapshots = 20

// SnapshotSection builds the checkpoint's "# Subagents" section, or nil when
// the registry has no children.
//
// Every spawned child is listed, not just the running ones: the captured
// checkpoint at step 7414 listed a child long after its turn ended. When the
// roster is longer than max, only the most recent children are kept and the
// section's header reports the total.
func (r *Registry) SnapshotSection(max int) *compaction.SubagentSection {
	children := r.List()
	if len(children) == 0 {
		return nil
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].SpawnStepIndex != children[j].SpawnStepIndex {
			return children[i].SpawnStepIndex < children[j].SpawnStepIndex
		}
		if !children[i].StartedAt.Equal(children[j].StartedAt) {
			return children[i].StartedAt.Before(children[j].StartedAt)
		}
		return children[i].ConversationID < children[j].ConversationID
	})
	if max <= 0 {
		max = DefaultMaxSubagentSnapshots
	}
	total := len(children)
	if len(children) > max {
		children = children[len(children)-max:]
	}
	refs := make([]compaction.SubagentRef, 0, len(children))
	for _, child := range children {
		if child == nil {
			continue
		}
		ref := compaction.SubagentRef{
			TypeName:       child.TypeName,
			Role:           child.Role,
			ConversationID: child.ConversationID,
			LogAbsoluteURI: child.LogAbsoluteURI,
			Killed:         child.State() == StateStopped,
			SpawnStepIndex: child.SpawnStepIndex,
		}
		if child.LogAbsoluteURI == "" {
			if dir := child.SubDir(); dir != "" {
				ref.LogAbsoluteURI = fileURI(dir + "/" + child.ConversationID + ".jsonl")
			}
		}
		if child.Worktree != "" {
			ref.WorkspaceURIs = []string{fileURI(child.Worktree)}
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return nil
	}
	return &compaction.SubagentSection{Total: total, Max: max, Entries: refs}
}

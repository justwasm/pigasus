// Package artifacts reads the artifacts a conversation wrote into its brain
// directory.
//
// 【一手】 agy 1.5.3 keeps the conversation's artifacts at
// <appDataDir>/brain/<conversation-id>/<name>.md next to a
// <name>.md.metadata.json sidecar:
//
//	{
//	  "summary": "…",
//	  "updatedAt": "2026-11-07T13:55:31.228605Z",
//	  "userFacing": true
//	}
//
// The checkpoint's resume block lists one entry per artifact, in name order:
//
//	[ARTIFACT: <stem>]
//	Path: file:///<appDataDir>/brain/<conversation-id>/<name>.md
//	Last Edited: 2026-11-07T13:55:31Z
//
// A file without the sidecar was not written through the artifact layer, so it
// is not listed. The clone has no artifact writer yet; this package is the
// read side, so a checkpoint still reports the artifacts a session inherited
// from agy or from a forked conversation.
package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/core/confhome"
)

// MetadataSuffix names the sidecar the artifact layer writes.
const MetadataSuffix = ".metadata.json"

// Dir is the conversation's artifact directory.
func Dir(conversationID string) string {
	if conversationID == "" {
		return ""
	}
	return filepath.Join(confhome.Antigravity(), "brain", conversationID)
}

// metadata is the sidecar's shape.
//
// updatedAt is the artifact's last-edit time; userFacing says whether the
// artifact is shown to the user (and whether it belongs in the list). The
// reference's other keys (summary, requestFeedback) do not appear in the
// checkpoint's entry.
type metadata struct {
	UpdatedAt  time.Time `json:"updatedAt"`
	UserFacing *bool     `json:"userFacing"`
}

// List returns the conversation's artifacts in name order.
//
// A sidecar that cannot be read or parsed is skipped: a half-written artifact
// must not take the whole checkpoint down.
func List(conversationID string) []compaction.ArtifactRef {
	dir := Dir(conversationID)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	refs := make([]compaction.ArtifactRef, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, MetadataSuffix) {
			continue
		}
		meta, ok := readMetadata(filepath.Join(dir, name+MetadataSuffix))
		if !ok {
			continue
		}
		if meta.UserFacing != nil && !*meta.UserFacing {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		refs = append(refs, compaction.ArtifactRef{
			Name:       stem,
			Path:       "file://" + filepath.Join(dir, name),
			LastEdited: meta.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs
}

// readMetadata reads and parses one sidecar.
func readMetadata(path string) (metadata, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata{}, false
	}
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{}, false
	}
	if meta.UpdatedAt.IsZero() {
		return metadata{}, false
	}
	return meta, true
}

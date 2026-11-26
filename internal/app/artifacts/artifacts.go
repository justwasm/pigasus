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
// is not listed. This package owns both halves: List reads the sidecar,
// WriteMetadata writes it, and IsArtifactFile decides which writes are
// artifacts in the first place (the reference's isActualArtifactFile). The
// write tools call the hook the hosts install; shell and script writes bypass
// it, which is exactly the reference's own rule ("not shell or Python
// scripts, which skip ArtifactMetadata").
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
//
// 【一手】 this is the path artifacts.tmpl tells the model to write to
// ("All artifacts should be written to the artifact directory:
// {{ArtifactDirectoryPath}}"), and it is NOT <antigravityHome>/artifacts —
// that directory belongs to the terminal sandbox's allow-list and is
// unrelated. See docs/guide/compaction.md §"artifacts.List".
func Dir(conversationID string) string {
	if conversationID == "" {
		return ""
	}
	return filepath.Join(confhome.Antigravity(), "brain", conversationID)
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
func readMetadata(path string) (Metadata, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, false
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, false
	}
	if meta.UpdatedAt.IsZero() {
		return Metadata{}, false
	}
	return meta, true
}

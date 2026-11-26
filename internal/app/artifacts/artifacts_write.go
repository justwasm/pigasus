package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Metadata is the sidecar's shape — the on-disk JSON form of
// codeium_common_pb.ArtifactMetadata in the reference's protojson
// camelCase.
//
// 【一手】 the reference names exactly these keys to the model:
// artifacts.tmpl tells it to set `ArtifactMetadata.UserFacing`, and
// generative_ui/SKILL.md tells it to pass "UserFacing: true and
// RequestFeedback: false in ArtifactMetadata". `updatedAt` is what the
// checkpoint's resume entry reads back. The remaining proto fields
// (artifactType, version) are not carried by the sidecar the reference
// writes for a markdown artifact, so they stay out of this struct.
type Metadata struct {
	Summary         string    `json:"summary,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
	UserFacing      *bool     `json:"userFacing,omitempty"`
	RequestFeedback *bool     `json:"requestFeedback,omitempty"`
}

// MetadataPath is <artifact>.metadata.json — the reference's
// getArtifactMetadataPath (recovered 0x7248de0).
func MetadataPath(path string) string { return path + MetadataSuffix }

// WriteMetadata writes the sidecar the artifact layer owns.
//
// This is the writer the clone lacked. Without it every file the model puts
// in the artifact directory stays invisible: List treats "no sidecar" as
// "not an artifact", so the file never reaches the checkpoint's resume
// block, the /artifact panel, or the status line's artifact count.
//
// An empty UpdatedAt is filled with the current time — a write is an edit.
// An existing sidecar's keys are preserved when the new metadata leaves
// them unset, so an edit that omits summary does not erase it.
func WriteMetadata(path string, m Metadata) error {
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = time.Now().UTC()
	}
	if prev, ok := readMetadata(MetadataPath(path)); ok {
		if m.Summary == "" {
			m.Summary = prev.Summary
		}
		if m.UserFacing == nil {
			m.UserFacing = prev.UserFacing
		}
		if m.RequestFeedback == nil {
			m.RequestFeedback = prev.RequestFeedback
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(MetadataPath(path), append(b, '\n'), 0o644)
}

// artifactExtensions are the artifact kinds the reference's prompt layer
// names: markdown documents (artifacts.tmpl), generative-UI HTML widgets
// (generative_ui/SKILL.md), and URL bookmarks (.url.json under the
// enable-url-artifacts experiment). Longest first so ".url.json" is not
// shadowed by a shorter match.
var artifactExtensions = []string{".url.json", ".html", ".md"}

// IsArtifactFile reports whether path is a file the artifact layer owns —
// the reference's isActualArtifactFile plus isSkippedArtifactPath.
//
// It qualifies when it sits DIRECTLY in the conversation's artifact
// directory (so brain/<conv>/scratch/ and brain/<conv>/.system_generated/
// are excluded), is not hidden, is not itself a sidecar, and carries an
// artifact extension. Returning false means "not an artifact", so the host
// must not write a sidecar for it.
func IsArtifactFile(conversationID, path string) bool {
	dir := Dir(conversationID)
	if dir == "" || path == "" {
		return false
	}
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(dir) {
		return false
	}
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, MetadataSuffix) {
		return false
	}
	for _, ext := range artifactExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// EnsureDir creates the conversation's artifact directory.
//
// artifacts.tmpl promises the model "You do NOT need to create this
// directory yourself, it will be created automatically when you create
// artifacts" — the reference's artifacts.EnsureArtifactDirectories
// (recovered 0x7246d60). The clone keeps that promise here, so the write
// root exists before a tool resolves a path into it.
func EnsureDir(conversationID string) error {
	dir := Dir(conversationID)
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

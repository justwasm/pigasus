package artifacts

import (
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/filesystem"
)

// InstallWriteRoot points the write-side tools at the conversation's
// artifact directory and installs the sidecar hook.
//
// The reference splits this across two recovered symbols — the artifact
// directory is one of its write roots, and artifacts.updateArtifactMetadata
// records the sidecar for a native-tool write. The clone had neither, so a
// write to the directory the system prompt names was rejected outright
// ("expected path … to not start with /"), and even a write that landed
// there stayed invisible because no sidecar existed.
//
// Call it once per conversation, after the workspace is known. An empty
// conversation id clears both, so a session with no artifacts cannot leave a
// stale root behind for the next one.
func InstallWriteRoot(conversationID string) {
	dir := Dir(conversationID)
	tools.SetArtifactWorkspace(dir)
	if dir == "" {
		filesystem.ArtifactWriteHook = nil
		return
	}
	// artifacts.tmpl promises the directory already exists.
	_ = EnsureDir(conversationID)
	filesystem.ArtifactWriteHook = func(path string, meta filesystem.ArtifactMetadata) {
		if !IsArtifactFile(conversationID, path) {
			return
		}
		_ = WriteMetadata(path, Metadata{
			Summary:         meta.Summary,
			UserFacing:      meta.UserFacing,
			RequestFeedback: meta.RequestFeedback,
		})
	}
}

package artifacts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/filesystem"
)

// TestWriteMetadataThenList closes the loop the clone used to break: the
// artifact directory is writable, a write records a sidecar, and List sees
// the result.
func TestWriteMetadataThenList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	conv := "conv-write"
	dir := Dir(conv)
	if dir != filepath.Join(home, "brain", conv) {
		t.Fatalf("Dir() = %q, want the brain directory", dir)
	}
	path := filepath.Join(dir, "report.md")
	// List enumerates the directory, so the artifact itself has to exist —
	// the sidecar alone is not an artifact.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(path, Metadata{Summary: "audit"}); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}
	// The sidecar lands next to the artifact, not somewhere else.
	if _, err := os.Stat(MetadataPath(path)); err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
	refs := List(conv)
	if len(refs) != 1 {
		t.Fatalf("List = %+v, want 1 entry", refs)
	}
	if refs[0].Name != "report" {
		t.Errorf("name = %q, want report", refs[0].Name)
	}
	if refs[0].Path != "file://"+path {
		t.Errorf("path = %q, want file://%s", refs[0].Path, path)
	}
}

// TestWriteMetadataPreservesOmittedKeys keeps an edit that sends only
// userFacing from erasing the summary a previous write set.
func TestWriteMetadataPreservesOmittedKeys(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	path := filepath.Join(Dir("c"), "a.md")
	no := false
	if err := WriteMetadata(path, Metadata{Summary: "keep me", UserFacing: &no}); err != nil {
		t.Fatal(err)
	}
	// A second write that names only requestFeedback.
	if err := WriteMetadata(path, Metadata{RequestFeedback: &no}); err != nil {
		t.Fatal(err)
	}
	meta, ok := readMetadata(MetadataPath(path))
	if !ok {
		t.Fatal("sidecar unreadable")
	}
	if meta.Summary != "keep me" {
		t.Errorf("summary = %q, want it preserved", meta.Summary)
	}
	if meta.UserFacing == nil || *meta.UserFacing {
		t.Errorf("userFacing = %v, want false preserved", meta.UserFacing)
	}
	if meta.UpdatedAt.IsZero() {
		t.Error("updatedAt not stamped")
	}
}

// TestUserFacingFalseIsNotListed pins the reference's filter
// (`userFacing !== false`): an explicit false hides, absent shows.
func TestUserFacingFalseIsNotListed(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	conv := "conv-facing"
	no := false
	hidden := filepath.Join(Dir(conv), "hidden.md")
	shown := filepath.Join(Dir(conv), "shown.md")
	if err := os.MkdirAll(Dir(conv), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{hidden, shown} {
		if err := os.WriteFile(p, []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteMetadata(hidden, Metadata{UserFacing: &no}); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(shown, Metadata{}); err != nil {
		t.Fatal(err)
	}
	refs := List(conv)
	if len(refs) != 1 || refs[0].Name != "shown" {
		t.Fatalf("List = %+v, want only 'shown'", refs)
	}
}

// TestIsArtifactFile pins the artifact-file predicate: directly in the
// artifact directory, not hidden, artifact extension.
func TestIsArtifactFile(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	conv := "conv-classify"
	dir := Dir(conv)
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(dir, "plan.md"), true},
		{filepath.Join(dir, "widget.html"), true},
		{filepath.Join(dir, "dashboard.url.json"), true},
		{filepath.Join(dir, "notes.txt"), false},
		{filepath.Join(dir, ".secret.md"), false},
		{filepath.Join(dir, "plan.md.metadata.json"), false},
		{filepath.Join(dir, "scratch", "tmp.md"), false},
		{filepath.Join(dir, ".system_generated", "logs", "x.md"), false},
		{filepath.Join(dir, "sub", "nested.md"), false},
	}
	for _, c := range cases {
		if got := IsArtifactFile(conv, c.path); got != c.want {
			t.Errorf("IsArtifactFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
	if IsArtifactFile("", filepath.Join(dir, "plan.md")) {
		t.Error("an empty conversation id must classify nothing")
	}
}

// TestEnsureDirCreatesTheArtifactDirectory keeps artifacts.tmpl's promise
// that the model does not have to create the directory itself.
func TestEnsureDirCreatesTheArtifactDirectory(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	if err := EnsureDir("conv-ensure"); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if fi, err := os.Stat(Dir("conv-ensure")); err != nil || !fi.IsDir() {
		t.Fatalf("artifact directory missing: %v", err)
	}
	if err := EnsureDir(""); err != nil {
		t.Errorf("EnsureDir(\"\") = %v, want nil", err)
	}
}

// TestInstallWriteRootMakesTheArtifactDirWritable is the regression test for
// the original defect: the system prompt names the artifact directory, and a
// write there was rejected with "expected path … to not start with /".
func TestInstallWriteRootMakesTheArtifactDirWritable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	conv := "conv-root"
	workspace := t.TempDir()

	// Before: the artifact directory is outside the workspace.
	if _, err := tools.WorkspacePath(workspace, filepath.Join(Dir(conv), "r.md")); err == nil {
		t.Fatal("precondition failed: artifact dir was already writable")
	}

	InstallWriteRoot(conv)
	t.Cleanup(func() { tools.SetArtifactWorkspace(""); filesystem.ArtifactWriteHook = nil })

	target := filepath.Join(Dir(conv), "r.md")
	got, err := tools.WorkspacePath(workspace, target)
	if err != nil {
		t.Fatalf("artifact dir still rejected: %v", err)
	}
	if got != target {
		t.Errorf("resolved %q, want %q", got, target)
	}

	// A path outside both roots is still refused.
	if _, err := tools.WorkspacePath(workspace, "/etc/passwd"); err == nil {
		t.Error("a path outside every write root must still be rejected")
	}
}

// TestWriteHookRecordsSidecarOnlyForArtifacts runs the hook the hosts
// install: a create_file into the artifact directory gets a sidecar, and a
// write into scratch does not.
func TestWriteHookRecordsSidecarOnlyForArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	conv := "conv-hook"
	InstallWriteRoot(conv)
	t.Cleanup(func() { tools.SetArtifactWorkspace(""); filesystem.ArtifactWriteHook = nil })

	ws := t.TempDir()
	yes := true
	artifact := filepath.Join(Dir(conv), "plan.md")
	if err := os.WriteFile(artifact, []byte("# plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	filesystem.ArtifactWriteHook(artifact, filesystem.ArtifactMetadata{Summary: "s", UserFacing: &yes})

	scratch := filepath.Join(Dir(conv), "scratch", "tmp.md")
	if err := os.MkdirAll(filepath.Dir(scratch), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratch, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	filesystem.ArtifactWriteHook(scratch, filesystem.ArtifactMetadata{})

	if _, err := os.Stat(MetadataPath(artifact)); err != nil {
		t.Errorf("artifact sidecar missing: %v", err)
	}
	if _, err := os.Stat(MetadataPath(scratch)); err == nil {
		t.Error("scratch file must not get a sidecar")
	}
	_ = ws
}

// TestWriteMetadataStampsUpdatedAt is the one fact the checkpoint's resume
// entry reads, so it must never be left zero.
func TestWriteMetadataStampsUpdatedAt(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	path := filepath.Join(Dir("c"), "a.md")
	before := time.Now().Add(-time.Second)
	if err := WriteMetadata(path, Metadata{}); err != nil {
		t.Fatal(err)
	}
	meta, ok := readMetadata(MetadataPath(path))
	if !ok {
		t.Fatal("sidecar unreadable")
	}
	if meta.UpdatedAt.Before(before) {
		t.Errorf("updatedAt = %v, want ~now", meta.UpdatedAt)
	}
}

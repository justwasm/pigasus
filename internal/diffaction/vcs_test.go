package diffaction

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVCSSupportAndDetection(t *testing.T) {
	dir := t.TempDir()
	if got := DetectVCS(dir); got != "" {
		t.Errorf("DetectVCS(empty dir) = %q, want empty", got)
	}

	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !PathExists(gitDir) {
		t.Errorf("PathExists(%q) = false, want true", gitDir)
	}
	if got := DetectVCS(dir); got != "git" {
		t.Errorf("DetectVCS(git) = %q, want git", got)
	}
}

func TestDiffFeedbackNoWorkspace(t *testing.T) {
	if got := DiffFeedback("", 0); got != "no workspace directory" {
		t.Errorf("DiffFeedback(\"\") = %q, want \"no workspace directory\"", got)
	}
}

func TestDiffFeedbackNotInGit(t *testing.T) {
	dir := t.TempDir()
	prevGitHead := GitHeadFuncSeam
	GitHeadFuncSeam = func(d string) (string, error) {
		return "", errors.New("fatal: not a git repo")
	}
	defer func() { GitHeadFuncSeam = prevGitHead }()

	if got := DiffFeedback(dir, 0); got != "Not in a git repository" {
		t.Errorf("DiffFeedback(no repo) = %q, want \"Not in a git repository\"", got)
	}
}

func TestDiffFeedbackCleanRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevDiff := VCSDiffFunc
	VCSDiffFunc = func(vcs, d string) (string, error) {
		return "", nil
	}
	defer func() { VCSDiffFunc = prevDiff }()

	if got := DiffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("DiffFeedback(clean) = %q, want \"No uncommitted changes\"", got)
	}
}

func TestDiffFeedbackWithError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevDiff := VCSDiffFunc
	VCSDiffFunc = func(vcs, d string) (string, error) {
		return "", errors.New("command failed")
	}
	defer func() { VCSDiffFunc = prevDiff }()

	got := DiffFeedback(dir, 0)
	if !strings.HasPrefix(got, "git diff: ") {
		t.Errorf("DiffFeedback(err) = %q, want git diff prefix", got)
	}
}

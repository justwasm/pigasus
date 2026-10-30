// vcs.go — version-control system detection, working-copy diff, and feedback.

package action

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DetectVCS returns the version-control system that owns dir, or "" when there
// is none. Recovered: the reference supports Git, Mercurial and Jujutsu for
// /diff (the 1.3.4 changelog names all three, and the enum carries
// VCS_TYPE_GIT / VCS_TYPE_JJ).
func DetectVCS(dir string) string {
	for {
		switch {
		case PathExists(filepath.Join(dir, ".git")):
			return "git"
		case PathExists(filepath.Join(dir, ".hg")):
			return "hg"
		case PathExists(filepath.Join(dir, ".jj")):
			return "jj"
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "" {
			return ""
		}
		dir = parent
	}
}

// PathExists reports whether p exists on disk.
func PathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// VCSDiff runs the working-copy diff. Pagers and colour are disabled the way
// the reference does it: the 1.3.4 changelog records that "agent commands now
// set JJ_PAGER=cat alongside PAGER and GIT_PAGER", and that /diff must not be
// fooled by repositories configured to always colour their output. The 1.3.6
// changelog line 7 adds the editor-disabling trio (GIT_EDITOR / HGEDITOR /
// JJ_EDITOR = false) so a future /commit-style slash that fails through to a
// real `git commit` cannot hang on a hidden vim session.
func VCSDiff(vcs, dir string) (string, error) {
	var args []string
	switch vcs {
	case "git":
		args = []string{"--no-pager", "diff", "--no-color"}
	case "hg":
		args = []string{"diff"}
	case "jj":
		args = []string{"--color=never", "diff"}
	default:
		return "", fmt.Errorf("unsupported VCS %q", vcs)
	}
	cmd := exec.Command(vcs, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PAGER=cat", "GIT_PAGER=cat", "JJ_PAGER=cat",
		"GIT_EDITOR=false", "HGEDITOR=false", "JJ_EDITOR=false",
		"HGPLAIN=1", "NO_COLOR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return string(out), nil
}

// VCSDiffFunc is the test seam for DiffFeedback. Production code reads it
// directly; tests override it with a function that returns canned output.
var VCSDiffFunc DiffFunc = VCSDiff

// GitHeadFuncSeam is the test seam for the AGY 1.3.3 line 52 fallback: it runs
// `git -C <workspace> diff HEAD`. Production code reads RunGit through it;
// tests override it with a function that returns canned output.
var GitHeadFuncSeam GitHeadFunc = func(dir string) (string, error) {
	return RunGit(dir, "diff", "HEAD")
}

// DiffFeedback renders /diff: the workspace's uncommitted changes.
func DiffFeedback(workspace string, maxCells int) string {
	return Feedback(workspace, maxCells, VCSDiffFunc, GitHeadFuncSeam)
}

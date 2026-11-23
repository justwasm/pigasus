package battlemode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func runGitWithInput(ctx context.Context, dir string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func detectGitRepo(ctx context.Context, dir string) (repoRoot string, headCommit string, ok bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", false
	}
	top, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", false
	}
	repoRoot = strings.TrimSpace(top)
	if repoRoot == "" {
		return "", "", false
	}
	head, err := runGit(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		// Repo exists but has no commits yet; fall back to directory copy mode.
		return "", "", false
	}
	headCommit = strings.TrimSpace(head)
	if headCommit == "" {
		return "", "", false
	}
	return repoRoot, headCommit, true
}

// computeGitDiff stages untracked files with intent-to-add (-N) inside the
// isolated worktree and computes the unified diff and changed file list against
// baseCommit.
func computeGitDiff(ctx context.Context, wtDir, baseCommit string) (string, []string, error) {
	if _, err := runGit(ctx, wtDir, "add", "-N", "--", "."); err != nil {
		return "", nil, err
	}
	diffOut, err := runGit(ctx, wtDir, "diff", "--no-color", baseCommit)
	if err != nil {
		return "", nil, err
	}
	namesOut, err := runGit(ctx, wtDir, "diff", "--name-only", baseCommit)
	if err != nil {
		return diffOut, nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(namesOut), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	sort.Strings(files)
	return diffOut, files, nil
}

// computeDirDiff computes a unified diff and changed file list between two
// non-git directories (baseDir and armDir).
func computeDirDiff(ctx context.Context, baseDir, armDir string) (string, []string, error) {
	baseFiles, err := collectFiles(baseDir)
	if err != nil {
		return "", nil, err
	}
	armFiles, err := collectFiles(armDir)
	if err != nil {
		return "", nil, err
	}

	allPathsMap := make(map[string]struct{}, len(baseFiles)+len(armFiles))
	for p := range baseFiles {
		allPathsMap[p] = struct{}{}
	}
	for p := range armFiles {
		allPathsMap[p] = struct{}{}
	}
	allPaths := make([]string, 0, len(allPathsMap))
	for p := range allPathsMap {
		allPaths = append(allPaths, p)
	}
	sort.Strings(allPaths)

	var changed []string
	var diffBuf strings.Builder
	for _, rel := range allPaths {
		bData, bOK := baseFiles[rel]
		aData, aOK := armFiles[rel]
		if bOK && aOK && bytes.Equal(bData, aData) {
			continue
		}
		changed = append(changed, rel)
		diffBuf.WriteString(unifiedFileDiff(ctx, rel, baseDir, armDir, bOK, aOK, bData, aData))
	}
	return diffBuf.String(), changed, nil
}

func unifiedFileDiff(ctx context.Context, rel, baseDir, armDir string, bOK, aOK bool, bData, aData []byte) string {
	if _, err := exec.LookPath("git"); err == nil {
		oldPath := "/dev/null"
		if bOK {
			oldPath = filepath.Join(baseDir, filepath.FromSlash(rel))
		}
		newPath := "/dev/null"
		if aOK {
			newPath = filepath.Join(armDir, filepath.FromSlash(rel))
		}
		cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-color", "--", oldPath, newPath)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		_ = cmd.Run() // git diff --no-index exits 1 when differences exist
		out := stdout.String()
		if out != "" {
			out = strings.ReplaceAll(out, oldPath, "a/"+rel)
			out = strings.ReplaceAll(out, newPath, "b/"+rel)
			return out
		}
	}

	// Pure-Go fallback unified diff.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", rel, rel))
	if !bOK {
		sb.WriteString("--- /dev/null\n")
	} else {
		sb.WriteString(fmt.Sprintf("--- a/%s\n", rel))
	}
	if !aOK {
		sb.WriteString("+++ /dev/null\n")
	} else {
		sb.WriteString(fmt.Sprintf("+++ b/%s\n", rel))
	}
	bLines := splitLines(string(bData))
	aLines := splitLines(string(aData))
	sb.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", len(bLines), len(aLines)))
	for _, l := range bLines {
		sb.WriteString("-" + l + "\n")
	}
	for _, l := range aLines {
		sb.WriteString("+" + l + "\n")
	}
	return sb.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func collectFiles(root string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dst, rel), info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if perm == 0 {
		perm = 0o644
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func syncDirChanges(baseSnapshotDir, armDir, targetDir string) ([]string, error) {
	baseFiles, err := collectFiles(baseSnapshotDir)
	if err != nil {
		return nil, err
	}
	armFiles, err := collectFiles(armDir)
	if err != nil {
		return nil, err
	}

	var applied []string
	// Remove files deleted in armDir.
	for rel := range baseFiles {
		if _, exists := armFiles[rel]; !exists {
			targetPath := filepath.Join(targetDir, filepath.FromSlash(rel))
			if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
				return applied, err
			}
			applied = append(applied, rel)
		}
	}
	// Copy added or modified files from armDir to targetDir.
	for rel, aData := range armFiles {
		bData, exists := baseFiles[rel]
		if exists && bytes.Equal(bData, aData) {
			continue
		}
		srcPath := filepath.Join(armDir, filepath.FromSlash(rel))
		dstPath := filepath.Join(targetDir, filepath.FromSlash(rel))
		info, err := os.Stat(srcPath)
		perm := fs.FileMode(0o644)
		if err == nil {
			perm = info.Mode().Perm()
		}
		if err := copyFile(srcPath, dstPath, perm); err != nil {
			return applied, err
		}
		applied = append(applied, rel)
	}
	sort.Strings(applied)
	return applied, nil
}

// Package convstore fork.go — session forking and persistence helpers.
package convstore

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"uuid"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// NewUUIDv4 makes a v4 UUID (8-4-4-4-12 hex digits).
func NewUUIDv4() (string, error) {
	return uuid.NewV4().String(), nil
}

// SessionToSteps converts an in-memory session into the convstore Step shape.
// Empty assistant messages are elided, and the last assistant message has
// trailing blank lines trimmed.
//
// This is the step shape WriteConversation emits: the fork writes what
// this conversion produces, so the forked store reads back the same way
// the live one does.
func SessionToSteps(sess []backend.Message) []Step {
	var out []Step
	lastAssistant := -1
	for i, m := range sess {
		switch m.Role {
		case "user":
			out = append(out, Step{
				Idx: int64(i), Type: StepUserInput, Status: 3,
				Text: m.Content,
			})
		case "assistant":
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			out = append(out, Step{
				Idx: int64(i), Type: StepModelResponse, Status: 3,
				Text: m.Content,
			})
			lastAssistant = len(out) - 1
		}
	}
	if lastAssistant >= 0 {
		out[lastAssistant].Text = backend.TrimTrailingBlankLines(out[lastAssistant].Text)
	}
	return out
}

// ForkConversation duplicates the live session into a fresh convstore database.
// Returns the new UUID (empty on failure) and feedback message for the user.
func ForkConversation(sess []backend.Message, currentID string) (string, string) {
	return ForkConversationSteps(SessionToSteps(sess), currentID)
}

// ForkConversationSteps duplicates a conversation's trajectory — the
// user inputs, the model responses and the tool calls — into a fresh
// convstore database.
//
// The reference's fork copies the trajectory, tool calls included: the
// fork is the same conversation under a new id, so the state the calls
// produced has to come along. The tool calls come along because they
// are what the rehydration pass reads back: a fork that dropped them
// would start blind (see internal/app/session/rehydration).
func ForkConversationSteps(steps []Step, currentID string) (string, string) {
	if len(steps) == 0 {
		return "", "Error: no active conversation to fork"
	}
	id, err := NewUUIDv4()
	if err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	dir := filepath.Join(StoreDir(), "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	path := filepath.Join(dir, id+".db")
	if err := WriteConversation(path, id, steps); err != nil {
		return "", "  Failed to fork: " + err.Error()
	}
	if currentID != "" {
		srcAnn := filepath.Join(StoreDir(), "annotations", currentID+".pbtxt")
		if annData, err := os.ReadFile(srcAnn); err == nil {
			dstAnn := filepath.Join(StoreDir(), "annotations", id+".pbtxt")
			_ = os.WriteFile(dstAnn, annData, 0o644)
		}
		return id, fmt.Sprintf("Forked conversation.\nTo go back, use: /resume %s", currentID)
	}
	return id, "Forked conversation."
}

// shouldSkipDir reports whether a directory in an artifact tree should be
// skipped during copy.
func shouldSkipDir(relPath string, name string) bool {
	if relPath == "" || relPath == "." {
		return false
	}
	if name == ".git" || name == ".jj" {
		return true
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if clean == ".git" || clean == ".jj" {
		return true
	}
	for _, skip := range []string{
		".system_generated/worktrees",
		".system_generated/subagents",
		".system_generated/messages",
	} {
		if clean == skip || strings.HasPrefix(clean, skip+"/") {
			return true
		}
	}
	return false
}

// CopyArtifactTree recursively walks srcDir and copies files and directories into dstDir,
// skipping directories filtered by shouldSkipDir (.git, .jj, .system_generated/{worktrees,subagents,messages}).
func CopyArtifactTree(srcDir, dstDir string) error {
	if srcDir == "" || dstDir == "" {
		return nil
	}
	srcInfo, err := os.Stat(srcDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", srcDir, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("%s is not a directory", srcDir)
	}
	cleanSrc := filepath.Clean(srcDir)
	cleanDst := filepath.Clean(dstDir)
	if cleanSrc == cleanDst {
		return nil
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dstDir, err)
	}

	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("rel path for %s: %w", path, err)
		}
		if d.IsDir() {
			if shouldSkipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			targetDir := filepath.Join(dstDir, rel)
			info, err := d.Info()
			perm := os.FileMode(0o755)
			if err == nil {
				perm = info.Mode().Perm()
			}
			return os.MkdirAll(targetDir, perm)
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		targetFile := filepath.Join(dstDir, rel)
		return copyFile(path, targetFile, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(out, in)
	return err
}

// ForkConversationFull duplicates an active conversation and its artifact tree,
// rendering a <fork_notice> block for the newly branched conversation.
func ForkConversationFull(steps []Step, currentID string, oldArtifactDir, newArtifactDir string, projectID string, droppedSubagents, canceledTasks bool) (string, string, string, error) {
	if currentID == "" && len(steps) == 0 {
		return "", "", "", fmt.Errorf("no active conversation to fork")
	}

	newID, err := NewUUIDv4()
	if err != nil {
		return "", "", "", fmt.Errorf("generate uuid: %w", err)
	}

	if newArtifactDir == "" && oldArtifactDir != "" {
		newArtifactDir = filepath.Join(filepath.Dir(oldArtifactDir), newID)
	}

	dir := filepath.Join(StoreDir(), "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	dstPath := ConversationPath(newID)
	copiedDB := false

	if currentID != "" {
		srcPath := ConversationPath(currentID)
		if srcInfo, err := os.Stat(srcPath); err == nil && srcInfo.Mode().IsRegular() {
			if err := copyFile(srcPath, dstPath, srcInfo.Mode().Perm()); err != nil {
				return "", "", "", fmt.Errorf("copy conversation db: %w", err)
			}
			copiedDB = true
			if sdb, err := sql.Open("sqlite3", "file:"+dstPath+"?mode=rw"); err == nil {
				_, _ = sdb.Exec(`UPDATE trajectory_meta SET trajectory_id = ?, cascade_id = ?`, newID, newID)
				sdb.Close()
			}
		}
	}

	if !copiedDB {
		if err := WriteConversation(dstPath, newID, steps); err != nil {
			return "", "", "", fmt.Errorf("write conversation: %w", err)
		}
	}

	if currentID != "" {
		srcAnn := filepath.Join(StoreDir(), "annotations", currentID+".pbtxt")
		if annData, err := os.ReadFile(srcAnn); err == nil {
			dstAnn := filepath.Join(StoreDir(), "annotations", newID+".pbtxt")
			_ = os.WriteFile(dstAnn, annData, 0o644)
		}
	}

	if oldArtifactDir != "" && newArtifactDir != "" {
		if err := CopyArtifactTree(oldArtifactDir, newArtifactDir); err != nil {
			return "", "", "", fmt.Errorf("copy artifact tree: %w", err)
		}
	}

	noticeData := agyprompt.ForkNoticeData{
		SourceCascadeID:  currentID,
		OldArtifactDir:   oldArtifactDir,
		NewArtifactDir:   newArtifactDir,
		DroppedSubagents: droppedSubagents,
		CanceledTasks:    canceledTasks,
	}
	forkNotice, err := agyprompt.RenderForkNotice(noticeData)
	if err != nil {
		return "", "", "", fmt.Errorf("render fork notice: %w", err)
	}

	var feedback string
	if projectID != "" {
		if currentID != "" {
			feedback = fmt.Sprintf("Forked conversation into project %q.\nTo go back, use: /resume %s", projectID, currentID)
		} else {
			feedback = fmt.Sprintf("Forked conversation into project %q.", projectID)
		}
	} else {
		if currentID != "" {
			feedback = fmt.Sprintf("Forked conversation.\nTo go back, use: /resume %s", currentID)
		} else {
			feedback = "Forked conversation."
		}
	}

	return newID, forkNotice, feedback, nil
}

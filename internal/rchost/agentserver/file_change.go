// file_change.go — FILE_CHANGE steps for the remote-agent server.
//
// The reference publishes a file edit as CORTEX_STEP_TYPE_FILE_CHANGE (86)
// with the file's absolute URI and a DiffBlock; the client's /diff opens
// that block with getCodeActionDiff. The bridge therefore emits this step
// for the file-mutating tools instead of a generic tool step.
package agentserver

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/infra/mockremoteagent"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
)

// fileEditArgs are the path spellings the write/edit tools accept.
type fileEditArgs struct {
	Path         string `json:"path"`
	TargetFile   string `json:"TargetFile"`
	TargetFileLC string `json:"targetFile"`
	AbsolutePath string `json:"absolutePath"`
}

// editedFilePath returns the path argument of a file-mutating tool call.
func editedFilePath(toolName string, args json.RawMessage) string {
	switch toolName {
	case "create_file", "write_to_file", "write_file", "replace_file_content",
		"edit_file", "sed_file", "delete_file", "move_file":
	default:
		return ""
	}
	var p fileEditArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return ""
	}
	for _, candidate := range []string{p.Path, p.TargetFile, p.TargetFileLC, p.AbsolutePath} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

// workspaceFile resolves a tool path against the workspace and reports its
// workspace-relative form. A path outside the workspace is rejected.
func workspaceFile(workspace, path string) (abs, rel string, ok bool) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(path) == "" {
		return "", "", false
	}
	if strings.HasPrefix(path, "file://") {
		if u, err := url.Parse(path); err == nil {
			path, _ = url.PathUnescape(u.Path)
		}
	}
	abs = path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workspace, path)
	}
	rel, err := filepath.Rel(workspace, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	return abs, filepath.ToSlash(rel), true
}

// gitOriginal reads the committed version of a workspace file. A missing
// file (new file, no repository) is an empty original.
func gitOriginal(workspace, rel string) string {
	cmd := exec.Command("git", "-C", workspace, "show", "HEAD:"+rel)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// diffBlockLines turns a unified diff into the DiffBlock line list. The
// line type numbers follow exa.diff_action_pb.UnifiedDiffLineType.
func diffBlockLines(rel, before, after string) []mockremoteagent.DiffLine {
	body := diffaction.UnifiedDiff("a/"+rel, "b/"+rel, before, after)
	if body == "" {
		return nil
	}
	var out []mockremoteagent.DiffLine
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			continue
		case strings.HasPrefix(line, "@@"):
			out = append(out, mockremoteagent.DiffLine{Type: 0, Text: line})
		case strings.HasPrefix(line, "+"):
			out = append(out, mockremoteagent.DiffLine{Type: 1, Text: line[1:]})
		case strings.HasPrefix(line, "-"):
			out = append(out, mockremoteagent.DiffLine{Type: 2, Text: line[1:]})
		default:
			out = append(out, mockremoteagent.DiffLine{Type: 3, Text: line})
		}
	}
	return out
}

// fileChangeStep builds and appends the FILE_CHANGE step for one tool call.
func fileChangeStep(session *mockremoteagent.CascadeSession, workspace, toolName string, args json.RawMessage) bool {
	path := editedFilePath(toolName, args)
	if path == "" {
		return false
	}
	abs, rel, ok := workspaceFile(workspace, path)
	if !ok {
		return false
	}
	before := gitOriginal(workspace, rel)
	after, err := os.ReadFile(abs)
	if err != nil {
		// A delete leaves no file to read; the original is the whole diff.
		after = nil
	}
	uri := (&url.URL{Scheme: "file", Path: abs}).String()
	session.AddFileChangeStep(uri, toolName, string(args), diffBlockLines(rel, before, string(after)))
	return true
}

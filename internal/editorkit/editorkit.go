// Package editorkit holds the editor-resolution helpers shared by the
// slash actions: splitting an editor command line, choosing the
// fallback editor, resolving the `editor` setting, and the
// /open-file plumbing.
//
// The prompt-editing gesture (ctrl+g) lives in cmd/wagyu because it
// drives a bubbletea process message; the resolution rules it depends
// on are pure and live here.
package editorkit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// SplitCommand splits an editor setting such as "code --wait" into a name and
// arguments.
func SplitCommand(s string) (string, []string) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

// EditorFallback returns the editor used when the `editor` setting is "auto"
// and $EDITOR is unset.
//
// Recovered from the changelog: "Fixed Windows editor fallback to use \"edit\"
// or \"notepad\" when the editor setting is \"auto\" and no editor is
// configured, instead of attempting to use \"vim\"." That fix is Windows-only,
// so `vim` is the Unix fallback and Windows prefers `edit`, then `notepad`.
func EditorFallback() string {
	if runtime.GOOS == "windows" {
		for _, c := range []string{"edit", "notepad"} {
			if _, err := exec.LookPath(c); err == nil {
				return c
			}
		}
		return "notepad"
	}
	return "vim"
}

// ResolveEditor returns the editor for /open and any notice to show first.
//
// Recovered: `No editor configured; using %s`, `Failed to resolve editor: %v`
// and the settings label `auto ($EDITOR)`. The `editor` settings key defaults to
// "auto", meaning "use $EDITOR"; see EditorFallback for the fallback itself.
//
// $EDITOR is split on whitespace only, never on `=`: the changelog records
// "Fixed `$EDITOR` environment variable parsing: resolved issues where arguments
// containing `=` (e.g., `--alternate-editor=vi`) were incorrectly split".
func ResolveEditor(configured string) (name string, args []string, notice string, err error) {
	configured = strings.TrimSpace(configured)
	if configured == "" || strings.EqualFold(configured, "auto") {
		if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
			name, args = SplitCommand(e)
			return name, args, "", nil
		}
		name = EditorFallback()
		return name, nil, fmt.Sprintf("No editor configured; using %s", name), nil
	}
	name, args = SplitCommand(configured)
	return name, args, "", nil
}

// PromptScratchFile writes the prompt to a scratch file for ctrl+g editing and
// returns its path. The caller removes it once the editor has exited.
func PromptScratchFile(prompt string) (string, error) {
	f, err := os.CreateTemp("", "agy-prompt-*.md")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.WriteString(prompt); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// OpenFeedback resolves /open into a command to run. It returns the line(s) to
// show before the editor starts and the command itself (nil when the file
// cannot be opened).
//
// Recovered: `Usage: /open <file_path>`, `File not found: %s`,
// `Opening file: %s`, `No editor configured; using %s` and
// `Failed to resolve editor: %v`. The caller emits
// `Editor exited with error: %v` when the process returns non-zero.
//
// pathExists is injected rather than called directly so the caller keeps
// control of the filesystem probe (cmd/wagyu routes it through
// internal/diffaction.PathExists so the /diff and /open paths agree).
func OpenFeedback(arg, configuredEditor, workspace string, pathExists func(string) bool) (string, *exec.Cmd) {
	path := UnquotePath(arg)
	if path == "" {
		return "Usage: /open <file_path>", nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	if !pathExists(path) {
		return fmt.Sprintf("File not found: %s", path), nil
	}
	name, args, notice, err := ResolveEditor(configuredEditor)
	if err != nil {
		return fmt.Sprintf("Failed to resolve editor: %v", err), nil
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Sprintf("Failed to resolve editor: %v", err), nil
	}
	c := exec.Command(name, append(args, path)...)
	c.Dir = workspace
	msg := fmt.Sprintf("Opening file: %s", path)
	if notice != "" {
		msg = notice + "\n" + msg
	}
	return msg, c
}

// UnquotePath strips one layer of matching single or double quotes, which is
// how a path with spaces is typed into the input box.
func UnquotePath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

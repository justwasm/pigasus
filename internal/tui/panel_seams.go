package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	"github.com/justwasm/pigasus/palette"
)

var (
	panelGray238  = palette.SubtleOpen
	panelGray238C = "\x1b[39m"
)

var clipboardWrite = slashactions.ClipboardWrite

const codesearchPipeMessage = "Cannot view a pipe, socket, or device; only regular files can be viewed."
const codesearchEmptyMessage = "/codesearch needs a query."

func splitFileLines(data []byte) []string {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

func editorBaseName(editor string) string {
	name := strings.ToLower(filepath.Base(editor))
	name = strings.TrimSuffix(name, ".exe")
	name = strings.TrimSuffix(name, ".cmd")
	return name
}

func editorLineArgs(editor, path string, line int) []string {
	switch editorBaseName(editor) {
	case "vi", "vim", "nvim", "neovim", "view", "gvim", "mvim", "vimdiff":
		return []string{fmt.Sprintf("+%d", line), path}
	case "code", "code-insiders", "codium", "vscodium":
		return []string{"-g", fmt.Sprintf("%s:%d", path, line)}
	case "subl", "sublime_text":
		return []string{fmt.Sprintf("%s:%d", path, line)}
	}
	return []string{path}
}

// PanelHostSeams adapts RootModel to the panel builders in internal/cliapp/panels.

// panel_seams.go — the host side of the panel registry.
//
// Every panel in internal/ui/views owns its own state, keys and rows. What
// it cannot own is the session: the task registry, the live backend, the
// transcript. Those are the host's, and the host answers them through the
// narrow interfaces the panels declare (internal/tui/panels).
//
// This file keeps only what all of them share — the package-level scratch
// state and the small helpers — so that reading one panel's adapter never
// requires loading every other panel's. The adapters themselves live in the
// panels_*.go files beside this one:
//
//	panels_tasks.go            /tasks
//	panels_config.go           /config
//	panels_settings.go         /permissions /hooks /plugins /credits
//	                           /add-dir /feedback /skills /provider
//	panels_rename_accounts.go  /rename and the account seams
//	panels_help.go             /help, active items and the swarm board
//	panels_agents.go           /agents and the subagent authorizations
//	panels_codesearch_model.go /codesearch and /model
//	panels_viewer_mention.go   the viewer, /resume rename, @-questions
//	panels_slash_rewind.go     the slash picker and /rewind

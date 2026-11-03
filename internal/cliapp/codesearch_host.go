// codesearch_host.go — the host's side of the /codesearch panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/codesearchpanel: it owns the
// argument tail, the query, the hits, the file set, the spinner, the
// cursor and the scroll window, the keys and the rows. The host
// supplies the seven things the panel cannot answer itself — the
// search root, the terminal's size, whether the progressive path is
// on, the runner factory, the way the panel reads a file, the way it
// opens one in the editor and the way out of the panel — and nothing
// else.
//
// The panel is rebuilt every time it opens: the host's slashPanel
// struct is reset when a panel closes, so a stale query, a stale hit
// set or a stale cursor is never shown.
//
// The search and the runner it fills live in
// internal/features/tools/codesearch: the panel drives them, it does
// not reimplement them.
//
// # The file viewer
//
// Cycle 383 (A-18): Enter opens the highlighted result in the panel's
// own read-only file viewer and Ctrl+G opens it in the external editor
// (official-docs/cli/commands/codesearch.md:36-37).
//
// The panel owns the view; the host owns the disk and the process
// (invariant 163): the panel asks for the file's lines and decides
// that a file is opened, and the host reads the file and starts the
// editor.

package cliapp

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/tools/codesearch"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
)

// codesearchPipeMessage is the reference's own sentence for a path that
// is not a regular file.
//
// 【strings】 the reference's own string table carries "Cannot view a
// pipe, socket, or device; only regular files can be viewed." verbatim;
// the surviving pruned index of that table,
// out/local-macos-1.4.11/strings-index.txt, carries the head of it —
// "Cannot view a pipe, socket, or device" — and the full dump is
// retired.
const codesearchPipeMessage = "Cannot view a pipe, socket, or device; only regular files can be viewed."

// newCodesearchPanel builds the /codesearch panel with the host's seams.
func (m *runMode) newCodesearchPanel() *codesearchpanel.Panel {
	return codesearchpanel.New(codesearchpanel.Seams{
		// The search runs in the session's working directory.
		Root: func() string {
			return m.cwd
		},
		// The panel's rows are laid out for the terminal's size.
		Size: func() (int, int) {
			return m.width, m.height
		},
		// The progressive path needs a TUI to tick on; the host turns
		// it off when there is none (print mode, the unit-test path).
		Streaming: func() bool {
			return m.streamingSearch
		},
		// The runner is the features package's: it spawns `rg --json`
		// and falls back to a local walk.
		NewRunner: codesearch.NewRunner,
		// The panel never touches the disk: it asks for the file's
		// lines and the host reads them.
		ReadFile: m.codesearchReadFile,
		// The panel decides that the file is opened and hands over the
		// path and the line; the host starts the editor.
		OpenEditor: m.codesearchOpenEditor,
		// The panel decides that the comments are sent and hands over
		// the text in the reference's wire format; the host closes the
		// panel and starts the turn.
		SendComments: m.codesearchSendComments,
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// codesearchPanel returns the /codesearch panel, building it if the
// host has not opened one yet.
func (m *runMode) codesearchPanel() *codesearchpanel.Panel {
	if m.panel.csp == nil {
		m.panel.csp = m.newCodesearchPanel()
	}
	return m.panel.csp
}

// codesearchEmptyMessage is the verbatim echo the slash command emits
// when the user types `> /codesearch` with no argument tail. The
// `⎿  ` wrapper is the standard REF slash-echo row (slashRespond).
const codesearchEmptyMessage = "/codesearch needs a query."

// codesearchReadFile reads a file for the panel's file viewer.
//
// The path is relative to the search root — the panel's hits carry the
// path the searcher saw, and the search ran in the session's working
// directory.
//
// The messages are the reference's own: `File not found: %s` (the
// editorkit sentence, shared with /open) and `Cannot view a pipe,
// socket, or device; only regular files can be viewed.` — both are
// strings of the reference's own table, and the surviving pruned index of
// that table, out/local-macos-1.4.11/strings-index.txt, carries the
// first in full and the head of the second — "Cannot view a pipe, socket,
// or device" — while the full dump is retired.
func (m *runMode) codesearchReadFile(path string) ([]string, string, bool) {
	full := m.codesearchPath(path)
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Sprintf("File not found: %s", full), false
		}
		return nil, err.Error(), false
	}
	if !info.Mode().IsRegular() {
		return nil, codesearchPipeMessage, false
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err.Error(), false
	}
	return splitFileLines(data), "", true
}

// codesearchPath resolves a path the panel carries against the search
// root.
func (m *runMode) codesearchPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(m.cwd, path)
}

// splitFileLines splits a file's bytes into its lines.
//
// The trailing newline is not a line of its own and a CRLF's carriage
// return is not part of the line's text — the same shape the searcher
// reports, so a hit's line number is the line's index here.
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

// codesearchSendComments delivers the pending line comments to the
// agent.
//
// The panel decides that the comments are sent and hands over the text
// in the reference's wire format — the literal "<file>" tag, the file,
// the line and the comment, one comment per line (【strings】
// "<file>%s:%d:%s\n"); the host closes the panel and starts the turn
// with the comments as the user's next message
// (official-docs/cli/commands/codesearch.md:97).
func (m *runMode) codesearchSendComments(text string) tea.Cmd {
	closed := m.closePanel()
	if strings.TrimSpace(text) == "" {
		return closed
	}
	return tea.Batch(closed, m.startModelTurn(text))
}

// codesearchOpenEditor opens a file in the external editor, at a line.
//
// The panel decides that the file is opened and hands over the path and
// the line; the host owns the process (invariant 163). The editor is
// the `editor` setting, resolved the way /open resolves it.
func (m *runMode) codesearchOpenEditor(path string, line int) tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.response = append(m.response, notice)
	}
	full := m.codesearchPath(path)
	c := exec.Command(name, append(args, editorLineArgs(name, full, line)...)...)
	c.Dir = m.cwd
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

// editorLineArgs returns the arguments that open a file at a line in
// the given editor.
//
// The reference's own line convention is not recovered, so this is the
// clone's: the vi family jumps with "+LINE", code and codium with
// "-g path:LINE", sublime with "path:LINE", and an editor the clone
// does not know gets the path alone.
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

// editorBaseName returns the editor command's own name, without the
// directory and without a Windows executable suffix.
func editorBaseName(editor string) string {
	name := strings.ToLower(filepath.Base(editor))
	name = strings.TrimSuffix(name, ".exe")
	name = strings.TrimSuffix(name, ".cmd")
	return name
}

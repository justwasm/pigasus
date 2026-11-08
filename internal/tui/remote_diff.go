// remote_diff.go — /diff in a `--remote-agent` session.
//
// The remote workspace is not on the client's disk, so the panel cannot
// read git. The proxy's getTurnDiff returns the turn's file map (path →
// FileDiffData with the two full versions), and the panel's All Changes tab
// is fed from it; the inline body is the unified diff the two versions
// produce.
package tui

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
)

// remoteFileDiff is one remote file's change.
type remoteFileDiff struct {
	added    int
	deleted  int
	original string
	modified string
}

// remoteDiffCache is the latest proxy turn diff.
type remoteDiffCache struct {
	files map[string]remoteFileDiff
	// codeLines maps a path to the getCodeActionDiff body of the step
	// that last touched it. The reference renders that block (not a
	// locally synthesized diff) for a code action.
	codeLines map[string][]string
	loaded    bool
	err       error
}

// prepareRemoteDiff fetches the latest turn's diff from the proxy.
func (m *RootModel) prepareRemoteDiff() error {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok {
		return errors.New("remote agent: no engine")
	}
	// Before the first turn there is no conversation to ask about; the
	// panel should show its empty state, not an error.
	if engine.CascadeID() == "" {
		m.diff.remote = &remoteDiffCache{files: map[string]remoteFileDiff{}, loaded: true}
		return nil
	}
	ctx, cancel := remoteagentclient.WithUnaryTimeout(context.Background())
	defer cancel()
	diff, err := engine.TurnDiff(ctx)
	if err != nil {
		m.diff.remote = &remoteDiffCache{loaded: true, err: err}
		return err
	}
	files := make(map[string]remoteFileDiff, len(diff.FileDiffs))
	for path, fd := range diff.FileDiffs {
		files[path] = remoteFileDiff{
			added:    int(fd.Additions),
			deleted:  int(fd.Deletions),
			original: fd.OriginalContents,
			modified: fd.ModifiedContents,
		}
	}
	cache := &remoteDiffCache{files: files, codeLines: map[string][]string{}, loaded: true}
	// The reference opens a code action's diff from getCodeActionDiff, so
	// fetch the block for every step this turn recorded. A failure here is
	// not fatal: the contents-based diff below still renders.
	for path, step := range engine.CodeSteps() {
		block, err := engine.CodeActionDiff(ctx, step)
		if err != nil {
			continue
		}
		if lines := codeActionLines(block); len(lines) > 0 {
			cache.codeLines[path] = lines
		}
	}
	m.diff.remote = cache
	return nil
}

// codeActionLines renders a DiffBlock's unifiedDiff lines with the marker
// each line type carries. The reference's three types are INSERT (+),
// DELETE (-) and UNCHANGED (space); UNSPECIFIED keeps the text as-is.
func codeActionLines(block remoteagentclient.CodeActionDiff) []string {
	if block.UnifiedDiff == nil {
		return nil
	}
	out := make([]string, 0, len(block.UnifiedDiff.Lines))
	for _, line := range block.UnifiedDiff.Lines {
		text := line.Text
		switch line.Type {
		case remoteagentclient.UnifiedLineInsert:
			if !strings.HasPrefix(text, "+") && !strings.HasPrefix(text, "@@") {
				text = "+" + text
			}
		case remoteagentclient.UnifiedLineDelete:
			if !strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "@@") {
				text = "-" + text
			}
		case remoteagentclient.UnifiedLineUnchanged:
			if !strings.HasPrefix(text, " ") && !strings.HasPrefix(text, "@@") {
				text = " " + text
			}
		}
		out = append(out, text)
	}
	return out
}

// RemoteDiffChanges feeds the diff panel in a remote session. ok=false
// leaves the panel on its local path (a local session).
func (m *RootModel) RemoteDiffChanges() (tracked, untracked []diffpanel.Change, ok bool) {
	if m == nil || !m.session.remoteAgent {
		return nil, nil, false
	}
	if m.diff.remote == nil || !m.diff.remote.loaded {
		return nil, nil, true
	}
	paths := make([]string, 0, len(m.diff.remote.files))
	for path := range m.diff.remote.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	tracked = make([]diffpanel.Change, 0, len(paths))
	for _, path := range paths {
		fd := m.diff.remote.files[path]
		tracked = append(tracked, diffpanel.Change{Path: path, Added: fd.added, Deleted: fd.deleted})
	}
	return tracked, nil, true
}

// RemoteDiffBody returns the inline diff for one remote file.
func (m *RootModel) RemoteDiffBody(target, kind string) ([]string, bool) {
	if m == nil || !m.session.remoteAgent {
		return nil, false
	}
	if m.diff.remote == nil || !m.diff.remote.loaded {
		return nil, true
	}
	if kind != "file" {
		return nil, true
	}
	// A code action's own block wins: it is the diff the reference shows.
	if lines, ok := m.diff.remote.codeLines[target]; ok && len(lines) > 0 {
		return lines, true
	}
	fd, ok := m.diff.remote.files[target]
	if !ok {
		return nil, true
	}
	body := diffaction.UnifiedDiff("a/"+target, "b/"+target, fd.original, fd.modified)
	if body == "" {
		return []string{}, true
	}
	return strings.Split(body, "\n"), true
}

// RemoteDiffVCS names the panel's source in a remote session.
func (m *RootModel) RemoteDiffVCS() string {
	if m == nil || !m.session.remoteAgent {
		return ""
	}
	return "remote"
}

// slashDiffRemote renders a short remote-change summary. Bare /diff opens
// the panel; this answers the argument forms and any path that bypasses it.
func (m *RootModel) slashDiffRemote(args string) (bool, tea.Cmd) {
	if err := m.prepareRemoteDiff(); err != nil {
		m.slashRespond("/diff", "error: "+err.Error())
		return true, nil
	}
	cache := m.diff.remote
	paths := make([]string, 0, len(cache.files))
	for path := range cache.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		m.slashRespond("/diff", "No file changes in the latest turn.")
		return true, nil
	}
	lines := []string{"Remote changes (latest turn):"}
	for _, path := range paths {
		fd := cache.files[path]
		lines = append(lines, "  "+path+" +"+strconv.Itoa(fd.added)+" -"+strconv.Itoa(fd.deleted))
	}
	m.slashContent(strings.Join(lines, "\n"))
	return true, nil
}

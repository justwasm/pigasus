// slash_list_panels.go — cycle 206: the four "list" panels (/tasks,
// /artifact, /codesearch, /context) and the two behaviours the earlier
// panel cycles left uncovered.
//
// Evidence (REF v1.4.0, private tmux socket `agy206`, 120–220 columns):
//
//  1. Aliases. The reference resolves a slash command's aliases before
//     it picks the panel, so `/cs` and `/search` open the Code Search
//     panel exactly like `/codesearch`, and `/settings` / `/quota` /
//     `/undo` open the /config, /usage and /rewind panels. The alias
//     table is already recovered in internal/tuilib/commands.go
//     (codesearch → cs, search). The clone's openSlashPanel did no
//     resolution, so `/cs` printed "Unknown command: /cs" into the
//     transcript and left the transcript as it was.
//     Captures: `probe_c.py alias` (REF `/cs`, `/search`) and
//     `probe_alias.py /settings /undo /quota`.
//
//  2. Key ownership. The four list panels have no rows, no search box
//     and no editor target, and the reference makes every key inert
//     while one is up — only Esc dismisses the panel. `probe_one.py`
//     drove REF's /tasks panel with C-c, C-g, C-o, C-l, Left, Right,
//     PgUp, PgDn, ctrl+home, ctrl+end, Tab, Enter, Backspace, k, x and
//     p; not one of them changed the frame. The clone leaked the
//     globals instead: C-g opened $EDITOR over the panel and C-l
//     toggled alt-screen (`probe_one.py our /tasks C-g|C-l`), and
//     PgUp/PgDn/ctrl+home/ctrl+end fell through to handleScrollKey.
//     listPanelOwningKey closes that hole.
//
// ctrl+c is deliberately NOT swallowed: REF does not quit on ctrl+c
// even with no panel open (`probe_one.py ref NONE C-c` → no change), and
// the clone's ctrl+c is inert with a panel up today, so letting it fall
// through keeps the observed behaviour and cannot trap the user.

package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tuilib"
)

// listPanelKind reports whether k is one of the four key-owning list
// panels: /tasks, /artifact, /codesearch, /context. The other row-less
// panels (/mcp, /permissions, …) are left alone here; they have their
// own cycles.
func listPanelKind(k panelKind) bool {
	switch k {
	case panelTasks, panelArtifact, panelCodeSearch, panelContext:
		return true
	}
	return false
}

// canonicalPanelCommand resolves a slash-command alias to the name the
// panel table is keyed by. The mapping comes from the recovered catalog
// in internal/tuilib (the same table the "/" picker renders), so it can
// never drift from the reference's alias list. A name that is not an
// alias is returned unchanged, which makes the call a no-op for every
// command that already works.
//
// "/cs: Code Search:" — REF opens the panel for the alias, whereas the
// clone used to print `⎿  Unknown command: /cs`.
func canonicalPanelCommand(cmd string) string {
	if _, ok := panelForCommand(cmd); ok {
		return cmd
	}
	for _, c := range tuilib.DefaultSlashCommands {
		for _, a := range c.Aliases {
			if a == cmd {
				return strings.TrimPrefix(c.Name, "/")
			}
		}
	}
	return cmd
}

// listPanelOwningKey is the key handler for the four list panels. It
// mirrors the reference's "owns every key until it is dismissed"
// contract: Esc closes the panel, ctrl+c is left to the global binding,
// and every other key is swallowed so it cannot reach the prompt,
// $EDITOR (ctrl+g), the alt-screen toggle (ctrl+l) or the transcript
// scroller (pgup/pgdown/ctrl+home/ctrl+end).
func (m *runMode) listPanelOwningKey(key tea.Key) (bool, tea.Cmd) {
	if key.Code == tea.KeyEsc {
		return true, m.closePanel()
	}
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		return false, nil
	}
	return true, nil
}

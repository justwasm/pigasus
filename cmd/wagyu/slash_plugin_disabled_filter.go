// slash_plugin_disabled_filter.go — cycle dpl: the "enabled-only
// customization query" the /plugin Installed tab exposes, plus the
// reset-on-close behaviour that closes the AGY 1.2.7 bug.
//
// The bug, in two sentences:
//
//	Before the fix, an Installed tab filtered to "enabled only" persisted
//	its filter across panel close/reopen, so the disabled rows never came
//	back. The filter only ever lives in panel state, so any path that
//	re-creates the panel from a fresh `installedPluginRows()` walk shows
//	the disabled rows again — that is exactly what the fix does.
//
// What this file owns:
//
//   - installedPluginRowsFiltered(enabledOnly bool) []pluginRow: the
//     reader the panel body uses. enabledOnly=false returns every row the
//     on-disk walk yields (the cycle-198 behaviour, unchanged); true
//     drops the rows whose plugins.<name>.enabled is false.
//
// What this file does NOT own:
//
//   - The panel state field that toggles the filter (slashPanel.
//     pluginEnabledOnly) — that lives in slash_panels.go alongside the
//     other plugin state. Keeping the state struct and the rendering
//     wiring close together preserves the cycle-198 + 219 layering.
//   - The reset on closePanel — same file as the state field, so the two
//     cannot drift.
package main

import (
	"github.com/justwasm/pigasus/internal/auth"
)

// installedPluginRowsFiltered returns the rows the Installed tab renders.
// When `enabledOnly` is false the result is byte-for-byte the cycle-198
// call: directory name ascending, plugin.json description when present,
// else the stock table. When true, rows whose plugins.<name>.enabled is
// false are dropped — the reference surface that satisfies an
// "enabled-only customization query".
//
// The helper reads enabled state fresh off disk on every call so the filter
// flips the instant space toggles an entry (cycle 198); no caching layer
// to invalidate.
func installedPluginRowsFiltered(enabledOnly bool) []pluginRow {
	all := installedPluginRows()
	if !enabledOnly {
		return all
	}
	out := make([]pluginRow, 0, len(all))
	for _, r := range all {
		on, err := auth.PluginEnabled(r.name)
		if err != nil || !on {
			continue
		}
		out = append(out, r)
	}
	return out
}

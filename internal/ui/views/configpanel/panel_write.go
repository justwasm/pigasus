// panel_write.go — the /config panel's writes.
//
// # One writer
//
// Every write goes through settings.json: the panel builds the value the
// reference's row stores and hands it to the settings layer, which edits
// the one key that changed and leaves the rest of the document alone.
//
// # A default is an absence
//
// The reference represents every setting at its default by *removing*
// the key. Writing the literal default (e.g. `agentMode: "default"`)
// makes 1.4.0 reject the whole file with
//
//	⚠ Settings Error
//	  invalid settings: agentMode: unrecognized value "default"
//
// and silently run with defaults — so the clone must delete, not write.

package configpanel

import (
	tea "charm.land/bubbletea/v2"

	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

// applyValue sets the chosen configuration value, persisting to
// settings.json unless the row is marked memoryOnly.
func (p *Panel) applyValue(row Row, val Val) {
	if p == nil {
		return
	}
	if IsTelemetryRow(row) && auth.IsBusinessAccount() {
		p.respond("/config", auth.TelemetryDisabledResponse)
		return
	}
	if row.Name == "Color Scheme" {
		p.applyTheme(val.Display)
	}
	if row.SettingsKey == "altScreenMode" {
		p.applyAltScreen(val.Stored)
	}
	if row.SettingsKey == "verbosity" {
		p.applyVerbosity(val.Display)
	}
	if row.MemoryOnly {
		if p.memory == nil {
			p.memory = map[string]string{}
		}
		p.memory[row.SettingsKey] = val.Display
		return
	}
	if val.Stored == nil {
		_ = settings.DeleteField(row.SettingsKey)
		p.afterWrite(row.SettingsKey)
		return
	}
	_ = settings.SetField(row.SettingsKey, val.Stored)
	p.afterWrite(row.SettingsKey)
}

// afterWrite re-reads the seasonal theme after the /config row switched
// it.
//
// 【一手】 agy 1.4.7: the theme follows the setting, so the themed banner,
// the themed prompt-box rules and the moon-phase spinner follow the row
// as it is switched.
func (p *Panel) afterWrite(key string) {
	if key != seasonal.OptInKey {
		return
	}
	p.refreshSeasonal()
}

// cycleValue advances the value of the setting the cursor is on by `dir`
// (+1 for right, -1 for left) through its cycle list, then persists it.
//
// A row with fewer than two values has nothing to cycle.
func (p *Panel) cycleValue(rows []Row, dir int) tea.Cmd {
	if p == nil {
		return nil
	}
	idx := p.cursor
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	row := rows[idx]
	if len(row.Values) < 2 {
		return nil
	}
	if IsTelemetryRow(row) && auth.IsBusinessAccount() {
		p.respond("/config", auth.TelemetryDisabledResponse)
		return nil
	}
	current := p.rawDisplayValue(row)
	next := row.Values[0]
	for i, v := range row.Values {
		if v.Display == current {
			next = row.Values[(i+dir+len(row.Values))%len(row.Values)]
			break
		}
	}
	p.applyValue(row, next)
	if row.Name == "Color Scheme" {
		return tea.ClearScreen
	}
	return nil
}

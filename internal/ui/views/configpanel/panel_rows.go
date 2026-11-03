// panel_rows.go — the /config panel's rows.
//
// The catalogue itself lives in configpanel.go; this file holds the
// panel's own view of it: the rows the dynamic gates leave, the seasonal
// row the reference draws only in season, the search filter over them
// and the display value of each row.

package configpanel

import (
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

// Catalogue returns the settings rows the panel shows, filtered by the
// dynamic gates — today only the "Editor Mode › Insert First" child
// row, which the reference draws immediately after Editor Mode while
// editorMode == "vim" (probe_ref_walk2.py row 8).
//
// The seasonal row is not part of the static catalogue: the reference
// draws it only while a seasonal event is in season.
func (p *Panel) Catalogue() []Row {
	out := make([]Row, 0, len(SettingsRows)+1)
	for _, r := range SettingsRows {
		if r.ShowWhenKey != "" {
			if raw, _ := settings.Field(r.ShowWhenKey); RawString(raw) != r.ShowWhenVal {
				continue
			}
		}
		out = append(out, r)
	}
	if ev := p.seasonalEvent(); ev != nil {
		out = insertSeasonalRow(out, seasonalConfigRow(ev))
	}
	return out
}

// Rows returns the rows the panel is showing right now: the whole
// catalogue while the search box is empty, the filtered list after that.
//
// Every cursor-indexed helper (the renderer, the value cycle, the
// submenu) goes through it, so the cursor always points at a row that is
// on screen.
func (p *Panel) Rows() []Row {
	return RowsFiltered(p.Catalogue(), p.query)
}

// RowCount is the cursor bound (0..count-1) for the panel's row list.
//
// While the option submenu is open the bound is the *value* count of the
// row under the cursor — the submenu is a list of values, not a list of
// rows.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	rows := p.Rows()
	if p.submenu {
		if p.cursor >= 0 && p.cursor < len(rows) {
			return len(rows[p.cursor].Values)
		}
		return 0
	}
	return len(rows)
}

// seasonalConfigRow is the "Seasonal Themes" row.
//
// 【一手】 agy 1.4.7's /config (out/changelog-147/ref/config.txt):
//
//	Seasonal Themes              on                 seasonalThemesOptIn
//
// The row's help line is verbatim in seasonal.RowDescription, its two
// stops read "off" and "on", and switching it on writes the event's own
// id into settings.json ("seasonalThemesOptIn": "halloween-2026").
// Switching it off deletes the key again — the reference's own
// "a setting at its default is absent" convention.
func seasonalConfigRow(ev *seasonal.Event) Row {
	return Row{
		Name:        seasonal.RowName,
		SettingsKey: seasonal.OptInKey,
		Values: []Val{
			V("off", ""),
			V("on", ev.ID),
		},
		Description: seasonal.RowDescription,
	}
}

// insertSeasonalRow puts the seasonal row where the reference draws it:
// between "Sandbox Mode" and "Show Active Tasks".
func insertSeasonalRow(rows []Row, row Row) []Row {
	for i, r := range rows {
		if r.Name != "Show Active Tasks" {
			continue
		}
		out := make([]Row, 0, len(rows)+1)
		out = append(out, rows[:i]...)
		out = append(out, row)
		out = append(out, rows[i:]...)
		return out
	}
	return append(rows, row)
}

// Value returns the display string for one row — what the picker paints
// in the value column.
//
// The value is read live from settings.json on every call, so ←/→ flips
// the value and persists before the next paint.
func (p *Panel) Value(row Row) string {
	if p == nil {
		return ""
	}
	if p.seams.OverrideFor != nil {
		if suffix, ok := p.seams.OverrideFor(row); ok {
			base := p.rawDisplayValue(row)
			if suffix != "" {
				return base + " " + suffix
			}
			return base
		}
	}
	return p.rawDisplayValue(row)
}

// rawDisplayValue returns the un-suffixed display value for one row.
func (p *Panel) rawDisplayValue(row Row) string {
	if IsTelemetryRow(row) && auth.IsBusinessAccount() {
		return auth.TelemetryDisabledValue
	}
	if row.MemoryOnly {
		if v, ok := p.memory[row.SettingsKey]; ok {
			return v
		}
		return row.Values[0].Display
	}
	raw, err := settings.Field(row.SettingsKey)
	if err != nil {
		return p.displayFor(row, nil)
	}
	return p.displayFor(row, raw)
}

// SettingValue returns the display string for the catalogue row at the
// given index — the unfiltered catalogue, so an index is stable while
// the search box filters the list.
func (p *Panel) SettingValue(idx int) string {
	if p == nil {
		return ""
	}
	rows := p.Catalogue()
	if idx < 0 || idx >= len(rows) {
		return ""
	}
	return p.Value(rows[idx])
}

// displayFor maps a stored leaf back to the display form of the matching
// cycle value. A value not present in the list (e.g. a `toolPermission`
// explicitly written as "always-proceed" while the default is absent, or
// a hand-edited colour) falls back to the first entry, which is the
// reference's own behaviour.
//
// Cycle 241 (AGY 1.4.1): Business accounts see the `Enable Telemetry`
// row painted as `off (disabled)` regardless of what settings.json (or,
// since it's memoryOnly, the panel's own memory) holds. The check sits
// in this lowest-level display function so the test, the panel's render
// and the live value agree on the Business-account locked display.
func (p *Panel) displayFor(row Row, raw any) string {
	locked := ""
	if auth.IsBusinessAccount() {
		locked = auth.TelemetryDisabledValue
	}
	return DisplayFor(row, raw, locked)
}

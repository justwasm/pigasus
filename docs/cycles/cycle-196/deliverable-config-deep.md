# Cycle 196 — Deep `/config` picker

## TL;DR

The /config picker now actually mutates state. ←/→ cycles the value of
the highlighted setting, persists it to settings.json via
`auth.SetSettingsField`, and the next render re-reads the live value so
the change is visible immediately. Bool-typed settings whose
settings.json shape is a nested object (`statusLine.enabled`,
`title.enabled`, etc.) are walked via dot notation. The picker gained
windowing so the cursor can reach rows 10-19 (the panel previously only
rendered rows 0-9; moving the cursor past 9 was silently capped).

## What was wrong

Cycle 193 implemented the /config *appearance*: 20 settings, name+value
columns, descriptions, counter, keyboard legend. But pressing ←/→ did
nothing — `handlePanelKey` only routed those keys to the `/effort`
horizontal slider, leaving the config picker inert. The TODO.md §5 P2
follow-up note ("reading the live settings.json is a follow-up") was
unaddressed.

The same cycle hardcoded every setting's `value` field — opening the
panel on a machine with `colorScheme: "dark"` in settings.json still
rendered `"light"` because the table literal was authoritative.

## What changed

### `internal/auth/compat.go`

Added two generic helpers that round-trip raw JSON values through
settings.json, supporting nested keys via dot notation:

- `SettingsField(key string) (any, error)` — reads. Walks `key` parts
  through nested objects, returns the JSON shape (string, bool,
  []any, etc.) as `any`. Returns `(nil, nil)` when the file or key is
  absent. Used by `panelConfigLines` on every render so the displayed
  value tracks disk state.
- `SetSettingsField(key string, value any) error` — writes. Creates
  intermediate nested objects as needed; preserves unknown fields;
  alphabetises keys via `updateSettings`'s map-marshalling.
  Cycle 196: the cycle-through helper calls this with the new value.

Both helpers are additive — the existing `SettingsBool` /
`SettingsString` / `SetSettingsBool` / `SetSettingsString` keep their
typed signatures and semantics. `SettingsField` is the TUI-only
counterpart (print-mode /config still uses its own typed mappers
because they format bool-vs-string distinctly).

### `cmd/wagyu/slash_panels.go`

#### `configSettingsRows` refactor

Each row now carries five fields instead of three:

```go
var configSettingsRows = []struct {
    name        string
    settingsKey string   // settings.json key, dot path
    values      []string // cycle list
    description string
    boolKind    bool     // persist as JSON bool?
}{
    {"Agent Mode", "agentMode", []string{"default", "accept-edits", "auto", "plan"}, "...", false},
    ...
    {"Status Line", "statusLine.enabled", []string{"off", "on"}, "...", true},
    ...
}
```

- `name`: panel display string (kept the cycle-193 wording).
- `settingsKey`: the settings.json key. Dot notation for nested
  objects (`statusLine.enabled`, `title.enabled`).
- `values`: the cycle list. ←/→ rotates through this list with wrap.
- `boolKind`: true → `SetSettingsField(key, true/false)`; false →
  `SetSettingsField(key, string)`. Drives the round-trip
  representation choice in `cycleConfigValue`.
- `description`: unchanged — used in the dim-gray description row
  beneath the list.

#### `configSettingValue(rowIdx int) string`

New helper. Reads the live value via `auth.SettingsField(row.settingsKey)`
and maps it back to the friendly cycle value (e.g. bool `true` →
`"on"`, JSON `always-proceed` → `"always-proceed"`). Falls back to
`row.values[0]` on missing key or read error.

#### `panelConfigLines`

- Reads the value through `configSettingValue(i)` instead of the
  removed `row.value` field, so the picker reflects live settings.json.
- Adds `m.panel.window` windowing: when the cursor goes below the
  visible 10-row window, the window scrolls so the cursor row is
  always visible. The counter reads `[W+1 to W+10 of 20 items]`
  instead of the static `[1-10 of 20 items]`.
- `panelRowCount(panelConfig)` now returns
  `len(configSettingsRows)` (20) so cursor movement reaches the full
  list; the visible 10-row window is just a render concern.

#### `handlePanelKey`

For `panelConfig`, routes ←/→ to the new `cycleConfigValue(±1)`
helper instead of swallowing them as inert. Up/Down keep their
no-wrap semantics (REF clamps on this panel — verified by the panel
showing no arrow at the top/bottom edge).

#### `cycleConfigValue(dir int)`

New helper. Determines the cycle list, finds the current value, picks
the next (or wraps), persists via `auth.SetSettingsField`. Cycle
length 1 is a no-op. Bool-typed settings store `next == "on"`; string
settings store `next` verbatim.

### `cmd/wagyu/slash_cycle196_test.go` (new file, +173 lines)

5 tests, all passing:

| Test | What it pins |
|---|---|
| `TestCycle196_ConfigCycleRight_SimpleString` | cursor on Color Scheme, +1 → "dark", value re-reads |
| `TestCycle196_ConfigCycleLeft_Wrap` | first value "light", ← → "system" (wrap to last) |
| `TestCycle196_ConfigCycle_BoolSetting` | nested-object bool: statusLine.enabled toggles, type/command preserved |
| `TestCycle196_ConfigCycle_KeyHandler` | `handlePanelKey(tea.KeyRight)` calls cycleConfigValue |
| `TestCycle196_ConfigCycle_AbsentKey` | missing key → values[0] default; cycle still works |
| `TestCycle196_ConfigValue_LiveRead` | opening panel reads disk state, not hardcoded default |

Each test sets `ANTIGRAVITY_HOME` to `t.TempDir()` so the real
settings.json is never touched. `cycle196TestHome` writes a starting
seed via `json.MarshalIndent` so the test is reproducible.

## Verification

### Build + full test suite

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu   BUILD_EXIT=0
go test -vet=off ./cmd/... ./internal/...      27 packages ok
go test -vet=off -run 'Cycle196' ./cmd/wagyu/  5/5 pass
```

### Live probe (`/tmp/agycmp/probe_cfg_simple.py`)

Run the clone in a tmux session, type `/config`, Enter, send one
Right, capture pane:

```
Initial:    > Agent Mode                   default
After R:    > Agent Mode                   accept-edits
settings.json: agentMode = "accept-edits"
```

### Live multi-cycle probe (`/tmp/agycmp/probe_cfg_multi.py`)

Cursor at row 0 (default), 3× Right: default → accept-edits → auto → plan.
Cursor moves to row 3 (Color Scheme, default "light"), Right →
"dark". Cursor scrolls to row 16 (Status Line, default "off"), Right
→ "on"; settings.json `statusLine.enabled` becomes `true`. Windowing
brings Status Line into the visible 10-row block.

## Limitations

- **`enter Select` is still inert.** REF's picker on this build
  appears to swallow Enter without doing anything visible; the legend
  advertises "Select" but the live probe doesn't show a sub-modal
  opening. Cycle 196 leaves Enter in the swallow branch. If REF ever
  opens an edit modal for the highlighted row, that's a future cycle.
- **The `description` for `Artifact Review` shows `"always-proceed"`
  in REF but the cycle list says `"always-proceed"` too.** Pre-cycle-196
  the description displayed `"always proceeds"` (with a space). The
  description text in `configSettingsRows` is the cycle-193 wording;
  the cycle list now matches what REF actually stores. The dim-178
  description row was never a byte-for-byte check — only the value
  column is.
- **`Animation Speed` (runningLightSpeed) defaults are
  `slow`/`medium`/`fast` here, but REF may use different enum values.**
  Live probe didn't reach that row; the cycle list is a guess from
  the print-mode defaults table. A future cycle would confirm by
  driving REF.
- **`Editor` cycle list is `auto`/`vi`/`vim`/`nano`/`code`** — these
  are guesses; the real cycle may include emacs, sublime, or the
  user's literal `$EDITOR` value. The user can type any editor name
  but the picker only cycles through this hardcoded list.
- **`toolPermission` is one of the 34 keys in `printSlashConfigKeys`
  but is not in the 20-row panel** — REF may surface it under a
  different row name ("Tool Permission" or similar), or the panel
  may show it under another label. Not yet verified.
- **`statusLine.type` and `title.type` are left at their existing
  values (`"command"`/`""`)** when toggling `enabled`. REF may rewrite
  the object when enabled → on; this cycle only flips the boolean and
  leaves the rest of the object intact. Live probe verified that
  `type` and `command` survive a toggle.

## Diff stat

```
 cmd/wagyu/slash_panels.go          | +145 / −55
 cmd/wagyu/slash_cycle196_test.go   | +173 (new)
 internal/auth/compat.go            |  +66
 docs/cycles/cycle-196/...          | +this file
```
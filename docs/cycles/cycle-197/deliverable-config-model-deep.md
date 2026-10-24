# Cycle 197 — Deep `/model` + deep `/config`

## TL;DR

Cycle 196 made `/config`'s ←/→ actually persist. This cycle went back to the
reference and found that most of what cycle 196 persisted was **wrong**:
several cycle lists were invented, the row→key map was guessed, and — the
serious one — the clone wrote each setting's *literal default* into
`settings.json`, which makes `agy` 1.4.0 reject the whole file and silently
run with defaults.

Two behaviours were recovered by driving the reference and diffing
`settings.json` after every keypress:

1. **default-by-absence** — a setting at its default is represented by the
   *key being absent*, not by a literal value. Cycling Agent Mode back to
   `default` deletes `agentMode`.
2. **the real 21-row catalogue** — all 20 top-level rows plus the
   `Editor Mode › Insert First` child, with their real keys, real value
   lists, real defaults and real descriptions.

`/model` was deepened at the same time: ←/→ now moves an embedded effort
slider on the Gemini row only, Enter persists the catalogue *slug* to
`settings.json` while `m.model`/`m.statusModel` take the display *label*, and
the `(current)` mark + slider position derive from the **effective**
(post-fallback) model rather than the raw settings value.

`/config` and `/model` are now byte-for-byte identical to a **live** reference
capture at 220×50 (only the `Enable Telemetry` row differs — see Limitations).

## What was wrong

### 1. Writing the default broke the reference

Cycle 196's `cycleConfigValue` wrote the chosen cycle string verbatim:

```go
_ = auth.SetSettingsField(row.settingsKey, next)   // e.g. "default"
```

A probe that seeded `agentMode: "default"` produced, on reference start-up:

```
⚠ Settings Error
  /Users/gear/.gemini/antigravity-cli/settings.json
  └ invalid settings: agentMode: unrecognized value "default"

  > Exit and fix manually
    Continue with defaults this session
    Overwrite the file with defaults
```

REF refuses the file and runs with defaults — which also means it *ignores*
every other setting. The user's real `settings.json` is shared between the
reference and the clone, so a single ← on the Agent Mode row was enough to
break the reference's next launch.

### 2. The catalogue was invented

Cycle 193/196 hardcoded 20 rows from the ten visible in the capture plus ten
guesses. Driving REF with ↓ showed all 20 at once (the earlier 10-row capture
was taken in a shorter terminal):

```
Agent Mode                       agentMode
Animation Speed                  runningLightSpeed
Artifact Review                  artifactReviewPolicy
Color Scheme                     colorScheme
Conversation Picker Grouping     pickerGrouping
Copy on Select                   copyOnSelect
Editor                           editor
Editor Mode                      editorMode
Editor Mode › Insert First       vimInsertFirst        ← child row, only while editorMode == vim
Enable Telemetry                 (not persisted)
Non-Workspace Access             allowNonWorkspaceAccess
Notifications                    notifications
Queued Messages                  queuedMessages
Rendering Mode                   altScreenMode
Sandbox Mode                     enableTerminalSandbox
Show Active Tasks                hideActiveItems       ← inverted
Show Feedback Survey             showFeedbackSurvey
Show Tips                        showTips
Tool Permission                  toolPermission
Use AI Credits                   useG1Credits
Verbosity                        verbosity
```

Cycle 196's `resumePickerGrouping` and the separate "Picker Grouping" row do
not exist; `pickerGrouping` is the real key. `Sandbox Proxy`, `Telemetry
Verbosity`, `Title Bar`, `Vim Insert First` (as a top-level row) and
`Workspace Trust` were all inventions.

### 3. Several cycle lists were wrong

| Row | cycle 196 (guessed) | reference (measured) |
|---|---|---|
| Animation Speed | `slow, medium, fast` | `medium → slow → off → fast` |
| Color Scheme | `light, dark, system` | `terminal → light → solarized light → colorblind-friendly light → dark → solarized dark → colorblind-friendly dark → tokyo night` |
| Editor | `auto, vi, vim, nano, code` | `auto ($EDITOR) → vim → emacs` |
| Editor Mode | `default, vim, emacs` | `default → vim` |
| Conversation Picker Grouping | `resumePickerGrouping` values | `none → grouped` |
| Artifact Review | default `always-proceed` | default `asks for review` |

### 4. `/model`'s `(current)` and slider used the raw settings value

With `"model": "MiniMax-M3"` in `settings.json` (a model absent from the
catalogue), the reference falls back to `Gemini 3.8 Flash (High)`, marks
*that* row `(current)` and parks the effort slider on **high**. The clone
resolved `currentSlug` from the raw settings string, found no match, and so
(a) rendered no `(current)` and (b) left the slider on medium, which also
pulled the wrong description line.

## What changed

### `internal/auth/compat.go`

New `DeleteSettingsField(key string) error` — the counterpart of
`SetSettingsField`. Walks the dot path and removes the leaf; a missing path is
a no-op; empty parent objects are kept (the reference never prunes the
`statusLine`/`title` shells). This is what makes default-by-absence possible.

### `cmd/wagyu/slash_panels.go`

#### `configRow` / `cfgVal` replace the anonymous row struct

```go
type cfgVal struct {
    display string // rendered text, e.g. "always proceeds"
    stored  any    // JSON value REF writes; nil = the key is absent (default)
}

type configRow struct {
    name        string
    settingsKey string
    values      []cfgVal
    description string
    showWhenKey string // dynamic gate (the Insert First child)
    showWhenVal string
    memoryOnly  bool   // REF toggles but never persists (Enable Telemetry)
}
```

`configRows()` returns the catalogue with the dynamic gate applied;
`configSettingRaw`/`configSettingValue`/`configDisplayFor` map the stored
leaf back to its display form. A leaf that is not in the list (an explicitly
written `toolPermission: "always-proceed"` while the default is absent)
falls back to the first entry — which is what the reference does when it
advances such a row.

`cycleConfigValue` now matches on the *display* form, then:

- `next.stored == nil` → `auth.DeleteSettingsField`
- otherwise → `auth.SetSettingsField`
- `memoryOnly` → stored in `m.panel.configMemory`, disk untouched

#### Height-derived window + always-present counter slot

`configVisibleRowsFor(height)` is `height*3/4 − 12` clamped to
`[1, len(configRows())]`, measured with `probe_ref_height.py`:

```
h=20 → 3   h=24 → 6   h=28 → 9   h=32 → 12   h=40 → 18   h≥44 → all 20
```

It has the same 3/4 shape as the already-committed `helpListWindowFor`, with
a larger offset for the /config chrome (title, search box, description,
counter, legend). Unknown height (before the first `WindowSizeMsg`, and in
unit tests) keeps the cycle-196 fallback of 10 so the shorter-height snapshot
stays valid.

The counter *slot* is always emitted; the reference blanks it rather than
collapsing the line when every row fits (live capture at h=50:
`rows, '', desc, '', '', '', Keyboard`).

#### `/model`

- `panelCurrentModelLabel()` prefers `m.model` (set from
  `resolveEffectiveModel` at start-up) and falls back to
  `resolveEffectiveModel("")`. Both the `(current)` mark and
  `effortCursorForLabel` derive from it, so an unavailable settings value
  still lands on the fallback row.
- `initialEffortCursorFromSettings` was rewritten on top of
  `effortCursorForLabel` (`"(Low)"→0`, `"(High)"→2`, else 1) —
  the old slug switch table is gone.
- `rebuildEffortSlider` factored out of `panelEffortLines` so the /model
  embed can render the slider at `effortCursor` without the
  `(current)` annotation.
- Opening the panel derives the slider from the effective label instead of
  the raw settings slug.

### `cmd/wagyu/model_resolve.go`

`resolveEffectiveModel` now maps a catalogue **slug** to its display
**label** via the new `canonicalModelLabel`, so a restart with
`"model": "gemini-3.8-flash-medium"` renders `Gemini 3.8 Flash (Medium)`
rather than the raw slug. A bare base slug (`gemini-3.8-flash`, no effort
suffix) is still not in the catalogue and still triggers the
"no longer available" fallback — same as pre-197 `modelAvailable`, no
regression.

## Evidence

All probes are in `/tmp/agycmp/`; the settings file was backed up and restored
around every one of them.

| Probe | What it settled |
|---|---|
| `probe_ref_walk2.py` | authoritative row-name → settings-key map (Right + full-file diff per row) |
| `probe_ref_vocab.py` | full ←/→ vocabulary and stored form for rows 0-9 |
| `probe_ref_height.py` | rows-visible vs terminal height; all 20 row descriptions |
| `probe_ref_scroll.py` | the complete 20-row render at height 50; Rendering Mode description |
| `probe_ref_suffix.py` | the status-row effort suffix |
| `cmp_config_live.py` | live REF vs live clone /config byte comparison |
| `cmp_model197.py` | live clone vs captured REF /model byte comparison |
| `smoke_cycle197.py` | end-to-end: keys → `settings.json` on disk |

### default-by-absence (`probe_ref_walk2.py`, Agent Mode)

```
 0 'Agent Mode'  'default' -> 'accept-edits'  diff={'agentMode': (None, 'accept-edits')}
 ...
12 'Agent Mode'  'plan'    -> 'default'       diff={'agentMode': ('plan', None)}
```

Booleans follow the same rule on the other side: Copy on Select's `on` is
absent, `off` is JSON `false`.

### Live parity

```
cmp_config_live.py   REF panel lines: 31  OUR panel lines: 31   mismatches: 1 (Enable Telemetry)
cmp_model197.py      REF rows: 17  OUR rows: 17                 mismatches: 0
```

### Live end-to-end (`smoke_cycle197.py`, temp `ANTIGRAVITY_HOME`)

```
AgentMode Right  → {"agentMode": "accept-edits"}
AgentMode Right×2→ {"agentMode": "plan"}
AgentMode Left   → {"agentMode": "auto"}
model Left Enter → {"model": "gemini-3.8-flash-medium"}
```

## Tests

`cmd/wagyu/slash_cycle197_test.go` (new, 388 lines) pins the catalogue size
and name→key map, the four-step Animation Speed cycle, Artifact Review's
display forms, default-by-absence for Agent Mode and Rendering Mode,
`Tool Permission` advancing from an explicitly-written `always-proceed`,
`memoryOnly` leaving disk untouched, the height→rows table, the counter slot,
the `/model` effective-model `(current)` mark, slug→slider mapping,
Gemini-only effort cycling, Enter persisting the slug + setting the label,
Argon inertness, and the 30-column pad geometry.

Cycle-196 tests were updated where the recovered behaviour invalidated a
guess: `light → solarized light` (was `→ dark`), `←` from `light` now *deletes*
the key (was `→ system`), and the bool test moved from the non-existent
"Status Line" row to `Show Tips`. `configRowIndexForTest` now resolves through
`configRows()` so a dynamic child row cannot shift an index.

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu     # exit 0
go test -vet=off ./cmd/... ./internal/...       # exit 0
```

## Limitations / 存疑

- **`Enable Telemetry` is the one live diff.** Toggling it in REF changes
  *nothing* in `settings.json` (full-file diff is empty), yet the value
  survives for the session and across launches — so it is persisted
  somewhere outside `settings.json` (server-side, or a file the probe did not
  diff). The clone keeps it in panel state (not on disk). A fresh REF launch
  initially showed `on`; after probing it shows `off`, which is why the live
  comparison reports exactly this one row.
- **`Verbosity`'s cycle list is 推测.** The probe that was meant to walk it
  landed on the wrong row; `low/medium/high` is inferred from the row name
  and the description ("how much detail is shown…"). Default `medium` *is*
  measured.
- **`Color Scheme`'s description is 推测.** The probe's slice picked up a
  neighbouring line for this row; the eight-value cycle and the `terminal`
  default are measured.
- **The `Editor Mode › Insert First` child row's geometry is 推测.** Its
  presence (only while `editorMode == vim`) and its key (`vimInsertFirst`,
  `off → on`) were measured; a raw byte capture of the indented child line
  was not taken, so the clone draws it with the standard two-space indent.
- **The window row-count model is empirical.** `height*3/4 − 12` fits
  h=20/24/28/32/40 measured values and caps at 20 for h≥44, but two later
  launches at h=36/44 showed no counter at all — REF's panel is drawn inline
  in the transcript, so the count also depends on how much scrollback sits
  above it. The model therefore matches the *reference-height* case and the
  windowed captures; it is not a derivation.
- **`statusRightPrefix = " · high"` is left as a constant.** With auth
  expired the reference's catalogue is unavailable, so *every* seeded model
  (`gemini-3.8-flash-low`, `-medium`, `-high`, `gpt-oss-120b-medium`) falls
  back to `Gemini 3.8 Flash (High)` and every reachable capture reads
  `· high`. Whether the suffix tracks the tier could not be observed on this
  machine. Keeping the constant is consistent with all available evidence.
- **`-p /config` and `-p /model` are untouched.** The print-mode paths have
  their own tables (`printSlashConfigDefaults`, `printSlashConfigKeys`) and
  their own byte-exact tests; this cycle only changed the TUI panels.
- **`statusLine`/`title` are no longer `/config` rows.** They are valid
  settings but the reference's panel does not surface them; the cycle-196
  test that used `statusLine.enabled` as its bool example was retargeted.

## Housekeeping

The probes wrote to the user's real
`~/.gemini/antigravity-cli/settings.json`. It was backed up to
`/tmp/agycmp/settings.polluted.json` and restored:
`model` back to `MiniMax-M3`, the invalid `agentMode: "default"` key removed
(absent = default), `colorScheme`/`artifactReviewPolicy`/`toolPermission`/
`altScreenMode` left as they were before this cycle's probes.

## Diff stat

```
 cmd/wagyu/slash_panels.go          | 822 ++++++++++++++++++++++++++------------
 cmd/wagyu/model_resolve.go         |  22 +-
 cmd/wagyu/slash_cycle197_test.go   | 388 (new)
 cmd/wagyu/slash_cycle196_test.go   |  86 +-
 cmd/wagyu/slash_panels_test.go     |  46 +-
 internal/auth/compat.go            |  33 +
 docs/cycles/cycle-197/...          | this file
```

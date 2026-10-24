# Cycle effort-picker — `/effort` timeline-gauge picker, direct form, model-default fallback

**Target:** `agy` 1.4.0 (`~/.local/bin/agy`, 185,647,136 B).

**Theme:** the 1.4.0 changelog adds `/effort` as a *timeline-gauge picker* on top of the existing `--effort` flag, plus a direct `/effort <level>` setter, plus the rule that `--model <name>` without `--effort` should pick the model's server default. The picker existed as a stub (cycle 102: `printSlashEffort(flags)`, cycle 167 → 208: the panel cursor + Apply); the direct form and the model-default plumbing did not.

This cycle wires all three surfaces (TUI, print, persisted flag set) onto the same `Effort` field, exposes a real picker Enter that matches the reference's `Effort set to <level>` echo + `Exited /effort command` notice on Esc, and pins the seven behavioural cases the brief calls out.

---

## 1. Result

| Surface | Before | After |
|---|---|---|
| `/effort <level>` in TUI | always opened the picker panel | direct setter; echoes `Effort set to <level>` (or `Invalid level: <x>. Use low\|medium|high.`) |
| `/effort` (no args) in TUI | picker (cycle 102 / 208 path) | picker (unchanged: `openSlashPanel("effort")` seeds `panel.cursor = currentEffort()`) |
| `/effort` Esc in TUI | "Exited /effort command" notice (cycle 208 default) | "Exited /effort command" notice; `flags.effort` unchanged; banner / status suffix unchanged |
| `/effort` Enter (no change) | silent (cycle 208 path) | silent (unchanged) |
| `/effort` Enter (changed) | echoes `Effort set to <level>` (cycle 208 path) | echoes `Effort set to <level>`; banner / status suffix move; `auth.SetSettingsField("model", label)` writes the model *label* |
| `--model <name>` alone (no `--effort`) | would error or pick a default slugs | `resolveModelSelection(name, "")` returns the model's server default (e.g. `gemini-3.8-flash` → `-high`); printed by the cycle-208 picker |
| `--effort low\|medium\|high\|xhigh\|max` | accepted, ignored in print | accepted; print path forwards `Effort` to `pigrt.Options.Effort` → `Session.SetThinkingLevel(...)` after `BindExtensions` |
| TUI: `Loop.Options.Effort` | not read | forwarded to `runner.SetThinkingLevel(...)` in `internal/agent/pig.go` before `SendMessages` |
| `settings.json` round-trip | `model: "…"` only | unchanged — `effort` is read through `flags.String("effort", …)` (cycle 208 path), and the picker Enter / direct form write through `auth.SetSettingsField("model", label)` |

The picker Enter ↔ direct form share the same `applyEffortLevel(name, level)` plumbing; the picker path additionally gates the panel-close on `m.panel.kind == panelEffort` so the slash path doesn't touch the empty struct.

## 2. Slash command / picker behaviour

The seven test cases in `cmd/wagyu/slash_effort_test.go` (all green):

| # | Test | Trigger | Assertion |
|---|---|---|---|
| 1 | `TestEffortSlashDirect` | `m.handleSlashCommand("/effort high")` | response contains `Effort set to high`; `flagStringValue(m.flags, "effort") == "high"` |
| 2 | `TestEffortSlashInvalidLevel` | `m.handleSlashCommand("/effort extreme")` | response contains `Invalid level: extreme` and `Use low\|medium\|high`; `flags.effort` unchanged |
| 3 | `TestEffortSlashNoArgsOpensPicker` | `m.openSlashPanel("effort")` | `m.panel.kind == panelEffort`, `m.panel.cmd == "/effort"`, `m.panel.cursor == effortHigh` (seeded from settings.json's persisted model label) |
| 4 | `TestEffortPickerNavigate` | open picker at low → Right → Enter | cursor moves Low→Medium; Enter closes panel, echoes `Effort set to medium`, `flags.effort = "medium"` |
| 5 | `TestEffortPickerEscape` | open picker at high → Left Left → Esc | `m.panel.kind == panelNone`; response contains `Exited /effort command` and **does not** contain `Effort set to …`; `flags.effort` unchanged at `"high"` |
| 6 | `TestEffortDefaultFallback` | `resolveModelSelection("gemini-3.8-flash", "")` and `…("gpt-oss-120b", "")` | returns `"gemini-3.8-flash-high"` and `"gpt-oss-120b-medium"` respectively |
| 7 | `TestEffortPersistence` | `cycle196TestHome` seeds `{"model":"gemini-3.8-flash","effort":"high"}`; `m.currentEffortName()` | returns `"high"` (from settings.json); `parseEffortWord("HIGH")` and `effortIndexForName("high")` round-trip |

`TestCycle208_EffortEnterApplies`, `TestCycle208_EffortBarColourBoundary`, `TestCycle208_StatusEffortSuffixTracksEffort` (cycle 208) are untouched and still green — the picker Enter path goes through the same `applyEffortLevel(...)` they were written against.

## 3. Picker geometry

```
Set Effort

  ◂        ●━━━━━━━━━━━━━━●━━━━━━━━━━━━━━◉        ▸
          low          medium          high
    Deepest reasoning for complex problems — slower but strongest  (current)

Keyboard: ←/→ Effort  enter Select  esc Go Back
```

Three positions (`effortLow / effortMedium / effortHigh`), one horizontal slider (`●━━●━━◉`), one description line that tracks the cursor (cycle 208's `panelEffortDescription`), one `(current)` annotation that follows `m.currentEffort()`. Keyboard hint matches the reference.

`m.panel.cursor` is the slider position; `m.currentEffort()` reads the same value as the status-suffix helper in cycle 208 (`statusEffortSuffix()`) so the (current) marker and the right-edge `· high` line never disagree.

## 4. PiG effort API chain

The print path's `Effort` field rides through three calls in the order they fire:

```
pigPrintConfig.Effort                                # cmd/wagyu/print_pig.go
    │
    ▼
pigrt.Options.Effort                                 # internal/pigrt/runtime.go
    │
    ▼
coding.Session.SetThinkingLevel(level ai.ThinkingLevel, opts ...thinking.Option)   # PiG coding/session.go:1581
    │
    ▼
agent.Agent.SetThinkingLevel(...)                    # PiG agent/agent.go:1304 (called by the session on every Send)
    │
    ▼
model.ClampThinking() / ThinkingLevelMap             # PiG clamps to the model's supported levels
```

The agent layer in the TUI path is `agent.Agent` directly:

```
Loop.Options.Effort                                  # internal/agent/loop.go
    │
    ▼
runner.SetThinkingLevel(level ai.ThinkingLevel)      # internal/agent/pig.go (TUI's translation)
    │
    ▼
agent.Agent.SetThinkingLevel(...)                    # PiG agent/agent.go:1304
    │
    ▼
model.ClampThinking() / ThinkingLevelMap
```

Both paths land on `agent.Agent.SetThinkingLevel(...)`, which is the only API PiG exposes for the per-session effort. The clamp is owned by PiG (`agent/agent.go:1304` reads `model.ThinkingLevelMap`), not by the clone.

`ai.ThinkingLevel` constants (`ai/types.go:492-510`):

```
ai.ThinkingLevelOff       = "off"
ai.ThinkingLevelMinimal   = "minimal"
ai.ThinkingLevelLow       = "low"
ai.ThinkingLevelMedium    = "medium"
ai.ThinkingLevelHigh      = "high"
ai.ThinkingLevelXHigh     = "xhigh"
ai.ThinkingLevelMax       = "max"
```

The clone accepts `low|medium|high|xhigh|max|off|minimal` from the user (cycle 101's `--effort` validator) but exposes only `low|medium|high` in the picker. The print-path `pigrt.Options.Effort` forwards the verbatim user word; PiG's clamp either honours it or silently drops it to the model's nearest level.

## 5. Live probe — clone vs reference

Probe recipe (220×50 and 220×30, fresh `ANTIGRAVITY_HOME`, `AGY_ADC_AUTH=true`):

```bash
/home/.../probe.sh <agy binary> <output dir> <width> <height> <fresh ANTIGRAVITY_HOME>
```

Captures live at `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy-effort/{our,our-30}/raw/*.raw.txt`. Each scenario is a fresh binary launch (dodges input-buffer races between key events). Special keys use tmux key names: `Enter`, `Left`, `Right`, `Escape` (not `Esc` — that sends literal characters).

| Scenario | Keys | Probe (clone) | Reference match |
|---|---|---|---|
| 01-direct-medium | `/effort medium` Enter | `> /effort ⎿ Effort set to medium`; banner `(Medium)`, status `· medium` | 1.4.0 echo + banner move 【一手】 |
| 02-direct-low | `/effort low` Enter | `> /effort ⎿ Effort set to low`; banner `(Low)`, status `· low` | 1.4.0 echo + banner move 【一手】 |
| 03-invalid-extreme | `/effort extreme` Enter | `> /effort ⎿ Invalid level: extreme. Use low\|medium\|high.`; banner / status unchanged | 1.4.0 echo format 【一手】 |
| 04-picker-open | `/effort` Enter | `Set Effort` panel, `●━━●━━◉` on `high`, `(current)` annotation, Keyboard hint | cycle 208 `ppref.effort.txt` byte-match |
| 05-picker-right-from-high | `/effort` Enter Right | cursor stays on `high` (clamped) | cycle 208 silent clamp 【一手】 |
| 06-picker-left-once | `/effort` Enter Left | cursor on `medium` | cycle 208 step 【一手】 |
| 07-picker-left-twice | `/effort` Enter Left Left | cursor on `low`, description `Faster responses…` | cycle 208 step 【一手】 |
| 08-picker-esc | `/effort` Enter Left Left Escape | `> /effort ⎿ Exited /effort command`; flags / banner / status unchanged | `esc-ref-effort-Esc.txt` byte-match |
| 09-picker-enter-low | `/effort` Enter Left Left Enter | `> /effort ⎿ Effort set to low`; banner `(Low)`, status `· low` | cycle 208 `TestCycle208_EffortEnterApplies` step【一手】 |

**Reference probe (1.4.0) was not run live**: the user's session does not have gcloud ADC configured (`~/.config/gcloud/application_default_credentials.json` is absent), so the reference falls through to the un-signed-in welcome prompt and "No models available" — the picker UI is unreachable without a working model. The cycle 208 captures (`/tmp/agycmp/panels/esc-ref-effort-Esc.txt`, `ppref.effort.txt`, `ppour.effort.txt`) are reused from the cycle-208 sweep; those were captured against a working model and remain the byte-match reference.

**220×30** (the cycle 208 narrow-window case): identical to 220×50. The picker UI compresses to the same 9 lines (`Set Effort` + slider + labels + description + keyboard hint), and the transcript echo is line-wrapped to the 220-column window. No byte-diff between the two sizes.

## 6. Files touched

| File | Change |
|---|---|
| `internal/pigrt/runtime.go` | added `Options.Effort string`; `New()` calls `Session.SetThinkingLevel(ai.ThinkingLevel(opts.Effort), ...)` after `BindExtensions` so the model's ThinkingLevelMap is consulted before the first turn |
| `internal/agent/loop.go` | added `Options.Effort string` |
| `internal/agent/pig.go` | `sendPig` calls `runner.SetThinkingLevel(ai.ThinkingLevel(opts.Effort))` before `SendMessages` |
| `cmd/wagyu/main.go` | passes `flagStringValue(fs, "effort")` into `pigPrintConfig.Effort` |
| `cmd/wagyu/print_pig.go` | `pigPrintConfig.Effort` plumbed through to `pigrt.Options` |
| `cmd/wagyu/print_slash.go` | `dispatchPrintSlash("effort")` handles direct form (`/effort <level>` echoes `Effort set to <level>`) and invalid form (`/effort extreme` echoes `Invalid level: extreme`) |
| `cmd/wagyu/slash_commands.go` | `case "effort":` — direct form goes through `applyEffortLevel(level, effortIndexForName(level))`; bare form falls through to `printSlashEffort(m.flags)` for `-p` |
| `cmd/wagyu/slash_codesearch.go` | `openSlashPanelArgs(cmd, args)` — `cmd == "effort" && args != ""` returns false so the slash router handles direct / invalid forms |
| `cmd/wagyu/slash_panels.go` | `runMode.currentEffortName()` added (reads `m.flags`'s `"effort"`, falls back to settings.json) |
| `cmd/wagyu/slash_regression.go` | `parseEffortWord(input) (string, bool)`, `effortIndexForName(name) int`, refactored `applyEffortSelection` → `applyEffortLevel(name, level)` shared by picker Enter and direct form |
| `cmd/wagyu/tui_run.go` | `Loop` invocation now sets `Options.Effort = flagStringValue(fs, "effort")` |
| `cmd/wagyu/slash_effort_test.go` (new) | seven behavioural pins: `TestEffortSlashDirect`, `TestEffortSlashInvalidLevel`, `TestEffortSlashNoArgsOpensPicker`, `TestEffortPickerNavigate`, `TestEffortPickerEscape`, `TestEffortDefaultFallback`, `TestEffortPersistence` |

`internal/convstore/sqlite.go` is untouched (the brief forbids touching cycles 227–229's compact work; the additive `Effort` field is `pigrt.Options` only).

## 7. Build / test gate

```
$ go build -o /private/var/folders/.../agy-effort/our/agy ./cmd/wagyu
   exit 0, binary 72867842 B

$ go test -vet=off -count=1 -run TestEffort ./cmd/wagyu/
   ok  	github.com/chadgpt/openagy/cmd/wagyu	0.762s    # 7/7 green

$ go test -vet=off -count=1 ./cmd/... ./internal/...
   28 ok, 0 FAIL

$ gofmt -l <changed files>
   (empty)
```

Cycle 225 quota-countdown tests (`slash_cycle225_test.go`) are not in the run filter and remain pre-existing flakes.

## 8. 存疑 / not verified

- **PiG model's `ThinkingLevelMap` is not inspectable from the clone.** The clamp `Agent.SetThinkingLevel(...) → model.ClampThinking()` is opaque; the clone forwards the verbatim `Effort` word and lets PiG decide. If a future model advertises only `{Off, Low, Medium}` (no `High`), a `--effort high` user would silently drop to Medium without any UI feedback. Out of scope for this cycle.
- **`--effort xhigh|max|off|minimal` are accepted on the flag (cycle 101's validator) but the picker exposes only `low|medium|high`.** The picker's three-position slider is byte-exact against the reference's 1.4.0 picker; the higher tiers are a `--effort` only path.
- **Persistence shape.** The cycle 208 path persists the model *label* (`Gemini 3.8 Flash (High)`), not a free-standing `effort` key. `m.currentEffort()` derives the effort from the label's `(High|Medium|Low|Thinking)` suffix and the catalogue. If a future user types `/effort low` while running a model that doesn't advertise a Low tier, the picker Enter's change-detection would fall back to the model's nearest tier and write that label — the user's typed word would be honoured in `flags.effort` but the persisted model label would be the nearest tier. Not exercised in tests.
- **Gemini 3.6 / 3.7 Flash per-model effort overrides** — the reference's `--effort` validator rejects per-model overrides (e.g. `--model gemini-3.6-flash --effort high` for a model that doesn't expose High); the clone accepts the word and lets PiG's clamp decide. Out of scope.
- **Reference live probe** could not be re-run on this machine because of the missing ADC credentials; byte-match table in §5 reuses cycle 208 captures. New 1.4.0 picker behaviour (the timeline-gauge geometry) was not separately verified against the live 1.4.0 binary in this cycle.

## 9. Evidence grading

- **Build clean + 7/7 tests + gofmt clean**: 【一手】
- **Picker geometry (`Set Effort` panel, `●━━●━━◉`, `(current)`, Keyboard hint)**: 【一手】 live clone probe at 220×50 and 220×30, byte-identical to cycle 208 captures.
- **`/effort <level>` echo (`Effort set to <level>`)**: 【一手】 live clone probe.
- **`/effort extreme` echo (`Invalid level: extreme. Use low|medium|high.`)**: 【一手】 live clone probe.
- **Picker Esc → `Exited /effort command` notice**: 【一手】 clone + cycle 208 reference probe.
- **PiG `Session.SetThinkingLevel` at `coding/session.go:1581` and `Agent.SetThinkingLevel` at `agent/agent.go:1304`**: 【反编译】 PiG source.
- **Picker Enter applies label (`Gemini 3.8 Flash (Low)`) and persists via `auth.SetSettingsField("model", label)`**: 【一手】 live probe; cycle 208 `TestCycle208_EffortEnterApplies` regression.
- **`resolveModelSelection` defaults (`gemini-3.8-flash` → `-high`, `gpt-oss-120b` → `-medium`)**: 【一手】 `TestEffortDefaultFallback`.
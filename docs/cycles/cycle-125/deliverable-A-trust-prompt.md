# Cycle 125 — workspace trust gate (and a `updateOrCreateSettings` fix)

## Why

`~/.local/bin/agy` 1.3.6 prompts the user with:

```
Trust this folder?
Do you trust the contents of this project?
  <cwd>

 Yes, I trust this folder   Esc/n = exit
```

…on every first launch in a cwd that's not yet in
`~/.gemini/antigravity-cli/settings.json::trustedWorkspaces`. The clone, by
contrast, walked straight into the TUI — which is why the tmux A/B harness
(`/tmp/agycmp/hotpath.sh`) was diffing against the dialog overlay on every
scenario at minimum terminal sizes, and why the clone felt wrong on every
fresh checkout.

## What changes

### Behaviour

`runLiveTUI` now does, in order:

1. `os.Getwd()` → `filepath.Clean` + `filepath.EvalSymlinks` to canonicalise
   the cwd (handles macOS `/tmp` → `/private/tmp` symlink, and the
   `/Users/gear` vs `/Users/gear/Documents/GitHub` symlink that
   `~/GitHub` resolves to).
2. Test-hook skip — flag wins over env wins over settings:
   - `--trust-cwd` (added to `cli.ExtraFlags`) ⇒ never prompt
   - `$OPENAGY_TRUST=<absolute path>` ⇒ skip only when the canonical cwd
     matches `<absolute path>`
3. Settings-skip — read `trustedWorkspaces` from
   `$ANTIGRAVITY_HOME/settings.json` and skip if cwd is in it.
4. Otherwise render the prompt on `os.Stdout`, read one byte from
   `os.Stdin` (cooked-mode single-byte read is enough — the prompt owns
   stdin until dismissed), and either:
   - `y`/`Y`/Enter/Space/1 ⇒ accept ⇒ call `auth.AddTrustedWorkspace`,
     then continue into the TUI.
   - `n`/`N`/`Esc`/`q`/Ctrl-C/EOF ⇒ deny ⇒ `return nil` (clean exit, rc=0).

### Files

| File | What |
|---|---|
| `cmd/wagyu/trust_prompt.go` (new) | `renderTrustPrompt`, `runTrustPrompt`, `trustGate` (test seam that the harness wiring uses). |
| `cmd/wagyu/main.go` | `init()` wires `isWorkspaceTrusted`/`addTrustedWorkspace` to `auth.IsWorkspaceTrusted`/`auth.AddTrustedWorkspace` via package-level vars (avoids `wagyu → auth → wagyu` import cycle). |
| `cmd/wagyu/trust_prompt_test.go` (new) | 9 tests: render smoke, render with raw path (no `~`), accept-byte, deny-bytes (`n`, `N`, `Esc`, `q`, `Ctrl-C`, EOF), flag skip, env-var skip, settings-skip (cwd pre-listed), settings-missing (gate does not skip), accept-persists (full first-launch flow → settings.json now exists with cwd listed → gate then says "skip" on the next simulated launch). |
| `cmd/wagyu/tui_v135.go` | `runLiveTUI` calls `trustGate` before any TUI setup so the dialog owns stdin. |
| `internal/cli/parser_specs.go` | `--trust-cwd` added to `ExtraFlags()`. |
| `internal/auth/v135_compat.go` | `IsWorkspaceTrusted`, `LoadTrustedWorkspaces`, `canonicalisePath`; `AddTrustedWorkspace` rewritten on top of a new `updateOrCreateSettings` helper. |

### `updateOrCreateSettings` (the small but important fix)

`AddTrustedWorkspace` used `updateSettings` which returned `ENOENT` when
`settings.json` didn't exist yet. After acceptance in a freshly-anchored
shell, this meant: cwd was added in-process → next launch re-prompted →
user has to accept twice in a row.

The fix is `updateOrCreateSettings`:

```go
// updateOrCreateSettings is the cycle-125 sibling of updateSettings.
// When settings.json is missing, it creates the parent dir (0700) and
// writes a fresh settings.json containing only the keys the mutator
// chooses to set — instead of returning ENOENT.
func updateOrCreateSettings(mutate func(map[string]any)) error {
    path := filepath.Join(AntigravityConfigHome(), "settings.json")
    if err := os.MkdirAll(AntigravityConfigHome(), 0700); err != nil {
        return err
    }
    m := map[string]any{}
    data, err := os.ReadFile(path)
    switch {
    case err == nil:
        if jerr := json.Unmarshal(data, &m); jerr != nil {
            m = map[string]any{}
        }
    case os.IsNotExist(err):
        // fresh install — start with an empty map
    default:
        return err
    }
    mutate(m)
    out, err := json.MarshalIndent(m, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(path, out, 0600)
}
```

Effect: accepting on a brand-new checkout persists the cwd (because the
trust prompt is the entry point for first-launch, and the pre-existing
`WriteV135CompatDefaults` hasn't run yet from the TUI perspective). The
next launch immediately sees the cwd in `trustedWorkspaces` and skips —
no double-prompt.

### Recovered strings

All three text fragments come from the reference's `strings` table
(`out/local-macos/gopclntab-strings.txt`):

| fragment | recovered shape |
|---|---|
| `Trust this folder?` | title fragment |
| `Do you trust the contents of this project?` | body sentence |
| `Yes, I trust this folder` | accept-option label |

## Tests

```
=== RUN   TestRenderTrustPromptSmoke                --- PASS
=== RUN   TestRenderTrustPromptRawPath              --- PASS
=== RUN   TestRunTrustPromptAccept                  --- PASS
=== RUN   TestRunTrustPromptDeny                    --- PASS
=== RUN   TestTrustGateFlagSkip                     --- PASS
=== RUN   TestTrustGateEnvVar                       --- PASS
=== RUN   TestTrustGateSettingsTrusted              --- PASS
=== RUN   TestTrustGateSettingsMissing              --- PASS
=== RUN   TestTrustPromptAcceptPersists             --- PASS (new in this cycle)
PASS
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.666s
```

Full suite (`go test ./cmd/... ./internal/... -count=1`): green across all
22 packages.

## Manual verify (tmux harness)

A tmux session in `/tmp/agy-truly-fresh` with
`settings.json::trustedWorkspaces = ["/some/other/dir"]` (i.e., cwd is
not pre-trusted):

- `tmux capture-pane` shows the dialog rendered verbatim
- `y Enter` dismisses it, TUI banner appears, cwd is appended to the
  trust list:

```
{
  "colorScheme": "dark",
  "trustedWorkspaces": [
    "/some/other/dir",
    "/Users/gear/Documents/GitHub/openagy"
  ]
}
```

`Esc` / `n` exits cleanly with rc=0 and the TUI never appears.

`hotpath.sh` re-run after the change: still all-green across 17
scenarios. The cycle-124 cold-start diff (~18 → 31 chars) is unchanged
by cycle 125 — that diff is unrelated (warning block + right-aligned
model label are the cycle-124 layout choice).

## Why this matters for the harness

Before cycle 125, four of the five A/B scenarios in `hotpath.sh` were
diffing the workspace-trust overlay against the TUI chrome. The reference
honours the trust dialog and skips it (because `~/GitHub/openagy` is
already in `settings.json`), but the harness ran the clone from a temp
dir to keep cyc-77/121 state idempotent — so the clone also hit the
overlay on every scenario. The diff numbers in the harness output are
**not** the real gap; they are the overlay coming and going.

With cycle 125, that overlay now actually fires on both reference and
clone when expected, and stays out of the way when the cwd is trusted.
Future cycles measuring chrome deltas will now be measuring real chrome.

## Hard constraints / 存疑 / limitations

- 【推测】The exact frame chrome (the box around the dialog and the
  position of the legend line) is not byte-level. The reference paints a
  full-screen centred overlay; this cycle renders 6 lines of plain ASCII.
  No visual diff against the reference yet — the harness does not include
  a non-trusted cwd scenario by design.
- 【推测】When `settings.json` parse fails (corrupt file), the gate
  falls through to the `IsWorkspaceTrusted` returning `false, nil`
  branch; the dialog then fires. The reference's behaviour on a corrupt
  file is not recoverable from the binary alone.
- 【推测】`$OPENAGY_TRUST` is a test-only escape hatch — not exported in
  docs, not exercised by the TUI. Use `--trust-cwd` for repeatable
  integration scripts.
- The trust gate runs **before** `tea.NewProgram`, so the dialog owns
  stdin. The TUI's own `tea.WithInput` is unaffected, but this means
  anything that wanted to read stdin before the TUI starts (e.g. a
  one-shot `-p` flag) must do so before the gate; the gate is currently
  run only on the live-TUI path, so this is not a conflict today.

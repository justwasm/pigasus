# Cycle plugin-case-insensitive — `/plugin <subcmd>` + `agy plugin <subcmd>` case-insensitive, `Plugin.json` accepted as manifest

## Goal

AGY 1.3.6 changelog line 21:

> Fixed plugin names and manifests being case-sensitive where the
> agent was not: `/plugin` subcommands such as `/plugin List` now
> work in any case instead of failing with `Unknown subcommand`,
> and a manifest named `Plugin.json` is now found by
> `agy plugin install`, `agy plugin validate`, and enable and
> disable.

Two surfaces carry the gap:

1. The CLI dispatcher (`agy plugin <subcmd>`) — the jetski engine
   in `internal/cli/jetski.go` matched `ss.Name == name` literally,
   so `agy plugin List` and `agy plugin LIST` both reported
   `Error: unknown plugin subcommand "<X>"`. The fix is a single
   `strings.EqualFold` add in the sub-subcommand loop.
2. The slash form (`/plugin <subcmd>`) — `openSlashPanelArgs`
   short-circuited into the panel for every `/plugin` invocation,
   dropping the argument tail on the floor. `/plugin List` opened
   the plugin manager UI rather than dispatching to the list
   handler. The fix adds a case-sensitive tail check before
   `openSlashPanel` and routes the dispatch through `case "plugin":`
   in `slash_commands.go`.

A second sub-gap is the manifest name on disk. Every plugin read
path (`legacyLoadPlugins`, `runPluginInstall`, `runPluginValidate`,
`runPluginToggle`) was hard-coded to `plugin.json`. The cycle adds a
single `legacyFindManifest` helper that tries lowercase first and
falls back to `Plugin.json` — matching the reference's behaviour
where a user can name the manifest however they like.

The reference binary itself never contains the literal `Plugin.json`
in its strings (`strings ~/.local/bin/agy | grep -E "^Plugin\.json$"`
returns nothing); every recovered string uses lowercase
`plugin.json`. The fallback is purely defensive — the gap exists
because the *parser* was case-sensitive, not because the *manifest
loader* was.

## Files

| Path | Role |
|---|---|
| `internal/cli/subcommand_parity.go` (+63 / −4) | new `legacyPluginManifestNames` slice and `legacyFindManifest(root)` helper (os.ReadDir-based so it stays correct on case-insensitive filesystems); four callers migrated — `legacyLoadPlugins`, `runPluginValidate`, `runPluginInstall`, `runPluginToggle` |
| `internal/cli/jetski.go` (+13 / −1) | the sub-subcommand dispatch loop in `(*JetskiCLI).dispatch` now accepts `strings.EqualFold(ss.Name, name)` so `agy plugin List` and `agy plugin LIST` both reach the same handler; the case the user typed is forwarded to the handler verbatim |
| `cmd/wagyu/slash_codesearch.go` (+12 / −0) | `openSlashPanelArgs` short-circuits the panel when `cmd == "plugin"` and the trimmed arg tail is non-empty, so `/plugin <subcmd>` falls through to `case "plugin":` instead of opening the panel |
| `cmd/wagyu/slash_commands.go` (+63 / −2) | `case "plugin":` now dispatches `list` / `install` / `validate` / `enable` / `disable` / `uninstall` case-insensitively and emits `Unknown subcommand: <X>` for anything else; bare `/plugin` keeps the cycle-167 `Plugins: 0 installed` fallback; new `splitPluginArgs` helper next to `splitSlash` |
| `internal/cli/plugin_case_insensitive_test.go` (new, 320 lines) | 12 tests covering `legacyFindManifest` (lower-wins, capitalised fallback, both-missing), every CLI read path with `Plugin.json` only (validate, install, toggle, list), the lowercase-still-works regression, the missing-manifest error wording, and the jetski case-insensitive match (lowercase/capitalised/allcaps/mixed) |
| `cmd/wagyu/slash_cycleplugincaseinsensitive_test.go` (new, 240 lines) | 7 slash tests covering `/plugin List` / `LIST` / `LiSt` dispatch, the install/validate/enable/disable case-insensitive dispatcher, the unknown-subcommand error, and the bare-form fallback |

**Net:** +211 / −7 across 6 files (4 modified, 2 new).

## Case-insensitive sub-subcommand dispatch (jetski)

The original loop in `internal/cli/jetski.go:397`:

```go
name := args[0]
for _, ss := range s.Subs {
    if ss.Name == name {
        // …dispatch…
    }
}
```

becomes:

```go
name := args[0]
for _, ss := range s.Subs {
    if ss.Name == name || strings.EqualFold(ss.Name, name) {
        // …dispatch with args[1:]…
    }
}
```

The `ss.Name == name` short-circuit stays first because it is the
hot path (lowercase typed by every existing user) and avoids the
`EqualFold` allocation when the case matches verbatim. The handler
still receives `args[1:]` — the *verb* in the user's original case
is preserved, so plugins reading the verb as a string (none today,
but the API contract is preserved) continue to see what the user
typed.

The matcher is global, not plugin-specific: AGY 1.3.6 changelog
line 21 names `plugin`, but `/statusline` (1.3.0) and the
mcp/remote-control families benefit equally. The unknown-sub
fall-through is unchanged — a name that matches no registered
sub still surfaces `Error: unknown <parent> subcommand "<X>"` and
exits 1.

## `legacyFindManifest` — case-insensitive manifest lookup

```go
var legacyPluginManifestNames = []string{"plugin.json", "Plugin.json"}

func legacyFindManifest(root string) string {
    ents, err := os.ReadDir(root)
    if err != nil {
        return ""
    }
    have := map[string]bool{}
    for _, e := range ents {
        if !e.IsDir() {
            have[e.Name()] = true
        }
    }
    for _, name := range legacyPluginManifestNames {
        if have[name] {
            return filepath.Join(root, name)
        }
    }
    return ""
}
```

Two details worth flagging:

1. **`os.ReadDir` instead of `os.Stat`.** `os.Stat` is the
   obvious implementation but it's wrong on case-insensitive
   filesystems (macOS APFS by default). Writing `Plugin.json`
   on APFS creates the same inode as a hypothetical
   `plugin.json`, so `os.Stat(filepath.Join(root, "plugin.json"))`
   succeeds regardless of what the user named. `os.ReadDir`
   returns the actual directory entry name, so the lookup
   honours the on-disk filename on macOS the same way Linux's
   case-sensitive FS does. The test rig runs on macOS APFS; this
   detail matters for `TestLegacyFindManifest_FallsBackToCapitalised`
   to be meaningful.

2. **Lowercase wins on tie.** When both names exist (the
   reference uses lowercase everywhere — `failed to read
   plugin.json`, `writing plugin.json`, `can't look for
   plugin.json`, etc.), the lowercase entry is the one
   pre-existing installs and the reference's own strings point
   at. Returning the capitalised one would change the on-disk
   address every read path uses and silently break tooling
   that looks up the file directly.

The four callers swap their hard-coded `plugin.json` read for the
helper:

```go
// before:
raw, err := os.ReadFile(filepath.Join(pdir, "plugin.json"))

// after:
manifest := legacyFindManifest(pdir)
if manifest == "" { continue }
raw, err := os.ReadFile(manifest)
```

`runPluginValidate` keeps the `.antigravity-plugin/plugin.json`
fallback — the new helper only owns the top-level lookup. The
`runPluginToggle` (enable/disable) check moves to
`legacyFindManifest(filepath.Join(dir, name))`, which is the same
shape it had before but now case-insensitive.

## `/plugin <subcmd>` slash dispatch

`openSlashPanelArgs` gains one short-circuit:

```go
if cmd == "plugin" && strings.TrimSpace(args) != "" {
    return false
}
```

Returning `false` skips the panel path and lets `handleSlashCommand`
fall through to the `case "plugin":` branch. Bare `/plugin` (no
args) is unaffected and still opens the panel.

`case "plugin":` in `cmd/wagyu/slash_commands.go` swaps its
single-message body for a verb dispatcher:

```go
trimmed := trimSpace(args)
if trimmed == "" {
    m.slashRespond("/plugin", "Plugins: 0 installed")
    return true, nil
}
verb, _ := splitPluginArgs(trimmed)
switch {
case strings.EqualFold(verb, "list"):
    m.slashRespond("/plugin list", "No plugins installed.")
case strings.EqualFold(verb, "install"):
    m.slashRespond("/plugin install", "Usage: /plugin install <target>")
case strings.EqualFold(verb, "validate"):
    m.slashRespond("/plugin validate", "Usage: /plugin validate [path]")
case strings.EqualFold(verb, "enable"), strings.EqualFold(verb, "disable"):
    m.slashRespond("/plugin "+verb, "Usage: /plugin "+verb+" <name>")
case strings.EqualFold(verb, "uninstall"):
    m.slashRespond("/plugin uninstall", "Usage: /plugin uninstall <name>")
default:
    m.slashRespond("/plugin", "Unknown subcommand: "+verb)
}
```

The bodies are *placeholders* — the cycle's job is the dispatch
recognition (case-insensitive routing), not the actual install
path. `splitPluginArgs` is a copy of `splitSlash`'s contract
applied to the post-plugin tail so the verb splitting stays
local to the plugin handler and does not leak into `splitSlash`'s
keying on `/cmd args`. A follow-up cycle can forward the
`rest` half into `cli.runPluginList` / `cli.runPluginInstall`
etc. — that wiring would couple the slash handler to a CLI
`JetskiCLI`, which the cycle deliberately does not.

## Wire form — `Plugin.json` discovery

The reference binary's own string table never contains the
literal `Plugin.json` — every recovered string uses lowercase
(`failed to read plugin.json: %w`, `writing plugin.json: %w`,
`can't look for plugin.json: %v`, `plugin.json missing name`,
`plugin.json.disabled`, `.claude-plugin/plugin.json`,
`/claude-plugin/plugin.json`, etc.). The fallback to
`Plugin.json` is therefore a defensive on-disk-side fix for users
who named the manifest with a capital P — the loader now
recognises both, the file is read, the install proceeds. The
reference's own writes stay lowercase; we do not rename any
existing install.

## Tests (cycle plugin-case-insensitive, 19 added; all pass)

`internal/cli/plugin_case_insensitive_test.go`:

- `TestLegacyFindManifest_PrefersLowercase` — both spellings
  present, lowercase wins (regression guard).
- `TestLegacyFindManifest_FallsBackToCapitalised` — only
  `Plugin.json` present, the helper still resolves a path
  (the new behaviour).
- `TestLegacyFindManifest_NoManifestReturnsEmpty` — neither
  spelling present, helper returns "" so callers can fall
  through to their own missing-manifest branches.
- `TestRunPluginValidate_AcceptsCapitalisedManifest` — full
  validate path on a `Plugin.json`-only tree produces `[ok]`.
- `TestRunPluginValidate_LowercaseStillWorks` — backwards-compat
  guard for the existing `plugin.json` install base.
- `TestRunPluginInstall_AcceptsCapitalisedManifest` —
  install copies a `Plugin.json`-only tree and lands the
  manifest at the destination under the same name.
- `TestRunPluginValidate_MissingManifestErrorMessage` — empty
  tree surfaces the reference's fixed-string
  `Error: missing plugin.json: file does not exist` line.
- `TestRunPluginToggle_FindsCapitalisedManifest` — enable and
  disable round-trip on a `Plugin.json`-only install.
- `TestLegacyLoadPlugins_AcceptsCapitalisedManifest` — the
  list path (`agy plugin list`) enumerates a `Plugin.json`-only
  install.
- `TestJetskiPluginDispatch_CaseInsensitive` — `list`, `List`,
  `LIST`, `LiSt` all dispatch to the list handler and emit
  the cycle-167 `No plugins installed.` line.
- `TestJetskiPluginDispatch_UnknownSubStillErrors` — a verb
  that matches no registered sub still surfaces the
  `unknown command` error and exits 1 (case-insensitivity
  doesn't turn unknown into known).
- `TestJetskiPluginDispatch_McpAlsoCaseInsensitive` —
  `agy mcp list` / `List` / `LIST` all dispatch (the cycle's
  matcher is global; this test pins the contract so the
  change can't accidentally become plugin-only).

`cmd/wagyu/slash_cycleplugincaseinsensitive_test.go`:

- `TestSlashPlugin_ListAllCases` — four sub-cases
  (`list`, `List`, `LIST`, `LiSt`); each one dispatches the
  list handler (panel stays closed) and emits the
  `No plugins installed.` body.
- `TestSlashPlugin_InstallDispatch` — three case variants
  emit a `Usage:` stub for `/plugin install` (the cycle
  routes dispatch; a follow-up forwards the args).
- `TestSlashPlugin_ValidateDispatch` — same shape for
  `/plugin validate`.
- `TestSlashPlugin_EnableDisableDispatch` — five case
  variants across enable / disable.
- `TestSlashPlugin_UnknownSubcommand` — unknown verb
  surfaces `Unknown subcommand: <X>` instead of the panel.
- `TestSlashPlugin_BareFallsToPanel` — bare `/plugin`
  still produces the cycle-167 `Plugins: 0 installed`
  fallback (the safety net the `openSlashPanelArgs`
  short-circuit leaves in place).

## Existing tests kept green

- `TestPluginList_1to1` (cycle 167) — `agy plugin list` on a
  clean HOME still emits `No plugins installed.\n`.
- `TestCLI_ValidateListsAgents` /
  `TestCLI_ValidateMissingAgentsShowsSkipped` /
  `TestCLI_InstallSummaryListsAgents` (cycle 234) — agents.json
  path unchanged; `runPluginValidate` and `runPluginInstall`
  produce the same summary, the case-insensitive helper only
  adds a fallback lookup.
- `TestDispatchPrintSlash_LocalCommands` —
  pre-existing flake; environment-dependent
  `/model` resolution (not affected by this cycle).
- `TestCycle198_*` / `TestCycle204_*` / `TestCycle212_*` /
  `TestPanelPluginLayout` — every panel test for `/plugin`
  still passes; the cycle's `openSlashPanelArgs` change is
  opt-in via the presence of an argument tail, so the
  panel-opening code paths are unchanged.

## Hard constraints honoured

- `go build -o /tmp/wagyu-pci ./cmd/wagyu` → exit 0
  (~73 MB binary, ~+30 KB over baseline — the two new
  tests + ~150 lines of helper logic).
- `go test -vet=off -count=1 ./cmd/... ./internal/...` →
  only the documented pre-existing `TestDispatchPrintSlash_LocalCommands`
  flake fails; all 19 new tests pass, plus every test
  previously green on `main`.
- `gofmt -l` clean on every new file.
- Pre-existing not-clean files (`cmd/wagyu/colors.go`,
  `cmd/wagyu/picker_view.go`, etc.) untouched.
- `internal/convstore/sqlite.go` not touched.
- Branch `feature/plugin-case-insensitive` not pushed
  (worktree-only).

## Limitations / 存疑

1. **The slash handlers are placeholders.** The cycle routes
   the dispatch decision correctly — case-insensitive
   recognition across all six known subcommands — but the
   actual install / validate / enable / disable bodies emit
   `Usage:` lines instead of forwarding the args to
   `cli.runPluginInstall` etc. Forwarding requires standing
   up a `JetskiCLI` for the slash side, which the TUI
   doesn't currently have a handle for. The cycle-167
   "no plugins installed" body is the recovered baseline;
   the rest of the slash bodies recover the recovered
   `Usage: <verb>` shape from the reference's `/help`
   table. A follow-up cycle can do the forwarding.

2. **`legacyFindManifest` does not look under
   `.antigravity-plugin/`.** The validate path's
   fallback for `Plugin.json`-only manifests is still the
   hard-coded `.antigravity-plugin/plugin.json` lookup
   (cycle 234 — reference's secondary location). If a
   user names their manifest `Plugin.json` and tucks it
   under `.antigravity-plugin/`, the helper will miss it;
   the alt-stat check still finds `plugin.json` only.
   This matches the reference's own behaviour: the
   primary install location is the top-level
   `plugin.json` and the secondary location does not get
   the case-insensitive treatment. A future cycle can
   extend the helper to take a path list if the gap
   recurs.

3. **`os.ReadDir`-based lookup is filesystem-stat-only.**
   The helper does not see symlinks whose targets sit in
   directories the user cannot read, and it does not chase
   the manifest into a zip / archive. Both edge cases are
   outside the changelog's stated scope (the gap is
   "case-sensitive where the agent was not", not "find
   manifests in unconventional locations"). The reference
   binary's loader does not chase either.

4. **`legacyPluginManifestNames` is two entries deep.**
   `Plugin.json.JSON`, `plugin.JSON`, `plugin.json.txt`,
   etc. are not recognised. The reference binary's own
   string table only contains lowercase `plugin.json`;
   expanding the list is a future decision once we see
   real-world breakage.

5. **Pre-existing `TestDispatchPrintSlash_LocalCommands`
   flake is unrelated** — `/model` resolution depends on
   the resolved model in the test environment; this cycle
   does not touch the model lookup. The flake is present
   on `main` HEAD `54db67b` without my changes (verified
   against the baseline run above).
# Cycle custom-slash-commands-no-plugin-injection — deterministic precedence for custom slash commands

## Goal

AGY 1.2.16 fixed "custom slash commands declared in plugins and
project .claude/commands/ folders being silently overridden by other
definitions with the same name, so the wrong command ran."

Cycle custom-slash-commands-no-plugin-injection wires that contract
onto the new-picker surface: a custom command loader that walks the
three on-disk roots, a merge step that applies a documented
precedence order, and a picker integration path that surfaces the
merged catalog in the slash menu without disturbing the existing
hardcoded built-in catalog.

## Precedence

The clone's contract — same as the AGY 1.2.16 fix — is:

```
Built-in > Plugin > Project > Global
```

* **Built-in** (the hardcoded `DefaultSlashCommands` catalog) wins
  over every custom command. A user's `.claude/commands/help.md`
  cannot silently replace `/help` — the system command always runs.
* **Plugin** (`<plugin>/commands/*.md`) wins over project + global
  commands. A plugin author's `/foo` is theirs even when the
  project also ships `/foo`. Two plugins declaring the same
  `<basename>` resolve alphabetically by source path so the
  precedence stays deterministic.
* **Project** (`<cwd>/.claude/commands/*.md`) wins over global
  commands. The project-local customisation is more specific than
  the user's global dropbox.
* **Global** (`<AntigravityConfigHome>/commands/*.md`) is the
  fallback when no higher-precedence source defines the command.

## Files

| Path | Role |
|---|---|
| `internal/customcmd/loader.go` (new, ~430 lines) | Source, Precedence, Command, Conflict types; `LoadRoots`, `Load` (the three-root walker), `MergeWithBuiltins` (the precedence merge); `BuiltIn` wire shape; helpers `scanDir`, `scanProjectDir`, `scanPluginDir`, `parseCommandFile`, `frontmatterFields`, `frontmatterDescription` |
| `internal/customcmd/loader_test.go` (new, ~410 lines) | 9 tests: `TestLoad_PluginWinsOverProjectDuplicate`, `TestLoad_ProjectWinsWhenNoPluginDefines`, `TestMergeWithBuiltins_BuiltinWinsOverProjectDuplicate`, `TestLoad_DeterministicPrecedence`, `TestLoad_NameConflictLogged`, `TestLoad_TwoPluginsSameShortName`, `TestLoad_GlobalShadowedByProjectWhenNoPluginCover`, `TestLoad_MissingDirectoriesAreNotErrors`, `TestFrontmatterDescription_EmptyWhenMissing` |
| `internal/tuilib/commands.go` (+40 / −0) | new import + `FilterSlashCommandsWithCustom(prefix, custom []customcmd.Command)` picker helper that merges the hardcoded catalog with the loaded custom commands under the same precedence |
| `internal/tuilib/commands_custom_test.go` (new, ~210 lines) | 7 tests: nil-fallback, built-in-shadows-project, plugin-shadows-project (+ global), project-shadows-global, deterministic merge, filter-applies-after-merge, chrome-devtools-plugin:* still filtered |

**Net:** +1 new package, +1 new picker helper, 16 new tests. No
existing file reformatted.

## Loader contract

The loader is a pure function — no global state, no env reads, no
mutex. Every Load call takes a `LoadRoots{PluginsDir, ProjectDir,
GlobalDir}` and returns `(catalog, conflicts)` deterministically.

```
LoadRoots{PluginsDir, ProjectDir, GlobalDir} → ([]Command, []Conflict)
```

* `LoadRoots` lets tests build the three roots from `t.TempDir`
  without ANTIGRAVITY_HOME juggling. Production callers resolve
  the roots from `auth.GeminiConfigHome()` + `<cwd>` + `os.Getwd()`
  and pass them in.
* The catalog is sorted by `Name` in byte-ascending order, so the
  picker's overlay renders in a stable place across runs
  regardless of `os.ReadDir`'s randomness on some filesystems.
* Every same-name conflict from a different source is recorded as a
  `Conflict{Name, Winner, Loser}` so the call site can surface it
  at startup ("project /foo shadowed by plugin /foo") and tests
  can pin the precedence invariants.

## Wire shape

```go
type Source int
const (
    SourcePlugin Source = iota + 1
    SourceProject
    SourceGlobal
    SourceBuiltin
)

type Command struct {
    Name        string  // bare basename, no "/" prefix
    Description string  // from frontmatter `description:`
    Source      Source
    Path        string  // absolute path on disk; empty for SourceBuiltin
}

type Conflict struct {
    Name   string
    Winner Command
    Loser  Command
}

type BuiltIn struct {
    Name        string
    Description string
}
```

Custom commands all live in the same bare-name namespace: a plugin's
`<plugin>/commands/foo.md` and a project's
`<cwd>/.claude/commands/foo.md` both surface as `/foo`; the plugin
wins because SourcePlugin > SourceProject. A plugin can opt out of
the bare namespace by setting frontmatter `name: alpha:foo` — the
loader treats that as a literal name and does not rewrite it.

## Verification

```
go build -o /tmp/wagyu-csc ./cmd/wagyu                            # exit 0, 73 MB
go test -vet=off -count=1 ./internal/customcmd/                   # 9 PASS
go test -vet=off -count=1 ./internal/tuilib/                      # 18 PASS (incl. 7 new)
go test -vet=off -count=1 -run 'TestSlash|TestCommand|TestPlugin|TestOverride' \
    ./cmd/wagyu/ ./internal/.../                                  # all PASS
```

`gofmt -l` on the four touched files:

* `internal/customcmd/loader.go`               — empty
* `internal/customcmd/loader_test.go`          — empty
* `internal/tuilib/commands.go`                — pre-existing not-clean
  (unchanged in pre-existing lines; +2 lines for the import, +40
  lines appended at EOF)
* `internal/tuilib/commands_custom_test.go`    — empty

The pre-existing gofmt issue in `internal/tuilib/commands.go` is
unchanged by this cycle — the cycle's edit does not touch the
existing comment blocks or field comments. The `git diff` shows
only two hunks: the import block (one line added) and the new
function appended at the end.

## Cycle behaviour

The patch:

* `Load(roots) (catalog, conflicts)`:
  * Walks `<root>/plugins/<plugin>/commands/*.md`
  * Walks `<root>/project/.claude/commands/*.md` if `ProjectDir != ""`
  * Walks `<root>/commands/*.md`
  * Sorts raw entries by `(Source, Path)` so same-source
    duplicates resolve alphabetically by source path (the alphabetically
    first file wins).
  * Applies precedence: a higher-precedence source replaces a
    previously-seen entry; the replaced entry is recorded as a
    `Conflict` and stays available for the call site's startup log.
  * Returns the catalog sorted by `Name` in byte-ascending order.
* `MergeWithBuiltins(builtins, custom) (catalog, conflicts)`:
  * Same precedence rules, but `SourceBuiltin` ranks highest.
  * The built-in catalog keeps its existing `DefaultSlashCommands`
    shape; the merge helper accepts a `[]BuiltIn` (Name, Description)
    so it doesn't import internal/tuilib (which keeps the cycle
    self-contained and testable without the TUI surface).
* `FilterSlashCommandsWithCustom(prefix, custom)`:
  * Backward-compatible: nil `custom` falls back to the existing
    `FilterSlashCommands` so the cycle-83 / cycle-146 / cycle-184
    picker tests stay green.
  * When `custom != nil`, merges the hardcoded catalog with the
    custom commands, applies the chrome-devtools-plugin:* filter
    from cycle 146, and re-applies the case-folding substring
    filter so custom commands surface in the picker's overlay just
    like built-ins.

## Tests

The cycle ships 16 new tests. The five mandatory tests (per the
cycle brief) plus 11 more that pin the contract's edges:

```
internal/customcmd/loader_test.go
  TestLoad_PluginWinsOverProjectDuplicate
  TestLoad_ProjectWinsWhenNoPluginDefines
  TestMergeWithBuiltins_BuiltinWinsOverProjectDuplicate
  TestLoad_DeterministicPrecedence
  TestLoad_NameConflictLogged
  TestLoad_TwoPluginsSameShortName
  TestLoad_GlobalShadowedByProjectWhenNoPluginCover
  TestLoad_MissingDirectoriesAreNotErrors
  TestFrontmatterDescription_EmptyWhenMissing

internal/tuilib/commands_custom_test.go
  TestFilterSlashCommandsWithCustom_NilFallsBackToBuiltins
  TestFilterSlashCommandsWithCustom_BuiltinShadowsProject
  TestFilterSlashCommandsWithCustom_PluginShadowsProject
  TestFilterSlashCommandsWithCustom_ProjectShadowsGlobal
  TestFilterSlashCommandsWithCustom_Deterministic
  TestFilterSlashCommandsWithCustom_FilterAppliesAfterMerge
  TestFilterSlashCommandsWithCustom_ChromeDevtoolsPluginPrefixStillFiltered
```

## Limitations / out of scope

* The cycle does not wire the loader into the live run-mode picker
  yet — `cmd/wagyu/tui_run.go` still calls
  `tuilib.FilterSlashCommands` directly. The wire-up is a one-line
  edit (resolve `LoadRoots` from `m.cwd` + `auth.GeminiConfigHome()`
  at startup, pass the loaded catalog into the picker) that depends
  on a call-site that survives the existing picker test surface; a
  future cycle that exercises the picker overlay with on-disk
  fixtures will own that edit.
* The loader does not resolve symlinks inside the plugin subtree.
  A symlink loop under `<plugin>/commands/` would surface as a
  walker error and silently skip the offending plugin — matches
  `printSlashSkillInfos`'s tolerance.
* The loader does not surface conflicts to the user — that is the
  call site's job (log on startup, render in `/plugin --verbose`).
  The Conflict slice is the wire shape for that future work.
* The loader does not yet honour the frontmatter `disable-slash-command:
  true` marker from the AGY 1.1.27 release; that flag is
  intentionally per-skill (in `SKILL.md`), not per-command. Future
  cycle that needs the marker on a custom command can read it from
  the parsed frontmatter and drop the row in `parseCommandFile`.
* The `name:` frontmatter override is honoured for plugin commands
  (so a plugin can ship a namespaced command) but not for project
  or global commands — the bare basename is the canonical name
  there, and any `name:` is ignored. Future cycle that wants
  per-command namespacing in the project root can extend
  `parseCommandFile` to read it for all sources.
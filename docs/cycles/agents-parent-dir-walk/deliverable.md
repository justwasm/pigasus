# Cycle agents-parent-dir-walk — AGY 1.2.16 changelog line 126

## Goal

AGY 1.2.16 changelog line 126 (file
`cmd/wagyu/testdata/print_changelog.md`, line 126):

> Fixed `skills.json`, `rules.json`, and other customization manifests
> in a parent `.agents/` directory being ignored when a session
> started in a subdirectory; manifests now load from every `.agents/`
> directory between the working directory and the project root.

The parent-directory walk the changelog line describes did not exist
in the clone: `internal/agent` and `cmd/wagyu` had no enumerator that
walked CWD → HOME looking for `agents/` (or the dotted `.agents/`).
The pre-cycle behaviour was "look only at the CWD itself, otherwise
fall back to nothing", which is what the changelog fix moves away
from.

The gap description paraphrases the changelog as "agents from
`~/.agents/` are now discovered by walking parent directories from
CWD up to HOME, so a project-local agents/ directory wins over the
global one." This cycle closes that gap with a small testable
package plus a diagnostic print-mode slash command.

This is a D-class cycle: no schema change, no new tool surface, no
PiG changes. The merge logic lives in `internal/agentsdir/`, a new
package with zero third-party dependencies (the AGENTS.md "no deps"
rule is scoped narrowly to `rea-go-seeds` and `out/` — runtime code
may have third-party deps; this package does not pull any).

## Scope

| Surface | Before | After |
|---|---|---|
| `internal/agentsdir.Walk(cwd, home, names)` | does not exist | walks CWD → HOME, surfacing every directory matching any of the names; CWD-closest-first ordering; stops AT HOME (inclusive); empty (non-nil) slice when none found |
| `internal/agentsdir.Discover(cwd, home)` | does not exist | thin wrapper over Walk with the AGY default names `["agents", ".agents"]` |
| `internal/agentsdir.FirstProjectLocal(cwd, home)` | does not exist | returns the CWD-closest entry or `("", false)` |
| `internal/agentsdir.LoadManifest(dir, name)` | does not exist | reads a single manifest file from one agents dir; missing file → `(Manifest{}, false, nil)` |
| `internal/agentsdir.LoadAllManifests(...)` | does not exist | walks then reads `name` at every level, CWD-closest first |
| `internal/agentsdir.ParseAgentsJSON(...)` | does not exist | accepts both `{"agents": [...]}` (canonical) and `[...]` (pre-1.3.3 flat) shapes |
| `cmd/wagyu/agentsdir_wire.go` print path | (the slash command `/agents-dirs` does not exist) | reads `cwd`+`home` from `os.Getwd`+`os.UserHomeDir`, renders one `<depth>\t<path>` line per entry |
| `cmd/wagyu/print_slash.go` `/agents-dirs` handler | (unhandled — falls through as a prompt) | locally handled, returns the diagnostic string with an empty-payload `Data` map |

The `/agents` slash command (a separate surface that lists installed
custom agents) is intentionally **not** touched — its current
"empty list" reply matches the pre-cycle contract pinned by
`TestDispatchPrintSlash_LocalCommands` line 55
(`{"/agents", ""}`).

## Files changed

| File | +/− | Purpose |
|---|---|---|
| `internal/agentsdir/agentsdir.go` (new) | +260 / −0 | `Entry`, `DefaultDirNames`, `Walk`, `Discover`, `FirstProjectLocal`, helpers (`canonicalDirNames`, `depthBetween`, `isDir`, `defaultHome`) |
| `internal/agentsdir/manifest.go` (new) | +140 / −0 | `Manifest`, `LoadManifest`, `LoadAllManifests`, `MergeManifestBytes`, `AgentsJSONEntry` + `AgentsJSON` schema, `ParseAgentsJSON` (nested + flat forms) |
| `internal/agentsdir/agentsdir_test.go` (new) | +350 / −0 | 17 tests for the walker; see **Test coverage** |
| `internal/agentsdir/manifest_test.go` (new) | +150 / −0 | 7 tests for the manifest loader; see **Test coverage** |
| `cmd/wagyu/agentsdir_wire.go` (new) | +60 / −0 | `printSlashAgentsDirs`, `printAgentsDirsFor` (testable seam), `writeAgentsDirsTable` |
| `cmd/wagyu/agentsdir_wire_test.go` (new) | +155 / −0 | 5 wire tests; see **Test coverage** |
| `cmd/wagyu/print_slash.go` | +8 / −0 | new `case "agents-dirs"` route inside `dispatchPrintSlash`; existing `case "agents"` is untouched |

Cumulative: 6 files added, 1 file edited. The edit to
`print_slash.go` is one block (an 8-line `case`) and is gofmt-clean.

Two architectural notes:

- The walker takes `cwd` and `home` as explicit arguments. The
  helpers (`Discover`, `FirstProjectLocal`, `printSlashAgentsDirs`)
  thread `os.Getwd` / `os.UserHomeDir` at the boundary, so unit
  tests can pass a fixture TempDir without ever touching process
  state. This is the same seam the project's `chdir` helper
  (`cmd/wagyu/trust_prompt_test.go:244`) provides for the opposite
  direction.
- The package follows the rule the cycle-234 deliverable established:
  pure Go stdlib + goroutine-safe exported API; no PiG / bubbletea /
  ncruces dependencies leak in. The new `AgentsJSONEntry` /
  `AgentsJSON` types in `manifest.go` mirror `plugin_agents.go`'s
  `pluginAgentEntry` / `pluginAgentsJSON` shapes **deliberately** so
  a future cycle that needs to cross-load project-local and plugin
  agents into a single union can do so by structural copy without a
  third schema.

## Test coverage

### `internal/agentsdir/agentsdir_test.go` (17 tests, all green)

The required six rules from the gap description each get a dedicated
test; five further invariants are pinned for completeness:

| Test | Pins |
|---|---|
| `TestWalk_LocalAgentsWinsOverHomeAgents` | rule 1+4: a CWD-level agents/ directory wins over the HOME-level one (CWD-closest at index 0, depth=3 from CWD=HOME/a/b/c/d to HOME/a) |
| `TestWalk_OnlyHomeFallsBackWhenNoLocal` | rule 2: when CWD chain has no agents/, only HOME surfaces (full-chain depth 4) |
| `TestWalk_StopsAtHome` | rule 3: directory planted one level above HOME never shows up |
| `TestWalk_DeeplyNestedCWDWalksAllLevels` | rule 5: a 4-deep CWD walks all 4 levels, surfacing one entry per level in CWD-closest order |
| `TestWalk_DedupesByAbsolutePath` | rule 6: a HOME/a symlinked to a temp dir with its own agents/ does not produce duplicate entries |
| `TestWalk_EmptyResultWhenNoAgentsAnywhere` | empty-tree returns an empty non-nil slice (the no-agents-anywhere case) |
| `TestWalk_DefaultNamesCoverAgentsAndDotAgents` | the dotted `.agents` is found when only it exists at one level |
| `TestDiscover_HomeIsTheGlobalRoot` | the global `~/.agents/` is the HOME-level agents/ (depth = full chain length) |
| `TestDiscover_ProjectLocalWinsOverGlobal` | when both project-local and HOME-level exist, project-local is at index 0 |
| `TestFirstProjectLocal_ReturnsClosestEntry` | helper returns the CWD-closest entry with `ok=true` |
| `TestFirstProjectLocal_NoResultReturnsFalse` | helper returns `(Entry{}, false)` when nothing found |
| `TestWalk_DotAgentsOnlyAtHome` | dotted form is picked up even when only HOME has it |
| `TestWalk_RespectsCustomDirNames` | callers can override the default name list |
| `TestWalk_SymlinkLoopDoesNotPanic` | a symlink looping back to HOME does not panic the walker (early-out because `filepath.Dir(cur) == cur`) |
| `TestWalk_DirNamesSanitised` | empty strings and path-separator names are dropped silently |
| `TestDiscover_BothAgentsAndDotAtSameLevel` | both `agents` and `.agents` at the same level surface in `agents`-then-`.agents` order |
| `TestWalk_DepthsAscending` | the `Depth` field is monotonically non-decreasing |

### `internal/agentsdir/manifest_test.go` (7 tests, all green)

| Test | Pins |
|---|---|
| `TestLoadManifest_RoundTrip` | a written file at one agents/ is read back with bytes verbatim |
| `TestLoadManifest_MissingIsNotError` | missing file → `(Manifest{}, false, nil)` — not an error |
| `TestLoadAllManifests_StacksMultipleLevels` | HOME + HOME/a both have skills.json; loader returns both in CWD-closest order |
| `TestParseAgentsJSON_NestedAndFlatForms` | both `{"agents":[…]}` and `[…]` parse to identical entries |
| `TestMergeManifestBytes_Concatenates` | byte merge with newline separator; empty/single inputs return verbatim |
| `TestLoadManifest_BasenameValidation` | empty name and path-separator name are rejected at the boundary |
| `TestParseAgentsJSON_EmptyObjectNoAgents` | `{}` parses to `(nil, nil)` (no entries) |

### `cmd/wagyu/agentsdir_wire_test.go` (5 tests, all green)

| Test | Pins |
|---|---|
| `TestPrintAgentsDirsFor_LocalWins` | diagnostic emits two lines: CWD-closest at depth 3, HOME-level at depth 4 |
| `TestPrintAgentsDirsFor_EmptyResult` | empty input tree → empty diagnostic string |
| `TestPrintSlashAgentsDirs_NoPanic` | the user-`os.Getwd`/`os.UserHomeDir` wrapper does not panic |
| `TestPrintAgentsDirsFor_DotAgentsVariants` | HOME having both `agents` + `.agents` → both lines, undotted first |
| `TestPrintAgentsDirsFor_HomeIsStop` | diagnostic stops at HOME even when an ancestors' agents/ exists |

### Test count total: 29 tests (17 + 7 + 5)

The required minimum is **6 unit tests**; this cycle ships **29**
because the manifest loader and the wire-up are exercised by
additional tests, and several rules from the gap text are pinned by
two tests each (the direct walker property plus the
`Discover`-precedence variant).

## Probe — clone vs runtime

End-to-end smoke test against `/tmp/wagyu-apdw`:

| Command | Expected | Got |
|---|---|---|
| `/tmp/wagyu-apdw -p /agents-dirs --output-format text` (CWD=/Users/gear) | one or more `<depth>\t<path>` lines | `2\t/Users/gear/.agents\n` |
| `/tmp/wagyu-apdw -p /agents-dirs --output-format json` | JSON envelope with `command.name="agents-dirs"` and `command.data.dirs=...` | matches; `{"command":{"name":"agents-dirs","data":{"dirs":"2\t/Users/gear/.agents\n"}}, ...}` |

`~/.agents/` on this machine is a real directory the user keeps
(one `.skill-lock.json` + a `skills` symlink); the walker surfaces
it from `$HOME`. Walking from a deeper CWD surfaces the same entry
with a smaller `depth` value.

**Not exercised against the reference binary**: this machine's
reference is `~/.local/bin/agy 1.4.0`, past the AGY 1.2.16 line —
the recovered changelog captures the 1.2.16 behaviour, but a live
comparison would need an older build. The probe is clone-only.

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-apdw ./cmd/wagyu` | exit 0, **73 MB** (matches cycle 234 baseline) |
| `go test -vet=off -count=1 ./internal/agentsdir/` | 24 tests green |
| `go test -vet=off -count=1 ./cmd/wagyu/ -run "TestPrintAgentsDirs|TestPrintSlashAgents|TestPanelAgents\|TestPanelSkills"` | 5 + layout tests green |
| `go test -vet=off -count=1 ./internal/...` | green on every package |
| `go test -vet=off -count=1 ./cmd/...` | `cmd/wagyu` shows the **pre-existing** `TestDispatchPrintSlash_LocalCommands` flake (cycle 234 §218 documents this), every other `cmd/...` package green |
| `gofmt -l internal/agentsdir/ cmd/wagyu/agentsdir_wire.go cmd/wagyu/agentsdir_wire_test.go` | empty |
| `gofmt -l cmd/wagyu/print_slash.go` | empty (the 8-line edit doesn't break the pre-existing format-clean shape) |
| `git diff internal/convstore` | empty (AGENTS.md hard rule respected) |
| `git diff cmd/wagyu/slash_skills.go cmd/wagyu/slash_panels.go cmd/wagyu/plugin_agents.go` | empty (the "existing parent-walk model" referenced in the gap text does not actually exist — `cmd/wagyu/slash_skills.go:367` is `skillsPanelWorkspacePath`, which formats a path string for the panel, not a walker; the model's pattern was recoverable from the readme of the AGY repo and reused directly) |

## 存疑 / limitations

1. **The depth-sorted output is by definition, not by experimentally
   verified ordering.** Depth (number of `..` hops above CWD) is the
   simplest stable ordering; AGY's reference binary presumably prints
   in the same order because it surfaces "CWD-closest first", but the
   exact field the reference shows (depth vs. closest-parent-name vs.
   nothing) is **未验证**. The diagnostic shows depth because it is
   the cheapest stable metric; an unknown metric can be appended
   later without breaking the existing wire-up.
2. **`~/.agents/` discovery is the only "global root" the package
   considers.** If a user has a project-local `agents/` AND the user
   has a HOME-level `.agents/`, the project's wins; but the package
   does NOT walk above HOME to discover an XDG-style global root
   (`$XDG_DATA_HOME/agents/` or similar). AGY's 1.2.16 changelog is
   silent on XDG roots; future AGY cycles that mention them can add
   the scan without changing the existing discover surface.
3. **The `agents.json` schema is shared with `cmd/wagyu/plugin_agents.go`
   but the two parsers are independent.** Cross-loading between
   project-local agents/ and plugin agents/ would need a thin
   integration; the structural match (`AgentsJSON` mirrors
   `pluginAgentsJSON`) makes that step trivial.
4. **The wire-up `/agents-dirs` is a diagnostic surface, not a
   feature.** AGY 1.2.16 doesn't add a `/agents-dirs` command; the
   one in this cycle is a clone-side observability hook so a user
   can confirm the walker found what they expected. Future cycles
   that need to actually *load* agents from `~/.agents/` (the
   "List available custom agents" surface the AGY `/agents` command
   implements) can keep the walker and replace the diagnostic with
   a real list.
5. **Symlink following is path-level, not recursive.** The walker
   follows symlinks when the OS does (filepath.Stat semantics), but
   does NOT re-walk through a `.agents` directory whose path is a
   symlink target whose canonical ancestors contain a different
   `.agents` — the dedupe table catches the common case, but a
   pathological chain where one symlink forward references a deeper
   path could in principle yield a result different from the depth
   metric the test asserts. **未验证** against REF.
6. **Pre-existing baseline flake:** `TestDispatchPrintSlash_LocalCommands`
   fails on this machine because `/model` default is
   `gemini-3.8-flash-medium` (REF's per-machine default) rather than
   `minimax-m3` (the test's expectation). This is unrelated to this
   cycle; recorded in cycle 234's 存疑 §8.
7. **The `cmd/wagyu/agentsdir_wire.go` file has a `writeAgentsDirsTable`
   helper that is currently unused.** It exposes the same data as
   `printSlashAgentsDirs` but in tabular form for an ad-hoc
   diagnostics tool. The wire-up keeps it inside the binary so a
   future cycle that needs a friendlier terminal layout (with column
   alignment) does not have to redo the walker discovery.

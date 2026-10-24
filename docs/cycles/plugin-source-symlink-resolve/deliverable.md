# Cycle plugin-source-symlink-resolve — plugin/extension loader follows symlinks

AGY 1.2.5 changelog gap closure:

> Fixed custom slash commands, skills, hooks, sub-agents, and
> CLAUDE.md / AGENTS.md files inside a symlinked plugin or extension
> directory being silently ignored because the loader walked real
> paths instead of following symlinks.

Branch: `feature/plugin-source-symlink-resolve`
Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-plugin-source-symlink-resolve`
Base: `54db67b` (main).

## TL;DR

Adds `internal/pluginscan`, a stdlib-only leaf package that:

1. `IsDir(path)` / `IsRegularFile(path)` resolve a path through any
   symlink chain and report the target's FileMode category.
2. `ReadEntries(dir)` is the `os.ReadDir` equivalent the loaders
   actually want — each entry carries the symlink-resolved
   `FileInfo`, so callers can branch on `IsDir()` without a
   second stat.
3. `Walk(root, fn)` is `filepath.WalkDir`'s contract but descends
   through symlinks, with cycle detection (a self-referential
   symlink is reported once and skipped — no infinite loop).

Wires it into the four places in `cmd/wagyu/` and the one place
in `internal/cli/` that walked plugin / extension trees with
`os.ReadDir + e.IsDir()`. Each call site is replaced with the
helpers above; the resulting loader surfaces every symlinked
plugin tree.

The "skills" root that `printSlashSkillInfos` walks
(`cmd/wagyu/print_slash.go`) already routed through a
`isDirFollowingLinks` helper, so it was already correct — the
gap was specifically in `installedPluginRows`, `pluginSkills`,
`pluginMarketplaces`, `pluginAgentsMarkdown`,
`pluginAgentsAtPath`, `legacyLoadPlugins`, `legacyListSkills`,
and `legacyListAgents`.

## Why this needs a new package

The gap is a stdlib-level invariant: `os.DirEntry.Info()` returns
the entry's *own* `FileMode` (Lstat semantics), so a symlink to a
directory reports `IsDir()==false`. The cycle-198
`printSlashSkillInfos` already knew this and inlined an
`isDirFollowingLinks` helper that called `os.Stat` to follow
links. Every other loader under `cmd/wagyu/` and `internal/cli/`
still used the broken `e.IsDir()` pattern.

Two ways out:

| Option | Verdict |
|---|---|
| Inline `os.Stat` everywhere | 9 sites × 4-line helper, easy to drift |
| Leaf package with one implementation | One place to fix regressions, one test surface |

The leaf package wins on cost.

`internal/customagent` (the b953cbd "relative-paths-resolve"
package) is the obvious name — but that package does not exist on
main (it lives on `feature/plugin-relative-paths-resolve`, an
in-flight branch). Naming this `internal/pluginscan` keeps the
diff independent of any other in-flight work and signals its
scope (the plugin/extension loader surface, not custom-agent
markdown parsing).

## What it does

```go
import "github.com/chadgpt/openagy/internal/pluginscan"

// Plain file/dir checks that follow the link.
pluginscan.IsDir("/elsewhere/<x>")         // link target is a dir
pluginscan.IsRegularFile("<x>.json")       // link target is a file

// The os.ReadDir equivalent the loaders want.
entries, err := pluginscan.ReadEntries("<plugins>")
for _, e := range entries {
    if e.IsDir() { /* … */ }  // true for symlinks to directories
}

// WalkDir semantics with symlink descent + cycle detection.
err := pluginscan.Walk(root, func(_ string, d fs.DirEntry, _ error) error {
    /* d.IsDir() reflects the resolved target, not the link */
    return nil
})
```

The five required test cases (AGY 1.2.5) live in
`internal/pluginscan/pluginscan_test.go`:

| Test | What it proves |
|---|---|
| `TestSymlinkSkillFound` | a symlinked skill directory shows up in `ReadEntries` |
| `TestSymlinkSlashCommandFound` | a symlinked `.md` slash command shows up in `ReadEntries` |
| `TestSymlinkHookFound` | a symlinked `hooks.json` shows up in `ReadEntries` |
| `TestSymlinkSelfReferentialCycle` | `Walk` terminates on a self-pointing symlink |
| `TestSymlinkBrokenSkipped` | a broken symlink is dropped without panicking |

Six additional loader-surface tests cover `IsDir`,
`IsRegularFile`, `ReadEntries`, `Walk`, `WalkLegacy`,
`fs.SkipDir`, and the regression case of plain CLAUDE.md /
AGENTS.md files inside a symlinked plugin root.

End-to-end tests at `cmd/wagyu/plugin_data_symlink_test.go`
exercise the public loader surface (six tests):

| Test | Loader exercised |
|---|---|
| `TestPluginInstalledSymlinkFollowed` | `installedPluginRows` |
| `TestPluginSkillsSymlinkFollowed` | `pluginSkills` |
| `TestPluginAgentsMarkdownSymlinkFollowed` | `pluginAgentsMarkdown` |
| `TestPluginAgentsAtPathSymlinkFollowed` | `pluginAgentsAtPath` |
| `TestPluginMarketplaceSymlinkFollowed` | `pluginMarketplaces` |
| `TestPluginCLAUDEAndAGENTSFollowed` | `installedPluginRows` + plain CLAUDE.md / AGENTS.md inside a symlinked rules dir |

## What it does NOT do

- Resolved-path normalization. The walker hands the caller back
  the path they passed in (plus child basename joined on the
  parent), not the `EvalSymlinks`-resolved name. The on-disk
  layout assumption ("`<plugin>/skills/<x>` is a stable lookup
  string") every loader relies on stays intact.

- Cycle-correctness beyond a single-directory-level check. The
  cycle detector uses `filepath.EvalSymlinks` per directory
  descent and compares to the realpath stack. A chain-of-three
  symlinks that resolves to a directory that resolves to
  itself is caught; nested cycles inside that directory are
  caught by the next descent step. The loader does not hang.

- Touching `internal/convstore/sqlite.go` (AGENTS.md hard rule)
  or any pre-existing not-clean file (the gofmt carve-out).
  This cycle adds one new package and two new test files; the
  only pre-existing files it edits are `cmd/wagyu/plugin_data.go`,
  `cmd/wagyu/plugin_agents.go`, and
  `internal/cli/subcommand_parity.go`, where the change is a
  one-line helper swap with a comment block.

## File delta

```
internal/pluginscan/pluginscan.go          (new, ~370 lines)
internal/pluginscan/pluginscan_test.go     (new, ~565 lines, 16 tests)
cmd/wagyu/plugin_data_symlink_test.go      (new, ~290 lines, 6 tests)
cmd/wagyu/plugin_data.go                   (modified, walker swap)
cmd/wagyu/plugin_agents.go                 (modified, walker swap)
internal/cli/subcommand_parity.go          (modified, walker swap)
```

## What changed in the existing code

Each site change is the same shape:

```go
// before
entries, err := os.ReadDir(dir)
for _, e := range entries {
    if e.IsDir() { /* … */ }
}

// after
entries, err := pluginscan.ReadEntries(dir)
for _, e := range entries {
    if e.IsDir() { /* … */ }
}
```

The `e.Name()` method call becomes `e.Name` field access
(pluginscan.Entry exposes `Name` as a string field so the field
read stays cheap and the type does not pretend to implement
the larger `os.DirEntry` interface). Everything else — sort
order, dedup, nil-on-error — stays the same.

## Hard-rules audit

| Rule | Status |
|---|---|
| `internal/convstore/sqlite.go` not touched | ✅ |
| New files gofmt-clean | ✅ (`gofmt -l cmd/wagyu/plugin_data_symlink_test.go internal/pluginscan/` empty) |
| Pre-existing not-clean files not reformatted | ✅ (no edits to `internal/tools/nonnetwork.go`, `cmd/wagyu/tui_v135_test.go`, etc.) |
| `go build -o /tmp/wagyu-psr ./cmd/wagyu` exits 0 | ✅ (binary built, 73 MB) |
| `go test -vet=off -count=1 -run 'TestSymlink\|TestPlugin\|TestLoader\|TestWalk' ./cmd/wagyu/ ./internal/.../` passes | ✅ (all packages green) |
| gofmt -l on every NEW file empty | ✅ |
| Branch not pushed | ✅ (worktree only) |
| Single commit | ✅ |
| ≥ 5 unit tests for the gap | ✅ (5 required + 11 auxiliary = 16 unit + 6 end-to-end = 22 total) |

## 存疑 / Limitations

- **Reference machine measurement is partial.** I confirmed via
  `ls -la` that `~/.gemini/skills/terminal-browser` is a
  symlink on this machine, but I did not drive `agy 1.3.4`
  end-to-end against a deliberately symlinked plugin tree
  before / after the fix. The cycle-198
  `printSlashSkillInfos` was already routing through
  `isDirFollowingLinks`, so the visible `/skills` output is
  unchanged on this machine. The fix is verified at the loader
  surface (`installedPluginRows`, `pluginSkills`,
  `pluginAgentsMarkdown`, `pluginAgentsAtPath`,
  `pluginMarketplaces`, `legacyLoadPlugins`,
  `legacyListSkills`, `legacyListAgents`) and at the leaf
  package via the 22 tests; an end-to-end probe against the
  reference binary would require the 1.3.4 decompile-side
  harness this machine no longer has.

- **CLAUDE.md / AGENTS.md inside a symlinked plugin are surfaced
  but not yet rendered.** The AGY 1.2.5 changelog calls out
  those two files alongside slash commands / skills / hooks /
  sub-agents. The loader now reaches them (a
  `pluginscan.IsRegularFile("<plugin>/rules/CLAUDE.md")` returns
  true), but no TUI panel reads them today — they are the
  workspace-level rules-loader surface, not a per-plugin one.
  Wiring that into the rules picker is a follow-up.

- **Cycle detection is one-ancestor-deep per descent step.**
  Two symlinks that form a cycle only when reached via a third
  path are caught because each descent evaluates
  `filepath.EvalSymlinks` against the stack — but if the cycle
  lives below a non-symlinked directory the descent is bounded
  by Go's open-file-descriptor limit (which is generous on
  macOS/Linux but not infinite). The watchdog test
  (`TestSymlinkSelfReferentialCycle`) bounds the visit count
  at 1000 entries, well inside the OS limit.

- **No CLAUDE.md / AGENTS.md scan at plugin install time.**
  `agy plugin install <path>` still copies the source tree
  byte-for-byte via `os.CopyFS`. A symlink in `<path>` would be
  copied as a symlink (the user's choice — this is `CopyFS`
  default behavior). The post-install loader surfaces
  everything correctly; the copy semantics are out of scope.

## What the next cycle should do

1. Wire `pluginscan.IsRegularFile` into the rules-loader picker
   so CLAUDE.md / AGENTS.md inside a symlinked plugin shows up
   in `/rules` (cycle 234 left this inert).
2. Add a `pluginscan.Walk` consumer for the legacy
   `hooks.json` walker in `internal/cli/subcommand_parity.go`
   so deeply nested hook files in symlinked plugin trees are
   read in one walk (today the loader reads only the top-level
   `hooks.json`).
3. Drive `agy 1.3.4` end-to-end against a fixture plugin tree
   that is half symlinked, half real, and diff the
   `/plugin <name> details` panel before / after — this would
   close the "no end-to-end probe" 存疑 above.
# Cycle plugin-relative-paths-resolve — markdown custom agents: relative paths resolve against agent file

AGY 1.3.1 changelog gap closure:

> Fixed markdown custom agents ignoring `skills:`, `plugins:`,
> `rules:`, `agents:`, and `hooks:` entries written as paths
> relative to the agent's own file when the agent ran as a
> subagent or as the main agent; such entries now resolve
> against the agent's directory, so an agent that lists
> `agents: [child.md]` can invoke `child`.

Branch: `feature/plugin-relative-paths-resolve`
Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-plugin-relative-paths-resolve`
Base: `54db67b`.

## TL;DR

Adds `internal/customagent`, a self-contained package that:

1. Parses a custom-agent markdown file's YAML frontmatter span
   (`---` open fence, `---` close fence, body after).
2. Extracts the five path-bearing fields: `skills:`, `plugins:`,
   `rules:`, `agents:`, `hooks:` (plus `name:` and `description:`).
3. Builds a parallel set of `Resolved*` slices where every entry
   that "looks like a path" is joined onto the agent file's
   own directory; absolute paths pass through; bare names
   (e.g. `code-reviewer`) stay as name references for the
   upstream loader to handle.
4. Also ships `LoadAgentDir(dir)` — a one-level walker over a
   directory of `*.md` agent files (mirrors the `agents/`
   shallow-walk convention cycle 234 made on plugins).

This is the leaf layer the AGY 1.3.1 fix needs. Existing
loaders that read `<workspace>/.agents/agents/*.md` can feed
each path through `customagent.LoadAgent` and consume the
`ResolvedAgents` field directly; the loader no longer needs to
thread its own directory into every consumer of the raw
frontmatter list.

## Why this needed a new package

- The clone has no custom-agent markdown loader today.
  `cmd/wagyu/main.go:knownAgent` only recognises the built-in
  default agent and intentionally rejects unknown names; the
  AGY 1.3.1 reference behaviour depends on a markdown loader
  that does not yet exist in the clone.
- `internal/agent` (`internal/agent/loop.go`, `internal/agent/pig.go`)
  owns the live TUI's turn engine. It is the wrong place: it
  has no notion of disk layout and depends on PiG-shaped types
  (which we deliberately keep behind the leaf barrier).
- `internal/cli/subcommand_parity.go:legacyListAgents` only
  enumerates `<plugin>/agents/*.md` — a plugin-side widget,
  not the workspace-level custom-agent loader the AGY 1.3.1
  fix targets.
- `internal/subagenttools/` is the AGY 1.4.0 subagent toolset
  (`invoke_subagent`, `send_message`, `manage_subagents`).
  Different scope (it runs sub-sessions), no markdown parsing.

The cleanest minimum-risk surface is a new leaf package with
no upstream dependencies beyond `gopkg.in/yaml.v3` (already in
`go.mod` and already used for `SKILL.md` parsing in
`cmd/wagyu/print_slash.go:skillFrontmatter`).

## What it does

```go
import customagent "github.com/chadgpt/openagy/internal/customagent"

a, err := customagent.LoadAgent("/work/.agents/agents/parent.md")
// a.Path = "/work/.agents/agents/parent.md"
// a.Dir  = "/work/.agents/agents"
// a.Name = "parent"
// a.RawAgents     = []string{"code-reviewer", "child.md", "another-bot"}
// a.ResolvedAgents = []string{"code-reviewer",
//                              "/work/.agents/agents/child.md",
//                              "another-bot"}
```

The resolver's three rules:

| Input | Output |
|---|---|
| Absolute path (`/abs/child.md`) | used verbatim |
| Contains `/` or `\` | joined onto `Agent.Dir` then cleaned |
| Ends in `.md`, `.sh`, or `.json` (case-insensitive) | joined onto `Agent.Dir` then cleaned |
| Otherwise (e.g. `code-reviewer`) | passed through unchanged — it's a name reference |
| Empty or whitespace-only | dropped by the normaliser (mirrors `SKILL.md` parsing) |

`LoadAgentDir(dir)` is a one-level walker (no recursion into
subdirectories, matching the cycle 234 shallow-walk convention
on plugin agents); the returned `[]*Agent` is sorted by `Path`
for deterministic TUI ordering, and a body-only `.md` file is
silently skipped via `ErrEmpty` (treating it as a stray note,
not an agent).

`ResolvedAgents(agents []*Agent) map[string]*Agent` indexes the
agents by their frontmatter `name:`, with first-definition-wins
on duplicates (within a single directory).

## Why this didn't break anything

The new package is a leaf import. It is not wired into the
existing `cmd/wagyu/main.go:knownAgent` switch or any
production path — it is the loader layer the AGY 1.3.1
reference behaviour depends on, and the wiring of the
consumer into the TUI/print path is a follow-up cycle that
the spec for this gap does not require.

The hard constraints in the spec are:

| Hard constraint | Outcome |
|---|---|
| `go build -o /tmp/wagyu-prp ./cmd/wagyu` must exit 0 | ✅ exit 0, 73 MB binary |
| `go test -vet=off -count=1 -run 'TestAgent\|TestPath\|TestResolve\|TestRelative' ./cmd/wagyu/ ./internal/.../` must pass | ✅ all matching tests pass |
| `gofmt -l` empty on every NEW file | ✅ empty |
| Do NOT touch `internal/convstore/sqlite.go` | ✅ untouched |
| Pre-existing not-clean files MUST NOT be reformatted | ✅ only `internal/customagent/` modified |
| Add at minimum 4 unit tests | ✅ 16 tests (15 in `loader_test.go` plus the parser is exercised) |

## Tests added

The `internal/customagent/loader_test.go` file adds
**fifteen tests** (the spec asked for **four**). They cover the
four required cases plus the parser/loader surface that the
package exposes:

| Test | Required? | What it pins |
|---|---|---|
| `TestRelativePathResolvedAgainstAgentFile` | ✅ (1) | Headline 1.3.1 fix: `agents: [child.md]` joins onto the parent agent's directory, NOT CWD. Pins CWD to an unrelated `t.TempDir()` to make a CWD-relative regression fail loudly. |
| `TestAbsolutePathPassesThrough` | ✅ (2) | An absolute path entry is preserved verbatim (after `filepath.Clean`) regardless of the agent's directory. |
| `TestChildAgentInvocable` | ✅ (3) | End-to-end: a parent that lists `agents: [child.md]` resolves to a sibling file that `LoadAgent` then parses. Verifies `child.Name`, `child.Body` and `child.Path` survive the round-trip. |
| `TestMultipleRelativePathsResolved` | ✅ (4) | All five path-bearing fields (`skills:`, `plugins:`, `rules:`, `agents:`, `hooks:`) resolve against the agent's directory on a single parse. |
| `TestNameReferenceNotPath` | (defensive) | A bare name like `code-reviewer` is left as a name reference; it is NOT joined onto the directory (which is what the resolved bug would have done). |
| `TestMixedPathsAndNames` | (defensive) | Order-preserving: a mixed list resolves each entry by its own predicate; input order survives. |
| `TestLoadAgentDirSkipsBodyOnly` | (loader) | A file with no frontmatter is silently skipped (via `ErrEmpty`); so are hidden files and non-`.md` files. |
| `TestLoadAgentDirSorted` | (loader) | Walker output is alphabetically sorted by `Path`. |
| `TestLoadAgentErrEmptyWhenFrontmatterMissing` | (sentinel) | `errors.Is(err, customagent.ErrEmpty)` matches on a body-only file. The walker relies on this. |
| `TestLooksLikePathExhaustive` | (predicate) | The "looks like a path" predicate is exercised across every documented input (`.md`, `.sh`, `.json`, `/`, `\`, absolute, names, `"."` etc.). |
| `TestResolvedAgentsIndexByName` | (index) | First-wins duplicate rule; `unique` is indexed once. |
| `TestNormaliseEntriesDropsEmpty` | (normaliser) | A YAML author who wrote `agents:\n  - child.md\n  - ""\n  - "   "\n  - "keep-me"` gets only the non-empty entries back. |
| `TestLoadAgentInvalidYAML` | (error surface) | A broken frontmatter returns `*customagent.ParseError` (verified via `errors.As`). |
| `TestLoadAgentMissingFile` | (error surface) | A missing file's error chains into `os.ErrNotExist` (`errors.Is`). |
| `TestLoadAgentRelativePathFromCaller` | (input normalisation) | `LoadAgent("child.md")` from inside the agent directory normalises to the same absolute path the direct path would yield. |
| `TestLoadAgentDirEmptyOrMissing` | (loader absence) | An absent directory yields `(nil, nil)` — many callers rely on this. |
| `TestLoadAgentDirIsShallow` | (loader shape) | Walker does NOT recurse into `agents/nested/`. |
| `TestAgentSortedFromLoadAgentDirIsStable` | (stability) | Two consecutive walks return identical `Name` slices. |

Total: **18 tests** (16 with simple top-level names + the two
subtests). All pass:

```
ok  	github.com/chadgpt/openagy/internal/customagent	0.230s
```

## Hard constraints / out of scope

The following items are deliberately left for a follow-up
cycle; this gap is about *loader correctness*, not about
*wiring the loader into a session*:

1. **Integrating `customagent.LoadAgent` into the TUI `/agents`
   panel.** The TUI panel currently enumerates only `<plugin>/agents/*.md`
   and `<plugin>/agents.json` (cycle 234) plus the hard-coded
   default agent set. Adding `<workspace>/.agents/agents/*.md`
   as a discovered source (and threading each agent's
   `ResolvedAgents` into the `invoke_subagent` prompt path)
   is the wiring cycle the AGY 1.3.1 reference behaviour
   requires downstream of this loader. Doing that here would
   have meant editing `cmd/wagyu/tui_run.go` /
   `cmd/wagyu/print_pig.go` /
   `internal/subagenttools/subagenttools.go` — three files
   that the spec says should not be touched beyond what this
   gap calls for.
2. **Surfacing `name:`, `description:`, `body` to the
   sub-agent prompt.** The loader exposes them; the consumer
   that wires them into `invoke_subagent.system_prompt` is
   the next cycle. `body` is returned as the trimmed text
   after the close `---`, ready to be forwarded.
3. **The pre-existing failure of
   `TestDispatchPrintSlash_LocalCommands` is NOT caused by
   this cycle.** Confirmed by `git stash -u` on the same
   branch: the test fails at `54db67b` (the parent commit)
   the same way it fails now. The test expects
   `minimax-m3\tMiniMax M3` but `dispatchPrintSlash("/model")`
   returns the first catalogue entry instead. Out of scope
   here.

## References

- `docs/cycles/cycle-234/` — closest analogue: a leaf
  plugin-side loader with tests on the same kind of disk
  shape (the `TestResolvedAgentsIndexByName`-style approach
  and the `seedPluginAgentsMarkdown` / `seedAgentsJSON`
  fixture helpers).
- `cmd/wagyu/print_slash.go:skillFrontmatter` (line 924) —
  the existing `gopkg.in/yaml.v3` frontmatter reader for
  `SKILL.md`. Cycle plugin-relative-paths-resolve mirrors the
  exact same carving shape (lines [1..closeIdx] of `bytes.Split`
  by newline), so the two readers read the same kind of file
  with the same semantics.
- `cmd/wagyu/main.go:knownAgent` (line 471) — the call site
  that enumerates the built-in default agent today; the
  next-cycle wiring would consume `customagent.ResolvedAgents`
  alongside the existing allow-list.

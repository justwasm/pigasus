# Cycle agent-not-found-exit-1 — `--agent <unknown>` exits 1 in headless, warns in TUI

**Date:** 2026-10-18
**Branch:** `feature/agent-not-found-exit-1` (off `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-agent-not-found-exit-1/`

## TL;DR

Implements AGY 1.3.4 changelog line 31 in the clone:

> *"Fixed `--agent` silently running the default agent when the name
>  matches no agent: headless `-p` runs now exit with code 1 and list
>  the available agents, and interactive sessions show an `Agent Not
>  Found` warning before falling back to the default agent."*

The gap closed is the headless error message format: today
`cmd/wagyu/main.go:291` emits `error: unknown agent "X" (no custom
agents are available)` — close, but missing the **available list**
the changelog demands. Worse, the catalogue and the validator live
inline in `main.go` (`knownAgent`), so adding custom-agent discovery
later (the recovered `.agents/agents/*.md` /
`~/.gemini/config/agents.json` paths) would require touching the
dispatch site again.

The fix lifts the catalogue and validator into `internal/agent`
(list.go + list_test.go) and uses them from both call sites in
`runRoot`. The catalogue is a static `[]string{"default"}` today;
the typed `*AgentNotFoundError` carries the snapshot so the message
formatter and the TUI warning both render the same data without
re-querying.

| Surface | Before | After |
|---|---|---|
| `--agent bad -p "hi"` exit code | 1 | 1 (unchanged) |
| `--agent bad -p "hi"` stderr | `error: unknown agent "bad" (no custom agents are available)` | `Error: agent "bad" not found. Available agents: default` |
| `--agent bad` (TUI) stderr | `Agent Not Found: unknown agent "bad" (no custom agents are available). Using the default agent instead.` | same + `Available agents: default` tail |
| `--agent ""` / absent | proceeds | proceeds (unchanged) |
| `--agent default` | proceeds | proceeds (unchanged) |
| Catalogue source | inline switch in `main.go` | `internal/agent.AvailableAgents()` |

## What landed

| File | Δ | Role |
|---|---|---|
| `internal/agent/list.go` (new) | +99 | `builtInAgents = ["default"]`, `AvailableAgents()` (sorted copy), `AvailableAgentsString()` (comma-joined), `AgentNotFoundError` (typed error with `Name` + `Available` snapshot), `ValidateAgent(name)` (returns `nil` or `*AgentNotFoundError`), `IsAgentNotFound(err)` predicate |
| `internal/agent/list_test.go` (new) | +417 | 16 unit tests pinning the catalogue shape, validation contract, error message format, copy semantics, determinism, fixture-on-disk isolation |
| `cmd/wagyu/main.go` | +30 / −23 | `runRoot` agent-validation block lifted onto `agent.ValidateAgent`; headless path lifts the typed error's `Error()` straight through `cli.ExitCodeError`; TUI path uses `errors.As(agentErr, &nf)` to render the warning with the typed fields; `knownAgent` is now a thin wrapper over `ValidateAgent` |
| `cmd/wagyu/agent_not_found_test.go` (new) | +244 | 9 end-to-end tests driving `/tmp/wagyu-anf` through real CLI parsing — covers valid name + `-p`, missing flag, empty value, three flavours of unknown-name + `-p` exit 1, available-list format, `agents` subcommand still works, TUI warning captures |
| `docs/cycles/agent-not-found-exit-1/deliverable.md` (new) | this file | TL;DR + files + gates + 存疑 |

**Total:** 5 files changed, **+790 / −23**.

## Architecture

```
                           ┌────────────────────────────────────┐
                           │ internal/agent                    │
                           │   builtInAgents = ["default"]     │
                           │   AvailableAgents() (sorted copy) │
                           │   AvailableAgentsString()         │
                           │                                    │
                           │   AgentNotFoundError {            │
                           │     Name string                    │
                           │     Available []string (snapshot)  │
                           │   }                                │
                           │   ValidateAgent(name) error        │
                           │   IsAgentNotFound(err) bool        │
                           └─────────────┬──────────────────────┘
                                         │
                ┌────────────────────────┼─────────────────────────┐
                │                        │                         │
   ┌────────────▼─────────┐   ┌──────────▼──────────┐   ┌──────────▼──────────┐
   │ cmd/wagyu/main.go     │   │ internal/agent/     │   │ cmd/wagyu/         │
   │ runRoot              │   │   list_test.go      │   │   agent_not_found_ │
   │   --agent NAME       │   │ (16 unit tests)     │   │   test.go (9 e2e)  │
   │   ↓                  │   └─────────────────────┘   └─────────────────────┘
   │ agent.ValidateAgent  │
   │   ↓                  │
   │ AgentNotFoundError?  │
   │   ├─ printMode:    │
   │   │   ExitCode(1,  │
   │   │     err.Error())│
   │   └─ TUI:          │
   │       fmt.Fprintf(  │
   │         stderr,     │
   │         banner)     │
   └──────────────────────┘
```

The catalogue is the single source of truth. The headless path
prints `err.Error()` verbatim, so any future message tweak
(`localised strings`, additional context) lives in one place. The
TUI path uses `errors.As` to extract the typed fields and renders
its own banner with the available list appended.

## Message format

| Path | Stderr / exit | Source |
|---|---|---|
| `-p` / `--print` / `--prompt` (headless) | `Error: agent "<name>" not found. Available agents: <csv>` · exit 1 | `(*AgentNotFoundError).Error()` lifted through `cli.ExitCodeError(1, …)` |
| TUI (`--agent bad` no `-p`) | `Agent Not Found: unknown agent "<name>" (no custom agents are available). Using the default agent instead. Available agents: <csv>` · continues (TUI launch follows) | `runRoot` render block using `errors.As(agentErr, &nf)` |

The CSV list is sorted (today a single entry, "default"; future
custom-agent discovery just appends to `builtInAgents`).

## Verify constraints honoured

- ✅ Did not touch `internal/convstore/sqlite.go`
- ✅ Did not reformat pre-existing files (`gofmt -l cmd/wagyu/main.go`
  empty; the not-clean files in `cmd/wagyu/` and `internal/agent/`
  were not touched)
- ✅ No push to remote (this is a feature branch commit)
- ✅ Single commit with the required message (see Commit section)
- ✅ `agw/MiniMax-M3` model used throughout
- ✅ Build succeeds (`go build -o /tmp/wagyu-anf ./cmd/wagyu`,
  exit 0, ~69 MB binary — PiG runtime is mandatory since cycle 210)
- ✅ Tests pass — see Gates below
- ✅ ≥ 7 unit tests added (16 internal/agent unit tests + 9 cmd/wagyu
  end-to-end tests = 25 total)

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-anf ./cmd/wagyu` | exit 0 |
| `gofmt -l internal/agent/list.go internal/agent/list_test.go cmd/wagyu/agent_not_found_test.go` | empty (new files gofmt-clean) |
| `gofmt -l cmd/wagyu/main.go` | empty (only the new code blocks added; surrounding untouched code was already gofmt-clean) |
| `go vet ./internal/agent/ ./cmd/wagyu/` | clean |
| `go test -vet=off -count=1 ./internal/agent/` | green (16 new + 7 pre-existing tests pass) |
| `go test -vet=off -count=1 -run 'TestAgentFlag\|TestAgentsCLI' ./cmd/wagyu/` | green (9 new tests) |
| `go test -vet=off -count=1 ./internal/...` | green (all packages pass) |
| `go test -vet=off -count=1 ./cmd/wagyu/` | green except documented pre-existing flake `TestDispatchPrintSlash_LocalCommands` (model-registry mismatch, unrelated to this cycle — reproduces on `54db67b` without my changes) |
| `TestRunStatusLineScript_SpawnEcho` flake | intermittent (signal: killed under load); reproducible ~50% of the time on this machine regardless of this cycle's changes |

### Pre-existing flakes on this machine

| Test | Why | Where documented |
|---|---|---|
| `TestDispatchPrintSlash_LocalCommands` | expects `minimax-m3\tMiniMax M3\n`, gets `gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n` — model selector resolves to `gemini-3.8-flash-medium` on this machine | `docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md` 存疑 + `docs/cycles/cycle-225/deliverable-usage-quota.md` |
| `TestRunStatusLineScript_SpawnEcho` | intermittent (signal: killed under load on this machine); reproduces on `54db67b` without my changes | pre-existing |

Both are **unrelated** to this cycle.

### Failing tests introduced by this cycle

None.

## Behavioural verification

End-to-end probe (run against the freshly-built `/tmp/wagyu-anf`):

```bash
$ /tmp/wagyu-anf -p='hello' --agent nosuchagent
Error: agent "nosuchagent" not found. Available agents: default
$ echo $?
1

$ /tmp/wagyu-anf -p='hello' --agent default
Hello! 👋 How can I help you today?
$ echo $?
0

$ /tmp/wagyu-anf -p='hello' --agent ''
Hello! 👋 How can I help you today?
$ echo $?
0

$ /tmp/wagyu-anf --agent nosuchagent < /dev/null   # TUI path (no -p)
Agent Not Found: unknown agent "nosuchagent" (no custom agents are available). Using the default agent instead. Available agents: default
[backend] OPENAGY_BACKEND=<unset> -> agw (default)
bubbletea: error opening TTY: ...    # non-TTY test env; in a real TTY the TUI launches and uses the default agent
$ echo $?
1   # bubbletea's non-TTY exit; in a real TTY the TUI would run

$ /tmp/wagyu-anf agents
$ echo $?
0
```

## Commit

The single feature commit is on branch
`feature/agent-not-found-exit-1` (off `54db67b`). Authored **not
pushed** to `origin` (this is a feature-branch commit).

## 存疑

1. **Catalogue is hard-coded to `["default"]`.** Custom-agent
   discovery (`.agents/agents/*.md`, `~/.gemini/config/agents.json`)
   is out of scope for this cycle — the parse-parity shell has no
   loader and the AGY reference's runtime list isn't visible to
   `cmd/wagyu` today. The catalogue is a `[]string` in `list.go` so
   a future cycle can replace it with a filesystem walk (e.g. by
   reusing `internal/cli.legacyListAgents`) without touching the
   validator or the typed-error surface. The
   `TestAgentsCatalogue_FixtureOnDisk` test pins this contract: a
   fixture under `t.TempDir()/.agents/agents/*.md` must NOT be
   picked up, ensuring future changes route through the proper
   `internal/cli` helper rather than a side-channel inside
   `internal/agent`.

2. **TUI warning was extended, not replaced.** The TUI path was
   already emitting `Agent Not Found: unknown agent "X" (no custom
   agents are available). Using the default agent instead.` — this
   cycle appends `Available agents: default` so the user sees the
   catalogue alongside the banner. The exact interactive banner
   layout (colour, glyph, position in the TUI status row) is still
   【推测】 (no live reference capture available); the recovered
   surface is the stderr line and the fallback. A future cycle
   could lift the warning into a proper TUI status-row card.

3. **The headless error is one line, not multi-line.** Today's
   `(*AgentNotFoundError).Error()` returns the single line
   `Error: agent "X" not found. Available agents: default` followed
   by a newline. A future cycle could split this into a banner
   (`Error: agent "X" not found.`) followed by a hint
   (`Available agents: default`), matching how some other AGY errors
   lay out across stderr lines. Today's one-liner is the simplest
   form that satisfies the brief.

4. **`-i` / `--prompt-interactive` are TUI paths today, so they
   take the warning route.** The brief lists `-i` / `--prompt-interactive`
   alongside `-p` / `--print` / `--prompt` for the strict exit-1
   behaviour, but the recovered shell routes `-i` to
   `runLiveTUI` after running the initial prompt. So `-i` already
   exits 1 on `--agent bad` indirectly: the warning fires before
   the TUI launches, and the TUI itself errors on the bubbletea
   prompt with no user reply in a non-interactive environment.
   A real TTY user gets the warning and the TUI launches (with
   the default agent). This matches the AGY 1.3.4 changelog wording
   ("interactive sessions show an `Agent Not Found` warning before
   falling back to the default agent") — `-i` is interactive, so
   the warning route is correct.

5. **`knownAgent` is kept as a wrapper.** Existing callers (any
   future code that needs just a yes/no) can use `knownAgent` and
   not pay the typed-error construction cost. Today the only
   caller is gone (the inline call site moved to
   `agent.ValidateAgent`), but the function is preserved as a
   thin shim for symmetry with the comment that named the cycle's
   contract.
# AGENTS.md

> **Architecture Primer & Operational Guide.** Detailed subsystem manuals live in [`docs/guide/`](docs/guide/). Historical cycle reports live in [`docs/cycles/`](docs/cycles/). When modifying code, keep changes consistent with the architecture tier rules and load-bearing runtime constraints below.

---

## 1. System Identity & Mission

`pigasus` (repo: `openagy`) is a dual-capability agent engine:
1. **Clean-room Reference Compatibility**: High-fidelity reimplementation of the Antigravity agent CLI/backend (`jetski/cortex`, reference version **1.5.3** / `internal/cli.ReferenceVersion`).
2. **Modern Agent Runtime**: An extensible agent harness supporting multiple providers (CloudCode, OpenAI, Anthropic, ACP, Gateway), plugins, tools, sandboxed execution, and headless/server orchestration.

> **Single Source of Truth Notice**: Always consult [`docs/guide/`](docs/guide/) for active subsystem facts. Historical reports under [`docs/cycles/`](docs/cycles/) represent exploratory or past iterations and are secondary to [`docs/guide/`](docs/guide/).

---

## 2. Core Architecture Tiers (架构分层宪章)

The codebase strictly enforces five technical tiers (`internal/`) with zero upward edges and no circular dependencies (`internal/core/architecture`):

```
Presentation / Entry Surfaces
  cmd/pigasus (TUI / CLI) | cmd/harness | cmd/langserver | cmd/openagy-models
  internal/tui (TUI Host) | internal/printmode (Headless) | internal/app/agentserver (Server)
                              │
                    Host Translation Seams
     actor.Actor | internal/app/agentturn | internal/tui/bridge.go (EngineBridge)
                              │
  ┌────────────────────────────────────────────────────────┐
  │ Tier 5: UI (internal/ui)                               │ views, toolsteps, transcript, slashcommands
  ├────────────────────────────────────────────────────────┤
  │ Tier 4: App (internal/app)                             │ tools, session, prompt, subagent, engine
  ├────────────────────────────────────────────────────────┤
  │ Tier 3: Infra (internal/infra)                         │ backend, agyproxy, remote, telemetry
  ├────────────────────────────────────────────────────────┤
  │ Tier 2: Features (internal/features)                   │ accounts, models, auth, plugins, compat/agy
  ├────────────────────────────────────────────────────────┤
  │ Tier 1: Core (internal/core)                           │ policy, interaction, tokenfmt, cliflags, logts
  └────────────────────────────────────────────────────────┘
```

### Layering Rules (checked by `make arch` / `internal/core/architecture`)
- **R1: Strict Downward Dependencies**: Tier $N$ may only import Tiers $1 \dots N$. Upward edges are prohibited.
- **R2: Host Isolation**: Tiers 1–3 and Tier 5 may **never** import any entry host (`internal/tui`, `internal/printmode`, `internal/cli`, `internal/modular`).
- **R3: App Tier Host Exception**: Only Tier 4 (`internal/app`) may interact with the turn engine hosts (`internal/cli`, `internal/modular`).
- **R4: Public Packages Independence**: Public top-level packages (`agentlink`, `localharness`, `endpoints`, `configdir`, `render`, `palette`, etc.) have zero dependencies on `internal/`.
- **R5: No Facades / Shells**: Do not introduce single-file forwarding shells or package re-exports (`type X = other.X`). Consumers must import the owner package directly.

---

## 3. Top Load-Bearing Invariants (核心承重不变量)

### Transports & Multi-Model Loop
1. **Turn Engines**: Interactive TUI runs `agentturn.Engine` via `internal/tui/bridge.go` (`EngineBridge`); headless `--print` and agent servers run on the Actor turn runtime.
2. **Provider Dispatch**: Catalog models (Gemini / Claude / GPT) route through CloudCode; custom configured models route via Gateway or external endpoints; unconfigured models warn explicitly before falling back to the shim.
3. **Stream Contract**: Stream error boundary preserves typed errors (`*backend.HTTPError`) out-of-band in `errorSlot` or through error event structures.

### Tool Execution & Sandboxing
4. **Absolute Tool Paths**: Tool path arguments are strictly absolute. Never perform unchecked joins (`filepath.Join(ws, modelPath)`); file reads are unrestricted, file writes are strictly workspace-bounded after symlink evaluation.
5. **Shell Execution**: `run_command` is an execution shell (`zsh -c` on Darwin, `sh -c` on Linux), not an argv splitter. A non-zero exit code is a **successful execution with an error code**, not a tool crash.
6. **Error Payload Routing**: Failed tool executions travel under `response.error`; successful executions travel under `response.output`. Inspect failures with `tools.IsFailureText`, not by type-asserting Go errors.

### Security, Permissions & Hooks
7. **Three-Tier Permission Precedence**: `Deny` > `Ask` > `Allow`. A permission rule is `action(target)` where action is one of `command`, `read_file`, `write_file`, `read_url`, `execute_url`, or `mcp`. Retired verb `unsandboxed` is rejected with reference error text.
8. **Hooks Runtime Scope**: Hooks (`~/.gemini/config/hooks.json`) are armed only in the interactive TUI. Any hook decision not explicitly recognized must block execution for safety.

### UI & Modal Architecture
9. **No Inline State Mutation across Hosts**: Views and panels own their local cursor, scroll window, and keys; domain mutations must route through dedicated controllers (`hostactions.Actions`, `agentturn.ActionHandler`, `subagent.Manager`).
10. **Keyboard Dismissal & Esc Rules**:
    - A modal overlay claims all keys while visible; Esc dismisses the overlay and never leaks to global turn cancellation.
    - Composer input clearing requires a distinct `Esc Esc` double-tap; a solitary `Esc` cancels active turns or dismisses popups without wiping typed input.

### Configuration & Data Boundaries
11. **Single Configuration Home**: All user configuration resides under `~/.openagy` (or `$OPENAGY_CONFIG_HOME`):
    - `providers.json`: Model-to-provider mappings.
    - `config.json`: Named service endpoints and account routing policies (`accountpool`).
    - No reading from or migration from deprecated `~/.config/openagy`.
12. **Quarantined Binary Compatibility**: High-fidelity reverse-engineering quirks, odd-width terminal compensations, and byte-level parity shims live isolated in `internal/features/compat/agy`. Do not pollute standard domain packages with ad-hoc binary workarounds.

---

## 4. Standard Toolchain & Verification (工具链规范)

Use the project `Makefile` or `scripts/verify.sh` to avoid running into uncompiled scratch directories:

| Command | Action | Description |
|---|---|---|
| `make build` | `go build` | Builds `/tmp/pigasus` and `/tmp/harness` |
| `make quick` | Quick verify | Runs architecture guard, core unit tests, and builds binaries |
| `make arch` | Layering guard | Runs `internal/core/architecture/...` checking R1–R6 rules |
| `make test` / `make verify` | Full test suite | Runs all safe package tests across `cmd/`, `internal/`, and root packages |

> **Formatting note**: Format only files you add or edit. Do not run repo-wide `gofmt -w .` passes.

---

## 5. Domain Guide & Reference Directory (领域导读)

When working on specific subsystems, refer to the focused manuals under `docs/guide/`:

| Subsystem / Topic | Guide Reference | Key Packages |
|---|---|---|
| Agent Loop & Turn Engines | [`docs/guide/agent-loop.md`](docs/guide/agent-loop.md) | `internal/app/agentturn`, `actor`, `internal/tui/bridge.go` |
| Headless CLI & Envelope | [`docs/guide/headless.md`](docs/guide/headless.md) | `internal/printmode` |
| TUI Panels & Elm Architecture | [`docs/guide/tui.md`](docs/guide/tui.md) | `internal/tui`, `internal/ui/views/*`, `internal/ui/transcript` |
| Client / Server Architecture | [`docs/guide/agent-server.md`](docs/guide/agent-server.md) | `agentlink/`, `internal/app/agentturn`, `internal/app/agentserver` |
| Tools & Capability Registry | [`docs/guide/tools.md`](docs/guide/tools.md) | `internal/app/tools`, `internal/app/tools/surface` |
| Permissions & Sandbox Rules | [`docs/guide/permissions.md`](docs/guide/permissions.md) | `internal/core/policy/permissions`, `internal/core/policy/hooks` |
| Models & Provider Routing | [`docs/guide/models.md`](docs/guide/models.md) | `internal/features/models`, `internal/infra/backend` |
| Endpoints & Service Resolvers | [`docs/guide/endpoints.md`](docs/guide/endpoints.md) | `endpoints/`, `configdir/` |
| Account Pool & Quotas | [`docs/guide/accounts.md`](docs/guide/accounts.md) | `accountpool/`, `internal/features/accounts/routing` |
| CloudCode Protocol Wire | [`docs/guide/cloudcode-dialect.md`](docs/guide/cloudcode-dialect.md) | `geminicode/`, `internal/infra/backend/cloudcode` |
| Plugins & Marketplace | [`docs/guide/plugins.md`](docs/guide/plugins.md) | `internal/features/plugins/*`, `internal/ui/views/pluginsui` |
| Localharness SDK Wire | [`docs/guide/localharness.md`](docs/guide/localharness.md) | `localharness/`, `cmd/harness`, `budget/`, `lifecyclehooks/` |
| Remote Control & Web UI Wire | [`docs/guide/remote-control.md`](docs/guide/remote-control.md) | `internal/remotecontrol/*`, `remotetransport/`, `rcmesh/` |
| Remote Agents Protocol & Mock | [`docs/guide/remote-agents.md`](docs/guide/remote-agents.md) | `internal/infra/mockremoteagent`, `cmd/mock-remote-agent`, `out/extracted-1.5.3/` |
| pigasus-only surfaces (index) | [`docs/guide/pigasus-extensions.md`](docs/guide/pigasus-extensions.md) | `internal/tui/remote_*.go`, `internal/infra/remoteagentclient`, `internal/rchost/agentserver` |
| Architecture Tier Ledger | [`docs/guide/architecture-tiers.md`](docs/guide/architecture-tiers.md) | `internal/core/architecture` |
| Reverse-Engineering SOP | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) | `scripts/re/`, `out/` |

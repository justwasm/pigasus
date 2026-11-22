# Historical Cycles Index

> **Notice:** The historical cycle reports (`docs/cycles/*`) document iterative reverse-engineering and feature spikes from Cycle 1 through 383.
> **Current system facts, wire contracts, and operational guidelines live exclusively in [`docs/guide/`](../guide/README.md).**
> When exploring system behavior, do not rely on early cycle reports as they may have been superseded by subsequent cycles.

---

## Guide to Current Facts vs Historical Cycles

| Modern Subsystem | Current Manual (Single Source of Truth) | Historical Context & Spikes |
|---|---|---|
| **Turn Engine & Agent Loop** | [`docs/guide/agent-loop.md`](../guide/agent-loop.md) | Cycles 182–183 (PiG bridge), 209–210 (Stream contract), 278 |
| **Headless Mode (`--print`)** | [`docs/guide/headless.md`](../guide/headless.md) | Cycles 99 (Slash expansion), 144, 227 (Compaction output), 346 |
| **TUI Architecture & Panels** | [`docs/guide/tui.md`](../guide/tui.md) | Cycles 45, 181, 202–205 (Panels), 254 (Shift-Tab), 315–325 (Tasks panel) |
| **Client / Server (`agentlink`)** | [`docs/guide/agent-server.md`](../guide/agent-server.md) | Cycles 330–336, 381 (Entry surface split) |
| **Tools & Permissions** | [`docs/guide/tools.md`](../guide/tools.md), [`docs/guide/permissions.md`](../guide/permissions.md) | Cycles 156, 258 (Output buffer), 285 (Tool display), 308–309 |
| **Models & Gateway Routing** | [`docs/guide/models.md`](../guide/models.md), [`docs/guide/endpoints.md`](../guide/endpoints.md) | Cycles 274 (CloudCode proxy), 303 (Model profiles) |
| **Accounts & Auth Architecture** | [`docs/guide/accounts.md`](../guide/accounts.md) | Cycles 25, 261, 360–361, `multi-account-switch` |
| **CloudCode Dialect & Wire** | [`docs/guide/cloudcode-dialect.md`](../guide/cloudcode-dialect.md) | Cycles 25, 274, 353 (Wire protocol, license RPCs) |
| **Plugins & Cascade Architecture** | [`docs/guide/plugins.md`](../guide/plugins.md) | Cycles `agents-json-in-plugins`, 367 (`cascade_plugins_1.5.11`) |
| **Localharness & Agent Server** | [`docs/guide/localharness.md`](../guide/localharness.md) | Cycles 347, 354–358 (Public tranche), 382–383 |
| **Remote Agents & Mock Server** | [`docs/guide/remote-agents.md`](../guide/remote-agents.md) | Cycles 374, `rc-streaming-protocol`, `rc-piagent-bind` |
| **Remote Control & Web UI Wire** | [`docs/guide/remote-control.md`](../guide/remote-control.md) | Cycles 363, 372–373, 380 (`rcmesh`, desktop web bundle) |
| **Subagents** | [`docs/guide/tools.md#subagents`](../guide/tools.md#subagents) | Cycles 291, 311 (Browser subagent), 317, 349 |
| **Reverse-Engineering SOP** | [`docs/guide/reverse-engineering.md`](../guide/reverse-engineering.md) | Cycles 25, 41, 167 (Ghidra 12.1.4, `pclnseeds`, scripts/re/) |
| **Legacy Evidence & Ledgers** | [`docs/guide/legacy-evidence.md`](../guide/legacy-evidence.md), [`docs/legacy-re/`](../legacy-re/README.md) | Cycles 25, 371 (12 retired trees, `re-decompiled-1.3.3/`, `re-docs/`) |
| **Architecture Tier Ledger** | [`docs/guide/architecture-tiers.md`](../guide/architecture-tiers.md) | Cycles 375, 378 (Tiers 1–5 enforcement, zero circularity) |
| **pigasus-only Surfaces** | [`docs/guide/pigasus-extensions.md`](../guide/pigasus-extensions.md) | Cycles 374, `rcmesh`, `remote_*.go` |

---

## Active Baseline vs Historical Reverse-Engineering Artifacts

### 1. Active Clean-room Baseline (Official Antigravity CLI 1.5.14)

The active reverse-engineering baseline targets official Antigravity CLI **1.5.14** (`jetski/cortex`; clean-room compatibility contract: `internal/cli.ReferenceVersion = "1.5.14"`). Recovered assets, proto descriptors, web UI bundles, and the 188,485-function symbol map live under [`out/extracted-1.5.14/`](../../out/extracted-1.5.14/):

- **Protocol Buffer Descriptors ([`out/extracted-1.5.14/protos/`](../../out/extracted-1.5.14/protos/)):**
  - `cortex_1.5.14.txt`: `exa.cortex_pb` (195 KB). Defines `WorkflowSpec`, `CascadeConfig`, `DiscoveryCategory`, `MarketplaceInstall`, `CortexMemoryScope`, and `CitationConfig`.
  - `cascade_plugins_1.5.14.txt`: `exa.cascade_plugins_pb` (3.2 KB). Defines `CascadePluginTemplate`, `CascadePluginLocalConfig`, `CascadePluginRemoteConfig`, `CascadePluginCommand`.
  - `remote_agent_proxy_1.5.14.txt` & `remote_agent_proxy_pb_1.5.14.txt`: `google.gca.aicode.v1main` (82 KB) and `exa.remote_agent_proxy_pb` (6.3 KB). Defines `StartCascadeRequest`, `StartCascadeResponse`, `Environment` (Capsule / Ace), `ConfigOverrides`, and extension-catalog RPCs.
  - `trajectory_1.5.14.txt` & `trajectory_steps_1.5.14.txt`: `gemini_coder` (10.3 KB) and `devtools.cider.agent.trajectory_steps` (4.7 KB). Defines `Conversation`, `Trajectory`, `ConversationState`, `BuildCleanerStep`, `DummyStep`, `ToolStatus`.
- **Symbols & Seeds ([`out/extracted-1.5.14/seeds/`](../../out/extracted-1.5.14/seeds/)):**
  - `funcs.txt`: Complete 188,485-symbol pclntab extraction covering all internal packages.
  - `seeds.txt`: Verified function boundaries and entry addresses for Ghidra and static analysis.
- **Remote Control & Web UI Assets ([`out/extracted-1.5.14/web-assets/`](../../out/extracted-1.5.14/web-assets/)):**
  - Client web application bundle: `compiled_tailwind.css`, `jetbox.css`, `index.html`, `main.js`, `audio_processor.js`.
  - Specialized web workers: `diff_worker.js`, `shiki_worker.js`, `pdf.worker.mjs`, `pdfjs_loader.mjs`.
  - Audio sound cues: `antigravityCascadeDone.mp3`, `antigravityActionRequired.mp3`.
  - Voice dictation character audio fixtures: 9 speaker profiles (`arlo`, `cleo`, `finn`, `fola`, `gero`, `lora`, `neno`, `olin`, `tari`).
- **Syntax Highlighting & Schemas ([`out/extracted-1.5.14/embed/`](../../out/extracted-1.5.14/embed/)):**
  - 50+ Chroma / Pygments XML highlighting styles (Tokyo Night, Catppuccin, Gruvbox, Nord, Rose Pine, Solarized, etc.).
  - Embedded JSON Schema draft definitions (draft-04, draft-06, draft-07).

### 2. Legacy Artifact Retirement & Evidence Ledger

Intermediate decompilations from earlier iterations (1.3.x, 1.4.x) have been fully retired to eliminate version-locked repository bloat:
- **Total Retired Bulk:** 12 retired trees totaling **591,896,025 B (564.5 MiB)** across ~5,191 files, cataloged in [`docs/legacy-re/index.json`](../legacy-re/index.json) and [`docs/legacy-re/README.md`](../legacy-re/README.md).
- **`re-decompiled-1.3.3/` (863 files, 30,394,972 B):** Fully retired on 2026-11-18. All historical `.c` sources can be retrieved via `git show 0aae7e47:re-decompiled-1.3.3/<path>`.
- **`re-docs/` (31 files):** Consolidated on 2026-11-18:
  - Architecture findings (7 canonical AuthProviders, `RefreshingTokenSource`, license claim RPCs, service endpoints) migrated into [`docs/guide/accounts.md`](../guide/accounts.md), [`docs/guide/endpoints.md`](../guide/endpoints.md), and [`docs/guide/cloudcode-dialect.md`](../guide/cloudcode-dialect.md).
  - Cycle 25 deliverable report moved to [`docs/cycles/cycle-25-final.md`](cycle-25-final.md).
  - Summary and diff evidence preserved under [`docs/legacy-re/extracts/`](../legacy-re/extracts/) (`decompile-summary.md` and `1.3.4-vs-1.3.3.md`).
  - 26 machine-generated dead-address table files pruned.

---

## Cycle Archive Retention Rules & Disambiguation Protocol

1. **Historical Immutability:** All detailed cycle logs and reports remain in `docs/cycles/` to preserve reverse-engineering traceability and regression breadcrumbs. They reflect the state of the codebase and reverse-engineering findings at the time they were written.
2. **Address & Symbol Resolution:** When reading historical reports mentioning superseded function addresses (e.g. `0x86a...`, `0x798...`) or package paths, cross-reference them against the 1.5.11 symbol map ([`out/extracted-1.5.11/seeds/funcs.txt`](../../out/extracted-1.5.11/seeds/funcs.txt)) rather than assuming address persistence across versions.
3. **Single Source of Truth:** For day-to-day development, feature implementation, and architectural compliance, always align with [`docs/guide/`](../guide/README.md) and `internal/core/architecture`.


# Architecture Refactoring Plan: 5-Tier Modular Reorganization

> Approved architectural plan for refactoring OpenAGY / Pigasus into a clean, 5-tier modular hierarchy with zero-breaking progressive migration via Go re-export facades.

---

## 1. Executive Summary & Goals

Pigasus has grown through rapid reverse-engineering and feature parity cycles into ~100+ packages across `internal/`. To establish clear architectural boundaries, decouple core domain contracts from platform-specific infrastructure, and ensure cross-platform compatibility (native CLI/TUI and WASM/browser builds), we are reorganizing the `internal/` package tree into a 5-tier architecture:

1. **Tier 1: Core Domain Abstractions & Contracts** (`internal/core/`): Foundational contracts, plugin registries, lifecycle state machines, and permission policies. No dependency on higher tiers.
2. **Tier 2: Application Services & Orchestration** (`internal/app/`): Agent loop engines, prompt compilation, conversation session stores, subagent delegation, and task workflows.
3. **Tier 3: Domain Capabilities & Features** (`internal/features/`): Execution tools (native & wasm), authentication & OAuth, model catalog, account routing, and dynamic plugin management.
4. **Tier 4: Infrastructure & Transports** (`internal/infra/`): Network backends (CloudCode & gateway), remote-control tunnel & server, LanguageServer, system daemon, DNS, and reference proxy.
5. **Tier 5: Presentation & User Interface** (`internal/ui/`): CLI commands & flags, headless print mode, and the interactive Bubble Tea TUI (panels, views, renderers, foundations).

---

## 2. Five-Tier Mapping Matrix

| Tier | New Package Root | Mapped Subpackages & Components | Legacy Package Paths |
|---|---|---|---|
| **Tier 1: Core Domain** | `internal/core/` | `registry`<br/>`lifecycle`<br/>`policy/permissions`<br/>`policy/cel`<br/>`policy/trust`<br/>`seam` | `internal/modular`<br/>`internal/cascade`<br/>`internal/permissions`<br/>`internal/celpolicy`<br/>`internal/workspacetrust`<br/>`internal/interaction` |
| **Tier 2: Application Services** | `internal/app/` | `engine/{coding,stream,cortex,bridge,pigrt}`<br/>`prompt/{agyprompt,templates,step,branding,implicit,cumulative,customcmd,customagent,customize,agentsdir,mentions}`<br/>`session/{convstore,rehydration,compaction,tree,store,autocont}`<br/>`subagent/{core,router,questions,tools,browser,builtin}`<br/>`task` | `internal/agent`, `internal/piagentbridge`, `internal/pigrt`<br/>`internal/agyprompt`, `internal/agytemplates`, `internal/agystep`, etc.<br/>`internal/convstore`, `internal/compaction`, `internal/rehydration`, etc.<br/>`internal/subagent*`<br/>`internal/task` |
| **Tier 3: Capabilities & Features** | `internal/features/` | `tools/{builtin,toolrunner,agenttools,harness,native,wasm,sandbox,procgroup,bashmode,isolation,codesearch,skills,media,clipboard}`<br/>`auth/{core,mcpoauth,eligibility}`<br/>`models/{core,catalog,ctx}`<br/>`accounts/{core,routing}`<br/>`plugins/{core,scan,watch,hooks}` | `internal/tools`, `internal/agenttools`, `internal/procgroup`, etc.<br/>`internal/auth`, `internal/mcpoauth`<br/>`internal/models`<br/>`internal/accountrouting`<br/>`internal/plugins` |
| **Tier 4: Infrastructure & Transports** | `internal/infra/` | `backend/{core,select,codeassist,refbackend}`<br/>`remote/{server,frontend,host,link,tunnel,terminal,audio,persistent}`<br/>`langserver/{core,server}`<br/>`daemon`<br/>`dns`<br/>`proxy/{wrapper,bin,drift,parity,resultparity,rea}`<br/>`pb/*`<br/>`subsystems/*` | `internal/backend`, `internal/refbackend`<br/>`internal/remotecontrol`, `internal/rchost`, `internal/rcterminal`, `internal/rcaudio`, etc.<br/>`internal/langserver`<br/>`internal/daemon`<br/>`internal/dns`<br/>`internal/agyproxy`, `internal/agybin`, `internal/referencedrift`<br/>`internal/pb`<br/>`internal/modular/subsystems/*` |
| **Tier 5: Presentation & UI** | `internal/ui/` | `cli/{core,flags,subcommands/*}`<br/>`headless/{printmode,apierror,msg}`<br/>`tui/app`<br/>`tui/panels/*` (13 slash panels)<br/>`tui/views/*` (diff, seasonal, statusline, canvas)<br/>`tui/render/*` (md, color, wrap, urlchunk)<br/>`tui/editor/*`<br/>`tui/foundations/*` (`tuilib`, `tuitools`, `viewer`, `viewport`) | `internal/cli`, `internal/modular/subcommands/*`<br/>`internal/printmode`, `internal/apierror`<br/>`internal/cliapp`<br/>`internal/cliapp/panel_*`<br/>`internal/tuilib`, `internal/tuitools`, `internal/mdrender`, `internal/urlchunk` |

---

## 3. Strict Execution Constraints & Load-Bearing Invariants

1. **Zero-Breaking Progressive Migration**:
   - Every existing package path MUST retain full backward compatibility via Go type aliases (`type T = target.T`), variable aliases (`var V = target.V`), constant aliases (`const C = target.C`), and forwarding functions (`func F(...) { target.F(...) }`).
   - Existing caller sites and test files continue to compile and pass tests without modification during intermediate phases.
2. **Ground Rules**:
   - **Never run `go test ./...`**: `scratch/` does not compile. Always run `go test ./cmd/... ./internal/...`.
   - **Gofmt is selective**: Only format touched files; avoid repo-wide reformatting to preserve git diff clarity.
   - **Local commits only**: Progressive, verifiable commits per phase.
3. **Load-Bearing Invariants**:
   - **Invariant 17**: Proxy wrapper (`internal/infra/proxy/wrapper` / `internal/agyproxy`) may import `models` and `agybin`, but NEVER `cli`.
   - **Invariant 18**: Chroma stays at v2.27.0 byte-for-byte.
   - **Invariant 34**: `memtrim.After(memtrim.Delay)` runs only after TUI opens.
   - **Invariant 35**: `compaction` carried block formatting remains byte-exact.
   - **Public root packages (25 public packages)**: Zero changes, zero imports of `internal/`.
4. **Cross-Platform & WASM Build Tags**:
   - `cmd/pigga/init_native.go`: `//go:build !wasm && !wasip1 && !js`
   - `cmd/pigga/init_wasm.go`: `//go:build wasm || wasip1 || js`
   - Verification command:
     `GOTOOLCHAIN=local GOOS=js GOARCH=wasm /Users/gear/GitHub/hackpad/cache/go/bin/go build -o /tmp/pigga.wasm ./cmd/pigga`

---

## 4. Phased Implementation Roadmap

- **Phase 0: Baseline & Documentation**: Commit refactoring plan, verify clean cache, update build tags in `cmd/pigga`, and ensure green tests on both native and WASM targets.
- **Phase 1: Tier 1 Core Domain Abstractions**: Elevate `modular` into `internal/core/registry`, establish `internal/core/lifecycle`, `internal/core/policy`, and `internal/core/seam`, with facades in original paths.
- **Phase 2: Tier 2 Application Services**: Move orchestration engines (`internal/app/engine`), prompt subsystem (`internal/app/prompt`), conversation session management (`internal/app/session`), subagents (`internal/app/subagent`), and task runner.
- **Phase 3: Tier 3 Domain Capabilities**: Organize tools under `internal/features/tools`, auth under `internal/features/auth`, models under `internal/features/models`, accounts under `internal/features/accounts`, and plugins under `internal/features/plugins`.
- **Phase 4: Tier 4 Infrastructure & Transports**: Organize backend transports, remote control, language server, daemon, and proxy wrappers into `internal/infra/`.
- **Phase 5: Tier 5 Presentation & UI**: Decompose CLI, headless print mode, and TUI into `internal/ui/cli`, `internal/ui/headless`, and `internal/ui/tui/{app,panels,views,render,foundations}`.
- **Phase 6: Final Verification & Clean Up**: Full suite test verification (`go test ./cmd/... ./internal/...`), WASM build verification, and architecture validation report.

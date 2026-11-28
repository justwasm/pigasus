# `cmd/*` Entrypoints

> **Canonical inventory of all 15 binaries under `cmd/`.** Each entrypoint sits above the five `internal/` tiers (see [`architecture-tiers.md`](architecture-tiers.md)) and wires together hosts, public packages, or developer tooling.

---

## 1. Primary Product Entrypoints

User-facing CLIs, TUIs, and configuration tools shipped as core OpenAGY / Pigasus surfaces.

| Command | Package | Built by `make build` | Purpose & Key Dependencies |
|---|---|---|---|
| `pigasus` | `cmd/pigasus` | Yes (`./bin/pigasus`) | Primary OpenAGY / Pigasus CLI & TUI binary (`internal/clirunner`, `internal/tui`, `internal/printmode`, `internal/app/subcommands/*`, `internal/rchost/agentserver`, `internal/ui/livechat`, `internal/ui/terminalapp`). Supports interactive TUI, `--print` headless mode, `server`, `agent-server`, `livechat`, `mic-serve`, `stats`, `agy`, and `proxy`. |
| `pigga` | `cmd/pigga` | Yes (`./bin/pigga`) | Modular, streamlined Antigravity CLI (`internal/modular`, `internal/app/engine/stream`) with build-tagged native (`!wasm && !wasip1 && !js`) and Wasm (`wasm || wasip1 || js`) toolset initialization. |
| `livechat` | `cmd/livechat` | Yes (`./bin/livechat`) | Standalone live multimodal chat TUI/Web UI (`internal/ui/livechat`, `internal/infra/livevoice`, `internal/voice`). Also embedded in the main binary as `pigasus livechat`. |
| `herdr` | `cmd/herdr` | Yes (`./bin/herdr`) | Herdr terminal multiplexer TUI and Unix domain socket RPC CLI (`herdr/`), managing panes, workspaces, and agent prompts (`ping`, `pane`, `workspace`, `agent prompt`). |
| `openagy-models` | `cmd/openagy-models` | No | Model & custom provider registry CLI (`internal/features/models`) managing `~/.openagy/providers.json` (`list`, `add`, `remove`, `test`, `install-context`). |

---

## 2. Compatibility / Proxy / Shim Entrypoints

Binaries that reproduce upstream Antigravity wire protocols, wrap vendor binaries, or translate third-party LLM HTTP APIs.

| Command | Package | Built by `make build` | Purpose & Key Dependencies |
|---|---|---|---|
| `harness` | `cmd/harness` | Yes (`./bin/harness`) | Clean-room clone of Google Antigravity's closed-source `localharness` agent backend (`localharness/`, `budget/`, `lifecyclehooks/`), speaking the two-phase stdio protobuf handshake + WebSocket protojson protocol. See [`localharness.md`](localharness.md). |
| `langserver` | `cmd/langserver` | No | Standalone rebuild of the Antigravity IDE's `language_server` binary (`internal/lsserver`), reproducing its flag surface, `-stamp`, startup log, and Connect-RPC / Web UI listeners. See [`ide-language-server.md`](ide-language-server.md). |
| `mock-remote-agent` | `cmd/mock-remote-agent` | Yes (`./bin/mock-remote-agent`) | Standalone compatibility binary for the Antigravity remote-agent proxy (`internal/rchost/agentserver`), also exposed as `pigasus agent-server`. See [`remote-agents.md`](remote-agents.md). |
| `agy-update` | `cmd/agy-update` | No | Standalone `agy update` self-updater parity binary (`internal/agyupdate`, `internal/cli.ReferenceVersion`) with advisory file locking. |
| `pigproxy` | `cmd/pigproxy` | No | Standalone CloudCode proxy & admin CLI (`internal/infra/agyproxy`), defaulting to `serve` and exposing `doctor`, `log`, `capture`, `balance`, `aggregate`, and `version`. See [`proxy.md`](proxy.md). |
| `anthropic-shim` | `cmd/anthropic-shim` | No | HTTP compatibility shim translating Anthropic Messages API calls (`POST /v1/messages`, `GET /v1/models`) onto `internal/core/backend.Client`. |
| `openai-shim` | `cmd/openai-shim` | No | HTTP compatibility shim translating OpenAI Chat Completions API calls (`POST /v1/chat/completions`, `GET /v1/models`) onto `internal/core/backend.Client`. |

---

## 3. Developer / Verification / Reverse-Engineering Tooling

Internal engineering tools for UI component inspection, AST inventory generation, and upstream binary drift detection.

| Command | Package | Built by `make build` | Purpose & Key Dependencies |
|---|---|---|---|
| `storybook` | `cmd/storybook` | Yes (`./bin/storybook`, symlink `./bin/storyboard`) | Interactive Antigravity TUI Component Storybook & Playground (`internal/ui/storybook`, `github.com/justwasm/storybook`) for browsing and testing UI components in isolation. See [`tui-storybook-blueprint.md`](tui-storybook-blueprint.md). |
| `tui-inventory` | `cmd/tui-inventory` | Yes (`./bin/tui-inventory`) | AST & reverse-engineered TUI symbol inventory generator scanning `internal/ui` and `out/extracted-1.5.18/seeds/funcs.txt` to emit [`tui_component_inventory.json`](tui_component_inventory.json). |
| `agy-canary` | `cmd/agy-canary` | Yes (`./bin/agy-canary`) | Automated upstream binary monitor & drift defense tool (`internal/infra/referencedrift`) checking `reference.lock.json` against a live `agy` binary (`-check`, `-json`, `-update*`). See [`reference-drift.md`](reference-drift.md). |

---

## 4. Architecture & Build Notes

- **Layering**: `cmd/*` packages are entrypoints outside `internal/` and outside the public root packages (`architecture_test.go` R6 applies only to root public packages, not `cmd/*`).
- **Verification**: `make test` (`./scripts/verify.sh all`) runs `go test ./cmd/...`; `make hygiene` (`./scripts/verify.sh hygiene`) runs `go vet ./...` and the AST hygiene analyzer (`devutils/hygiene`) across all `cmd/*` packages.

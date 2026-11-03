# The clone's guide

> [`AGENTS.md`](../../AGENTS.md) is the one-screen orientation; **these files
> are the detail**. Open the one that matches what you are changing — each is a
> focused topic, not a narrative.

| File | Topic | Read it when you… |
|---|---|---|
| [`architecture.md`](architecture.md) | the package map, the two turn engines, the two transports | you need the shape of the tree |
| [`architecture-tiers.md`](architecture-tiers.md) | the five tiers under `internal/`, the facade map, and the rule for new packages | you are about to move a package, or a document names a path you cannot find |
| [`agent-loop.md`](agent-loop.md) | PiG's stream contract, the bridge, the transport split, the PiG traps | touch `internal/agent`, `internal/pigrt`, `internal/piagentbridge` |
| [`headless.md`](headless.md) | the headless surface: `--print`, the JSON envelope, the NDJSON stream, the stdin pipeline, the exit codes | touch `internal/printmode`, the `--print` flags, or a script that reads stdout |
| [`agent-server.md`](agent-server.md) | the client/server split: `pigasus server`, `pigasus tui --server`, the `agentlink` wire, the `agentturn` seam | touch `agentlink`, `internal/app/agentturn`, `internal/app/agentserver`, or the TUI's turn loop |
| [`models.md`](models.md) | model routing, `providers.json`, the one writer, the catalogue | touch `internal/infra/backend`, `internal/features/models`, `internal/cli` |
| [`endpoints.md`](endpoints.md) | the named service endpoints, `config.json`, the resolver, the precedence | touch `endpoints`, or a URL that points at a Google service |
| [`cloudcode-dialect.md`](cloudcode-dialect.md) | the CloudCode wire: the request envelope, the SSE frames, the tool round trip — the contract for a local service *and* the derivation of each field | touch `geminicode/`, or answer the clone from a local service |
| [`accounts.md`](accounts.md) | the account registry, the shared credential, the tier/quota cache, the account-routing policy, the virtual unified pool account | touch `internal/features/accounts`, `internal/features/auth`, `accountpool`, `internal/infra/agyproxy` |
| [`prompt.md`](prompt.md) | the system prompt, the skill/rule discovery, `--agent`, the recovered `.tmpl` bundle | touch `internal/app/prompt/agyprompt`, `internal/app/prompt/agytemplates`, `internal/app/prompt/step`, `internal/app/agenttools` |
| [`tools.md`](tools.md) | the 12 wire declarations, the 60 advertised names, the result texts, the two command-result shapes | touch `internal/app/tools`, `internal/app/toolrunner`, `internal/ui/tuitools` |
| [`permissions.md`](permissions.md) | the permission engine: the `action(target)` grant grammar, the deny/ask/allow precedence, `toolPermission` and the hook verdicts | touch `internal/core/policy/permissions`, the `/permissions` panel, or a rule that does not match |
| [`hooks.md`](hooks.md) | the `hooks.json` runtime: the six events, the document shape, the decisions and the handler execution | touch `internal/core/policy/hooks`, the `/hooks` panel, or a hook that does not fire |
| [`plugins.md`](plugins.md) | the `/plugin` Discover tab's shelves, `agy plugin import`, the MCP server conversion | touch `internal/ui/views/pluginsui`, the `/plugin` panel, or the plugin importer |
| [`tui.md`](tui.md) | the transcript display, the model picker, argument completion, bash mode, syntax highlighting | touch a panel or the composer |
| [`compaction.md`](compaction.md) | the two compaction paths, the reference's numbers, the context window | touch `internal/core/compaction`, `internal/pigrt` |
| [`reference-drift.md`](reference-drift.md) | the gate, the version-scoped artefacts, re-evidencing | the reference binary moves |
| [`legacy-evidence.md`](legacy-evidence.md) | what the 1.3.x-era evidence sweep retired, what survived, and the recipe that rebuilds it | a citation points at a retired path, or you need the old artefacts back |
| [`proxy.md`](proxy.md) | the CloudCode proxy, the launcher, the capture directory | touch `internal/infra/agyproxy`, `internal/infra/proxy/bin` |
| [`remote-control.md`](remote-control.md) | the tunnel, the daemon, the Web UI's wire, the terminals and the voice | touch `internal/remotecontrol`, `internal/rchost`, `internal/infra/remote/link` |
| [`reverse-engineering.md`](reverse-engineering.md) | pclntab → seeds → Ghidra → reports, the formats, the analysis gotchas | reverse the binary |
| [`ide-language-server.md`](ide-language-server.md) | the IDE's second reference binary: the name diff vs the CLI, the shared prompt, the spawn contract, the decompiled cross-check, the Web UI's wire and `--reference-backend` | cross-check the CLI, open the IDE-only surface, or run the reference as the web backend |
| [`localharness.md`](localharness.md) | the SDKs' second reference binary: the stdio+websocket transport, the bidirectional hook protocol, the budget manager | touch `localharness`, `lifecyclehooks`, `budget` |
| [`public-packages.md`](public-packages.md) | which packages are public, the rule that keeps them importable, and the ROI ranking of what is queued next | you are about to move a package out of `internal/`, or you want to know what a third party can import |
| [`environment.md`](environment.md) | the two RE environments, the toolchain, the machine gotchas | run anything that needs Ghidra |

Every claim in these files is backed by a cycle report under
[`docs/cycles/`](../cycles/): **the report is the evidence, the guide is the
current state**. When they disagree, the newer report wins — and the guide
gets updated in the same change.

---

*Provenance: these files were split out of `AGENTS.md` on 2026-10-24 (the
pre-split file is `544959d3`), which is what keeps `AGENTS.md` down to one
screen. The split was mechanical — the sections were moved verbatim — so a
claim that is wrong here is wrong there too; fix both.*

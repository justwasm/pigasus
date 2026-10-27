# The clone's guide

> [`AGENTS.md`](../../AGENTS.md) is the one-screen orientation; **these files
> are the detail**. Open the one that matches what you are changing — each is a
> focused topic, not a narrative.

| File | Topic | Read it when you… |
|---|---|---|
| [`architecture.md`](architecture.md) | the package map, the two turn engines, the two transports | you need the shape of the tree |
| [`agent-loop.md`](agent-loop.md) | PiG's stream contract, the bridge, the transport split, the PiG traps | touch `internal/agent`, `internal/pigrt`, `internal/piagentbridge` |
| [`models.md`](models.md) | model routing, `providers.json`, the one writer, the catalogue | touch `internal/backend`, `internal/models`, `internal/cli` |
| [`endpoints.md`](endpoints.md) | the named service endpoints, `config.json`, the resolver, the precedence | touch `endpoints`, or a URL that points at a Google service |
| [`cloudcode-dialect.md`](cloudcode-dialect.md) | the CloudCode wire: the request envelope, the SSE frames, the tool round trip — the contract for a local service *and* the derivation of each field | touch `geminicode/`, or answer the clone from a local service |
| [`accounts.md`](accounts.md) | the account registry, the shared credential, the tier/quota cache | touch `internal/accounts`, `internal/auth` |
| [`prompt.md`](prompt.md) | the system prompt, the skill/rule discovery, `--agent`, the recovered `.tmpl` bundle | touch `internal/agyprompt`, `internal/agytemplates`, `internal/agystep`, `internal/agenttools` |
| [`tools.md`](tools.md) | the 12 wire declarations, the 60 advertised names, the result texts, the two command-result shapes | touch `internal/tools`, `internal/toolrunner`, `internal/tuitools` |
| [`tui.md`](tui.md) | the transcript display, the model picker, argument completion, bash mode, syntax highlighting | touch a panel or the composer |
| [`compaction.md`](compaction.md) | the two compaction paths, the reference's numbers, the context window | touch `internal/compaction`, `internal/pigrt` |
| [`reference-drift.md`](reference-drift.md) | the gate, the version-scoped artefacts, re-evidencing | the reference binary moves |
| [`proxy.md`](proxy.md) | the CloudCode proxy, the launcher, the capture directory | touch `internal/agyproxy`, `internal/agybin` |
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

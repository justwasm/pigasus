# The clone's guide

> [`AGENTS.md`](../../AGENTS.md) is the one-screen orientation; **these files
> are the detail**. Open the one that matches what you are changing — each is a
> focused topic, not a narrative.

| File | Topic | Read it when you… |
|---|---|---|
| [`architecture.md`](architecture.md) | the package map, the two turn engines, the two transports | you need the shape of the tree |
| [`agent-loop.md`](agent-loop.md) | PiG's stream contract, the bridge, the transport split, the PiG traps | touch `internal/agent`, `internal/pigrt`, `internal/piagentbridge` |
| [`models.md`](models.md) | model routing, `providers.json`, the one writer, the catalogue | touch `internal/backend`, `internal/models`, `internal/cli` |
| [`accounts.md`](accounts.md) | the account registry, the shared credential, the tier/quota cache | touch `internal/accounts`, `internal/auth` |
| [`prompt.md`](prompt.md) | the system prompt, the skill/rule discovery, `--agent` | touch `internal/agyprompt`, `internal/agenttools` |
| [`tools.md`](tools.md) | the 12 wire declarations, the 60 advertised names, the result texts | touch `internal/tools`, `internal/toolrunner` |
| [`tui.md`](tui.md) | the transcript display, the model picker, argument completion, bash mode | touch a panel or the composer |
| [`compaction.md`](compaction.md) | the two compaction paths, the reference's numbers, the context window | touch `internal/compaction`, `internal/pigrt` |
| [`reference-drift.md`](reference-drift.md) | the gate, the version-scoped artefacts, re-evidencing | the reference binary moves |
| [`proxy.md`](proxy.md) | the CloudCode proxy, the launcher, the capture directory | touch `internal/agyproxy`, `internal/agybin` |
| [`reverse-engineering.md`](reverse-engineering.md) | pclntab → seeds → Ghidra → reports, the formats, the analysis gotchas | reverse the binary |
| [`ide-language-server.md`](ide-language-server.md) | the IDE's second reference binary: the name diff vs the CLI, the shared prompt, the spawn contract, the decompiled cross-check | cross-check the CLI, or open the IDE-only surface |
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

# AGENTS.md

> **One screen of orientation.** The detail lives in [`docs/guide/`](docs/guide/) —
> open the entry that matches what you are changing. Every claim in the guide is
> backed by a cycle report under [`docs/cycles/`](docs/cycles/); the report is the
> evidence, the guide is the current state. When they disagree, the newer report
> wins and the guide gets updated.
>
> | I want to… | Read |
> |---|---|
> | change the turn loop, the transports or the bridge | [`docs/guide/agent-loop.md`](docs/guide/agent-loop.md) |
> | change model routing or the provider config | [`docs/guide/models.md`](docs/guide/models.md) |
> | change the prompt or `--agent` | [`docs/guide/prompt.md`](docs/guide/prompt.md) |
> | change a tool or its result text | [`docs/guide/tools.md`](docs/guide/tools.md) |
> | change a TUI panel | [`docs/guide/tui.md`](docs/guide/tui.md) |
> | re-run the Ghidra pipeline | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) |
> | the reference binary moved | [`docs/guide/reference-drift.md`](docs/guide/reference-drift.md) |
> | cross-check the CLI against the IDE's binary | [`docs/guide/ide-language-server.md`](docs/guide/ide-language-server.md) |

---

## What this repository is

**Not an application** — a reverse-engineering research workspace with three
things in it:

| Path | What it is |
|---|---|
| `rea-go-seeds/` | a vendored copy of the OSS `pclnseeds` tool: recovers Go function names from a stripped binary's `pclntab` (175 lines, stdlib only, zero dependencies) |
| `out/` | the committed **evidence** directory — reports, seed files, call graph, Ghidra postScripts, raw `readelf`/`strings` dumps. *Results*, not programs |
| `cmd/`, `internal/` | the **clone**: a from-scratch Go reimplementation of the Antigravity CLI, built for byte-level parity with the reference binary |

The reference is **Antigravity CLI** (`agy`) — a closed-source Google-internal
agent CLI (`google3/third_party/jetski/…`). The clone emulates **agy 1.4.7**
(`internal/cli.ReferenceVersion`).

There is a **second reference binary**: the Antigravity **IDE** ships its own
`language_server` (same monorepo commit as the CLI). Its evidence lives in
`out/ide-language-server/` — use it to cross-check a finding, never to mix
addresses.

| | |
|---|---|
| Module | `github.com/justwasm/pigasus` |
| Entry point | `cmd/pigasus` (also `cmd/openai-shim`, `cmd/anthropic-shim`, `cmd/openagy-models`; helpers in `devutils/`) |
| Build | `go build -o /tmp/pigasus ./cmd/pigasus` |
| Test | `go test ./cmd/... ./internal/...` |
| Size | ~110 MB (PiG's `coding` runtime links `go/types`, `go/doc`, `runtime/pprof`) |

---

## Ground rules

- **Never `go test ./...`** — `scratch/` has never compiled.
- **`gofmt -l` is not clean** for `internal/` (50+ files, always has been). Do
  not reformat wholesale; it buries real diffs.
- **A second agent shares this tree.** Re-read a file before editing it, stage
  only your own paths, commit as soon as they are green, and re-check
  `git status` before concluding that a change vanished.
- **`pigasus` commands are a superset of `agy`.** Extension commands (`provider`,
  `model`, `proxy`, `accounts`) are publicly visible subcommands in `pigasus`.
  In the TUI, `/accounts` and `/provider` are publicly listed slash commands.
- **Evidence grades** — keep them when adding findings: 【一手】 first-hand
  against the binary · 【反编译】 from decompiled output at a confirmed address ·
  【strings】 string-table hit only · 【推测】 hypothesis.
- **Report language follows the file being edited**: `out/*.md` is zh-CN,
  `docs/cycles/*` is English. New reports end with a 硬约束 / 存疑 /
  limitations section.
- **Local commits only.** Nothing is pushed from this checkout.

---

## The clone in one map

```
cmd/pigasus ──┬── TUI (bubbletea)      → internal/agent.Loop → PiG  agent.Agent
               └── --print (headless)   → internal/pigrt    → PiG  coding.Session
                        │
                  internal/piagentbridge   ← the only PiG translation layer
                        │
                  internal/backend         ← CloudCode | gateway (OpenAI / Anthropic)
                        │
                  internal/tools           ← 12 wire declarations, 60 advertised names
                        │
                  internal/agyprompt       ← the reference's system prompt, byte for byte
```

- **Two turn engines.** `--print` runs PiG's `coding.Session` (that is what
  brings MCP and skills); the TUI runs PiG's `agent.Agent`.
  `internal/agent.Loop.Send` is a one-line delegation.
- **Two transports.** A model from the reference's catalogue (Gemini / Claude /
  GPT-OSS) → **CloudCode**; a model *you* configured → **your endpoint**. A
  model nobody configured lands on the shim **with a warning that names
  `providers.json`** — answering from the shim without saying so would look
  like a working model.
- **One configuration file**: `~/.config/openagy/providers.json`, provider
  centric. `internal/models.Editor` is its only writer; the
  `provider`/`model` commands and the TUI's `/provider` panel are its only
  front-ends.
- **The prompt is the reference's**, not PiG's: `internal/agyprompt` rebuilds
  it byte for byte (the 1.4.6 capture is 70,853 B —
  `internal/agyprompt/testdata/capture_1.4.6.txt`).
- **The tool surface is two lists**: the wire carries the reference's **12**
  declarations, `init.tools` advertises **60** names.

---

## Load-bearing invariants

The facts that cost real time to rediscover. Each one breaks something
subtle if it is violated.

**The agent loop**

1. `pigasus agy` is dispatched **before the parser exists** — only a literal
   first argument `agy` selects the launcher, and the vendor binary must see
   its command line untouched.
2. The extension runtime must be **shared**: the `*extension.ExtensionRuntime`
   handed to `RuntimeOptions` has to be the one the built-in factories
   registered into, or MCP servers never reach `session_start`.
3. `ai.Provider.Stream(ctx, transcript, opts)` **does not receive the model**,
   so a multi-model provider must be pinned per model (`Provider.Model(label)`).
4. PiG copies only `err.Error()` across the stream boundary — a typed error
   (`*backend.HTTPError`) has to be recorded out of band (`errorSlot`,
   `pigrt.ErrorSlot`), and a pre-stream failure must be returned as
   `(nil, err)`, never as an in-stream event.
5. PiG's `emit` **blocks**, so the event drain runs independently of `Send`;
   and `Send` returns before the drain has processed the last events — call
   `session.FlushEvents(ctx)` before clearing `onText` or stdout truncates.
6. The native provider is **OpenAI-shaped only**; an Anthropic-configured
   provider goes through `BackendProvider`/`StreamFn`. One implementation of
   the Messages dialect, not two.
7. The gateway client and the native provider must stay **behaviourally
   identical**: `ExternalModel` remap, `SystemFingerprint`, `User-Agent`, the
   `*backend.HTTPError` classification.

**Transports and tools**

8. `run_command` is a **shell**, not an argv splitter (`zsh -c` on darwin). The
   reference's own `mvdan/sh` is only a *parser* — it never executes anything.
9. A **non-zero exit is a nil-error result**, not a failure — the model reads
   the code.
10. Tool path arguments are **absolute**. Never `filepath.Join(workspace,
    modelPath)` — it turns `/etc/x` into `<ws>/etc/x` with no error. Reads are
    not workspace-bound; writes are (compared after symlink resolution).
11. Function declarations must be **unique** per request — Gemini rejects the
    whole request if a name appears twice.
12. On macOS the live token is in the **keychain**, not the on-disk copies.

**The workspace**

13. **Never mix addresses between the two RE environments** (macOS Mach-O vs
    the Linux/ELF sandbox). See
    [`docs/guide/environment.md`](docs/guide/environment.md).
14. **Every address moves on every `agy` release.** Regenerate the seeds
    before citing one.
15. The proxy's env vars and on-disk paths (`OPENAGY_WRAPPER_*`,
    `~/.openagy/wrapper.{json,log}`) keep their names **on purpose** —
    renaming them silently moves an existing capture directory.
16. `internal/agyproxy` may import `internal/models` (read-only) and
    `internal/agybin`, but **not** `internal/cli`.

---

## Commands

```bash
go build -o /tmp/pigasus ./cmd/pigasus          # build
go test ./cmd/... ./internal/...                # test (never ./...)
go build ./cmd/...                              # what CI builds

/tmp/pigasus --help                              # visible subcommands (reference twelve + openagy superset)
/tmp/pigasus proxy version                      # vendor binary + registry
/tmp/pigasus provider list                       # the provider registry
/tmp/pigasus model list                          # the models it serves
```

CI: `.github/workflows/test.yml` (build + test + three offline smoke steps) and
`.github/workflows/release.yml` (cross-platform archives; the published repo is
`justwasm/pigasus`). The snapshot publisher is `scripts/publish-snapshot.sh`
(`--watch` follows the tag; the allowlist is what ships).

---

## This machine

| Tool | Version | Path |
|---|---|---|
| Go | 1.27.x | `~/go/bin/go` |
| JDK | 21.0.12.1 | `/opt/homebrew/opt/openjdk@21` |
| Ghidra | 12.1.4 | `~/tools/ghidra_12.1.4_PUBLIC` |
| rea-agents | 6.4.0 | `/opt/homebrew/bin/rea` |

`rea doctor --provider ghidra` is all-green. The macOS RE pipeline is
configured and runs end to end here — results in
`out/local-macos/FINDINGS.md`, environment in `out/local-macos/env.sh`, and
the SOP in
[`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md).

Four machine gotchas, each of which cost real time:

1. **The official Ghidra zip ships no macOS decompiler** — build it
   (`support/gradle && ./gradlew buildNatives`, ~3 min).
2. **Ghidra searches the CWD for scripts before `-scriptPath`** — run
   `analyzeHeadless` from a neutral CWD (`/tmp`) and keep the script in its own
   directory.
3. **The REA wrapper cannot analyse this binary** (a ~5.5 min internal timeout
   against an ~11 min seed pass). Bypass it — the recipe is in the guide.
4. `~/GitHub` is a symlink to `~/Documents/GitHub`; Ghidra canonicalises paths,
   so it shows up in error messages.

---

## Where to start

| If you want to… | Read / run |
|---|---|
| Build and test the clone | `go build -o /tmp/pigasus ./cmd/pigasus` then `go test ./cmd/... ./internal/...` |
| Understand the turn loop | [`docs/guide/agent-loop.md`](docs/guide/agent-loop.md) — `internal/pigrt/runtime.go` (print) and `internal/agent/pig.go` (TUI) |
| See the loop run end to end | `go run ./devutils/pigloop-demo -mode loop` (scripted gateway, no network) |
| Touch the gateway transport | `internal/piagentbridge/gatewayprovider.go` + `internal/backend/gateway.go` — the two must stay behaviourally identical |
| Know what the last cycle changed | `git log --oneline -20` and the newest `docs/cycles/cycle-<n>/` |
| Match the reference's system prompt | [`docs/guide/prompt.md`](docs/guide/prompt.md) — `internal/agyprompt`, ground truth `internal/agyprompt/testdata/capture_1.4.6.txt` |
| Change what the model can call | [`docs/guide/tools.md`](docs/guide/tools.md) — `internal/tools/catalog.go` |
| Configure a model | [`docs/guide/models.md`](docs/guide/models.md) — `~/.config/openagy/providers.json` |
| Know whether the reference binary moved | `go test ./internal/referencedrift/` — it reports which evidence is stale |
| Reverse the binary | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) |
| Find an external reference | `docs/research/REFERENCES.md` |

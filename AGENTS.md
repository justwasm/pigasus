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
> | change the headless surface — `--print`, the envelope, the stream | [`docs/guide/headless.md`](docs/guide/headless.md) |
> | change model routing or the provider config | [`docs/guide/models.md`](docs/guide/models.md) |
> | point a Google service at a local one | [`docs/guide/endpoints.md`](docs/guide/endpoints.md) |
> | change the CloudCode wire, or stand up a local service that speaks it | [`docs/guide/cloudcode-dialect.md`](docs/guide/cloudcode-dialect.md) — `geminicode/` |
> | change the prompt or `--agent` | [`docs/guide/prompt.md`](docs/guide/prompt.md) |
> | change a tool or its result text | [`docs/guide/tools.md`](docs/guide/tools.md) |
| change a permission rule, the ask tier or `toolPermission` | [`docs/guide/permissions.md`](docs/guide/permissions.md) |
| change the `hooks.json` runtime, an event or a decision | [`docs/guide/hooks.md`](docs/guide/hooks.md) |
> | change a TUI panel | [`docs/guide/tui.md`](docs/guide/tui.md) |
> | change the `/plugin` panel or `agy plugin import` | [`docs/guide/plugins.md`](docs/guide/plugins.md) |
> | change the cloud remote view, its tunnel or its wire | [`docs/guide/remote-control.md`](docs/guide/remote-control.md) |
> | move a package, or work out where a package actually lives | [`docs/guide/architecture-tiers.md`](docs/guide/architecture-tiers.md) — the five tiers and the facade map |
> | re-run the Ghidra pipeline | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) |
> | the reference binary moved | [`docs/guide/reference-drift.md`](docs/guide/reference-drift.md) |
> | cross-check the CLI against the IDE's binary | [`docs/guide/ide-language-server.md`](docs/guide/ide-language-server.md) |
> | change the IDE's browser bridge (`webview_browser`) | [`docs/guide/ide-language-server.md`](docs/guide/ide-language-server.md) — `agentbrowser` |
> | wire the SDKs' agent backend, hooks or budgets | [`docs/guide/localharness.md`](docs/guide/localharness.md) — `localharness/`, `lifecyclehooks/`, `budget/` |
> | change the client/server split, the wire or the turn seam | [`docs/guide/agent-server.md`](docs/guide/agent-server.md) — `agentlink/`, `internal/app/agentturn/`, `internal/app/agentserver/` |
> | find a command or a flag that `--help` does not list | [`docs/guide/hidden-commands-and-flags.md`](docs/guide/hidden-commands-and-flags.md) — the hidden surface, in one place |
> | know which packages are public, and what is queued next | [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |

---

## What this repository is

**Not an application** — a reverse-engineering research workspace with three
things in it:

| Path | What it is |
|---|---|
| `rea-go-seeds/` | a vendored copy of the OSS `pclnseeds` tool: recovers Go function names from a stripped binary's `pclntab` (175 lines, stdlib only, zero dependencies) |
| `out/` | the committed **evidence** directory — reports, seed files, call graph, Ghidra postScripts, raw `readelf`/`strings` dumps. *Results*, not programs |
| `cmd/`, `internal/` | the **clone**: a from-scratch Go reimplementation of the Antigravity CLI, built for byte-level parity with the reference binary |
| `geminicode/` | the **CloudCode dialect** as a standalone package — the wire types, the request envelope, the SSE frames. No `internal/` dependency, so it imports on its own; `internal/infra/backend` adapts it. See [`docs/guide/cloudcode-dialect.md`](docs/guide/cloudcode-dialect.md) |
| `localharness/`, `lifecyclehooks/`, `budget/`, `harnessprompt/` | the **SDK-facing surface** as public packages: the `localharness` wire (both halves), the bidirectional hook protocol, the limit manager, the harness system instruction. No `internal/` dependency, so a third party implementing the SDK side imports them directly. See [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |
| `lsproto/`, `remotetransport/`, `rctoken/`, `originpolicy/`, `endpoints/` | the **Web UI's wire** as public packages: the proto-JSON dialect, the Connect and gRPC-Web transport, the token format, the origin policy, the named-endpoint resolver. No `internal/` dependency, so a local service that answers the Web UI imports them directly. See [`docs/guide/remote-control.md`](docs/guide/remote-control.md) |
| `render/`, `diffsyntax/`, `palette/`, `retry/`, `retrybudget/`, `atomicfile/` | the **library tranche** as public packages: the markdown renderer, the diff highlighter, the true-colour palette, the retry helpers, the atomic file write. No `internal/` dependency, so they import on their own. See [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |
| `browsercdp/`, `browserbridge/`, `agentbrowser/`, `mcp/`, `envref/`, `grpc/` | the **browser and MCP surface** as public packages: the Chrome DevTools Protocol client, the in-Chrome bridge protocol, the browser tool's broker, the MCP client, the env-reference resolver, the recovered gRPC service registry. No `internal/` dependency, so they import on their own. See [`docs/guide/tools.md`](docs/guide/tools.md) |
| `vim/`, `imagery/` | the **modal editor and downscaler** as public packages: the modal editing engine, the image downscaler. No `internal/` dependency, so they import on their own. See [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |
| `rcterminal/`, `urlchunk/`, `procgroup/`, `celpolicy/`, `dns/` | the **sixth tranche** as public packages: the live pty terminal, the terminal hyperlink chunker, the process group manager, the CEL policy evaluator, the pure-Go DNS fallback. No `internal/` dependency, so they import on their own. See [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |
| `accountpool/` | the **account-routing policy** as a public package: the `config.json` section's shape, the five strategies, the quota threshold, the decision. Pure — no I/O, no `internal/` dependency — so it imports on its own; the runtime is `internal/features/accounts/routing`. See [`docs/guide/accounts.md`](docs/guide/accounts.md) |
| `rcmesh/` | the **WebRTC mesh signaling and transport wire** as a public package: the signaling client, ECDSA P-256 channel binding, ICE configuration parser, chunk reassembler, and peer manager. No `internal/` dependency; native Pion lives in `modules/rcmesh-pion/`. See [`docs/guide/remote-control.md`](docs/guide/remote-control.md) |
| `configdir/` | the **one configuration home** as a public package: `~/.openagy` (or `$OPENAGY_CONFIG_HOME`), the directory every configuration file of the clone lives in. No `internal/` dependency, so it imports on its own. See [`docs/guide/endpoints.md`](docs/guide/endpoints.md) |
| `agentlink/` | the **client/server wire** as a public package: the frames, the client, the server, the bearer token. No `internal/` dependency, so a third party can attach its own client to a `pigasus server`. The runtime is `internal/app/agentturn/` + `internal/app/agentserver/`. See [`docs/guide/agent-server.md`](docs/guide/agent-server.md) |

The reference is **Antigravity CLI** (`agy`) — a closed-source Google-internal
agent CLI (`google3/third_party/jetski/…`). The clone emulates **agy 1.4.12**
(`internal/cli.ReferenceVersion`).

There is a **second reference binary**: the Antigravity **IDE** ships its own
`language_server` (same monorepo commit as the CLI). Its evidence lives in
`out/ide-language-server/` — use it to cross-check a finding, never to mix
addresses.

And a **third**: `localharness`, the agent backend the `google-antigravity`
PyPI SDKs spawn. It owns the agent loop and speaks a stdio+websocket
protocol; it embeds the *same* `jetski/cortex` engine the CLI uses. Its
evidence lives in `out/localharness/`, and the clone's counterparts are
`localharness/`, `lifecyclehooks/`, `budget/` — plus `cmd/harness`, the clone
of that binary.

| | |
|---|---|
| Module | `github.com/justwasm/pigasus` |
| Entry point | `cmd/pigasus` (also `cmd/harness`, `cmd/pigcap`, `cmd/openai-shim`, `cmd/anthropic-shim`, `cmd/openagy-models`, `cmd/agy-update`, `cmd/langserver`, `cmd/pigga`; helpers in `devutils/`) |
| Build | `go build -o /tmp/pigasus ./cmd/pigasus` |
| Test | `go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...` |
| Size | ~132 MiB / ~139 MB (PiG's `coding` runtime links `go/types`, `go/doc`, `runtime/pprof`) |

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
  In the TUI, `/accounts`, `/provider` and `/pool` are publicly listed slash
  commands.
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
                  internal/infra/backend   ← CloudCode | gateway (OpenAI / Anthropic)
                        │
                        └─→ geminicode   ← the CloudCode dialect, standalone (no internal/ deps)
                        │
                  internal/app/tools      ← the tool contract (Tool, Registry, ArgsOf)
                        │
                  internal/app/tools/*    ← 12 wire declarations, 60 advertised names
                     shell · filesystem · browser · web · tasks · surface
                        │
                  internal/app/prompt/agyprompt    ← the reference's system prompt, byte for byte
                        │
                  internal/app/prompt/agytemplates ← the reference's 17 .tmpl files, embedded once
                        │
                  internal/app/prompt/step ← the reference's step strings + the result reader
                                                (the old path internal/agystep is gone)

cmd/harness  ──┬── localharness          ← the localharness wire, both halves
               ├── internal/pigrt         → PiG  coding.Session   (the same turn engine)
               ├── internal/app/tools/surface ← the reference's tool names
               ├── budget                 ← the limits
               └── lifecyclehooks         ← the hook model

pigasus server ─── agentlink                   ← the client/server wire (standalone)
pigasus tui   ─── internal/app/agentturn      ← the turn seam: Local | Remote
               └── internal/app/agentserver   ← the host: workspace, tools, policy

the public surface (no internal/ dependency — importable by anyone)
  geminicode       the CloudCode dialect: the envelope, the SSE frames
  localharness     the SDK's wire, both halves        (+ localharness/pb)
  lifecyclehooks   the hook protocol
  budget           the limit manager
  harnessprompt    the harness's system instruction
  lsproto          the Web UI's proto-JSON dialect
  remotetransport  the Connect and gRPC-Web transport
  rctoken          the remote-control token
  originpolicy     the browser-origin policy
  endpoints        the named service endpoints
  configdir        the clone's one configuration home
  accountpool      the account-routing policy: the strategies, the threshold
  render           the markdown renderer
  diffsyntax       the diff highlighter
  palette          the true-colour ANSI palette
  retry            the retry / backoff helper
  retrybudget      the per-turn retry wait cap
  atomicfile       the atomic file write
  browsercdp       the Chrome DevTools Protocol client
  browserbridge    the in-Chrome bridge protocol
  agentbrowser     the browser tool's broker
  mcp              the MCP client
  envref           the ${env:NAME} resolver
  grpc             the recovered v4 gRPC service registry
  vim              the modal editing engine
  imagery          the image downscaler
  rcterminal       the Web UI's live pty terminal
  urlchunk         the terminal OSC 8 hyperlink chunker
  procgroup        the cross-platform process group manager
  celpolicy        the CEL policy evaluation engine
  dns              the pure-Go DNS resolver fallback
  rcmesh           the WebRTC mesh signaling wire and chunk reassembler
  agentlink        the client/server wire: the frames, the client, the server
```

- **Every Google service URL is a named endpoint.** `endpoints`
  owns the names, the compiled-in defaults and the resolver over
  `~/.openagy/config.json`; the live call sites read
  `endpoints.Current()` instead of a constant. A URL that still names a
  Google service in the *live* paths is a gap. See
  [`docs/guide/endpoints.md`](docs/guide/endpoints.md).

- **Two turn engines.** `--print` runs PiG's `coding.Session` (that is what
  brings MCP and skills); the TUI runs PiG's `agent.Agent`.
  `internal/agent.Loop.Send` is a one-line delegation.
- **Two transports.** A model from the reference's catalogue (Gemini / Claude /
  GPT-OSS) → **CloudCode**; a model *you* configured → **your endpoint**. A
  model nobody configured lands on the shim **with a warning that names
  `providers.json`** — answering from the shim without saying so would look
  like a working model.
- **One configuration home, two configuration files.** Everything lives in
  `~/.openagy` (or `$OPENAGY_CONFIG_HOME`) — `configdir` is the resolver,
  and there is no second home any more: the retired
  `~/.config/openagy` was moved into it by hand. Inside it,
  `providers.json` is provider centric (`internal/features/models.Editor` is its only
  writer; the `provider`/`model` commands and the TUI's `/provider` panel are
  its only front-ends) and `config.json` holds the named **service
  endpoints**, the **account-routing policy** and the **proxy's own state**
  under a `"wrapper"` section — `endpoints` is its only reader *and* its
  only writer: `endpoints.SaveAccountPool` is what the TUI's Pool tab writes
  through, `endpoints.SaveWrapper` is what the proxy writes through. The
  split between the two files is the point: `providers.json` answers *"which
  model talks to which provider"*, `config.json` answers *"which service
  does the clone itself talk to"* and *"which of my accounts serves this
  turn"*.
- **The prompt is the reference's**, not PiG's: `internal/app/prompt/agyprompt` rebuilds
  it byte for byte (the 1.4.6 capture is 70,853 B —
  `internal/app/prompt/agyprompt/testdata/capture_1.4.6.txt`). It is **rendered from the
  reference's own `.tmpl` files** (recovered into
  `internal/app/prompt/agytemplates/templates/`, 17 of them) — seven of the twelve
  sections come from a template, the other five have none in the binary. The
  reference's builtin skill bundle is in `internal/features/tools/skills/`.
- **A command's result has two shapes.** The reference's *current* one
  (`Command exited with code N.` + `Command output:`) is the CLI's own
  formatter, and it is what the clone sends. The *step string*
  (`The command exited with code N.` + `Output:`) is what the recovered
  `step_strings/run_command.tmpl` renders, and it is what the reference
  produced at 1.4.2/1.4.3. `internal/app/prompt/step` renders it and reads
  both; `internal/ui/tuitools` and `internal/app/session/convstore` read through it. A
  consumer written against only one of them silently mis-reads the other.
- **The tool surface is two lists**: the wire carries the reference's **12**
  declarations, `init.tools` advertises **60** names.
- **A `!` command has its own store.** The reference never puts a shell
  command in the conversation: it writes it into an *implicit trajectory* —
  `<gemini-dir>/implicit/<uuid>.pb`, AES-256-GCM, one file per workspace.
  `internal/app/prompt/implicit` writes that format byte for byte, and
  `internal/app/prompt/cumulative` converts the trajectory into the
  reference's own chat messages for the next prompt — the command still never
  reaches the conversation store.
- **A third surface exists but is not wired into the turn loop yet**:
  `localharness/` (the SDKs' transport to the `localharness` agent
  backend), `lifecyclehooks/` (its bidirectional hook protocol) and
  `budget/` (its limit manager). They are **public** packages on purpose —
  a third party implementing the SDK side needs them — see
  `docs/guide/localharness.md` and `docs/guide/public-packages.md`.
- **The Web UI has two surfaces of its own**: the terminal
  (`rcterminal/` — a live shell per terminal, on a pty) and the
  voice (`internal/rcaudio` — a session that transcribes through the
  Gemini transport). Both answer the reference's own LanguageServerService
  methods, and both are **not** the dashboard's `RemoteAgentTerminal*`
  contract.

---

## Load-bearing invariants

The facts that cost real time to rediscover. Each one breaks something
subtle if it is violated.

**The tree**

- **`internal/` is five tiers**, and **69 of its top-level packages were
  shells** — a package whose whole surface was aliases and forwarding calls
  into one other package. **All 69 are retired** (cycles 376 and 377): the
  shells are gone, every importer names the tier package directly, and the
  old paths (`internal/accounts`, `internal/agystep`, `internal/toolrunner`,
  `internal/interaction`, `internal/rcterminal`) no longer exist. The map is
  [`docs/guide/architecture-tiers.md`](docs/guide/architecture-tiers.md).
  A new package goes into its tier and gets **no** shell.
- **The tiers are numbered by dependency order** (cycle 378):
  `core (1) < features (2) < infra (3) < app (4) < ui (5)`. A package in
  tier *N* may import tiers 1..*N* and nothing above. The measurement says
  **zero upward edges** and **zero import cycles** across the 167
  production packages, and the test-only graph has none either.
- **The lower four tiers import no host.** `internal/core`,
  `internal/features`, `internal/infra` and `internal/ui` never import
  `internal/agent`, `internal/pigrt`, `internal/piagentbridge`,
  `internal/modular` or `internal/cli`. **Only the app tier may use the
  turn engines**, and the hosts sit above every tier. The `ui` tier may
  read the `app` tier (a view renders what the app produced); the `app`
  tier never reads the `ui` tier.
- **Every package at the top level of `internal/` is a host, an entry
  surface, or evidence** (cycle 378). A package that is none of the three
  is in the wrong place — put it in its tier.
- **A fact belongs to the tier that owns it, not to the host that happened
  to hold it.** When a lower tier needs a fact that lives in `internal/cli`,
  the fact moves down and the host keeps at most a thin re-export. The
  reference's version is `internal/core/version`; the model catalogue, its
  gates and the Claude-5.5 gate are `internal/features/models/catalog`;
  the marketplace link is `internal/features/plugins/marketplace`;
  `UsageLogo` is `internal/ui/tuilib`; `CountLabel` is
  `internal/core/plural`. The catalogue cannot import the CLI host — that
  would be a cycle.

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
12. A **failed tool call travels under `response.error`**; only a call that
    succeeded travels under `response.output` (agy 1.4.8). The body is the
    same string either way — the reference's own failure text. Read the
    signal with `tools.IsFailureText`, never by matching on the Go error.
13. On macOS the live token is in the **keychain**, not the on-disk copies.

**The workspace**

14. **Never mix addresses between the two RE environments** (macOS Mach-O vs
    the Linux/ELF sandbox). See
    [`docs/guide/environment.md`](docs/guide/environment.md).
15. **Every address moves on every `agy` release.** Regenerate the seeds
    before citing one.
16. The proxy's env vars and on-disk paths (`OPENAGY_WRAPPER_*`,
    `~/.openagy/wrapper.log`, `~/.openagy/captures/`) keep their names
    **on purpose** — renaming them silently moves an existing capture
    directory. The one that went is `wrapper.json`: the proxy's state is
    the `"wrapper"` section of the configuration document now.
17. `internal/infra/agyproxy` may import `internal/features/models` (read-only) and
    `internal/infra/proxy/bin`, but **not** `internal/cli`.
18. **chroma stays at v2.27.0** — the reference embeds upstream v2.27.0's 353
    files byte for byte. v2.24.1 cannot highlight Arturo, Gemfile.lock,
    LilyPond or Templ, and the bump changes no rendered byte, so only
    `internal/ui/mdrender/chroma_reference_test.go` catches a downgrade.

**The `localharness` wire**

19. The stdio length prefix is **4 bytes, little-endian**. A big-endian
    prefix desynchronises the stream permanently.
20. The first websocket frame is a **standalone `InitializeConversationEvent`**,
    not wrapped in an `InputEvent` — that message's `oneof` has no member
    for it.
21. The `x-goog-api-key` header carries the key from the `OutputConfig`; the
    upgrade is refused without it, **before** it becomes a websocket.
22. **stdout is the protocol channel** on the harness side: the
    `OutputConfig` frame and nothing else. A stray newline is read as the
    first byte of a length prefix.
23. A correlation slot must be **reserved before its request is written** —
    the client may answer on the very next frame.
24. A hook verdict the harness **cannot interpret must never be treated as
    "allow"**. The reference's own string — `"Unrecognized pre-tool hook
    decision %q; blocked execution for safety."` — pins this.

**The remote control's dialects**

25. **The IDE dialect wins over the alias table.** `DeleteAgentMessage` is
    two different RPCs: the Web UI's queued card sends `{messageId,
    recipient}` (a *pending* message), the reference CLI's own gRPC surface
    sends `{conversation_id, message_id}` (a *stored* one). The IDE's own
    tables are consulted **first** — the alias table
    (`internal/remotecontrol/reference_surface.go`) is only the fallback.
26. **The queue is dropped by id, never by index.** The index moves when
    the queue grows or shrinks; the id is the one the
    `pendingAgentMessages` update carries and the one the app hands back.
27. **The transport masks an *unnamed* failure as HTTP 200 `{}`** on the
    Connect dialect. A failure that **names** a code answers the
    reference's way: `-32018` / `-32019` → `not_found` / 404, `-32602` →
    `invalid_argument` / 400, body `{"code":…,"message":…}`
    (`remotetransport/connect_errors.go`). Everything else keeps
    the catch-all — the page calls methods the clone does not implement,
    and those must not take the page down. A curl that does not name
    `application/grpc-web+json` sees a masked failure as an empty body;
    the gRPC-Web dialect carries it in the `Grpc-Status` header.
28. **The reference mode's page must run on the WebSocket transport.** The
    reference's bundle picks the transport out of the URL and defaults to
    fetch, where six long-lived streams take Chrome's six connections per
    host and the turn's own calls queue forever — the page sits on
    "Working". The asset proxy therefore answers the document with a 302 to
    `useWebSocket=true` (`internal/infra/refbackend/transport.go`); the
    reference's bytes stay untouched.
29. **The Mendel flag set is decided by the identity of the read.** The
    backend serves a *different* flag set per product: `antigravity/hub/…`
    gets the desktop app's features (voice input, the jetbox terminal, the
    aux pane redesign), `antigravity/cli/…` and `antigravity/ide/…` get
    them off. Only the product token in the `User-Agent` decides — not the
    version, the build number, the body or the endpoint. The clone's own
    read sends the CLI's identity, so its page is the CLI's page;
    `pigasus web --desktop` switches that one read to the hub's identity
    and the desktop UI appears. The `remote-control` family has the same
    switch — `remote-control serve --desktop`,
    `remote-control run --desktop` and
    `remote-control start --desktop` (the plist carries it into the
    daemon) — on the same rule. The cache is **per identity**
    (`internal/remotecontrol/mendel`) — one shared cache hands the
    CLI's set to the desktop page.
30. **The Web UI's terminal and voice are their own surfaces.** The LS
    table is consulted **before** the alias table, so `CreateTerminal`
    and its four siblings reach the Web UI's own handlers
    (`internal/remotecontrol/ls_terminals.go`) and not the dashboard's
    `RemoteAgentTerminal*` contract — the alias table's
    `CreateTerminal → CreateRemoteAgentTerminal` is the fallback, not the
    route. The terminal is a live shell on a pty (`rcterminal/`),
    the voice is a session that transcribes through the Gemini transport
    (`internal/rcaudio`) — the reference's own STT is local. The wire is
    canonical proto-JSON (the member name at the top level), not
    protobuf-es's `{case,value}` envelope.
31. **The printed link goes through Google's account chooser.** The cloud's
    instance rows are **per Google account** and the dashboard's `/u/<N>/`
    segment selects one, so the bare `/r/<id>` link resolves against the
    browser's *default* account and answers "No instance found". Every link
    the clone prints is therefore
    `https://accounts.google.com/AccountChooser?Email=<addr>&continue=<deep link>`
    (`internal/infra/remote/link`, the chooser is the named endpoint
    `account_chooser`). The **deep link** is the reference's own; the
    chooser around it is the clone's own and does **not** match the
    reference's bytes. A URL shown in the TUI is **never left on one
    line**: the renderer *clips* a line that is wider than the terminal,
    so the tail of a long URL disappears. It is cut into chunks, and
    every chunk carries the **complete** URL as its OSC 8 target
    (`urlchunk/`, shared with the add-account screen). The
    transcript's selection joins two rows with the same hyperlink target
    without a newline, so copying the chunks yields the URL.
32. **The printed link must name the id the tunnel registered.** The
    installation uuid is persisted in
    `~/.openagy/remote-control-instance-id`; a configuration
    directory that cannot be written would mint a **fresh** uuid on every
    call, and the daemon would register one id while the link it printed
    named another — the dashboard answers "No instance found" for a link
    the daemon itself printed. So the uuid is **memoised per process**
    (`persistentrc.InstallationUUID`) and the daemon's id is threaded into
    the printer (`rchost.runServeEventLoop`). On macOS the starter and the
    daemon are **separate processes**, so the id is resolved *and
    persisted* before the daemon is bootstrapped
    (`persistentrc.EnsureInstanceID`) — and that file holds the **uuid**,
    not the full id: the reader rejects anything that is not a uuid.
33. **There is no launchd off macOS, so the daemon runs in the
    foreground.** `remote-control start` registers a launchd agent on
    macOS; on every other platform it runs the daemon in the foreground
    (the same stack as `remote-control serve`, through the `serveHandler`
    bridge) instead of failing with "launchd-backed mode requires
    macOS". The stderr note says so; the stdout sentinel
    `Remote-control daemon stopped.` is shared with `remote-control stop`.
    The `start` **usage block stays the reference's**, byte for byte.
    `remote-control run` is the **platform-independent** spelling — the
    foreground daemon on every platform — and `serve`, `run` and the
    non-darwin `start` share one implementation
    (`cli.runRCForegroundDaemon`); only the label differs. `run` is
    **hidden**: the reference's `remote-control --help` lists start,
    status and stop only, and that block is the reference's.

**The 1.4.10 alignment (cycle 363)**

34. **The memory trim is armed in the interactive TUI only.** The
    reference returns ~100 MB of start-up memory to the OS 15 s after the
    *interactive UI* opens; print and headless are explicitly unaffected
    (`internal/infra/memtrim`, armed in `internal/cliapp/tui_entry.go`).
    Arming it elsewhere is a behaviour the reference does not have.
35. **The resume block's bytes must not change.** The 1.4.10 split
    (`compaction.CarriedBlock` + `compaction.ResumingBlock`) is a
    *refactor*: the bytes the model sees after a compaction are
    byte-for-byte what they were. The carried tokens are added to the
    *after* figure only — the `(N summary)` label stays the summary
    alone.
36. **The media type follows the bytes, never the file name.** A JPEG
    saved as a PNG must travel as `image/jpeg`; `imagery.Prepare` returns
    the format it produced and every attachment path sends *that*
    (`internal/features/tools/clipboard/media.go`,
    `internal/features/tools/clipboard/paste.go`,
    `internal/remotecontrol/message_image.go`). The cap is 2000 px per
    side (`imagery.MaxSide`).
37. **A subagent's parent context lives in the child's own session.** The
    parent's task and id are the *oldest* messages of the subagent's
    conversation, so a summary drops them first — the clone keeps the
    parent's half itself (`internal/app/subagenttools/parentcontext.go`) and
    installs it through PiG's `session_before_compact` hook. The hook's
    extension is loaded into the **subagent's** session with the
    **shared** `*extension.ExtensionRuntime` (invariant 2) — a runtime
    the session's runner does not use never fires the handler.
38. **There is no scrollback yet.** bubbletea v2's renderer has no notion
    of one (the string does not appear in `cursed_renderer.go`), so the
    clone redraws the whole conversation every frame; the 1.4.10
    streaming scrollback is **open** — it needs a renderer the clone
    owns, not a bubbletea setting.

## The 1.4.11 alignment (cycle 364)

39. **`finished_reason` is the envelope's last member, and it is omitted
    on a normal finish.** The field is `json:"finished_reason,omitempty"`
    on `printmode.jsonEnvelope`; it is never an empty string. Its three
    values are `print_timeout`, `interrupted` and `error` — the
    reference's own words.
40. **The interrupt has to be caught.** An uncaught `SIGINT` kills the
    process, and a killed process writes no envelope, so the
    `interrupted` reason would be unreachable. The guard
    (`internal/printmode/print_interrupt.go`) cancels the turn's context
    on the first `Ctrl+C` and restores the default disposition with
    `signal.Reset` **before** the cancel — the second `Ctrl+C` stays
    fatal.
41. **A shelf shows six plugins and a `⋯ N more — enter to show` row.**
    `Ctrl+E` expands or collapses the shelf under the cursor and `[`/`]`
    jump between shelves. `Ctrl+E` is matched as
    `key.Code == 'e' && key.Mod.Contains(tea.ModCtrl)` — Bubble Tea
    v2 v2.1.0 has no `tea.KeyCtrlE`.
42. **The import's report lines go to stdout; only the `[error]` line
    goes to stderr.** A discovery failure also goes to stderr and still
    exits **0**; an unknown source exits **1**. `Staged to <dir>` is
    printed **per source**, so the bare `agy plugin import` prints it
    twice when both sources find something.
43. **The import's components are recorded in two different orders.** The
    report prints `skills, agents, commands, mcpServers`; the
    `import_manifest.json` stores `skills, agents, mcpServers,
    commands`. Both are the reference's own — do not "tidy" one into
    the other.
44. **`${CLAUDE_PLUGIN_ROOT}` expands to the staged plugin directory**,
    not to the Claude Code directory the plugin came from. A staged
    `plugin.json` carries only `name`, `version`, `description` and
    `author` (the author's *name*).
45. **A `project` or `local` scoped Claude record is skipped silently** —
    no line at all, not a `[skip]` line — and the "already imported"
    skip is keyed on the **(name, source)** pair, decided by
    `import_manifest.json` and not by the staged directory's existence.

---

## The plugin panels and the instance rename (cycle 367)

46. **A known member with the wrong JSON type fails the Web UI's
    decode.** The Web UI decodes with the reference's own descriptor
    and `ignoreUnknownFields: true` — an unknown member is fine, but a
    *known* member with the wrong type fails the whole decode, and the
    panel renders an error instead of the list. Every plugin answer
    goes through the reference's own codec in a round-trip test
    (`internal/remotecontrol/plugins_ls_test.go`).
47. **The rename must not drop the tunnel.** The reference renames a
    registered instance over the API (`{proxyURL}/v1internal:updateInstance`)
    with the session up; a reconnect would restore the **old** name,
    because the re-registration carries the name the client holds — so
    a successful rename also moves `Client`'s own name. The source of
    truth is the configuration file
    (`userSettings.cliRemoteControlHostname`), and the watcher reads the
    **configured** name, not the resolved one: the resolved name falls
    back to the host name, which would look like a rename nobody asked
    for.
48. **Every answer member must be a member the descriptor knows, and
    every enum value must be a value the enum *names*.** An unknown
    member is dropped by the Web UI's decoder without an error, and an
    unknown **enum name** takes the whole list with it — protojson
    rejects the message that carries it. The wire carries
    `REFRESH_CUSTOMIZATION_TYPE_PLUGIN`, never `PLUGIN` (the bundle's
    own enum is numeric, which is why the short name looks right in the
    source and is wrong on the wire). The audit runs over the **whole**
    service — every method of the 1.4.8 fixture against every answer
    (`internal/remotecontrol/ls_schema_audit_test.go`). The language
    server's contracts and the clone's legacy dashboard contracts are
    **not** each other's: an alias between them drops every member.

---

## The capture's size ceiling (cycle 369)

49. **The capture directory is held to a size ceiling.** The default is
    **1 GiB** (`DefaultCaptureMaxBytes`), the **oldest** files go, and `0`
    means no ceiling. The capture grew without a bound before this: two
    weeks of use left 14 GiB of it on the machine, every file carrying an
    unredacted OAuth bearer token. The log file has been capped at 4 MiB
    since cycle 274; the capture had no equivalent.
50. **The rotation only removes the files the proxy wrote.** The name rule
    (`<epochmillis>-<seq>-<method>.json`) is shared by the rotation and by
    `pigasus proxy capture purge`, so a capture directory pointed at
    something else keeps its other files — and those files do not count
    towards the ceiling.
51. **Pruning never happens at construction.** `newCapture` only starts the
    goroutine; the first *write* prunes. `pigasus proxy doctor` builds a
    `Server`, so it stays read-only with respect to the user's files. The
    pass is amortised — one directory scan per 64 MiB written, never one per
    record — and it runs on the capture goroutine, not the response path.
52. **`0` means no ceiling at every layer** (the environment, the file's
    `capture_max_bytes`, the `Config` pointer). The default applies only
    when the setting is **absent**; the file member is a pointer for
    exactly that reason.

---

## The one configuration home (cycle 370)

53. **There is one configuration home: `~/.openagy`.** The retired
    `~/.config/openagy` was moved into it by hand; the code does not
    read it and does not migrate it. `configdir` is the resolver —
    `$OPENAGY_CONFIG_HOME` first, `$HOME/.openagy` second — and every
    path resolver goes through it (`endpoints`, `internal/features/models`,
    `internal/features/accounts`, `internal/infra/remote/persistent`,
    `internal/infra/agyproxy`).
54. **The proxy's state is the `"wrapper"` section of `config.json`.**
    There is no `wrapper.json` and no `OPENAGY_WRAPPER_CONFIG`: the
    section's shape belongs to `internal/infra/agyproxy`, so `endpoints`
    carries it **verbatim** (`json.RawMessage`) and owns the read and
    the write. The section's owner still owns the shape.
55. **`XDG_CONFIG_HOME` does not apply to the clone's own files.** The
    retired home was XDG-shaped by accident; the clone's own
    configuration is not an XDG application, so it does not follow
    `XDG_CONFIG_HOME`.
56. **A test must not reach the user's own home.** `internal/infra/agyproxy`
    points the configuration home at a scratch directory for the whole
    package (`TestMain`): the proxy's state is read and written through
    that home, so a test without the override would edit the user's
    `config.json`. The same rule covers the **launchd daemon**: a launchd
    label is global to the login session, so registering one from a
    temporary home *replaces* the user's daemon — it stops the running job
    and points its log into a directory that is deleted with the test.
    `persistentrc.StartOfficialDaemon` refuses a plist path inside the
    temporary directory (`*TemporaryHomeError`, symlinks resolved on both
    sides), and `internal/cliapp`'s `TestMain` stubs the call so the suite
    never asks launchd for anything.

---

## The client/server split (cycle 374)

57. **A turn ends with exactly one `result` frame — a cancelled turn
    included.** The client waits for the terminator; a turn that ends
    without one leaves the client hanging. The terminator's write is
    therefore *bounded* (`agentturn.emitResult`), not cancelled.
58. **The frames are written with the connection's context, not the
    turn's.** A cancelled turn still owes the client its terminator, and
    a write on the cancelled context fails exactly when it is needed
    (`agentserver.runTurn`).
59. **The remote engine's reader goroutine lives for the whole
    session.** A read interrupted by a turn's cancellation *closes* the
    websocket (`coder/websocket`), which would take the session's next
    turn with it; the cancel frame goes out on the still-open connection.
60. **The server demands a bearer token, checked before the websocket
    upgrade.** It runs shell commands against its own workspace, so an
    unauthenticated peer is a peer with the machine's shell. The default
    bind is the loopback.
61. **The transcript travels with the prompt.** The server keeps no
    conversation, and every connection gets its own conversation id.
62. **The local and the remote path render the same frames through the
    same collector** (`internal/cliapp/tui_stream_prompt.go`'s
    `turnCollector`). A difference between them is a bug in the
    collector, not in the paths.

---

## The tier placement (cycle 375)

63. **A host never imports another host.** Three hosts run a session — the
    interactive TUI (`internal/cliapp`), the headless print path
    (`internal/printmode`) and the agent server (`internal/app/agentserver`)
    — and none of them may import another: the server cannot import
    `internal/cliapp` (an import cycle) and it must not import
    `internal/printmode` either (an entry surface is *above* a tier
    package). What they share lives **below** them, in
    `internal/app/session/setup`: the permission policy, the terminal
    sandbox, the discovered subagents and the MCP wiring. The turn seam
    (`internal/app/agentturn`) and the host (`internal/app/agentserver`)
    are tier 2; the host is *not* tier 4, because it needs the seam.
64. **A new package is placed in a tier or it is entered in the ledger.**
    `docs/guide/architecture-tiers.md`'s "packages that did not move" table
    is the record of the justified exceptions; a package that is in
    neither place has not been placed at all.

---

## The facade retirement (cycle 376)

65. **A move that renames a package's qualifier does it by import
    alias**, not by rewriting the call sites. The 58 retired shells' names
    survive as explicit aliases (`import agystep "…/internal/app/prompt/step"`),
    so the diff stays inside the import block and a qualifier can never
    collide with a local identifier. The alternative — renaming
    `agystep.` to `step.` — shadowed a local variable in
    `internal/app/prompt/cumulative` and broke the build.
66. **A facade must be a pure alias.** One was not:
    `internal/plugins` re-exported `WatcherPtr = &core.WatcherPtr` — a
    *pointer* where the package exports the value — and the consumer
    stored through it. Removing the shell turned that into a copy of an
    `atomic.Pointer`; `go vet`'s copylocks check caught it, and the
    consumer now takes the address explicitly.
67. **A package with no importer is not a second implementation.**
    `internal/permissions/store` (762 lines) was the always-allow store,
    orphaned when `cmd/wagyu` was removed; the live one is
    `internal/remotecontrol/permissions/store_fs.go`. It was deleted.

---

## The shell retirement (cycle 377)

68. **A shell is a shape, not a filename.** Cycle 376 retired the 58
    shells named `facade.go` and *missed eleven more* that hid behind
    `branding.go`, `autocont.go`, `prompt.go`, `stream.go` and the
    `internal/modular/*` registration shims. The rule is now: a package
    whose whole surface is aliases (`type X = other.X`) and forwarding
    calls into one other package is a shell — whatever its file is
    called. Cycle 377 retired all eleven.
69. **A test that imports a shell is a test of the shell.** The only
    importers left for `internal/modular/{engines/stream,toolsets/native,
    toolsets/wasm}` were three tests that imported the shim rather than
    the package they were checking —
    `internal/app/engine/stream/stream_test.go`,
    `internal/app/tools/native/native_test.go` and
    `internal/app/tools/wasm/wasm_test.go`. The shells added a hop
    without adding a registration: the tier package's own `init()` is
    what registers, and `cmd/pigga` already imported the tier packages
    directly.
70. **A shell that forwards a *pointer* is not a shell either.** The
    `internal/plugins` case (invariant 66) is the same failure mode as
    the eleven: the re-export changed what the consumer held.

## The tool split (cycle 379)

71. **The contract is `internal/app/tools`; the catalogue is
    `internal/app/tools/surface`.** The six executors live in
    `shell`, `filesystem`, `browser`, `web` and `tasks`. The split
    **distributes** the public API: `internal/app/tools` re-exports
    nothing, because a facade is a shell (invariant 68) and the shells
    are retired.
72. **A test follows its subject.** A test that **dispatches** a tool by
    name exercises the catalogue and lives in `surface`; a test that
    **constructs** a tool directly lives with the capability that owns
    it. A capability's test may not import `surface` — `surface`
    imports every capability, and the reverse edge is a cycle.
73. **The reference's own bytes never take a qualifier.** The split
    rewrote the call sites by a regex once, and the regex also rewrote
    the *string literals*: the reference's `"Tool is running as a
    background task"` became `"tools.Tool is running as a background
    task"`. The launch acknowledgement is the reference's text, byte for
    byte — a qualifier inside it is a bug. After any rename pass, grep
    for `[A-Za-z_]\w*\.(tools|shell|filesystem|browser|web|tasks|surface)\.`
    and for a spurious `pkg.Symbol` inside a string literal.

## The remote-control split (cycle 380)

74. **`internal/remotecontrol` is a dispatcher and fourteen domain
    packages.** The root keeps the fan-out, the language-server table,
    the trajectory, the interaction host and the PiG-backed session; each
    domain package owns its own store, its own wire shapes, its own
    method names and its own vocabulary. The dispatcher tries the
    sixteen routers in the reference's order and answers `-32601` when
    none of them claims the method.
75. **The dispatcher keeps the fan-out; the domain keeps the table.**
    Every domain answers `Route(seams, method, params)
    (any, *rpc.ErrorBody, bool)` — the third value is false when the
    method is not the domain's, and the dispatcher tries the next one. A
    domain that owned the fan-out would own every other domain's
    method names too. The same rule holds for the **language-server
    table**: a domain may keep a table of its own inside it, and the
    dispatcher must not re-implement the domain's cases inline. The
    permissions domain does (`permissions.LSRoute`), and the host's
    dispatcher used to name its two method names in its own switch.
76. **A domain never sees the dispatcher.** Its dependencies arrive
    through a `Seams` struct the host assembles — the store, a
    `Broadcast` callback, a registry, a session directory. The domain
    packages are **stdlib-only**: the only clone imports they carry are
    `remotecontrol/rpc` (the envelope) and `internal/core/seam` (the
    interaction contract).
77. **The split renames; it never re-exports.** The moved symbols are
    renamed at their new home — `CascadeRegistry` → `cascades.Registry`,
    `MessageBackend` → `cascades.Backend`, `MessagePrompt` →
    `cascades.Prompt`, `NewCascade` → `cascades.New`. A facade is a
    shell (invariant 68), and the shells are retired.
78. **A configuration member's *name* is a contract with the operator.**
    The split changed the *types* of `Config.Cascades` and
    `Config.MessageBackend`, not their names. A rename that changes every
    call site's qualifier is a second, unrelated diff (invariant 73).
79. **The dependency between a domain and its host runs domain → host.**
    The cascade domain owns the turn and asks the *sink* whether it can
    install the interactive tools' host
    (`cascades.InteractionHostInstaller`), and the root implements it on
    `*lsTrajectorySink` / `*lsThoughtSink`. The first extraction left
    `lsInstallInteractionHost` as dead code and the Web UI's permission
    card would never have appeared again; two tests now pin the seam
    (`internal/remotecontrol/ls_interaction_test.go` and
    `internal/remotecontrol/cascades/cascade_test.go`).
80. **A duplicated fixture is cheaper than a cycle.** The root's
    `captureSink` and the root's `codeAsInt` / `sizeAsInt` /
    `jsonMarshal` / `jsonRPCFor` helpers could not cross the package
    boundary — sharing them would be an import cycle — so the domain
    packages carry their own copies.
81. **The root's *field* names keep their old names, and the guide's
    file tables follow the code.** The `remote-control.md` guide's "Where
    to look" table names the domain packages, not the files they came
    from; a table entry that names a path that no longer exists is a
    stale guide.

## The panel split (cycle 381)

82. **A panel owns its state, its rows and its keys; the host keeps
    facts.** A panel is a self-contained interactive surface: it lives at
    `internal/ui/views/<name>/` and owns its state, its rows, its keys
    and its writes. The host (`internal/cliapp/<name>_host.go`) supplies
    a `Seams` struct of callbacks, or a `State` of values, and routes
    the panel's keys and asks the panel for its rows. A host file that
    *behaves* like the panel it owns is the panel, wherever it sits.
83. **A panel is rebuilt on every open.** A panel that survives a close
    carries a stale cursor, a stale stage or a stale draft into the next
    open. The host builds the panel when the command opens it and drops
    it when the panel closes.
84. **A panel answers `handled=false` for the keys it does not own**, so
    Ctrl+C still reaches the global handlers. A panel that swallows
    every key takes the user out of the application.
85. **A list panel owns its cursor; a viewer panel does not own the
    offset.** The host's `m.panel.cursor` and `m.panel.window` are for
    the panels that do not own one — a cursor in two places is a cursor
    in one place by accident. The *viewer* panels are the exception:
    the host's window offset is shared with `/codesearch` and the
    `/tasks` detail views, so the viewer takes the offset as an
    argument and hands the clamped one back.
86. **A visible window follows its cursor.** The `/rewind` picker's
    window used to stay put, so the cursor could walk off the top of the
    visible rows. A cursor the user cannot see is not a cursor.
87. **The ui tier imports no host.** None of the panel packages names
    `internal/cliapp`, `internal/agent`, `internal/pigrt` or
    `internal/piagentbridge`. A panel that needs the host's facts
    takes them as a seam.
88. **A shell with no caller is dead weight, and a shell with a caller
    is still a shell.** A one-line forward into another package is a
    shell whether or not anything calls it (invariant 68); the call
    sites name the package that owns the fact. A scan for the shape —
    a one-line forward with no caller left in the tree — must find
    nothing.
89. **A panel with a sub-view owns *both* cursors, and a host global
    key that pre-empts a panel binding reaches the panel through an
    exported method.** The `/diff` panel has a list cursor and an
    inline-body cursor, so one shared offset cannot describe both. And
    the host's global Ctrl+O handler runs *before* the panel's own key
    handler, so the panel exposes `ToggleFoldAtCursor()` for the host
    to call — the host must not reach into the panel's fold map. The
    host's fact (Ctrl+O folds the hunk and never mutates
    `m.expanded`) is pinned by `internal/cliapp/diff_host_test.go`; the
    panel's folding behaviour is pinned with the panel.
90. **A tick's message and the switch that handles it are one fact.**
    A tick command that produces a message the host's update switch
    does not name is a spinner that never moves, and nothing else in
    the tree notices: the command returns, the host drops the message,
    and the row sits on its first frame. The compaction spinner's tick
    was broken that way for two years — commit `64e71d67` aliased the
    `/usage` panel's tick message and left the compaction spinner's
    alone, so the command produced `tuiviewer.CompactSpinnerTickMsg`
    while the switch named the host's own `compactSpinnerTickMsg`. The
    message and the command that arms it therefore live in one file
    (`internal/cliapp/tui_compact_spinner.go`), and the pin is end to
    end: the command's message advances the frame
    (`internal/cliapp/tui_compact_spinner_test.go`).
91. **A "generic" package that holds one owner's facts has no owner.**
    `internal/ui/foundations/viewer` held the changelog panel's
    excerpt, the `/usage` panel's countdown and tick and the diff
    panel's inline row — three panels' facts under a generic name. Each
    moved to the panel it belongs to; what is left in the package is the
    machinery all of them share (the document spec, the window
    arithmetic, the counter row). A fact belongs to the tier that owns
    it, not to the package that happened to be near it.
92. **The compiler does not catch an unused function.** Go complains
    about an unused import or an unused local variable, never about an
    unused function, so dead code accumulates silently in the biggest
    package — and a dead function still reads as a live seam, which is
    worse than no function at all. `internal/cliapp/deadcode_test.go`
    is the guard: it parses the package with `go/ast` (a regex is not
    trustworthy for this) and asserts that every unexported function
    the host declares is called from somewhere in the package. A
    declaration that is deliberately kept although nothing calls it
    carries a `deadcode: staged` marker in its doc comment, with the
    reason written next to it — the marker is a way to record a
    decision, not a way to hide dead code.

## The tree-wide dead-export sweep (cycle 381)

93. **The guard covers the unexported surface; the sweep covers the
    exported one.** An exported function may be called from another
    package, so the host-local guard says nothing about it. The sweep
    resolves every `pkgpath.Symbol` through the import map with
    `go/parser` — a name counts as live only when a call site really
    names it. A **bare-name `rg` sweep is not trusted on its own**: it
    has false positives (a name inside a string) and false negatives
    (a truncated `head` hid a live use of `printSlashSkillInfos`
    during cycle 381). Re-grep a name with a generous limit before
    deleting it.
94. **A dead export is deleted unless it carries reference
    provenance.** A recovered reference string, a recovered behaviour
    or a guide-protected endpoint alias is **kept**, and the reason is
    recorded in the cycle report — not left in a comment the next
    reader has to interpret. A dead export with no provenance is
    **deleted**. A dead export whose literal duplicate already exists
    is **wired**: the call site names the symbol instead of repeating
    the literal, byte for byte.
95. **The public top-level packages are not swept.** `palette`,
    `grpc`, `agentlink`, `browserbridge`, `lifecyclehooks`, `budget`,
    `harnessprompt`, `remotetransport`, `rctoken`, `originpolicy`,
    `endpoints`, `configdir`, `accountpool` and the rest are a
    **compatibility promise** — an export nobody calls today is still
    a third party's API. The evidence packages (`internal/cortex`,
    `internal/language_server`, `internal/planrender`), the generated
    `*_pb` packages and `scratch/` (which has never compiled) are out
    of scope for the same reason.
96. **A removed half leaves a record, not a hole.** The
    `internal/rctunnel` supervisor half was orphaned by earlier
    cycles and went with the sweep; the `out/` evidence and the cycle
    report keep the finding. A symbol that carries reference
    provenance is never deleted to make a number look better — the
    clone does not implement the v1.4.0 resume hint, the no-tool-call
    nudge or the default-mode shift+tab hint, and the record says so.

## The forwarder retirement (cycle 381)

97. **A forwarder with a caller is still a shell.** The dead-export
    sweep finds what *nobody calls*; a one-line forward into another
    package is a shell whether or not anything calls it (invariant 68).
    The host's last 70 went in cycle 381's 34th tranche, and the
    call sites now name the package that owns the fact.
98. **A type alias to another package's type is a shell too.**
    `type X = otherpkg.X` is the same shape as a forwarding function
    and goes the same way — `EligibilityError` became
    `eligibility.Error`.
99. **A retired forwarder's doc facts move down, or they die with the
    shell.** A forwarder's doc comment is sometimes the only place a
    fact was written down. A sentence that is only true of the shell
    dies with it; a sentence about the behaviour moves to the package
    that owns the behaviour; a sentence that turns out to be **false**
    is dropped, not relocated. The check is a prompt for a decision,
    not a verdict — it compares word by word and flags a target that
    carries the same fact in other words.

## The subagent-question picker (cycle 381, tranche 35)

100. **A panel's staged answer outlives its overlay, so that panel is
     not rebuilt on every open.** The picker stages a question, the
     answer is typed into the *composer's* prompt and submitted there,
     so a panel rebuilt on open would take the staged answer with it.
     `internal/ui/views/subagentquestionpanel` is built once with the
     session; its cursors are still reset when the picker opens. The
     host keeps the `*subagentquestions.Manager` (the tool seam
     enqueues into the same store) and supplies three seams: where a
     row is echoed, what the composer holds, and how it is cleared.
101. **A modal overlay claims every key while it is up.** The picker
     swallows the keys under it — including Ctrl+C — which is what the
     reference's own overlay did. The host's key pipeline asks the
     panel (`HandleKey`) and the panel answers whether it took the key;
     a closed picker takes none.
102. **A panel's rendered bytes are pinned, not eyeballed.** The
     picker's rows, its notices and its two confirmations are asserted
     byte for byte against the host's own output
     (`internal/ui/views/subagentquestionpanel/panel_bytes_test.go`).
     The comparison caught a colour segment written with the *closing*
     dim code where the reference writes the *opening* one — the text
     was the same and the colour was not.

## The /help overlay (cycle 381, tranche 36)

103. **The /help overlay is a panel, and it is modal.**
     `internal/ui/views/helppanel` owns the tab, the list cursor, the
     window, the rows and the keys. The host keeps the facts the panel
     cannot know — the model, the tier, the cwd, the email, the
     conversation id, the terminal's size, whether vim mode is on — as
     seams, and its `/help` command and its `?` keybinding only open
     it. While the overlay is up it claims every key under it, the same
     exception class as the picker (invariant 101).
104. **The overlay that owns a surface writes its own exit line.** The
     "/help" block and the "Exited /help command" line are the panel's
     (`Cancel()`), so the host's Esc path and the panel's own "q"
     produce the same bytes. The host keeps no echo of its own.
105. **A panel reads the palette directly; the host keeps no colour
     alias for it.** The host used to re-read the overlay's colours into
     its own aliases whenever the scheme changed — a second copy of a
     fact `tuilib` already owns, and `tuilib` refreshes its own aliases
     on the same event. A copy of a colour goes stale when a panel is
     added.
106. **An overlay's open resets its window and its cursor.** The
     reference keeps no per-tab scroll position, and it keeps none
     across an open either.

---

## The slash-command popup (cycle 381, tranche 37)

107. **The slash-command popup is a panel, and it is not modal.**
     `internal/ui/views/slashpicker` owns the filtered rows, the
     highlight, the scroll window, the on-screen rectangle, the rows it
     writes, the keys it handles (Up, Down, Tab, Esc) and the mouse
     events it consumes. The host keeps the facts the panel cannot know
     — whether the alternate screen is up, the terminal's width, which
     screen row is the composer and what the composer holds — as seams.
     It is **not** modal: it is a completion popup over a *live*
     composer, so the keys it does not own (the letters the user types)
     reach the composer. A modal overlay claims every key under it
     (invariant 101); this one answers `handled=false` for everything
     but its own keys.
108. **A catalogue and the view over it are different owners.**
     `tuilib.SlashCommand`, `DefaultSlashCommands`,
     `FilterSlashCommands` and `containsFold` are the catalogue — the
     list of what the reference offers. The popup is the view over it,
     and the view is the half that moves: `tuilib.PickerState`,
     `tuilib.PickerKind`, `tuilib.ModelRow`, `tuilib.readToken`,
     `tuilib.PickerLayout` and `tuilib.PickerGeom` left the catalogue
     with it.
109. **A panel that keeps its own copy of a colour refreshes it.** The
     popup's four colours (`nameHi`, `descDim`, `labelDim`, `keyOpen`)
     are the palette's, so the panel registers its own
     `palette.OnSchemeChange` callback: an alias copied once keeps the
     old scheme's escape. The host's `pickerNameHi` / `pickerDescDim` /
     `pickerLabelDim` / `pickerKeyOpen` aliases went with the panel.
110. **The popup's rectangle is the panel's.** The host tells the panel
     where its rows start (`SetBodyStart`) and how many rows the body
     lost to the clip (`ClipBody`); the panel answers the rectangle, and
     the mouse handlers map an (x, y) onto a row with it. The host keeps
     no geometry of its own.

---

## The mention dropdown (cycle 381, tranche 38)

111. **A widget's facts stay in the tier that owns them; the widget
     moves to the tier that renders it.** The "@" mention grammar
     (`Parse`) and the workspace listing (`List`, `FilterEntries`)
     stayed in `internal/app/prompt/mentions/files` — the app tier;
     the dropdown that renders them is
     `internal/ui/views/mentionpicker`, in the ui tier. A panel may
     import the tier below it, and fourteen ui-tier files already do.
112. **A completion popup that holds no candidate still swallows its
     completion key.** The mention dropdown's Enter completes the
     mention; when there is nothing to complete, the Enter is swallowed
     anyway and the prompt is **not** submitted. A popup that falls
     through to the host's submit on an empty list is a different
     popup.
113. **A panel's rendered bytes are pinned where they do not depend on
     the machine.** The mention dropdown's listing carries the
     workspace's absolute path, so the golden pins the query mode and
     the placeholder listing (which carry no path) and the listing's
     first row at a width narrow enough for the path cell to hold the
     reference's "/..." truncation. A golden that depends on the length
     of the temporary directory is a golden that fails on another
     machine.

---

## The active-items block (cycle 381, tranche 39)

114. **The gate belongs to the surface it switches off.** "Show Active
     Tasks" (`hideActiveItems`) switches *the active-items block* off,
     so the block reads the setting: the panel's `Hidden()` is the
     host's old `isHideActiveItems()`. The host asks the block two
     separate questions — `Hidden()` (is the block switched off?) and
     `Count()` (how much is in flight?) — and the alt+j gesture's
     `Count() > 0` test stays **ungated**, exactly as it was: the
     gesture toggles the focus on a running task even when the block
     is hidden.
115. **A panel's host field is a pointer and every method of the
     panel is safe on a nil receiver.** `m.activeItems` was a value
     (`activeItemsModel`); it is a `*activeitemspanel.Panel` now. A
     dozen host tests build a `&runMode{}` by hand, and a panel that
     panicked on a nil receiver would take all of them down — so a nil
     panel answers "nothing is running" and writes nothing.
116. **A move may drop a method that nothing called.** The
     active-items widget's `SelectLast` had no caller; as a method of
     an exported type it would have been a dead export (invariant 94),
     so it went with the widget instead of travelling to the new
     home.
117. **The block's bytes are pinned by a two-tree dump, and the dump
     has to defeat the clock.** The rows carry a time cell
     ("[15:04:05]") and the subagent row an elapsed cell (" · 15s"),
     so the dump masks both. It also gives every record a **distinct**
     start time: the registry reads a map and sorts with an unstable
     sort, so two records that started in the same second come out in
     an arbitrary order — the pre-tranche tree produces a different
     order on two consecutive runs of the same fixture.

118. **A view takes a `State`; only a surface that acts takes a
     `Seams`.** The status row has no keys and no writes of its own —
     the host asks it for a line — so it draws from a `State` of
     values. A `Seams` of callbacks for a view that never calls one is
     a shell in a struct. The row is `internal/ui/views/statusrow`; the
     host's facts reach it through `internal/cliapp/statusrow_host.go`.
119. **An entry surface is above tier 5, so a fact the ui tier needs
     cannot live there.** `internal/printmode` is an entry surface, and
     the ui tier may not import it. The token formatter ("184 K" /
     "1.2 M") therefore lives in `internal/core/tokenfmt` (tier 1),
     below everyone. The old `printmode.TokenK` is **gone**, not left
     behind as an alias — a one-line forward into another package is a
     shell (invariant 68).
120. **A number has one owner.** The status row's canonical width is the
     host's `separatorWidth`, handed in as `State.CanonicalWidth`; the
     row keeps no second copy of the constant. A copy of a number goes
     stale when the first one moves.
121. **The row's bytes are pinned by a two-tree dump, and the reference's
     own asymmetries are part of the pin.** The right-padding count
     **excludes** " · Queue mode" in the built-in case and
     **includes** it in the `--agent` case, and the static non-tty row
     keeps its own literal grey (`38;2;154;160;166`) rather than the
     palette's. Both look like mistakes and neither is one: the
     pre-tranche tree and this tree agree on every one of them over 24
     fixtures (15 465 bytes). The dump masks the compaction's elapsed
     cell (`(12s)`), because it carries the clock.

---

## The transcript's own vocabulary (cycle 381, tranches 41–44)

122. **A move renames; it never re-exports.** The transcript's four
     record types were the host's `turnBlock` / `userBlock` /
     `assistantBlock` / `liveStepRun`; they are now `transcript.Block` /
     `UserBlock` / `AssistantBlock` / `LiveStepRun`. The host keeps no
     `type turnBlock = transcript.Block` alias — a package-level alias
     into another package is a shell (invariant 68), whether or not
     anything calls it.

123. **A render cache has one owner.** The tool-step block carries a
     two-slot cache (the collapsed rows, the expanded rows, each keyed
     by the width and the verbosity it was painted at). The host used
     to reach into `b.cachedCol` / `b.cachedExp` and seed them itself;
     `internal/ui/transcript` owns the cache and exports `NewBlock`,
     which picks the slot from the block's own `Expanded`. A cache the
     host can stomp is a cache with two owners.

124. **A classifier and a row are different owners.** The turn error's
     message is classified in the host (`agent.TurnErrorMessage`) — it
     classifies the host's error type — and the row that prints it is
     a view (`toolsteps.ErrorRow`), beside the other step rows. The
     row owns the trim; the classifier owns the decision.

125. **A pure helper that reads host state must be split, not moved.**
     The composer's mouse-to-offset arithmetic was a method that read
     the value, the line count and the prompt's screen row. The
     arithmetic moved (`internal/ui/foundations/inputselection.OffsetAt`);
     the host kept a thin method that supplies the three facts. The
     same shape applies to the transcript's four geometry helpers
     (`internal/ui/foundations/viewport`), which were pure over their
     parameters and moved whole.

126. **A range normalisation travels with the function that consumes
     it.** The old host method took its selection range from the
     store, which normalises `start <= end`; a raw reversed range
     could never reach it. `inputselection.ApplyHighlight` normalises
     itself (swap when `start > end`), so the pure function and the
     old host behaviour agree.

127. **The reference's own quirks are part of the pin.** The move made
     two visible: `FindLineSpan` with a negative line returns the
     *last* line's span, and a selection that starts inside the value
     but ends past it is clamped — and because the highlight offsets by
     the prompt's two cells, a rendered row *without* the prompt gets
     the highlight pushed off its end. Neither is a defect the move
     introduced; both are pinned so a later change cannot "fix" them
     silently.

---

## The layering guard (cycle 381, tranche 46)

128. **The layering rule is checked by `go test`, not by a script.**
     `internal/core/architecture` (tier 1, stdlib only — it measures
     the tree rather than being part of it) asserts six rules: **R1**
     no upward edge, **R2** no tier imports a host, **R3** no ui-tier
     package imports a host, **R4** no production import cycle, **R5**
     no test-only import cycle, **R6** no public package imports an
     `internal/` package. A violation fails the suite; it no longer
     waits for a cycle report to run a script.

129. **A rule that does not hold as written is excused by name, and
     the exemption prints its size.** R2 as literally specified fails:
     20 tier-4-to-host edges exist. AGENTS.md says "only the app tier
     may use the turn engines", so the app tier is exempted for the
     five engine hosts, and the count is printed on every run rather
     than hidden in a list of 20 names.

130. **An exception list cannot rot silently.** The two findings that
     are excused — the test-only cycle between `remotetransport` and
     `internal/remotecontrol`, and `remotetransport`'s test-only
     import of `internal/remotecontrol` — carry a one-line reason, and
     an exception that no longer excuses anything is reported as
     stale.

131. **The in-tree measurement keeps the out-of-tree one honest.** The
     guard and `devutils/tiergraph.py` print the same two numbers for
     the `internal/` subtree (225 production packages, 204 test-only)
     and the same fifteen-row tier-to-tier edge table. The script's
     own host check was **dead code**: it tested the *imported*
     package's tier, and a host is untiered, so the branch never fired
     and it printed `(none)` while 19 tier-to-host edges existed.

132. **Invariant 63 is contradicted by the tree, and the guard says
     so.** "A host never imports another host" — but `internal/cliapp`
     imports `internal/printmode` in eight production files. The guard
     has no rule for it (a rule would fail immediately); the edge is
     recorded in the cycle report instead.

---

## The viewer documents, the bash block and the banner (cycle 381, tranches 48, 51, 52)

133. **A viewer document has its own home, and the host keeps the
     offset.** `/changelog`, `/feature-diff` and `/usage` each own their
     document, their visible-row count and their counter row. The
     *window* is the host's fact — `m.panel.window` is shared with
     `/codesearch` and the `/tasks` detail views — so the three
     functions take `(rows, start, end, total)` and render the window
     they are given.

134. **A document that is rebuilt on every frame is memoised where it
     lives.** The `/changelog` document is built from 180 KB of embedded
     markdown and the build ran on every TUI draw — 511 760 ns/op and
     about 150 KB of garbage per frame, even in the common "no source"
     case where the parse is thrown away. The parse is memoised with a
     `sync.Once` inside `internal/ui/views/changelog`; the bytes are
     unchanged.

135. **A block's colours are read at the use site.** The bash block's
     `bashBodyOKOpen` / `bashBodyFailOpen` were host variables refreshed
     by `renderer.go`. The package reads `palette.WarningBodyOpen` and
     `palette.ToolRemoveBodyOpen` directly — the palette's own variables
     are live — so both host variables and the two refresh lines are
     gone. A refresh alias is a second owner of a colour.

136. **A type alias into another package is a shell.**
     `type bashOutput = tools.SafeBuffer` was deleted with the move; the
     host names `*tools.SafeBuffer` directly and the tier-2 package
     never needed the buffer at all.

137. **A package that cannot import the helper takes the value
     instead.** `internal/ui/tuilib` imports `internal/ui/views/seasonal`,
     so the seasonal package may not import tuilib: the **host** shortens
     the working directory (`tuilib.ShortenCwd`) and passes the result in
     as `State.Cwd`. The CLI's name and the version travel in the state
     as well.

---

## The notice rows, the status line and the eligibility notice (cycle 381, tranches 47, 49, 53)

138. **A row's bytes are named by shape, not by purpose.** Ten places
     in the host wrote the same `⎿` notice, in **three** distinct
     shapes — the compact one, the indented one and the dim one. They
     moved into `internal/ui/rows` (tier 5) as `NoticeCompact`,
     `NoticeIndented` and `NoticeDim`. The same bytes serve different
     callers — the remote-control card's echo row and the slash
     command's echo row are the *same* indented shape — so a
     purpose-named function would be wrong for the other caller.

139. **A row's colours are read at the use site.** The package reads
     `palette.WarningBlueOpen`, `palette.SubtleOpen`,
     `palette.DimGrayOpen` and `palette.DimGrayClose` directly, so a
     scheme change is picked up without a refresh alias. The
     `"\x1b[39m"` close has no palette export, so the package declares
     it locally — the same precedent as the other ui-tier panels.

140. **A fact two tiers need belongs below both of them.**
     `FormatLogTS` — the log file's timestamp suffix — lived in
     `internal/ui/tuilib` and was used by tuilib's help panel *and*
     by the status-line renderer. `internal/ui/views/statusline` may
     not import tuilib (tuilib pulls in `internal/features/auth` and
     `internal/app/prompt/customcmd`), so the formatter moved down to
     a new tier-1 package, `internal/core/logts`.

141. **A wire's shape moves; the facts that fill it do not.**
     `buildStatusLinePayload` stayed in the host as the *collector*:
     it gathers what the host alone knows into one `PayloadState`
     literal and hands it to `statusline.PayloadFrom`. The JSON
     document's members and omit rules are the reference's wire
     contract and belong to the package; the session's token counts,
     the quota bucket and the email are the host's.

142. **A UI row does not belong in a features package.**
     `eligibility.RenderNoticeRow` was a row renderer in a tier-2
     package, which made `internal/features/auth/eligibility` depend
     on `palette` and `render` for one function. The row moved to
     `internal/ui/views/announce`; the eligibility package keeps the
     *facts* (the typed error, the `Reason`, the `VerificationURL`,
     the `HTTPStatus`, the print-mode `ELIGIBILITY_DENIED` envelope)
     and is now free of UI deps. The move **renames** — the old name
     is gone, not aliased.

143. **A catalogue's members are a contract and are not renumbered.**
     The announcement catalogue (`Catalog`, `VisibleCards`, the ids
     and the kinds) was not touched by the eligibility move: a
     renumbering would be a wire change, not a refactor.

---

## The voice surface, the history and the flags (cycle 381, tranches 54, 55, 56, 57)

144. **A state machine moves next to the views it decides
     between.** The dictation overlay is four phases — idle,
     connecting, recording, finishing — and every view (the banner,
     the timer, the failure notice) follows from the phase. The
     package that owned the views did not own the state machine
     that chose one, so the machine was in the host; it moved next
     to the views (`internal/ui/views/voice`).

145. **A surface's seams are the facts the host owns.** The voice
     surface answers "which phase are we in?"; it cannot answer "how
     is a transcription session established?" or "where does a
     confirmed draft go?". Those are the seams, and every one of
     them is optional so the package is usable from a test without a
     running TUI.

146. **A store stays with the host; the arithmetic over it moves.**
     The composer's selection *store* reads and writes host state
     (`m.store.InputSelStart/End`), so it stays; the *value*
     arithmetic over a range (`CursorPosition`, `DeleteRange`,
     `ReplaceRange`) is pure and moved into
     `internal/ui/foundations/inputselection`.

147. **A rule that two callers ask has one owner.** The
     blank-prompt rule was introduced for the prompt history, and the
     submit path asks the same question; the rule moved with the
     history as `history.IsBlank` rather than staying in two places.

148. **The registry owns the routing; the host owns the facts.** The
     registry knows which panel a command opens and what each panel's
     kind means; the row counts and the `/codesearch` handler are the
     host's and arrive through `internal/cliapp/slash_panel_wiring.go`.

149. **A flag's policy is a tier-1 fact; its registration stays with
     the parser.** `--project`, `--browser` and `--log-file` say what
     a value *means*, so their policies moved to
     `internal/core/cliflags/{projects,browser,logfile}`; the flag
     table, the defaults, the aliases and the help texts are the
     parser's own vocabulary (`cli.FlagSpec`) and stayed.

150. **A tier-1 policy that needs a tier-4 fact takes it as a
     seam.** A new project's id is a conversation id, and the
     generator lives in tier 4, so `projects.Apply` cannot call it —
     the host supplies it.

---

## The dead-surface sweep (cycle 381, tranche 45)

151. **A test file is not a caller.** The host-local dead-code guard
     counts a production declaration as live only when a **production**
     file names it. A function only the tests call is a function the
     product never runs — it reads as a live seam, and it is not one.

152. **Liveness is transitive.** A declaration whose every production
     caller is itself dead is dead as well: `statusRow` is called only
     by the tests, so the three status-line helpers it called went
     with it. The guard computes the greatest fixpoint over the call
     graph, not a one-hop check.

153. **An allowlist entry that no longer excuses anything is an
     error.** The map records a *decision* — a recovered reference
     behaviour, a seam a later cycle wires up — and a decision that
     has expired must not sit in the map quietly.
     `TestDeadCodeAllowlistIsMinimal` fails on a stale entry.

154. **An exported name nobody outside the package calls is
     unexported.** `NewRunMode`, `EnableTools`, `DefaultRouter` and
     `BtwScrollBy` had no caller outside `internal/cliapp`, so they
     became `newRunMode`, `enableTools`, `defaultRouter` and
     `btwScrollBy`. The host's exported surface is now eleven names.
     The BTW methods that carry **reference provenance**
     (`ShowBtwHistory`, `IsBtwActive`, `DismissBtwResponse`,
     `ToggleBtwExpanded`, `NavigateBtw`) keep theirs.

## The second wave (cycle 381, tranches 58, 59, 62, 64)

155. **A badge's bytes and the "is it on" fact have two owners, and
     the split is by question.** The three vim badge strings belong to
     the engine (`vim.Mode.Badge()` — the public package owns the
     reference's bytes); *whether vim is on* is the host's fact, so
     `internal/ui/editor/vimkeys.Badge(enabled, mode)` takes it as a
     parameter. The host's collector must **not** build the engine to
     ask it a question — the engine must not exist when vim is off.

156. **The transcript's buffer stays the host's; the ui tier owns the
     mechanics.** `m.response` is read from twenty-odd files the
     transcript package does not own, so a type that *owned* the buffer
     would have put the whole host behind a constructor.
     `transcript.Document` is a **value**: the host builds it from its
     slices, hands it over and takes it back. The slice headers stay
     the host's, and `ReplaceRange` mutates the caller's backing array
     exactly as the host method did — a caller passes a fresh slice.

157. **A tier that needs a host's emptiness rule receives it as a
     value.** The turn's emptiness rule stays in the host
     (`agentturn.IsEmptyAssistantTurn`) and travels into the ui tier as
     `Options.EmptyTurn`, so `internal/ui/transcript` never imports
     `internal/app/agentturn` and through it never imports the engine
     host.

158. **A colour is read at the use site, and the palette's value is
     the owner's.** The host's `cDimGrayOpen` was a direct assignment
     from `palette.DimGrayOpen` (`renderer.go:119`); the moved row code
     reads the palette directly. A refresh alias is a second owner of a
     colour, and a colour with two owners goes stale.

159. **Two surfaces that look alike are two owners.** The `/model`
     *popup* filters the live catalogue by substring over the label or
     the id and resolves an argument by a full, case-insensitive match;
     the `/model` *panel* is a static row list over the reasoning-tier
     and effort models, matched by subsequence over the reference's
     display labels. Different row source, different match rule — so
     `internal/ui/views/modelargpopup` owns the resolution and
     `internal/ui/views/modelpicker` owns the panel.

## The second wave's seams (cycle 381, tranches 61, 65)

160. **A registry that caches a snapshot must keep the snapshot
     lossless.** The quota snapshot dropped the bucket's wire id, and
     the status line keys its quota map by that id — so a quota read
     back from the registry's cache rendered `"quota":null` instead of
     `"quota":{"3p-5h":…}`. A cache that loses a field is a second
     owner of a lossy conversion, and the loss shows up in the wire.

161. **A freshness policy belongs next to the thing it governs.**
     `accounts.QuotaFreshness` (two minutes) sits next to
     `Account.Quota`, and `accounts.QuotaTimeout` (45 seconds) sits
     next to `FetchQuota`. A host that keeps its own copy of the rule
     is a second owner of it — and the host's copy was in a
     *different representation* from the registry's, which is exactly
     why the duplication survived so long.

162. **A seam's callbacks are required; its widgets are nil-safe.**
     The composer's `Picker`, `Files` and `History` are nil when the
     panel was never opened, and the ui tier must not panic on them;
     the `Seams` callbacks are required, and a nil one is a
     programming error the doc comment names.

163. **The OS write stays host-side.** A ui-tier package may *decide*
     that a selection is copied and hand the text over; the host
     performs `clipboard.Write`. A ui package that wrote to the system
     clipboard would be doing I/O the host owns.

164. **A widget crosses a seam as an interface, and the host keeps
     ownership of it.** The composer's `Editor` interface and its
     `TextArea` adapter write *through* to the host's textarea; the ui
     tier never holds the widget itself.

165. **Two key handlers with different tails are not one handler.**
     The Enter key's alt/shift branch dismisses the mode hint and the
     ctrl+j branch does not; a split that duplicated the composer half
     would be a second owner of that rule.

## The third wave (cycle 381, tranches 69, 75, 76, 77)

166. **The policy of a mutating operation belongs to the layer that
     owns the state it mutates.** The rewind truncates the live
     in-memory session, and the session layer owns that slice — so the
     policy lives in `internal/app/session/rewind`. A ui package that
     mutates a slice it does not own cannot write the result back,
     and that is not an implementation detail: it is the bug.

167. **A panel that names a thing does not own the thing's
     mutation.** The rewind panel's job is to name the picked step;
     the session's job is to truncate. The panel keeps the cursor, the
     window and the keys; the policy keeps the count and the echo.

168. **A second copy of a path shape is a second owner of the fact.**
     The CLI's log file's name was assembled in three places;
     `confhome.CLILogPath(ts)` is now the one owner.

169. **An inlined shell keeps its guards.** The empty-model check in
     the context-size expression and the nil-task-reg check are
     load-bearing, not boilerplate: `modelctx.ContextWindowFor("")`
     returns the *default* window, not zero.

170. **A type that a ui panel needs must live below the ui tier.** The
     markdown-agent catalogue was in an engine host, so the panel
     carried a second copy of `Agent` and the host mapped one into the
     other field by field. The duplication was not laziness — the host
     was the only reachable home for the type. Moving the owner below
     the tier removes the copy without a forwarder: the panel names
     `agents.Agent` directly, and `internal/cliapp`'s production files
     importing `internal/printmode` went from nine to eight.

171. **The store that owns a record owns its assembly.** The
     conversation store owns the trajectory table, so it owns the steps
     the table is built from. The host holds the four facts — they are
     the live state of a running session — and hands them over; the
     store decides what they mean.

172. **A literal is a second owner too.** The `/context` panel's title
     row carried the literal `"Gemini 3.8 Flash (High)"` while the
     catalogue named the default; the viewport's fallback width was
     `200` in two packages; the `/changelog` scroll rule was
     re-implemented in the host. A copied constant is a copy of a fact
     exactly as a copied function is — the owner asks, and the literal
     goes.

173. **The slash grammar has one owner, and its two rules are
     neighbours, not one rule.** The host's rule (a leading `/`, space
     / tab / newline separators, a space+tab trim) and the print mode's
     rule (an optional `/`, `strings.Fields` separators, a
     `strings.TrimSpace` trim) differ observably at `\v`, `\f`, `\r`
     and at every Unicode whitespace rune, and the difference is
     reachable from a real dispatcher: `agy -p $'/help\vfoo'` dispatches
     `/help` under the print rule and is an unknown command under the
     host's. Both live in `internal/core/slashparse` (tier 1) so a
     future unification is a one-place change — but unifying them is a
     **behaviour change** and needs its own evidence, not a refactor's.

---

## The official-docs alignment (cycle 383)

The `official-docs/` tree is the **documentation** of the reference, and
it is a second source of truth next to the binary. Cycle 383 surveys it
surface by surface — the ledgers live in
`docs/cycles/cycle-383-docs-alignment/` — and closes the divergences in
separate tranches.

174. **A permission rule is `action(target)`, and the action is one of
     six names.** `command`, `read_file`, `write_file`, `read_url`,
     `execute_url`, `mcp`. The seventh verb the reference once had,
     `unsandboxed`, is **rejected** with the reference's own sentence —
     `unsandboxed is no longer a valid rule, use command() instead` —
     not with a paraphrase.

175. **The permission precedence is Deny > Ask > Allow.** The
     `permissions.ask` rule is a third outcome, not an allow. A rule
     the policy does not recognise is rejected, never ignored.

176. **A hook decision the documentation does not name is a prompt.**
     `allow`, `auto_approve`, `deny`, `ask`, `force_ask` and
     `deny_unless_prior_grant` map onto the policy's `Verdict`; an
     unrecognised decision must never be treated as "allow".

177. **The hooks runtime is armed in the interactive TUI only.** Print
     mode and the agent server do not run hooks. The hooks file is
     `~/.gemini/config/hooks.json` — the reference's path, not an XDG
     one. The runtime lives in `internal/core/policy/hooks` (tier 1),
     and the shell a handler runs through is a **seam**, because the
     core tier cannot reach the app tier's shell.

178. **The MCP configuration has three roots, and one resolver.**
     `~/.gemini/config/mcp_config.json` (`mcpServers`),
     `~/.gemini/antigravity-cli/mcp.json` (`servers`) and
     `<workspace>/.agents/mcp_config.json` (`mcpServers`) —
     `mcp/configpaths.go` owns all three.

179. **The user-rules budget is 20 000 tokens** (`len/4`), and a rule
     whose trigger is `manual` is **never loaded**.

180. **`toolPermission`'s absent case is the clone's own.** The
     reference's default is `request-review`; the clone honours the
     setting when it is set and stays allow-by-default when it is
     absent. Changing that changes every unconfigured install's
     behaviour — it is a decision, not an alignment.

181. **The repaint engine is a mechanic; the host keeps the
     decisions.** `internal/ui/transcript` rewrites the rows; the host
     owns `m.response`, `m.blocks`, `m.assistants`, `m.userBlocks`, the
     scroll state, the expansion flags, the cache-dirty flags and every
     "when to repaint" call. The engine's entry points are
     `*Document` methods over a `Document` **value** — the host hands
     the document over and takes it back.

182. **A transcript golden is produced by the base tree.** A golden
     generated by the tree under test pins the change, not the
     behaviour; the repaint golden was produced at the base commit and
     copied back verbatim.

---

## The slash-command vocabulary and the live-step mechanic (cycle 381, tranches 88 and 91)

183. **The slash-command vocabulary has one owner.**
     `internal/ui/slashcommands` owns the rows, the alias rule
     (`Canonical`) and the command → panel answer (`PanelFor`). The
     host's dispatch table is keyed by the canonical name only, and the
     panel kind of a row is *derived* from `internal/ui/panelregistry`
     at init — never copied.

184. **An alias the host answers with a command of its own resolves to
     itself.** The catalogue lists `"branch"` as an alias of `/fork`, and
     the host has a `/branch` of its own, so the vocabulary resolves the
     word to `/branch` and never to `/fork`.

185. **The print mode keeps its own alias spellings.**
     `internal/printmode/print_slash.go` has its own slash grammar;
     canonicalising the names there would change what a print-mode run
     answers.

186. **The live-step batch mechanic is the document's; the host keeps
     the buffer and the decisions.** The batch arithmetic, the batch's
     membership rule (`toolsteps.BatchSteps`) and the step-row growth
     live in `internal/ui/transcript` and `internal/ui/toolsteps`;
     `m.response`, `m.liveStepRuns`, `m.liveBatch`, `m.batchBreak`, the
     cache-invalidation flags and every "when to render" decision stay in
     the host. The step travels **by pointer**, so the batch number the
     mechanic assigns is the exact value the host's index keeps.

## The legacy evidence and the grant vocabulary (cycle 383, sweeps S and U, fixes 8, 9, 90, 92)

187. **A retired artefact is never registered in the drift gate.**
     `internal/infra/referencedrift/reference.lock.json` is for artefacts
     the clone *depends on*: it compares digests and treats a stale
     `captured_at` as SUSPECT. A superseded release is stale **by
     construction**, so registering it would turn the gate permanently
     red. The 1.3.x trees that were retired on 2026-11-02 are recorded
     in `docs/legacy-re/index.json`, **not** in the lock. The seven
     registered `out/` paths keep their **exact bytes**.

188. **A citation into retired material names the surviving copy and
     quotes the string.** A line number into a deleted file is a dead
     pointer. The surviving copies are the pruned
     `out/local-macos-1.4.11/strings-index.txt`, the cited windows under
     `docs/legacy-re/extracts/`, and the live
     `out/local-macos/seeds/`. The redirect table is
     `docs/legacy-re/index.json`'s `redirects` block and the prose form
     is `docs/legacy-re/README.md`.

189. **The RE pipeline's scripts live in `scripts/re/`, not in `out/`.**
     `out/` does not ship in the release snapshot, so a script that
     lives there ships nowhere. `docs/guide/reverse-engineering.md` and
     `scripts/macos-re.sh` name the new paths. The guide page for the
     retirement is `docs/guide/legacy-evidence.md`.

190. **The 1.4.11 string dump's cited numbers are line numbers, not hex
     offsets.** The committed dump had its `strings -t x` offset column
     stripped, so the offsets are unrecoverable; the index carries the
     line numbers and the command that regenerates the whole dump.

191. **A grant's target is matched per action.**
     `internal/core/policy/permissions/matching.go` owns the rules:
     `command` is token-wise against the command's **leading** tokens
     (a token may carry the reference's anchored `regex:` form),
     `read_file`/`write_file` are path prefixes (Windows-normalised
     first), `read_url`/`execute_url` are hostnames (and subdomains),
     and `mcp` names a server and a tool (`server/tool`, `server/*`,
     `*`). An implicit write is allowed by a read grant; an implicit
     read is denied by a write grant. **One shared prefix rule for six
     actions is a bug, not a simplification.**

192. **The remote-control permission grammar is a deliberate duplicate
     of the core one.** `internal/remotecontrol/permissions` is
     stdlib-only and cannot import `internal/core/policy/permissions`,
     so the grammar exists twice. The action set, the three error
     sentences (`invalid grant string: %q`, `unknown action %q in grant
     string: %q`, `unsandboxed is no longer a valid rule, use
     command() instead`) and the retired `unsandboxed` are the **shared
     facts** and must not drift.

193. **A forwarder with a caller is still a shell, and an accessor over
     a field is the same shape.** Tranches 90 and 92 removed the host's
     27 one-line forwarders; every call site now names the package that
     owns the fact. A method that returns the host's **own** field
     through an accessor is a hop that adds nothing, and it goes the
     same way. A method that *adapts* — a different arity, a different
     type, a decision — is not a forwarder and stays, with the reason
     next to it.

194. **A fact that a forwarder's doc comment carried does not die with
     the forwarder.** The sentence moves to the package that owns the
     behaviour; a sentence that turns out to be false is dropped, not
     relocated.

## The retired evidence, the citation rule and the new key behaviours (cycle 383)

195. **A citation names the string, not the deleted line.** A line
     number into a retired file is a dead pointer: the reader who
     follows it finds nothing and cannot tell whether the fact was
     invented or lost. Every comment and every document quotes the
     *fact* and names the **surviving copy** — the pruned
     `out/local-macos-1.4.11/strings-index.txt`, the cited windows under
     `docs/legacy-re/extracts/`, the live `out/local-macos/seeds/`, the
     `re-docs/` report copies, the
     `internal/app/prompt/agytemplates/templates/` bundle.

196. **A retired artefact is not rewritten into fiction.** A cycle
     report that *performed* a rename keeps its table; a `.mavis/plans/`
     file that *planned* one keeps its plan. Both gain a short
     retirement pointer and nothing else — the record of what was true
     when it was written stays true.

197. **A skills reload is best-effort.** `/skills reload` re-scans the
     skill roots; a root that cannot be read **keeps the entries the
     previous scan found for it**. A read error never drops a loaded
     skill, and the reload never panics. The verb's spelling rule
     lives with the reload it selects (`skills.IsReload`), not in the
     slash-command vocabulary — the vocabulary owns the *commands* and
     has no notion of a command's arguments.

198. **The composer is cleared on `esc esc`, not on one `Esc`.** The
     first `Esc` **arms** the clear; any other key breaks the pair; an
     `Esc` that ends up elsewhere (closing an overlay, cancelling a
     turn, dismissing a card) keeps its old meaning and is **never** the
     second half of a pair. The arming carries the composer content it
     was armed on, so a composer that changed in the meantime is not the
     one the user armed.

199. **The `vim` package takes no count prefix.** The count plumbing is
     **removed, not zeroed**: `3dd` deletes one line, and a leading
     digit is an invalid key. `vim` is a **public compatibility
     surface** — a behaviour change is allowed, an exported name's
     change is not, and neither is silent.

200. **A panel names; the layer that owns the state mutates.** The
     `/agents` panel owns its keys and its rows: it names the subagent
     to kill (`K`) and the pending approval to answer (`A`/`D`). The
     **subagent manager** owns the state both mutate
     (`internal/app/subagent/core`). A panel that mutates a slice it
     does not own cannot write the result back — that is the bug, not
     an implementation detail (invariant 166, applied to a panel).

201. **A panel kind owns every key it receives.** The panel registry's
     route table hands a key to the open panel and the panel answers
     whether it took it (invariants 83, 84). A legend that advertises
     a key the panel does not implement is worse than no legend: the
     key is **swallowed**, and the user sees nothing happen.

---

## Commands

```bash
go build -o /tmp/pigasus ./cmd/pigasus          # build
go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...   # test (never ./...)
go build ./cmd/...                              # what CI builds

/tmp/pigasus --help                              # 20 visible subcommands (reference 12 + openagy 8)
/tmp/pigasus proxy version                      # vendor binary + registry
/tmp/pigasus provider list                       # the provider registry
/tmp/pigasus model list                          # the models it serves
/tmp/pigasus server --port 7777                 # host the agent (prints the token)
/tmp/pigasus tui --server ws://127.0.0.1:7777    # attach the session to it

go build -o /tmp/harness ./cmd/harness           # the localharness clone
/tmp/harness --help                              # the harness's own flags
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
| Host the agent, or attach a session to it | [`docs/guide/agent-server.md`](docs/guide/agent-server.md) — `pigasus server`, `pigasus tui --server`, `agentlink/` (the wire), `internal/app/agentturn/` (the seam) |
| See the loop run end to end | `go run ./devutils/pigloop-demo -mode loop` (scripted gateway, no network) |
| Touch the gateway transport | `internal/piagentbridge/gatewayprovider.go` + `internal/infra/backend/gateway/gateway.go` — the two must stay behaviourally identical |
| Know what the last cycle changed | `git log --oneline -20` and the newest `docs/cycles/cycle-<n>/` |
| Match the reference's system prompt | [`docs/guide/prompt.md`](docs/guide/prompt.md) — `internal/app/prompt/agyprompt`, ground truth `internal/app/prompt/agyprompt/testdata/capture_1.4.6.txt` |
| Change what the model can call | [`docs/guide/tools.md`](docs/guide/tools.md) — `internal/app/tools/surface/catalog.go` |
| Change the `/plugin` panel or `agy plugin import` | [`docs/guide/plugins.md`](docs/guide/plugins.md) — `internal/ui/views/pluginsui`, `internal/cliapp/plugin_host.go`, `internal/features/plugins/import` |
| Change how code is highlighted | [`docs/guide/tui.md`](docs/guide/tui.md) — `internal/ui/mdrender/chroma_inline.go` (fences) and `diffsyntax/syntax.go` (diffs); chroma is pinned to **v2.27.0** |
| Configure a model | [`docs/guide/models.md`](docs/guide/models.md) — `~/.openagy/providers.json` |
| Point a Google service at a local one | [`docs/guide/endpoints.md`](docs/guide/endpoints.md) — `~/.openagy/config.json`, `endpoints` |
| Route turns over the account pool | [`docs/guide/accounts.md`](docs/guide/accounts.md) — `accountpool/` (the policy), `internal/features/accounts/routing/` (the runtime) |
| Change the CloudCode wire, or answer it from a local service | [`docs/guide/cloudcode-dialect.md`](docs/guide/cloudcode-dialect.md) — `geminicode/` (the dialect), `internal/infra/backend/cloudcode/cloudcode.go` (the adapter) |
| Know whether the reference binary moved | `go test ./internal/infra/referencedrift/` — it reports which evidence is stale |
| Know what the 1.3.x-era evidence sweep retired, and how to rebuild it | [`docs/guide/legacy-evidence.md`](docs/guide/legacy-evidence.md) — the ledger, the recipes, the redirect table |
| Reverse the binary | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) |
| Wire the SDKs' agent backend, hooks or budgets | [`docs/guide/localharness.md`](docs/guide/localharness.md) — `cmd/harness` (the clone of the binary), `localharness/` (the wire), `lifecyclehooks/`, `budget/` |
| Know whether the tree still respects the tier rule | `go test ./internal/core/architecture/` — the in-tree guard (R1–R6); `devutils/tiergraph.py` prints the same numbers |
| Know which packages are public, and what is queued next | [`docs/guide/public-packages.md`](docs/guide/public-packages.md) — the ROI ranking and the public-surface rule |
| Find an external reference | `docs/research/REFERENCES.md` |

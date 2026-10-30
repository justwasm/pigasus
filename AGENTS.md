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
| point a Google service at a local one | [`docs/guide/endpoints.md`](docs/guide/endpoints.md) |
> | change the CloudCode wire, or stand up a local service that speaks it | [`docs/guide/cloudcode-dialect.md`](docs/guide/cloudcode-dialect.md) — `geminicode/` |
> | change the prompt or `--agent` | [`docs/guide/prompt.md`](docs/guide/prompt.md) |
> | change a tool or its result text | [`docs/guide/tools.md`](docs/guide/tools.md) |
> | change a TUI panel | [`docs/guide/tui.md`](docs/guide/tui.md) |
> | change the `/plugin` panel or `agy plugin import` | [`docs/guide/plugins.md`](docs/guide/plugins.md) |
> | change the cloud remote view, its tunnel or its wire | [`docs/guide/remote-control.md`](docs/guide/remote-control.md) |
> | re-run the Ghidra pipeline | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) |
> | the reference binary moved | [`docs/guide/reference-drift.md`](docs/guide/reference-drift.md) |
> | cross-check the CLI against the IDE's binary | [`docs/guide/ide-language-server.md`](docs/guide/ide-language-server.md) |
> | change the IDE's browser bridge (`webview_browser`) | [`docs/guide/ide-language-server.md`](docs/guide/ide-language-server.md) — `agentbrowser` |
> | wire the SDKs' agent backend, hooks or budgets | [`docs/guide/localharness.md`](docs/guide/localharness.md) — `localharness/`, `lifecyclehooks/`, `budget/` |
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
| `geminicode/` | the **CloudCode dialect** as a standalone package — the wire types, the request envelope, the SSE frames. No `internal/` dependency, so it imports on its own; `internal/backend` adapts it. See [`docs/guide/cloudcode-dialect.md`](docs/guide/cloudcode-dialect.md) |
| `localharness/`, `lifecyclehooks/`, `budget/`, `harnessprompt/` | the **SDK-facing surface** as public packages: the `localharness` wire (both halves), the bidirectional hook protocol, the limit manager, the harness system instruction. No `internal/` dependency, so a third party implementing the SDK side imports them directly. See [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |
| `lsproto/`, `remotetransport/`, `rctoken/`, `originpolicy/`, `endpoints/` | the **Web UI's wire** as public packages: the proto-JSON dialect, the Connect and gRPC-Web transport, the token format, the origin policy, the named-endpoint resolver. No `internal/` dependency, so a local service that answers the Web UI imports them directly. See [`docs/guide/remote-control.md`](docs/guide/remote-control.md) |
| `render/`, `diffsyntax/`, `palette/`, `retry/`, `retrybudget/`, `atomicfile/` | the **library tranche** as public packages: the markdown renderer, the diff highlighter, the true-colour palette, the retry helpers, the atomic file write. No `internal/` dependency, so they import on their own. See [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |
| `browsercdp/`, `browserbridge/`, `agentbrowser/`, `mcp/`, `envref/`, `grpc/` | the **browser and MCP surface** as public packages: the Chrome DevTools Protocol client, the in-Chrome bridge protocol, the browser tool's broker, the MCP client, the env-reference resolver, the recovered gRPC service registry. No `internal/` dependency, so they import on their own. See [`docs/guide/tools.md`](docs/guide/tools.md) |
| `vim/`, `imagery/` | the **last two leaves** as public packages: the modal editing engine, the image downscaler. No `internal/` dependency, so they import on their own. See [`docs/guide/public-packages.md`](docs/guide/public-packages.md) |
| `accountpool/` | the **account-routing policy** as a public package: the `config.json` section's shape, the four strategies, the quota threshold, the decision. Pure — no I/O, no `internal/` dependency — so it imports on its own; the runtime is `internal/accountrouting`. See [`docs/guide/accounts.md`](docs/guide/accounts.md) |

The reference is **Antigravity CLI** (`agy`) — a closed-source Google-internal
agent CLI (`google3/third_party/jetski/…`). The clone emulates **agy 1.4.11**
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
| Entry point | `cmd/pigasus` (also `cmd/harness`, `cmd/openai-shim`, `cmd/anthropic-shim`, `cmd/openagy-models`; helpers in `devutils/`) |
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
                  internal/backend         ← CloudCode | gateway (OpenAI / Anthropic)
                        │
                        └─→ geminicode       ← the CloudCode dialect, standalone (no internal/ deps)
                        │
                  internal/tools           ← 12 wire declarations, 60 advertised names
                        │
                  internal/agyprompt       ← the reference's system prompt, byte for byte
                        │
                  internal/agytemplates    ← the reference's 17 .tmpl files, embedded once
                        │
                  internal/agystep         ← the reference's step strings + the result reader

cmd/harness  ──┬── localharness          ← the localharness wire, both halves
               ├── internal/pigrt         → PiG  coding.Session   (the same turn engine)
               ├── internal/tools         ← the reference's tool names
               ├── budget                 ← the limits
               └── lifecyclehooks         ← the hook model

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
```

- **Every Google service URL is a named endpoint.** `endpoints`
  owns the names, the compiled-in defaults and the resolver over
  `~/.config/openagy/config.json`; the live call sites read
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
- **Two configuration files**: `~/.config/openagy/providers.json` (provider
  centric — `internal/models.Editor` is its only writer; the
  `provider`/`model` commands and the TUI's `/provider` panel are its only
  front-ends) and `~/.config/openagy/config.json` (the named **service
  endpoints** and the **account-routing policy** — `endpoints` is its only
  reader *and* its only writer: `endpoints.SaveAccountPool` is what the TUI's
  Pool tab writes through). The split is the point: `providers.json`
  answers *"which model talks to which provider"*, `config.json` answers
  *"which service does the clone itself talk to"* and *"which of my accounts
  serves this turn"*.
- **The prompt is the reference's**, not PiG's: `internal/agyprompt` rebuilds
  it byte for byte (the 1.4.6 capture is 70,853 B —
  `internal/agyprompt/testdata/capture_1.4.6.txt`). It is **rendered from the
  reference's own `.tmpl` files** (recovered into
  `internal/agytemplates/templates/`, 17 of them) — seven of the twelve
  sections come from a template, the other five have none in the binary. The
  reference's builtin skill bundle is in `internal/builtinskills/`.
- **A command's result has two shapes.** The reference's *current* one
  (`Command exited with code N.` + `Command output:`) is the CLI's own
  formatter, and it is what the clone sends. The *step string*
  (`The command exited with code N.` + `Output:`) is what the recovered
  `step_strings/run_command.tmpl` renders, and it is what the reference
  produced at 1.4.2/1.4.3. `internal/agystep` renders it and reads both;
  `internal/tuitools` and `internal/convstore` read through it. A consumer
  written against only one of them silently mis-reads the other.
- **The tool surface is two lists**: the wire carries the reference's **12**
  declarations, `init.tools` advertises **60** names.
- **A `!` command has its own store.** The reference never puts a shell
  command in the conversation: it writes it into an *implicit trajectory* —
  `<gemini-dir>/implicit/<uuid>.pb`, AES-256-GCM, one file per workspace.
  `internal/implicit` writes that format byte for byte, and
  `internal/cumulativeprompt` converts the trajectory into the reference's own
  chat messages for the next prompt — the command still never reaches the
  conversation store.
- **A third surface exists but is not wired into the turn loop yet**:
  `localharness/` (the SDKs' transport to the `localharness` agent
  backend), `lifecyclehooks/` (its bidirectional hook protocol) and
  `budget/` (its limit manager). They are **public** packages on purpose —
  a third party implementing the SDK side needs them — see
  `docs/guide/localharness.md` and `docs/guide/public-packages.md`.
- **The Web UI has two surfaces of its own**: the terminal
  (`internal/rcterminal` — a live shell per terminal, on a pty) and the
  voice (`internal/rcaudio` — a session that transcribes through the
  Gemini transport). Both answer the reference's own LanguageServerService
  methods, and both are **not** the dashboard's `RemoteAgentTerminal*`
  contract.

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
    `~/.openagy/wrapper.{json,log}`) keep their names **on purpose** —
    renaming them silently moves an existing capture directory.
17. `internal/agyproxy` may import `internal/models` (read-only) and
    `internal/agybin`, but **not** `internal/cli`.
18. **chroma stays at v2.27.0** — the reference embeds upstream v2.27.0's 353
    files byte for byte. v2.24.1 cannot highlight Arturo, Gemfile.lock,
    LilyPond or Templ, and the bump changes no rendered byte, so only
    `internal/mdrender/chroma_reference_test.go` catches a downgrade.

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
    `useWebSocket=true` (`internal/refbackend/transport.go`); the
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
    (`internal/remotecontrol/mendel_flags.go`) — one shared cache hands the
    CLI's set to the desktop page.
30. **The Web UI's terminal and voice are their own surfaces.** The LS
    table is consulted **before** the alias table, so `CreateTerminal`
    and its four siblings reach the Web UI's own handlers
    (`internal/remotecontrol/ls_terminals.go`) and not the dashboard's
    `RemoteAgentTerminal*` contract — the alias table's
    `CreateTerminal → CreateRemoteAgentTerminal` is the fallback, not the
    route. The terminal is a live shell on a pty (`internal/rcterminal`),
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
    (`internal/rclink`, the chooser is the named endpoint
    `account_chooser`). The **deep link** is the reference's own; the
    chooser around it is the clone's own and does **not** match the
    reference's bytes. A URL shown in the TUI is **never left on one
    line**: the renderer *clips* a line that is wider than the terminal,
    so the tail of a long URL disappears. It is cut into chunks, and
    every chunk carries the **complete** URL as its OSC 8 target
    (`internal/urlchunk`, shared with the add-account screen). The
    transcript's selection joins two rows with the same hyperlink target
    without a newline, so copying the chunks yields the URL.
32. **The printed link must name the id the tunnel registered.** The
    installation uuid is persisted in
    `~/.config/openagy/remote-control-instance-id`; a configuration
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
    (`internal/memtrim`, armed in `internal/cliapp/tui_entry.go`).
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
    (`internal/clipboard/media.go`, `internal/clipboard/paste.go`,
    `internal/remotecontrol/message_image.go`). The cap is 2000 px per
    side (`imagery.MaxSide`).
37. **A subagent's parent context lives in the child's own session.** The
    parent's task and id are the *oldest* messages of the subagent's
    conversation, so a summary drops them first — the clone keeps the
    parent's half itself (`internal/subagenttools/parentcontext.go`) and
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

## Commands

```bash
go build -o /tmp/pigasus ./cmd/pigasus          # build
go test ./cmd/... ./internal/...                # test (never ./...)
go build ./cmd/...                              # what CI builds

/tmp/pigasus --help                              # visible subcommands (reference twelve + openagy superset)
/tmp/pigasus proxy version                      # vendor binary + registry
/tmp/pigasus provider list                       # the provider registry
/tmp/pigasus model list                          # the models it serves

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
| See the loop run end to end | `go run ./devutils/pigloop-demo -mode loop` (scripted gateway, no network) |
| Touch the gateway transport | `internal/piagentbridge/gatewayprovider.go` + `internal/backend/gateway.go` — the two must stay behaviourally identical |
| Know what the last cycle changed | `git log --oneline -20` and the newest `docs/cycles/cycle-<n>/` |
| Match the reference's system prompt | [`docs/guide/prompt.md`](docs/guide/prompt.md) — `internal/agyprompt`, ground truth `internal/agyprompt/testdata/capture_1.4.6.txt` |
| Change what the model can call | [`docs/guide/tools.md`](docs/guide/tools.md) — `internal/tools/catalog.go` |
| Change the `/plugin` panel or `agy plugin import` | [`docs/guide/plugins.md`](docs/guide/plugins.md) — `internal/pluginsui`, `internal/cliapp`'s plugin panel, `internal/features/plugins/import` |
| Change how code is highlighted | [`docs/guide/tui.md`](docs/guide/tui.md) — `internal/mdrender/chroma_inline.go` (fences) and `diffsyntax/syntax.go` (diffs); chroma is pinned to **v2.27.0** |
| Configure a model | [`docs/guide/models.md`](docs/guide/models.md) — `~/.config/openagy/providers.json` |
| Point a Google service at a local one | [`docs/guide/endpoints.md`](docs/guide/endpoints.md) — `~/.config/openagy/config.json`, `endpoints` |
| Route turns over the account pool | [`docs/guide/accounts.md`](docs/guide/accounts.md) — `accountpool/` (the policy), `internal/accountrouting/` (the runtime) |
| Change the CloudCode wire, or answer it from a local service | [`docs/guide/cloudcode-dialect.md`](docs/guide/cloudcode-dialect.md) — `geminicode/` (the dialect), `internal/backend/cloudcode.go` (the adapter) |
| Know whether the reference binary moved | `go test ./internal/referencedrift/` — it reports which evidence is stale |
| Reverse the binary | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) |
| Wire the SDKs' agent backend, hooks or budgets | [`docs/guide/localharness.md`](docs/guide/localharness.md) — `cmd/harness` (the clone of the binary), `localharness/` (the wire), `lifecyclehooks/`, `budget/` |
| Know which packages are public, and what is queued next | [`docs/guide/public-packages.md`](docs/guide/public-packages.md) — the ROI ranking and the public-surface rule |
| Find an external reference | `docs/research/REFERENCES.md` |

# AGENTS.md

## What this repository is

This is **not an application**. It is a reverse-engineering research workspace with three
things in it:

| Path | What it is |
|---|---|
| `rea-go-seeds/` | A vendored copy of the upstream OSS project [`VallierDev/rea-go-seeds`](https://github.com/VallierDev/rea-go-seeds). One Go file (`main.go`, ~175 lines) that recovers Go function names from a stripped binary's `pclntab` and writes a REA/Ghidra seed file. Module name is `pclnseeds`, **stdlib-only, zero third-party dependencies** (a hard project constraint — see `out/two-prong-attack.md` §9). |
| `out/` | The committed evidence directory: reports, seed files, call graph, Ghidra postScripts, raw `readelf`/`strings` dumps. Everything here is a *result*, not a program. |
| `cmd/wagyu/`, `internal/` | The **clone**: a from-scratch Go reimplementation of the Antigravity CLI, aiming for byte-level parity with the reference binary. Built with `go build -o /tmp/wagyu ./cmd/wagyu`; the tested surface is `go test ./cmd/... ./internal/...` (never `./...` — `scratch/` has never compiled). Each change is a numbered *cycle* with a report under `docs/cycles/cycle-<n>/`. Renamed from `cmd/antigravity` in cycle 121 so the command no longer collides with the reference's own name. Since cycle 183 the agent loop is `github.com/MichaelKinsy/PiG`'s, wrapped by `internal/piagentbridge` — see **The agent loop** below. |
| `cmd/agy/`, `devutils/` | The **`agy` wrapper** (cycle 274): launches the *official* `agy` with a local CloudCode proxy, so it can use models it does not ship with (MiniMax M3 via `agw.k0s.io`) and so its requests/responses can be captured. Does not patch the vendor binary. Separate from the clone — see **The `agy` wrapper** below. |

Reports written before cycle 121 spell the clone's entry point `cmd/antigravity`; the
rename is a directory move only — the package is `main` and nothing imports it.

`gofmt -l` is **not** clean for the clone and never has been: 50 files under `internal/`
plus `cmd/wagyu/picker_view.go` and `cmd/wagyu/tui_v135_test.go`. Leave them alone unless
you are already editing them; do not reformat them wholesale (it buries real diffs).

Three subtrees live under `out/`, and they are **not interchangeable**:

| Subtree | Target | Built on |
|---|---|---|
| `out/` top level + `out/decompile/` | Linux/ELF sandbox | Debian 12 at `/workspace/agy/`, agy ~1.0 |
| `out/local-macos/` | **this machine**, macOS/arm64 | agy **1.3.4** — the working pipeline |
| `rea-go-seeds/README.md`, `docs/TUTORIAL.md` | macOS/Mach-O walkthrough | the tutorial case |

`out/local-macos/` is the live one. Start there; `out/local-macos/FINDINGS.md` records
what the current agy build actually contains.

The target binary under study is **Antigravity CLI** (`agy`) — a closed-source Google
internal monorepo agent CLI (`google3/third_party/jetski/...`), built with Google’s
internal Go toolchain (`go1.28-20260921-RC02`, tags `fieldtrack,boringcrypto,simd`).

---

## Two different environments — the single biggest source of confusion

The committed artifacts were produced in a **Debian 12 x86-64 sandbox rooted at
`/workspace/agy/`**. This checkout lives on macOS/arm64. Two distinct targets exist:

| | macOS/arm64 case | Linux sandbox case |
|---|---|---|
| Target | `~/.local/bin/agy` | `/workspace/agy/install/antigravity` |
| Format | Mach-O arm64, 180 MB | ELF64 x86-64 PIE, 214 MB |
| pclntab section | `__lrodata_gopcln` (62.5 MB) | folded into `.lrodata` |
| `.text` start address | `0x1000011c0` (loaded at `0x100000000`) | `0x5dc5000` (PIE, no fixed base) |
| Write-ups | `rea-go-seeds/README.md`, `rea-go-seeds/docs/TUTORIAL.md` | everything in `out/` |

**Never mix addresses between the two.** A function named
`.../store.store.(*Manager).waitForEligibility` exists at `0x899cd40` in the ELF build
and at `0x10262f660` in the Mach-O build. The `.text` start address is what gets
handed to `gosym.NewLineTable`, and getting it wrong silently shifts every PC.

Everything under `out/` — except `out/local-macos/` — is hardcoded to `/workspace/agy/`.
`out/decompile/DecompilePriority.java` has 6 such references and
`out/decompile/build_reports.py` has 4. **Rewrite these constants if you run them
elsewhere**, or they will silently write to a nonexistent tree.

### Toolchain — installed and working on this machine

The macOS/arm64 pipeline has been configured and run end-to-end here; see
**`out/local-macos/FINDINGS.md`** for results and `out/local-macos/env.sh` for the
environment.

| Tool | Version | Path |
|---|---|---|
| Go | 1.27.0 | `/Users/gear/go/bin/go` |
| JDK | 21.0.12.1 (Homebrew `openjdk@21`) | `/opt/homebrew/opt/openjdk@21` |
| Ghidra | 12.1.4 | `~/tools/ghidra_12.1.4_PUBLIC` |
| rea-agents | 6.4.0 | `/opt/homebrew/bin/rea` |

`rea doctor --provider ghidra` is all-green. Node is v26.11.1 (REA needs 22/24/26+).

Four things this machine gotcha-checks, each of which cost real time:

1. **The official Ghidra zip ships no macOS decompiler.** You must build it:
   `cd ~/tools/ghidra_12.1.4_PUBLIC/support/gradle && JAVA_HOME=<jdk21> ./gradlew buildNatives`
   (~3 min) → `Ghidra/Features/Decompiler/build/os/mac_arm_64/decompile`. Without it
   `rea doctor` fails `ghidra-native_decompiler`.
2. **REA defaults Ghidra's heap to `-Xmx2G`** (`dist/ghidra/GhidraJavaLaunch.js:53`);
   override with `GHIDRA_HEADLESS_MAXMEM`. Raising it is worth doing but is **not** the
   cause of the wrapper failures — see the next section.
3. **Ghidra searches the current working directory for scripts before `-scriptPath`.** A
   stray `DecompilePriority.java` in the CWD is picked up and fails with *"Failed to find
   source bundle containing script"*. Run `analyzeHeadless` from a neutral CWD (e.g.
   `/tmp`) and keep the script in its own directory.
4. **`~/GitHub` is a symlink to `~/Documents/GitHub`.** Ghidra canonicalises paths, which
   shows up in error messages; harmless once you know.
5. **The committed seeds and Ghidra project predate agy 1.4.4.** `out/local-macos/seeds/`
   holds **154,525** functions; the installed 1.4.4 binary has **160,459**
   (`cd rea-go-seeds && go run . ~/.local/bin/agy /tmp/seeds144` — 1 second, no
   network). Every address in `seeds.txt`, the project `out/local-macos/proj` and
   all of `dec/` is therefore **pre-1.4.4**; citing them for 1.4.4 findings means
   decompiling whatever function happens to sit at the old address. Regenerate
   before anything else. Related: Ghidra's `&UNK_<addr>,<len>` refs are unreliable
   for literals whose pointer it failed to track — read the instruction stream
   instead (`out/local-macos/scan_strings.py`, `out/local-macos/find_callers.py`).
   See `docs/cycles/async-subagent-framing/deliverable.md`.

`.crush/` is Crush runtime state (sqlite db + logs), not project content. `.crush/init` is
empty.

---

## The agent loop (cycle 183; print mode moved onto PiG in cycle 210)

There are **two** turn engines in the tree, and the split is now sharp:

| Engine | Owns | Where |
|---|---|---|
| `internal/pigrt` (PiG `coding.Session`) | **`--print`** — the whole headless surface | `cmd/wagyu/print_pig.go` |
| `internal/agent.Loop` (PiG `agent.Agent`) | the **interactive TUI** only | `cmd/wagyu/tui_run.go` |

`internal/agent.Loop` was the clone's turn engine. Until cycle 182 it walked the
model → tool → model cycle itself; since cycle 183 `Send` delegates to
`github.com/MichaelKinsy/PiG`'s `agent.Agent`, with `internal/piagentbridge` as
the only translation layer. Since cycle 210 a `-p` turn does not go through it
at all — the print path runs PiG's `coding.Runtime`/`coding.Session` directly,
which is what brings MCP and skills. Read in this order:

| File | What it is |
|---|---|
| `cmd/wagyu/print_pig.go` | the print path: `runPigPrintTurn`, `pigPrintModel` (which `ai.Provider`), `pigHistory` (`-c` seeding) |
| `internal/pigrt/runtime.go` | the `coding.Session` wrapper: `New`, `Run`/`RunWith`, the event drain, `turnFailure` |
| `internal/pigrt/mcpconfig.go` | the agy `mcp.json` → PiG `mcp`-extension config bridge |
| `internal/pigrt/skills.go`, `errors.go`, `history.go` | skill roots, the out-of-band `ErrorSlot`, `SeedHistory` |
| `internal/piagentbridge/backendprovider.go` | any `backend.Client` → `ai.Provider` for the session (mock/shim; agw uses the native provider) |
| `internal/agent/pig.go` | the *TUI*'s translation: `sendPig`, `pigMessages`, `chunkSink`, `runnerTool`, `toolCallFromPig`, `metaFromPig`, and `nativeProvider` |
| `internal/agent/loop.go` | `agent.Loop`'s **public API, unchanged since 183**. `Send` is a one-line delegation. `Options` gained `Provider` as an *override* |
| `internal/piagentbridge/bridge.go` | `backend.Client` → PiG, the tool catalogue bridge, `Describer`/`Describe` |
| `internal/piagentbridge/stream.go` | `BackendStreamFn`, `pump`, `ToStreamRequest`; `BackendStreamFnWithObserver` carries chunk metadata that `ai.Usage` cannot |
| `internal/piagentbridge/agwprovider.go` | the self-written native `ai.Provider` over agw's OpenAI-shaped SSE endpoint |

Four things about PiG's stream contract are **undocumented upstream** and the
adapter depends on all four. They were found by reading `agent/agent_loop.go`:

1. Event order is `Start → Text* → ToolCall* → Done|Error`.
2. `End(result)` is **mandatory** — `consumeStream` reads `stream.Result()`.
3. `StopReasonToolUse` is the only signal that triggers the tool batch.
4. Tool declarations ride on the initial `SystemMessage.ToolsAdded`.

Five further traps, all load-bearing:

- `ai.Provider.Stream(ctx, transcript, opts)` **does not receive the model**
  (`agent_loop.go:412`), so a multi-model provider must be pinned per model —
  hence `Provider.Model(label)` / `pinnedProvider`.
- PiG copies only `err.Error()` across the stream boundary
  (`agent_loop.go:494`, `return errorAssistant, nil, nil`). Any error whose
  concrete type matters — `*backend.HTTPError`, which
  `cmd/wagyu/api_error.go:102` recovers with `errors.As` — has to be recorded
  out-of-band. Hence `errorSlot` in `pig.go` (TUI) and `pigrt.ErrorSlot`
  (print), and the rule that both transports return a **pre-stream** failure as
  `(nil, err)` rather than an in-stream event.
- Because of that same line **`coding.Session.Send` can return a nil error for a
  turn that failed.** The print path recovers the failure from the returned
  messages (`pigrt.turnFailure`) and prefers the recorded original; check
  `ctx.Err()` *before* it so `--print-timeout` stays a partial-output success.
- PiG's `emit` **blocks** until the event is received, so the event drain
  goroutine must run independently of `Send` (128-buffer channel, `EventDone`
  left open).
- `Send` returns before the drain has *processed* the last events, so a trailing
  text delta can still be in flight. `pigrt.RunWith` calls
  `session.FlushEvents(ctx)` (a FIFO barrier the drain acknowledges) before
  clearing `onText`, or stdout gets truncated.

**Do not re-add a second transport.** The clone's agw client and the native
provider must stay behaviourally identical: `ExternalModel` remap,
`SystemFingerprint` (`backend.AgwSystemFingerprint`), `User-Agent`
(`backend.AgwUserAgent`), and the `*backend.HTTPError` classification. The
switch is implicit — `nativeProvider` derives a provider from any backend that
implements `AgwConfig()`, so `cmd/wagyu` is edited by zero lines and both the
`--print` and TUI paths move together. `backend.NewMock()` and the lifecycle
wrappers do not describe themselves: the TUI keeps the `StreamFn` path, and the
print path wraps them in `piagentbridge.BackendProvider` (the same `StreamFn`).

Cost, and the reason to be careful: `cmd/wagyu` was **+12.1 MB (+50.8 %)** since
`b36b3cb` (24.9 MB → 37.6 MB) when cycle 183 added PiG, and PiG brings 1 direct
+ 24 indirect module requirements. Cycle 210 then put PiG's `coding` runtime on
the print path unconditionally, so the binary is now **~69 MB**
(`-tags nocodemode` → ~64 MB). Cycle 278 moved PiG to **v0.5.1**, another
**+11 MB** (→ 102 MB) because 0.5 links `go/types`, `go/doc`, `go/format`,
`runtime/pprof` and `internal/zstd`. Two things about that upgrade are worth
remembering:

- **`sum.golang.org` has no checksum for `v0.5.1`.** Both the proxy and
  `GOPROXY=direct` fail with `unknown revision v0.5.1`; use
  `GONOSUMDB='github.com/MichaelKinsy/PiG,github.com/MichaelKinsy/PiG/*'`.
- **The extension runtime must be shared.** Built-ins are factories in 0.5,
  and the `*extension.ExtensionRuntime` passed to `RuntimeOptions` has to be
  the same one the factories registered into, or MCP servers never reach
  `session_start` (`TestRuntimeConnectsMCPFromAgyConfig`).

Reports: `docs/cycles/cycle-183/`, `docs/cycles/cycle-209/`,
`docs/cycles/cycle-210/`, `docs/cycles/cycle-278/`.

---

## Model routing (`internal/backend/cloudcode.go`, cycle cloudcode-model-switching)

There are **two real transports**, and the selected model decides which one runs:

| Model family | Transport | Endpoint |
|---|---|---|
| Gemini / Claude / GPT-OSS (catalogue) | `backend.CloudCodeClient` | `https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse` |
| MiniMax (agw registry) | `backend.AgwK0sClient` | `https://agw.k0s.io/v1/chat/completions` |

`pickBackendWithLog` (`cmd/wagyu/main.go`) does the split at startup; the TUI
does it again on every `/model` change through `runMode.backendFor`
(`syncBackendForModel`). An explicit `--backend agw` /
`OPENAGY_BACKEND=agw` still forces the gateway. `--backend cloudcode`
(`google`, `gemini`, `cc`) forces the official path.

The CloudCode wire was transcribed from live 1.4.3 traffic (report:
`docs/cycles/cloudcode-model-switching/`). Five things are load-bearing:

- **`daily-` matters.** `cloudcode-pa.googleapis.com` answers 429 for this
  account; `daily-cloudcode-pa` answers 200. The `cmd/agy` wrapper's upstream
  is the prod host, which is why it looked quota-exhausted.
- **`sessionId` is a JSON string**, not a number
  (`400 Invalid value at 'request.session_id' (TYPE_STRING)`).
- **Tool results are model-role contents** with
  `functionResponse{id,name,response:{output}}`, and the model's
  `thoughtSignature` is replayed on the `functionCall` part. Signatures are kept
  in memory per client.
- **Function declarations must be unique.** The clone advertises
  `invoke_subagent` twice (its catalogue + PiG); Gemini rejects the whole
  request. The transport keeps the first per name.
- **macOS token = keychain, not the files.** The reference stores the live token
  in the login keychain (`gemini`/`antigravity`); the on-disk cascade can be a
  stale fixture. The client prefers the keychain and refreshes there.
- **Reasoning is wrapped, not dropped.** CloudCode sends reasoning as
  `thought:true` text parts (Claude does; Gemini Flash's measured turns return
  only signature-only parts). The transport wraps each run in the reference's
  `<think>…</think>` markers, which is what `internal/agent.Sanitizer` already
  consumes for MiniMax — the TUI renders `▸ Thought for …` and ctrl+o expands
  the body, while print mode strips it.

The catalogue stays at 1.4.3's own `--model` list — the server advertises newer
ids (`gemini-3.1-pro-high`, Argon) that the reference's validator itself
rejects, so accepting them would diverge.

---

## The system prompt (`internal/agyprompt`, cycle agy-system-prompt)

The reference sends a **34,350-byte** instruction block on every request; before
this cycle the clone sent none. `internal/agyprompt` reproduces it byte for
byte. Read in this order:

| File | What it is |
|---|---|
| `internal/agyprompt/agyprompt.go` | `Options` + `Build`: ten XML sections, fixed order, no trailing newline |
| `internal/agyprompt/sections/*.txt` | the eleven static bodies (embedded) |
| `internal/agyprompt/testdata/capture_1.4.4.txt` | the ground truth — a real agy 1.4.4 request recorded by the `cmd/agy` wrapper |
| `internal/agyprompt/skills.go` | `DiscoverSkills`: the agy skill roots → the `<skills>` list |
| `internal/pigrt/runtime.go` | an empty `Options.SystemPrompt` now means **the reference prompt**, not PiG's; `PigPrompt: true` restores PiG's |
| `cmd/wagyu/tui_system_prompt.go` | the TUI's builder, passed as `agent.Options.SystemPrompt` |

Three things to know:

- **The system prompt had never reached the wire.** `piagentbridge.systemText`
  read only `SystemMessage.Content`, but a `coding.Session` puts the text in
  `Sections` and leaves `Content` empty — so every prompt was rendered as `""`
  and dropped, on both transports. It now renders content **and** sections the
  way PiG's `ai.GetCurrentSystemPrompt` does. `internal/piagentbridge/system_text_test.go`
  pins it.
- **The capture is authoritative.** The leaked
  [system_prompts_leaks PR #233](https://github.com/asgeirtj/system_prompts_leaks/pull/233)
  is the **IDE** prompt, not the CLI's; it corroborates the identity line and
  the `<user_information>` fields and nothing more. Both external references are
  listed in `docs/research/REFERENCES.md`.
- **Releasing a new `agy` invalidates this fixture.** `internal/referencedrift` now
  detects that (see *Reference drift* below): it names this file as suspect and
  prints the re-capture procedure. Note that `TestBuildMatchesCapture` will **not**
  be what fails first — it only compares our `Build()` against this committed
  capture, so both can be stale together and stay green. Re-capture via
  `OPENAGY_CAPTURE_DIR` (see the wrapper section below), regenerate
  `sections/*.txt`, then reseal the lock.

Report: `docs/cycles/agy-system-prompt/deliverable.md`.

---

## The tool surface (`internal/tools`, cycle 275)

There are **two** tool lists, and they are not the same list (cycle 293):

| Surface | Built from | Contents |
|---|---|---|
| Model declarations | `internal/tools/model_tools_1.4.4.json` via `ModelDeclarations()` | **12** function declarations, capture order |
| `init.tools` / available | `internal/tools/catalog.go` (`referenceSpecs`, `Catalog`) | **60** names |

The reference's `-p --output-format stream-json` init event lists 60 names, but
the CloudCode request body carries twelve declarations:
`read_file, run_command, manage_task, send_message, schedule, invoke_subagent,
define_subagent, manage_subagents, write_file, edit_file, search_web,
ask_question` — identical in 1.4.3 and 1.4.4. The clone used to send the 60 as
declarations under 1.3.6 names, so reference-shaped calls failed twice
(`Tool read_file not found`, then a required-field mismatch). `Catalog` remains
the 60-name list for `init.tools` and is unchanged; `RuntimeCatalog` adds
`read_file` / `write_file` / `edit_file` executors under the reference's names.

Two traps: the captured schemas use protojson's uppercase `type` values, which
are *not* valid JSON Schema and are rejected by PiG's metaschema check before
any call runs — `tools.SchemaParams` lowercases them for PiG and the transport
uppercases them again on the wire; and when a PiG subagent extra owns the same
name (`invoke_subagent`, `send_message`, `manage_subagents`), the captured
declaration must win on the wire while the extra still executes
(`declOverrideTool` in `internal/agent`, `SurfaceTools` in
`internal/piagentbridge`). Report: `docs/cycles/cycle-293-model-tool-surface/`.

`internal/tools/catalog.go` is the available-surface catalogue
(`referenceSpecs`); each executor beside it lives in `command.go` /
`filesystem.go` / `nonnetwork.go`. Two executors used to fail calls the
reference completes, and the failure looked like a *stale* `(Failed)` badge
because the command really had run:

- **`run_command` is a shell, not an argv splitter.** The reference advertises no
  `run_shell_command` at all, and its declaration says "Make sure to specify
  CommandLine exactly as it should be run in the shell". `RunCommand` runs
  `CommandLine` through the host's default shell (cycle 276 — `DefaultShellArgv`
  → `zsh -c` / `bash -c` / `powershell -Command`) and returns the reference's
  result text verbatim: `Created At…` / `Completed At…` /
  `Command exited with code N.` / `Command output:` (or `No output from the
  command.`), with no blank line after the stamp and no trailing newline.
  **A non-zero exit is a `nil`-error result, not a failure** — the model reads
  the code. (Returning an error made `Dispatch` discard the output.) Text and
  truncation are captured in `docs/cycles/command-output-truncation/`; the
  cycle's `captures.md` is the ground truth, not the older reconstructions.

  **The clone's `mvdan/sh/v3/interp` is a deviation, and the reference's own
  `mvdan/sh` is NOT an interpreter.** The 1.4.2 binary embeds only
  `mvdan/sh/syntax` (317 funcs) + `fileutil` — **zero** `interp`, zero `expand`
  (same in 1.3.4: 292 + 1). `syntax.(*Parser).Parse` is called from
  `cortex/command/command.parseCommand` / `walkCommandCalls`,
  `cortex/policyguardian/vetting.{SplitLiteralCommand,isReadOnlyCommand}`,
  `security/safetext/shprintf` and `terminal.parseShell` — i.e. **command analysis
  and auto-run policy**, never execution. Execution is a real shell:
  `zsh -c "<CommandLine>"` on macOS (verified live: `$0=zsh`, `$-=569X`,
  stdin on a pty, parent = the `agy` binary), spawned through
  `cortex/command/command.(*goRunner).Run` → `google3/devtools/ai/sandbox/exebox`
  → `google3/third_party/golang/pty`. The shell name is
  `terminal.GetDefaultShellName()`, a **hardcoded per-OS default** — darwin→`zsh`
  (【一手】, disassembly + live turn), linux→`bash` and windows→
  `powershell`/`pwsh` (【推测】: the two remaining branches of the same type
  switch, one of which calls `os/exec.LookPath("pwsh")`). It does **not** read
  `$SHELL` — the string `SHELL` does not occur in the binary, and on a machine
  whose `$SHELL` is `/opt/homebrew/bin/bash` it still runs `zsh`. That value is
  also what fills `Shell: %s` in the declaration
  (`cortex/config.defaultConversationalCascadeConfig` calls it).
  **Cycle 276 fixed both**: `run_command` spawns that shell
  (`DefaultShellArgv`), and `runCommandDescription` advertises
  `DefaultShellName()` instead of `$SHELL`. Only `run_shell_command` — a
  clone-only compat alias the reference does not advertise — still uses the
  pure-Go `mvdan/sh/v3/interp`.
- **stdin is a pty, and the result text is ANSI-stripped.** The
  reference gives the child a pty on **stdin only** — stdout/stderr stay pipes
  (`TTY0=/dev/ttys005`, `TTY1=not a tty`, `speed 9600 baud; rows 24; columns
  80; icanon echo isig icrnl opost onlcr`, `$-=569X`). Its output is
  `stripANSI` + `CR → LF`: `printf 'a\nb\n'` → `a\nb\n` (no CRLF rewrite),
  `printf 'a\r\nb\n'` → `a\n\nb\n`, a bare `\r` becomes a line break, a
  100-char line is **not** wrapped at 80, and SGR is gone. There is no
  terminal emulation. **Cycle 277 recorded a naive `LF → CRLF` rewrite
  instead — that capture is byte-identical to the clone's own formatter and
  never came from the reference**; the live 1.4.4 request bodies in
  `docs/cycles/command-output-truncation/captures.md` supersede it.
  `RunWithPTYStdin` (pty details) is still cycle 277's and still right. Three
  measured traps live in `pty_unix.go`: the parent must close its copy of the
  slave fd after `Start` (else the child wedges in `exit()`, `STAT ?Es`,
  `wait4` never returns); the
  master must be **drained** (the pty echoes our own input back, and undrained
  it costs a fixed ~600 ms per command on Darwin); and the master must not be
  closed while the child lives (SIGHUP). One deliberate deviation: the clone
  is synchronous and ignores `WaitMsBeforeAsync`, so instead of blocking on a
  stdin read it writes a `VEOF` (^D) — the reference backgrounds the command
  and only *eventually* reports the same `…:empty` result.
- **Path arguments are absolute.** `view_file.AbsolutePath`,
  `write_to_file.TargetFile` and `replace_file_content.TargetFile` all say "Must
  be an absolute path" in the capture, and a live turn read `/etc/hosts`, so
  reads are not workspace-bound (`readPath`); writes are (`workspacePath`, and it
  compares after symlink resolution, so `~/GitHub` and `~/Documents/GitHub` are
  one workspace). Never `filepath.Join(workspace, modelPath)` — it turns
  `/etc/x` into `<ws>/etc/x` with no error.

The declarations and result texts are 【一手】, read out of the request bodies the
`cmd/agy` wrapper captures (`~/.openagy/captures/`), and the shipped
`testdata/advertised_tools_1.3.6.txt` still holds the 60 advertised *names*.
Only `run_command` and `view_file` have a captured schema; the rest are still
cycle-98 reconstructions. Reports: `docs/cycles/cycle-275/deliverable.md`
(the surface), `docs/cycles/cycle-277/deliverable.md` (pty + CRLF/ANSI).

**Result texts are replayed, not remembered.** `internal/resultparity` parses
`internal/tools/testdata/result_texts_1.4.4.txt` (32 captured call/result pairs)
and replays each one against the real executors — 28 compared, 4 skipped with a
recorded reason, and a bijection test that fails if a section is neither. Run
`go test ./internal/resultparity/` after touching any tool result text. The
fixture has **no `run_command` section** — the most-used tool's text lives in
`docs/cycles/command-output-truncation/captures.md` and is pinned by
`internal/tools/command_output_truncation_test.go` instead. It calls
`Tool.Run`, not `Dispatch`, because `Dispatch`/`piagentbridge` add
`ApplyOutputBudget` on top (see below).

**That budget is a guess and contradicts the capture.** `DefaultMaxResultTokens
= 8000` (cycle 248, self-described as "the reference binary ships a
model-class-dependent value") with a `/4` chars-per-token estimate caps
model-visible text near 32 KB and wraps it in `_truncated: true` / `_preview: |`.
The 1.4.4 capture shows the reference handing the model a 46080-byte `read_file`
body whole, with no wrapper. Report: `docs/cycles/result-text-replay/deliverable.md`.

## Custom agents (`--agent`, cycle `custom-agents`)

`--agent <name>` selects a Markdown agent, not a built-in catalogue entry.
`internal/agent/list.go` discovers them from four sources and `ValidateAgent`
produces the reference's exact errors. Read it before touching the flag.

| Source | Root |
|---|---|
| Workspace custom | `<cwd>/.agents/agents/*.md` |
| User custom | `~/.gemini/config/agents/*.md` |
| Workspace plugin | `<cwd>/.agents/plugins/<plugin>/agents/*.md` |
| User plugin | `~/.gemini/config/plugins/<plugin>/agents/*.md` |

Frontmatter must carry non-empty `name` and `description`; `--agent` matches
the **name**, case-sensitively, and the body becomes the system prompt. An
`agents.json` beside `agents/` excludes by **file stem**, not name. Plugins
need a parseable `plugin.json`; user plugins also honor `config.json`
`plugins.<name>.enabled=false`. `default`/`""` is always accepted and is never
listed in `(available: …)`.

A custom agent replaces both prompt and tool surface:

- `internal/agyprompt.BuildAgent` emits `<System Prompt>\n<body>\n</System
  Prompt>` then `user_information, skills, messaging, artifacts, guidelines,
  communication_style`. No `identity`, `subagents`, `slash_commands`, or
  `conversation_transcript`; `sections/agent_messaging.txt` is the reduced
  messaging body; builtin skills are filtered out.
- Skills in both prompts are the builtin allowlist, `<cwd>/.agents/skills`,
  and plugin `skills/` (a parseable `plugin.json` is required; user plugins
  also honor `config.json`). The shared `~/.gemini/skills` library is
  `/skills`-only and must not reach the prompt.
- `internal/agenttools` holds the captured 1.4.3 declarations for exactly
  `send_message, view_file, read_url_content, search_web, schedule,
  generate_image, manage_task`. `Tools` adapts them to PiG; `Registry` is the
  filtered TUI catalogue. Print mode sets `AgentBody`, `ExtraTools`, skips the
  subagent services; the TUI sets `runMode.agentBody` and builds `BuildAgent`
  in `systemPrompt`.

Two measured traps: the seven tool declarations are byte-identical to the
reference, so do not "improve" their descriptions; and an unknown name on a
real TTY renders the in-TUI `⚠ Agent Not Found` card (orange header, `#586E75`
dim body), not a stderr line. Report: `docs/cycles/custom-agents/`.

## The transcript display (cycle 285)

`cmd/wagyu/tui_tools.go` renders what the model *did*, and 1.4.3's display differs
from the tool result in three ways that cost a cycle to measure. Evidence is
byte-level pty captures (`docs/cycles/cycle-285-tool-display/`), not the
`tmux capture-pane` string table:

- **Two forms per call.** Collapsed is a summary (`● Ran (cmd) (ctrl+o to expand)`,
  `● Explored 1 file (base)`, ` failed` appended on a non-zero exit, red bullet);
  expanded is the call (`● Bash(cmd)` — **no space** before the paren, `● Read(path)`)
  with the hint on the *last detail row*. Expanded bullets stay green even on
  failure; red is collapsed-only.
- **The detail body is not the result body.** `run_command` shows
  `Command exited with code N.` + blank + `Command output:` (or
  `No output from the command.`), never `Created At`/`Completed At`; `view_file`
  shows `Read N lines`, not the file.
- **Blocks are interleaved, one blank row around each.** Prose that precedes a
  call stays above its block; sequential calls separated by text are separate
  blocks; only calls batched in one assistant message share a block, and that
  block prints **one summary row per category** (`● Explored 1 file (a)` then
  `● Edited 1 file (a)`), keeping those rows above the per-call heads when
  expanded. `finaliseLiveTurn` re-renders each batch in place
  (`toolStep.batch`), and the spinner label is `Generating` → `Working` →
  `Running command`/`Reading file`, painted `#657B83` with a five-character
  `#268BD2` shimmer (`spinnerText`).
- **File writes and edits.** Collapsed is `● Edited 1 file (base)` for both a
  create and an edit (a create is *not* "Wrote"); expanded is
  `● Create(path)` / `● Edit(path)`. A create's detail is `Wrote N line(s)`; an
  edit's is a unified hunk (`+A / -B lines`, 3 context lines, `#EEE8D5` stripe
  on changed rows, `#DC322F` / `#859900` text). A **one-line read prints no
  detail row** (the hint lands on the head); two or more print `Read N lines`.
  The detail chevron `⎿` is `#586E75`, not the warning blue.
- **The in-flight row is the summary with a present-participle verb** and the
  amber bullet: `● Running (cmd)`, `● Exploring 1 search (q)`,
  `● Exploring 1 file (x)`. Searches collapse to `● Explored 1 search (q)` and
  expand to `● WebSearch(q)`; the spinner says `Searching web`. A failed
  non-command call reddens the bullet but prints no ` failed` — that word is
  command-only.

---

## Reference drift (`internal/referencedrift`, cycle `reference-drift`)

The parity claim in the README — "byte-level parity with `agy X.Y.Z`" — expires, and
Google's auto-updates expire it without asking. `internal/referencedrift` is what notices.

`reference.lock.json` records three things:

| Field | Meaning |
|---|---|
| `version` | the reference release the clone emulates. Must equal `cli.ReferenceVersion`; `internal/cli`'s `TestReferenceVersion_MatchesDriftLock` pins the two together. |
| `lag_acknowledged_through` | the newest upstream release we have **consciously declined** to re-evidence, with `lag_note` saying why. |
| `artifacts[]` | every version-scoped fixture: `path`, `captured_at`, `digest` (sha256), `live`, and `how` to re-verify. |

The gate's semantics are the non-obvious part, and they are deliberate:

- A **digest mismatch or missing artifact** is a bug in *this* repo. Always fails.
- The live binary **past** `lag_acknowledged_through` fails — nobody has looked at that release.
- The live binary **within** the acknowledged ceiling passes, but the report lists which
  artifacts are now suspect.

So the gate is **not** "live version must equal the pin". Lagging upstream is normal for a
reverse-engineering workspace, and a gate phrased that way goes permanently red on every
patch release, gets skipped, and recreates the exact false confidence it exists to remove.
Accepting drift is a committed decision with a `git blame`, not an env var.

`CompareVersions` orders numerically; `"1.4.10" < "1.4.9"` lexicographically, which would let a
patch release read as *older* than the pin and walk drift straight through the gate.

An artifact's `captured_at` is allowed to exceed `version` — the changelog is captured at 1.4.3
while the clone still emulates 1.4.2. The two fields answer different questions ("what do we
emulate" vs "when was this recorded"). What is forbidden is an artifact stamped from a release
*newer than the installed binary*.

When the reference moves:

```sh
go test ./internal/referencedrift/                          # see what is now suspect
agy changelog > internal/cli/testdata/changelog_reference.txt   # re-capture what you can
go test ./internal/referencedrift/ -run TestReferenceLock -update   # reseal the digests
```

`-update` is idempotent. It rewrites digests only — `captured_at` stays put, because moving an
artifact onto a newer reference is the decision the gate is meant to make loud.

Reports: `docs/cycles/reference-drift/deliverable.md`. Known still-unverified against 1.4.3:
`internal/agyprompt/testdata/capture_1.4.4.{txt,json}` (re-evidenced at 1.4.4) and
`internal/tools/testdata/advertised_tools_1.4.3.txt` (re-verified at 1.4.4).

---

## The `agy` wrapper (`cmd/agy`, cycle 274) — custom models + request capture

A **second product**, separate from the clone. It does not reimplement `agy`; it
launches the *real* binary with a local CloudCode proxy in front, so the official
`agy` can use models it does not ship with (first target: **MiniMax M3** via
`agw.k0s.io`), and so every request/response pair can be captured for reversing
prompts and tool-call formats.

The whole design rests on one measured fact: **`agy` 1.4.2 honours
`CLOUD_CODE_URL`.** Export it and every CloudCode RPC arrives at
`<base>/v1internal:<method>`:

```
$ CLOUD_CODE_URL=http://127.0.0.1:50999 agy models
POST /v1internal:loadCodeAssist
POST /v1internal:listExperiments
```

So the vendor binary is **never patched**. The TypeScript reference
(`kelvinzer0/antigravity-cli`) did patch it; this port deliberately does not —
`~/.local/bin/agy` is SHA-256-identical before and after.

```
cmd/agy              launcher: proxy up → CLOUD_CODE_URL → exec real agy → exit code
devutils/agybin      resolve the real agy (resolved-path + inode self-exec guard)
devutils/agyproxy    HTTP server: passthrough + fetchAvailableModels merge +
                     Gemini ⇄ provider translation + JSON capture
devutils/geminiwire  Gemini CloudCode wire types
internal/models      provider registry (contract unchanged; gained ToolSpec/ToolCall)
```

```bash
go build -o /tmp/agy-wrapper ./cmd/agy
/tmp/agy-wrapper                 # real agy, with registry models available
/tmp/agy-wrapper wrapper doctor  # agy path/version, registry, port, capture dir
/tmp/agy-wrapper wrapper models  # registry CRUD → ~/.config/openagy/models.json
/tmp/agy-wrapper wrapper capture on|off|dir
```

Five things that bite:

- **Only a literal first arg `wrapper` selects wrapper subcommands.** Everything
  else (`agy models`, `agy --version`, `agy -p …`) passes through untouched.
- **Proxy logs go to `~/.openagy/wrapper.log`, never stderr.** fd 2 is the
  terminal the TUI owns; writing request lines there smears the screen. Tail it
  with `agy wrapper log`; `OPENAGY_WRAPPER_LOG=stderr` is the debugging escape
  hatch (and does smear). An unwritable log falls back to discarding, not fd 2.
- **MiniMax M3 works out of the box**: first run creates
  `~/.config/openagy/models.json` with a single `agw` entry. Never overwrite it.
- Built from `./cmd/agy` the binary is named `agy`, i.e. on `PATH` it *is* a
  drop-in agy. The self-exec guard is what stops it recursing into itself.
- `internal/models` is shared with the clone; the cycle-274 additions are
  additive (`Tools` nil ⇒ byte-identical wire), but `go build ./cmd/wagyu`
  after touching it.

**Capturing** is the reverse-engineering use. Set `OPENAGY_CAPTURE_DIR` (or
`wrapper capture on`) and every pair is written as raw JSON — no re-encoding, so
prompts and tool schemas diff verbatim. One `-p` turn yields the full Antigravity
system prompt (~34 KB, `<identity>…`), all 13 tool declarations, and the raw SSE
frames. Files are unredacted by design; the directory is `0700`. Two RE findings
fell out of the captures: upstream `fetchAvailableModels.models` is a **map keyed
by model id** (not an array — the one contract deviation), and `agy` runs a
separate 520-char-prompt conversation-title call. A third came later: the ~34 KB
`<identity>…` prompt is the reference's system prompt, now reproduced byte for
byte in `internal/agyprompt` (cycle agy-system-prompt) — see
`docs/research/REFERENCES.md` for the external material it was checked against.

Known limits before trusting a capture: the Google path is location-blocked for
this account (403 passthrough only, so Google-turn fidelity is **unproven**),
images / `thought` / compaction are not forwarded, and the Anthropic/Google
providers in `internal/models` still ignore `Tools`.

Report: `docs/cycles/cycle-274/deliverable.md`. Usage: `cmd/agy/README.md`.

---

## Commands

### Go tool (`rea-go-seeds/`)

```bash
cd rea-go-seeds

# Build (verified working: exit 0, ~3 MB binary)
go build -o /tmp/pclnseeds .

# Generate seeds — writes <outdir>/seeds.txt and <outdir>/funcs.txt
go run . /path/to/stripped-binary /path/to/outdir
#   ~12 s for a 214 MB binary, no network, no dependencies

# Checks — there are NO tests and no CI in this repo
go vet ./...      # clean
gofmt -l .        # clean
```

- `go.mod` declares `go 1.26.2`, but the source is pure stdlib and **compiles fine on
  1.22.10** (proven in `out/restore-symbols.md` §2, and again with a from-source Go 1.27.2
  in `out/go-build-evidence/`). Do not “fix” the go directive — output is byte-identical
  (SHA-256 `e1504128…`) across 1.22.10 / 1.26.2 / 1.27.2-from-source. That
  reproducibility is a deliberate verified claim in this repo.
- With only one argument, output goes to the *current* directory, not next to the binary
  (the doc comment in `main.go` says otherwise — the comment is wrong, the code defaults
  `outdir = "."`).

### REA + Ghidra (historical; requires the gitignored toolchains)

```bash
npm install -g rea-agents@6.4.0        # bare install gets stale 4.0.1
export JAVA_HOME=<jdk21>
export GHIDRA_INSTALL_DIR=<ghidra_12.1.4_PUBLIC>
export REA_GHIDRA_SEED_FILE=$PWD/out/seeds.txt
rea doctor --provider ghidra --json     # must be all-green
rea analyze <binary> --provider ghidra --json
```

**`rea analyze` has a hard ~5.5-minute internal timeout** (`provider_timeout`,
`timeout_ms: 330000`) with no `--timeout` / `--max-wait` flag and no documented env var to
raise it (`out/ghidra-analyze.md` §8). On a 155K-function Go binary Ghidra needs
20–40 min, so **the REA wrapper cannot be used for full analysis**.

**Do not assume a bigger heap fixes the wrapper.** Three attempts were measured on this
machine, and none of them produced an answer:

| `GHIDRA_HEADLESS_MAXMEM` | Outcome |
|---|---|
| default `2G` | `binary_overview` failed after ~60 s with `Ghidra headless launch failed`; the bridge died and the run's **orphaned Ghidra JVM kept running for ~25 min** |
| `8G` (`rea analyze`) | ran until externally cancelled — never returned a result |
| `10G` (`rea --mcp`) | survived **621 s** (the seeds alone need ~10 min) then the MCP server exited `rc=0` without answering |

The real constraint is the wrapper's time budget versus the ~10 min that 154,525 seeds
take to apply, at any heap size. The orphan is the nastiest part: after the wrapper gives
up it leaves a JVM burning CPU and ~1.3 GB in
`/var/folders/*/T/rea-ghidra-*`. Check `pgrep -f AnalyzeHeadless` and clear it with
`/bin/kill -9 <pid>` (the shell's `kill` builtin is not available here) plus `rm -rf` on
the orphaned temp dir.

Bypass the wrapper entirely — this is the verified-working recipe:

```bash
cd /tmp                                  # neutral CWD, see gotcha 3 above
export JAVA_HOME=/opt/homebrew/opt/openjdk@21
export GHIDRA_INSTALL_DIR=~/tools/ghidra_12.1.4_PUBLIC
SEEDS=/Users/gear/GitHub/openagy/out/local-macos/seeds/seeds.txt
SCRIPTS=/Users/gear/GitHub/openagy/out/local-macos/ghidra-scripts
PROJ=/Users/gear/GitHub/openagy/out/local-macos/proj
mkdir -p "$PROJ"

# ① import + seed  (~11 min, measured)
MAXMEM=8G VMARGS="-XX:ParallelGCThreads=4 -XX:CICompilerCount=4" \
  "$GHIDRA_INSTALL_DIR/support/analyzeHeadless" "$PROJ" agy \
  -import ~/.local/bin/agy -noanalysis -max-cpu 4 \
  -scriptPath /opt/homebrew/lib/node_modules/rea-agents/bridge/ghidra \
  -preScript  ReaGhidraApplySeeds.java  "$SEEDS" \
  -postScript ReaGhidraNoReturnFix.java
# no -deleteProject: you want the project to persist for step ②

# ② decompile a hand-picked list  (56 s for 1,959 functions)
MAXMEM=8G VMARGS="-XX:ParallelGCThreads=4 -XX:CICompilerCount=4" \
  "$GHIDRA_INSTALL_DIR/support/analyzeHeadless" "$PROJ" agy \
  -process -noanalysis -max-cpu 4 \
  -scriptPath "$SCRIPTS" -postScript DecompilePriority.java
```

- **`-noanalysis` is the single most important flag.** Two separate reasons:
  1. Ghidra’s *Decompiler Switch Analyzer* decompiles every function to recover switch
     tables; on 155K Go functions that takes days.
  2. Importing with `-noanalysis` means **no disassembly pass has run when the seeds are
     applied**, so Go thunks cannot collide with instruction boundaries. The Linux/ELF
     pass in `out/` imported *with* analyzers and got **860/1498 = 57 %**, skipping 637
     functions as *"function X inside instruction at Y"*. The macOS pass got
     **2101/2101 = 100 %, 0 failures**. This is why the failure rate in `out/` is not a
     property of the binary.
- Omit `-deleteProject` on the import so step ② can `-process` the same project.
- `DecompilePriority.java` is **resume-safe**: it skips any function whose output file
  already exists and is >100 bytes, so re-running only decompiles what is new (the
  second run above took 13 s to add 142 functions).
- Heap for this machine (16 GB): `MAXMEM=8G`. The sandbox used `MAXMEM=2G` on 3 GB.

### MCP plan format

`rea-go-seeds/examples/plan-agy.jsonl` — one JSON object per line:

```json
{"label": "open", "tool": "open_binary", "args": {"path": "/abs/path", "provider_id": "ghidra"}}
{"label": "x", "tool": "procedure_pseudo_code", "args": {"procedure": "0x1028b6010"}}
```

`reamcp.py` writes each response to `./mcp/<NN>-<label>.json`, dumps the tool catalog to
`./mcp/tools.json`, and server stderr to `./mcp/server.err`.

**The addresses in the shipped `plan-agy.jsonl` are stale.** They belong to the build the
tutorial was written against; `agy` 1.3.4 shifts all of them (e.g. `waitForEligibility`
moved `0x10260fc60` → `0x10262f660`). Use `out/local-macos/build_plan.py`, which resolves
names → addresses from a fresh `seeds.txt` instead of hardcoding them:

```bash
python3 out/local-macos/build_plan.py out/local-macos/seeds/seeds.txt ~/.local/bin/agy \
  > out/local-macos/plan-local.jsonl
```

It prints any unresolved targets to stderr — that list must be empty.

---

## Architecture / data flow

```
stripped Go binary
      │
      ▼  ①  rea-go-seeds/main.go  loadPclntab()
   pclntab bytes + .text start vaddr
      │
      ▼  gosym.NewLineTable → gosym.NewTable → tab.Funcs
   155,184 {entry, name} pairs, deduped by entry, sorted by address
      │
      ├──────────────► seeds.txt   (0xADDR \t function \t NAME)   ← the commit-checked artifact
      │                funcs.txt   (ADDR SPACE NAME)
      │
      ▼  ②  REA_GhidraApplySeeds.java preScript (REA_GHIDRA_SEED_FILE)
   Ghidra program DB with ~155K named functions
      │
      ▼  ③  DecompilePriority.java postScript
   dec/<HEX>_<shortname>.c
      │
      ▼  ④  build_reports.py
   decompile-summary.md + 26 × decompile-<pkg>.md
```

A **second, independent path** produces the call graph (Subagent B in
`out/two-prong-attack.md` §4): `objdump -d` the `.text` → stream-parse `call <addr>`
instructions → bisect against `seeds.txt` addresses for function boundaries →
`call-graph.json` / `call-graph.dot` / `xrefs.txt`. This bypasses Ghidra entirely.

### Stage ① — `loadPclntab` (the only non-obvious code in the repo)

Tries container formats in order: Mach-O → ELF → PE.

- **Mach-O**: `__gopclntab`, then `__lrodata_gopcln` (Google-internal toolchain rename).
- **ELF**: `.gopclntab` first; if absent, scan `.lrodata` / `.rodata` / `.data.ro` /
  `.text` for the Go 1.20+ pclntab magic `f1 ff ff ff 00 00`. **The ELF fallback is a
  local patch**, not upstream — upstream only handled the Mach-O rename, which is why
  `gore` and `redress` find nothing here. Preserve it.
- **PE**: scan `.rdata` / `.text` for the same magic.

**The second argument to `gosym.NewLineTable` must be the `.text` start address, not the
pclntab address.** The ELF branch computes `pclnVaddr`, uses it to locate the section, then
explicitly discards it (`_ = pclnVaddr`) and passes `.text.Addr`. Passing the pclntab base
yields silently wrong PCs.

Filters applied to `tab.Funcs`: skip empty names, names containing space/tab/NUL, names
longer than 2000 chars, and duplicate entry addresses (Go emits `.deferwrap1/2`,
`func1`, thunk aliases at shared addresses).

### Stage ③ — `DecompilePriority.java`

Ghidra `GhidraScript`; **class name must match the filename**. Reads `priority-funcs.txt`
(same TSV as seeds.txt), 60 s per-function timeout, writes `dec/<HEX>_<stem>.c` where
`stem` = everything after the last `.` in the Go name, non-alphanumerics → `_`,
truncated to 80 chars.

Resume-safe: a function whose output file already exists and is `> 100` bytes is skipped,
and every line is processed independently, so a crash keeps partial progress.
`_progress.txt`, `_errors.txt`, `_summary.txt` are written alongside.

### Stage ④ — `build_reports.py`

Package split heuristic: if the name contains `(`, the package is everything **before the
first `(`** (covers `pkg.(*Type).Method`); otherwise everything before the **last `.`**
(covers `pkg.Func`). Recounts by scanning `dec/` for `.c` files and matching the hex
prefix — it does not trust `_summary.txt`.

---

## Data formats

| File | Format |
|---|---|
| `out/seeds/seeds.txt`, `out/decompile/priority-funcs.txt` | `0xADDR<TAB>function<TAB>NAME`. REA seed format: ≤ 1,000,000 entries, ≤ 32 MiB, names must contain no whitespace. Also accepts `label` / `code` kinds; this project only emits `function`. |
| `out/seeds/funcs.txt` | `ADDR NAME` — no `0x` prefix, single space. |
| `out/decompile/xrefs.txt` | `from_addr<TAB>from_name<TAB>to_addr<TAB>to_name<TAB>count`, `#` comment lines allowed. |
| `out/decompile/call-graph.json` | `{metadata{...}, nodes[{addr,name,package,in_priority}], edges[]}`. Renderable with Graphviz via `call-graph.dot`. |
| Decompiled `.c` header | `// Source: 0x<hex>  <name>` / `// Decompiled at: <date>` / `// Signature: <sig>` |
| `out/*.json` (e.g. `ghidra-analysis.json`) | Raw REA `--json` error envelopes — `{"error","code","category","retryable","remediation","details"}`. These are **failure logs**, not results. |

**Sanity check on any seed work:** the canonical output is 155,184 lines / 18,013,123 B
with SHA-256 `e1504128b7661974855e2f4009a4c6c26ee53e2f64ba587bb7b8cf7b8ba8aa3a`, recorded in
`out/seeds/SHA256`. If your run differs, something about the pclntab location changed.

---

## Gotchas

**Analysis fidelity (Go-specific, all recorded in `out/deep-analysis-v3.md`)**

- **A static call graph sees ~3 % of the real control flow.** Goroutines, channels and
  interface dispatch mean *0 static callers ≠ dead code* — usually it means
  `runtime.newproc` or a func-pointer call. `out/decompile/auth-flow.md` states this
  explicitly and reports both what disassembly shows and what the name implies.
- `out/deep-analysis-v3.md` contains a **v2→v3 correction section** that retracts earlier
  claims in `out/qa-report.md` (notably the 30 s/60 s timeout attribution and the
  usefulness of the call graph). When the two disagree, **v3 is authoritative**.
- ~1.5 % of seeds fail in Ghidra with `function X inside instruction at Y` — Go thunks
  share an address with their target and Ghidra cannot disambiguate. Those functions never
  exist in the DB, so `DecompilePriority` reports them as `SKIP no function at`. 637 of
  1,498 priority funcs skipped for this reason. **This is expected, not a script bug.**
- Timeout constants are format-string parameterized (`…did not settle within %s`), so
  `strings` will never show “30” or “60”. You have to find the immediate in the
  disassembly (`0x2faf080` = 50 ms) or read the decompiled control flow.
- Binary strings are frequently truncated mid-token (`AGY_CLI_DISABLE_AUTO_UPDATEF`, flags
  like `--approv`). Flag and env-var names recovered this way are inferred, not exact.

**Tooling**

- Ghidra’s `search_strings` only indexes NUL-terminated `Data`; most Go strings are
  absent. Use `strings(1)` on the binary for anything string-shaped and cross-reference.
  This is why `out/urls-google.txt`, `out/models-gemini.txt`, `out/grpc-paths.txt`,
  `out/gopclntab-strings.txt` exist alongside the REA JSON.
- Ghidra sometimes resolves rodata string addresses to a blob of neighbouring data in the
  decompiler window. Read the file manually at that offset. For the Mach-O target
  `addr − 0x100000000` = file offset holds exactly.
  **Slice by the emitted length, not to the next NUL.** Ghidra writes `&UNK_<vaddr>,<len>`
  and Go's linker concatenates every short string into one blob, so reading to a NUL
  returns the neighbouring strings as garbage — see `out/local-macos/resolve_strings.py`.
- Only *short* literals reach the decompiler this way. Long user-facing strings reach the
  code through Go's `concatstring` call path and never appear as `UNK_` refs; `strings(1)`
  is the tool for those. **Cross-check both** — a finding from one that the other does not
  confirm is weak.
- REA’s own `search_strings` missing a string does not mean the string is absent.
- `inspect-go-binary` is documented upstream but **not shipped in npm 6.4.0** (main-branch
  only). Read docs against the installed version, not the website.
- Ghidra official zips ship **no macOS native decompiler**. You must build it:
  `cd $GHIDRA/support/gradle && JAVA_HOME=<jdk21> ./gradlew buildNatives` (~100 s) →
  `Ghidra/Features/Decompiler/build/os/mac_arm_64/decompile`.
- REA supports only Node 22.x (≥22.19) / 24.x (≥24.11) / 26+. Node 25.x makes
  `rea doctor` report `missing_dependency` even though the CLI runs.
- `observe-native-calls` sets breakpoints **by symbol name**, so it is useless on a
  stripped Go binary unless you re-symbolize first.

**Environment (sandbox-specific, from `out/prep-env.md`)**

- `github.com` is network-blocked there; `codeload.github.com/<owner>/<repo>/tar.gz/main`
  works. Mirrors that did work: `mirrors.aliyun.com/golang`, `mirrors.tuna…/Adoptium`,
  `registry.npmmirror.com`. Tuna has no `/golang/`; Huawei's `/golang/` returns 401;
  Aliyun has no `/Adoptium/`.
- A 30 s bash timeout truncated both `npm install -g` and the Go tarball extraction on
  first attempt (Go lost 65 of 72 stdlib packages, breaking every later `go run`).
  Extract to `/tmp` (tmpfs) first, then `cp -a` to the NAS mount, and
  `chmod -R a+rX` afterwards.
- Keep `<tool>/<tool>-<version>/*` flattened into `<tool>/` root. A `current` symlink
  breaks the expected contract paths.

---

## Conventions

- **Zero third-party dependencies** is a stated constraint for the research
  tools (`out/two-prong-attack.md` §9), but only the narrow ones:
  - `rea-go-seeds/` Go code: stdlib only (see `rea-go-seeds/go.mod` — single
    `module pclnseeds` line, no require block).
  - `out/decompile/build_reports.py` and `out/local-macos/*.py`: Python stdlib
    only.
  - `out/local-macos/ghidra-scripts/*.java`: Ghidra API only.
  The clone itself (`cmd/wagyu/`, `internal/`) does pull third-party deps —
  the charm.land TUI stack (`bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`,
  `x/ansi`, `x/term`, `ultraviolet`, `colorprofile`, `go-runewidth`,
  `cancelreader`) for the TUI, and `github.com/ncruces/go-sqlite3` as the
  read-only SQLite reader in `internal/convstore` (replaces a hand-rolled
  B-tree reader in cycle 175; see `docs/cycles/cycle-175/`) — and since cycle
  183 also `github.com/MichaelKinsy/PiG` for the agent loop. Treat the
  no-deps rule as scoped to the research artifacts above, not the runtime
  binary.
- **Evidence grading** used throughout the reports — keep it when adding findings:
  【一手】first-hand verified against the binary · 【反编译】from decompiled output at a
  confirmed address · 【strings】string-table hit only · 【推测】hypothesis.
- **Report language follows the file being edited**: `out/*.md` is zh-CN;
  `out/decompile/*.md`, `out/go-build-evidence/*` and `rea-go-seeds/README.md` are English.
  Mixed-language commits are normal here.
- `out/` reports end with a 硬约束 / 存疑 / limitations section enumerating what was
  *not* verified. New reports follow that structure — do not omit it.
- Go style: tabs, standard library naming, `error` strings lowercase. `rea-go-seeds/`
  is a single-file `main` package; the clone (`cmd/`, `internal/`) is the opposite —
  26 packages, so a change there usually spans several files.

---

## Where to start

**Working on the clone (`cmd/` + `internal/`)** — 26 packages, 748 `Test*`:

| If you want to… | Read / run |
|---|---|
| Build and test it | `go build -o /tmp/wagyu ./cmd/wagyu` then `go test ./cmd/... ./internal/...` (27 packages ok) |
| Understand the agent loop | the **The agent loop** section above, then `internal/pigrt/runtime.go` (print) and `internal/agent/pig.go` (TUI) |
| See the loop run end to end | `go run ./devutils/pigloop-demo -mode loop` (scripted gateway, no network) |
| Touch the agw transport | `internal/piagentbridge/agwprovider.go` + `internal/backend/agw_k0s.go` — the two must stay behaviourally identical |
| Know what the last cycle changed | the newest `docs/cycles/cycle-<n>/` report; `git log --oneline -20` |
| Reproduce what `agy` actually prints | the cycle-report table pattern of probe-vs-clone; `internal/cli/testdata/changelog_reference.txt` holds recovered strings |
| Match the reference's system prompt | `internal/agyprompt` (`agyprompt.go` + `sections/*.txt`); ground truth is `testdata/capture_1.4.4.txt`. Report: `docs/cycles/agy-system-prompt/` |
| Change what the model can call, or how a tool behaves | `internal/tools/catalog.go` (the surface) then `command.go` / `filesystem.go`; the reference's own declarations are in the wrapper captures. Report: `docs/cycles/cycle-275/` |
| Find an external reference | `docs/research/REFERENCES.md` (prompts, proxies, model lists) |

**Wrapping the official `agy` with custom models (`cmd/agy`, cycle 274)** — see
**The `agy` wrapper** section above for the mechanism:

| If you want to… | Read / run |
|---|---|
| Run official agy with registry models | `go build -o /tmp/agy-wrapper ./cmd/agy` then `/tmp/agy-wrapper` |
| Add a model / provider | `/tmp/agy-wrapper wrapper models add …` → `~/.config/openagy/models.json` |
| Capture requests for prompt/tool RE | `OPENAGY_CAPTURE_DIR=/tmp/cap /tmp/agy-wrapper -p "…" --model <slug>` |
| Understand the redirection (no patching) | **The `agy` wrapper** above — `CLOUD_CODE_URL` |
| Know what is *not* covered | `docs/cycles/cycle-274/deliverable.md` → 硬约束 / 存疑 / limitations |
| Know whether the reference binary has moved | `go test ./internal/referencedrift/` — reports which evidence is stale and how to re-capture it |
| Re-evidence a fixture after an `agy` release | `internal/referencedrift` 的 lock 流程: re-capture, then `-update` to reseal the digests |

**On this machine (macOS research pipeline), start here** — already configured and
run end to end:

| If you want to… | Read / run |
|---|---|
| See what was found in *your* agy build | `out/local-macos/FINDINGS.md` |
| Set the environment | `source out/local-macos/env.sh` |
| Re-derive the 154,525 names | `cd rea-go-seeds && go run . ~/.local/bin/agy ../out/local-macos/seeds` |
| Change which functions get decompiled | edit `PACKAGES` in `out/local-macos/build_priority.py`, re-run it, then re-run `analyzeHeadless -process` (incremental, ~13 s) |
| Rebuild the MCP query plan for a new agy | `out/local-macos/build_plan.py` (addresses shift on every release) |
| Watch a run | `tmux new-session -d -s reagy -c /tmp` then paste the ①/② `analyzeHeadless` block from **Commands** above. Do **not** use `run-re.sh` — it drives the REA MCP path, which does not complete on this binary (see the wrapper table). |

**Historical / reference material:**

| If you want to… | Read |
|---|---|
| Understand the tool | `rea-go-seeds/README.md`, then `main.go` (175 lines, the whole thing) |
| Understand the full RE story | `rea-go-seeds/docs/TUTORIAL.md` (zh-CN, macOS/Mach-O case) |
| Understand the Linux/ELF pipeline | `out/restore-symbols.md` → `out/ghidra-analyze.md` → `out/decompile/deliverable.md` |
| Understand the analysis methods and their limits | `out/deep-analysis-v3.md` (read the corrections section), `out/decompile/auth-flow.md` |
| See what was concluded on the ELF build | `out/qa-report.md` (zh-CN, superseded on some points), `out/decompile/decompile-summary.md` |
| Write a new Ghidra script | `out/local-macos/ghidra-scripts/DecompilePriority.java` |
| Drive REA programmatically | `rea-go-seeds/examples/reamcp.py` + a plan from `build_plan.py` |

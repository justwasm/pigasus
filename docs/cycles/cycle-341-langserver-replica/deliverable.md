# `cmd/langserver` — a 1:1 replica of the Antigravity IDE's `language_server`

**Cycle:** `cycle-341-langserver-replica`
**Touches:** `cmd/langserver` (new), `internal/lsserver` (new),
`internal/rchost/remote_control_standalone.go` (new),
`internal/remotetransport/tcp.go`, `internal/remotetransport/tcp_auth.go`,
`internal/remotetransport/tcp_server.go`, `internal/rcfrontend/frontend.go`
**Report language:** English (like the other `docs/cycles/` reports)
**Evidence:** `out/langserver/` — zh-CN, the raw captures

---

## 0. Summary

The repository had a *documented* IDE language server and no binary.
`docs/guide/ide-language-server.md` described the reference's spawn recipe,
its `--stamp` contract and its port reporting; `out/ide-language-server/`
held the IDE's own JavaScript. What it did not hold was a `language_server`
that answers like the reference.

This cycle adds one: `cmd/langserver`, built from the repository's existing
packages only, speaking the same `exa.language_server_pb.LanguageServerService`
the IDE's bundle speaks.

The reference is the oracle:

```
/Applications/Antigravity.app/Contents/Resources/bin/language_server
```

161,803,376 bytes, built 2026-10-23. Every claim below was measured against
that binary on this machine, side by side with the clone, over the same
interface. The evidence is in `out/langserver/`; the grades are 【一手】
(first-hand, against the binary), 【反编译】, 【strings】, 【推测】.

**The headline:** in the IDE's own mode the clone's answers are **byte-identical
to the reference's for all fifteen RPC methods**, and the flag surface is
byte-identical for all fifteen argument-parsing cases.

| | reference | clone | verdict |
|---|---|---|---|
| `--help` | 68 flags | 68 flags | identical |
| flag parsing (27 cases) | — | — | **27/27 identical** (24 byte-identical, 3 parse-identical — the run itself differs) |
| `--stamp` | 4 lines | 4 lines | identical |
| RPC answers, standalone (15 methods) | — | — | **15/15 identical** |
| RPC answers, hub (15 methods) | — | — | 10/15 identical, 5 account-shaped |
| the whole service (351 methods) | 153 answered, 186 refused | — | **312 answered, 0 × "Method not found"** |
| web UI page | 3866 B | 3866 B | identical modulo the token |
| token gate (401 body, channels) | — | — | identical |
| on-disk layout | — | — | identical modulo `.checksum` |

---

## 1. The flag surface

The reference prints Go's `flag` package default output, so the surface is
reproducible exactly — the same names, the same defaults, the same usage
strings. The clone registers 68 flags in `internal/lsserver/flags.go` and
prints them itself (`internal/lsserver/help.go`) because Go 1.27's
`flag.PrintDefaults` uses the multi-line format and the reference uses a
one-line format:

```
  -analytics_server_url="": Analytics server host
  -cdp_port=9222: Port for Chrome DevTools Protocol
  -model_api_client_type=ccpa: Which model client to use: ccpa or gemini. Defaults to ccpa.
```

String defaults are quoted — **except** `model_api_client_type`, which the
reference registers with its own flag type and which therefore stays bare.
That one exception was measured, not guessed.

### The hidden flags

The IDE passes three flags the reference accepts and does **not** list:

| flag | what it does | the clone |
|---|---|---|
| `--enable_sidecars` | the sidecar switch | accepted; its value decides whether the `sidecars` block carries `allowAll` |
| `--host_bridge_url` | the IDE's main-process bridge | accepted, ignored |
| `--host_bridge_token` | the bridge's token | accepted, ignored |

```
--enable_sidecars absent → {"sidecars":{"enabled":true,"allowAll":true},…}
--enable_sidecars present → {"sidecars":{"enabled":true},…}
```

The clone carries them through the **same scan** as the visible flags and
removes them from the vector it hands the visible flag set — that reproduces
both halves of the behaviour: the flag is accepted, and `--help` does not list
it. The sidecar flag's value is kept, because that flag changes one thing.

That the two host-bridge flags exist at all was **not** inferred from the
help: the live IDE on this machine spawns the reference with

```
language_server --standalone --override_ide_name antigravity --subclient_type hub \
  --override_ide_version 2.26.0 --override_user_agent_name antigravity \
  --https_server_port 0 --csrf_token <uuid> --app_data_dir antigravity \
  --api_server_url https://generativelanguage.googleapis.com \
  --cloud_code_endpoint https://daily-cloudcode-pa.googleapis.com \
  --enable_sidecars --host_bridge_url=http://127.0.0.1:56307 --host_bridge_token=<hex>
```

and the reference starts normally with them — so they are defined but
hidden. The clone would have answered `flags provided but not defined` and
exited 2 without them. The capture is `out/langserver/spawn-args.txt`.

---

## 2. The parser is not Go's `flag` package

This is the part that costs the most if it is wrong, because the IDE reads
the exit code. The reference's parser:

| case | reference | Go's `flag` |
|---|---|---|
| `--help` | exit **2** | exit 0 |
| unknown flags | `flags provided but not defined: -bogus1 -bogus2` | `flag provided but not defined: -bogus1` (singular) |
| a bad int value | `invalid value "notanumber" for flag -cdp_port: strconv.ParseInt: parsing "notanumber": invalid syntax` | same text |
| a bad bool value | `invalid boolean value "maybe" for  -standalone: …` (two spaces) | `invalid boolean value "maybe" for -standalone: …` |
| `--help <pattern>` | prints only the flags whose name contains the pattern | not supported |
| a pattern that matches nothing | `flag: no flags match help pattern "CDP_PORT"` | not supported |
| after a parse error | `error during flag parsing, exiting cleanly with status 2...` | nothing |

Two rules are easy to get wrong and were measured carefully:

- **A value error wins over the collected unknown flags.** `-cdp_port=bad --bogus`
  reports the value error, not the unknown flag.
- **The pattern match is a case-sensitive substring.** `--help port` prints the
  four `*port*` flags; `--help CDP_PORT` prints nothing and complains.

### The hidden flags take part in the same scan

They are hidden — the usage does not list them — but they are **not** parsed
separately. The evidence:

| case | reference | why it matters |
|---|---|---|
| `--cdp_port=bad --enable_sidecars=maybe` | the *int* error | the complaint is reported at the flag's position |
| `--bogus1 --enable_sidecars=maybe` | the *bool* error | an unknown flag is only collected, and the collection is reported at the end |
| `--enable_sidecars=maybe --bogus1` | the *bool* error | same, in the other order |
| `foo --enable_sidecars=maybe` | **no complaint at all** | the scan stopped at the positional argument, so the hidden flag behind it was never seen |
| `--enable_sidecars true --stamp` | the LS starts | a boolean flag takes no value: `true` is a positional argument |
| `--host_bridge_url http://x --stamp` | the LS starts | a string flag takes the next argument |
| `--host_bridge_url` | `flag needs an argument: -host_bridge_url` | a string flag needs its argument |
| `--host_bridge_url --stamp` | the LS starts | the next argument is the value, flag-looking or not |
| `--enable_sidecars=maybe` | `invalid boolean value "maybe" for  -enable_sidecars: …` | the hidden boolean is checked like any other |

So the clone's scan carries the hidden flags and the visible ones together
(`internal/lsserver/args.go`), and hands the visible flag set a vector with
the hidden flags removed (`Args.Rest`).

`internal/lsserver/args.go` implements the scan; `internal/lsserver/flags_test.go`
and `internal/lsserver/args_test.go` pin the table — 27 cases, all measured
against the reference (`out/langserver/flags.txt`).

---

## 3. The answers

The reference's answers are **hand-built JSON with a fixed key order** — not a
Go map, not a protobuf field order. The clone builds them the same way
(`internal/lsserver/answers.go`).

Standalone:

```json
{"config":{"sidecars":{"enabled":true,"allowAll":true},"standalone":true,"appDataDir":"antigravity-ide","maxNumTrackedWorkspaces":10,"configDir":"config","sandboxAvailable":true}}
```

Hub (the IDE's mode) — `antigravityHub` is inserted after `appDataDir`,
`subclientType` after `maxNumTrackedWorkspaces`:

```json
{"config":{"sidecars":{"enabled":true},"standalone":true,"appDataDir":"antigravity-ide","antigravityHub":true,"maxNumTrackedWorkspaces":10,"subclientType":"hub","configDir":"config","sandboxAvailable":true}}
```

The whole catalogue is in `out/langserver/answers.json`, both modes, fifteen
methods, with the dynamic values normalised (`resetTime`, the tracing id,
the instance id, the gemini directory).

### Three places where the answer follows a flag

**The `antigravityHub` marker is switched by the IDE's name.** The reference
adds it — and reports a `-v2` instance id — only when
`--override_ide_name` is exactly `antigravity` *and* the subclient type is
`hub`. The user agent name is not the switch, and the name is
case-sensitive. Measured:

| flags | `antigravityHub` | instance id |
|---|---|---|
| `--subclient_type hub --override_ide_name antigravity` | yes | `<uuid>-v2` |
| `--subclient_type hub --override_ide_name Antigravity` | no | `<uuid>-v1` |
| `--subclient_type hub --override_ide_name foo` | no | `<uuid>-v1` |
| `--subclient_type hub --override_user_agent_name antigravity` | no | `<uuid>-v1` |
| `--standalone --override_ide_name antigravity` | no | `-v1` |

**The sidecar block has three shapes**, and no single flag decides it:

| condition | block |
|---|---|
| no subclient type, no `--enable_sidecars` | `{"enabled":true,"allowAll":true}` |
| `--enable_sidecars` | `{"enabled":true}` |
| a subclient type, no `--enable_sidecars` | `{}` |

**The instance id** is the process's own id for the `cli` and the `hub`
subclients; the standalone server and the `sdk` subclient report none, and
their answer is the bare `-v1`.

### The mocked catalogue

`--use_mocked_data=true` makes the reference serve a fixed catalogue:
`Mock User`, `mock-user@google.com`, three model labels
(`gemini-3.1-pro`, `gemini-3.1-ultra`, `claude-4.5-opus`), a
`MODEL_PLACEHOLDER_M*` model alias per label, a `quotaInfo.resetTime` of
*now + 4h* in UTC with six-digit microseconds, and a per-call tracing id in
the `unimplemented` answer. The clone reproduces it byte for byte
(`internal/lsserver/mocked.go`); the only difference is the timestamp, which
the reference also moves on every call.

---

## 4. The web UI page

The reference serves a **3866-byte page** whose title is `Antigravity` — the
IDE's page, not the CLI's (`Jetski Web`). The clone's embedded asset is the
CLI's page, so the clone has to produce the IDE's:

- the title is rewritten;
- the configuration script is the reference's one-line form, injected
  directly under `<head>`:

```html
<script>window.__APP_CONFIG__ = {"productName":"antigravity-ide","csrfToken":"…","appVersion":"2.26.0","releaseChannel":"stable","devMode":false};</script>
```

- the touch patches and the account switcher — both the clone's own
  additions — are off.

The page's `appVersion` is the `--override_ide_version` value, and
`"0.0.0.0"` when that flag was not passed — *not* the language server's own
version (`1.11.0`, which is what the log line reports).

`internal/rcfrontend` grew one option, `ReferencePage`, for this. The
reference's `/main.js` and the clone's embedded `main.js` are byte-identical
(10,461,461 bytes): the bundle was already the reference's.

The reference serves the page **only** in the hub mode; every other mode
answers 404 on every path but the service itself.

---

## 5. The token gate

The reference reads the secret from **exactly one channel** — the
`x-codeium-csrf-token` header. The cookie, `Authorization: Bearer` and the
`token` query parameter are all *not* channels; each of them is answered
`unauthenticated: missing CSRF token`. The clone's transport reads four
channels (the remote-control page and the cloud tunnel need them), so the
transport grew `ReferenceTokenGate` to narrow it back to the reference's
one.

The denied answer is the Connect-RPC error body, and it carries **no**
`WWW-Authenticate` header:

```
HTTP/1.1 401 Unauthorized
Content-Type: application/json
Content-Length: 74

{"code":"unauthenticated","message":"unauthenticated: missing CSRF token"}
```

A wrong secret gives `… invalid CSRF token`. Both are byte-identical to the
reference's.

The page and `/healthz` are **not** gated — the IDE loads the page from
`https://127.0.0.1:<port>/` without a secret. The service calls are gated
for every peer, **loopback included**. The transport grew
`AllowUnauthenticatedWebUI` for the first half of that.

---

## 6. The on-disk layout

Identical, except for the `.checksum` value (the reference hashes its
embedded archive, the clone hashes the tree it installed):

```
<gemini_dir>/antigravity-ide/{brain,builtin,conversations,crashes,daemon}/
<gemini_dir>/antigravity-ide/builtin/{.checksum,keep.txt,skills/}
<gemini_dir>/antigravity-ide/installation_id
<gemini_dir>/antigravity-ide/daemon/ls_<sha256(workspace_id)[:16]>.json
<gemini_dir>/config/{.migrated,mcp_config.json}
```

The discovery file has a two-space indent and **no trailing newline**; so
does `installation_id` (a UUID). The nine bundled skill directories have the
same names on both sides.

One real bug was found and fixed while doing this: the clone's
`prepareAppDataDir` joined the *absolute* daemon path onto the app data
directory, producing `<app-data>/<app-data>/daemon`. The clone still wrote
the discovery file to the right place — `WriteDiscovery` creates its own
directory — so the bug was invisible from the outside.

---

## 7. The startup log

The clone reproduces the reference's glog line prefix and the reference's own
`file.go:line` positions:

```
ERROR: logging before google.Init: I1027 04:47:41.335291       1 server.go:1710] Starting language server process with pid 75710
```

`ERROR: logging before google.Init: ` is the reference's own glog complaint
about logging before `google.Init` — the clone copies it, because the
reference copies it from its own upstream.

The port wording follows the flag: a port the operator asked for is
`fixed port at N`, a port the OS picked is `random port at N`. The reference
says so, and the clone now says so too.

The clone reproduces **nine** of the reference's startup lines, with the
reference's own `file.go:line` positions — `server.go:1710`, `server.go:513`,
`server.go:1766`, `server.go:672`, `server.go:687`, `server.go:695`,
`migrate.go:50`, `migrate.go:158`, `server.go:2965`, `server.go:2789`. The
reference also prints lines the clone does not fabricate: the background
polling of the account and the network (`cache.go`, `errorreport.go`,
`auth_provider.go`), and its own components (`defaults.go:732`,
`manager.go:108`, `server.go:3249`, `server.go:3256`, `server.go:4095`,
`server.go:3741`, `profiler.go:231`).

Two differences remain and both are benign: the migrate lines carry the
thread id of a background goroutine in the reference (a different number
every run) and the main thread in the clone, and the startup takes 6.4s in
the reference's hub mode (it waits for the account) against 168ms in the
clone. The side-by-side capture is `out/langserver/startup.txt`.

---

## 8. What is *not* reproduced

### The stdin metadata read — 【一手】

The reference, **without** `--standalone`, reads its initial metadata from
stdin before it does anything else:

```
ERROR: logging before google.Init: F1027 04:46:24.812226       1 main.go:326] Language server failed - Failed to read initial metadata from stdin: %!s(<nil>)
```

(EOF on stdin → `log.Fatalf`, with a Go stack trace and `SIGABRT`.) With
stdin held open it **blocks** there: the first two log lines appear and then
nothing. With `--standalone` the read does not happen at all.

The IDE always passes `--standalone` and closes stdin immediately
(`out/ide-language-server/ide-js/dist/languageServer.js`: "Close stdin
immediately — the LS may block waiting for metadata on stdin."), so the IDE
never sees this.

The clone does not read stdin: in the non-standalone mode it starts
immediately. **Accepted difference.** The metadata's *format* is unknown —
the IDE never writes it and the reference only dies without it — so
reproducing it would need a decompile of `setUpMetadataProvider`
(`server.go:575`). 【推测】

### The hub mode's account-shaped answers

Five methods differ in hub mode, all for the same reason: the reference is
backed by a live account and the clone is not.

| method | reference | clone |
|---|---|---|
| `GetMendelFlags` | the language server's own flag set | the remote-control flag set |
| `GetAuthStatus` | nine granted scopes | the clone's own project/location |
| `GetUserStatus` | the account's `planStatus` | the clone's own model list |
| `GetAvailableModels` | the account's model table | `{}` |
| `GetCascadeModelConfigData` | the account's tiered configs | `{}` |

The clone does not fabricate a plan status or a catalogue it does not have.

### The transport's catch-all

A POST to a method that does not exist answers `200 {}` on the clone and
`404` on the reference; a GET answers `405` on the clone and `404` on the
reference. That catch-all is deliberate — see the load-bearing invariant
about the Connect dialect's unnamed failures: the page calls methods the
clone does not implement, and those must not take the page down. It is
**not** changed for this surface.

---

## 9. The IDE shell, running on the clone

The clone is a *server*. The question this section answers is whether the
real IDE can be pointed at it — not through the clone's own web UI, but
through the IDE's own Electron shell.

It can. `scripts/install-ide-shell.sh` copies the IDE app, changes its
identity, swaps `Contents/Resources/bin/language_server` for the clone and
re-signs the result. The copy is isolated from the original:

| | original | Pigasus |
|---|---|---|
| bundle identifier | `com.google.antigravity` | `io.github.justwasm.pigasus` |
| product name (`app.getName()`) | `Antigravity` | `Pigasus` |
| language server data dir | `~/.gemini/antigravity` | `~/.gemini/pigasus` |
| Electron `userData` | `~/Library/Application Support/Antigravity` | `.../Pigasus` |

The original is only ever read: its signature still verifies, its shipped
`language_server` is byte-for-byte untouched, and its own instance kept
running through the whole experiment.

Four macOS rules had to be found to make it launch; each one killed a
launch first 【一手】:

1. **The resource seal is enforced at exec time.** Editing `Info.plist`,
   `app.asar` or the language server binary invalidates the main
   executable's signature and the kernel kills the process
   (`amfid: The signature on the file is invalid`, Code=-420). The bundle
   has to be re-signed.
2. **The hardened-runtime flag has to go.** With it set, library
   validation rejects the Electron Framework (`mapping process and mapped
   file (non-platform) have different Team IDs`). The entitlements are
   kept; the flags are not.
3. **The helper apps are found by name.** Electron 44 builds
   `<productName> Helper.app/Contents/MacOS/<productName> Helper`
   (`shell/app/electron_main_delegate_mac.mm`), so all four helper bundles
   and their executables have to be renamed with the product.
4. **The language server's data directory is passed by the IDE**, as
   `--app_data_dir <app.getName().toLowerCase()>` — so isolation means
   repacking `app.asar`'s `package.json`, not merely renaming the bundle.

What the shell then does, all measured: it spawns the clone with the
**same argument vector** it uses for the reference, reads the port out of
the clone's log with its own regex, and loads `https://127.0.0.1:<port>/`.
**The whole workbench — 10,461,461 bytes of `main.js` — is served by the
language server**; the IDE's JavaScript contains no agent logic at all.

Live result: startup, the port handshake, the page, the bundle, the CSRF
gate, the project list, the sidebar and the model picker all work, and so
does the **agent** — a message sent from the workbench is answered and
rendered.

> **Correction (2026-10-27).** This section used to say that "the clone
> answers 15 of the reference's 351 `LanguageServerService` methods and
> none of them is a cascade method". Both halves were wrong:
>
> - **15** is the number of answers compared **byte for byte**
>   (`answers.json`), not the number of methods implemented. The cascade
>   methods have been in `internal/remotecontrol/dispatch_ls.go` all
>   along — they are why a message sent from the workbench gets an
>   answer at all.
> - The real gap was different: the workbench asks **23 methods** while
>   the page loads, and the clone did not answer a batch of them, so
>   each one became a `-32601` in the console
>   (`skills_fetch_failed`, `Failed to fetch admin controls`). The next
>   section measures the whole surface and closes that gap.

Evidence: `out/langserver/pigasus-app.txt`; the guide's section "The IDE
shell, running on the clone"; the script: `scripts/install-ide-shell.sh`.

---

## 10. The 351 methods, measured

The service has **351 methods**. The clone answers **all 351** — nothing
comes back as `-32601` "Method not found".

Two facts made the measurement possible 【一手】:

1. **The workbench speaks gRPC-Web, not Connect.** It posts
   `application/grpc-web+json` with the five-byte frame envelope and
   reads the trailers envelope at the end. The plain-JSON dialect masks a
   failure as `200 {}` — under it "answered" and "not implemented" are the
   same bytes, and the whole gap would have been invisible. Only the
   gRPC-Web dialect puts the failure in the `Grpc-Status` header.
2. **A stream that says nothing is an answer.** A subscription that
   holds the connection open and sends no frame is recorded as an *empty
   stream*, not as a failure.

`scripts/probe_ls_methods.py` asks all 351 methods of a running instance
(request body: each request message's default instance, `{}`),
normalises the uuids, the tracing ids and the timestamps, and
`scripts/build_ls_coverage.py` merges the two captures into
`out/langserver/coverage-351.json`.

| | reference | clone |
|---|---|---|
| answered | 153 | **310** |
| refused (an error) | 186 | 33 (the clone's own parameter validation) |
| empty stream (held open) | 7 | 8 |
| closes the connection | 5 | 0 |
| **"Method not found"** | — | **0** |

The reference's 186 refusals break down as 52 *not implemented*, 44
*empty request* (`unknown uri scheme`), 37 *missing argument*, 22
*deprecated*, 19 *not found*, 9 *not initialised*. Five methods make the
reference **close the connection without answering**:
`CheckoutCommit`, `ProvideCompletionFeedback`,
`SubscribeToAgentBrowserRequests`, `UpdateCustomizationPathsFile` and
`WatchDirectory`.

Of the reference's 153 answers, **111 are exactly `{}`** — the default
instance — and the clone's answer is byte-identical to the reference's
for **110** of the 153; the 43 that differ are the ones the clone answers
from its own state (the skill catalogue, the conversation list, the model
names).

### 10.1 The fallback

A method the clone does not implement is answered with the **default
instance of that method's response message** — the schema knows what each
answer looks like, and the default instance is what "nothing to report"
means. `internal/lsproto/default.go` derives it from the embedded
descriptor set; `internal/lsserver/coverage.go` wires it in and adds the
answers that carry content (the skill catalogue, the workspace
directories, the static experiments).

That is a **deliberate difference** from the reference, which refuses: a
workbench that gets `-32601` logs an error for every call it makes while
the page loads.

### 10.2 The conversation batch

The batch that *reads a conversation back* is answered from the clone's
own state (`internal/remotecontrol/dispatch_ls_trajectory.go`,
`internal/remotecontrol/dispatch_ls_conversation.go`,
`internal/remotecontrol/ls_turns.go`):

| method | answer |
|---|---|
| `GetCascadeTrajectory` | the conversation's trajectory (its step list) and the run status |
| `GetCascadeTrajectorySteps` | the step list, from a `stepOffset` |
| `SearchConversations` | the clone's own conversation list (title, workspace, time) |
| `GetConversationMetadata` | the conversation's metadata (`trajectoryId`, `numSteps`, `metadata{createdAt, workspaceUris}`) |
| `GetTrajectoryTurns` | the conversation's *turns* — one user message and the reply that followed it |
| `GetAllCascadeTrajectories` | a summary per conversation (`summary`, `stepCount`, `trajectoryId`, `status`, `createdTime`, …) |
| `DeleteCascadeTrajectory` | drops the conversation's trajectory |
| `GetUserTrajectory` | one trajectory, by trajectory id |
| `GetUserTrajectoryDebug` | the mainline trajectory the debug panel reads |
| `GetUserTrajectoryDescriptions` | the trajectory list |

A **turn** starts at a user-input step and runs until the next one; the
summary is the reference's own `exa.jetski_cortex_pb.TurnSummary`
(`startStepIndex`, `firstStep`, `responsePreview`).

Measured in standalone mode with the mocked catalogue:

```json
{"trajectory":{"trajectoryId":"traj-c-test-1","cascadeId":"c-test-1",
  "steps":[{"type":14,"userInput":{"items":[{"text":"hello there"}]}},
           {"type":15,"plannerResponse":{"response":"mock reply …"}}],
  "metadata":{}},
 "status":"CASCADE_RUN_STATUS_IDLE","numTotalSteps":2}
```

### 10.3 In the shell

With the fresh binary installed into `/Applications/Pigasus.app` and the
workbench reloaded, the page asks **23 methods** while it loads and the
console holds **no error at all**. Sending a message renders the answer
and the conversation appears in the sidebar.

Evidence: `out/langserver/coverage-351.json`,
`out/langserver/conversation.json`.

---

## 11. The second round: the trajectory id, the failure format, the two streams

The first round made every method *answer*. The second round gave the
conversation batch its **content**, and produced findings that only a
live instance can produce.

### 11.1 The trajectory id is `traj-` + the conversation id 【一手】

The workbench **never sends a trajectory id**: it mints one uuid per
conversation, sends it as the `cascadeId`, and uses it as the
`conversationId` afterwards. The trajectory id is the *server's*:

```
StartCascade {"cascadeId":"XYZ-9"}
GetCascadeTrajectory
  → {"trajectory":{"trajectoryId":"traj-XYZ-9","cascadeId":"XYZ-9","metadata":{}},
     "status":"CASCADE_RUN_STATUS_IDLE"}
```

Another id gives the same shape (`ref-probe-1` → `traj-ref-probe-1`): the
rule is the prefix **`traj-`** plus the conversation id. The clone mints
the same shape (`lsTrajectoryID`), and `GetUserTrajectory` — which reads
by *trajectory* id — looks the trajectory up by that id, not by the
conversation id (`lsTrajectoryStore.LookupByTrajectory`).

### 11.2 The failure format 【一手】

A refused call carries its code and its text in the `Grpc-Message`
header, in the reference's own format:

```
grpc-status 5
grpc-message: not_found: failed to get trajectory: trajectory not found for id <id> (error ID: <32 hex>)
```

The `error ID` is the reference's own tracing id — a fresh 32 hex digits
per call — so two runs of the same call differ only there. The clone
writes the same shape (`grpcWebErrorMessage` in
`internal/remotetransport/grpcweb_codec.go`), which is what makes five
methods of the batch **byte-identical** after the normalisation:
`GetUserTrajectory`, `GetConversationMetadata`, `GetTrajectoryTurns`,
`EndAudioSession` and `SendAudioChunk`.

### 11.3 Two streams hold the connection open 【一手】

A `StreamAgentStateUpdates` or `StreamTerminalOutput` call that carries
no id is **not** a failure in the reference — it answers the status line
and the content type and then says nothing at all, holding the
connection open:

```
StreamAgentStateUpdates {}
  → HTTP 200  Content-Type: application/grpc-web+json
    (the connection stays open, no frame)
```

The clone used to answer `grpc-status 13 / stream failed` for both. It
now holds the stream open the same way, and the transport flushes the
response headers *before* the first frame — the reference sends them at
once, and a client that waits for them would otherwise see nothing at
all.

### 11.4 The debug panel's `mainline` is a list 【一手】

`GetUserTrajectoryDebug` answers the mainline entry as a **list**, and
the trajectory in it carries its type and its metadata:

```json
{"mainline":[{"trajectory":{"trajectoryId":"<uuid>","cascadeId":"<uuid>",
  "trajectoryType":"CORTEX_TRAJECTORY_TYPE_USER_MAINLINE",
  "metadata":{"createdAt":"<timestamp>","initializationStateId":"<uuid>"}},
  "trajectoryScope":{}}]}
```

`exa.language_server_pb.GetUserTrajectoryDebugResponse.mainline` is a
repeated `ImplicitTrajectory` — the clone answers a list too
(`lsUserTrajectoryJSON`).

### 11.5 What the second round changed

| | reference | clone (round 1) | clone (round 2) |
|---|---|---|---|
| answered | 153 | 312 | **310** |
| refused (an error) | 186 | 33 | 33 (the wording is now the reference's) |
| empty stream (held open) | 7 | 6 | **8** |
| byte-identical answers | — | 99 / 351 | **110 / 351** |

The two fewer answers are the two streams: they moved from *failed* to
*empty stream*, which is still an answer — and the one the reference
gives.

The capture tool is `scripts/probe_ls_conversation.py`: it makes the
**same** conversation on both instances (the same client-minted id) and
asks the batch about it, method by method. The result is
`out/langserver/conversation.json`.

---

## 12. The third round: the real model, and the terminal's directory

Two behaviours, not shapes.

### 12.1 The turn reaches a real model 【一手】

The workbench's message used to be answered by the **stand-in**:

```
mock reply from model=minimax-m3.1-flash-medium: 你是什么模型
```

`rchost.BackendPicker` — the hook that names the transport a turn rides on
— **defaults to the stand-in**. The CLI overrides it
(`internal/cliapp/run.go`); the standalone language server is another
process, so nothing overrode it there.

`cmd/langserver/backend.go` overrides it now, with the clone's own rule
(`internal/backendselect`): a model the user configured goes to its own
endpoint, a model of the reference's catalogue goes to CloudCode, and a
model nobody configured gets the stand-in **with a warning** — an answer
that silently came from the stand-in would look like a working model.

The mocked mode keeps the stand-in (`--use_mocked_data=true` is the
reference's own offline switch, and the coverage probe runs against it).

Measured in the shell: a message sent in the workbench was answered by the
model — the console logged *"Time to first token: 3213ms (turn 3)"* — and
the rendered reply was the model's own; `mock reply from model=…` appears
nowhere in the page.

### 12.2 The terminal's directory is `~`, never the server's cwd 【一手】

The workbench's `CreateTerminal` request names **no** workspace:

```json
{"conversationId":"5049af4a-f836-4c31-ba47-15cea4f6da6c"}
```

The clone took the **language server's working directory** as the
terminal's directory, and the IDE launches that server with the working
directory `/` — so the terminal opened in `/`.

The reference's own process table says otherwise (the two processes are in
the same tree):

```
the reference's language_server:   cwd = /
the shell it spawned:              cwd = /Users/gear
```

So the rule is: **the request's workspace URI, else the user's home
directory** — never the server's working directory (`internal/rcterminal`).
Measured in the shell: the terminal's shell runs in `/Users/gear`, and the
create answer carries `"workspaceUri":"file:///Users/gear"`.

---

## 13. Hard constraints

1. `--help` exits **2**. The IDE reads the exit code.
2. **68 flags**, no more, no fewer.
3. The answer's **key order** is part of the contract — build the JSON by
   hand.
4. The secret is read from **`x-codeium-csrf-token` only**.
5. The page and `/healthz` are **not** gated; the service calls are gated
   for every peer, loopback included.
6. In standalone mode the fifteen answers must stay **byte-identical**.
7. `--enable_sidecars`, `--host_bridge_url` and `--host_bridge_token` stay
   **out** of `--help` but must be **accepted** — the IDE passes all three,
   and the sidecar one keeps changing the sidecar block.
8. The workbench speaks **gRPC-Web**; the plain-JSON dialect masks a
   failure as `200 {}`, so the coverage can only be measured in the
   gRPC-Web dialect.
9. A method that is not implemented is answered with the **default
   instance of its response message**, never with `-32601` — the
   reference answers every method of the service, and the workbench logs
   an error for every call it cannot have answered.
10. The trajectory id is **`traj-` plus the conversation id** — the
    workbench never sends one, and the reference's own is derived from
    the id it was given.
11. A stream with nothing to say **holds the connection open**; it is
    not a failure and it does not close.
12. The turn in the IDE shell must reach a **real model**:
    `rchost.BackendPicker` defaults to the stand-in, so
    `cmd/langserver` overrides it — only `--use_mocked_data=true` keeps
    the stand-in.
13. A terminal's directory is the **request's workspace URI, else the
    user's home directory** — never the language server's working
    directory, which is `/` in the IDE.

## 14. Open questions

- The stdin metadata's shape (above) — needs `server.go:575`.
- The reference's hub-mode Mendel set is large and account-derived; the
  clone's local set is deliberately different.
- The reference's non-hub modes answer 404 on every path but the service —
  the clone mirrors that, but the reference's *rule* for which mode serves
  the page was not traced beyond "hub serves it".

## 15. Limitations

- All evidence comes from the **macOS arm64** build of the reference binary.
  The same binary on Linux was not exercised.
- The hub-mode reference instance was backed by a real account, so its
  answers contain account data. Shapes compare; values do not.
- The reference's `StreamAgentStateUpdates` **never produces a frame** in
  its mocked mode, so what the reference would put on that stream has no
  first-hand evidence; the clone's frame shape was recovered from the
  IDE's own bundle.
- The reference's mocked mode answers a **fixture** trajectory for any
  cascade id (`{"trajectoryId":"mock-trajectory-id","metadata":{}}`). The
  clone answers its own state instead — the fixture is the reference's
  test data, and the clone's answer has to be its own truth. The two
  differ in the coverage artifact for exactly that reason.
- The reference's `GetUserTrajectoryDebug` is **not stable**: on a fresh
  instance it answers the mocked mainline list, and once a conversation
  has been made on it, it closes the connection without answering. Both
  answers are in `out/langserver/conversation.json`.

## 16. How to verify

```bash
go build -o /tmp/langserver ./cmd/langserver
go test ./internal/lsserver/ ./cmd/langserver/

# the flag surface, byte for byte
diff <(/Applications/Antigravity.app/Contents/Resources/bin/language_server --help 2>&1) \
     <(/tmp/langserver --help 2>&1)

# the answers, byte for byte
M=exa.language_server_pb.LanguageServerService
diff <(curl -s -X POST "http://127.0.0.1:8791/$M/GetServerConfiguration" \
        -H 'Content-Type: application/json' -d '{}') \
     <(curl -s -X POST "http://127.0.0.1:8797/$M/GetServerConfiguration" \
        -H 'Content-Type: application/json' -d '{}')

# the whole service, in the dialect the workbench speaks
scripts/probe_ls_methods.py http://127.0.0.1:8791 /tmp/ref.json
scripts/probe_ls_methods.py http://127.0.0.1:8797 /tmp/clone.json
scripts/build_ls_coverage.py /tmp/ref.json /tmp/clone.json

# the conversation batch, the same conversation on both instances
scripts/probe_ls_conversation.py http://127.0.0.1:8791 http://127.0.0.1:8797 \
    out/langserver/conversation.json

# a terminal with no workspace starts in the user's home, not in "/"
curl -s -X POST "http://127.0.0.1:8797/exa.language_server_pb.LanguageServerService/CreateTerminal" \
     -H 'Content-Type: application/grpc-web+json' \
     --data-binary $'\x00\x00\x00\x00\x18{"conversationId":"t-1"}'
# → the frame's payload: {"terminal":{…,"workspaceUri":"file:///Users/gear",…}}
```

The raw captures are in `out/langserver/`; the write-up is
`docs/guide/ide-language-server.md`.

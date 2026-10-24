# Cycle `rc-wire-live-verify` — the reference has no local JSON-RPC, but it does ship its descriptor

**Status**: done (scope re-cut after measurement)
**Reference**: `~/.local/bin/agy` 1.4.4, sha256 `9abb478686dc08d48699ba1604c14d15819ef6a2542d5d32173acbd5f02d712d`
**Scope change**: TODO priority 1 assumed `agy remote-control start --bind=127.0.0.1:17532` and a local
`ws://…/rpc`. Neither exists in 1.4.4. This cycle records what *is* there, and replaces the
unverifiable "replay 47 methods" plan with an authoritative, byte-level ground truth: the
`FileDescriptorProto` that the Go runtime embeds in the binary.

## TL;DR

1. agy 1.4.4 **does not listen for JSON-RPC**. `remote-control` has `start|status|stop`
   and a hidden `serve`; `serve` starts the **language server** (`HTTPS (gRPC)` + `HTTP` on
   random loopback ports) and opens an outbound **V2 webchannel** connection to
   `https://jetski-webchannel.googleapis.com:443`. Probing both ports returns `404` for
   `/` and `/rpc`.
2. Therefore the clone's `ws://…/rpc` surface is an invention, not a port of the reference.
   The claim it can be verified by "replaying the methods against agy" was never executable.
3. The reference's **real** remote-agent surface is recoverable exactly: the binary embeds
   the uncompressed `FileDescriptorProto` for
   `google/gca/aicode/v1main/remote_agent_proxy.proto` —
   **71 RPCs, 155 messages, 4 enums**. It is committed under
   `out/local-macos/protos/` (see that README for extraction details).
4. Method-level reality: the clone has **42** JSON-RPC methods, the reference **71** RPCs.
   34 map 1:1 and are now accepted under the reference's names; **37** reference RPCs are
   unimplemented and **8** clone methods have no reference counterpart.
5. Field-level: proto field names are all `snake_case` and several differ from the clone's
   (`message_text` vs `text`, `project_id` vs `projectId`). Two are now aliased; the rest are
   listed below. `StartCascadeRequest.initial_prompt` is *behaviourally* unimplemented — the
   clone starts an empty cascade and returns.

## 1. What the probes actually returned 【一手】

All commands were run from `/tmp` with a 12 s ceiling; no LaunchAgent was installed.

| Probe | Result |
|---|---|
| `agy remote-control --help` | subcommands `start`, `status`, `stop` (no `serve`) |
| `agy remote-control start --help` | `--name`, `--session` **only** — no `--bind`, no port, no token |
| `agy remote-control status` | not running; instance name `gear-tailac191-ts-net-deep-void` |
| `agy remote-control serve --help` | does not print help — it *runs* the daemon in the foreground (undocumented subcommand) |
| `agy --log-file /tmp/rc-probe.log remote-control serve` | authenticates, then: `Language server listening on random port at 51855 for HTTPS (gRPC)`, `… at 51856 for HTTP`; blocks until SIGTERM |
| `curl http://127.0.0.1:51856/` and `/rpc` | `404 page not found` |
| `curl -k https://127.0.0.1:51855/` and `/rpc` | `HTTP/2 404` (gRPC server, no HTTP handler) |
| `lsof -p <pid>` | only those two listeners; no unix socket |

The outbound side is in the log of the same daemon (run at 02:59:05):

```
I1022 02:59:05.643323  server.go:3812] [RemoteControl] Resolved proxyServerURL: "jetski-webchannel.googleapis.com:443"
I1022 02:59:05.643389  server.go:3902] Remote control enabled, starting connection
I1022 02:59:05.643561  remote_control_v2.go:1930] [remote-control-<uuid>-v2] Starting V2 remote control connection to: https://jetski-webchannel.googleapis.com:443
I1022 02:59:06.122938  remote_control_v2.go:2056] Connection status: Connected
```

So the reference's transport is the same direction as a browser: the CLI dials **out** to
Google, and the dashboard reaches it through Google's signaling. A local dashboard cannot
"just connect" to agy; that is a property of the product, not a missing feature of the clone.

`start` was **not** executed: its help text says "Register and start the daemon", i.e. it
writes a LaunchAgent (`~/Library/LaunchAgents/…antigravity…`), which is intrusive on the
user's machine. The foreground `serve` path produced all the evidence above.

## 2. Ground truth: the embedded descriptor 【一手】

There is no gzip in this build: the `FileDescriptorProto` sits verbatim in `__TEXT,__const`.
The file-name literal is at `0x3999829`; 40,796 bytes later the next blob begins. Parsing with
`descriptor_pb2.FileDescriptorProto` yields:

```
name      google/gca/aicode/v1main/remote_agent_proxy.proto
package   google.gca.aicode.v1main
service   JetskiRemoteAgentProxyService        71 methods
messages  155
enums     4  (VcsType, IdentityType, PermissionSettingsScope, PermissionGrantList)
imports   16
streaming StreamAgentStateUpdates, StreamTerminalOutput  (server-streaming)
```

The first twelve RPCs are the core loop:

```
StartCascade → SendUserCascadeMessage → StreamAgentStateUpdates → GetConversation →
ListConversations → ListWorkspaces → CreateWorkspace → ListAvailableModels →
ListSlashCommands → ReadFile → GetTurnDiff → GetCodeActionDiff …
```

Note the naming: there is **no `RemoteAgent` infix**. The clone's `StartRemoteAgentCascade`
corresponds to `StartCascade`, `SendRemoteAgentCascadeMessage` to `SendUserCascadeMessage`,
and so on.

The internal twin — `third_party/jetski/remote_agent_proxy_pb/remote_agent_proxy.proto`,
package `exa.remote_agent_proxy_pb`, 10 messages — is also committed. It defines
`Environment`, `ConfigOverrides`, `TriggerOption`/`RemoteTrigger`, `ResourceProviderInfo`,
`PluginInfo`, i.e. the types carried *inside* the proxy requests.

## 3. Gap analysis

### 3.1 Method level

| | Count |
|---|---:|
| Reference RPCs (`JetskiRemoteAgentProxyService`) | 71 |
| Clone JSON-RPC methods | 42 |
| Mapped 1:1 (now aliased) | 34 |
| Reference-only (unimplemented) | 37 |
| Clone-only (invented) | 8 |

Mapped (reference name → clone handler):

```
StartCascade→StartRemoteAgentCascade          ReadFile→ReadRemoteAgentFile
SendUserCascadeMessage→SendRemoteAgentCascadeMessage   ReadDir→ReadRemoteAgentDir
CancelCascade→CancelRemoteAgentCascade        ListEnvironments→ListRemoteAgentEnvironments
HandleCascadeUserInteraction→HandleRemoteAgentCascadeUserInteraction
ListConversations→ListRemoteAgentConversations
ListAvailableModels→ListRemoteAgentModels     ListSlashCommands→ListRemoteAgentSlashCommands
ListProjects→…Projects   CreateProject→…  UpdateProject→…  DeleteProject→…
ListTriggers→…  CreateTrigger→…  UpdateTrigger→…  DeleteTrigger→…  ListTriggerOptions→…
ListPlugins→…  DisconnectPlugin→…
ListResourceProviders→…  ListAvailableResources→…  UpdateProjectResources→…
DeleteAgentMessage→…  DeleteConversation→…
CreateTerminal→…  ListTerminals→…  SendTerminalInput→…  CloseTerminal→…
SetPermissionPreset→…  SetArtifactReviewMode→…
AddPermissionGrants→…  RemovePermissionGrants→…
```

Reference-only (37): `StreamAgentStateUpdates`, `GetConversation`, `ListWorkspaces`,
`CreateWorkspace`, `GetTurnDiff`, `GetCodeActionDiff`, `ListIdentities`,
`GetVersionControlState`, `EnsureEnvironment`, `UpsertConversationConfig`,
`ListProjectResources`, `StreamTerminalOutput`, `UploadProjectFile`, `ReadProjectResource`,
`GetPermissionSettings`, `SetBrowserJsExecutionPolicy`, `RenewTriggerCredentials`,
`InstallPlugin`, `UninstallPlugin`, `ListPluginInstallations`, `GetMendelFlags`,
`GetUserQuotaSummary`, `GetUserAccountSettings`, `SetUserAccountSettings`, `GetUserProfile`,
`RevertCascadeToStep`, `GetCascadeNuxes`, `GenerateConversationTitle`, `AskSideQuestion`,
`DeleteAllUserData`, `GetUserDataDeletionStatus`, `StartBattleMode`, `EndBattleMode`,
`EliminateBattleModeArm`, `GetClientStorageItems`, `UpdateClientStorageItems`,
`GeneratePluginAccessToken`.

Clone-only (8): `RemoteControlStatus`, `RemoteControlOnOutput`, `RemoteControlOnMessage`,
`remoteControlVersion`, `remoteControlDeepLink`, `SetRemoteControlSession`,
`RemoteAgentErrorStatus`, `ResizeRemoteAgentTerminal`. These are daemon-management methods
(MCP handshake, streaming sink, session binding) rather than remote-agent operations; they
have no reference RPC and should stay clone-only.

> The previous "47 methods" figure included the MCP methods (`initialize`, `tools/list`,
> `tools/call`, …) and notifications. The routed RC surface is 42.

### 3.2 Field level (mapped operations)

Reference request/response messages, from the descriptor:

| RPC | Request fields | Response fields |
|---|---|---|
| `StartCascade` | `initial_prompt#2`, `items#7`, `workspace_uris#4`, `environment#5`, `force_third_party_routing#8`, `config_overrides#6`, `project_id#9`, `ace_vm_id#10`, `media#11`, `title#12` | `cascade_id#1`, `execution_status#2` |
| `SendUserCascadeMessage` | `cascade_id#1`, `message_text#2`, `items#4`, `delivery_strategy#5`, `continue_execution#6`, `config_overrides#7`, `media#8`, `artifact_comments#9`, `file_diff_comments#10`, `file_comments#11` | `cascade_id#1`, `execution_status#2` |
| `CancelCascade` | `cascade_id#1`, `reason#2`, `ace_vm_id#3`, `step_indices#4` | (empty) |
| `ReadFile` | `path#1`, `conversation_id#2`, `revision#3`, `user_initiated#4` | `content#1`, `truncated#2` |
| `ReadDir` | `path#1`, `conversation_id#2`, `user_initiated#3` | `entries#1`, `truncated#2` |
| `ListProjects` | (empty) | `projects#1` |
| `SetPermissionPreset` | `scope#1`, `project_id#2`, `preset#3` | — |

Differences the clone still has:

| Area | Clone | Reference | Status |
|---|---|---|---|
| Start request | `clientInfo`, `projectId`, `model`, `backend`, `effort`, `agent`, `sandbox`, `scaffold`, `sessionMode` | `initial_prompt`, `items`, `workspace_uris`, `environment`, `config_overrides`, `project_id`, `ace_vm_id`, `media`, `title` | `project_id` aliased; **`initial_prompt` ignored** — no turn is started |
| Start response | `cascade_id`, `conversationId`, `created_at`, `model`, `backend`, `version` | `cascade_id`, `execution_status` | extra fields; `execution_status` absent |
| Send request | `cascade_id`, `text`, `agent`, `model`, `effort`, `scaffold`, `sessionMode`, `timeoutMs` | `cascade_id`, `message_text`, `items`, `delivery_strategy`, `continue_execution`, … | `message_text` aliased |
| Send response | `conversationId`, `reply`, `durationMs`, `toolCalls`, `stopReason` | `cascade_id`, `execution_status` | the clone is synchronous; the reference is fire-and-stream |
| ReadFile result | `path`, `size`, `truncated`, `encoding`, `content` | `content`, `truncated` | superset |
| ReadDir | `path`, `maxEntries` | `path`, `conversation_id`, `user_initiated` | clone-only paging knobs |

`ExecutionStatus` is defined in an *imported* file (`cortex_pb` / `aicode_content.proto`), not
in this descriptor, so its shape is not yet extracted; the clone does not emit it.

## 4. What this cycle changed

* `out/local-macos/protos/` — the two descriptors (`.txt` text format + `.proto`
  reconstruction) and a README with offsets, lengths and the extraction procedure.
* `internal/remotecontrol/reference_surface.go` — the 71 RPC names in declaration order,
  `referenceRPCAliases` (34 pairs), `canonicalMethod`, `ReferenceRPCForCloneMethod`.
* `internal/remotecontrol/dispatch_route.go`/`dispatch.go` — `routeMethod` resolves the
  reference name before routing, so a caller written against the proto works unchanged;
  legacy names still work.
* `dispatch_cascade_helpers.go` — `project_id` → `projectId`, `message_text` → `text`
  (explicit clone spelling wins when both are present).
* `internal/remotecontrol/testdata/jetski_remote_agent_proxy_1.4.4.txt` — 71
  `RPC\tRequest\tResponse` lines, pinned by `TestReferenceRPCMethodsMatchFixture`;
  `TestReferenceAliasesDispatch` proves every alias is routed (not `-32601`);
  `TestReferenceAliasInverseIsInjective` and `TestReferenceAliasParams` cover the rest.

Deliberately **not** done: renaming the clone's methods (breaking change for existing
dashboards, and the reference wire is gRPC anyway), and inventing `execution_status` values
without the imported enum.

## 5. 硬约束 / 存疑 / limitations

**硬约束**

* The reference exposes **no local JSON-RPC**, so "byte-level wire parity" for the clone's
  `/rpc` surface is not a meaningful target. Any future claim of RC parity must say *which*
  layer it compares (method names/fields here vs. the webchannel transport).
* The descriptor is version-scoped. Offsets (`0x3999829`, `0x367539d`) move on every release;
  re-derive, never carry them forward.
* `start` was not run; the registered LaunchAgent plist and its `--session` semantics are
  unverified.

**存疑**

* Why one `serve` run connected to the webchannel within ~3 s and another did not connect in
  12 s. The log line `[RemoteControl] Subscription callback triggered … RemoteControlEnabled
  value: false` suggests the connection is gated on a settings subscription that may need a
  TUI/companion session; not proven.
* Whether the 34 "same operation" mappings are semantically identical. `ListEnvironments` →
  `ListRemoteAgentEnvironments` is the weakest: the proto's `ListWorkspaces` may be the real
  counterpart, but the response shapes (`Environment` vs `Workspace`) have not been compared.
* `StartCascadeRequest.initial_prompt` is field **2**, not 1 — field 1 is absent. That looks
  like a removed/renamed field; the historical name is unknown.

**limitations**

* Only the two `remote_agent_proxy` descriptors were extracted. `ExecutionStatus`,
  `CascadeUserInteraction`, `ConfigOverrides`' nested types and `Media` live in imports.
* The `.proto` reconstructions drop proto options (visibility, datapol) and will not compile
  without Google's internal include tree.
* No webchannel frames were captured. Doing so needs a MITM of `jetski-webchannel.googleapis.com`
  with a trusted CA, and the payload is protobuf over Google's signaling — a separate cycle.
* `TestReferenceAliasesDispatch` proves routing, not semantics: a handler answering `-32602`
  for empty params is expected.

## 6. Reproduction

```bash
# 1. the surface (no daemon needed)
strings -a ~/.local/bin/agy | grep remote_agent_proxy.proto
# → the literal lives at 0x3999829 (public) and 0x367539d (internal) in 1.4.4

# 2. the daemon, non-intrusively (foreground; Ctrl-C / SIGTERM to stop)
agy --log-file /tmp/rc.log remote-control serve

# 3. confirm there is no JSON-RPC
curl -i http://127.0.0.1:<HTTP port from the log>/rpc        # 404
curl -k -i https://127.0.0.1:<HTTPS port from the log>/rpc   # HTTP/2 404

# 4. the clone's side
go test ./internal/remotecontrol/ -run TestReference -count=1
```

# Parity matrix — rc-final-parity-verify

Verified live: **21 / 22**
Gaps: **1**; skipped intentionally: **0**

| Method | Cycle | Status | Notes |
|---|---|---|---|
| `initialize` | `rc-ws-transport` | verified | serverInfo.name=openagy-remote-control, version=0.1.0, protocolVersion=2024-11-05 |
| `tools/list` | `rc-ws-transport` | verified | 3 tools: get_output, list_agents, status |
| `tools/call` | `rc-status-tool` | verified | rich envelope: pid/version/backend/model/workspace/started_at/uptime/connected_clients/cascades/auth/daemon |
| `StartRemoteAgentCascade` | `rc-cascade-start` | verified | cascade_id=cas_31cb38ef5f77a8e3, backend='shim' (params.backend honored since rc-polish G16) |
| `SendRemoteAgentCascadeMessage` | `rc-cascade-message` | gap | send error: {'code': -32603, 'data': 'remotecontrol: turn failed\npiagent: session event channel closed without done', 'message': 'Internal error'} |
| `HandleRemoteAgentCascadeUserInteraction` | `rc-handle-user-interaction` | verified | unknown interaction_id → code=-32006 (Interaction not found) |
| `RemoteAgentErrorStatus` | `rc-status-tool` | verified | errors ring empty (1 entries) |
| `ListRemoteAgentModels` | `rc-list-readonly` | verified | 7 model rows; default_model=gemini-3.8-flash-high |
| `ListRemoteAgentSlashCommands` | `rc-list-readonly` | verified | 27 slash commands |
| `ListRemoteAgentConversations` | `rc-list-readonly` | verified | conversations slice (0 rows; empty on fresh store) |
| `ListRemoteAgentEnvironments` | `rc-list-readonly` | verified | os=darwin arch=arm64 cwd=/Users/gear/Documents/GitHub/openagy-rc |
| `ReadRemoteAgentFile` | `rc-file-ops` | verified | utf-8 content, 4341 bytes |
| `ReadRemoteAgentDir` | `rc-file-ops` | verified | 224 entries |
| `ListRemoteAgentProjects` | `rc-projects` | verified | projects slice (0 rows) |
| `CreateRemoteAgentProject` | `rc-projects` | verified | create+update+delete round-trip ok |
| `UpdateRemoteAgentProject` | `rc-projects` | verified | verified as part of the create+update+delete round-trip |
| `DeleteRemoteAgentMessage` | `rc-conversation-deletions` | verified | non-existent conversation → code=-32012 (Message not found) |
| `DeleteRemoteAgentConversation` | `rc-conversation-deletions` | verified | non-existent conversation → code=-32011 (Conversation not found) |
| `EnsureRemoteAgentACEVM` | `rc-status-tool` | verified | stub: ensured=False version=0.0.0-stub capabilities=[] |
| `SetRemoteControlSession` | `rc-set-remote-control-session` | verified | session_id bound; status.auth.session_id reflects |
| `remoteControlVersion` | `rc-version` | verified | version=1.4.2 build_label=unknown daemon_sha=unknown |
| `DeleteRemoteAgentProject` | `rc-conversation-deletions` | verified | verified as part of the create+update+delete round-trip |

## Doc-only methods (cycle brief)

| Item | Kind | Notes |
|---|---|---|
| `StreamRemoteAgentStateUpdates` | constant | Discovered in cycle 1 research as a server-push surface. The dispatcher exposes it as `NotifStreamRemoteAgentStateUpdates` (internal/remotecontrol/streaming.go) but the live wire emissions are the cycle-5 trio: `cascade/state`, `cascade/text_delta`, `cascade/tool_call`. See deliverable §wire-shape-diff. |
| `remoteControlDeepLink` | stub | Not implemented. Agy exposes this for `agy://…` deep-link handling (see out/local-macos/dec/). The openagy remote-control surface has no deep-link concept yet. Park for a future `rc-deep-link` cycle. |
| `remoteAgentEndpoint / postRemoteAgentProto / getRemoteAgentProto` | covered | Proto-transport-specific. JSON-RPC over WebSocket covers the same surface; no proto transport is shipped. Cycle rc-piagent-bind uses JSON-RPC for every cascade interaction. |
| `getAICodeRemoteURL` | constant | URL constant surfaced via the `daemon.addr` block of the rich-status tool. Not a method. |

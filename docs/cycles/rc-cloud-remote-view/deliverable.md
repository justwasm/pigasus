# Cycle `rc-cloud-remote-view` — the cloud remote view's history, its stream, and its think trace

**Status**: done; the cloud's remote view works against the clone over the
WebChannel tunnel, with no mesh in the picture (§7.4, §7.5)
**Reference**: Antigravity CLI `agy` 1.4.7, cloud remote control at
`https://antigravity.google.com/u/2/r/<instance-id>`
**Scope**: `pigasus remote-control serve` — the three gaps the cloud view had:
no conversation history, no streaming, and no collapsible think trace.

## TL;DR

1. **The cloud hands the browser the real CLI web bundle**, and it does so
   only when the instance advertises the reference's own version string:
   the clone said `2.24.0-v2` and the cloud served the *IDE's* bundle; the
   reference says `cli-1.4.7-v2` and the cloud serves `/web/cli-1.4.7/`
   (【一手】 `internal/rctunnel/client.go`, `DefaultClientVersion`).
2. **The dialect follows the caller's own content type, and the default is
   JSON.** The app's unary calls name `application/proto` and the clone
   answers protobuf; the dashboard's host page names nothing and the clone
   answers JSON — which is what the reference answers too. `internal/lsproto`
   bridges the two with the descriptor closure **extracted from the frontend
   bundle itself** (351 RPCs, 807 messages, 757 KB) — no schema is
   regenerated.
3. **History.** The sidebar lists the reference's own conversations (493–501
   of them, live), and a conversation's steps are read from the store and
   served in the first frame of the state stream. The cold load went from
   **20.7 s to 5.29 s** once the frontend's reconnect storm was answered
   with an open stream instead of a closed one.
4. **Streaming.** The answer grows in place: one planner-response step per
   *model message*, a tool call ends the message that asked for it.
5. **The think trace.** The "Thought for 1s ›" panel renders — and this cycle
   also **persists the thinking and its duration**, so the panel survives a
   reload instead of vanishing with the live trajectory (【一手】 the
   reference's own stored blob carries both: `thinking` and a
   `"6.773079s"` duration).
6. **The cloud's `close` frame ends the session, and the instance id is the
   bare uuid.** Both fixed in `a90629d2` (§7.2, §7.3) — the clone used to
   hold a zombie session the cloud had disowned, under an id the cloud's
   registry does not use.
7. **The registry row is per Google account, and the tunnel's liveness owns
   it.** The cloud's rows are scoped to the account (`X-Goog-AuthUser`), and
   the dashboard's path segment `/u/<N>/` selects one — N is the index in
   the browser's account chooser. The clone's row lives under the account
   its daemon authenticates as, and its `statusLastUpdatedAt` advances every
   ~3 s with **no mesh in the picture at all**. "Instance disconnected" and
   "No instance found" were an account-scope artefact, not a missing mesh
   (§7.4) — the reference's own row behaves the same way.
8. **The remote view is live.** `https://antigravity.google.com/u/3/r/<id>`
   opens the CLI's own bundle against the clone: the app boots, the sidebar
   lists the clone's conversations, a message typed in the browser runs the
   clone's model and streams back, and the "Thought for 1s ›" panel
   renders (§7.5).
9. **A conversation the Web UI creates gets a name.** The app's own
   fallback is the string *"Untitled Conversation"* (it lives in the app
   bundle, `cli-1.4.7-main.js`, and in no reference binary), and it shows
   it whenever the instance's summary message carries no title. The
   instance has to name the conversation, and the clone now does — the
   store layer writes the title when the clone creates the conversation,
   and an empty title never erases a stored one (§7.6).
10. **A session the cloud has disowned ends the session.** The forward
    channel answers `400 Bad Request — "Unknown SID That's all we know."`,
    the poll is cancelled, and the run loop hands the instance a fresh
    handshake and registration — the reference's own shape ("Connection
    ended. Retrying immediately..." → "Starting V2 remote control
    connection"). Kept alive, the clone's answers were dropped, the cloud
    cancelled the browser's request ten seconds later, and the dashboard's
    liveness probe never completed (§7.7).
11. **The default dialect is JSON, and the host page needs it.** The
    dashboard's host page asks the instance for `GetMendelFlags` and for
    the status **without naming a content type**, and the reference answers
    in JSON. The clone used to answer protobuf (`CgA=`), so the host page's
    `JSON.parse` threw and it never read the instance's answer at all
    (§7.7).
12. **The Mendel flags answer carries the remote-control experiments.** The
    reference's own answer is 56 758 bytes of JSON — 508 experiments and
    `webrtcMeshReady: true`. The clone answers the same shape with the
    remote-control subset and the mesh experiment disabled, because it has
    no WebRTC mesh (§7.7).

---

## 1. The version string decides which bundle the cloud serves

【一手】 The cloud's remote view is not a page of its own: it is an iframe
onto the CLI's web bundle, and the URL carries the bundle's version. The
instance announces that version in its handshake.

| what the instance advertises | what the cloud serves |
|---|---|
| `2.24.0-v2` (the clone, before this cycle) | the IDE's bundle — the wrong UI, the wrong schemas |
| `cli-1.4.7-v2` (the reference, and the clone now) | `/web/cli-1.4.7/` — the CLI's own bundle |

The clone's `internal/rctunnel/client.go` now carries
`DefaultClientVersion = "cli-1.4.7-v2"`, the reference's own string. The
instance id is derived from a persisted uuid, so a restart keeps the same
instance and the cloud's URL keeps working.

## 2. The dialect: binary protobuf in, proto-JSON inside

【一手】 The cloud's v2 mode speaks Connect's **binary unary** framing:
the request body is a binary `exa.language_server_pb.LanguageServerService`
request, and the answer must be a binary response. The clone's dispatcher
answers JSON, and the frontend's protobuf-es decoder rejected it
("invalid end group tag", "premature EOF") — the remote-control page spun
forever on "Connecting to instance…".

`internal/lsproto` owns the bridge:

```
binary protobuf → proto-JSON → the dispatcher's JSON
the dispatcher's JSON → proto-JSON → binary protobuf
```

The schema is the **descriptor closure the frontend bundle already
embeds** (`internal/lsproto/descriptor/language_server.protoset`, extracted
by `out/ide-language-server/extract_ls_protoset.py`). Nothing is
regenerated: the clone reads the reference's own schema out of the
reference's own bytes.

Two details the closure decided:

- **An `Any` the schema does not know is dropped.** The reference's
  tool-call steps carry `exa.cortex.core.tools.*` payloads, and the
  frontend's bundle does not embed those schemas — proto-JSON refuses to
  marshal such an `Any`, and the refusal costs the *whole step*. The
  Web UI never reads that payload (it renders a tool call from the step's
  metadata), so the clone clears it before the marshal.
- **The caller's content type wins, and the default is JSON.** A caller that
  names `application/json` gets JSON; a caller that names none gets
  Connect's own default — JSON. The reference answers the same way (§7.7).

## 3. The wire: what the frontend actually calls

【一手】 From the daemon's own log, the frontend's 42 distinct RPC paths.
The three that carry the conversation are:

| RPC | what the clone answers |
|---|---|
| `JetboxSubscribeToSummaries` | the sidebar: `{updates: map<conversationId,summary>, deletes: []}` |
| `StreamAgentStateUpdates` | the trajectory: one frame per change, **kept open** |
| `JetboxWriteSummary` | the title/preview the frontend writes after a turn |

**`RequestAgentStatePageUpdate` is never called** — 0 occurrences in the
whole daemon log. The pagination hook in the bundle calls it, but the
frontend's slice is filled by the stream instead: the response message
(`AgentStatePageUpdateResponse`) is **empty**, and the steps travel through
the stream's first frame. The clone answers the page request with `{}` and
seeds the trajectory, so the steps arrive the way the reference sends them.

The stream request carries the slice the frontend already holds:

```json
{"conversationId":"…","subscriberId":"…",
 "initialStepsPageBounds":{"endIndexExclusive":7},
 "trajectoryVerbosity":"CLIENT_TRAJECTORY_VERBOSITY_PROD_UI",
 "disableRehydration":true,"enableLatencyTelemetry":true}
```

and the answer is one frame:

```json
{"update":{"conversationId":"…","trajectoryId":"…","status":1,
  "mainTrajectoryUpdate":{
    "stepsUpdate":{"indices":[0,1,2,3,4,5,6],"steps":[…],
                   "totalLength":7,"pageBounds":{"startIndex":0,"endIndexExclusive":7}},
    "lastStepType":15}}}
```

【一手】 The frame was checked against a live conversation of seven steps
(five written by the reference, two by the clone): the indices, the total
length, the bounds, the step types and the statuses all survive the round
trip, and `metadata.sourceTrajectoryStepInfo` is stamped on every step the
clone serves.

**Every step needs that stamp.** 【一手】 The bundle groups a conversation's
steps into turns by the index in the metadata — `jV(metadata)` reads
`metadata.sourceTrajectoryStepInfo.stepIndex` — and keys each turn by
`<trajectoryId>-<index>`. A step without an index lands in the unnamed
`"initial"` bucket, and **two such steps in one conversation produce two
turns with the same key**: the Web UI then renders one of them and the
other is gone. The reference's own steps always carry the index, so the
clone stamps it on the steps it builds *and* on the steps it reads back.

## 4. History: where the steps come from

【一手】 The store is the ground truth. The reference keeps every step of
every conversation as a binary protobuf blob — a `gemini_coder.Step` — in
`~/.gemini/antigravity-cli/conversations/<id>.db`, and the clone reads
them with the same schema the frontend uses. The blob is rich: the
reference's own planner-response step carries the answer, the model's
reasoning, the usage metadata and the user's config.

The clone **appends** to a conversation it did not start; it never
rewrites one. A rewrite would replace the reference's rows with the
clone's own (poorer) `Step` shape. The step count in the summary row is
refreshed from the file's own row count.

The sidebar's titles come from the reference's own summary rows: the clone
decodes the `raw_summary` blobs when it can, and rebuilds the columns when
it cannot. A row whose conversation file does not exist is left out of the
list — the reference's summary table holds conversations this machine has
never stored.

## 5. Streaming and the think trace

【一手】 One model message is one planner-response step; a tool call ends
the message that asked for it, so the next message starts a new step. The
sink publishes every delta onto the trajectory as it arrives: the answer
grows in place, the thinking appears before the first word of the answer.

The frontend's renderer (bundle, `cli-1.4.7-main.js`) reads:

| what | where |
|---|---|
| the reasoning | `step.value.rawThinking \|\| step.value.thinking` |
| the answer | `step.value.response` |
| the label | `thinkingDuration.seconds` → `Thought for ${max(1, seconds)}s`, or `Thought Process` when there is no duration |
| the live label | counted from `metadata.viewableAt` |

**`thinkingDuration` is a proto-JSON *string*** ("1.500s"), not an object —
the frontend's protobuf-es decoder rejects the object form, and a rejected
frame kills the **whole** trajectory stream, not just the field.

### 5.1 The thinking is persisted, too

【一手】 The reference's own stored planner-response blob carries the
thinking and the time it took:

```
plannerResponse:
  messageId: "bot-f4af3582-…"
  thinking: "The user's request is for Nanchang's weather…"
  thinkingDuration: "6.773079s"
```

Before this cycle the clone stored only the answer, so a reloaded page
showed the answer and no panel. The store now writes all three — the
answer (field 1), the thinking (field 3) and the duration (field 11, a
`google.protobuf.Duration`) — and the clone's own reader reads them back.
`internal/convstore/thinking_test.go` pins the round trip, both through the
clone's reader and through the reference's schema.

## 6. What the reference does that the clone does not

| | reference | clone |
|---|---|---|
| the transport | a WebRTC mesh session **and** a Boq WebChannel | the WebChannel backchannel only (`rc-webrtc-mesh-defer`) — and the remote view works with it (§7.5) |
| the instance id | `<installation_uuid>-v2`, the uuid persisted in `antigravity_state.pbtxt` — the `remote-control-` in its log lines is the session's tag, not the id | the same shape, persisted ✓ (§7.3) |
| a restart | the cloud's WebChannel reconnects on its own ("Unknown SID (61087)", "Refusing a proxy request for mesh session …: this handler is draining while the WebChannel reconnects") | the clone reopens its backchannel on the same session ✓ |
| a continued conversation | the file keeps every turn; the view shows the new turn | the same: the file keeps every turn, the trajectory restarts with a new id ✓ |
| the restart notice | writes a `type 101` system-message step — the reference's own format string is "All your subagents and background tasks have been stopped due to %s." with `%s` = "server restart" — and it resumes what it lost: "Resuming %d conversation(s) interrupted by server restart" 【strings】 | does not write the notice; it *does* continue a conversation the daemon has no cascade for (`lsResumeCascade`), because the store is the state |
| the summary blob | writes `raw_summary` | writes the columns only, and leaves the reference's blob alone ✓ |
| the conversation's name | names a conversation when it creates one — the reference's own RPC list carries `GenerateConversationTitle` (`google.gca.aicode.v1main.JetskiRemoteAgentProxyService`, `internal/remotecontrol/reference_surface.go:108`), and the app shows the name the instance's summary message carries | names a conversation it creates, in the store layer: a heuristic title (the reference asks the model) written to the summaries row and the metadata cache (§7.6) |

## 7. The cloud's instance registry, and the "close" that ended the clone's row

### 7.1 The dashboard's contract

【一手】 The dashboard's own bundle (`boq-pgm.js`) decides what the page
shows from one field of the registry row:

- `loadInstance(a)` sets the app iframe **only** when
  `a.status !== INSTANCE_STATUS_DISCONNECTED`;
- a row with `status = INSTANCE_STATUS_DISCONNECTED` (2) renders the
  "Instance disconnected" card;
- a row that does not exist at all renders "Instance unavailable — No
  instance found."

The registry is `devtools_jetski_boq_api_proto.ApiService/ListInstances`:
17 rows, `max_connected_instances = 10`, no pagination. Each row is
`{id, metadata{name, version}, status, statusLastUpdatedAt}`.

**The rows are per Google account.** The request carries `X-Goog-AuthUser`,
and the same instance id has *one row per account* — its own name, its own
status, its own timestamp. The dashboard's path segment `/u/<N>/` is that
index, N being the position of the account in the browser's account
chooser (§7.4).

### 7.2 The "close" frame ends the session

【一手】 At 12:36:23.404 the cloud sent the string `close` on the
backchannel. In the same second the clone's registry row flipped to
`status = 2` and its `statusLastUpdatedAt` froze at `1792989383` — and it
stayed frozen for the rest of the day, while the clone kept answering the
cloud's `GetStatus` pings every ~15 s **on the same SID**.

The reference's own captured log says what the frame means:

```
[[2,["close"]]]
WebChannel OnClose callback triggered
Connection loop exited: WebChannel connection closed
Connection ended quickly (lasted 8.011215375s). Treating as failure for backoff.
Reconnection attempt 45 failed. Retrying in 4.802795379s...
Connection status: Connected
```

The frame ends the session; only a fresh handshake — a new SID and a new
registration — brings the instance back. The clone ignored it: it kept
reading the dead SID, kept re-opening the poll on it (HTTP 200) and kept
answering the pings — a zombie session the cloud had already disowned.

**Fixed** (`a90629d2`): `processInboundChunk` returns `errServerClosed`,
`readBackChannel` propagates it, and `pumpBackChannel` hands the session
back instead of re-opening the poll on the dead SID. Validated live:
`server signaled close; ending the session` → `reconnecting in 1s` → a new
session → a new registration.

### 7.3 The instance id is the bare uuid

【一手】 The reference's captured registration body:

```json
{"metadata":{"name":"pigasus-test-inst","version":"2.24.0-v2"},
 "instanceId":"0d159523-6e02-4362-abab-93e0c69edea4-v2",
 "supportsPayloadText":true}
```

and the cloud's registry lists that instance under the same id. The
`remote-control-` in the reference's log lines is the tag of its
remote-control **session**, not part of the id — the clone had registered
`remote-control-<uuid>-v2`.

**Fixed** (`a90629d2`): `persistentrc.InstanceIDPrefix` is empty and
`NewInstanceID()` returns `<uuid>-v2`. The registration's answer is now
logged too: the cloud replies `200` with its own chunk (`"8\n[1,1,7]\n"`),
and a refusal would only ever show up there.

### 7.4 The registry row is per account, and the tunnel's liveness owns it

> **Corrected 2026-10-26.** The previous revision of this report concluded
> that "the registry row is the mesh's" and that the clone therefore could
> not enter the cloud's registry. **That was wrong**, and the mistake was a
> measurement error: every `ListInstances` call had been made with the
> browser's *default* account. The rows were there all along.

【一手】 `ListInstances` with `X-Goog-AuthUser: 0…4` returns a different row
set per account, and the same instance id appears under every account with
its own name, status and timestamp:

| auth user | rows | the clone's `0698e8f6-…-v2` | the reference's `0d159523-…-v2` |
|---|---|---|---|
| 0 | 7 | absent | `status = 2`, frozen |
| 1 | 1 | absent | `status = 2`, frozen |
| 2 | 17 | `status = 2`, frozen (48 421 s) | **`status = 1`, 22 s old** |
| 3 | 8 | **`status = 1`, 3 s old** | `status = 2`, frozen (43 600 s) |
| 4 | 20 | absent | `status = 2`, frozen |

The account index is the browser's account-chooser order:

| idx | account | |
|---|---|---|
| 0 | navigaid@gmail.com | the browser's default — the bare `/r/<id>` URL resolves here |
| 1 | btwiuse@gmail.com | |
| 2 | libredot@gmail.com | **the reference** — its token's `id_token_email`, and its own log: `Headless auth: authenticated as "libredot@gmail.com"` |
| 3 | naivgaid@gmail.com | **the clone** — `accounts.Current()`'s active account |
| 4 | stakingminer@gmail.com | |
| 5 | justwasm@gmail.com | |

An instance's row therefore lives under **the account its daemon
authenticates as**: the clone registers with `naivgaid@gmail.com` (index 3),
the reference with `libredot@gmail.com` (index 2).

Both binaries read the **same** token file —
`~/.gemini/antigravity-cli/antigravity-oauth-token`, the reference's own
path and the clone's unified token (the clone's `/remote-control on` saves
its active account's token there). The account a daemon registers under is
therefore whatever that file held when the daemon started, which is why the
two views need different `/u/<N>/` paths.

【一手】 That shared file is not a theory: on a restart of the clone's
daemon (2026-10-26 15:22) the file was rewritten with
`stakingminer@gmail.com`, the clone's daemon logged
`Headless auth: authenticated as "stakingminer@gmail.com"` — and **the
reference's own daemon**, which reads the same file, logged the same
account two seconds later (`cli-20261026_152214.log`, pid 13858). The
clone's instance moved from index 3 to index 4 with it, and the live view
followed: `https://antigravity.google.com/u/4/r/<id>`. The clone's token
refresh therefore *does* move the reference's registration — an artefact of
two binaries sharing one path, not a protocol difference.

And the clone's row advances its `statusLastUpdatedAt` every ~3 s — with
**no mesh session anywhere**: the clone has no `MeshDaemon`, no signaling
poll, no ICE config. The **WebChannel tunnel's liveness writes the row**.

⇒ The six registrations that "created no row" did create one; they were
read under the wrong account. And the re-registration that "did not touch
the row" updated the row under the clone's own account, not under the
default one.

**What the mesh actually is.** The reference runs a *second* transport
alongside the WebChannel: its own log shows the signaling poll, the ICE
config from the api server, the pinned server-vouched WebCrypto key and the
"authenticated WebRTC DataChannel" the transport multiplexer upgrades to.
Its signaling lives on the same boq service the dashboard uses:

| RPC | what |
|---|---|
| `devtools_jetski_boq_api_proto.ApiService/InitiateMeshSession` | the browser starts a session; the answer carries the session id, the STUN/TURN servers and the signaling timeout |
| `…/PollSignalingMessages` | the instance's poll: `poll_target` is a oneof (`session_id` = 1, `instance_id` = 3), `after_timestamp` = 2, `include_authorized_keys` = 4 |
| `…/SendSignalingMessage` | the offer, the answer, the ICE candidates |

【一手】 The instance side of that poll **works with the CLI's own
credentials**: `POST https://antigravity.google.com/$rpc/devtools_jetski_boq_api_proto.ApiService/PollSignalingMessages`
with `Authorization: Bearer <the CLI's OAuth token>` and **no** API key
answers `200` — for the reference's instance it carries the authorized key
bundle (the PEM the browser vouched), for the clone's an empty one. The
browser's API key with the CLI's token is refused: *"The API Key and the
authentication credential are from different projects"* (consumer project
`736576405720`).

The mesh is the **browser↔instance direct data channel**, not the entry
gate. The reference's own remote view needs it: at
`/u/2/r/0d159523-…-v2` the cloud creates the app iframe (its row is live
under account 2) but the page sits at **"Connecting to instance…"** — the
attachment never completes. The clone's remote view needs no mesh: its
tunnel carries the app's RPCs (§7.5).

⇒ **The remote view is not gated on the mesh.** `rc-webrtc-mesh-defer` stays
deferred as a *transport* item (the reference's browser↔instance channel),
not as the gate for the cloud's view.

**【一手】 Several live instances under one account break the attach.**
The registry sweep of account 4 (2026-10-26 15:45) found **three** rows
with `status = 1`: the clone's `0698e8f6-…-v2` ("pigasus-test-device-
noble-bolt"), the reference's `0d159523-…-v2` ("test-device-noble-bolt"),
and a third clone instance ("gear-tailac191-ts-net-astral-nebula"). The
outer page then sits at **"Connecting to instance…"** and the browser
console carries the cloud's own proxy refusing the app's boot call:

```
[Host] Upstream proxy execution error on
/exa.language_server_pb.LanguageServerService/GetMendelFlags:
RpcError(FAILED_PRECONDITION): Precondition check failed.
```

The clone's daemon still answers the whole boot sequence over its tunnel
(`GetMendelFlags`, `GetStatus`, `JetboxSubscribeToSummaries` in the
daemon log), so the tunnel is alive — the relay hands some of the page's
calls to a *different* instance of the same account, and the reference's
instance refuses them while its mesh session drains ("Refusing a proxy
request for mesh session …: this handler is draining while the WebChannel
reconnects", the reference's own log).

【推测】 The view therefore attaches only while the account's live rows
belong to a single instance. That is an artefact of running the reference,
the clone and a second clone daemon against one Google account on one
machine — not a difference between the two binaries.

> **Corrected 2026-10-26 (later the same day).** The conclusion above is
> **wrong**. The page binds by **instance id**, not by account: with the
> reference's instance and the clone's instance live under the *same*
> account (index 3 — both binaries read the same token file), each view
> talks to its own instance, and both mount the app iframe
> (`https://antigravity.static.usercontent.goog/web/cli-1.4.7/?iframe=true`,
> 1470×780).
>
> What the sweep really caught was **two daemons registering the same
> instance id** — the reference's daemon and a second clone daemon, both
> evicting each other's session — plus the *actual* cause of the
> "Instance disconnected" card, which is not a protocol difference at all:
> the cloud had disowned the session and answered the forward channel with
> `400 "Unknown SID"`, so every answer was dropped and the dashboard's
> liveness probe timed out. §7.7 has the fix and the evidence.
>
> 【推测】 the `FAILED_PRECONDITION` in the browser console was the
> reference's instance refusing a proxy request while its own handler was
> draining — the reference's log says so — and the call was addressed to the
> reference's instance, not to the clone's. The cloud picks the instance by
> id; the *page* was on the clone's id.

### 7.5 The cloud remote view, live

【一手】 `https://antigravity.google.com/u/3/r/0698e8f6-2cee-42c0-a454-02c764f6b9b6-v2`
— the account the clone's daemon registers with — opens the CLI's own
bundle against the clone:

| what | evidence |
|---|---|
| the app boots | the iframe is `https://antigravity.static.usercontent.goog/web/cli-1.4.7/?iframe=true` (780 px) and the daemon answers the whole boot sequence over the tunnel: `GetMendelFlags`, `GetLocalUserInfo`, `JetboxSubscribeToSummaries`, `JetboxSubscribeToState`, `GetServerConfiguration`, `ProjectUpdatesStream`, `GetUserStatus`, `ReadProjects`, `ListProjects`, `GetAllSkills`, `FetchUserInfo`, `GetSlashCommands`, `GetCustomizationStates`, `GetAgentScripts`, `SubscribeToPlugins`, `HasAuthToken`, `GetAuthStatus`, `FetchAdminControls`, `GetCascadeNuxes` |
| the history renders | the sidebar lists the clone's own conversations (17 in "CLI Project", plus the per-workspace folders) and a conversation opens with its turns (`hi` → `Hello! How can I help you today?`) |
| the clone's own state | the picker shows the clone's model (`MiniMax M3`) and the sidebar's instance chip reads `pigasus-gear.tailac191.ts.net` with a green dot |
| a turn runs | a message typed in the browser crosses the tunnel as `StartCascade` → `SendUserCascadeMessage`, with `StreamAgentStateUpdates` open and `JetboxWriteSummary` / `GetTurnDiff` alongside; the reply streamed back (`ok`) |
| the think trace renders | the panel reads **"Thought for 1s ›"** above the answer |

The bare URL `https://antigravity.google.com/r/<id>` — the one the CLI
prints — resolves against the browser's **default** account (index 0) and
answers *"Instance unavailable — No instance found. Try switching to a
different account."* The reference's own bare URL fails the same way
(*"Instance disconnected"*), because its live row is under index 2 (§7.4).

### 7.6 The name a conversation gets

【一手】 The Web UI's header falls back to the string **"Untitled
Conversation"** when the instance's summary message carries no title. The
string is the *app's* own: four hits in `cli-1.4.7-main.js`, none in any
reference binary. The title the app shows is the one the instance serves
in the summary message — and the clone builds that message from the
summaries row's `title` column (`internal/remotecontrol/ls_history.go`,
`lsSummaryFromColumns`).

Nothing else fills that column on this path. The app's
`JetboxWriteSummary` request carries the summary the instance gave it,
and that summary has no title of its own; the app never asks the instance
to name the conversation (no `GenerateConversationTitle` ever appears in
the daemon's inbound-RPC log). The instance has to name the conversation
itself — which is what the reference does when it creates one, and what
the clone now does too.

**The fix is in the store layer** (`internal/convstore`, not in the remote
control path, so the TUI's own path and the remote path share one rule):

- `AppendConversationSteps` — the remote path's only entry into the store —
  names the conversation when it creates it: the title is derived from the
  first user-input step and written to the summaries row's `title` and
  `preview` columns and to the metadata cache's `Title`, which is exactly
  what the TUI path writes (`internal/cliapp/tui_prompt_submit.go`).
  A conversation the *reference* created keeps the reference's own name.
- An **empty title never erases a stored one.** Both upserts
  (`WriteSummaryRow`, `updateConversationSummariesDB`) keep the stored
  `title`/`preview` when the incoming one is empty, so the app's
  title-less summary write cannot blank the name the instance stored.

**The store's table shape, and a silent write failure.** The reference
creates `conversation_summaries` with 21 columns and two of them
(`last_modified_time`, `last_user_input_time`) are `NOT NULL` **without a
default**. The clone's own insert named neither, so on a store the clone
made on its own the insert failed — silently, the error was dropped — and
the conversation never reached the index at all. The clone now creates the
reference's table (`internal/convstore/summary_schema.go`) and names both
columns; an older clone's six-column table is migrated in place (the
missing columns are added).

Live, after the fix: a new conversation created in the Web UI
(`439bc43a-c633-4f6f-be1b-72acccc4452c`, message *"reply with exactly one
word: ok"*) shows **"CLI Project / reply with exactly one word ok"** in the
header — not "Untitled Conversation" — and its row reads
`title='reply with exactly one word ok'`, `preview='reply with exactly one
word ok'`, `step_count=2`, `app_data_dir=''` and no `raw_summary` (the
clone's own shape; the reference writes `app_data_dir='antigravity-cli'`
and the blob).

**The reference's reconcile pass takes the name back.** Two binaries
sharing one store fight over the summaries table. The reference's own
daemon reconciles it when it starts — its log says

```
[Summaries] failed to generate summary for b89ee11b-… while reconciling:
conversation b89ee11b-… not found          (reconcile_summaries.go)
```

— and for every row it *can* read it rewrites the row in the reference's
own shape: `app_data_dir='antigravity-cli'`, a fresh `raw_summary` blob,
and **an empty title**, because the conversation's steps are the clone's
shape and not the reference's protobuf, so the reference cannot generate
a name for them. The clone's row comes back untitled, and the Web UI
falls back to "Untitled Conversation" again. The reference's blob carries
no title either — the message's title field is `summary`
(`exa.jetski_cortex_pb.CascadeTrajectorySummary`, decoded from the
reference's own blob: `{"summary":"QEMU Playground Debug Handoff", …}`),
and it is absent from the blob the reconcile pass wrote.

The reconcile pass **leaves the metadata cache alone**, so the clone's own
name survives there, and the clone reads it back:

- `convstore.ListSummaries` gives a row without a title the name the
  metadata cache holds (the row's own title always wins);
- `remotecontrol.lsFillSummaryTitle` gives the summary *message* the
  row's title when the message itself carries none — this is the path
  that matters when the reference has written a blob, because then the
  message is the reference's own bytes and the columns are not read.

Verified live after the reference's daemon restarted and blanked the row:
the Web UI's header still reads **"reply with exactly one word ok"**.

> The account index moves with the daemon's account. The clone's daemon
> re-authenticates at start-up and writes the account it picked into the
> shared token file (`~/.gemini/antigravity-cli/antigravity-oauth-token`),
> so a restart can move the instance to another account — here from index 3
> (`naivgaid@gmail.com`) to index 4 (`stakingminer@gmail.com`), and the
> view moved with it to `/u/4/r/<id>` (§7.4).



### 7.7 The dialect the cloud relays, the dead session, and the Mendel flags

Three findings from the evening of 2026-10-26, all of them measured against
the **reference's own live instance** through the cloud's `ProxyCommand` API
(`devtools_jetski_boq_api_proto.ApiService/ProxyCommand` — the same call the
dashboard's host page makes) and against the clone's own daemon.

#### 7.7.1 The caller's content type picks the dialect, and the default is JSON

【一手】 The dashboard's host page asks the instance two things *before* it
mounts the app, and it names **no content type** on either:

```js
probeGetStatus() {
  return Promise.race([
    this.proxyCommand(_.KG.RemoteControlComponent.GET_STATUS_PATH,
                      new TextEncoder().encode("{}")),
    new Promise((_, reject) => setTimeout(
      () => reject(new Error("liveness probe timed out")),
      this.config.rcLivenessProbeTimeoutMs()))])          // 10 000 ms
}

async fetchMendelFlags() {
  const {payload} = await this.proxyCommand(
    "/exa.language_server_pb.LanguageServerService/GetMendelFlags",
    new TextEncoder().encode("{}"));
  return payload;
}
```

The reference answers both in **JSON**, with the content type
`application/json`:

| call | the reference's answer | the clone's answer (before) | the clone's answer (now) |
|---|---|---|---|
| `GetMendelFlags` | `application/json`, 56 758 bytes of JSON | `application/proto`, `CgA=` (protobuf) | `application/json`, the same shape |
| `GetUserStatus` | `application/json` | `application/proto` | `application/json` |
| `GetMendelFlags` **with** `application/proto` | protobuf | protobuf | protobuf |

⇒ The rule is Connect's own: **the caller's content type picks the dialect,
and a caller that names none gets JSON.** The clone used to default to
protobuf, so the host page's `JSON.parse` threw on the answer — the
console carried the host page's own fallback ("Failed to read
remote-control-webrtc-mesh flag; defaulting to WebChannel"), and the answer
the page actually received was not the instance's answer at all.

Note what this is **not**: the protobuf answer was not what kept the page at
"Connecting to instance…". The host page's fallback is the WebChannel
anyway, and the page was stuck for a different reason — the answers never
arrived at all, because the cloud had disowned the session (§7.7.2). The
dialect was a *divergence from the reference*; the dead session was the
outage.

The cloud's relay is what makes the rule observable: the inbound request
carries the *page's* content type, and the answer comes back in that
dialect (`internal/rctunnel/httpproxy.go`, `proxiedContentType`).

#### 7.7.2 A session the cloud has disowned ends the session

【一手】 The clone's own log, on the day:

```
[CloudTunnel] forward POST error ... status=400 ... Unknown SID
[CloudTunnel] proxyRequest reqID=13 was canceled by server
```

and the dashboard said **"Instance disconnected / Waiting for connection or
Wi-Fi to resume…"** — while the instance was registered, its row was live
(`status = 1`), and the daemon was answering everything.

The chain: the cloud's forward channel answers `400 Bad Request — "Your
client has issued a malformed or illegal request. Unknown SID That's all we
know."`; the answer is therefore never delivered; the cloud cancels the
browser's request ten seconds later; the host page's liveness probe (10 s)
never completes, and `probeFailed()` is true — the "Instance disconnected"
card.

A 4xx on the forward channel means the cloud does not hold the session any
more. The reference's own log says what it does with that:

```
Connection ended with error: context canceled
Connection ended. Retrying immediately...
Starting V2 remote control connection to: https://jetski-webchannel.googleapis.com:443
```

⇒ The clone now **ends the session** there: `markSessionDead` cancels the
poll, the poll's read returns, and the run loop hands the instance a fresh
handshake and a fresh registration (`internal/rctunnel/client.go`). Four
regression tests pin it (`internal/rctunnel/dead_session_test.go`).

【一手】 The recovery was observed live, on the clone's own restart
(2026-10-26 17:57). The daemon was replaced under the same instance id,
the cloud evicted the new session ten seconds later (the old daemon's
lingering session), and the clone answered the first 4xx the way the
reference does:

```
[17:57:39.029] the cloud disowned the session (forward POST HTTP 400); ending the session
[17:57:39.029] Connection ended with error: the cloud disowned the session: context canceled
[17:57:39.029] Reconnection attempt 0 failed. Retrying in 2s...
[17:57:41.531] connected session sid=mvfFi8t_pRnYO9IrR82InQ gsession=6UQ-GV…
[17:57:48.545] registered instance "pigasus-gear.tailac191.ts.net" (id: 0698e8f6-…-v2)
```

Twenty-five of the page's boot calls were answered with the 400 before the
new registration took hold — the cloud routes by instance id, and the
instance's session was gone until the re-registration landed. After it, the
app's calls were answered again (the state stream, the summaries stream,
the project-updates stream, `GetStatus`).

【一手】 The **reference** is hit by the same cloud-side churn, and it
reacts the way the clone now does — its own log, 2026-10-26 18:03:

```
W1026 18:03:12.880810 WebChannel: XMLHTTP Unknown SID (62843)
I1026 18:03:12.881418 Connection loop exited: WebChannel connection closed
I1026 18:03:12.882084 Connection ended quickly (lasted 11.573896875s). Treating as failure for backoff.
I1026 18:03:12.882147 Connection ended with error: WebChannel connection close
```

⇒ The session churn is the cloud's, not the clone's, and the clone's
reaction is now the reference's reaction, down to the wording.

#### 7.7.3 The Mendel flags answer

【一手】 The reference's answer, captured through the cloud:

```json
{"experimentConfig":{"experiments":[…508 entries…]},"webrtcMeshReady":true}
```

- 508 experiments; the fields present are `keyString` (508), `source` (508),
  `disabled` (243), `string` (122), `json` (7);
- the field order is the proto's own (`key_string`, `disabled`, `string`,
  `json`, `csv`, `source` — `exa.codeium_common_pb.ExperimentWithVariant`),
  because the answer is protojson;
- the remote-control experiments:

| key | value |
|---|---|
| `remote-control-version` | `"v2"` |
| `remote-control-webrtc-mesh` | (enabled — no `disabled` field) |
| `remote-control-proxy-server-url` | `"jetski-webchannel.googleapis.com:443"` |
| `remote-control-silence-timeout-seconds` | `"60"` |
| `remote-control-setting-enabled` | (enabled) |
| `remote-control-companion-url` | `"https://antigravity.google.com"` |

The clone's answer (`internal/remotecontrol/mendel_flags.go`) has the same
shape, the same field order and the same remote-control values — with two
deliberate differences:

| | the reference | the clone | why |
|---|---|---|---|
| the experiment set | all 508, live from the Mendel service | the remote-control subset | the clone cannot fetch the live set: every path tried against `aicode.googleapis.com` answers **404**, with and without the CLI's API key (【一手】) |
| the mesh | enabled, `webrtcMeshReady: true` | **disabled**, and `webrtcMeshReady` left out | the clone has no WebRTC mesh: no data channel, no signalling. With the experiment on, the host page prefers the mesh for the app's calls, and there is no mesh to prefer |

⇒ The clone's page reaches the same state as the reference's page: both
mount the app iframe and both show "Connecting to instance…" — the host
page's overlay stays until the app iframe reports its UI as ready, and
neither instance's app gets there in this environment (§9). The mesh
experiment's value is a *capability* statement, and it is the one place
where the clone's answer deliberately differs from the reference's.

#### 7.7.4 What the view looks like now, live

【一手】 2026-10-26 17:46 — the clone's view at
`https://antigravity.google.com/u/3/r/0698e8f6-…-v2` and the reference's at
`https://antigravity.google.com/u/3/r/0d159523-…-v2`:

| | the clone | the reference |
|---|---|---|
| the app iframe | `https://antigravity.static.usercontent.goog/web/cli-1.4.7/?iframe=true` (1470×780) | the same |
| the page's text | "Skip to main content Connecting to instance... feedback" | the same |
| the daemon's log | no `Unknown SID`, no `canceled by server` | — |
| the app's calls | `GetAllSkills`, `GetUserStatus`, `GetCascadeNuxes`, `ProjectUpdatesStream`, `JetboxSubscribeToState`, `JetboxSubscribeToSummaries` — all answered | — |

⇒ The clone's cloud remote view renders **identically to the reference's**.


## 8. Hard constraints / 硬约束

- **The three server streams must stay open.** The frontend reconnects the
  moment a stream ends; a closed stream is a reconnect storm (the cold
  load was 20.7 s before the streams were kept open, 5.29 s after).
- **`thinkingDuration` is a string.** The object form kills the whole
  frame.
- **Every served step carries `metadata.sourceTrajectoryStepInfo`** with
  `cascadeId`, `trajectoryId` and `stepIndex`. Two steps without an index
  in one conversation collapse into one turn.
- **The store is append-only.** The reference's rows are richer than the
  clone's `Step` shape; a rewrite loses the reasoning, the usage metadata
  and the user config.
- **The version string is the bundle.** `cli-1.4.7-v2` is what makes the
  cloud serve the CLI's own bundle.
- **A restarted daemon keeps its instance id** (the uuid is persisted) but
  takes a fresh tunnel session; the browser's in-flight requests from
  before the restart are answered with HTTP 400 by the cloud (the
  reference's own log shows the same churn).
- **The cloud's `close` frame ends the session.** It is not a stream event
  to keep reading past: the cloud has already disowned the session, and the
  only way back is a new handshake and a new registration (§7.2).
- **A 4xx on the forward channel ends the session too.** `400 "Unknown
  SID"` means the cloud does not hold the session any more: the poll is
  cancelled, the connection is closed, and the run loop hands the instance a
  fresh handshake and registration. Kept alive, the session swallows every
  answer, the cloud cancels the browser's request ten seconds later, and
  the dashboard's liveness probe never completes (§7.7).
- **The default dialect is JSON.** The caller's own content type picks the
  dialect; a caller that names none gets Connect's default, and the
  reference answers the same way. The dashboard's host page names none and
  reads the answers with `JSON.parse` — a protobuf answer breaks the page
  (§7.7).
- **The Mendel flags answer carries the remote-control experiments, with
  the mesh experiment disabled.** The clone has no WebRTC mesh, and a mesh
  that is on makes the host page hold at "Connecting to instance…" (§7.7).
- **The instance id is `<uuid>-v2`.** `remote-control-` is the reference's
  log tag for its remote-control session, not part of the id (§7.3).
- **The registration's answer is the cloud's own chunk.** It is logged
  (`registration answer: "8\n[1,1,7]\n"`); a refusal shows up nowhere
  else.
- **The registry row is scoped to an account.** The URL the user opens has
  to select the account the daemon registered with (`/u/<N>/`); the bare
  `/r/<id>` URL resolves against the browser's *default* account and the
  cloud answers "No instance found. Try switching to a different account."
  The reference's own bare URL fails the same way (§7.4) — this is a
  browser-side account choice, not a protocol difference.
- **Read the registry under the instance's own account.** `ListInstances`
  without a deliberate `X-Goog-AuthUser` sweep answers the question for one
  account only, and a row that is absent there is not absent at all.
- **The instance names the conversation it creates.** The Web UI's header
  falls back to "Untitled Conversation" whenever the summary message the
  instance serves carries no title, and the app never asks for one — the
  app's own summary write carries no title either, so the column is filled
  at creation time and nowhere else (§7.6).
- **An empty title must not erase a stored one.** The app's summary write
  is the instance's own summary echoed back; a title-less write leaves the
  row's title alone.
- **The summaries table is the reference's.** `last_modified_time` and
  `last_user_input_time` are `NOT NULL` without a default, so every write
  has to name them — the clone's insert that named neither failed
  silently and the conversation never reached the index (§7.6).
- **The name has to survive the other binary.** The reference's reconcile
  pass rewrites the summaries rows it can read, with an empty title when
  it cannot generate one (§7.6), so the title is read from the row *and*
  from the metadata cache, and the summary message takes the row's title
  when the message itself carries none.

## 9. Open questions / 存疑

- **The mixed conversation.** A conversation the reference wrote and the
  clone continued shows only the clone's turn in the Web UI. The clone
  serves all seven steps (verified on the wire), and the frontend's own
  renderer groups the slice into turns by the step index. Whether the
  reference shows all seven or only the new turn could not be checked
  live — the reference's own remote view never reconnected during this
  cycle. 【推测】 the reference shows only the new turn, because its
  continued conversation starts a new trajectory.
- **The `xrb` key registry.** The frontend keeps a per-conversation key
  registry in a `useRef`, and it survives a conversation switch. Two
  conversations whose turn ranges overlap therefore produce the same key
  for several turns. Whether the reference's own UI has the same collision
  is unknown.
- **The cloud's instance attachment.** The reference's own remote view sat
  at "Connecting to instance…" for the whole cycle: the cloud creates the
  app iframe (its row is live under account 2) but the browser↔instance
  attachment never completes, while the clone's view attaches over the
  WebChannel. 【推测】 the reference's app attaches over the mesh's data
  channel and the clone's over the tunnel — the two carry the same
  `LanguageServerService` calls, but the reference's multiplexer prefers the
  mesh. The reference's own working remote view was never observed, so
  which of the two the reference's *browser* ends up on is unverified.
  **Corrected 2026-10-26 (later the same day):** the clone's view now
  renders *identically* to the reference's — the same iframe, the same
  "Connecting to instance…" text (§7.7.4) — so the two are no longer
  distinguishable from the outside. What neither of them does is get the
  app iframe to report its UI as ready; that overlay is the host page's
  `iframeInitializing()` state, and it is the last visible difference from
  a working view.
- **The silence timeout.** The reference's binary carries the string
  `Connection ended due to silence timeout (lasted %v). Treating as failure
  for backoff.` and the flag `remote-control-silence-timeout-seconds` = `60`
  (§7.7.3), so the reference ends a connection that has been silent for a
  minute. The clone has no such rule: when the poll ends, it re-opens it on
  the same SID. 【推测】 "silent" counts the frames the cloud sends on the
  backchannel, and the cloud's long poll returns well inside 60 s, so the
  reference's timer is a *failure* detector rather than a keepalive — which
  is why the clone, whose poll also keeps returning, would not trip it
  either. The rule was not implemented, and the exact trigger is not
  recovered from the binary.
- **What the mesh session needs from the instance.** The signaling side is
  recovered (§7.4): the poll's request and response shapes, the endpoint,
  and the CLI's own bearer token. What is *not* recovered is the data
  channel's own protocol: the WebCrypto key vouching the instance pins
  ("Pinned server-vouched WebCrypto public key"), the
  `TransportMultiplexer` that carries the RPCs over it, and the ICE
  configuration's own shape (the dashboard's `stunServers` /
  `turnServers` lists). 【推测】 the mesh is a *transport* and the RPCs over
  it are the same `LanguageServerService` calls the WebChannel carries —
  the reference's multiplexer serves both.
- **Whether the mesh changes the view at all.** The clone's view works
  without it (§7.5), so the mesh is not a gate. Whether the reference's
  view *differs* in any visible way when the mesh carries the traffic
  (a lower latency, a different attach handshake) is unverified.
- **The summaries row the reference wrote for a clone conversation.** The
  reference's start-up reconcile pass rewrites the rows it can read in the
  reference's shape, with an empty title (§7.6) — that is now explained.
  What is not explained is *which* instance the cloud delivered the app's
  `JetboxWriteSummary` to when the row was written in the reference's
  shape: the clone's daemon never writes `app_data_dir` or `raw_summary`
  (grep: only readers), and the account's other live instance at the time
  is the reference's own. 【存疑】 the cloud delivers a summary write to
  one instance of the account — which one, and whether that is decided by
  the page's attachment, was not followed end to end.

## 10. Limitations

- A resumed turn does **not** replay the stored history into the model:
  `pigrt.Runtime.SeedHistory([]pigrt.HistoryTurn)` exists and is not
  wired to the remote-control path.
- The clone's stored steps carry no `metadata` beyond what the stamp
  adds; the reference's carry the usage metadata, the user config and the
  model ids.
- The reference's `raw_summary` blob is not written, and the reference's
  restart notice is not written.
- The title the clone gives a conversation is a **heuristic** (the first
  words of the first message, cleaned); the reference asks the model
  (`GenerateConversationTitle`). The two names therefore differ in wording,
  not in kind — the Web UI shows the clone's name the same way it would
  show the reference's (§7.6).
- The clone ignores the frontend's `initialStepsPageBounds` request field
  and always answers the whole conversation.
- The picker offers only the user's configured models; a Gemini 3.8 Flash
  turn has not been exercised through CloudCode.
- The live browser verification of the last two items was cut short by
  the cloud's degradation (the reference's own permission refresh was
  failing with error 437 in the same window).
- The clone's **JSON** answers are `json.Marshal` of a Go map, so their keys
  come out in alphabetical order; the reference's answers are protojson, so
  its keys come out in field-number order. The bytes differ, the JSON does
  not — every client parses it. The `GetMendelFlags` answer is the
  exception: it is built from structs, in the proto's own field order
  (§7.7.3).
- The clone's Mendel flags are the **remote-control subset**, not the live
  set: the Mendel endpoint is unreachable with the clone's OAuth token
  (every path tried answers 404), so the 502 other experiments of the
  reference's answer are not served (§7.7.3).
- The app iframe never reports its UI as ready — in this environment, for
  **both** binaries: the reference's own view sits at the same
  "Connecting to instance…" overlay. The overlay is the host page's
  `iframeInitializing()` state, and what the app waits for is inside
  Google's web bundle, not in the instance's answers (§7.7.4, §9).
- The registry row is scoped to the account the daemon authenticates as,
  and the **instance** cannot know the browser's account index: the clone
  prints the reference's bare URL (`https://antigravity.google.com/r/<id>`),
  which the browser resolves against its default account. With several
  accounts signed in, that URL lands on the wrong account and the cloud
  answers "No instance found. Try switching to a different account." — the
  reference's own bare URL fails the same way (§7.4). The user has to open
  the dashboard on the account the daemon registered with, or use the
  `/u/<N>/` path.
- The clone's daemon message for `remote-control start` — "Open
  https://antigravity.google.com and look for %q." — is the reference's
  own string 【strings】 (`Open https://antigravity.google.com and look for
  %q.` in the binary). The reference's binary also carries a fourth
  variant, "Open %s on another device and look for this hostname.", whose
  call site was never located.

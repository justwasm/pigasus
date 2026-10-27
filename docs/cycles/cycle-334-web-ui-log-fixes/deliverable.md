# The two errors in the operator's web log — deliverable

**Cycle:** `cycle-334-web-ui-log-fixes`
**Subject:** the operator's own `pigasus web` log carried two UI errors.
Both were real gaps in the clone's `LanguageServerService` surface: the
credits panel's `GetLoadCodeAssist` had **no route at all**, and the
queued card's `DeleteAgentMessage` was routed to the **wrong handler**,
so the message the user threw away stayed in the queue.
**Evidence:** 【一手】 the operator's log
(`out/web-ui-log-fixes/operator_log.txt`); 【一手】 the frontend bundle
the clone serves (`out/web-ui-log-fixes/frontend_calls.txt`); 【一手】 the
live `v1internal:loadCodeAssist` answer of the operator's own account
(`out/web-ui-log-fixes/load_code_assist_live.json`); 【一手】 the clone's
own rig answering the two calls over Connect-RPC
(`out/web-ui-log-fixes/wire_check.txt`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the credits panel** | `ui_client_credits_fetch_failed` — *"[CloudCodeService] loadCodeAssist returned no response"*, twice | the account's own code-assist state, read live from the cloud and passed through |
| **the queued card's delete** | `-32602 Invalid params (conversation_id is required)` — the message stayed queued and ran later | the message is dropped from the queue, by its id |
| **the reference CLI's own delete** | the dashboard's `DeleteRemoteAgentMessage` | unchanged — the two dialects keep their own handler |

---

## 1. The log

The operator ran the clone's own Web UI and pasted the log
(`out/web-ui-log-fixes/operator_log.txt`). Two errors, and nothing else
in it:

```
23:40:46 remotecontrol: web UI error report: {"errorTrace":{"errorId":"ui_client_credits_fetch_failed",
  "stacktrace":"Error: [CloudCodeService] loadCodeAssist returned no response\n    at Object.loadCodeAssist (http://127.0.0.1:8889/main.js:18902:519)", …}}
23:41:19 remotecontrol: web UI error report: {"errorTrace":{"errorId":"ui_client_credits_fetch_failed", …}}
23:52:13 remotecontrol: web UI error report: {"errorTrace":{"errorId":"ui_client_queued_message_delete_failed",
  "stacktrace":"ConnectError: [internal] rpc error -32602: Invalid params (conversation_id is required)\n …", …}}
```

The queue's own lines read the second one back to the reader: `2 waiting`
at 23:52:13 (the delete had already failed) and `3 waiting` at 23:52:36 —
**the message the user deleted was never dropped**, and it ran at
00:06:46.

The rest of the log is the queue working: the messages ran in the order
they were sent, which is the reference's own rule (FIFO, "goes to the
agent together with them, in order" — `internal/remotecontrol/ls_queue.go`).
The 14 minutes between the first queueing and the first run are the turn
that was in flight; the clone logs *message queued* only while a turn
runs, and the queue drained on its own afterwards.

---

## 2. The first error: the credits panel had no route

### 2.1 What the app asks for

【一手】 the Web UI's own bundle
(`internal/rcfrontend/assets/main.js`, verbatim excerpts in
`out/web-ui-log-fixes/frontend_calls.txt`) — the credits panel calls two
methods and reads one member out of the first:

```js
e.retrieveUserQuotaSummary?.(void 0,G)?.then(F=>{r(F.response?.groups|| []);u(F.response?.description)})
        .catch(F=>{b(ig.USER_QUOTA_FETCH_FAILED,F)});
e.loadCodeAssist(void 0,void 0,G).catch(F=>{b(ig.CREDITS_FETCH_FAILED,F)})
```

and the service wrapper is the one that threw:

```js
if(!r.response)throw Error("[CloudCodeService] loadCodeAssist returned no response")
```

`retrieveUserQuotaSummary` had a route (`internal/remotecontrol/dispatch_quota.go`).
**`loadCodeAssist` had none** — and the transport answers `{}` for a
method it does not know. `{}` has no `response` member, so the app threw,
and the clone's own error report wrote the operator's log line.

### 2.2 The RPC

【反编译】 `exa.language_server_pb.LanguageServerService` carries it
(`internal/lsproto/descriptor/language_server.protoset`):

```
LanguageServerService.GetLoadCodeAssist(exa.language_server_pb.GetLoadCodeAssistRequest)
    → exa.language_server_pb.GetLoadCodeAssistResponse
  request:  force_refresh = 1
  response: response = 1  (.google.internal.cloud.code.v1internal.LoadCodeAssistResponse)
```

The answer's member is the **cloud's own** `v1internal:loadCodeAssist`
answer, so the clone does not have to invent one.

### 2.3 What the cloud answers

【一手】 the operator's own account, 2026-10-27 — the live answer of
`https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`
(`out/web-ui-log-fixes/load_code_assist_live.json`; the operator's
address is redacted):

```json
{
  "currentTier": {"id": "free-tier", "name": "Antigravity", …},
  "allowedTiers": [{"id": "free-tier", "isDefault": true}, {"id": "standard-tier", …}],
  "cloudaicompanionProject": "aicode-consumers",
  "gcpManaged": false,
  "paidTier": {"id": "g1-pro-tier", "name": "Google AI Pro",
               "availableCredits": [{"creditType": "GOOGLE_ONE_AI",
                                      "minimumCreditAmountForUsage": "50"}]}
}
```

The tier the session runs on is the free one; `paidTier` is the plan the
account bought. The clone's `internal/auth/plan_tier.go` already reads
this endpoint — but it keeps the label and throws the rest away.

### 2.4 The fix

`internal/auth/code_assist_state.go` reads the endpoint and returns the
answer **verbatim** (`FetchCodeAssistState`), with the keychain token
first — the same order `plan_tier.go` uses, because the on-disk token can
be stale and resolve to another account.

`internal/remotecontrol/dispatch_codeassist.go` routes
`GetLoadCodeAssist` and answers `{"response": <the cloud's answer>}`:

* the answer is cached for **two minutes** (the Web UI's own cache is
  five seconds; the tier changes slowly and every read costs a round
  trip),
* a failed read is **not** an error — the panel is a read, not a
  command: the clone answers with the account's cached plan tier, and
  with an empty `response` if even that is unknown.

【一手】 the clone's own rig, over the Web UI's own Connect-RPC path
(`out/web-ui-log-fixes/wire_check.txt`):

```
POST /exa.language_server_pb.LanguageServerService/GetLoadCodeAssist
{"forceRefresh":true}
→ {"response":{"currentTier":{"id":"free-tier","name":"Antigravity",…}, …}}
```

The account's real state, through the real route.

---

## 3. The second error: the queued card's delete went to the wrong handler

### 3.1 What the app sends

【一手】 the card's delete action in the bundle:

```js
function w6b(a,b){var {deleteAgentMessage:c}=PB(),… r(await p(c({messageId:m,recipient:a}))),g.current?.() …}
```

`a` is the conversation the card belongs to, `m` is the **id of the
pending message** — the `id` the `pendingAgentMessages` update carries
(`internal/remotecontrol/ls_trajectory.go`, `lsPendingAgentMessageJSON`).

### 3.2 Two different RPCs with one name

【反编译】 the two services disagree about the shape of the same method
name:

| service | request | who calls it |
|---|---|---|
| `exa.language_server_pb.LanguageServerService.DeleteAgentMessage` | `message_id = 1`, `recipient = 2` | **the Web UI's queued card** |
| `google.gca.aicode.v1main.DeleteAgentMessage` | `conversation_id = 1`, `message_id = 2` | the reference CLI's own gRPC surface |

(the second one: `out/local-macos/protos/remote_agent_proxy_1.4.4.txt`,
the FileDescriptorProto recovered from the agy 1.4.4 binary.)

The clone's alias table (`internal/remotecontrol/reference_surface.go`)
maps the reference's name onto the dashboard's own
`DeleteRemoteAgentMessage`, whose handler wants `conversation_id` and
`message_id` — and the alias table is consulted **after** the IDE
dialect's own tables. So the Web UI's shape arrived at the dashboard's
handler and was refused:

```
rpc error -32602: Invalid params (conversation_id is required)
```

### 3.3 The fix

The IDE dialect now has its own route, in the table that is consulted
first (`internal/remotecontrol/dispatch_ls.go`):

```go
case "DeleteAgentMessage":
    return d.handleLSDeleteAgentMessage(params)
```

The handler (`internal/remotecontrol/ls_queue_rpc.go`) looks the
message up **by id** — the queue's *index* moves when the queue grows or
shrinks, the id does not — and drops it
(`lsTrajectory.DropPendingByID`). A message that is not waiting anymore
is not an error: the delete is idempotent and the reference's own answer
is empty.

The reference CLI's dialect keeps its own handler: a request that names
a **conversation** and no recipient is handed back to the router
(`ok=false`), the alias table maps the name, and the dashboard's
`DeleteRemoteAgentMessage` answers it as it always did.

【一手】 the clone's own rig, over the Web UI's own path
(`out/web-ui-log-fixes/wire_check.txt`):

```
{"messageId":"conv-x-q1","recipient":"nope"}   → -32004 Cascade not found (nope)
{"conversation_id":"conv-x","message_id":"m1"} → -32012 Message not found  (the legacy handler)
{}                                            → -32602 Invalid params (recipient is required)
```

---

## 4. What was *not* changed

* **The queue's order.** The log shows the `WHEN_IDLE` message running
  before the `NEXT_INVOCATION` one. The reference's queue is FIFO
  ("in order"), so that is the reference's rule, not a defect.
* **`GetAvailableModels`.** The Web UI calls it at startup and swallows
  the failure (`.catch(()=>b.result?.tiers)`), so it never reached the
  operator's log. It is still unrouted — see 存疑.
* **`GetUserStatus`'s `userTier`.** The app's credits reducer reads
  `userStatus.userTier?.availableCredits`, and the clone's own
  `userTier` block carries no credits. That is a different surface
  (the account's own status, not the code-assist state), and it is
  another agent's file.

## 硬约束

1. **The IDE dialect wins over the alias table.** `DeleteAgentMessage`
   with a `recipient` is the Web UI's queued-card delete and goes to the
   queue. A `DeleteAgentMessage` with a `conversation` is the reference
   CLI's own and goes to the legacy handler. The two shapes are
   disjoint, so both keep working.
2. **The queue is dropped by id, never by index.** The id is the one the
   `pendingAgentMessages` update carries and the one the app hands back.
3. **`GetLoadCodeAssist` always answers a `response` member.** The app
   throws on a missing one, so the offline fallback answers the account's
   cached tier (or an empty object) — never an error.
4. **The cloud's answer is passed through, not rebuilt.** The clone does
   not invent a tier: `internal/auth/code_assist_state.go` returns the
   body verbatim.
5. **The keychain token wins.** The on-disk token can be stale and
   resolve to another account — the rule `plan_tier.go` already follows.
6. **The cache is two minutes.** Longer than the Web UI's own five
   seconds, shorter than the tier's own life. The cache is **keyed by
   the active account**: the state belongs to the credential the
   session's turns use, so a switch is answered with a fresh read.

## 存疑

- **`GetAvailableModels` is still unrouted.** The app calls it at
  startup and swallows the failure, so it never reached the log — but
  the tier list the app keeps (`b.result?.tiers`) is empty. It was left
  alone because the model catalogue is another agent's work in this tree.
- **The app's own credits row reads `GetUserStatus`, not
  `GetLoadCodeAssist`.** The clone's `userTier` block carries no
  `availableCredits`, so the panel's *credit* row stays empty even
  though the *fetch* no longer fails. The state this cycle passes
  through does carry the credits (`paidTier.availableCredits`).
- **The live answer was captured on one account.** A paid account whose
  `currentTier` is the paid tier would answer differently; the clone
  passes whatever the cloud says.
- **The queued-card delete was not exercised in a browser.** The wire
  check is a curl against the clone's own rig, and the queue's drop is
  pinned by the dispatcher's own tests — the two together, not a click.

## Limitations

- The measurements are from **one** machine, one date and one operator
  account.
- The `GetLoadCodeAssist` answer costs **one** CloudCode request per
  two minutes of panel use. A machine with no credential answers the
  fallback, not the cloud's state.
- The `DeleteAgentMessage` route drops a **queued** message. A message
  that already ran is not deleted from the conversation's store by this
  route — the dashboard's own `DeleteRemoteAgentMessage` is the path for
  that, and it is unchanged.
- The live capture was made with the operator's own credential; the
  evidence file has the operator's address redacted.

## Files

| | |
|---|---|
| `internal/auth/code_assist_state.go` | the cloud's code-assist state, verbatim |
| `internal/remotecontrol/dispatch_codeassist.go` | the `GetLoadCodeAssist` route, the cache, the fallback |
| `internal/remotecontrol/dispatch_codeassist_test.go` | the answer's shape, the failed read, the cache |
| `internal/remotecontrol/ls_queue.go` | `DropPendingByID` |
| `internal/remotecontrol/ls_queue_rpc.go` | the IDE dialect's `DeleteAgentMessage` handler |
| `internal/remotecontrol/ls_delete_message_test.go` | the card's delete, the wrong message, the two dialects |
| `internal/remotecontrol/dispatch_ls.go` | the route for the IDE dialect |
| `internal/remotecontrol/dispatch.go` | the router: the code-assist table sits next to the quota table |
| `out/web-ui-log-fixes/operator_log.txt` | the operator's log, and its reading |
| `out/web-ui-log-fixes/frontend_calls.txt` | the bundle's own calls, verbatim |
| `out/web-ui-log-fixes/load_code_assist_live.json` | the cloud's live answer (the address redacted) |
| `out/web-ui-log-fixes/wire_check.txt` | the clone's own rig answering the two calls |

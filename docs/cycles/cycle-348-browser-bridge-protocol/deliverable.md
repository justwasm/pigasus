# Cycle 348 deliverable: three places where the bridge was not speaking the project's dialect

**Cycle:** `cycle-348-browser-bridge-protocol`
**Subject:** cycle 344 taught the clone the bridge's protocol; this
cycle goes back to the project's own runtime and compares it with the
clone's implementation line by line. Three differences came out.
**Method:** the sibling project's source read first-hand
(`../open-claude-in-chrome/host/tool-runtime.js` and
`host/tool-definitions.js`), and each difference reproduced against
the clone's own fake bridge before it was changed.

---

## 0. The short answer

The bridge spoke the right *messages* but not always the right
*dialect*. Three fixes:

| | before | now |
|---|---|---|
| **the primary dies** | the client was stranded: every later call failed with "the extension is not connected", for good | the client promotes itself after two seconds, or joins whoever won the port |
| **`"tabId":"590534703"`** | the extension was handed a string and answered "unknown tab" | the bridge coerces it to a number, and the three array-shaped arguments to arrays |
| **`navigate`'s URL** | the description said "must be absolute (https://… or file://…)" — so the model never tried the two other forms the tool takes | the description says what the bridge accepts: with or without a protocol, and `"forward"` / `"back"` |

Each one is small. The first one is the difference between a browser
that comes back and a browser that does not.

---

## 1. The primary's death, and the client's stranded hour

### What the project does

The project's runtime is either the **primary** (it owns the port and
speaks to the extension's native host) or a **client** (it asks the
primary). The native host connects to the primary's port and retries
every 1.5 s, so the primary is a role that has to be re-taken whenever
it dies.

The project's client does not simply wait. When its socket to the
primary closes, it schedules a **promotion**: two seconds later it
tries to bind the port itself. If the bind fails because somebody
else already took it, it joins that process instead — the same race,
the same order, either way the browser is reachable again.

【一手 — `../open-claude-in-chrome/host/tool-runtime.js`: the promotion
is scheduled on the primary socket's close, after a 2000 ms delay; the
client's own `waitForLink` polls for "a usable browser link in
*whichever* role this process ends up in", because "a client whose
primary just died may reconnect to a new primary, or promote itself".】

### What the clone did

Nothing. The clone's `pump` read the connection until it closed, set
the link to nil, and returned. The client stayed a client forever,
with a dead link, and every call answered:

```
the Open Claude in Chrome extension is not connected
```

which is the right *sentence* and the wrong *story* — the extension
was there, the primary was gone, and the clone had no way to become
the primary.

### What the clone does now

`internal/browserbridge/bridge.go`:

- `pump` notices that the link it was reading was the link, and that
  this client is not the primary (the primary has a listener; the
  client does not), and that the client is not closed — then it
  starts a promotion;
- `promote` waits the project's two seconds, tries to bind the port,
  and either serves the primary's own protocol from then on or — if
  the port was taken in the meantime — joins the winner as a client.

The **primary never promotes**: there the lost connection is the
extension's native host, and the primary waits for it to come back
(the native host retries on its own).

### The tests

Two, both against the clone's own fake bridge:

| | |
|---|---|
| `TestClientPromotesItselfWhenThePrimaryDies` | the primary dies; within a few seconds the port answers again, and the newcomer gets the primary's own `client_ack` |
| `TestClientJoinsTheWinnerWhenAnotherProcessTakesThePort` | the primary dies and another process takes the port first; the client joins *that* one (it greets it) |

Both fail without the change — the first one fails with "the client
never took the port", the second with "the client never joined the
new primary".

---

## 2. The arguments that arrive off the wire as the wrong type

### What the project does

The project's runtime coerces the arguments before the call
(`coerceArgs` in `host/tool-runtime.js`):

- `tabId`: a string becomes a number (`Number(args.tabId)`);
- `coordinate`, `start_coordinate`, `region`: a string is parsed as
  JSON, and becomes an array.

The reason is the wire, not the model: a number written as a string is
still a string on the JSON-RPC call, and the extension compares it
against real numbers and real arrays. Without the coercion, a
perfectly reasonable `{"tabId": "590534703"}` is answered as an
unknown tab.

【一手 — the same file; the function is documented in place as "Args
are coerced (string→number/array) before the call so this is safe to
invoke directly with values straight off the MCP wire."】

### What the clone did

Nothing: the arguments were forwarded verbatim.

### What the clone does now

`internal/browserbridge/coerce.go` — `coerceArgs`, called from
`Call`. Two deliberate differences from the project's:

- a `tabId` that does not parse as a number is **left as it
  arrived**. The project sends `NaN`, which JSON cannot carry, so its
  `JSON.stringify` turns it into `null` — the extension then reports
  "no such tab" instead of "your tab id is not a number". Leaving the
  value alone keeps the extension's own complaint accurate.
- a coordinate that parses as JSON but not as an **array** is left
  alone too: `12` is a number, not a point, and guessing where the
  pair should be is worse than saying so.

The tests pin all of it: the string tab id, the three array-shaped
arguments, the values that must survive untouched, and the calls whose
arguments are not an object at all.

---

## 3. The URL `navigate` takes

### What the project does

The project's `navigate` takes a URL **with or without a protocol**
(https:// is assumed) and takes `"forward"` and `"back"` as URLs, for
the browser's history.

【一手 — `../open-claude-in-chrome/host/tool-definitions.js`, the
`navigate` entry: the url parameter is described as "The URL to
navigate to. Can be provided with or without protocol (defaults to
https://). Use \"forward\" to go forward in history or \"back\" to go
back in history."】

### What the clone did

Say something else. The clone's own description:

> Point a tab at a URL and wait for the page to load. The URL must be
> absolute (https://… or file://…).

and the schema's `url` property: "The absolute URL to load."

That is a **narrower contract than the bridge has** — and the model's
only source for the contract is the description. So the model was
never told that `example.com` works, and never told that `back`
works; it read a rule that the tool does not enforce.

### What the clone does now

The description says what the bridge accepts, in the clone's own
words:

> Point a tab at a URL and wait for the page to load. The URL may be
> written with or without a protocol (https:// is assumed), and
> "forward" or "back" move through the browser's history instead of
> loading a page.

and the schema's `url` property says the same. A test
(`TestNavigateDescriptionMatchesTheBridgesOwnSemantics`) pins that the
word "absolute" does not come back, and that the three words the model
needs — "protocol", "forward", "back" — are all there.

---

## 4. 硬约束 (hard constraints)

- **No code and no text from the project.** The project is PolyForm
  Noncommercial 1.0.0. The protocol is an interface and the clone
  implements it independently; every description above is the clone's
  own prose, and the three differences are re-derived from the
  project's *behaviour*, not copied from its source.
- **The wire format does not move.** The four message shapes, the
  4-byte length prefix and the newline-delimited JSON are the
  project's, and they stay byte for byte.
- **The primary never promotes.** Only a client does. A primary that
  lost the native host waits for the native host, which retries on
  its own every 1.5 s; promoting there would tear the port away from
  the process that owns it.
- **The advertised catalogue does not change.** The bridge tools stay
  inside the browser agent's own tool set; the coercion happens on
  the way out and changes nothing the model sees.

## 5. 存疑 (open questions)

- **How often does the primary actually die?** The promotion is
  written to the project's behaviour and tested against a fake
  bridge, but this machine has not been observed losing a primary in
  the middle of a browsing task. The 1.5 s native-host retry means
  the recovery is quick *if* the port is free.
- **Should a client that lost its primary reject its pending calls?**
  The project's runtime rejects them at once; the clone's calls wait
  for their own timeout (60 s). Failing them early would be
  friendlier to the model — it is not done here, because the
  promotion usually reconnects the client before the timeout and the
  pending call may yet be answered.
- **The coercion's `region` is only used by two of the seventeen
  tools.** The third array-shaped argument the project coerces
  (`start_coordinate`) and the clone's own coordinate parameter are
  the same shape; the project coerces them all three, and so does the
  clone.

## 6. Limitations

- **The bridge's own tests are offline.** The live bridge test
  (`PIGASUS_BRIDGE_LIVE=1`) exercises the real extension on this
  machine; the promotion tests use the clone's fake bridge, because
  killing a live primary would take the user's own session with it.
- **The promotion's window is the project's, not measured.** Two
  seconds comes from the project's runtime; the clone has no
  measurement of its own that would justify a different number.
- **The argument coercion covers four arguments.** A call whose
  arguments are not an object is forwarded untouched, and so is a
  value that does not parse — the bridge does not second-guess the
  model.

## Files

| | |
|---|---|
| `internal/browserbridge/coerce.go` | **new** — the argument coercion |
| `internal/browserbridge/coerce_test.go` | **new** — what is coerced, and what is not |
| `internal/browserbridge/bridge.go` | the client's promotion; `dial` now reuses `join` |
| `internal/browserbridge/promote_test.go` | **new** — the promotion, and the race for the port |
| `internal/browserbridge/bridge_test.go` | the fake bridge can be stopped and can record greetings |
| `internal/tools/browserbridge.go` | `navigate`'s description and schema |
| `internal/tools/browserbridge_test.go` | the description is pinned |
| `docs/guide/tools.md` | *Three details of the wire the clone now matches* |

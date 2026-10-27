# Cycle 344 deliverable: the browser the user is looking at — the Open Claude in Chrome bridge, spoken directly

**Cycle:** `cycle-344-browser-bridge`
**Subject:** the user's own words — *"我觉得还是不要这样，不如接入
../open-claude-in-chrome/"* — after cycle 343 had reached the user's
browser the long way (attach over the DevTools protocol, seed the
profile, hope the port is open).
**Method:** the sibling project's source read first-hand, its wire
protocol measured against the running bridge on this machine, and the
clone's own client driven against the user's open browser.

---

## 0. The short answer

The clone now speaks the **Open Claude in Chrome** bridge protocol in
Go, directly — no MCP, no node — and drives the browser the user
already has open: their tabs, their logins, their session.

| | before | now |
|---|---|---|
| reaching the user's browser | a debugging port, a profile copy, a toggle in `chrome://inspect` | the extension that is already in their browser |
| the bridge | the project's own MCP server, in node | the clone's own client, in Go (`internal/browserbridge`) |
| the tools | the clone's twenty-three `browser_*` tools | seventeen bridge tools, named as the project names them |
| the switch | `--use_local_chrome` → attach over CDP | `--use_local_chrome` → the extension when it is there, the DevTools road when it is not |

Measured on this machine: the clone's client joined the running bridge
and read the user's real tab list in **10 ms** — the same list the
project's own client returns over the same port.

---

## 1. Why the previous road was walked and then dropped

Cycle 343 got the user their session, over the DevTools protocol. It
costs, on Chrome 136 and later:

- remote debugging has to be **turned on by hand**
  (`chrome://inspect/#remote-debugging`), and the port behind that
  toggle is 【二手 — the issue tracker】 9222, not measured;
- the profile of another application is protected from an app without
  Full Disk Access on macOS, so the seeding is skipped exactly where it
  is wanted;
- Chrome 136+ **ignores** `--remote-debugging-port` on the default
  profile.

The alternative the user named is a project in the same workspace:
`../open-claude-in-chrome`. It is a Chrome extension plus a native
messaging host, and it drives the browser the user already runs — with
their logins, their tabs, their extensions — with no flag and no
profile copy.

【一手 — the project's own `README.md`, `extension/manifest.json` (the
`debugger` and `nativeMessaging` permissions), `host/native-host.js`,
`host/tool-runtime.js`】

---

## 2. The project, read first-hand

```
extension/manifest.json     the extension: MV3, "debugger", "nativeMessaging", <all_urls>
extension/background.js     the tool handlers; the reply is tool_response / tool_error
host/native-host.js         Chrome's native messaging ⇄ TCP, a dumb pipe
host/tool-runtime.js        the primary/client multiplexer, the tool_request protocol
host/tool-definitions.js    26 tool definitions (name, description, zod parameter shape)
```

**The licence is PolyForm Noncommercial 1.0.0.** So the clone copies
**no code and no text** from the project: it re-implements the wire
protocol (an interface) and writes its own tool descriptions in its own
prose. The tool *names* and the *argument shapes* are the protocol's
vocabulary — the extension dispatches on them — and they are used as
they are.

【一手 — `LICENSE` (PolyForm Noncommercial 1.0.0), `host/tool-definitions.js`】

On this machine the project is **installed and running**:

| | |
|---|---|
| the native host manifest | `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/com.anthropic.open_claude_in_chrome.json` → `~/.config/open-claude-in-chrome/native-host-wrapper.sh` → `host/native-host.js` |
| the port | 18765 — the project's default (`DEFAULT_PORT`); `~/.config/open-claude-in-chrome/config.json` may name another one |
| who holds it | a primary (the project's MCP server, in node) and two clients connected |

---

## 3. The protocol

Newline-delimited JSON, both directions, over TCP on 127.0.0.1.

```
client  → {"type":"client_hello"}
primary → {"type":"client_ack","clientId":"1148"}

client  → {"type":"tool_request","id":"t2","tool":"get_page_text","args":{"tabId":…}}
primary → {"id":"t2","type":"tool_response","result":{"content":[{"type":"text","text":"…"}]}}
primary → {"id":"t2","type":"tool_error","error":"Unknown tool: …"}
```

【一手 — `host/tool-runtime.js` (`client_hello` → `client_ack`;
`tool_request` forwarded with the id it arrived with), and the measured
exchange above】

The result is **MCP-shaped**: a `content` array of typed parts, of which
the text ones carry the answer. The clone renders them into the text
the model reads, and hands over anything it does not recognise as it
arrived — the model can read JSON, and guessing would lose information.

### 3.1 Two roles, and the 500 ms rule

A connection is classified by its **first message**:

- `client_hello` → a **client** (another process joining the port);
- anything else, or **silence past 500 ms** → the extension's **native
  host**.

The native host says nothing until the extension has something to say,
so silence is the native host. The project's own runtime uses the same
rule and the same 500 ms.

【一手 — `host/tool-runtime.js`: the early-data listener reads the first
line, and a `classifyTimeout` of 500 ms decides the rest】

### 3.2 The clone takes both roles

- **client** — the port is already served (the project's own MCP server,
  another agent): the clone greets, joins, and leaves that server alone.
- **primary** — the port is free: the clone binds it and waits. The
  extension's native host retries every 1.5s, so it finds the clone
  within a second or two, and the browser becomes drivable **with
  nothing else running** — no node, no MCP server, no second process.

As a primary the clone also **serves the project's other clients**: a
tool request from another client is forwarded to the native host with a
prefixed id, and the answer is routed back with the client's own id.
Taking the port must not break a Claude Code session that starts later.

A second native host is refused with the project's own words — *"Another
browser profile is already connected."* — and dropped.

### 3.3 Measured, against the running bridge

```
$ python3 probe.py           # a hand-written client, before the Go one existed
hello -> {'type': 'client_ack', 'clientId': '1148'}
<- 0.00s {"id":"probe_1","type":"tool_response","result":{"content":[{"type":"text",
     "text":"{\"availableTabs\":[{\"tabId\":590534703,\"title\":\"127.0.0.1\",…}]}"}}]}}

$ PIGASUS_BRIDGE_LIVE=1 go test ./internal/browserbridge/ -run TestLiveBridge -v
    bridge_test.go:313: tabs: {"content":[{"type":"text","text":"{\"availableTabs\":[…]}"}]}
--- PASS: TestLiveBridge (0.01s)
```

The tab list is the user's own: their local development tabs, the
pages they have open. The round trip is **10 ms**.

---

## 4. The tool surface

Seventeen tools, in `internal/tools/browserbridge.go`, named as the
project names them:

`tabs_context_mcp` · `tabs_create_mcp` · `tabs_close_mcp` ·
`navigate` · `computer` · `find` · `form_input` · `get_page_text` ·
`read_page` · `javascript_tool` · `read_console_messages` ·
`read_network_requests` · `resize_window` · `upload_image` ·
`file_upload` · `set_tab_focus` · `switch_browser`

Each is one bridge call with the project's own argument shape; the
descriptions are the clone's own prose.

The project's **recorder, plan, shortcut and config tools are left
out** — they belong to a Claude Code session, not to a browsing task.

### 4.1 They are not advertised

The advertised catalogue (`internal/tools/catalog.go`) is the
reference's own list of **sixty** tool names, and it stays at sixty: the
bridge tools are the **browser agent's** alone, in their own registry
(`internal/tools.BridgeCatalog`), and the main catalogue does not carry
them. This is pinned by a test
(`internal/tools/browserbridge_test.go`: "the advertised catalogue
carries the bridge tool …").

---

## 5. The switch

`--use_local_chrome` — *"Whether to use local chrome"* 【strings — the
1.3.4 strings dump; the flag table in `out/local-macos/FINDINGS.md` §2,
item 7】 — is the reference's own knob for the user's browser.

The clone reads it as what it says, and reaches the user's Chrome
through the extension when the extension is there. When it is not, the
clone falls back to the DevTools road of cycle 343 — the reference's
own mechanism for the same flag.

| | |
|---|---|
| no flag | the DevTools surface: attach to a running Chrome, or start a visible one |
| `--use_local_chrome` | the **live** surface when the extension answers, the DevTools surface otherwise |

The surface decides three things at once, so they cannot disagree:
the tool set, the prompt, and the probe.

- **the tool set** — `browsersubagent.Tools(reg)`
  (`internal/browsersubagent/surface.go`);
- **the prompt** — the lead sentence follows the surface. The managed
  surface speaks of the Chrome DevTools Protocol; the live surface
  speaks of the user's own browser, and there the reference's own rule
  comes back to life ("**Do NOT Attempt to Launch Chrome**", 【strings】
  0x1036a448d) — because the browser is already there;
- **the probe** — `/browser` asks the extension for the session's tabs
  before the model is asked. It opens nothing and moves nothing, and it
  is the only way to know that the extension is attached to the browser
  the user is actually using.

---

## 6. What changed

| | |
|---|---|
| `internal/browserbridge/bridge.go` | **new** — the protocol: the two roles, the classification, the multiplexer |
| `internal/browserbridge/session.go` | **new** — the shared connection, the policy, the check |
| `internal/browserbridge/bridge_test.go` | **new** — a fake bridge, the two roles, the client multiplexing, the project's config file, and a live test |
| `internal/tools/browserbridge.go` | **new** — the seventeen tools, the rendering of the answer |
| `internal/tools/browserbridge_test.go` | **new** — the surface, the rendering, and the catalogue that does *not* carry it |
| `internal/browsersubagent/surface.go` | **new** — the two surfaces, the choice, the bridge tool set |
| `internal/browsersubagent/surface_test.go` | **new** — the surfaces, the prompt, and a live test |
| `internal/browsersubagent/browser_subagent.go` | the prompt follows the surface |
| `internal/cliapp/slash_browser.go` | the probe follows the surface; the tool set follows it |
| `internal/cliapp/tui_runmode_agent.go` | the tool set follows the surface |
| `internal/printmode/print_pig.go` | the tool set follows the surface |

---

## 7. 硬约束 (hard constraints)

- **The bridge is the project's own protocol.** The clone speaks it; it
  does not embed the project, and it does not depend on node, on the
  project's MCP server, or on any process the user has to start.
- **The extension has to be there.** Without it — not installed, not
  enabled, not running in the browser the user is using — the live
  surface cannot answer, and the clone says so:
  *"the Open Claude in Chrome extension is not connected: start Chrome
  with the extension installed and enabled"*.
- **One browser at a time.** A second native host is refused with the
  project's own message.
- **The advertised catalogue stays at sixty names.** The bridge tools
  are the browser agent's, not the conversation's.
- **The tool names and the argument shapes are the project's.** They are
  the protocol's vocabulary; the descriptions are the clone's own.
- **Nothing of the project's code or text is copied** — the licence is
  PolyForm Noncommercial.

## 8. 存疑 (open questions)

- **The upstream's own client behaviour is 【推测】 beyond the parts
  that were measured.** The measured parts: the greeting, the request,
  the answer, the error, the 500 ms classification. The parts that were
  read but not exercised: the `recording_*` events the extension sends
  on its own, and the `switch_browser` hand-off (which releases the
  browser's hold "for ~15s" — the clone does not implement that
  hand-off and simply calls the tool).
- **The primary role was measured only against a fake native host.** The
  live runs on this machine found the port already served, so the clone
  joined it as a client; the primary path (bind, wait, serve) is
  exercised by unit tests over a fake native host and a fake client.
- **The 60 s tool timeout is the project's own** (`tool-runtime.js`); the
  clone's `DefaultTimeout` is the same number, but the reference CLI's
  own timeout for its MCP browser tools is not recovered.
- **Whether the reference's own `--use_local_chrome` is the same
  promise** — "the user's own Chrome" — is 【strings】 only. Its
  mechanism is the DevTools protocol (an MCP server in node); the
  clone's mechanism is the extension. The promise is the same.

## 9. Limitations

- **The live surface needs the user's own extension.** It is the user's
  browser, their tabs and their logins: the tools act on what they see.
  A task that opens ten tabs leaves ten tabs.
- **The bridge surface is not the reference's surface.** The reference
  advertises twenty-three `browser_*` tools and drives them over MCP
  (`mcp(chrome_devtools/*)`); the clone's live surface is the
  extension's seventeen, and the clone's managed surface is its own
  twenty-three. A model that was written against one is not
  interchangeable with the other — the *names* differ, and the guide
  says so.
- **The primary role's client multiplexing** forwards requests to the
  native host and routes the answers back; it does not implement the
  project's recording events, so a Claude Code session that records
  through the clone's primary would not get its recordings.
- **`--use_local_chrome` picks the live surface only if the extension
  answers at that moment.** The check is one call; an extension that is
  installed but not running falls back to the DevTools road.
- **The live tests need the user's browser.** They are off by default
  (`PIGASUS_BRIDGE_LIVE=1`), because a test must not touch the user's
  browser without being asked to.
- **Cycle 343's work is untouched** by this cycle: the DevTools road
  stays as the fallback, and its profile seeding stays as it was.

## Files

| | |
|---|---|
| `internal/browserbridge/bridge.go` | **new** — the protocol |
| `internal/browserbridge/session.go` | **new** — the shared connection |
| `internal/browserbridge/bridge_test.go` | **new** — the two roles, the fake bridge, the live test |
| `internal/tools/browserbridge.go` | **new** — the seventeen tools |
| `internal/tools/browserbridge_test.go` | **new** — the surface and the rendering |
| `internal/browsersubagent/surface.go` | **new** — the two surfaces |
| `internal/browsersubagent/surface_test.go` | **new** — the surfaces and the live test |
| `internal/browsersubagent/browser_subagent.go` | the prompt follows the surface |
| `internal/cliapp/slash_browser.go` | the probe and the tool set follow the surface |
| `internal/cliapp/tui_runmode_agent.go` | the tool set follows the surface |
| `internal/printmode/print_pig.go` | the tool set follows the surface |
| `docs/guide/tools.md` | *The live browser* |

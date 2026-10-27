# Cycle 343 deliverable: the browser is the user's own — no node, a window, and the session that is already logged in

**Cycle:** `cycle-343-browser-session`
**Subject:** a user report about the browser stack — "最好 self contained，
而且支持非 headless 模式，并且能直接使用当前用户的 session 而不是独立的干净的
session，省的重新登陆" — and the rework that answers it.
**Method:** the 1.4.7 binary read first-hand (`strings`), the clone's own
browser client read and measured, and two real-Chrome runs on this machine
(one that starts the browser, one that attaches to a running one).

---

## 0. The short answer

The clone's browser stack was self-contained already, and the other two
wishes were not met. All three are met now.

| wish | before | now |
|---|---|---|
| no external node dependency | met — the clone speaks the DevTools protocol itself and never shells out to node or npm | met, and now *documented and pinned* (§4) |
| non-headless mode | the flag existed (`--local_chrome_headless=false`) but the client started a **headless** browser by default, and the flag table's default (`true`) made the visible mode a hidden opt-in | the browser the clone starts **shows a window** by default; the reference's flag is honoured as soon as it is given (§5) |
| the current user's session | the clone started a **clean** Chrome with a profile of its own — every site asked for a login again | the clone **attaches to the Chrome the user already runs**, and drives a tab of its own in it; when there is no such Chrome, it starts one and copies the user's session into the profile (§3) |

The reference reaches the same place by a different road — an npm package
and a node runtime. The clone keeps its own road, and gets the same
browser.

---

## 1. The reference's browser, read first-hand

【一手 — `strings -a ~/.local/bin/agy`, the 1.4.7 binary】

The reference's `/browser` is an **agent**, not a tool call:

> Invoke a browser agent for web tasks.

Its prompt lead is `You are a browser agent.` and the agent's own rules
include:

> **Do NOT Attempt to Launch Chrome**: You must NEVER attempt to start,
> launch, or spin up Chrome yourself.

and, when the browser is not reachable:

> Explicitly ask the user to enable remote debugging in Chrome by
> navigating to: `chrome://inspect/#remote-debugging` and checking the
> appropriate settings, or ensuring that Chrome is running with debugging
> enabled.

So the reference **does not start a browser of its own**: it attaches to
the Chrome the user has running, and the user's session — the cookies,
the logins — is the session the agent works in. That is exactly what the
user asked for.

The reference's mechanism is a node package. Its embedded launcher is a
shell script:

```sh
set -euo pipefail
log() { echo "chrome-devtools-mcp launcher: $*" >&2; }
# node_ok reports whether $1 is a Node.js >= 20.
node_ok() {
  [ -n "$1" ] && [ -x "$1" ] &&
    "$1" -e 'process.exit(parseInt(process.versions.node, 10) >= 20 ? 0 : 1)' >/dev/null 2>&1
path_node="$(command -v node || true)"
if command -v npx >/dev/null 2>&1 && node_ok "$path_node"; then
  log "using npx with Node.js $("$path_node" --version)"
  exec npx -y chrome-devtools-mcp@1.10.1 "$@"
```

and when there is no node it downloads one:

```sh
node_dir="${CHROME_DEVTOOLS_MCP_NODE_DIR:-/tmp/node-v22.20.0}"
dir="${CHROME_DEVTOOLS_MCP_DIR:-/tmp/chrome-devtools-mcp-1.10.1}"
entry="$dir/package/build/src/bin/chrome-devtools-mcp.js"
... "https://registry.npmjs.org/chrome-devtools-mcp/-/chrome-devtools-mcp-1.10.1.tgz" ...
```

The tools the agent gets are the MCP server's, with the reference's own
prefix: `mcp_chrome_devtools_navigate_page`,
`mcp_chrome_devtools_list_console_messages`,
`mcp_chrome_devtools_list_network_requests`,
`mcp_chrome_devtools_take_screenshot`,
`mcp_chrome_devtools_take_snapshot`, `mcp_chrome_devtools_select_page`,
`mcp_chrome_devtools_performance_start_trace`, … — eighteen names, all of
them the MCP server's, none of them the DevTools protocol's.

The permission patterns are the MCP call forms: `mcp(chrome_devtools/*)`
and `mcp(chrome-devtools/*)`.

**The clone does not adopt this.** The user asked for a self-contained
stack, and the reference's road needs a node runtime, an npm download at
run time, and an MCP server process. The clone already had the other
road — a DevTools protocol client of its own (`internal/browsercdp`) — and
this cycle makes that road reach the **same browser the reference
reaches**.

---

## 2. What the clone did before

`internal/browsercdp` had two roads and no order between them:

```go
func startOrConnectBrowser(ctx context.Context) (*Client, error) {
	cfg := CurrentConfig()
	if attachRequested(cfg) {
		return attachLocalChrome(ctx, cfg)
	}
	return launchChrome(ctx, cfg)
}
```

`attachRequested` is true only when the user passes `--use_local_chrome`
or sets `CHROME_REMOTE_DEBUGGING_PORT`. So the **default** was the
second road: start a Chrome of the clone's own, headless, with a profile
of its own under the user's cache dir. Every site the agent visited asked
for a login — the clean session the user complained about.

The discovery of a running Chrome existed, and was good:

```go
func localChromePorts(cfg Config) []string {
	// the port the DevToolsActivePort file names, then the environment
	// override, then the configured port
}
```

but it was only consulted on the road the user had to ask for by name.

---

## 3. The policy now

### 3.1 Attach first

The order is the user's Chrome first, and the clone's own browser only
when there is none:

```go
func startOrConnectBrowser(ctx context.Context) (*Client, error) {
	cfg := CurrentConfig()
	if attachRequested(cfg) {
		return attachLocalChrome(ctx, cfg)
	}
	// The user's own Chrome first: attaching to it is what makes the
	// session the user's.
	if port, wsURL, err := resolveRunningChrome(ctx, cfg); err == nil {
		return connectCDP(ctx, wsURL, nil, port)
	}
	return launchChrome(ctx, cfg)
}
```

`--use_local_chrome` keeps its meaning: attach, and never start one.

### 3.2 A tab of the clone's own

A Chrome the clone attaches to has the user's tabs open. chromedp attaches
to *a* page target — the first one the browser offers — and the clone's
tools would then navigate the user's tab away.

The clone opens a tab of its own instead (`PUT /json/new` on the DevTools
endpoint, the target id handed to chromedp's `WithTargetID`), and closes
it again when the client stops. The session is the user's — it is their
profile the browser runs — but the tabs are theirs.

The real-Chrome test pins it: after the run, the browser has **two** page
tabs, and the one the Chrome was started with still shows `about:blank`.

### 3.3 Seed the profile the clone starts

When the clone has to start a browser, it copies the user's session into
the profile it starts with (`internal/browsercdp/profile.go`): the
cookies, the saved passwords, and the `Local State` that holds the key
they are encrypted with.

The preferences are deliberately **not** copied: they carry the chrome
around the session (the theme, the extensions, "restore the last session"),
and a browser that opens the user's last twenty tabs is not what the
clone's tools drive.

The copy is best effort and never fails the browser. Where a file cannot
be read, it is skipped.

### 3.4 The window

The browser the clone starts shows a window. The reference's *flag table*
is untouched — `--local_chrome_headless` still defaults to `true` in
`internal/cli/parser_flags.go` — but the clone honours the flag once it
is **given**, and its own default is the visible browser.

---

## 4. Measured on this machine

Chrome 155.0.8059.85, installed and running.

**The clone never shells out to node.** `grep -rn "npx\|nodejs\|\"node\""`
over `internal/` and `cmd/` finds usage strings and comments only — the
one hit is the reference's own `mcp add fs npx -y …` example in the MCP
help text. There is no npm package, no node download, no MCP server
process: the browser is Chrome and the protocol is a websocket.

**macOS protects the Chrome profile.** The measurements, from a Go
program and from the shell alike:

| call | result |
|---|---|
| `os.ReadDir("~/Library/Application Support/Google/Chrome")` | `operation not permitted` |
| `os.ReadFile("…/Chrome/Local State")` | `operation not permitted` |
| `os.Stat("…/Chrome/Default/Cookies")` | nil — the file exists |
| `os.ReadFile("…/Chrome/Default/Network/Cookies")` | `no such file or directory` (this Chrome keeps the cookies in `Default/Cookies`) |

The protection falls on the *contents*, not on the names. So on this
machine:

- the **seeding** cannot work — the cookies cannot be read. It is
  best effort by design and the browser starts with an empty session
  instead of failing.
- the **`DevToolsActivePort` discovery** cannot read the file either, so
  the port probe is the road that works: `--cdp_port` (9222 by default).

Chrome 136 and later ignore `--remote-debugging-port` on the default
profile; the way to turn remote debugging on for a running Chrome is the
page the reference's own agent prompt names,
`chrome://inspect/#remote-debugging`, which serves on **9222**
【二手 — the chrome-devtools-mcp issue tracker, "the 'Server running at'
indicator shows port 9222"】. That is why the configured port is the
fallback the clone reaches for, and why the error message names that
page:

```
no Chrome browser is available: … no Chrome is listening on port 9222 … —
start Chrome with --remote-debugging-port=9222, or turn remote debugging
on for the running Chrome at chrome://inspect/#remote-debugging, or drop
--use_local_chrome to let the clone start its own
```

**Both roads work.** Two real-Chrome tests, off by default, on with
`PIGASUS_BROWSER_TEST=1`:

| test | what it drives |
|---|---|
| `TestBrowserCDP_RealChrome` | the browser the clone **starts** — navigation, evaluation, console, network |
| `TestBrowserCDP_RealChromeAttaches` | a Chrome that was **already running** — the clone opens its own tab in it, drives it, and leaves the tab the browser was started with alone |

Both pass on this machine. The visible mode passes too
(`PIGASUS_BROWSER_TEST_VISIBLE=1`): a window opens on the desktop, the
tools drive it, and it closes with the clone.

The old real-Chrome test was **broken** before this cycle: it set
`CHROME_REMOTE_DEBUGGING_PORT` to a free port and expected a browser,
which the attach-only policy could not give it. It is fixed and split
along the two roads.

---

## 5. The four flags

The reference's four hidden flags stay exactly as they were — same names,
same types, same defaults, same usage strings, still hidden:

| flag | reference's default | the clone's reading |
|---|---|---|
| `--cdp_port` | `9222` | the port of the local Chrome; also the fallback of the discovery |
| `--local_chrome_headless` | `true` | honoured when **given**; the clone's own default is a visible browser |
| `--local_chrome_user_data_dir` | `""` | the profile the started Chrome uses; a profile the user names is never overwritten by the seeding |
| `--use_local_chrome` | `false` | attach, and never start a browser |

One divergence, on purpose and on the record: the reference's flag
*default* for the headless switch stays `true` in the clone's flag table
(parity), but the clone's **policy** default is the visible browser. The
user asked for a browser that can be watched and taken over, and the
browser is now the user's own — a hidden window makes that impossible.

---

## 6. What changed

| | |
|---|---|
| `internal/browsercdp/client.go` | attach first; the clone's own tab in a Chrome it attached to; the launch arguments in their own function; `CheckBrowser` probes the running Chrome first |
| `internal/browsercdp/config.go` | the policy default is a visible browser |
| `internal/browsercdp/profile.go` | **new** — the session seeding (cookies, passwords, the key) |
| `internal/browsercdp/session_test.go` | **new** — the discovery, the tab, the arguments, the seeding |
| `internal/browsercdp/realchrome_test.go` | the two roads, against a real Chrome |
| `internal/cliapp/browser_flags.go` | the headless flag is honoured when it is given |
| `internal/cliapp/browser_flags_test.go` | the policy default, and the flag in both directions |
| `docs/guide/tools.md` | the session policy, the own tab, the visible default |

---

## 7. 硬约束 (hard constraints)

- The clone's browser path is **self-contained**: no node, no npm, no MCP
  server, no process but Chrome itself. The reference's road (the embedded
  `npx -y chrome-devtools-mcp@1.10.1` launcher, the node download into
  `$CHROME_DEVTOOLS_MCP_NODE_DIR`) is **not** adopted.
- The session of a Chrome the clone attaches to is the **user's**: it is
  the user's profile the browser runs. The clone never copies a session
  *out of* a running Chrome, and never writes into the user's profile.
- The seeding **only reads** the user's profile, and only writes into the
  profile the clone owns.
- The reference's four hidden flags keep their names, types, defaults,
  usage strings and hidden state. The clone's flag table is the
  reference's.
- The clone drives a **tab of its own** in a Chrome it attached to, and
  closes it when it stops. It does not navigate the user's tabs.
- A Chrome the clone started stops with the clone
  (`browsercdp.Shutdown()`); a Chrome it attached to is left alone.

## 8. 存疑 (open questions)

- **The reference's own default is unknown.** The reference's flag table
  says `--local_chrome_headless=true`, and its agent prompt says it never
  launches Chrome — so the reference *attaches* by default, on the
  browser the user runs. What the reference does when the user has no
  Chrome running is not recovered: it may start one, or it may fail with
  the "Failed to set up system MCP manager" message. The clone starts a
  visible one.
- **The port behind `chrome://inspect/#remote-debugging`** is 9222 per
  the issue tracker 【二手】, not measured on this machine: the toggle was
  not turned on in the user's Chrome, and the DevToolsActivePort file
  cannot be read here (macOS, §4). The clone therefore keeps **three**
  ways to find the browser (the file, the environment override, the
  configured port) and the port probe is the one that works on macOS
  without Full Disk Access.
- **The seeding's value is unmeasured here.** On this machine the
  profile's contents are unreadable, so the copy never runs; the code is
  exercised by unit tests over a synthetic profile tree. On a machine
  where the profile *is* readable, the copy of a live SQLite database may
  catch the last write halfway — the copy is atomic (write beside, then
  rename), but a torn *source* is not repaired.
- **The twenty-three `browser_*` tools** the reference advertises are
  still unrecovered in their implementation (cycle 311). Whether they are
  themselves MCP-backed is 【推测】 — the MCP names and the native names
  coexist in the binary, and both surfaces are advertised.

## 9. Limitations

- The session policy is **not** the reference's implementation. The
  reference reaches the user's browser through an MCP server and a node
  runtime; the clone reaches it through the DevTools protocol directly.
  The *outcome* is the same, the mechanism is the clone's own.
- The seeding copies three files. A session that lives elsewhere — the
  local storage of a site, a service worker's token, a session cookie
  that is `HttpOnly` **and** in the local storage rather than the cookie
  jar — does not come along.
- `PUT /json/new` is the DevTools HTTP endpoint's new-tab request. A
  browser that refuses it (an older one, or one with the DevTools HTTP
  endpoint turned off) falls back to the tab the browser offers by
  itself — which may be the user's tab.
- The real-Chrome tests need a Chrome on the machine and start processes;
  they are off by default (`PIGASUS_BROWSER_TEST=1`).
- The macOS measurement in §4 was taken with the clone's own Go
  process, which inherits the terminal's permissions. A terminal with
  Full Disk Access reads the profile fine, and the seeding works there.
- The other agent's in-flight work on the same tree
  (`internal/browserbridge/`, the browser agent's second surface) was
  not touched by this cycle; it landed in cycle 344
  (`docs/cycles/cycle-344-browser-bridge/deliverable.md`), and the
  DevTools road stayed as its fallback.

## Files

| | |
|---|---|
| `internal/browsercdp/client.go` | attach first, the clone's own tab, the launch arguments, `CheckBrowser` |
| `internal/browsercdp/config.go` | the policy default: a visible browser |
| `internal/browsercdp/profile.go` | **new** — the session seeding |
| `internal/browsercdp/session_test.go` | **new** — the discovery, the tab, the arguments, the seeding |
| `internal/browsercdp/realchrome_test.go` | the two roads, against a real Chrome |
| `internal/cliapp/browser_flags.go` | the headless flag, honoured when given |
| `internal/cliapp/browser_flags_test.go` | the policy default, and the flag |
| `docs/guide/tools.md` | *The session is the user's* |

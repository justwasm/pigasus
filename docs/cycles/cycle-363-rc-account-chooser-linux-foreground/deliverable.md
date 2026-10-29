# Cycle 363 deliverable: the account chooser link, and the foreground daemon

**Cycle:** `cycle-363-rc-account-chooser-linux-foreground`
**Subject:** two changes to the cloud remote control, both clone-side:

1. every browser link the clone prints now goes through **Google's account
   chooser**, so the browser lands on the account the daemon registered with
   instead of the browser's default one;
2. `remote-control start` **runs the daemon in the foreground** where there
   is no launchd, instead of refusing.

**Evidence:** 【一手】 first-hand — a live Linux container against the real
cloud, and a real browser against the real dashboard · 【反编译】 decompiled
output at confirmed addresses · 【strings】 string-table hits only ·
【推测】 hypothesis

---

## 0. The short answer

| | |
|---|---|
| The link the clone prints | `https://accounts.google.com/AccountChooser?Email=<address>&continue=<deep link>` |
| Why | the cloud's instance rows are **per Google account**; the bare deep link resolves against the browser's *default* account and answers "No instance found" |
| The builder | `internal/rclink` (`DeepLink`, `BrowserURL`, `BrowserURLForAccount`, `ActiveAccount`) |
| The chooser's endpoint | `endpoints` — `account_chooser` / `DefaultAccountChooser`, `Set.AccountChooserURL` |
| The Linux path | `remote-control start` → `startRCForeground` → `runRCForegroundDaemon`, blocking until SIGINT/SIGTERM |
| The platform-independent path | `remote-control run` — the same foreground daemon on every platform (hidden) |
| The bug this cycle found | the printed link named a **different** instance id than the one the tunnel registered |
| Verified | a Linux container registered with the cloud, and the browser's Web UI rendered **that container's** remote view |

---

## 1. The account chooser link

### 1.1 The problem it solves

The cloud's instance registry (`devtools_jetski_boq_api_proto.ApiService/ListInstances`)
keys its rows by the account that registered them, and the dashboard's path
segment `/u/<N>/` selects the account — N is the index of the account in the
browser's chooser. §7.4 of
[`docs/cycles/rc-cloud-remote-view/deliverable.md`](../rc-cloud-remote-view/deliverable.md)
measured it: the same instance id has one row per account, and the bare
`https://antigravity.google.com/r/<id>` resolves against the browser's
*default* account.

The consequence is a link that cannot work: with several accounts signed in,
the bare URL lands on the wrong account and the cloud answers

> No instance found. Try switching to a different account.

The instance *is* registered — the dashboard is looking in the wrong row.

### 1.2 The shape

Google's account chooser asks which account to use and then continues to a
target:

```
https://accounts.google.com/AccountChooser?Email=<address>&continue=<deep link>
```

`Email` names the account; `continue` carries the whole deep link,
percent-encoded. The parameters come out in the order Google's own links
carry them ("Email" first) because `url.Values.Encode` sorts the keys — the
byte order the user asked for falls out of the standard library rather than
out of a hand-rolled string.

【一手】 2026-10-29, live: opening

```
https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2F0698e8f6-2cee-42c0-a454-02c764f6b9b6-v2
```

in a real browser landed on

```
https://antigravity.google.com/u/3/r/0698e8f6-2cee-42c0-a454-02c764f6b9b6-v2?pli=1
```

— the chooser resolved the account and the dashboard's own `/u/3/` segment
carried it. The account index is the browser's, and the chooser is what
computes it.

### 1.3 What is clone-side

The **deep link** is the reference's own: `{companionOrigin}/r/{instanceID}`
【一手】 the reference's `Open https://antigravity.google.com and look for
%q.` and its `Instance name: %s (find it at https://antigravity.google.com)`.

The **chooser around it** is not. The reference never builds a chooser link
— it asks the user to find the instance by name on the dashboard. This is
deliberately a clone-side shape, and it is the one place in this cycle where
the clone deliberately does not match the reference's bytes.

### 1.4 Where the link is printed

| surface | the line |
|---|---|
| `remote-control start` (macOS) | `Open <chooser url> on another device to take over.` + `Instance name: <name>` |
| `remote-control start` (foreground) | `remote-control start: open <chooser url> on another device to take over.` |
| `remote-control serve` | `remote-control serve: open <chooser url> on another device to take over.` |
| `remote-control status` | `Instance name: <name> (find it at <chooser url>)` |
| the TUI's `/remote-control` | the lead line, then `Hostname:`, then the URL — **cut into chunks, one chunk per row** |

The lead prose became "Open on another device to take over:" / "Open on
another device to continue this conversation:" — the id no longer rides in
the sentence.

### 1.4a The TUI's URL is cut into chunks, and every chunk is the link

The first version of this cycle printed the TUI's URL on **one** line, with
the reasoning that "a wrapped copy would no longer paste". That reasoning
was wrong, and the wrongness was visible: the TUI draws its view into a
cell buffer the size of the terminal, so a line wider than the terminal is
**clipped**, not soft wrapped — the tail of a long line simply
disappears. The chooser link is ~164 characters and its tail carries the
instance id, the part that names the daemon. On one line, the tail was not
on screen at all: a link the user cannot see is a link the user cannot
open.

The TUI now shows the URL the way the **add-account screen** shows one
(`internal/loginui`): the URL is cut into chunks, one chunk per row, and
**every chunk carries the complete URL as its OSC 8 hyperlink target**. A
cmd+click (or a tap) on any part of the displayed URL opens the whole
link.

The chunking and the hyperlink frame live in a **new** package,
`internal/urlchunk`, which the add-account screen shares — the two
surfaces show a URL the same way, so they cut it the same way. The
transcript's chunk budget is the terminal width less the five-column
indent and one column of slack.

**Copying the chunks yields the URL.** The transcript's own mouse
selection (`internal/tuiviewport.SelectionText`) treats two rows that
carry the *same* hyperlink target as one logical string: they are joined
without a newline, and the continuation row's indent — the layout's, not
the link's — is dropped. Two rows that link to *different* URLs keep
their newline.

A terminal that does not speak OSC 8 sees the plain text: the URL, one row
at a time, with no escape sequences.

A chooser that names **no** account chooses nothing, so with nothing signed
in the link falls back to the bare deep link. A fallback that cost the user
a click would be worse than no fallback at all.

### 1.5 The chooser is a named endpoint

`AGENTS.md`'s rule — *every Google service URL is a named endpoint* — applies
here too: `endpoints` gained `AccountChooser` (`account_chooser`) /
`DefaultAccountChooser` and `Set.AccountChooserURL(email, target)`. A local
service can stand in for the chooser through
`~/.config/openagy/config.json`.

The builder lives in a **new** package, `internal/rclink`. It imports only
`endpoints` and `internal/accounts` — **not** `internal/persistentrc` — so
`persistentrc` can import it without a cycle, and the callers pass the
instance id in. That is what lets `persistentrc.PlaintextDaemonStatus`
print the link.

---

## 2. The foreground daemon

### 2.1 What was broken

`remote-control start` registers a **launchd** agent on macOS. On Linux the
registration cannot happen at all: `persistentrc.Start` refuses with

```
persistent remote-control daemon: launchd-backed mode requires macOS
(running on linux/amd64); use `wagyu --remote-control` for the per-session variant
```

【一手】 2026-10-29, measured in a `debian:bookworm-slim` container with
the `GOOS=linux` build of `cmd/pigasus`.

### 2.2 What the daemon actually is

The *daemon* is not macOS-only. `remote-control serve` — the foreground
twin — already worked on Linux: it brings up the dispatcher, the
TCP+WebSocket server and the cloud tunnel, and blocks. The **registration**
is the macOS-only part, not the daemon.

So on every other platform `remote-control start` runs the daemon in the
foreground rather than pretending to be a service manager. The path reuses
the existing `serve` handler through the `serveHandler` bridge
(`internal/cli/serve_bridge.go`), so there is one daemon implementation, not
two.

The stderr note says what happened and why:

```
remote-control start: linux/amd64 has no launchd — running the daemon in the foreground, as `remote-control run` does (Ctrl-C to stop)
```

The stdout lines are the daemon's own, and it prints the sentinel
`Remote-control daemon stopped.` when it comes down — the same sentence
`remote-control stop` prints, so a script can share one grep between the
two.

`remote-control stop` keeps the reference's stdout sentinel and adds a
stderr note saying where the daemon actually is: there is no registered
daemon to stop, because the daemon runs in the terminal that started it.

`remote-control status` gains a `Daemon mode: foreground` line on such a
platform.

### 2.3 `remote-control run`: the platform-independent spelling

The fallback above leaves `start` doing two different things on two
different platforms — a launchd agent on macOS, the foreground
everywhere else. The user asked for a command whose behaviour does not
depend on the platform, and `remote-control run` is it:

```
$ pigasus remote-control run
remote-control run: listening on ws://127.0.0.1:8787/rpc (path=/rpc, name=pigasus-7411f32fc696, antigravity_home=/root/.gemini/antigravity-cli)
remote-control run: open https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2F98bc1aa9-eb81-4c35-8c20-fde1e5b64300-v2 on another device to take over.
```

【一手】 2026-10-29, measured in the `debian:bookworm-slim` container
(`linux/amd64`) and on the macOS host — the same output shape on both,
and the printed id matched the registered id on both.

| command | platform | what it does |
|---|---|---|
| `remote-control start` | macOS | registers a launchd agent; the daemon runs in its own process |
| `remote-control start` | elsewhere | runs the daemon in the foreground |
| `remote-control run` | everywhere | runs the daemon in the foreground |
| `remote-control serve` | everywhere | the reference's name for the same thing; the launchd plist runs it |

**One implementation behind the three names.** `serve`, `run` and the
non-darwin `start` all call `runRCForegroundDaemon`
(`internal/cli/subcommand_rc_foreground.go`); the only thing that
differs is the **label** the daemon's progress lines carry, so the user
sees the command they actually typed. The flags are `serve`'s —
`--addr`, `--name`, `--no-login`, `--desktop`.

`--desktop` is the remote-control twin of `pigasus web --desktop`: the
daemon's Mendel read carries the **hub's** identity instead of the CLI's,
so the backend answers the desktop flag set — the voice input, the jetbox
terminal, the aux pane redesign. It travels the same path on all three
foreground surfaces (`ServeArgs.Desktop` → `buildServeStack` →
`desktopMendelUserAgent`), and `TestServeStack_DesktopIdentity` pins the
seam between the flag and the identity.

`run` is **hidden**: the reference's `remote-control --help` lists start,
status and stop only, and that block is the reference's, byte for byte
(the golden file `internal/cli/testdata/help_1.4.6.txt` pins it). `run`
is accepted, not advertised — the same way `serve` is.

The launchd branch stays the platform-dependent one. `run` never
registers an agent, on any platform — the property the command exists
for, and the one `TestRemoteControlRun_IsPlatformIndependent` pins.

### 2.4 The reference's usage block is untouched

`remote-control start --help` still prints the reference's block byte for
byte: `--name` and `--session` only. `--addr`, `--log-file` and `--desktop`
stay accepted-but-not-advertised, exactly as before this cycle.

---

## 3. The bug this cycle found

### 3.1 What happened

The first Linux run printed a link naming one instance id and registered
another:

```
remote-control start: open https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&continue=…%2F7327d0cc-f915-45a4-884a-3b7592f1d1f6-v2 on another device to take over.
[CloudTunnel] registered instance "pigasus-1f1f8239a9bf" (id: 3da6eeed-0f99-4998-91cd-a7617327d4ac-v2)
```

Two ids for one daemon. The printed link would have answered "No instance
found" — for an instance that was, at that moment, online.

### 3.2 The cause

`persistentrc.InstallationUUID()` reads the persisted uuid from
`~/.config/openagy/remote-control-instance-id` and mints one when the file
is absent. The container's configuration directory was mounted **read-only**,
so the write failed silently (`_ = os.WriteFile(...)`) and **every call
minted a fresh uuid**.

Two call sites asked for the id independently: the daemon's
(`persistentrc.InstanceID()`, the one the tunnel registers) and the printer's
(`rclink.BrowserURL(persistentrc.InstanceID(), "")`). Two calls, two uuids.

This is not only a container problem. A read-only home, a `HOME` pointing
into a read-only volume, a config directory the user cannot write — any of
them reproduce it, and the symptom is a link the daemon itself printed that
does not resolve.

### 3.3 The fix

Two changes, both necessary:

- **the uuid is memoised for the life of the process**
  (`persistentrc.InstallationUUID`, a `sync.Once`). Within one process the
  id never changes, so the daemon and the printer cannot disagree — and the
  memoised value is the one that gets persisted when the directory is
  writable.
- **the printed link reuses the id the tunnel registered** rather than
  deriving a second one. `runServeEventLoop` now takes the `instanceID` the
  tunnel client was given.

The second change alone would have been enough for the *daemon*; the first
alone would have been enough for the *process*. Together they close the
class.

### 3.4 The second race: the macOS daemon is a *separate* process

The memoisation fixes the daemon and the printer **when they are the same
process**. On macOS they are not: `remote-control start` writes a LaunchAgent
plist and bootstraps it, and the agent runs `<binary> remote-control serve`
as its own process. That daemon reads the id from the same file — and if the
starter had not written it yet, the two processes would each mint one, and
the link the starter prints would name an id the daemon never registered.

So the id is now resolved **and persisted before the daemon is bootstrapped**
(`persistentrc.EnsureInstanceID`, called by `startOfficialDaemon`). The
persisted value is the **uuid**, not the full id: the file is what
`loadOrMintInstallationUUID` reads back, and it rejects anything that is not
a uuid — an id carrying the "-v2" suffix would be rejected and a fresh uuid
minted on the next start.

`EnsureInstanceID` reports whether the file now holds the id. When it could
not, `remote-control start` says so on stderr:

```
remote-control start: the instance id could not be persisted (<path>); the daemon may register a different one.
```

When the file holds a *different* uuid — it changed under the starter —
the starter **takes it back**: it owns the daemon it is about to launch,
and the daemon reads the file, so the two have to agree.

### 3.5 A bug the new test found

`EnsureInstanceID` was first written to compare the file's contents against
the **full instance id** (`<uuid>-v2`) while writing the id into the file.
Two things were wrong with that: the comparison never matched (the file
holds the uuid, the id carries the suffix), so the function always reported
"not persisted"; and writing the id into the file would have put a
non-uuid into the uuid file — the next start would have rejected it and
minted a fresh one, undoing the persistence the function exists for.

`TestEnsureInstanceIDPersistsTheID` caught both: it asserts the file holds
the uuid, that the uuid has the uuid shape, and that the returned id is the
uuid with the suffix.

### 3.6 The regression tests

- `TestInstallationUUIDIsStableWithinAProcess` makes the configuration
  directory unwritable and asserts the uuid does not change across calls.
- `TestInstallationUUIDIsPersisted` asserts the file is written where the
  package says it is.
- `TestEnsureInstanceIDPersistsTheID` asserts the file holds the uuid and
  the returned id carries the suffix.
- `TestEnsureInstanceIDReportsAnUnwritableDirectory` asserts the failure
  mode is reported rather than swallowed.
- `TestEnsureInstanceIDTakesTheFileBack` asserts the starter's uuid wins
  when the file changes under it.

The memoisation is process-global on purpose — a running daemon must never
see the id change under it — so it is also shared state between tests. The
tests that bind the id to their own temporary configuration directory ask
for the reset explicitly (`resetInstallationUUIDForTest`); the suite passes
under `-shuffle=on`.

---

## 4. The live verification

### 4.1 The Linux container

【一手】 2026-10-29. `GOOS=linux GOARCH=amd64 go build ./cmd/pigasus`, in
`debian:bookworm-slim` with `ca-certificates` installed and the OAuth token
mounted in:

```
remote-control start: linux/amd64 has no launchd — running the daemon in the foreground, as `remote-control run` does (Ctrl-C to stop)
using existing token at /root/.gemini/antigravity-cli/antigravity-oauth-token
remote-control start: listening on ws://127.0.0.1:8787/rpc (path=/rpc, name=pigasus-aa8572b56009, antigravity_home=/root/.gemini/antigravity-cli)
remote-control start: open https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2F614633c6-3552-4d78-9a64-02f45c881c3a-v2 on another device to take over.
[CloudTunnel] connected session sid=I6JNYT5Agw0Jgk6JXHd_Zg …
[CloudTunnel] registered instance "pigasus-aa8572b56009" (id: 614633c6-3552-4d78-9a64-02f45c881c3a-v2)
```

The printed id and the registered id are the same — §3's fix, visible.

### 4.2 The browser

The chooser link was opened in a real browser. It landed on

```
https://antigravity.google.com/u/3/r/614633c6-3552-4d78-9a64-02f45c881c3a-v2?pli=1
```

and the Web UI rendered the Antigravity page. The container's log recorded
the browser's own calls arriving over the tunnel:

```
[CloudTunnel] inbound RPC path=/exa.language_server_pb.LanguageServerService/GetStatus reqID=1 callerContentType="application/json"
[CloudTunnel] inbound RPC path=/exa.language_server_pb.LanguageServerService/GetMendelFlags reqID=5 …
[CloudTunnel] inbound RPC path=/exa.language_server_pb.LanguageServerService/JetboxSubscribeToState reqID=10 …
[CloudTunnel] inbound RPC path=/exa.language_server_pb.LanguageServerService/GetLocalUserInfo reqID=8 …
```

**The device was online, and it was the Linux container that answered.**

### 4.3 The re-run against the final binary

The container was re-run after §3.4–§3.6's changes, to confirm the shipped
code still behaves. Same result, and the Web UI's own device switcher
(bottom-left) named the container's instance:

```
remote-control start: open https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2Fb1d28878-693b-4abb-bf9f-a1b4db6d431a-v2 on another device to take over.
[CloudTunnel] registered instance "pigasus-2808d71eefe1" (id: b1d28878-693b-4abb-bf9f-a1b4db6d431a-v2)
```

The chooser link landed on
`https://antigravity.google.com/u/3/r/b1d28878-693b-4abb-bf9f-a1b4db6d431a-v2?pli=1`,
the page rendered, and the switcher at the bottom-left read
**`pigasus-2808d71eefe1`** with a green dot — the container's own name, the
one its log line registered. The container's log took the browser's calls:

```
[CloudTunnel] inbound RPC path=/exa.language_server_pb.LanguageServerService/GetMendelFlags reqID=25 …
[CloudTunnel] inbound RPC path=/exa.language_server_pb.LanguageServerService/GetRemoteControlInfo reqID=27 …
[CloudTunnel] inbound RPC path=/exa.language_server_pb.LanguageServerService/GetStatus reqID=30 …
```

The printed id and the registered id were the same again.

### 4.4 What the macOS host showed

The same chooser link, opened against the host's own daemon, rendered the
same page. The account index was `/u/3/` in both cases — the chooser
resolved the same account.

---

## 5. Hard constraints / 硬约束

- The chooser link's **parameter order** is `Email` then `continue`, and
  that order is `url.Values.Encode`'s, not a hand-written string. A future
  change to the encoder's sort would change the bytes; the test in
  `internal/rclink` pins the exact string.
- The **deep link** the chooser continues to is the reference's own
  `{companionOrigin}/r/{instanceID}`. The chooser around it is the clone's
  own and is deliberately **not** byte-compatible with the reference.
- The printed link **must** name the id the tunnel registered. The id is
  memoised per process (`persistentrc.InstallationUUID`) and the daemon's
  id is threaded into the printer (`runServeEventLoop`).
- On macOS the starter and the daemon are **separate processes**, so the id
  is persisted *before* the daemon is bootstrapped
  (`persistentrc.EnsureInstanceID`). The persisted file holds the **uuid**,
  never the full id — the reader rejects anything that is not a uuid.
- The chooser falls back to the bare deep link when **no** account is signed
  in. A chooser that names no account chooses nothing.
- The `remote-control start` **usage block is the reference's**, byte for
  byte. The foreground mode changes the *behaviour*, not the advertised
  flags.
- The `remote-control --help` block is the reference's too: it lists
  start, status and stop only. `serve` and `run` are **accepted, not
  advertised**.
- `serve`, `run` and the non-darwin `start` share **one** implementation
  (`runRCForegroundDaemon`). Only the label differs.
- `remote-control stop` keeps the reference's stdout sentinel
  (`Remote-control daemon stopped.`) on every platform; the foreground
  note is on **stderr**.
- `rclink` must **not** import `internal/persistentrc` — the dependency
  direction is what keeps the import graph acyclic. `persistentrc` imports
  `rclink`, not the other way round.
- A URL shown in the TUI is **never left on one line**: the renderer
  *clips* a line that is wider than the terminal, so a long URL loses
  its tail. It is cut into chunks, and every chunk carries the complete
  URL as its OSC 8 target (`internal/urlchunk`, shared with the
  add-account screen).
- The transcript's selection treats two rows with the **same** hyperlink
  target as one logical string: they are joined without a newline, and the
  continuation row's indent is dropped. Two rows with different targets
  keep their newline.

## 6. Open questions / 存疑

- The chooser resolves the account, but the **`/u/<N>/` segment** it lands
  on is the browser's account index, and the browser decides it. If the
  user removes the account from the browser's list, the chooser asks again
  and the index changes. The link stays correct; the *URL* does not.
- The chooser is Google's own and its behaviour is not pinned by the
  reference binary — it is pinned only by the live measurement in §4.2.
  A change on Google's side would change the redirect's shape.
- Whether a chooser link can be **deep-linked into a Web UI already open**
  (rather than a fresh navigation) was not exercised.
- The `Email` parameter is the account's address, not the account's index.
  Whether Google prefers an index parameter was not investigated — the
  address works and the address is what the daemon knows.
- The foreground daemon is **not persistent**: it dies with its terminal
  and does not come back after a crash. A systemd unit would fix that, and
  is a separate cycle.
- The Linux path was verified in a **container**, not on a bare metal
  Linux host. The container had no init system, which is exactly the
  condition the change targets; a systemd host would take the same branch
  (there is no launchd), but that was not measured.

## 7. Limitations

- The browser verification of the Linux container stopped at the Web UI's
  own first-run "Security Notice & Data Use" dialog. The dialog was **not**
  accepted — it is a consent decision on the user's account — but the
  connection was already proven by the inbound RPCs in §4.2 and by the
  rendered page.
- The MacOS host's own daemon (`/tmp/pigasus2 remote-control serve`, pid
  5331) was running throughout, so both the host and the container were
  registered at the same time. The container's link was checked against
  the container's id.
- `marketplace.antigravity.com` does not resolve from this machine — an
  unrelated, pre-existing limitation.
- The clone's Mendel flags remain the **remote-control subset**, not the
  live set (§7.7.3 of the cloud-remote-view cycle).
- The 29 failing `internal/cliapp` colour assertions are **pre-existing**
  and belong to another agent's palette change; they are unchanged by this
  cycle (the same 29 fail on the parent commit).

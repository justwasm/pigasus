# The cloud remote control

> The Antigravity **cloud** can drive a local Antigravity: the browser opens
> `https://antigravity.google.com/u/2/r/<instance-id>`, the cloud routes the
> page to the machine, and the instance answers the CLI's own
> `LanguageServerService` over a tunnel. `pigasus remote-control serve` is
> that instance.
>
> The remote view is **not a page of the cloud's own**: it is an iframe onto
> the CLI's web bundle, and the instance's handshake says which bundle it
> gets. Evidence and the full write-up:
> [`docs/cycles/rc-cloud-remote-view/`](../cycles/rc-cloud-remote-view/deliverable.md).

## What it is

```
pigasus remote-control serve          the instance: tunnel + LanguageServerService
```

| | |
|---|---|
| The tunnel | `internal/rctunnel` — the cloud's WebChannel, the handshake, the request forwarding |
| The dialect | `lsproto`` — binary protobuf ⇄ proto-JSON, over the descriptor closure the frontend bundle embeds |
| The surface | `internal/remotecontrol` — the `LanguageServerService` the web bundle talks to |
| The state | the trajectory (`internal/remotecontrol/ls_trajectory.go`) — the one structure the TUI, `--print` and the web view all render from |
| The store | `~/.gemini/antigravity-cli/conversations/<id>.db` — the reference's own files, appended to, never rewritten |

## The three things that decide whether the page works

**The version string decides the bundle.** The cloud serves
`/web/cli-<version>/` — the CLI's own bundle — only when the instance
advertises the CLI's version. The clone says `cli-<cli.ReferenceVersion>-v2`
— `cli-1.4.8-v2` today (`internal/rctunnel/client.go`,
`DefaultClientVersion`); anything else gets the IDE's bundle, with the IDE's
schemas and the IDE's UI. The reference derives the string from its own
version rather than carrying it as a literal, so the clone does the same.

**The dialect decides whether the frame decodes.** The cloud's v2 mode asks
for `application/proto` and rejects a JSON body. The clone's dispatcher
speaks JSON, so `lsproto`` bridges the two with the descriptor
closure the frontend bundle already carries — the reference's own schema,
read out of the reference's own bytes, never regenerated.

**The caller's own content type picks the dialect, and the default is
JSON.** A caller that names `application/proto` — the app's unary calls do —
gets protobuf; a caller that names `application/json` gets JSON; a caller
that names **nothing** gets Connect's own default, JSON, and so does the
reference. The dashboard's host page is such a caller: it asks for
`GetMendelFlags` and for the status without naming a content type and reads
both answers with `JSON.parse`. The clone used to answer protobuf, so the
parse threw and the page never read the instance's answer at all — the
host page's own fallback ("Failed to read remote-control-webrtc-mesh flag;
defaulting to WebChannel") is what the page did instead.

**The streams decide whether the page spins.** The frontend reconnects the
moment a server stream ends, so the three streams it opens — the state
stream, the summaries stream, the project-updates stream — stay **open**.
A closed stream is a reconnect storm: the sidebar's cold load was 20.7 s
before, 5.29 s after.

## The tunnel's own contract

Three things the clone learned the hard way, all of them from the cloud's
live traffic (`internal/rctunnel/client.go`):

| | |
|---|---|
| **The `close` frame ends the session.** | The cloud sends the string `close` on the backchannel when it is done with the session. It is not an event to read past: the cloud has already disowned the session, and the only way back is a new handshake and a new registration. The clone used to keep reading the dead SID and keep re-opening the poll on it (HTTP 200) — a zombie session. |
| **A 4xx on the forward channel ends the session too.** | The cloud answers `400 Bad Request — "Unknown SID That's all we know."` when the session it is asked to forward into is not the one it holds. The session is gone then: the poll is cancelled, the connection is closed, and the run loop hands the instance a fresh handshake and registration. The reference does the same — its log reads "Connection ended. Retrying immediately..." and then "Starting V2 remote control connection". Kept alive instead, the clone kept posting into a disowned session, every answer was dropped, the cloud cancelled the browser's request ten seconds later ("proxyRequest reqID=13 was canceled by server") and the dashboard's liveness probe never completed — "Instance disconnected" while the instance sat right there, registered and healthy. |
| **The instance id is `<uuid>-v2`.** | The `remote-control-` in the reference's log lines is the tag of its remote-control *session*, not part of the id. `persistentrc.InstanceIDPrefix` is empty; the uuid is persisted in `~/.config/openagy/remote-control-instance-id`. |
| **The registration's answer is the cloud's own chunk.** | It is logged (`registration answer: "8\n[1,1,7]\n"`). A refusal shows up nowhere else, so it is the one line to read when a registration does not take. |

## The Mendel flags answer

`GetMendelFlags` is the one answer the dashboard's host page reads before it
decides anything. Its answer is JSON, and it is read with `JSON.parse`:

```
MHe = (body) => body.webrtcMeshReady === false ? false
    : (body.experimentConfig?.experiments ?? []).some(
        e => e.keyString === "remote-control-webrtc-mesh" && !e.disabled)
```

The clone answers it in the reference's shape (`internal/remotecontrol/mendel_flags.go`):
`{"experimentConfig":{"experiments":[…]}}`, the field order the proto's own
field order gives, and the remote-control experiments with the reference's
own values — `remote-control-version` = `v2`,
`remote-control-proxy-server-url` = `jetski-webchannel.googleapis.com:443`,
`remote-control-silence-timeout-seconds` = `60`,
`remote-control-companion-url` = `https://antigravity.google.com`,
`remote-control-setting-enabled`.

The clone reads the **live** set as well — `internal/auth/mendel.go` calls
CloudCode's `ListExperiments` with the account's own token, caches it for
ten minutes, and appends it after the local six.

Two things differ on purpose:

| | reference | clone |
|---|---|---|
| the experiment set | all 508 of the Mendel service's, live | all 508 of the Mendel service's, live — but read with the **CLI's** identity, so thirty app features come back `disabled` (see below) |
| the mesh | enabled, and `webrtcMeshReady: true` | **disabled**, and `webrtcMeshReady` left out (protojson omits a false boolean) — the clone has no WebRTC mesh: no data channel, no signalling |

The mesh experiment's key is read by the host page, and with the mesh on
the page prefers the mesh's data channel for the app's calls — the clone has
no mesh to prefer, so it reports the experiment as disabled and the page
takes the WebChannel path. The reference, which does implement the mesh,
reports it as enabled.

## The identity decides the flag set

【一手】 2026-10-27, measured: the same token, the same request body, the
same endpoint, **only the `User-Agent` differing** — the backend answers a
*different* Mendel set per product.

| `User-Agent` | `enable-audio-transcription` | `enable-jetbox-terminal-v2` | `enable-aux-pane-redesign` |
|---|---|---|---|
| `antigravity/hub/2.26.0 (aidev_client; …)` | **true** | **true** | **true** |
| `antigravity/cli/1.4.4 (aidev_client; …)` | false | false | false |
| `antigravity/ide/2.26.0 (aidev_client; …)` | false | false | false |

Only the **product token** decides. The version, the build number (`cl=`),
the body's `metadata.updateChannel` and the endpoint do not: ten hub
versions from `0.0.1` to `3.0.0` answer identically, three runs each.

The reference's own `language_server` reads with the **hub** identity
(`antigravity/hub/2.26.0 (aidev_client; os_type=darwin; arch=arm64;
cl=1005016104)`, captured off the wire). The clone's own read sends the
**CLI's** identity (`internal/auth/mendel.go`), so it gets the CLI's set —
and thirty of its flags come back `disabled`, among them the three that
gate the desktop UI.

That is why the clone's page is the CLI's page: the voice input, the
redesigned aux pane and the Terminal tab are all switched off by flags the
backend hands out on the strength of the identity.

Evidence: [`out/web-desktop-mode/`](../../out/web-desktop-mode/) — the UA
matrix, three live answers side by side, the reference's own read off the
wire, and the three pages in the browser.

## The desktop mode

`pigasus web --desktop` asks for the reference's **desktop** UI: the voice
input, the jetbox terminal, the redesigned aux pane.

```bash
pigasus web --desktop
```

The remote-control family asks for the same thing with the same switch:

```bash
pigasus remote-control serve --desktop    # the foreground daemon
pigasus remote-control run --desktop      # the same, under the clone's own name
pigasus remote-control start --desktop    # the persistent daemon
```

| | |
|---|---|
| the switch | `internal/cli/subcommand_web.go` → `internal/rchost/web_reference.go`'s `desktopMendelUserAgent(desktop)` → `remotecontrol.Config.MendelUserAgent` → the identity the `GetMendelFlags` read carries (`internal/auth/mendel.go`'s `MendelHubUserAgent`) |
| the bundle | **unchanged, byte for byte** — the page, the bundle and the RPC surface are the same either way |
| the cache | **per identity** (`internal/remotecontrol/mendel_flags.go`) — the default page and the desktop page read the same backend with different identities, and the backend answers each one its own set |
| reference mode | the switch has **no effect** there: the reference answers `GetMendelFlags` itself, and it is the hub identity already — the reference's own page is the desktop one |

With the flag on, the clone's page carries the same feature surface as the
reference's: the microphone, the permission-preset selector, the sidebar
search, "Customizations", and the overview panel with its Terminal tab.

### The remote-control path

The switch is the same rule — the identity is the only thing that decides —
on two more surfaces:

| | |
|---|---|
| `remote-control serve` | `internal/cli/serve_bridge.go`'s `ServeArgs.Desktop` → `internal/rchost/remote_control_serve.go`'s `buildServeStack` → the same `desktopMendelUserAgent(desktop)` |
| `remote-control run` | the same `ServeArgs.Desktop` — `run` and `serve` share one foreground implementation, so the switch travels the same path |
| `remote-control start` | the **plist** carries it: the macOS daemon's `ProgramArguments` end in `'--desktop'`, and the clone's own plist (`persistentrc.Spec.Desktop`) ends in `--desktop` for the hidden `remote-control-foreground` |
| the foreground twin | `internal/rchost/remote_control_foreground.go`'s `parseForegroundFlags` reads `--desktop` → `buildForegroundDispatcher` → the same `Config.MendelUserAgent` |

The usage blocks differ on purpose. `remote-control serve --help` **lists**
the switch — that block is the clone's own. `remote-control run --help`
lists it too (the clone's own block). `remote-control start --help`
does **not** — that block is the reference's, byte for byte, and the clone
follows the same rule it follows for `--addr` and `--log-file`: accepted,
not advertised.

Evidence: [`out/web-desktop-mode-remote-control/`](../../out/web-desktop-mode-remote-control/) —
four live daemons' `GetMendelFlags` answers side by side, the two pages in
the browser, and the two plists.


## The terminal and the voice

The desktop UI's **Terminal** tab and its **microphone** are two more
LanguageServerService surfaces. They are **not** the dashboard's
`RemoteAgentTerminal*` contract — that one stays exactly as it was; these
are separate methods with their own handlers and their own registries.

### The terminal

| | |
|---|---|
| the methods | `CreateTerminal`, `ListTerminals`, `SendTerminalInput`, `StreamTerminalOutput`, `CloseTerminal` |
| the handlers | `internal/remotecontrol/ls_terminals.go`, `ls_terminals_stream.go` |
| the registry | `internal/rcterminal` — a live shell per terminal, on a pty (`github.com/creack/pty`) |
| the routing | the **LS table is consulted before the alias table** (`dispatch.go`), so `CreateTerminal` reaches the Web UI's handler and not the dashboard's `CreateRemoteAgentTerminal` |
| the wire | canonical proto-JSON (the member name at the top level), not protobuf-es's `{case,value}` envelope — the bundle's protobuf-es reads the flattened form |

The clone spawns `$SHELL -l` in the terminal's directory, and the create
answer carries the shell's name as the title (`"bash"`). The output travels
base64-encoded in `{"output":…}` frames; the input is acknowledged with
`{"ackedOffset":"…"}` — **a string**, and **omitted when it is zero**, so a
pure resize answers `{}`. A chunk whose offset does not line up with the
stream's is dropped, the way the reference drops it.

**The terminal's directory** is the request's workspace URI — and, when the
request names none, the **user's home directory**. It is *never* the
server's working directory.

【一手】 2026-10-27, the reference's own process table: the Antigravity IDE's
`language_server` runs with the working directory `/`, while the shell it
spawns for a terminal runs in `/Users/gear` — the home. The clone follows:
the reference's `CreateTerminal` request carries only a `conversationId`
(no workspace), and the IDE's language server runs with the working
directory `/` as well, so the clone's terminal used to start in `/`.

Two rules the reference has that are easy to lose:

- the **workspace URI is canonicalised** (`file:///tmp/web-ws` comes back
  as `file:///private/tmp/web-ws`);
- a **split** joins its source's group, and the group is named after the
  terminal that heads it (`split-<root id>`) — and the head terminal only
  learns its own group once a split is created *from* it.

A terminal that is gone answers `404 {"code":"not_found","message":"not_found: terminal <id> not found"}`.

### The voice

| | |
|---|---|
| the methods | `StreamAudioTranscription` (a stream), `SendAudioChunk`, `EndAudioSession` |
| the handlers | `internal/remotecontrol/ls_audio.go` |
| the registry | `internal/rcaudio` — the session, the recorded bytes, the frames |
| the frames | `{"ready":{"sessionId":…}}` → `{"transcription":{"text":…}}` → `{"complete":{}}` |

The reference transcribes the recording with a **local** engine: its
`language_server` answers the transcript without a cloud call. The clone has
no local engine, so the recording rides the Gemini transport the clone
already speaks — the PCM goes up as an `inlineData` part
(`audio/pcm;rate=16000`), the model answers with the words. The model is
`gemini-3.8-flash-high` unless `$OPENAGY_AUDIO_MODEL` says otherwise, and
the token comes from the macOS keychain like every other CloudCode call.

The difference is **when** the text arrives: the reference streams a
partial hypothesis while the user is still speaking, the clone transcribes
once when the session ends.

### The errors

A failure that names its code answers the way the reference answers it: the
HTTP status the code maps to, and `{"code":…,"message":…}` as the body
(`remotetransport/connect_errors.go`). `-32018` / `-32019` are
`not_found` (404), `-32602` is `invalid_argument` (400). A failure that names
**no** code keeps the catch-all — HTTP 200 with an empty body — because the
page calls methods the clone does not implement and those must not take the
page down.


## The cloud's entry point is the account-scoped registry row

The dashboard opens the app **only** for an instance the cloud believes is
connected, and that belief lives in the cloud's instance registry
(`devtools_jetski_boq_api_proto.ApiService/ListInstances`): a row's `status`
and `statusLastUpdatedAt`. `status = 2` renders the "Instance disconnected"
card, and a row that does not exist renders "Instance unavailable — No
instance found." — the app iframe is created only for a row that is neither.

**The rows are per Google account.** The request carries `X-Goog-AuthUser`,
and the same instance id has one row per account — its own name, its own
status, its own timestamp. The dashboard's path segment `/u/<N>/` selects
the account: N is the index of the account in the browser's account
chooser.

| idx | account | a live row |
|---|---|---|
| 0 | navigaid@gmail.com | — (the browser's default — the bare `/r/<id>` URL lands here) |
| 1 | btwiuse@gmail.com | — |
| 2 | libredot@gmail.com | the reference (`agy remote-control serve`) |
| 3 | naivgaid@gmail.com | the clone (`pigasus remote-control serve`) |
| 4 | stakingminer@gmail.com | — |
| 5 | justwasm@gmail.com | — |

An instance's row lives under **the account its daemon authenticates as**.
The clone registers with its active account (`naivgaid@gmail.com`), the
reference with the account in its own token (`libredot@gmail.com` — its log:
`Headless auth: authenticated as "libredot@gmail.com"`).

Both binaries read the **same** token file,
`~/.gemini/antigravity-cli/antigravity-oauth-token`, so the account a daemon
registers under is whatever that file held when it started. On 2026-10-26
17:43 it held `naivgaid@gmail.com`, and **both** instances sat under index 3
— the reference's own log reads `Headless auth: authenticated as
"naivgaid@gmail.com"`. The table above is the state at the time of the
measurement, not a fixed mapping.

**The tunnel's liveness writes that row** — the clone's
`statusLastUpdatedAt` advances every ~3 s with no mesh session anywhere.
The reference additionally runs a **WebRTC mesh**: a second transport whose
signaling lives on the same boq service the dashboard uses
(`InitiateMeshSession`, `PollSignalingMessages`, `SendSignalingMessage`),
and whose instance side works with the CLI's own OAuth bearer token (no API
key; the browser's API key refuses it with "The API Key and the
authentication credential are from different projects"). The mesh is the
**browser↔instance direct data channel** — the reference's own remote view
sat at "Connecting to instance…" because that attachment never completed.
The clone's does not need it: the tunnel carries the app's RPCs.

So the cloud remote view is open to the clone:

```
https://antigravity.google.com/u/3/r/<instance-id>
```

— with the account the daemon registered with. The bare URL the CLI prints
(`https://antigravity.google.com/r/<id>`) resolves against the browser's
*default* account and answers "No instance found. Try switching to a
different account."; the reference's bare URL fails the same way.

## The printed link goes through Google's account chooser

The account-index problem above is the browser's, not the daemon's: the
instance knows **which account** it registered under, but not **which
index** that account has in the browser's chooser. The bare deep link
(`/r/<id>`) therefore resolves against the browser's default account, and
with several accounts signed in the dashboard answers "No instance found.
Try switching to a different account." — the row exists, the dashboard
looks in the wrong account's.

Google's account chooser resolves it. It asks which account to use, names
the one the daemon registered with, and continues to the target:

```
https://accounts.google.com/AccountChooser?Email=<address>&continue=<deep link>
```

Every browser link the clone prints goes through it. `Email` is the
signed-in account's address; `continue` carries the whole deep link,
percent-encoded. The parameters come out in the order Google's own links
carry them ("Email" first) because `url.Values.Encode` sorts the keys.

| where | the line |
|---|---|
| `remote-control start` | `Open <chooser url> on another device to take over.` + `Instance name: <name>` |
| `remote-control serve` | `remote-control serve: open <chooser url> on another device to take over.` |
| `remote-control status` | `Instance name: <name> (find it at <chooser url>)` |
| the TUI's `/remote-control` | the lead line, then the hostname, then the URL — **cut into chunks, one chunk per row** |

### The TUI's URL is cut into chunks, and every chunk is the link

The TUI draws its view into a cell buffer the size of the terminal, so a
line wider than the terminal is **clipped**, not soft wrapped: the tail of
a long line simply disappears. The chooser URL is ~164 characters and its
tail carries the instance id — the part that names the daemon — so a URL
left on one line was a URL whose tail the user could not see, could not
select and could not click.

So the TUI shows it the way the **add-account screen** shows a URL
(`internal/loginui`): the URL is cut into chunks, one chunk per row, and
**every chunk carries the complete URL as its OSC 8 hyperlink target**. A
cmd+click (or a tap) on any part of the displayed URL opens the whole
link.

```
> /remote-control
  ⎿  Remote control on for this session.
     Open on another device to take over:
     Hostname: laptop
     https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&cont
     inue=https%3A%2F%2Fantigravity.google.com%2Fr%2F98bc1aa9-eb81-4c35-8c20-fd
     e1e5b64300-v2
```

The chunking and the hyperlink frame live in **`internal/urlchunk`**,
which the add-account screen shares — the two surfaces show a URL the
same way, so they cut it the same way. The transcript's chunk budget is
the terminal width less the five-column indent and one column of slack.

**Copying the chunks yields the URL.** The transcript's own mouse
selection (`internal/tuiviewport.SelectionText`) treats two rows that
carry the *same* hyperlink target as one logical string: they are joined
without a newline, and the continuation row's indent — the layout's, not
the link's — is dropped. Two rows that link to *different* URLs keep
their newline.

A terminal that does not speak OSC 8 sees the plain text: the URL, one
row at a time, with no escape sequences.

**This shape is the clone's own.** The reference prints the bare companion
origin and asks the user to look for the instance by name ("Open
https://antigravity.google.com and look for %q.") — it never builds a
chooser link. The *deep link* it points at is the reference's own
(`{companionOrigin}/r/{instanceID}`); the chooser around it is not.

The chooser is a **named endpoint** (`account_chooser` in
`~/.config/openagy/config.json`), so a local service can stand in for it.
The builder lives in `internal/rclink`; the resolver in
`endpoints.Set.AccountChooserURL`.

A chooser that names **no** account chooses nothing, so with nothing
signed in the link falls back to the bare deep link — a fallback that
costs the user a click would be worse than none.

## The daemon runs in the foreground where there is no launchd

`remote-control start` registers a **launchd** agent on macOS. There is no
launchd on Linux, and the clone ships no systemd unit, so the *persistent*
daemon cannot be registered there — `persistentrc.Start` refuses with
"launchd-backed mode requires macOS".

The daemon itself is not macOS-only. The foreground path brings up the
very same stack — the dispatcher, the TCP+WebSocket server, the cloud
tunnel — and blocks until the user interrupts it. So on every other
platform `remote-control start` **runs the daemon in the foreground**
instead of pretending to be a service manager:

```
$ pigasus remote-control start
remote-control start: linux/amd64 has no launchd — running the daemon in the foreground, as `remote-control run` does (Ctrl-C to stop)
remote-control start: listening on ws://127.0.0.1:8787/rpc (path=/rpc, name=pigasus-1f1f8239a9bf, antigravity_home=/root/.gemini/antigravity-cli)
remote-control start: open https://accounts.google.com/AccountChooser?Email=…&continue=… on another device to take over.
```

The note is on **stderr**; the stdout lines are the daemon's own. The
daemon prints the sentinel `Remote-control daemon stopped.` when it comes
down — the same sentence `remote-control stop` prints, so a script can
share one grep between the two.

`remote-control stop` on such a platform keeps the reference's stdout
sentinel and adds a stderr note saying where the daemon actually is (there
is no registered daemon to stop — the daemon runs in the terminal that
started it).

`remote-control status` gains a `Daemon mode: foreground` line there.

The reference's `start` usage block is unchanged: it lists `--name` and
`--session` only, and `--addr` / `--log-file` / `--desktop` stay
accepted-but-not-advertised.

### `remote-control run` is the platform-independent spelling

`start` is the platform-dependent command: a launchd agent on macOS, the
foreground everywhere else. `remote-control run` is the foreground one —
**the same on every platform**, macOS included:

```
$ pigasus remote-control run
remote-control run: listening on ws://127.0.0.1:8787/rpc (path=/rpc, name=pigasus-7411f32fc696, antigravity_home=/root/.gemini/antigravity-cli)
remote-control run: open https://accounts.google.com/AccountChooser?Email=…&continue=… on another device to take over.
```

It takes the same flags as `serve` — `--addr`, `--name`, `--no-login`,
`--desktop` — and it is the same implementation: `serve`, `run` and the
non-darwin `start` all go through one `runRCForegroundDaemon`. The only
difference between them is the **label** the daemon's progress lines
carry, so the user sees the command they actually typed.

| command | platform | what it does |
|---|---|---|
| `remote-control start` | macOS | registers a launchd agent; the daemon runs in its own process |
| `remote-control start` | elsewhere | runs the daemon in the foreground |
| `remote-control run` | everywhere | runs the daemon in the foreground |
| `remote-control serve` | everywhere | the reference's name for the same thing; the launchd plist runs it |

`run` is **hidden**: the reference's `remote-control --help` lists start,
status and stop only, and that block is the reference's, byte for byte.
`run` is accepted, not advertised — the same way `serve` is.

### The instance id the link names is the id the daemon registered

The link has to name the id the cloud's registry knows the instance by —
the id the tunnel **registered**. So the printed link reuses the id the
daemon resolved, rather than deriving a second one.

Deriving it twice is not safe: the installation's uuid is persisted in
`~/.config/openagy/remote-control-instance-id`, and a configuration
directory that cannot be written (a read-only home, a container's
read-only mount) would mint a **fresh** uuid on every call. The daemon
would then register one id and print a link naming another — and the
dashboard would answer "No instance found" for a link the daemon itself
printed. The uuid is therefore memoised for the life of the process
(`persistentrc.InstallationUUID`).

On macOS the memoisation is not enough on its own: `remote-control start`
bootstraps a LaunchAgent that runs `<binary> remote-control serve` as its
**own process**, and that process reads the id from the same file. The id
is therefore resolved *and persisted* before the daemon is bootstrapped
(`persistentrc.EnsureInstanceID`), so the starter and the daemon cannot
each mint one. The file holds the **uuid**, not the full id — the reader
rejects anything that is not a uuid, and the id adds the `-v2` suffix.

If the id cannot be persisted, `remote-control start` says so on stderr
rather than printing a link that may not resolve.

## The trajectory is the shared state

A conversation is a list of steps, and the Web UI renders that list. The
clone keeps one trajectory per conversation, keyed by the cascade id, and
the same steps are what the TUI and `--print` show.

| | |
|---|---|
| `Append` / `Update` | a step lands, or grows in place — the answer's text is written into the same slot the first word landed in |
| `Seed` | the store's steps fill a trajectory the daemon has not run in this process |
| `Restart` | a continued conversation starts a new trajectory id — the Web UI resets its slice when the id changes, so the view shows the new turn and the file keeps every turn |
| `Snapshot` / `Wait` | the state stream's view of it |

**Every step carries `metadata.sourceTrajectoryStepInfo`** with
`cascadeId`, `trajectoryId` and `stepIndex`. The frontend groups a
conversation's steps into turns by that index and keys each turn by
`<trajectoryId>-<index>`; two steps without an index in one conversation
collapse into one turn, and one of them disappears.

## The chat is one conversation

The Web UI's composer sends **one message at a time**. The message before it
is not in the prompt the server receives — so a turn engine that starts from a
blank transcript answers every message as if it were the first one, and the
chat has no memory of itself.

The clone replays the conversation's earlier turns into the model's context
before the new prompt:

| | |
|---|---|
| **Where the history comes from** | the conversation store — `conversations/<id>.db`, the same file the sidebar reads and the same file the turn is appended to when it ends |
| **Which turns** | the user and assistant prose (`Conversation.ChatTurns`); the tool calls are not replayed |
| **How much** | `convstore.DefaultHistoryTurns` (40) and `convstore.DefaultHistoryChars` (100 000) — the caps the `--continue` path uses |
| **Where it is replayed** | `pigrt.Runtime.SeedHistory` — the same seam `--print -c` uses, so the wire is identical to the CLI's resumed turn |

The read happens **before** the turn runs, and the turn's own steps are
appended to the store **after** it ends — so the message being sent is never
in its own history.

【strings】 the reference's own `exa.cortex_pb.CascadeConfig` carries the field
`use_db_trajectory` — "load the trajectory from the db" — beside
`conversation_history_config`
(`out/local-macos/protos/cortex_1.4.6.txt`), so the reference resolves the
context from the store the same way. 【一手】 the reference's own language
server answers a message sent into a conversation it did not start, and that
conversation's file grows by two steps — the message and the answer — with
the earlier steps still in place.

On this path the **conversation id is the cascade id**: the frontend mints the
uuid and sends it as the cascade id, and the clone stores the turn under it
(`lsStoreTurn` → `conversations/<id>.db`). A conversation the daemon has no
cascade for — one opened from the sidebar — is resumed from the same file
(`lsResumeCascade`), so it carries its history too.

The replay is **best-effort**: a store that cannot be read leaves the session
as it was and the turn runs, the way the print path's implicit trajectory is.

## The conversation's name

The Web UI's header shows the title the **instance** serves in its summary
message, and falls back to the string *"Untitled Conversation"* when there is
none. The app never asks for a title — its own summary write carries none —
so the instance has to name the conversation when it creates it.

| | |
|---|---|
| **Where the name is written** | `internal/convstore` — the summaries row's `title` and `preview` columns, and the metadata cache's `Title`; the same two writes the TUI path makes |
| **When** | when the clone creates the conversation (`convstore.AppendConversationSteps`, the remote path's only entry into the store) — a conversation the reference created keeps the reference's own name |
| **How** | `convstore.GenerateConversationTitle` — a heuristic over the first user message; the reference asks the model instead (`GenerateConversationTitle` is in the reference's RPC list) |
| **What protects it** | an empty title never erases a stored one: the app's title-less summary write leaves the row's title alone |

**Two binaries, one store.** The reference's own daemon reconciles the
summaries table when it starts (`reconcile_summaries.go`) and rewrites the
rows it can read in the reference's shape — with an *empty title* when it
cannot generate one, because the clone's steps are not the reference's
protobuf. It leaves the metadata cache alone, so the clone reads the name
back from there: a row without a title takes the cache's title, and the
summary message takes the row's title when the message itself carries
none. Without that, the Web UI's header falls back to "Untitled
Conversation" every time the reference restarts.

The summaries table is the **reference's** table — 21 columns, with
`last_modified_time` and `last_user_input_time` `NOT NULL` and no default, so
every write names them. The clone creates the same table
(`internal/convstore/summary_schema.go`) and migrates an older clone's
six-column table in place.

## The steps are the reference's own blobs

The store keeps every step as a binary protobuf — a `gemini_coder.Step` —
and the clone reads them with the same schema the frontend uses, so the
model's reasoning, the usage metadata and the user's config all survive.
The clone **appends** to a conversation it did not start.

A model response's blob carries three things the Web UI reads:

| field | what |
|---|---|
| `plannerResponse.response` | the answer |
| `plannerResponse.thinking` | the reasoning — the collapsible panel's body |
| `plannerResponse.thinkingDuration` | a proto-JSON **string** ("1.500s") — the panel's label |

The duration is a string because `google.protobuf.Duration` is one in
proto-JSON; the frontend's protobuf-es decoder rejects the object form, and
a rejected frame kills the whole trajectory stream, not just the field.

## The tools that need the user

Three of the advertised tools cannot answer themselves: they need the user.
`ask_question` asks, `ask_permission` and `ask_custom_permission` ask for a
verdict. The reference **blocks the tool call** until the user replies, and the
reply becomes the tool's result — the model reads the answer, not a timeout.

The clone does the same, over one seam:

| | |
|---|---|
| **`internal/interaction`** | the seam: a tool asks through the `Host` its context carries, and the transport that put the `Host` there answers. `internal/tools` and `internal/remotecontrol` both import it — they cannot import each other |
| **the host's home** | `internal/remotecontrol` — it installs the host on the turn's context (`(*CascadeRegistry).SendStream`), so the tool that asks is the one running in that turn |
| **the waiting step** | the question becomes a trajectory step of type `CORTEX_STEP_TYPE_ASK_QUESTION` (138) in status `CORTEX_STEP_STATUS_WAITING` (9), carrying the questions twice: as the step's own `askQuestion`, and as `requestedInteraction.askQuestion` |
| **the answer** | `HandleCascadeUserInteraction`, in the **IDE dialect** — the cloud dialect's alias is routed after it, so the IDE's method name wins |
| **the answer's shape** | `{cascadeId, interaction:{trajectoryId, stepIndex, askQuestion:{responses:[…]}}}` — the oneof member at the top level, as proto-JSON spells it |

The frontend's card reads the waiting step, and the answer names the step by
the trajectory id and the step index the step's own metadata carries
(`metadata.sourceTrajectoryStepInfo`).

**Only `ask_question` is model-reachable**, and that matches the reference: the
twelve declarations the model receives
(`internal/tools/model_tools_1.4.6.json`) carry `ask_question` and neither of
the two permission tools. The other two are in the sixty-name catalogue — the
surface the headless client is told about — so they are reachable, but not by
the model.

The headless paths are unchanged: with no host on the context, the tools still
say that the user cannot be reached.

The reference's own bytes for this round trip — its waiting step, its answer
payload, and the clone's — are in
[`out/web-ui-interaction/`](../../out/web-ui-interaction/); the write-up is
[`docs/cycles/cycle-327-web-ui-interaction-round-trip/`](../cycles/cycle-327-web-ui-interaction-round-trip/deliverable.md).

## The account switcher rides along

The web UI the remote view embeds carries a top-right account switcher: the
same list / switch / add / remove the TUI's `/accounts` panel offers, over
`/__agy/api/accounts*` (`internal/rcfrontend/accountsapi.go`). It is not a
bundle patch — it attaches itself to the DOM on the titlebar's stable test id
— and it is served by the same `internal/rcfrontend` handler in both the
standalone and the remote case, because the cloud's iframe reads the page the
instance serves.

Whether the cloud relays `/__agy/api/accounts` to the instance is not
measured; the local server routes every non-RPC path to the web UI, but the
cloud's routing table decides what it forwards. See
[`docs/guide/accounts.md`](accounts.md#the-web-uis-account-switcher).

The app's own quota panel is answered by the **dispatcher**, not by the web
UI: the model picker's "View Usage" asks the language server for the quota
over the WebSocket
(`/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary`),
and the route is `internal/remotecontrol/dispatch_quota.go`. It answers the
active account's quota in proto-JSON, so the app renders its own rows and its
own ring gauges — and the numbers follow a switch. The same route serves the
standalone `pigasus web` and the remote view, because both speak the same
WebSocket protocol.

## The picker lists a tiered model once per tier

The Web UI's model picker reads one list — the `clientModelConfigs` of
`GetUserStatus` — and it maps every entry through one function. That
function reads the entry's **label** and sends the entry's **number** back
(`StartCascade`'s `requestedModel`); it reads **no** tier field, so a
reasoning tier cannot travel as a field.

It travels as a **row**, the way the reference's own catalogue does it: a
model that declares tiers (`reasoning_efforts` in
`~/.config/openagy/providers.json`) is listed once per tier — *MiniMax M3.1
Flash (High)*, *… (Xhigh)* — and each row gets its own number, so the
selection comes back as that row's id (`minimax-m3.1-flash-xhigh`). The
registry answers that id with the base model **at that tier**, and the tier
rides the request as the upstream's `reasoning_effort` hint.

Nothing in `internal/rcfrontend/assets/main.js` changed.

Evidence: [`docs/cycles/cycle-335-model-reasoning-tiers/`](../cycles/cycle-335-model-reasoning-tiers/deliverable.md).

## The credits panel and the queued card

Two more of the Web UI's own calls are the dispatcher's job, and both were
missing or misrouted once — the operator's log carried both errors
([`docs/cycles/cycle-334-web-ui-log-fixes/`](../cycles/cycle-334-web-ui-log-fixes/deliverable.md)).

**The credits panel asks for the account's code-assist state.**
`GetLoadCodeAssist` has no route in the IDE's own tables, so the route sits
next to the quota one (`internal/remotecontrol/dispatch_codeassist.go`). Its
answer's only member is the **cloud's own** `v1internal:loadCodeAssist`
answer, so the clone reads that endpoint and passes the body through
(`internal/auth/code_assist_state.go`) instead of inventing a tier. The app
throws on a missing `response` member — *"loadCodeAssist returned no
response"* — so the answer always carries one: the cloud's state when the
read succeeds, the account's cached tier when it does not. The state is
cached for two minutes — keyed by the active account, so a switch is
answered with a fresh read; the Web UI's own cache is five seconds.

**The queued card's delete is the IDE's `DeleteAgentMessage`, not the
dashboard's.** The card hands the **id** of the pending message and the
conversation it waits in (`{messageId, recipient}`); the clone's own
`DeleteRemoteAgentMessage` names the conversation and a **stored** message
(`{conversation_id, message_id}`) instead. The alias table
(`internal/remotecontrol/reference_surface.go`) maps the reference's gRPC
name onto the dashboard's one, and it is consulted *after* the IDE's own
tables — so the card's shape used to arrive at the dashboard's handler and
be refused with *"conversation_id is required"*, and the message the user
threw away stayed in the queue. The IDE's shape is now routed to the queue
(`internal/remotecontrol/ls_queue_rpc.go`), which drops the message **by id**:
the index moves when the queue grows or shrinks, the id does not. The
reference CLI's own dialect still reaches the dashboard's handler.

## The file answers speak proto-JSON

Four methods of `exa.language_server_pb.LanguageServerService` carry the
Web UI's file surface: `StatUri`, `ReadDir`, `ReadFile` and `WriteFile`.
Their answers are proto-JSON, and three of proto-JSON's rules decide
whether the Web UI can read a file at all:

| rule | an answer looks like |
|---|---|
| a `bytes` field is base64 | `{"content":"Ly8gUGFja2FnZSByY3Rva2Vu…","language":"LANGUAGE_GO"}` |
| an enum is its **name** | `{"fileType":"FILE_TYPE_FILE", …}` |
| an `int64` is a **string** | `{"size":"9293"}` |

The first one is not a nicety: the app's own protobuf-es decoder
refuses a raw text in a `bytes` field with *"Cannot decode field
exa.language_server_pb.ReadFileResponse.content from JSON: invalid
base64 string"*, and the file never opens. The same rule holds on the
request side — the Web UI sends `WriteFileRequest.content` base64-encoded,
and the reference refuses anything else with *"invalid value for bytes
field content"*.

`ReadFileResponse.language` is `exa.codeium_common_pb.Language`, and
which name the reference picks for a given file is **its** table, so the
clone's table is measured rather than guessed: 67 names, probed against
the reference's own language server, zero differences (the quirks
included — the reference answers `LANGUAGE_XML` for a `.rs` file). The
measurement and the two captures are in
[`out/ls-file-content/`](../../out/ls-file-content/); the write-up is
[`docs/cycles/cycle-329-ls-file-content-bytes/`](../cycles/cycle-329-ls-file-content-bytes/deliverable.md).

The answers' **field order** is the descriptor's declaration order, the
same rule the Mendel flags answer follows
(`lsproto`/order.go`).

## The reference mode serves the reference's own page

`pigasus web --reference-backend 127.0.0.1:8802` makes the clone a **front**
for a reference `language_server` that is already running: the page, the
bundle and the CSS are the **reference's own bytes**, and the clone proxies
the reference's RPCs to it.

| | |
|---|---|
| The assets | `internal/refbackend/assets.go` — GET/HEAD only, the reference's own headers, `If-None-Match` → 304, and the reference's SPA fallback (an unknown path answers the reference's index document) |
| The token | `internal/refbackend/csrf.go` — the CSRF token is read out of the reference's own index document; a 401 re-learns it and retries once |
| The seam | `internal/rchost/web_reference_assets.go` — `/__agy/*` is the clone's own, everything else is the reference's |

The reference's RPCs need that token, and the reference's own page carries
it in `window.__APP_CONFIG__`. So in reference mode the clone **adopts the
reference's token as its own transport secret**: the byte-identical page and
the clone's transport have to agree about the secret, and the reference's own
token is the only one both of them know.

The reference's page speaks **gRPC-Web**, not Connect — so the clone's
transport grew a second dialect (`remotetransport/grpcweb_codec.go`):

| | Connect (the clone's own page) | gRPC-Web (the reference's own page) |
|---|---|---|
| the unary request | plain JSON | **always** enveloped |
| the unary answer | plain JSON | a message envelope **and** a trailers envelope (`0x80`, the body is the header block `grpc-status: 0\r\n`) |
| the failure | an error envelope | **no body** — the status is in the HTTP headers (`Grpc-Status`, `Grpc-Message`) |
| the end of a stream | the `0x02` end-of-stream envelope | the same trailers envelope |

**Whether a call streams is decided by the method descriptor**
(`lsproto.Codec.IsServerStreaming`), not by the content type: gRPC-Web's
content type does not mark a stream, and the reference's own page calls a
server-streaming method with the unary content type.

Evidence: [`out/web-reference-mode/`](../../out/web-reference-mode/) — the
reference's index document, its assets' headers and hashes, the gRPC-Web
framing side by side, and the cancel round trip.

### The page runs on the WebSocket transport

The reference's bundle picks its RPC transport **out of the page's query
string** — and its default is the fetch transport:

```js
function AHc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");
return b!==null?b==="true"?"explicit":"off":a.get("wsTransport")==="3"?"hub":"off"}
```

| query | transport |
|---|---|
| `useWebSocket=true` | `explicit` — WebSocket |
| `useWebSocket=false` (or any other value) | `off` — fetch |
| `wsTransport=3` | `hub` — WebSocket, with a fetch fallback after a 4 s dial timeout |
| nothing | `off` — fetch |

On the fetch transport the app opens **six** long-lived server streams
(`JetboxSubscribeToState`, `JetboxSubscribeToSummaries`,
`ProjectUpdatesStream`, `SubscribeToMcpServerStates`, `SubscribeToPlugins`,
`StreamAgentStateUpdates`), each one a response that never ends and each
one a connection. Chrome keeps **six** connections per host: the pool is
exactly full, so the turn's own calls queue behind the streams and the page
sits on **Working** with no output — while the model itself has already
answered in four seconds.

The clone's own page never had the problem: `internal/rcfrontend` patches
the bundle (`websocket-transport-default`, applied in `serveMainJS`) and
defaults to the WebSocket transport. Reference mode serves the reference's
**own bytes**, so the URL is the only lever: the asset proxy answers the
document with a **302** to the same path and query plus
`useWebSocket=true` (`internal/refbackend/transport.go`). The bytes stay
the reference's — same md5, same ETag, same SPA fallback — and
`?useWebSocket=false` is the operator's way back to the fetch transport.

`useWebSocket` is the **reference's own** parameter — the clone neither
invents nor patches it; the clone's own page has used it all along (the
`websocket-transport-default` patch only changes the *default*). In
reference mode the parameter was reachable before this cycle too: the asset
proxy passes the query through, so `?useWebSocket=true` typed by hand ran
the page on the WebSocket and the turn worked (measured on the binary from
before the cycle: 4 static resources, 0 fetch/XHR, the answer in seconds).
The 302 changes the **default**, not the capability.

| | fetch transport | WebSocket transport |
|---|---|---|
| the page's HTTP requests | 628, a large share of them `(pending)` | **0** — every RPC over one WebSocket |
| the answer | about two minutes after the model finished | in the same second |
| the console | 0 errors, `Cold load completed: 25154ms` | 0 errors, 0 warnings |

Evidence: [`out/web-ui-working-stuck/`](../../out/web-ui-working-stuck/) and
[`docs/cycles/cycle-335-web-ui-working-stuck/`](../cycles/cycle-335-web-ui-working-stuck/deliverable.md).

### And when the clone starts the reference itself

`--reference-backend=<path to language_server>` makes the clone **start** the
reference (`internal/refbackend/spawn.go`). Three of the arguments decide
whether its web app opens at all, and three more decide whether it opens on
real data — measured on the reference binary, one flag at a time, in
[`out/refbackend-spawn/`](../../out/refbackend-spawn/):

| argument | without it |
|---|---|
| `-subclient_type=hub` | the reference serves no page: `GET /` → **404** |
| `-csrf_token=<minted>` | the page loads, but the bundle rejects itself — the screen stays empty (`Uncaught (in promise)`) |
| `-app_data_dir=antigravity-cli` | the page's product is `antigravity-ide` and the app opens its **sign-in** page, not a product's UI |
| `-use_mocked_data` *(the clone does **not** pass it)* | the reference answers with *Mock User*, the mock model `gemini-3.1-pro` and **0 conversations** |
| `-api_server_url=…` `-cloud_code_endpoint=…` | the reference talks to its default API host (`http://0.0.0.0:50001`), has no account, and the app opens the sign-in page |
| `-gemini_dir=…` *(the clone does **not** pass it)* | the reference reads an empty throwaway directory: no account, no projects, no history |

The secret is minted for the run (24 random bytes, base64url) and the
reference then *demands* it on every call (`unauthenticated: missing CSRF
token`); the proxy learns it from the reference's own page
(`internal/refbackend/csrf.go`) and presents it.

#### And the operator's own models, offered to the reference

The reference takes its model catalogue from the CloudCode endpoint
(`fetchAvailableModels`) and answers `GetAvailableModels` with exactly
that catalogue. So the clone starts the reference with
`-cloud_code_endpoint` pointing at **its own** CloudCode proxy — the one
`pigasus agy` uses (`internal/agyproxy`) — and the operator's models
from `~/.config/openagy/providers.json` appear in the reference's own
model picker, and a turn that picks one is translated to the operator's
provider.

| | |
|---|---|
| **no configured model** | no proxy at all — the reference talks to `https://daily-cloudcode-pa.googleapis.com` exactly as before |
| **a configured model** | the proxy is started on a loopback port, the reference's endpoint points at it, and the proxy's upstream is the same cloud host — every other request is forwarded unchanged |
| **`-cloud_code_endpoint` in `OPENAGY_REFERENCE_BACKEND_ARGS`** | the operator's own endpoint wins and **no proxy is started** (the reference takes the last occurrence of a flag) |
| **capture** | off unless `OPENAGY_CAPTURE_DIR` is set — the reference polls the catalogue |

Nothing is patched and nothing is written into the operator's state: the
reference itself promotes the catalogue it was given into the entries its
picker reads (`cascadeModelConfigData.clientModelConfigs`), and the
placeholder the proxy advertises (`MODEL_PLACEHOLDER_M<n>`, the
reference's own slot for a model its enum cannot name) comes back on the
wire, where the proxy recognises it again and translates the request.

Measured end to end: the picker listed *MiniMax M3* and *MiniMax M3.1
Flash* beside the reference's own four models, and a turn on *MiniMax M3*
was served by the operator's provider
(`minimax-m3 → MiniMax-M3 (stream, 1 msg, 12 tool)`).

Evidence: [`docs/cycles/cycle-332-reference-backend-custom-models/`](../cycles/cycle-332-reference-backend-custom-models/deliverable.md).

**The reference runs against the operator's real state.** Its own default
`-gemini_dir` is `.gemini` under the home directory, and the app data
directory is resolved inside it — so the rig serves the operator's real
account, the real model catalogue, the real projects and the real session
history (`antigravity-cli` → 398 trajectories, 360 with a title; the
sidebar lists 25 of them). The reference *writes* there as well: it owns
that directory. `OPENAGY_REFERENCE_BACKEND_ARGS` overrides any of it —
the reference takes the last occurrence of a flag, so
`-gemini_dir=/tmp/isolated` still isolates it.

Evidence: [`docs/cycles/cycle-330-reference-backend-spawn/`](../cycles/cycle-330-reference-backend-spawn/deliverable.md).

## The Stop button cancels the turn

The Web UI's chat area carries a stop button —
`button[aria-label="Cancel (⌃C)"]` — and it sends
`CancelCascadeInvocation({cascadeId, killBackgroundTasks:true})`. The clone
used to answer only `CancelRemoteAgentCascade` (the dashboard's method), so
the IDE dialect's three cancel methods went nowhere and **the button did
nothing**.

All three answer now (`internal/remotecontrol/ls_cancel.go`):

| method | request | answer |
|---|---|---|
| `CancelCascadeInvocation` | `{cascadeId, killBackgroundTasks, notifyParent}` | `{}` |
| `ForceStopCascadeTree` | `{conversationId}` | `{"stoppedConversationIds":[…]}` |
| `CancelCascadeSteps` | `{cascadeId, stepIndices}` | `{}` |

The cancel is a **real** cancel: the turn's context dies, the trajectory's
last frame reports the answer step as `INTERRUPTED` (12) and the executor
back to `IDLE` (1). Measured at four landings — 0.2 s, 3 s, 5 s and 60 s
into the turn — the turn ended 0.04 s, 0.02 s, 0.04 s and 0.37 s after the
cancel respectively. `killBackgroundTasks` kills the background tasks through
`internal/tools/tasks_kill.go`.

## Where to look

| | |
|---|---|
| `internal/rclink/rclink.go` | the browser link: the deep link, and the account chooser around it |
| `internal/urlchunk/urlchunk.go` | the URL block: the chunking and the OSC 8 frame — shared with the add-account screen |
| `internal/cliapp/slash_rc_url.go` | the TUI's `/remote-control` link rows |
| `internal/persistentrc/instance_id.go` | the installation uuid — memoised, so the daemon and the printed link agree |
| `internal/cli/subcommand_rc.go` | `remote-control start`: the launchd branch, and the foreground branch |
| `internal/cli/subcommand_rc_foreground.go` | the one foreground implementation: `serve`, `run` and the non-darwin `start` share it |
| `internal/cli/subcommand_rc_run.go` | `remote-control run` — the platform-independent foreground command |
| `internal/rchost/remote_control_serve.go` | the daemon: the dispatcher, the server, the tunnel, and the printed link |
| `internal/rctunnel/client.go` | the handshake, the version string, the instance registration |
| `internal/rctunnel/httpproxy.go` | the chunk framer and the streaming forward |
| `lsproto`/lsproto.go` | the descriptor closure and the service index |
| `lsproto`/decode.go` | `DecodeStep` — the stored blob, and the `Any` the schema cannot resolve |
| `internal/remotecontrol/ls_trajectory.go` | the trajectory |
| `internal/remotecontrol/ls_context.go` | the conversation's earlier turns, replayed into the model |
| `internal/remotecontrol/ls_steps.go` | the proto-JSON step builders |
| `internal/remotecontrol/dispatch_ls.go` | the state stream |
| `internal/remotecontrol/ls_page.go` | the page request and the stored steps |
| `internal/remotecontrol/ls_turn_sink.go` | the sink that publishes a turn while the model is talking |
| `internal/remotecontrol/ls_history.go` | the summaries, the projects, the title |
| `internal/remotecontrol/ls_files.go` | the file answers' shapes: base64, the enum names, the measured language table |
| `internal/remotecontrol/ls_terminals.go` | the Web UI's terminal methods, and the terminal answers' shapes |
| `internal/remotecontrol/ls_terminals_stream.go` | the terminal output stream: the output and title frames |
| `internal/remotecontrol/ls_audio.go` | the Web UI's voice methods, and the transcription frames |
| `internal/remotecontrol/ls_unmarshal.go` | the reference's answer for a malformed request |
| `internal/rcterminal/` | the terminal registry, the pty session, the OSC scanner |
| `internal/rcaudio/` | the audio sessions, and the Gemini-backed transcriber |
| `remotetransport/connect_errors.go` | the Connect code a failure answers with |
| `internal/remotecontrol/ls_cancel.go` | the three cancel RPCs, and the turn they kill |
| `internal/remotecontrol/dispatch_quota.go` | the quota panel's route |
| `internal/remotecontrol/dispatch_codeassist.go` | the credits panel's route: the cloud's code-assist state, cached |
| `internal/remotecontrol/ls_queue_rpc.go` | the queue's RPCs: send all, drop by index, drop by id |
| `internal/remotecontrol/model_config_fields.go` | the model config's fields the Web UI's picker reads — the eleven, and proto-JSON's rule for each |
| `internal/auth/mendel.go` | the Mendel read, and the identity that decides the flag set |
| `internal/remotecontrol/mendel_flags.go` | the answer the page reads, and the per-identity cache |
| `internal/rchost/remote_control_backends.go` | the picker's rows: the configured models, expanded one row per reasoning tier |
| `internal/refbackend/assets.go` | the reference's own page and assets, forwarded |
| `internal/refbackend/transport.go` | the transport switch: the document is redirected to the WebSocket transport |
| `internal/refbackend/csrf.go` | the reference's CSRF token: learned from its index document, refreshed on a 401 |
| `internal/refbackend/custommodels.go` | the operator's own models, offered to the reference through the CloudCode proxy |
| `internal/rchost/web_reference_assets.go` | the seam: `/__agy/*` is the clone's, the rest is the reference's |
| `remotetransport/grpcweb_codec.go` | the gRPC-Web framing — the reference's own page's dialect |
| `internal/tools/tasks_kill.go` | the background tasks a cancel kills |
| `internal/interaction/interaction.go` | the seam: the host, the request, the answer |
| `internal/remotecontrol/ls_interaction.go` | the waiting step, and the host that parks on the answer |
| `internal/remotecontrol/ls_interaction_answer.go` | the answer's three shapes (canonical, wrapped, bare string) |
| `internal/remotecontrol/ls_interaction_rpc.go` | `HandleCascadeUserInteraction` in the IDE dialect |
| `internal/tools/tools_interaction.go` | the three tools that need the user |
| `internal/convstore/append.go` | appending a turn to a conversation that already exists, and naming it |
| `internal/convstore/summary_schema.go` | the summaries table's shape — the reference's own |
| `out/web-ui-interaction/` | the reference's own waiting step and answer payload, and the clone's, side by side |
| `out/web-reference-mode/` | the reference's own page and assets through the clone, the gRPC-Web framing, and the cancel round trip |
| `out/model-config-alignment/` | the picker's mapper, verbatim, and the live `GetUserStatus` with the per-model attributes stated |
| `out/web-ui-log-fixes/` | the operator's own log, the bundle's two calls, the cloud's live code-assist state, and the clone's rig answering both |
| `out/web-ui-working-stuck/` | the reference page's transport, the pending requests on the fetch transport, and the zero POSTs on the WebSocket one |
| `out/web-desktop-mode/` | the UA matrix that decides the Mendel flag set, three live answers side by side, and the three pages in the browser |
| `out/web-desktop-mode-remote-control/` | the same switch on the remote-control path: four live daemons' answers, the two pages, the two plists |
| `out/web-terminals/` | the terminal and the voice: the reference's answers and the clone's, side by side |

## Known limits

- The replay carries the **prose only**. The store does keep the tool
  results (`convstore.Step.ToolResult`), but the replay seam is
  prose-shaped — `pigrt.HistoryTurn` is a role and a text, and the TUI's
  own session is the same — so a tool call from an **earlier** turn is not
  in the model's context. The rule is the clone's own and it is the same
  on every path: the TUI's resumed conversation and `--print -c` replay
  the same prose. The tool calls of the **current** turn are in the
  context — the agent runs its tool loop inside the turn.
- The clone writes the summary's columns, not the reference's `raw_summary`
  blob; a row the reference wrote keeps its blob.
- The **permission card** is wired for a tool that asks, but the clone's own
  permission store (`internal/permissions`) does not raise a
  `requestedInteraction` of its own: the reference's card is raised by the
  agent's permission subsystem on a generic step, not by a tool call. A
  `run_command` the clone's policy does not allow is refused by the store, not
  asked about in the page.
- The reference's **own standalone language server does not resume the turn**
  after the answer: the unary `HandleCascadeUserInteraction` request stays open
  and the step stays *Waiting for user input*. The clone's round trip
  completes. See
  [`docs/cycles/cycle-327-web-ui-interaction-round-trip/deliverable.md`](../cycles/cycle-327-web-ui-interaction-round-trip/deliverable.md).
- The title the clone gives a conversation is a heuristic; the reference asks
  the model. The two differ in wording, not in kind.
- The clone has no WebRTC mesh session — only the WebChannel backchannel
  (`rc-webrtc-mesh-defer`). The mesh is the reference's browser↔instance
  direct data channel, not the gate: the clone's `GetMendelFlags` answer
  reports the mesh experiment as disabled, and the host page takes the
  WebChannel path. The two views then render the same — the app iframe is
  mounted and the host page shows "Connecting to instance…", which is where
  the **reference's** own view sits in this environment too, because neither
  app iframe reports its UI as ready.
- The registry row is scoped to the account the daemon authenticates as,
  and the instance cannot know the browser's account index: the printed
  link therefore goes through Google's account chooser, which resolves the
  account and continues to the deep link. The chooser is the clone's own
  shape — the reference prints the bare origin and asks the user to find
  the instance by name.
- The chooser resolves the account, but the `/u/<N>/` segment it lands on
  is the **browser's** account index. Remove the account from the
  browser's list and the chooser asks again, and the index changes; the
  link stays correct, its target URL does not.
- The foreground daemon is **not persistent**: it dies with its terminal
  and does not come back after a crash. A systemd unit would fix that.
- Several instances live under one account — the reference's daemon and the
  clone's read the same token file — and the page binds by **instance id**:
  each view talks to its own instance, and both mount the app iframe.
- The clone's **JSON** answers are Go maps, and `encoding/json` writes a
  map's keys in alphabetical order — but every answer the schema knows is
  **reordered** before it leaves (`lsproto`/order.go`), into the
  descriptor's *declaration* order, which is the order protojson writes
  and the order the reference's own bytes carry. A method the schema
  does not know keeps the ordinary encoding: the schema is not the
  authority on what the clone serves.
- The picker offers the user's configured models only; the reference's
  catalogue is not served here.
- The clone's transport masks a **failed dispatch** as HTTP 200 `{}` on the
  Connect dialect, so a failed cancel looks exactly like a successful one.
  The gRPC-Web dialect does not have that shape — it carries the status in
  the headers. Not changed here (it is another surface's business).
- The reference's own trajectory frames reach the subscriber at the **end** of
  the turn, so a cancel has to be measured by the turn's end time; a live
  frame is not a witness.
- The status the **reference** puts on a stopped turn is unobserved — it
  would need a live turn in the reference itself. The clone uses
  `INTERRUPTED` (12) for an interrupted turn and `CANCELED` (6) for a step
  that was cancelled by name.
- The Web UI's **`GetAvailableModels`** is still unrouted. The app calls it
  at startup and swallows the failure, so the tier list it keeps is empty —
  the picker's own list comes from `GetUserStatus`, and that one is served.
- The app's **credits row** reads `userStatus.userTier?.availableCredits`,
  and the clone's own `userTier` block carries no credits. The *fetch* no
  longer fails (`GetLoadCodeAssist` is served), but the row stays empty.

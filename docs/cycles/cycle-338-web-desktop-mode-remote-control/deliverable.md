# The remote-control family's `--desktop` mode — deliverable

**Cycle:** `cycle-338-web-desktop-mode-remote-control`
**Subject:** `pigasus web --desktop` asked for the reference's **desktop**
UI; the remote-control family could not. The remote-control daemon answers
`GetMendelFlags` with the **CLI's** flag set, so the page it serves — and
the dashboard's page behind the tunnel — never gets the voice input, the
jetbox terminal or the aux pane redesign. This cycle gives
`remote-control serve` and `remote-control start` the same switch, on the
same rule: the identity is the only thing that decides.
**Evidence:** 【一手】 four live daemons' `GetMendelFlags` answers side by
side (`out/web-desktop-mode-remote-control/mendel-ab.txt`); 【一手】 the two
pages in the browser (`out/web-desktop-mode-remote-control/browser-ab.txt`);
【一手】 the two LaunchAgent plists
(`out/web-desktop-mode-remote-control/plist-ab.txt`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **`remote-control serve`** | the CLI's page — no microphone, no Terminal tab | `--desktop` asks for the reference's desktop UI |
| **`remote-control start`** | the CLI's page, permanently | `--desktop` writes the switch into the plist, and the daemon serves the desktop UI |
| **the rule** | the identity decides the flag set | **unchanged** — the switch only picks the identity |
| **the page** | — | **the same bytes** either way; the bundle is the reference's own |

---

## 1. The rule, restated

The backend serves a **different Mendel flag set per product**. The product
token in the `User-Agent` is the only thing that decides — not the version,
not the build number, not the body, not the endpoint:

| `User-Agent` | `enable-audio-transcription` | `enable-jetbox-terminal-v2` | `enable-aux-pane-redesign` |
|---|---|---|---|
| `antigravity/hub/2.26.0 (aidev_client; …)` | **true** | **true** | **true** |
| `antigravity/cli/1.4.4 (aidev_client; …)` | false | false | false |
| `antigravity/ide/2.26.0 (aidev_client; …)` | false | false | false |

【一手】 measured 2026-10-26/27, ten hub versions from `0.0.1` to `3.0.0`,
three runs each — see `internal/auth/mendel.go`'s package comment and
`out/web-desktop-mode/mendel-ua-matrix.txt`.

The clone's own read sent the **CLI's** identity, so it got the CLI's set,
and thirty of its flags came back `disabled` — among them the three that
gate the desktop UI. That is why the clone's page was the CLI's page.

`pigasus web --desktop` already fixed that for the standalone web server.
The remote-control family did not: its dispatcher was built with an **empty**
`MendelUserAgent`, which means the CLI's own.

This cycle changes **nothing** about the rule. It only adds the switch to
two more surfaces.

---

## 2. What this cycle ships

### 2.1 `remote-control serve --desktop`

| | |
|---|---|
| the flag | `internal/cli/subcommand_rc_serve.go` — `rcServeOpts.desktop` |
| the typed hop | `internal/cli/serve_bridge.go` — `ServeArgs.Desktop` |
| the handler | `internal/rchost/remote_control_serve.go` — `RunServe` → `buildServeStack(addr, instanceID, token, args.Desktop)` |
| the identity | the same `desktopMendelUserAgent(desktop)` the web server uses (`internal/rchost/web_reference.go`) |

The usage block **lists** the switch — that block is the clone's own.

### 2.2 `remote-control start --desktop`

The persistent daemon is a **plist**, so the switch has to survive into
`ProgramArguments`.

Two plists exist, and both were changed:

| plist | what it runs | the switch |
|---|---|---|
| the **official** one (`BuildOfficialPlist`, macOS) | `exec '<binary>' 'remote-control' 'serve'` | `'--desktop'` appended to that command line |
| the clone's own (`buildPlist`, `persistentrc.Spec.Desktop`) | `<binary> remote-control-foreground --addr … --name … --log-file …` | `--desktop` appended to `ProgramArguments` |

The hidden `remote-control-foreground` reads it in `parseForegroundFlags`
(`internal/rchost/remote_control_foreground.go`) and hands it to
`buildForegroundDispatcher`, which sets the same `Config.MendelUserAgent`.

The usage block does **not** list the switch: `remote-control start --help`
is the reference's, byte for byte, and the clone follows the same rule it
follows for `--addr` and `--log-file` — accepted, not advertised.

### 2.3 One accessor

`remotecontrol.Dispatcher.MendelUserAgent()` — the config field is
unexported, and the identity is the thing under test.

---

## 3. The live diff

【一手】 four live daemons on one machine, the same account, the same
token; the **only** difference is the switch.

```
=== 8938  remote-control serve --desktop ===
experiments: 508
  enable-aux-pane-redesign     disabled= False
  enable-audio-transcription   disabled= False
  enable-jetbox-terminal-v2    disabled= False

=== 8939  remote-control serve ===
experiments: 508
  enable-aux-pane-redesign     disabled= True
  enable-audio-transcription   disabled= True
  enable-jetbox-terminal-v2    disabled= True

=== 8942  remote-control-foreground --desktop ===
experiments: 508
  enable-jetbox-terminal-v2    disabled= False
  enable-audio-transcription   disabled= False
  enable-aux-pane-redesign     disabled= False

=== 8941  remote-control-foreground ===
experiments: 508
  enable-jetbox-terminal-v2    disabled= True
  enable-audio-transcription   disabled= True
  enable-aux-pane-redesign     disabled= True
```

The other 505 flags do not move.

【一手】 the two pages in Chrome, read off the DOM the same way:

```
                       8939 (default)      8938 (--desktop)
----------------------  ------------------  ------------------
"Record voice memo"     —                   1
"Customizations"        —                   1
"Request Review"        —                   1
```

The page is the same file on both (`internal/rcfrontend/assets/main.js`,
the reference's own bytes) and the RPC surface is the same too; the
difference is the answer to `GetMendelFlags`.

【一手】 the two plists, rendered by the clone's own code — the one without
the switch is byte-identical to the one before this cycle.

---

## 4. The roads not taken

| candidate | why not |
|---|---|
| make the remote-control page a **different** bundle | the reference serves the same bundle for both identities; the difference is the answer, not the bytes |
| add a `--desktop` to the reference's `start` usage block | that block is the reference's, byte for byte; the clone's extra flags are accepted, not advertised |
| put the switch in the `rc_secret` file or the environment | the daemon is launched by launchd; the plist is the only channel that survives the restart |
| give the **persistent** daemon its own Web UI handler | the foreground daemon is the tunnel's endpoint — the dashboard's page is the one that talks to it, and the page it serves locally is `serve`'s |

---

## Known limits

- 【存疑】 one of the four live daemons (8940, `remote-control-foreground
  --desktop`) answered with the six **local** flags only — the backend read
  failed — on three consecutive probes, while the same command on another
  port (8942) answered with the full 508 and the same daemon without the
  switch (8941) answered with the full 508 too. The read has a **four
  second** budget across both endpoints (`mendelFetchTimeout`), so a slow
  endpoint eats it; that is the plausible cause, but it was not isolated
  this cycle.
- 【一手】 the switch is a **clone extension**: the reference's
  `remote-control` has no such flag, the same way `pigasus web` is one.
- 【一手】 only the macOS path was exercised. On Linux and Windows
  `persistentrc.Start` answers `ErrUnsupported` (no plist is written), so
  the switch has no landing there.
- 【一手】 the launchd lifecycle of a `--desktop` persistent daemon was not
  driven end to end — the plist's bytes were compared, the restarted
  daemon's behaviour was not.

---

## References

- `docs/guide/remote-control.md` — "The desktop mode" and "The remote-control path"
- `out/web-desktop-mode-remote-control/` — the evidence, with the zh-CN README
- `out/web-desktop-mode/` — the previous cycle's UA matrix and the three pages
- `internal/auth/mendel.go` — the measurement that pins the rule

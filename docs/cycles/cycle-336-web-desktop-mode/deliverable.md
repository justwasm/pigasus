# The desktop UI is a different Mendel set — deliverable

**Cycle:** `cycle-336-web-desktop-mode`
**Subject:** `pigasus web` serves the reference's own bundle but not the
reference's own *desktop* UI: no voice input, no redesigned aux pane, no
Terminal tab in the right-hand sidebar. The page and the bundle are the
same bytes either way, so the difference has to come from an answer — and
it does: the backend serves a **different Mendel flag set per product**,
and the identity that decides it is the `User-Agent` of the page's own
`ListExperiments` read.
**Evidence:** 【一手】 the UA matrix — the same token, the same body, the
same endpoint, only the `User-Agent` differing
(`out/web-desktop-mode/mendel-ua-matrix.txt`); 【一手】 the reference's own
Mendel read off the wire (`out/web-desktop-mode/reference-listexperiments.txt`);
【一手】 three live `GetMendelFlags` answers side by side
(`out/web-desktop-mode/flag-set-diff.txt`); 【一手】 the three pages in the
browser (`out/web-desktop-mode/dom-diff.txt`); 【反编译】 the bundle's own
gates (`internal/rcfrontend/assets/main.js` — `lQc()`, `Mbc()`, `N7a()`,
`auxPaneComponent`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the page's Mendel read** | always the CLI's identity (`antigravity/cli/1.4.4 …`) | the CLI's by default, **the hub's** under `--desktop` |
| **the flag set the backend serves** | the CLI's — thirty app features off | the hub's — the voice input, the jetbox terminal, the aux pane redesign on |
| **the page's voice input** | absent | **present** (`aria-label="Record voice memo"`) |
| **the right-hand sidebar** | absent — the old aux pane, no overview | **the redesigned one**: Overview / Review / Files / **Terminal** / Extensions |
| **the bundle** | — | **unchanged, byte for byte** |

---

## 1. The symptom, stated exactly

`pigasus web --reference-backend` (the reference's own page) shows:

* the **voice input** next to the composer,
* **Customizations** and **Search Conversations** in the left sidebar,
* the **permission-preset selector** under the composer,
* the **right-hand sidebar**: the overview panel with its Overview /
  Review / Files / **Terminal** / Extensions tabs.

`pigasus web` (the clone's own page) shows none of it, and its right-hand
sidebar does not exist at all.

Both pages are served from `internal/rcfrontend/assets/main.js` — the
reference's own bytes, byte for byte. So the difference is an *answer*.

## 2. What the bundle reads

【反编译】 the bundle's own gates, in the clone's asset file:

**the voice input** —

```js
// lQc(): is the streaming transcription path available?
enabled: yi(a, "enable-audio-transcription")

// Mbc(): the microphone
Dc = hc ? { …, hasMicrophone: bc }   // hc = isAudioStreamingAvailable
        : { …, hasMicrophone: Mc }   // Mc = N7a(): the feature + enumerateDevices()
```

`hc` false means the microphone falls back to `N7a()`, which needs the
feature `inputComponent.enableMicrophone` to be true. With
`enable-audio-transcription` off the whole chain breaks and the composer
draws no microphone.

**the right-hand sidebar** —

```js
auxPaneComponent: {
  feature: { location: "sidebar", redesignEnabled: false, overviewPanelMode: "full" },
  applyAll: [{ runtimeGating: { customFlag: "enable-aux-pane-redesign",
                                host: [ei.Web.Desktop, ei.Electron] },
               feature: { redesignEnabled: true } }],
}
```

`enable-aux-pane-redesign` off → the old aux pane → the page draws no
`overview-panel` at all.

So both symptoms are Mendel flags. Which flags, and why are they off?

## 3. The lever: the identity of the read

【一手】 the same account's bearer token, the same request body, the same
endpoint (`https://daily-cloudcode-pa.googleapis.com/v1internal:listExperiments`),
**only the `User-Agent` differing**:

| `User-Agent` | `enable-audio-transcription` | `enable-jetbox-terminal-v2` | `enable-aux-pane-redesign` |
|---|---|---|---|
| `antigravity/hub/2.26.0 (aidev_client; …)` | **true** | **true** | **true** |
| `antigravity/cli/1.4.4 (aidev_client; …)` | false | false | false |
| `antigravity/ide/2.26.0 (aidev_client; …)` | false | false | false |

**Only the product token decides.** Ten hub versions (`0.0.1` … `3.0.0`),
three runs each: identical answers. The build number (`cl=`), the body's
`metadata.updateChannel` and the endpoint (`cloudcode-pa` vs
`daily-cloudcode-pa`) make no difference either.

The reference's own `language_server` reads with the **hub** identity —
captured off the wire with the clone's own proxy in front of it:

```
User-Agent: antigravity/hub/2.26.0 (aidev_client; os_type=darwin; arch=arm64; cl=1005016104)
```

The clone's own read (`internal/auth/mendel.go`) sent the **CLI's**
identity, so the backend answered the CLI's set — thirty app features
`disabled: true`, the three above among them.

## 4. The three answers

【一手】 three live `GetMendelFlags` answers — 508 experiments each, the
same keys, the same order:

| pair | differing entries |
|---|---|
| the clone's default vs the reference's | **34** |
| the clone's `--desktop` vs the reference's | **10** — and those ten differ in *values* (a plugin shelf's JSON, a seasonal theme's period list), not in the disabled state |
| the clone's default vs its own `--desktop` | **30 flags switch from `disabled: true` to on** |

The thirty include every flag the desktop UI reads:
`enable-audio-transcription`, `enable-jetbox-terminal-v2`,
`enable-aux-pane-redesign`, `enable-split-view`,
`enable-permissions-selector`, `enable-conversation-search-v2`,
`enable-plugins`, `enable-sidecars`, `enable-pdf-viewer`,
`scheduled-tasks-v2` …

## 5. The implementation

`--desktop` moves one thing: the identity of the page's Mendel read.

| | where |
|---|---|
| the identity itself | `internal/auth/mendel.go` — `MendelUserAgentCLI`, `MendelHubUserAgent(version)` |
| the read | `internal/auth/mendel.go` — `FetchMendelFlagsWithUserAgent`, `FetchMendelFlagsWithTokenAndUserAgent` |
| the seam | `internal/remotecontrol/daemon_config.go` — `Config.MendelUserAgent` (empty → the CLI's own) |
| the answer | `internal/remotecontrol/mendel_flags.go` — `mendelFlagsAnswer(ctx, userAgent)`, and a cache **keyed by the identity** |
| the handler | `internal/remotecontrol/dispatch_languageserver.go` — `GetMendelFlags` |
| the switch | `internal/rchost/web_reference.go` — `desktopMendelUserAgent(desktop)`; `internal/cli/subcommand_web.go` — `--desktop` |

The cache is per identity on purpose: the clone's default page and the
desktop page read the same backend with different identities, and the
backend answers each one its own set — one shared cache would hand the
CLI's set to the desktop page.

`pigasus rc serve` keeps the CLI's identity: the dashboard's host page is
not the desktop UI.

## 6. The result, in the browser

【一手】 three pages, the same conversation open, the overview panel
pulled open on the two that have one:

| | clone (default) | clone (`--desktop`) | reference |
|---|---|---|---|
| microphone | 0 | **1** | 1 |
| permission-preset selector | 0 | **1** | 1 |
| sidebar search | 0 | **1** | 1 |
| "Customizations" | 0 | **1** | 1 |
| overview panel host | 0 | **1** | 1 |
| overview panel tabs | — | **Overview / Review / Files / Terminal / Extensions** | the same |
| terminal row | — | **1** | 1 |

The two remaining differences are the two servers' own state, not the
UI's shape: the *stored* permission preset ("Request Review" vs "Turbo")
and the reference's announcement banner, which the clone has no answer
for.

## 7. The roads not taken

Every one of these was measured and ruled out before the `User-Agent`:

| candidate | measured |
|---|---|
| `__APP_CONFIG__`'s `ideName` / `ideVersion` / `isRemoteControl` | the bundle never reads them; it reads `appVersion`, `devMode`, `csrfToken`, `isRemoteControl`, `productName`, `releaseChannel` |
| `__APP_CONFIG__.productName` | only the display name ("Antigravity" vs "Antigravity CLI") |
| the host enum (`oka()`) | `web.desktop` on all three pages |
| `GetServerConfiguration`'s `subclientType` / `sandboxAvailable` | flipping both to the reference's values did **not** bring the microphone back |
| the request body's `metadata.updateChannel` | no effect (the matrix) |
| the endpoint | no effect (the matrix) |

---

## Known limits

* 【一手】 the backend is a **rollout**: one read out of ~120 answered
  `enable-audio-transcription: false` for a hub identity that answered
  `true` in every other read. The clone reads the set once per ten
  minutes and caches it, so a single reduced read does not take the page
  down — but the *first* page load can catch one.
* 【一手】 the reference's own answer marks six **plugin** flags disabled
  (`enable-plugin-auth`, `enable-pdf-viewer`, `enable-mcp-plugin-auth`,
  `enable-agent-plugins-spec`, `enable-hub-builtin-browser`,
  `enable-list-plugin-accounts-tool`) where the desktop answer does not.
  The reference decides that on its own side; the reason was not chased.
* 【推测】 whether the six plugin flags are visible on the page was not
  measured — that needs an account with the plugins installed.
* 【一手】 `sandboxAvailable` is `false` in the clone and `true` in the
  reference. It gates the *additional permission presets* and the
  sandboxed execution mode — not the flags above — and the clone has no
  sandbox, so it was left alone.
* 【一手】 the Terminal tab is **drawn**, but the clone's terminal backend
  is not the reference's; whether clicking it opens a working terminal
  was not verified in this cycle.

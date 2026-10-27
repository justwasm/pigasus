# Multi-account switching

> The account registry, the shared credential, and the per-account tier/quota cache.

## Multi-account switching (cycle `multi-account-switch`)

The reference keeps exactly **one** credential: the OAuth token in the macOS
keychain (service `gemini`, account `antigravity`) plus three on-disk copies.
Signing in again replaces it. The clone is a superset: it keeps a registry of
the accounts the user signed in to and lets the user pick the active one
**without leaving the session**.

| File | What it is |
|---|---|
| `internal/accounts/store.go` | the registry: `~/.config/openagy/accounts.json` (0600, atomic; `OPENAGY_ACCOUNTS_CONFIG` overrides) |
| `internal/accounts/activate.go` | `Current`, `Register`, `RegisterWithEmail`, `Activate`, `Remove` |
| `internal/cliapp/slash_panel_accounts.go` | the `/accounts` panel (clone-only; **listed** in the catalogue, with the Pool tab beside the account list — see *The pool's own panel* below) |
| `internal/cliapp/account_identity.go` | `syncAccountIdentity` (the session follows an account switched outside the TUI), `registerCurrentAccount` |
| `internal/cli/accounts_cmds.go` | the hidden `agy accounts {list,switch,remove}` |
| `internal/printmode/print_slash.go` | print mode's `/accounts` |

Five things that are easy to get wrong:

- **The active account IS the shared credential.** A switch is
  `auth.SaveTokenUnified(targetToken)` — the keychain entry plus the three
  on-disk paths — so the installed `agy` runs as that account from its next
  start-up (or its next token refresh; a *running* `agy` keeps the credential
  it loaded in memory). The reference's `id_token_email` field must be written
  too: `auth.SaveTokenUnified` fills it from the token's own identity, and
  `AntigravityToken.UserEmail()` reads the signed claim first and the field
  second (the field goes stale, the claim does not).
- **The transport has to be rebuilt.** The CloudCode client caches the keychain
  token it read, so a switch sets `m.backend = m.backendFor(m.model)`.
- **A failed switch must restore the previous credential.**
  `auth.RefreshIfNeeded` calls `ClearTokenUnified()` on a definitive refresh
  rejection, so a failed switch would otherwise sign the user out.
- **The `/usage` Account line follows the credential, not the session's
  start-up state.** `syncAccountIdentity` re-reads it before a model turn and
  before a slash command opens a panel; without it the panel showed the
  account the session started with after an outside switch.
- **Removing the *active* account signs out for real** (the credential is
  cleared) and continues on another registered account. `/logout` keeps the
  reference's byte-exact reply and additionally forgets the account.

Report: `docs/cycles/multi-account-switch/`.

### The tier and quota cache (cycle `accounts-tier-usage`)

The registry carries two more per-account answers, so the panel can say
*which* account to switch to without a second trip to `/usage`.

| File | What it is |
|---|---|
| `internal/accounts/cache.go` | `Get`, `Update`, `SetPlanTier`, `SetQuota`, `SetToken` — the atomic read-modify-write cycles (the package mutex serializes them) |
| `internal/auth/quota_snapshot.go` | `QuotaSnapshot` (the serializable form of a summary), `QuotaWindow`, `WeeklyRemaining`, `FiveHourRemaining` |
| `internal/auth/quota.go` | `FetchQuotaSummaryWithToken` — the quota of *one* account |
| `internal/auth/plan_tier.go` | `FetchPlanTierWithToken` |
| `internal/auth/unified_config.go` | `RefreshQuiet` — refreshes **without** writing or clearing the shared credential |
| `internal/cliapp/account_identity.go` | `fetchAccountQuotaCmd`, `accountAccessToken`, the session's `quotaCache` |

Five things that are easy to get wrong:

- **An inactive account's refresh must not touch the shared credential.**
  `RefreshIfNeeded` writes it (that would *switch* the active account) and
  clears it on a rejection (that would *sign the user out*). `RefreshQuiet`
  does neither — it is the only refresh an inactive account may use.
- **The tier is queried for the active account only** and cached per account,
  so an account that has been active once keeps its tier. The panel's `r`
  key refreshes the *quota* of every account, not the tiers.
- **The panel shows the lowest remaining fraction across the model groups.**
  The response has one group per family and one bucket per window; the row
  shows one number per window. A **missing** bucket is `5h -`, never `0.0%` —
  two of this machine's accounts have no five-hour bucket at all.
- **`/usage` reuses the snapshot `/accounts` fetched** (`quotaCache`, 2
  minutes). Opening `/usage` after `/accounts` shows the live numbers within
  a second, with no request; a cold session still queries the server.
- **The fetch is serialized and budgeted at 45 s.** It is two round trips
  (a refresh measured 9.2 s, the quota call 6.9 s) — a 20 s budget turned a
  slow success into a timeout.

Report: `docs/cycles/accounts-tier-usage/`.

## The account-routing policy (cycle `cycle-360-account-pool-routing`)

The registry answers "which account did the user pick?". The **policy** answers
"which account serves *this* turn?" — automatically, driven by the accounts'
remaining quota.

The policy is a section of `~/.config/openagy/config.json`, beside the
endpoint overrides. It is **not** in `providers.json`: that file answers
"which model talks to which provider", and its only writer is
`internal/models.Editor`.

```json
{
  "endpoints": { },
  "account_pool": {
    "enabled": true,
    "strategy": "quota_aware",
    "threshold": 0.15,
    "refresh_interval": "2m",
    "cooldown": "5m",
    "accounts": ["a@example.com", "b@example.com"],
    "max_attempts": 3,
    "switch_on_quota_error": true
  }
}
```

| File | What it is |
|---|---|
| `accountpool/accountpool.go` | the policy: the `Config` shape, the four strategies, the threshold, `Choose` — **pure**, no I/O, no `internal/` dependency, so it is importable on its own |
| `internal/accountrouting/accountrouting.go` | the runtime: the router, the quota cache, the credential lookup, the failover wrapper |
| `endpoints/config.go` | `Config.AccountPool`, `CurrentAccountPool()` and `SaveAccountPool()` — the file's only reader **and** writer |
| `internal/backendselect/backendselect.go` | installs the token function and the wrapper — **always**, so the policy can be turned on live |

| strategy | what it does |
|---|---|
| `sticky` | the account the session is signed in with, until its quota drops below the threshold |
| `round_robin` | the next account of the pool, in turn |
| `quota_aware` | the account with the most quota left |
| `failover` | the account the session is signed in with, until a quota error comes back |

Six things that are easy to get wrong:

- **The shared credential is NOT rewritten.** The routed account is *this
  process's* account; the keychain entry and the three on-disk copies keep
  pointing at the account the user picked, so the installed `agy` and the
  `/accounts` panel keep showing what the user expects. A load balancer that
  rewrote the keychain would change the identity of every other program on
  the machine.
- **An inactive account is refreshed with `RefreshQuiet`, never
  `RefreshIfNeeded`.** The latter *writes* the shared credential (it would
  switch the active account) and *clears* it on a rejection (it would sign the
  user out) — the same rule the tier/quota cache follows.
- **The quota figure is the LOWEST bucket** across the account's windows, not
  the weekly one: the five-hour window runs out long before the weekly one.
  An **unknown** figure is not a zero — the account is eligible and ranks
  last. A pool does not punish an account for not having been asked.
- **The threshold is a preference, not a wall.** When every account is below
  it, the one with the most left is used: a turn with no account cannot be
  served at all.
- **The decision is per turn.** The CloudCode client holds a `TokenFunc` and
  the geminicode client calls it before **every** request, so the router is
  installed there — a pool that rotates rotates between two sentences of the
  same answer. A quota read never blocks a turn: the figure is seeded from the
  registry's snapshot and refreshed in the background.
- **The routing is installed ALWAYS, not only when the policy is on.** The
  policy is read per turn, so the router has to be in place before the policy
  exists. With the routing off, the router answers with the **transport's own**
  credential — `backend.DefaultTokenSource()`, the very source the client
  would have built for itself — so the default path is byte-identical to
  before. The transport **installs** that source when it is built, so a
  rebuilt transport (the TUI does that on a model change) gets a fresh one.
  Cycle 361 changed this: see *The pool's own panel* below.

A quota error — the 429 the reference reports as *"Individual quota reached"* —
puts the account out of the pool for `cooldown` and retries the turn on
another account, up to `max_attempts`. The default is one attempt: the turn
fails as before and the **next** turn goes to another account.

Report: `docs/cycles/cycle-360-account-pool-routing/`.

## The pool's own panel (cycle `cycle-361-account-pool-panel`)

The policy had two homes: the configuration file and the documentation. A file
the user has to edit by hand is a policy they forget about. Cycle 361 gave it
a **panel** — the second tab of `/accounts`, and the `/pool` command on its
own.

| File | What it is |
|---|---|
| `internal/cliapp/slash_panel_pool.go` | the Pool tab: the rows, the keys, the writes |
| `internal/cliapp/slash_panel_accounts.go` | the tab bar, the tab switch, the account list's own half |
| `internal/cliapp/slash_panels.go` | `initOpenedSlashPanelData` — `/pool` opens the panel on the Pool tab |
| `internal/cliapp/slash_panel_routing.go` | `commandPanels["pool"]`, the Pool tab's row count |
| `internal/tuilib/commands.go` | the `/pool` row in the catalogue (the clone's **fourth** superset command) |
| `endpoints/config.go` | `SaveAccountPool` — the file's writer |

The tab bar is the `/resume` picker's: the active tab is bold, underlined and
blue, the inactive one is the separator grey, and the row ends with
*"(tab to cycle)"*. The reference has no counterpart for any of this — it has
one credential and no switcher — so the whole panel is clone-only.

| Row | What it does |
|---|---|
| Enabled | turns the routing on or off |
| Strategy | walks the four strategies |
| Threshold | the quota floor, in percent (`25%` and `0.25` both read) |
| Refresh interval | how often a quota figure is read again |
| Cooldown | how long a spent account stays out |
| Max attempts | how many accounts one turn may try |
| Switch on quota error | whether a quota error moves the next turn |
| Accounts in pool | the allow-list, as a submenu over the registry's accounts |

Five things that are easy to get wrong:

- **A change is written out at once.** There is no "save" button and nothing
  is staged: the panel writes the section on every committed change, and the
  routing reads the policy per turn, so the **next** turn runs on the new
  policy. Nothing is rebuilt and nothing has to be confirmed.
- **The panel writes through the file's own writer.** `endpoints.SaveAccountPool`
  is the only writer of `config.json`; the panel builds an `accountpool.Config`
  and hands it over. The edit is made on the raw document, so the endpoint
  overrides and any section a newer version wrote survive byte for byte.
- **The router must already be installed.** The panel's "immediate effect" is
  only true because the transport carries the router whether the policy is on
  or not; the off-path answers with `backend.DefaultTokenSource()`, so the
  wrapper is not a second implementation of the credential cascade.
- **The cursor belongs to the view, not to the panel.** It is reset when the
  tab changes, so the Pool tab opens on its first row and the account list
  opens on its first account. `m.panel.cursor` is shared by every panel kind.
- **A value that does not parse is refused.** A threshold of "soon" leaves the
  row open with a notice; the panel never writes a policy it could not read
  back.

Report: `docs/cycles/cycle-361-account-pool-panel/`.

## The web UI's account switcher

The web UI (`pigasus web`, and the cloud remote view) carries the same three
operations in its top-right corner: a button with the active account's initial
opens a panel that lists the registered accounts, switches to another one, adds
one and removes one. It is the web UI's mirror of the TUI's `/accounts` panel.

| File | What it is |
|---|---|
| `internal/rcfrontend/accountsapi.go` | the API: the list, the switch, the removal, the token guard, the payloads |
| `internal/rcfrontend/accountsignin.go` | the "add account" flow: the authorization URL, the code exchange, the registration |
| `internal/rcfrontend/accountswidget.go` | the widget: the CSS, the script, the mount |
| `internal/rcfrontend/frontend.go` | `Options.DisableAccountSwitcher`, the routing, the injection into the served HTML |
| `internal/remotecontrol/dispatch_quota.go` | the `RetrieveUserQuotaSummary` route: the app's own quota panel, answered from the active account |

| Endpoint | What it does |
|---|---|
| `POST /__agy/api/accounts` | the accounts, the active one, the registry's path, each row's cached quota |
| `POST /__agy/api/accounts/quota` | one account's remaining quota — the live query, then the cache |
| `POST /__agy/api/accounts/switch` | `{"email": "…"}` — activate |
| `POST /__agy/api/accounts/remove` | `{"email": "…"}` — forget |
| `POST /__agy/api/accounts/add` | start a sign-in → `{"id": "…", "authUrl": "…"}` |
| `POST /__agy/api/accounts/add` | `{"id": "…", "code": "…"}` — exchange the code, register the account |

Every endpoint answers a POST **and** a GET: the remote-control tunnel turns
every proxied request into a POST (`internal/rctunnel/httpproxy.go`), so a
caller that reaches the server through the cloud tunnel must not get a 405 for
using the wrong verb.

Six things that are easy to get wrong:

- **The widget is not a bundle patch.** The bundle is minified and its
  identifiers move on every release (`docs/guide/reference-drift.md`), so a
  rewrite of it breaks silently. The widget attaches itself to the DOM
  instead, on the stable test id the pane titlebar carries
  (`[data-testid="pane-titlebar-toolbar"]`), and falls back to a fixed button
  in the top-right corner when that slot is not there (the phone layout).
- **The panel cannot live in the titlebar.** The titlebar clips its overflow
  (`overflow: hidden`), so a panel inside it is cut off after the first row.
  Measured in the browser: the panel's box was 260×213 px and 40 px of it were
  visible. The panel is a `position: fixed` element in `document.body`,
  positioned from the button's rectangle.
- **The endpoint is guarded by a per-process token.** The token is 24 random
  bytes from `crypto/rand`, written into the page the server serves and
  required as `X-Agy-Accounts-Token`, `?token=` or a `token` body field. The
  endpoint sends **no** CORS header, so a page on another origin cannot read
  the answer — `pigasus web` binds `0.0.0.0` by default, and a LAN-visible
  "switch my account" endpoint needs more than a constant.
- **The device-authorization grant does not work for this client.** Google's
  device endpoint refuses the CLI's own public client id:
  `{"error":"invalid_client","error_description":"Invalid client type."}`
  【一手】 measured 2026-10-26. The "add account" flow is therefore the
  reference's own browser OAuth flow (PKCE) with the paste-back the TUI's
  login modal also needs: the server keeps the verifier, the browser sends
  back the code Google gives.
- **The active credential is the truth.** `accounts.Current()` (the shared
  credential) decides which row is marked active; the registry's own `Active`
  field is only a hint, because the reference can change the credential too.
- **A switch reaches the running session on its next turn.** The remote
  control's backend is built per turn
  (`internal/rchost/remote_control_backends.go`), so the new credential is
  read then. A *running* `agy` keeps the credential it loaded in memory, as
  everywhere else in the clone.

### Seeing that the switch worked

A switch has to be *visible*, or it is a guess. The report that started this
was exactly that: the account was switched, the page was reloaded, the Settings
panel still showed the old address, and there was no way to tell whether the
switch had happened. Three places answer that now — all of them in the
widget's own script (`internal/rcfrontend/accountswidget.go`):

| Where | What it shows |
|---|---|
| a toast in the top-right corner | "Now signed in as …" — the switch's answer, independent of the panel's lifetime |
| the Settings sidebar's Account row | the live address, in the app's own chip |
| the model picker's "View Usage" | the account's remaining quota per window — the app's own panel, fed by the `RetrieveUserQuotaSummary` route |

Four things that are easy to get wrong:

- **The app's own address is a start-up snapshot.** The Settings panel renders
  the address the server told it when the session began: the `GetUserStatus` RPC
  answers `d.cfg.AccountEmail` (`internal/remotecontrol/dispatch_userstatus.go`)
  【反编译】 — the clone's dispatcher, not a live read of the credential. A
  switch rewrites the shared credential; the snapshot does not follow it, so
  the panel would keep showing the old address. The proper fix — a live
  `GetUserStatus` — lives in `internal/remotecontrol`, which is not this
  change's path, so the widget rewrites the address in the DOM instead.
  【一手】 measured in the browser: after a switch the sidebar's Account row and
  the "Send feedback as …" label show the new address, before and after a
  reload.
- **The quota panel is the app's, so the backend has to answer it.** The
  "View Usage" entry of the model picker opens a panel that asks the language
  server for the account's quota:
  `/exa.language_server_pb.LanguageServerService/RetrieveUserQuotaSummary`
  over the WebSocket. Without a route the transport answers `{}`, the panel
  finds no groups and renders "No quota information available." — the Web UI
  cannot tell which account it is on, which is the one thing the switcher has
  to prove. The route is `internal/remotecontrol/dispatch_quota.go`; the
  answer is the active account's quota, so the numbers follow the switch.
  【一手】 measured in the browser: bravo shows 45 % / 91 %, alpha 12 % / 35 %
  after the switch, in the app's own rows and its own ring gauges.
- **The answer is proto-JSON, and the oneof member is at the bucket's top
  level.** The response is `exa.language_server_pb.RetrieveUserQuotaSummaryResponse`
  → `google.internal.cloud.code.v1internal.RetrieveUserQuotaSummaryResponse`:
  `{"response": {"description": "…", "groups": [{"displayName": "…",
  "description": "…", "buckets": [{"bucketId": "…", "displayName": "…",
  "description": "…", "window": "…", "remainingFraction": 0.45,
  "resetTime": "2026-10-29T12:00:00Z", "disabled": false}]}]}}`.
  `remaining_fraction` is a member of the `remaining` oneof, so proto-JSON
  writes it under its JSON name (`remainingFraction`) *at the bucket's top
  level* — nested under `"remaining"` the app decodes no fraction and renders
  an empty ring. The "Resets in 3d 4h" line is only computed when the bucket
  carries a non-empty `description`, so the description is never empty.
  【一手】 the descriptor closure the Web UI parses
  (`lsproto`/descriptor/language_server.protoset`,
  `google/internal/cloud/code/v1internal/quota_summary.proto`).
- **The widget does not draw the quota.** The panel is the app's own
  component; a widget that appended its own rows would replace it with a
  hand-made one. The widget reads the quota only for *its own* account rows
  (`/__agy/api/accounts/quota`). React still owns every node it rendered: the
  widget rewrites a text node's `nodeValue` rather than the element's
  `textContent`, and never replaces an element the app rendered. The browser
  console stays clean across a switch and a reload.
- **The quota belongs to the account, not to the session.**
  `accounts.FetchQuota` queries with the account's own credential, so the
  numbers are the account's. The registry keeps the snapshot; the route
  serves a snapshot younger than two minutes as it is and queries otherwise,
  and a failed query falls back to the cached numbers instead of to an error.

### The widget's own rows

The widget's list shows every registered account, not only the active one, so
each row carries that account's quota. The panel has a **fixed width**
(`min(380px, 80vw)`), so it does not move when the list becomes the sign-in
session and back. The row is drawn at the density of the TUI's `/usage` panel
(`internal/usagepanel`):

```
user04@example.com
Google AI Pro
GEMINI MODELS
Weekly Limit Remaining      Five Hour Limit Remaining
33.0%                       60.0%
████████████████░░░░░░░░░░  ██████████████████████████░░░░░
Refreshes in 71h 57m        Refreshes in 1h 57m
▸ CLAUDE AND GPT MODELS
```

- The panel's **width is constant**. It used to be the content's own width, so
  the sign-in session — which carries Google's long sign-in URL — widened the
  panel under the user's cursor. 【一手】 measured in the browser: 280 px in the
  list state, 372 px in the sign-in state, 380 px in both after the fix.
- The quota is **one column per window, side by side**: the weekly limit on the
  left, the five hour limit on the right, so the left column reads the week's
  remainder and the right one the five hours'. The order is the window's own
  (`weekly`, then `5h`), not the order the server happened to send.
- The **bar** is the fraction that is left, and its colour uses the web app's
  own gauge thresholds: red under 10 %, yellow under 25 %, green above
  (`$Mb` in the bundle). The **percentage** sits under the window's name and the
  **countdown** under the bar — the countdown is the TUI's own wording
  (`tuiviewer.QuotaCountdown`: "71h 57m", "Refreshes in").
- The **first group is open** and the other model groups (Claude and GPT
  models) are behind a **disclosure**: a group with many accounts would
  otherwise make the list twice as long. The closed line carries **no
  countdown** — the bucket's own row carries it once the group is open, and a
  closed line that repeated it was noise. An open group lays its windows out
  in the same two columns.
- The **active account** is marked with the app's own selected surface — the
  `--sidebar-secondary` token the app paints a selected conversation with —
  behind the address, and with the foreground colour for the address itself.
  The other addresses sit at the muted colour, so the active one is found
  without a badge, a border or a colour of its own. 【一手】 measured in the
  browser against the app's own tokens.
- The numbers are the **registry's snapshot** (the live read is the app's own
  panel's job), and the active account's row uses the widget's own live read
  when it has one.

### The list scrolls

The list has its own scroll area (`max-height: min(320px, 50vh)`), and it
**cannot be scrolled** unless the widget stops rebuilding the panel when the
page moves. The scroll listener was a `window` listener in the *capture*
phase, so the list's own scroll event re-rendered the panel — and a
re-render replaces the list element, so the scroll position went back to zero
on every wheel tick. The symptom was a scrollbar that never moved.

Two changes fix it: the scroll and resize listeners only **move** the panel
(`reposition`) instead of rebuilding it, and a rebuild that does happen (a
switch, a sign-in) **restores** the list's `scrollTop`. 【一手】 measured in
the browser with twelve accounts: the list's `scrollHeight` is 2157 px against
a 320 px viewport, and the wheel moves it.


### Hard constraints

- The switch is **local to the machine**: the active account *is* the shared
  credential (the keychain plus three on-disk copies), so a switch rewrites
  it. Nothing about the switch travels to the cloud.
- The widget is **on by default**; the opt-out is the negative flag
  `Options.DisableAccountSwitcher`, because the front-ends that build the
  handler (`internal/rchost`) are not part of this change.
- The sign-in needs a **paste-back**: the code comes back on Google's callback
  page, on another origin, so the browser cannot read it for the widget.

### Open questions

- Whether the cloud forwards `/__agy/api/accounts` to the instance is not
  measured: the tunnel proxies the browser's requests to the local server,
  and the local server routes every non-RPC path to the web UI
  (`remotetransport/tcp_server.go`), but the cloud's own routing
  table decides which paths it relays. Unverified for the remote view; the
  standalone `pigasus web` is measured.

### Limitations

- The tier and the quota in the widget's own list are the **cached** answers
  from the registry; the widget does not refresh them. The app's own quota
  panel does refresh: the route reads the server when the cached snapshot is
  older than two minutes, and falls back to the cache when the read fails.
- The widget is served by `internal/rcfrontend`, so it needs the page to come
  from the clone's own server. A page served by the cloud's own copy of the
  bundle would not carry it.
- The registry's accounts are the clone's; the reference `agy` has exactly one
  credential and no switcher, so none of this has a byte-for-byte counterpart.

## Logging in on Android/Termux (cycle `android`)

On Termux `/etc/resolv.conf` points at a loopback nameserver that nothing
answers on, so **every** outbound hostname lookup fails — including the
OAuth device flow the login runs on. `internal/dns` replaces
`net.DefaultResolver` so the lookups go to a working resolver instead.

| File | What it is |
|---|---|
| `internal/dns/android.go` | the fallback: `install`, `dialWithFallback`, the detection rule |
| `internal/dns/android_test.go` | the proof that the plain HTTP path picks the resolver up |
| `cmd/*/main.go` | the four binaries blank-import it (`pigasus`, the two shims, `openagy-models`) |

Five things that are easy to get wrong:

- **The fix is at the resolver, not at the call site.** `net.DefaultResolver`
  is read *at dial time*: `net.Dialer.resolver()` returns it whenever the
  Dialer's own `Resolver` field is nil, `net/http`'s default transport dials
  through a zero `net.Dialer` (`zeroDialer`), and `golang.org/x/oauth2` falls
  back to `http.DefaultClient`. So `auth.fetchOAuthToken`,
  `auth.populateUserEmailFromUserInfo` and the device flow's `DeviceAuth` /
  `DeviceAccessToken` are all covered without touching one of them.
  `TestInstall_RoutesHTTPThroughTheCustomResolver` pins that.
- **Two detection signals, either one is enough.** `runtime.GOOS ==
  "android"` is baked in at build time (the release ships a
  `GOOS=android/arm64` archive) and survives a scrubbed environment;
  `TERMUX_VERSION` is exported by the Termux shell and also covers a
  `linux/arm64` build inside a proot distro.
- **The dial ignores the server the resolver asks for.** That *is* the
  hack: the resolver wants the dead `/etc/resolv.conf` nameserver, and the
  dial goes to the configured list instead.
- **The fallback is a dial fallback, not a query fallback.** A UDP dial to a
  closed port *succeeds* — the port only shows up as an ICMP error on the
  first read — so `1.1.1.1` is only reached when `8.8.8.8` cannot be
  routed at all.
- **A public resolver bypasses a split-DNS setup.** `$OPENAGY_DNS_SERVERS`
  (comma-separated `host:port`) points the fallback at a different
  resolver — a VPN's own, or Termux's `dnsproxy` on `127.0.0.1:53`. A
  hardcoded public resolver is never consulted by a VPN that splits DNS.

**What it does not cover:** the vendor `agy` binary that `pigasus agy`
execs. That is a separate process with its own resolver and cannot be
patched from here. Its CloudCode traffic goes to `127.0.0.1` (the launcher
exports `CLOUD_CODE_URL`), so only its own OAuth round trip would need
DNS — sign in with `pigasus login` first and the child finds the token on
disk instead.

Report: `docs/cycles/android-termux-dns/` (the package landed with the
Android release target; the tests and the `$OPENAGY_DNS_SERVERS` override
were added later).


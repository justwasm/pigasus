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
| `internal/cliapp/slash_panel_accounts.go` | the `/accounts` panel (clone-only, invocable but **not** listed — the catalogue count is byte-pinned to 99) |
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


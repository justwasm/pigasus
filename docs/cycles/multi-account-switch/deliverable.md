# Cycle multi-account-switch — several Google accounts, one shared credential

Status: implemented, verified live against the installed **agy 1.4.6**, tests green.
Scope: `internal/accounts` (new), `internal/auth`, `internal/cli`, `internal/printmode`, `cmd/wagyu`.

The reference (`agy`) keeps exactly one credential: the OAuth token in the macOS
login keychain plus three on-disk copies of the same blob. Signing in again
replaces it. There is no list of accounts, no switch, and no way to use a
second account's quota without signing the first one out.

The clone is a **superset** here. It keeps a registry of the accounts the user
signed in to, lets the user pick which one is active *without leaving the
session*, and writes the choice into the **same credential the reference
reads** — so the installed `agy` runs as the selected account too, from its
next start-up.

---

## 1. The shared credential is the switch

The clone does not invent a second credential store. The *active account* **is**
the account whose token lives in the shared locations:

| Location | Written by | Read by |
|---|---|---|
| macOS keychain, service `gemini`, account `antigravity` | `auth.SaveKeychainToken` | the reference (first), the clone |
| `~/.config/antigravity/oauth-token` | `auth.SaveAntigravityTokenTo` | both |
| `~/.config/antigravity/antigravity-auth.json` | idem | both |
| `~/.gemini/antigravity-cli/antigravity-oauth-token` | idem | both |

`auth.LoadTokenUnified` reads them in that order (the env var
`JETSKI_OAUTH_TOKEN` first, the keychain second). A switch is therefore one
write: `accounts.Activate` refreshes the target account's token when it is
about to expire and calls `auth.SaveTokenUnified` — every location at once.

Two consequences fall out of that, and both were measured:

- **The clone's next request uses the new account.** The CloudCode client
  caches the keychain token it read, so the session's transport is rebuilt
  after a switch (`applyActiveAccount` → `m.backendFor(m.model)`).
- **The installed `agy` uses the new account** — on its next start-up, and on
  its next token refresh, because it reads the same keychain entry. A *running*
  `agy` keeps the credential it loaded in memory (see §7).

## 2. The reference's own email field (measured)

The reference does not keep the `id_token` in the credential. After its first
refresh it writes:

```json
{"auth_method": "consumer", "id_token_email": "navigaid@gmail.com",
 "token": {"access_token": "ya29.…", "refresh_token": "1//…", "expiry": "…"}}
```

【一手】 read out of the live keychain entry with
`security find-generic-password -s gemini -a antigravity -w` (the blob is
`go-keyring-base64:` + base64 JSON).

Two experiments against the installed **agy 1.4.6** pinned down how it reads the
account (both in `captures.md`):

| Credential written | The reference's banner shows |
|---|---|
| `{"id_token": "<jwt with email claim X>"}` | **X** |
| `{"id_token_email": "X"}` | **X** |

So the reference reads the `id_token` claim and the `id_token_email` field,
whichever it finds. The clone therefore:

- **writes both** — `auth.SaveTokenUnified` fills `id_token_email` from the
  token's own identity, so a credential the clone writes always names the
  account. This is what makes the switch visible to the reference;
- **reads both** — `AntigravityToken.UserEmail()` prefers the signed claim and
  falls back to the field, so a credential the *reference* refreshed (no
  `id_token` any more) still identifies its account.

The claim wins because the field can be stale: the reference keeps the field
across a refresh, so it still named an account that was no longer the one the
token belonged to (measured on this machine — see §8).

## 3. The registry

`internal/accounts` holds the list.

| File | What it is |
|---|---|
| `internal/accounts/store.go` | `Account`, `Store`, `Path`, `Load`, `Save` (atomic, 0600), `Find`/`Put`/`Remove`/`Emails` |
| `internal/accounts/activate.go` | `Current`, `Register`, `RegisterWithEmail`, `Activate`, `Remove` |

- The file is `~/.config/openagy/accounts.json` (0600, atomic write). The env
  override is `OPENAGY_ACCOUNTS_CONFIG`; `XDG_CONFIG_HOME` is honoured.
- The tokens live in it in plaintext, refresh token included, so switching back
  needs no new sign-in. The reference keeps its own token in plaintext too, so
  the posture is the same; the file is 0600.
- `Account.AddedAt` is immutable — re-registering an account does not restart
  the clock.
- The *email* match is case-insensitive (the user types it by hand).

`accounts.Current()` returns the shared credential and the account's email. It
resolves the email in three steps: the token's `id_token` claim, the token's
`id_token_email` field, and — for a credential whose id_token is gone — the
registry, matched by **refresh token**. The refresh token is the stable half of
the credential: the access token rotates every hour, the refresh token does
not.

## 4. The `/accounts` panel (clone-only)

`cmd/wagyu/slash_panel_accounts.go`. The reference has no such command, so
there is nothing to match byte for byte; like `/provider`, the command is
**invocable but not listed** — `internal/tuilib.DefaultSlashCommands` holds 99
entries and the count is byte-pinned to the reference's.

```
Accounts
> navigaid@gmail.com              -
  switch-probe@example.com        -                  (active)
  /Users/gear/.config/openagy/accounts.json
Keyboard: ↑/↓ Navigate  enter Switch  a Add account  d Remove  esc Exit
```

| Key | Action |
|---|---|
| `↑`/`↓` | move the cursor |
| `enter` | make the row the active account |
| `a` | open the sign-in modal and add an account |
| `d` | forget an account |
| `esc` | leave the panel |

The panel re-reads the registry on every open and after every action, and it
**learns an account that became active outside the clone** (a switch from the
`agy accounts` command, a sign-in in another terminal): the shared credential
is the truth, and an account nobody registered is registered on the spot.

## 5. The switch, step by step

`accounts.Activate(ctx, email)`:

1. look the account up in the registry (case-insensitive);
2. refresh its token when it is within five minutes of expiry;
3. `auth.SaveTokenUnified(tok)` — keychain plus the three on-disk paths;
4. update the registry's entry (`LastUsed`, the refreshed token) and its
   `Active` field.

A failed switch leaves the session signed in as it was: the previous credential
is written back before the error is returned. That matters because
`auth.RefreshIfNeeded` calls `ClearTokenUnified()` when the refresh server
definitively rejects the refresh token (`invalid_grant`) — a failed switch
would otherwise sign the user out.

After the switch the session state follows:

| State | Where |
|---|---|
| the banner's account | `m.email` (`applyActiveAccount`) |
| the transport | `m.backend` is rebuilt from `m.backendFor(m.model)` — the CloudCode client caches the keychain token |
| the plan tier | `fetchPlanTierCmd()` |
| the `/usage` panel | the account line and a fresh quota fetch (§6) |

### Removing an account

`d` on the **active** account signs out for real: the shared credential is
cleared (`auth.ClearTokenUnified`), so neither the clone nor the reference
keeps using it — and the session continues on another registered account, so a
sign-out does not end the session. `d` on an inactive account just forgets it.

`/logout` keeps the reference's byte-exact reply line and additionally forgets
the active account from the registry, so `/accounts` cannot switch back to a
credential that was signed out.

## 6. The `/usage` panel and an account switched outside the TUI

The bug this cycle closed: the `/usage` panel's `Account:` line came from the
session's cached `m.email`, so an account selected **outside** the TUI was not
shown — the panel kept the account the session started with.

`cmd/wagyu/account_identity.go` adds `syncAccountIdentity()`:

- it re-reads the shared credential (the keychain, then the on-disk copies —
  the same cascade `auth.LoadTokenUnified` uses);
- when the account changed, `m.email` follows, the transport is rebuilt, and a
  plan-tier refresh is scheduled;
- it runs **before a model turn** (`startModelTurn`) and **before a slash
  command opens a panel** (`dispatchSlashLine`) — the two moments where a
  stale identity would be visible.

The credential read is one `security` subprocess (~30 ms) per turn, which is
invisible next to a model turn.

The reference caches its credential in memory for the whole session, so it
picks an outside switch up on its next token refresh (or on the next start-up).
The clone re-reads instead — the superset the panel promises.

## 7. What the installed `agy` sees

Measured live (full transcripts in `captures.md`):

| Moment | The reference's banner |
|---|---|
| the clone switched to the probe account | `switch-probe@example.com` (a fresh `agy` start) |
| the clone switched back | `navigaid@gmail.com` (a fresh `agy` start) |
| a *running* `agy`, switched from the clone | unchanged — it holds its credential in memory |

The third row is the reference's own behaviour, not a divergence: it re-reads
the keychain on start-up and on a refresh, not per request.

## 8. The CLI

`agy accounts <list|switch|remove>` (`internal/cli/accounts_cmds.go`). The
command is hidden, like the other clone-only subcommands: `agy --help` lists
the reference's twelve visible subcommands and nothing else.

```
$ agy accounts list
accounts: /Users/gear/.config/openagy/accounts.json
navigaid@gmail.com                       -
naivgaid@gmail.com                       -
btwiuse@gmail.com                       -                  (active)
```

`switch` writes the shared credential (so `agy` itself is now that account),
`remove` forgets an account and signs out when it was the active one. Print
mode answers `/accounts` with the same list (`internal/printmode/print_slash.go`).

## 9. The sign-in side

| Path | What it does |
|---|---|
| the TUI's `/login` modal | on success the token is registered (`registerAccount`), the session's account and transport are updated |
| `internal/cli/dispatch.go` (the device flow) | the same registration |
| start-up | the account the session starts on is registered (`registerCurrentAccount`), so `/accounts` lists it instead of showing an empty registry |

## 10. Files and tests

| File | What it is |
|---|---|
| `internal/accounts/store.go` | the registry (path, load, atomic 0600 save, find/put/remove) |
| `internal/accounts/activate.go` | `Current`, `Register`, `RegisterWithEmail`, `Activate`, `Remove` |
| `internal/accounts/accounts_test.go` | 11 hermetic tests (temp `HOME`, `OPENAGY_DISABLE_KEYCHAIN`) |
| `cmd/wagyu/slash_panel_accounts.go` | the `/accounts` panel: render, keys, switch/remove/add |
| `cmd/wagyu/slash_panel_accounts_test.go` | 10 panel tests, including the outside-switch sync |
| `cmd/wagyu/account_identity.go` | `syncAccountIdentity`, `registerCurrentAccount` |
| `internal/auth/antigravity_token.go` | the `id_token_email` field, `UserEmail()`'s two sources |
| `internal/auth/unified_config.go` | `SaveTokenUnified` fills the reference's email field |
| `internal/cli/accounts_cmds.go` | the hidden `agy accounts` subcommand |
| `internal/printmode/print_slash.go` | print mode's `/accounts` |

The shared credential locations, the keychain service/account and the refresh
rule are documented in `AGENTS.md` (see "The `agy` wrapper" and the model
routing section); this cycle adds the account registry on top of them.

### 硬约束

- **One credential at a time.** The shared locations hold exactly one account.
  The registry is the clone's; the reference still sees one credential, so
  "switching" for the reference means "the next start-up runs as the selected
  account".
- **The tokens in the registry are plaintext**, refresh token included. The
  file is 0600, the same posture as the reference's own token file, but it is
  one more copy of a long-lived secret.
- **`agy` needs a restart** (or a token refresh) to see a switch. A running
  `agy` keeps the credential it loaded in memory.
- **The plan tier of an account is only known for the account the session runs
  on.** The registry does not store a tier per account, so the panel shows `-`
  for the others.
- **A switch costs a refresh when the target token is about to expire** — a
  network call to the sign-in server. A failed refresh leaves the session on
  the previous account and restores its credential.
- **`OPENAGY_DISABLE_KEYCHAIN=1` disables the keychain writes**, so the
  switch only reaches the on-disk copies in that mode.

### 存疑

- The reference's **`id_token_email` field** is read from the keychain entry
  only. Whether the reference also reads the on-disk copies' field was not
  measured (the keychain is present on this machine, so the file copies were
  never the deciding path).
- Whether the reference **refreshes the keychain entry when the id_token is
  present** was not measured: the experiment that wrote a doctored `id_token`
  left the entry untouched, so the refresh had not happened.
- The reference's own field can be **stale** — on this machine it named an
  account the token no longer belonged to. Which of the two the reference
  prefers when both disagree (the claim vs the field) was not measured
  separately; the clone prefers the claim.
- The **plan tier** of an account is fetched per session, not per account. A
  tier stored in the registry (`Account.PlanTier`) is never written by the
  clone today.
- The **quota numbers** in `/usage` are fetched live with the shared
  credential, so they follow the account — but the reference's own quota
  display for a *running* `agy` was not captured (the account on this machine
  is location-blocked, so the reference's CloudCode calls answer 403).

### limitations

- The live verification used a **synthetic second account**: the same valid
  access token with a doctored `email` claim. It proves the *mechanism* (the
  credential write, the panel, the banner, the `/usage` line, the reference's
  own banner) and nothing about a second account's *quota* — the token was the
  real one, so the quota stayed the real account's. A real second account was
  not signed in during the cycle.
- The **`agy accounts` CLI** was exercised for `list` and for the error path;
  `switch` and `remove` were exercised through the panel and the package
  tests, not through the CLI's own argv handling.
- The panel's **layout** is the clone's own shape (a title, rows, a notice, the
  registry path, the legend). The reference has no panel to match.
- The reference's **`/usage` panel** was not compared: the reference on this
  machine cannot reach CloudCode (403), so its quota view never rendered.

# Cycle 293: Sign-in / Sign-out Flow Parity with `agy 1.4.3`

## Overview

The clone's auth surface was three stubs: `hasOAuthToken()` returning a bool, a
`/logout` that deleted one file, and a `NewRunMode` that hardcoded
`navigaid@gmail.com` as the account email (a value transcribed out of a capture,
not a real identity). This cycle replaces all three with the reference's actual
flow, captured live from `agy 1.4.3`.

---

## 1. Recovered behaviour

Captured by running the reference binary under a throwaway `HOME` so the
machine's real credentials could not leak into the capture:

| Surface | Recovered output |
|---|---|
| Unauthenticated entry | 5-row half-block logo, then `Welcome to the Antigravity CLI. You are currently not signed in.` |
| Method menu | ` ⣷  Signing in... Select login method:` / `> 1. Google OAuth` / `  2. Use a Google Cloud project` |
| Footer | `↑/↓ Navigate · enter Select` |
| After OAuth | `Your browser should open automatically. If not:` + URL + `Copy and paste the URL or click on the link below:` + two 78-cell rules around ` → Click here to authenticate`, then `If you aren't automatically redirected, paste the authorization code below:` + `> authorization code...` |
| Footer | `esc Back · enter Submit` |
| `/logout` (OAuth) | `Signed out from server: daily-cloudcode-pa.googleapis.com` |
| `/logout` (`GEMINI_API_KEY`) | `The active session uses a Gemini API key from the environment (GEMINI_API_KEY). To change it, update or unset the environment variable.` |
| `/logout` (`AGY_ADC_AUTH`) | `The active session uses Application Default Credentials (AGY_ADC_AUTH). To change accounts, run 'gcloud auth application-default login' or unset the variable.` |

The `Signed out from server: %s` format string is 【一手】 — it is at
`0x103307c93` in `out/local-macos/xref/strings-addrs.tsv`. The argument is
still 【推测】: the only server identity the clone holds is the Cloud Code
endpoint host.

---

## 2. What changed

### 2.1 macOS Keychain write path (`internal/auth/keychain_darwin.go`)

The client already *read* the keychain (`gemini` / `antigravity`) but could not
*write* it, so a completed sign-in was lost on the next launch. Added
`SaveKeychainToken`, byte-identical to the reference's own encoding:

```
go-keyring-base64:<base64 of the token JSON>
```

with `SaveKeychainTokenFn` as the test seam. Writes are suppressed under
`flag.Lookup("test.v") != nil` or `OPENAGY_DISABLE_KEYCHAIN`: `/usr/bin/security
add-generic-password` raises an interactive macOS authorisation prompt when it
is invoked from a test subprocess, which killed the test binary.

### 2.2 PKCE + authorization-code exchange (`internal/auth/oauth.go`)

`GeneratePKCE` (32-byte verifier, S256 challenge, 16-byte state) and
`BuildAuthorizationURL` reproduce the 1.4.3 parameter set — `access_type=offline`,
`prompt=consent`, `redirect_uri=https://antigravity.google/oauth-callback`,
and the seven scopes (`cloud-platform`, `userinfo.email`, `userinfo.profile`,
`cclog`, `experimentsandconfigs`, `aicode`, `openid`).
`ExchangeAuthorizationCode` posts to `https://oauth2.googleapis.com/token`.

### 2.3 Login TUI (`cmd/wagyu/login_model.go`, new)

Four states — method select → OAuth (URL + code input) → exchanging → success —
rendering the captured strings. The bubbletea v2 signature is worth recording:
`Model.View()` returns a `tea.View`, built with `tea.NewView(str)` and read in
tests via `m.View().Content`.

### 2.4 `/login` without a restart

The clone previously answered `/login` with `Please restart the CLI…`, which the
reference never says. `runMode` now carries a `login *loginModel`; `/login` and
the "no credentials and a model prompt was submitted" path both install it, and
`Update` / `View` delegate to it while it is non-nil. On success the session
picks up the new credentials immediately: the transport is rebuilt through
`m.backendFor(m.model)` and `m.email` is re-resolved. Esc on the method menu
cancels back to the transcript; Ctrl+C quits.

### 2.5 Paste in the code field

Bracketed paste arrives as `tea.PasteMsg`, which the login model had no case for,
and `ctrl+v` fell through to a default branch that appended nothing. Both now
insert: `tea.PasteMsg` is trimmed and appended, and `ctrl+v` / `cmd+v` read the
OS clipboard through the existing `clipboardReadText()` seam.

### 2.6 Real account email

`navigaid@gmail.com` was a capture fixture, not an identity. Two causes:

1. `LoadTokenUnified` consulted the on-disk token files *before* the keychain,
   so a stale `antigravity-oauth-token` (whose `id_token` was empty) shadowed
   the live keychain entry. The order is now keychain-first, matching the
   reference's own note that the macOS token lives in the keychain.
2. Nothing ever read the `email` claim. Added `(*AntigravityToken).UserEmail()`,
   which base64url-decodes the JWT payload, plus `EffectiveUserEmail()` with a
   `userinfo` fallback and `AGY_USER_EMAIL` / `OPENAGY_USER_EMAIL` overrides.

The banner, `/help` and `/usage` now resolve through `EffectiveUserEmail()` and
fall back to the fixture only when no credential exists at all (which is the
state the offline tests exercise).

---

## 3. Tests

`cmd/wagyu/login_model_test.go` (new) covers the initial view, menu navigation,
the OAuth screen, paste (all three paths), the Cloud-project branch, the
exchange success/error transitions, and `/logout` in each of its three modes.
`internal/auth/antigravity_token_test.go` pins the JWT `email` decode.

`unified_config_test.go` gained an `ANTIGRAVITY_HOME` / `HOME` isolation fix:
`TestSaveTokenUnified_WritesAllPaths` and `TestRefreshIfNeeded_ClientSecret` were
writing through the real `~/.gemini/antigravity-cli/antigravity-oauth-token`
because they only set `HOME`, clobbering the live credential during a test run.
That is what left the machine holding a `ya29.fake-refreshed` placeholder.
`TestPanelUsageLayout` now asserts against `auth.EffectiveUserEmail()` rather
than the fixture string.

---

## 4. Deliberately unchanged

`/logout` remains idempotent and still returns a `Signed out from server:` line
when no token exists, matching the reference's behaviour rather than erroring.
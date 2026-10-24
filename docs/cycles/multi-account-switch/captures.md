# Cycle multi-account-switch — live captures

Everything below was recorded on this machine (macOS/arm64) against the
installed **agy 1.4.6** (`~/.local/bin/agy`) and the clone built from this
tree (`go build -o /tmp/wagyu ./cmd/wagyu`).

The tmux sessions are `acctlive` (the clone) and `accref` (the reference).

---

## 1. The registry the clone keeps

The user's own accounts, learned by the clone as it started and as the accounts
were switched (the panel re-reads the shared credential on every open, and
registers an account that became active outside the clone):

```
$ /tmp/wagyu accounts list
accounts: /Users/gear/.config/openagy/accounts.json
navigaid@gmail.com                       -
naivgaid@gmail.com                       -                  (active)
btwiuse@gmail.com                       -
```

## 2. The `/accounts` panel

```
$ tmux send-keys -t acctlive "/accounts" Enter
```

```
  ▀▀▄      ▄▀▀    Pigasus CLI 1.4.5
   ▀▀▀    ▀▀▀     naivgaid@gmail.com (Google AI Plus)
    ▀▀▄▄▀▀▀▀      MiniMax M3.1 Flash
     ▀▀▀▀▀▀       /tmp
      ▀▀▀▀
──────────────────────────────────────────────────────────────────────────────
>
──────────────────────────────────────────────────────────────────────────────
Accounts
> navigaid@gmail.com              -
  naivgaid@gmail.com              Google AI Plus     (active)
  btwiuse@gmail.com               -
  libredot@gmail.com              -
  chap67j8man@gmail.com           -
  /Users/gear/.config/openagy/accounts.json
Keyboard: ↑/↓ Navigate  enter Switch  a Add account  d Remove  esc Exit
```

The `(active)` marker is on the row the **shared credential** names, not on the
registry's own `Active` field — the reference can change the credential too.

## 3. The `/usage` panel follows the account

```
$ tmux send-keys -t acctlive "/usage" Enter
```

```
└ Models & Quota
  Account: naivgaid@gmail.com
```

The account line is the session's identity, resolved from the shared credential
when the panel opens (`syncAccountIdentity`), not a value cached at start-up.

## 4. The reference sees the same account

```
$ tmux send-keys -t acref "$HOME/.local/bin/agy" Enter
```

```
      ▄▀▀▄        Antigravity CLI 1.4.6
     ▀▀▀▀▀▀       naivgaid@gmail.com
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     /tmp
  ▄▀▀      ▄▀▀
```

The clone and the reference read the same keychain entry, so they name the same
account.

---

## 5. The two experiments that pinned the reference's account source

Both experiments wrote a credential into all four shared locations and started
a **fresh** `agy`. The access token was the real one in both cases; only the
email differed, so the banner is the discriminator.

### Experiment A — an `id_token` whose claim carries the email

```json
{"auth_method": "consumer",
 "id_token": "<jwt with email claim switch-probe@example.com>",
 "token": {"access_token": "ya29.…", "refresh_token": "1//…", "expiry": "…"}}
```

```
      ▄▀▀▄        Antigravity CLI 1.4.6
     ▀▀▀▀▀▀       switch-probe@example.com (Google AI Pro)
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
```

The reference reads the **`id_token` claim**. The keychain entry was not
rewritten afterwards (the keys were still `auth_method`, `id_token`, `token`),
so no refresh happened during the experiment.

### Experiment B — the reference's own `id_token_email` field

```json
{"auth_method": "consumer",
 "id_token_email": "switch-probe@example.com",
 "token": {"access_token": "ya29.…", "refresh_token": "1//…", "expiry": "…"}}
```

```
      ▄▀▀▄        Antigravity CLI 1.4.6
     ▀▀▀▀▀▀       switch-probe@example.com
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
```

The reference reads the **field** too. This is the shape the reference itself
writes after a refresh — its refresh response carries no `id_token`, so the
email would be lost otherwise.

The clone therefore writes both fields (`auth.SaveTokenUnified` fills
`id_token_email` from the token's own identity) and reads both
(`AntigravityToken.UserEmail`).

## 6. The keychain entry, read raw

```
$ security find-generic-password -s gemini -a antigravity -w
go-keyring-base64:eyJ0b2tlbiI6eyJhY2Nlc3N0b2tlbiI6…
```

decoded:

```json
{"auth_method": "consumer",
 "id_token_email": "navigaid@gmail.com",
 "token": {"access_token": "ya29.…", "refresh_token": "1//…", "expiry": "2026-10-23T15:39:49.57094+08:00"}}
```

Note that the field and the `id_token` claim can disagree on a long-lived
machine: the reference keeps the field across a refresh, so it went stale. The
clone prefers the signed claim and only falls back to the field.

---

## 7. The switch, end to end

With a synthetic second account in the registry (the same valid access token
with a doctored `email` claim — the mechanism is what is under test, not the
quota):

```
Accounts
> navigaid@gmail.com              -
  switch-probe@example.com        -                  (active)
  /Users/gear/.config/openagy/accounts.json
Keyboard: ↑/↓ Navigate  enter Switch  a Add account  d Remove  esc Exit
```

`↑` + `enter`:

```
Accounts
> navigaid@gmail.com              -                  (active)
  switch-probe@example.com        -
  Switched to navigaid@gmail.com.
  /Users/gear/.config/openagy/accounts.json
```

The shared credential after the switch:

```
$ security find-generic-password -s gemini -a antigravity -w | base64 -d
{"auth_method": "consumer", "id_token": "…", "id_token_email": "navigaid@gmail.com",
 "token": {"access_token": "ya29.…", …}}
```

and a fresh `agy`:

```
      ▄▀▀▄        Antigravity CLI 1.4.6
     ▀▀▀▀▀▀       navigaid@gmail.com
```

## 8. An account switched *outside* the TUI

The shared credential was rewritten from the shell (what the
`agy accounts switch` command and a sign-in in another terminal do), while the
clone kept running.

Before the fix the `/usage` panel kept the account the session started with —
the report that opened this cycle. After it:

```
$ tmux send-keys -t acctlive "/usage" Enter
```

```
└ Models & Quota
  Account: switch-probe@example.com
```

The same with a real account, switched from the shell while the clone ran
(`agy accounts switch btwiuse@gmail.com`, then the panel reopened):

```
└ Models & Quota
  Account: chap67j8man@gmail.com
```

and the banner of the same running session:

```
   ▀▀▀    ▀▀▀     switch-probe@example.com (Google AI Pro)
```

`cmd/wagyu/account_identity.go` — `syncAccountIdentity` — re-reads the shared
credential before a model turn and before a slash command opens a panel.

## 9. Removing an account

```
$ tmux send-keys -t acctlive "d"
```

```
Accounts
  navigaid@gmail.com              -                  (active)
  Removed switch-probe@example.com.
  /Users/gear/.config/openagy/accounts.json
```

Removing the **active** account signs out for real (the shared credential is
cleared) and continues on another registered account, so the session survives.

## 10. The CLI

```
$ /tmp/wagyu accounts list
accounts: /Users/gear/.config/openagy/accounts.json
navigaid@gmail.com                       -
naivgaid@gmail.com                       -                  (active)
btwiuse@gmail.com                       -
```

```
$ /tmp/wagyu accounts switch nobody@example.com
accounts: unknown account "nobody@example.com"
$ echo $?
1
```

Print mode:

```
$ /tmp/wagyu --backend shim -p "/accounts"
navigaid@gmail.com	-
naivgaid@gmail.com	-	(active)
btwiuse@gmail.com	-
```

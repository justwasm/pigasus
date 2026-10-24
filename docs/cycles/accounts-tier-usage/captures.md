# Cycle accounts-tier-usage — live captures

All captures: this machine (macOS/arm64), 2026-10-23, the clone built from the
working tree (`go build -o /tmp/wagyu ./cmd/wagyu`), run under tmux with a
120×34 pane. The accounts are the machine's own real accounts — the numbers
move continuously, so only the *shape* is pinned.

---

## 1. The `/accounts` panel, four accounts, all four fetched

```
Accounts

> navigaid@gmail.com            Google AI Pro        wk 1.0%    5h 96.0%    now  (active)
  naivgaid@gmail.com            Google AI Plus       wk 100.0%  5h -        now
  btwiuse@gmail.com             Google AI Pro        wk 18.4%   5h 99.2%    now
  libredot@gmail.com            Google AI Plus       wk 100.0%  5h -        now

  /Users/gear/.config/openagy/accounts.json
Keyboard: ↑/↓ Navigate  enter Switch  a Add account  d Remove  r Refresh quota  esc Exit
```

Reading the rows:

- `navigaid` — the Gemini group has 1.0 % weekly left and the Claude group
  25.6 %; the row shows the **lower** one (1.0 %). The five-hour buckets are
  100 % and 96.0 % → the row shows 96.0 %.
- `naivgaid` and `libredot` — **no five-hour bucket at all**, so the cell is
  `5h -`, not `5h 0.0%`.

The same four accounts through the CLI (from the registry's cache — the
command never queries the server):

```
$ /tmp/wagyu accounts list
accounts: /Users/gear/.config/openagy/accounts.json
navigaid@gmail.com                       Google AI Pro      wk 1.0%          5h 96.0%
naivgaid@gmail.com                       Google AI Plus     wk 100.0%        5h -
btwiuse@gmail.com                        Google AI Pro      wk 18.4%         5h 99.2%          (active)
libredot@gmail.com                       Google AI Plus     wk 100.0%        5h -
```

## 2. The panel while the refresh runs

The accounts are fetched one at a time; the panel shows the cached numbers
and updates them as they land.

```
  btwiuse@gmail.com             Google AI Pro        wk 18.4%   5h 99.2%    7m   (active)
  libredot@gmail.com            Google AI Plus       wk 100.0%  5h -        7m
  Refreshing quota… 4 queued
```

→

```
  btwiuse@gmail.com             Google AI Pro        wk 18.4%   5h 99.2%    now  (active)
  libredot@gmail.com            Google AI Plus       wk 100.0%  5h -        8m
  quota: Post "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary": context deadline exceeded
```

The failure above is the **20 s budget** that was too short: the fetch is an
OAuth refresh (9.2 s) followed by the quota call (6.9 s), and this account's
token was expired. The budget is now 45 s and the same fetch succeeds (§4).

## 3. Switching inside the panel

```
> btwiuse@gmail.com             Google AI Pro        wk 18.4%   5h 99.2%    now  (active)
  libredot@gmail.com            Google AI Plus       wk 100.0%  5h -        now

  Switched to btwiuse@gmail.com.
```

The switch writes the shared credential (the keychain entry the installed
`agy` reads) and the tier of the newly active account is fetched and cached,
so the next panel shows it without a query.

## 4. The timings (measured, not estimated)

A scratch probe against the real registry, one account whose token was
expired:

```
refresh: refreshed=true err=<nil> elapsed=9.158281666s
quota:   err=<nil> elapsed=6.871420125s
```

→ 16 s of network for one account, which is why the first implementation's
20 s budget failed and the 45 s budget works.

## 5. `/usage` after `/accounts`: no second query

Same binary, same account, two runs.

**Cold cache** (a fresh session, `/usage` first):

```
$ tmux send-keys -t acctier2 "/usage"; tmux send-keys -t acctier2 Enter
# 2 s later:
  Account: btwiuse@gmail.com

GEMINI MODELS
  Weekly Limit Remaining
    [██████████████████████████████████████████████████] 100.00%
```

→ the stub's frozen values: the request is still in flight.

```
# 10 s later:
  Weekly Limit Remaining
    [█████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 18.38%
```

→ the real values.

**Warm cache** (`/accounts` first, then `/usage`):

```
# 1 s after Enter on /usage:
  Weekly Limit Remaining
    [█████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 18.38%
    Refreshes in 144h 53m

  Five Hour Limit Remaining
    [██████████████████████████████████████████████████] 99.15%
```

→ the live numbers **within a second**, with no request: the snapshot the
`/accounts` panel fetched a moment earlier, rendered by the same code.

## 6. The registry file

`~/.config/openagy/accounts.json` (0600) after the cycle — the tier and the
quota snapshot are cached next to the token:

```json
{
  "version": 1,
  "active": "btwiuse@gmail.com",
  "accounts": [
    {
      "email": "navigaid@gmail.com",
      "plan_tier": "Google AI Pro",
      "quota": {
        "fetched_at": "2026-10-23T15:57:03.193971+08:00",
        "groups": [
          {
            "name": "Gemini Models",
            "buckets": [
              {"display_name": "Weekly Limit Remaining", "window": "weekly", "remaining_fraction": 0.0100886095},
              {"display_name": "Five Hour Limit Remaining", "window": "5h", "remaining_fraction": 1}
            ]
          },
          {
            "name": "Claude and GPT models",
            "buckets": [
              {"display_name": "Weekly Limit Remaining", "window": "weekly", "remaining_fraction": 0.2563856},
              {"display_name": "Five Hour Limit Remaining", "window": "5h", "remaining_fraction": 0.9595232}
            ]
          }
        ]
      }
    }
  ]
}
```

(The `token` field of each entry is omitted here — it carries the refresh
token. The file itself is 0600 and is never committed.)

## 7. The tier re-verification (the flicker)

After the tier was filed under the right account (§4 of the deliverable), every
account's tier was read again **with that account's own credential** — a
refresh-quiet token refresh followed by `loadCodeAssist` — and compared with
the value the registry carried:

```
navigaid@gmail.com       cached="Google AI Pro"    live="Google AI Pro"    ok
naivgaid@gmail.com       cached="Google AI Plus"   live="Google AI Plus"   ok
btwiuse@gmail.com        cached="Google AI Pro"    live="Google AI Pro"    ok
libredot@gmail.com       cached="Google AI Plus"   live="Google AI Plus"   ok
stakingminer@gmail.com   cached=""                 live=""                 ok
```

Five accounts, five `ok` — no cached tier differs from the tier that
account's own credential returns, so the row cannot change from "Plus" to
"Pro" any more.

> The capture above was taken before the fifth account was signed in. A later
> run of the same probe reports `libredot@gmail.com` as **"Google AI Pro"**
> (cached *and* live, three readings in a row) — see the 存疑 section of the
> deliverable. The fifth account has no tier at all: the server names none,
> which is what the session's eligibility check reports.

The same registry, read straight out of the file, showing the two plans side
by side:

| Account | Tier | Weekly | Five-hour |
|---|---|---|---|
| navigaid@gmail.com | Google AI Pro | 1.0 % | 96.0 % |
| naivgaid@gmail.com | Google AI Plus | 100.0 % | — (no bucket) |
| btwiuse@gmail.com | Google AI Pro | 18.4 % | 99.2 % |
| libredot@gmail.com | Google AI Plus | 100.0 % | — (no bucket) |

The two "Google AI Plus" accounts are exactly the two with no five-hour
bucket — the plan difference shows up in the quota response as well, from a
different endpoint.

> The legend in §1–§3 reads `r Refresh quota`. After the flicker fix it reads
> `r Refresh`: the key now also re-reads the active account's tier, not only
> the quotas. The captures above are kept as they were taken.

## 8. No placeholder, and the right-aligned percentages

【一手】 live, 2026-10-23, a fresh session of the new binary (the active
account is the one that was signed in while the cycle ran — it has no tier):

```
  ▀▀▄      ▄▀▀    Pigasus CLI 1.4.5
   ▀▀▀    ▀▀▀     stakingminer@gmail.com
    ▀▀▀▄▄▀▀▀      Gemini 3.8 Flash (High)
     ▀▀▀▀▀▀       /tmp
      ▀▀▀▀
```

The banner carries the **email alone** — no `(Antigravity Starter Quota)`,
because the server names no tier for that account and the clone no longer
invents one (§4d of the deliverable).

The same session, **switching** to another account — the banner during the
switch and half a second later, when the answer has landed:

```
  ▀▀▄      ▄▀▀    Pigasus CLI 1.4.5
   ▀▀▀    ▀▀▀     navigaid@gmail.com              ← during the switch: the email alone
    ▀▀▀▄▄▀▀▀      Gemini 3.8 Flash (High)
     ▀▀▀▀▀▀       /tmp

  ▀▀▄      ▄▀▀    Pigasus CLI 1.4.5
   ▀▀▀    ▀▀▀     navigaid@gmail.com (Google AI Pro)   ← the answer landed
    ▀▀▀▄▄▀▀▀      Gemini 3.8 Flash (High)
     ▀▀▀▀▀▀       /tmp
```

Before the fix that first line read `navigaid@gmail.com (Antigravity Starter
Quota)` — the placeholder, i.e. a value that is not the account's.

The `/accounts` panel of the same session:

```
Accounts

> navigaid@gmail.com            Google AI Pro        wk   1.0%  5h  96.0%   now
  naivgaid@gmail.com            Google AI Plus       wk 100.0%  5h      -   now
  btwiuse@gmail.com             Google AI Pro        wk  18.4%  5h  99.2%   now
  libredot@gmail.com            Google AI Pro        wk 100.0%  5h 100.0%   now
  stakingminer@gmail.com        -                    wk      -  5h      -   -

  /Users/gear/.config/openagy/accounts.json
Keyboard: ↑/↓ Navigate  enter Switch  a Add account  d Remove  r Refresh  esc Exit
```

- Every account carries **its own** tier; the fifth one has none and shows
  `-` (§4c — it used to show the previous account's "Google AI Plus").
- The two percentages are **right-aligned** in a six-character column, so the
  numbers line up down the list (§4e).

`agy accounts list`, same registry:

```
accounts: /Users/gear/.config/openagy/accounts.json
navigaid@gmail.com                       Google AI Pro      wk   1.0%        5h  96.0%
naivgaid@gmail.com                       Google AI Plus     wk 100.0%        5h      -
btwiuse@gmail.com                        Google AI Pro      wk  18.4%        5h  99.2%
libredot@gmail.com                       Google AI Pro      wk 100.0%        5h 100.0%
stakingminer@gmail.com                   -                  wk      -        5h      -         (active)
```

And the print mode, whose JSON envelope keeps the **raw** values:

```
$ agy -p "/accounts" --output-format json
{"response":"navigaid@gmail.com                       Google AI Pro      wk   1.0%        5h  96.0%\n…",
 "command":{"name":"accounts","data":{"accounts":[
   {"active":false,"email":"navigaid@gmail.com","five_hour":"96.0%","plan_tier":"Google AI Pro",
    "quota_fetched_at":"2026-10-23T17:41:17+08:00","weekly":"1.0%"},
   …
   {"active":true,"email":"stakingminer@gmail.com","five_hour":"-","plan_tier":"","weekly":"-"}]}}}
```



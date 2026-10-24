# Cycle accounts-tier-usage — cached tiers and per-account usage in `/accounts`

Status: implemented, verified live against the user's own accounts, tests green.
Scope: `internal/auth`, `internal/accounts`, `cmd/wagyu`, `internal/cli`, `internal/printmode`.

The previous cycle (`multi-account-switch`) gave the clone a list of accounts and
a switch. What it did **not** give is the thing that makes a list of accounts
useful: the two numbers that say *which* account to switch to — the plan tier
and the quota. The registry even had a `PlanTier` field, but nothing ever
wrote it.

This cycle fills both in, and makes the `/usage` panel stop being the only
place the quota can be seen.

---

## 1. What was asked

> In `/accounts` I want you to cache the queried account tier: *Starter, Google
> AI Pro, AI Plus*, etc. Each time only the selected active account has its
> tier queried, and ideally this screen can also show each account's
> usage/quota, so you don't have to query again in the `/usage` page.

Three requirements, and they pull in different directions:

| Requirement | Consequence |
|---|---|
| cache the tier | the tier must survive the session that learned it → it belongs in the registry, not in the TUI's memory |
| only the active account is queried | one tier query per session, not one per account → the tier of an account is learned when it *becomes* active |
| show every account's usage | the quota has to be queried for accounts that are **not** active → each query needs *that account's* credential |

The last one is the interesting part, and it is where the safety work went
(§4).

## 2. The tier cache

`accounts.Account.PlanTier` now gets written. The write happens where the
tier is fetched:

| File | What it does |
|---|---|
| `cmd/wagyu/tui_stream_prompt.go` | `fetchPlanTierCmd` became a **method**: it fetches the tier, then caches it under the account behind the shared credential (`accounts.SetPlanTier`) |
| `internal/accounts/cache.go` | `SetPlanTier` / `SetQuota` / `SetToken` / `Update` — the atomic read-modify-write cycles |

The tier is queried exactly where it was before — once per session, for the
active account — but the answer is now *kept*, so the panel can show the tier
of every account that has been active at least once:

```
> navigaid@gmail.com            Google AI Pro        wk   1.0%  5h  96.0%   now
  naivgaid@gmail.com            Google AI Plus       wk 100.0%  5h      -   now
  btwiuse@gmail.com             Google AI Pro        wk  18.4%  5h  99.2%   now
  libredot@gmail.com            Google AI Pro        wk 100.0%  5h 100.0%   now
  stakingminer@gmail.com        -                    wk      -  5h      -   -
```

【一手】 live capture, this machine's registry. The fifth account was signed in
while the cycle ran; it has no tier (§4c).

## 3. The per-account quota

`accounts.Account.Quota` is a new field holding an `auth.QuotaSnapshot` — the
serializable form of the live `retrieveUserQuotaSummary` response
(`internal/auth/quota_snapshot.go`).

**The number the panel shows.** The response has one group per model family
("Gemini Models", "Claude and GPT models") and one bucket per window inside a
group ("Weekly Limit Remaining", "Five Hour Limit Remaining"). The panel
shows one number per window: the **lowest remaining fraction across the
groups**, because that is the window that decides whether the account can keep
working. `navigaid` above has 1.0 % weekly left on Gemini and 25.6 % on
Claude, so the row says `wk 1.0%`.

**A bucket may be missing.** 【一手】 two of this machine's accounts (both
"Google AI Plus") have **no five-hour bucket** — only a weekly one. The panel
prints `5h -` for those, and never `5h 0.0%`: a missing window is not an
exhausted one. `QuotaWindow()` classifies a bucket by the reference's own
display name first and the wire's `window` field second (`weekly` / `5h`).

**The fetch order.** The panel queries one account at a time, the active
account first (its snapshot is the one `/usage` reuses, §5). A registry with
four accounts does not fire four token refreshes at once.

**The `r` key** re-queues every account's quota **and** re-reads the tier of the
account the session runs on. The tier of an *inactive* account is not queried:
it belongs to the account that was active when it was learned (§硬约束).

## 4. The credential each query uses — and the refresh that must not fire

The quota belongs to the account behind a credential, so each account is
queried with its own access token. That splits the refresh into two paths,
and the difference is the whole safety argument:

| Account | Token | Refresh |
|---|---|---|
| active | the **shared** credential (`auth.LoadTokenUnified`) | `RefreshIfNeeded` — writes the shared credential (correct: it *is* the shared account) |
| inactive | its own **stored** token | `RefreshQuiet` — new in `internal/auth/unified_config.go` |

`RefreshQuiet` is a new method, and it differs from `RefreshIfNeeded` in two
ways that matter:

- it does **not** call `SaveTokenUnified` — a refresh that wrote the shared
  credential would silently *switch the active account*;
- it does **not** call `ClearTokenUnified` on a rejected refresh — a revoked
  refresh token on an *inactive* account would otherwise *sign the user out*
  of the active one.

Both are silent-corruption failures, and both are avoided by the one method
that exists for this path.

**The credential is read once.** The token that answers the request and the
account the answer is filed under have to come from the *same* read of the
shared credential. The first implementation read it twice — once for the
token, once for the email — which means a switch landing between the two
reads files one account's tier (or quota) under another account's name.
The tier path (`fetchPlanTierCmd`) and the quota path
(`accountAccessToken`) now take the token and the email out of a single
`accounts.Current()` call.

## 4a. The registry writes are serialized

The quota fetch writes the registry from its own goroutine, and the panel,
the CLI and the sign-in flow write it from the UI thread. The package now
holds a mutex over every read-modify-write cycle (`Load`, `Save`,
`Register`, `Activate`, `Remove`, `Update`), so a concurrent write is not
lost. `Activate` holds it across the token refresh — a network call — which
blocks the background fetch for the length of that refresh (measured: 9 s).

## 4b. The tier that changed under the user's eyes

> 而且我发现这个查询的时候会闪，比如说默认显示为 Plus，然后变成 Pro，你是不是使用
> Plus 作为 默认的 Tier 字符串了?
>
> ("…and I noticed it flickers during this query — it shows *Plus* by default and
> then becomes *Pro*. Did you use *Plus* as the default tier string?")

**No.** The default tier string is the reference's own placeholder,
`"Antigravity Starter Quota"` (`cmd/wagyu/main_dispatch.go:280`,
`cmd/wagyu/tui_runmode_new.go:56`). There is no "Plus" in the code except
inside the two tier names the server itself returns.

The "Plus" the user saw was the **registry's cached tier**, and the flicker
was that cache being **corrected while the panel was open**: the panel renders
the cached tier, the live tier query lands, the cache is rewritten, the rows
are re-read, and the row shows the new value.

Two causes, both fixed:

| Cause | Fix |
|---|---|
| **the race of §4** — the tier was read from one credential and filed under another account's name, so the cache could hold *another* account's tier ("Google AI Plus" under a "Google AI Pro" account) | the credential is read **once** (§4) |
| the panel fell back to the session's tier, which is the **placeholder** until the answer arrives, so a row could show the placeholder and then the real tier | `runMode.tierFetched`: only a *fetched* tier is rendered; an account without one shows `-` |

【一手】 The registry was re-verified account by account afterwards — every
account's tier read again with **that account's own credential** — and every
cached value matches:

| Account | Cached | Live (own credential) |
|---|---|---|
| navigaid@gmail.com | Google AI Pro | Google AI Pro |
| naivgaid@gmail.com | Google AI Plus | Google AI Plus |
| btwiuse@gmail.com | Google AI Pro | Google AI Pro |
| libredot@gmail.com | Google AI Pro | Google AI Pro |
| stakingminer@gmail.com | *(none)* | *(none)* |

So no wrong tier is left in the registry, and the "Plus → Pro" transition can
no longer happen: the cache the panel renders *is* the tier that account's own
credential returns. A consistent detail: the two "Google AI Plus" accounts are
exactly the two with **no five-hour bucket** (§3) — a plan-level difference
that matches the tier, read from two independent endpoints.

The fifth row is the account the user signed in with while this cycle was
running: the server names **no** tier for it, and the session's own
eligibility check says the account is not available in this location. It has
no cached tier, and the panel shows `-` for it (§4c).


## 4c. The tier that belonged to another account

> 我发现新增的这个账号并没有 Google AI Plus 的 tier，但是还是默认显示 AI Plus:
> stakingminer@gmail.com
>
> ("…the account I just added does not have the Google AI Plus tier, but it
> still shows AI Plus by default…")

The account had just been added by a sign-in, and it has **no** tier at all
(§4b). The panel showed "Google AI Plus" anyway.

**The cause.** The tier was the *session's*, not the account's. A session that
signs in as another account kept the tier it had read for the previous one,
and the panel fell back to it for the new row:

| step | what the session knew |
|---|---|
| the session ran as an "AI Plus" account | `m.tier = "Google AI Plus"` |
| the user signed in as `stakingminer@gmail.com` | the tier was never re-read — the session still said "Google AI Plus" |
| the panel rendered the new row | the registry carries no tier for it, so the panel fell back to the session's tier → "Google AI Plus" |

Three fixes, one idea — **the tier belongs to an account, not to a session**:

| Fix | Where |
|---|---|
| the session records *which* account its tier was read for (`tierAccount`), and the panel falls back to it only for that account | `cmd/wagyu/tui_run.go`, `cmd/wagyu/slash_panel_accounts.go` |
| a sign-in forgets the previous account's tier and reads the **new** account's tier | `cmd/wagyu/tui_update_mouse.go` (`finishLogin` returns the tier command now) |
| a switch and an outside change of the credential forget it too | `applyActiveAccount` → `forgetSessionTier`, `syncAccountIdentity` |

The tier answer now carries the account it belongs to (`planTierMsg.email`),
so a tier can never be filed under, or rendered against, another account.

## 4d. No placeholder at all

> 但是我发现你现在又把 Antigravity Starter Quota 设成默认值了 … 我宁愿你什么都不要显示，
> 只展示邮箱即可 … 包括 /accounts 也是，我切换的时候会闪过这个 Antigravity Starter Quota
> 字样，然后再显示成查询出来的结果，这原本就是不该出现的
>
> ("…you have made 'Antigravity Starter Quota' the default again … I would
> rather you showed nothing at all, just the email … including /accounts: when
> I switch it flashes this 'Antigravity Starter Quota' and then the queried
> result. That should never have appeared.")

Agreed — the placeholder is gone. **There is no default tier string in the
clone any more**: the session starts with *no* tier and shows the email alone
until the server names one.

| Place | Before | Now |
|---|---|---|
| the banner (`email (tier)`) | `navigaid@gmail.com (Antigravity Starter Quota)` until the answer lands | `navigaid@gmail.com` |
| the `/help` panel | the same placeholder | the email alone |
| the `/accounts` row | `-` (the placeholder was never rendered as a tier) | `-` |
| a switch | the placeholder appeared, then the real tier | the email alone, then the real tier |

The reference's own default string is still the *server's* value for a free
account (`paidTier.name == "Antigravity Starter Quota"`, see
`internal/auth/plan_tier.go`), so it can still show up — but only when the
server actually names it. What is gone is the clone *inventing* it.

The banner and the `/help` panel already rendered the email alone for an empty
tier (`renderBanner`, `tuilib.RenderHelpGeneralMetadata`), so the change was
to stop substituting the placeholder: `resolveRunModeDefaults` no longer
fills it in, `main_dispatch.go` starts the banner with an empty tier, and
`forgetSessionTier` empties it on a switch or a sign-in.

## 4e. The percentages are right-aligned

> 还有 Accounts 列表涉及 % 的栏我希望都靠右对齐，而不是靠左，比如 weekly/5 hour quota
>
> ("…in the accounts list I want the % columns right-aligned instead of
> left-aligned — the weekly and the five-hour quota…")

The two quota columns are percentages, so they are now rendered
**right-aligned in a fixed six-character column** (`internal/accounts/format.go`
— `QuotaColumnWidth`, `Account.WeeklyPercent`, `Account.FiveHourPercent`,
`Account.QuotaColumns`, `Account.QuotaCell`). "100.0 %" is the widest value a
quota can produce, so six characters hold every one of them; a dash (no
bucket in that window) is padded like a number, so the column keeps its shape.

The same helper renders the three account lists, so they agree:

```
navigaid@gmail.com            Google AI Pro        wk   1.0%  5h  96.0%   now
naivgaid@gmail.com            Google AI Plus       wk 100.0%  5h      -   now
btwiuse@gmail.com             Google AI Pro        wk  18.4%  5h  99.2%   now
libredot@gmail.com            Google AI Pro        wk 100.0%  5h 100.0%   now
stakingminer@gmail.com        -                    wk      -  5h      -   -
```

The print mode's `/accounts` table was tab-separated, where a right-aligned
column is not expressible; it now uses the same fixed-width layout as
`agy accounts list`. The JSON envelope still carries the **raw** values
(`"45.2%"`), so nothing but the human-readable table changed.

## 5. No second query: the session's quota cache

> …so you don't have to query again in the `/usage` page.

The `/usage` panel now **reuses** the snapshot the `/accounts` panel already
fetched. `runMode.quotaCache` holds `email → {summary, fetchedAt}`; the
`/usage` fetch reuses an entry that is younger than `quotaCacheFresh`
(2 minutes) and queries the server otherwise.

【一手】 A/B, same binary, same account, two runs:

| Run | 1–2 s after `enter` on `/usage` | 10 s after |
|---|---|---|
| cold cache (fresh session) | the stub's frozen values (`100.00%`) — the request is still in flight | `18.38%` / `99.15%` (the real values) |
| warm cache (after `/accounts`) | `18.38%` / `99.15%` — **already there** | the same |

The warm run shows the live numbers within a second, with no request: the
numbers are the ones the `/accounts` panel fetched a moment earlier, and they
are identical to the ones a cold fetch produces.

## 6. The panel layout

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

- The **age** column (`now`, `7m`, `3h`) says how old the cached number is, so
  a stale quota is not read as a live one.
- The **status** line counts the queue and disappears when it is empty.
- A failed fetch prints `quota: <error>` under the rows; the row itself keeps
  its cached numbers.
- The **tier** column renders the *cached* tier. An account without a tier
  shows `-`; the session's own tier is only used for the account it was read
  for (§4c).
- The two percentages are **right-aligned** (§4e), so the numbers line up
  down the column.

## 7. The other surfaces

| Surface | Change |
|---|---|
| `agy accounts list` | the tier and the two quota columns (from the cache — the command never queries the server), the percentages right-aligned (§4e) |
| print mode `/accounts` | the same fixed-width table as `agy accounts list`; the JSON envelope carries `weekly`, `five_hour`, `quota_fetched_at` with the **raw** values |
| the registry file | `Account.Quota` is new; the file stays `0600` and atomic |

## 8. Measurements

【一手】 this machine, 2026-10-23, the four real accounts:

| Call | Elapsed |
|---|---|
| OAuth refresh of an expired account token (`RefreshQuiet`) | **9.16 s** |
| `retrieveUserQuotaSummary` (after a refresh) | **6.87 s** |

The first implementation gave the whole fetch a **20 s** budget, which turned
a slow-but-successful fetch into `context deadline exceeded` for one of the
four accounts (seen live). The budget is now **45 s**: the two round trips
plus headroom. The accounts are still fetched one at a time, so the panel
takes about a minute to refresh four accounts on this network — it shows the
cached numbers meanwhile and updates them as they land.

The registry writes are now serialized by a package-level mutex in
`internal/accounts` (`Load`, `Save`, `Register`, `Activate`, `Remove`,
`Update`): the quota fetch runs in its own goroutine, and a concurrent write
(a switch, a sign-in, the CLI) must not be lost.

---

## 硬约束

- The tier is queried for the **active account only**, as asked. An account
  that has never been active shows `-` until it becomes active once. The `r`
  key refreshes the *quota* of every account and the *tier* of the active
  account — the tier of an inactive account is not queried (§4b).
- The tier belongs to an **account**: it is read for one account, cached
  under that account, and rendered only against that account. A sign-in, a
  switch or an outside change of the credential forgets the previous
  account's tier (§4c).
- **There is no default tier string.** Until the server names one, the banner
  and the `/help` panel show the email alone and the `/accounts` row shows
  `-` (§4d).
- The quota of an inactive account is queried with that account's own token.
  If the token is expired, it is refreshed **quietly** — the shared
  credential is neither written nor cleared (§4). This is the only path that
  may touch a non-active account's credential.
- The panel's quota numbers are the **lowest remaining fraction across the
  model groups**, not per group. The per-group detail stays in `/usage`.
- A snapshot is reused by `/usage` for **2 minutes**. After that the panel
  queries the server again.
- `/accounts` remains **invocable but not listed**: the picker's catalogue
  entry count stays byte-pinned to the reference's 99.

## 存疑

- 【推测】 The 2-minute reuse window is a judgement call, not a measured
  value. The quota moves in hours (the weekly window creeps, the five-hour
  window creeps); 2 minutes is short enough to be current and long enough to
  cover the "open `/accounts`, then `/usage`" flow. A different window would
  change how often the server is asked, not what is shown.
- 【推测】 "Lowest across the groups" is a summary, not a reference
  behaviour: the reference has no multi-account panel to match. The
  alternative — one column per group — does not fit a list row.
- 【推测】 The 45 s budget is generous. The two measured calls took 16 s
  together on this network; a faster network needs far less. A *too* long
  budget is only visible as a slow error report.
- The tier names ("Google AI Pro", "Google AI Plus", "Antigravity Starter
  Quota") are the server's own `paidTier.name` values — 【一手】 for the two
  this machine returns, 【推测】 for the rest of the catalogue.
- 【存疑】 **`libredot@gmail.com` read "Google AI Plus" earlier in the day
  and reads "Google AI Pro" now**, with its *own* credential, three times in
  a row. The registry's token for that account is its own (the `id_token`
  claim says `libredot@gmail.com`, and no other account shares its
  refresh-token fingerprint), so the earlier reading is not a mis-filed
  credential. Either the account's plan changed, or the server's
  `loadCodeAssist` answer depends on something that changed (an experiment
  assignment). Not resolved.
- The **tier has no age column**, unlike the quota. A tier cached weeks ago is
  rendered like a fresh one. The tier is a plan attribute that changes rarely,
  so this is a minor gap — a timestamp on the cached tier would make a stale
  one visible, exactly as the quota's age column does. Not done in this cycle.


## limitations

- The account's **quota is not the same thing as the tier's entitlement**.
  A "Google AI Plus" account on this machine reports a *weekly* bucket only
  (100 % remaining); the tier says nothing about the five-hour window
  because there is none.
- The clone's `/usage` panel renders the two groups it receives; the
  `/accounts` panel reduces them to two numbers. Nothing was lost — the
  `/usage` document still has the per-group bars.
- The quota fetch is **serialized** on purpose. Four accounts take about a
  minute on this network; parallelizing would be faster and would hit the
  OAuth endpoint with four refreshes at once.
- The reference (`agy`) has no account registry, no `/accounts` panel and no
  per-account quota. Everything in this cycle is a clone-only superset, and
  none of it can be matched byte for byte against the reference.
- The live verification used **this machine's real accounts**. The numbers
  in the captures are the user's own quota and change continuously; the
  *shape* of the response is what the cycle pins.

# Cycle 368 deliverable: the virtual unified pool account

**Cycle:** `cycle-368-unified-pool-aggregate`
**Subject:** the **aggregate** mode — the account pool's quota answered as *one*
virtual account's, instead of the single account a turn happened to run on.
**Implementation:** `a1e257f2` (*feat(proxy): implement virtual unified pool
account aggregation mode*, 2026-10-30). This cycle is the **documentation** of
that mode: the guide had four strategies, the mode had none.
**Evidence:** 【一手】 first-hand against this machine's own state (the proxy
admin commands, the doctor's own output, the upstream captures) · 【推测】 hypothesis

---

## 0. The short answer

| # | what | the answer |
|---|---|---|
| 1 | the mode | `accountpool.StrategyAggregate` — the **fifth** strategy — plus the proxy's three interceptions |
| 2 | the routing half | identical to `quota_aware`: the account with the most quota left |
| 3 | the difference | the **presentation**: the pool's quota and tier are answered as one account's |
| 4 | where it is visible | the vendor `agy` behind `pigasus agy` — the interception lives in the *proxy process* |
| 5 | where it is **not** visible | the clone's own TUI, print mode and Web UI — they read the *active* account's real quota |
| 6 | the merge | `remainingFraction` is the **average** over the pool, not the sum; an account in cooldown contributes 0 |
| 7 | how to turn it on | four ways: the admin command, the policy's `strategy`, the environment variable, the launch flag |
| 8 | the coupling | `aggregate` **implies** `balance` — the pool has to move a turn before the merged numbers mean anything |

---

## 1. What was missing

The mode was implemented and tested in cycle `a1e257f2` and **never
documented**. A search across `docs/` for `aggregate` returned two unrelated
hits (a "multi-cascade sinks" sentence and a "does not aggregate" note). The
consequences:

- `docs/guide/accounts.md` listed **four** strategies — `sticky`,
  `round_robin`, `quota_aware`, `failover` — while
  `accountpool.Strategies()` returned **five**. The fifth was reachable but
  unnamed.
- `internal/cliapp/slash_panel_pool.go` documented the same four in its
  header comment, although the `/pool` panel's `←`/`→` walk uses
  `accountpool.Strategies()` and therefore *did* reach `aggregate`.
- `docs/guide/public-packages.md` called `accountpool` "the config shape, the
  four strategies, …" — the public surface's own description was stale.
- `docs/guide/proxy.md` listed `pigasus proxy doctor|log|capture|version`; the
  proxy group had grown `balance` and `aggregate`, and the package's own
  `README.md` did not mention either.

So a user who asked "how do I make the pool look like one account?" had no
answer anywhere in the tree.

---

## 2. What the mode is

### 2.1 The strategy

```go
// accountpool/accountpool.go
const StrategyAggregate Strategy = "aggregate"
```

`Choose` treats it exactly like `quota_aware` — the account with the most
quota left — and changes only the reason string
(`"unified pool: most quota left (X%)"`). The difference is *not* in the
routing; it is in the proxy:

```go
// accountpool/accountpool.go
func (c Config) Normalised() Config {
    …
    if out.Strategy == StrategyAggregate {
        out.Aggregate = true
    }
    …
}
```

A policy whose strategy is `aggregate` is normalised to carry `aggregate: true`
(`Normalised()`), and the proxy's `aggregateActive()` reads the strategy
directly as well — so the flag in the file is a convenience, not the
switch.

The aliases `agg`, `unified`, `super` and `pool_aggregate` all parse onto it
(`ParseStrategy`).

One more thing the proxy does with it: `effectivePolicy()` in
`internal/agyproxy/server.go` rewrites the **effective** strategy to
`aggregate` when the aggregate switch is on and the policy names no strategy
of its own. The persisted file is not touched — the rewrite is the proxy's
own view of the policy for the turn it is about to run.

### 2.2 The switch

`internal/agyproxy/server.go`:

```go
func (s *Server) aggregateActive() bool {
    if s.cfg.Aggregate { return true }
    for _, env := range []string{EnvAggregate, EnvAccountPoolAggregate} { … }
    if p := endpoints.CurrentAccountPool(); p != nil {
        if p.Aggregate || p.Strategy == accountpool.StrategyAggregate {
            return true
        }
    }
    return false
}
```

Four inputs feed it (§4).

---

## 3. The three interceptions

All of them are in `internal/agyproxy/aggregate.go` (422 lines), reached from
`Server.dispatch` in `internal/agyproxy/server.go`:

```go
case strings.HasSuffix(lp, ":retrieveuserquotasummary"), strings.HasSuffix(lp, ":retrieveuserquota"):
    if s.aggregateActive() { s.handleAggregatedQuota(w, r, body) } else { s.passthrough(w, r, body) }
case strings.HasSuffix(lp, ":loadcodeassist"):
    if s.aggregateActive() { s.handleAggregatedCodeAssist(w, r, body) } else { s.passthrough(w, r, body) }
```

### 3.1 The quota merge

`handleAggregatedQuota` collects the pool's quota and answers **one** merged
view instead of the upstream account's own.

**The candidates** are `router.CandidateAccounts(ctx)` — the accounts the
policy's allow-list admits, not the whole registry. An empty candidate set
falls through to the ordinary passthrough.

**The collection** (`collectQuotaSummaries`) runs the accounts concurrently and
takes the first of three sources for each:

1. the test fetcher (`SetQuotaFetcher`), when one is installed;
2. the registry's **cached** snapshot, when it is younger than the policy's
   `refresh_interval`;
3. a live `accounts.FetchQuota` with a **3-second** timeout, falling back to
   the cached snapshot when the read fails.

**The merge** (`mergeQuotaSummaries`) keys a bucket on
`(group displayName, bucketId, window)`. The first account that has a bucket
names it; the other accounts are folded into it.

| the merged field | how it is computed |
|---|---|
| `remainingFraction` | `sum(account fractions) / pool size` — the **average**, capped at 1, floored at **0.01** when any account had quota, rounded to two decimals |
| `disabled` | `true` when no account is active, or the average is zero |
| `resetTime` | the **earliest** reset across the pool, RFC 3339 |
| `description` | `Unified Pool: N/M accounts ready (X% capacity)`, plus the refresh time when the bucket is disabled |
| `description` (top level) | `OpenAGY Unified Pool (N accounts). Automatic load balancing and failover active.` |

Three consequences worth writing down:

- **The average is not the sum.** Six accounts with 60 % each read as 60 %,
  not 360 %. The figure means "how much of the pool is left", not "how much
  allowance exists".
- **An account in cooldown contributes 0** (`router.IsExcluded`) and does not
  count towards the active count — but it *is* in the denominator.
- **An account whose quota could not be read is in the denominator and not in
  the numerator**: a pool of six with one unreachable account reads five
  sixths. `totalPool` is `len(accs)`, the candidate count, not the number of
  accounts that answered.

The answer carries two headers of its own:

```
X-Openagy-Aggregate-Mode: true
X-Openagy-Pool-Size: N
```

**The shape is the upstream's own.** The synthesized body was checked against a
real upstream capture on this machine —
`~/.openagy/captures/1792445064632-009-retrieveUserQuotaSummary.json` 【一手】:

```json
{"groups":[{"buckets":[{"bucketId":"gemini-weekly","displayName":"Weekly Limit Remaining",
  "window":"weekly","resetTime":"2026-10-26T20:56:40Z","remainingFraction":1}],
  "displayName":"Gemini Models","description":"Models within this group: …"}],
 "description":"Within each group, models share a weekly limit…"}
```

which is what `aggregatedQuotaWire` produces. The upstream's top-level
`buckets` member is **not** answered — the upstream capture does not carry it
either, and the Web UI's own bundle reads `response.groups`.

### 3.2 The tier merge

`handleAggregatedCodeAssist` answers `loadCodeAssist` with the pool's
**highest** tier, taken from the registry's *cached* plan tier — not from a
live call:

| the pool holds | the answer's tier |
|---|---|
| an account whose tier contains "ultra" | `Google AI Ultra` (`ultra-tier`) |
| an account whose tier contains "pro" | `Google AI Pro` (`pro-tier`) |
| otherwise | `Antigravity Starter Quota` (`free-tier`) |

The upstream's answer is still fetched (with the routed account's token) and
its body is **upgraded in place**: `paidTier` is set to the pool's tier, and
`currentTier.name` becomes `Antigravity (Google AI Pro)` with
`currentTier.description` = `OpenAGY Unified Pool (N accounts)`.

When the upstream call fails — no token, a non-200, an undecodable body — the
proxy **synthesizes** the whole answer instead of failing the turn:
`currentTier`, `allowedTiers` (`free-tier` + `standard-tier`),
`cloudaicompanionProject`, `gcpManaged` and `paidTier`. The header
`X-Openagy-Aggregate-Tier` carries the tier's name.

### 3.3 The passthrough

Every other RPC goes through the ordinary balanced passthrough, with one
change (`internal/agyproxy/passthrough.go`):

```go
if s.aggregateActive() {
    if cands, err := s.router.CandidateAccounts(r.Context()); err == nil && len(cands) > maxAttempts {
        maxAttempts = len(cands)
    }
}
```

One turn may therefore try **every** candidate account, whatever the policy's
`max_attempts` says. The aggregate view is a statement about the pool's
*allowance*; this is what makes the pool's *throughput* match it.

---

## 4. The four ways to turn it on

| # | how | where it lands |
|---|---|---|
| 1 | `pigasus proxy aggregate on` | `~/.openagy/wrapper.json` → `{"balance":true,"aggregate":true}` |
| 2 | `"strategy": "aggregate"` in `account_pool` | `~/.config/openagy/config.json` → the `account_pool.strategy` member — the proxy reads the strategy itself, so a separate `"aggregate": true` is not required |
| 3 | `OPENAGY_PROXY_AGGREGATE=1` (or `OPENAGY_ACCOUNT_POOL_AGGREGATE=1`) | the process's environment |
| 4 | `pigasus agy --aggregate` | the launcher's own flag, for one run |

The admin command is `internal/agyproxy/admin.go:cmdAggregate`; with no
argument it prints the status, and `on`/`off` (plus `enable`/`disable`/`1`/`0`
/`true`/`false`) write the file. **`on` writes `balance: true` as well** — the
mode needs the balancer, and a pool that cannot move a turn would present a
merged allowance it cannot deliver.

The environment variables are read by `resolveSettings` in
`internal/agyproxy/fileconfig.go`, which also applies the policy's own flag:

```go
if p := endpoints.CurrentAccountPool(); p != nil {
    if p.Aggregate || p.Strategy == accountpool.StrategyAggregate {
        s.Aggregate = true
        s.Balance = true
    }
}
if s.Aggregate { s.Balance = true }
```

The launcher's flag (`internal/agyproxy/launch.go:parseProxyFlags`) accepts
`--aggregate`, `--aggregate=on|off`, `--no-aggregate` and the `-aggregate`
spelling, and it is consumed **before** the vendor binary sees the command
line — the vendor `agy` must not learn a proxy flag.

The status is visible in the doctor:

```
$ pigasus proxy doctor
…
balance     on
aggregate   off
pool        on (strategy: quota_aware, threshold: 5.0%)
```

【一手】 — measured on this machine, with `OPENAGY_PROXY_AGGREGATE=1` the same
doctor prints `aggregate   on`.

---

## 5. The files

| File | What it is |
|---|---|
| `internal/agyproxy/aggregate.go` | **new** — the three interceptions: `handleAggregatedQuota`, `collectQuotaSummaries`, `mergeQuotaSummaries`, `handleAggregatedCodeAssist`, `bestPlanTier`, `writeSynthesizedCodeAssist` (422 lines) |
| `internal/agyproxy/aggregate_test.go` | **new** — the seven tests below (469 lines) |
| `internal/agyproxy/admin.go` | `cmdAggregate` (the admin command) and the doctor's `aggregate` line |
| `internal/agyproxy/ctl.go` | the `aggregate` case in the admin dispatcher and its usage line |
| `internal/agyproxy/fileconfig.go` | `EnvAggregate`, `EnvAccountPoolAggregate`, `fileConfig.Aggregate`, `resolveSettings` |
| `internal/agyproxy/server.go` | `Server.Aggregate`, `aggregateActive()`, `effectivePolicy()`, the dispatch |
| `internal/agyproxy/passthrough.go` | the `maxAttempts` scaling |
| `internal/agyproxy/launch.go` | `parseProxyFlags` — the `--aggregate` flag |
| `internal/features/accounts/routing/routing.go` | `CandidateAccounts` — the pool the merge reads, and `IsExcluded` |
| `accountpool/accountpool.go` | `StrategyAggregate`, the aliases, `Config.Aggregate`, `Normalised()`, the `Choose` case |
| `internal/cliapp/main_dispatch.go` | the `aggregate` name in the hidden `proxy` group |

---

## 6. The tests

`internal/agyproxy/aggregate_test.go`:

| test | what it pins |
|---|---|
| `TestAggregateQuota_Calculation` | two accounts, one with quota: the fraction is **0.50**, the bucket is not disabled, the description names `1/2 accounts ready` and `50% capacity` |
| `TestAggregateQuota_AllExhausted` | both accounts spent: the fraction is `0.0`, the bucket **is** disabled, the description names `0/2 accounts ready` |
| `TestAggregateQuota_WithCooldown` | one account in cooldown: the fraction is **0.30**, the bucket is not disabled, the description names `1/2 accounts ready` |
| `TestAggregateCodeAssist_TierResolution` | the tier table of §3.2, case by case |
| `TestAggregateHTTPDispatch_Intercept` | the dispatch reaches the merge: 200, `X-Openagy-Aggregate-Mode: true`, the top-level description says `Unified Pool` |
| `TestAggregateHTTPDispatch_CodeAssist` | the same for `loadCodeAssist`, plus `X-Openagy-Aggregate-Tier: Google AI Pro` and the upgraded `paidTier` |
| `TestAggregateAdminCommand` | `proxy aggregate on` prints `aggregate: on`, the file keeps it, `off` prints `aggregate: off` |

`go test ./cmd/... ./internal/...` is green at this commit.

---

## 7. What the mode does **not** do

- **It does not touch the clone's own quota surfaces.** The interception runs
  in the *proxy process*, so it is the vendor `agy` behind `pigasus agy` that
  sees the merged view. The clone's TUI (`internal/cliapp`), print mode and
  Web UI (`internal/remotecontrol/dispatch_quota.go`) read the **active**
  account's own quota, through `internal/auth` — the real number, for one
  account.
- **It does not merge the accounts' identities.** The turn still runs on one
  account, with that account's credential; the merge is a presentation of the
  pool's allowance, not a credential.
- **It does not make the pool's *allowance* additive.** A sixth account adds a
  sixth of the average, not a sixth of the total.

---

## 硬约束 / Hard constraints

- **The interception is the proxy's, not the clone's.** Only `pigasus agy` —
  the vendor binary launched with `CLOUD_CODE_URL` pointing at
  `internal/agyproxy` — travels through the interceptions. Every other
  surface reads the real quota of the account it is on.
- **The merged fraction is an average, not a sum.** `sumFraction / totalPool`,
  with `totalPool` = the **candidate** count. A pool of six with one
  unreachable account reads five sixths, not one sixth and not six.
- **An account in cooldown contributes zero and does not count as active**, but
  it is still in the denominator.
- **The floor is 0.01, not 0.** A bucket whose average would round to zero
  reads `0.01` while any account has quota; it is `disabled` only when no
  account is active or the average is exactly zero.
- **`aggregate` implies `balance`, in every direction.** The admin command
  writes both flags; `resolveSettings` sets both; `startProxyServer` sets
  `balance = true` when the aggregate flag is on. A pool that cannot move a
  turn would advertise an allowance it cannot deliver.
- **The strategy's routing is `quota_aware`'s.** `Choose` returns the account
  with the most quota left, and only the *reason* string differs
  (`unified pool: most quota left (X%)`).
- **The balance implication lives in the proxy, not in the policy.** The
  policy's `Normalised()` only sets `Aggregate`; it is `resolveSettings` and
  `balanceEnabled` in `internal/agyproxy` that turn the balancer on when the
  policy names the aggregate strategy. The admin command writes both flags
  itself.
- **The answer's shape is the upstream's own.** The synthesized body was
  checked against a real upstream capture; the member names
  (`groups`/`buckets`/`displayName`/`bucketId`/`window`/`remainingFraction`/
  `resetTime`/`description`) are the upstream's.
- **The vendor binary must not see the flag.** `--aggregate` is consumed by
  `parseProxyFlags` before the arguments are handed to `agy`, and everything
  after a bare `--` is left alone.
- **The persisted names do not move.** `~/.openagy/wrapper.json` keeps its
  `balance` / `aggregate` keys, and the `OPENAGY_*` variables keep their
  names — a rename would silently reset an installed proxy's settings.

## Open questions / 存疑

- **The `retrieveUserQuota` branch is unverified.** The interception matches
  `:retrieveuserquotasummary` **and** `:retrieveuserquota`, and answers both
  with the same body. Every upstream capture on this machine shows
  `retrieveUserQuotaSummary`; none shows the shorter `retrieveUserQuota`, so
  the second branch's shape is the clone's own guess. 【推测】
- **Whether the upstream's `buckets` member is needed is not established.**
  The upstream capture carries `groups` and a top-level `description`; the
  upstream's own `buckets` (the *ungrouped* answer) is not answered by the
  synthesized body either.
- **The `paidTier` upgrade may not be what the reference does.** The mode
  presents the pool's highest tier as the session's tier; the reference has
  one credential and one tier, so there is no byte-for-byte counterpart to
  compare against. What the vendor CLI *renders* from the upgraded body was
  not observed in a screenshot. 【推测】
- **The tier is the registry's cached one.** `bestPlanTier` reads
  `Account.PlanTier`, which the registry refreshes on its own schedule; a
  freshly upgraded account can therefore be read at its old tier until the
  cache catches up.
- **No live six-account run was measured.** The merge was exercised by the
  unit tests and by reading the code; the machine's own pool has six
  `Google AI Pro` accounts, so the *tier* merge's `ultra` branch was not
  exercised against this pool.

## Limitations

- **The documentation is the deliverable; the implementation was not changed.**
  The mode's code is `a1e257f2`'s, unmodified by this cycle. The files this
  cycle touched are the two guides, the package's `README.md`, the panel's
  header comment and this report.
- **The aggregate view was not observed in the vendor CLI's own UI.** The
  interceptions were verified at the HTTP level (the headers, the status, the
  body) and by the unit tests; the vendor TUI's rendering of the merged
  numbers was not screenshotted.
- **The upstream capture used for the shape check is one file** from
  2026-10-23, for an account on a different plan than this machine's pool.
  The `description` text the upstream carries is per-account and per-plan; the
  synthesized one replaces it.
- **The `retrieveUserQuota` interception is unmeasured** (see *Open
  questions*), and no upstream call was observed that would exercise it.
- **The pool's accounts are the clone's.** The reference `agy` has exactly one
  credential and no pool, so the whole mode — like the pool itself — has no
  byte-for-byte counterpart in the reference.

---

Report language note: this file is English because `docs/cycles/*` is English.
The evidence files under `out/` are Chinese, per the repository's convention.

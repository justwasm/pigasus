# Cycle 360 deliverable: quota-aware account switching, as a policy in `config.json`

**Cycle:** `cycle-360-account-pool-routing`
**Subject:** the clone keeps a *registry* of Google accounts
(`internal/accounts`, `~/.config/openagy/accounts.json`) and lets the user
pick the active one by hand. This cycle adds the automatic half: a **policy**
that says which account a turn runs on, how the pool rotates, and when a turn
moves — driven by each account's remaining quota.
**Evidence:** 【一手】 first-hand against the clone (the shipped binaries, the
hermetic test suites, the live configuration file) · 【推测】 hypothesis where
the reference has no counterpart

---

## 0. The short answer

| | |
|---|---|
| where the policy lives | `~/.config/openagy/config.json`, section `"account_pool"` — **not** `providers.json` |
| who reads the file | `endpoints` (still the single reader of `config.json`) |
| the policy's shape | `accountpool` — a **public** package, no `internal/` dependency |
| the runtime | `internal/accountrouting` — the registry, the quota service, the credential, the failover wrapper |
| strategies | `sticky` (default), `round_robin`, `quota_aware`, `failover` |
| the trigger | one threshold — the fraction of a quota window an account must still have |
| the decision | **per turn**, through the transport's token function — not per session |
| the shared credential | **not** rewritten: the routed account is this process's account, the user's active account stays what `agy` and `/accounts` show |
| the default path | **unchanged**: with no policy in the file, the transport keeps its own credential source |
| tests | 15 hermetic router/wrapper tests + the policy package's own tests — all green |

Two new packages, one changed dispatch line, one changed configuration
struct:

| file | what |
|---|---|
| `accountpool/accountpool.go` | the policy: the `Config` shape, the four strategies, the threshold, `Choose` — **pure**, no I/O |
| `accountpool/accountpool_test.go` | the policy's own tests (the strategies, the threshold, the fallback, the normalisation) |
| `internal/accountrouting/accountrouting.go` | the runtime: the router, the quota cache, the credential lookup, the failover wrapper |
| `internal/accountrouting/accountrouting_test.go` | 15 hermetic tests — no keychain, no network, no machine state |
| `endpoints/config.go` | `Config.AccountPool` + `CurrentAccountPool()` — the section's reader |
| `internal/backendselect/backendselect.go` | installs the token function and the wrapper — **only** when the policy is on |

---

## 1. What was asked

The request was, in short: an automatic account-switching / load-balancing
mode over the account pool, with **several strategies** and **trigger
thresholds**, driven by the accounts' remaining quota, configured from a file
the clone already reads.

Two constraints shaped the design:

1. **The policy belongs in `config.json`, not `providers.json`.**
   `providers.json` answers *"which model talks to which provider"* — it is
   provider-centric, and `internal/models.Editor` is its only writer. The
   account pool is a different question: *"which of my accounts serves this
   turn"*. `config.json` already answers *"which service does the clone
   itself talk to"*, and it is the file with a section per concern. The pool
   policy is a second section of that file.

2. **No TUI entry for now.** The section is read and honoured; nothing new
   appears in a panel, and the catalogue's command count is untouched.

## 2. The section

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

| field | default | what it does |
|---|---|---|
| `enabled` | `false` | turns the routing on. Off by default: the routing changes *which credential a turn runs on*, so it is the user's explicit choice |
| `strategy` | `sticky` | which account a turn picks — see §3 |
| `threshold` | `0` | the eligibility floor, 0..1 |
| `refresh_interval` | `1m` | how often a quota figure is read again |
| `cooldown` | `5m` | how long an account stays out after the server said its quota is spent |
| `accounts` | every account | the allow-list — the pool is these accounts and no others |
| `max_attempts` | `1` | how many accounts **one** turn may try after a quota error |
| `switch_on_quota_error` | `true` | whether a quota error moves the *next* turn to another account |

Every field has a default, so an empty section is a valid one — it describes
a pool that is switched off. A malformed value never keeps the policy from
being used: an unparsable duration falls back to the default, an unknown
strategy to `sticky`, a threshold outside 0..1 is clamped into it, a negative
attempt count becomes one.

### 2.1 The reader

`endpoints` stays the **single** reader of `config.json` — that is the
invariant the file's design rests on, and the section does not break it:

```go
type Config struct {
    Endpoints  map[string]string  `json:"endpoints,omitempty"`
    AccountPool *accountpool.Config `json:"account_pool,omitempty"`
}
```

`endpoints.CurrentAccountPool()` returns the section (or `nil`), re-reading
the file only when its stamp changed — the same stamp the endpoint overrides
use, so a policy the user edits mid-session is picked up on the next read.
The returned value is a **copy**: the caller may normalise it without
touching the cached one.

`accountpool` is a **public** package: it depends on nothing but the standard
library, so it can be imported by anything.

    $ go list -deps ./accountpool | grep pigasus
    github.com/justwasm/pigasus/accountpool

## 3. The strategies

| strategy | what it does |
|---|---|
| `sticky` | the account the session is signed in with, until its quota drops below the threshold |
| `round_robin` | the next account of the pool, in turn |
| `quota_aware` | the account with the most quota left |
| `failover` | the account the session is signed in with, until a quota error comes back |

The difference between `sticky` and `failover` is *what moves the turn*: the
threshold moves it in `sticky`, the server's quota error moves it in
`failover`. Both keep the session's account until something happens to it.

### 3.1 The threshold is a preference, not a wall

One number — the fraction of a quota window an account must still have — is
the eligibility floor. When **every** account in the pool is below it, the one
with the most left is used anyway: a turn with no account cannot be served at
all, so the floor yields rather than the turn fails.

Three rules make the figure well-defined:

- **The figure is the lowest bucket.** A quota summary has one group per
  family and one bucket per window (weekly, five-hour). The *lowest* remaining
  fraction across all of them is the one that decides whether the account can
  serve a turn right now — the five-hour window runs out long before the
  weekly one.
- **An unknown figure is not a zero.** An account whose quota has never been
  read is *eligible* but ranks last. A pool does not punish an account for
  not having been asked.
- **In `failover` the threshold does not apply.** The threshold is the
  `sticky` strategy's trigger; `failover` is triggered by the server.

### 3.2 The tie-breaks

`quota_aware` picks the account with the most left. A tie goes to the account
that served a turn **longest ago**, so a tie does not keep hammering the same
account. `round_robin` rotates over the *eligible* list; the cursor is the
index of the chosen account, so the next decision starts after it.

## 4. The runtime

### 4.1 The decision is per turn

The transport asks the clone for its credential **once per request** — the
CloudCode client holds a `TokenFunc`, and the geminicode client calls it
before every `Open`. The router is installed as that token function, so the
routing decision is taken *per turn*, not once per session: a pool that
rotates rotates between two sentences of the same answer.

    cmd/pigasus ──▶ internal/agent.Loop / internal/pigrt
                          │
                          ▼
                 internal/backendselect   ← the only place a transport is built
                          │  installs the token function + the wrapper
                          ▼
                 internal/accountrouting.Router
                          │  Decide → accountpool.Choose
                          ▼
                 internal/backend.CloudCodeClient ──▶ CloudCode

### 4.2 The quota figure is read in the background

A turn is never held up by a quota lookup. The figure comes from
`retrieveUserQuotaSummary` — the same call the `/usage` panel makes, with
**that account's own credential** — and it is read:

- from the registry's own snapshot first (`internal/accounts`' cached quota),
  which costs nothing and which the `/accounts` panel keeps fresh;
- from the server in the background when the figure at hand is stale
  (`refresh_interval`), one read per account at a time.

Until the first figure arrives, the account is eligible and ranks last.

### 4.3 A quota error moves the pool

The wrapper around the transport (`accountrouting.Wrap`) watches for the one
failure the pool acts on: `backend.IsQuotaReachedErr` — the 429 the reference
reports as *"Individual quota reached"*.

- The account is put out of the pool for the policy's `cooldown`.
- The turn is tried again on another account, up to `max_attempts`.
- With `max_attempts: 1` (the default) the turn fails as it did before, and
  the **next** turn goes to another account.

A quota error that arrives **mid-turn** (after the turn has produced output)
cannot be retried — the turn already said something — but it still puts the
account out of the pool, so the next turn goes elsewhere.

Any other failure is the transport's own: the runtime's retry decides.

## 5. The credential rules

These are the load-bearing parts. Getting them wrong is silent: a turn that
runs as the wrong account still *works*.

- **The shared credential is not rewritten.** The routed account is *this
  process's* account. The keychain entry and the three on-disk copies keep
  pointing at the account the user picked, so the installed `agy` and the
  `/accounts` panel keep showing what the user expects. A load balancer that
  rewrote the keychain would change the identity of every other program on
  the machine.
- **An inactive account is refreshed quietly.** `RefreshIfNeeded` *writes* the
  shared credential (that would switch the active account) and *clears* it on
  a rejection (that would sign the user out). `RefreshQuiet` does neither —
  it is the only refresh an inactive account may use.
- **The active account's credential is the shared one**, read through the
  router's own cache — the macOS keychain entry wins over the on-disk copies,
  exactly as the transport's own token source prefers it.
- **The pool never leaves the session without an account.** When the pool is
  empty, the shared credential answers.

## 6. The default path is untouched

The routing is installed **only when the policy is on when the transport is
built**:

```go
router := accountrouting.Shared()
if !accountrouting.Enabled() {
    return backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{Model: slug})
}
client := backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{
    Model:     slug,
    TokenFunc: router.TokenFunc,
})
return accountrouting.Wrap(client, logWriter)
```

So a clone with no `account_pool` section behaves exactly as it did before
this cycle: the same token source, the same transport type, the same bytes on
the wire. The wrapper re-reads the policy per turn, so a policy the user
turns **off** mid-session falls back to the plain transport without a
restart; a policy turned **on** mid-session needs the transport to be rebuilt
(the TUI rebuilds it when the model changes; the print path builds it per
run).

The wrapper is transparent: `accountrouting.Unwrap` hands back the transport
behind it, for the callers that need the concrete type.

## 7. The tests

| suite | what it pins |
|---|---|
| `accountpool` | the four strategies, the threshold's fallback, the allow-list, the normalisation (a bad duration, a bad strategy, an out-of-range threshold), the tie-breaks |
| `internal/accountrouting` | the decision per strategy, the threshold skip, the rotation, the off/disabled paths, the credential for the routed *and* the active account, the cooldown, the lowest-bucket figure, the failover retry, the give-up, the no-retry case, the pass-through, `Unwrap` |
| `endpoints` | the section is read, and re-read when the file changes |
| `internal/cliapp` | the transport type assertion, now unwrap-tolerant |

The router tests are **hermetic**: the registry and the configuration file
are temporary files (`OPENAGY_ACCOUNTS_CONFIG`, `OPENAGY_CONFIG`), the
credentials are crafted with a one-hour expiry so nothing refreshes, and the
shared-credential read is an injectable seam (`Router.loadShared`) so a test
never reaches for the machine's keychain.

## 硬约束 / 存疑 / limitations

- **硬约束**: the policy lives in `config.json` under `"account_pool"`, and
  `endpoints` remains the file's **only** reader.
- **硬约束**: the shared credential is **never** rewritten by the routing. The
  routed account is this process's account; the user's active account is
  untouched.
- **硬约束**: an inactive account's credential is refreshed with
  `RefreshQuiet` only. `RefreshIfNeeded` would switch the active account, and
  its rejection path would sign the user out.
- **硬约束**: a quota lookup never blocks a turn. The figure is read in the
  background, and an unknown figure ranks an account last instead of
  excluding it.
- **硬约束**: the routing changes *which credential a turn runs on*, so it is
  **off** unless the configuration file says otherwise.
- **硬约束**: the threshold is a preference. When every account is below it,
  the one with the most left is used — a turn with no account cannot be
  served.
- **硬约束**: the quota figure is the **lowest** bucket across the account's
  windows, not the weekly one alone.
- **硬约束**: no TUI entry. The panel surface and the catalogue's command
  count are unchanged.
- **存疑**: `max_attempts` above 1 replays a turn's request on another
  account. A quota error arrives before the model produced anything, so the
  replay is safe *in the case the wrapper recognises*; a quota error that
  arrives late is not retried, only recorded. The boundary is the server's
  answer, not the clone's guess.
- **存疑**: the cooldown is a local belief. The clone decides an account is
  out of quota and keeps that belief for `cooldown`; the server's own answer
  is the truth, and a quota that comes back earlier than the cooldown stays
  out until the cooldown passes.
- **存疑**: the strategy names are the clone's own. The reference has exactly
  one credential and no pool, so there is no byte-for-byte counterpart for
  any of this — the names are a clone-side convention.
- **limitations**: the routing applies to the clone's **own** transport
  dispatch (`internal/backendselect`) — the TUI, the print mode, the remote
  control, the language server and the harness's own dispatch. The SDK-facing
  harness path that builds a client from the *SDK's* configuration keeps the
  SDK's credential: an explicit `access_token` is the SDK's business.
- **limitations**: there is no panel for the policy and no command that
  prints the pool's state. The routing's log lines go to the transport's log
  writer, and `OPENAGY_ACCOUNT_POOL_DEBUG=1` adds them to stderr.
- **limitations**: a policy turned **on** mid-session needs the transport to
  be rebuilt (the TUI does that when the model changes; the print path
  builds it per run). Turning it off takes effect on the next turn.
- **limitations**: the quota figure is read per account with that account's
  own credential, so a pool of N accounts costs N quota reads per
  `refresh_interval`. The read is serialised per account and bounded by a
  20-second budget.

---

Reports: [`cycle-351-slash-command-alignment`](../cycle-351-slash-command-alignment/deliverable.md)
(the previous cycle in this checkout).

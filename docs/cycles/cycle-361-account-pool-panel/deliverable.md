# Cycle 361 deliverable: the account pool gets a panel, and the policy takes effect live

**Cycle:** `cycle-361-account-pool-panel`
**Subject:** cycle 360 put the account-routing policy in `~/.config/openagy/config.json`
and left two gaps: the policy had **no front-end** (a file the user edits by
hand is a policy they forget about), and turning it **on** mid-session needed
the transport rebuilt. This cycle closes both.
**Evidence:** 【一手】 first-hand against the clone (the shipped binaries, the
hermetic test suites, the live configuration file) · 【推测】 where the
reference has no counterpart — it has one credential and no pool, so **none**
of this has a byte-for-byte counterpart.

---

## 0. The short answer

| | |
|---|---|
| what was added | the **Pool tab** of `/accounts`, and the `/pool` command that opens it |
| the tab bar | the `/resume` picker's own — bold + underlined + blue for the active tab, separator grey for the other, *"(tab to cycle)"* at the end |
| what it edits | `enabled`, `strategy`, `threshold`, `refresh_interval`, `cooldown`, `max_attempts`, `switch_on_quota_error`, and the `accounts` allow-list |
| how a change is applied | **at once** — the panel writes the section on every committed change, and the routing reads the policy **per turn** |
| who writes the file | `endpoints.SaveAccountPool` — the file's **only** writer, the same package that is its only reader |
| what was removed | the "turning the policy on needs the transport rebuilt" limitation |
| how | the router is installed **always**; with the policy off it answers with `backend.DefaultTokenSource()` — the transport's own credential source, not a copy of it |
| the catalogue | 108 → **109** rows: the clone's fourth superset command (`/pool`) |
| tests | 4 writer tests (`endpoints`) · 8 panel tests + 1 router test (`internal/cliapp`, `internal/backendselect`) · 1 router test (`internal/accountrouting`) — all green |

Six files, one of them new:

| file | what |
|---|---|
| `endpoints/config.go` | `SaveAccountPool` — the writer: a targeted `sjson` edit on the raw document, `pretty.Pretty`, `atomicfile.WriteFile` at 0600, and the cached snapshot dropped |
| `endpoints/config_test.go` | four tests: the write is seen at once, the rest of the document survives, a nil policy removes the section, a malformed document is refused |
| `internal/backend/cloudcode.go` | `DefaultTokenSource()` — the transport's own token source, exported so the router can answer with *exactly* it |
| `internal/accountrouting/accountrouting.go` | the router's `fallback` — the off-policy and empty-pool paths answer with that source |
| `internal/backendselect/backendselect.go` | the router is installed **always**, not only when the policy is on |
| `internal/cliapp/slash_panel_pool.go` | **new** — the Pool tab: the rows, the keys, the writes |
| `internal/cliapp/slash_panel_accounts.go` | the tab bar, the tab switch, the account list's own half |
| `internal/cliapp/slash_panels.go` | `/pool` opens the panel on the Pool tab |
| `internal/cliapp/slash_panel_routing.go` | `commandPanels["pool"]`, the Pool tab's row count |
| `internal/tuilib/commands.go` | the `/pool` row in the catalogue |

---

## 1. What was asked

Two things, in the user's own words:

> 那你现在解决这两个问题，配置文件不需要 watch，只要能通过重启生效我就满足了
> 但是现在给 tui 加上一个 slash command 来配置，或者放在 /accounts 菜单下面，通过 tab 切换到这个配置的分页，参考 /resume 的设计，从这里配置应该立马生效而无需重启

Translated:

1. **Fix the two limits** cycle 360 reported. The configuration file does not
   need to be watched — a restart is enough for a file edit.
2. **Give the TUI a slash command to configure the policy**, or put it under
   `/accounts` as a **tab** switched with Tab, **modelled on `/resume`**. A
   change made there must **take effect immediately, without a restart**.

The two halves of the request are one design. A panel that writes the file is
only "immediate" if the transport already carries the router; and a router
that is only installed when the policy is on cannot be turned on from a panel
without a restart. So the router became permanent, and the panel became the
policy's front-end.

---

## 2. The writer

`config.json` had a reader and no writer. Cycle 360 noted that as a limit; a
file with a reader and no writer is a file only a text editor can change.

The writer went into **`endpoints`**, not into `accountpool`:

- `accountpool` is a **public** package with no I/O and no `internal/`
  dependency. That is what makes it importable on its own, and it is the
  property the previous cycle's report leaned on. A file writer would have
  cost that.
- `endpoints` is the file's **only** reader. Giving it the writer keeps the
  file with one reader and one writer, which is the rule the rest of the
  repository already follows (`providers.json` has `internal/models.Editor` as
  its only writer).

### The write is a targeted edit, not a re-marshal

```go
updated, err = sjson.SetRawBytes(document, accountPoolKey, encoded)
```

The edit is made on the **raw** document, so:

- the **key order** survives — `endpoints` stays where the user put it;
- a section **a newer version of the clone wrote** survives with it, which a
  `map[string]json.RawMessage` round-trip would not guarantee;
- the rest of the document is not re-encoded at all.

The result is formatted with `pretty.Pretty` and written with
`atomicfile.WriteFile(path, buf, 0o600)` — the same atomic write the provider
editor uses.

### A malformed document is refused, not replaced

The writer parses the file first (`gjson.ValidBytes`). A document it cannot
read is an **error**: the writer never overwrites a file it could not parse.
That is the same rule the endpoints reader follows — a broken file is not a
reason to take the clone down.

### The cached snapshot is dropped

`CurrentAccountPool()` caches the parsed policy under the file's stamp (path,
size, mtime). A write changes the stamp, so the next read would re-read
anyway — but the writer drops the snapshot explicitly
(`accountPoolCache.Store(nil)`), so the change is seen at once and does not
depend on the filesystem's timestamp resolution.

---

## 3. The router is installed always

Cycle 360's limitation was:

> a policy turned **on** mid-session needs the transport to be rebuilt (the TUI
> does that when the model changes; the print path builds it per run). Turning
> it off takes effect on the next turn.

The reason was that `newOfficialBackend` installed the router only when
`accountrouting.Enabled()` was true **at build time**:

```go
if !accountrouting.Enabled() {
    return backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{Model: slug})
}
```

### The off-path must answer with the transport's own credential

Installing the router always means the router now answers *every* request —
including the ones made with no policy in the file. If the router carried its
own copy of the credential cascade, the two would drift, and the drift would
show up as "the clone stopped working" for a user who never touched the pool.

So the transport's own token source is now **exported**:

```go
// internal/backend/cloudcode.go
func DefaultTokenSource() func(context.Context) (string, error) {
	return defaultTokenSource()
}
```

and the router holds it as its `fallback`. The off-policy path and the
empty-pool path answer with it. The keychain-preference cascade, the unified
cache and the refresh are **one** implementation, not two.

### The transport installs it, at build time

The router is a process-wide singleton, so it cannot hold a *per-transport*
source. The transport therefore **installs** its own source when it is built:

```go
router := accountrouting.Shared()
router.SetFallback(backend.DefaultTokenSource())
```

That matters for the timing. The source captures two things at creation: the
keychain's preference (`auth.HasKeychainToken()`) and the keychain token
itself. A transport that is **rebuilt** — the TUI does that when the model
changes — installs a **fresh** source, so a sign-in that happened between the
two builds is seen. A router that captured the source once, at first use,
would have kept the stale answer for the rest of the session.

The router's own default (used when it is exercised without a transport, as
in the tests) is the same source, created lazily on the first off-policy
call.

### The source is created once and kept

The source caches the credential in memory. A second cache would be a second
answer to the same question, and the two could disagree.

### What the routed path still uses

The routed path keeps the router's own `sharedToken`: it needs the token
**object**, not just the string, because the decision needs the account's
email and a refresh must not write the shared credential. That is a different
question, so a different answer is right.

### The proof

- `internal/backendselect/backendselect_test.go` — `newOfficialBackend` with
  **no policy in the file** returns a client that is *not* a plain
  `*backend.CloudCodeClient`: the router is in place. `accountrouting.Unwrap`
  hands the CloudCode client back.
- `internal/accountrouting/accountrouting_test.go` — with the routing off, the
  token function answers with the transport's own source. The test stubs the
  fallback, so it never touches the machine's keychain.

---

## 4. The panel

### The tab bar is the `/resume` picker's

The reference has no tabbed panel, so there is nothing to match byte for
byte. The **closest** thing the reference does have is the `/resume` picker's
tab bar, and the request named it as the model:

```go
b.WriteString("   ")
writeTab("Accounts", m.panel.acct.tab != accountsTabPool)
b.WriteString("    ")
writeTab("Pool", m.panel.acct.tab == accountsTabPool)
b.WriteString(" " + cDimGrayOpen + "  (tab to cycle)" + pickerKeyClose)
```

The active tab is `\x1b[1;4m` + blue (bold, underlined), the inactive one is
the separator grey — exactly `renderResumeTabBar`'s shape
(`internal/cliapp/picker_resume.go`).

### The rows

| row | value | how it changes |
|---|---|---|
| Enabled | on / off | `←/→` or `space` |
| Strategy | sticky · round_robin · quota_aware · failover | `←/→` walks the list |
| Threshold | a percentage | `←/→` steps by 5 %; `enter` types a number |
| Refresh interval | a duration | `←/→` walks a ladder (30 s … 1 h); `enter` types one |
| Cooldown | a duration | as above |
| Max attempts | a number | `←/→` by one; `enter` types one |
| Switch on quota error | on / off | `←/→` or `space` |
| Accounts in pool | *N* of *M* | `enter` opens the allow-list submenu |

The threshold reads **both** `25%` and `0.25`: the policy stores a fraction,
but a user thinks in percent.

### The allow-list submenu

`enter` on *Accounts in pool* opens a submenu over the registry's accounts,
with a **`(all accounts)`** row at the top — an empty allow-list is what the
policy reads as "every account", so that row clears the list rather than
leaving it ambiguous.

### A value that does not parse is refused

A threshold of *"soon"* leaves the row open with a notice and writes nothing.
The panel never writes a policy it could not read back.

### The panel owns every key

The Pool tab's `←/→` mean "change this value" and `enter` means "edit this
field", so the keys must never fall through to the generic list navigation.
The `/accounts` panel already owned **every** key
(`internal/cliapp/slash_panel_keymap.go` → `handleAccountsPanelKey`), and the
Pool tab is reached through it — the generic handler is never consulted.

The one thing that changed in the keymap was **nothing**: the accounts case
was already there. The tab's keys are routed from the accounts handler.

### The cursor belongs to the view

`m.panel.cursor` is shared by every panel kind. A tab change resets it: the
Pool tab opens on its first row, the account list on its first account. The
submenu has its own cursor (`allowCursor`).

---

## 5. The immediate effect

The chain, end to end:

1. The panel writes the section — `endpoints.SaveAccountPool`.
2. The file's cached snapshot is dropped.
3. The **next turn** calls the transport's token function — the geminicode
   client calls `c.token(ctx)` before **every** request
   (`geminicode/client.go`, `Open`).
4. The token function reads the policy — `accountrouting.Policy()` →
   `endpoints.CurrentAccountPool()` — and re-reads the file when its stamp
   moved.
5. The decision is taken and the routed account's credential is handed to the
   request.

Nothing is rebuilt, nothing is restarted, and nothing has to be confirmed.

**硬约束**: the *file* is still not watched. A policy edited by hand in a text
editor is picked up by the same per-turn read — the stamp moves — so it is
seen at the next turn too. The user allowed a restart for file edits; the
per-turn read does better than that, and the panel's own writes are immediate
either way.

**硬约束**: the SDK-facing harness path (`cmd/harness/models.go`) keeps the
SDK's explicit `access_token`. An explicit credential is the SDK's business,
and the pool does not override it.

---

## 6. What the catalogue cost

`/pool` is a **listed** command, so the catalogue grew from 108 to 109 rows
and three byte-pinned pins moved with it:

| pin | before | after |
|---|---|---|
| `internal/cliapp/tui_run_test.go` — the catalogue's size | 108 | 109 |
| `internal/cliapp/picker_view_test.go` — the picker's tail | `103 more` | `104 more` |
| `internal/tuilib/commands_order_test.go` — the sorted first twelve | unchanged | unchanged (`/pool` sorts past the twelfth row) |

The reference's own catalogue is 105 rows; the clone's four superset commands
are `/feature-diff`, `/accounts`, `/provider` and `/pool`.

---

## 7. The constraints

- **硬约束**: `endpoints` is the file's only reader **and** its only writer.
  `accountpool` stays pure — no I/O, no `internal/` dependency — so it is
  still importable on its own.
- **硬约束**: the router is installed on **every** CloudCode transport. With
  the policy off, it answers with `backend.DefaultTokenSource()`, so the
  default path is byte-identical to before this cycle.
- **硬约束**: the shared credential is **not** rewritten. The routed account
  is *this process's* account; the keychain entry and the three on-disk copies
  keep pointing at the account the user picked.
- **硬约束**: the threshold is a preference. When every account is below it,
  the one with the most left is used — a turn with no account cannot be
  served.
- **硬约束**: the quota figure is the **lowest** bucket across the account's
  windows, not the weekly one alone.
- **硬约束**: an inactive account is refreshed with `RefreshQuiet`, never
  `RefreshIfNeeded` — the latter writes the shared credential and clears it
  on a rejection.
- **硬约束**: the panel writes the file through `endpoints`, never directly.
  A panel that wrote the file itself would be a second writer.
- **硬约束**: a value that does not parse is refused. The panel never writes
  a policy it could not read back.
- **硬约束**: the writer never overwrites a document it cannot parse.
- **存疑**: the tab bar's **wording** — the two tab names and the
  *"(tab to cycle)"* hint — is the clone's own. The reference has no tabbed
  panel, so there is nothing to match. The *shape* (bold + underlined + blue
  for the active tab, grey for the other) is the `/resume` picker's.
- **存疑**: the duration **ladder** (30 s, 1 m, 2 m, 5 m, 15 m, 1 h) is the
  clone's own. `enter` takes any Go duration, so the ladder is only the quick
  way; a user with an unusual cadence types it.
- **存疑**: the strategy names are the clone's own (cycle 360). The reference
  has one credential and no pool.
- **limitations**: the pool's policy is edited in the **TUI**. The print
  mode (`agy -p /pool`) has no panel — the print path builds its transport
  per run, so a policy in the file is in force for that run.
- **limitations**: the **web UI** has no pool panel. Its account switcher
  (`internal/rcfrontend`) switches the active account by hand, like the
  clone's account list; the policy is the TUI's and the file's.
- **limitations**: the panel does not show the **pool's live state** — which
  account the last turn was routed to, and why. The routing's log lines go to
  the transport's log writer, and `OPENAGY_ACCOUNT_POOL_DEBUG=1` adds them to
  stderr.
- **limitations**: the quota figure is read per account with that account's
  own credential, so a pool of *N* accounts costs *N* quota reads per
  `refresh_interval`. The read is serialised per account and bounded by a
  20-second budget.
- **limitations**: `max_attempts` above 1 replays a turn's request on another
  account (cycle 360). A quota error that arrives late is not retried, only
  recorded.

---

## 8. The diff from cycle 360

Cycle 360's own limitation list is now:

| cycle 360's limitation | cycle 361 |
|---|---|
| *"a policy turned **on** mid-session needs the transport to be rebuilt"* | **gone** — the router is installed always |
| *"there is no panel for the policy and no command that prints the pool's state"* | **half gone** — there is a panel and a command; the pool's *live state* is still not shown |
| *"`config.json` has no writer"* | **gone** — `endpoints.SaveAccountPool` |

---

Reports: [`cycle-360-account-pool-routing`](../cycle-360-account-pool-routing/deliverable.md)
(the previous cycle in this checkout).

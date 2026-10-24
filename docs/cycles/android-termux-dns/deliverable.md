# Android / Termux DNS fallback — deliverable

**Cycle:** `android-termux-dns`
**Subject:** `internal/dns` — the Termux resolver fallback, the binaries
that import it, and the tests that were missing
**Evidence:** the measurements quoted inline; every command is reproducible
on this machine

---

## 0. Summary

A question was asked: *does the Android build apply the Termux DNS hack
when it logs into an account?*

The honest answer had two halves.

**The mechanism works, and it does cover the login.** `internal/dns`
replaces `net.DefaultResolver`; that variable is read **at dial time** by
`net.Dialer`, `net/http`'s default transport and `golang.org/x/oauth2`, so
the OAuth device flow — the login — is covered without touching a single
call site. This was verified by reading the Go and `x/oauth2` sources *and*
by running a probe.

**But nobody had ever verified it.** The package shipped with the Android
release target in a four-file, 46-line commit with **no test, no doc
entry, and no mention in the guide**. Three other binaries that make
outbound calls did not import it at all.

This cycle closes both halves: it adds the missing tests, the missing
imports, a more reliable detection rule, an escape hatch for split-DNS
setups, and the guide entry that says all of this out loud.

---

## 1. Was the hack applied to the login?

### 1.1 The chain, verified in the sources

| Step | Source | What it says |
|---|---|---|
| the binary imports the package | `cmd/pigasus/main.go` | `_ "github.com/justwasm/pigasus/internal/dns"` — a blank import, so the package's `init()` runs before `main()` |
| the package replaces the resolver | `internal/dns/android.go` | `net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: …}` |
| the resolver is read at dial time | `net/dial.go` | `func (d *Dialer) resolver() *Resolver { if d.Resolver != nil { return d.Resolver }; return DefaultResolver }` |
| HTTP dials through a zero Dialer | `net/http/transport.go` | `func (t *Transport) dial(…) { … return zeroDialer.DialContext(ctx, network, addr) }` — `zeroDialer` has a nil `Resolver`, so it resolves through `DefaultResolver` |
| x/oauth2 falls back to the default client | `x/oauth2@v0.37.0/internal/transport.go` | `func ContextClient(ctx) *http.Client { … return http.DefaultClient }` |
| the device flow uses it | `x/oauth2@v0.37.0/deviceauth.go:110` | `internal.ContextClient(ctx).Do(req)` — and the same for `DeviceAccessToken` |
| the clone's own token calls use it | `internal/auth/oauth.go:236,263` | `http.DefaultClient.Do(req)` in `fetchOAuthToken` and `populateUserEmailFromUserInfo` |

So the login path is:

```
pigasus login / the TUI login panel
  → auth.DeviceFlow → cfg.DeviceAuth / cfg.DeviceAccessToken
      → internal.ContextClient(ctx) → http.DefaultClient
          → http.DefaultTransport → zeroDialer
              → net.Dialer.resolver() → net.DefaultResolver  ← the hack
```

Nothing in that chain captures a resolver at init time, so the order of
package initialisation does not matter either.

### 1.2 …and verified by running it

A probe program installed a `net.DefaultResolver` whose `Dial` returned a
marker error, then made a plain `http.Get`:

```
err: Get "https://oauth2.googleapis.com/token": dial tcp: lookup oauth2.googleapis.com
     on [fd7a:115c:a1e0::53]:53: PROBE-DIAL-CALLED
net.DefaultResolver.Dial invoked: 16 time(s)
resolved err contains probe marker: true
```

The plain HTTP path reaches the custom `Dial`. That is now a permanent
test (`TestInstall_RoutesHTTPThroughTheCustomResolver`).

---

## 2. What was actually wrong

| # | Problem | Severity |
|---|---|---|
| 1 | `internal/dns` had **no test at all** | the hack was decorative until proven |
| 2 | the detection was `TERMUX_VERSION` **only** | a scrubbed environment, or a plain (non-Termux) Android run, loses the fallback |
| 3 | `cmd/openai-shim`, `cmd/anthropic-shim` and `cmd/openagy-models` did **not** import the package | all three make outbound calls (`daily-cloudcode-pa.googleapis.com`, the provider endpoint) and would fail to resolve on Termux |
| 4 | the nameserver list was **hardcoded** to public resolvers | a public resolver bypasses a split-DNS setup — a VPN's own resolver, or Termux's `dnsproxy` on `127.0.0.1:53`, is never consulted |
| 5 | the package was **undocumented** | nothing in `docs/guide/` mentioned it |

---

## 3. The changes

### 3.1 `internal/dns/android.go`

* `android()` — the detection rule is now
  `runtime.GOOS == "android" || os.Getenv("TERMUX_VERSION") != ""`.
  The first signal is baked in at build time (the release ships a
  `GOOS=android/arm64` archive) and survives a scrubbed environment; the
  second covers a `linux/arm64` build running inside a proot distro.
* `install(nameservers)` — the assignment, factored out of `init()` so a
  test can drive it.
* `nameservers()` — honours `$OPENAGY_DNS_SERVERS` (comma-separated
  `host:port`; a bare address gets the DNS port). Unset or all-blank falls
  back to the previous default list, so the behaviour without the variable
  is unchanged.
* The package doc now states *why* overriding `net.DefaultResolver` is
  enough, with the three source references.

### 3.2 `internal/dns/android_test.go` — new

| Test | What it pins |
|---|---|
| `TestInstall_RoutesHTTPThroughTheCustomResolver` | **the load-bearing property**: a plain `http.Client` reaches the custom `Dial` (the login path never mentions DNS) |
| `TestDialWithFallback_FallsThroughToTheNextNameserver` | the fall-through, and that the error names the *last* nameserver |
| `TestDialWithFallback_IgnoresTheRequestedServer` | the dial ignores the server the resolver asks for — the hack itself |
| `TestAndroid_DetectsEitherSignal` | both detection signals |
| `TestNameservers_HonoursTheOverride` | the `$OPENAGY_DNS_SERVERS` parsing |

The tests use a closed loopback port, so **nothing leaves the machine** and
the suite stays offline.

### 3.3 The three other binaries

`cmd/openai-shim/main.go`, `cmd/anthropic-shim/main.go` and
`cmd/openagy-models/main.go` now blank-import `internal/dns`. All three
make outbound calls; on Termux they could not resolve anything.

### 3.4 `docs/guide/accounts.md`

A new section, *"Logging in on Android/Termux"*, states the chain, the two
detection signals, the dial-vs-query distinction of the fallback, the
split-DNS caveat, and what the hack does **not** cover.

---

## 4. What the hack does *not* cover

**The vendor `agy` binary that `pigasus agy` execs.** It is a separate
process with its own resolver; nothing in this repository can patch it.
Its CloudCode traffic goes to `127.0.0.1` (the launcher exports
`CLOUD_CODE_URL=http://127.0.0.1:<port>`), so it needs no DNS for the
model calls — but the reference does carry its own OAuth device flow
(`https://oauth2.googleapis.com/token` and
`cloud/verticals/edge/device_authorization/server` are both in the 1.4.7
binary's string table), and *that* round trip needs DNS.

The workaround is to sign in first: `pigasus login` writes the token to
the canonical path, and the child finds it on disk instead of running its
own OAuth.

---

## 5. 硬约束 — hard constraints

* **`net.DefaultResolver` must stay a package-level override.** It is the
  only thing that covers the login: the OAuth code paths never mention
  DNS, and none of them may start.
* **Do not give the login code its own `http.Transport` with a
  `DialContext`.** That would bypass `net.DefaultResolver` and silently
  undo the hack. Every client in the tree uses `&http.Client{Timeout: …}`
  with no transport, or a transport without `DialContext`.
* **`PreferGo: true` is load-bearing.** Without it a cgo-enabled build
  falls back to `getaddrinfo`, which is exactly the resolver that does not
  work on Termux.
* **The `$OPENAGY_DNS_SERVERS` name is public.** It is a knob, not an
  internal detail; renaming it would break a Termux user's setup.
* **The four binaries must all import the package.** A new `cmd/` that
  makes outbound calls has to blank-import `internal/dns`.

---

## 6. 存疑 — open questions

* **Is `8.8.8.8` reachable from a given mobile network?** Some carriers
  block port 53 to public resolvers, and some networks route it to a
  captive portal. The `$OPENAGY_DNS_SERVERS` escape hatch exists for
  exactly that, but the default was not changed — there is no measurement
  from a real Termux device in this repository.
* **Does the reference's own OAuth need the same treatment?** Its token
  file name in the 1.4.7 string table is `jetski-standalone-oauth-token`,
  while the clone writes `antigravity-oauth-token`. If the two do not
  line up, `pigasus agy` will still run its own OAuth on Termux — which
  needs DNS the hack cannot provide. **Not verified here**; it is a
  credential-path question, not a DNS one.
* **Was the original symptom ever the DNS at all?** The report that
  prompted this cycle found the symptom was a VPN, not a resolver. That
  does not make the hack wrong — it makes it unproven, which is what this
  cycle fixes — but no end-to-end run on a real device was done either
  way.

---

## 7. Limitations

* **No Termux device was involved.** Every measurement here is on macOS
  with the resolver deliberately pointed at a closed loopback port. The
  Android build itself (`GOOS=android GOARCH=arm64`) was compiled, not
  run.
* **The vendor binary was not exercised on Android.** §4 is read off the
  1.4.7 binary's string table and the launcher's environment handling,
  not from a device.
* **The fallback's query-level behaviour is untested.** A nameserver that
  accepts the connection but never answers leaves the resolver waiting
  for its own timeout — the fallback is a *dial* fallback, and
  `TestDialWithFallback_FallsThroughToTheNextNameserver` says so.
* **No measurement of whether 8.8.8.8 or 1.1.1.1 is the better default**
  for any particular carrier.

# Cycle 351 deliverable: every Google service URL becomes a configurable endpoint

**Cycle:** `cycle-351-configurable-endpoints`
**Subject:** the clone's Google-owned service URLs were compiled-in constants.
They are now **named endpoints** that `~/.config/openagy/config.json` can point
at a local service — the preparation step for replacing Google's services with
the operator's own.
**Evidence:** 【一手】 first-hand against the working tree (the inventory, the
resolver, the tests) · 【反编译】 n/a — no binary was disassembled this cycle ·
【strings】 n/a · 【推测】 n/a — nothing here is a hypothesis

---

## 0. The short answer

| | before | after |
|---|---|---|
| Google service URLs in the live paths | a constant per call site | **one named endpoint per service**, resolved at use time |
| where the answer lives | `const` in the leaf package | `internal/endpoints` |
| can the user change it | only through the scattered env vars that already existed | **yes — one JSON file** |
| the file | — | `~/.config/openagy/config.json` |

Fourteen named endpoints, one resolver, one file. The compiled-in defaults
are unchanged, so a clone with no configuration file talks to exactly the
services it talked to before.

## 1. The inventory that started it

A sweep of `cmd/**` and `internal/**` (non-test Go) for URLs that point at a
Google-owned service found **123 hits across 48 files**. They were sorted into:

| Class | Count | What it means |
|---|---|---|
| **live** | 24 | the clone itself dials it, loads it, or hands it to a browser |
| **reference-only** | 71 | `internal/grpc`, `internal/codeassistclient`, `internal/cortex_pb`, `internal/language_server_pb` — the reference's own code, rebuilt for the wire |
| **dead** | 19 | declared in production code, no live caller |
| **test-only** | 9 | in a `_test.go` |

Two findings from the sweep are worth recording because they *narrow* the work:

- **`proto-default` has zero instances.** Every generated `.pb.go` in the tree
  was checked: the only `http` / `google` hits are protobuf field names
  (`xxx_hidden_HttpHeaders`, `http_code`) and raw-descriptor byte fragments
  (`x1d.google`, `x19.google`). No generated descriptor carries a URL default,
  so there was nothing to make configurable there.
- **The existing env overrides were already scattered and already
  load-bearing** — `CLOUD_CODE_URL`, `OPENAGY_UPSTREAM`,
  `ANTIGRAVITY_RC_ORIGIN_PATTERNS`, `OPENAI_SHIM_UPSTREAM`,
  `ANTHROPIC_SHIM_UPSTREAM`. They are the *house rule's* second level, and
  they keep it.

The user's scope decision was **live paths only**: the reference-only and dead
packages stay as they are.

## 2. The design

### 2.1 The file

`~/.config/openagy/config.json` — **a second file**, beside the
provider-centric `providers.json`. The split is the point: `providers.json`
answers *"which model talks to which provider"*, `config.json` answers *"which
service does the clone itself talk to"*.

```json
{
  "endpoints": {
    "cloudcode": "http://127.0.0.1:8080",
    "oauth2_token": "http://127.0.0.1:8080/oauth/token"
  }
}
```

A **named section**, not a bare map, so the document can grow a second section
without a format change.

The path resolution mirrors `internal/models.ProvidersPath()`:

```
$OPENAGY_CONFIG                            ← the override
$XDG_CONFIG_HOME/openagy/config.json       ← the convention
$HOME/.config/openagy/config.json          ← the convention
```

`OPENAGY_CONFIG` is the sibling of `OPENAGY_PROVIDERS_CONFIG`.

### 2.2 The package

`internal/endpoints` — two files, no dependency on any other internal
package (so any package may import it without a cycle):

- `endpoints.go` — the **names** (the keys the file carries), the
  **compiled-in defaults**, the `Set` snapshot, `Current()`, the two
  derivations (`WebChannelHostPort()`, `CloudCodeHost()`, `OAuth2AuthHost()`)
  and `Names()` (the table, so a front-end need not repeat the list).
- `config.go` — the **file**: `Path()`, `Config`, `Load()`, and the cache.

### 2.3 The precedence

The house rule, unchanged and applied in this order:

1. an explicit value the caller passes,
2. the **environment variable** the endpoint already had,
3. **the configuration file**,
4. the **compiled-in default**.

Every env var that already existed keeps winning over the file: a variable
set for one invocation is more specific than a file that is always there. The
resolver itself knows nothing about env vars — they stay in the packages that
already had them, so the change is additive and the old behaviour is
untouched.

### 2.4 The resolution

`Current()` returns a `Set`. The file is read **only when it changed on disk**
— the cache key is the path plus the file's size and modification time, the
same identity `internal/models.CachedRegistry` uses. The cache lives in an
`atomic.Pointer`, so the read is lock-free and costs a `stat`.

`Current()` hands out a **snapshot**: read one, use it for one operation. A
turn does not see the CloudCode host change halfway through.

A **missing** file is an empty configuration. A **malformed** file is not an
error either — the defaults are used and the clone starts. A half-written
edit must not take the CLI down.

### 2.5 The seams

Three endpoints had a test seam before they had a resolver. The seam still
wins over the configuration:

| Seam | Before | After |
|---|---|---|
| `auth.TokenURL` | `var` = the Google URL | `var` = **empty**; empty means "the resolver answers" |
| `auth.DeviceFlowEndpoint` | `var` = `endpoints.Google` (x/oauth2) | `var` = **empty**; empty means "the resolver answers" |
| `mendelHosts` | `var` = the two CloudCode hosts | `mendelHostsOverride` (nil by default) + `mendelHosts()` |

The two exported `var`s used to *be* the Google URLs. Making them empty is the
only way the configuration file can reach them — a non-empty default would
always beat the resolver. The tests that point one at an `httptest.Server`
(`internal/loginui/login_model_test.go`, `internal/cli/login_flow_test.go`,
`internal/auth/device_flow_test.go`) keep working unchanged.

## 3. The fourteen endpoints

| Name | Default | Live call sites |
|---|---|---|
| `cloudcode` | `https://daily-cloudcode-pa.googleapis.com` | `internal/backend/cloudcode.go` (the transport), `internal/auth/{load_code_assist,quota,plan_tier,code_assist_state}.go`, `internal/agyproxy/server.go` + `admin.go` (the upstream), `internal/refbackend/{custommodels,spawn}.go`, `internal/rctunnel/supervisor.go`, `cmd/{openai,anthropic}-shim/main.go` |
| `cloudcode_prod` | `https://cloudcode-pa.googleapis.com` | `internal/auth/mendel.go` (the first host the flag call tries) |
| `oauth2_token` | `https://oauth2.googleapis.com/token` | `internal/auth/{oauth,unified_config,antigravity_token}.go` |
| `oauth2_auth` | `https://accounts.google.com/o/oauth2/auth` | `internal/auth/oauth.go` (the consent page), `internal/auth/device_flow.go` |
| `oauth2_device` | `https://oauth2.googleapis.com/device/code` | `internal/auth/device_flow.go` |
| `oauth_redirect` | `https://antigravity.google/oauth-callback` | `internal/auth/oauth.go` |
| `userinfo` | `https://www.googleapis.com/oauth2/v3/userinfo` | `internal/auth/{oauth,antigravity_token}.go` |
| `webchannel` | `https://jetski-webchannel.googleapis.com/v1internal:connectInstanceV2` | `internal/rctunnel/client.go`; the Mendel flag `remote-control-proxy-server-url` takes its **host:port** |
| `generativelanguage` | `https://generativelanguage.googleapis.com` | `internal/rctunnel/supervisor.go`, `internal/refbackend/spawn.go` (`--api_server_url`), `internal/models/provider.go` (the error hint) |
| `companion_origin` | `https://antigravity.google.com` | `internal/lsserver/answers.go` (the `companionOrigin` payload), `internal/remotecontrol/dispatch_languageserver.go`, `internal/remotecontrol/mendel_flags.go` (the `remote-control-companion-url` flag), `internal/cliapp/slash_commands_handlers.go` (the remote-view URL), `internal/cli/subcommand_rc.go`, `internal/persistentrc/instance_name.go` |
| `origin_patterns` | `https://antigravity.google,https://antigravity-companion.google` | `internal/originpolicy/originpolicy.go` (the browser **Origin allowlist**) — a comma-separated list |
| `terms` | `https://antigravity.google/terms` | `internal/onboarding/step_tos.go` |
| `privacy` | `https://policies.google.com/privacy` | `internal/onboarding/step_tos.go` |
| `marketplace` | `https://marketplace.antigravity.com/skills?q=` | `internal/cli/marketplace_link.go` |

Three **derivations** came out of the table rather than out of the file:

- `WebChannelHostPort()` — the Mendel flag carries `host:port`, and the
  endpoint is a full URL. `https` ⇒ `:443`, `http` ⇒ `:80`, an explicit port
  wins, an unparseable endpoint yields the empty string.
- `CloudCodeHost()` — the `Signed out from server: <host>` line.
- `OAuth2AuthHost()` — the line the device-code login announces.

## 4. What changed, file by file

**New** — `internal/endpoints/{endpoints,config}.go` + two test files.

**Rewired** — 22 production files, 12 test files. The rule was uniform: the
package-level constant became an **alias of the endpoint package's default**,
and the **live call site** read the resolver. The alias keeps the exported
name (`auth.CloudCodeEndpoint`, `backend.CloudCodeEndpoint`,
`agyproxy.DefaultUpstream`, `rctunnel.DefaultWebChannelEndpoint`,
`onboarding.TermsLinkTerms`, `cli.MarketplaceLinkPattern`, …) compiling and
documented, while there is exactly **one** source of truth for the value
itself.

Three defaults that appeared **twice** in the tree
(`auth.CloudCodeAPI`, `auth.CloudCodeDaily`, `auth.GeminiAPI`) are now
aliases too, so the two copies cannot drift.

## 5. The tests

`internal/endpoints` has its own suite (19 cases): the defaults, an override,
an unknown name, an empty value, the origin list, the origin list that
carries no usable entry (it must **fall back**, never empty the allowlist),
the shared-slice guard, the `host:port` derivation, the file's path
resolution, the missing file, the malformed file, the cache's invalidation,
and the table's completeness.

Two hermetic rules shaped the rest of the suite:

- **Every** test that touches the resolver points `$OPENAGY_CONFIG` at a
  temporary path. The resolver reads the **user's real** configuration file
  when the variable is unset — a test that read it would depend on the
  machine it runs on.
- Every test that asserts on a resolved URL now derives the expectation from
  the resolver (`endpoints.Current().CompanionOrigin`, …) instead of pinning
  a literal. Eight such assertions existed; they are now correct under *any*
  configuration, not only under the default one.

`go test ./cmd/... ./internal/...` is green.

## 6. Hard constraints

- **The compiled-in defaults did not move.** A clone with no configuration
  file talks to exactly the services it talked to before this cycle.
- **The env vars did not move and still win.** `CLOUD_CODE_URL`,
  `OPENAGY_UPSTREAM`, `ANTIGRAVITY_RC_ORIGIN_PATTERNS`,
  `OPENAI_SHIM_UPSTREAM`, `ANTHROPIC_SHIM_UPSTREAM`.
- **The reference-only packages were not touched** — `internal/grpc`,
  `internal/codeassistclient`, `internal/cortex_pb`,
  `internal/language_server_pb`. Their constants are not on a live path.
- **The reference's client ids and scopes were not touched.** They are
  identifiers, not services.
- **The static diagnostic dump was not touched** — `printOAuthConfig`
  (`internal/cliapp/main_dispatch.go`) prints a reconstruction of the
  reference's OAuth skeleton, marked `"status": "stub"`. It is documentation,
  not a live path, and its `device_url` is the *reference's*
  `cloudaicompanion.googleapis.com`, not the clone's device endpoint.
- **The origin allowlist cannot be emptied by accident.** A value that
  carries no usable entry falls back to the defaults: an empty allowlist
  means "allow every origin" in the remote-control host, and that must never
  happen because of a typo.

## 7. Open questions

- **There is no writer.** The file is hand-edited. `internal/models.Editor`
  is the *only* writer of `providers.json`; nothing writes `config.json` yet.
  A `pigasus endpoints` command that prints the resolved table, and an
  `Editor`-shaped writer for the file, are the natural next step — they were
  left out of this cycle because the request was the override itself.
- **The shims' upstream is derived, not stored.** `cmd/openai-shim` and
  `cmd/anthropic-shim` build their default as
  `endpoints.Current().CloudCode + "/v1internal:generateChat"`. A local
  service that speaks a different RPC path is not reachable through the
  configuration alone — the shims take a full URL on the command line.
- **The reference backend's `--api_server_url` is a *host*, not a base URL**,
  and the clone passes the configured `generativelanguage` host through. A
  local service that expects a path prefix is not reachable that way either.

## 8. Limitations

- The resolver's cache is keyed on **size + mtime**. An edit that preserves
  both (a same-length rewrite inside the filesystem's timestamp
  resolution) is not seen until the next change. The same identity
  `internal/models.CachedRegistry` uses, and the same limitation.
- The configuration is read on **every** `Current()` call's cache miss, and
  the miss is decided by a `stat` — cheap, but not free on a path that
  resolves per request.
- No live run against a *local* replacement service has been performed. The
  resolver's contract is covered by unit tests; the end-to-end story (a local
  OAuth service, a local Code Assist service, a local companion page) is
  untested and is the next cycle's job.
- The reference's own behaviour is unchanged: `agy` still has Google's
  services compiled in. This cycle changes **the clone**, so that the
  operator can run it against something else.

---

Reports: `docs/guide/endpoints.md` (the current state).

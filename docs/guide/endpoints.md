# The service endpoints

> Every Google-owned service the clone dials has a name, and every name can be pointed somewhere else from `~/.config/openagy/config.json`.

## What this is for

The reference binary has Google's services compiled in — that is what `agy`
*is*. A clone whose point is to be able to point at a **local** service
instead needs that URL to be a *value*, not a literal.

`endpoints` is the one place that says which service is which URL,
and the one place a user's configuration can change the answer. Every live
call site reads `endpoints.Current()` instead of a constant.

The configuration file is `~/.config/openagy/config.json` — **a second file**,
next to the provider-centric `providers.json` (see
[`models.md`](models.md)). The split is deliberate: `providers.json` answers
*"which model talks to which provider"*, `config.json` answers *"which
service does the clone itself talk to"*.

```json
{
  "endpoints": {
    "cloudcode": "http://127.0.0.1:8080",
    "oauth2_token": "http://127.0.0.1:8080/oauth/token",
    "oauth2_auth": "http://127.0.0.1:8080/oauth/authorize",
    "companion_origin": "http://localhost:5173",
    "origin_patterns": "http://localhost:5173"
  },
  "account_pool": {
    "enabled": true,
    "strategy": "quota_aware",
    "threshold": 0.15
  }
}
```

The file has **two sections**. `endpoints` is the endpoint overrides; the second
one is the account-routing policy, whose shape belongs to the `accountpool`
package — `endpoints` only carries it, so the file keeps a single reader.
Cycle 361 added the writer for that second section; see *How it is written*
below.

A missing file is an **empty configuration**, not an error. A malformed file
is not an error either: the compiled-in defaults are used and the clone
starts. A half-written edit must not take the CLI down.

## The table

| Name | Compiled-in default | What it is |
|---|---|---|
| `cloudcode` | `https://daily-cloudcode-pa.googleapis.com` | the Code Assist host: the clone's own transport, `loadCodeAssist`, the quota / tier calls, the proxy's upstream, the shims' upstream, and the endpoint the spawned reference is given |
| `cloudcode_prod` | `https://cloudcode-pa.googleapis.com` | the production Code Assist host — the first of the two the Mendel flag call tries |
| `oauth2_token` | `https://oauth2.googleapis.com/token` | the refresh-token grant and the device flow's token exchange |
| `oauth2_auth` | `https://accounts.google.com/o/oauth2/auth` | the consent page the browser sign-in opens |
| `oauth2_device` | `https://oauth2.googleapis.com/device/code` | the device-code endpoint |
| `oauth_redirect` | `https://antigravity.google/oauth-callback` | where the browser sign-in's authorization code comes back to |
| `userinfo` | `https://www.googleapis.com/oauth2/v3/userinfo` | where the user's email is read from |
| `webchannel` | `https://jetski-webchannel.googleapis.com/v1internal:connectInstanceV2` | the cloud tunnel the remote-control view connects to (a **full** URL — the RPC path is part of it) |
| `generativelanguage` | `https://generativelanguage.googleapis.com` | the Generative Language API host, passed to the spawned reference as `--api_server_url` |
| `companion_origin` | `https://antigravity.google.com` | the browser origin the companion web app lives on: the URL the user is told to open, the `companionOrigin` the Web UI is told about |
| `origin_patterns` | `https://antigravity.google,https://antigravity-companion.google` | the browser **Origin allowlist** the remote-control host serves — a comma-separated list |
| `terms` | `https://antigravity.google/terms` | the Terms of Service link the onboarding renders |
| `privacy` | `https://policies.google.com/privacy` | the Privacy Policy link the onboarding renders |
| `marketplace` | `https://marketplace.antigravity.com/skills?q=` | the plugin marketplace's skill search endpoint (the trailing `?` is part of it) |

`endpoints.Names()` returns this table, so a front-end can print it without
repeating the list.

## Precedence

The house rule, unchanged:

1. an explicit value the caller passes (a flag, a `Config` field),
2. the **environment variable** the endpoint already had,
3. **this configuration file**,
4. the **compiled-in default**.

The environment variables stay where they were — `CLOUD_CODE_URL`
(`internal/backend`), `OPENAGY_UPSTREAM` (`internal/agyproxy`),
`ANTIGRAVITY_RC_ORIGIN_PATTERNS` (`originpolicy`),
`OPENAI_SHIM_UPSTREAM` / `ANTHROPIC_SHIM_UPSTREAM` (the shims) — and they
still win over the file: a variable set for one invocation is more specific
than a file that is always there.

## Where the file is

```
$OPENAGY_CONFIG                              ← the override
$XDG_CONFIG_HOME/openagy/config.json         ← the convention
$HOME/.config/openagy/config.json            ← the convention
```

`OPENAGY_CONFIG` is the sibling of `OPENAGY_PROVIDERS_CONFIG`, which names
the provider-centric file.

## How it is read

`endpoints.Current()` returns a `Set` — the resolved table. The file is read
**only when it changed on disk** (the cache key is the path plus the file's
size and modification time, the same identity `internal/models.CachedRegistry`
uses), so calling it on a hot path costs a `stat` and a pointer load. The
cache is held in an `atomic.Pointer`, so the read is lock-free.

`Current()` hands out a **snapshot**: read one and use it for the whole of
one operation, so a turn does not see the CloudCode host change halfway
through.

## How it is written

`endpoints` is the file's **only** reader *and* its only writer. Cycle 361 gave
it the writer it did not have: `endpoints.SaveAccountPool(policy)` rewrites the
`account_pool` section — the account-routing policy, see
[`accounts.md`](accounts.md) — and leaves the rest of the document byte for byte
as it was.

Three things about that write:

- **The edit is made on the raw document** (`sjson.SetRawBytes` /
  `sjson.DeleteBytes`), not by re-marshalling a decoded map. The key order
  survives, and a section a newer version of the clone wrote survives with it.
- **The write is atomic** (`atomicfile.WriteFile`, 0600) and the cached snapshot
  is dropped, so the policy the routing reads is the one that was just written —
  and the routing reads it per turn, so a policy changed in the TUI's own panel
  takes effect on the next turn.
- **A malformed document is refused, not replaced.** A file the writer cannot
  parse is an error; the writer never overwrites a file it could not read.

The provider-centric file has its own writer (`internal/models.Editor`) and its
own front-ends (`pigasus provider`, `pigasus model`, the TUI's `/provider`
panel). The two files never meet: `providers.json` answers *"which model talks
to which provider"*, `config.json` answers *"which service does the clone
itself talk to"* and *"which of my accounts serves this turn"*.

## The seams

Three endpoints had a **test seam** before they had a resolver, and the seam
still wins over the configuration:

| Seam | Where | Shape |
|---|---|---|
| `auth.TokenURL` | `internal/auth/oauth.go` | empty by default; a non-empty value is the token endpoint |
| `auth.DeviceFlowEndpoint` | `internal/auth/device_flow.go` | empty by default; a non-empty `oauth2.Endpoint` is the device endpoint |
| `mendelHostsOverride` | `internal/auth/mendel.go` | nil by default; a non-empty list replaces the resolver's hosts |

The two exported `var`s used to *be* the Google URLs; they are now empty and
the resolver answers. A test that points one at an `httptest.Server` keeps
working unchanged.

## What is **not** covered

The scope is the **live paths** — the URLs the clone itself dials, loads or
hands to a browser. Three groups are deliberately left alone:

- **The reference-only reconstructions** — `grpc`,
  `internal/codeassistclient`, `internal/cortex_pb`,
  `internal/language_server_pb`. These are the reference's own code, rebuilt
  for the wire; their `EndpointProd` / `EndpointDaily` constants are not on
  any live path.
- **The reference's OAuth client ids and scopes** — `*.apps.googleusercontent.com`
  and `https://www.googleapis.com/auth/…`. They are *identifiers*, not
  services you dial.
- **The static diagnostic dump** — `printOAuthConfig`
  (`internal/cliapp/main_dispatch.go`) prints a reconstruction of the
  reference's OAuth skeleton, marked `"status": "stub"`. It is documentation,
  not a live path.

The dead `var`s in `internal/auth/oauth.go` (`IamCredsURL`, `AICodeAPI`,
`CloudCodeStage`) stay as they are, for the same reason. The three that
*duplicate* a live default (`CloudCodeAPI`, `CloudCodeDaily`, `GeminiAPI`)
are aliases of the endpoint package's constants, so the two cannot drift.

## Reports

`docs/cycles/cycle-351-configurable-endpoints/deliverable.md`.

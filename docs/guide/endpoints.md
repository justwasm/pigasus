# The service endpoints

> Every Google-owned service the clone dials has a name, and every name can be pointed somewhere else from `~/.openagy/config.json`.

## What this is for

The reference binary has Google's services compiled in — that is what `agy`
*is*. A clone whose point is to be able to point at a **local** service
instead needs that URL to be a *value*, not a literal.

`endpoints` is the one place that says which service is which URL,
and the one place a user's configuration can change the answer. Every live
call site reads `endpoints.Current()` instead of a constant.

The configuration file is `~/.openagy/config.json` — **a second file**,
next to the provider-centric `providers.json` (see
[`models.md`](models.md)), in the clone's single configuration home. The
split is deliberate: `providers.json` answers *"which model talks to which
provider"*, `config.json` answers *"which service does the clone itself talk
to"*.

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
  },
  "wrapper": {
    "capture": true,
    "capture_max_bytes": 1073741824
  }
}
```

The file has **three sections**. `endpoints` is the endpoint overrides; the
second one is the account-routing policy, whose shape belongs to the
`accountpool` package — `endpoints` only carries it, so the file keeps a
single reader. The third is `wrapper`, the proxy's own persisted state:
its shape belongs to `internal/infra/agyproxy`, so `endpoints` carries it
**verbatim**, as an opaque section. Cycle 361 added the writer for the
second section; cycle 370 added the third; see *How it is written* below.

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
| `account_chooser` | `https://accounts.google.com/AccountChooser` | Google's account chooser — the remote-control link goes *through* it, so the browser picks the account the daemon registered with and then continues to the deep link |
| `origin_patterns` | `https://antigravity.google,https://antigravity-companion.google` | the browser **Origin allowlist** the remote-control host serves — a comma-separated list |
| `terms` | `https://antigravity.google/terms` | the Terms of Service link the onboarding renders |
| `privacy` | `https://policies.google.com/privacy` | the Privacy Policy link the onboarding renders |
| `marketplace` | `https://marketplace.antigravity.com/skills?q=` | the plugin marketplace's skill search endpoint (the trailing `?` is part of it) |
| `auto_updater` | `https://antigravity-cli-auto-updater-974169037036.us-central1.run.app` | the Cloud Run auto-updater service host used to fetch update manifests |
| `poll_signaling` | `https://jetski-webchannel.googleapis.com/v1internal:pollSignalingMessages` | the cloud RPC polling for WebRTC mesh signaling messages (derived from `webchannel` host when unset) |
| `send_signaling` | `https://jetski-webchannel.googleapis.com/v1internal:sendSignalingMessage` | the cloud RPC sending WebRTC mesh signaling messages (derived from `webchannel` host when unset) |

`endpoints.Names()` returns this table, so a front-end can print it without
repeating the list.

## Precedence

The house rule, unchanged:

1. an explicit value the caller passes (a flag, a `Config` field),
2. the **environment variable** the endpoint already had,
3. **this configuration file**,
4. the **compiled-in default**.

The environment variables stay where they were — `CLOUD_CODE_URL`
(`internal/infra/backend`), `OPENAGY_UPSTREAM` (`internal/infra/agyproxy`),
`ANTIGRAVITY_RC_ORIGIN_PATTERNS` (`originpolicy`),
`OPENAI_SHIM_UPSTREAM` / `ANTHROPIC_SHIM_UPSTREAM` (the shims) — and they
still win over the file: a variable set for one invocation is more specific
than a file that is always there.

`CLOUD_CODE_URL` is applied **in `currentOverrides()`**, not only in the
transport: every caller of `Current()` — the plan-tier lookup behind the
banner and `/accounts`, the quota summary, the reference-backend spawn —
reads the same host. When only the transport honoured it, a run pointed at a
local service still sent its tier and quota calls to Google with the
operator's real token (found by `tuialign`: the banner had no
`(Antigravity Pro)` because the tier call never reached the mock).

## Where the file is

```
$OPENAGY_CONFIG                              ← the override
$OPENAGY_CONFIG_HOME/config.json             ← the configuration home
$HOME/.openagy/config.json                   ← the configuration home
```

`OPENAGY_CONFIG` is the sibling of `OPENAGY_PROVIDERS_CONFIG`, which names
the provider-centric file.

The configuration home is **one directory** — `$HOME/.openagy`, or
`$OPENAGY_CONFIG_HOME` where it says otherwise. The retired
`$HOME/.config/openagy` is gone; its files were moved into the
configuration home by hand.

## How it is read

`endpoints.Current()` returns a `Set` — the resolved table. The file is read
**only when it changed on disk** (the cache key is the path plus the file's
size and modification time, the same identity `internal/features/models.CachedRegistry`
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
as it was. Cycle 370 added `endpoints.SaveWrapper(raw)`, which does the same
for the `wrapper` section — the proxy's own state, see
[`proxy.md`](proxy.md).

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

The provider-centric file has its own writer (`internal/features/models.Editor`) and its
own front-ends (`pigasus provider`, `pigasus model`, the TUI's `/provider`
panel). The two files never meet: `providers.json` answers *"which model talks
to which provider"*, `config.json` answers *"which service does the clone
itself talk to"* and *"which of my accounts serves this turn"*.

## The seams

Three endpoints had a **test seam** before they had a resolver, and the seam
still wins over the configuration:

| Seam | Where | Shape |
|---|---|---|
| `auth.TokenURL` | `internal/features/auth/oauth.go` | empty by default; a non-empty value is the token endpoint |
| `auth.DeviceFlowEndpoint` | `internal/features/auth/device_flow.go` | empty by default; a non-empty `oauth2.Endpoint` is the device endpoint |
| `mendelHostsOverride` | `internal/features/auth/mendel.go` | nil by default; a non-empty list replaces the resolver's hosts |

The two exported `var`s used to *be* the Google URLs; they are now empty and
the resolver answers. A test that points one at an `httptest.Server` keeps
working unchanged.

### The environment seam is also a test hazard

`CLOUD_CODE_URL` (and the voice / origin variables) win over *everything*,
including the configuration file — which is the documented precedence and the
right answer for one invocation. The consequence for a test binary is that
`go test` inherits whatever a developer's shell carries, and the launcher
exports `CLOUD_CODE_URL=http://127.0.0.1:<port>` for the reference binary. A
test that resolves an endpoint therefore measures the developer's machine:

- `endpoints.Current().CloudCode` stops being Google and becomes the local
  proxy;
- anything that *compares* against it — `isSandboxFailoverEligible` accepts a
  custom endpoint only when it equals the resolved primary — takes the other
  branch. `TestCloudCodeImageSandboxFailover` reported `calls primary/mirror =
  0/1` for exactly this reason, on a machine that had the proxy running.

Two defences, both in place:

- `internal/testguard` un-sets every endpoint variable the resolver reads
  (`endpointEnvNames`) from a package's `TestMain`, so a test resolves the
  compiled-in table. A test that needs an endpoint variable still sets it with
  `t.Setenv` afterwards.
- A test that must be independent of the resolver names the constant it means
  (`endpoints.DefaultCloudCode`) rather than asking `Current()` what it is.

## What is **not** covered

The scope is the **live paths** — the URLs the clone itself dials, loads or
hands to a browser. Three groups are deliberately left alone:

- **The reference-only reconstructions** — `grpc`,
  `internal/infra/codeassistclient`, `internal/cortex_pb`,
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

The dead `var`s in `internal/features/auth/oauth.go` (`IamCredsURL`, `AICodeAPI`,
`CloudCodeStage`) stay as they are, for the same reason. The three that
*duplicate* a live default (`CloudCodeAPI`, `CloudCodeDaily`, `GeminiAPI`)
are aliases of the endpoint package's constants, so the two cannot drift.

## Discovered Reference Endpoints and OAuth Identities

Reverse engineering of the official binary (`jetski/cortex`, versions 1.3.3 through 1.5.11) identified the complete set of Google-internal and public endpoints, client IDs, and OAuth scopes used across the CLI and Code Assist client:

### Discovered Service Hosts

| Service / Purpose | Host URL | Description |
|---|---|---|
| Code Assist Public (Prod) | `https://cloudcode-pa.googleapis.com` | Production Code Assist gateway (`CloudCodeAPI`) |
| Code Assist Daily | `https://daily-cloudcode-pa.googleapis.com` | Staging/daily gateway used for consumer accounts (`CloudCodeDaily`) |
| Code Assist Sandbox | `https://autopush-cloudcode-pa.sandbox.googleapis.com` | Internal Google autopush sandbox (`CloudCodeStage`) |
| AI Code License Management | `https://aicode.googleapis.com` | ManagementService RPCs (`FetchLicenses`) (`AICodeAPI`) |
| OAuth2 Device Code | `https://oauth2.googleapis.com/device/code` | Device flow challenge endpoint |
| OAuth2 Token (Standard) | `https://oauth2.googleapis.com/token` | Standard OAuth token exchange and refresh |
| OAuth2 Token (mTLS) | `https://oauth2.mtls.googleapis.com/token` | Enterprise mTLS token endpoint |
| GCP IAM Credentials | `https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/...` | ADC service account impersonation (`IamCredsURL`) |
| Generative Language API | `https://generativelanguage.googleapis.com` | Gemini developer API endpoint (`GeminiAPI`) |

### Discovered OAuth Client IDs & Scopes

- **OAuth Client ID 1 (CLI / Public Antigravity):**
  `1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com`
- **OAuth Client ID 2 (IDE / Internal):**
  `884354919052-36trc1jjb3tguiac32ov6cod268c5blh.apps.googleusercontent.com`
- **OAuth Scopes:**
  - `https://www.googleapis.com/auth/cloud-platform`
  - `https://www.googleapis.com/auth/cclog`
  - `https://www.googleapis.com/auth/aicode`
  - `https://www.googleapis.com/auth/drive.readonly` (and drive related scopes)

## Reports

`docs/cycles/cycle-351-configurable-endpoints/deliverable.md`,
`docs/cycles/cycle-370-one-config-home/deliverable.md`.

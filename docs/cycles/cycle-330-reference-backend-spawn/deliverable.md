# The reference's own page opens — the spawn recipe the reference wants — deliverable

**Cycle:** `cycle-330-reference-backend-spawn`
**Subject:** `pigasus web --reference-backend=<language_server>` — the
reference the clone *starts* (rather than an operator's running one),
the arguments that decide whether its web app opens at all, and the
arguments that decide whether it opens on the operator's **real** data
instead of the mock.
**Evidence:** 【一手】 the reference's own answers, one flag at a time
(`out/refbackend-spawn/spawn_matrix.sh`, captured in
`out/refbackend-spawn/spawn_matrix.txt`); 【一手】 the mock-off matrix
(`out/refbackend-spawn/real_data_matrix.sh` →
`out/refbackend-spawn/real_data_matrix.txt`); 【一手】 the same
instances in a browser (`out/refbackend-spawn/browser_boot.txt`); 【一手】
the rig itself, end to end: the page renders the product's own UI and
the reference answers the proxied RPCs with its own secret.

---

## 0. Summary

| | before | after |
|---|---|---|
| **`GET /` on the spawned reference** | `404 Not Found` — the reference serves no page | `200 text/html` — the reference's own page |
| **the page in the browser** | `打不开: reference backend: 404 Not Found` | the product's own UI: *New Conversation*, the model picker, the composer |
| **the console** | — | `[SidebarLoad] Conversation list loaded (25 conversations)` — no error |
| **the reference's own secret** | none — the reference accepted every call | minted for the run; the reference demands it and the proxy presents it |
| **the product the page claims to be** | — | `antigravity-cli` (the CLI's), not `antigravity-ide` |
| **the data behind the page** | `-use_mocked_data=true` — *Mock User*, `gemini-3.1-pro`, **0 conversations** | the operator's real account, the real catalogue (*Gemini 3.8 Flash High*, *Gemini 4 Argon*), **25 real conversations** with their titles |
| **where the reference reads** | a throwaway temp gemini directory | the operator's own `~/.gemini` — the reference's own default |

---

## 1. The symptom

```console
$ go run ./cmd/pigasus/ web --port 8888 --reference-backend=/Applications/Antigravity.app/Contents/Resources/bin/language_server --open
pigasus web: server listening on [::]:8888
  - Backend:     reference language_server (/Applications/Antigravity.app/Contents/Resources/bin/language_server)
  - Frontend:    the reference's own assets
…
http://127.0.0.1:8888/

打不开: reference backend: 404 Not Found
```

The page the browser asked for was the reference's own index document
(the rig serves the reference's assets, so the UI and the answers come
from the same product). The clone's asset proxy fetched it and got
`404 Not Found`.

【一手】 The reference the clone started was running with:

```
language_server -http_server_port=60337 -standalone=true -persistent_mode=true
                 -use_mocked_data=true -gemini_dir=<tmp>
```

and that instance answers `GET /` with `404 text/plain`. **The
reference serves its own web app only in the "hub" subclient mode.**

A second, quieter symptom showed up after that one was fixed: the page
opened, but it was a **mock** — *Mock User*, `gemini-3.1-pro`, no
conversations, no projects. The reference's own flag says so:

```
$ language_server --help
  -use_mocked_data
        use mocked data for the server (default false)
  -mock_fixture string
        path to mock fixture file (requires --use_mocked_data)
```

`-use_mocked_data` is **off** by default; the clone's spawn was turning
it on. That is the whole of the second bug.

---

## 2. The flags, measured

【一手】 2026-10-26, the same binary
(`/Applications/Antigravity.app/Contents/Resources/bin/language_server`),
one flag changed at a time (`out/refbackend-spawn/spawn_matrix.sh`):

| flags | `GET /` | `window.__APP_CONFIG__` |
|---|---|---|
| `-subclient_type=hub -app_data_dir=antigravity -csrf_token=…` | `200 text/html` | `{"productName":"antigravity","csrfToken":"ref-csrf-token",…}` |
| without `-subclient_type=hub` | **`404 text/plain`** | — (no page at all) |
| without `-app_data_dir` | `200 text/html` | `{"productName":"antigravity-ide",…}` |
| without `-csrf_token` | `200 text/html` | `{"productName":"antigravity","csrfToken":"",…}` |

The RPC surface is the same in every row: the Connect-RPC service
answers on the HTTP port whatever the mode. Only the *page* differs.

### 2.1 `-subclient_type=hub` — the page exists or it does not

The reference's HTTP server serves the SPA, the bundle and the
`window.__APP_CONFIG__` only in the hub subclient mode. A plain
standalone instance is the *CLI's* language server: it answers RPCs and
nothing else.

```
-http_server_port=8931 -standalone=true -persistent_mode=true -use_mocked_data=true
    GET /          → 404 text/plain
    GET /main.js   → 404

-http_server_port=8932 … the same … -subclient_type=hub
    GET /          → 200 text/html        (the reference's page)
    GET /main.js   → 200 text/javascript  (the bundle, 10.4 MB)
```

The reference's own IDE invocation carries the same flag
(`--subclient_type hub`), which is where the recipe comes from.

### 2.2 `-csrf_token` — the page loads, or the app rejects itself

This one does not change anything the *server* says about the page: the
page is served either way. It changes what the *app* does with it.

【一手】 In a browser, against the same binary:

```
### hub + app_data_dir + csrf_token
page:    "Antigravity"  len=372
         "New Conversation / Conversation History / Customizations / Projects /
          Settings / No Project / Ask anything, @ to mention, … / Gemini 3.8 Flash High / …"
console: (no error)

### hub + app_data_dir, no csrf_token
page:    "Antigravity"  len=0
console: [log] Uncaught (in promise) (http://127.0.0.1:9204/main.js)
```

Without a secret the bundle rejects itself and the page stays blank.
With one, the app boots.

The reference *demands* the secret afterwards — it is not a decoration:

```
POST /exa.language_server_pb.LanguageServerService/GetStatus
  without the secret → {"code":"unauthenticated","message":"unauthenticated: missing CSRF token"}
  with the secret    → {}
```

The proxy presents it (see `internal/refbackend/csrf.go`), so the rig
keeps working.

### 2.3 `-app_data_dir` — which product the page is, and whose history it serves

The page's `productName` decides what the app opens:

| page | app |
|---|---|
| `antigravity-ide` | the IDE's *sign-in* page — "Welcome to Antigravity Ide / Continue with Google" |
| `antigravity` | the IDE's own UI — the composer, the model picker, the settings |
| `antigravity-cli` | the **CLI's** own UI — the same composer, and the CLI's own sessions in the sidebar |

The third row is the important one, and it is not visible from the page
alone: **the app data directory decides whose state the reference
serves.** The directory is the reference's own, relative to its gemini
directory, and the two products keep different ones:

```
~/.gemini/antigravity/       the IDE's  — 1,245 rows, none of them titled
~/.gemini/antigravity-cli/   the CLI's  — 14,446 rows, 345 of them titled
```

【一手】 The same binary, the same real account, one flag apart
(`out/refbackend-spawn/real_data_matrix.txt`):

| `-app_data_dir` | `productName` | `JetboxSubscribeToSummaries` |
|---|---|---|
| `antigravity-cli` | `antigravity-cli` | **398 trajectories, 360 with a title** |
| `antigravity` | `antigravity` | **0 trajectories** |

The IDE's directory holds the *untitled* CLI rows, so the IDE's own UI
has nothing to list — the sidebar answers "No conversations yet" under
every project. The CLI's directory holds the titled sessions, so the
CLI's UI lists them: *Image Recognition And Analysis*, *AI Model
Identity Inquiry*, *项目咨询与识别*, *Print Working Directory* … and
"[SidebarLoad] Conversation list loaded (25 conversations)".

The clone is a **CLI** clone and the operator's sessions are the CLI's,
so the spawn uses `antigravity-cli`.

### 2.4 the mock mode off — what the real data looks like

【一手】 The same binary, the mock flag on and off
(`out/refbackend-spawn/real_data_matrix.txt`):

```
### mocked (the old spawn's flags)
GetUserStatus              -> {"userStatus":{"pro":true,"name":"Mock User",
                              "email":"mock-user@google.com",…}}
JetboxSubscribeToSummaries -> 0 trajectories, 0 with a project, 0 with a title

### real, the CLI's app data dir
GetUserStatus              -> {"userStatus":{"disableTelemetry":true,"name":"…",
                              "email":"…@gmail.com","planStatus":{"planInfo":
                              {"planName":"Pro",…}},…}}
JetboxSubscribeToSummaries -> 398 trajectories, 359 with a project, 360 with a title
```

The mocked instance is not broken — it answers cleanly, with a mock
user, the mock model (`gemini-3.1-pro`) and an empty index. It is just
not the operator's data.

Two more flags are needed for the real data to be *usable*, and they
are the IDE's own:

```
-api_server_url=https://generativelanguage.googleapis.com
-cloud_code_endpoint=https://daily-cloudcode-pa.googleapis.com
```

Without them the reference talks to its **default** API host —
`http://0.0.0.0:50001`, nothing — and the app opens its *sign-in* page
instead of the product's UI, because there is no account to sign in
with. With them the same instance answers with the operator's account
and the real catalogue. The clone's own CloudCode channel uses the same
endpoint (`internal/backend/cloudcode.go`), so both sides of a
comparison talk to the same cloud.

### 2.5 the gemini directory — the reference's default is the real one

【一手】 The reference's own help:

```
-gemini_dir string
      Path where Gemini files are stored. If absolute path, will set directly.
      If relative path, will be resolved relative to HomeDir. (default ".gemini")
```

So **not passing** `-gemini_dir` at all means `~/.gemini` — the
operator's real state. The old spawn passed a fresh temp directory, so
the reference answered from an empty world: no account, no projects, no
conversations, and the app data directory was created *inside* the temp
directory and removed with it.

The operator can still isolate the reference — the flag is honoured
when it comes from `OPENAGY_REFERENCE_BACKEND_ARGS`, and the reference
takes the **last** occurrence of a flag, so the caller's value wins.

---

## 3. The fix

`internal/refbackend/spawn.go` — the spawn now starts the reference
with

```
-http_server_port=<port> -standalone=true -persistent_mode=true
-subclient_type=hub -app_data_dir=antigravity-cli
-csrf_token=<minted>
-api_server_url=https://generativelanguage.googleapis.com
-cloud_code_endpoint=https://daily-cloudcode-pa.googleapis.com
-override_ide_name=antigravity -override_user_agent_name=antigravity
-override_ide_version=<the IDE bundle's CFBundleShortVersionString>
```

and **no** `-use_mocked_data`, and **no** `-gemini_dir`.

The caller's own arguments come last, so the reference takes theirs:
`OPENAGY_REFERENCE_BACKEND_ARGS="-gemini_dir=/tmp/isolated"` still
isolates the reference, and `-app_data_dir=antigravity` still switches
the rig to the IDE's product.

The secret is minted for the run — 24 random bytes, base64url — and it
never leaves the machine:

- the browser learns it from the page the reference serves (the rig
  serves that page verbatim);
- the proxy learns it from the same document
  (`internal/refbackend/csrf.go`) and presents it on every call;
- the clone's own remote-control secret *becomes* it
  (`internal/rchost/web.go`), so the browser's token is accepted by
  the clone's transport and by the reference at the same time.

The version is read out of the app bundle the reference binary lives in
(`/Applications/Antigravity.app/Contents/Info.plist` →
`CFBundleShortVersionString`); the page then reports the IDE's own
version (`appVersion: 2.26.0`) instead of `0.0.0.0`, and the bundle
asks for its assets with it (`audio_processor.js?v=2.26.0`).

Nothing is removed when the reference stops: it ran against the
operator's own state, and what it wrote there belongs to them.

### 硬约束

1. **The reference serves its web app only in the hub subclient mode.**
   A plain standalone instance answers `GET /` with 404 — and the rig
   has nothing to serve.
2. **The web app does not boot without a secret.** The page loads, the
   bundle rejects itself with `Uncaught (in promise)`, and the screen
   stays empty.
3. **The secret is not optional once it exists.** The reference then
   refuses every call that does not present it
   (`unauthenticated: missing CSRF token`).
4. **The product name comes from the app data directory**, and it
   decides whether the app opens a product's UI or its sign-in page.
5. **The rig serves the reference's own page**, so the page's
   `__APP_CONFIG__` and the answers come from the same instance — the
   clone never rewrites them.
6. **The mock mode must stay off.** `-use_mocked_data=true` replaces
   the account, the model catalogue and the conversation index with a
   mock user, `gemini-3.1-pro` and an empty index.
7. **The gemini directory must not be overridden** unless the operator
   asks for it: the reference's own default (`.gemini` under the home
   directory) is the operator's real state, and the app data directory
   is resolved inside it.
8. **The app data directory decides whose history is served.**
   `antigravity-cli` serves the CLI's titled sessions (398 trajectories,
   360 titled); `antigravity` serves the IDE's directory, which holds
   the untitled rows — 0 conversations in the sidebar.
9. **The real endpoints are the IDE's own.** With the default API host
   (`http://0.0.0.0:50001`) the reference has no account and the app
   opens its sign-in page.
10. **The reference owns its app data directory.** Running it against
    the operator's real state means it *writes* there — the summaries
    file and the conversation database are rewritten by the reference
    itself.

## 存疑

- **The reference's IDE invocation was read off a running IDE's command
  line**, not from the IDE's own source. The recipe the clone now uses
  is the *measured minimum* that makes the page open, the app boot and
  the data real; the IDE passes the same flags, and the two were
  compared against each other, but not flag by flag for every one of
  them (the endpoints and the override flags were measured together).
- **Why the IDE's app data directory lists nothing** was not traced
  into the reference's index code. What is measured: the directory
  holds 1,245 rows whose titles are empty, and the reference's
  summaries stream answers 0 trajectories for it, while the CLI's
  directory answers 398 (360 titled).
- **Whether the app *needs* the secret, or merely *prefers* it**, was
  not traced into the bundle. What is measured is the behaviour: with
  a secret the app boots, without one it rejects itself. The bundle's
  own code path was not read.
- **The summaries stream grows while the reference runs.** A fresh
  instance answers 398 trajectories for the CLI's directory; a
  long-running one answers ~1,300 (402 with a project, 77 with a
  title). Which of the two sets the app renders — the sidebar showed
  25 and "See all (84)" — was not traced.
- **The cloud backends are not available to this account.** The same
  stream with `{"backend":2}` answers `failed_precondition: cloud
  conversations are not enabled for this user`, and `{"backend":3}`
  answers `failed_precondition: enterprise remote conversations are
  not enabled for this user`. So what the rig shows is the *local*
  index only.

## Limitations

- The measurements are from **one** machine, one IDE build
  (`language_server` 1.11.x era, the binary under
  `/Applications/Antigravity.app`) and one date. A different IDE
  release may want a different recipe — the flags are the reference's
  own, but their necessity was measured *here*.
- **The account is the operator's**, taken from the keychain: the
  reference answers with whatever identity is stored there, and the
  identity seen during the measurement changed between two runs
  (two different `@gmail.com` accounts). The *shape* of the answer
  (a real user, a Pro plan, the real catalogue) is what was measured.
- The **websocket** transport of the reference (the app's own state
  stream) was not compared against the clone's on this path; the app
  boots and renders, which exercises it only as far as the UI's own
  first screen.
- The **`-app_data_dir` name** is the reference's own; the clone passes
  it through unchanged and does not create or migrate anything itself.
- The rig's page is the **reference's own page**, so any difference
  between the clone's UI and the reference's UI is out of scope here:
  this cycle is about the page opening at all, and about the data
  behind it being real.

## Files

| | |
|---|---|
| `internal/refbackend/spawn.go` | the spawn recipe: the hub mode, the app data directory, the minted secret, the IDE's endpoints |
| `internal/refbackend/spawn_test.go` | the flags pinned, the mock *off*, the gemini dir not overridden |
| `out/refbackend-spawn/spawn_matrix.sh` | the matrix, one flag at a time, from the reference's own answers |
| `out/refbackend-spawn/spawn_matrix.txt` | its captured output |
| `out/refbackend-spawn/real_data_matrix.sh` | the same matrix with the mock off: account, catalogue, index |
| `out/refbackend-spawn/real_data_matrix.txt` | its captured output |
| `out/refbackend-spawn/count_summaries.py` | counts the trajectories in the reference's summaries stream |
| `out/refbackend-spawn/browser_boot.txt` | the instances in a browser: the page, the console |

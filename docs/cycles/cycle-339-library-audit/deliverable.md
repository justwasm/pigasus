# Where this repository could stop reinventing the wheel — an audit

**Cycle:** `cycle-339-library-audit`
**Subject:** "which parts of this project could be built on an existing
library instead of a hand-rolled one?" — answered by reading the whole
tree, not by guessing from the dependency list.
**Method:** every package under `internal/` and `cmd/` was read for
hand-rolled algorithms; every candidate was checked against `go.mod`,
the module graph, the local module cache, and the **reference binary's own
dependency set** (`docs/cycle38-deps-alignment.md`, 【strings】 evidence
from the 1.3.4 binary).

---

## 0. The short answer

Four numbers frame everything.

| | |
|---|---|
| non-test source under `internal/` + `cmd/` | **248,220 lines** |
| of which: packages whose doc comments pin them to the reference's bytes (`parity`, `1:1`, `recovered from`, `decompiled`, `byte-for-byte`) | **353,036 of 377,695 lines incl. tests — ~93%** |
| hand-rolled code that an existing library could replace | **≈ 1,340 lines — ~0.5%** |
| code that duplicates *another copy inside this repository* (dead or parallel) | **≈ 900 lines** |

The repository is already library-backed almost everywhere it can be. The
big names are all third-party: `charm.land/bubbletea/v2` + `bubbles/v2`
(the TUI), `charm.land/glamour/v2` + `goldmark` (markdown),
`alecthomas/chroma/v2` (highlighting), `coder/websocket`,
`creack/pty`, `fsnotify`, `ncruces/go-sqlite3`, `zalando/go-keyring`,
`google/renameio/v2`, `golang.org/x/oauth2`, `google.golang.org/protobuf`
(including `dynamicpb` + `protojson` against a recovered descriptor set),
`mvdan.cc/sh/v3` (the reference's own shell parser), `charmbracelet/x/ansi`,
`rivo/uniseg`, `mattn/go-runewidth`, `aymanbagabas/go-osc52`.

What is left is ~1.3k lines of library-replaceable code plus ~900 lines
that duplicate a copy *inside this repository*, and **the deciding
question is never "is there a library?" but "does the reference itself use
one?"**.

---

## 1. The rule that decides every case

`docs/cycle38-deps-alignment.md` is the repository's own dependency
policy, and it is a **1:1 alignment with the reference binary's dependency
set**, not a free-for-all. The 1.3.4 binary's third-party set was
recovered from its string table; the clone's `go.mod` was then made to
match it — cobra and pflag were *deleted* because the reference does not
use them, `muesli/reflow` was *added* because the reference does.

So a library swap falls into exactly one of three classes:

| class | test | verdict |
|---|---|---|
| **A — the reference uses it too** | the library is in the reference's dependency set | the swap is **alignment**, not invention: `charmbracelet/x/ansi`, `muesli/reflow`, `muesli/termenv`, `golang.org/x/sync` (errgroup + singleflight), `zalando/go-keyring`, `aymanbagabas/go-udiff` |
| **B — the standard library already has it** | `go doc` finds it | free: `hash/fnv`, `strconv`, `slices`/`maps`, `encoding/json`, `google.golang.org/protobuf/encoding/protowire`, `google/renameio/v2` |
| **C — the reference does not have it** | not in the reference's set | **do not add**: `sahilm/fuzzy`, `agnivade/levenshtein`, `modelcontextprotocol/go-sdk`, `connectrpc.com/connect`, `sourcegraph/jsonrpc2`, `r3labs/sse-client`, `howett.net/plist` |

Class C is not a hard ban — it is a real cost: every added module is a
dependency the reference binary does not have, and this repository's whole
premise is that the clone's behaviour and dependency set should match it.

**Class C has exactly one exception**, and it is not about the library but
about the *code*: where the clone's surface is its own invention, there is
no reference byte to preserve, so a new library is allowed to enter. The
browser surface is that one case — see §4.

---

## 2. Class B — free wins (stdlib or an already-declared dependency)

These need no new module and no behaviour change.

| # | Where | What is hand-rolled | Replace with | Note |
|---|---|---|---|---|
| B1 | `internal/cli/narration_catalogue.go:98-121` | FNV-1a-64 inlined: the offset basis `0xcbf29ce484222325`, the prime `0x100000001b3`, the multiply-xor loop | **`hash/fnv`** (stdlib) | The comment says *"pulling in hash/fnv for one call site would be overkill"* — but `hash/fnv` is **the standard library**, and `internal/mdrender/fnv_cache.go:57` and `internal/builtinsubagent/image_generator.go:46` already import it. Byte-identical output. |
| B2 | `internal/implicit/implicit.go:284-320`, `internal/auth/compat_helpers.go:12`, `internal/convstore/fork.go:16`, `internal/persistentrc/instance_id.go:119` | **four** separate UUIDv4 implementations plus a UUIDv5 and an `isUUID` validator | **`github.com/google/uuid`** (already in `go.mod` as *indirect*, in the local cache) | `uuid.NewSHA1` is bit-identical to the hand-rolled v5; `uuid.NewRandom` to the v4. The ids are documented as opaque (*"the reference's ids are random, so there is nothing to match"*). |
| B3 | 17 files (`internal/remotecontrol/*_store_fs.go` ×7, `internal/models/{edit,providers}.go`, `internal/cli/{dispatch_mcp,plugin_cleanup_state}.go`, `internal/accounts/store.go`, `internal/agyproxy/fileconfig.go`, `internal/rchost/rctoken.go`, `internal/tools/filesystem_manage.go`, `internal/auth/antigravity_token.go`) | temp-file + `os.Rename` atomic writes, ~300 lines | **`github.com/google/renameio/v2`** (a **direct** dependency) | It is currently used in exactly **one** place — `internal/keyring/keyring.go:185` — and that package has **zero importers** (see C-notes below). Use `renameio.WithStaticPermissions(0o600)` for the credential files: the default `WithExistingPermissions()` would change the mode semantics. |
| B4 | `internal/convstore/protobuf.go:25-116` + `internal/convstore/write.go:398-416` | two hand-rolled protobuf varint/tag codecs | **`google.golang.org/protobuf/encoding/protowire`** (direct dep) | `protowire` emits byte-identical varints and tags, so the stored blobs do not change. **The reader half of `internal/implicit/protobuf.go` is the exception** — it deliberately preserves unknown fields so a trajectory the reference wrote can be rewritten without losing what the clone does not model; `protowire` has no such mode (see §5). |
| B5 | `internal/mcpoauth/signin.go:300` | a hand-written JSON serialiser, key by key, "so two stores with the same records produce identical bytes" | **`encoding/json`** | `json.Marshal` already sorts map keys and emits compact output — byte-identical, ~30 lines deleted. |
| B6 | `internal/lifecyclehooks/proto.go:356` | a hand-rolled insertion sort for map keys, *"keeps the dependency surface at zero"* | **`slices.Sorted(maps.Keys(m))`** (Go 1.23+; the module is `go 1.26.2`) | ~10 lines deleted. |
| B7 | `internal/permissions/policy.go:350`, `internal/taskregistry/taskregistry.go:397` | two hand-rolled `itoa` | **`strconv.Itoa` / `strconv.FormatUint`** | |
| B8 | `internal/diffaction/diffaction.go:30-46` | a regex ANSI stripper (`\x1b\[[0-9;?]*[a-zA-Z]`) | **`ansi.Strip`** (direct dep; already used at `internal/tuiviewport/selection.go:47`) | The regex only strips CSI; `ansi.Strip` also removes OSC/DCS. On a `git --no-color` body the two agree — but the function's doc cites a 1.3.4 changelog regression it fixes, so the behaviour has to be re-pinned. |
| B9 | `internal/agyproxy/passthrough.go:49-179` | a hand-rolled reverse proxy: header copying, hop-by-hop filtering, streaming | **`net/http/httputil.ReverseProxy`** (stdlib) — **already used in this repo** at `internal/rctunnel/proxy.go:69` | The proxy must stay *verbatim* (exact method, path, headers, status, unbuffered body), and `ReverseProxy` adds `X-Forwarded-For`, rewrites `Host`, and answers errors with its own 500. The capture layer becomes a `ModifyResponse` hook. POSSIBLE-BUT-RISKY; the in-repo precedent shows the clone is comfortable with it. |

---

## 3. Class A — the reference uses the same library

| # | Where | What is hand-rolled | Replace with | Risk |
|---|---|---|---|---|
| A1 | `internal/auth/keychain_darwin.go` (~120 lines) | hand-written `/usr/bin/security find-generic-password` / `add-generic-password` calls, plus the `go-keyring-base64:` prefix handling | **`github.com/zalando/go-keyring`** (a **direct** dependency) | **The single best find in this audit.** The file's own comment says *"Real agy binary uses github.com/zalando/go-keyring with service `gemini` and account `antigravity`"* — go-keyring's macOS backend does exactly what the file re-implements, byte for byte on disk. Two deltas to handle: go-keyring's `Get` decodes the `go-keyring-base64:` prefix itself (the manual stripping must go), and its `Set` feeds the payload over **stdin** whereas the clone passes it on the **argv** (`-w <token>`) — the swap closes a small `ps`-visible leak of the OAuth token. Keep it darwin-only: `keychain_other.go` deliberately stubs the other platforms. |
| A2 | `internal/tuilib/text.go:58-120` + `internal/tuilib/text_escape.go` (~100 lines) | an ANSI-aware hard wrap (`WrapRows` + `CellByteIndex`) and a hand-written CSI/OSC/DCS escape scanner | **`charmbracelet/x/ansi`** — `ansi.Hardwrap`, `ansi.DecodeSequence` (direct dep) | `internal/loginui/wrap.go:63` already made this exact swap (`ansi.Wrap`), so the precedent is in-tree. `ansi.Hardwrap` drops leading spaces on wrapped lines and measures with its own width method, so the viewport's golden tests must be re-pinned. Note: the **reference embeds `muesli/reflow`** (`ansi/`, `truncate/`, `wordwrap/`), so `muesli/reflow` — already in the module graph via `vito/midterm`, in `go.sum`, and in the local cache — is the parity-aligned choice; `x/ansi` is the cheaper one. |
| A3 | `internal/pluginsui/render.go:161`, `internal/configpanel/configpanel.go:365`, `internal/skillsui/detail_table.go:106` | **three** independent greedy word wraps — two of them measuring in *bytes*, one in *runes*, so they disagree on CJK and two are cell-width-wrong | **one** helper over `ansi.Wordwrap` / `muesli/reflow/wordwrap` | Each surface is byte-pinned by its own tests; the win is consolidation, not a per-call swap. |
| A4 | `internal/planrender/plan_render.go:264-306` | a cell-budget wrap loop | **`ansi.Hardwrap`** | Its truncation sibling already uses `rwidth.Truncate`; only the wrap loop is hand-rolled. The plan output is compared against the AGY 1.4.0 transcript, so re-pinning is required. |
| A5 | `internal/harnesstools/diff.go:61-177` | a "common prefix + common suffix" diff — the file admits it reports a two-change file as one region | **`github.com/aymanbagabas/go-udiff`** (already used by `internal/tuitools/diff.go` and `internal/diffaction/diffaction.go`) | Not a drop-in: udiff would make the clone *more correct than the reference* for multi-change files, and the hunk → `LINE_ACTION_NONE/DELETE/INSERT` mapping has to be written. |
| A6 | `internal/codeassistclient/refreshing_token_source.go:79-170` | a hand-rolled singleflight around the OAuth refresh | **`golang.org/x/sync/singleflight`** | The reference's binary carries `golang.org/x/sync` (errgroup + singleflight) and the clone does **not import it at all** — it is in `go.mod` as *indirect*. The clone's semantics deliberately differ (`With401Retry` must bail out instead of joining), so the seam survives and only the join/wait half swaps. |
| A7 | `internal/auth/oauth.go:149-282` (~135 lines) | a hand-rolled PKCE (verifier, `sha256` challenge, state) and the authorization-code exchange | **`golang.org/x/oauth2`** — `pkce.GenerateVerifier`, `pkce.S256ChallengeOption`, `(*Config).AuthCodeURL`, `(*Config).Exchange` | This code is a **clone extension**: the package doc states the reference performs no OAuth device flow and has *"no PKCE"*. `x/oauth2` namespaces its query parameters differently, so the Web-UI login URL has to be re-pinned. The fabricated unsigned `id_token` is clone-specific and stays. |
| A8 | `internal/auth/credentials.go:289-330` + `internal/auth/unified_config.go:296` | `parseRefreshError` **scrapes the HTTP status out of an error string** (`strings.Index(msg, "HTTP ")`), because the refresh failure was flattened with `fmt.Errorf`; `postTokenForm` builds the form body by string concatenation | **`golang.org/x/oauth2`** — `*oauth2.RetrieveError` carries `Response.StatusCode` and `ErrorCode` **structurally** | The string-scraping is the ugly part and it disappears. `postTokenForm` becomes `url.Values{}.Encode()`. Pin `Endpoint.AuthStyle = oauth2.AuthStyleInParams` — Google's public client puts `client_secret` in the form, and x/oauth2's default would issue an extra style-probe request, changing the wire bytes. |
| A9 | `internal/clipboard/clipboard.go:27-102` | a hand-rolled clipboard that shells out to `pbpaste`/`pbcopy`/`xclip`/`xsel`/`wl-copy`/`wl-paste` | **`github.com/atotto/clipboard`** (already in `go.mod` as *indirect*, via `bubbles/textarea`) | **The reference vendors this exact library** — 【反编译】 the recovered symbols are `google3/third_party/golang/github_com/atotto/clipboard/v/v0/clipboard.readAll` and `…clipboard.getPasteCommand`, i.e. the reference shells out to the same commands. Keep the OSC-52 branch (`internal/clipboard/osc52.go` already uses `go-osc52`, a direct dep) and replace only the shell-out half (~60 lines). One behaviour delta: `atotto` returns a `missingCommands` error where the clone returns `""` silently. |

---

## 4. Class C — a library exists, but the reference does not have it

| candidate | the place it would replace | why not |
|---|---|---|
| `github.com/sahilm/fuzzy` | `internal/modelpicker/modelpicker.go:134`, `internal/tuilib/picker.go:295` | The matching semantics were **measured from the reference**: the picker matches a *substring* (`son` must not match "Claude Opus 4.6 (Thinking)"), the model picker a *case-insensitive subsequence* (`clauson` matches "Claude Sonnet 4.6 (Thinking)"), with a specific relevance order. A generic ranker breaks the measured ordering. |
| `github.com/agnivade/levenshtein` | `internal/cli/plugin_validate_format.go:268` (22 lines) | A 22-line DP table for a "did you mean" hint is not worth a new module — and the reference does not have one. |
| `github.com/modelcontextprotocol/go-sdk` | `internal/mcp` | The reference's client **is** that SDK (【反编译】 the 1.4.7 binary carries `modelcontextprotocol/go_sdk`), so it would be parity-*aligned*. But the clone already speaks MCP through **PiG's** `mcp` package (`internal/mcp/client.go` is a thin pool over `pigmcp.Client`), and the stated reason is *"so the clone does not carry a second MCP stack"*. Swapping would mean replacing PiG's client inside the agent loop — a change to the engine, not to a helper. |
| `connectrpc.com/connect` | `internal/remotetransport` (14k lines: the Connect codec, the Connect-WebSocket codec, the gRPC-Web codec) | The **client is the reference's own bundle**, which cannot be changed: the server must speak exactly what that bundle sends, and three dialects have to coexist (Connect over HTTP, Connect over WebSocket, gRPC-Web) under the clone's own CSRF and Origin policy. The hand-rolled codec is the compatibility shim, not an implementation detail. |

### The exception: `chromedp` — the one large swap the constraints *permit*

`internal/browsercdp` (926 non-test lines: a CDP client over
`coder/websocket`, plus the Chrome-launch half — binary discovery,
`--remote-debugging-port`, the WS-URL fetch) is the largest hand-rolled
protocol client in the tree, and `github.com/chromedp/chromedp`
(v0.14.2, offline in the module cache) would replace most of it.

**Why it is permitted when `connectrpc.com/connect` is not:** the browser
surface is **the clone's own invention**. `internal/tools/catalog.go:112`
marks the browser tool descriptions *"as invented: the reference
describes these names nowhere"*, and `docs/guide/tools.md:539` records
that the reference attaches to the *user's* Chrome while the clone
launches its own headless one. Nothing here is reference-byte-pinned —
the 23 tool names, the descriptions and the result strings
(`"Navigated to %s (Title: %v)"`) are all clone-owned prose.

**Why it is still risky:** chromedp's API is context/tab/`Action`-
oriented, not `Call(method, params)`, so all 16 action methods and
`internal/tools/browser.go` (510 lines, 23 executors) have to be
rewritten; the console/network capture (`handleEvent`, 70 lines) becomes
`ListenTarget` + `cdproto`; `findChromeBinary` (45 lines) would be
replaced by chromedp's own discovery, changing behaviour on machines
with both Chrome and Chromium; and the process lifecycle moves into
`ExecAllocator`. The blast radius is the whole browser surface of the
agent. **POSSIBLE-BUT-RISKY, and the only large swap on the list.**

**What landed:** the swap is done, and the risk was smaller than the
paragraph above feared: only the *transport* moved. The launch half and
the action layer stayed the clone's own. See §10.5.

---

## 5. Where a library would *break* parity — do not touch

These look like candidates and are not. Each one is pinned by a comment
and by a test to bytes the reference produces.

* `internal/implicit/protobuf.go` — the reader preserves unknown fields so
  a trajectory the reference wrote can be rewritten without losing what
  the clone does not model. `protowire` has no such mode. (Its *writer*
  half swaps safely.)
* `internal/modelpicker`, `internal/tuilib/picker.go` — the reference's
  measured matching semantics (substring vs subsequence).
* `internal/mdrender/safe_boundary.go` — a 200-line conservative markdown
  scanner; a goldmark-AST version would be slower and *less* safe.
* `internal/render/hyperlink.go` — hand-builds the OSC 8 frame; `x/ansi`'s
  `SetHyperlink` terminates with BEL, not ST, so the bytes would change.
* `internal/persistentrc/darwin_plist.go`, `internal/auth/compat.go` — the
  reference's own plist and prototext bytes.
* `internal/implicit` AES-256-GCM — the key is the reference's literal and
  the layout is `nonce‖ct‖tag`; a KDF would break the reference's reader.
* `internal/budget`, `internal/permissions`, `internal/sandbox`,
  `internal/compaction`, `internal/rehydration`, `internal/cumulativeprompt` —
  reference formats and decompiled addresses.
* `internal/rcfrontend/assets/main.js`, `diff_worker.js` — the **reference's
  own vendored assets** (byte-identical, hash-pinned). The 250-line
  Myers/LCS diff in `diff_worker.js` is the *reference's* code, not the
  clone's.

---

## 6. The network and protocol surface, read directly

The protocol packages were read first-hand as well. The result is the
same shape: **the wire formats belong to the reference, so the libraries
that could replace the code are the ones the reference itself uses.**

| package | lines | what it implements | library already in use | a library swap? |
|---|---|---|---|---|
| `internal/remotetransport` | 14,280 | the Connect codec, the Connect-over-WebSocket codec and the gRPC-Web codec (~845 lines of it), plus the clone's CSRF and Origin policy | `coder/websocket`, `net/http` | `connectrpc.com/connect` covers the Connect and gRPC-Web *protocols*, but **not** the reference bundle's WebSocket frame dialect, and the client here is the reference's own `main.js`, which cannot be changed. It also cannot reproduce the reference's own error-code space (`-32011`/`-32018`/`-32019` -> `not_found`/404, `-32602` -> `invalid_argument`/400), nor the rule that an *unnamed* failure is masked as HTTP 200 `{}` while a *named* one answers the reference's way, nor the gRPC-Web trailers living in the **body** rather than in headers. Not substitutable. |
| `internal/backend` | 9,733 | the CloudCode / gateway / Gemini transports | plain `net/http` | Nothing to replace: it is `http.Client` plus a stream decoder. Four **SSE** readers (~230 lines of `data:`/`event:`/`[DONE]` framing across the OpenAI, Anthropic, CloudCode and Gemini dialects) could move to `r3labs/sse-client` or `launchdarkly/eventsource` — neither is in the local cache, and a generic library would not carry the per-dialect termination rules (`[DONE]`, the XSSI prefix, the deliberate `event: error` deviation from the SDK's wording). **Low value.** |
| `internal/agyproxy` | 3,581 | the local CloudCode proxy in front of the vendor binary | `net/http`, `httptest` | Stdlib already; the proxy's on-disk config and env-var names are pinned by `AGENTS.md` invariant 15. Its `passthrough.go` (179 lines) could use `httputil.ReverseProxy` — see B9. |
| `internal/rctunnel` | 3,057 | Google's own `webchannel` connect protocol | `net/http` | The protocol is Google's internal and undocumented; **no library exists** for it. Nothing to swap to. (`internal/rctunnel/proxy.go:69` already uses `httputil.NewSingleHostReverseProxy`.) |
| `internal/grpc` | 2,971 | **not** a gRPC implementation — a recovered *descriptor registry* for the reference's services, with placeholder clients (`ErrNotImplemented`, `ErrMTLSRequired`) | `net/http` | There is no gRPC wire to swap; the repository has no `google.golang.org/grpc` dependency at all. |
| `internal/localharness` | 2,798 | the SDKs' stdio+websocket wire | `google.golang.org/protobuf`, `protojson` | The 4-byte little-endian prefix and the event shapes are the reference's. The framing *is* the protocol (~85 lines); everything else in the package is already library-backed. |
| `internal/mcp` | 1,936 | the MCP tool pool | **PiG's `mcp` package** (`pigmcp.Client`, stdio + streamable-HTTP transports) | Not a wheel: it is a thin pool over PiG's MCP client. The *reference* uses the official `modelcontextprotocol/go-sdk`, so switching would be parity-*aligned* — but it would mean a second MCP stack, which the package explicitly refuses. |
| `internal/browsercdp` | 1,120 | a Chrome DevTools Protocol client **and** the Chrome launcher | `coder/websocket` | `chromedp` is the candidate, and unlike every other network surface **this one is permitted** — the browser surface is the clone's own invention. See the exception in §4. |
| `internal/remotecontrol` | 4,412 | a hand-rolled JSON-RPC 2.0 engine (~85 lines of protocol machinery) over three transports | `net/http`, `coder/websocket` | `sourcegraph/jsonrpc2` would trade "one function I can read" for a `Conn` per transport plus a `Handler` interface, and the clone's error codes and its notification-drop rule are load-bearing for the Web UI. **Low value.** |
| `internal/rcterminal` | 1,337 | a live shell on a pty | `creack/pty` | Already library-backed. |
| `internal/rcaudio` | 503 | the voice session | `net/http` | The reference's speech-to-text is **local**; there is no server-side library to swap in. |
| `internal/persistentrc` | 2,667 | the macOS launchd daemon | stdlib | Its plist is hand-written XML **on purpose**: the bytes must match what the reference's own `buildPlist` wrote. |

---

## 7. Two findings that are not about libraries

1. **`github.com/zalando/go-keyring` is a direct dependency that is not
   linked into any binary.** `go list -deps ./cmd/... | grep -i zalando`
   returns nothing. Its only intended wrapper, `internal/keyring`, has
   **zero importers** — and its `Service` constant is `"antigravity-cli"`,
   which does not even match the reference's `"gemini"`/`"antigravity"`
   pair, which is why it was abandoned. Meanwhile `internal/auth` shells
   out to `security` by hand (A1). Either adopt the library (A1) or drop
   the dependency.
2. **`internal/auth/compat.go:181` iterates a Go map to write the
   reference's `jetski_state.pbtxt`**, so the file's bytes differ from run
   to run. Sorting the keys is a two-line fix — a real bug, no library
   involved. **Fixed** (`a0274bfb`): the migrations are written in key
   order, and a test renders the same state twenty times to pin it.

---

## 8. The wheel reinvented *inside* the repository

A separate category that is not about third-party libraries at all: the
same helper written twice, in two packages, where one copy is dead.

| duplicate | where | the fix |
|---|---|---|
| the MCP `tools/list` decoder | `internal/mcp/tool_registry.go:134-327` (345 lines) and `internal/pigrt/alwaysload.go:150-282` (404 lines) — the same decoder, twice, with the same `_meta` key constant `AntigravityAlwaysLoadMetaKey = "antigravity/alwaysLoad"` in both | **Both are dead outside their own tests.** Every exported symbol of the first, and `ParseToolsList`/`SplitToolsList`/`AlwaysLoadHintsFromToolsList` of the second, has **zero non-test callers**; the live path (`loadAlwaysLoadHints`) reads the static `mcp.json` config, not a `tools/list` payload. Delete both (~750 lines, ~110 of them the duplicated part). If the dynamic path is ever wired, take the entries from `pigmcp.Client.ListTools` — **already a direct dependency** — and read `tool.Meta` instead of hand-decoding `_meta`. |
| the Connect 5-byte envelope codec | `internal/refbackend/connect.go:101,111` duplicates `internal/remotetransport/tcp_connectstream.go:51` / `connect_codec.go:149` | de-duplicate (~30 lines) |
| `${VAR:-default}` expansion | `internal/mcp/header_substitute.go` re-implements the dialect that `internal/models.ExpandEnvRef` (`internal/models/providers.go:244`) already covers | de-duplicate |
| the backoff schedule | written **four** times: `internal/retry/backoff.go`, `internal/backend/gemini_api_key_retry.go:352`, `internal/piagentbridge/stall.go:193`, and (a different concept that reads like one) `internal/retrybudget/retrybudget.go` | consolidate into `internal/retry` — a refactor, not a library swap. The *numbers* (30 s cap, 1 s initial, ×2, 5 attempts) are pinned to AGY changelog entries, so no third-party backoff library fits. |
| the ANSI stripper | `internal/tools/ansi.go:33` (the reference's byte filter) and `internal/cliapp/slash_regression.go:326` | one owner; the reference-faithful filter is the one to keep |
| the macOS keychain | `internal/auth/keychain_darwin.go` (live) and `internal/keyring/keyring.go` (dead) | see A1 and §7 |

See §10 for what actually happened to each of these — three of the rows
above did not survive contact with the code.

---

## 9. Dependency hygiene (free, no code)

```
$ GOPROXY=off go mod tidy -diff
+ github.com/aymanbagabas/go-udiff v0.4.1     # used directly by internal/diffaction, internal/tuitools
+ github.com/tidwall/pretty v1.2.0           # used directly
```

`go.mod` is untidy: two modules that are imported directly are still
marked `// indirect`. Every module involved is already in the local cache,
so `go mod tidy` runs offline.

**Fixed** — `go.mod` is tidy now: `github.com/aymanbagabas/go-udiff` and
`github.com/tidwall/pretty` are direct requirements, and the swaps in this
audit moved `github.com/google/uuid`, `github.com/atotto/clipboard`,
`golang.org/x/oauth2` and `golang.org/x/sync` out of the `// indirect`
block with them. (`go.sum` carries a large unrelated diff in this tree;
see Limitations.)

Two more direct requirements — `github.com/charmbracelet/x/term` and
`github.com/muesli/cancelreader` — are imported **only** by `scratch/`,
the package that has never compiled (`AGENTS.md`: *"Never `go test ./...`
— `scratch/` has never compiled"*). They are direct because of
`docs/cycle38-deps-alignment.md`'s policy, not because the shipped binary
uses them.

---

## 10. What landed

The audit was written first and then implemented, item by item, each item in
its own commit. This section is the record of what shipped, what changed on
the way, and what was deliberately not done.

### 10.1 The swaps — all of them

| # | landed as | commit | note |
|---|---|---|---|
| B1 | `hash/fnv` | `9f1c0ed6` | byte-identical |
| B2 | `github.com/google/uuid` (v4 + v5 + the validator) | `9f1c0ed6` | four implementations → one |
| B3 | `github.com/google/renameio/v2` behind a new `internal/atomicfile` | `b72c352b` | **the wrapper is not optional**: `renameio.WriteFile` cannot force the mode, and the credential files must be `0600` exactly. `internal/atomicfile.WriteFile(path, data, perm)` is the seam, and a test asserts the mode. |
| B4 | `google.golang.org/protobuf/encoding/protowire` | `1a22f698` | the stored blobs do not move |
| B5 | `encoding/json` | `1a22f698` | ~30 lines deleted; the byte-for-byte promise holds because `json.Marshal` sorts map keys |
| B6 | `slices.Sorted(maps.Keys(m))` | `1a22f698` | |
| B7 | `strconv` | `1a22f698` | two `itoa`s |
| B8 | `charmbracelet/x/ansi`'s `Strip` in `internal/diffaction` | `1a22f698` | the diff path's pinned cases still pass |
| B9 | `net/http/httputil.ReverseProxy` | `64d5cf51` | the three deviations the clone needs (the already-read body, the dropped `Accept-Encoding`, the restored forwarding headers) live in the `Rewrite` hook |
| A1 | `github.com/zalando/go-keyring` | `2b786d52` | **the token no longer appears on a command line** — go-keyring's macOS backend feeds `security` over stdin |
| A2 | `charmbracelet/x/ansi` (`DecodeSequence`, `Hardwrap`, `FirstGraphemeCluster`) | `bcbc21cb` | three hand-written scanners deleted; a sentinel bug in the cell index went with them |
| A3 | one new `internal/textwrap` over `ansi.Wordwrap` / `ansi.Wrap` | `cad07a3b` | three surfaces → one helper; the skills table's metrics became cell-aware |
| A4 | `ansi.Hardwrap` in `internal/planrender` | `369a7504` | |
| A5 | `github.com/aymanbagabas/go-udiff` in `internal/harnesstools` | `df7f4687` | `Diff` now returns one block per hunk; the single-change output is byte-identical |
| A6 | `golang.org/x/sync/singleflight` | `8a5d46e8` | only the join/wait half swapped — the "a refresh is running" probe stays an atomic counter, because `With401Retry` has to bail out instead of joining |
| A7, A8 | `golang.org/x/oauth2` (`pkce.GenerateVerifier`, `S256ChallengeOption`, `AuthCodeURL`, `Exchange`, `*oauth2.RetrieveError`) | `ed4efcce` | the authorization URL is **byte-identical** to the hand-rolled builder; `Endpoint.AuthStyle = oauth2.AuthStyleInParams` keeps the wire bytes; `internal/auth/unified_helpers.go` is gone |
| A9 | `github.com/atotto/clipboard` | `4e75cfa7` | the OSC-52 branch and its seams stay |
| the dead copies | `internal/mcp/tool_registry.go`, `internal/mcp/always_load_test.go`, `internal/keyring/` | `8853157d` | the unlinked direct dependency behind `internal/keyring` is gone with it |

### 10.2 The de-duplications

| duplicate | landed as | commit |
|---|---|---|
| the exponential backoff | `internal/retry.Step(attempt, initial, cap)`; `internal/retry`, `internal/backend` and `internal/piagentbridge` all call it | `b1879021` |
| the Connect 5-byte envelope | `internal/remotetransport.ConnectEnvelope` / `ReadConnectEnvelope`; `internal/refbackend` calls them | `90e18d06` |
| the `${VAR:-default}` scanner | the new `internal/envref` package (`Expand`, `ExpandWhole`) | `f69ec05a` |
| the map-order bug in the jetski state file | the migrations are written in key order | `a0274bfb` |

### 10.3 Three rows of the audit that did not survive contact with the code

1. **The ANSI stripper is not a duplicate.** The audit paired
   `internal/tools/ansi.go:33` with `internal/cliapp/slash_regression.go:326`
   — but the latter is a one-line delegation to `diffaction.StripANSI`, and
   after B8 that one *is* `ansi.Strip`. So the question became "should the
   command-output stripper become `ansi.Strip` too?". It was measured
   instead of assumed:
   * all **14** pinned cases of `internal/tools/ansi_test.go` agree;
   * **42,314 of 200,000** random escape-token strings differ — all of them
     malformed sequences (the VT parser swallows the DCS / SOS / PM / APC
     string states, the hand-rolled filter leaks their payload);
   * and the decisive one: **`ansi.Strip` drops invalid UTF-8 bytes**
     (`"\xff\xfe"` → `" "`), while the tool path's filter is a *byte*
     filter. Cycle 277's evidence says so in as many words: *"Everything
     that is not an escape sequence survives untouched"* — and command
     output can be binary.

   So the two strippers have different semantics on purpose: the tool path
   keeps the reference's byte filter, the diff path uses the library. The
   reference's binary does link `ansi.Strip`
   (`out/seeds/funcs.txt:144061` 【一手】), which is why the diff path
   moved — but the tool path's evidence points the other way.
2. **The backoff was written three times, not four.** `internal/retrybudget`
   tracks the *cumulative* wait of a turn; it does not schedule one. The
   three schedules that did exist now share `internal/retry.Step`, and the
   numbers did not move (1 s / 2 s / 4 s / 8 s / 16 s / 30 s).
3. **`muesli/reflow` is not in the local module cache** (v0.3.0's zip is
   missing), so the A2/A3 work went onto `charmbracelet/x/ansi` — which
   turns out to be the *better* answer anyway: the reference embeds **both**
   libraries (【一手】 `out/seeds/funcs.txt:143970-144094`:
   `ansi.Hardwrap`, `ansi.Wordwrap`, `ansi.Wrap`, `ansi.DecodeSequence`,
   `ansi.StringWidth`, `ansi.Truncate`, `ansi.Strip`), and x/ansi's
   `Wordwrap` is what the three surfaces' own primitives were approximating.

### 10.4 One copy of the `tools/list` decoder was kept on purpose

The audit proposed deleting *both* copies of the always-load decoder. The
`internal/mcp` copy is gone (`8853157d`). The `internal/pigrt` copy stays,
because after the deletion it is the **only** copy — and it carries the
reference's `_meta` semantics (`docs/cycles/cycle-249-mcp-alwaysload/`),
which is knowledge this repository keeps on purpose.

That decision surfaced a **new finding**: the always-load feature is
**inert**. `Runtime.alwaysLoad` is filled by `loadAlwaysLoadHints` and read
by nothing but the three accessor methods, which have no non-test callers;
the dynamic channel (`Options.AlwaysLoadToolsByServer`) has no production
writer, because PiG's MCP extension does not expose a tool's `_meta`. So an
MCP server that marks a tool `antigravity/alwaysLoad` gets **no** pinned
tool in the clone. That is a parity gap, not a library problem, and it
wants its own cycle.

### 10.5 The one large swap: `chromedp` — landed

`internal/browsercdp` now speaks the DevTools Protocol through
`github.com/chromedp/chromedp` v0.14.2. The hand-rolled protocol client
— the websocket, the message ids, the pending map, the event pump, the
raw JSON event decoding — is gone. What is left is the clone's own
*policy*: which Chrome to run, how to launch it, which env gates it, and
the 16 action methods' CDP calls and result strings.

**What stayed on purpose**

- **the launch half.** `findChromeBinary`, the `--headless=new` flag set,
  the temporary user-data dir, the `CHROME_BIN` /
  `CHROME_REMOTE_DEBUGGING_PORT` / `PIGASUS_ENABLE_BROWSER` gates and the
  "already running on 9222" path are the clone's own behaviour.
  chromedp's `ExecAllocator` would have replaced all of it with a
  different policy (its own binary discovery, its own flags, its own
  process lifecycle) — the audit's own warning about exactly this.
- **the action layer.** Every action still sends the same CDP method with
  the same parameters and returns the same string
  (`"Navigated to %s (Title: %v)"`, `"Clicked pixel (%d, %d)"`, …). The
  layer now talks to a `Caller` interface (`Call` + `Evaluate`) instead of
  a concrete `*Client`, which is what makes it testable without a
  browser.
- **the console and network capture.** The same two event streams, the
  same line format (`[LOG] hello`, `[WARNING] …`), the same
  `NetworkRequest` shape. The typed `cdproto` events replace the raw JSON
  decoding, and a console argument is still rendered as the *value*, not
  as its JSON text.
- **the protocol error text.** The client re-renders chromedp's
  `*cdproto.Error` as `cdp %s: %s (code %d)`, the clone's own format.

**What changed**

- The client attaches to a **page**, so the page-scoped commands carry a
  session id. **The old client sent them on the browser connection, where
  Chrome answers `'Page.navigate' wasn't found` (-32601).** Measured
  against a real Chrome, not read off the code: every page-scoped browser
  tool was broken — `Page.navigate`, `Runtime.evaluate`, `Input.*`,
  `Page.captureScreenshot`, `Emulation.*` — and only
  `Target.getTargets` (`list_browser_pages`) worked. The `*.enable` calls
  failed silently, because their errors were discarded
  (`_, _ = c.Call(...)`). The console and network capture never fired at
  all, for the same reason: page events need a session.
- The routing is explicit: `Browser`, `Target`, `SystemInfo` and
  `Tracing` answer on the browser connection, everything else on the page
  session. A page-scoped method that Chrome does not know is retried once
  on the browser connection.
- chromedp's remote allocator opens **its own tab** in the browser it
  attaches to, and closes it again when the client closes. For the clone's
  own headless Chrome that is a second tab next to `about:blank`; for a
  user's Chrome it is a tab the client owns and cleans up.

**The tests**

- `internal/browsercdp/actions_test.go` — the mock CDP server is gone. The
  action layer is driven by a fake transport that records the traffic, so
  the same results *and* the exact CDP calls are pinned (the mouse
  sequence, the wheel delta's sign, the navigation's URL).
- `internal/browsercdp/client_test.go` — the domain routing, the
  unknown-method retry, the protocol error text and the console value
  rendering.
- `internal/browsercdp/realchrome_test.go` — a real Chrome, end to end:
  navigation, evaluation, the console log, the network log. It is off by
  default (`PIGASUS_BROWSER_TEST=1`).

### 10.6 What is left of the audit

Nothing in §2, §3 or §4 remains unimplemented — including the
`chromedp` exception in §4, which landed with the transport (§10.5). The
rows in §4 (the class-C candidates) stay rejected, for the reasons given
there.



1. **A library swap is only "free" when the reference's own dependency set
   contains it.** `docs/cycle38-deps-alignment.md` is the policy; every
   judgement in this audit is anchored to it.
2. **The reference's bytes are never negotiable.** A swap that changes a
   stored file, a wire frame, a rendered row or an error string is not a
   refactor, it is a behaviour change — and must be re-pinned.
3. **The clone's own page is the reference's own bundle.** `main.js` and
   `diff_worker.js` are vendored byte-for-byte; the hand-rolled diff
   inside `diff_worker.js` is the reference's code.
4. **`internal/implicit`'s unknown-field preservation stays.** It is what
   lets the clone rewrite a trajectory the reference wrote.
5. **`internal/mcp` stays on PiG's client** unless PiG's MCP stack itself
   is replaced — the point of the current choice is that the clone carries
   one MCP stack, not two.

## 存疑

- **The 93%/7% split is a keyword heuristic**, not a line-by-line
  classification: a package is counted as parity-bound if its doc
  comments mention `parity`, `1:1`, `recovered from`, `decompiled` or
  `byte-for-byte`. It is a good enough proxy to say where to look, and
  wrong at the edges.
- **The reference's dependency set is from 1.3.4**, recovered by string
  matching. The clone targets 1.4.7. A library the 1.4.7 binary added
  would not show up in that evidence. `muesli/reflow`, `x/sync`,
  `go-keyring` and `atotto/clipboard` are all confirmed present (the
  last by recovered symbol names), so the class-A findings hold, but the
  set is not exhaustive.
- **"Byte-identical" was checked by reading the library's source, not by
  running both** — with two exceptions, which *were* run as differential
  tests against the hand-rolled code (the scratch test was not kept):
  the hand-rolled FNV-1a (`internal/cli/narration_catalogue.go`) is
  bit-identical to `hash/fnv.New64a` on empty, ASCII, binary and CJK
  inputs; the hand-rolled UUIDv5 (`internal/implicit/implicit.go`) is
  bit-identical to `uuid.NewSHA1` on the same four shapes. The
  candidates that still need a real differential test before landing are
  A2 (`ansi.Hardwrap`), A4, A5 and B8.
- **The implementation phase turned most of those "still need a test"
  candidates into measured comparisons**, and the measurements changed
  two verdicts:
  * `ansi.Wordwrap` and the clone's greedy wrap differ at width 1 and in
    whitespace handling — 16,378 of 30,000 random strings, every
    difference whitespace-related. The clone's width-1 primitive is
    therefore kept as a special case inside the new `internal/textwrap`.
  * `ansi.Wrap` over whitespace-normalised input matched the old
    `MDWrapTableCell` **byte for byte on 50,000 of 50,000** random ASCII
    cases (width 1 excepted).
  * the authorization URL built by `x/oauth2` is byte-identical to the
    hand-rolled builder (a test pins the whole query string).
  * the ANSI-stripper question was settled by a 200,000-case fuzz —
    see §10.3.
- **The `chromedp` judgement rests on the clone's own documentation**,
  not on the reference's browser tools: `internal/tools/catalog.go:112`
  marks the browser descriptions as invented and `docs/guide/tools.md:539`
  records that the clone's browser client is its own. This audit did not
  decompile the reference's browser tools, so if the reference *does*
  have browser tools of its own, the exception in §4 shrinks — and the
  swap that landed (§10.5) would then owe the reference's behaviour
  instead of the clone's.
- **The dead-code claims in §8 are grep-based**: zero *non-test* callers
  of an exported symbol. A caller behind a build tag, or in a package
  this audit did not read, would not have been found. One of them turned
  out to be a **feature gap rather than dead code** — see §10.4: the
  always-load hints are parsed and stored, and nothing consumes them.
- **The "no library exists" verdicts are for the Go ecosystem as of the
  local module cache** — Google's `webchannel` tunnel protocol and the
  launchd plist shape were checked against the cache, not against a
  registry.

## Limitations

- The scan read the source; the differential tests described in §10.3 were
  run during the implementation phase, but only against the candidates
  that were landed — the rejected class-C candidates were argued from
  their source, not measured.
- The module cache was the source of truth for "is this library already
  available offline"; a module absent from the cache but present in
  `go.sum` would have been reported as "new dependency". `muesli/reflow`
  is exactly that case: it is in `go.sum` (via `vito/midterm`) but its
  v0.3.0 zip is not in the cache.
- The three subagent scans (text/diff, network/protocol, storage/crypto)
  were run against the same tree but at different moments; the tree was
  moving (another agent was working in it), so a package could have been
  read in two states.
- **The browser surface is now exercised against a real Chrome**
  (`internal/browsercdp/realchrome_test.go`, off by default). The failure
  the audit read off the code was measured on the wire: Chrome answers
  `'Page.navigate' wasn't found` (-32601) when a page-scoped command
  arrives on a browser-level websocket. What the test does *not* cover is
  a Chrome that is not the clone's own headless one — the "attach to the
  user's browser on 9222" path is exercised by the same code but is not
  in the test.
- `go.sum` in this tree is canonical (`go mod tidy`): the `chromedp` swap
  added 18 lines and removed none.

## Files

| | |
|---|---|
| `docs/cycle38-deps-alignment.md` | the dependency-alignment policy this audit is anchored to |
| `internal/cli/narration_catalogue.go` | B1, the hand-rolled FNV-1a |
| `internal/implicit/implicit.go`, `internal/auth/compat_helpers.go`, `internal/convstore/fork.go`, `internal/persistentrc/instance_id.go` | B2, four UUID implementations |
| `internal/convstore/protobuf.go`, `internal/convstore/write.go` | B4, the varint codecs |
| `internal/auth/keychain_darwin.go` | A1, the hand-rolled macOS keychain |
| `internal/auth/oauth.go`, `internal/auth/credentials.go` | A7, A8, the hand-rolled OAuth |
| `internal/tuilib/text.go`, `internal/tuilib/text_escape.go` | A2, the wrap and the escape scanner |
| `internal/harnesstools/diff.go` | A5, the prefix/suffix diff |
| `internal/codeassistclient/refreshing_token_source.go` | A6, the singleflight |
| `internal/clipboard/clipboard.go` | A9, the shell-out clipboard |
| `internal/agyproxy/passthrough.go` | B9, the hand-rolled reverse proxy |
| `internal/mcp/tool_registry.go`, `internal/pigrt/alwaysload.go` | the two dead copies of the `tools/list` decoder |
| `internal/browsercdp/` | the one large swap the constraints permit (`chromedp`) — landed, see §10.5 |
| `internal/keyring/keyring.go` | the dead package behind the unlinked direct dependency |
| `internal/textwrap/` | the one word wrap the three surfaces share (A3) |
| `internal/envref/` | the one variable-reference scanner (the `${VAR}` de-duplication) |
| `internal/remotetransport/connect_envelope.go` | the one Connect envelope codec |
| `internal/retry/backoff.go` | the one exponential backoff |
| `internal/atomicfile/` | the thin wrapper `renameio` needs for an exact `0600` (B3) |

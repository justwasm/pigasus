# PiG 0.5.1 → 0.5.2 — dependency bump and migration

## Scope

`github.com/MichaelKinsy/PiG` (the agent loop the clone runs on) moved from
**v0.5.1** to **v0.5.2**. The release notes were read first
(<https://github.com/MichaelKinsy/PiG/releases/tag/v0.5.2>), then the upgrade
was performed and every breaking change in those notes was checked against the
clone's own PiG surface.

Outcome: **the clone's PiG surface is source-compatible with 0.5.2 — no code
change was required.** The four upstream behaviour changes that reach the
clone were traced and none of them changes the wire or the event contract.
Verified by a build, the full test suite, a live print turn and a live tool
round-trip against the configured provider.

| | |
|---|---|
| Commits | `41080b7c` (go.mod re-tidy), `a87cd7b2` (the bump) |
| Binary | 111,159,826 B → **109,824,450 B** (−1.3 MB) |
| Test surface | `go test ./cmd/... ./internal/...` — green |
| Evidence grades | 【一手】 observed live on this machine · 【反编译】 read out of the module source · 【推测】 hypothesis |

---

## 1. What upstream changed

0.5.2 is a bug-fix release plus a **Pi v1.4.0 catch-up**. The breaking
changes named in the release notes, and whether the clone uses them:

| Upstream change | Used by the clone? |
|---|---|
| `chord/host`: `CreateHost` needs `Self`, `Connect` takes `ConnectOptions`, `Host.Get` waits for ready, `GetOptions.At` is gone | no — the clone does not use `chord` |
| `chord/output` (new package; the output buffer types moved out of `durable/harness`) | no |
| `durable/storage/sqlite`: `Options` → `SqliteStorageOptions`, node options split | no — the clone does not use PiG's durable store |
| `ai.TelemetryContext` → `ai.Telemetry` | no — the clone never referenced it |
| `ai.PiUserAgent` removed → `ai.FormatUserAgent` | no — the clone sets its own User-Agent |
| Go SDK: dialog methods return `any`; `ModelRegistry.Find` returns an error; `sdk.ReadonlySessionManager` replaces `sdk.SessionManager` | no — the clone does not use the Go SDK |
| `agent.SettleGate` removed from the public API | no — the clone never referenced it |
| `extension.CallLane` removed | no — the clone never referenced it |

The clone's PiG imports, and nothing else:

```
ai                              (44 files)
agent                           (37 files)
coding                          (9)
coding/extension                (4)
coding/mcpext                   (3)
coding/extension/factoryload    (2)
coding/extension/builtin        (2)
mcp, mcp/mcptest                (2)
```

`grep` for every removed symbol (`SettleGate`, `CallLane`, `PiUserAgent`,
`TelemetryContext`, `ReadonlySessionManager`, `ModelRegistry`) over
`internal/`, `cmd/`, `devutils/` returns **no hits** 【一手】 — which is why
the upgrade compiles unchanged.

## 2. The API the clone depends on, diffed

Both module versions were compared in the module cache 【反编译】:

- **`ai/provider.go` is byte-identical.** The `ai.Provider` interface the clone
  implements (`gatewayprovider.go`, `backendprovider.go`) did not change, so
  the documented contract — *`ai.Provider.Stream(ctx, transcript, opts)` does
  not receive the model*, hence `Provider.Model(label)` / `pinnedProvider` —
  still holds.
- **`agent/agent_loop.go` changed by a handful of lines**: a new
  `DefaultAgentIdentity` and one new field on the stream options. The event
  order (`Start → Text* → ToolCall* → Done|Error`), the mandatory
  `End(result)`, `StopReasonToolUse` as the only trigger for the tool batch
  and the tool declarations riding on the initial `SystemMessage.ToolsAdded`
  are all untouched.
- **`agent/agent.go` is additive**: a new `ExtensionTool` interface, an
  `Identity` option, `SetLoopTurns`, `EffectiveStreamFunction`. Nothing the
  clone calls was removed.
- **`coding/session.go` changed internally** (`detachExtensionHost`,
  `internal/changeobservers`); the public `Session`/`Runtime` API the print
  path uses is unchanged.
- **`coding/mcpext`** changed: a UTF-8 BOM strip before `JSON.parse` (the
  upstream fix #11191) and the internal `CallLane` → `jsloop.Lane` rename.
  The clone hands the agy `mcp.json` to PiG and does not parse it itself, so
  the BOM fix is a pure gain.

## 3. The four behaviour changes that reach the clone

**3.1 A client identity is now stamped on every stream request.**
`agent_loop.go` merges `ai.DefaultAgentIdentity` (PiG's own User-Agent and
provider attribution) into the stream options. The clone's providers build
their HTTP requests themselves —
`internal/piagentbridge/gatewayprovider.go:106-111` sets
`backend.GatewayUserAgent`, `internal/backend/gateway.go:590` sets the same —
so the identity never reaches the wire 【一手】. **The User-Agent is
unchanged.** Adopting PiG's identity would have been a silent wire change,
which is why it is called out in `AGENTS.md` as a "do not fix this".

**3.2 `tool_execution_end` now follows the order the tools settle** rather
than the completion order (the `jsloop` event-loop machinery). The clone's
own tools settle when they return, so for them the two orders are the same
【推测】 — the clone's tools are synchronous executors. A future tool that
yields would be the first to see a difference.

**3.3 `factoryload`'s `refreshTools` is a no-op while a factory is loading.**
The clone registers the built-in extensions into one shared
`*extension.ExtensionRuntime` and hands that same runtime to
`RuntimeOptions` — the constraint `AGENTS.md` records as load-bearing
(`TestRuntimeConnectsMCPFromAgyConfig`). Upstream's change makes the loading
state behave the way the clone already depends on 【反编译】.

**3.4 `mcpext` strips a UTF-8 BOM** before parsing the MCP config. The
clone passes the agy `mcp.json` straight to PiG, so a BOM in that file now
works where it previously failed.

## 4. Verification

| Check | Result |
|---|---|
| `go build -o /tmp/pigasus-new ./cmd/pigasus` | ok, 109,824,450 B 【一手】 |
| `go test ./cmd/... ./internal/...` | green 【一手】 |
| `go build ./cmd/...` (CI's build step) | ok 【一手】 |
| live print turn (`-p "Reply with exactly: PIG052 OK" --model minimax-m3.1-flash`) | `PIG052 OK`, exit 0 【一手】 |
| live tool round-trip (`-p "Use run_command to run exactly: echo PIG052_TOOL …"`) | the model called `run_command`, the clone's executor ran it, the result came back and the model answered 【一手】 |
| cold-cache `go mod download` with `GONOSUMDB` **unset** | exit 0, the PiG v0.5.2 zip fetched, **no checksum-database lookup** 【一手】 |

The last row matters for CI: `sum.golang.org` is **unreachable** from this
machine (a lookup for PiG at *any* version times out after 30 s), which is
what the `AGENTS.md` note about "no checksum for v0.5.1" was really about.
But the checksum database is only consulted when a module version has to be
**added** to `go.sum`; once the hash is in `go.sum`, the fetch verifies
against `go.sum` alone. So `test.yml` needs no `GONOSUMDB`, while the first
`go get` of a new PiG release does (the release workflow already sets it).

## 5. Repo-side changes

1. **`41080b7c` — `go.mod` re-tidy.** The `go.mod` was stale after the
   `cmd/pigasus → internal/` refactor: six modules the clone imports
   directly were still marked `// indirect`
   (`charmbracelet/x/term`, `creativeprojects/go-selfupdate`,
   `google/renameio/v2`, `muesli/cancelreader`, `zalando/go-keyring`,
   `golang.org/x/sys`) and five test-only dependencies of dependencies were
   missing from `go.sum`. `go mod tidy` fixes both; no version changed.
2. **`a87cd7b2` — the bump.** `github.com/MichaelKinsy/PiG v0.5.2` and
   `github.com/MichaelKinsy/PiG/extensions/sdk v0.5.2` (indirect). The
   `go.sum` diff is four lines.
3. **`AGENTS.md`** — the PiG section now records v0.5.2, the measured binary
   size, the three traps above, and the corrected checksum-database story.
4. **This report.**

## 存疑

1. **`tool_execution_end` ordering for a yielding tool.** The clone's tools
   are synchronous today, so settle order and completion order coincide. If
   an asynchronous tool is ever added, the TUI's block order would follow
   the settle order — unverified, because no such tool exists.
2. **The binary size is not attributable to the bump alone.** 109.8 MB is
   the current tree's size; the 0.5.1 → 0.5.2 delta of −1.3 MB was measured
   on the *same* tree, so the 102 MB figure in the older notes is stale for
   the clone's own growth, not for the dependency.
3. **The live turns exercised the OpenAI-shaped dialect only.** The
   configured provider on this machine is `type: openai`. The Anthropic
   dialect goes through `BackendProvider`/`StreamFn` rather than the native
   provider, and was not driven live.

## Limitations

- **The upstream contracts remain undocumented.** The four stream-contract
  facts this clone depends on (event order, the mandatory `End(result)`,
  `StopReasonToolUse`, `ToolsAdded` on the system message) were read out of
  `agent/agent_loop.go` for 0.5.2 and are unchanged — but they are still
  *undocumented upstream*, so a future 0.5.x could move them without a
  changelog entry.
- **One pre-existing test is timing-sensitive.**
  `internal/piagentbridge/stall_test.go`'s
  `TestStallWatcher_TimerResetsOnEachEvent` failed once during the full-suite
  run on this machine (it uses a 30 ms idle window against a 10 ms note
  loop, so a scheduler hiccup during a parallel build can exceed it). It
  passed in three subsequent runs of the package and under six background
  CPU hogs 【一手】. It is **clone code with no PiG dependency**
  (`internal/piagentbridge/stall.go`), so the failure is not caused by this
  upgrade; it was left alone rather than papered over here.
- **Only the `ai`/`agent`/`coding`/`mcp` surface was diffed.** PiG's other
  packages (durable, chord, the CLI) were not reviewed beyond noting that the
  clone does not import them.

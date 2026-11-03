# The CloudCode proxy

> The instrument: it runs the *official* `agy` against a local proxy so its traffic can be captured.

## The CloudCode proxy (`internal/infra/agyproxy`, cycle 274; merged in `wrapper-merge`, flattened in `wrapper-flatten`) — custom models + request capture

A local CloudCode proxy in front of the *official* `agy`. It does not reimplement
`agy`; it launches the **real** binary with `CLOUD_CODE_URL` pointing at the proxy,
so the official `agy` can use models it does not ship with (first target: **MiniMax
M3**), and so every request/response pair can be captured for reversing prompts and
tool-call formats.

**Two cycles flattened it into the clone.** `wrapper-merge` moved the code out of
`cmd/agy`; `wrapper-flatten` dissolved the wrapper as a *product*: there is no
second binary, no `wrapper` namespace, and no second copy of the registry CLI.
What survives is the **instrument** — the only way to observe the official
binary's wire traffic, and the only way to run the official binary against the
configured providers. The clone's own transport (`internal/infra/backend`) already
speaks CloudCode *and* OpenAI/Anthropic natively, so the proxy has nothing to
teach the clone; it is the *reference* the clone is measured against.

```
pigasus agy [args…]                    the vendor agy behind the local proxy
pigasus proxy doctor|log|capture|balance|aggregate|version
pigasus provider …, pigasus model …     the registry (canonical; `pigasus providers` is an alias)
```

`pigasus agy` is dispatched in `cmd/pigasus/main.go` **before the parser exists** —
the vendor binary must see its command line untouched (`pigasus agy --help`
prints the *vendor's* help) and must not inherit the clone's startup side
effects. The `proxy` group is the clone's own: `pigasus --help` lists it
beside the reference's twelve visible subcommands, but `agy --help` does not
list it at all — the reference has no proxy.

The models come from the **same configuration the clone reads** —
`~/.openagy/providers.json` (cycle `provider-config-gateway`). The proxy
invents nothing: a first run creates no registry file, because a proxy that
wrote an endpoint the user never configured would advertise a model that cannot
work. It only *reads* the registry; the editor is `internal/cli`'s.

The whole design rests on one measured fact: **`agy` 1.4.2 honours
`CLOUD_CODE_URL`.** Export it and every CloudCode RPC arrives at
`<base>/v1internal:<method>`:

```
$ CLOUD_CODE_URL=http://127.0.0.1:50999 agy models
POST /v1internal:loadCodeAssist
POST /v1internal:listExperiments
```

So the vendor binary is **never patched**. The TypeScript reference
(`kelvinzer0/antigravity-cli`) did patch it; this port deliberately does not —
`~/.local/bin/agy` is SHA-256-identical before and after.

```
internal/cliapp/main_dispatch.go the `proxy` group
                                  (doctor|log|capture|balance|aggregate|version)
internal/infra/agyproxy      the proxy: HTTP server (passthrough + fetchAvailableModels
                       merge + Gemini ⇄ provider translation + JSON capture) plus
                       the launcher (ctl.go, launch.go), the admin commands
                       (admin.go), the persisted config (fileconfig.go) and the
                       log sink (log.go)
internal/infra/proxy/bin  resolve the real agy (resolved-path + inode self-exec guard)
geminicode/            Gemini CloudCode wire types
internal/features/models        provider registry (the provider-centric config)
internal/cli           the registry's only CLI (provider/model)
```

```bash
go build -o /tmp/pigasus ./cmd/pigasus
/tmp/pigasus agy                  # real agy, with registry models available
/tmp/pigasus proxy doctor         # agy path/version, registry, port, capture dir
/tmp/pigasus proxy capture on|off|dir|max|purge
/tmp/pigasus provider list        # provider CRUD → ~/.openagy/providers.json
/tmp/pigasus model list           # the models those providers serve
```

The registry editor is provider-centric: `providers add|remove|list` owns the
endpoint, the credential and the protocol; `models add|remove|list` owns the
models under a provider. `models add` **requires** `--provider` and refuses an
unknown one. **There is one implementation**: `internal/cli/provider_cmds.go`
(the clone's hidden `provider` / `model` commands). The wrapper's second copy was
deleted in cycle `wrapper-merge`; the legacy `agy wrapper providers|models`
spelling was deleted in cycle `wrapper-flatten` along with the binary that
owned it — `pigasus providers` is the alias that survives.

Five things that bite:

- **Only a literal first arg `agy` selects the launcher.** Everything else
  (`pigasus models`, `pigasus --version`, `pigasus -p …`) is the clone's own.
- **Proxy logs go to `~/.openagy/wrapper.log`, never stderr.** fd 2 is the
  terminal the TUI owns; writing request lines there smears the screen. Tail it
  with `pigasus proxy log`; `OPENAGY_WRAPPER_LOG=stderr` is the debugging escape
  hatch (and does smear). An unwritable log falls back to discarding, not fd 2.
- **The `OPENAGY_WRAPPER_*` env vars and the `wrapper.log` path kept their
  names on purpose.** They are the on-disk contract of an installed proxy;
  renaming them would silently move an existing capture directory and reset
  the persisted settings. The *command surface* was flattened, the *files*
  were not. The one exception is the proxy's own configuration file: there
  is no `wrapper.json` any more — its state is the `"wrapper"` section of
  the configuration document (`~/.openagy/config.json`), and
  `OPENAGY_WRAPPER_CONFIG` is gone with it.
- **The registry is the user's, not the proxy's.** Configure the provider
  first (`pigasus provider add …`), then the models
  (`pigasus model add … --provider …`). The proxy creates no file and
  no entry on first run.
- The self-exec guard in `internal/infra/proxy/bin` (resolved path **and** inode) is
  what stops the launcher from exec'ing itself through a symlink.
- `internal/features/models` is shared with the clone; the cycle-274 additions are
  additive (`Tools` nil ⇒ byte-identical wire), but `go build ./cmd/pigasus`
  after touching it.

**Capturing** is the reverse-engineering use. Set `OPENAGY_CAPTURE_DIR` (or
`pigasus proxy capture on`) and every pair is written as raw JSON — no re-encoding, so
prompts and tool schemas diff verbatim. One `-p` turn yields the full Antigravity
system prompt (~34 KB, `<identity>…`), all 13 tool declarations, and the raw SSE
frames. Files are unredacted by design; the directory is `0700`. Two RE findings
fell out of the captures: upstream `fetchAvailableModels.models` is a **map keyed
by model id** (not an array — the one contract deviation), and `agy` runs a
separate 520-char-prompt conversation-title call. A third came later: the ~34 KB
`<identity>…` prompt is the reference's system prompt, now reproduced byte for
byte in `internal/app/prompt/agyprompt` (cycle agy-system-prompt) — see
`docs/research/REFERENCES.md` for the external material it was checked against.

**The capture directory is held to a size ceiling** (cycle
`cycle-369-capture-rotation`): the default is **1 GiB**, and once the directory
is over it the **oldest** files are deleted. The capture is a debugging store,
and it grew without a bound — two weeks of use left a machine carrying 14 GiB
of it, every file with an unredacted OAuth token in its headers. The log file
has always been truncated at 4 MiB; the capture had no equivalent until now.

```sh
pigasus proxy capture max             # show the ceiling
pigasus proxy capture max 500MB       # set it (0 = unlimited)
pigasus proxy capture purge           # delete every captured pair
OPENAGY_CAPTURE_MAX_BYTES=256MB pigasus agy -p "…"   # one-off ceiling
```

The rotation only removes the files the proxy wrote — the name is
`<epochmillis>-<seq>-<method>.json` — so a directory pointed at something else
keeps its other files. It never runs on the response path, and it is
amortised: a prune pass is a directory scan, so it runs once per 64 MiB
written. The **first** record written after a start prunes, so a directory
that is already over its ceiling is trimmed as soon as the proxy sees traffic.
`pigasus proxy doctor` prints the ceiling on its `capture` line.

**A second user: the reference backend.** `pigasus web
--reference-backend=<language_server>` starts the IDE's own
`language_server` with `-cloud_code_endpoint` pointing at **this** proxy
(`internal/infra/refbackend/custommodels.go`). The reference reads its
catalogue from that endpoint and promotes it into the entries its own
model picker shows, so the operator's models appear there too — and a
turn that picks one is translated to the operator's provider, exactly as
on the `agy` path. With no configured model there is no proxy at all.

**A third user: the account pool.** The proxy can drive a **pool** of Google
accounts instead of the one the credential names. Two switches, both persisted
in the `"wrapper"` section of `~/.openagy/config.json`:

| switch | what it does |
|---|---|
| `balance` | one turn may be tried on another account of the pool when the account it ran on is out of quota |
| `aggregate` | the pool's quota and tier are answered as **one virtual account's** — the merged view |

```sh
pigasus proxy balance on|off      # the load balancer
pigasus proxy aggregate on|off    # the virtual unified account
pigasus proxy doctor              # both lines, plus the pool's strategy
```

`aggregate` implies `balance`, and it is reachable four ways: the admin
command, the account-routing policy (`"strategy": "aggregate"` in
`config.json`), `OPENAGY_PROXY_AGGREGATE=1`, and `pigasus agy --aggregate`.
The interceptions, the merge algorithm and the two things the mode does *not*
touch are in the account guide: [`accounts.md`](accounts.md).

Known limits before trusting a capture: the Google path is location-blocked for
this account (403 passthrough only, so Google-turn fidelity is **unproven**),
images / `thought` / compaction are not forwarded, and the Anthropic/Google
providers in `internal/features/models` still ignore `Tools`.

Reports: `docs/cycles/cycle-274/deliverable.md`, `docs/cycles/wrapper-merge/deliverable.md`,
`docs/cycles/wrapper-flatten/deliverable.md`, `docs/cycles/cycle-368-unified-pool-aggregate/deliverable.md`,
`docs/cycles/cycle-369-capture-rotation/deliverable.md`,
`docs/cycles/cycle-370-one-config-home/deliverable.md`.
Usage: `internal/infra/agyproxy/README.md`.


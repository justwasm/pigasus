# Cycle 370 deliverable: one configuration home

**Cycle:** `cycle-370-one-config-home`
**Subject:** the clone kept its configuration in **two** directories —
`~/.config/openagy` and `~/.openagy` — and the proxy's own state in a
**third** file (`wrapper.json`) inside the second one. There is one home now:
`~/.openagy`, and the proxy's state is the `"wrapper"` section of the
configuration document that lives there.
**Evidence:** 【一手】 first-hand against this machine's own configuration
directories, and the clone's own `pigasus proxy doctor` before and after

---

## 0. The short answer

| # | what | the answer |
|---|---|---|
| 1 | the defect | two configuration homes, and the clone's own files were XDG-shaped by accident — one directory to back up, one to remember, and a half-migrated installation when only one of them is copied |
| 2 | the fix | one home: `~/.openagy` (or `$OPENAGY_CONFIG_HOME`), resolved by the new public `configdir` package |
| 3 | the third file | `wrapper.json` is gone; the proxy's state is the `"wrapper"` section of `config.json` |
| 4 | who owns it | `endpoints` owns the document — it reads the section **verbatim** (`json.RawMessage`) and writes it (`endpoints.SaveWrapper`); `internal/agyproxy` still owns the *shape* |
| 5 | XDG | `XDG_CONFIG_HOME` no longer applies to the clone's own files |
| 6 | the migration | done **by hand** on this machine; the code does not read the retired home and does not migrate it |
| 7 | what moved | `accounts.json`, `config.json`, `models.json.bak`, `providers.json`, `remote-control-instance-id` — five files, and the retired directory is gone |

---

## 1. What was found

### 1.1 The two homes

【一手】 — the machine, before the change:

```
~/.config/openagy/          36 KB
  accounts.json             18,296 B
  config.json                1,128 B   ← the endpoint overrides + the account-routing policy
  models.json.bak              560 B   ← a leftover of the retired flat format
  providers.json               928 B
  remote-control-instance-id    37 B

~/.openagy/                882 MB
  captures/                  2,423 files
  wrapper.json                  44 B   ← the proxy's own state
  wrapper.log              1,251,152 B
```

Two directories, two sets of rules, and a third file for the proxy because the
proxy had its own configuration file on top of the clone's.

### 1.2 What read them

Five resolvers, each with its own copy of the convention:

| resolver | what it answered |
|---|---|
| `endpoints.Path()` | the configuration document — `$OPENAGY_CONFIG` > `$XDG_CONFIG_HOME/openagy/config.json` > `$HOME/.config/openagy/config.json` |
| `internal/models.ProvidersPath()` | the provider registry — `$OPENAGY_PROVIDERS_CONFIG` > XDG > `$HOME/.config/openagy/providers.json` |
| `internal/features/accounts/core.Path()` | the account registry — `$OPENAGY_ACCOUNTS_CONFIG` > XDG > `$HOME/.config/openagy/accounts.json` |
| `internal/infra/remote/persistent.configDir()` | the installation uuid's directory — XDG > `$HOME/.config/openagy` |
| `internal/agyproxy` | the capture directory, the log and its own configuration file — `$HOME/.openagy/…` |

The first four were XDG-shaped; the fifth was not. The proxy's log and capture
directory were already in `~/.openagy` because the wrapper era put them there
(invariant 16), and the *configuration* was in the other home. So a user who
backed up `~/.openagy` — the directory the proxy's own output pointed at —
lost the provider registry, the account registry and the endpoint overrides.

### 1.3 The third file

`internal/agyproxy/fileconfig.go` had its own `fileConfig` type and its own
`wrapper.json`, written with `atomicfile.WriteFile` at 0600, next to a
`config.json` that held something else entirely. Two writers, two atomic
writes, two files that had to agree about the same machine.

---

## 2. The implementation

### 2.1 `configdir` — the one home

A new **public** package (`configdir`, 69 lines, no `internal/` dependency):

```go
func Dir() string   // $OPENAGY_CONFIG_HOME, else $HOME/.openagy, else ""
func Path(name string) string
```

Every one of the five resolvers now goes through it. `XDG_CONFIG_HOME` is
**not** consulted: the clone's own configuration is not an XDG application,
and following it is what produced the second home in the first place.

`OPENAGY_CONFIG_HOME` is the one knob. It is also what the tests use.

### 2.2 The `"wrapper"` section

`endpoints/config.go` grew a third section and the writer for it:

```go
type Config struct {
    Endpoints   map[string]string   `json:"endpoints,omitempty"`
    AccountPool *accountpool.Config `json:"account_pool,omitempty"`
    Wrapper     json.RawMessage     `json:"wrapper,omitempty"`
}

func CurrentWrapper() json.RawMessage
func SaveWrapper(raw []byte) error
```

The section is carried **verbatim**: its shape belongs to `internal/agyproxy`,
so `endpoints` does not know what `balance` or `capture_max_bytes` mean — it
only knows that the document has a section named `wrapper`. The file keeps a
single reader and a single writer, and the owner of the shape keeps the
shape.

Both writes now go through one `updateDocument` helper, so the two sections
cannot lose each other in a read-modify-write race, and the edit is still
made on the raw document (`sjson`), so the key order and any section a newer
version wrote survive.

### 2.3 The proxy's side

`internal/agyproxy/fileconfig.go` lost its own file:

```go
func loadConfig() fileConfig {
    var c fileConfig
    raw := endpoints.CurrentWrapper()
    …
}

func saveConfig(c fileConfig) error {
    buf, err := json.Marshal(c)
    …
    return endpoints.SaveWrapper(buf)
}
```

`OPENAGY_WRAPPER_CONFIG` is **gone** — it named the file that does not exist
any more. The remaining `OPENAGY_WRAPPER_*` names (`_ADDR`, `_LOG`) and the
`wrapper.log` / `captures/` paths keep their names on purpose (invariant 16):
they are the on-disk contract of an installed proxy, and renaming them would
silently move an existing capture directory.

### 2.4 The machine's migration

Done by hand, with the file list read first:

| from | to |
|---|---|
| `~/.config/openagy/accounts.json` | `~/.openagy/accounts.json` |
| `~/.config/openagy/config.json` | `~/.openagy/config.json` |
| `~/.config/openagy/models.json.bak` | `~/.openagy/models.json.bak` |
| `~/.config/openagy/providers.json` | `~/.openagy/providers.json` |
| `~/.config/openagy/remote-control-instance-id` | `~/.openagy/remote-control-instance-id` |

The proxy's state was folded into the moved document before the move:

```json
{
  "endpoints": { … },
  "account_pool": { "enabled": true, "strategy": "quota_aware", "threshold": 0.05, … },
  "wrapper": { "balance": true, "aggregate": false }
}
```

`~/.config/openagy` was removed. A backup of it was left in
`/tmp/openagy-legacy-backup` for this session.

### 2.5 What the clone says now

【一手】 — `pigasus proxy doctor` after the migration:

```
registry    /Users/gear/.openagy/providers.json (2 models)
            minimax-m3       MiniMax M3           openai     → MiniMax-M3           …
            minimax-m3.1-flash MiniMax M3.1 Flash   openai     → MiniMax-M3.1-Flash   …
config      /Users/gear/.openagy/config.json
log         /Users/gear/.openagy/wrapper.log
capture     /Users/gear/.openagy/captures (on)
            2535 files, 903.7 MiB (max 1.0 GiB)
balance     on
aggregate   off
pool        on (strategy: quota_aware, threshold: 5.0%)
```

Every line resolves into the one home, and the state that was in the retired
files — the two models, the account-routing policy, the `balance` flag — came
with it.

---

## 3. The tests

- `configdir/configdir_test.go` — the resolution order (`$OPENAGY_CONFIG_HOME`
  over `$HOME/.openagy`) and the empty path when there is no home.
- `endpoints/config_test.go` — the new section: the writer puts it there, it
  leaves the rest of the document byte for byte, an empty value removes it.
- `internal/agyproxy/main_test.go` — **`TestMain`**: the whole package runs
  with the configuration home pointed at a scratch directory. The proxy's
  state is read and written through that home, so a test without the override
  would edit the user's own `config.json`. This is not theoretical: during
  this cycle a test run folded the user's `wrapper.json` into a
  `~/.openagy/config.json` that the test had created.

`go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...` is green at
this commit.

---

## 4. The files

| File | What changed |
|---|---|
| `configdir/configdir.go` | **new** — the one configuration home |
| `configdir/configdir_test.go` | **new** — the resolution order |
| `endpoints/config.go` | `Path()` → `configdir.Path("config.json")`; the `Wrapper` section; `CurrentWrapper` / `SaveWrapper`; the shared `updateDocument` |
| `endpoints/config_test.go` | the new section's tests; the path tests |
| `internal/agyproxy/fileconfig.go` | the state is the `"wrapper"` section; `EnvWrapperConfig` is gone |
| `internal/agyproxy/log.go` | the log path goes through `configdir` |
| `internal/agyproxy/ctl.go` | the usage text names `OPENAGY_CONFIG` |
| `internal/agyproxy/main_test.go` | **new** — the package's scratch configuration home |
| `internal/models/providers.go` | `ProvidersPath()` → `configdir` |
| `internal/features/accounts/core/store.go` | `Path()` → `configdir` |
| `internal/infra/remote/persistent/instance_id.go` | `configDir()` → `configdir` |
| `cmd/openagy-models/main.go` | the default path in the usage text |
| `docs/guide/endpoints.md` | the three sections, the new convention |
| `docs/guide/proxy.md`, `internal/agyproxy/README.md`, `docs/guide/accounts.md` | the state lives in the document |
| `docs/guide/public-packages.md` | the `configdir` row |
| `AGENTS.md` | invariants 16 and 53–56 |

---

## 硬约束 / Hard constraints

- **There is one configuration home: `~/.openagy`.** `$OPENAGY_CONFIG_HOME`
  overrides it. The retired `~/.config/openagy` was moved by hand; the code
  does not read it, does not migrate it and does not fall back to it.
- **The proxy's state is the `"wrapper"` section of `config.json`.** There is
  no `wrapper.json` and no `OPENAGY_WRAPPER_CONFIG`. The section's shape
  belongs to `internal/agyproxy`; `endpoints` carries it verbatim and owns
  the read and the write.
- **`XDG_CONFIG_HOME` does not apply to the clone's own files.** The retired
  home was XDG-shaped by accident.
- **The remaining `OPENAGY_WRAPPER_*` names and the `wrapper.log` /
  `captures/` paths keep their names on purpose** (invariant 16): renaming
  them would silently move an existing capture directory and reset the
  persisted settings.
- **The two configuration files keep their split.** `providers.json` answers
  *"which model talks to which provider"*; `config.json` answers *"which
  service does the clone itself talk to"*, *"which of my accounts serves this
  turn"* and *"what does the proxy remember"*.
- **A test must not reach the user's own home.** `internal/agyproxy` points
  the configuration home at a scratch directory for the whole package.

## Open questions / 存疑

- **The migration is not in the code, so it is not in any release.** A user
  who upgrades from an older build finds an empty `~/.openagy` and a
  populated `~/.config/openagy`, and the clone reads neither the old
  providers nor the old accounts. The user asked for a hand migration and
  for no compatibility code, so that is what this cycle does — but the
  question "what does a user upgrading from build N do?" is open.
- **`models.json.bak` was moved as-is.** It is a leftover of the flat
  provider format that was retired in cycle
  `legacy-config-retirement`; nothing reads it. It was kept because it was in
  the directory and deleting a user's file is not a migration's business.
- **The `~/.config` directory itself was left alone.** Only the `openagy`
  subdirectory was removed.

## Limitations

- **Nothing migrates the capture directory or the log.** They were already in
  `~/.openagy`, so they did not move; but the point stands that a user who
  had set `OPENAGY_CAPTURE_DIR` or `OPENAGY_WRAPPER_LOG` keeps those paths,
  and the code does not rewrite them either.
- **The `wrapper.log` file keeps growing** (1.25 MB at the time of writing)
  up to the 4 MiB cap cycle 274 put on it. That cap is unchanged.
- **The instance id did not move in the daemon's favour.** The uuid file was
  moved with everything else, so the daemon and the starter still read the
  same id — but a process that had already memoised the old path in this
  session would still be holding it.

---

Report language note: this file is English because `docs/cycles/*` is English.
The evidence files under `out/` are Chinese, per the repository's convention.

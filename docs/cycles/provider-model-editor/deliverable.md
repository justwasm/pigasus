# Cycle `provider-model-editor` — one writer, three front-ends

**Status**: implemented, `cmd/... ./internal/...` green (27 packages)
**Reference for the design**: `github.com/charmbracelet/crush` (read from
`../crush`), plus the clone's own `agy models` / `--help` parity rules
**Evidence**: `captures.md` in this directory — the live tmux capture of the
`/provider` panel (the panel and the verification probe), and the
byte-level CLI transcripts

The provider configuration was written by two programs that each rewrote
the whole document from a typed struct, and **there was no user-facing way
to edit it at all**. Adding a provider meant hand-writing JSON.

| | before | after |
|---|---|---|
| writers | `models.SaveProviders` (typed rewrite), the wrapper's CLI (typed rewrite), `cmd/openagy-models` (typed rewrite of the *legacy* file) | one `models.Editor`, three front-ends |
| a write | dropped every field the type does not model, reshuffled the key order | rewrites the one field that changed; unknown fields, key order and the legacy section survive |
| concurrency | none | a cross-process lock (`<path>.lock`, `O_EXCL`), an atomic rename, `0600` |
| the CLI | — | `agy provider list\|show\|add\|remove`, `agy model list\|show\|add\|remove` (hidden) |
| the TUI | — | `/provider`: a list, a form, and add/edit/delete |
| the wrapper | `agy wrapper providers …` (typed rewrite) | the same commands, on the same editor |

---

## 1. The writer

`internal/models/edit.go` is the only code that writes the file.

```go
ed := models.ProviderEditor()          // ~/.config/openagy/providers.json
providers, err := ed.List()
err = ed.PutProvider("gateway", p)     // the provider's own fields
err = ed.PutModel("gateway", m)        // one model under it
_, found, err := ed.DeleteModel("minimax-m3")
err = ed.DeleteProvider("gateway")
```

The document is read as an **ordered JSON object** (`internal/models/jsonedit.go`)
rather than unmarshalled into a struct, so:

- a key the clone does not model (an OAuth block, a field a newer version
  writes) survives the write;
- the key order survives the write, so a diff of the file shows the one
  line that changed;
- the legacy flat section, when it lives in the same file, survives the
  write.

Two write paths were bugs until the object was re-attached to its parent:
a mutated child (`providers.Set(id, obj)`) has to be written back into the
document (`doc.Set("providers", providers)`). `DeleteModel` originally
never re-attached the section, so the file was unchanged while the command
reported success. `internal/models/edit_test.go` pins the shape after every
mutation.

### The lock

`withFileLock` creates `<path>.lock` with `O_CREATE|O_EXCL`, waits up to
five seconds for a peer's lock, and removes a lock that is older than
thirty seconds (a writer that died between the lock and the write). The
write itself is a `0600` temp file plus a rename.

This is the same shape as crush's `ConfigStore.atomicWrite`
(`internal/config/store.go:301`): lock, read, transform, write atomically,
unlock — with crush's `flock` replaced by an `O_EXCL` sentinel so the
implementation stays portable and dependency-free.

### Merge, not replace

Every `add` merges: **a flag the user did not pass keeps the value the
file already had.** `agy provider add gateway --base-url X` changes the
endpoint and leaves the credential, the headers and the models alone.
Clearing a field is an explicit act (`--api-key ""`).

---

## 2. The CLI

`agy provider …` and `agy model …` are **hidden** subcommands: `agy --help`
lists the reference's twelve visible subcommands and nothing else, and
`agy models`'s output is byte-pinned. The commands that *change* the
configuration therefore live under their own names.

```
agy provider list                       the providers and their models
agy provider show <id>                  one provider in detail
agy provider add <id> [flags]           create or update a provider
agy provider remove <id>                remove a provider and its models

agy model list [--provider <p>]          the configured models
agy model show <id>                     one model in detail
agy model add <id> --provider <p>       create or update a model
agy model remove <id>                   remove a model
```

`provider add` flags: `--name --type --base-url --api-url --api-key
--timeout-seconds --max-retries --header name=value --model id[=upstream-id]`.
`model add` flags: `--provider --model --name --context-window
--default-max-tokens --reasoning-effort`.

Both the dash and the underscore spelling of a multi-word flag are
accepted (`--base-url` and `--base_url`), because shell users type dashes
and JSON-shaped copy/paste carries underscores.

The identifier may appear before or after the flags
(`splitPositionalID` knows which flags take a value, so a flag's value is
never mistaken for the id).

**The credential is never printed.** `provider show` renders `$NAME` as it
is written and a literal as `**** (literal, hidden)`.

---

## 3. The TUI panel

`/provider` opens a panel that lists the configured providers, the models
each one serves, and the reference's own catalogue:

```
Providers

> gateway                 openai     https://gw.example/v1/chat/completions
    minimax-m3              MiniMax M3               model=MiniMax-M3               ctx=1048576
  local                   openai     http://127.0.0.1:11434/v1/chat/completions
  builtin (Google CloudCode)
    gemini-3.8-flash-high   Gemini 3.8 Flash (High)
    ...

  /tmp/…/providers.json
Keyboard: ↑/↓ Navigate  a Add provider  m Add model  enter Edit  d Delete  esc Exit
```

Keys: `↑/↓` navigate, `enter` edits the row under the cursor, `a` adds a
provider, `m` adds a model under the cursor's provider, `d` deletes (twice
— the first press arms it, the second carries it out), `esc` exits.

The form is a list of fields; `↑/↓` move between them, `←/→` cycle a
choice field (the protocol, the reasoning effort) the way `/config` does,
`enter` saves and `esc` cancels. The text fields are
`charm.land/bubbles/v2/textinput` — the same component `/rename` uses.

**The credential is never rendered.** An environment reference (`$NAME`) is
shown as it is written, because it names a variable and not a secret. A
literal is shown as `(hidden — leave empty to keep)` and the field starts
empty; an empty field keeps the stored value.

**`/provider` is not in the picker's catalogue.** The reference's entry
count (99, and the "↓ 94 more" hint) is byte-pinned, so the command is
invocable but unlisted. `internal/tuilib.DefaultSlashCommands` is
untouched.

### Why the panel reads the file on every open

The file is shared with the CLI and the wrapper. The panel holds no copy
of it: `reloadProviderRows` re-reads it when the panel opens and after
every write. The `/model` panel is fresh for the same reason — `cli.ModelCatalogue()`
reads the file when it is called.

---

## 4. The verification

Typing a key into the configuration and getting no feedback is the failure
mode this section exists for: the key is wrong, and the only symptom is a
turn that fails later, somewhere else. Crush's API-key dialog answers the
question at the point of entry — *does this endpoint accept this
credential?* — and the clone now answers it too, in the panel and on the
command line.

### The probe

`internal/models/verify.go` is the whole of it. It makes **one** request
and reports what came back:

| the provider has… | the probe is… | it costs |
|---|---|---|
| a model | a minimal chat completion (`max_tokens: 16`, one "ping") | a few tokens |
| no model | `GET <base>/models` | nothing |

The chat completion is the same request the clone sends later, so a pass
means the endpoint, the credential *and* the model id all work. The listing
is the probe for a provider that has no model yet, and it needs no model id.

The listing URL follows the base's own convention: the OpenAI dialect's
base carries the version (`https://host/v1` → `https://host/v1/models`) and
the Anthropic dialect's does not (`https://host/anthropic` →
`https://host/anthropic/v1/models`), which is what `Endpoint()` follows.

The outcome is classified by the HTTP status, because the statuses call for
different fixes:

| the answer | the panel says |
|---|---|
| 2xx | `✓ the model answered (stop)` |
| 401 / 403 | `✗ the credential was rejected (HTTP 401)` |
| 404 | `✗ the endpoint has nothing there (HTTP 404)` |
| 429 | `✗ the endpoint is rate-limiting (HTTP 429)` |
| other | `✗ the endpoint answered HTTP 500` |
| no answer | `✗ the endpoint could not be reached: …` |

The credential is **the caller's**, not the file's: `VerifyProvider` takes
it as an argument, so the panel probes a key the user has typed and has not
saved yet. An environment reference (`$NAME`) is expanded before the
request.

### In the panel

`v` probes. In the list it probes the provider under the cursor; in the
form it probes the values on screen — the endpoint, the credential, and the
provider's model — *before* anything is written. The probe is a `tea.Cmd`,
so the modal never blocks on the network, and the answer arrives as a
message the update loop routes back to the panel.

```
Providers
> gateway                 openai     http://127.0.0.1:8099/v1/chat/completions
    minimax-m3              MiniMax M3               model=MiniMax-M3
  verifying gateway…
```

```
  ✓ gateway: the model answered (stop)
```

A save starts a probe too: the write happens, and the panel then reports
whether the provider it just wrote actually answers.

### On the command line

`agy provider test <id> [--model <id>]` is the headless twin: one request,
one line, and an exit code that says whether it worked.

```
$ agy provider test gateway
✓ gateway: the model answered (stop)
$ echo $?
0
$ agy provider test wrongkey
✗ wrongkey: the credential was rejected (HTTP 401)
$ echo $?
1
```

### The typed error

The transport's upstream failure used to be a formatted string
(`"upstream 401: …"`). It is now a typed `*models.UpstreamError` with the
same message, so the status can be recovered without parsing the text
(`models.UpstreamStatus`). Nothing that prints the message changes.

---

## 5. The other two front-ends

### The wrapper

`agy wrapper providers add|remove|list` and `agy wrapper models
add|remove|list` now go through the same editor, so the wrapper and the
clone cannot drift. The wrapper's own flags are unchanged (`-provider`,
`-base-url`, `-api-key`, `-external-model`, …), and the *merge* semantics
are new: `providers add` no longer blanks the fields it was not given.

`agy wrapper models remove` looks in the provider section first and falls
back to the legacy flat file.

### `cmd/openagy-models`

The tool was the last writer of the legacy flat file — a third write path
that could not see the provider section. It now writes the provider-centric
file through the same editor: `--provider` names the provider the model is
added under, and when that provider does not exist yet it is created from
`--api-url`, `--api-key` and `--type`, so the one-shot invocation still
works.

The `test` (smoke test) and `install-context` subcommands are unchanged.

---

## 6. What was taken from crush

`../crush` is the reference for the *design*, not for the bytes: the
reference binary has no panel for this, so there is nothing to match byte
for byte.

| crush | this cycle |
|---|---|
| `crush.json` holds a `providers` map; a provider holds its endpoint, key and models | the same shape in `providers.json` |
| a write is `sjson.Set` on the raw JSON → atomic → `flock` → reload (`internal/config/store.go:301`) | the same cycle, with an ordered JSON object instead of `sjson` and an `O_EXCL` lock instead of `flock` |
| `crush models` is **read-only**; the TUI's `/models` dialog groups by provider and marks the configured ones | `agy model list` is read-only; the `/provider` panel groups by provider and marks the built-in group |
| the dialog's `ctrl+e` opens the API-key dialog, which **verifies** the key with the provider before saving (`internal/ui/dialog/api_key_input.go`: `verifyAPIKey` → `ProviderConfig.TestConnection`, then `saveKeyAndContinue`) | the panel's `v` key verifies the endpoint and the credential the same way, and a save verifies the provider it just wrote |
| `crush`'s provider entries carry a `Disable` flag | not carried over: the clone has no use for it |

Crush's probe is a model listing for the providers it knows and a
pattern check for the ones it cannot (MiniMax, Alibaba: *"we have no good
endpoint we can use to validate the API key"*). The clone has no
provider table at all, so its probe is chosen by what the provider has: a
chat completion when it has a model, a listing when it does not.

---

## 7. Parity

Nothing the reference prints changed:

| surface | check |
|---|---|
| `agy --help` | twelve visible subcommands — `provider` and `model` are hidden |
| `agy models` | byte-pinned catalogue, unchanged |
| the picker's catalogue | 99 entries, unchanged — `/provider` is invocable but unlisted |
| `agy provider`/`agy model` | **new** surface, openagy-only |

---

## 硬约束

- **One writer.** `internal/models/edit.go` is the only code that writes
  the provider section. The clone's CLI, the TUI panel, the wrapper's CLI
  and `cmd/openagy-models` all go through it. A second implementation of
  "what a write does to the file" is a bug, not a convenience.
- **The credential is never rendered.** `models.MaskAPIKey` masks a
  literal; the panel's form field starts empty for a literal and an empty
  field keeps the stored value. The capture directory is `0700` and the
  raw captures are never committed (they carry bearer tokens).
- **The key order and the unknown fields of the file survive a write.**
  The file is edited as an ordered JSON object, not re-serialised from a
  struct.
- **`/provider` is not in the picker's catalogue.** The reference's entry
  count is byte-pinned; a listed `/provider` would change the "↓ 94 more"
  hint.
- **`agy models` stays byte-pinned.** The commands that change the
  configuration are `provider` and `model`, not `models`.
- **A write is atomic and serialised.** A temp file plus a rename, under
  a lock that another writer cannot take.
- **The probe is one request, and it never blocks the UI.** The panel's
  verification is a `tea.Cmd`; the modal stays responsive while it is in
  flight, and the answer comes back as a message.
- **The probe never prints the credential.** It reports a status and a
  one-line reason, nothing else.

## 存疑

- **The probe is a chat completion, so it costs a few tokens.** A listing
  would be free, but it is not available on every endpoint, and the chat
  completion is the request the clone will actually make. The probe is
  16 tokens; a save starts one.
- **The panel does not verify the credential on every keystroke.** Crush's
  dialog probes as soon as the key is complete; the clone probes on `v` and
  on a save, because a probe per keystroke is a request per keystroke.
- **The panel's layout is the clone's own.** The reference has no such
  panel, so the row shape, the notice line and the legend are a design
  decision, not a recovered surface.
- **The delete needs two presses.** That is the clone's own safety
  measure; nothing in the reference says anything about it.
- **`--header` is not in the panel's form.** The extra headers are
  preserved by a write (the field is not modelled by the form, so the
  stored value survives), but there is no way to *add* one from the TUI.
  The CLI can.
- **The built-in group is read-only.** The reference's six models are
  served by Google's endpoint and are not in the file; the panel lists
  them so the user sees the whole catalogue, and refuses to edit them
  ("the built-in models are selected with /model").
- **The legacy flat file is still read.** A model can live in
  `~/.config/openagy/models.json` and in the provider section at the same
  time; the provider section wins. `model remove` looks in both.
- **Print mode has no `/provider`.** `agy -p "/provider"` forwards the
  token to the model, as any unknown slash command does. The headless way
  to read the configuration is `agy provider list`, and to probe it
  `agy provider test <id>`.

## limitations

- The panel was exercised against a fixture with two providers and one
  model, on one machine, on 2026-10-23. The capture in `captures.md` is
  the whole of the live evidence.
- The TUI panel was not exercised against a *real* provider: the captures
  run `--backend shim` against a local mock endpoint, so no request left
  the machine. The write path is the same code the CLI uses, and the CLI's
  writes were exercised separately.
- The probe was only exercised against a mock that answers the OpenAI
  dialect. The Anthropic dialect's *listing* path was exercised by a unit
  test, but its chat probe was not run against a live Anthropic-shaped
  endpoint.
- The probe's failure wording is the clone's own: the reference has no
  such surface, and crush's dialog shows a spinner and a state, not a
  sentence.
- `cmd/openagy-models`'s `--provider` changed meaning: it was the wire
  protocol (`openai|anthropic|…`), it is now the name of the provider the
  model is added under. The protocol moved to `--type`. The tool is a
  development helper and is not referenced by any other code, but an old
  invocation that relied on `--provider=openai` creating an "openai"
  provider now creates a provider *named* "openai" with the `openai`
  protocol — the same outcome, by a different route.
- No `internal/referencedrift` artifact was added: the provider
  configuration is the user's file, not a version-scoped fixture.
- The three front-ends were not exercised concurrently against one file.
  The lock's behaviour under contention is covered by a unit test (two
  goroutines, one file, the result is valid JSON), not by a live race.

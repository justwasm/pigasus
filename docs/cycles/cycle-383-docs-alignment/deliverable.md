# Cycle 383 — the official-docs alignment

## The problem

The tree carries a crawled copy of the Antigravity CLI's **official
documentation** — 46 files under `official-docs/`. Until now the clone
was aligned against the *binary* (strings, decompilation, captured
output) and against the *cycle reports*, but never against the
*documentation*. The two are not the same thing: the documentation
names surfaces the binary only implies, it fixes the exact spelling of
a message, and it states the defaults the capture happened to override.

Cycle 383 therefore works in **slices**. Each slice is a **read-only
survey**: the documented surface is compared against the clone, and
every divergence lands in a ledger with a `file:line` anchor on both
sides. The ledger is the evidence; the fixes are separate tranches, so
that a behaviour change is always reviewable on its own.

| slice | surface | ledger | findings |
|---|---|---|---|
| A | the CLI's command surface | `slice-a-cli-commands.md` | A-1…A-33 |
| B | the TUI's interactive surfaces | `slice-b-tui-surfaces.md` | B-1…B-32 |
| C | the configuration surfaces | `slice-c-config-surfaces.md` | C-1…C-27 |
| D | the headless, session and SDK surfaces | `slice-d-headless-sessions-sdk.md` | D-n |

The ledgers are the **survey**; this report is the **fixes** that
closed them. A finding that is still open stays in its ledger and is
named in **存疑** below — the cycle does not pretend that a survey
ended the divergence.

## What was done

Ten fixes, each on its own branch, each green on its own, each
merged with `--no-ff`:

| commit | what it did | closed |
|---|---|---|
| `699f5636` | the reference's grant grammar, the ask tier and `toolPermission` | C-4, C-5, C-6 |
| `99260d9e` | one resolver for the global, workspace and reference MCP config files | C-8, C-9, C-10, C-11 |
| `83226341` | the modular rules directory, its frontmatter triggers and the token budget | C-15, C-16, C-17 |
| `81c2f360` | the documented `hooks.json` runtime, its six events and its decision enum | C-11…C-14 |
| `a2229f2f` | the vim engine's redo key, the VISUAL replace and the NORMAL-mode insert entries | B-10, B-12, B-13, B-14 |
| `8a20e8bc` | the quota freshness rule, the log timestamp and the stale comments find their owners | the recorded 存疑 |
| `3b75838c` | the effort vocabulary, the reference's invalid-level message and the headless `/effort` | A-1, A-2, A-3, and A-5 **withdrawn** |
| `fix/b15` | the status line's payload members and the built-in row's status cells — see `fix-b-statusline-surfaces.md` | B-1, B-2, B-3, B-4, and B-5 **partly** |
| `fix/a1213` | the `/agents` panel kills a subagent and answers the pending approval inline | A-12, A-13 |
| `fix/a19` | the `/codesearch` file viewer leaves line comments, and the pending notes travel in the reference's own wire format | A-19 |
| `fix/c22` | the model catalogue carries the three documented families, with the ids recovered from the reference binary | C-22 |
| `fix/b25` | the plugin-import report bytes are the reference's own | B-25 |
| `fix/c23` | `default-cli-project` is the default project a session runs in | C-23 |
| `fix/b30` | updater advisory lock and documented error strings | B-29, B-30 |
| `fix/c10` | MCP server properties, object-shaped env/headers, and document handling | C-8, C-10 |
| `fix/a4` | effort not adjustable message, dynamic model tiers, and 5-place slider | A-4 |
| `fix/a26` | `/keybindings` reports bindings, `/keybindings edit` launches editor | A-26 |
| `fix/a15` | `/resume` Other tab desktop conversation discovery, search preview matching, and import flow | A-15 |
| `fix/c26` | settings.json normalization, defaults owner, and marketplace plugin install | C-21, C-26 |
| `fix/d-headless-schema` | headless streaming prompt NDJSON loop, error envelopes, and schema options | D-01, D-02, D-04, D-07, D-08, D-09, D-10, D-14 |
| `fix/d-harness-mcp` | harness stdio and HTTP MCP server synthesis and config parsing | D-12 |
| `refactor/cliapp-slashactions` | extract pure slash command prompt generation and resolution to tier 4 (`slashactions`) | architectural slimming |


### 1. The permission engine takes the reference's grant grammar

`internal/core/policy/permissions` held a **loose** rule grammar: a
rule matched a tool by a `tool:pattern` pair, and the pattern was
matched as a substring. The documentation specifies something
stricter — a rule is `action(target)`, where the action is one of six
named verbs and the target is the argument the rule is about.

The policy now parses that grammar. The six actions are `command`,
`read_file`, `write_file`, `read_url`, `execute_url` and `mcp`; the
seventh verb the reference once had, `unsandboxed`, is **rejected**
with the reference's own words — `unsandboxed is no longer a valid
rule, use command() instead` — because that is the reference's
replacement text, not the clone's paraphrase.

The **ask** tier was missing entirely: the documentation describes a
three-level outcome — allow, ask, deny — and the clone had two. The
`permissions.ask` rule now reads, and the precedence is **Deny >
Ask > Allow** — an explicit deny wins over an explicit ask, and both
win over an allow.

**`toolPermission` gained its documented vocabulary.** The setting
takes four values — `always-proceed`, `strict`, `request-review` and
`proceed-in-sandbox` — and the reference's own default is
`request-review`. The clone honours the setting **when it is set**
and keeps its **allow-by-default** posture when it is absent: a
missing setting is not a policy, and inventing one would change every
unconfigured install's behaviour. `OPENAGY_PERMISSION_POLICY` still
overrides both, because it is the clone's own escape hatch.

The configuration panel's credits row was relabelled **"Use G1
Credits"**, which is the documentation's spelling; the reference's
capture had carried the clone's older label.

### 2. The MCP configuration surfaces

The clone resolved **one** MCP configuration file. The reference has
**three**, with different roots and different shapes:

| file | root member | shape |
|---|---|---|
| `~/.gemini/config/mcp_config.json` | `mcpServers` | the global registry |
| `~/.gemini/antigravity-cli/mcp.json` | `servers` | the reference's own list |
| `<workspace>/.agents/mcp_config.json` | `mcpServers` | the workspace list |

`mcp/configpaths.go` now owns all three resolvers — `GlobalConfigPath`,
`ReferenceConfigPath(agentDir)` and `WorkspaceConfigPath(workspace)` —
and `ConfigFile.MCPServers` plus `AllServers()` read across them.
`setup.InstallMCPProvider(agentDir, workspace)` is the one place that
installs a provider from a configuration file, so the agent server,
the interactive TUI and the print path all see the same servers.

A server entry gained the members the documentation lists: `cwd`,
`authProviderType`, `oauth`, `enabledTools`, `disabledTools` and
`timeoutSeconds` (plus `serverUrl` and `disabled`). `env` and
`headers` are `map[string]string`, as the reference's own files are.
The timeouts are the reference's: **300 s** to connect, **180 s** per
call.

### 3. The modular rules directory

The clone read a `rules/` directory as a flat list of markdown files.
The documentation describes a **modular** layout: a rule file carries
frontmatter, and the frontmatter's **trigger** decides whether the
rule is loaded at all.

`internal/app/prompt/agyprompt` gained three files:
`rules_frontmatter.go` (the trigger vocabulary), `rules_include.go`
(the `@[label](path)` expansion) and `rules_manifest.go` (the
`.agents/rules.json` manifest, with `inherits`, `entries`,
`include_only` and `exclude`). Four triggers exist — `always_on`,
`model_decision`, `glob` and `manual` — and **`manual` is never
loaded**, which is the documentation's rule rather than the clone's
choice.

The **token budget** for the user rules changed from 30 000 to
**20 000** tokens, counted as `len/4`.

### 4. The hooks runtime

The clone parsed a `hooks.json` and listed its events in a panel; it
did not **run** them. The documentation describes a runtime with six
events and a decision vocabulary, and the reference's own binary logs
the hook lifecycle.

A new tier-1 package `internal/core/policy/hooks` (stdlib only) owns
the whole thing: `events.go` (the six names), `document.go` (the shape
and the loader), `decision.go` (the vocabulary), `paths.go` (the
resolution) and `runtime.go` (the execution — JSON on stdin, JSON on
stdout, a per-handler timeout, the documented **30 s** default). The
shell a handler runs through is a **seam** (`Runtime.Shell`), because
the core tier cannot reach the app tier's shell; the host passes
`shell.DefaultShellArgv`.

`internal/core/policy/permissions/policy.go` maps the six hook
decisions onto its own `Verdict`: `allow` and `auto_approve` allow
(an explicit deny rule still wins over `auto_approve`), `deny` denies,
`ask` and `force_ask` prompt, and `deny_unless_prior_grant` denies
without a prior grant and allows with one. **A decision the
documentation does not name is a prompt** — never an allow.

The hooks panel now lists **six** rows, `SessionStart` appended to the
reference's own five, and its `IsTool` cell is derived from
`Event.UsesMatcher()` rather than from a hard-coded index.

The configuration path moved to the documented one:
`~/.gemini/config/hooks.json`. The clone's legacy XDG branch is
deleted.

All six events fire **live in the interactive TUI** only. They are not
armed in print mode and not armed in the agent server.

### 5. The vim engine's divergences

Four documented divergences, all in the public `vim/` package:

| | before | after |
|---|---|---|
| **B-10** | `ctrl+r` redid | `ctrl+r` is consumed and does **not** redo — it opens artifact review |
| **B-13** | redo had no key | `U` redoes (NORMAL only; VISUAL `U` still uppercases) |
| **B-12** | no VISUAL replace | `r{char}` replaces every selected character |
| **B-14** | `/` and `!` were inert in NORMAL | they insert the character and enter INSERT (NORMAL only) |

The two existing pins that asserted the **old** behaviour were
**updated, not deleted**: `vim/vim_test.go:TestUndoRedo` and
`vim/spec_test.go:TestInsertRunUndoAcrossLines` now use `U`, with the
reason written next to the assertion. The `/help` overlay's VIM golden
follows `vim/help.go` (a new `/ !` row, `u, ctrl+r` → `u, U`, the
`r{char}` description, and the item count 20 → 21).

The host's own call sites moved with the engine: with vim on, `!` is no
longer swallowed into the bash-mode marker (the engine owns it), and a
submitted vim prompt that begins with `!` runs as a shell command with
the `!` stripped. Outside vim mode nothing changes.

**B-13 and B-14 were pinned further** on branch `fix/b1314`; the tranche
report is
[`fix-b-vim-redo-insert-entries.md`](fix-b-vim-redo-insert-entries.md).
No behaviour changed there: both findings were already implemented by
`a2229f2f`, and the branch added the pins the fix had left out — the
redo stack's arithmetic (the steps come back most-recent-first, a new
edit empties the stack, an empty stack rings no bell), the insert
entries the new bindings did **not** change (`i`, `a`, `I`, `A`, `o`,
`O`), the bell an unbound key still rings, and the fact that the
character and the text typed after it are **one** undo step. The host's
half gained the round trip — `u` empties the composer, `U` brings the
text back — in `internal/cliapp/vim_redo_test.go`.

### 6. The recorded open items find their owners

Cycle 383's own sweep left a short list of **second owners** and
**stale comments**. Fix 6 closed them:

- **The two-minute quota freshness rule** was copied in two places
  (`internal/rcfrontend/accountsapi.go`,
  `internal/remotecontrol/dispatch_quota.go`). The owner is
  `accounts.QuotaFreshness` (tier 2, next to `Account.Quota`), and both
  call sites now ask it. The two copies did **not** disagree with each
  other or with the owner.
- **The log file's timestamp suffix** was spelled out in
  `internal/features/auth/compat.go`. The owner is
  `internal/core/logts`'s `FormatLogTS()` (tier 1), and the copy is
  gone — the same format string, the same bytes.
- **`internal/pigrt/skills.go`'s local `readPluginEnabled`** was a
  byte-identical copy of `plugins.Enabled`. The copy is deleted and the
  call site asks the owner.
- **Three dead exports** in `internal/ui/views/rewindpanel`
  (`StepHeight`, `RenderStepRows`, `StepCounter`) had no caller outside
  the package, so they are unexported.
- **Four stale comments** named code that no longer exists
  (`cliapp.loadedStepErr`, `internal/agent`'s `config.json` reading,
  `internal/cliapp/slash_panel_accounts.go`, `registerAccount`) or a
  surface that moved (the `/help` overlay's rows, the status line's
  error card). They now name what is actually there.

One item was **not** closed, and it is the cycle's own sharpest
question: see **存疑** below.

### 7. The effort surface

The clone's `/effort` recognised **three** of the reference's **five**
levels, and the catalogue already held all five — only the gate was
wrong.

- **`ParseEffortWord` now asks the catalogue.** The owner is
  `internal/features/models`'s closed five-word set (`models.EffortLevels`
  and `models.IsEffortLevel`), and the print mode's second, shorter list
  is gone. The `/effort` *slider* keeps its three places — the reference
  publishes three tiers for its three-tier model — but the list comes
  from `catalog.ReferenceEffortTiers()`, so the slider and the catalogue
  cannot drift apart.
- **The invalid-level message is the reference's own.**
  `Invalid effort "extreme" (valid: low, medium, high, xhigh, max)` —
  the reference's format string is `Invalid effort %q (valid: %s)` (agy
  1.4.12 offset `0x3680e49`). The formatter,
  `models.InvalidEffortMessage`, sits next to the vocabulary it prints,
  and the host's call site names it.
- **Headless `/effort` is read-only.** `agy -p "/effort low"` now fails
  with `error: /effort takes no arguments, got "high"` and exit code
  **2**; the bare `/effort` still prints the current level and exits 0.
  The message is the reference's own capture
  (`docs/cycles/model-argument-completion/captures.md:176-177`).

**A-5 was withdrawn, not implemented.** The survey had read the
reference's effort blurbs off a **string dump** and taken the dump's line
break for a newline. `strings` splits a printable run at every non-ASCII
byte: the separator is the reference's own **em dash** (`e2 80 94`, agy
1.4.12 offset `0x3740275`), and the clone's em dash was byte-identical
from the start. The ledger entry is corrected in place, and the
correction is recorded here because **a string dump is not a byte dump** —
every other finding that leans on a dump's line break deserves the same
second look.

### 8. The panel bytes the documentation named

Seven divergences of the slice-A ledger were **byte** divergences: the
clone drew the right panel with the wrong characters, or advertised a
row it did not have.

- **A-10 — the `/context` panel's "Related" hint.** The reference's own
  string is `Related: /artifact \n /skill \n /rewind` — with a
  **literal** backslash-n, verified byte-exact in the reference's
  string table. The panel renders **three dim rows**, split on that
  literal, not one row with embedded newlines.
- **A-11 — the `/agents` panel's group header.** It is
  `▸ Subagents (%d running, %d done)`. The binary's
  `# Subagents (%d total)` belongs to a **different surface** (a harness
  serializer's checkpoint message), and citing it here would have been a
  plausible-looking mistake.
- **A-22 — `Ctrl+R` opens the Artifact Review panel.** The clone's
  `/help` catalogue already said `ctrl+r   Review artifact`; the key
  did nothing.
- **A-23 — the `/help` table drops the Undo and Redo rows.** The clone
  has no undo/redo stack over the prompt's edits, and the editor tier
  owns the only undo that exists.
- **A-24 — two `/help` rows corrected** (the `alt+j` and `ctrl+k`
  descriptions) against the reference's own bytes.
- **A-25 — the `/changelog` document is memoised where it lives.**
  It is built from 180 KB of embedded markdown, and the build ran on
  every TUI draw — 511 760 ns/op and about 150 KB of garbage per frame,
  even in the common "no source" case where the parse was thrown away.
  A `sync.Once` inside `internal/ui/views/changelog` fixes it; the bytes
  are unchanged.
- **A-30 — `Ctrl+G` is an alias of `e`.**

### 9. The grant's *target* has per-action matching rules

The grant **grammar** was already correct after fix 1: `action(target)`,
six live actions, the retired `unsandboxed`. What was missing was the
**second half** — how a grant's *target* is matched against a concrete
operation. One shared prefix rule stood in for all six actions.

`internal/core/policy/permissions/matching.go` now owns the per-action
rules the documentation states:

- **`command`** — token-wise. The granted target is split into
  whitespace-separated tokens and the command is split the same way; the
  granted tokens must match the command's **leading** tokens one for
  one, each as a full word. A token may carry the reference's own
  anchored `regex:` form.
- **`read_file` / `write_file`** — a path **prefix** match, with the
  Windows path normalisation applied first.
- **`read_url` / `execute_url`** — a hostname match, and a match on a
  **subdomain** of the granted host.
- **`mcp`** — `server/tool`, `server/*` or `*`.
- **An implicit write is allowed by a read grant, and an implicit read
  is denied by a write grant.**

**Fix 9** carried the same vocabulary to the two surfaces that had kept
the retired one: `internal/remotecontrol/permissions` and the harness's
`harnessPolicy`. The remote-control package is **stdlib-only** and
cannot import the core package, so the grammar is a **deliberate
duplicate** — the action set, the three error sentences and the
`unsandboxed` refusal are the shared facts, and the two must not drift.

The reference's own sentences are quoted byte for byte:
`invalid grant string: %q`, `unknown action %q in grant string: %q` and
`unsandboxed is no longer a valid rule, use command() instead`.

### 10. Four slash-command surfaces

- **A-6 — `/plugin` has the alias `/plugins`.** The catalogue row
  carries `plugins`, so the vocabulary resolves it and the host's table
  — keyed by the canonical name only — answers it with the same handler.
  The argument rules resolve the alias **first**, so `/plugins list`
  dispatches the verb instead of opening the Plugins Manager with the
  argument swallowed.
- **A-8 — the command is `/teamwork-preview` and `/teamwork` is its
  alias.** The catalogue row is renamed, the host's key follows, and the
  echo carries the **canonical** name.
- **A-28 — the unknown-subcommand message is the reference's own:**
  `Unknown subcommand "bogus".`
- **A-31 — the exit echo is canonical.** `/settings` exits with
  `Exited /config command` — the name of the command that actually ran,
  not the alias that was typed.

### 11. The `/context` panel shows the documented breakdown

The documentation lists **thirteen** breakdown rows; the panel drew
**six**. The missing seven — Rules, Workflows, Plugins, MCP tools,
System overhead, System messages and the Checkpoint buffer — are now
rows, in the documentation's order, with the documentation's markers:

```
◉ User messages      ⛁ System prompt     ◉ System messages
◉ Agent responses    ⛁ System tools      □ Free space
◉ Tool calls         ⛁ System overhead
                     ⛁ Rules
                     ⛁ Skills
                     ⛁ Subagents
```

**Four of the rows have no count source in the clone** — System
overhead, Workflows, Plugins and MCP tools render `0`. That is recorded
as 存疑 rather than invented: the row *shape* is the documentation's,
the *numbers* are not available.

### 12. `/codesearch` filters **include**, and a result **opens**

- **A-17 — `f:`, `file:` and `path:` are includes, `-` excludes.** The
  clone treated them as excludes.
- **A-18 — Enter opens a file viewer**, and `Ctrl+G` opens the file in
  `$EDITOR`.

### 13. The `/diff` panel has its own keys

- **A-20 — `f` is the reference's "Toggle Full File"**
  (`KeyDiffFullFile`), and `Ctrl+O` stays on the fold
  (`KeyDiffExpandFold`). The whole-file body is the file on disk with
  the diff's changes **spliced** into it — unchanged regions from the
  file, hunk bodies from the diff body, removed lines inserted — and it
  carries the file's own line numbers. Over 100 000 lines or 10 MB the
  toggle is refused and the footer says why.
- **A-21 — the file view's own keys**: `n`/`N` (the next and the
  previous change, continuing into the neighbouring file's changes and
  landing on that file's **first** change), `/` (the search), `c`/`d`
  (add/edit and delete a line comment), `←`/`→` (the next and the
  previous file, each file remembering its cursor), and the **Unsent
  Comments** screen (Y sends, N exits without sending, Esc returns; the
  two keys are capital-only, so the small `y`/`n` do not answer the
  screen).

Two new packages carry the mechanics: `internal/ui/views/diff/keytable`
owns the binding ids, the hint labels and the per-view tables — the
footer is rendered from the same rows the dispatch reads — and
`internal/ui/views/diff/fullfile` owns the read, the two ceilings and
the splice.

### 14. The legacy evidence was retired, not kept

The reference moves. Every address in the 1.3.x-era reverse-engineering
artefacts was **already stale** against the clone's 1.4.12 target, and a
stale artefact that *looks* current is worse than none: it sends the
next reader to look up an address in the wrong binary.

Two sweeps retired **653 847 030 B (623.6 MiB)** and replaced them with
a **ledger**:

- **`docs/legacy-re/`** — `README.md` (the ledger, one section per
  retired tree), `recipes.md` (the rebuild commands, **verbatim**),
  `index.json` (the machine-readable index and the redirect table) and
  `extracts/` (the preserved fragments).
- **`docs/guide/legacy-evidence.md`** — the guide page that says what
  was retired, what survived and why.

What survived is exactly what a rebuild could not reproduce: the **cited
string-table windows** (five live code comments cite the 1.3.4 dumps for
*changelog-line provenance* — a history that is not recoverable by
re-dumping the current binary), the **cited decompiled C** (217 of the
1 660 `.c` files of the 1.3.5 run), the **re-anchored call graph** (the
only hand-written synthesis in the retired tree, cited by live
production code), the **1.3.5 PTY logs** (the only record of the
reference's onboarding screen, approval card and eligibility error), the
**1.3.4 provenance** (the binary identity, the OAuth client IDs, the
S2Av2/mTLS finding) and the **seed stubs**.

The retired 1.3.x artefacts were **not** registered in
`internal/infra/referencedrift/reference.lock.json`: that registry is
for artefacts the clone *depends on*, and a digest mismatch there turns
the gate permanently red — a superseded release is stale by
construction. The seven registered `out/` paths kept their **exact
bytes**.

The RE pipeline's **scripts** moved out of `out/` into **`scripts/re/`**:
`out/` does not ship in the release snapshot, and a script that ships
nowhere is a script that is gone.

### 15. `/skills reload` re-reads the skills from disk

`official-docs/cli/reference.md:47` documents the verb — "Run `/skills
reload` to reload skills from disk" — and the reference's string table
carries the literal `Reload skills from disk` next to `/skills reload`
【strings】. The clone ignored the argument and always printed the same
listing.

The reload is a **re-scan of the skill roots**, and the discovery's roots
are now units: a root that cannot be read **keeps the entries the
previous scan found for it**. The reload is best-effort — a read error
never drops a loaded skill, and it never panics.

Both entry surfaces answer the same two parts: the TUI's `/skills reload`
and the headless `agy -p /skills reload`. The bare `/skills` is
unchanged — it still opens the `/skills` panel, and the handler's own
listing keeps the bytes it had.

The verb's spelling rule (case-insensitive, trimmed) lives **with the
reload it selects**, in the skills package. The slash-command
vocabulary is untouched: it owns the commands, it has no notion of a
command's arguments, and the reference's picker advertises no argument
completion.

### 16. The composer, the editor key and the help key

- **B-20 — the `/keybindings` panel named a key the clone does not
  bind.** The block said `Ctrl+T  Edit prompt in $EDITOR` while the
  `/help` SHORTCUTS catalogue said `ctrl+g` and the host's key pipeline
  opens the prompt in `$EDITOR` on **Ctrl+G**. The reference binds "Open
  editor" to `ctrl+g` (`official-docs/cli/using.md:52`). The row now
  says `Ctrl+G`, and a test pins that the block and the catalogue name
  the same key.
- **B-21 — the composer is cleared on `esc esc`, not on one `Esc`.**
  "Type `esc esc` to clear your prompt box (when no streaming is
  active)" — `official-docs/cli/using.md:22`. The first `Esc` **arms**
  the clear and the second one empties the composer. Any other key
  breaks the pair, and an `Esc` that ends up elsewhere — closing an
  overlay, cancelling a turn, dismissing a card — keeps its old meaning
  and is **never** the second half of a pair. The arming carries the
  composer content it was armed on, so a composer that changed in the
  meantime (a submit, a history recall) is not the one the user armed.
- **B-22 — `?` opens the shortcuts overlay** whether the composer holds
  text or not (`official-docs/cli/using.md:24`). The composer **keeps
  its draft**: the overlay is a view over the session, not an owner of
  the text. The one exception is the vim engine's INSERT mode, where
  `?` is a character of the buffer — the reference's own guide scopes
  the key to NORMAL mode (`official-docs/cli/vim-editor-mode.md:73`).

### 17. The vim engine takes no count prefix

The documentation says "Commands take no count prefix, so `3dd` deletes
one line" (`official-docs/cli/vim-editor-mode.md:132`). The clone read a
leading count and threaded it into every command, so `3dd` deleted three
lines.

The count plumbing is **removed, not zeroed**: the `count`/`opCount`
fields of the parsed command, `readCount`, and the count parameter of
every motion, find, word, object and action helper are gone. A leading
digit is now an invalid key — it rings the bell and is dropped.

The `/help` VIM tab no longer advertises a count prefix; it says
`no count prefix` / `3dd deletes one line` instead.

**B-10** (`Ctrl+R` must not redo in NORMAL) and **B-12** (`r{char}`
replaces every selected character in VISUAL) were already implemented
and are pinned by `vim/official_docs_test.go` — they are reported here
as closed, with no code change.

The `vim` package is a **public compatibility surface**: no exported
name was added, removed or changed. Only the behaviour and the
unexported internals moved.

### 18. Every citation into the retired trees was repointed

The retirement removed 623.6 MiB of evidence. A citation into a
**deleted** file is a dead pointer: the reader who follows it finds
nothing and cannot tell whether the fact was invented or lost. Two
passes repointed them — the **code-side** citations (25 files) and the
**documentation-side** ones (70 files).

**A citation now names the string, not the deleted line.** The line
number was an artefact of the deleted file; the *fact* is the string.
Each comment quotes the string verbatim and names the **surviving
copy** — the pruned `strings-index.txt`, the cited window under
`docs/legacy-re/extracts/`, the live `out/local-macos/seeds/`, the
`re-docs/` report copy, the `internal/app/prompt/agytemplates/templates/`
bundle.

**History is not rewritten into fiction.** The cycle reports that
*performed* the renames, the `.mavis/plans/` files that *planned* them
and the root-level cycle deliverables keep their content; each gained a
short retirement pointer. A report that says a path was true when it was
written stays true.

Two factual corrections rode along: a deliverable called the 1.3.4
string table the "1.3.6 string table", and a plan pointed at
`docs/v135-states/cycle-44-compare.png`, which never existed in that
tree.

### 19. The status line's payload and the built-in row

Branch `fix/b15`; the tranche report is
[`fix-b-statusline-surfaces.md`](fix-b-statusline-surfaces.md). It closes
**B-1** through **B-5**, three of them wholly and two partly.

- **The payload carries `vim`** (B-1). The member existed and nothing
  ever assigned it, so a script could never read `.vim.mode`. It is now
  filled, and the mode names are the payload package's own
  (`statusline.VimModeName` — `NORMAL`, `INSERT`, `VISUAL`, `VISUAL
  LINE`), not the engine's badge spelling. The member is present **only
  when vim editor mode is enabled**, and the engine is never built when
  it is off.
- **Two of the four empty members are populated** (B-2).
  `pending_input_count` counts the queued user messages, and
  `subagents` is the registry's own list mapped into the payload's
  entry. `artifact_count` and `tool_confirmation_pending` stay **empty**
  — the clone has no artifact counter and its `pendingPermission` is a
  test-only seam — and both are 存疑.
- **`agent_state` reaches four of the seven documented states** (B-3):
  `idle`, `working`, `tool_use`, `error`. The three the clone cannot
  reach (`reviewing`, `authenticating`, `initializing`) are 存疑. The
  `error` state needed one fact the host did not have: the turn's
  outcome is now recorded as `tea.ProgramStateError` for a failed turn
  (`internal/cliapp/tui_update_stream.go`), which also changes the
  terminal's OSC 7501 report from `Done` to `Error` after a failed
  turn.
- **The built-in row carries status cells** (B-4): the agent state
  (omitted while idle), the context window's usage (`%.1f%%`) and the
  remaining AI credits. Every cell is omitted when its fact is absent.
  The documentation's rich line describes the downloadable *example
  script*, not the built-in row — 【一手】 agy 1.4.7's own built-in row
  carries the mode, the model and the effort tier and nothing else — so
  the cells are the clone's own reading of the documentation, and the
  payload carries everything a script needs.
- **The AI-credits cell is rendered but never appears yet** (B-5). The
  format string is the reference's own — 【strings】 `"AI Credits: %d"`
  in agy 1.4.12's string table — and the row renders it, but the host
  has **no G1 balance** to show (the only reader is the print mode's
  on-demand `/credits`). The plumbing a follow-up needs is 存疑, and the
  documentation's **low-quota highlight** is not implemented: the
  warning threshold is not recovered.

**The row's bytes are now pinned in the tree.** The pin used to be an
**out-of-tree** two-tree dump (cycle 381, tranche 46), so nothing in
the suite failed when the row changed; it is now
`internal/ui/views/statusrow/testdata/statusrow.golden`, rendered from
the fixture set in `internal/ui/views/statusrow/golden_test.go`. The
reference's two asymmetries — the queue mode's suffix excluded from the
built-in padding count and included in the `--agent` one, and the static
non-tty row's literal grey — are preserved.
### 20. The `/agents` panel kills a subagent and answers inline

Branch `fix/a1213`; it closes **A-12** and **A-13**.

- **A-12 — `K` kills the active subagent.** The legend advertised
  `k Kill Active Subagent` and the panel swallowed the key: a panel kind
  owns **every** key it receives (`internal/ui/panelregistry/route.go`), so
  `k` did nothing at all. The panel now names the subagent and asks the
  host; **the policy lives with the state it mutates** — the manager owns
  the tracked subagents and their states, so the manager decides whether a
  kill is allowed (`internal/app/subagent/core/kill.go`). The kill itself
  is the subagent's own: `Subagent.Kill` terminates the process and moves
  it to the killed state, so the row keeps its place in the panel and
  shows its final status.
- **A-13 — `A` approves and `D` denies from the list.** The panel's rows
  carry the pending approval, and the panel answers it. The manager owns
  the approvals it mutates (`internal/app/subagent/core/approvals.go`).

**The panel owns its keys and names the subagent; the host supplies the
seams; the manager owns the state the kill and the answers mutate.**
That is the cycle-381 rule (invariants 82 and 166) applied to this panel:
a panel that mutates a slice it does not own cannot write the result
back.

The lifecycle package's integration test moved to the subagent package:
the manager now depends on the lifecycle's queue, and the test follows
the dependency — the two formed a **test-only** import cycle, which the
architecture guard rejects (invariant 128, R5).

### 21. The `/codesearch` file viewer leaves line comments

Branch `fix/a19`; the tranche report is
[`fix-a19-codesearch-comment-flow.md`](fix-a19-codesearch-comment-flow.md).
It closes **A-19**.

The reference's `/codesearch` opens a file viewer for a result, and inside
the viewer the user can leave a note on a line of the file. The note is
marked in the gutter, and when the viewer is left the CLI asks whether
the pending notes travel to the agent. The clone had **none** of it — no
subview, no gutter and no `Unsent Comments` string anywhere in the tree.

**The wire format is the binary's, not the documentation's.** 【strings】
the format constant `<file>%s:%d:%s\n` occurs **exactly once** in agy
1.4.11 (file offset `0x359ebb2`), and the one ADRP+ADD reference to it
(`0x102bed52c`) loads the length 15 and calls the fmt helper with three
arguments — the function is `(*DiffModel).formatDiffComments`. The
documentation's schematic (`official-docs/cli/commands/codesearch.md:95`)
is `<file>:<line>: <comment>`: it drops the file's own name and puts a
space after the colon. **The binary wins** — this is a wire format.

The clone's format is therefore

```
<file>PATH:LINE:BODY\n
```

one comment per line.

Two new packages carry the mechanics:

- `internal/ui/views/linecomments` — the comment model, the 💬 marker,
  the wire format (`Format` / `FormatAll`) and the **Unsent Comments**
  screen. `Unsent Comments` is the reference's own string (offset
  `0x359ebc1`); the screen's `Y` sends and closes, `N` discards and
  closes, `Esc` returns — and the two keys are **capital-only**, so the
  small `y`/`n` do not answer the screen.
- `internal/ui/foundations/textbuffer` — the multi-line buffer the
  comment editor and the `/` search prompt share.

The **panel decides** and the **host closes**: `internal/cliapp/codesearch_host.go`
gained one seam, `SendComments`; the panel answers "send" or "discard"
and the host closes the panel and starts the turn with the wire text.

> **存疑**: the ledger filed A-19 against the `/diff` panel's comment
> flow; the `/diff` panel has **no** comment flow in this tree, so the
> shared home was **created**, not found. It has one consumer today —
> the `/diff` panel's own comment keys are **A-21**, still open.
### 22. The catalogue carries the three documented model families

**C-22.** The documentation's reasoning-model table lists ten models
(`official-docs/models.md:19-30`); the clone's catalogue carried six of
them plus the two gated Claude 5.5 rows. **Gemini 3.7 Flash, Gemini 3.6
Flash and Gemini 3.1 Pro** were absent — and the ledger was right that
the ids are the contract: a guessed id routes a turn to a model that
does not exist.

The ids were **recovered**, not guessed, and from three independent
places:

- 【一手】 **agy 1.4.2**, the vendor binary's own `agy models` output
  (`docs/cycles/cycle-274/deliverable.md` §7.3): fourteen rows, in one
  order, with these ids and these labels.
- 【strings】 **agy 1.4.12**, the current reference binary: its own tier
  table carries `gemini-3.7-flash-high`, `-medium`, `-low`,
  `gemini-3.6-flash-high`, `-medium`, `-low`, `gemini-3.1-pro-high` and
  `gemini-3.1-pro-low`, and the labels "Gemini 3.7 Flash (High)",
  "(Medium)", "(Low)", the same three for 3.6, and "Gemini 3.1 Pro
  (High)" / "(Low)". **"Gemini 3.1 Pro (Medium)" is not in the binary** —
  the Pro family publishes two tiers, not three, and the clone does not
  invent the third.
- 【strings】 **agy 1.4.11**: `gemini-3.1-pro-high`
  (`out/local-macos-1.4.11/strings-index.txt:422`) — the id survived from
  1.4.11 to 1.4.12.

The rows sit where the reference prints them: after the Gemini 3.8 Flash
block, before the Claude rows, 3.7 before 3.6 before 3.1 Pro, and the
Pro family's high before its low.

**The catalogue is static and carries no plan dimension.** The
documentation marks the three families **Enterprise-only**
(`official-docs/models.md:23-25`), and the server's own catalogue for a
free account does not carry them
(`out/refbackend-custom-models/available_models.json`, 2026-10-26). The
reference prints them when the account's server advertises them; the
clone's rows are the union of the models the reference's binary
**knows** — the catalogue already lists Claude Sonnet 4.6, which the same
table marks unavailable on Enterprise. A server that does not carry one
refuses the turn; the clone does not stand in for the server's plan.

The evidence is in `out/model-catalogue-3x/`.

### 23. Plugin import report format parity

Branch `fix/b25`; closes **B-25**.
The plugin-import report strings and spacing match the reference binary
(`[ok]    <name>`, `✔ <component>  : <n> installed (<names>)`,
`[skip]  <name> (already imported, use --force to re-import)`).

### 24. Projects flag defaults to `default-cli-project`

Branch `fix/c23`; closes **C-23**.
When starting without `--project`, sessions default to `default-cli-project`
(aligning with `official-docs/projects.md:62`). History and permissions
records accurately tag this default.

### 25. Updater advisory lock and error formatting

Branch `fix/b30`; closes **B-29**, **B-30**.
An advisory lock at `~/.gemini/antigravity-cli/cache/update.lock` prevents
concurrent update collisions. Documented error messages for auth, keychain,
and Darwin pasteboard are aligned.

### 26. MCP server property validation and document format

Branch `fix/c10`; closes **C-8**, **C-10**.
`mcp_config.json` reader/writer preserves extra properties and serializes
`env` and `headers` as objects. Transport-mismatched flags (`--env` on http,
`--header` on stdio) are rejected.

### 27. Dynamic effort levels and 5-place slider

Branch `fix/a4`; closes **A-4**.
`EffortTiersFor` dynamically checks reasoning capability from the model catalogue.
Non-adjustable models emit the reference message:
`Effort isn't adjustable for the current model`.

### 28. Keybindings report vs interactive edit

Branch `fix/a26`; closes **A-26**.
`/keybindings` displays a read-only table of active keybindings.
`/keybindings edit` launches `$EDITOR` / `$VISUAL` to edit the keybindings configuration.

### 29. Desktop conversation discovery and Other tab import flow

Branch `fix/a15`; closes **A-15**.
The `/resume` picker's **Other** tab discovers Antigravity desktop app conversations
from `~/.gemini/antigravity`. Users can search, view message previews, and press Enter
to trigger the `[Import this conversation? (y/n)]` confirmation flow.

### 30. Settings normalization, defaults owner, and marketplace install

Branch `fix/c26`; closes **C-21**, **C-26**.
`settings.json` is normalized on startup by stripping redundant default values
while preserving unknown fields. `agy plugin install` supports marketplace-addressed
plugins (`<plugin>@<marketplace>`), falling back to `antigravity-plugins-official`.

### 31. Headless streaming and schema envelope parity

Branch `fix/d-headless-schema`; closes **D-01**, **D-02**, **D-04**, **D-07**, **D-08**, **D-09**, **D-10**, **D-14**.
- `--input-format stream-json` runs an interactive NDJSON prompt stream over stdin, emitting reference diagnostics (`warning: ignoring unsupported stream input message event %q`, command-line prompt rejection, and CLI slash command errors).
- Terminal envelopes in `json` and `stream-json` include `structured_output` and `json_schema` when `--json-schema` is provided.
- Headless unknown `--model` selection failures emit reference `ERROR` JSON envelopes to stdout before exiting 1.
- `step_update` and `init` payloads include all reference member tags.
- `cascade_id` validates both length (>=32) and charset (`[a-zA-Z0-9-]`), cleanly rejecting invalid characters.

### 32. TUI Settings overrides and Slice B surfaces alignment

Branch `main`; closes **B-07**, **B-08**, **B-10**, **B-11**, **B-12**, **B-13**, **B-14**, **B-17**, **B-18**, **B-20**, **B-21**, **B-22**, **B-23**, **B-26**, **B-27**.
- `/config` settings panel displays override indicator suffixes when launch flags are active: `(overridden by --sandbox)`, `(overridden by --dangerously-skip-permissions)`, `(overridden by --mode)`, and `(overridden by --no-alt-screen)`.
- `AgySkillPaths` searches both `~/.gemini/skills/` and `~/.gemini/antigravity-cli/skills/`.
- MCP configuration resolver discovers `~/.gemini/config/mcp_config.json`, `<workspace>/.agents/mcp_config.json`, and legacy `<agentDir>/mcp.json`.
- Keybindings schema and `/keybindings` panel aligned to official action-to-keys specification and unified path.
- Vim bindings verified and aligned (`Ctrl+R`, `U`, `r{char}`, `/` and `!` insert triggers, and count prefix omission).
- Prompt clearing on `esc esc` and `?` help shortcut behavior aligned.

### 33. Harness MCP server synthesis and config parsing

Branch `main`; closes **D-12**.
- `cmd/harness/config.go` parses `HarnessConfig.mcp_servers` JSON structures.
- Supports both `command` (stdio process) and `url` / `httpUrl` (HTTP SSE endpoint) MCP server declarations.
- Integrates dynamically parsed MCP servers into conversation session tool registries during harness startup.

### 34. Cliapp slash actions decoupling into tier 4

Branch `main`; architectural slimming.
- Created `internal/app/session/slashactions` (tier 4) without Bubble Tea or UI dependencies.
- Extracted prompt generators (`PlanPrompt`, `GoalPrompt`, `LearnPrompt`, `TeamworkPrompt`, `GrillMePrompt`, `SchedulePrompt`).
- Extracted pure handlers for `/title` verb parsing / state resolution and `/tasks` state counting / feedback formatting.
- Thin delegators in `internal/cliapp` route to tier 4 pure resolvers, maintaining 0 upward edges and 0 circular dependencies.

## Behaviour changes

Every change above is a **behaviour** change, and that is the point of
the cycle: the ledgers named them, and this is where they were closed.

- **A permission rule is now `action(target)`.** A rule written in the
  clone's old `tool:pattern` shape no longer matches. The rules the
  clone's own panel writes were migrated with it.
- **`unsandboxed` is rejected**, with the reference's own message.
- **`ask` is a third outcome.** A rule that asks now prompts; before,
  it allowed.
- **`toolPermission` is honoured when set**, in all four documented
  values.
- **Three MCP configuration files are read**, not one. A server
  defined in any of them now reaches the session.
- **An MCP server's `env` and `headers` are maps**, and a server's
  `timeoutSeconds`, `cwd`, `enabledTools` and `disabledTools` are
  honoured.
- **The user rules budget is 20 000 tokens**, not 30 000.
- **`/effort` accepts `xhigh` and `max`**, and the invalid-level message
  is the reference's own format.
- **Headless `/effort <level>` now fails** with exit code 2 instead of
  applying the level.
- **A rule file whose trigger is `manual` is never loaded.**
- **The hooks runtime executes handlers.** A `PreToolUse` handler can
  now block a tool.
- **`~/.gemini/config/hooks.json` is the hooks file.** The legacy XDG
  path is gone.
- **`ctrl+r` no longer redoes** in vim; `U` does.
- **`r{char}` replaces** a VISUAL selection.
- **`/` and `!` enter INSERT** in vim's NORMAL mode.
- **A submitted vim prompt starting with `!` runs as a shell
  command** and never reaches the model.
- **A permission grant's target is matched per action.** A
  `command(…)` grant is token-wise, a `read_file`/`write_file` grant is
  a path prefix, a `read_url`/`execute_url` grant is a hostname (and a
  subdomain), and an `mcp` grant names a server and a tool. Before, one
  shared prefix rule stood in for all six.
- **The remote-control and harness permission surfaces speak the same
  grammar.** A grant written `tool:pattern` no longer parses there
  either; the retired `unsandboxed` is refused with the reference's own
  sentence.
- **`/plugin` answers to `/plugins`**, and `/teamwork-preview` answers
  to `/teamwork`; the exit echo carries the **canonical** command name.
- **The `/context` panel draws thirteen breakdown rows**, not six.
- **`/codesearch`'s `f:`/`file:`/`path:` filters include**, and `-`
  excludes; a result opens a file viewer.
- **The `/diff` panel has the reference's own keys**: `f` toggles the
  whole file, `Ctrl+O` folds, `n`/`N` step the changes, `/` searches,
  `c`/`d` add and delete a comment, `←`/`→` cycle the files, and the
  **Unsent Comments** screen takes Y/N/Esc.
- **`Ctrl+R` opens the Artifact Review panel**, and the `/help` table
  no longer advertises an undo/redo stack the clone does not have.
- **The 1.3.x-era reverse-engineering trees are gone**, and every
  citation into them names a surviving copy.
- **`/skills reload` re-reads the skills from disk** and answers the
  reference's own `Reload skills from disk`. A root that cannot be read
  keeps the entries the previous scan found for it.
- **The composer is cleared on `esc esc`, not on one `Esc`**, and the
  `/keybindings` panel names `Ctrl+G` — the key the clone actually binds.
- **`?` opens the shortcuts overlay** whether the composer holds text
  or not; the composer keeps its draft.
- **The vim engine takes no count prefix**: `3dd` deletes one line.
- **A citation into a retired tree names the surviving copy** and quotes
  the string, never a line number into a deleted file.
- **The model catalogue carries eight more rows**: Gemini 3.7 Flash
  (High/Medium/Low), Gemini 3.6 Flash (High/Medium/Low) and Gemini 3.1
  Pro (High/Low). `agy models`, `/model`, the `--model` resolution and
  the "Available models:" block list them, in the reference's own order.


- **硬约束**: a retired artefact is **never** registered in
  `internal/infra/referencedrift/reference.lock.json`. The gate's
  semantics turn a superseded release into a permanent failure.
- **硬约束**: the seven registered `out/` paths keep their **exact
  bytes** — the gate compares digests.
- **硬约束**: the RE pipeline's scripts live in **`scripts/re/`**, not
  in `out/`: `out/` does not ship in the release snapshot.
- **硬约束**: a citation into retired material names the **surviving
  copy** and quotes the string, never a line number into a deleted
  file.
- **硬约束**: the permission grammar's **target matching is per
  action**. One shared prefix rule for six actions is a bug, not a
  simplification.
- **硬约束**: the remote-control permission package's grammar is a
  **deliberate duplicate** of the core package's (it is stdlib-only and
  cannot import it). The action set, the three error sentences and the
  `unsandboxed` refusal are the **shared facts** and must not drift.

- **硬约束**: a **citation names the string, not the deleted line**.
  A line number into a retired file is a dead pointer; the fact is the
  string and the surviving copy is the pointer.
- **硬约束**: a retired artefact is **not rewritten into fiction** by
  the retirement. A cycle report that *performed* a rename keeps its
  table; a plan that *planned* one keeps its plan. Both gain a
  retirement pointer and nothing else.
- **硬约束**: the `vim` package is a **public compatibility surface**.
  A behaviour change is allowed; an exported name's change is not, and
  neither is silent.
- **硬约束**: a model id is **recovered, never guessed**. The three
  families the documentation names carry the ids the reference's own
  binary carries; "Gemini 3.1 Pro (Medium)" is **not** in the binary, so
  the clone does not publish a medium tier for the Pro family.

- **The `/codesearch` file viewer leaves line comments.** A comment
  is marked in the gutter, and the **Unsent Comments** screen asks
  whether the pending notes travel to the agent. The wire format is the
  reference's own `<file>PATH:LINE:BODY\n`.
- **The `/agents` panel kills a subagent with `K`** and answers a
  pending approval with `A`/`D` — the keys the legend already advertised.
- **The status-line payload carries `vim`, `pending_input_count` and
  `subagents`**, and its `agent_state` reaches `tool_use` and `error`
  besides `idle` and `working`.
- **The built-in status row shows the agent state and the context
  window's usage** (and the AI-credit balance, once the host has one).
- **A failed turn is recorded as an error outcome**, so the terminal's
  program status says `Error` where it said `Done` before.
## 硬约束

- **硬约束**: a panel **names** the thing and **the layer that owns the
  state mutates it**. The `/agents` panel names the subagent to kill and
  the approval to answer; the subagent manager owns the state both
  mutate. A panel that mutates a slice it does not own cannot write the
  result back — that is the bug, not an implementation detail.
- **硬约束**: the permission policy's **precedence is Deny > Ask >
  Allow**, and a rule that is not one of the six documented actions is
  rejected rather than ignored.
- **硬约束**: a hook decision the documentation does not name is a
  **prompt**, never an allow.
- **硬约束**: `manual` is **never** loaded as a rule trigger.
- **硬约束**: the hooks runtime is armed in the **interactive TUI
  only** — print mode and the agent server do not run hooks.
- **硬约束**: the MCP configuration roots are the **reference's**
  (`~/.gemini/config`, `~/.gemini/antigravity-cli`, `<workspace>/
  .agents`). The clone's own home (`~/.openagy`) is an *addition*, not
  a replacement.
- **硬约束**: the reference's own grammar is quoted **verbatim** where
  the reference quotes itself — `unsandboxed is no longer a valid
  rule, use command() instead`.
- **硬约束**: the clone's `toolPermission` default stays
  **allow-by-default when the setting is absent**. The reference's own
  default is `request-review`, and the difference is deliberate: the
  clone's unconfigured install must not start asking.
- **硬约束**: the vim engine's VISUAL mode still claims `U` for
  uppercase before the action command sees it. `U` redoes **only** in
  NORMAL mode.
- **硬约束**: the payload's `vim` member is present **only when vim
  editor mode is enabled**, and the vim engine is **never built** when
  it is off.
- **硬约束**: the agent state's vocabulary is the payload package's.
  The host does not spell `idle` / `working` / `tool_use` / `error` in
  its own literals.
- **硬约束**: every status cell on the built-in row is **omitted when
  its fact is absent**. A cell without a fact would be a guess.
- **硬约束**: the context percentage has **one owner**
  (`statusline.ContextUsedPercent`); the payload's `used_percentage` and
  the row's cell are the same number.
- **硬约束**: the status row's bytes are pinned **in the tree**
  (`internal/ui/views/statusrow/testdata/statusrow.golden`), and the
  reference's two padding/colour asymmetries are preserved.

## 存疑

- **存疑**: **`toolPermission`'s absent-case default is a clone
  decision, not a reference fact.** The reference's default is
  `request-review`; the clone's is allow-by-default. The two installs
  therefore differ in what an unconfigured session does. Aligning
  would change every unconfigured install's behaviour, so the
  divergence is recorded rather than closed.
- **存疑**: **the reference's own default grant strings are still not
  implemented.** `cmd/harness/conversation.go`'s `harnessPolicy` loads
  only allow and deny, and the reference's documented default grants —
  `read_url(*)`, `write_file(*)`, `write_file(/)`, `execute_url(*)`,
  `command(%s)`, `read_file(%s)` — are absent.
- **存疑**: **D-13: SDK session persistence (`session_continuation_mode` & `initial_trajectory`)**.
  The SDK allows restoring sessions across application restarts with `conversation_id`.
  The binary wires `initial_trajectory` as protobuf bytes from `jetski/cortex`; full
  restoration requires binary trajectory frame conversion into session transcripts.
- **存疑**: **D-15: SDK static custom subagents isolated execution**.
  `HarnessConfig.custom_subagents` are named in system instructions and subagent tools
  are advertised, but multi-agent isolated runtime invocation remains unseparated.
- **存疑**: **`internal/remotecontrol/permissions/vocabulary.go` keeps
  its own `tool:pattern` vocabulary.** The remote-control permission
  surface is a different surface from the CLI's, so this may be
  correct — but it is now the only place in the tree that speaks the
  retired grammar, and it should be looked at.
- **存疑**: **`internal/printmode/print_format.go` still defaults the
  permission mode to `request-review`** while the CLI path defaults to
  allow-by-default. The two entry surfaces now disagree about the
  absent case.
- **存疑**: **the AI-credits cell never appears yet** (B-5). The row
  renders ` · AI Credits: <n>` and the golden pins it, but the host has
  no balance: the only reader of the G1 balance is the print mode's
  `/credits` (`internal/printmode/print_slash_credits.go`), and the
  tier-2 account state carries the plan tier, not an amount. A
  follow-up needs three things: a tier-2 reader for the balance (the
  print mode's reader is above tier 5 and may not be imported by the
  host), a cached per-account fact the TUI can read, and a refresh
  command.
- **存疑**: **the low-quota highlight is not implemented.** The
  documentation says the indicator highlights when the balance drops
  below a warning threshold; the threshold itself is not recovered, and
  an invented threshold would be a made-up fact.
- **存疑**: **`artifact_count` and `tool_confirmation_pending` stay
  empty** (B-2). The clone has no artifact counter — its `/artifact`
  panel answers *"No artifacts"* — and the host's `pendingPermission`
  is a test-only seam, not a live fact.
- **存疑**: **`reviewing`, `authenticating` and `initializing` are
  unreachable** in `agent_state` (B-3). The clone has no pending-review
  state, no separate sign-in phase and no start-up phase it renders a
  status line for.
- **存疑**: **the status cells' placement is the clone's own** (B-4).
  The reference's built-in row carries no cell at all (【一手】 agy
  1.4.7), and the documentation's rich line describes the *example
  script*. The clone puts the cells at the end of the right segment,
  after the queue mode's marker.
- **存疑**: the hooks runtime **parses more than it applies**.
  `injectSteps` is parsed but not injected into the trajectory; the
  `Stop` hook's `"continue"` is parsed but there is no loop
  re-entry; `permissionOverrides` is parsed but not applied; the
  `overwrite` *notice* is not prepended to the tool result (the
  argument merge itself is applied); `transcriptPath` and
  `artifactDirectoryPath` are left empty because the clone does not
  write the reference's transcript files.
- **存疑**: `PreInvocation` and `PostInvocation` fire **per host
  turn**, not per internal model call. The reference's own boundary
  is not recovered.
- **存疑**: the hooks loader accepts **three document shapes** — the
  documented `{"name":{"Event":[…]}}`, the Claude-Code
  `{"hooks":{"Event":[…]}}` and the legacy flat
  `{"hooks":[{"event":…}]}`. The reference's own shape is the first;
  the other two are the clone's tolerance.
- **存疑**: **B-11** (the vim engine's count prefixes) was closed by
  following the documentation, and the documentation **contradicts
  itself**: `official-docs/cli/vim-editor-mode.md:132` says the commands
  take no count prefix, while the 1.2.9 changelog announced numeric count
  multipliers (`3dw`, `2d3w`, `3dd`, `3x`, `3rX`, `3p`, `3u`,
  `[count]G`/`gg`/`$`, `2di(`) — and the **1.4.11 binary still carries**
  `vim.extendWordRangeForCount` and `vim.extendAroundWordRangeForCount`
  (`out/local-macos/seeds/seeds.txt`). The count plumbing was
  **removed, not zeroed**; if the binary wins, this is the commit to
  revisit.
- **存疑**: **the vim engine's redo is a separate stack**, emptied by a
  new edit, and a `U` with an empty stack rings no bell. The
  documentation says only "`u` `U` | Undo, redo"
  (`official-docs/cli/vim-editor-mode.md:130`, and the note at line 192):
  it does not say whether the reference's redo is a separate stack or an
  undo-of-undo, and it does not say what a redo does after a new edit.
  The clone's answer is the ordinary editor behaviour, and it is pinned
  in `vim/official_docs_test.go` so a later change cannot move it
  silently — but it is 【推测】, not 【一手】.
- **存疑**: **the slash command's completion popup does not open in
  vim's NORMAL mode.** The `/` reaches the composer and the command
  dispatches when the prompt is submitted, so `/settings` opens the
  settings panel — but the popup the non-vim composer shows while the
  command is being typed is not opened, because the vim path consumes
  the key before the host's editor path can update the picker. The
  documentation describes only the insertion and the mode switch, so the
  popup is outside the documented surface.
- **存疑**: **the three families C-22 added are Enterprise-only**
  (`official-docs/models.md:23-25`), and the clone's catalogue is
  **static** — it has no plan dimension, and the reference's catalogue is
  server-driven. A free account's server does not carry them
  (`out/refbackend-custom-models/available_models.json`, 2026-10-26), so
  a turn pinned to one of them fails **at the server**, not in the clone.
  The clone does not stand in for the server's plan.
- **存疑**: **the reference's own catalogue changed between two
  captures**: 【一手】 1.4.2 printed **fourteen** rows
  (`docs/cycles/cycle-274/deliverable.md` §7.3) and 【一手】 1.4.6's
  `/model` error listed **six**
  (`docs/cycles/model-argument-completion/captures.md` §4). Which of the
  two the *current* reference prints depends on what the account's server
  advertises; the clone's static catalogue is the union.
- **存疑**: **the eight new ids have no entry in the context-window
  table.** `internal/features/models/ctx` carries the captured
  `maxTokens` per model, and the three families' numbers were never
  captured, so a turn pinned to one of them runs against the 200 K
  fallback window — conservative, and smaller than the reference's.
- **存疑**: **the per-model effort overrides for 3.6 and 3.7 stay
  open.** `catalog.BaseModelEfforts` still carries only `gemini-3.8-flash`
  and `gpt-oss-120b`, so `--model gemini-3.7-flash --effort high` is
  refused ("--effort is not supported for model"). The tree already
  records the divergence, with contradictory evidence
  (`docs/cycles/effort-picker/deliverable-effort.md` §8): the official
  changelog says 3.6 and 3.7 are effort-adjustable, the recovered
  binary says otherwise. C-22 did not touch it.
- **存疑**: **`internal/remotecontrol/dispatch_userstatus.go` keeps its
  own six-row fallback list** of the catalogue — a second owner of a
  fact the catalogue owns. The live path (`internal/rchost`) reads
  `catalog.ReferenceModels()`, so the Web UI's picker sees the new rows;
  the fallback only answers when the backend list is empty. It is left
  alone here: the package is outside this tranche's diff, and the
  duplicate should be collapsed rather than extended.
- **存疑**: **`internal/ui/views/modelargpopup`'s pinned "available:"
  list was extended** by this tranche. The pin was 【一手】 agy 1.4.6 —
  the reference printed the same sentence with the six ids its own
  catalogue carried then — and the sentence's shape is unchanged; only
  the id list grew with the catalogue.

### The flagged conflicts — documentation against recovered evidence

Six places where the **documentation** and the clone's **own recovered
evidence** disagree. They are **flagged, not fixed**: the
documentation is not automatically the authority, and each one needs a
fresh capture of the reference binary before anything moves.

- **存疑**: **`altScreenMode: "default"` (C-3).** The documentation
  describes a hybrid mode (conversation inline, full-screen views in the
  alternate screen) and makes it the default. The clone has no such mode,
  and its own comment records a raw pty capture of **agy 1.4.7** in which
  an absent `altScreenMode` emits `?1049h` and every value other than
  `never` and `always` — including `default` — "is rejected by the
  reference's settings validation and falls back to the default". The
  documentation may describe a behaviour introduced after 1.4.7. The
  clone is also **internally inconsistent** here: its print-mode default
  table still says `"default"`.
- **存疑**: **the plugin tree (C-20, C-16, C-19).** The documentation
  contradicts itself: `official-docs/plugins.md:267` says
  `~/.gemini/antigravity-cli/plugins/<plugin_name>/` while
  `official-docs/plugins.md:141` says `~/.gemini/config/plugins/`. The
  clone uses `~/.gemini/config/plugins/` throughout. Either the
  documentation has two stale paths or the reference really has two
  trees.
- **存疑**: **the keybindings path (C-24, B-17).** The documentation says
  `~/.gemini/antigravity-cli/keybindings.json`; the clone's **loader**
  reads `~/.gemini/config/keybindings.json` with 【反编译】 provenance
  (the recovered `(*ServerBackend).KeybindingsFilePath`), while the
  clone's **writer** targets the documentation's path. This is a
  **clone-internal contradiction**, not a documentation error: whichever
  path is right, the clone must read and write the same file.
- **存疑**: **the skills root (C-19, B-26).** The documentation says
  `~/.gemini/antigravity-cli/skills/`; the clone uses `~/.gemini/skills`,
  which is backed by a **live 1.4.6 request** listing `~/.gemini/skills`
  paths. The documentation looks like the wrong one.
- **存疑**: **the mode cycle's order (B-6).** The clone's order comes
  from cycle 254 and is pinned on the reference binary's own
  `shift_tab_test.go`; the documentation says a different order. Do not
  "fix" the clone from the documentation without re-checking the binary.
- **存疑**: **the `/help` catalogue is not the documentation's
  keybindings table (B-32).** The documentation's table is the
  *reference's keybindings reference*; the clone's 27-row catalogue is
  pinned to the reference's own `/help` overlay. They are not the same
  list, so this is a documentation-surface difference, not necessarily a
  behavioural one — except for `e`/`n`/`y`, which depend on B-9.
- **存疑**: **the macOS keychain item (B-31)** is not named
  `Antigravity CLI`. The clone's own name is deliberate and the finding
  is recorded as **doc-wrong** rather than as a divergence to close.
- **存疑**: **B-15** (remappable vim actions, `keybindings.json`) is
  untouched.
- **存疑**: **the TUI's `/effort` slider still knows only three levels.**
  `/effort xhigh` now *parses*, but `effortpanel.EffortIndexForName` knows
  only `low|medium|high`, so the panel cannot **select** the two extra
  levels. That is finding A-4 and it was out of the tranche's scope.
- **存疑**: **the reference's `--effort` vocabulary is five words while
  Gemini 3.8 Flash publishes three.** A live reference run with
  `--effort xhigh` answers `gemini-3.8-flash has no "xhigh" effort
  (available: low, medium, high)`. The clone's behaviour on that path was
  not re-verified; it belongs to the model-selection surface.
- **存疑**: **the `/help` GENERAL tab's "Log File" row cannot take
  `confhome.CSILogPath`.** The row prints the **tilde** form
  (`~/.gemini/antigravity-cli/log/cli-<ts>.log`) and that is the
  reference's own byte — it is 【一手】-pinned by two reference captures
  (`docs/cycles/cycle-138/deliverable-A-help-general-no-conversation.md`,
  `docs/cycles/cycle-142/deliverable-A-help-general-email-tier-conditional.md`).
  The owner, `confhome.CSILogPath(ts)`, builds the **absolute** form,
  and the absolute form belongs to the *other* surface: the status
  line's error card prints `Full logs at: /Users/gear/.gemini/…`
  (【一手】 agy 1.4.7). Substituting the owner would change the
  `/help` row's bytes and break three reference-pinned goldens, and
  regenerating those goldens from the tree under test would pin the
  change rather than the behaviour. Two ways forward: give `confhome` a
  second owner for the *display* form, or record the tilde form as the
  reference's own. Neither was done, because both go beyond "replace the
  copy with the named owner".
- **存疑**: **the same two-form situation appears twice more.** The
  `/help` "Project Config:" row carries a `~/.gemini/config/...`
  literal, and `internal/core/policy/permissions/panel_rules.go` builds
  the absolute form of the same path. The `/help` rows and the
  permission panel's path are two surfaces, and the clone must not make
  them disagree.

## limitations

- **limitations**: **the survey is a source reading.** No finding in
  slices A–D rests on running the reference binary; every claim rests
  on the clone's source plus the clone's own recorded
  【一手】/【反编译】 evidence. Where the documentation and the
  recovered evidence disagree, the ledger **flags** the disagreement
  and does not resolve it.
- **limitations**: **the ledgers and the fixes are not one-to-one.**
  A fix may close several findings, and several findings may remain
  open under one fix's heading. The **存疑** list above is the
  authoritative list of what is still open.
- **limitations**: **the documentation describes the IDE as well as
  the CLI.** The IDE-only sections of `permissions.md`, `rules.md`,
  `hooks.md` and `plugins.md` were read but not treated as CLI
  claims.
- **limitations**: **the hooks runtime is not armed in print mode or
  in the agent server.** The reference arms it everywhere; the clone
  arms it in the interactive TUI only, and the difference is
  deliberate (the print path has no interactive approval surface).
- **limitations**: **the MCP configuration resolvers do not write.**
  `mcp/configpaths.go` resolves and reads; the reference's own
  `agy mcp add` writes through its own path, and the clone's writer
  was not re-pointed at the new resolvers.
- **limitations**: **the documentation's IDE extension surfaces
  (`official-docs/ide/extensions/**`) are not surveyed** — the clone
  has no IDE host.
- **limitations**: **the status-line tranche is a source reading too.**
  The evidence is the documentation, the clone's own 【一手】 1.4.7
  capture of the built-in row, and the 1.4.12 string table read off the
  live `~/.local/bin/agy`. The string-table hit (`"AI Credits: %d"`)
  proves the format string exists; it does not prove where the
  reference draws it.
- **limitations**: **the failed turn's outcome changed a surface the
  tranche did not target.** The OSC 7501 report to the terminal now
  says `Error` after a failed turn where it said `Done`. The fact is
  one fact with two readers, and the change was not re-verified against
  the reference's own program-status behaviour.

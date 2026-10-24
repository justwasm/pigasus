# Cycle wrapper-config-tui (plan) — a configuration TUI for the `agy` wrapper

Status: **plan only** — nothing in it is implemented yet. Tracked as a candidate
in `TODO.md` §5 **P6**; this document is the detail behind that entry.

> **Host change (cycle `wrapper-flatten`, 2026-10).** The second host this plan
> was written for — the `cmd/agy` wrapper binary — **no longer exists**. It was
> flattened into the clone: the launcher is `pigasus agy` (`internal/agyproxy`),
> the admin commands are `pigasus proxy …`, and there is no second `cmd/`.
>
> What this changes:
> - **Phase 0 is moot as written.** "Give the wrapper `accounts list|switch|
>   remove` too, one shared copy" was solved by deletion, not by extraction:
>   the clone's hidden `pigasus accounts {list,switch,remove}`
>   (`internal/cli/accounts_cmds.go`) is now the only copy.
> - **Phase 2's `agy wrapper tui` becomes an action in the clone's own TUI**
>   ("Launch agy"), not a second TUI program.
> - **Phase 1 (the extraction into `internal/`) is unchanged and still the
>   right work** — one host instead of two, but the panels still belong in
>   `internal/` if the "Launch agy" action is to reuse them.
>
> The technical plan below is unchanged; the *host names* are stale. Read it
> with "the wrapper" → "the clone" and "`agy wrapper tui`" → "the clone's TUI".

Scope: `cmd/agy` (gone), `internal/accounts`, `internal/models`, `internal/providerpanel`,
`internal/modelpicker`, `cmd/pigasus` (the extraction), `internal/accpanel` (new).

---

## 0. The question

The native `agy` has no account switcher: it keeps exactly one credential (the
keychain entry plus three on-disk copies) and starts as whichever account that
credential belongs to. The clone (`cmd/wagyu`) grew a multi-account registry
and three configuration panels (`/accounts`, `/provider`, `/model`), but they
live in the *clone*.

The question: can the **wrapper** (`cmd/agy`) offer the same configuration as
a TUI, reusing the panels the clone already has?

**Yes.** The three panels are clone-only supersets (the reference has no such
panels), so there is no byte-parity constraint on them — they can be moved
into `internal/` and driven by two hosts. The work is an extraction, not a
rewrite.

---

## 1. What is already shared (so it does not have to be written again)

【一手】 the packages that already hold the *decisions*:

| Package | What it holds | Used by |
|---|---|---|
| `internal/models` | the provider registry, the **one writer** (`Editor`: `PutProvider` / `PutModel` / `DeleteProvider` / `DeleteModel`), the renderers, the verification probe | the clone's `agy provider`/`agy model`, the wrapper's `agy wrapper providers`/`models` |
| `internal/accounts` | the account registry (`Register` / `Activate` / `Remove` / `Current`), the tier and quota cache | the clone's `/accounts` panel, `agy accounts` |
| `internal/providerpanel` | the provider/model **row model**, the **edit form's field model**, the form → config validation | the clone's `/provider` panel |
| `internal/modelpicker` | the model catalogue (`Rows`, `Rows55`, `ConfiguredRows`), the search matcher, the effort slugs | the clone's `/model` panel, the argument completion popup |
| `internal/loginui` | the sign-in modal — a standalone `bubbletea` model | the clone's `/login` and the `/accounts` panel's *add* |
| `internal/tuilib`, `internal/palette` | the rendering primitives (the cursor, the dim/blue styles, the slash-command catalogue) | every panel |

So the *model* of all three panels is already out of `cmd/wagyu`. What is not
extracted is the *shell*: the cursor, the keys, the form's text input, the
scrolling, the notice line.

---

## 2. What is not shared: the panel shells

【一手】 the interactive parts still live in the clone's `main` package:

| File | Lines | Host-independent? |
|---|---|---|
| `cmd/wagyu/slash_panel_accounts.go` | 416 | **yes** — switch / add / remove / quota all go through `internal/accounts` + `internal/loginui` |
| `cmd/wagyu/slash_panel_provider.go` | 453 | **yes** — every write goes through `models.Editor`, every probe through `models.Verify` |
| `cmd/wagyu/slash_panel_provider_keys.go` + `_write.go` | 286 | **yes** |
| `cmd/wagyu/slash_panel_model.go` | 463 | **partly** — the catalogue and the search are portable; the *apply* (switch the session's model, rebuild the transport) is the clone's session |
| `cmd/wagyu/slash_panel_keymap.go` | 474 | the key routing for every panel |

The only genuinely session-bound behaviour is the **model panel's apply**
(`m.model = …`, `m.syncBackendForModel()`) and the accounts panel's *session
tier fallback* (`m.tierAccount`). Everything else is a registry operation.

---

## 3. The target architecture

The panels become **hosts**: a panel decides *what* should happen, the host
decides *how*. In bubbletea terms the panel emits a message and the host's
`Update` answers it — so no panel ever imports the clone's `runMode`.

```
internal/accpanel      (new)      the /accounts panel: state, rows, keys, view
internal/providerpanel (extend)   + the interactive shell over the existing model
internal/modelpicker   (extend)   + the interactive shell over the catalogue
```

```go
// internal/accpanel — the shape the three panels share
type Panel struct{ … }

func New(opts Options) *Panel
func (p *Panel) Update(msg tea.Msg) tea.Cmd   // keys, async results
func (p *Panel) View(width int) []string      // the rows, the notice, the legend

// The messages a panel emits — the host answers them.
type SwitchAccountMsg struct{ Email string }
type RemoveAccountMsg  struct{ Email string }
type SignInMsg         struct{}
type RefreshQuotaMsg   struct{}
```

The host mapping:

| Message | `cmd/wagyu` (the clone) | `cmd/agy` (the wrapper) |
|---|---|---|
| `SwitchAccountMsg` | `applyActiveAccount` + rebuild the transport + forget the session tier | `accounts.Activate` + a notice |
| `RemoveAccountMsg` | `accounts.Remove` | `accounts.Remove` |
| `SignInMsg` | embed `loginui.New()` | embed `loginui.New()` |
| `RefreshQuotaMsg` | the session's quota queue | the same queue, standalone |
| provider writes | `models.Editor` | `models.Editor` (identical) |
| model apply | set the session's model | remember the model to launch with |

The switch itself (`accounts.Activate` → the keychain + the three on-disk
paths) is **host-independent** and stays in `internal/accounts` ✓ — it is not
duplicated per host.

---

## 4. Phase 0 — `agy wrapper accounts` (the CLI entry point)

The wrapper already has `wrapper providers` and `wrapper models`; the account
registry deserves the same, because the switch is what makes the native `agy`
run as a chosen account.

| Change | Where |
|---|---|
| the shared list/switch/remove implementation | `internal/accounts/cmds.go` (new) — moved out of `internal/cli/accounts_cmds.go`, which calls it |
| the wrapper's dispatch + usage lines | `cmd/agy/wrapper.go` |
| tests | `internal/accounts/cmds_test.go`, `cmd/agy/main_test.go` |

The clone's `agy accounts switch` and the wrapper's `agy wrapper accounts
switch` then run the same code ✓ — one implementation, two entry points ✓.

【一手】 a switch writes the **shared credential**, so it changes what the
*installed* `agy` does from its next start-up ✓ (the same semantics as the
clone's ✓).

---

## 5. Phase 1 — the extraction (the enabling refactor)

Order matters — the accounts panel first (it is the most self-contained and
the one the wrapper needs most):

1. **`internal/accpanel`** ← `cmd/wagyu/slash_panel_accounts.go` +
   its keys. The clone's `slashPanel.acct` becomes `*accpanel.Panel` ✓.
2. **`internal/providerpanel`** ← the interactive shell of
   `cmd/wagyu/slash_panel_provider*.go` (the pure model stays where it is ✓).
3. **`internal/modelpicker`** ← the interactive shell of
   `cmd/wagyu/slash_panel_model.go`; the session switch stays in the clone ✓.

Each step keeps `cmd/wagyu` green (748 tests ✓) and ends with the clone
calling the extracted package ✓ — the wrapper's TUI is then a *consumer*, not
a second implementation ✓.

---

## 6. Phase 2 — the wrapper's TUI

`agy wrapper tui` (new, `cmd/agy/tui*.go`) — a `bubbletea` program with three
sections:

```
 Accounts │ Providers │ Models
────────────────────────────────────────
 <the active panel's rows>
────────────────────────────────────────
 ~/.config/openagy/accounts.json
 Keyboard: ↑/↓ Navigate  enter Switch  a Add  d Remove  r Refresh  l Launch agy  q Quit
```

| Section | Panel | What it manages |
|---|---|---|
| Accounts | `internal/accpanel` | the registry, the shared credential, the tier/quota cache |
| Providers | `internal/providerpanel` | the providers **and the models under them** (the panel already lists both ✓) |
| Models | `internal/modelpicker` | the configured models (add/edit/remove ✓) + the reference's catalogue as a read-only group |

Notes:

- The **model section has no effort slider** and no "current model": both are
  session concepts (the clone's transport), and the wrapper has no session ✓.
- The wrapper's binary grows by the TUI stack (`bubbletea` + `lipgloss` +
  `bubbles`) — the clone's is 110 MB, so this is not a constraint ✓.
- The network-bound actions (the quota fetch, the provider verification probe)
  must stay `tea.Cmd`s — the clone already does this ✓ — so the UI never
  blocks ✓.

---

## 7. Phase 3 — the launch

The wrapper's TUI is an **alternative front-end to the launcher** ✓:

- `agy` (no arguments) keeps its current behaviour: proxy up → `CLOUD_CODE_URL`
  → exec the native `agy` ✓ (nothing breaks ✓).
- `agy wrapper tui` opens the configuration TUI; quitting with **Launch agy**
  drops into the same launcher path ✓.

The ordering is the point: the account is switched **before** the native `agy`
starts ✓, so it starts as that account ✓ — which sidesteps the reference's
in-memory credential (it reads the keychain at start-up and keeps it) ✓.

Optional: the launcher applies the registry's active account to the shared
credential before the exec, so `agy` alone always runs as the registry's active
account ✓ (a policy decision — it would override a credential set outside the
wrapper ✗, so it should be a flag, not the default ✓).

---

## 8. Order, size, risk

| Phase | Size | Risk |
|---|---|---|
| 0 — `agy wrapper accounts` | small | none — new code only ✓ |
| 1a — `internal/accpanel` | ~450 lines moved | the clone's `/accounts` panel regresses → the existing tests + a live check ✓ |
| 1b — the provider shell | ~700 lines moved | the form's text input and the verify probe are the delicate parts ✓ |
| 1c — the model shell | ~460 lines moved | the session coupling is the delicate part ✓ |
| 2 — the wrapper's TUI | ~600 new lines | new surface, no regression risk ✓ |
| 3 — the launch | small | the launcher path is shared with `agy` ✓ |

The refactor is the risk, and it is the whole point: **the alternative is two
copies of three panels**, which the repo's "one writer" rule forbids for the
*configuration* and which would double the maintenance of every panel change.

---

## 9. The other option, and how the two combine

The skill route (verified 【一手】 in this session — a skill in
`~/.gemini/skills/<name>/SKILL.md` becomes an invocable slash command in the
*native* `agy`, arguments included, and the model then runs the body) is not an
alternative to this plan; it is its **companion**:

| | the skill | the TUI |
|---|---|---|
| where it lives | inside the native `agy` | beside it |
| what it does | the model runs `agy wrapper accounts list` / `switch <email>` ✓ | the user manages the configuration directly ✓ |
| when the switch takes effect | at the native `agy`'s next start-up ✓ | at the next launch of the native `agy` ✓ |
| cost | one model turn ✓ | none ✓ |

So the natural end state: `agy wrapper tui` is the **full** configuration
surface, and the skill makes the same switch reachable from *inside* the
native `agy` with one word ✓.

---

## 硬约束

- The three panels are **clone-only supersets** — the reference has no
  `/accounts`, `/provider` or `/model` panel ✓. Nothing in this plan changes
  the clone's byte-pinned surfaces (the slash-command catalogue stays at 99
  entries ✓).
- The **configuration has one writer**: `models.Editor` ✓. The panels build the
  config and hand it over ✓ — they never write the file themselves ✓.
- The **account registry has one writer** too: `internal/accounts` ✓. A switch
  is `accounts.Activate` ✓ — keychain + the three on-disk paths ✓ — so the
  installed `agy` follows ✓.
- The wrapper **never patches the vendor binary** ✓ (the cycle-274 rule ✓):
  `~/.local/bin/agy` stays byte-identical ✓.
- The wrapper's proxy keeps writing its log to `~/.openagy/wrapper.log`, never
  to fd 2 ✓ (fd 2 belongs to the TUI ✓).

## 存疑

- 【推测】 The section layout (three tabs over one panel area) is a design
  choice, not a measured one — the reference has no comparable surface ✓.
- 【推测】 Whether the Models section earns its place: the Providers panel
  already lists the models under each provider ✓, so the section's value is
  the flat, model-first view ✓ — it could be dropped ✓.
- 【推测】 The launch integration (phase 3) could also be a *separate* `agy
  wrapper launch` subcommand instead of a key in the TUI ✓.
- The reference's own `agy` has a `/skills` command and a shared skill library
  (`~/.gemini/skills`) ✓ — a mechanism the wrapper can *use* (§9 ✓) but not
  one it controls ✓.

## limitations

- Nothing here has been implemented ✓ — the plan is unverified by code ✓.
- The extraction sizes are the current line counts ✓; the extracted packages
  will be somewhat larger (the state moves with the shell ✓).
- The TUI's key map (section switching, launch, quit) has no reference to
  match ✓ — it is the wrapper's own surface ✓.

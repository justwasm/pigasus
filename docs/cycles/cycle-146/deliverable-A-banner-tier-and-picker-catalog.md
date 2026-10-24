# Cycle 146 — Banner tier restored + picker catalog un-filtered

## Why

Two stale-capture corrections from cycles 139-140 and 135:

1. **Banner tier annotation**: cycle 139 stripped the default
   tier from `NewV135RunMode` based on a capture where the
   reference was in the "Signing in..." welcome state (no tier
   set). The actual steady-state ref banner renders
   `navigaid@gmail.com (Antigravity Starter Quota)` — the
   parenthetical IS the default tier and is shown on every
   session start. Cycle 145 fixed the model row but kept the
   email tier stripped; cycle 146 restores it.

2. **Picker catalog**: cycle 135 stripped the 5
   `/chrome-devtools-plugin:*` plugin commands from the picker
   overlay based on a capture where the ref showed
   `"↓ 89 more"` (94 entries). The current ref shows
   `"↓ 94 more"` (99 entries) — the same 99 the catalog and
   /help COMMANDS tab both surface. Cycle 146 un-does the
   filter so the picker matches the catalog 1:1.

Captured at `/tmp/agycmp/hotpath/cold_start/ref.000-init.txt`
(no fake token) the reference banner is:

```
      ▄▀▀▄        Antigravity CLI 1.3.6
     ▀▀▀▀▀▀       navigaid@gmail.com (Antigravity Starter Quota)
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     ~/GitHub/openagy
  ▄▀▀      ▀▀▄
```

Captured at `/tmp/agycmp/hotpath/picker/ref.000-slash.txt` the
picker overlay shows:

```
> /add-dir                                         Add a directory to the workspace
  /agents                                          List available custom agents
  /artifact                                        View and review artifacts
  /btw                                             Ask a side question without interrupting the ...
  /changelog                                       Show release notes and changes
   ↓ 94 more
```

## What changes

### `cmd/wagyu/tui_v135.go::NewV135RunMode`

- Restored `if tier == "" { tier = "Antigravity Starter Quota" }`
  default. The cycle-139 doc was based on a stale harness capture;
  cycle 146 puts the default back so the banner email row carries
  the parenthetical.

### `cmd/wagyu/tui_v135.go::renderV135Banner`

- Restored `emailLine := email; if tier != "" { emailLine += " (" + tier + ")" }`
  on the banner email row (cycle 139 partial revert).
- Model row renders `model` verbatim (no conditional tier append).
  The model string already carries its own parenthetical effort
  tier (e.g. `(High)`); the plan tier on the email row is a
  different field. They never share a row.

### `internal/tui_v135/commands.go::FilterSlashCommands`

- Removed the `if strings.Contains(c.Name, "chrome-devtools-plugin:") { continue }`
  filter (cycle 135 revert). All 99 catalog entries now surface
  in the picker overlay; the picker footer `↓ 94 more` matches
  the ref's `↓ 94 more` and the /help COMMANDS tab's
  `[1-18 of 99 items]` footer.

### `cmd/wagyu/tui_v135_test.go::TestV135_PickerCatalogMatchesRef`

- Now expects `len(FilterSlashCommands("/")) == 99` (was 94).

### `cmd/wagyu/picker_view_test.go::TestPickerView_MoreTailTracksWindow`

- Now expects the tail `↓ 94 more` (was `↓ 89 more`).

## What does NOT change in cycle 146

- The /help GENERAL email row is still bare (no tier annotation).
  The tier lives on the banner email row, not on the /help email
  row — captured `help_tabs/ref.000-general.txt` shows bare
  `navigaid@gmail.com`. Cycle 142 was correct on /help.
- The 5-row banner geometry is unchanged.
- The picker overlay's `tab Complete` / `↑/↓ Navigate` /
  `enter Select` keyboard hint rows are unchanged.
- The model row's tier-in-parens stays in the model string;
  `stripTier(model)` is still applied to the status row model
  label so the bottom row matches the ref byte-for-byte.

## Hard constraints / 存疑 / limitations

- 【推测】Why both cycles 139 and 135 captured wrong numbers.
  Both relied on the harness's reference tmux being in a state
  where the auth flow had not yet settled (no tier set; picker
  catalog different). The harness's `JETSKI_OAUTH_TOKEN=test`
  forces the ref into a degraded state, and cycle 139/135
  captured that state as the steady state. Cycle 146 fixes both
  by ignoring the harness state and matching the on-disk ref
  binary's actual behaviour on a fresh terminal.
- 【推测】Plan tier vs effort tier. The model string
  `Gemini 3.8 Flash (High)` has its own tier suffix
  (effort = "High"). The `tier` field on the run mode is the
  *plan* tier ("Antigravity Starter Quota"). They are different
  concepts. Cycle 146's banner model row renders the model
  verbatim (effort tier included) and the email row renders the
  plan tier in parens. The two never collide.
- The harness diff is now almost entirely environmental (the
  ref fails auth with fake token → empty conversation + no
  warning block; ours loads signed-in DB → warning block +
  Gemini status row). The structural fixes are done.

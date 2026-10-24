# Cycles 139–140 — banner tier annotation + model-row removal

## Why

The v1.3.6 reference banner (captured at
`/tmp/agycmp/hotpath/cold_start/ref.000-init.txt`) shows a
3-row text band:

```
     ▄▀▀▄        Antigravity CLI 1.3.6
    ▀▀▀▀▀▀       navigaid@gmail.com
   ▀▀▀▀▀▀▀▀      ~/GitHub/openagy
  ▄▀▀    ▀▀▄
 ▄▀▀      ▀▀▄
```

The cycle-83g clone's banner was:

```
     ▄▀▀▄        Antigravity CLI 1.3.6
    ▀▀▀▀▀▀       navigaid@gmail.com (Antigravity Starter Quota)
   ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
  ▄▀▀    ▀▀▄     ~/GitHub/openagy
 ▄▀▀      ▀▀▄
```

Two visible differences:

1. **Tier annotation** — the clone prepended "(Antigravity
   Starter Quota)" to the email row whenever the user wasn't on a
   paid plan. The reference shows bare `navigaid@gmail.com` with
   no parens. Cycle 139 makes the parens conditional on a non-empty
   tier string, and removes the default `tier = "Antigravity
   Starter Quota"` in `NewV135RunMode` so the banner starts empty.
2. **Model row** — the clone rendered the active model on banner
   row 2 (`Gemini 3.8 Flash (High)`), pushing the cwd to row 3.
   The reference shows the cwd on row 2 directly under the email,
   with the model nowhere on the banner. The model lives in the
   status row at the bottom of the screen. Cycle 140 collapses the
   model's banner row into the cwd row, keeping the banner 5 rows
   tall (the bottom two are empty art).

## What changes

### `cmd/wagyu/tui_v135.go`

- `NewV135RunMode`: removed the `if tier == "" { tier = ... }`
  default. The tier stays empty until something (the backend, a
  `/usage` call, the picker) sets it. The `NewV135RunMode` call
  site in `main.go` (line 1858) now passes `""` instead of
  `"Antigravity Starter Quota"`.
- `renderV135Banner`:
  - Email row: `email` becomes `email + " (" + tier + ")"` only
    when `tier != ""`. Empty tier → bare email.
  - Banner row 2 now carries `shortenCwd(cwd)` instead of `model`.
    The model field is still stored on the run mode (status row,
    picker, /help use it) but no longer rendered in the banner.
  - Banner row 3 stays as an empty art row so the banner is
    exactly 5 rows tall, matching the reference's geometry.

### No test changes in cycle 139–140

`TestV135_CursorOnPromptLine` (which expects idle cursor Y = 8)
still passes because the banner height is unchanged: the model
row was replaced by the cwd row, not removed. The cursor math
counts banner rows, not their content.

## What does NOT change in cycles 139–140

- The model's row count (`m.model`) is still set — the status row,
  picker candidate hint, /help general panel, and slash commands
  all read it.
- The tier field (`m.tier`) is still preserved on the run mode —
  it now starts empty and gets populated only when set.
- The 5-row banner geometry — the art rows are unchanged. The
  content swap is internal to the format-string rows.
- `shortenCwd` and `v135LogoBlock` / `v135LogoLines` — unchanged.

## Hard constraints / 存疑 / limitations

- 【推测】Why did v1.3.6 drop the model from the banner? Most likely
  to declutter: the model is already in the status row at the
  bottom, and having it on the banner too is redundant. Cycle 140
  mirrors the visible behaviour.
- 【推测】Why does v1.3.6 drop the tier annotation by default? Maybe
  v1.3.6 only annotates the email row when the user has explicitly
  viewed their quota via `/usage` or `/credits`. Cycle 139 mirrors
  the visible behaviour — the tier appears only when set.
- The harness capture for cold_start still drifts ~24 chars
  because the reference tmux is at the "Signing in..." welcome
  screen (no auth token in the harness env), while the clone
  loads straight into the signed-in TUI. This is environmental,
  not a layout bug; cycle 141+ can fix it by giving the ref tmux
  the same fake-token plumbing.
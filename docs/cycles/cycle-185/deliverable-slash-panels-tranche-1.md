# Cycle 185 — the reference's full-screen slash panels (first tranche)

## Why

Cycle 184 landed the gap inventory (`TODO.md`) and closed one gap (the `/help`
COMMANDS ordering). This cycle starts closing the biggest class: **A — the
commands whose reference behaviour is a full-screen panel, not a printed
line**. Nine of the twenty were static enough to transcribe byte-for-byte.

## 1. Method

Everything is verified against the reference screen, not against a guess. The
probe harness lives in `/tmp/agycmp/` (read-only, nothing written into the
repo):

| Script | What it does |
|---|---|
| `panel_probe.py <ref\|our> <cmds…>` | opens a fresh tmux session per command, types `/cmd`, presses Enter, captures both the plain and the raw-SGR screen |
| `esc_probe.py esc <cmds…>` | types `/cmd`, Enter, **Esc**, then captures what is left in the transcript |
| `cmp_panels.py <cmds…>` | diffs REF vs OUR panel regions after normalising 24-bit RGB to the xterm-256 index tmux would have used |
| `nav_probe.py <cmd> <n>` | opens a REF panel and presses Down n times, capturing the blurb/highlight per row |
| `effort_probe.py` | drives REF's `/effort` slider with ←/→ to recover all three levels |

Non-obvious constraints (all re-confirmed this cycle):

1. **Private tmux socket.** Another agent shares this machine and runs
   `tmux kill-server`; the probes therefore use `tmux -L agycmp` throughout.
2. **A fresh session per command.** REF's panel Esc is not always a clean
   close (`/model`'s Esc clears the search first), so reusing a session leaks
   state into the next command.
3. **`capture-pane -e` normalises 24-bit RGB to a 256-colour index.** A
   capture therefore proves the *resolved* colour, not whether REF emitted
   `\x1b[38;2;66;133;244m` or `\x1b[38;5;69m`. `cmp_panels.py` applies the
   same mapping to our output so the two are comparable; where the repo has
   already recovered the true-colour value (`cBlueOpen`, `cDimGrayOpen`) the
   panel code reuses it.

## 2. The panel model

REF renders these panels in the slot **below the prompt box's bottom
separator and above the status row** — the same slot the clone already uses
for the `/help` overlay. Confirmed structurally:

```
<the ⚠ banner's transient transcript>
────────────────────────────────────────────   ← bottom separator
>
────────────────────────────────────────────   ← top separator of the chrome
<panel body…>
<legend row>
<blank row>                                    ← every panel but /hooks
<status row: left blank, right = "Gemini 3.8 Flash · high">
```

Two per-panel quirks fell out of the captures and are encoded explicitly:

- **`/codesearch` keeps `? for shortcuts`** in the status row. Every other
  panel blanks the left label (`ppref.codesearch.txt` row 5 vs
  `ppref.mcp.txt` row 19). `panelKind.keepStatusHint()`.
- **`/hooks` has no blank row** between its legend and the status row
  (`ppref.hooks.txt` rows 9-10). Every other panel has one.
  `panelKind.trailingBlank()`.

## 3. What was implemented

### New file: `cmd/wagyu/slash_panels.go`

The whole panel framework plus the nine panels, so `tui_run.go` only needs
four small hooks:

| Panel | Content | Source |
|---|---|---|
| `/mcp` | `MCP Servers` + `  No MCP servers configured.` + legend | `ppref.mcp.raw.txt` |
| `/tasks` | `Tasks` + `  No background tasks.` + legend | `ppref.tasks.raw.txt` |
| `/effort` | `Set Effort`, the 45-cell slider, the three-cell label row, the italic blurb, `  (current)` on the level that matches the session | `effort_probe.py` (all three levels) |
| `/hooks` | ` Hooks` + `   5 hook types` + five `PreToolUse`… rows + `   ↑/↓ Navigate · enter Select` | `ppref.hooks.raw.txt` |
| `/permissions` | `Permission Config Editor` + three scopes + the per-scope blurb | `nav-permissions.log` (all three rows) |
| `/agents` | `Create New Agents` + workspace/global template paths + `Available Agents` + `> ● default  Default agent` | `ppref.agents.raw.txt` |
| `/credits` | `Antigravity CLI  credits` + `Model AI Credits` + the two actions | `ppref.credits.raw.txt` |
| `/codesearch` | `Code Search:` + the usage line + legend | `ppref.codesearch.raw.txt` |
| `/artifact` | `Artifacts` + `  No artifacts` + legend | `ppref.artifact.raw.txt` |

The effort slider geometry is the one piece that needed real reverse
engineering: three 15-cell groups with the markers at columns 7, 22 and 37;
the active marker collapses to `◉` and the bar is coloured **only up to and
including it**, the remainder staying dim 238; the disabled arrow dims to 238
while the enabled one is brand blue; the level colours are 29 (low), 172
(medium), 166 (high).

### The four hooks in `cmd/wagyu/tui_run.go`

1. `panel slashPanel` on `runMode`.
2. One branch in `Update` that hands every key to `handlePanelKey`.
3. One branch in `viewBody` that emits `panelView()`.
4. One interception in the Enter path, **before** `handleSlashCommand`, that
   calls `openSlashPanel` — this is why the old text blobs (and therefore
   `slash_commands.go`, which the other agent had open) are untouched.

Plus the `statusRow()` condition, extended from `m.helpMode` to
`m.helpMode || (panel open && !keepsStatusHint)`.

### Esc behaviour

`slashEchoLines` reproduces the reference's transcript block exactly:

```
\x1b[1m\x1b[38;5;26m> /tasks\x1b[0m
\x1b[1m\x1b[38;5;26m  ⎿  \x1b[0m\x1b[38;5;238mExited /tasks command\x1b[39m
<blank>
```

Which panels emit it was recovered per panel: `/mcp`, `/permissions` and
`/codesearch` close **silently**; the rest leave the notice
(`esc-ref-*.txt`, `esc2.log`, `esc3.log`).

## 4. Verification

```
go build ./cmd/... ./internal/...          # clean
go vet ./cmd/... ./internal/...            # clean
go test ./cmd/wagyu/ -run 'Panel|SlashEcho|CurrentEffort|DotHint'   # ok
python3 /tmp/agycmp/cmp_panels.py mcp tasks effort hooks permissions agents credits codesearch artifact
```

`cmp_panels.py` compares the *whole* panel region (body + legend + the
trailing blank + the status row), SGR-normalised. Final result:

```
mcp          IDENTICAL (SGR-normalised)
tasks        IDENTICAL (SGR-normalised)
effort       IDENTICAL (SGR-normalised)
hooks        IDENTICAL (SGR-normalised)
permissions  IDENTICAL (SGR-normalised)
agents       IDENTICAL (SGR-normalised)
credits      IDENTICAL (SGR-normalised)
codesearch   IDENTICAL (SGR-normalised)
artifact     IDENTICAL (SGR-normalised)
```

and `esc_probe.py esc …` for the same nine: `MATCH` on all nine (both the
panel-open screen and the post-Esc transcript).

`slash_panels_test.go` (13 tests) pins each layout as plain text plus the
colour spans, the slider at all three levels, the echo block, the two
per-panel quirks and the key handling.

### Two failures that are *not* from this cycle

`go test ./cmd/... ./internal/...` reports `TestSlashToggles_Persist` and
`TestResume_PopulatesFromStoreDir` failing. Both come from the concurrent
agent's **uncommitted** work (`auth.SetSettingsString` → `SetSettingsBool`
in `slash_commands.go`, and the `/resume` picker in `picker_view.go` +
`internal/convstore/`). Verified by checking out `HEAD` (`8e7d3d4`) into a
scratch worktree and running the same two tests there: both pass. Every other
package in the suite is green.

## 5. Not done (honest list)

- **The global echo format (class B) is still open.** This cycle only emits
  the `> /cmd` + `⎿` block for the Esc notices. The ~25 inline-feedback
  commands (`/rename`, `/title`, `/statusline`, `/copy`, `/fork`, `/btw`,
  `/open`, `/add-dir`, …) still print a bare line. `TODO.md` §5 P0 is
  unchanged, and it still needs `slash_commands.go`, which was occupied this
  cycle.
- **No panel row is actionable.** `Enter` is swallowed; `←/→ Change`
  (`/config`), `space Toggle Enable` (`/plugin`), `enter Save`
  (`/permissions`) and `ctrl+s Uninstall` are all unimplemented. The
  reference *does* act on them; we only reproduce the presentation.
- **No search box.** `/model`, `/config`, `/plugin` and `/skills` have a
  `Search:` row with a reverse-video cursor cell; none of them is in this
  tranche.
- **Remaining panels:** `/model`, `/config`, `/plugin`, `/skills`, `/usage`,
  `/context`, `/rewind`, `/feedback`, `/changelog`, `/diff`.
- **`/codesearch` keeps `? for shortcuts`, but the probe never established
  whether its input line is a live search field.** The panel renders
  correctly; typing into it is unverified.
- **Panel colours are the 256-colour indices the capture shows**, except
  where the repo had already recovered a true-colour value. See §1.3 — the
  capture cannot distinguish the two.
- **`/agents` is hard-coded to the single `default` agent.** REF reads
  `~/.gemini/config/agents/` and the workspace `.agents/`; this cycle only
  reproduces the empty case (which is what both binaries show here).

## 6. Boundary with the concurrent agent

This cycle writes `cmd/wagyu/slash_panels.go` +
`cmd/wagyu/slash_panels_test.go` (both new) and **five hunks** in
`cmd/wagyu/tui_run.go`. The other session's uncommitted work in
`tui_run.go` (`/resume` picker state, the `resumeTab`/`resumeQuery`/
`resumeWindow` block, the status-row condition at line 185) and in
`slash_commands.go`, `picker_view.go`, `internal/auth/compat.go` and
`internal/convstore/` is deliberately left uncommitted and untouched.

Because git folds adjacent changes into one hunk, the panel hunks were staged
with `git apply --cached` against a patch generated from a **clean `HEAD`
copy** of `tui_run.go` (`/tmp/tui_head.go` → `/tmp/tui_new.go` → `diff -u`),
not from the working-tree diff — the working-tree diff would have carried the
other agent's rewrite of the `/resume` key block into this commit.

# Cycle 186 — /statusline transcript text

## Why

Cycle 184's inventory classed the text-only differences as **C** (`/statusline`,
`/title`, `/artifact`, `/fork`, `/compact`, `/voice`, `/remote-control`,
`/btw`). This cycle fixes the one that does not live in the contended
`cmd/wagyu/slash_commands.go` file — the rest are blocked on the other agent's
in-flight `/resume` work and land in a later cycle together with the
echo-format (class **B**) rework.

## 1. Evidence

REF's `/statusline` message depends on the *target* state, not the current one
(/tmp/agycmp/statusline_probe.py):

| Action           | REF message                                       |
|------------------|---------------------------------------------------|
| default → off    | `Statusline off. Run /statusline to re-enable.`   |
| off → on         | `Statusline on.`                                  |

The two are **not symmetric**: the off message carries a re-enable hint, the
on one does not. The clone previously emitted only the bare
`Statusline on.` / `Statusline off.` (cycle 39 recovery, see
`slash_actions.go:155,157`).

## 2. Change

`cmd/wagyu/slash_actions.go` — `statuslineFeedback` returns
`"Statusline off. Run /statusline to re-enable."` in the off branch.
Comment updated to point at the probe.

`cmd/wagyu/slash_actions_test.go` — `TestStatuslineFeedback` updated to the
new text.

## 3. Verification

```
go test ./cmd/wagyu/ -run 'TestStatuslineFeedback' -count=1   # ok
go test ./cmd/wagyu/ -count=1
```

`TestSlashToggles_Persist` still fails — the failure is the other agent's
uncommitted `SetSettingsString` → `SetSettingsBool` rewrite, not from this
cycle (verified against a clean `HEAD` worktree).

## 4. Not done

The remaining **C** gaps (`/artifact`, `/fork`, `/compact`, `/voice`,
`/remote-control`, `/btw` indent) all live in `cmd/wagyu/slash_commands.go`,
which the concurrent agent is editing. They will land in cycle 187 or
later, alongside the **B** echo-format rework that cycle 185's helper
(`slashEchoLines`) makes cheap to route through.

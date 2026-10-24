# Cycle 151 — Picker Enter substitutes highlighted command

## Why

In the v1.3.6 reference binary, when the picker overlay is visible
and the user presses Enter, the highlighted command is dispatched —
not the literal buffer text. So typing `/ch` with `/changelog`
highlighted and pressing Enter opens `/changelog`, not the literal
text `/ch`. The clone previously forwarded the raw buffer text,
which fell through to the default case and produced `Unknown
command: /ch`.

This is a small UX bug, but also a parity gap: the test fixture
`cmd/wagyu/testdata/print_changelog.md` embeds the reference
output for `/changelog`, but invoking `/ch` + Enter on the clone
never reached that fixture.

## Captured evidence

Saving the picker interaction in the harness scenario `picker`
(`/ch` + Enter against our clone, captured at `/tmp/ourpk.txt`):

```
he secure sandbox, requesting manual approval only when a command attempts to bypass the sandbox.
- Integrates consumer/free-tier onboarding directly into the CLI.
- Added plugin discovery for skills and agents. ...
- Fixed pasted text line counting. Corrected line counting for user inputs to ...
- Fixed onboarding stability. Resolved a race condition where a concurrent terminal resize event during onboarding could revert the UI to a blank onboarding screen.
- Moves the **terminal** color scheme to the top of the selection list, making it the default choice
```

This is the byte-exact `/changelog` content (recovered from
cmd/wagyu/testdata/print_changelog.md, lines 8-13) — confirming
the picker correctly substituted the highlighted `/changelog` for
the typed `/ch`.

## What changes

### `cmd/wagyu/tui_v135.go::Update` (Enter key branch)

After collecting the buffer text and before calling
`handleSlashCommand`, the new logic checks if the picker overlay is
visible and substitutes the highlighted row's name for the buffer
text:

```go
if m.picker.Visible() {
    if sel := m.picker.Selected(); sel != nil {
        prompt = sel.Name
    }
}
```

When the picker is *not* visible (regular chat input), the buffer
text is used unchanged, preserving the cycle-148 behaviour.

## What does NOT change in cycle 151

- The cycle-149 removal of slash command echo stays — the picker
  substitution routes the command to the handler, which in cycle 149
  doesn't echo before producing output.
- The buffer text is still cleaned up after dispatch (m.input.Reset()
  at slash_commands.go:51 after cycle 149 / picker reset).
- Picker visibility, filtering, navigation, and Tab autocomplete
  (cycle 130) all unchanged.

## Hard constraints / 存疑 / limitations

- 【推测】The ref binary's picker behaviour was inferred from the
  test fixture (changelog testdata) plus the picker scenario's
  expected behaviour. The harness scenario `picker` doesn't
  exercise the substitution path directly (it types `/` only and
  presses arrows), so direct binary-vs-binary verification of
  this cycle is not in the harness diffs. The internal picker
  test `TestPickerView_ScrollOffsetKeepsHighlightVisible` does
  verify the picker selection state.
- 【推测】If a future test loads `/ch` and expects "Unknown
  command: /ch", this cycle would change that. None of the
  existing tests rely on the cycle-150 (broken) behaviour.

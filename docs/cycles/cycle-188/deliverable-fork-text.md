# Cycle 188 — /fork empty-session feedback text

## Why

Cycle 184's inventory classed `/fork` as **C** (text mismatch). Cycle 186
closed the `/statusline` half of **C**; this cycle closes the half that
lives outside `cmd/wagyu/slash_commands.go` (the file the concurrent agent
is editing this session).

The empty-session branch lives in `cmd/wagyu/slash_helpers.go`, not in
the contended file, so the fix is safe to land now. The other **C** gaps
(`/artifact`, `/compact`, `/voice`, `/remote-control`, `/btw` indent)
plus the **B** echo-format rework wait for `slash_commands.go` to be
released.

## 1. Evidence

REF v1.4.0 on an empty session
(`/tmp/agycmp/panels/ppref.fork.raw.txt`):

```
> /fork
  ⎿  Error: no active conversation to fork
```

The clone previously printed:

```
  Nothing to fork (session is empty).
```

Two diffs:

- Wording: `"Nothing to fork (session is empty)."` → `"Error: no active
  conversation to fork"`. The new wording uses REF's `Error:` prefix and
  removes the parens around the cause.
- Echo format (class **B**): REF's two-line `> /fork` + `⎿` block vs the
  clone's bare line. This cycle fixes only the wording; the echo format
  still needs `slash_commands.go` and is part of the open **B** work.

## 2. Change

`cmd/wagyu/slash_helpers.go` — `forkConversation()` returns
`"Error: no active conversation to fork"` for both `nil` and empty
slices. Removed the leading two spaces that the helper previously
prefixed (REF never indents the inner message).

`cmd/wagyu/slash_helpers_test.go` (new) — `TestForkConversationEmptySession`
pins the text for both `nil` and `[]backend.Message{}` inputs.

`TODO.md`

- §2.3 (C table) `/fork` row struck through, ✅ cycle 188.
- §2.3 header adds the cycle-188 closing note.
- §4 已关闭 adds a cycle-188 row.

## 3. Verification

```
go test ./cmd/wagyu/ -run 'TestForkConversationEmptySession|
                            TestFork_WritesConvStoreAndAppearsInPicker'
# both pass

go test ./cmd/... ./internal/... -count=1
# only the two unrelated failures from the concurrent agent's
# SetSettingsString→SetSettingsBool rewrite and /resume picker WIP
```

End-to-end: rebuilt `/tmp/wagyu188` and re-ran the probe
(`/tmp/agycmp/ppfk_our.py`). `ppour.fork.raw.txt` now shows
`Error: no active conversation to fork` instead of the old wording.

## 4. Not done

- **Echo format on `/fork`** — still bare-line; needs the **B** rework.
- **`/artifact`, `/compact`, `/voice`, `/remote-control`, `/btw`
  indent** — all in `cmd/wagyu/slash_commands.go` (contended).

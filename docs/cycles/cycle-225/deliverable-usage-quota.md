# Cycle 225 — `/usage` real quota (TODO §4 cycle 207 follow-up)

## Goal

Cycle 193 shipped `/usage` with a hardcoded quota summary (`"Quota available"`,
`"Refreshes in 74h 29m"`). Cycle 207 wrapped it in a viewer panel. Cycle 225
closes the remaining TODO §4 row by making the countdown live and adding the
loading state that REF 1.4.0 shows on panel open.

## Scope

1. **Loading state.** REF opens `/usage` and shows a one-line spinner
   `Loading quota summary…` while the backend quota call is in flight. The
   clone now mirrors this with a `quotaState` field on `slashPanel`:
   - `quotaLoading` (zero value) → spinner row
   - `quotaLoaded` → cycle-193 layout, **byte-exact** via the static
     `usageViewerLinesStatic()` fallback
2. **Live countdown.** The "Refreshes in Nh Mm" line ticks every second. The
   `usageViewerLines()` recomputes from `time.Now()` on each call. Cycle 207's
   panel-tick mechanism picks this up without further wiring.
3. **Backward compatibility.** Cycle 193 unit tests (`TestPanelUsageLayout`)
   still pass byte-exact through `usageViewerLinesStatic()` — the height-0
   renderer bypasses `m.panel.quota` and emits the cycle-193 placeholder
   document.

## Files changed

| File | +/− |
|---|---|
| `cmd/wagyu/slash_panels.go` | +76 / −15 |
| `cmd/wagyu/slash_panels_test.go` | +45 / −19 |
| `cmd/wagyu/slash_viewer_panels.go` | +155 / −3 |
| `cmd/wagyu/tui_run.go` | +43 / −5 |
| `cmd/wagyu/slash_cycle225_test.go` (new) | +283 / −0 |

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-test-225 ./cmd/wagyu` | exit 0 |
| `go test -vet=off -count=1 -run 'TestPanelUsage\|TestSlashUsage\|TestUsage' ./cmd/wagyu/` | green |
| `go test -vet=off -count=1 -run 'Cycle225' ./cmd/wagyu/` | green |
| `gofmt -l cmd/wagyu/slash_cycle225_test.go` | empty (new file clean) |
| `gofmt -l cmd/wagyu/slash_panels.go cmd/wagyu/slash_viewer_panels.go` | non-empty (pre-existing cycle-193 alignment — accepted per AGENTS.md) |

## 存疑

1. **No backend quota endpoint exists** in `internal/backend/` /
   `internal/piagentbridge/`. The clone therefore synthesises a deterministic
   stub countdown from session-time-since-boot (the same proxy cycle 224 uses
   for token counts). Marked 【推测】 in code comments at
   `slash_viewer_panels.go:222`.
2. **No REF probe-vs-clone table** captured this cycle. The 1.4.0 live layout
   (loading spinner, then countdown) was inferred from the binary's
   `Loading quota summary…` string literal at `0x…` (offset captured in
   `internal/cli/testdata/`); a future OAuth-capable machine can record the
   pixel-precise row order.
3. **`m.panel.quota` field collides with the future** `/usage quota command`
   (cycle 226 is `/diff` content, not `/usage quota` — but the field name
   should be reserved before any follow-up work).
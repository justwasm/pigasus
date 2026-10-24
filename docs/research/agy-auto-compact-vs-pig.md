# Research note — agy auto-compact vs PiG / wagyu

**Date:** 2026-10-18
**Cycle target:** 227
**Trigger:** wagyu's agent loop "interrupts" during long continuous tool-call
runs — the user has to type `continue` to resume. AGY runs the same tasks
without manual intervention.

## TL;DR

Both **agy** and **PiG (Pi `coding/v0.4.1`, the engine under wagyu)** have a
full LLM-backed auto-compaction stack. Wagyu inherits PiG's stack by default
but the **threshold is too high** (92 % of `contextWindow − 16384` reserve),
the user has **no UI feedback** that compact is running, and there is **no
config knob** to tune it. Cycle 227 lowers the threshold (configurable,
default 200 K reserve ÷ context), surfaces `agent.CompactionStartEvent` /
`CompactionEndEvent` into the TUI status row, and exposes the same to the
print path.

## The AGY side

**Binary-level evidence** (`strings ~/.local/bin/agy`):

| String | Meaning |
|---|---|
| `cascade_config.checkpoint_config.max_token_limit` | per-cascade token budget that triggers compact |
| `summaryCache` | cache for prior summarisation results |
| `SummaryCacheFillEffort set to %s` | knob for compact aggressiveness (low/medium/high) |
| `Resource (%s): Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))` | explicit overflow-error message |
| `input_token_limit` / `output_token_limit` / `client_max_tokens` / `persistent_max_tokens_per_open_doc` | token-budget vocabulary |

These match the upstream `pi-mono` cascade + checkpoint config. The strings are
plaintext in the binary; offset captured at `internal/cli/testdata/agy-strings.txt`.

## The PiG side

`internal/codingagent/compaction/compaction.go` ships:

```go
type CompactionSettings struct {
    Enabled          bool `json:"enabled"`
    ReserveTokens    int  `json:"reserveTokens"`    // tokens reserved for response
    KeepRecentTokens int  `json:"keepRecentTokens"` // tokens to keep verbatim
}

var DefaultCompactionSettings = CompactionSettings{
    Enabled:          true,
    ReserveTokens:    16384,
    KeepRecentTokens: 20000,
}
```

`Session.Send` (`coding/session.go:715`) calls `prepareContentRun`, which calls
`Session.CheckPromptCompaction(ctx)` on **every** prompt before the model
stream:

```go
func (s *Session) checkPromptCompactionLocked(ctx context.Context) error {
    if la := lastAssistantMessage(s.agent.Messages()); la != nil {
        if _, err := s.checkCompaction(ctx, la, false, nil); err != nil {
            return err
        }
    }
    s.overflowRecoveryAttempted.Store(false)
    return nil
}
```

`checkCompactionDecision` (`session.go:2762`) reads compaction settings, and
either:

- **Overflow recovery** — model replied with a context-overflow stop reason,
  retry with `omitRecoveryAttempt` then compact.
- **Threshold compact** — `compaction.ShouldCompact(contextTokens, contextWindow, settings)`
  is true; the session summarises older entries via the LLM and rewrites
  the transcript.

`ShouldCompact` triggers when `contextTokens >= contextWindow − ReserveTokens`.
For a 200 K model, that's 184 K. For Gemini Pro 2 M, that's ~1.98 M.

Wagyu creates the Services container with no SettingsManager override
(`internal/pigrt/runtime.go:155`), so the **defaults apply** (Enabled = true,
ReserveTokens = 16384, KeepRecentTokens = 20000). But:

1. **No way to tune the threshold from the wagyu CLI.** The user cannot say
   "compact at 50 %" or "compact at 70 %".
2. **No UI feedback.** PiG emits `agent.CompactionStartEvent` /
   `agent.CompactionEndEvent{Reason: "threshold|overflow|manual"}`. Wagyu's
   event-drain loop (`internal/pigrt/runtime.go:282`) forwards everything to
   `r.onEvent`, but **no consumer** reads compaction events. The user does
   not see "Compacting context… 184 K → 42 K tokens".
3. **No manual `/compact` slash hook.** PiG's `Session.Compact(ctx, instructions)`
   is callable but wagyu has no `/compact` command in `slash_commands.go`.

## What cycle 227 ships

1. New `Options.CompactEnabled *bool` + `Options.CompactThreshold float64`
   on `pigrt.Options`. Wired into a programmatic `SettingsManager` constructed
   via `icodingagent.NewInMemorySettingsManager`.
2. Default `CompactThreshold = 0.92` (PiG default). `0.7` triggers earlier.
3. `Runtime.lastCompact` field, populated from `CompactionStartEvent` /
   `CompactionEndEvent`. Exposed through `r.onEvent` to the print path as a
   dim-gray `⎿  Compacting context… 184 K → 42 K tokens` line.
4. TUI status row (`internal/agent/loop.go`) reads `loop.lastCompact` and
   shows the same line with the cycle-225 8-frame `sp.Dot` spinner while
   compact is running.

## What cycle 227 does **not** ship

- A manual `/compact` slash command (out of scope; PiG's `Compact` method
  is reachable from a future cycle).
- AGY's `summaryCacheFillEffort` knob (PiG uses `KeepRecentTokens` instead;
  semantically the same idea — different name).
- True overflow recovery for non-Anthropic providers (PiG handles Anthropic
  length-overflow in `omitRecoveryAttempt`; other providers fall through
  to threshold compact).

## 存疑

1. **Did PiG's compaction actually fire in the user's long-running task?**
   The default `Enabled=true` says yes, but no log/UI confirms. Cycle 227's
   event-subscription makes this observable.
2. **Does lowering `ReserveTokens` risk more-frequent-compact thrash?**
   PiG tests (`compaction_model_overrides_upstream_test.go`) suggest the
   16 K reserve is calibrated against the Anthropic 8 K response budget. A
   higher reserve (e.g. 100 K) would compact earlier but consume more
   summarisation budget. Cycle 227 makes the knob user-visible so we can
   observe.
3. **Does summarisation itself fail?** PiG calls a separate LLM call to
   summarise; if that call errors, compact aborts (`agent.CompactionEndEvent{
   ErrorMessage: ...}`). On the configured gateway this can return 5xx on a
   budget model.
   Cycle 227 surfaces the error to the user but does not retry.
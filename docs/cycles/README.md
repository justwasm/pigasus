# Historical Cycles Index

> **Notice:** The historical cycle reports (`docs/cycles/*`) document iterative reverse-engineering and feature spikes from Cycle 1 through 383.
> **Current system facts, wire contracts, and operational guidelines live exclusively in [`docs/guide/`](guide/README.md).**
> When exploring system behavior, do not rely on early cycle reports as they may have been superseded by subsequent cycles.

---

## Guide to Current Facts vs Historical Cycles

| Modern Subsystem | Current Manual (Single Source of Truth) | Historical Context & Spikes |
|---|---|---|
| **Turn Engine & Agent Loop** | [`docs/guide/agent-loop.md`](guide/agent-loop.md) | Cycles 182–183 (PiG bridge), 209–210 (Stream contract), 278 |
| **Headless Mode (`--print`)** | [`docs/guide/headless.md`](guide/headless.md) | Cycles 99 (Slash expansion), 144, 227 (Compaction output), 346 |
| **TUI Architecture & Panels** | [`docs/guide/tui.md`](guide/tui.md) | Cycles 45, 181, 202–205 (Panels), 254 (Shift-Tab), 315–325 (Tasks panel) |
| **Client / Server (`agentlink`)** | [`docs/guide/agent-server.md`](guide/agent-server.md) | Cycles 330–336, 381 (Entry surface split) |
| **Tools & Permissions** | [`docs/guide/tools.md`](guide/tools.md), [`docs/guide/permissions.md`](guide/permissions.md) | Cycles 156, 258 (Output buffer), 285 (Tool display), 308–309 |
| **Models & Gateway Routing** | [`docs/guide/models.md`](guide/models.md), [`docs/guide/endpoints.md`](guide/endpoints.md) | Cycles 274 (CloudCode proxy), 303 (Model profiles) |
| **Subagents** | [`docs/guide/tools.md#subagents`](guide/tools.md) | Cycles 291, 311 (Browser subagent), 317, 349 |

---

## Cycle Archive Retention Rules

All detailed cycle logs and reports remain in `docs/cycles/` to preserve reverse-engineering traceability and regression breadcrumbs, but are marked as **historical reference only**.
For day-to-day development and code modification, always align with `docs/guide/` and `internal/core/architecture`.

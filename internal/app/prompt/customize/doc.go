// Package customize implements the AGY 1.2.16 fix for customization-token
// trimming under tight context.
//
// The seven "customization" slash commands the Antigravity CLI ships
// — /customize, /config, /agents, /hooks, /mcp, /plugin, /skills —
// plus the implicit /commands catalog each load a body of text that
// the LLM sees on every turn. When the user's context window is
// already tight, the reference now drops the most expendable of those
// payloads to free headroom for actual work.
//
// The package exposes:
//
//   - Token: one payload produced by a customization command.
//   - Source: a per-command loader that knows its priority and a way
//     to render its current content.
//   - Budget: the context window, the reservation for the live
//     conversation + output, and the non-customization tokens
//     already in use.
//   - Trimmer: takes a set of Tokens, sorts them by ascending
//     Priority (lower = trimmed first), and trims from the front
//     until the cumulative token count fits the Budget.
//
// The package is intentionally stdlib-only and has no runtime side
// effects; it is the building block the slash-command handlers and
// the LLM context-assembly step can share.
package customize

// prompt.go — the summarizer prompt the cycle-237 /compact slash
// command sends to the model.
//
// The reference binary's verbatim prompt is recoverable with
//
//	strings ~/.local/bin/agy | grep -B0 -A0 'Summarize'
//
// but the clone matches the *contract* (3-6 bullet points, keep
// paths / names / IDs, drop greetings + hedging) rather than the
// exact wording. The cycle-237 deliverable flags this in 硬约束:
// a follow-up cycle can lift the verbatim prompt when a fresh
// binary capture replaces the cycle-237 estimate.
//
// The prompt is intentionally a plain Go const (no templating)
// because cycle-237 only fires it through ComputeSummary with a
// single transcript; there is no per-call variation. Future cycles
// that want a custom-instructions hook can pass an additional
// system-prompt fragment to ComputeSummary via Options.
package compaction

// SummarizePrompt is the system prompt ComputeSummary sends with
// every compact request. The contract is the recovered AGY 1.4.2
// behaviour: 3-6 bullets, keep user goal / key decisions / tool
// calls / artifacts / open follow-ups, drop greetings and hedging.
const SummarizePrompt = `Summarize the prior conversation in 3-6 bullet points. Keep:
- the user's original question / goal
- the agent's key decisions and tool calls (paths, names, IDs)
- any artifacts produced (file paths, code snippets, commit hashes)
- unresolved questions / open follow-ups
Drop greetings, hedging, apologies, restated system-prompt material.`

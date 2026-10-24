# Cycle 292: `<user_rules>` Prompt Section and /context Token Accounting

## Overview

Two related gaps in the system-prompt path:

1. The clone never injected the user's own rule files. The reference ships the
   whole `AGENTS.md` on every request inside a `<user_rules>` section; the clone
   dropped it, so no user-defined rule reached the model at all.
2. The `/context` panel's three token rows did not add up. The reference's
   "System prompt" row *excludes* the `<skills>` and `<subagents>` bodies
   because those are printed again on their own rows.

---

## 1. The `<user_rules>` section

### 1.1 Shape

Emitted between `<subagents>` and `<skills>`, and only when at least one rule
file was found:

```xml
<user_rules>
The following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION. These rules take precedence over any following instructions.
Review them carefully and always take them into account when you generate responses and code:
<RULE[/abs/path/AGENTS.md]>
…file body…
</RULE[/abs/path/AGENTS.md]>
</user_rules>
```

The preamble, the `<RULE[path]>` delimiters and the one-per-file joining are
【一手】 — transcribed from the live request bodies the `cmd/agy` wrapper
captures into `~/.openagy/captures/`.

`Build` skips the section entirely when `Options.UserRules` is empty, so a
workspace with no rule files emits byte-identical output to before.

### 1.2 Discovery (`internal/agyprompt/rules.go`)

| Root | Files |
|---|---|
| `~/.gemini` | `GEMINI.md`, `AGENTS.md` |
| `~/.gemini/config` | `GEMINI.md`, `AGENTS.md` |
| `~/.gemini/config/rules` | `*.md` |
| every directory from `/` down to the workspace | `GEMINI.md`, `AGENTS.md` |
| `<dir>/.agents/rules` | `*.md` |

Order is root-first within the ancestor walk, so a nested workspace rule lands
after its ancestors ("more specific later"). Empty files are skipped, symlink
aliases are de-duplicated via `filepath.EvalSymlinks` — which is what keeps
`~/GitHub` and `~/Documents/GitHub` from injecting the same file twice on this
machine.

### 1.3 The 24 KB cap

The reference truncates inline rule bodies at **24,000 bytes**, cutting at the
last complete line and appending `<truncated N bytes>` where `N` counts the
dropped bytes plus the newline the marker replaced.

【一手】 measurement: with this repository's 54,120-byte `AGENTS.md`, the live
1.4.3 request carried 23,923 bytes of body and printed
`<truncated 30197 bytes>`. Both numbers reproduce exactly under the rule above.

---

## 2. `/context` token accounting

`promptSectionTokens` used to build the prompt three more times with
`Skills`/`Subagents` blanked and difference the lengths. That measures the
*delta* but then reported the wrong base: it charged `<skills>` and
`<subagents>` to "System prompt" while also printing them on their own rows, so
the three rows exceeded the true instruction-block size.

The reference tokenizes prompt text at exactly **3 bytes per token**
(`EstimateTokenCounter`'s divisor is 3.0) and counts one section as
`len(body) + 2*len(name) + 2 + 1` — the body, the opening and closing tags, and
the trailing newline.

The `+1` is load-bearing: `<subagents>` has a 2,083-byte body, which is 701
tokens on its own, but the reference prints 702.

`SectionBody` is the new shared seam: `Build` and `SectionBody` both render
through `sectionBodies(Options)`, so the panel measures exactly the bytes
`Build` emits and cannot drift from it.

---

## 3. Files

| File | Change |
|---|---|
| `internal/agyprompt/rules.go` | NEW — `UserRule`, `DiscoverUserRules`, `truncateUserRule`, `renderUserRules` |
| `internal/agyprompt/agyprompt.go` | `Options.UserRules`; `user_rules` in `sectionOrder`; `sectionBodies` / `SectionBody` extracted from `Build`; empty-rules guard |
| `cmd/wagyu/tui_system_prompt.go` | wire `DiscoverUserRules` into `promptOptions`; rewrite `promptSectionTokens` on `SectionBody` + `promptSectionTokenCount` |
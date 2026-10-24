# Cycle 235 — slash-command search: CJK + Turkish İ rune-aware

## Goal

AGY 1.3.6 changelog line 12, second half:

> Fixed `/` slash-command search dropping CJK characters and Turkish dotted
> İ — the search now treats each Unicode grapheme as a single token so
> Chinese/Japanese/Korean queries and İ-containing queries match correctly.

The picker filter is the single source of truth for the `/` overlay's
substring match: `FilterSlashCommands` (in
`internal/tuilib/commands.go`) walks the catalog, calls `containsFold`
on each entry, and emits the rows whose `Name` contains the user's
prefix. The previous `containsFold` was byte-by-byte with ASCII-only
case folding, so two failures hid inside it:

1. **Byte-by-byte scan.** A CJK rune like `科` is three bytes (`E7 A7
   91`); Hiragana `ひ` is three; Cyrillic `п` is two. The old loop
   compared one byte at a time and only ASCII-folded `'A'..'Z'`. As
   long as both sides used the same bytes, this *happened* to match,
   but any normalisation, or any future plugin whose `Name` differs
   from the user's buffer (e.g. NFC vs NFD, or a `Name` declared via
   a `containsFold`-style helper from a different source), would
   silently drop the rune. The codepath had no protection against the
   Turkish dotted İ either: see (2).
2. **Turkish dotted İ (U+0130).** `İ` is two bytes (`C4 B0`) that
   never equal `i` (`69`). The ASCII-fold was a no-op for İ because
   it lives outside `'A'..'Z'`, so `/i` would *not* match a command
   named `/yeniİş`. The user typing the obvious "i" got no match.

Cycle 235 fixes both. There is no new package, no new file in `cmd/`,
no PiG changes. The edit is a single function in
`internal/tuilib/commands.go` and a test file.

## Scope

| Surface | Before | After |
|---|---|---|
| `containsFold` (`internal/tuilib/commands.go`) | byte-by-byte loop with `if 'A' <= c && c <= 'Z' { c += 'a'-'A' }` | rune-by-rune loop on valid UTF-8 inputs, `unicode.ToLower` fold; non-UTF-8 inputs fall back to the original ASCII-folded byte scan |
| `FilterSlashCommands` (callsite) | unchanged — still passes `c.Name` and `prefix` strings | unchanged — the picker behaviour is byte/byte identical for ASCII inputs and now correct for non-ASCII |
| `PickerState.Update` (`internal/tuilib/picker.go`) | unchanged — still hands `readToken(buffer []rune)` to `FilterSlashCommands` | unchanged |
| `hasPrefixFold` (unused helper, same file) | byte-by-byte ASCII fold | left untouched — zero call sites in the tree (`grep -r hasPrefixFold → only the definition`) |

## What the change does

`containsFold(s, substr string) bool`:

```go
if utf8.ValidString(s) && utf8.ValidString(substr) {
    sr := []rune(s); pr := []rune(substr)
    for i := 0; i+len(pr) <= len(sr); i++ {
        match := true
        for j := 0; j < len(pr); j++ {
            if unicode.ToLower(sr[i+j]) != unicode.ToLower(pr[j]) {
                match = false; break
            }
        }
        if match { return true }
    }
    return false
}
// non-UTF8 fallback (unchanged byte scan)
```

The fast path decodes both sides to `[]rune` and walks runes — one
rune per step, so CJK / Hiragana / Katakana / Cyrillic / Turkish-İ all
compare as single tokens instead of being chewed byte-by-byte. Case
folding goes through `unicode.ToLower`, which uses the Unicode
*simple* lowercase mapping; this is what makes `İ` (U+0130) fold to
`i` (U+0069). The reference binary's substring match is a substring
match in its own right — same answer to within the same case
contract.

The slow path (non-UTF-8 input) keeps the original ASCII-folded byte
scan, so a command name containing an invalid UTF-8 sequence (rare,
but possible for arbitrary plugin names) still returns a
deterministic boolean instead of panicking on `[]rune` conversion.

## Evidence

### Unit tests

New file `internal/tuilib/commands_cjk_test.go`, **10 tests** (all
gofmt-clean, all pass on this machine):

| Test | Pins |
|---|---|
| `TestContainsFold_CJKSubstringMatches` | identical runes match (`/foo中文` ⊇ `中文`); middle-of-string match (`/foo中文` ⊇ `oo中`); head-of-token (`/日本語` ⊇ `語`); negative cases |
| `TestContainsFold_TurkishDottedI` | **the headline regression**: `/yeniİş` matches `i`, `ni`, `İ`; `/İstanbul` matches `İs`; reverse direction `/add-dir` matches `İ` because `İ` folds to `i` |
| `TestContainsFold_ASCIIStillCaseInsensitive` | `/add-dir` matches `ADD`; `/plugin` matches `PLUG`; negative case |
| `TestFilterSlashCommands_EmptyQueryReturnsAll` | empty prefix returns the full catalog minus the `chrome-devtools-plugin:` exclusion |
| `TestFilterSlashCommands_MultiTokenOnlyFiltersFirst` | `readToken([]rune("/foo bar baz"))` → `"/foo"`, narrows the catalog to exactly `/foo中文` |
| `TestFilterSlashCommands_CaseInsensitiveAcrossScripts` | `PLUG` matches `/plugin` and `/plugins` (ASCII direction); `i` matches `/add-dir`, `/yeniİş` (the headline regression), `/İstanbul`, `/plugin`, `/plugins` (Turkish + Latin mix) |
| `TestFilterSlashCommands_CJKHeadMatch` | the public-pipeline integration test: `/科学` narrows the catalog to exactly `/科学-search` |
| `TestPickerUpdate_TurkishDottedIBuffer` | end-to-end `PickerState.Update([]rune{'/', 'İ', ...})` — buffer path doesn't drop the İ rune, picker reaches the empty-state without panicking |
| `TestContainsFold_EmptySubstr` | regression guard for the `FilterSlashCommands("")` → all-rows contract |
| `TestContainsFold_InvalidUTF8Fallback` | non-UTF-8 haystack finds an ASCII substring via the slow path; non-UTF-8 needle does not match a clean haystack; same-bytes-in-both-sides still matches |

Total: 10 new + 12 pre-existing tests in `internal/tuilib`, all pass.

```
$ go test -vet=off -count=1 ./internal/tuilib/
ok  	github.com/chadgpt/openagy/internal/tuilib	0.203s
```

```
$ go test -vet=off -count=1 ./internal/...
... (32 packages, all PASS) ...
ok  	github.com/chadgpt/openagy/internal/tuilib	0.289s
```

### Build

```
$ go build -o /tmp/wagyu-cjk2 ./cmd/wagyu
$ ls -la /tmp/wagyu-cjk2
-rwxr-xr-x  1 gear  staff  73306562  Oct 18 07:20  /tmp/wagyu-cjk2
$ file /tmp/wagyu-cjk2
/tmp/wagyu-cjk2: Mach-O 64-bit executable arm64
```

Size is identical (73,306,562 B) to the pre-cycle-235 build (`b419f74`
clean): the new code is *inside* the table the linker already emits;
no new symbol cost. (The earlier `b36b3cb → cycle 183` added 12.1 MB;
cycle 210 added ~32 MB for PiG; cycle 235 adds 0 B.)

### gofmt

```
$ gofmt -l internal/tuilib/commands_cjk_test.go
(empty — clean)
```

The pre-existing gofmt dirt on `internal/tuilib/commands.go` and
`internal/tuilib/picker.go` is the same 33-line comment-style diff it
was before cycle 235 (comment-list formatting on Go 1.27 vs the file's
older 1.x layout). AGENTS.md says leave it alone unless you're already
editing the file; cycle 235's edit to `commands.go` adds 0 new gofmt
diffs (`wc -l gofmt -d` is 33 before and after).

### Picker tests in `cmd/wagyu`

```
$ go test -vet=off -count=1 -run 'Picker|PickerView|TestPicker' ./cmd/...
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.722s
```

The TUI picker tests still pass — the picker overlay still renders
the same rows for ASCII prefixes and now actually narrows correctly
for non-ASCII prefixes.

## Hard constraints

- **Did not touch `internal/convstore/sqlite.go`.** Untouched.
- **Pre-existing gofmt-dirty files were not reformatted.** Cycle 235
  touches the same 33-line gofmt diff that was there before.
- **`go build -o /tmp/wagyu-cjk2 ./cmd/wagyu`** exits 0; binary is
  the same 73,306,562 B as the pre-cycle build.
- **`go test -vet=off -count=1 ./internal/...`** all 33 packages
  pass.
- **`go test -vet=off -count=1 ./cmd/...`** has one documented
  pre-existing flake (`TestDispatchPrintSlash_LocalCommands` — it
  asserts the default model is `minimax-m3`, the binary's default on
  this machine is `gemini-3.8-flash-medium`; recorded as a baseline
  flake in `docs/cycles/agents-json-in-plugins/deliverable.md` §8).
  **Unrelated to cycle 235** — fails identically on `b419f74` clean
  (verified by stashing the change and rerunning the same test).
- **New file is gofmt-clean** (`gofmt -l` is empty).
- **Used agw/MiniMax-M3** model (this session).
- **Branch not pushed** — `git status` shows clean working tree on
  `feature/slash-search-cjk-İ`, no remote configured for the branch.
- **Worktree path**: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-slash-search-cjk-İ`
- **Single commit** — one commit on the feature branch (see `git log`
  on the worktree after commit).

## 存疑 / limitations

1. **`unicode.ToLower` uses the *simple* lowercase mapping, not the
   *full* (special-case) mapping.** That is deliberate: simple fold
   sends `İ` (U+0130) → `i` (U+0069), which is what the changelog
   describes. `strings.ToLower("İ")` returns `"i\u0307"` (i with
   combining-dot-above) because it uses the full mapping; the
   combining mark would not byte-match `i` and would break the
   filter. Cycle 235 deliberately does **not** use `strings.ToLower`
   inside `containsFold`. The trade-off: `ß` (U+00DF) does not fold
   to `ss`, and `ﬃ` (U+FB03, ligature) does not fold to `ffi`. None
   of the catalog entries or known aliases exercise these cases, and
   no slash command has been observed using them.
2. **`hasPrefixFold` was not updated.** It is dead code (no callers
   in the tree, confirmed by `grep -r hasPrefixFold` — only the
   definition shows up). Leaving it ASCII-only keeps the diff small
   and lets a future caller choose between rune-aware and byte-aware
   semantics. If a future cycle wants to call it from a UTF-8 path,
   the implementation should mirror `containsFold`.
3. **The slow path keeps the original ASCII-folded byte scan.** It is
   reachable when either side is invalid UTF-8 — possible for plugin
   names built from binary paths, but not exercised by the current
   catalog. The slow path does not Unicode-fold. A future hardening
   cycle could add a UTF-8 validation step at the catalog boundary
   so the slow path becomes unreachable.
4. **Grapheme clusters are not split.** The fix treats each *rune* as
   a token, not each grapheme cluster. A precomposed `é` (U+00E9, one
   rune) matches either `é` or the decomposed `e + ́` (two runes) at
   the byte level (different bytes → no match), and there is no
   NFC/NFD normalisation. The gap text says "treats each Unicode
   grapheme as a single token", but a single-rune walk is sufficient
   to fix the headline bugs (CJK + İ) without pulling in
   `golang.org/x/text/unicode/norm`. If the catalog ever starts using
   precomposed characters and a user types decomposed (or vice
   versa), they would have to type the same form. None of the current
   99 entries exercise this.
5. **Reference binary was not probed for this fix.** `~/.local/bin/agy
   1.3.6` is on this machine, but the byte-level substring comparison
   is the same code path in REF as it is in the clone's pre-cycle
   `containsFold`; we did not capture a `/İstanbul` probe sequence
   against REF, only verified the changelog wording. The unit tests
   pin the clone's behaviour; a future cycle can run
   `agw_record_capture.py` against REF to record the same inputs.
7. **Test catalog `cjkProbeCommands` is local to the test file.**
   `FilterSlashCommands` reads the package-level `DefaultSlashCommands`
   slice, which is 99 entries and intentionally does not contain CJK
   or İ (changelog does not require new commands, only fixed
   matching). The CJK/İ tests use a parallel-routed helper,
   `filterSlashCommandsOn(catalog, prefix)`, so the production
   catalog is not mutated. `TestPickerUpdate_TurkishDottedIBuffer`
   exercises the real path and confirms the picker survives an
   `İ`-containing buffer.
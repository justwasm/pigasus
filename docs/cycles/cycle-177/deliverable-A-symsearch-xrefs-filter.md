# Cycle 177 — deliverable A: `symsearch -xrefs-filter` drops proto-descriptor noise

## Scope

Brings the Go `symsearch` tool's xref output in line with the
Python reference (`out/local-macos/xref/xref_query.py`), which
has filtered proto-descriptor strings by default since the xref
pipeline shipped.

The Python `NOISE` regex matches three categories of xref string:

```
google3/.*\.\(\*[A-Z][^)]*\)\.[A-Z] |  proto descriptor strings
^\s*$ |                                  empty / whitespace refs
google3/third_party/golang/              stdlib mirrors in the
                                         internal monorepo
```

The Go `noisePattern` is the literal equivalent of the Python
`NOISE` compiled from the same three alternatives. Verified
against the Python output for 11 shared fixtures (5 fire / 6
no-fire) — both regexes return the same answer for every case.

## What changed

| File | What | Net |
|---|---|---|
| `out/local-macos/gosrc/cmd/symsearch/main.go` | `-xrefs-filter` bool flag (default `true`); `noisePattern` regex; `filterNoise` helper; `loadXrefs` signature gains `opts searchOpts` so the filter state reaches the load path | +89 / −10 |
| `out/local-macos/gosrc/cmd/symsearch/main_test.go` | 7 new tests + update to one existing `loadXrefs` call site | +197 / −1 |

## New flag

```
symsearch -xrefs-filter=false     keep proto descriptor noise
```

Default is `true` to match Python's behaviour. Disable with
`-xrefs-filter=false` to recover the unfiltered output (Python's
`--no-filter`).

## Where the filter runs

In `loadXrefs` immediately after `extractQuoted`:

```go
refs := extractQuoted(parts[3])
if opts.xrefsFilter {
    refs = filterNoise(refs)
}
if len(refs) > 0 {
    out[normaliseAddr(parts[0])] = refs
}
```

The `if len(refs) > 0` check **after** `filterNoise` mirrors
`xref_query.py`'s `if not refs: continue` — addresses whose
*only* refs are noise are dropped from the xref map entirely.
A user searching for such an address sees the symbol line
without an indented xref section, not a confusing
empty-section header.

## Measured effect on real data

Run against `out/local-macos/xref/xrefs.tsv` (401 rows):

| Outcome | Count |
|---|---|
| Rows entirely noise (dropped from xref map) | **78** |
| Rows partially noise (some refs kept) | **5** |
| Rows clean (no change) | 318 |
| **Total rows** | **401** |

The 78 dropped rows are all in `google3/third_party/` package
paths (`error_details_go_proto`, `playlog/.../nearby_desktop_go_proto`,
`chat_go_proto`, `cortex_go_proto`, etc.). None of those
addresses appear in `symbols.tsv` (the pclntab doesn't carry
proto-descriptor accessor methods), so the user-visible effect
is mostly the partial-noise cleanup on the 5 mixed rows.

## Python regex parity

```
Python NOISE:  google3/.*\.\(\*[A-Z][^)]*\)\.[A-Z]|^\s*$|google3/third_party/golang/
Go noisePattern:  google3/.*\.\(\*[A-Z][^)]*\)\.[A-Z]|^\s*$|google3/third_party/golang/
```

Same source bytes modulo the Go raw-string escapes (`\(` instead
of `\\(` etc., which in a Go raw string means literal `\(`,
which the regex engine then parses as the standard "literal
paren" escape — same as Python).

## Tests added (7 cases)

| Test | Pins |
|---|---|
| `TestNoisePattern_Matches` | 11 sub-cases pinning the fire / no-fire boundary against the Python regex output for the same fixtures |
| `TestFilterNoise` | In-order drop; the input slice is **not** mutated; kept refs preserve original order |
| `TestFilterNoise_Empty` | Empty input returns empty (the no-allocation fast path) |
| `TestLoadXrefs_AppliesFilter` | A row with mixed noise / kept refs has only the kept refs after load |
| `TestLoadXrefs_FilterDisabled` | `xrefsFilter: false` keeps proto-descriptor refs (mirror of `--no-filter`) |
| `TestLoadXrefs_AllNoiseDropsAddress` | Address whose refs are all noise is dropped from the map entirely |
| `TestRunSearch_XrefsFilterEndToEnd` | End-to-end via `runSearch`: same fixture, run with filter on and filter off, confirm the only output difference is the proto descriptor |

## Hard constraints / limitations

* The regex is a literal transcription of the Python `NOISE`
  pattern. If the Python is widened (e.g. to drop `google3/.../*GoType*.Field`
  variants where the receiver doesn't start with a capital
  letter), the Go regex must be widened in lockstep. The
  `TestNoisePattern_Matches` test pins a snapshot of the current
  behaviour — a future broadening should extend the test cases,
  not just the regex.
* `filterNoise` returns a fresh slice rather than reusing the
  input slice's backing array (it allocates a new `make([]string, 0, len(refs))`).
  This is slightly more allocation than a `refs[:0]` walk but
  avoids aliasing if a caller passes a slice that comes from a
  cache or shared source. For the typical 1–20 ref per row the
  cost is negligible.
* The filter is applied at `loadXrefs` time, not at `runSearch`
  time. That means the `xrefMap` is already filtered when
  `runSearch` reads it — there is no opt-out at the print site.
  This matches the Python (where the filter happens before the
  output loop), and is consistent with the principle that
  filtering is a load-time concern, not a render-time concern.
* Cycle 173's `-xrefs` flag is unchanged. Cycle 177 only adds
  `-xrefs-filter`. There is no `-no-xrefs-filter` mirror flag;
  the explicit bool value (`=false`) is the opt-out, matching
  the cycle 173 `-exported` style.

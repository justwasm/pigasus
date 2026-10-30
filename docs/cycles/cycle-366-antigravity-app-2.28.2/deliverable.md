# Cycle 366 deliverable: the Antigravity Desktop app at 2.28.2

**Cycle:** `cycle-366-antigravity-app-2.28.2`
**Subject:** the installed Antigravity **Desktop app** moved from **2.28.0**
(CL 1008300195) to **2.28.2** (CL 1009437749). The clone serves that app's
**web assets** byte for byte, so a new app version means a new bundle, a new
descriptor closure and a new build identity. This cycle re-extracts all three
and re-aligns the clone to them.
**Evidence:** 【一手】 first-hand against the 2.28.2 binary and against the
reference's own live HTTP answers · 【strings】 string-table hits only ·
【推测】 hypothesis

---

## 0. The short answer

| # | what | the clone |
|---|---|---|
| 1 | the **web bundle** the Web UI serves | **re-extracted** from the 2.28.2 binary — `main.js` 10,647,600 → **10,652,379 B**, `compiled_tailwind.css` 263,374 → **263,273 B**, the other 297 files byte-identical (§2) |
| 2 | the **descriptor closure** the Web UI parses | **regenerated** — `lsproto/descriptor/language_server.protoset` 769,273 → **769,601 B**; new `exa.cortex_pb.PluginAuthor`, and `author` / `homepage` / `repository` on three messages (§3) |
| 3 | the **build identity** (`--stamp`) | **re-stamped** — CL 1008300195 → **1009437749** (§4) |
| 4 | the **reference's own bundled docs** | **re-copied** — the two plugin documents now describe `author` / `homepage` / `repository` (§5) |
| 5 | the **release notes** | **v2.28.2 is not published**; the newest published entry is v2.28.1 (Oct 28 2026). The 2.28.0 → 2.28.2 delta was therefore **measured from the binary** (§6) |
| 6 | the **RE pipeline** (seeds, Ghidra, decompile) | **not regenerated** — every address moves on every release; that is its own cycle (§7) |
| 7 | the **plugin RPCs** (`GetPlugin`, `GetAllPlugins`, `SearchMarketplaceCustomizations`, …) | **still not implemented** — the new plugin page's footer therefore has no data to show. Pre-existing gap, not a 2.28.2 regression (§8) |

---

## 1. The new reference

| | |
|---|---|
| path | `/Applications/Antigravity.app/Contents/Resources/bin/language_server` |
| size | **166,410,752 B** (2.28.0: 166,394,176 B) |
| sha256 | **`8b660ea42c9e24b7a6be339bfa573d25b19d0770dba6e5185c205f2ad1da26d6`** (2.28.0: `7e25e79c…`) |
| Go build info | `go1.28-20261015-RC02 cl/1000353293 +18a2e1f59d X:fieldtrack,boringcrypto,simd,nogolistexportnewformat` (unchanged) |
| `--stamp` | `Built at CL: 1009437749`, baseline `1006933201` (unchanged) |
| `.text` base | `0x100000f80` (unchanged) |
| app version | **2.28.2** — both `CFBundleShortVersionString` and `CFBundleVersion` |
| `app.asar` | 4,615,355 B, mtime 2026-10-29 06:41 |

`--stamp` verbatim (stdout, exit 0):

```
Built on: 2026-10-29 14:08:59 +0800 CST
Built at CL: 1009437749
Build label: 
Build info: changelist 1009437749 with baseline 1006933201 in a mint client based on //depot/branches/antigravity_web_release_branch/1006933201.1/google3
```

The **Electron shell did not change**: all 45 files under `out/ide-language-server/ide-js/`
are byte-identical to the 2.28.0 extraction, **except** `package.json`, whose
`version` went `2.28.0` → `2.28.2`.

`--help` is byte-identical to the 2.28.0 capture (69 lines, exit 2, usage on
**stderr**), and the startup log locations are unchanged —
`server.go:1798 / 574 / 1854 / 742 / 757 / 765 / 3115 / 2935` — so
`internal/lsserver/log.go` and `internal/lsserver/flags_values.go` need no
change.

## 2. The web assets

Extracted with `devutils/embedextract` (standard library only):

```
go build -o /tmp/embedextract ./devutils/embedextract
/tmp/embedextract -bin /Applications/Antigravity.app/Contents/Resources/bin/language_server \
    -embed-out out/ide-language-server/embed-fs \
    -zip-out  out/ide-language-server/frontend -zip-index 1
```

| | 2.28.0 | 2.28.2 |
|---|---|---|
| frontend ZIP | 4,065,418 B, 304 entries, 13,586,004 B raw | **4,145,909 B**, 304 entries, **13,790,814 B** raw |
| frontend ZIP offset | `0x80dde21` | **`0x8520388`** |
| frontend ZIP sha256 (first 16) | `a9d20a25b4e25d75` | **`fe3146627742ede5`** |
| `main.js` | 10,647,600 B | **10,652,379 B** (sha256 `c3c5c9bb6d418e8d919cb94aa7de4df9ba449056b7d6122c32279ce72e2d2bad`) |
| `compiled_tailwind.css` | 263,374 B | **263,273 B** (sha256 `147c3de455a5f3f1708d182a356361d66b9d097bf17749666802c7ad7d0b9673`) |
| the other 297 files | — | byte-identical |

The ZIP is the **second** embedded archive; the first is Go's `time/tzdata`
(`b2d18a7c8fa81420…`, byte-identical in both binaries).

**The reference's own live answers match the extracted bytes.** A standalone
reference on 127.0.0.1:8944 answers:

| path | status | content-type | bytes |
|---|---|---|---|
| `/` | 200 | `text/html; charset=utf-8` | 3,969 |
| `/index.html` | 200 | `text/html; charset=utf-8` | 3,969 |
| `/main.js` | 200 | `text/javascript; charset=utf-8` | **10,652,379** |
| `/jetbox.css` | 200 | `text/css; charset=utf-8` | 95,487 |
| `/compiled_tailwind.css` | 200 | `text/css; charset=utf-8` | **263,273** |
| `/prism_bundle.js` | 200 | `text/javascript; charset=utf-8` | 141,963 |
| `/diff_worker.js` | 200 | `text/javascript; charset=utf-8` | 9,952 |
| `/symbols-icons/x.svg` | 200 | `text/html; charset=utf-8` | 3,969 (SPA fallback) |
| `/unknown` | 200 | `text/html; charset=utf-8` | 3,969 (SPA fallback) |

The capture is in `out/ide-language-server/frontend-http-2282.txt`.

`internal/rcfrontend/assets/` and `out/ide-language-server/frontend/` are now
byte-identical to that extraction (`diff -rq` is silent).

**The patch anchors did not move.** `internal/rctunnel/patches` anchors on
exact substrings of the bundle, so a new bundle can silently break the
proxy. Applying the whole patch set to both bundles gives the **same**
verdict: 31 applied, 7 disabled, 9 "not found", 3 "not needed" — no anchor
regressed.

> One required patch, `base-url-origin`, matches nothing — and it matched
> nothing **before** this cycle either. Upstream now computes the RPC base
> from the origin itself (`b=a.baseUrl||window.location.origin`), so the
> patch is obsolete rather than broken. `MissingRequired()` is not called
> anywhere outside the tests, so this is a stale label in a report, not a
> runtime failure. It is left alone here (see *Open questions*).

## 3. The wire

The bundle embeds every `FileDescriptorProto` it parses as a base64 blob.
Diffing the **descriptor closure semantically** (not textually — the bundle is
Closure-minified with renamed identifiers) against the 2.28.0 closure:

* **one new message**: `exa.cortex_pb.PluginAuthor { name: string, url: string }`
* **`exa.cortex_pb.PluginItem`**: `+ author` (9, `PluginAuthor`), `+ homepage`
  (10, string), `+ repository` (11, string)
* **`exa.language_server_pb.Plugin`**: `+ author` (28, `PluginAuthor`),
  `+ homepage` (29, string), `+ repository` (30, string)
* **`exa.language_server_pb.MarketplaceCustomization`**: `+ author` (10),
  `+ homepage` (11), `+ repository` (12)
* **the service's method count did not change: 356**

`repository` is a plain **string** on the wire, while `plugin.json` allows
either a string or `{"type":"git","url":"https://…"}` — the loader flattens
the object form to the URL.

`lsproto/descriptor/language_server.protoset` was regenerated from the new
bundle (769,273 → **769,601 B**); `go test ./lsproto/...` is green, and
`out/ide-language-server/protos/language_server.protoset` carries the same
bytes.

## 4. The build identity

`internal/lsserver/stamp.go` now reports the 2.28.2 identity:

| variable | value |
|---|---|
| `StampBuiltOn` | `2026-10-29 14:08:59 +0800 CST` |
| `StampCL` | `1009437749` |
| `StampBaseline` | `1006933201` (unchanged) |
| `StampLabel` | empty (unchanged) |

The doc comment above them said `Built info:` where the reference prints
`Build info:` — a stale typo, corrected while the block was being rewritten.

`out/ide-language-server/stamp.txt`, `out/langserver/stamp.txt` and
`out/ide-language-server/identity.txt` carry the same values.

## 5. The reference's own bundled documents

The `//go:embed` file system is 118 records / 189,929 B (2.28.0: 118 /
185,241 B) — 71 files, **two** of which changed:

| file | what changed |
|---|---|
| `assets/shared/skills/plugin/SKILL.md` | the manifest table gained `author` (string / object), `homepage` (string) and `repository` (string / object) rows; and the closing note changed from "*`author` and `homepage` are silently discarded by the loader*" to "*Unknown top-level fields are silently discarded*" |
| `assets/external/skills/agy-customizations/docs/plugins.md` | the same three fields documented, with their shapes and their `https://` requirement |

Both files were copied into the clone's own bundle
(`internal/features/tools/skills/skills/`), which was in sync with the
reference's before this cycle.

**The CLI carries the same two documents** (agy 1.4.11, sha256 `09cefed6…` —
the same binary cycle 365 measured):

* `agy-customizations/docs/plugins.md` is **byte-identical** to the IDE's
  2.28.2 copy;
* `plugin/SKILL.md` differs from the IDE's copy in **exactly one line**:

  ```
  agents: [.agents[]?.agent.name],      ← CLI (agy 1.4.11)
  agents: [.agents[]?.name],             ← IDE (App 2.28.2)
  ```

  That line was already `.name` in the IDE's 2.28.0 copy, so the divergence is
  the IDE's, and it predates this cycle.

So the "author / homepage / repository are supported" documentation is **not**
an IDE-only change — the CLI's own bundle says the same thing.

## 6. The release notes

**v2.28.2 is not published.** The vendor's changelog
(`https://antigravity.google/docs/changelog.md`) has, as its newest entry,
**v2.28.1 — October 28, 2026**, whose three bullets are:

1. the **conversation navigator** (a rail at the left edge of a conversation,
   one dot per turn, grouped into chapters by date);
2. a **notice about third party developer tools** on the Marketplace tab and
   on third-party plugins' pages, linking the Google Privacy Policy;
3. **each plugin's developer, version and links on its page**.

Measured against the two bundles:

| release-note bullet | in the 2.28.0 bundle? | in the 2.28.2 bundle? |
|---|---|---|
| 1 · conversation navigator | **yes** (`"Conversation navigator"` × 1) | yes (unchanged) |
| 2 · third-party tools notice | **no** (`third-party-tools-disclaimer-text` × 0, `third-party` × 0) | **yes** (the flag × 1, `third-party` × 2, `third party` × 1) |
| 3 · plugin's developer, version, links | **no** (`homepage` × 0, `PluginAuthor` × 0) | **yes** (`homepage` × 4, `PluginAuthor` × 1, `plugin-detail-footer-homepage` × 1) |

So the 2.28.0 bundle the clone was already serving carried the **first**
bullet only: the conversation navigator. The 2.28.2 bundle adds the second
and the third — and the third is exactly the wire delta of §3:
`PluginAuthor` plus the three fields.

**The front end's own strings, new in 2.28.2**: the plugin detail page's
footer (`plugin-detail-footer-homepage`, `plugin-detail-footer-repository`,
a `<dl>` titled "Links") and the third-party notice (test id
`third-party-tools-disclaimer`, template variable `{privacyPolicyLink}` →
`https://policies.google.com/privacy`).

**The notice reaches the UI without a clone change**: the flag is a Mendel
flag, the clone passes the backend's flag set through verbatim
(`internal/remotecontrol/mendel_flags.go`), and the value is a JSON string the
front end `JSON.parse`s.

## 7. What was *not* re-run: the RE pipeline

The seeds, the Ghidra project and the decompiled output still describe the
**2.28.0** binary:

* `out/ide-language-server/seeds/{seeds,funcs}.txt` — 170,786 lines, the
  2.28.0 set;
* the `dec/` output, `name-diff/`, `cross-check.*` — all 2.28.0.

Every address moves on every release, so regenerating them is a separate,
multi-hour cycle, not a side effect of a bundle refresh. `out/ide-language-server/summary.txt`
now says so next to the numbers it carries, because those numbers (170,685 IDE
names) are the 2.28.0 ones.

The **CLI's** seed set (`out/local-macos/seeds/`) was regenerated by other
work in this tree and is **not** part of this cycle.

## 8. The plugin RPCs — an open gap, not a regression

The new plugin page needs the plugin RPCs to answer with an author and two
links. The clone implements **none** of them:

| method | clone |
|---|---|
| `GetPlugin`, `GetAllPlugins`, `SearchMarketplaceCustomizations`, `GetMarketplaceCustomization`, `GetMarketplaceBundles`, `GetMarketplaceFeatured`, `ListRemotePlugins`, `InstallCustomization`, … | not implemented |

(`internal/lsserver/dispatch.go` answers 17 methods; `internal/remotecontrol`
answers the **dashboard's** `ListRemoteAgentPlugins` /
`DisconnectRemoteAgentPlugin`, which are a different contract.)

So the clone's Web UI's Marketplace tab has **no data** to show — with or
without the new fields. That gap predates 2.28.2.

What the clone *does* carry for the plugin schema is the descriptor closure
(§3) and the reference's own documentation (§5), so the moment the RPCs are
implemented the fields are already in the schema.

**The clone's own `plugin.json` reader still drops the three fields.**
`internal/features/plugins/core.Manifest` keeps `name`, `description` and
`version`; the importer (`internal/features/plugins/import/`) keeps the
author's **name** only — which is exactly what the reference's staged
`plugin.json` carries (measured at agy 1.4.11, cycle 364). The clone's
`plugin validate` already accepts `author`, `homepage` and `repository` as
known fields, so no manifest is reported as carrying an unknown field.

No code change was made here: the fields have **no observable surface** in
the clone today (no panel shows them, no RPC answers them), and inventing a
surface the reference does not have would be a fabrication. The gap is
recorded here instead.

## 9. The CLI comparison, re-measured

`~/.local/bin/agy` is agy **1.4.11**, 197,441,184 B, sha256 `09cefed6…` — the
same binary cycle 365 measured (unchanged).

| | IDE `language_server` 2.28.2 | CLI `agy` 1.4.11 |
|---|---|---|
| tzdata ZIP sha256 | `b2d18a7c8fa81420…` | `b2d18a7c8fa81420…` (identical) |
| frontend ZIP sha256 | `fe3146627742ede5…` | `57a1cb1d13cc3de3…` |
| frontend ZIP size | 4,145,909 B | 5,251,052 B |
| frontend entries | 304 | 547 |
| files in common | 299 | 299 |
| **byte-identical** | **296 / 299** | **296 / 299** |
| `//go:embed` records | 118 / 189,929 B | 472 / 2,326,425 B |

The three that differ: `compiled_tailwind.css`, `main.js`,
`symbols-icons/symbol-icon-theme.json` — build-version differences, not
packaging ones. The CLI ships 240 files the IDE does not (179 Prism language
components, 32 extra file-type icons, 27 voice assets, two extra page
bundles) and 353 TextMate grammars the IDE does not have at all.

The guide's old claim — "301 of 304 entries are byte-identical; the three that
differ are `compiled_tailwind.css`, `index.html`, `main.js`" — was measured
against agy 1.4.7 and the 2.26.0-era IDE. It has been corrected.

**The CLI's bundle carries the same plugin schema** (`Plugin.author` /
`homepage` / `repository`, `PluginAuthor`) and the same third-party-notice
strings, and the CLI's service has **362** methods to the IDE's 356. The
plugin change is therefore a **shared monorepo change**, not an IDE-only one.

---

## 硬约束 / Hard constraints

1. **The clone's served bundle is the reference's bytes.** The 299 files in
   `internal/rcfrontend/assets/` are byte-identical to the 2.28.2 extraction,
   and the reference's own HTTP answers have the same content lengths.
2. **The clone's descriptor closure is the reference's schema.** It is
   extracted from the bundle the clone serves — not regenerated from a
   `.proto` source, which does not exist.
3. **The stamp is the reference's identity.** The IDE parses
   `/Built at CL: (\d+)/` out of stdout; a clone that printed anything else
   would lose the field in the IDE's crash report.
4. **The patch anchors are a moving target.** Every patch in
   `internal/rctunnel/patches` is anchored on an exact substring of the
   bundle. A bundle refresh must be followed by a patch-set comparison —
   otherwise a stale anchor silently disables a fix.
5. **The `//go:embed` file system and the embedded ZIP are two different
   mechanisms** with two different extraction paths (`-embed-out` vs
   `-zip-out -zip-index N`). The frontend is archive #1; archive #0 is Go's
   tzdata and is not Google's product asset.
6. **Local commits only.** Nothing in this cycle is pushed.

## Open questions / 存疑

- **v2.28.2's release notes do not exist.** The mapping in §6 is onto
  **v2.28.1**'s notes, because those are the notes the shipped features
  match; the 2.28.0 → 2.28.2 delta itself is a measurement, not a note.
- **`base-url-origin` is a required patch that matches nothing** — it
  matched nothing before this cycle either. Upstream computes the base from
  `window.location.origin` now. Marking it `Optional` would change the
  patch report, not the served bytes; it was left alone because it is a
  separate concern from a bundle refresh and `internal/rctunnel/` is busy
  with other work in this tree.
- **The plugin RPCs are still unimplemented**, so the new page features have
  no data (§8). Whether to implement them is a bigger decision than this
  cycle's.
- **The clone's own `plugin.json` reader still drops `author` / `homepage` /
  `repository`** (§8). The reference's loader keeps them; the clone has
  nowhere to show them today.
- **The `plugin/SKILL.md` one-line divergence** between the CLI's copy and
  the IDE's copy (§5) is the IDE's, and it predates this cycle. The clone
  ships the IDE's copy.
- **Whether the RE pipeline should be regenerated at 2.28.2** is a separate
  decision, not this cycle's (§7).

## Limitations

- **The RE pipeline was not re-run.** Every address in the repository still
  refers to the 2.28.0 binary. The seed set, the decompiled output and the
  name diffs are a *snapshot*, and `out/ide-language-server/summary.txt` says
  so where the numbers are.
- **No page was rendered.** The front-end features were identified from the
  bundle's own strings and test ids, not from a browser screenshot; the
  DOM-level proof of the plugin footer is missing.
- **Only the HTTP port was captured** from the live reference. The HTTPS
  (gRPC) port was not re-probed, and `/healthz` was not fetched.
- **`appVersion` in the served page was `0.0.0.0`** in the standalone
  capture — the standalone mode fills in a placeholder, and the real version
  travels through `--override_ide_version`. The app's own version was read
  from `Info.plist`, not from the page.
- **The CSS diff was not analysed semantically.** `compiled_tailwind.css`
  moved by 101 bytes (a handful of `gap-x-*` / `gap-y-*` utility classes);
  the clone serves the reference's bytes either way, so no semantic claim is
  made about them.
- **The 3 unresolved types in the protoset** (`exa.cortex_pb.CustomExtensions`,
  `google.internal.cloud.code.v1internal.Entitlement`, `google.rpc.Status`)
  are still unresolved — expected, and unchanged by this cycle.

---

Report language note: this file is English because `docs/cycles/*` is English.
The evidence files under `out/` are Chinese, per the repository's convention.

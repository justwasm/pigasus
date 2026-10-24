# Cycle 113 — the advertised tool surface is 60 tools, not 32

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the clone has advertised the wrong tool surface since cycle 97. The
reference advertises **60** tools; the clone advertised **32**, four of which the
reference never emits. This cycle replaces the pclntab-derived reconstruction
with a live capture.

---

## 1. How the surface was recovered 【一手】

The `stream-json` print-mode format emits an `init` event **before** the model
call, so it is capturable even though the account quota is exhausted:

```
$ agy -p='x' --output-format stream-json
{"event":"init","conversation_id":"9705c77f-…","init":{"cwd":"/tmp","tools":[…],"permission_mode":"always-proceed"}}
```

`init.tools` is the advertised list, already sorted by the reference
(`printmode.buildInitPayload` → `advertisedTools` → `sort.Strings`).

**The list is fixed, not workspace-filtered.** The capture was repeated from
three working directories — a bare `/tmp`, a temp workspace holding `.git` and
an `.ipynb`, and this Go repository — and all three produced the **same 60
names, byte-identical**. Only `init.cwd` changed.

The 60 names are checked in as
`internal/tools/testdata/advertised_tools_1.3.6.txt`.

## 2. What was wrong with the old surface

| | Old (pclntab reconstruction) | Captured |
|---|---|---|
| size | 32 | **60** |
| MCP tool | `call_mcp` | `call_mcp_tool` |
| invented | `codebase_search`, `notify_user`, `view_content_chunk` | absent |
| missing | — | 32 browser / subagent / workflow / marketplace names |

The reconstruction was built from the `tools.New*ToolConverter` /
`tools.New*Tool` symbol names in the pclntab. That works for the tools that have
a dedicated converter symbol, but the browser family (`browser_*`,
`capture_browser_*`, `click_browser_pixel`, `execute_browser_javascript`,
`open_browser_url`, `read_browser_page`, `list_browser_pages`), the subagent
family (`define_subagent`, `invoke_subagent`, `manage_subagents`,
`browser_subagent`) and the workflow/marketplace/knowledge tools
(`run_workflow`, `search_marketplace`, `list_plugin_accounts`,
`delete_knowledge`, `wait_5_seconds`) have no such symbol, so they were missed
entirely. Conversely `codebase_search`, `notify_user` and `view_content_chunk`
were recovered from converter symbols that are **not** on the advertised list —
the symbols exist, but the tools are not advertised.

## 3. Implementation

`internal/tools/catalog.go` — `referenceSpecs` rewritten in the captured
(sorted) order. Names with a real executor stay backed by it; the other 34 are
registered as `pendingTool`, which fails loudly with `ErrNotImplemented` rather
than silently succeeding.

Backed by a real executor (26): `ask_custom_permission`, `ask_permission`,
`ask_question`, `browser_click_element`, `browser_drag_pixel_to_pixel`,
`browser_press_key`, `browser_scroll`, `browser_select_option`,
`capture_browser_console_logs`, `capture_browser_screenshot`, `command_status`,
`find_by_name`, `finish`, `grep_search`, `list_dir`, `list_permissions`,
`manage_inbox`, `manage_task`, `multi_replace_file_content`, `notebook_edit`,
`notebook_execution`, `replace_file_content`, `run_command`, `schedule`,
`sed_file`, `send_command_input`, `send_message`, `view_file`, `wait`,
`write_to_file`.

Placeholders (30): the remaining `browser_*` verbs, `call_mcp_tool`,
`click_browser_pixel`, `define_subagent`, `delete_knowledge`,
`execute_browser_javascript`, `generate_image`, `invoke_subagent`,
`list_browser_pages`, `list_plugin_accounts`, `list_resources`,
`manage_subagents`, `open_browser_url`, `read_browser_page`, `read_resource`,
`read_url_content`, `run_workflow`, `search_marketplace`, `search_web`,
`wait_5_seconds`.

The three unadvertised executors (`CodebaseSearch`, `NotifyUserTool`,
`ViewContentChunk`) stay in the package — their implementations are still
correct — but are no longer advertised, exactly like the reference.

## 4. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* New tests: `TestCatalogMatchesRecoveredAdvertisedList` (the registry's names,
  sorted, must equal the 60-name fixture — the fixture is asserted to hold
  exactly 60 entries so a truncated file cannot pass) and
  `TestCatalogDropsUnadvertisedNames`.
* Three existing tests (`TestViewContentChunk`, `TestCodebaseSearch`,
  `TestHeadlessInteractionTools`) kept covering their executors by registering
  them explicitly on top of `Catalog`, with a comment recording why they are no
  longer advertised.
* **Wire-level verification.** With `OPENAGY_AGW_URL` pointed at a local capture
  server, the request body the clone actually sends was inspected:

  ```
  advertised on the wire: 60
  sorted == reference capture: True
  in reference order: True
  ```

  Before this cycle the same probe reported 32 with the four wrong names.
* **Live tool call still works** (the rename and the 28 extra placeholders did
  not disturb dispatch):

  ```
  $ agy -p='Use the run_command tool to run exactly: echo hello-from-tool. Then report its stdout verbatim.'
  rc=0
  Stdout verbatim:

  ```
  hello-from-tool
  ```
  ```

* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both documented cycle-101 diffs).

## 5. Divergences and limitations

* **34 of the 60 advertised tools have no executor.** They are advertised so
  the surface matches; calling one returns `ErrNotImplemented`, whereas the
  reference would run it (Chrome DevTools Protocol, subagents, marketplace).
  This is the same posture as before, just with the correct name list.
* **The tool *schemas* are still the OpenAI-shaped reconstruction.** Only the
  names are 【一手】 here. `pendingTool` schemas are the empty object
  `{"type":"object","properties":{}}`; the reference advertises Cortex protobuf
  schemas whose parameters are unrecovered. A model that calls a placeholder
  with arguments gets the arguments ignored.
* **`policy.Classify` still lists `codebase_search` and `view_content_chunk`**,
  which are no longer advertised. The entries are dead lookups; they were left
  in place because the browser family's read/write classification is unknown
  and guessing it would change permission-gate behaviour.
* **`wait` and `wait_5_seconds` are both advertised** by the reference. The
  clone backs `wait` with the real executor and leaves `wait_5_seconds` a
  placeholder, because the recovered `WaitTool` takes a duration argument and
  nothing in the capture distinguishes the two tools' semantics.
* **`init.permission_mode`** is captured as `always-proceed` (the default
  `toolPermission` setting). It is not emitted by the clone yet — that belongs
  to the `stream-json` implementation, not to the catalog.
* **`--output-format json` / `stream-json` are still unimplemented** (the clone
  prints plain text and ignores the flag). That is the next cycle; this one was
  done first because `stream-json`'s `init` event has to report a correct tool
  list.

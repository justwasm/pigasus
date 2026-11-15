# WireAlign — Wire-Level Parity & Prompt Alignment Harness

`wirealign` is an automated observation and alignment testing harness for **wire-level parity** between the official Google Antigravity binary (`agy`, v1.5.9) and `pigasus` (`openagy`).

### Why WireAlign is simpler than TUIAlign
- **TUIAlign** focuses on the terminal frontend: it requires orchestrating isolated `tmux` sessions, emulating streaming LLM thought reasoning and tokens, capturing screen buffers, parsing 24-bit ANSI SGR TrueColor sequences, and diffing Elm UI frame renders.
- **WireAlign** focuses strictly on the **OUTBOUND REQUEST**: it has no need to mock complex LLM thought streams or simulate turn responses. Its sole mission is ensuring that the outbound HTTP request (`systemInstruction` prompt structure, 12 model tool declarations, parameters schema, and contents) matches the reference with byte-for-byte and semantic fidelity.

### Single Source of Truth: `agytemplates`
Rather than relying on brittle, frozen `.json` capture snapshots that drift with developer environments, **WireAlign treats `internal/app/prompt/agytemplates/templates` as its Single Source of Truth**:
- System prompt sections are verified directly against the authentic Go templates recovered from the reference binary.
- Tool declarations are verified against `internal/app/tools/surface`.
- Every scenario check renders the authentic templates dynamically under that scenario's flags (`generic-system-prompt`, `planning_mode`, `sandbox`, `autonomous`).

---

## 1. Core Architecture & Features

```
┌────────────────────────────────────────────────────────┐
│                      WireAlign                         │
│ ┌─────────────────┐  ┌───────────────────────────────┐ │
│ │  Proxy / Server │  │   Payload & Prompt Parser     │ │
│ │  (Mock / Relay) │  │   (XML Deconstruction)        │ │
│ └────────┬────────┘  └───────────────┬───────────────┘ │
│          │                           │                 │
│          ▼                           ▼                 │
│ ┌────────────────────────────────────────────────────┐ │
│ │                  Diff Engine                       │ │
│ │  - Prompt Section Order & Byte Parity              │ │
│ │  - Tools Surface (12 Declarations, Schemas)        │ │
│ │  - Semantic Diagnostics (Sandbox, Identity, Rules) │ │
│ └────────────────────────────┬───────────────────────┘ │
│                              ▼                         │
│ ┌────────────────────────────────────────────────────┐ │
│ │                  Report Engine                     │ │
│ │  - ANSI Terminal Report with Pass/Fail Badges      │ │
│ │  - Standalone Visual HTML Dashboard                │ │
│ └────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────┘
```

1. **Prompt XML Deconstruction**:
   - Parses the system instruction into discrete XML tags (`<identity>`, `<terminal_sandbox>`, `<artifacts>`, `<conversation_transcript>`, `<guidelines>`, `<communication_style>`, `<slash_commands>`, `<messaging>`, `<subagents>`, `<skills>`, `<user_rules>`, `<user_information>`).
   - Inspects section presence, sequence order, and byte length.
   - Verifies crucial prompt switches (e.g. `generic-system-prompt: true`, "Files relating to..." vs "Code relating to...").

2. **Tool Declarations & Schema Parity**:
   - Compares model-facing tools (`tools[].functionDeclarations`).
   - Verifies the strict absence of `run_command.BypassSandbox` in unsandboxed mode, and its presence in sandboxed mode.
   - Guards against description leakage (e.g. ensuring `invoke_subagent` does not carry extraneous catalog text).

3. **Deterministic Mock & Interception Server**:
   - Runs a local CloudCode endpoint on `127.0.0.1:<port>`.
   - Embeds pre-captured RPC fixtures (`loadCodeAssist`, `onboardUser`, `fetchAvailableModels`, `retrieveUserQuotaSummary`) allowing offline execution without Google accounts or network connections.
   - Capable of live traffic relaying to real CloudCode endpoints (`-upstream URL`) with automatic JSON wire dumping.

4. **Ephemeral Jitter Normalization**:
   - Automatically masks non-deterministic run variables (UUIDs, timestamps, temporary directory roots, home paths) so comparison reflects pure prompt and tool schema semantics.

5. **Visual HTML & ANSI Diagnostics**:
   - Generates standalone visual HTML reports (`wirealign diff -html report.html ...`).
   - Identifies specific diagnostic codes:
     - `DIAG_SANDBOX_MISMATCH`: Leaked sandbox tags or parameters
     - `DIAG_IDENTITY_DRIFT`: Non-generic coding identity
     - `DIAG_SECTION_ORDER`: Inverted section tags
     - `DIAG_USER_INFO_WORDING`: Outdated file wording
     - `DIAG_INVOKE_SUBAGENT_DESCRIPTION`: Dirty subagent tool descriptions

---

## 2. Common Scenarios Supported

| Scenario | Scope & Verifications |
|---|---|
| `standard_turn` | Standard interactive 1st turn: clean unsandboxed prompt (no `<terminal_sandbox>`), `generic-system-prompt: true`, 12 base tools without `BypassSandbox`. |
| `sandboxed_turn` | Sandboxed execution: `<terminal_sandbox>` tag present between `<identity>` and `<artifacts>`, `run_command` has `BypassSandbox` parameter. |
| `subagent_turn` | Delegated subagent execution: isolated messaging prompt, restricted tools, identity alignment. |
| `clean_no_rules` | Empty workspace without `AGENTS.md`: `<user_rules>` omitted, `<skills>` immediately precedes `<user_information>`. |
| `compact_turn` | Post-compaction continuation turn: verifies `<CONTEXT_SUMMARY>` formatting in conversation history. |

---

## 3. CLI Quickstart

### Compare two captured wire JSON files
```bash
./scripts/wire-align.sh diff agy_req.json pigasus_req.json
```

### Generate a visual HTML report
```bash
./scripts/wire-align.sh diff -html /tmp/wire_parity.html agy_req.json pigasus_req.json
```

### Run built-in scenario checks
```bash
./scripts/wire-align.sh test
# or via Makefile
make wirealign
```

### Start standalone capture & mock proxy
```bash
./scripts/wire-align.sh server -addr 127.0.0.1:9099 -capture-dir /tmp/wire_captures
# In another terminal:
export CLOUD_CODE_URL=http://127.0.0.1:9099
pigasus -p "check wire"
```

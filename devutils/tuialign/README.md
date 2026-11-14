# TUIAlign — Scriptable TUI Parity & Alignment Testing Harness

`tuialign` is a deterministic testing and visual alignment harness designed to achieve pixel-perfect fidelity between the official Google Antigravity binary (`agy`, v1.5.8) and `pigasus` (`openagy`).

It eliminates non-determinism from real LLM APIs and network latency by orchestrating isolated `tmux` sessions alongside an embedded, mock CloudCode proxy server.

---

## 1. Key Capabilities

1. **Zero-Patch Compatibility with Official `agy`**:
   - Intercepts all traffic via `CLOUD_CODE_URL=http://127.0.0.1:<port>`.
   - Embeds real CloudCode RPC fixtures (`loadCodeAssist`, `onboardUser`, `retrieveUserQuotaSummary`, and the full 22-model `fetchAvailableModels` dictionary from `daily-cloudcode-pa.googleapis.com`).
   - Works fully offline without Google accounts, network connections, or OAuth tokens.
   - Handles background agent tasks automatically (e.g. `checkpoint/...` title generators) so they do not exhaust programmed test turns.

2. **Deterministic Mock LLM Engine**:
   - Streams SSE candidate frames with custom delays, thought reasoning, thought token metrics, text chunks, tool calls, and usage statistics.
   - Per-client turn tracking ensures both binaries receive identical SSE frames in identical sequence.

3. **Isolated `tmux` Automation**:
   - Runs in a dedicated socket (`-L tuialign -f /dev/null`) to prevent interfering with user tmux sessions.
   - Uses temporary disposable Git workspaces (`mktemp -d` + `git init`) for instant sub-second boot times.
   - Automatically handles workspace trust gates.

4. **Two Scriptable Interfaces**:
   - **Declarative DSL (`.align`)**: High-level test scenarios with clean syntax (`MOCK_TURN`, `BOOT_BOTH`, `WAIT_READY`, `TYPE`, `ENTER`, `KEYS`, `SNAPSHOT`, `DIFF`).
   - **Bash/Shell Library (`tuialign.sh`)**: Sourced shell functions (`tuialign_start`, `tuialign_mock_turn_thought`, `tuialign_boot_both`, `tuialign_send`, `tuialign_wait`, `tuialign_snapshot`) for native bash automation.

5. **Interactive Live Mode**:
   - Splits tmux horizontally (`agy` on the left, `pigasus` on the right) with `synchronize-panes on`.
   - Every keystroke you type is sent simultaneously to both TUIs in real time against the local mock backend.

6. **Parity Diff Engine & Color-Aware Alignment**:
   - Captures screen buffers with embedded 24-bit TrueColor sequences (`capture-pane -e -p`).
   - Virtual terminal SGR parser converts escape codes into structured `StyledCell { Rune, Fg, Bg, Bold, Dim, ... }`.
   - Distinguishes **Text Diffs** (`✗`) vs **Color Diffs** (`~` e.g. text matches, but `fg #5F6368` vs `#657B83`).
   - Strips dynamic non-functional jitters (elapsed durations like `149ms` vs `152ms`, temp paths, email/account lines).
   - Generates standalone **HTML Visual Parity Reports** (`/tmp/tuialign_<snapshot>.html`) where ANSI is mapped to inline CSS spans, viewable in any browser.

---

## 2. Directory Structure

```
devutils/tuialign/
├── main.go                     # CLI entrypoint (run, live, server, diff)
├── server.go                   # Mock CloudCode HTTP/SSE server & REST control API
├── fixture.go                  # Embedded CloudCode RPC payloads
├── fixtures/
│   └── models.json             # Real 22-model CloudCode catalogue fixture (143KB)
├── tmux.go                     # Tmux driver & workspace isolator
├── diff.go                     # Visual side-by-side diff & parity calculation
├── scenario.go                 # .align DSL parser & test runner
├── tuialign.sh                 # Sourced Bash driver library
├── scenarios/
│   ├── 01_thinking_toggle.align        # Thought streaming & ctrl+o expansion test
│   ├── 02_queued_message.align         # In-flight message queue & prompt steering test
│   ├── 03_long_line_wrap.align         # 1.5.7 long continuous line wrap test
│   ├── 04_markdown_codeblock.align     # 1.5.7 markdown code fence & inline code test
│   ├── 05_multi_turn_conversation.align# Multi-turn conversation & turn separator test
│   ├── 06_tool_call_execution.align    # Tool call execution & step collapse/expand test
│   ├── 07_slash_commands_menu.align    # Slash command picker (/) popup & filtering test
│   ├── 08_tool_failure_error.align     # Non-zero exit code failure formatting & thought order
│   ├── 09_multiple_tools_group.align   # 1.5.7 multi-tool calls grouping in a single turn
│   ├── 10_composer_multiline_and_clear.align # Input editing, backspacing & Ctrl+U line kill
│   ├── 11_config_panel.align           # 1.5.7 /config settings panel & agentMode defaults
│   ├── 12_streaming_live_indicator.align # Live Generating... indicator vs finished thought
│   ├── 13_file_read_tool_step.align    # read_file tool step summary label & line count
│   ├── 14_help_shortcuts_panel.align   # /help panel navigation & exit note
│   ├── 15_model_picker.align           # 1.5.7 /model picker overlay & effort selector
│   ├── 16_empty_prompt_and_whitespace.align # Empty prompt & whitespace Enter submission
│   ├── 17_status_panel.align           # /status / /statusline toggle behavior
│   ├── 18_permissions_panel.align      # 1.5.7 /permissions rule editor & scope selection
│   ├── 19_tasks_panel.align            # 1.5.7 /tasks background task monitoring
│   ├── 20_changelog_panel.align        # /changelog viewer, scrolling & line ranges
│   ├── 21_settings_showtips_off.align  # --settings flag & showTips=false noise suppression
│   ├── 22_btw_side_question.align      # /btw side question card (thick border, thought isolation)
│   ├── 23_steer_interrupt_turn.align   # Mid-stream user steering (STEER) & queue transition
│   ├── 24_statusline_toggle_and_config.align # Statusline configuration styles & /statusline toggle
│   ├── 25_subagent_trajectory_detail.align   # Subagent detail: header, tools, answer & Trajectory section
│   ├── 26_tool_output_preformatted.align     # run_command body keeps its indentation & backticks (no markdown)
│   ├── 27_repeated_tool_steps.align          # Sequential bash steps: one row, the last call
│   ├── 28_parallel_tool_batch.align          # Parallel batch: last call collapsed, all expanded
│   ├── 29_subagent_not_in_tasks.align        # A delegated subagent is not a /tasks row
│   ├── 30_bottom_right_status_working_alignment.align # Bottom-right statusline (auto · Gemini 3.8 Flash · high, no working leak)
│   ├── 31_terminal_resize_sigwinch.align       # Terminal resize mid-turn (SIGWINCH dynamic reflow)
│   └── run_shell_example.sh            # Native bash script automation example
└── README.md
scripts/
└── tui-align.sh                # Executable wrapper (auto-compiles /tmp/pigasus)
```

---

## 3. Quickstart

### Running an Automated Scenario

```bash
# Run thinking toggle alignment test
./scripts/tui-align.sh run devutils/tuialign/scenarios/01_thinking_toggle.align

# Run message queue alignment test
./scripts/tui-align.sh run devutils/tuialign/scenarios/02_queued_message.align
```

### Running a Native Bash Test Script

```bash
bash devutils/tuialign/scenarios/run_shell_example.sh
```

### Launching Interactive Live Mode (Dual Synchronized Panes)

```bash
# Opens an interactive tmux session with both agy and pigasus connected to the mock server
./scripts/tui-align.sh live
```

Inside live mode:
- Pane 0 (left): official `agy`
- Pane 1 (right): `pigasus`
- Synchronized typing: anything you type is mirrored to both panes.

---

## 4. Scenario DSL (`.align`) Reference

| Command | Description | Example |
|---|---|---|
| `NAME <string>` | Name of the test scenario | `NAME "Thinking Toggle"` |
| `WINDOW <w> <h>` | Pane dimensions | `WINDOW 100 30` |
| `TARGET <agy\|pigasus\|both>` | Which binary to boot | `TARGET both` |
| `MOCK_TURN` | Begins a new mock turn specification | `MOCK_TURN` |
| `THOUGHT <text> [TOKENS <n>] [DELAY <d>]` | Emits thought reasoning chunk | `THOUGHT "Analyzing..." TOKENS 120 DELAY 100ms` |
| `TEXT <text> [DELAY <d>]` | Emits model text chunk | `TEXT "Hello world!" DELAY 50ms` |
| `TOOL <name> <json_args>` | Emits a tool call chunk | `TOOL run_command {"CommandLine":"ls"}` |
| `USAGE [PROMPT <n>] [CANDIDATES <n>] [THOUGHTS <n>]` | Sets token metrics | `USAGE PROMPT 18000 CANDIDATES 20 THOUGHTS 120` |
| `FINISH <STOP\|MAX_TOKENS>` | Marks turn completion | `FINISH STOP` |
| `BOOT_BOTH` | Spawns side-by-side tmux session | `BOOT_BOTH` |
| `WAIT_READY [timeout]` | Waits for prompt `> ` | `WAIT_READY 10s` |
| `WAIT_TEXT <text> [timeout]` | Waits until text appears | `WAIT_TEXT "Hello" 15s` |
| `EXPECT_TEXT <text> [timeout]` | Same wait, but reported as a pass/fail assertion — a missing string ends the scenario with exit 1 | `EXPECT_TEXT "int \`json:\"input_tokens\"\`" 5s` |
| `ASSERT_NOT_TEXT <text>` | Fails when the string *is* on screen — the negative half of a gate | `ASSERT_NOT_TEXT "int json:"` |
| `ASSERT_PARITY <pct>` | Fails when the last snapshot's parity is below `pct` | `ASSERT_PARITY 80` |
| `TYPE <text>` | Types text into input buffer | `TYPE "hi"` |
| `ENTER` | Presses Enter (`C-m`) | `ENTER` |
| `KEYS <key...>` | Sends raw tmux keys | `KEYS C-o` |
| `SLEEP <duration>` | Pauses test execution | `SLEEP 500ms` |
| `RESIZE <w> <h>` | Resizes terminal dimensions mid-session (sends `SIGWINCH`) | `RESIZE 80 25` |
| `SNAPSHOT <label>` | Captures screens and logs diff | `SNAPSHOT "expanded"` |
| `DIFF [min%]` | Computes summary parity metrics; with a percentage it also fails the run when any snapshot is below it | `DIFF 100` |
| `CLEANUP` | Terminates tmux session and server | `CLEANUP` |

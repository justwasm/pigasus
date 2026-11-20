## 1.5.13

- Added Gemini 4 Neon (Low, Medium, High, and XHigh) to `/model` and `--model` when you sign in with Application Default Credentials (`AGY_ADC_AUTH`), and added support for Gemini 4 Neon through an enterprise model gateway.
- Improved mouse selection in Markdown tables in the `full screen (altscreen)` rendering mode and in the artifact viewer, where dragging now selects text column by column instead of copying across column borders, and copying a whole table copies its cells separated by tabs.
- Improved multi-step efficiency: the agent now executes independent searches, reads, and edits in parallel in the same turn and chains related commands to reduce round-trips.
- Fixed custom models in `settings.json` ignoring `modelFeatures` flags set to `false`, such as `supportsThinking` and `supportsToolCalls`; the CLI no longer asks the model for thinking or tool calls when the model is configured as not supporting them.
- Fixed agents autonomously starting a swarm without `/swarm` being invoked; the swarm skill is no longer offered to the agent on its own.
- Fixed `Ctrl` and `Alt` shortcuts, such as `Ctrl+C` and `Ctrl+D`, failing to match on non-QWERTY keyboard layouts, such as Neo2, Dvorak, AZERTY, and German QWERTZ, in terminals like Ghostty.
- Fixed `Enter` in `!` shell mode in Vim mode inserting a newline instead of running the command.
- Fixed `agy update` on Android (Termux) failing with a `/apex/com.android.runtime/bin` directory error.
- Fixed the `/config` hint for Queued Messages under `Send Immediately` to accurately describe that messages join the current turn and the agent reads them after its current step instead of interrupting the agent.
- Fixed JSON output and `--output-format json` / `stream-json` status reading `ERROR` after a turn succeeded on retry.
- Fixed `agy acp` creating new sessions in the server working directory instead of the editor's workspace folder (`params.Cwd`).
- Fixed `agy acp` ignoring embedded resource contents attached to user prompts, such as unsaved buffer contents from Zed.
- Fixed `agy acp` model list including internal placeholder models (`MODEL_PLACEHOLDER_...`) and disambiguated models sharing the same name by appending their ID.

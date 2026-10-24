// parser_flags.go — flag specifications for StandardFlags and ExtraFlags.
package cli

// StandardFlags returns the top-level flags emitted by the reference
// binary's `--help`. Order, type, short alias and aliases match 1:1.
//
// The visible column is the reference's 1.4.6 `--help`: 21 long flags
// plus the 3 short aliases (-c, -i, -p) — 24 entries. Three more are
// Hidden, so they never show up in `--help`:
//
//   - the recovered hidden `-v N` int flag (cycle 101);
//   - the hidden `--verbose` bool flag added by the
//     headless-jsonl-mode-trailing-newline cycle (AGY 1.2.7 — emits the
//     `assistant_message` event in `--output-format stream-json`);
//   - the hidden `--print-partial-response` bool flag.
//
// `--print-partial-response` is hidden because the reference has no
// such flag: the 1.4.6 binary carries no "print-partial-response"
// string, its `--help` does not list it, and its changelog never
// mentions it (the cycle report that added it quoted a 1.2.10
// changelog line that is in neither the live changelog nor the
// vendored `internal/cli/testdata/changelog_reference.txt`). It stays
// parseable as a clone extra, like `--verbose`; the visible `--help`
// stays byte-identical with the reference's.
func StandardFlags() []FlagSpec {
	return append(standardFlagsCore(), standardFlagsTail()...)
}

func standardFlagsCore() []FlagSpec {
	return []FlagSpec{
		{Name: "add-dir", Type: "stringSlice", Def: "[]", Usage: "Add a directory to the workspace (repeatable)"},
		{Name: "agent", Type: "string", Usage: "Agent for the current CLI session"},
		{Name: "c", Type: "bool", Usage: "Short alias for --continue"},
		{Name: "continue", Type: "bool", Usage: "Continue the most recent conversation"},
		{Name: "conversation", Type: "string", Usage: "Resume a previous conversation by ID"},
		{Name: "dangerously-skip-permissions", Type: "bool", Usage: "Auto-approve all tool permission requests without prompting"},
		{Name: "disable-slash-commands", Type: "bool", Usage: "Disable slash command and skill expansion in print mode"},
		{Name: "effort", Type: "string", Usage: "Reasoning effort for the current CLI session (low|medium|high|xhigh|max)"},
		{Name: "i", Type: "string", Usage: "Short alias for --prompt-interactive"},
		{Name: "input-format", Type: "string", Def: "text", Usage: "Input format for print mode (text, stream-json). stream-json reads one NDJSON message per line from stdin and runs a turn for each; it requires --output-format stream-json"},
		{Name: "json-schema", Type: "string", Usage: "Optional JSON schema string or path to a schema file to enforce structured output (for stream-json, only applicable to the final result)"},
		{Name: "log-file", Type: "string", Usage: "Override CLI log file path"},
		{Name: "mode", Type: "string", Usage: "Set the agent execution mode for this session (" + ModeList() + ")"},
		{Name: "model", Type: "string", Usage: "Model for the current CLI session"},
	}
}

func standardFlagsTail() []FlagSpec {
	return []FlagSpec{
		{Name: "new-project", Type: "bool", Usage: "Create a new project for this session"},
		{Name: "output-format", Type: "string", Def: "text", Usage: "Output format for print mode (text, json, stream-json)"},
		{Name: "p", Type: "string", Usage: "Short alias for --print"},
		{Name: "print", Type: "string", Usage: "Run a single prompt non-interactively and print the response"},
		// Hidden: the reference 1.4.6 has no such flag (no string in
		// the binary, absent from its --help and from its changelog).
		// Parseable as a clone extra so the partial-response
		// side-channel stays reachable without diverging from the
		// reference's visible --help.
		{Name: "print-partial-response", Type: "bool", Hidden: true, Def: "false", Usage: "Stream each chunk of the model's output to stdout in headless mode as it arrives instead of buffering until the turn completes"},
		{Name: "print-timeout", Type: "duration", Def: "0s", Usage: "Optional time limit for print mode; 0 waits until the turn completes"},
		{Name: "project", Type: "string", Usage: "Project ID or project name for the current CLI session"},
		{Name: "prompt", Type: "string", Usage: "Alias for --print"},
		{Name: "prompt-interactive", Type: "string", Usage: "Run an initial prompt interactively and continue the session"},
		{Name: "remote-control", Type: "bool", Usage: "Create a remote connection for the CLI session on start up"},
		{Name: "sandbox", Type: "bool", Usage: "Run in a sandbox with terminal restrictions enabled"},
		{Name: "v", Type: "int", Hidden: true, Usage: ""},
		{Name: "verbose", Type: "bool", Hidden: true, Def: "false", Usage: "Print extra stream-json events (assistant_message) in --output-format stream-json"},
	}
}

// ExtraFlags returns the cycle-33+ openagy-specific flags that are NOT
// part of the v4 binary 1:1 surface.
func ExtraFlags() []FlagSpec {
	return append(extraFlagsCore(), extraFlagsRC()...)
}

func extraFlagsCore() []FlagSpec {
	return []FlagSpec{
		{Name: "backend", Type: "string", Hidden: true, Usage: "Backend type: gateway (real) or shim (mock). Default: gateway (auto-falls back to shim if no OAuth token)"},
		{Name: "alt-screen", Type: "bool", Def: "true", Hidden: true, Usage: "Use the alternate screen buffer (default true; toggle at runtime via Ctrl+L)"},
		{Name: "no-alt-screen", Type: "bool", Def: "false", Hidden: true, Usage: "Disable alt-screen at startup (overrides --alt-screen; runtime toggle still works)"},
		{Name: "trust-cwd", Type: "bool", Def: "false", Hidden: true, Usage: "Skip the on-startup workspace-trust dialog (or set $OPENAGY_TRUST=1)"},
		{Name: "max-thinking-budget", Type: "int", Def: "0", Hidden: true, Usage: "Cap thinking tokens per turn (integer); values <= 0 disable thinking entirely"},
		{Name: "diff-with-agy", Type: "bool", Def: "false", Hidden: true, Usage: "Print AGY 1.4.2 feature parity (aligned / gap / diverged)"},
		{Name: "debug", Type: "bool", Def: "false", Hidden: true, Usage: "Enable verbose debug logging"},
	}
}

func extraFlagsRC() []FlagSpec {
	return []FlagSpec{
		{Name: "remote-control-tcp", Type: "string", Def: "", Hidden: true, Usage: "Bind addr (host:port) for the WebSocket JSON-RPC transport; empty disables. Replaces --remote-control when set"},
		{Name: "remote-control-socket", Type: "string", Def: "", Hidden: true, Usage: "Override the unix socket path for --remote-control. $OPENAGY_RC_SOCKET still wins"},
		{Name: "experimental-rc-v2", Type: "bool", Hidden: true, Usage: "Opt-in to the deferred v2 (WebRTC mesh / Hub) features. No-op today; reserved for forward compat."},
	}
}

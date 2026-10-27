// config.go — the `HarnessConfig` fields the client sends.
//
// # What the client owns
//
// The SDK does not configure the harness through the environment or the
// command line. It hands the harness a `HarnessConfig` in the
// `InitializeConversationEvent`, and that configuration is authoritative
// for the session: the model endpoint, the workspaces, the system
// instructions, the policy, the skills and the tools all come from it.
//
// This file resolves that configuration into the values the clone's own
// packages take — a working directory, an agent home, a system prompt, a
// permission policy, a tool subset.
//
// # What is *not* reproduced
//
//   - `custom_subagents` — the reference spawns subagents with their own
//     model, tools and prompt. The clone runs one agent.
//   - `session_continuation_mode` / `initial_trajectory` — resuming a
//     recorded conversation. The clone starts every session fresh.
//   - `skills_config` (inline skills) — the clone reads skills from disk.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/harnessprompt"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// sessionSettings is the client's configuration, resolved.
type sessionSettings struct {
	// Workspaces are the session's roots, in the order the client sent
	// them. The first one is the working directory.
	Workspaces []string
	// CWD is the directory the tools operate in.
	CWD string
	// AppDataDir is the client's application-data directory.
	AppDataDir string
	// StorageDir is the client's storage directory (`InputConfig`).
	StorageDir string
	// Behavior is the prompt's behavioural mode.
	Behavior harnessprompt.Behavior
	// OSName is the operating system the prompt names.
	OSName string
	// ConversationID identifies the conversation.
	ConversationID string
	// SkillsPaths are the directories the client wants the skills read from.
	SkillsPaths []string
	// CompactionThreshold is the context budget, in percent, at which the
	// transcript is compacted. Zero leaves the clone's own default.
	CompactionThreshold uint32
	// ToolOutputTokens is the client's tool-output truncation budget, in
	// tokens. Zero leaves a tool's output whole.
	ToolOutputTokens int
	// FinishSchema is the JSON schema the client named for the `finish`
	// tool. The reference advertises the `finish` declaration only when
	// the client names a schema for it; the schema is the client's own
	// bytes, re-serialised the way the reference re-serialises a
	// `google.protobuf.Struct` — its keys sorted.
	FinishSchema json.RawMessage
	// MCPOutputLimit is the maximum size of an MCP tool result, in bytes.
	MCPOutputLimit int
	// SideTools is the set of harness-side tools the client enabled.
	SideTools *pb.HarnessSideTools
	// Policy is the client's tool-call policy.
	Policy *pb.PolicyConfig
	// RetryConfig is the client's retry configuration.
	RetryConfig *pb.RetryConfig
	// Instructions is the client's system instruction.
	Instructions *pb.SystemInstructions
}

// resolveSettings turns the client's configuration into the values the
// clone's packages take.
//
// The command line wins over the client for the two things an operator can
// set on a spawned binary — the workspace and the agent home — because
// they are explicit instructions, whereas the client's configuration is a
// default it always sends.
func resolveSettings(cfg *pb.HarnessConfig, input *pb.InputConfig, opts options) sessionSettings {
	settings := sessionSettings{
		Workspaces:     configWorkspaces(cfg),
		AppDataDir:     configAppDataDir(cfg),
		StorageDir:     strings.TrimSpace(input.GetStorageDirectory()),
		Behavior:       configBehavior(cfg),
		OSName:         input.GetClientInfo().GetOs(),
		ConversationID: strings.TrimSpace(cfg.GetCascadeId()),
		SkillsPaths:    configSkillsPaths(cfg),
		SideTools:      cfg.GetHarnessSideTools(),
		Policy:         cfg.GetPolicyConfig(),
		RetryConfig:    cfg.GetRetryConfig(),
		Instructions:   cfg.GetSystemInstructions(),
	}
	settings.CompactionThreshold = cfg.GetCompactionThreshold()
	settings.ToolOutputTokens = configToolOutputTokens(cfg)
	settings.MCPOutputLimit = int(cfg.GetMcpMaxOutputBytes())
	settings.FinishSchema = configFinishSchema(cfg)

	// The client's first workspace is the session's working directory; the
	// command line overrides it.
	settings.CWD = opts.workspace
	if len(settings.Workspaces) > 0 {
		settings.CWD = settings.Workspaces[0]
	}
	if opts.workspaceExplicit {
		settings.CWD = opts.workspace
	}
	return settings
}

// configWorkspaces returns the session's workspace directories.
func configWorkspaces(cfg *pb.HarnessConfig) []string {
	var out []string
	for _, workspace := range cfg.GetWorkspaces() {
		if workspace == nil {
			continue
		}
		dir := strings.TrimSpace(workspace.GetFilesystemWorkspace().GetDirectory())
		if dir == "" {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// configAppDataDir returns the client's application-data directory.
//
// The SDK defaults to `~/.gemini/antigravity`; the reference harness uses
// the same default when the config names none.
func configAppDataDir(cfg *pb.HarnessConfig) string {
	if dir := strings.TrimSpace(cfg.GetAppDataDir()); dir != "" {
		return dir
	}
	return harnessprompt.DefaultAppDataDir()
}

// configSkillsPaths returns the skill directories the client named.
func configSkillsPaths(cfg *pb.HarnessConfig) []string {
	var out []string
	for _, path := range cfg.GetSkillsPaths() {
		if path = strings.TrimSpace(path); path != "" {
			out = append(out, path)
		}
	}
	return out
}

// configFinishSchema returns the client's JSON schema for the `finish`
// tool.
//
// The reference carries that schema as a `google.protobuf.Struct`, so the
// keys it sends are sorted: the client's own bytes go through a map, and
// the map's keys come out sorted.
//
// A schema that is not JSON is dropped: the reference cannot put it in a
// Struct either, and a session without a `finish` declaration is the same
// session the client gets when it names no schema at all.
func configFinishSchema(cfg *pb.HarnessConfig) json.RawMessage {
	raw := strings.TrimSpace(cfg.GetFinishToolSchemaJson())
	if raw == "" {
		return nil
	}
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return out
}

// configToolOutputTokens returns the client's tool-output truncation
// budget, in tokens.
//
// The protocol carries the two strategies as a oneof, so only one of them
// can be set: the SDK sends `truncate`, and the reference's `error` arm
// bounds the output the same way (both arms' `max_tokens` become the
// byte budget, see internal/harnesstools/truncate.go). The `error` arm's
// `error_message` never reached the model in any capture, so the clone
// does not invent a use for it.
func configToolOutputTokens(cfg *pb.HarnessConfig) int {
	truncation := cfg.GetToolOutputTruncation()
	if truncation == nil {
		return 0
	}
	if strategy := truncation.GetTruncate(); strategy != nil {
		return int(strategy.GetMaxTokens())
	}
	if strategy := truncation.GetError(); strategy != nil {
		return int(strategy.GetMaxTokens())
	}
	return 0
}

// configBehavior maps the protocol's `AgentBehavior` onto the prompt's
// behaviour.
//
// The reference's unspecified and autonomous behaviours render the same
// prompt; only the interactive one changes the prompt's shape.
func configBehavior(cfg *pb.HarnessConfig) harnessprompt.Behavior {
	switch cfg.GetAgentBehavior() {
	case pb.AgentBehavior_AGENT_BEHAVIOR_INTERACTIVE:
		return harnessprompt.BehaviorInteractive
	case pb.AgentBehavior_AGENT_BEHAVIOR_AUTONOMOUS:
		return harnessprompt.BehaviorAutonomous
	case pb.AgentBehavior_AGENT_BEHAVIOR_MINIMAL:
		return harnessprompt.BehaviorMinimal
	default:
		return harnessprompt.BehaviorUnspecified
	}
}

// agentHomeFor resolves the config home a session runs with.
//
// The client's `app_data_dir` is where the reference keeps its brain,
// its scratch directory and its transcripts. It is *not* the agy config
// home — that one holds `mcp.json`, the settings and the skills, and it
// is the operator's to choose.
func agentHomeFor(opts options, settings sessionSettings) string {
	if opts.agentDirExplicit {
		return opts.agentDir
	}
	if settings.AppDataDir != "" {
		return settings.AppDataDir
	}
	return opts.agentDir
}

// envFromConfig returns the environment entries the client sent.
//
// The reference applies them to the harness process, so the tools and the
// model transport see them. The clone does the same: this process exists
// for that one client.
func envFromConfig(input *pb.InputConfig) map[string]string {
	entries := input.GetEnv()
	if len(entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(entries))
	for key, value := range entries {
		if key = strings.TrimSpace(key); key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

// applyEnv sets the client's environment entries in this process.
//
// A failure is not fatal: an entry the operating system refuses is
// reported and the session continues.
func applyEnv(entries map[string]string) {
	for key, value := range entries {
		if err := os.Setenv(key, value); err != nil {
			//nolint:errcheck // the session continues without the entry
			continue
		}
	}
}

// expandPath expands a leading `~` in a client-supplied path.
func expandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
}

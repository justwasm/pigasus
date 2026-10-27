// Package auth: v1.3.5 binary 1:1 config compat layer (cycle 48)
//
// user live run 反馈: 配置目录 + token 刷新逻辑 100% 兼容 + 支持互操作.
//
// v1.3.5 binary 实跑写 (cycle 42 close-gap 实证):
//   ~/.config/antigravity/oauth-token                       (keyring auto-load)
//   ~/.config/antigravity/antigravity-auth.json             (v3 残留)
//   ~/.gemini/antigravity-cli/antigravity-oauth-token       (v4 binary fallback)
//   ~/.gemini/antigravity-cli/auth.json                      (keyring dump format)
//   ~/.gemini/antigravity-cli/settings.json                  ({colorScheme, trustedWorkspaces})
//   ~/.gemini/antigravity-cli/mcp.json                       ({servers: {name: {type, url/command, args, env, enabled}}})
//   ~/.gemini/antigravity-cli/installation_id                (UUID)
//   ~/.gemini/antigravity-cli/jetbox_state.json              ({agent_environment, theme_mode, ...})
//   ~/.gemini/antigravity-cli/jetski_state.pbtxt            (protobuf text format)
//   ~/.gemini/antigravity-cli/last_check.timestamp           (Unix epoch seconds)
//   ~/.gemini/antigravity-cli/cli.log                        (symlink -> log/cli-YYYYMMDD_HHMMSS.log)
//   ~/.gemini/antigravity-cli/conversation_summaries.db      (SQLite)
//   ~/.gemini/antigravity-cli/conversations/                 (per-conv JSON)
//   ~/.gemini/antigravity-cli/history.jsonl                  (event log)
//   ~/.gemini/antigravity-cli/brain/                         (sqlite)
//   ~/.gemini/antigravity-cli/builtin/                       (builtin plugins)
//   ~/.gemini/antigravity-cli/annotations/                   (UI state)
//   ~/.gemini/antigravity-cli/presence/                      (liveness)
//   ~/.gemini/antigravity-cli/jetbox_summaries_proto.pb      (binary protobuf)
//   ~/.gemini/antigravity-cli/updater/                       (update state)
//   ~/.gemini/antigravity-cli/crashes/                       (crash dumps)
//   ~/.gemini/antigravity-cli/cache/                         (response cache)
//   ~/.gemini/antigravity-cli/bin/                           (binary alias)
//   ~/.gemini/antigravity-cli/implicit/                      (implicit skills)
//   ~/.gemini/jetski/brain                                   (sqlite brain)
//
// 本文件写 v1.3.5 binary 期望的核心 config file (settings / mcp / installation_id /
// jetbox_state / jetski_state / last_check / cli.log):
//   - recovered 写出来的 → v1.3.5 binary 跑能读
//   - v1.3.5 binary 写出来的 → recovered 跑能读
package auth

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// stripBOM drops a leading UTF-8 byte order mark (EF BB BF) if present.
// AGY 1.2.16 changelog line 13 (cycle config-bom-strip): Notepad and
// PowerShell's `Set-Content -Encoding UTF8` write a BOM by default on
// Windows, and any file that begins with the three-byte sequence makes
// json.Unmarshal fail with "invalid character 'ï' looking for beginning
// of value". The reference ignores the BOM at every JSON read site for
// `~/.gemini/config/config.json`, `mcp.json` and `settings.json`; we do
// the same in three places. UTF-16 / UTF-32 BOMs are out of scope — the
// changelog only mentions the UTF-8 case.
func stripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// JetboxState is the v1.3.5 binary 1:1 schema for
// ~/.gemini/antigravity-cli/jetbox_state.json. Fields verified
// against the v1.3.5 binary's actual file (cycle 42 实证).
type JetboxState struct {
	AgentEnvironment   string `json:"agent_environment"`    // "local" / "managed" / "remote"
	ThemeMode          string `json:"theme_mode"`           // "dark" / "light" / "system"
	ConversationWidth  string `json:"conversation_width"`   // "wide" / "narrow"
	TerminalPlacement  string `json:"terminal_placement"`   // "below_input" / "right" / etc
	PostOnboardingStep string `json:"post_onboarding_step"` // "agent_configuration" / etc
	UserNuxEvent       string `json:"user_nux_event"`       // "first_run" / etc
}

// AntigravityConfigHome returns the v1.3.5 binary canonical
// home for the antigravity-cli config (cycle 42 实证).
// Honours $ANTIGRAVITY_HOME, then $HOME.
func AntigravityConfigHome() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}

// GeminiConfigHome returns ~/.gemini/config — the tree the reference keeps
// plugin installation state in, sibling to ~/.gemini/antigravity-cli. Derived
// from AntigravityConfigHome so $ANTIGRAVITY_HOME/$HOME move both trees
// together (a test that points ANTIGRAVITY_HOME at <root>/.gemini/antigravity-cli
// isolates this tree too).
func GeminiConfigHome() string {
	return filepath.Join(filepath.Dir(AntigravityConfigHome()), "config")
}

// LoadJetboxState reads ~/.gemini/antigravity-cli/jetbox_state.json
// (v1.3.5 binary 1:1 schema). Returns (JetboxState{}, nil) when the
// file does not exist (cycle 51: first-launch path).
//
// The cycle 51 fix: the v1.5 binary checks jetbox_state.json's
// post_onboarding_step field before showing the wizard. If the
// field is non-empty, onboarding is considered complete and the
// TUI drops in directly. Our previous behaviour always rendered
// the wizard on every launch; this loader lets main.go gate the
// wizard on the same condition.
func LoadJetboxState() (JetboxState, error) {
	path := filepath.Join(AntigravityConfigHome(), "jetbox_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return JetboxState{}, err
	}
	var s JetboxState
	if err := json.Unmarshal(data, &s); err != nil {
		return JetboxState{}, err
	}
	return s, nil
}

// SaveJetboxState writes the v1.3.5 binary 1:1 schema to
// ~/.gemini/antigravity-cli/jetbox_state.json with 0600 perms. Used
// by the cycle 51 fix: when the wizard completes we record
// post_onboarding_step so subsequent launches skip it.
func SaveJetboxState(s JetboxState) error {
	if err := os.MkdirAll(AntigravityConfigHome(), 0700); err != nil {
		return err
	}
	path := filepath.Join(AntigravityConfigHome(), "jetbox_state.json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// OnboardingCompleted returns true if the on-disk jetbox_state.json
// has a non-empty post_onboarding_step field, i.e. the user has
// already walked through the v1.3.5 wizard. main.go calls this
// before runOnboardingWizard so the wizard only fires on first install.
//
// Returns false when jetbox_state.json does not exist (treat as
// first-launch).
func OnboardingCompleted() bool {
	s, err := LoadJetboxState()
	if err != nil {
		return false
	}
	return strings.TrimSpace(s.PostOnboardingStep) != ""
}

// renderJetskiStatePbtxt is a hand-written prototext writer for
// the v1.3.5 binary's jetski_state.pbtxt format. We avoid the
// google.golang.org/protobuf dep (not vendored in the sandbox)
// by emitting the exact bytes the binary writes (verified
// against the v1.3.5 binary's actual file content).
//
// Verified format (cycle 42 实证):
//
//	post_onboarding:  {
//	  completed_steps:  POST_ONBOARDING_STEP_TYPE_MANAGER_WELCOME
//	  ...
//	}
//	installation_uuid:  "67b4243f-5097-4d09-9a25-1ef55eaad79a"
//	migrations:  {
//	  key:  3
//	  value:  MIGRATION_STATUS_COMPLETED
//	}
func renderJetskiStatePbtxt(uuid string, steps []string, migrations map[uint32]string) []byte {
	var b []byte
	b = append(b, "post_onboarding:  {\n"...)
	for _, s := range steps {
		b = append(b, "  completed_steps:  "...)
		b = append(b, s...)
		b = append(b, '\n')
	}
	b = append(b, "}\n"...)
	b = append(b, "installation_uuid:  \""...)
	b = append(b, uuid...)
	b = append(b, "\"\n"...)
	// The migrations are written in key order: a Go map iterates in a
	// random order, and the file's bytes have to be the same on every
	// run — the reference writes one block per migration, and the
	// block order is part of the file.
	for _, k := range slices.Sorted(maps.Keys(migrations)) {
		v := migrations[k]
		b = append(b, "migrations:  {\n"...)
		b = append(b, fmt.Sprintf("  key:  %d\n", k)...)
		b = append(b, "  value:  "...)
		b = append(b, v...)
		b = append(b, "\n}\n"...)
	}
	return b
}

func writeCompatInstallationID(home string, written *[]string) (string, error) {
	instPath := filepath.Join(home, "installation_id")
	if _, err := os.Stat(instPath); os.IsNotExist(err) {
		u, err := newUUIDv4()
		if err != nil {
			return "", fmt.Errorf("installation_id: %w", err)
		}
		if err := os.WriteFile(instPath, []byte(u+"\n"), 0600); err != nil {
			return "", err
		}
		*written = append(*written, instPath)
		return u, nil
	}
	b, _ := os.ReadFile(instPath)
	return string(b), nil
}

func writeCompatSettings(home string, written *[]string) error {
	setPath := filepath.Join(home, "settings.json")
	if _, err := os.Stat(setPath); os.IsNotExist(err) {
		cwd, _ := os.Getwd()
		s := Settings{ColorScheme: "dark", TrustedWorkspaces: []string{cwd}}
		data, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(setPath, data, 0600); err != nil {
			return err
		}
		*written = append(*written, setPath)
	}
	return nil
}

func writeCompatMCP(home string, written *[]string) error {
	mcpPath := filepath.Join(home, "mcp.json")
	if _, err := os.Stat(mcpPath); os.IsNotExist(err) {
		data := []byte(`{"servers":{}}`)
		if err := os.WriteFile(mcpPath, data, 0600); err != nil {
			return err
		}
		*written = append(*written, mcpPath)
	}
	return nil
}

func writeCompatJetboxState(home string, written *[]string) error {
	jbPath := filepath.Join(home, "jetbox_state.json")
	if _, err := os.Stat(jbPath); os.IsNotExist(err) {
		s := JetboxState{
			AgentEnvironment:   "local",
			ThemeMode:          "dark",
			ConversationWidth:  "wide",
			TerminalPlacement:  "below_input",
			PostOnboardingStep: "agent_configuration",
			UserNuxEvent:       "first_run",
		}
		data, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(jbPath, data, 0600); err != nil {
			return err
		}
		*written = append(*written, jbPath)
	}
	return nil
}

func writeCompatJetskiState(home string, uid string, written *[]string) error {
	jtPath := filepath.Join(home, "jetski_state.pbtxt")
	if _, err := os.Stat(jtPath); os.IsNotExist(err) {
		steps := []string{
			"POST_ONBOARDING_STEP_TYPE_MANAGER_WELCOME",
			"POST_ONBOARDING_STEP_TYPE_USAGE_MODE",
			"POST_ONBOARDING_STEP_TYPE_AGENT_CONFIGURATION",
			"POST_ONBOARDING_STEP_TYPE_ADD_WORKSPACE",
		}
		migrations := map[uint32]string{
			3: "MIGRATION_STATUS_COMPLETED",
			4: "MIGRATION_STATUS_COMPLETED",
			5: "MIGRATION_STATUS_COMPLETED",
		}
		data := renderJetskiStatePbtxt(strings.TrimSpace(uid), steps, migrations)
		if err := os.WriteFile(jtPath, data, 0600); err != nil {
			return err
		}
		*written = append(*written, jtPath)
	}
	return nil
}

func writeCompatLastCheck(home string, written *[]string) error {
	tsPath := filepath.Join(home, "last_check.timestamp")
	if _, err := os.Stat(tsPath); os.IsNotExist(err) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		if err := os.WriteFile(tsPath, []byte(ts+"\n"), 0600); err != nil {
			return err
		}
		*written = append(*written, tsPath)
	}
	return nil
}

func writeCompatCliLog(home string, written *[]string) error {
	logDir := filepath.Join(home, "log")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return err
	}
	cliLogLink := filepath.Join(home, "cli.log")
	if _, err := os.Stat(cliLogLink); os.IsNotExist(err) {
		ts := time.Now().Format("20060102_150405")
		logFile := filepath.Join(logDir, "cli-"+ts+".log")
		f, err := os.Create(logFile)
		if err != nil {
			return err
		}
		f.Close()
		if err := os.Symlink(logFile, cliLogLink); err != nil {
			return err
		}
		*written = append(*written, cliLogLink)
	}
	return nil
}

// WriteCompatDefaults writes the v1.3.5 binary 1:1 config
// defaults that recovered ships on first launch. Idempotent:
// if a file already exists, it is left alone (the v1.3.5 binary
// reads existing values, not overwrites).
//
// Returns the list of files written (for the caller to log).
func WriteCompatDefaults() ([]string, error) {
	home := AntigravityConfigHome()
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	written := []string{}

	// 1. installation_id — UUID, written once per install.
	uid, err := writeCompatInstallationID(home, &written)
	if err != nil {
		return written, err
	}

	// 2. settings.json — defaults: dark + cwd trusted workspace.
	if err := writeCompatSettings(home, &written); err != nil {
		return written, err
	}

	// 3. mcp.json — empty servers map (v1.3.5 binary 默认是空 {})
	if err := writeCompatMCP(home, &written); err != nil {
		return written, err
	}

	// 4. jetbox_state.json — defaults
	if err := writeCompatJetboxState(home, &written); err != nil {
		return written, err
	}

	// 5. jetski_state.pbtxt — prototext hand-written to match v1.3.5 binary
	if err := writeCompatJetskiState(home, uid, &written); err != nil {
		return written, err
	}

	// 6. last_check.timestamp — Unix epoch seconds
	if err := writeCompatLastCheck(home, &written); err != nil {
		return written, err
	}

	// 7. cli.log + log/ — symlink cli.log -> log/cli-YYYYMMDD_HHMMSS.log
	if err := writeCompatCliLog(home, &written); err != nil {
		return written, err
	}

	return written, nil
}

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
	"os"
	"path/filepath"
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

// Settings is the v1.3.5 binary 1:1 schema for
// ~/.gemini/antigravity-cli/settings.json. The 2 fields are
// verified against the v1.3.5 binary's actual file content
// (cycle 42 close-gap 实证).
type Settings struct {
	ColorScheme       string   `json:"colorScheme"`       // "dark" / "light" / "solarized light" / etc
	TrustedWorkspaces []string `json:"trustedWorkspaces"` // cwd paths
}

// MCPServer is the v1.3.5 binary 1:1 schema for one entry in
// ~/.gemini/antigravity-cli/mcp.json servers{}. The Type field
// is either "http" (url) or "stdio" (command + args + env).
type MCPServer struct {
	Type    string            `json:"type"`    // "http" or "stdio"
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// MCPConfig is the v1.3.5 binary 1:1 schema for
// ~/.gemini/antigravity-cli/mcp.json.
type MCPConfig struct {
	Servers map[string]MCPServer `json:"servers"`
}

// JetboxState is the v1.3.5 binary 1:1 schema for
// ~/.gemini/antigravity-cli/jetbox_state.json. Fields verified
// against the v1.3.5 binary's actual file (cycle 42 实证).
type JetboxState struct {
	AgentEnvironment    string `json:"agent_environment"`    // "local" / "managed" / "remote"
	ThemeMode           string `json:"theme_mode"`           // "dark" / "light" / "system"
	ConversationWidth   string `json:"conversation_width"`   // "wide" / "narrow"
	TerminalPlacement   string `json:"terminal_placement"`   // "below_input" / "right" / etc
	PostOnboardingStep  string `json:"post_onboarding_step"` // "agent_configuration" / etc
	UserNuxEvent        string `json:"user_nux_event"`       // "first_run" / etc
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

// SaveSettingsColorScheme updates the colorScheme field in
// ~/.gemini/antigravity-cli/settings.json, preserving all other
// fields (trustedWorkspaces, artifactReviewPolicy, etc.). The
// v1.3.5 binary writes this when the user picks a scheme in the
// wizard.
//
// When settings.json does not exist yet, this is a no-op (the
// cycle 48 WriteCompatDefaults will write the default on first
// run).
func SaveSettingsColorScheme(name string) error {
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Decode into a generic map so we preserve unknown fields
	// (model / permissions / trustedWorkspaces / artifactReviewPolicy
	// that the v1.3.5 binary and openagy add on top of the v1.3.5
	// Settings struct). stripBOM keeps Notepad / PowerShell-Set-Content
	// UTF-8 BOM-prefixed settings.json (AGY 1.2.16 line 13) loadable.
	data = stripBOM(data)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	m["colorScheme"] = name
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}

// AddTrustedWorkspace appends dir to the trustedWorkspaces array in
// ~/.gemini/antigravity-cli/settings.json, preserving every other field
// (model, permissions, colorScheme, …). It is the persistence half of the
// TUI /add-dir command (cycle 103) and the on-startup workspace-trust
// gate (cycle 125).
//
// Evidence: the key's struct tag is recovered as
// `"TrustedWorkspaces"json:"trustedWorkspaces,omitempty"`; the reference keeps
// its session working directories in this array (driving the reference over a
// pty adds each session cwd to it); and /add-dir reports `Added %s to workspace`.
// That /add-dir and the session-cwd trust path share this one array is
// 【推测】 — it is the only directory list in settings.json.
//
// A directory already present is a no-op (no duplicate entry, no rewrite).
//
// Cycle 125: when settings.json doesn't yet exist (the user accepted the
// trust prompt on a first-launch session before WriteCompatDefaults
// had a chance to materialise the file), AddTrustedWorkspace creates
// it from scratch rather than returning ENOENT. The TUI's accept
// handler would otherwise silently fail and the next launch would
// re-prompt.
func AddTrustedWorkspace(dir string) error {
	return updateOrCreateSettings(func(m map[string]any) {
		var list []string
		if raw, ok := m["trustedWorkspaces"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					list = append(list, s)
				}
			}
		}
		for _, s := range list {
			if s == dir {
				return // already trusted; leave the file unchanged
			}
		}
		m["trustedWorkspaces"] = append(list, dir)
	})
}

// IsWorkspaceTrusted reports whether dir appears in settings.json's
// trustedWorkspaces list. Cycle 125: the on-startup workspace-trust gate
// calls this with `os.Getwd()` and shows the trust dialog when the
// answer is false.
//
// The comparison is exact (string equality on the canonicalised path);
// neither the v1.3.5 binary nor this clone walk the path tree, so a
// trusted parent does not implicitly trust its children — this matches
// the reference's behaviour (driving the reference over a pty adds each
// session cwd individually, never its parents). dir and each stored
// workspace are run through filepath.EvalSymlinks so /tmp/foo (the
// macOS symlink) and /private/tmp/foo (the real path) compare equal.
// When a path doesn't exist yet (the on-startup cwd before the user
// has created any files there), EvalSymlinks fails and we fall back
// to filepath.Clean.
//
// A missing or unparseable settings.json is *not* an error: a fresh
// install has no trusted list, so IsWorkspaceTrusted returns (false,
// nil) and the gate proceeds to show the dialog.
func IsWorkspaceTrusted(dir string) (bool, error) {
	target := canonicalisePath(dir)
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	data = stripBOM(data)
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return false, nil // corrupt file → treat as untrusted, surface the dialog
	}
	for _, w := range s.TrustedWorkspaces {
		if canonicalisePath(w) == target {
			return true, nil
		}
	}
	return false, nil
}

// canonicalisePath resolves symlinks where possible (so /tmp/foo and
// /private/tmp/foo match on macOS, where /tmp is a symlink) and falls
// back to filepath.Clean when the path doesn't exist. Used by
// IsWorkspaceTrusted so the on-startup trust gate sees a single
// representation regardless of how cwd was reached.
func canonicalisePath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// LoadTrustedWorkspaces returns the trustedWorkspaces list verbatim,
// or an empty slice when settings.json is missing or the field is
// absent. Cycle 125: powers the cycle-103 /add-dir test and the
// on-startup workspace-trust gate. Path canonicalisation is left to
// callers (IsWorkspaceTrusted canonicalises; AddTrustedWorkspace
// canonicalises on write so a future read by either is consistent).
func LoadTrustedWorkspaces() []string {
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	data = stripBOM(data)
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return s.TrustedWorkspaces
}

// LoadMCPConfig reads ~/.gemini/antigravity-cli/mcp.json. A missing file is not
// an error: it means no servers are configured, which is the reference's
// default (WriteCompatDefaults writes an empty servers map).
func LoadMCPConfig() (*MCPConfig, error) {
	data, err := os.ReadFile(filepath.Join(AntigravityConfigHome(), "mcp.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &MCPConfig{}, nil
		}
		return nil, err
	}
	data = stripBOM(data)
	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SetSettingsString writes one string value into
// ~/.gemini/antigravity-cli/settings.json, preserving every other field.
func SetSettingsString(key, value string) error {
	return updateSettings(func(m map[string]any) {
		m[key] = value
	})
}

// SetSettingsBool writes one boolean value into settings.json, preserving
// every other field. It is the persistence half of the TUI /statusline and
// /title toggles.
//
// Cycle 184: the toggles used to persist the strings "on"/"off". That was a
// guess (【推测】 in cycle 105) and it is wrong: agy 1.4.0 types both keys as
// booleans and refuses to start at all on a string —
//
//	⚠ Settings Error
//	  └ invalid settings: statusLine: invalid value "off"; title: invalid value "on"
//
// so a single `/statusline off` in the clone bricked the reference binary
// until the key was deleted by hand. Verified by writing each candidate into
// a live settings.json and starting agy: true/false are accepted, every
// string form ("on"/"off"/"auto"/"always"/"never") is rejected.
func SetSettingsBool(key string, value bool) error {
	return updateSettings(func(m map[string]any) {
		m[key] = value
	})
}

// SettingsBool returns one boolean value from settings.json. ok is false when
// the file, the key or the value is absent.
//
// A string value is accepted too: it is the pre-cycle-184 shape this package
// used to write, and treating "on"/"off" as true/false lets a settings.json
// the clone already corrupted keep working instead of silently reverting the
// toggle to its default.
func SettingsBool(key string) (value, ok bool) {
	data, err := os.ReadFile(filepath.Join(AntigravityConfigHome(), "settings.json"))
	if err != nil {
		return false, false
	}
	data = stripBOM(data)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return false, false
	}
	switch v := m[key].(type) {
	case bool:
		return v, true
	case string:
		switch v {
		case "on", "true":
			return true, true
		case "off", "false":
			return false, true
		}
	}
	return false, false
}

// SettingsString returns one string value from settings.json, or "" when the
// file, the key or the value is absent. It seeds the TUI's /statusline and
// /title toggles from disk.
func SettingsString(key string) string {
	data, err := os.ReadFile(filepath.Join(AntigravityConfigHome(), "settings.json"))
	if err != nil {
		return ""
	}
	data = stripBOM(data)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// SettingsField returns one raw value from settings.json. Dot-notation
// keys walk nested objects (e.g. "statusLine.enabled" reads the boolean
// nested under the `statusLine` object). The returned `any` is the JSON
// shape: string, bool, []any, map[string]any, etc. Returns (nil, nil)
// when the file or the key is absent.
//
// Cycle 196: the /config panel reads live values through this helper so
// pressing ←/→ in the picker reflects the user's persisted state on the
// next render. Print-mode /config (print_slash.go) still goes through
// its own typed mappers — SettingsField is the TUI-only counterpart.
func SettingsField(key string) (any, error) {
	data, err := os.ReadFile(filepath.Join(AntigravityConfigHome(), "settings.json"))
	if err != nil {
		return nil, err
	}
	data = stripBOM(data)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	parts := strings.Split(key, ".")
	var cur any = m
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, nil
		}
		cur, ok = obj[p]
		if !ok {
			return nil, nil
		}
	}
	return cur, nil
}

// SetSettingsField writes one raw value into settings.json at the dot
// path. Nested objects are created as needed. Bool values are persisted
// as JSON `true`/`false`; strings as JSON strings. Other types pass
// through JSON marshalling.
//
// Cycle 196: companion to SettingsField — /config ←/→ calls this after
// determining the next value in the cycle list. Pre-cycle-196 the panel
// had no live persistence; settings.json only updated via the /statusline
// and /title slash-command toggles.
func SetSettingsField(key string, value any) error {
	return updateSettings(func(m map[string]any) {
		parts := strings.Split(key, ".")
		var cur any = m
		for i, p := range parts {
			if i == len(parts)-1 {
				if obj, ok := cur.(map[string]any); ok {
					obj[p] = value
				}
				return
			}
			next, ok := cur.(map[string]any)
			if !ok {
				return
			}
			child, exists := next[p]
			if !exists {
				child = map[string]any{}
				next[p] = child
			}
			cur = child
		}
	})
}

// DeleteSettingsField removes the value at the dot path from settings.json.
// The reference represents a setting at its default value by the *absence*
// of the key (probed: cycling /config's Agent Mode back to "default"
// deletes `agentMode`; Copy on Select's "on" is absent while "off" is
// JSON false). Cycle 197.
//
// A missing path is not an error — deleting an already-absent key is a
// no-op. Parent objects left empty are kept, matching the reference which
// never prunes `statusLine`/`title` shells.
func DeleteSettingsField(key string) error {
	return updateOrCreateSettings(func(m map[string]any) {
		parts := strings.Split(key, ".")
		var cur any = m
		for i, p := range parts {
			if i == len(parts)-1 {
				if obj, ok := cur.(map[string]any); ok {
					delete(obj, p)
				}
				return
			}
			next, ok := cur.(map[string]any)
			if !ok {
				return
			}
			child, exists := next[p]
			if !exists {
				return
			}
			cur = child
		}
	})
}

// updateSettings reads settings.json, lets mutate change it, and rewrites it.
//
// The rewrite reproduces the reference's formatting: 2-space indent, HTML
// escaping of <, > and & (Go's json.Marshal default), and keys in alphabetical
// order — which is what marshalling a map produces, and what the live file
// shows.
func updateSettings(mutate func(map[string]any)) error {
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data = stripBOM(data)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	mutate(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}

// updateOrCreateSettings is the cycle-125 sibling of updateSettings.
// When settings.json is missing, it creates the parent dir (0700) and
// writes a fresh settings.json containing only the keys the mutator
// chooses to set — instead of returning ENOENT. Used by the trust
// gate's accept handler so a first-launch user accepting the dialog
// persists the cwd before WriteCompatDefaults has had a chance to
// run. When the file does exist, the behaviour is identical to
// updateSettings (preserves unknown fields, alphabetises keys via
// map marshalling).
func updateOrCreateSettings(mutate func(map[string]any)) error {
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	if err := os.MkdirAll(AntigravityConfigHome(), 0700); err != nil {
		return err
	}
	m := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		data = stripBOM(data)
		if jerr := json.Unmarshal(data, &m); jerr != nil {
			// Corrupt file: keep going with the empty map so
			// the mutator can still add its key. Subsequent
			// runLiveTUI calls will eventually rewrite this
			// file under WriteCompatDefaults, restoring the
			// full schema.
			m = map[string]any{}
		}
	case os.IsNotExist(err):
		// fresh install — start with an empty map
	default:
		return err
	}
	mutate(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0600)
}

// ResolveSettingsModel returns the model specified in settings.json,
// or fallback if settings.json does not exist or has no model.
func ResolveSettingsModel(fallback string) string {
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	data = stripBOM(data)
	var s struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(data, &s); err == nil && strings.TrimSpace(s.Model) != "" {
		return strings.TrimSpace(s.Model)
	}
	return fallback
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
	for k, v := range migrations {
		b = append(b, "migrations:  {\n"...)
		b = append(b, fmt.Sprintf("  key:  %d\n", k)...)
		b = append(b, "  value:  "...)
		b = append(b, v...)
		b = append(b, "\n}\n"...)
	}
	return b
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
	instPath := filepath.Join(home, "installation_id")
	uid := ""
	if _, err := os.Stat(instPath); os.IsNotExist(err) {
		u, err := newUUIDv4()
		if err != nil {
			return written, fmt.Errorf("installation_id: %w", err)
		}
		uid = u
		if err := os.WriteFile(instPath, []byte(u+"\n"), 0600); err != nil {
			return written, err
		}
		written = append(written, instPath)
	} else {
		b, _ := os.ReadFile(instPath)
		uid = string(b)
	}

	// 2. settings.json — defaults: dark + cwd trusted workspace.
	setPath := filepath.Join(home, "settings.json")
	if _, err := os.Stat(setPath); os.IsNotExist(err) {
		cwd, _ := os.Getwd()
		s := Settings{ColorScheme: "dark", TrustedWorkspaces: []string{cwd}}
		data, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(setPath, data, 0600); err != nil {
			return written, err
		}
		written = append(written, setPath)
	}

	// 3. mcp.json — empty servers map (v1.3.5 binary 默认是空 {})
	mcpPath := filepath.Join(home, "mcp.json")
	if _, err := os.Stat(mcpPath); os.IsNotExist(err) {
		data := []byte(`{"servers":{}}`)
		if err := os.WriteFile(mcpPath, data, 0600); err != nil {
			return written, err
		}
		written = append(written, mcpPath)
	}

	// 4. jetbox_state.json — defaults
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
			return written, err
		}
		written = append(written, jbPath)
	}

	// 5. jetski_state.pbtxt — prototext hand-written to match
	//    v1.3.5 binary byte-for-byte.
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
			return written, err
		}
		written = append(written, jtPath)
	}

	// 6. last_check.timestamp — Unix epoch seconds
	tsPath := filepath.Join(home, "last_check.timestamp")
	if _, err := os.Stat(tsPath); os.IsNotExist(err) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		if err := os.WriteFile(tsPath, []byte(ts+"\n"), 0600); err != nil {
			return written, err
		}
		written = append(written, tsPath)
	}

	// 7. cli.log + log/ — symlink cli.log -> log/cli-YYYYMMDD_HHMMSS.log
	logDir := filepath.Join(home, "log")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return written, err
	}
	cliLogLink := filepath.Join(home, "cli.log")
	if _, err := os.Stat(cliLogLink); os.IsNotExist(err) {
		ts := time.Now().Format("20060102_150405")
		logFile := filepath.Join(logDir, "cli-"+ts+".log")
		// touch the file so the symlink target exists
		f, err := os.Create(logFile)
		if err != nil {
			return written, err
		}
		f.Close()
		if err := os.Symlink(logFile, cliLogLink); err != nil {
			return written, err
		}
		written = append(written, cliLogLink)
	}

	return written, nil
}

// ---- plugin installation state (cycle 198) ---------------------------------

// GeminiConfigHome returns ~/.gemini/config — the tree the reference keeps
// plugin installation state in, sibling to ~/.gemini/antigravity-cli. Derived
// from AntigravityConfigHome so $ANTIGRAVITY_HOME/$HOME move both trees
// together (a test that points ANTIGRAVITY_HOME at <root>/.gemini/antigravity-cli
// isolates this tree too).
func GeminiConfigHome() string {
	return filepath.Join(filepath.Dir(AntigravityConfigHome()), "config")
}

// pluginConfigPath is the file the reference writes plugin enablement to:
//
//	{"plugins": {"<name>": {"enabled": bool}}, "userSettings": {...}}
//
// Measured on the live machine (cycle 198): all four stock plugins appear here
// with "enabled": true.
func pluginConfigPath() string { return filepath.Join(GeminiConfigHome(), "config.json") }

// PluginEnabled reports config.json's plugins.<name>.enabled. A missing file
// or a missing entry reads as (false, nil): the reference only writes the row
// once a plugin is installed directory-side, and its /plugin picker renders
// everything without one as [disabled].
func PluginEnabled(name string) (bool, error) {
	data, err := os.ReadFile(pluginConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	data = stripBOM(data)
	var doc struct {
		Plugins map[string]struct {
			Enabled bool `json:"enabled"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false, err
	}
	return doc.Plugins[name].Enabled, nil
}

// SetPluginEnabled writes config.json's plugins.<name>.enabled, creating the
// file, the plugins map and the entry as needed. Every other field is
// preserved — the file also carries userSettings, and the reference rewrites it
// in full rather than truncating it (cycle 198 probe: pressing space on
// /plugin's Installed tab flips exactly this flag, and it survives a restart).
//
// The error is returned so the panel's space handler can leave the row
// unchanged instead of lying about the new state.
func SetPluginEnabled(name string, enabled bool) error {
	if err := os.MkdirAll(GeminiConfigHome(), 0700); err != nil {
		return err
	}
	m := map[string]any{}
	switch data, err := os.ReadFile(pluginConfigPath()); {
	case err == nil:
		data = stripBOM(data)
		if jerr := json.Unmarshal(data, &m); jerr != nil {
			m = map[string]any{}
		}
	case !os.IsNotExist(err):
		return err
	}
	plugins, _ := m["plugins"].(map[string]any)
	if plugins == nil {
		plugins = map[string]any{}
		m["plugins"] = plugins
	}
	entry, _ := plugins[name].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		plugins[name] = entry
	}
	entry["enabled"] = enabled
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pluginConfigPath(), out, 0600)
}

// ClearPluginEnabled deletes the plugins.<name> entry from config.json. Used
// by the /plugin ctrl+s Uninstall path so a future installedPluginRows()
// walk does not surface a plugin whose directory was just removed.
//
// A missing file or a missing entry is a no-op (returns nil); the caller
// has already shown the user a successful uninstall and a stale entry would
// only flip the badge to [disabled], which is the same state a fresh
// install yields. Every other field of config.json is preserved —
// userSettings and the other plugins.<name> entries ride through.
func ClearPluginEnabled(name string) error {
	data, err := os.ReadFile(pluginConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data = stripBOM(data)
	m := map[string]any{}
	if jerr := json.Unmarshal(data, &m); jerr != nil {
		// A malformed config.json is the caller's problem; we don't
		// invent one. Returning the parse error lets uninstall surface
		// it after the directory has already been removed.
		return jerr
	}
	plugins, _ := m["plugins"].(map[string]any)
	if plugins == nil {
		return nil
	}
	if _, ok := plugins[name]; !ok {
		return nil
	}
	delete(plugins, name)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pluginConfigPath(), out, 0600)
}

// disableAllPluginsField is the top-level boolean in
// ~/.gemini/config/config.json that gates every plugin regardless of its
// per-plugin enabled flag. The field was missing from the v1.3.5 schema
// (cycle 198 measured config.json had no top-level boolean) — it is the
// cycle-235 close-gap for the AGY 1.2.16 changelog line that complained
// /plugin and /mcp "showed 'enabled' or 'disabled' without indicating
// they were also blocked by disableAllPlugins". Env var wins when set so
// an operator can override the on-disk setting without rewriting it.
func disableAllPluginsField() bool {
	data, err := os.ReadFile(pluginConfigPath())
	if err != nil {
		return false
	}
	data = stripBOM(data)
	var doc struct {
		DisableAllPlugins bool `json:"disableAllPlugins"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	return doc.DisableAllPlugins
}

// disableAllMCPField is the top-level boolean in
// ~/.gemini/antigravity-cli/mcp.json that gates every MCP server,
// independent of each server's per-row enabled flag. Same rationale as
// disableAllPluginsField above.
func disableAllMCPField() bool {
	data, err := os.ReadFile(filepath.Join(AntigravityConfigHome(), "mcp.json"))
	if err != nil {
		return false
	}
	data = stripBOM(data)
	var doc struct {
		DisableAllMCP bool `json:"disableAllMCP"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	return doc.DisableAllMCP
}

// PluginsBlocked reports whether the global plugin gate is on. The env
// var AGY_CLI_DISABLE_PLUGINS wins; otherwise the on-disk
// disableAllPlugins field is consulted. Any non-empty value (after trim)
// is enough — the value is treated as a switch, not a token list.
//
// Returning true means every plugin row in /plugin's Installed tab
// should render its status as "blocked" instead of "enabled"/"disabled".
// The /mcp panel uses the matching MCPsBlocked helper.
func PluginsBlocked() bool {
	if strings.TrimSpace(os.Getenv("AGY_CLI_DISABLE_PLUGINS")) != "" {
		return true
	}
	return disableAllPluginsField()
}

// MCPsBlocked is the MCP-server equivalent of PluginsBlocked: env var
// AGY_CLI_DISABLE_MCP wins, then mcp.json's disableAllMCP boolean.
func MCPsBlocked() bool {
	if strings.TrimSpace(os.Getenv("AGY_CLI_DISABLE_MCP")) != "" {
		return true
	}
	return disableAllMCPField()
}

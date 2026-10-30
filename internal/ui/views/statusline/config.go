// Package statusline manages external status-line scripts, window-title
// scripts, pricing calculations, configuration, and rendering state for the
// Antigravity TUI.
package statusline

import (
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// Config is the effective configuration of a `statusLine` (or `title`)
// block in settings.json.
//
// The block on disk carries `type`, `command`, `padding`, `enabled` and
// `stack_with_default`; see [wireConfig] for the exact shape and the
// member order the reference writes.
//
// Evidence:
//   - 【一手】 agy 1.4.7's own settings.json holds
//     `{"type":"","command":"","enabled":false}` for both blocks.
//   - 【一手】 the reference re-encodes the block from its own struct: a
//     block that read `{"enabled":false,"command":"…","type":"command"}`
//     came back as `{"type":"command","command":"…","enabled":true}`
//     after `/title on`.
//   - 【反编译】 the binary's string table carries
//     `json:"padding,omitempty"` and `json:"stack_with_default,omitempty"`
//     next to `json:"command,omitempty"`.
type Config struct {
	// Type is the block's kind. "command" is the only supported one.
	Type string
	// Command is the shell command that renders the line (status line) or
	// the title (window title).
	Command string
	// Padding is the number of blank lines above a custom status line.
	Padding int
	// Enabled reports whether the command runs. A false value makes the
	// CLI fall back to the built-in status line.
	//
	// A block that does not carry the member is on: the member's default
	// is true.
	//
	// Evidence 【一手】: agy 1.4.7 ran a title script whose block read
	// `{"type":"command","command":"…"}` — no `enabled` member.
	Enabled bool
	// StackWithDefault renders the custom status line *below* the
	// built-in one instead of replacing it.
	StackWithDefault bool
}

// wireConfig is the on-disk shape of a `statusLine` / `title` block.
//
// The member order is the reference's declaration order — type, command,
// padding, enabled, stack_with_default — and `enabled` is a pointer so a
// block can be written back without the member.
//
// Evidence 【一手】: agy 1.4.7 wrote
// `{"type":"command","command":"…","padding":3,"enabled":false,"stack_with_default":true}`
// for a block whose members had been shuffled on disk.
type wireConfig struct {
	Type             string `json:"type"`
	Command          string `json:"command"`
	Padding          int    `json:"padding,omitempty"`
	Enabled          *bool  `json:"enabled,omitempty"`
	StackWithDefault bool   `json:"stack_with_default,omitempty"`
}

// Active reports whether the block runs a command.
func (c Config) Active() bool {
	return c.Enabled && strings.TrimSpace(c.Command) != ""
}

// LoadConfig reads the nested `statusLine` block from settings.json and
// returns it together with whether the block exists.
//
// A boolean `statusLine` is read as an object without a command, which is
// how the reference treats the legacy shape: no script runs.
//
// Evidence: the reference's own status line documentation — "Set
// `statusLine` to an object, not to `true` or `false`. The CLI reads a
// boolean as an object without a `command`, so no script runs."
func LoadConfig() (Config, bool) {
	return loadBlock("statusLine")
}

// LoadTitleConfig reads the nested `title` block from settings.json.
//
// The title block takes the same shape as the status-line block; the
// reference's own terminal title documentation lists `command`, `type` and
// `enabled` for it.
func LoadTitleConfig() (Config, bool) {
	return loadBlock("title")
}

// loadBlock reads one settings block that holds a status-line style
// configuration.
func loadBlock(key string) (Config, bool) {
	raw, err := auth.SettingsField(key)
	if err != nil || raw == nil {
		return Config{}, false
	}
	switch v := raw.(type) {
	case map[string]any:
		cfg := Config{}
		cfg.Type, _ = v["type"].(string)
		cfg.Command, _ = v["command"].(string)
		// `enabled` defaults to true when the member is absent.
		cfg.Enabled = true
		if en, ok := v["enabled"].(bool); ok {
			cfg.Enabled = en
		}
		if p, ok := v["padding"].(float64); ok {
			cfg.Padding = int(p)
		}
		if s, ok := v["stack_with_default"].(bool); ok {
			cfg.StackWithDefault = s
		}
		return cfg, true
	case bool:
		// The legacy boolean form: an object without a command.
		return Config{Enabled: v}, true
	}
	return Config{}, false
}

// PersistVisibility writes the block's `enabled` member into settings.json
// without clobbering the command or the layout members.
//
// Evidence 【一手】: agy 1.4.7's `/statusline off` left
// `{"type":"command","command":"…","padding":3,"enabled":false,"stack_with_default":true}`.
func PersistVisibility(visible bool) error {
	return persistBlock("statusLine", visible)
}

// PersistCommand stores a custom status-line command.
//
// The block is written *without* an `enabled` member: the member's default
// is true, so the script runs right away. The layout members survive.
//
// Evidence 【一手】: agy 1.4.7's `/statusline <command>` wrote
// `{"type":"command","command":"…"}` — no `enabled` — and the reference's
// own status line documentation says the command "immediately updates your
// settings and starts running the script".
func PersistCommand(command string) error {
	cfg, _ := loadBlock("statusLine")
	cfg.Type = "command"
	cfg.Command = command
	return writeBlock("statusLine", cfg, nil)
}

// ClearCommand removes the custom status-line command and reverts to the
// built-in default.
//
// The reference removes the `statusLine` key from settings.json outright —
// it does not leave an empty block behind.
//
// Evidence 【一手】: agy 1.4.7's settings.json after `/statusline delete`
// held no `statusLine` key at all.
func ClearCommand() error {
	return auth.DeleteSettingsField("statusLine")
}

// PersistTitleEnabled writes the window-title block's `enabled` member.
//
// Evidence 【一手】: agy 1.4.7's `/title on` wrote
// `{"type":"command","command":"…","enabled":true}` and `/title off` wrote
// the same block with `enabled:false`.
func PersistTitleEnabled(enabled bool) error {
	return persistBlock("title", enabled)
}

// persistBlock writes a block's `enabled` member, keeping the command and
// the layout members.
func persistBlock(key string, enabled bool) error {
	cfg, ok := loadBlock(key)
	if !ok {
		// A missing block becomes the reference's empty one.
		cfg = Config{}
	}
	return writeBlock(key, cfg, &enabled)
}

// writeBlock writes the whole block back, in the reference's member order.
//
// A nil `enabled` leaves the member out; the reference's default for the
// member is true.
func writeBlock(key string, cfg Config, enabled *bool) error {
	wire := wireConfig{
		Type:             cfg.Type,
		Command:          cfg.Command,
		Padding:          cfg.Padding,
		Enabled:          enabled,
		StackWithDefault: cfg.StackWithDefault,
	}
	return auth.SetSettingsField(key, wire)
}

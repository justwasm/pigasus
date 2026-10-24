// Package store owns the writer the permission system uses to persist
// "Always allow" rules. It is the file-level seam AGY 1.3.4 changelog
// line 40 calls out:
//
//	"Fixed 'Always allow' rules approved from a Remote Control browser
//	 session still applying after you removed them in the browser's
//	 rules editor or the shared tab of /permissions; these rules are
//	 now saved only to the shared config.json."
//
// Before this cycle a Remote Control approval could land in the
// project-level config (`~/.gemini/config/projects/default-cli-project.json`)
// and survive a deletion made in either the browser's rules editor or
// the Shared tab of /permissions, because both deletion surfaces read
// only the shared config. The writer below tags every rule with the
// Source that approved it and routes SourceRemoteControl entries
// through the shared config only, so a deletion in either surface
// takes effect on the next rule lookup.
//
// The package is stdlib-only and depends only on internal/auth for the
// $ANTIGRAVITY_HOME-respecting home directory; this matches the
// stdlib-only constraint the research artifacts carry.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// Source tags an "Always allow" rule with who approved it. The
// writer uses the tag to decide where the rule lands.
type Source string

const (
	// SourceLocal is the default for the TUI's own prompts. Existing
	// behaviour preserved: a rule with this source goes wherever the
	// active scope points (shared, project, or global).
	SourceLocal Source = "local"
	// SourceRemoteControl tags rules approved from a Remote Control
	// browser session. Such rules are written to the shared
	// config.json only; they never land in the project-level config.
	// The browser's rules editor and the Shared tab of /permissions
	// both read the shared config, which is what closes the AGY
	// 1.3.4 line 40 gap.
	SourceRemoteControl Source = "remote_control"
)

// Rule is one entry in an allow list. The shape mirrors the two
// settings.json shapes internal/permissions/settings.go already
// accepts: "tool:pattern" (string) or {"tool": "...", "pattern":
// "..."} (object).
type Rule struct {
	Tool    string `json:"tool,omitempty"`
	Pattern string `json:"pattern"`
}

// Entry is a Rule plus the Source that put it there. Source "" means
// the entry predates source tracking — those are kept for back-compat
// so an existing shared.json from a prior build still loads and is
// editable through RemoveAllow (which removes "" entries regardless
// of the source argument).
type Entry struct {
	Rule   Rule   `json:"rule"`
	Source Source `json:"source,omitempty"`
}

// SharedConfig is the on-disk shape of the shared config.json. It
// carries the three lists the Shared tab of /permissions manages
// (allow, deny, ask) and a small schema field so a future build can
// tell an old-format file from a new-format one.
type SharedConfig struct {
	// Schema is the schema version. Bumped when the on-disk shape
	// changes incompatibly. Missing or zero means "v0": the file is
	// either a legacy session-local blob or a fresh install.
	Schema int `json:"schema,omitempty"`
	// Allow is the always-allow list. SourceRemoteControl entries
	// land here and ONLY here.
	Allow []Entry `json:"allow"`
	// Deny is the always-deny list. Source field carries the same
	// meaning as on Allow.
	Deny []Entry `json:"deny"`
	// Ask is the always-ask list. Source field carries the same
	// meaning as on Allow.
	Ask []Entry `json:"ask"`
}

// DefaultPath returns the canonical shared config path. Honours
// $ANTIGRAVITY_HOME (set by the test rig) before falling back to
// $HOME/.gemini/antigravity-cli/shared.json. Theantigravity-cli
// sibling is the same tree auth.AntigravityConfigHome() owns, so a
// test that points ANTIGRAVITY_HOME at <root>/.gemini/antigravity-cli
// isolates the shared config too.
func DefaultPath() string {
	return filepath.Join(auth.AntigravityConfigHome(), "shared.json")
}

// Load reads the shared config from path. A missing file is not an
// error: it returns an empty SharedConfig so a fresh install starts
// with no allow/deny/ask rules.
//
// An unparseable file is also returned as empty rather than an error:
// the AGY 1.3.4 line 40 fix cannot block a startup on a malformed
// file. Callers that need to distinguish "file missing" from "file
// corrupt" can os.Stat before calling.
func Load(path string) (SharedConfig, error) {
	if path == "" {
		return SharedConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SharedConfig{}, nil
		}
		return SharedConfig{}, err
	}
	var cfg SharedConfig
	if uerr := json.Unmarshal(data, &cfg); uerr != nil {
		return SharedConfig{}, nil
	}
	return cfg, nil
}

// Write persists the shared config to path. The parent directory is
// created with 0700 (matching auth.WriteCompatDefaults) and the file
// is written with 0600.
//
// The on-disk shape is a stable JSON object so future cycles can add
// fields without breaking this version's readers: fields absent from
// the struct decode to zero values, and unknown fields in the file
// are dropped on rewrite (matching the rest of the auth package's
// settings.json writers).
func Write(path string, cfg SharedConfig) error {
	if path == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	cfg.Schema = currentSchemaVersion
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// currentSchemaVersion is bumped when the on-disk shape changes
// incompatibly. v1 introduces the per-entry Source tag and the
// top-level schema field; older files (without the schema field)
// decode as Schema = 0 and are migrated on first write.
var currentSchemaVersion = 1

// AlwaysAllow appends rule to the shared allow list with the given
// source tag. The rule is de-duplicated by (Rule, Source) — the same
// Tool:Pattern from the same source is added at most once. A second
// approval from the same source returns nil with no file rewrite, so
// calling this in a tight loop is safe.
//
// SourceRemoteControl is the contract that closes the AGY 1.3.4 line
// 40 gap: rules tagged RemoteControl live in the shared config only.
// The function never touches the project-level config, so a delete
// in either the browser's rules editor or the Shared tab of
// /permissions (both of which read this file) takes effect on the
// next rule lookup.
func AlwaysAllow(path string, source Source, rule Rule) error {
	if source == "" {
		source = SourceLocal
	}
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	for _, existing := range cfg.Allow {
		if existing.Rule == rule && (existing.Source == source ||
			(existing.Source == "" && source == SourceLocal)) {
			return nil
		}
	}
	cfg.Allow = append(cfg.Allow, Entry{Rule: rule, Source: source})
	return Write(path, cfg)
}

// RemoveAllow removes rule from the shared allow list, filtered by
// source tag. An untagged entry (Source == "") is removed regardless
// of the source argument — this keeps deletion idempotent across the
// pre-source-tracking upgrade: a legacy entry with the same Rule is
// considered "the same rule" regardless of which source the caller
// passes.
//
// A no-op (rule absent) is not an error: deletion is idempotent.
func RemoveAllow(path string, source Source, rule Rule) error {
	if source == "" {
		source = SourceLocal
	}
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	out := cfg.Allow[:0]
	for _, existing := range cfg.Allow {
		if existing.Rule == rule && (existing.Source == source || existing.Source == "") {
			continue
		}
		out = append(out, existing)
	}
	cfg.Allow = out
	return Write(path, cfg)
}

// AllowedBy returns the subset of cfg.Allow that match every source
// filter. The Shared tab of /permissions uses it to render the list
// when a filter (Local-only or RemoteControl-only) is active.
func (cfg SharedConfig) AllowedBy(source Source) []Entry {
	if source == "" {
		return cfg.Allow
	}
	out := make([]Entry, 0, len(cfg.Allow))
	for _, e := range cfg.Allow {
		if e.Source == source {
			out = append(out, e)
		}
	}
	return out
}

// String renders a rule as "tool:pattern" — the same shape the
// settings.json readers and the project-config readers accept.
func (r Rule) String() string {
	if r.Tool == "" {
		return r.Pattern
	}
	return r.Tool + ":" + r.Pattern
}

// ParseRule splits "tool:pattern" on the first colon. A leading "!"
// is left in place; the existing settings parser handles it.
func ParseRule(s string) Rule {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "!")
	if i := strings.IndexByte(s, ':'); i > 0 {
		return Rule{Tool: s[:i], Pattern: s[i+1:]}
	}
	return Rule{Pattern: s}
}

// AlwaysAllowFromSource writes one "Always allow" rule with the given source tag to DefaultPath().
func AlwaysAllowFromSource(source Source, rule string) error {
	parsed, err := parseSharedRule(rule)
	if err != nil {
		return err
	}
	return AlwaysAllow(DefaultPath(), source, parsed)
}

// RemoveAllowFromSource deletes one rule from DefaultPath() filtered by source tag.
func RemoveAllowFromSource(source Source, rule string) error {
	parsed, err := parseSharedRule(rule)
	if err != nil {
		return err
	}
	return RemoveAllow(DefaultPath(), source, parsed)
}

func parseSharedRule(s string) (Rule, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		return Rule{}, os.ErrInvalid
	}
	if s == "" {
		return Rule{}, os.ErrInvalid
	}
	return ParseRule(s), nil
}

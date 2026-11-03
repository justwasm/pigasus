// Package confhome resolves the directories the reference keeps its own
// configuration in.
//
// The reference binary (Antigravity CLI) keeps two trees under the user's
// home, and both are read by the clone on almost every launch:
//
//	~/.gemini/antigravity-cli   the CLI's own state: settings.json, mcp.json,
//	                            jetbox_state.json, the brain, the logs
//	~/.gemini/config            the sibling tree the plugin installation
//	                            state lives in
//
// This package owns *where* those trees are and nothing else. The files
// inside them are owned by the package that reads them: the settings
// store by `internal/core/settings`, the MCP list by
// `internal/features/auth`, the plugin list by
// `internal/features/plugins/core`.
//
// It is a tier-1 package: the policies (`internal/core/policy/...`) read
// the settings store, and the store reads this.
package confhome

import (
	"os"
	"path/filepath"
)

// Antigravity returns the canonical home for the antigravity-cli config.
//
// Honours $ANTIGRAVITY_HOME, then $HOME.
//
// 【一手】 the v1.3.5 binary's own config tree (cycle 42 实证) — the
// settings.json, mcp.json and jetbox_state.json the reference writes all
// live under it.
func Antigravity() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}

// Gemini returns ~/.gemini/config — the tree the reference keeps plugin
// installation state in, sibling to ~/.gemini/antigravity-cli.
//
// Derived from Antigravity() so $ANTIGRAVITY_HOME/$HOME move both trees
// together (a test that points ANTIGRAVITY_HOME at
// <root>/.gemini/antigravity-cli isolates this tree too).
func Gemini() string {
	return filepath.Join(filepath.Dir(Antigravity()), "config")
}

// CLILogPath is the path of the CLI's own log file.
//
// The reference writes one log file per launch into the `log` directory
// of its own home, and names it `cli-<timestamp>.log`. The timestamp's
// shape is `YYYYMMDD_HHMMSS` — `internal/core/logts` renders it.
//
// The path is a fact about the CLI's own files, so it lives here, next to
// the home it hangs under: the /help overlay prints the file's name and
// the status line's error card points at it, and neither surface may
// carry a second copy of the shape.
func CLILogPath(ts string) string {
	return filepath.Join(Antigravity(), "log", "cli-"+ts+".log")
}

// StripBOM drops a leading UTF-8 byte order mark (EF BB BF) if present.
//
// 【一手】 AGY 1.2.16 changelog line 13 (cycle config-bom-strip): Notepad
// and PowerShell's `Set-Content -Encoding UTF8` write a BOM by default on
// Windows, and any file that begins with the three-byte sequence makes
// json.Unmarshal fail with "invalid character 'ï' looking for beginning of
// value". The reference ignores the BOM at every JSON read site for
// `~/.gemini/config/config.json`, `mcp.json` and `settings.json`; we do
// the same at every read site. UTF-16 / UTF-32 BOMs are out of scope — the
// changelog only mentions the UTF-8 case.
//
// It lives here because every JSON file the reference writes lives under
// one of the two homes above.
func StripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

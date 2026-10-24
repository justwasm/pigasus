package printmode

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"gopkg.in/yaml.v3"
)

// jsonSkillInfo is one `/skills` row, in the reference's field order:
//
//	{"name":"agents-sdk","description":"…","path":"/Users/…/SKILL.md","builtin":false,"model_invocable":true}
//	{"name":"chrome-devtools-plugin:…","…","plugin":"chrome-devtools-plugin","builtin":false,"model_invocable":true}
//
// `plugin` is present only for entries discovered under a plugin's skills/
// directory, and sits between `path` and `builtin`.
//
// `model_invocable` is true for all 57 skills on the capture machine; the
// frontmatter marker that would clear it is unrecovered, so the clone always
// reports true.
type SkillInfo struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Path           string `json:"path"`
	Plugin         string `json:"plugin,omitempty"`
	Builtin        bool   `json:"builtin"`
	ModelInvocable bool   `json:"model_invocable"`
}

type jsonSkillInfo = SkillInfo

func collectSkillsFromDir(dir, prefix string, listed map[string]bool, builtin bool) []jsonSkillInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []jsonSkillInfo
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if !isDirFollowingLinks(path) {
			continue
		}
		if listed != nil && !listed[e.Name()] {
			continue
		}
		skillMD := filepath.Join(path, "SKILL.md")
		name, desc := skillFrontmatter(skillMD)
		if name == "" {
			name = e.Name()
		}
		if builtin && e.Name() == "automation" {
			// The bundled automation skill declares `name: automation`
			// plus `metadata.icon: schedule`; agy 1.4.3 lists it as
			// "schedule automation" in /skills. Other builtin names
			// come straight from frontmatter.
			name = "schedule automation"
		}
		if prefix != "" {
			name = prefix + ":" + name
		}
		found = append(found, jsonSkillInfo{
			Name:           name,
			Description:    desc,
			Path:           skillMD,
			Plugin:         prefix,
			Builtin:        builtin,
			ModelInvocable: true,
		})
	}
	return found
}

func collectPluginSkills(geminiDir string) []jsonSkillInfo {
	var found []jsonSkillInfo
	pluginsDir := filepath.Join(geminiDir, "config", "plugins")
	if plugins, err := os.ReadDir(pluginsDir); err == nil {
		for _, p := range plugins {
			if isDirFollowingLinks(filepath.Join(pluginsDir, p.Name())) {
				found = append(found, collectSkillsFromDir(filepath.Join(pluginsDir, p.Name(), "skills"), p.Name(), nil, false)...)
			}
		}
	}
	return found
}

// printSlashSkillInfos enumerates the installed skills.
//
// The reference merges skills from the builtin bundle, the user's
// ~/.gemini/skills, and every plugin's skills/ directory (whose entries are
// prefixed "<plugin>:<name>"). The display name comes from the SKILL.md
// frontmatter, not the directory — e.g. builtin/skills/antigravity_guide
// declares `name: antigravity-guide`. Entries may be symlinks (this machine's
// ~/.gemini/skills/terminal-browser is), so the directory test follows the
// link.
//
// After the merge, the result is folded by content hash (slash_skills_dedup.go)
// so a SKILL.md discovered through both a plugin's skills/ directory and a
// project-level `.claude/skills/` folder appears once, with its declared
// (frontmatter `name:`) name. The reference's AGY 1.2.7 changelog records
// this as the gap fix; see the file comment of slash_skills_dedup.go for
// the rule and its evidence grade.
// PrintSlashSkillInfos enumerates skills discovered from workspace and user home.
func PrintSlashSkillInfos() []SkillInfo {
	return printSlashSkillInfosInternal()
}

func printSlashSkillInfos() []jsonSkillInfo {
	return PrintSlashSkillInfos()
}

func printSlashSkillInfosInternal() []jsonSkillInfo {
	// Cycle plugin-fsnotify: clear the watcher staleness flag so the
	// next /skills render starts from a known-fresh state. The flag
	// is the same one installedPluginRows / pluginMarketplaces flip;
	// print mode leaves pluginWatcherPtr nil and this is a single
	// pointer read.
	maybeConsumeStaleness()
	var found []jsonSkillInfo
	home := auth.AntigravityHome()
	gemini := filepath.Dir(home) // ~/.gemini

	// The builtin bundle ships nine skills on disk but `/skills` lists only
	// two of them (agy-customizations, antigravity-guide); the other seven
	// (automation, generative_ui, migrate-workflows, permissioned-github,
	// plugin, run-workflow, ui-plugin-navigation) are internal and have no
	// frontmatter marker that distinguishes them. The set below is therefore
	// empirical: it reproduces the reference's output on this machine, but
	// the reference's selection rule is unknown.
	found = append(found, collectSkillsFromDir(filepath.Join(home, "builtin", "skills"), "", printSlashBuiltinListed, true)...)
	found = append(found, collectSkillsFromDir(filepath.Join(gemini, "skills"), "", nil, false)...)

	// Note: ~/.claude/skills is deliberately NOT a root. The 1.3.6 reference
	// lists `agents-sdk` (which lives in ~/.gemini/skills) but none of the
	// Claude-only skills (algo/alpha/assets/…), so it does not read the
	// Claude directory. The dedup pass below is what would collapse any
	// duplicates if a second root was added in a later release.
	found = append(found, collectPluginSkills(gemini)...)

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return dedupeSkillInfosByHash(found)
}

// printSlashSkills lists installed skills as "<name>\t<description>", which is
// the shape `agy -p /skills` emits (57 entries on the capture machine).
func printSlashSkills() string {
	var b strings.Builder
	for _, s := range printSlashSkillInfos() {
		fmt.Fprintf(&b, "%s\t%s\n", s.Name, s.Description)
	}
	return b.String()
}

// PrintSlashSkills lists installed skills as "<name>\t<description>".
func PrintSlashSkills() string {
	return printSlashSkills()
}

// printSlashSkillsData renders the `/skills` command payload.
func printSlashSkillsData() any {
	return map[string]any{"skills": printSlashSkillInfos()}
}

// isDirFollowingLinks reports whether path is a directory, following symlinks
// (DirEntry.IsDir is false for a symlink to a directory).
func isDirFollowingLinks(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// printSlashBuiltinListed is the subset of the builtin skill bundle the
// reference's `/skills` panel lists. It is deliberately NOT the prompt
// allowlist (agyprompt.BuiltinSkillDirs): agy 1.4.3 lists five builtin
// skills, while its system prompt advertises only agy-customizations and
// antigravity-guide. The remaining four directories (generative_ui,
// permissioned-github, plugin, ui-plugin-navigation) are internal and stay
// out of both surfaces. Verified live at 120x40 on agy 1.4.3.
var printSlashBuiltinListed = map[string]bool{
	"agy-customizations": true,
	"antigravity_guide":  true,
	"automation":         true,
	"migrate-workflows":  true,
	"run-workflow":       true,
}

func extractFrontmatterBytes(b []byte) ([]byte, bool) {
	lines := bytes.Split(b, []byte{'\n'})
	if len(lines) == 0 || !bytes.Equal(bytes.TrimSpace(lines[0]), []byte("---")) {
		return nil, false
	}
	for i := 1; i < len(lines); i++ {
		if bytes.Equal(bytes.TrimSpace(lines[i]), []byte("---")) {
			return bytes.Join(lines[1:i], []byte{'\n'}), true
		}
	}
	return nil, false
}

// skillFrontmatter returns the `name:` and `description:` values from a
// SKILL.md YAML frontmatter block. Frontmatter must open with `---` on the
// first line and close with `---` on a later line; only the bytes between
// those fences are fed to the YAML parser, so trailing markdown body lines
// after the close fence never see a YAML decoder.
//
// Cycle 175 replaces the previous from-scratch block-scalar walker with
// gopkg.in/yaml.v3, the same library the reference compiles into its binary
// (verified via `strings`: the `yaml:"…"` tag interface and `encoding/yaml`-ish
// error strings appear in `~/.local/bin/agy`). What this gains over the
// hand-rolled walker:
//   - comments (`#`), anchors (`&`) and aliases (`*`)
//   - flow-style mappings (`{a: b}`) and sequences (`[…]`)
//   - escape sequences inside double-quoted scalars (`\n`, `\t`, Unicode)
//   - every block-scalar flavour (`>`, `>-`, `>+`, `|`, `|-`, `|+`) parsed
//     the same way the rest of the YAML world reads them
//
// `description` is right-trimmed of `\n` to match the prior contract — the
// callers append it to a `\t` separator in `/skills` output, where a trailing
// newline would emit a stray blank line.
func skillFrontmatter(path string) (name, description string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	fm, ok := extractFrontmatterBytes(b)
	if !ok {
		return "", ""
	}

	var doc struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}

	// Cycle `yaml-colon-description`: every plain-scalar value in the
	// frontmatter that is unquoted AND contains a ": " sequence gets
	// wrapped in double quotes before the YAML decoder sees it. YAML 1.2
	// reserves ": " (colon-space) for the key/value separator, so a plain
	// scalar like "Agent: helper" raises "mapping values are not allowed
	// in this context". Auto-quoting preserves the bytes verbatim while
	// making the scanner accept them. Block scalars (">", "|", and their
	// "-"/"+" chomping modifiers) and pre-quoted scalars are passed
	// through untouched; continuation lines are folded into the parent
	// value before that decision is made. A description that the
	// SKILL.md author wrote as a mapping or sequence (rare; the
	// reference never does this) still fails to unmarshal, and we
	// surface that as empty, matching the prior "best effort" contract.
	if err := yaml.Unmarshal(normalizeFrontmatter(fm), &doc); err != nil {
		return "", ""
	}
	return strings.TrimSpace(doc.Name), strings.TrimRight(doc.Description, "\n")
}

// needsDoubleQuoting reports whether v is a plain-scalar YAML value whose
// "key: v" line would be rejected by yaml.v3 because v contains a ": "
// (colon-space) sequence. Block scalars and pre-quoted scalars are never
// double-quoted; empty or whitespace-only values are not.
//
// The same predicate is also exported so tests can pin the boundary the
// preprocessor observes.
func needsDoubleQuoting(v []byte) bool {
	if len(v) == 0 {
		return false
	}
	switch v[0] {
	case '"', '\'', '|', '>', '[', '{', '&', '*', '!', '%', '@', '`':
		return false
	}
	// '-' is a sequence-indicator at column 0; here we already trimmed to
	// the value side, but a YAML sequence value (`- foo`) still starts with
	// '-' and is not a plain scalar.
	if v[0] == '-' {
		return false
	}
	return bytes.Contains(v, []byte(": "))
}

// escapeDoubleQuoted returns b with the byte sequences that YAML double-
// quoted strings reserve escaped: `\` → `\\`, `"` → `\"`, newline → `\n`,
// carriage return → `\r`, tab → `\t`, backspace → `\b`, form-feed → `\f`,
// and the eight other ASCII control bytes (`\x00`–`\x17` except those
// covered) → `\xNN`. The intent is round-trip — the resulting bytes are a
// valid YAML double-quoted scalar and yaml.v3 unescapes them back to the
// original b.
func escapeDoubleQuoted(b []byte) []byte {
	out := make([]byte, 0, len(b)+8)
	for _, c := range b {
		switch c {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case 0:
			out = append(out, '\\', '0')
		default:
			if c < 0x20 {
				out = append(out, '\\', 'x', "0123456789abcdef"[c>>4], "0123456789abcdef"[c&0x0f])
			} else {
				out = append(out, c)
			}
		}
	}
	return out
}

// foldPlainScalar merges a `key: value` line and its indented plain-scalar
// continuation lines into a single logical value, the way YAML 1.2 folds a
// multi-line plain scalar: non-empty continuation lines collapse to a
// single separating space, blank continuation lines become a single `\n`.
// Block-scalar headers (lines whose value starts with `|` or `>`) are not
// continuations and `foldPlainScalar` never sees them — the caller skips
// block scalars before invoking it.
func foldPlainScalar(first []byte, rest [][]byte) []byte {
	out := append([]byte(nil), first...)
	for _, line := range rest {
		if len(bytes.TrimSpace(line)) == 0 {
			out = append(out, '\n')
			continue
		}
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, ' ')
		}
		out = append(out, bytes.TrimSpace(line)...)
	}
	return out
}

func collectContinuationLines(lines [][]byte, start int) ([][]byte, int) {
	var continuation [][]byte
	j := start
	for j < len(lines) {
		next := lines[j]
		if len(next) == 0 || (next[0] != ' ' && next[0] != '\t') {
			break
		}
		continuation = append(continuation, next)
		j++
	}
	return continuation, j
}

func quotePlainScalar(key, folded []byte) []byte {
	var emitted []byte
	emitted = append(emitted, key...)
	emitted = append(emitted, ':', ' ')
	emitted = append(emitted, '"')
	emitted = append(emitted, escapeDoubleQuoted(folded)...)
	emitted = append(emitted, '"')
	return emitted
}

func normalizeKeyValue(lines [][]byte, i int, key, rest []byte) ([][]byte, int) {
	continuation, nextIdx := collectContinuationLines(lines, i+1)
	folded := foldPlainScalar(rest, continuation)
	if !needsDoubleQuoting(folded) {
		result := make([][]byte, 0, 1+len(continuation))
		result = append(result, lines[i])
		result = append(result, continuation...)
		return result, nextIdx - 1
	}
	return [][]byte{quotePlainScalar(key, folded)}, nextIdx - 1
}

// normalizeFrontmatter walks fm one line at a time and auto-quotes any
// plain-scalar value that contains a ": " sequence. Block scalars (>|, |,
// etc.) and pre-quoted scalars pass through untouched.
//
// Lines that consist only of whitespace, of `# …` comments, or that start a
// new top-level key are emitted as-is. Indented continuation lines of a
// plain-scalar value are folded into the parent value via foldPlainScalar
// before the `: ` check is applied, so multi-line plain scalars like
//
//	description: First line
//	  continues here: with colon
//
// round-trip into a single double-quoted scalar instead of failing.
//
// This is the precondition step for yaml.Unmarshal in skillFrontmatter; it
// keeps cycle 175's move to yaml.v3 working for SKILL.md files where the
// description is an unquoted plain scalar that happens to mention
// "Agent: helper" or "Step 1: foo. Step 2: bar.".
func normalizeFrontmatter(fm []byte) []byte {
	lines := bytes.Split(fm, []byte{'\n'})
	var out [][]byte
	inBlock := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if inBlock {
			out = append(out, line)
			// A non-blank line that is no longer indented terminates the
			// block scalar's body. YAML allows many chomping styles; the
			// close-with-an-unindented-line heuristic is sufficient for
			// the shapes skill authors actually use.
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && len(bytes.TrimRight(line, " \t")) > 0 {
				inBlock = false
			}
			continue
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' || line[0] == ' ' || line[0] == '\t' {
			out = append(out, line)
			continue
		}

		idx := bytes.IndexByte(line, ':')
		if idx < 0 {
			out = append(out, line)
			continue
		}

		key := line[:idx]
		rest := bytes.TrimSpace(line[idx+1:])

		// Block-scalar header (`key: |` or `key: >`, optionally with a
		// chomping indicator like `-` or `+` and/or an explicit
		// indentation indicator).
		if len(rest) > 0 && (rest[0] == '|' || rest[0] == '>') {
			out = append(out, line)
			inBlock = true
			continue
		}

		// Empty / comment-only value.
		if len(rest) == 0 || rest[0] == '#' {
			out = append(out, line)
			continue
		}

		var emitted [][]byte
		emitted, i = normalizeKeyValue(lines, i, key, rest)
		out = append(out, emitted...)
	}

	return bytes.Join(out, []byte{'\n'})
}

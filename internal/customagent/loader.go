// Package customagent parses custom agent Markdown files (the *.md
// files under <workspace>/.agents/agents/, ~/.gemini/config/agents/, or
// a plugin's agents/ directory) and resolves the relative paths that
// appear in their YAML frontmatter.
//
// AGY 1.3.1 changelog gap closure:
//
//	Fixed markdown custom agents ignoring `skills:`, `plugins:`,
//	`rules:`, `agents:`, and `hooks:` entries written as paths
//	relative to the agent's own file when the agent ran as a
//	subagent or as the main agent; such entries now resolve
//	against the agent's directory, so an agent that lists
//	`agents: [child.md]` can invoke `child`.
//
// Prior to this package, a session loader that saw `agents:
// [child.md]` in a custom agent at `.agents/agents/parent.md` looked
// for `child.md` relative to the process's CWD (or some other
// platform-rooted path), which silently failed for any agent
// installed under a per-project `.agents/agents/` tree.
//
// What this package does
//
//   - Splits a `.md` file into its YAML frontmatter span and its
//     markdown body (both are returned, not just parsed).
//   - Unmarshals the frontmatter into a typed struct: `name`,
//     `description`, plus the five path-bearing fields
//     `skills:`, `plugins:`, `rules:`, `agents:`, and `hooks:`.
//     A missing field stays the empty slice (not nil) so downstream
//     `len()` checks stay predictable.
//   - Computes a resolved copy of each path-bearing field. The
//     resolver's rule is the simple one:
//   - an absolute path is used verbatim;
//   - a string that contains a path separator or ends in a
//     known extension is treated as a path and joined to the
//     agent file's directory;
//   - everything else is left alone — it's a name reference
//     (e.g. `code-reviewer`) handled by the upstream loader,
//     not by this package.
//     `Filepath.IsAbs` is the only platform check we rely on; no
//     CWD or working-directory resolution happens here, so a
//     caller can hand this package any kind of base directory
//     and the result is deterministic.
//
// What it does NOT do
//
//   - Discover agents on disk. `LoadAgent` operates on one path the
//     caller chose (so it is testable in isolation); directory
//     walking is the caller's job — see `LoadAgentDir` for the
//     obvious one-level walker used in tests.
//   - Validate that the resolved path actually exists. The AGY
//     1.3.1 fix is about *resolution*, not about enforcing
//     presence — a relative entry that points nowhere still fails
//     the same way the absolute one would, and the upstream
//     loader already reports "file not found" with the right
//     message.
//   - Touch the existing `internal/agent` package, whose API is
//     the TUI loop's job (cycle 183 wiring and PiG translation).
//     This package is a leaf dependency, used at agent-load
//     time only.
package customagent

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// resolveAlwaysPath is the set of file extensions a frontmatter
// entry is treated as a path on. Anything ending in one of these
// (case-insensitive) gets joined to the agent file's directory;
// anything else is left as a name reference. We accept every
// extension the AGY 1.3.0+ changelog mentions for these fields:
// Markdown, shell scripts, and JSON manifests.
//
// `.md` and `.sh` cover the changelog's wording "agents:,
// rules:, plugins: written as paths". `.json` covers the
// `plugins.json` / `rules.json` / `agents.json` shape the
// 1.3.3 changelog added (so a relative pointer to a sibling
// `plugins.json` is resolved correctly too).
var resolveAlwaysPathExtensions = []string{".md", ".sh", ".json"}

// resolveContainSeparator is the second predicate: a string that
// contains a forward slash (or, on Windows, a backslash) is
// treated as a path. We don't try to be clever here — the agent
// file's own directory plus whatever the entry looks like is
// what gets handed to the upstream loader.
//
// This is kept as a function literal so adding "looks like a
// path" rules later is one-line.
func looksLikePath(entry string) bool {
	if entry == "" {
		return false
	}
	// Unconditional separators: any kind of slash means "this is
	// at least one directory below the agent file".
	if strings.ContainsAny(entry, "/\\") {
		return true
	}
	lower := strings.ToLower(entry)
	for _, ext := range resolveAlwaysPathExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// Agent is one parsed custom agent. The two field groups
// (Name/Description vs. Skills/Plugins/Rules/Agents/Hooks)
// reflect what the AGY 1.3.x schema actually carries: a name
// + description (required) plus the optional path-bearing
// dependency lists.
//
// Path contains the absolute path to the .md file this agent
// was parsed from. Dir is `filepath.Dir(Path)` and is the base
// every "resolved" field below is anchored to.
type Agent struct {
	// Path is the absolute path to the .md file.
	Path string `yaml:"-"`
	// Dir is the directory the Path file lives in. Every
	// "resolved" field below is joined against Dir, never
	// against the process's CWD.
	Dir string `yaml:"-"`
	// Body is the markdown content after the closing `---`
	// fence. Empty when the file has no body. The body is not
	// validated as markdown here — callers can do that if they
	// care (the cycle 218 mdrender package is one option).
	Body string `yaml:"-"`
	// Name is the frontmatter `name:` field. Optional — the
	// upstream loader falls back to the file stem. We
	// intentionally do not enforce uniqueness.
	Name string `yaml:"name"`
	// Description is the frontmatter `description:` field,
	// right-trimmed of trailing newlines (matching the
	// convention `skillFrontmatter` in cmd/wagyu/print_slash.go
	// established for SKILL.md).
	Description string `yaml:"description"`

	// RawX are the original frontmatter entries, verbatim. They
	// are preserved so callers can choose to honour a name
	// reference instead of the resolved path (e.g. when the
	// upstream loader has its own catalogue that knows about
	// the name).
	RawSkills  []string `yaml:"skills,omitempty"`
	RawPlugins []string `yaml:"plugins,omitempty"`
	RawRules   []string `yaml:"rules,omitempty"`
	RawAgents  []string `yaml:"agents,omitempty"`
	RawHooks   []string `yaml:"hooks,omitempty"`

	// ResolvedX are the same entries with every "looks like a
	// path" element joined onto Dir. An entry that was absolute
	// already is left untouched. An entry that does not look
	// like a path is left as-is — it is treated as a name
	// reference by the upstream loader.
	ResolvedSkills  []string `yaml:"-"`
	ResolvedPlugins []string `yaml:"-"`
	ResolvedRules   []string `yaml:"-"`
	ResolvedAgents  []string `yaml:"-"`
	ResolvedHooks   []string `yaml:"-"`
}

// ParseError describes a failure to parse a single agent file.
// It implements error and carries the file path so a caller
// listing a directory can surface which file broke without
// losing the underlying YAML/IO error.
//
// `errors.As(err, &pe)` is the canonical check, since the
// underlying YAML error (or fs path error) is the part the
// diagnostic display actually wants to surface.
type ParseError struct {
	Path string
	Err  error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("customagent: parse %s: %v", e.Path, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// ErrEmpty is returned by LoadAgent when the file has no
// frontmatter at all (the frontmatter fence is `---` on line 1
// and on a later line; an entirely body-only file yields this).
// Callers can treat it as a soft "skip".
var ErrEmpty = errors.New("customagent: file has no frontmatter")

// LoadAgent parses a single custom-agent .md file and returns
// the resolved Agent. The file must exist and be readable; an
// unreadable file or a malformed frontmatter is reported as
// *parseError.
//
// An absolute `path` is used verbatim; a relative one is
// resolved against the caller's CWD via `filepath.Abs` so
// `Agent.Dir` is always absolute. The custom-agent loader
// stores files under `<workspace>/.agents/agents/`, so most
// callers pass an already-absolute path the directory walker
// produced.
func LoadAgent(path string) (*Agent, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("customagent: empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("customagent: abs %q: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, &ParseError{Path: abs, Err: err}
	}
	a, err := parseFrontmatter(abs, data)
	if err != nil {
		return nil, &ParseError{Path: abs, Err: err}
	}
	return a, nil
}

// LoadAgentDir is a one-level walker over a directory of .md
// agent files. Hidden files (`.*`) and non-`.md` files are
// skipped; sub-directories are not recursed into (cycle 234
// made plugins' agents/ shallow by default — the same
// convention holds for custom agents). The result is sorted by
// file name for deterministic test output.
//
// A directory that does not exist or is empty yields (nil,
// nil) so the caller can distinguish "absent" from "broken".
// An individual file that fails to parse is collected into
// `bad` — the load keeps going so a single malformed agent
// does not block the rest.
func LoadAgentDir(dir string) (agents []*Agent, bad []error, err error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil, errors.New("customagent: empty dir")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filepath.Ext(name) != ".md" {
			continue
		}
		full := filepath.Join(dir, name)
		a, err := LoadAgent(full)
		if err != nil {
			// ErrEmpty means "no frontmatter" — silently
			// skipped. Anything else is a real failure and
			// gets surfaced via `bad`.
			if errors.Is(err, ErrEmpty) {
				continue
			}
			bad = append(bad, err)
			continue
		}
		agents = append(agents, a)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Path < agents[j].Path
	})
	return agents, bad, nil
}

// parseFrontmatter carves out the bytes between the opening
// and closing `---` fences, runs them through yaml.v3, and
// builds the Agent.
//
// Frontmatter rules: must open with `---` on the first line
// (after trimming leading whitespace) and close with another
// `---` line later in the file. An unterminated frontmatter
// is reported as ErrEmpty — the body that follows is probably
// a Markdown file the user dropped in, not a custom agent.
//
// We deliberately do NOT use yaml.v3's "multiple documents"
// mode; a single frontmatter block is enough.
func parseFrontmatter(path string, raw []byte) (*Agent, error) {
	lines := bytes.Split(raw, []byte{'\n'})
	if len(lines) == 0 || !bytes.Equal(bytes.TrimSpace(lines[0]), []byte("---")) {
		return nil, ErrEmpty
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if bytes.Equal(bytes.TrimSpace(lines[i]), []byte("---")) {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return nil, ErrEmpty
	}
	fmBytes := bytes.Join(lines[1:closeIdx], []byte{'\n'})
	body := strings.Join(toStringLines(lines[closeIdx+1:]), "\n")
	body = strings.TrimLeft(body, "\n")

	a := &Agent{
		Path: path,
		Dir:  filepath.Dir(path),
		Body: strings.TrimRight(body, "\n"),
	}

	// The unmarshal target type is an alias of Agent without
	// the unexported bookkeeping. We use a local shape so the
	// yaml tags on the primary struct stay the source of truth
	// while still letting us pull only the document fields.
	type wireDoc struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Skills      []string `yaml:"skills"`
		Plugins     []string `yaml:"plugins"`
		Rules       []string `yaml:"rules"`
		Agents      []string `yaml:"agents"`
		Hooks       []string `yaml:"hooks"`
	}
	var doc wireDoc
	if err := yaml.Unmarshal(fmBytes, &doc); err != nil {
		return nil, err
	}

	a.Name = strings.TrimSpace(doc.Name)
	a.Description = strings.TrimRight(doc.Description, "\n")
	a.RawSkills = normaliseEntries(doc.Skills)
	a.RawPlugins = normaliseEntries(doc.Plugins)
	a.RawRules = normaliseEntries(doc.Rules)
	a.RawAgents = normaliseEntries(doc.Agents)
	a.RawHooks = normaliseEntries(doc.Hooks)

	a.ResolvedSkills = resolveEntries(a.Dir, a.RawSkills)
	a.ResolvedPlugins = resolveEntries(a.Dir, a.RawPlugins)
	a.ResolvedRules = resolveEntries(a.Dir, a.RawRules)
	a.ResolvedAgents = resolveEntries(a.Dir, a.RawAgents)
	a.ResolvedHooks = resolveEntries(a.Dir, a.RawHooks)

	return a, nil
}

// normaliseEntries trims whitespace and drops empty strings.
// A nil slice stays nil (so the len() check matches raw ==
// resolved) but a slice of [""] collapses to nil — a YAML
// author who listed `skills:` with a single blank line should
// not produce a path-bearing list.
func normaliseEntries(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// resolveEntries is the heart of the AGY 1.3.1 fix. It runs
// every entry through `looksLikePath`; matches get joined
// against `baseDir` (the agent file's directory, never CWD),
// absolute entries are kept as-is, and name references fall
// through.
//
// Inputs:
//   - baseDir is the agent file's directory. Always absolute;
//     LoadAgent normalises `path` before calling parseFrontmatter.
//   - entries are the raw frontmatter strings.
//
// Output: the same slice, transformed in place — entries that
// did not look like a path are at the same index they came in,
// and entries that did look like a path are at the same index
// they came in but already resolved. We keep the input order so
// a `len-skewed` diff against the raw slice is impossible.
func resolveEntries(baseDir string, entries []string) []string {
	if entries == nil {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = resolveOne(baseDir, e)
	}
	return out
}

// resolveOne is the per-entry resolver. Kept separate so its
// behaviour can be tested directly without a YAML round-trip.
func resolveOne(baseDir, entry string) string {
	if entry == "" {
		return entry
	}
	// Absolute path or already-has-separator -> use as-is.
	if filepath.IsAbs(entry) {
		return filepath.Clean(entry)
	}
	if !looksLikePath(entry) {
		// Name reference — the upstream loader has its
		// own catalogue (system agents, plugin agents,
		// etc.). We must not join it onto baseDir.
		return entry
	}
	joined := filepath.Join(baseDir, entry)
	return filepath.Clean(joined)
}

// toStringLines is a small helper used by parseFrontmatter to
// turn the post-fence [][]byte slice back into strings for the
// Body field. We don't care about byte/string pooling here —
// the frontmatter parsing dominates the cost.
func toStringLines(b [][]byte) []string {
	out := make([]string, len(b))
	for i, line := range b {
		out[i] = string(line)
	}
	return out
}

// ResolvedAgents returns a flat, sorted, deduplicated list of
// every path-bearing resolution across the resolved slices of
// the given agents. Callers that want to verify "child.md is
// invocable from parent.md" do:
//
//	if _, ok := customagent.ResolvedAgents(agents)[child]; ok {
//	    /* invoke */
//	}
//
// Empty input yields an empty map; no callers panic on a nil
// return (we return a fresh, non-nil map for that).
func ResolvedAgents(agents []*Agent) map[string]*Agent {
	out := make(map[string]*Agent)
	for _, a := range agents {
		if a == nil || a.Name == "" {
			continue
		}
		if _, ok := out[a.Name]; ok {
			// First definition wins. The AGY 1.3.6
			// plugin-agents merge uses the opposite
			// rule, but those are independent agents
			// (different sources). Within a single
			// directory the first encountered file
			// should stay authoritative; otherwise
			// the user can't tell which "name:"
			// they will get when two files happen
			// to share one.
			continue
		}
		out[a.Name] = a
	}
	return out
}

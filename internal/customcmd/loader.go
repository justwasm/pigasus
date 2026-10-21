// Package customcmd loads custom slash commands declared on disk and
// merges them with the built-in catalog under a deterministic
// precedence order.
//
// Cycle custom-slash-commands-no-plugin-injection. AGY 1.2.16 fixed
// "custom slash commands declared in plugins and project .claude/commands/
// folders being silently overridden by other definitions with the same
// name, so the wrong command ran." The clone's surface mirrors that
// behaviour:
//
//	Built-in > Plugin > Project > Global
//
// Built-in commands are the system surface (DefaultSlashCommands) and
// cannot be silently overridden; plugin commands win over project and
// global commands so a plugin author can rely on their /foo staying
// theirs even when a project also ships /foo; project commands win
// over the user's global commands/ dropbox; global commands are the
// fallback.
//
// The loader enumerates three on-disk roots:
//
//	~/.gemini/config/plugins/<plugin>/commands/*.md   (SourcePlugin)
//	<cwd>/.claude/commands/*.md                       (SourceProject)
//	~/.gemini/antigravity-cli/commands/*.md           (SourceGlobal)
//
// Plugin, project and global commands all live in the same bare-name
// namespace — the loader keys every command by its basename (with an
// optional frontmatter `name:` override), not by the directory it
// came from. A plugin's `<plugin>/commands/foo.md` and a project's
// `<cwd>/.claude/commands/foo.md` therefore both surface as "/foo";
// the AGY 1.2.16 contract is that the plugin's wins. Two plugins
// shipping the same basename resolve alphabetically by source path
// so the precedence stays deterministic.
//
// Merge applies the precedence above, returns a deterministic catalog
// (sorted by Name in byte-ascending order) and records every
// same-name conflict in a Conflict slice so the call site can log
// them and tests can pin the precedence invariants.
//
// No global state is consulted by the loader — every Load call takes
// the cwd + ANTIGRAVITY_HOME-like config root as arguments, so the
// package is straightforward to test from t.TempDir + t.Setenv.
package customcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source identifies which on-disk root produced a Command. Higher
// Precedence() wins in a same-name merge.
type Source int

const (
	// SourcePlugin is a plugin-shipped custom command under
	// <plugin>/commands/<name>.md. Surfaced in the picker as
	// "/<plugin>:<name>".
	SourcePlugin Source = iota + 1
	// SourceProject is a project-local custom command under
	// <cwd>/.claude/commands/<name>.md.
	SourceProject
	// SourceGlobal is a user-global custom command under
	// <AntigravityConfigHome>/commands/<name>.md.
	SourceGlobal
	// SourceBuiltin is the hardcoded DefaultSlashCommands catalog.
	// SourceBuiltin only appears in merge inputs; the on-disk
	// loader never produces it.
	SourceBuiltin
)

// String renders the source for logs and tests.
func (s Source) String() string {
	switch s {
	case SourceBuiltin:
		return "builtin"
	case SourcePlugin:
		return "plugin"
	case SourceProject:
		return "project"
	case SourceGlobal:
		return "global"
	}
	return "unknown"
}

// Precedence returns the merge rank for s. Higher wins. Built-in >
// Plugin > Project > Global matches the AGY 1.2.16 contract — see
// the package comment.
func (s Source) Precedence() int {
	switch s {
	case SourceBuiltin:
		return 4
	case SourcePlugin:
		return 3
	case SourceProject:
		return 2
	case SourceGlobal:
		return 1
	}
	return 0
}

// Command is one slash command after the merge step. The Name field
// is the bare form ("foo") for non-plugin commands and the prefixed
// form ("<plugin>:foo") for plugin commands so two plugins can ship
// the same short name without colliding.
//
// Path is empty for SourceBuiltin entries.
type Command struct {
	Name        string
	Description string
	Source      Source
	Path        string
}

// Conflict is a same-name duplicate where the higher-precedence
// source won. Recorded so the call site can log it at startup and so
// tests can pin the precedence contract.
type Conflict struct {
	Name   string
	Winner Command
	Loser  Command
}

// LoadRoots are the three on-disk roots the loader walks. Each is a
// directory that should contain either <basename>.md files (project,
// global) or <plugin>/<basename>.md subtrees (plugin). A missing
// directory is silently skipped — first-run machines have none of
// these roots.
//
// All paths may be empty; the loader simply skips empty roots so a
// caller that has no plugin or project context (e.g. print mode in a
// directory that is not a project) still works.
type LoadRoots struct {
	// PluginsDir is the directory whose <plugin>/commands/*.md
	// subtree holds plugin commands. Defaults to
	// <GeminiConfigHome>/plugins when zero.
	PluginsDir string
	// ProjectDir is the directory whose .claude/commands/*.md
	// subtree holds project commands. Pass "" to skip the
	// project root entirely.
	ProjectDir string
	// GlobalDir is the directory whose *.md files are global
	// commands. Defaults to <AntigravityConfigHome>/commands
	// when zero.
	GlobalDir string
}

// Load walks the three roots and returns the merged catalog plus
// every conflict encountered. The catalog is sorted by Name in
// byte-ascending order so the loader's output is deterministic
// across runs — Go map iteration is randomised and the AGY 1.2.16
// contract requires that "/foo" lookup succeed the same way on every
// invocation regardless of filesystem walk order.
func Load(roots LoadRoots) (catalog []Command, conflicts []Conflict) {
	// Collect raw candidates from every root, then merge with the
	// precedence rule. The raw list is sorted by (Source, Path) so
	// two plugins shipping the same "<plugin>:foo" file resolve
	// deterministically (the alphabetically first path wins).
	var raw []Command
	raw = append(raw, scanDir(roots.GlobalDir, SourceGlobal, "")...)
	raw = append(raw, scanProjectDir(roots.ProjectDir)...)
	raw = append(raw, scanPluginDir(roots.PluginsDir)...)
	sort.Slice(raw, func(i, j int) bool {
		if raw[i].Source != raw[j].Source {
			return raw[i].Source < raw[j].Source
		}
		return raw[i].Path < raw[j].Path
	})

	// First-write wins within a single source (the loader has no
	// notion of "later" within a source — alphabetical path sort
	// already de-conflicted same-source duplicates). Cross-source
	// duplicates resolve by Source.Precedence(): higher precedence
	// replaces a previously-seen entry, the replaced entry is
	// recorded as a Conflict so the caller can log it.
	byName := make(map[string]Command, len(raw))
	for _, c := range raw {
		if existing, ok := byName[c.Name]; ok {
			// Same source, same name: skip the duplicate so we
			// don't record noise. (The sorted-by-path scan above
			// means the first one wins; the second is dropped.)
			if existing.Source == c.Source {
				continue
			}
			// Different sources: keep the higher-precedence one,
			// record the loser. Equal-precedence cross-source
			// duplicates are resolved by Source value — Plugin
			// wins over Project over Global because of the
			// numeric ordering, and two Project entries can only
			// collide if two cwd roots both define "/foo" (a
			// misconfiguration; the first wins, the loser is
			// recorded).
			if c.Source.Precedence() > existing.Source.Precedence() {
				conflicts = append(conflicts, Conflict{Name: c.Name, Winner: c, Loser: existing})
				byName[c.Name] = c
			} else {
				conflicts = append(conflicts, Conflict{Name: c.Name, Winner: existing, Loser: c})
			}
			continue
		}
		byName[c.Name] = c
	}

	catalog = make([]Command, 0, len(byName))
	for _, c := range byName {
		catalog = append(catalog, c)
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].Name < catalog[j].Name })
	return catalog, conflicts
}

// MergeWithBuiltins is the cycle custom-slash-commands-no-plugin-injection
// helper for the picker path. It applies the same precedence rules
// as Load but takes a built-in catalogue (e.g. tuilib.DefaultSlashCommands)
// as a fourth source. Built-in entries have SourceBuiltin and no
// Path; their Name is the slash form ("/foo") without the plugin
// prefix.
//
// Built-in commands always win over plugin/project/global commands
// with the same name, matching the AGY 1.2.16 contract.
//
// The returned catalog is sorted by Name (the leading "/" is stripped
// for comparison so "/add-dir" sorts before "/agents").
func MergeWithBuiltins(builtIns []BuiltIn, custom []Command) (catalog []Command, conflicts []Conflict) {
	raw := make([]Command, 0, len(builtIns)+len(custom))
	for _, b := range builtIns {
		raw = append(raw, Command{
			Name:        strings.TrimPrefix(b.Name, "/"),
			Description: b.Description,
			Source:      SourceBuiltin,
		})
	}
	raw = append(raw, custom...)
	// Same merge logic as Load: stable sort by (Source, Path), then
	// iterate and keep the highest-precedence entry per name.
	sort.Slice(raw, func(i, j int) bool {
		if raw[i].Source != raw[j].Source {
			return raw[i].Source < raw[j].Source
		}
		return raw[i].Path < raw[j].Path
	})
	byName := make(map[string]Command, len(raw))
	for _, c := range raw {
		if existing, ok := byName[c.Name]; ok {
			if existing.Source == c.Source {
				continue
			}
			if c.Source.Precedence() > existing.Source.Precedence() {
				conflicts = append(conflicts, Conflict{Name: c.Name, Winner: c, Loser: existing})
				byName[c.Name] = c
			} else {
				conflicts = append(conflicts, Conflict{Name: c.Name, Winner: existing, Loser: c})
			}
			continue
		}
		byName[c.Name] = c
	}
	catalog = make([]Command, 0, len(byName))
	for _, c := range byName {
		catalog = append(catalog, c)
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].Name < catalog[j].Name })
	return catalog, conflicts
}

// BuiltIn is the cycle 224 wire shape for built-in catalog rows.
// tuilib.SlashCommand satisfies this interface (Name, Description);
// the merge helper accepts it without importing internal/tuilib so
// the cycle stays self-contained.
type BuiltIn struct {
	Name        string
	Description string
}

// scanDir walks dir and parses every *.md file as a custom command
// with the given source. prefix is prepended to the basename to form
// the Name; pass "" for non-namespaced sources.
func scanDir(dir string, source Source, prefix string) []Command {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Command
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		basename := strings.TrimSuffix(name, ".md")
		full := filepath.Join(dir, name)
		cmd, ok := parseCommandFile(full, basename)
		if !ok {
			continue
		}
		cmd.Source = source
		cmd.Path = full
		if prefix != "" {
			cmd.Name = prefix + ":" + basename
		}
		out = append(out, cmd)
	}
	return out
}

// scanProjectDir walks <dir>/.claude/commands/ if dir is non-empty.
// A missing .claude/commands subtree is silently skipped — first-run
// machines never have one.
func scanProjectDir(dir string) []Command {
	if dir == "" {
		return nil
	}
	return scanDir(filepath.Join(dir, ".claude", "commands"), SourceProject, "")
}

// scanPluginDir walks <dir>/<plugin>/commands/*.md. A missing dir
// or a missing commands/ subdirectory in a plugin is silently
// skipped. Plugin commands use the bare basename (not the
// "<plugin>:<basename>" form) so two plugins that both ship /foo
// resolve deterministically by source path — the AGY 1.2.16
// contract is "plugin wins", and the precedence ranks all
// SourcePlugin entries at the same level. To explicitly namespace a
// plugin command, set the frontmatter `name:` field to e.g.
// `alpha:foo`; the loader treats that as a literal name and does
// not rewrite it.
func scanPluginDir(dir string) []Command {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Command
	for _, p := range entries {
		if !p.IsDir() {
			continue
		}
		// Skip the plugin's own dir name for the command name —
		// the command lives under <plugin>/commands/<basename>.md
		// and surfaces as /<basename> unless the file's frontmatter
		// sets `name:` to override.
		out = append(out, scanDir(filepath.Join(dir, p.Name(), "commands"), SourcePlugin, "")...)
	}
	return out
}

// parseCommandFile reads path and extracts description + name from
// the frontmatter. Frontmatter follows the same shape as SKILL.md
// files (cycle 175's skillFrontmatter contract): a leading "---"
// line, a YAML block, a closing "---" line; the body below is the
// markdown payload. The body is intentionally not parsed here — the
// picker only needs the description to render, and the executor
// reads the file again at command time.
//
// The `name:` field is optional. When present it overrides the
// basename-derived default name; the loader treats it as a literal
// name so a plugin author can declare `name: alpha:foo` to
// explicitly namespace a command.
func parseCommandFile(path, defaultName string) (Command, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Command{}, false
	}
	desc, name := frontmatterFields(data)
	if name == "" {
		name = defaultName
	}
	return Command{Name: name, Description: desc}, true
}

// frontmatterFields extracts the `description:` and `name:` scalars
// from the YAML frontmatter block, mirroring print_slash.go's
// skillFrontmatter contract. A missing or malformed frontmatter
// yields ("", "") so the caller can still list the command with an
// empty description and the basename-derived default name.
//
// The body is intentionally left in the returned string — we
// deliberately do not YAML-parse the body, only the bytes between
// the fences — so a body that happens to contain a "name:" or
// "description:" line (common in prompt templates) cannot affect
// the loader's view.
func frontmatterFields(data []byte) (description, name string) {
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) == 0 || !bytes.Equal(bytes.TrimSpace(lines[0]), []byte("---")) {
		return "", ""
	}
	close := -1
	for i := 1; i < len(lines); i++ {
		if bytes.Equal(bytes.TrimSpace(lines[i]), []byte("---")) {
			close = i
			break
		}
	}
	if close < 0 {
		return "", ""
	}
	fm := bytes.Join(lines[1:close], []byte{'\n'})
	var doc struct {
		Description string `yaml:"description"`
		Name        string `yaml:"name"`
	}
	if err := yaml.Unmarshal(fm, &doc); err != nil {
		return "", ""
	}
	return strings.TrimRight(doc.Description, "\n"), strings.TrimSpace(doc.Name)
}

// frontmatterDescription extracts just the description scalar; kept
// for callers that don't need the name field. New code should call
// frontmatterFields instead.
func frontmatterDescription(data []byte) string {
	desc, _ := frontmatterFields(data)
	return desc
}

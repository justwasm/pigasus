// Package pluginimport implements `agy plugin import` — the two importers
// the reference ships: the Gemini CLI extension importer and the Claude Code
// plugin importer.
//
// 【一手】 agy 1.4.11 (sha256 09cefed6a1d217e14ba1751dce4442b97c2797e88c297c77fda3690f92ceddcd)
// was probed with a synthetic $HOME; every string, every field and every
// on-disk artefact reproduced below was read out of that transcript. The
// report is docs/cycles/cycle-364-agy-1.4.11-alignment/deliverable.md.
//
// The shape of one run, as measured:
//
//	agy plugin import            → gemini first, then claude
//	agy plugin import gemini     → the Gemini CLI importer only
//	agy plugin import claude     → the Claude Code importer only
//	agy plugin import <other>    → "Error: unknown import source or invalid path: <other>"
//	agy plugin import claude --force
//
// The report of a source that found nothing is "No <source> extensions
// found."; a source that found something ends with a blank line and
// "Staged to <config home>". The per-plugin lines are
//
//	[ok]    <name>
//	        ✔ <component>  : <n> installed (<names>)
//	[skip]  <name> (already imported, use --force to re-import)
//	[error] <name>: <error>
//
// The [error] line is the only line the reference writes to stderr; every
// other line goes to stdout.
package pluginimport

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// Source names one importer. The reference's two importers are the Gemini
// CLI extension importer and the Claude Code plugin importer.
type Source string

const (
	// SourceGemini reads ~/.gemini/extensions/<name>/gemini-extension.json.
	SourceGemini Source = "gemini"
	// SourceClaude reads ~/.claude/plugins/installed_plugins.json and the
	// top-level plugin directories under ~/.claude/plugins/.
	SourceClaude Source = "claude"
)

// The source strings the reference writes into import_manifest.json.
const (
	manifestSourceGemini = "gemini-cli"
	manifestSourceClaude = "claude-code"
)

// Roots are the directories the importers read and write.
type Roots struct {
	GeminiHome    string // ~/.gemini
	ConfigHome    string // ~/.gemini/config
	ExtensionsDir string // ~/.gemini/extensions
	ClaudeHome    string // ~/.claude
	ClaudePlugins string // ~/.claude/plugins
}

// DefaultRoots resolves the roots the reference uses: $HOME for ~/.gemini
// and ~/.claude, and the clone's own $ANTIGRAVITY_HOME-aware config home for
// ~/.gemini/config.
func DefaultRoots() Roots {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	geminiHome := filepath.Join(home, ".gemini")
	return Roots{
		GeminiHome:    geminiHome,
		ConfigHome:    auth.GeminiConfigHome(),
		ExtensionsDir: filepath.Join(geminiHome, "extensions"),
		ClaudeHome:    filepath.Join(home, ".claude"),
		ClaudePlugins: filepath.Join(home, ".claude", "plugins"),
	}
}

// Options configures one import run.
type Options struct {
	// Roots are the directories to read and write.
	Roots Roots
	// Force re-imports a plugin the manifest already lists.
	Force bool
	// Out receives the report; ErrOut receives the [error] lines. The
	// reference writes every line but the [error] one to stdout.
	Out    io.Writer
	ErrOut io.Writer
}

// Run imports from every source in order and writes the reference's report.
//
// The bare `plugin import` runs the Gemini CLI importer first and the
// Claude Code importer second; each source prints its own "Staged to" line.
func Run(opts Options, sources []Source) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.ErrOut == nil {
		opts.ErrOut = opts.Out
	}
	manifest := readManifest(opts.Roots.ConfigHome)
	for _, src := range sources {
		plugins, err := discover(src, opts.Roots)
		if err != nil {
			return fmt.Errorf("error importing from %s: %w", src, err)
		}
		if len(plugins) == 0 {
			fmt.Fprintf(opts.Out, "No %s extensions found.\n", src)
			continue
		}
		for _, p := range plugins {
			res := importPlugin(opts, src, p, manifest)
			writeReport(opts, res)
			if res.status == statusOK {
				manifest.set(src, res)
			}
		}
		fmt.Fprintf(opts.Out, "\nStaged to %s\n", opts.Roots.ConfigHome)
	}
	return manifest.write(opts.Roots.ConfigHome)
}

// status is the per-plugin outcome.
type status int

const (
	statusOK status = iota
	statusSkip
	statusError
)

// component is one staged component of a plugin (skills, agents, commands,
// mcpServers) and the names it covers.
type component struct {
	label string
	names []string
}

// result is one plugin's report.
type result struct {
	name       string
	status     status
	err        error
	components []component
	warnings   []string
}

// writeReport prints one plugin's lines. The [error] line goes to stderr;
// every other line goes to stdout.
func writeReport(opts Options, res result) {
	switch res.status {
	case statusSkip:
		fmt.Fprintf(opts.Out, "  [skip]  %s (already imported, use --force to re-import)\n", res.name)
	case statusError:
		fmt.Fprintf(opts.ErrOut, "  [error] %s: %v\n", res.name, res.err)
	default:
		fmt.Fprintf(opts.Out, "  [ok]    %s\n", res.name)
	}
	for _, c := range res.components {
		line := fmt.Sprintf("          ✔ %-12s: %d installed", c.label, len(c.names))
		if c.label == componentCommands {
			line += " (converted to skills)"
		}
		line += " (" + strings.Join(c.names, ", ") + ")"
		fmt.Fprintln(opts.Out, line)
	}
	for _, w := range res.warnings {
		fmt.Fprintf(opts.Out, "          ⚠ %s\n", w)
	}
}

// The component labels the reference prints. The report prints them in the
// order below; the import manifest records them in manifestOrder.
const (
	componentSkills     = "skills"
	componentAgents     = "agents"
	componentCommands   = "commands"
	componentMCPServers = "mcpServers"
)

// reportOrder is the order the component lines appear in.
var reportOrder = []string{componentSkills, componentAgents, componentCommands, componentMCPServers}

// manifestOrder is the order the components are recorded in
// import_manifest.json — the reference's staging order, which differs from
// the report's.
var manifestOrder = []string{componentSkills, componentAgents, componentMCPServers, componentCommands}

// importPlugin stages one plugin and reports what happened.
func importPlugin(opts Options, src Source, p candidate, manifest *importManifest) result {
	res := result{name: p.name}
	if !opts.Force && manifest.has(src, p.name) {
		res.status = statusSkip
		return res
	}
	comps, warns, err := stagePlugin(opts, src, p)
	res.components = comps
	res.warnings = warns
	if err != nil {
		res.status = statusError
		res.err = err
		return res
	}
	res.status = statusOK
	return res
}

// stagePlugin copies the plugin into ~/.gemini/config/plugins/<name>/, writes
// the CLI's own plugin.json and converts the components the importer owns.
func stagePlugin(opts Options, src Source, p candidate) ([]component, []string, error) {
	dest := filepath.Join(opts.Roots.ConfigHome, "plugins", p.name)
	if err := os.RemoveAll(dest); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dest, 0700); err != nil {
		return nil, nil, err
	}
	if err := copyTree(p.dir, dest); err != nil {
		return nil, nil, err
	}
	// The CLI's own manifest: name, version, description and the author's
	// name. Everything else the source manifest carries is dropped.
	if err := writeCLIManifest(dest, p.manifest); err != nil {
		return nil, nil, err
	}
	comps := map[string][]string{}
	skills := skillDirs(filepath.Join(dest, "skills"))
	if len(skills) > 0 {
		comps[componentSkills] = skills
	}
	agents := agentFiles(filepath.Join(dest, "agents"))
	if len(agents) > 0 {
		comps[componentAgents] = agents
	}
	if src == SourceClaude {
		// A Claude Code command is a slash command; the CLI loads it as a
		// skill, so the importer converts it.
		cmds, err := convertCommands(dest, p.name)
		if err != nil {
			return nil, nil, err
		}
		if len(cmds) > 0 {
			comps[componentCommands] = cmds
		}
	}
	servers, warns, err := convertMCPServers(p.dir, dest)
	if err != nil {
		return nil, warns, err
	}
	if len(servers) > 0 {
		comps[componentMCPServers] = servers
	}
	out := make([]component, 0, len(comps))
	for _, label := range reportOrder {
		if names, ok := comps[label]; ok {
			out = append(out, component{label: label, names: names})
		}
	}
	return out, warns, nil
}

// writeCLIManifest writes the plugin.json the CLI reads: the subset of the
// source manifest the reference carries over.
func writeCLIManifest(dest string, m sourceManifest) error {
	out := struct {
		Author      string `json:"author,omitempty"`
		Description string `json:"description,omitempty"`
		Name        string `json:"name"`
		Version     string `json:"version,omitempty"`
	}{
		Author:      m.Author,
		Description: m.Description,
		Name:        m.Name,
		Version:     m.Version,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	// The reference's plugin.json ends with a newline.
	return os.WriteFile(filepath.Join(dest, "plugin.json"), append(data, '\n'), 0600)
}

// copyTree copies src into dst recursively.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// skillDirs lists the skill directory names under dir (the directories that
// carry a SKILL.md).
func skillDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// agentFiles lists the agent file names under dir.
func agentFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

// convertCommands turns <plugin>/commands/<name>.md into the skill
// <plugin>/skills/<plugin>-cmd-<name>/SKILL.md and returns the command
// names.
func convertCommands(dest, plugin string) ([]string, error) {
	dir := filepath.Join(dest, "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		skillDir := filepath.Join(dest, "skills", plugin+"-cmd-"+name)
		if err := os.MkdirAll(skillDir, 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), body, 0600); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// manifest is the on-disk import_manifest.json the reference keeps in
// ~/.gemini/config.
type importManifest struct {
	entries []manifestEntry
}

// manifestEntry is one imported plugin.
type manifestEntry struct {
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	ImportedAt string   `json:"importedAt"`
	Components []string `json:"components"`
}

// readManifest reads ~/.gemini/config/import_manifest.json. A missing or
// unparseable file reads as an empty manifest.
func readManifest(configHome string) *importManifest {
	m := &importManifest{}
	data, err := os.ReadFile(filepath.Join(configHome, "import_manifest.json"))
	if err != nil {
		return m
	}
	var doc struct {
		Imports []manifestEntry `json:"imports"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return m
	}
	m.entries = doc.Imports
	return m
}

// has reports whether the manifest already lists name for the source.
func (m *importManifest) has(src Source, name string) bool {
	want := manifestSource(src)
	for _, e := range m.entries {
		if e.Source == want && e.Name == name {
			return true
		}
	}
	return false
}

// set records a successful import, replacing an earlier entry with the same
// source and name.
func (m *importManifest) set(src Source, res result) {
	want := manifestSource(src)
	entry := manifestEntry{
		Name:       res.name,
		Source:     want,
		ImportedAt: time.Now().UTC().Format(time.RFC3339),
		Components: manifestComponents(res),
	}
	for i, e := range m.entries {
		if e.Source == want && e.Name == res.name {
			m.entries[i] = entry
			return
		}
	}
	m.entries = append(m.entries, entry)
}

// manifestComponents lists the staged components in the reference's
// manifest order. An empty list is written as null.
func manifestComponents(res result) []string {
	var names []string
	for _, label := range manifestOrder {
		for _, c := range res.components {
			if c.label == label {
				names = append(names, label)
			}
		}
	}
	return names
}

// write writes the manifest back to the config home.
func (m *importManifest) write(configHome string) error {
	if len(m.entries) == 0 {
		return nil
	}
	doc := struct {
		Imports []manifestEntry `json:"imports"`
	}{Imports: m.entries}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configHome, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configHome, "import_manifest.json"), data, 0600)
}

// manifestSource maps a Source to the string the reference records.
func manifestSource(src Source) string {
	if src == SourceClaude {
		return manifestSourceClaude
	}
	return manifestSourceGemini
}

// list.go — markdown custom agents and the `--agent` validator.
//
// The reference loads custom agents from four flat sources:
//
//	<workspace>/.agents/agents/*.md
//	~/.gemini/config/agents/*.md
//	<workspace>/.agents/plugins/<plugin>/agents/*.md
//	~/.gemini/config/plugins/<plugin>/agents/*.md
//
// A Markdown file is an agent when its YAML frontmatter has a non-empty
// `name` and `description`; the body is the agent's system prompt. Files
// without frontmatter, without either field, or with invalid YAML are
// skipped silently. Each source may also carry an `agents.json` next to the
// `agents/` directory whose `exclude` list drops agents by Markdown file
// stem (not frontmatter name). Plugin folders load only with a parseable
// plugin.json; user plugins obey config.json's plugins.<name>.enabled.
//
// `default` (and the empty string) is the built-in agent: it is accepted but
// never appears in the custom list.
//
// Reference: AGY 1.3.4 changelog — "Fixed `--agent` silently running the
// default agent when the name matches no agent: headless `-p` runs now exit
// with code 1 and list the available agents, and interactive sessions show an
// `Agent Not Found` warning before falling back to the default agent."
// The exact message forms are 【一手】 from 1.4.3:
//
//	error: unknown agent "nosuch" (no custom agents are available)
//	error: unknown agent "nosuch" (available: foo, bar)
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/justwasm/pigasus/internal/plugins"
)

// Agent is one markdown agent definition.
type Agent struct {
	// Name is the frontmatter `name`, which is what `--agent` matches.
	Name string
	// Description is the frontmatter `description` (required by the loader).
	Description string
	// Body is the Markdown body after the frontmatter; the reference sends it
	// as the agent's system prompt.
	Body string
	// Path is the absolute path of the defining .md file.
	Path string
}

// DiscoverOptions overrides the ambient directories (tests). Empty fields use
// the process working directory / home.
type DiscoverOptions struct {
	Workspace string
	Home      string
}

// Discover returns the custom agents visible to this process, sorted by name.
// The workspace scope wins when both scopes define the same name.
func Discover() []Agent {
	return DiscoverWith(DiscoverOptions{})
}

// DiscoverWith is Discover with explicit roots.
func DiscoverWith(opts DiscoverOptions) []Agent {
	workspace := opts.Workspace
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	// The user customizations tree is ~/.gemini/config, sibling to the app
	// data dir; $ANTIGRAVITY_HOME moves both together (auth does the same).
	// DiscoverOptions.Home pins it for tests.
	configHome := filepath.Join(home, ".gemini", "config")
	if opts.Home == "" {
		if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
			configHome = filepath.Join(filepath.Dir(h), "config")
		}
	}

	byName := map[string]Agent{}
	// Lower precedence first; later scopes replace earlier ones. A workspace
	// markdown agent outranks a user agent and both outrank plugin-provided
	// agents (the reference's explicit customizations scopes).
	for _, agents := range [][]Agent{
		discoverPlugins(filepath.Join(configHome, "plugins"),
			filepath.Join(configHome, "config.json")),
		discoverPlugins(filepath.Join(workspace, ".agents", "plugins"), ""),
		discoverScope(configHome),
		discoverScope(filepath.Join(workspace, ".agents")),
	} {
		for _, a := range agents {
			byName[a.Name] = a
		}
	}

	out := make([]Agent, 0, len(byName))
	for _, a := range byName {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// discoverScope reads `<scope>/agents/*.md` with `<scope>/agents.json`
// exclusions. `scope` is the directory that contains `agents/`.
func discoverScope(scope string) []Agent {
	agentsDir := filepath.Join(scope, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil
	}
	excluded := readAgentExclusions(filepath.Join(scope, "agents.json"))
	var out []Agent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(agentsDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		a, ok := parseAgentFile(raw)
		if !ok {
			continue
		}
		// agents.json excludes Markdown *stems*, not frontmatter names
		// (verified 1.4.3: agents/bar.md with name baz is excluded by "bar",
		// not "baz").
		stem := strings.TrimSuffix(e.Name(), ".md")
		if excluded[stem] || a.Name == "default" {
			continue
		}
		a.Path = path
		out = append(out, a)
	}
	return out
}

// parseAgentFile splits the frontmatter and body. The loader requires a
// leading `---` block and both name and description; anything else is ignored.
func parseAgentFile(raw []byte) (Agent, bool) {
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return Agent{}, false
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Agent{}, false
	}
	front := rest[:end]
	body := strings.TrimPrefix(rest[end+len("\n---"):], "\n")
	var fields struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(front), &fields); err != nil {
		return Agent{}, false
	}
	name := strings.TrimSpace(fields.Name)
	description := strings.TrimSpace(fields.Description)
	if name == "" || description == "" {
		return Agent{}, false
	}
	return Agent{Name: name, Description: description, Body: strings.TrimSpace(body)}, true
}

// discoverPlugins reads `<pluginsDir>/<plugin>/agents/*.md` for every
// installed plugin. A directory is a plugin only when it has a parseable
// plugin.json (the reference ignores a folder without one — verified 1.4.3,
// re-verified 1.4.5).
// configPath, when non-empty, is the user config.json whose
// plugins.<name>.enabled=false disables a plugin (workspace plugins were not
// affected by `.agents/config.json` in the probe, so the workspace call passes
// "").
//
// The plugin set itself comes from internal/plugins, so the prompt builder's
// rule discovery and this loader agree on which plugins exist.
func discoverPlugins(pluginsDir, configPath string) []Agent {
	var out []Agent
	for _, dir := range plugins.LoadedDirs(pluginsDir, configPath) {
		out = append(out, discoverScope(dir)...)
	}
	return out
}

// readAgentExclusions returns the `exclude` set of an agents.json, or nil when
// the file is absent or unreadable. The --agent loader does not surface these
// errors; `agy plugin validate` is where they are reported.
func readAgentExclusions(path string) map[string]bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	raw = []byte(strings.TrimPrefix(string(raw), "\ufeff"))
	var cfg struct {
		Exclude []string `json:"exclude"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	if len(cfg.Exclude) == 0 {
		return nil
	}
	set := make(map[string]bool, len(cfg.Exclude))
	for _, name := range cfg.Exclude {
		set[name] = true
	}
	return set
}

// Lookup resolves a custom agent by name. `default` and "" are not custom
// agents; callers handle them before consulting Lookup.
func Lookup(name string) (Agent, bool) {
	for _, a := range Discover() {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// AvailableAgents returns the sorted custom agent names. `default` is not
// listed: the reference's error message only lists custom agents.
func AvailableAgents() []string {
	agents := Discover()
	out := make([]string, len(agents))
	for i, a := range agents {
		out[i] = a.Name
	}
	return out
}

// AvailableAgentsString renders the catalogue as a comma-separated list.
func AvailableAgentsString() string {
	return strings.Join(AvailableAgents(), ", ")
}

// AgentNotFoundError is the typed error ValidateAgent returns.
type AgentNotFoundError struct {
	// Name is the unrecognised name the user supplied (verbatim).
	Name string
	// Available is the sorted custom-agent list at validation time.
	Available []string
}

// Error is the reference's headless one-line form.
func (e *AgentNotFoundError) Error() string {
	if len(e.Available) == 0 {
		return fmt.Sprintf("error: unknown agent %q (no custom agents are available)", e.Name)
	}
	return fmt.Sprintf("error: unknown agent %q (available: %s)", e.Name, strings.Join(e.Available, ", "))
}

// ValidateAgent resolves a --agent flag value. Empty and "default" are always
// accepted; any other value must name a discovered custom agent.
func ValidateAgent(name string) error {
	if name == "" || name == "default" {
		return nil
	}
	for _, a := range Discover() {
		if a.Name == name {
			return nil
		}
	}
	return &AgentNotFoundError{Name: name, Available: AvailableAgents()}
}

// IsAgentNotFound reports whether err is an *AgentNotFoundError.
func IsAgentNotFound(err error) bool {
	_, ok := err.(*AgentNotFoundError)
	return ok
}

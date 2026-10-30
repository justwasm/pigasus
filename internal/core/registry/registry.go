package registry

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
)

var (
	registryMu          sync.RWMutex
	engines             = make(map[string]Engine)
	providers           = make(map[string]ToolProvider)
	subsystems          = make(map[string]Subsystem)
	subcommands         = make(map[string]cli.SubcommandSpec)
	tools               = make(map[string]Tool)
	defaultEng          = ""
	tuiRunner           func(w io.Writer, fs *flag.FlagSet) error
	tuiSnapshotRenderer func(w io.Writer) error
)

// RegisterEngine registers an agent loop engine.
func RegisterEngine(name string, e Engine) {
	registryMu.Lock()
	defer registryMu.Unlock()
	engines[name] = e
	if defaultEng == "" {
		defaultEng = name
	}
}

// GetEngine retrieves an engine by name.
func GetEngine(name string) (Engine, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	e, ok := engines[name]
	return e, ok
}

// DefaultEngine returns the default engine.
func DefaultEngine() (Engine, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if defaultEng == "" {
		return nil, fmt.Errorf("registry: no agent engine registered")
	}
	e, ok := engines[defaultEng]
	if !ok {
		return nil, fmt.Errorf("registry: default engine %q not found", defaultEng)
	}
	return e, nil
}

// SetDefaultEngine overrides the default engine name.
func SetDefaultEngine(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	defaultEng = name
}

// ListEngines returns all registered engine names.
func ListEngines() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(engines))
	for n := range engines {
		names = append(names, n)
	}
	return names
}

// RegisterToolProvider registers a tool provider.
func RegisterToolProvider(p ToolProvider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	providers[p.Name()] = p
}

// GetToolProviders returns all registered tool providers.
func GetToolProviders() []ToolProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	list := make([]ToolProvider, 0, len(providers))
	for _, p := range providers {
		list = append(list, p)
	}
	return list
}

// AllToolDeclarations aggregates all declarations from registered providers and individual tools.
func AllToolDeclarations() []backend.ToolDecl {
	registryMu.RLock()
	defer registryMu.RUnlock()
	var decls []backend.ToolDecl
	seen := make(map[string]bool)
	for _, p := range providers {
		for _, d := range p.Declarations() {
			if !seen[d.Name] {
				seen[d.Name] = true
				decls = append(decls, d)
			}
		}
	}
	for _, t := range tools {
		d := t.Decl()
		if !seen[d.Name] {
			seen[d.Name] = true
			decls = append(decls, d)
		}
	}
	return decls
}

// DispatchToolCall dispatches a tool call to the registered provider or tool.
func DispatchToolCall(ctx context.Context, call backend.ToolCall) (string, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, p := range providers {
		res, err := p.Run(ctx, call)
		if err == nil {
			return res, nil
		}
	}
	if t, ok := tools[call.Name]; ok {
		return t.Run(ctx, []byte(call.Arguments))
	}
	return "", ErrUnknownTool(call.Name)
}

// ErrUnknownTool returns an error indicating tool is not recognized.
func ErrUnknownTool(name string) error {
	return fmt.Errorf("registry: unknown tool %q", name)
}

// RegisterTool registers an individual tool.
func RegisterTool(t Tool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	tools[t.Name()] = t
}

// GetTool retrieves a registered tool by name.
func GetTool(name string) (Tool, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	t, ok := tools[name]
	return t, ok
}

// ListTools returns all registered tools.
func ListTools() []Tool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	list := make([]Tool, 0, len(tools))
	for _, t := range tools {
		list = append(list, t)
	}
	return list
}

// RegisterSubsystem registers an optional platform subsystem.
func RegisterSubsystem(s Subsystem) {
	registryMu.Lock()
	defer registryMu.Unlock()
	subsystems[s.Name()] = s
}

// GetSubsystems returns all registered subsystems.
func GetSubsystems() []Subsystem {
	registryMu.RLock()
	defer registryMu.RUnlock()
	list := make([]Subsystem, 0, len(subsystems))
	for _, s := range subsystems {
		list = append(list, s)
	}
	return list
}

// GetSubsystem returns a registered subsystem by name.
func GetSubsystem(name string) (Subsystem, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	s, ok := subsystems[name]
	return s, ok
}

// HasSubsystem reports whether a named subsystem is registered and enabled.
func HasSubsystem(name string) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	s, ok := subsystems[name]
	return ok && s.Enabled()
}

// RegisterSubcommand registers a subcommand spec.
func RegisterSubcommand(spec cli.SubcommandSpec) {
	registryMu.Lock()
	defer registryMu.Unlock()
	subcommands[spec.Name] = spec
}

// RegisterSubcommands registers multiple subcommand specs.
func RegisterSubcommands(specs ...cli.SubcommandSpec) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, spec := range specs {
		subcommands[spec.Name] = spec
	}
}

// GetSubcommands returns all registered subcommand specs, sorted by Name.
func GetSubcommands() []cli.SubcommandSpec {
	registryMu.RLock()
	defer registryMu.RUnlock()
	list := make([]cli.SubcommandSpec, 0, len(subcommands))
	for _, spec := range subcommands {
		list = append(list, spec)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// SetTUIRunner registers the interactive TUI runner.
func SetTUIRunner(fn func(w io.Writer, fs *flag.FlagSet) error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	tuiRunner = fn
}

// GetTUIRunner returns the registered interactive TUI runner, if any.
func GetTUIRunner() func(w io.Writer, fs *flag.FlagSet) error {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return tuiRunner
}

// SetTUISnapshotRenderer registers the static TUI snapshot renderer.
func SetTUISnapshotRenderer(fn func(w io.Writer) error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	tuiSnapshotRenderer = fn
}

// GetTUISnapshotRenderer returns the registered static TUI snapshot renderer, if any.
func GetTUISnapshotRenderer() func(w io.Writer) error {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return tuiSnapshotRenderer
}

// ClearRegistry resets all registries. Useful for tests.
func ClearRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	engines = make(map[string]Engine)
	providers = make(map[string]ToolProvider)
	subsystems = make(map[string]Subsystem)
	subcommands = make(map[string]cli.SubcommandSpec)
	tools = make(map[string]Tool)
	defaultEng = ""
	tuiRunner = nil
	tuiSnapshotRenderer = nil
}

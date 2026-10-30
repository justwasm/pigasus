package modular

import (
	"context"
	"flag"
	"io"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/registry"
)

// RegisterEngine registers an agent loop engine.
func RegisterEngine(name string, e Engine) {
	registry.RegisterEngine(name, e)
}

// GetEngine retrieves an engine by name.
func GetEngine(name string) (Engine, bool) {
	return registry.GetEngine(name)
}

// DefaultEngine returns the default engine.
func DefaultEngine() (Engine, error) {
	return registry.DefaultEngine()
}

// SetDefaultEngine overrides the default engine name.
func SetDefaultEngine(name string) {
	registry.SetDefaultEngine(name)
}

// ListEngines returns all registered engine names.
func ListEngines() []string {
	return registry.ListEngines()
}

// RegisterToolProvider registers a tool provider.
func RegisterToolProvider(p ToolProvider) {
	registry.RegisterToolProvider(p)
}

// GetToolProviders returns all registered tool providers.
func GetToolProviders() []ToolProvider {
	return registry.GetToolProviders()
}

// AllToolDeclarations aggregates all declarations from registered providers and individual tools.
func AllToolDeclarations() []backend.ToolDecl {
	return registry.AllToolDeclarations()
}

// DispatchToolCall dispatches a tool call to the registered provider or tool.
func DispatchToolCall(ctx context.Context, call backend.ToolCall) (string, error) {
	return registry.DispatchToolCall(ctx, call)
}

// ErrUnknownTool returns an error indicating tool is not recognized.
func ErrUnknownTool(name string) error {
	return registry.ErrUnknownTool(name)
}

// RegisterTool registers an individual tool.
func RegisterTool(t Tool) {
	registry.RegisterTool(t)
}

// GetTool retrieves a registered tool by name.
func GetTool(name string) (Tool, bool) {
	return registry.GetTool(name)
}

// ListTools returns all registered tools.
func ListTools() []Tool {
	return registry.ListTools()
}

// RegisterSubsystem registers an optional platform subsystem.
func RegisterSubsystem(s Subsystem) {
	registry.RegisterSubsystem(s)
}

// GetSubsystems returns all registered subsystems.
func GetSubsystems() []Subsystem {
	return registry.GetSubsystems()
}

// GetSubsystem returns a registered subsystem by name.
func GetSubsystem(name string) (Subsystem, bool) {
	return registry.GetSubsystem(name)
}

// HasSubsystem reports whether a named subsystem is registered and enabled.
func HasSubsystem(name string) bool {
	return registry.HasSubsystem(name)
}

// RegisterSubcommand registers a subcommand spec.
func RegisterSubcommand(spec cli.SubcommandSpec) {
	registry.RegisterSubcommand(spec)
}

// RegisterSubcommands registers multiple subcommand specs.
func RegisterSubcommands(specs ...cli.SubcommandSpec) {
	registry.RegisterSubcommands(specs...)
}

// GetSubcommands returns all registered subcommand specs, sorted by Name.
func GetSubcommands() []cli.SubcommandSpec {
	return registry.GetSubcommands()
}

// SetTUIRunner registers the interactive TUI runner.
func SetTUIRunner(fn func(w io.Writer, fs *flag.FlagSet) error) {
	registry.SetTUIRunner(fn)
}

// GetTUIRunner returns the registered interactive TUI runner, if any.
func GetTUIRunner() func(w io.Writer, fs *flag.FlagSet) error {
	return registry.GetTUIRunner()
}

// SetTUISnapshotRenderer registers the static TUI snapshot renderer.
func SetTUISnapshotRenderer(fn func(w io.Writer) error) {
	registry.SetTUISnapshotRenderer(fn)
}

// GetTUISnapshotRenderer returns the registered static TUI snapshot renderer, if any.
func GetTUISnapshotRenderer() func(w io.Writer) error {
	return registry.GetTUISnapshotRenderer()
}

// ClearRegistry resets all registries. Useful for tests.
func ClearRegistry() {
	registry.ClearRegistry()
}

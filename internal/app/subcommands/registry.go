// Package subcommands holds the clone's subcommand dispatch table.
//
// The table is not a *registry* in the sense of `internal/core/registry`:
// that registry holds the pluggable pieces of a session — the turn
// engines, the tool providers, the platform subsystems — and every one
// of them is defined below the CLI. A subcommand is the opposite: it is
// the CLI's own dispatch surface, and its handler needs the CLI engine
// itself (`*cli.JetskiCLI`), so the spec type cannot live below the
// host that owns it.
//
// The table therefore sits in the app tier, next to the registration
// shims that fill it, and the core registry no longer has to know that
// subcommands exist at all.
package subcommands

import (
	"sort"
	"sync"

	"github.com/justwasm/pigasus/internal/core/cliflags"
)

var (
	mu        sync.RWMutex
	table     = make(map[string]cliflags.SubcommandSpec)
	providers = make([]func() []cliflags.SubcommandSpec, 0)
)

// RegisterProvider registers a dynamic provider of subcommand specs.
func RegisterProvider(fn func() cliflags.SubcommandSpec) {
	mu.Lock()
	defer mu.Unlock()
	providers = append(providers, func() []cliflags.SubcommandSpec {
		return []cliflags.SubcommandSpec{fn()}
	})
}

// RegisterMultiProvider registers a dynamic provider of multiple subcommand specs.
func RegisterMultiProvider(fn func() []cliflags.SubcommandSpec) {
	mu.Lock()
	defer mu.Unlock()
	providers = append(providers, fn)
}

// RegisterSubcommand registers a subcommand spec.
func RegisterSubcommand(spec cliflags.SubcommandSpec) {
	mu.Lock()
	defer mu.Unlock()
	table[spec.Name] = spec
}

// RegisterSubcommands registers multiple subcommand specs.
func RegisterSubcommands(specs ...cliflags.SubcommandSpec) {
	mu.Lock()
	defer mu.Unlock()
	for _, spec := range specs {
		table[spec.Name] = spec
	}
}

// GetSubcommands returns all registered subcommand specs, sorted by Name.
func GetSubcommands() []cliflags.SubcommandSpec {
	mu.RLock()
	defer mu.RUnlock()
	combined := make(map[string]cliflags.SubcommandSpec, len(table)+len(providers))
	for name, spec := range table {
		combined[name] = spec
	}
	for _, p := range providers {
		for _, spec := range p() {
			combined[spec.Name] = spec
		}
	}
	list := make([]cliflags.SubcommandSpec, 0, len(combined))
	for _, spec := range combined {
		list = append(list, spec)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

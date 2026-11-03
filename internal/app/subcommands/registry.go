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

	"github.com/justwasm/pigasus/internal/cli"
)

var (
	mu    sync.RWMutex
	table = make(map[string]cli.SubcommandSpec)
)

// RegisterSubcommand registers a subcommand spec.
func RegisterSubcommand(spec cli.SubcommandSpec) {
	mu.Lock()
	defer mu.Unlock()
	table[spec.Name] = spec
}

// RegisterSubcommands registers multiple subcommand specs.
func RegisterSubcommands(specs ...cli.SubcommandSpec) {
	mu.Lock()
	defer mu.Unlock()
	for _, spec := range specs {
		table[spec.Name] = spec
	}
}

// GetSubcommands returns all registered subcommand specs, sorted by Name.
func GetSubcommands() []cli.SubcommandSpec {
	mu.RLock()
	defer mu.RUnlock()
	list := make([]cli.SubcommandSpec, 0, len(table))
	for _, spec := range table {
		list = append(list, spec)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

package agentturn

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// ProcCloser closes a process's stdin and terminates the process when Close is called.
type ProcCloser struct {
	Cmd   *exec.Cmd
	Stdin io.Closer
}

// Close closes Stdin and kills the process if still running.
func (p *ProcCloser) Close() error {
	if p.Stdin != nil {
		_ = p.Stdin.Close()
	}
	if p.Cmd != nil && p.Cmd.Process != nil {
		_ = p.Cmd.Process.Kill()
		_ = p.Cmd.Wait()
	}
	return nil
}

// SpawnOptions defines common configuration when launching an external agent backend.
type SpawnOptions struct {
	BinaryPath string
	CWD        string
	Model      string
	Effort     string
	SessionID  string
	Args       []string
}

// SpawnerFunc creates an Engine instance for a registered backend.
type SpawnerFunc func(ctx context.Context, opts SpawnOptions) (Engine, error)

// Driver describes a registered external agent engine plugin.
type Driver struct {
	Name         string
	Aliases      []string
	Description  string
	DefaultModel string
	Spawn        SpawnerFunc
}

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]Driver)
)

// Register registers an agentturn backend driver.
// If Register is called twice with the same name or with a nil SpawnerFunc, it panics.
func Register(driver Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	if driver.Name == "" {
		panic("agentturn: Register driver with empty name")
	}
	if driver.Spawn == nil {
		panic("agentturn: Register driver with nil Spawn")
	}
	name := strings.ToLower(driver.Name)
	if _, dup := drivers[name]; dup {
		panic("agentturn: Register called twice for driver " + name)
	}
	drivers[name] = driver
	for _, alias := range driver.Aliases {
		aliasLower := strings.ToLower(alias)
		drivers[aliasLower] = driver
	}
}

// Lookup finds a registered driver by name or alias.
func Lookup(name string) (Driver, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	d, ok := drivers[strings.ToLower(strings.TrimSpace(name))]
	return d, ok
}

// Drivers returns a sorted list of all unique registered driver names.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	seen := make(map[string]bool)
	names := make([]string, 0, len(drivers))
	for _, d := range drivers {
		if !seen[d.Name] {
			seen[d.Name] = true
			names = append(names, d.Name)
		}
	}
	sort.Strings(names)
	return names
}

// Spawn launches an engine using the registered driver for the specified backend.
func Spawn(ctx context.Context, backend string, opts SpawnOptions) (Engine, error) {
	d, ok := Lookup(backend)
	if !ok {
		return nil, fmt.Errorf("agentturn: unknown backend driver %q (registered: %s)", backend, strings.Join(Drivers(), ", "))
	}
	return d.Spawn(ctx, opts)
}

// Package remotecontrol — cascade_registry.go: the
// (*CascadeRegistry) type plus its sentinels and accessors.
// Pure file split; every method here was previously inline in
// cascade.go. Cycle rc-filesize-internal-remotecontrol lifts
// them out so cascade.go can stay focused on the *Cascade
// struct + its immediate getters.
//
// Send / SendStream live in cascade_io.go (the cycle that
// introduced streaming); the lifecycle hooks (Cancel /
// StartCancel / Focus / etc.) live in cascade_lifecycle.go.
package remotecontrol

import (
	"errors"
	"sync"
)

// CascadeRegistryDefaultMax is the per-process concurrency cap a
// fresh CascadeRegistry uses when Config.Cascades is left nil.
const CascadeRegistryDefaultMax = 256

// ErrRegistryFull is returned by CascadeRegistry.Register when
// adding another cascade would push past Max.
var ErrRegistryFull = errors.New("remotecontrol: cascade registry is full")

// ErrCascadeNotFound is returned by future cascade-keyed methods
// when the dashboard's cascade_id doesn't match any registered
// cascade.
var ErrCascadeNotFound = errors.New("remotecontrol: cascade not found")

// CascadeRegistry hands out IDs, looks them up, and bounds the
// per-process concurrency. One *CascadeRegistry per
// *Dispatcher (built by NewDispatcher when Config.Cascades is
// nil).
//
// Safe for concurrent use: Register/Get/Drop/Len take the
// RWMutex (write-lock for Register/Drop to serialise the map;
// read-lock for Get/Len to scale under the model's per-turn
// notification flood).
type CascadeRegistry struct {
	mu       sync.RWMutex
	cascades map[string]*Cascade
	max      int
}

// NewCascadeRegistry returns a registry with the given max
// capacity. max <= 0 falls back to CascadeRegistryDefaultMax so
// a misconfigured caller can't disable the cap.
func NewCascadeRegistry(max int) *CascadeRegistry {
	if max <= 0 {
		max = CascadeRegistryDefaultMax
	}
	return &CascadeRegistry{
		cascades: map[string]*Cascade{},
		max:      max,
	}
}

// Register inserts c into the registry, returning ErrRegistryFull
// when the cap is reached. The cascade's ID is the key; a cascade
// with the same ID twice is silently overwritten.
func (r *CascadeRegistry) Register(c *Cascade) error {
	if r == nil || c == nil {
		return errors.New("remotecontrol: Register called on nil registry or cascade")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cascades) >= r.max {
		return ErrRegistryFull
	}
	r.cascades[c.ID] = c
	return nil
}

// Get returns the cascade registered under id, or (nil, false)
// when no such cascade exists.
func (r *CascadeRegistry) Get(id string) (*Cascade, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.cascades[id]
	return c, ok
}

// Drop removes the cascade under id. A Drop on an unknown id is a
// no-op.
func (r *CascadeRegistry) Drop(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cascades, id)
}

// Len returns the number of cascades currently registered.
func (r *CascadeRegistry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cascades)
}

// Max returns the configured cap.
func (r *CascadeRegistry) Max() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.max
}

// IDs returns a snapshot of every registered cascade id. Cheap
// O(n) copy so a future cycle's list method can render a
// dashboard-friendly view without holding the lock.
func (r *CascadeRegistry) IDs() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.cascades))
	for id := range r.cascades {
		out = append(out, id)
	}
	return out
}

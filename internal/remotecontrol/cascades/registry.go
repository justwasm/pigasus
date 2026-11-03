// Package cascades — registry.go: the
// (*Registry) type plus its sentinels and accessors.
// Pure file split; every method here was previously inline in
// cascade.go. Cycle rc-filesize-internal-remotecontrol lifts
// them out so cascade.go can stay focused on the *Cascade
// struct + its immediate getters.
//
// Send / SendStream live in turn.go (the cycle that
// introduced streaming); the lifecycle hooks (Cancel /
// StartCancel / Focus / etc.) live in lifecycle.go.
package cascades

import (
	"errors"
	"sync"
)

// RegistryDefaultMax is the per-process concurrency cap a
// fresh Registry uses when Config.Cascades is left nil.
const RegistryDefaultMax = 256

// ErrRegistryFull is returned by Registry.Register when
// adding another cascade would push past Max.
var ErrRegistryFull = errors.New("remotecontrol: cascade registry is full")

// ErrNotFound is returned by future cascade-keyed methods
// when the dashboard's cascade_id doesn't match any registered
// cascade.
var ErrNotFound = errors.New("remotecontrol: cascade not found")

// Registry hands out IDs, looks them up, and bounds the
// per-process concurrency. One *Registry per
// *Dispatcher (built by NewDispatcher when Config.Cascades is
// nil).
//
// Safe for concurrent use: Register/Get/Drop/Len take the
// RWMutex (write-lock for Register/Drop to serialise the map;
// read-lock for Get/Len to scale under the model's per-turn
// notification flood).
type Registry struct {
	mu       sync.RWMutex
	cascades map[string]*Cascade
	max      int
}

// NewRegistry returns a registry with the given max
// capacity. max <= 0 falls back to RegistryDefaultMax so
// a misconfigured caller can't disable the cap.
func NewRegistry(max int) *Registry {
	if max <= 0 {
		max = RegistryDefaultMax
	}
	return &Registry{
		cascades: map[string]*Cascade{},
		max:      max,
	}
}

// Register inserts c into the registry, returning ErrRegistryFull
// when the cap is reached. The cascade's ID is the key; a cascade
// with the same ID twice is silently overwritten.
func (r *Registry) Register(c *Cascade) error {
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
func (r *Registry) Get(id string) (*Cascade, bool) {
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
func (r *Registry) Drop(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cascades, id)
}

// Len returns the number of cascades currently registered.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cascades)
}

// Max returns the configured cap.
func (r *Registry) Max() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.max
}

// Active returns the number of registered cascades that are not
// closed.
//
// The count is computed under the registry's read lock, so a
// caller that only wants the number never has to walk the map
// itself — the registry owns its own entries.
func (r *Registry) Active() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	active := 0
	for _, c := range r.cascades {
		if c == nil {
			continue
		}
		if !c.IsClosed() {
			active++
		}
	}
	return active
}

// IDs returns a snapshot of every registered cascade id. Cheap
// O(n) copy so a future cycle's list method can render a
// dashboard-friendly view without holding the lock.
func (r *Registry) IDs() []string {
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

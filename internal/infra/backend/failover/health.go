package failover

import (
	"sync"
	"time"
)

// HealthState represents the circuit-breaker state of a failover target.
type HealthState int

const (
	// StateClosed indicates the target is healthy and accepting traffic.
	StateClosed HealthState = iota
	// StateOpen indicates the target recently failed and is in cooldown.
	StateOpen
	// StateHalfOpen indicates the cooldown elapsed and the next request probes recovery.
	StateHalfOpen
)

func (s HealthState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

const (
	defaultBaseCooldown = 15 * time.Second
	defaultMaxCooldown  = 5 * time.Minute
)

// HealthConfig configures a HealthTracker's cooldown schedule and clock.
type HealthConfig struct {
	BaseCooldown time.Duration
	MaxCooldown  time.Duration
	Now          func() time.Time
}

type targetHealth struct {
	state               HealthState
	consecutiveFailures int
	openUntil           time.Time
}

// HealthTracker tracks cross-turn circuit-breaker health per failover target.
type HealthTracker struct {
	mu           sync.Mutex
	baseCooldown time.Duration
	maxCooldown  time.Duration
	now          func() time.Time
	targets      map[string]*targetHealth
}

// NewHealthTracker constructs an isolated HealthTracker.
func NewHealthTracker(cfg ...HealthConfig) *HealthTracker {
	var c HealthConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	if c.BaseCooldown <= 0 {
		c.BaseCooldown = defaultBaseCooldown
	}
	if c.MaxCooldown <= 0 {
		c.MaxCooldown = defaultMaxCooldown
	}
	if c.MaxCooldown < c.BaseCooldown {
		c.MaxCooldown = c.BaseCooldown
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return &HealthTracker{
		baseCooldown: c.BaseCooldown,
		maxCooldown:  c.MaxCooldown,
		now:          c.Now,
		targets:      make(map[string]*targetHealth),
	}
}

var defaultHealthTracker = NewHealthTracker()

// DefaultHealthTracker returns the process-wide shared HealthTracker used by
// failover.New so target health persists across turns.
func DefaultHealthTracker() *HealthTracker {
	return defaultHealthTracker
}

// Reset clears all tracked target health states.
func (h *HealthTracker) Reset() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.targets = make(map[string]*targetHealth)
}

// State returns the current circuit state for target, promoting expired Open
// circuits to HalfOpen.
func (h *HealthTracker) State(target string) HealthState {
	if h == nil || target == "" {
		return StateClosed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.targets[target]
	if !ok {
		return StateClosed
	}
	if entry.state == StateOpen && !h.now().Before(entry.openUntil) {
		entry.state = StateHalfOpen
	}
	return entry.state
}

// ShouldAttempt reports whether target is healthy or ready for a HalfOpen probe.
func (h *HealthTracker) ShouldAttempt(target string) bool {
	if h == nil || target == "" {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.targets[target]
	if !ok {
		return true
	}
	switch entry.state {
	case StateClosed, StateHalfOpen:
		return true
	case StateOpen:
		if !h.now().Before(entry.openUntil) {
			entry.state = StateHalfOpen
			return true
		}
		return false
	default:
		return true
	}
}

// CooldownRemaining returns the remaining Open cooldown duration for target, or 0.
func (h *HealthTracker) CooldownRemaining(target string) time.Duration {
	if h == nil || target == "" {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.targets[target]
	if !ok || entry.state != StateOpen {
		return 0
	}
	rem := entry.openUntil.Sub(h.now())
	if rem <= 0 {
		return 0
	}
	return rem
}

// RecordSuccess resets target back to StateClosed.
func (h *HealthTracker) RecordSuccess(target string) {
	if h == nil || target == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.targets, target)
}

// RecordFailure opens the circuit for target with exponential cooldown backoff.
func (h *HealthTracker) RecordFailure(target string, err error) {
	if h == nil || target == "" || err == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.targets[target]
	if !ok {
		entry = &targetHealth{}
		h.targets[target] = entry
	}
	entry.consecutiveFailures++
	entry.state = StateOpen
	cooldown := h.baseCooldown
	for i := 1; i < entry.consecutiveFailures; i++ {
		if cooldown >= h.maxCooldown/2 {
			cooldown = h.maxCooldown
			break
		}
		cooldown *= 2
	}
	if cooldown > h.maxCooldown {
		cooldown = h.maxCooldown
	}
	entry.openUntil = h.now().Add(cooldown)
}

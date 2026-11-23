package gateway

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/core/apierror"
)

// KeyAccount represents one API key / account credential in a gateway KeyPool.
type KeyAccount struct {
	ID     string
	APIKey string
}

type keyEntry struct {
	id            string
	key           string
	failures      int
	cooldownUntil time.Time
}

// KeyPoolOption configures a KeyPool.
type KeyPoolOption func(*KeyPool)

// WithKeyPoolClock overrides the clock used for cooldown calculations (for tests).
func WithKeyPoolClock(now func() time.Time) KeyPoolOption {
	return func(p *KeyPool) {
		if now != nil {
			p.now = now
		}
	}
}

// WithKeyPoolCooldown overrides the base and max cooldown durations.
func WithKeyPoolCooldown(base, max time.Duration) KeyPoolOption {
	return func(p *KeyPool) {
		if base > 0 {
			p.baseCooldown = base
		}
		if max > 0 {
			p.maxCooldown = max
		}
	}
}

// KeyPool manages thread-safe multi-account API key rotation with per-key cooldowns.
type KeyPool struct {
	mu           sync.Mutex
	entries      []keyEntry
	idx          int
	baseCooldown time.Duration
	maxCooldown  time.Duration
	now          func() time.Time
}

// NewKeyPool constructs a KeyPool from the given accounts, skipping empty keys
// and deduplicating identical keys while preserving order.
func NewKeyPool(accounts []KeyAccount, opts ...KeyPoolOption) *KeyPool {
	seen := make(map[string]bool, len(accounts))
	entries := make([]keyEntry, 0, len(accounts))
	for _, acc := range accounts {
		k := strings.TrimSpace(acc.APIKey)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		entries = append(entries, keyEntry{
			id:  strings.TrimSpace(acc.ID),
			key: k,
		})
	}
	if len(entries) == 0 {
		return nil
	}
	p := &KeyPool{
		entries:      entries,
		baseCooldown: 30 * time.Second,
		maxCooldown:  5 * time.Minute,
		now:          time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	if p.maxCooldown < p.baseCooldown {
		p.maxCooldown = p.baseCooldown
	}
	return p
}

// Len returns the number of distinct keys in the pool.
func (p *KeyPool) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// CurrentKey returns the active healthy key (or the key whose cooldown expires
// soonest if all keys are currently cooling down).
func (p *KeyPool) CurrentKey() (id, key string) {
	if p == nil {
		return "", ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return "", ""
	}
	now := p.now()
	n := len(p.entries)
	for offset := 0; offset < n; offset++ {
		i := (p.idx + offset) % n
		e := &p.entries[i]
		if e.cooldownUntil.IsZero() || !now.Before(e.cooldownUntil) {
			p.idx = i
			return e.id, e.key
		}
	}
	// All keys are in cooldown: pick the one whose cooldown expires soonest.
	bestIdx := p.idx
	bestUntil := p.entries[bestIdx].cooldownUntil
	for offset := 1; offset < n; offset++ {
		i := (p.idx + offset) % n
		if p.entries[i].cooldownUntil.Before(bestUntil) {
			bestIdx = i
			bestUntil = p.entries[i].cooldownUntil
		}
	}
	p.idx = bestIdx
	return p.entries[bestIdx].id, p.entries[bestIdx].key
}

// MarkSuccess clears any failure streak / cooldown for key.
func (p *KeyPool) MarkSuccess(key string) {
	if p == nil || key == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.entries {
		if p.entries[i].key == key {
			p.entries[i].failures = 0
			p.entries[i].cooldownUntil = time.Time{}
			return
		}
	}
}

// MarkFailure puts key into exponential cooldown if err is a key-rotation-eligible
// error (401/403/429/quota/auth failure), advances the active index, and reports
// whether another healthy key is immediately available.
func (p *KeyPool) MarkFailure(key string, err error) bool {
	if p == nil || key == "" || !isKeyRotationErr(err) {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return false
	}
	now := p.now()
	matched := -1
	for i := range p.entries {
		if p.entries[i].key == key {
			matched = i
			p.entries[i].failures++
			cooldown := p.baseCooldown
			for f := 1; f < p.entries[i].failures; f++ {
				if cooldown >= p.maxCooldown/2 {
					cooldown = p.maxCooldown
					break
				}
				cooldown *= 2
			}
			if cooldown > p.maxCooldown {
				cooldown = p.maxCooldown
			}
			p.entries[i].cooldownUntil = now.Add(cooldown)
			break
		}
	}
	if matched >= 0 {
		p.idx = (matched + 1) % len(p.entries)
	}
	for i := range p.entries {
		if i == matched {
			continue
		}
		if p.entries[i].cooldownUntil.IsZero() || !now.Before(p.entries[i].cooldownUntil) {
			return true
		}
	}
	return false
}

func isKeyRotationErr(err error) bool {
	if err == nil {
		return false
	}
	if apierror.IsAuthFailureErr(err) || apierror.IsQuotaReachedErr(err) {
		return true
	}
	var he *apierror.HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests:
			return true
		}
	}
	return false
}

var (
	sharedKeyPoolsMu sync.Mutex
	sharedKeyPools   = make(map[string]*KeyPool)
)

// SharedKeyPool returns a process-wide KeyPool for cacheKey, creating one if
// absent. Used by backendselect so per-key cooldowns persist across turns.
func SharedKeyPool(cacheKey string, accounts []KeyAccount) *KeyPool {
	if len(accounts) == 0 {
		return nil
	}
	if cacheKey == "" {
		return NewKeyPool(accounts)
	}
	sharedKeyPoolsMu.Lock()
	defer sharedKeyPoolsMu.Unlock()
	if p, ok := sharedKeyPools[cacheKey]; ok {
		return p
	}
	p := NewKeyPool(accounts)
	if p != nil {
		sharedKeyPools[cacheKey] = p
	}
	return p
}

// ResetSharedKeyPools clears cached shared key pools (for tests).
func ResetSharedKeyPools() {
	sharedKeyPoolsMu.Lock()
	defer sharedKeyPoolsMu.Unlock()
	sharedKeyPools = make(map[string]*KeyPool)
}

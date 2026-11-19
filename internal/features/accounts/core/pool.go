// Package core — pool.go
//
// The account pool's policy, and the registry it belongs to.
//
// # Why it lives here
//
// The pool is a statement about the accounts: which of them a turn may run on,
// how much quota one must still have, how long a spent one stays out. That is
// the same file's business as the accounts themselves — `accounts.json` is
// where an account is added, disabled and signed in — so the policy is a
// section of it rather than a second document to keep in step.
//
// # The shape
//
//	{
//	  "version": 1,
//	  "active": "a@example.com",
//	  "accounts": [ ... ],
//	  "account_pool": {
//	    "enabled": true,
//	    "threshold_5h": 0.15,
//	    "threshold_weekly": 0.10,
//	    "accounts": ["a@example.com", "b@example.com"]
//	  }
//	}
//
// The section is optional: a registry without one is a pool that has never
// been configured, and the callers answer their own defaults for it.
package core

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/justwasm/pigasus/accountpool"
)

// poolCache holds the last snapshot of the policy, with the stamp of the file
// it was built from, so the policy is re-read only when `accounts.json`
// changed on disk.
var poolCache atomic.Pointer[poolSnapshot]

type poolSnapshot struct {
	stamp  string
	policy *accountpool.Config
}

// poolMu serialises the policy's read-modify-write cycles, so two writers
// cannot lose each other's section.
var poolMu sync.Mutex

// storeStamp renders the identity of the registry file: its path, size and
// modification time when it exists.
func storeStamp(path string) string {
	if path == "" {
		return "-"
	}
	st, err := os.Stat(path)
	if err != nil {
		return path + "\x00-"
	}
	return fmt.Sprintf("%s\x00%d:%d", path, st.Size(), st.ModTime().UnixNano())
}

// CurrentAccountPool returns the policy `accounts.json` carries, or nil when
// it carries none.
//
// The returned policy is a copy: a caller may normalise it without touching
// the cached one.
func CurrentAccountPool() *accountpool.Config {
	path := Path()
	stamp := storeStamp(path)
	if cached := poolCache.Load(); cached != nil && cached.stamp == stamp {
		return cached.policy
	}
	var policy *accountpool.Config
	if st, err := Load(); err == nil && st.AccountPool != nil {
		copied := *st.AccountPool
		policy = &copied
	}
	poolCache.Store(&poolSnapshot{stamp: stamp, policy: policy})
	return policy
}

// SaveAccountPool writes the policy into `accounts.json`.
//
// It rewrites the one section and leaves the accounts alone — the registry is
// read, the section replaced, and the file written atomically with 0600, the
// same as every other write to it. The cached snapshot is dropped, so the
// policy the routing reads is the one that was just written.
func SaveAccountPool(policy *accountpool.Config) error {
	poolMu.Lock()
	defer poolMu.Unlock()
	return savePoolLocked(policy)
}

// savePoolLocked is SaveAccountPool without the package lock.
func savePoolLocked(policy *accountpool.Config) error {
	mu.Lock()
	defer mu.Unlock()
	st, err := loadLocked()
	if err != nil {
		return err
	}
	st.AccountPool = policy
	if err := st.saveLocked(); err != nil {
		return err
	}
	poolCache.Store(nil)
	return nil
}

// PoolPath is the file the policy is written to: the registry's own path,
// spelled out for the callers that report it to the user.
func PoolPath() string { return Path() }

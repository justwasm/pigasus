// Package remotecontrol —
// conversation_config_store.go: the public type
// surface for the ConversationConfigStore
// implementation pair. Methods live in the *_fs.go /
// *_mem.go siblings.
//
// Why type definitions live in this file: both the
// FS and Mem implementations share the same
// ConversationConfigStore interface (defined in
// conversation_config.go) and both reference the
// ConversationConfigState / ConversationConfig value
// types that are exported for the dispatcher and
// the dashboard. Keeping the type declarations
// next to the package doc lets a reader pick up the
// data model without having to chase the FS / Mem
// implementation files.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - conversation_config_store.go         — package
//     doc + FSConversationConfigStore /
//     onDiskConversationConfigState type
//     declarations.
//   - conversation_config_store_fs.go      — the
//     FSConversationConfigStore methods
//     (Snapshot / Upsert / Get) plus load /
//     atomicWrite / emptyState I/O helpers.
//   - conversation_config_store_mem.go      — the
//     MemConversationConfigStore methods plus
//     the clone / sort helpers both stores use.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"sync"
	"time"
)

// FSConversationConfigStore is the production
// ConversationConfigStore implementation. The state
// lives in a single JSON file at Path; the store
// serialises every read-modify-write (Upsert)
// through a single mutex. Snapshot / Get read paths
// take the read lock so concurrent reads don't race
// a writer's in-progress mutation.
//
// Concurrency: the store uses a single RWMutex so
// the dashboard's per-conversation upsert surface is
// serialised. The brief doesn't call for high
// concurrency on the conversation-config surface —
// a per-conversation upsert is human-paced, not
// high-frequency — so a single mutex is correct
// and cheap. A future cycle that grows to
// high-frequency polling only has to swap mu for a
// per-conversationID RWMutex.
type FSConversationConfigStore struct {
	// Path is the absolute path to the JSON
	// file the store reads / writes. Production
	// wiring uses
	// auth.AntigravityHome()/conversation_config.json;
	// tests pass a t.TempDir()-rooted path.
	Path string

	// now is the clock the store stamps
	// UpdatedAt with. nil → time.Now
	// (production). Tests override it to make
	// the timestamps deterministic.
	now func() time.Time

	// mu serialises the per-mutation
	// read-modify-write cycle. Held only across
	// the in-memory mutation + atomic save;
	// snapshot reads (Snapshot / Get) take the
	// read lock so concurrent reads don't race
	// a writer's in-progress mutation.
	mu sync.RWMutex

	// ensureDirOnce guards the directory
	// MkdirAll so concurrent first-use calls
	// don't race.
	ensureDirOnce sync.Once
}

// onDiskConversationConfigState is the JSON shape
// the FSConversationConfigStore writes to disk. The
// Version / Configs / UpdatedAt fields map 1:1 onto
// ConversationConfigState's JSON tags so the wire
// format and the on-disk format stay identical.
// Future versions of the file format (cycle n+1)
// write a new struct and branch on the Version
// stamp inside the reader.
type onDiskConversationConfigState struct {
	Version   int                           `json:"version"`
	Configs   map[string]ConversationConfig `json:"configs"`
	UpdatedAt time.Time                     `json:"updated_at"`
}

// Package remotecontrol — plugins_store.go: the public
// type surface for the PluginStore implementation pair.
// Methods live in the *_fs.go / *_mem.go siblings.
//
// Why type definitions live in this file: both the FS
// and Mem implementations share the same PluginStore
// interface (defined in plugins.go) and both reference
// the PluginState / Plugin value types that are exported
// for the dispatcher and the dashboard. Keeping the
// type declarations next to the package doc lets a
// reader pick up the data model without having to
// chase the FS / Mem implementation files.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - plugins_store.go         — package doc +
//     FSPluginStore /
//     onDiskPluginState type
//     declarations.
//   - plugins_store_fs.go      — the FSPluginStore
//     methods (Snapshot /
//     Disconnect) plus load /
//     atomicWrite /
//     fixtureSeedState I/O
//     helpers.
//   - plugins_store_mem.go      — the MemPluginStore
//     methods plus the
//     clonePluginState /
//     sortPluginsByID helpers
//     both stores use.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"sync"
	"time"
)

// FSPluginStore is the production PluginStore
// implementation. The state lives in a single JSON file at
// Path; the store serialises every read-modify-write
// (Disconnect) through a single mutex. Snapshot is the read
// path and takes the read lock so concurrent reads don't
// race a writer's in-progress mutation.
//
// Concurrency: the store uses a single RWMutex so the
// dashboard CRUD surface is serialised. The brief doesn't
// call for high concurrency on the plugin surface — two
// short-lived mutations on a single file per minute is
// typical — so a single mutex is correct and cheap. A
// future cycle that grows to high-frequency polling only
// has to swap mu for a per-mutation RWMutex.
type FSPluginStore struct {
	// Path is the absolute path to the JSON file the
	// store reads / writes. Production wiring uses
	// auth.AntigravityHome()/plugins.json; tests pass a
	// t.TempDir()-rooted path.
	Path string

	// now is the clock the store stamps UpdatedAt with
	// (and ConnectedAt on the fixture-seed path). nil →
	// time.Now (production). Tests override it to make
	// the timestamps deterministic.
	now func() time.Time

	// mu serialises the per-mutation read-modify-write
	// cycle. Held only across the in-memory mutation +
	// atomic save; snapshot reads (Snapshot) take the
	// read lock so concurrent reads don't race a
	// writer's in-progress mutation.
	mu sync.RWMutex

	// ensureDirOnce guards the directory MkdirAll so
	// concurrent first-use calls don't race.
	ensureDirOnce sync.Once
}

// onDiskPluginState is the JSON shape the FSPluginStore
// writes to disk. The Version / Plugins / UpdatedAt fields
// map 1:1 onto PluginState's JSON tags so the wire format
// and the on-disk format stay identical. Future versions of
// the file format (cycle n+1) write a new struct and branch
// on the Version stamp inside the reader.
type onDiskPluginState struct {
	Version   int       `json:"version"`
	Plugins   []Plugin  `json:"plugins"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Package projects — store.go: the
// FSStore + onDiskRecord + MemStore
// types + the cross-store helpers (NewFSStore +
// recordFromDisk). The load / write / List / Create /
// Get / Update / Delete / sweepName / readRecord /
// writeRecord methods live in store_fs.go
// (FSStore) and store_mem.go
// (MemStore).
//
// Why two implementations:
//
//   - FSStore is the production seam: one JSON
//     file per project under
//     $ANTIGRAVITY_HOME/projects/<id>.json.
//
//   - MemStore is the test seam: a map +
//     mutex.
//
// Both share the same id-mint helper (newID from
// projects.go) and the same Project record layout.
//
// Stdlib-only. No third-party deps.
package projects

import (
	"sync"
	"time"
)

// WorkspaceResolver is the narrow view of the host's filesystem
// scope the project store needs: it resolves a workspace path to
// its canonical form, or refuses it.
//
// The domain declares the interface it needs rather than naming
// the files domain's wider Provider: a project store never reads
// a file, it only vets a workspace path.
type WorkspaceResolver interface {
	ResolvePolicy(path string) (canonical string, err error)
}

// FSStore is the production Store
// implementation. Each project lives in its own JSON
// file under RootDir/<id>.json; the directory layout
// matches convstore.StoreDir()'s sibling convention
// (one row per file, no central index).
//
// Concurrency: the store uses a single mutex (mu) to
// serialise the per-file read-modify-write cycle.
//
// Cycle rc-conversation-deletions adds BoundChecker:
// when Delete is called with failIfBound=true, the
// store asks the checker how many conversations
// reference the project's workspace and refuses with
// ErrBound when the count is > 0.
type FSStore struct {
	// RootDir is the directory the store reads / writes
	// project files from.
	RootDir string

	// FS is the host's filesystem-scope policy. nil
	// means "no policy" — the dispatcher's
	// CreateRemoteAgentProject / UpdateRemoteAgentProject
	// handlers will then accept any workspace path.
	FS WorkspaceResolver

	// BoundChecker reports how many conversations
	// reference a given workspace directory. nil means
	// "no conversations bound".
	BoundChecker func(projectWorkspace string) (int, error)

	// now is the clock the store stamps CreatedAt /
	// UpdatedAt with. nil → time.Now (production).
	now func() time.Time

	// mu serialises the per-id read-modify-write cycle.
	mu sync.Mutex

	// ensureDirDone guards the RootDir MkdirAll call.
	ensureDirOnce sync.Once
}

// onDiskRecord is the JSON shape the FSStore
// writes to disk. It is intentionally a separate struct
// from the wire-facing Project so the on-disk layout can
// grow without breaking the dashboard envelope.
//
// Field names follow the brief's on-disk hint in
// docs/cycles/PLAN.md: lowercase JSON keys, RFC3339Nano
// for the timestamps.
//
// Cycle rc-resources extends the layout with the
// Resources []string field (a resource-id list the
// project is bound to).
type onDiskRecord struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Workspace     string `json:"workspace"`
	DefaultModel  string `json:"default_model"`
	DefaultEffort string `json:"default_effort"`
	// Resources is the resource-id list the project
	// is bound to. Cycle rc-resources adds this
	// field. nil = no bindings (matches a pre-
	// cycle project file).
	Resources []string  `json:"resources,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewFSStore returns a fresh FSStore
// rooted at rootDir. When fs is non-nil, the store
// validates the workspace path against the host's
// filesystem scope at Create time (rejects
// files.ErrPathNotAllowed → -32007). When boundChecker is
// non-nil the store uses it during Delete's
// failIfBound check. when now is nil, the store uses
// time.Now.
func NewFSStore(rootDir string, fs WorkspaceResolver, boundChecker func(string) (int, error), now func() time.Time) *FSStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSStore{RootDir: rootDir, FS: fs, BoundChecker: boundChecker, now: now}
}

// recordFromDisk is the on-disk-record → wire-shape
// translation. Kept as a helper so Create / Get /
// Update all build the same Project struct without
// copy-pasting the field list.
func recordFromDisk(rec onDiskRecord) Project {
	// Cycle rc-resources: copy the Resources
	// field on every read so a dashboard that
	// holds onto the returned Project can't
	// observe in-place mutations the store
	// performs on its internal state.
	var resources []string
	if rec.Resources != nil {
		resources = make([]string, len(rec.Resources))
		copy(resources, rec.Resources)
	}
	return Project{
		ID:            rec.ID,
		Name:          rec.Name,
		Workspace:     rec.Workspace,
		DefaultModel:  rec.DefaultModel,
		DefaultEffort: rec.DefaultEffort,
		Resources:     resources,
		CreatedAt:     rec.CreatedAt,
		UpdatedAt:     rec.UpdatedAt,
	}
}

// MemStore is the in-memory Store
// fixture tests inject. It is a thin slice + mutex;
// the methods implement the Store interface
// exactly the way FSStore does, with two
// differences:
//
//   - It does not consult a filesystem scope (workspaces
//     pass through verbatim).
//
//   - It does not touch the clock — CreatedAt /
//     UpdatedAt are stamped with the supplied now
//     function so tests can drive monotonic time.
//
// Cycle rc-conversation-deletions adds BoundChecker to
// mirror the FSStore seam.
type MemStore struct {
	mu     sync.Mutex
	rows   map[string]Project // keyed by ID
	byName map[string]string  // name → id, for the duplicate-name check

	// BoundChecker reports how many conversations
	// reference a given workspace directory.
	BoundChecker func(projectWorkspace string) (int, error)

	now func() time.Time
}

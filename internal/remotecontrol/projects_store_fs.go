// Package remotecontrol — projects_store_fs.go: the
// *FSProjectStore's ensureRootDir / pathFor / List /
// Create / Get / Update / Delete / sweepName /
// readRecord / writeRecord methods. Pure file split out
// of projects_store.go; projects_store.go carries the
// type definitions + cross-store helpers, and
// projects_store_mem.go carries the *MemProjectStore
// constructors + List / Create / Get / Update / Delete
// methods.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/atomicfile"
)

// ensureRootDir creates the project's RootDir if it
// doesn't exist. Single-shot (sync.Once) so concurrent
// Create calls don't race on the MkdirAll.
func (s *FSProjectStore) ensureRootDir() error {
	var err error
	s.ensureDirOnce.Do(func() {
		err = os.MkdirAll(s.RootDir, 0o700)
	})
	return err
}

// pathFor returns the on-disk path for an id. Used by
// the load / save / delete helpers below.
func (s *FSProjectStore) pathFor(id string) string {
	return filepath.Join(s.RootDir, id+".json")
}

// List enumerates the projects whose workspace path
// starts with the supplied prefix. Empty prefix means
// "all projects". The returned slice is sorted by
// Creation time ascending (oldest first) so a dashboard
// that walks the slice sees a stable sequence.
//
// `limit` is applied after the prefix filter; an
// empty/zero/negative limit means "no cap".
//
// An absent / unreadable RootDir returns nil (no error)
// — matches the convstore behaviour where a missing
// cache file is "no projects yet" rather than a hard
// failure.
func (s *FSProjectStore) List(ctx context.Context, prefix string, limit int) ([]Project, error) {
	_ = ctx
	if err := s.ensureRootDir(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.RootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	records, err := s.readProjectRecords(entries)
	if err != nil {
		return nil, err
	}
	prefix = strings.TrimSpace(prefix)
	out := make([]Project, 0, len(records))
	for _, rec := range records {
		if prefix != "" && !strings.HasPrefix(rec.Workspace, prefix) {
			continue
		}
		out = append(out, projectFromDisk(rec))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			// Stable tiebreak by ID so the
			// slice has a fully-deterministic
			// order when timestamps collide
			// (sub-microsecond writes from
			// two concurrent Creates).
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Create inserts a new project. The store validates the
// inputs (no empty name / workspace), generates a UUID,
// resolves the workspace path through the host's
// FSProvider scope (when one is installed), and writes
// the new record atomically. A duplicate name returns
// ErrProjectExists.
//
// Workspace resolution: when s.FS is non-nil,
// (*FSProjectStore).Create calls ResolvePolicy(ws) and
// uses the returned canonical path. ErrPathNotAllowed
// surfaces as-is so the dispatcher can map it to -32007.
//
// Concurrency: the read-modify-write (sweep + insert)
// runs under s.mu so two concurrent Creates can't both
// pass the duplicate-name check and both write.
func (s *FSProjectStore) Create(ctx context.Context, p NewProject) (Project, error) {
	_ = ctx
	name, ws, err := s.validateAndResolveProjectCreate(p)
	if err != nil {
		return Project{}, err
	}
	if err := s.ensureRootDir(); err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Duplicate-name sweep: walk the directory and
	// reject the create if any existing record carries
	// the same (trimmed) name.
	if err := s.sweepName(name, ""); err != nil {
		return Project{}, err
	}
	id, err := newProjectID()
	if err != nil {
		return Project{}, err
	}
	now := s.now()
	rec := onDiskProject{
		ID:            id,
		Name:          name,
		Workspace:     ws,
		DefaultModel:  strings.TrimSpace(p.DefaultModel),
		DefaultEffort: strings.TrimSpace(p.DefaultEffort),
		Resources:     nil,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.writeRecord(rec); err != nil {
		return Project{}, err
	}
	return projectFromDisk(rec), nil
}

// validateAndResolveProjectCreate is the
// Create-side parameter-validation chain: trim +
// non-empty + closed-set effort + ResolvePolicy for
// the workspace path. Returns (name, workspace, nil)
// on success; (zero, zero, err) on failure. Extracted
// from Create to keep that handler body under the
// 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func (s *FSProjectStore) validateAndResolveProjectCreate(p NewProject) (string, string, error) {
	name := normaliseProjectName(p.Name)
	ws := normaliseProjectWorkspace(p.Workspace)
	if name == "" {
		return "", "", fmt.Errorf("rc-projects: name is required")
	}
	if ws == "" {
		return "", "", fmt.Errorf("rc-projects: workspace is required")
	}
	if p.DefaultEffort != "" && !IsValidEffort(p.DefaultEffort) {
		return "", "", fmt.Errorf("rc-projects: default_effort must be one of low|medium|high|xhigh|max")
	}
	if s.FS != nil {
		canonical, err := s.FS.ResolvePolicy(ws)
		if err != nil {
			return "", "", err
		}
		ws = canonical
	}
	return name, ws, nil
}

// Get returns the project by id. Unknown id →
// ErrProjectNotFound.
func (s *FSProjectStore) Get(ctx context.Context, id string) (Project, error) {
	_ = ctx
	id = strings.TrimSpace(id)
	if id == "" {
		return Project{}, ErrProjectNotFound
	}
	rec, err := s.readRecord(id)
	if err != nil {
		return Project{}, err
	}
	return projectFromDisk(rec), nil
}

// Update applies the patch to the project identified by
// id. Empty fields in the patch are skipped. A name
// patch that collides with another row (excluding the
// row being patched) returns ErrProjectExists.
// UpdatedAt is stamped with s.now() at the end of a
// successful patch.
func (s *FSProjectStore) Update(ctx context.Context, id string, patch ProjectPatch) (Project, error) {
	_ = ctx
	id = strings.TrimSpace(id)
	if id == "" {
		return Project{}, ErrProjectNotFound
	}
	if patch.DefaultEffort != "" && !IsValidEffort(patch.DefaultEffort) {
		return Project{}, fmt.Errorf("rc-projects: default_effort must be one of low|medium|high|xhigh|max")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.readRecord(id)
	if err != nil {
		return Project{}, err
	}
	if err := s.applyProjectPatchLocked(&rec, id, patch); err != nil {
		return Project{}, err
	}
	rec.UpdatedAt = s.now()
	if err := s.writeRecord(rec); err != nil {
		return Project{}, err
	}
	return projectFromDisk(rec), nil
}

// applyProjectPatchLocked mutates rec in place with
// the patch fields. Caller must hold s.mu and pass
// the matching id (for the sweepName exclude-id
// check). Extracted from Update to keep that handler
// body under the 50-fn ceiling cycle
// rc-filesize-internal-remotecontrol enforces.
func (s *FSProjectStore) applyProjectPatchLocked(rec *onDiskProject, id string, patch ProjectPatch) error {
	// Name patch — only when supplied AND different
	// from the current value. "" means "leave
	// unchanged".
	newName := strings.TrimSpace(patch.Name)
	if newName != "" && newName != rec.Name {
		if err := s.sweepName(newName, id); err != nil {
			return err
		}
		rec.Name = newName
	}
	if patch.DefaultModel != "" {
		rec.DefaultModel = strings.TrimSpace(patch.DefaultModel)
	}
	if patch.DefaultEffort != "" {
		rec.DefaultEffort = strings.TrimSpace(patch.DefaultEffort)
	}
	// Cycle rc-resources: when the patch carries a
	// non-nil Resources pointer, replace-style bind.
	if patch.Resources != nil {
		resources := *patch.Resources
		if resources == nil {
			rec.Resources = []string{}
		} else {
			resourcesCopy := make([]string, len(resources))
			copy(resourcesCopy, resources)
			rec.Resources = resourcesCopy
		}
	}
	return nil
}

// Delete removes the project identified by id. Cycle
// rc-conversation-deletions adds it for
// DeleteRemoteAgentProject.
//
// The method's contract:
//   - missing id (empty / unknown) → ErrProjectNotFound.
//   - failIfBound=true AND any conversation still
//     references the project's workspace →
//     ErrProjectBound (the dispatcher maps it to
//     -32013).
//   - failIfBound=false but bound: the conversation
//     references survive the deletion.
//   - all-clear → os.Remove the project file.
func (s *FSProjectStore) Delete(ctx context.Context, id string, failIfBound bool) error {
	_ = ctx
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrProjectNotFound
	}

	s.mu.Lock()
	rec, err := s.readRecord(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	workspace := rec.Workspace
	hasBound := failIfBound && s.BoundChecker != nil
	s.mu.Unlock()

	if hasBound {
		count, berr := s.BoundChecker(workspace)
		if berr != nil {
			return fmt.Errorf("rc-projects: bound check failed: %w", berr)
		}
		if count > 0 {
			return ErrProjectBound
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-read under the new lock to detect a concurrent
	// delete (TOCTOU); if the file is already gone we map
	// to ErrProjectNotFound so the wire sees a uniform
	// "unknown id" envelope regardless of who won.
	path := s.pathFor(id)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("rc-projects: stat %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("rc-projects: remove %s: %w", path, err)
	}
	return nil
}

// readProjectRecords walks a directory listing of project
// json files, reads + parses each, and returns the slice
// of successfully-decoded records. Per-file read / parse
// failures are skipped silently so a partially-written
// row doesn't poison the whole list (matches the
// "tolerate partial-failure on read" precedent set by
// the antigravity-token + convstore helpers).
//
// Extracted from List to keep that handler under the
// 50-fn ceiling cycle rc-filesize-internal-remotecontrol
// enforces.
func (s *FSProjectStore) readProjectRecords(entries []os.DirEntry) ([]onDiskProject, error) {
	var out []onDiskProject
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := filepath.Join(s.RootDir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			// Tolerate per-file read failures
			// (a partially-written file or a
			// permission flip): skip the row
			// rather than fail the whole list.
			continue
		}
		var rec onDiskProject
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// sweepName walks the project files looking for a
// project whose name matches want. excludeID (the id
// of the row being updated, when non-empty) is
// skipped so an Update that doesn't change the name
// doesn't trip the duplicate check on itself. A
// matching row → ErrProjectExists.
func (s *FSProjectStore) sweepName(want, excludeID string) error {
	entries, err := os.ReadDir(s.RootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if excludeID != "" && id == excludeID {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.RootDir, e.Name()))
		if err != nil {
			continue
		}
		var rec onDiskProject
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		if rec.Name == want {
			return ErrProjectExists
		}
	}
	return nil
}

// readRecord loads an on-disk project by id. Unknown id
// → ErrProjectNotFound. A malformed JSON file also
// surfaces as ErrProjectNotFound.
func (s *FSProjectStore) readRecord(id string) (onDiskProject, error) {
	data, err := os.ReadFile(s.pathFor(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return onDiskProject{}, ErrProjectNotFound
		}
		return onDiskProject{}, err
	}
	var rec onDiskProject
	if err := json.Unmarshal(data, &rec); err != nil {
		return onDiskProject{}, ErrProjectNotFound
	}
	return rec, nil
}

// writeRecord atomically writes the record to disk:
// temp file + rename. Matches the antigravity-token /
// convstore style.
func (s *FSProjectStore) writeRecord(rec onDiskProject) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	final := s.pathFor(rec.ID)
	return atomicfile.WriteFile(final, data, 0o600)
}

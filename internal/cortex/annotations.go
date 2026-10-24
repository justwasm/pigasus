// Package cortex — annotations.go: per-conversation file annotations.
//
// pclntab entries:
//   - NewAnnotationsManager 0x7fc3f20
//   - lockFor               0x7fc4020
//   - GetAnnotations        0x7fc4100
//   - UpdateAnnotations     0x7fc4360
//   - Mutate                0x7fc4660
//   - save                  0x7fc48a0
//   - DeleteAnnotations     0x7fc4d00
package cortex

import (
	"sync"
	"time"
)

// AnnotationsManager is the recovered `(*AnnotationsManager)` from
// google3/third_party/jetski/cortex/cortex.
//
// Models the per-conversation file annotation store (think "user
// starred this file" / "this file has comment X"). The 1.3.5 binary
// persists annotations to a per-trajectory JSON file.
type AnnotationsManager struct {
	mu           sync.Mutex
	trajectoryID string
	store        string                // backing path (cycle 41 doc: lifecycle.go)
	cache        map[string]Annotation // path -> annotation
}

// Annotation is a single annotation entry (file + key/value tag).
type Annotation struct {
	Path     string
	Key      string
	Value    string
	Created  time.Time
	Modified time.Time
}

// GetAnnotations returns the annotations for a path. v1.3.5 binary
// addr 0x7fc4100, name=GetAnnotations, signature=(cm
// *CascadeManager, path string) (Annotation, bool).
func (cm *CascadeManager) GetAnnotations(path string) (Annotation, bool) {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	a, ok := cm.annotations.cache[path]
	return a, ok
}

// UpdateAnnotations updates annotations for a path. v1.3.5 binary
// addr 0x7fc4360, name=UpdateAnnotations, signature=(cm
// *CascadeManager, a Annotation) error.
func (cm *CascadeManager) UpdateAnnotations(a Annotation) error {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	a.Modified = time.Now()
	cm.annotations.cache[a.Path] = a
	return nil
}

// Mutate atomically mutates annotations. v1.3.5 binary addr 0x7fc4660,
// name=Mutate, signature=(cm *CascadeManager, fn func(map[string]Annotation)) error.
func (cm *CascadeManager) Mutate(fn func(map[string]Annotation)) error {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	fn(cm.annotations.cache)
	return nil
}

// DeleteAnnotations deletes annotations for a path. v1.3.5 binary
// addr 0x7fc4d00, name=DeleteAnnotations, signature=(cm
// *CascadeManager, path string) error.
func (cm *CascadeManager) DeleteAnnotations(path string) error {
	cm.annotations.mu.Lock()
	defer cm.annotations.mu.Unlock()
	delete(cm.annotations.cache, path)
	return nil
}

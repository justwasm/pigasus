package referencedrift

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Refresh recomputes every artifact digest in l from the bytes on disk under
// repoRoot, in place. Pair it with WriteLock to rewrite the registry.
//
// It exists so that re-capturing evidence stays a mechanical two-step rather
// than a hand-edited-JSON chore: re-record the fixture, run the gate with
// -update, commit both the fixture and the lock in one reviewable diff. The
// CapturedAt versions are deliberately *not* refreshed — moving an artifact
// onto a newer reference is the decision this gate is meant to make loud, so
// it stays a deliberate edit.
func Refresh(repoRoot string, l *Lock) error {
	for i := range l.Artifacts {
		a := &l.Artifacts[i]
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(a.Path)))
		if err != nil {
			return fmt.Errorf("referencedrift: refresh %s: %w", a.Path, err)
		}
		a.Digest = Digest(b)
	}
	return nil
}

// WriteLock serialises l back over the lock file under repoRoot.
func WriteLock(repoRoot string, l *Lock) error {
	out, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("referencedrift: marshal lock: %w", err)
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(LockPath))
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

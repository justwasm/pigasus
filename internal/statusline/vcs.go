package statusline

import (
	"os"
	"path/filepath"
	"sync"
)

// vcsKinds are the version control systems the reference recognises.
const (
	VCSTypeGit = "git"
	VCSTypeJJ  = "jj"
	VCSTypeHG  = "hg"
)

// vcsCache remembers the last directory that was probed, so the payload
// rebuild (which happens several times a second) does not walk the
// directory tree every time.
type vcsCache struct {
	mu    sync.Mutex
	dir   string
	found *VCSInfo
}

var vcsMemo vcsCache

// DetectVCS reports the version control system that owns dir, or nil when
// dir is not inside a repository.
//
// The reference's payload carries the *type* only — agy 1.4.7 emits
// `{"type":"git"}` and never fills the branch or dirty members, even in a
// dirty working tree. Evidence 【一手】: agy 1.4.7 was run with a capture
// script bound to `statusLine.command` in a dirty git repository and in a
// clean one; both payloads carried `vcs` with `type` and nothing else.
//
// The upstream example script reads `.vcs.branch` and `.vcs.dirty`, so this
// shape is what the script sees: jq's `// ""` and `// false` fallbacks turn
// the missing members into an empty branch and a clean tree.
func DetectVCS(dir string) *VCSInfo {
	if dir == "" {
		return nil
	}
	vcsMemo.mu.Lock()
	if vcsMemo.dir == dir {
		found := vcsMemo.found
		vcsMemo.mu.Unlock()
		return found
	}
	vcsMemo.mu.Unlock()

	found := detectVCSUncached(dir)

	vcsMemo.mu.Lock()
	vcsMemo.dir = dir
	vcsMemo.found = found
	vcsMemo.mu.Unlock()
	return found
}

// detectVCSUncached walks up from dir looking for a repository marker.
func detectVCSUncached(dir string) *VCSInfo {
	current := dir
	for {
		for _, kind := range []string{VCSTypeGit, VCSTypeJJ, VCSTypeHG} {
			marker := filepath.Join(current, "."+kind)
			if info, err := os.Stat(marker); err == nil && info.IsDir() {
				return &VCSInfo{Type: kind}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

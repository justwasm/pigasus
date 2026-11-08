// remote_files.go — the @-mention workspace listing for `--remote-agent`.
//
// The picker normally walks the local disk. A remote-agent session's
// workspace lives on the proxy, so the host supplies a lister backed by
// readDir instead. The first call starts a background walk and returns
// nothing; later calls return the cache, so typing "@" never blocks the
// composer on the network.
package tui

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	atfiles "github.com/justwasm/pigasus/internal/app/prompt/mentions/files"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// remoteFileCache lists one remote workspace and caches the result.
type remoteFileCache struct {
	client *remoteagentclient.Client

	mu      sync.Mutex
	entries []atfiles.Entry
	loaded  bool
	loading bool
}

func newRemoteFileCache(client *remoteagentclient.Client) *remoteFileCache {
	return &remoteFileCache{client: client}
}

// List satisfies mentionpicker.Seams.List.
func (c *remoteFileCache) List(root string) ([]atfiles.Entry, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	c.mu.Lock()
	if c.loaded {
		out := append([]atfiles.Entry(nil), c.entries...)
		c.mu.Unlock()
		return out, nil
	}
	if c.loading {
		c.mu.Unlock()
		return nil, nil
	}
	c.loading = true
	c.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		entries, err := walkRemoteWorkspace(ctx, c.client, root)
		c.mu.Lock()
		if err == nil {
			c.entries = entries
			c.loaded = true
		}
		c.loading = false
		c.mu.Unlock()
	}()
	return nil, nil
}

const (
	remoteWalkMaxEntries = 20000
	remoteWalkMaxDepth   = 24
)

// walkRemoteWorkspace recursively lists root through readDir. It is
// best-effort: a failed subtree is skipped, and the caps stop a runaway
// workspace from turning the dropdown into a network storm.
func walkRemoteWorkspace(ctx context.Context, client *remoteagentclient.Client, root string) ([]atfiles.Entry, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	out := make([]atfiles.Entry, 0, 256)
	var walk func(dir, rel string, depth int) error
	walk = func(dir, rel string, depth int) error {
		if depth > remoteWalkMaxDepth || len(out) >= remoteWalkMaxEntries {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := client.ReadDir(ctx, dir)
		if err != nil {
			return err
		}

		// Add every direct child before descending, so a workspace with a
		// huge subtree still lists its top-level files (go.mod, README, …)
		// instead of burning the cap inside the first directory.
		type childDir struct {
			path string
			rel  string
		}
		dirs := make([]childDir, 0, len(entries))
		for _, e := range entries {
			if e.Name == "" || e.Name == ".DS_Store" {
				continue
			}
			childRel := e.Name
			if rel != "" {
				childRel = rel + "/" + e.Name
			}
			childPath := filepath.Join(dir, e.Name)
			if e.IsDir {
				if skipRemoteDir(e.Name) {
					continue
				}
				out = append(out, atfiles.Entry{Path: childPath, Rel: childRel + "/", IsDir: true})
				dirs = append(dirs, childDir{path: childPath, rel: childRel})
			} else {
				out = append(out, atfiles.Entry{Path: childPath, Rel: childRel})
			}
			if len(out) >= remoteWalkMaxEntries {
				return nil
			}
		}
		for _, d := range dirs {
			if err := walk(d.path, d.rel, depth+1); err != nil {
				return err
			}
			if len(out) >= remoteWalkMaxEntries {
				return nil
			}
		}
		return nil
	}
	if err := walk(root, "", 0); err != nil {
		// The root itself failed (unreachable proxy): report it so the
		// picker retries on the next keystroke. A failure below the root
		// only drops that subtree.
		if rootErr := ctx.Err(); rootErr != nil {
			return nil, rootErr
		}
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// skipRemoteDir mirrors atfiles.List's exclusions.
func skipRemoteDir(name string) bool {
	switch name {
	case ".git", ".crush", "node_modules":
		return true
	}
	return false
}

// inbox.go — the notice inbox the manage_inbox tool reads.
//
// Cycle 379: it lived in internal/app/tools/support.go, whose other four
// contents belonged to the shell and the filesystem. The inbox is a
// *task* concern: the tool that writes to it and the tool that lists
// it are both in this package.

package tasks

import (
	"sync"
)

// ---- inbox -----------------------------------------------------------------

type inboxStore struct {
	mu    sync.Mutex
	items []string
}

var inbox inboxStore

func (i *inboxStore) add(msg string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.items = append(i.items, msg)
}

func (i *inboxStore) list() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string(nil), i.items...)
}

func (i *inboxStore) clear() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	n := len(i.items)
	i.items = nil
	return n
}

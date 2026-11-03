// windowtitle.go — the host's side of the window title.
//
// The store itself is internal/ui/views/statusline's: the title script
// runs on the tick goroutine and the renderer reads the title on the
// model's goroutine, so the value needs its own lock. The host keeps the
// lazily created store and the accessor the status area reads it through.
package cliapp

import (
	"sync"

	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// windowTitleInitMu guards the creation of a runMode's title store.
//
// The store is created with the model, so this lock is only taken on the
// path where a model was built without the constructor (the tests).
var windowTitleInitMu sync.Mutex

// titleStoreFor returns the model's title store, creating it if the model
// was built without one.
func (m *runMode) titleStoreFor() *statusline.TitleStore {
	if m == nil {
		return nil
	}
	windowTitleInitMu.Lock()
	defer windowTitleInitMu.Unlock()
	if m.titleStore == nil {
		m.titleStore = &statusline.TitleStore{}
	}
	return m.titleStore
}

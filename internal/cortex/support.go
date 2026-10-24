// Package cortex — support.go: background runner, presence tracking, and lookup utilities.
//
// pclntab entries:
//   - backgroundRunner.Go            0x7fd6e00
//   - backgroundRunner.CancelAndWait 0x7fd6e60
//   - presenceTracker.count          (inferred)
//   - presenceTracker.remove         (inferred)
//   - providerServerURLMap.get       0x7fec5e0
//   - providerServerURLMap.update    0x7fec480
//   - providerServerURLMap.remove    (inferred)
//   - projectLookupImpl.GetProjectIDForConversationID 0x7feb960
package cortex

import (
	"sync"
	"time"
)

// presenceTracker is the recovered `(*presenceTracker)`.
type presenceTracker struct {
	mu     sync.Mutex
	active map[string]*presenceEntry
}

type presenceEntry struct {
	id       string
	joinedAt time.Time
}

// count returns the number of active presences.
func (p *presenceTracker) count() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.active)
}

// remove removes a presence by id.
func (p *presenceTracker) remove(id string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, id)
}

// backgroundRunner is the recovered `(*backgroundRunner)`.
type backgroundRunner struct {
	mu    sync.Mutex
	tasks []bgTask
}

type bgTask struct {
	Name string
	Stop chan struct{}
	Done chan struct{}
}

// Go runs a function in the background. v1.3.5 binary addr 0x7fd6e00,
// name=Go, signature=(bg *backgroundRunner, name string, fn func()).
func (bg *backgroundRunner) Go(name string, fn func()) {
	bg.mu.Lock()
	task := bgTask{Name: name, Stop: make(chan struct{}), Done: make(chan struct{})}
	bg.tasks = append(bg.tasks, task)
	bg.mu.Unlock()
	go func() {
		defer close(task.Done)
		fn()
	}()
}

// CancelAndWait cancels all background tasks and waits. v1.3.5 binary
// addr 0x7fd6e60, name=CancelAndWait, signature=(bg
// *backgroundRunner) error.
func (bg *backgroundRunner) CancelAndWait() error {
	bg.mu.Lock()
	tasks := append([]bgTask(nil), bg.tasks...)
	bg.mu.Unlock()
	for _, t := range tasks {
		close(t.Stop)
		<-t.Done
	}
	return nil
}

// providerServerURLMap is the recovered `(*providerServerURLMap)`.
type providerServerURLMap struct {
	mu   sync.RWMutex
	urls map[string]string // provider name -> URL
}

// get returns the URL for a provider. v1.3.5 binary addr 0x7fec5e0,
// name=get, signature=(m *providerServerURLMap, provider string) string.
func (m *providerServerURLMap) get(provider string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.urls[provider]
}

// update sets the URL for a provider. v1.3.5 binary addr 0x7fec480,
// name=update, signature=(m *providerServerURLMap, provider string,
// url string).
func (m *providerServerURLMap) update(provider, url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.urls[provider] = url
}

// remove removes a provider. v1.3.5 binary addr (inferred from pclntab),
// name=remove, signature=(m *providerServerURLMap, provider string).
func (m *providerServerURLMap) remove(provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.urls, provider)
}

// projectLookupImpl is the recovered `(*projectLookupImpl)`.
type projectLookupImpl struct {
	mu    sync.Mutex
	cache map[string]string // convID -> projectID
}

// GetProjectIDForConversationID returns the project id for a
// conversation. v1.3.5 binary addr 0x7feb960,
// name=GetProjectIDForConversationID, signature=(p
// *projectLookupImpl, convID string) string.
func (p *projectLookupImpl) GetProjectIDForConversationID(convID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cache[convID]
}

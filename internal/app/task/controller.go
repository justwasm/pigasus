package task

import (
	"sync"
	"sync/atomic"
	"time"
)

// TaskNotification carries information about a completed background task.
type TaskNotification struct {
	TaskID   string
	Title    string
	Body     string
	LogPath  string
	Priority string
	At       time.Time
}

// TaskInbox is the queue of task notifications that have not reached the model yet.
type TaskInbox struct {
	mu    sync.Mutex
	items []TaskNotification
}

// NewTaskInbox returns an empty inbox.
func NewTaskInbox() *TaskInbox {
	return &TaskInbox{}
}

// Add queues one notification.
func (i *TaskInbox) Add(n TaskNotification) {
	if i == nil {
		return
	}
	if n.TaskID == "" && n.Body == "" {
		return
	}
	i.mu.Lock()
	i.items = append(i.items, n)
	i.mu.Unlock()
}

// Len reports how many notifications are waiting.
func (i *TaskInbox) Len() int {
	if i == nil {
		return 0
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.items)
}

// Drain removes the queued notifications and returns them.
func (i *TaskInbox) Drain() []TaskNotification {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.items) == 0 {
		return nil
	}
	out := i.items
	i.items = nil
	return out
}

// Controller encapsulates task registry management, background task subscriptions,
// dirty-flag tracking, and task notifications inbox.
type Controller struct {
	mu sync.RWMutex

	// reg is the background work registry.
	reg *Registry

	// subscribed is true after StartSubscription() is called.
	subscribed bool

	// subCancel unsubscribes from the registry event channel.
	subCancel func()

	// dirty is an atomic flag set when task state transitions occur.
	dirty int32

	// onChange is called when registry transitions are received.
	onChange func()

	// inbox collects notifications from background tasks.
	inbox *TaskInbox
}

// NewController creates a new Controller with the provided task registry.
func NewController(reg *Registry) *Controller {
	if reg == nil {
		reg = New()
	}
	return &Controller{
		reg:   reg,
		inbox: NewTaskInbox(),
	}
}

// Registry returns the underlying task registry.
func (c *Controller) Registry() *Registry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reg
}

// SetRegistry updates the underlying task registry.
func (c *Controller) SetRegistry(reg *Registry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subCancel != nil {
		c.subCancel()
		c.subCancel = nil
		c.subscribed = false
	}
	c.reg = reg
}

// Inbox returns the task inbox queue.
func (c *Controller) Inbox() *TaskInbox {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inbox
}

// StartSubscription subscribes to task registry events and updates the dirty flag.
// It is idempotent; subsequent calls do not start duplicate subscriptions.
func (c *Controller) StartSubscription() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reg == nil || c.subscribed {
		return
	}
	ch, cancel := c.reg.Subscribe()
	c.subCancel = cancel
	c.subscribed = true

	go func() {
		for range ch {
			atomic.StoreInt32(&c.dirty, 1)
			c.mu.RLock()
			fn := c.onChange
			c.mu.RUnlock()
			if fn != nil {
				fn()
			}
		}
	}()
}

// SetOnChange registers a callback invoked when task transitions occur.
func (c *Controller) SetOnChange(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onChange = fn
}

// Subscribed reports whether the controller is actively subscribed.
func (c *Controller) Subscribed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subscribed
}

// ConsumeDirty atomically checks and clears the dirty flag.
func (c *Controller) ConsumeDirty() bool {
	return atomic.SwapInt32(&c.dirty, 0) == 1
}

// MarkDirty explicitly sets the dirty flag.
func (c *Controller) MarkDirty() {
	atomic.StoreInt32(&c.dirty, 1)
}

// Close unsubscribes from registry updates.
func (c *Controller) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subCancel != nil {
		c.subCancel()
		c.subCancel = nil
		c.subscribed = false
	}
}

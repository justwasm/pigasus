package agentbrowser

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// The window the broker picks for one action is decided in this order.
//
// 【反编译】 2026-10-27, `agentbrowser.selectWindowForRootLocked` and
// its reasons: `sender_window_id`, `sender_window_id_grace` and
// `displayed_conversation`. The target window of the request wins over
// all of them — it is the only one the caller named.
const (
	reasonTargetWindow      = "target_window_id"
	reasonSenderWindow      = "sender_window_id"
	reasonSenderWindowGrace = "sender_window_id_grace"
	reasonDisplayed         = "displayed_conversation"
	reasonOpenConversation  = "open_conversation"
	reasonOnlyWindow        = "only_window"
)

// selectWindow picks the window that must run the request.
//
// The reference's own failure is a single message: no window could
// serve the request.
func (b *Broker) selectWindow(req *Request) (*window, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if id := strings.TrimSpace(req.TargetWindowID); id != "" {
		if w, ok := b.windows[id]; ok {
			return w, nil
		}
		return nil, errNoWindows
	}

	root := strings.TrimSpace(req.RootConversationID)
	if root == "" {
		return b.onlyWindow()
	}
	if w, ok := b.senderWindowLocked(root); ok {
		return w, nil
	}
	if w, ok := b.displayedWindowLocked(root); ok {
		return w, nil
	}
	if w, ok := b.openWindowLocked(root); ok {
		return w, nil
	}
	return b.onlyWindow()
}

// onlyWindow returns the connected window when there is exactly one.
func (b *Broker) onlyWindow() (*window, error) {
	if len(b.windows) != 1 {
		return nil, errNoWindows
	}
	for _, w := range b.windows {
		return w, nil
	}
	return nil, errNoWindows
}

// senderWindowLocked returns the window that took the user's last
// message of the conversation.
func (b *Broker) senderWindowLocked(root string) (*window, bool) {
	id, ok := b.senderWindow[root]
	if !ok {
		return nil, false
	}
	w, ok := b.windows[id]
	return w, ok
}

// displayedWindowLocked returns the window that shows the
// conversation.
func (b *Broker) displayedWindowLocked(root string) (*window, bool) {
	for _, w := range b.sortedWindowsLocked() {
		if strings.TrimSpace(w.sub.DisplayedRootConversationID) == root {
			return w, true
		}
	}
	return nil, false
}

// openWindowLocked returns the window that has the conversation open.
func (b *Broker) openWindowLocked(root string) (*window, bool) {
	for _, w := range b.sortedWindowsLocked() {
		for _, id := range w.sub.OpenConversationIDs {
			if strings.TrimSpace(id) == root {
				return w, true
			}
		}
	}
	return nil, false
}

// sortedWindowsLocked returns the connected windows in a stable order,
// so two brokers with the same windows pick the same one.
func (b *Broker) sortedWindowsLocked() []*window {
	out := make([]*window, 0, len(b.windows))
	for _, w := range b.windows {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sub.WindowID < out[j].sub.WindowID })
	return out
}

// requestTimeoutForAction returns how long the broker waits for the
// IDE half to fulfil one action.
//
// 【反编译】 2026-10-27, `agentbrowser.requestTimeoutForAction`: the
// base timeout is 30s. A batch gets the length of its own steps — 20s
// per step, clamped to [20s, 120s] — plus 10s of slack, and never less
// than the base timeout.
func (b *Broker) requestTimeoutForAction(action, paramsJSON string) time.Duration {
	base := b.timeout
	if base <= 0 {
		base = defaultRequestTimeout
	}
	if !strings.EqualFold(strings.TrimSpace(action), "batch") {
		return base
	}
	dur := time.Duration(batchStepCount(paramsJSON)) * batchStepBudget
	if dur < batchMinBudget {
		dur = batchMinBudget
	}
	if dur > batchMaxBudget {
		dur = batchMaxBudget
	}
	if total := dur + batchSlack; total > base {
		return total
	}
	return base
}

// batchStepCount counts the steps of a batch request.
func batchStepCount(paramsJSON string) int {
	if strings.TrimSpace(paramsJSON) == "" {
		return 0
	}
	var payload struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal([]byte(paramsJSON), &payload); err != nil {
		return 0
	}
	return len(payload.Steps)
}

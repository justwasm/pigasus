// Package remotecontrol — convstore_helpers.go:
// per-conversation state lives here so the
// rc-conversation-deletions cycle can wire
// DeleteRemoteAgentMessage +
// DeleteRemoteAgentConversation through a small,
// dispatcher-shaped seam.
//
// Why a separate file (and not an edit to internal/
// convstore/list.go or the dispatcher core):
//
//   - the destructive helpers (DeleteMessage /
//     DeleteConversation) live in convstore's
//     `delete.go` because the constraint there is
//     "sqlite.go stays read-only; anyone can open
//     their own read-write connection as long as it
//     lives outside sqlite.go". add a thin dispatcher
//     seam here so the per-handler bodies in
//     delete_dispatcher.go can stay dispatcher-shaped
//     (a pure read / write API the handler dispatches
//     against) and the convstore internals stay
//     out of the dispatcher's import graph.
//
//   - the in-memory MemConversationStore is the
//     test seam: production wires a
//     FSConversationStore adapter that wraps
//     convstore's public read / write API; tests
//     inject a MemConversationStore so the
//     "delete a single message" / "delete whole
//     conversation" code paths run without an
//     env-managed antigravity home directory.
//
// Cycle rc-conversation-deletions adds the seam;
// later cycles that want richer conversation
// behaviour (e.g. project_id binding in
// conversation_summaries.db) can extend the
// ConversationStore interface without re-touching
// delete_dispatcher.go.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// Conversation is the per-conversation record the
// dispatcher layer reads. It's intentionally narrow:
// only the fields the destructive handlers + the
// List* handler actually inspect (id, title, project
// pointer, message count). Future cycles that need
// more (timestamps, model, etc.) extend this struct
// without breaking the JSON-RPC envelope shape.
type Conversation struct {
	// ID is the conversation id (matches the basename
	// of conversations/<id>.db minus the .db suffix).
	ID string
	// Title is the picker's title field — empty when
	// the conversation has no user step yet.
	Title string
	// ProjectID is the project the conversation is
	// currently bound to, if any. Empty when the
	// conversation's workspace doesn't match a known
	// project's workspace directory. The
	// DeleteRemoteAgentProject's fail_if_bound check
	// uses this field transitively through the
	// ProjectStore's BoundChecker seam (see
	// projects_store.go:FSProjectStore.BoundChecker).
	ProjectID string
	// Workspace is the absolute canonical workspace
	// directory the conversation was recorded
	// against. Empty when the conversation file
	// carries no workspace metadata.
	Workspace string
	// MessageCount is the number of stored steps in
	// the conversation (matches the picker column).
	MessageCount int
}

// ConversationStore is the per-conversation seam the
// destructive JSON-RPC methods in
// delete_dispatcher.go call into.
//
// Five methods:
//
//   - Get(ctx, id) returns the
//     *Conversation record by id; missing id →
//     ErrConversationNotFound. Used to surface
//     "remaining" counts in
//     DeleteRemoteAgentConversation and to enforce
//     the "exactly this conversation" check in
//     DeleteRemoteAgentMessage.
//
//   - DeleteMessage(ctx, convID, messageID) removes
//     one step from the conversation's `steps` table.
//     Returns the post-delete remaining count so the
//     dispatcher can echo it back to the dashboard.
//     messageID matches the on-disk `idx` integer
//     encoded as a decimal string (see
//     convstore.DeleteMessage in
//     internal/convstore/delete.go).
//
//   - DeleteConversation(ctx, convID, purgeAssociated)
//     removes the conversation file outright.
//     purgeAssociated=true also clears any
//     references in conversation_metadata.json and
//     conversation_summaries.db.
//
//   - CountMessages(ctx, convID) returns the number
//     of stored steps in the conversation. Used as a
//     fast read path so the dispatcher doesn't have
//     to round-trip through Get for every
//     DeleteRemoteAgentMessage call.
//
//   - MessageByID(ctx, convID, messageID) reports
//     whether the conversation has a step with the
//     given message id. The dispatcher uses this as
//     the second-step guard inside DeleteMessage's
//     own ErrMessageNotFound path; the underlying
//     convstore already returns ErrMessageNotFound,
//     but the dispatcher layer's envelope map
//     (mapConvError) prefers this helper when it's
//     available so the dispatcher can decide on a
//     per-handler basis whether to surface -32012
//     vs -32011 (the boundary case "conversation
//     exists but message doesn't" lands as -32012,
//     "conversation doesn't exist" as -32011).
//
// Errors that the dispatcher maps to JSON-RPC codes:
//
//	ErrConversationNotFound → -32011
//	ErrMessageNotFound      → -32012
//
// Other errors → -32603 "Internal error". The
// helpers below wrap (or pass through) errors from
// the underlying convstore functions; both helper
// functions and the wrapping code map a nil error
// from `convstore.ErrMessageNotFound` /
// `convstore.ErrConversationNotFound` directly so
// the interface contract is uniform.
type ConversationStore interface {
	Get(ctx context.Context, id string) (Conversation, error)
	DeleteMessage(ctx context.Context, convID, messageID string) (remaining int, err error)
	DeleteConversation(ctx context.Context, convID string, purgeAssociated bool) error
	CountMessages(ctx context.Context, convID string) (int, error)
	MessageByID(ctx context.Context, convID, messageID string) (bool, error)
}

// ErrConversationNotFound is the sentinel
// ConversationStore.Get / DeleteConversation
// returns when the supplied id is unknown. The
// dispatcher translates to JSON-RPC -32011
// "Conversation not found".
//
// ErrMessageNotFound is the sentinel
// ConversationStore.DeleteMessage / MessageByID
// returns when the conversation has no step with
// the given message id. -32012.
//
// Sentinel names match convstore.Err* exported in
// internal/convstore/delete.go; we deliberately
// re-export them here so the dispatcher doesn't
// have to import internal/convstore for the
// sentinel matching.
var (
	ErrConversationNotFound = errors.New("rc-deletions: conversation not found")
	ErrMessageNotFound      = errors.New("rc-deletions: message not found")
)

// MemConversationStore is the test seam for
// ConversationStore. It does not import
// internal/convstore — it stores plain Conversation
// rows in a map, mirroring MemProjectStore's
// "stdlib-only" stance for the dispatcher package's
// test fixtures.
//
// Useful for unit tests that exercise the
// dispatcher's envelope shape (delete message,
// delete conversation, project-bound delete) without
// an env-managed convstore / sqlite layer.
//
// A nil *MemConversationStore is a usable empty
// store (matches MemProjectStore's zero-value
// semantics) so tests that don't need any setup
// can omit construction.
type MemConversationStore struct {
	mu    sync.Mutex
	rows  map[string]Conversation // keyed by ID
	msgs  map[string][]int64      // convID -> list of message indices
	nowMs func() string           // RFC3339Nano-ish clock for any UI timestamps
}

// NewMemConversationStore returns a fresh,
// empty MemConversationStore. nowMs is unused in
// the current cycle (the dispatcher doesn't read
// per-conversation timestamps yet); nil is fine.
func NewMemConversationStore(nowMs func() string) *MemConversationStore {
	return &MemConversationStore{
		rows:  map[string]Conversation{},
		msgs:  map[string][]int64{},
		nowMs: nowMs,
	}
}

// Add stores a single conversation row. The msgs
// slice records the message indices that
// DeleteMessage / MessageByID look up. Tests
// construct a conversation by:
//
//	mcs := NewMemConversationStore(nil)
//	mcs.Add(Conversation{ID: "abc", Title: "...",
//	                       Workspace: "/w",
//	                       MessageCount: 5},
//	         []int64{0, 1, 2, 3, 4})
//
// The Add helper keeps the row's MessageCount in
// sync with len(messageIndices); the explicit
// MessageCount field is normalised here so
// CountMessages can return the list length without
// trusting the caller's value.
func (m *MemConversationStore) Add(row Conversation, msgIndices []int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := strings.TrimSpace(row.ID)
	if id == "" {
		return
	}
	if msgIndices == nil {
		msgIndices = []int64{}
	}
	row.MessageCount = len(msgIndices)
	row.ID = id
	m.rows[id] = row
	m.msgs[id] = append([]int64{}, msgIndices...)
}

// Get returns the stored row by id. Missing id →
// ErrConversationNotFound.
func (m *MemConversationStore) Get(ctx context.Context, id string) (Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return Conversation{}, ErrConversationNotFound
	}
	row, ok := m.rows[id]
	if !ok {
		return Conversation{}, ErrConversationNotFound
	}
	return row, nil
}

// DeleteMessage removes the message with the
// given id (an int64, here parsed from the decimal
// string the dispatcher received) from the store.
// Returns the post-delete remaining count.
// ErrMessageNotFound on:
//
//   - empty convID or messageID;
//   - unknown convID (the dispatcher prefers
//     -32011 for this case —
//     DeleteMessage returns ErrConversationNotFound
//     but mapConvError catches both via the
//     existing sentinel-errors branch in
//     delete_dispatcher.go);
//   - unknown message id.
func (m *MemConversationStore) DeleteMessage(ctx context.Context, convID, messageID string) (int, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	convID = strings.TrimSpace(convID)
	messageID = strings.TrimSpace(messageID)
	if convID == "" || messageID == "" {
		return 0, ErrMessageNotFound
	}
	msgs, ok := m.msgs[convID]
	if !ok {
		return 0, ErrConversationNotFound
	}
	idx, perr := parseDeleteInt64(messageID)
	if perr != nil {
		return 0, ErrMessageNotFound
	}
	out := make([]int64, 0, len(msgs))
	found := false
	for _, x := range msgs {
		if x == idx {
			found = true
			continue
		}
		out = append(out, x)
	}
	if !found {
		return 0, ErrMessageNotFound
	}
	m.msgs[convID] = out
	row := m.rows[convID]
	row.MessageCount = len(out)
	m.rows[convID] = row
	return len(out), nil
}

// DeleteConversation removes the entire row.
// Returns ErrConversationNotFound when the id is
// unknown. purgeAssociated is accepted but the
// in-memory store has no associated state to
// purge, so the parameter is documented here for
// forward parity with the production
// FSConversationStore.
func (m *MemConversationStore) DeleteConversation(ctx context.Context, convID string, purgeAssociated bool) error {
	_ = ctx
	_ = purgeAssociated
	m.mu.Lock()
	defer m.mu.Unlock()
	convID = strings.TrimSpace(convID)
	if convID == "" {
		return ErrConversationNotFound
	}
	if _, ok := m.rows[convID]; !ok {
		return ErrConversationNotFound
	}
	delete(m.rows, convID)
	delete(m.msgs, convID)
	return nil
}

// CountMessages returns the number of messages
// currently stored against convID. Returns 0 for
// unknown ids so the dispatcher can distinguish
// "empty conversation" from "unknown
// conversation" via Get (the user's code path
// always calls Get first when it cares).
func (m *MemConversationStore) CountMessages(ctx context.Context, convID string) (int, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	convID = strings.TrimSpace(convID)
	if convID == "" {
		return 0, nil
	}
	msgs, ok := m.msgs[convID]
	if !ok {
		return 0, nil
	}
	return len(msgs), nil
}

// MessageByID reports whether convID has a step
// with the supplied message id. err == nil with
// (false) for unknown convIDs and malformed
// messageIDs so the dispatcher keeps the same
// contract as the underlying convstore helper.
func (m *MemConversationStore) MessageByID(ctx context.Context, convID, messageID string) (bool, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	convID = strings.TrimSpace(convID)
	messageID = strings.TrimSpace(messageID)
	if convID == "" || messageID == "" {
		return false, nil
	}
	msgs, ok := m.msgs[convID]
	if !ok {
		return false, nil
	}
	idx, perr := parseDeleteInt64(messageID)
	if perr != nil {
		return false, nil
	}
	for _, x := range msgs {
		if x == idx {
			return true, nil
		}
	}
	return false, nil
}

// parseDeleteInt64 parses the decimal-string
// message id the dispatcher received. Mirrors
// convstore.DeleteMessage's contract: anything
// that isn't a uint64 decimal → an error so the
// caller maps to ErrMessageNotFound.
//
// int64 (vs uint64) is intentional: the convstore
// step ids are stored as int64 in the `steps.idx`
// column, and accepting a negative integer would
// silently zero-match any positive-step
// conversation. The helper accepts ≥0 only.
func parseDeleteInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, errors.New("just minus")
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not decimal")
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		return 0, errors.New("negative")
	}
	return n, nil
}

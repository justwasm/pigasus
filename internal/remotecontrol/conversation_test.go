// Tests for the rc-conversation-deletions cycle's
// MemConversationStore fixture + the dispatcher
// entry points for DeleteRemoteAgentMessage /
// DeleteRemoteAgentConversation /
// DeleteRemoteAgentProject.
//
// The MemConversationStore is the test-only seam;
// the production FSConversationStore adapter wraps
// internal/convstore and is exercised separately by
// the convstore_test.go suite in internal/convstore
// (TestDeleteConversation_* / TestDeleteMessage_*
// — see the delete.go side of the seam).
//
// Wire-level round-trips live in
// remotetransport/tcp_test.go (mirroring
// the rc-file-ops / rc-projects split the previous
// cycles established).
package remotecontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// newDeleteDispatcher returns a Dispatcher with the
// supplied seams pre-installed. Mirrors
// newProjectsDispatcher / newFileTestDispatcher
// from the prior cycles.
func newDeleteDispatcher(t *testing.T, projects ProjectStore, conv ConversationStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:       sess,
		Projects:      projects,
		Conversations: conv,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchDelete sends one of the three destructive
// methods through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchProjects
// from projects_test.go.
func dispatchDelete(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	if params != nil {
		var err error
		raw, err = jsonMarshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
	}
	req := jsonRPCFor(1, method, raw)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		return nil, errObj
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result, got %#v", method, resp)
	}
	return res, nil
}

// ---- MemConversationStore in-process tests ----

// TestMemConvStore_DeleteMessage_Ok pins the
// happy path: five messages, delete one, four
// remain, the right one is gone.
func TestMemConvStore_DeleteMessage_Ok(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-1", Title: "first", Workspace: "/ws/foo"},
		[]int64{0, 1, 2, 3, 4})

	remaining, err := mcs.DeleteMessage(context.Background(), "conv-1", "2")
	if err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if remaining != 4 {
		t.Errorf("remaining = %d, want 4", remaining)
	}
	ok, err := mcs.MessageByID(context.Background(), "conv-1", "2")
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	if ok {
		t.Errorf("MessageByID(2) = true after delete, want false")
	}
	for _, want := range []int64{0, 1, 3, 4} {
		ok, err := mcs.MessageByID(context.Background(), "conv-1", itoa(want))
		if err != nil {
			t.Fatalf("MessageByID(%d): %v", want, err)
		}
		if !ok {
			t.Errorf("MessageByID(%d) = false, want true", want)
		}
	}
}

// TestMemConvStore_DeleteMessage_NotFound covers
// the "unknown message id" path: returns
// ErrMessageNotFound so the dispatcher maps to
// -32012.
func TestMemConvStore_DeleteMessage_NotFound(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-2"}, []int64{0, 1})

	_, err := mcs.DeleteMessage(context.Background(), "conv-2", "999")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("err = %v, want ErrMessageNotFound", err)
	}
}

// TestMemConvStore_DeleteMessage_UnknownConv
// covers the "unknown conversation" path: returns
// ErrConversationNotFound so the dispatcher maps to
// -32011.
func TestMemConvStore_DeleteMessage_UnknownConv(t *testing.T) {
	mcs := NewMemConversationStore(nil)

	_, err := mcs.DeleteMessage(context.Background(), "ghost", "0")
	if !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("err = %v, want ErrConversationNotFound", err)
	}
}

// TestMemConvStore_DeleteMessage_MalformedID
// covers the "non-integer message id" path:
// returns ErrMessageNotFound. The on-disk
// MessageByID parser also returns (false, nil)
// for this case; the dispatcher uses the
// returned error code to decide between -32011
// and -32012.
func TestMemConvStore_DeleteMessage_MalformedID(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-3"}, []int64{0, 1})

	_, err := mcs.DeleteMessage(context.Background(), "conv-3", "abc")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("err = %v, want ErrMessageNotFound", err)
	}
}

// TestMemConvStore_DeleteConversation_Ok covers
// the happy path: full conversation delete, the
// row is gone, message list is gone.
func TestMemConvStore_DeleteConversation_Ok(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-4", ProjectID: "proj-x"}, []int64{0, 1, 2})

	if err := mcs.DeleteConversation(context.Background(), "conv-4", false); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}
	if _, err := mcs.Get(context.Background(), "conv-4"); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("Get after delete: err = %v, want ErrConversationNotFound", err)
	}
	n, err := mcs.CountMessages(context.Background(), "conv-4")
	if err != nil {
		t.Fatalf("CountMessages: %v", err)
	}
	if n != 0 {
		t.Errorf("CountMessages after delete = %d, want 0", n)
	}
}

// TestMemConvStore_DeleteConversation_NotFound
// covers the unknown-id → -32011 path.
func TestMemConvStore_DeleteConversation_NotFound(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	err := mcs.DeleteConversation(context.Background(), "ghost", false)
	if !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("err = %v, want ErrConversationNotFound", err)
	}
}

// TestMemConvStore_DeleteConversation_PurgeAssociated
// pins the wired purge_associated=true path:
// in-memory the parameter is accepted but no
// associated state exists to purge, so the
// invariant is "deletion still succeeds and the
// row is gone".
func TestMemConvStore_DeleteConversation_PurgeAssociated(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-5"}, []int64{0, 1})

	if err := mcs.DeleteConversation(context.Background(), "conv-5", true); err != nil {
		t.Fatalf("DeleteConversation purge: %v", err)
	}
	if _, err := mcs.Get(context.Background(), "conv-5"); !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("Get after purge: err = %v, want ErrConversationNotFound", err)
	}
}

// ---- Dispatcher entry points ----

// TestDeleteRemoteAgentMessage_Dispatcher pins
// the in-process envelope: the response carries
// `ok`, `deleted_at`, `remaining`.
func TestDeleteRemoteAgentMessage_Dispatcher(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-6"}, []int64{0, 1, 2, 3, 4})
	d := newDeleteDispatcher(t, nil, mcs)

	res, errObj := dispatchDelete(t, d, "DeleteRemoteAgentMessage", map[string]any{
		"conversation_id": "conv-6",
		"message_id":      "2",
	})
	if errObj != nil {
		t.Fatalf("DeleteRemoteAgentMessage: error = %#v", errObj)
	}
	if res["ok"] != true {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if res["deleted_at"] == "" {
		t.Errorf("deleted_at missing")
	}
	// remaining is set as a Go int in the handler
	// map; the dispatcher passes it through without
	// round-tripping it through json (it's just a
	// map[string]any → returned as-is). So the
	// type is `int`, not `float64` like after a real
	// ws round-trip — accept both.
	rem, ok := remainingAsInt(res["remaining"])
	if !ok {
		t.Errorf("remaining = %T (%v), want int 4", res["remaining"], res["remaining"])
	}
	if rem != 4 {
		t.Errorf("remaining = %d, want 4", rem)
	}
}

// TestDeleteRemoteAgentMessage_UnknownConv pins
// the -32011 envelope.
func TestDeleteRemoteAgentMessage_UnknownConv(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	d := newDeleteDispatcher(t, nil, mcs)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentMessage", map[string]any{
		"conversation_id": "ghost",
		"message_id":      "0",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentMessage: no error, got result")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32011 {
		t.Errorf("error.code = %v, want -32011 (Conversation not found)", code)
	}
}

// TestDeleteRemoteAgentMessage_UnknownMessage pins
// the -32012 envelope.
func TestDeleteRemoteAgentMessage_UnknownMessage(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-7"}, []int64{0, 1})
	d := newDeleteDispatcher(t, nil, mcs)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentMessage", map[string]any{
		"conversation_id": "conv-7",
		"message_id":      "999",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentMessage: no error, got result")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32012 {
		t.Errorf("error.code = %v, want -32012 (Message not found)", code)
	}
}

// TestDeleteRemoteAgentMessage_EmptyParams pins
// the -32602 (Invalid params) envelope for empty
// ids.
func TestDeleteRemoteAgentMessage_EmptyParams(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	d := newDeleteDispatcher(t, nil, mcs)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentMessage", map[string]any{
		"conversation_id": "",
		"message_id":      "0",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentMessage: no error")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32602 {
		t.Errorf("error.code = %v, want -32602 (Invalid params)", code)
	}
}

// TestDeleteRemoteAgentMessage_NoStore pins the
// "no conversation store installed" → -32603 path.
func TestDeleteRemoteAgentMessage_NoStore(t *testing.T) {
	d := newDeleteDispatcher(t, nil, nil)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentMessage", map[string]any{
		"conversation_id": "conv",
		"message_id":      "0",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentMessage: no error")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32603 {
		t.Errorf("error.code = %v, want -32603 (Internal error)", code)
	}
}

// TestDeleteRemoteAgentConversation_Dispatcher pins
// the in-process envelope.
func TestDeleteRemoteAgentConversation_Dispatcher(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	mcs.Add(Conversation{ID: "conv-8"}, []int64{0, 1, 2})
	d := newDeleteDispatcher(t, nil, mcs)

	res, errObj := dispatchDelete(t, d, "DeleteRemoteAgentConversation", map[string]any{
		"conversation_id": "conv-8",
	})
	if errObj != nil {
		t.Fatalf("DeleteRemoteAgentConversation: error = %#v", errObj)
	}
	if res["ok"] != true {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if res["deleted_at"] == "" {
		t.Errorf("deleted_at missing")
	}
}

// TestDeleteRemoteAgentConversation_NotFound pins
// the -32011 envelope.
func TestDeleteRemoteAgentConversation_NotFound(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	d := newDeleteDispatcher(t, nil, mcs)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentConversation", map[string]any{
		"conversation_id": "ghost",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentConversation: no error")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32011 {
		t.Errorf("error.code = %v, want -32011 (Conversation not found)", code)
	}
}

// TestDeleteRemoteAgentConversation_EmptyParams pins
// the -32602 envelope.
func TestDeleteRemoteAgentConversation_EmptyParams(t *testing.T) {
	mcs := NewMemConversationStore(nil)
	d := newDeleteDispatcher(t, nil, mcs)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentConversation", map[string]any{
		"conversation_id": "",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentConversation: no error")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32602 {
		t.Errorf("error.code = %v, want -32602 (Invalid params)", code)
	}
}

// TestDeleteRemoteAgentProject_Dispatcher pins
// the happy-path project delete envelope.
func TestDeleteRemoteAgentProject_Dispatcher(t *testing.T) {
	ps := NewMemProjectStore(nil)
	created, err := ps.Create(context.Background(), NewProject{
		Name:      "alpha",
		Workspace: "/ws/alpha",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	d := newDeleteDispatcher(t, ps, nil)

	res, errObj := dispatchDelete(t, d, "DeleteRemoteAgentProject", map[string]any{
		"id": created.ID,
	})
	if errObj != nil {
		t.Fatalf("DeleteRemoteAgentProject: error = %#v", errObj)
	}
	if res["ok"] != true {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if res["deleted_at"] == "" {
		t.Errorf("deleted_at missing")
	}
}

// TestDeleteRemoteAgentProject_NotFound pins the
// -32004 envelope.
func TestDeleteRemoteAgentProject_NotFound(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newDeleteDispatcher(t, ps, nil)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentProject", map[string]any{
		"id": "ghost",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentProject: no error")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32004 {
		t.Errorf("error.code = %v, want -32004 (Project not found)", code)
	}
}

// TestDeleteRemoteAgentProject_EmptyParams pins
// the -32602 envelope.
func TestDeleteRemoteAgentProject_EmptyParams(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newDeleteDispatcher(t, ps, nil)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentProject", map[string]any{
		"id": "",
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentProject: no error")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32602 {
		t.Errorf("error.code = %v, want -32602 (Invalid params)", code)
	}
}

// TestDeleteRemoteAgentProject_Bound pins the
// -32013 envelope.
func TestDeleteRemoteAgentProject_Bound(t *testing.T) {
	ps := NewMemProjectStore(nil)
	created, err := ps.Create(context.Background(), NewProject{
		Name:      "beta",
		Workspace: "/ws/beta",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	ps.BoundChecker = func(workspace string) (int, error) {
		if strings.TrimSpace(workspace) == "/ws/beta" {
			return 1, nil
		}
		return 0, nil
	}
	d := newDeleteDispatcher(t, ps, nil)

	_, errObj := dispatchDelete(t, d, "DeleteRemoteAgentProject", map[string]any{
		"id": created.ID,
	})
	if errObj == nil {
		t.Fatalf("DeleteRemoteAgentProject: no error")
	}
	if code, ok := errCodeAsInt(errObj["code"]); !ok || code != -32013 {
		t.Errorf("error.code = %v, want -32013 (Project is bound)", code)
	}
}

// TestDeleteRemoteAgentProject_BoundForce pins
// the "fail_if_bound=false" override that
// bypasses the bound check.
func TestDeleteRemoteAgentProject_BoundForce(t *testing.T) {
	ps := NewMemProjectStore(nil)
	created, err := ps.Create(context.Background(), NewProject{
		Name:      "gamma",
		Workspace: "/ws/gamma",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	called := 0
	ps.BoundChecker = func(workspace string) (int, error) {
		called++
		return 1, nil
	}
	d := newDeleteDispatcher(t, ps, nil)

	f := false
	res, errObj := dispatchDelete(t, d, "DeleteRemoteAgentProject", map[string]any{
		"id":            created.ID,
		"fail_if_bound": &f,
	})
	if errObj != nil {
		t.Fatalf("DeleteRemoteAgentProject: error = %#v", errObj)
	}
	if res["ok"] != true {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if called != 0 {
		t.Errorf("BoundChecker called %d times, want 0 (fail_if_bound=false skipped it)", called)
	}
}

// itoa is a small helper for the table-walk loops
// above (avoids pulling strconv into this file's
// already-long import list).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// remainingAsInt normalises the "int / float64 /
// json.Number" outcomes a Go value can take after
// the dispatcher's map[string]any round-trip. The
// handler returns `int` in-process, but a future
// cycle that wires the value through fmt.Sprintf
// or a different encoding could land as either
// int or float64. Accept both shapes for forward
// safety.
func remainingAsInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	}
	return 0, false
}

// errCodeAsInt normalises the JSON-RPC error code,
// which the dispatcher writes as `int` in-process
// and as `float64` after a real json.Marshal →
// Unmarshal round-trip (i.e. ws tests). Both
// shapes are accepted here so the in-process tests
// in this file stay simple while the wire tests in
// tcp_test.go can keep their `float64` casts.
func errCodeAsInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	}
	return 0, false
}

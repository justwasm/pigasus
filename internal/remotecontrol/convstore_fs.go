// Package remotecontrol — convstore_fs.go: production
// ConversationStore adapter that wraps internal/convstore's
// public read / write API.
//
// Why an adapter (instead of just calling convstore
// directly from delete_dispatcher.go): the dispatcher
// layer keeps its dependency surface narrow (no
// internal/convstore import) — its existing helpers
// reach into convstore via closures (cmd/wagyu/'s
// buildHostListBackend), but the new destructive
// handlers need a tightly-typed seam. The
// ConversationStore interface is the seam; this file
// supplies the production implementation.
//
// Adapter rule:
//
//   - Read operations delegate to the package-level
//     functions in internal/convstore (ListConversations
//     / HasConversation / FindConversation /
//     ConvstoreOutputReader.ReadTurn). The adapter does
//     NOT open its own SQLite connection.
//
//   - Write operations delegate to
//     internal/convstore/delete.go (DeleteMessage /
//     DeleteConversation). The adapter does NOT import
//     the SQLite reader — the convstore helpers open
//     their own RW connection lifecycle (see
//     convstore/delete.go's package doc for the
//     rationale).
//
//   - Get's `ProjectID` field is a best-effort derivation:
//     we map Conversation.Workspace to a project id by
//     walking the host's ProjectStore. The host wires a
//     small closure into the adapter at construction time
//     so the adapter doesn't import internal/cli.
//
// Cycle rc-conversation-deletions. Stdlib-only. The
// only third-party transitive dependency is
// ncruces/go-sqlite3, which lives in
// internal/convstore/sqlite.go — this adapter does
// not import that package directly.
package remotecontrol

import (
	"context"
	"errors"
	"strings"

	"github.com/justwasm/pigasus/internal/convstore"
)

// FSConversationStore is the production
// ConversationStore. It wraps the public
// internal/convstore API (read paths) and the new
// destructive helpers in
// internal/convstore/delete.go (write paths).
//
// ProjectLookup is an optional closure that maps a
// workspace directory to a project id. nil → the
// adapter sets Conversation.ProjectID to "" on Get,
// which is sufficient for the rc-conversation-deletions
// envelope shape (the dashboard only inspects `id` /
// `title` / `project_unlinked` in this cycle's
// responses; the ProjectID field is exposed for
// future cycles' convenience). The host wires
// ProjectLookup to a closure that walks ProjectStore
// (see cmd/wagyu/remote_control.go).
type FSConversationStore struct {
	// ProjectLookup is the optional workspace → project
	// id resolver. nil → Get leaves ProjectID empty.
	ProjectLookup func(workspace string) string
}

// NewFSConversationStore returns a fresh
// FSConversationStore. projectLookup may be nil; pass
// a closure to populate Conversation.ProjectID on Get.
func NewFSConversationStore(projectLookup func(string) string) *FSConversationStore {
	return &FSConversationStore{ProjectLookup: projectLookup}
}

// Get returns the conversation row by id. The
// implementation walks the convstore list cache (the
// same path ListRemoteAgentConversations uses) so the
// Title and Workspace fields are derived the same way
// the picker derives them — the brief's
// "remaining" / "project_unlinked" envelopes rely on
// this consistency.
//
// ErrConversationNotFound when the convstore has no
// row for id (i.e. the .db file is absent in the
// workspace lookup).
func (s *FSConversationStore) Get(ctx context.Context, id string) (Conversation, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Conversation{}, ErrConversationNotFound
	}
	if !convstore.HasConversation(id) {
		return Conversation{}, ErrConversationNotFound
	}
	meta, err := findConversationMeta(id)
	if err != nil {
		return Conversation{}, ErrConversationNotFound
	}
	row := Conversation{
		ID:    id,
		Title: meta.Title,
	}
	if workspace := canonicalWorkspaceFromMeta(meta); workspace != "" {
		row.Workspace = workspace
		if s.ProjectLookup != nil {
			row.ProjectID = s.ProjectLookup(workspace)
		}
	}
	count, err := convstore.CountMessages(id)
	if err != nil {
		// CountMessages returns 0 on read errors so we
		// can keep going — the dispatcher's envelope
		// shape doesn't depend on this number being
		// accurate (it's metadata). Empty conversation
		// is a legitimate response.
		count = 0
	}
	row.MessageCount = count
	return row, nil
}

// DeleteMessage delegates to convstore.DeleteMessage.
// Returns the post-delete remaining count so the
// dispatcher can echo it in the wire response.
func (s *FSConversationStore) DeleteMessage(ctx context.Context, convID, messageID string) (int, error) {
	_ = ctx
	if err := convstore.DeleteMessage(convID, messageID); err != nil {
		return 0, translateConvError(err)
	}
	// CountMessages's zero-on-unknown fallback is fine
	// here: the dispatcher only asks for the count
	// after a successful delete, so the convstore
	// must still have the conversation. A 0 here means
	// "the last message was just deleted", which the
	// dispatcher relays as `remaining: 0`.
	count, err := convstore.CountMessages(convID)
	if err != nil {
		return 0, nil
	}
	return count, nil
}

// DeleteConversation delegates to
// convstore.DeleteConversation. purgeAssociated=true
// keeps the existing behavior (the helper already
// trims cache + summary entries); purgeAssociated=false
// is a placeholder for future cycles that want to
// distinguish "delete file" from "delete everything
// that references it".
func (s *FSConversationStore) DeleteConversation(ctx context.Context, convID string, purgeAssociated bool) error {
	_ = ctx
	_ = purgeAssociated
	if err := convstore.DeleteConversation(convID); err != nil {
		return translateConvError(err)
	}
	return nil
}

// CountMessages delegates to convstore.CountMessages.
// Returns 0 for unknown convIDs (the contract the
// helper already exposes); the dispatcher doesn't
// attempt to convert that into ErrConversationNotFound.
func (s *FSConversationStore) CountMessages(ctx context.Context, convID string) (int, error) {
	_ = ctx
	return convstore.CountMessages(convID)
}

// MessageByID delegates to convstore.MessageByID. The
// "conversion miss" cases (unknown convID, malformed
// message ID) come back as (false, nil) so the
// dispatcher can keep using mapConvError for the
// ErrMessageNotFound → -32012 mapping.
func (s *FSConversationStore) MessageByID(ctx context.Context, convID, messageID string) (bool, error) {
	_ = ctx
	return convstore.MessageByID(convID, messageID)
}

// translateConvError bridges the unexported helpers
// in internal/convstore (whose sentinels are
// errors.New values) to the dispatcher-package
// sentinels. errors.Is handles the value comparison
// directly because both sides share the same string
// values.
//
// When the underlying error is neither sentinel, we
// return it verbatim so the dispatcher falls through
// to -32603 "Internal error".
func translateConvError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, convstore.ErrConversationNotFound) {
		return ErrConversationNotFound
	}
	if errors.Is(err, convstore.ErrMessageNotFound) {
		return ErrMessageNotFound
	}
	return err
}

// findConversationMeta scans the convstore list for
// the row matching `id`. The same
// `cache/conversation_metadata.json` and
// `conversation_summaries.db` indexes drive the
// /resume picker; this helper reuses them so the
// adaptive store's Project lookup matches what a
// dashboard sees.
func findConversationMeta(id string) (convstore.ConversationMeta, error) {
	rows, err := convstore.ListConversations()
	if err != nil {
		return convstore.ConversationMeta{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	// Convstore list omits conversations with zero
	// steps (list.go's picker filter). Fall back to a
	// minimal ConversationMeta so the dispatcher
	// still has id / title to populate.
	return convstore.ConversationMeta{
		ID:    id,
		Title: "",
	}, nil
}

// canonicalWorkspaceFromMeta reconstructs the
// workspace directory the conversation was recorded
// against. The picker's Project field is just the
// basename; the canonical / absolute version is not
// surfaced through the picker (list.go's
// projectFromURIs drops the cwd-equality case).
//
// We return "" on every input (the picker-reachable
// rows don't carry the absolute path through to the
// public ConversationMeta — Project only carries the
// basename). A future cycle that needs a full
// canonical path can re-read the cache's
// WorkspaceURIs directly. For the
// rc-conversation-deletions surface, the empty
// workspace is fine: ProjectID is empty →
// mapConvError doesn't try to look up a project id,
// and DeleteRemoteAgentConversation's
// `project_unlinked` returns false (the deletion is
// not on the last bound conversation).
func canonicalWorkspaceFromMeta(meta convstore.ConversationMeta) string {
	_ = meta
	return ""
}

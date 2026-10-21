// Package remotecontrol — projects.go: the project-storage
// types and the ProjectStore interface that the three
// JSON-RPC project methods introduced in cycle
// rc-projects call into.
//
// Why split: the three JSON-RPC methods
// (ListRemoteAgentProjects / CreateRemoteAgentProject /
// UpdateRemoteAgentProject) need a small CRUD-shaped seam
// they can talk to; the dispatcher does NOT know about the
// on-disk format. Putting the types in their own file keeps
// the per-handler bodies in project_dispatcher.go focused
// on the JSON-RPC envelope shape.
//
// Storage shape:
//
//	$ANTIGRAVITY_HOME/projects/<id>.json
//
// The id is a UUID minted by the storage layer at Create
// time; the brief gives no rule about the format, so the
// store uses crypto/rand 16-byte hex (RFC4122 §4.4's
// pseudo-UUID style — not a real UUID but distinguishable
// from a project name).
//
// Cycle rc-projects adds three JSON-RPC methods and one
// storage seam. Future cycle rc-conversation-deletions
// adds DeleteRemoteAgentProject + a cascade-delete of
// conversations bound to a project; the seam is shaped so
// that cycle can extend it without re-touching the
// dispatcher.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Project is the per-record shape the dashboard sees on
// ListRemoteAgentProjects. The fields mirror the
// antigravity wire envelope: id (UUID), name (display),
// workspace (abs canonical path), default_model, default_effort
// (low|medium|high|xhigh|max or ""), created_at / updated_at
// (RFC3339Nano UTC).
//
// The full Project struct is what ProjectStore hands back;
// the per-method handler translates it to the wire
// envelope (cf. project_dispatcher.go's map[string]any
// builders).
//
// Cycle rc-resources adds the Resources field: a
// resource-id list the project is bound to. The dispatcher
// persists the bind via ProjectPatch.Resources (a *[]string
// so Update's "leave unchanged" semantic distinguishes
// "absent" from "explicit empty list"). nil = the bind
// field is absent on the wire / from-disk parse — the
// dashboard calls Update with an explicit slice when it
// wants to manage bindings. The on-disk shape is
// backward-compat (a project record without a Resources
// field parses as nil).
type Project struct {
	// ID is the storage-layer UUID. The dashboard uses
	// this to address a project in subsequent calls
	// (Update, Delete — when Delete lands).
	ID string
	// Name is the human-facing display name. Unique
	// within the store; a second Create with the same
	// name returns ErrProjectExists.
	Name string
	// Workspace is the canonical absolute path the
	// project lives under. The brief calls for the
	// resolver to canonicalise ("/abs canonical") so
	// dash works find the right row when the dashboard
	// sends a relative path.
	Workspace string
	// DefaultModel is the model's id (the same string
	// ListRemoteAgentModels returns). Empty when the
	// project uses the host's resolved default.
	DefaultModel string
	// DefaultEffort is one of DefaultEffortValues or ""
	// (use host default).
	DefaultEffort string
	// Resources is the resource-id list the project
	// is bound to. Cycle rc-resources adds this
	// field; nil means "no bindings stored"
	// (matches the on-disk parse for a project
	// record without a Resources field, so this
	// cycle is backward-compat at the project-file
	// level).
	Resources []string
	// CreatedAt is the file's creation time; UTC.
	CreatedAt time.Time
	// UpdatedAt is the last mutation time; UTC. Set
	// equal to CreatedAt at Create time; bumped by
	// Update.
	UpdatedAt time.Time
}

// NewProject is the input shape for Create. It carries
// only the user-supplied fields; the store mints the ID
// and stamps CreatedAt / UpdatedAt.
type NewProject struct {
	Name          string
	Workspace     string
	DefaultModel  string
	DefaultEffort string
}

// ProjectPatch is the input shape for Update. Each field
// uses "" as "leave unchanged" so the dashboard can patch
// a single field without re-sending the rest. A nil-typed
// pointer would also work but the brief's wire shape uses
// omitted-vs-empty for the same effect, and "" matches the
// json.Unmarshal zero value.
//
// Cycle rc-resources adds the Resources *[]string field:
// nil means "leave unchanged"; a non-nil pointer (even
// pointing at an empty slice) means "set Resources to this
// value". The pointer-form guard distinguishes "field
// absent" from "explicit empty list" — matches the
// rc-conversation-deletions' `fail_if_bound *bool` pattern
// (a key-without-value vs explicit-false distinction).
type ProjectPatch struct {
	Name          string // "" = unchanged
	DefaultModel  string // "" = unchanged
	DefaultEffort string // "" = unchanged
	// Resources is the resource-id list the project
	// is bound to. nil = unchanged; non-nil =
	// replace-style bind. The store copies the slice
	// verbatim (after sorting at the resource-store
	// level).
	Resources *[]string // nil = unchanged
}

// ProjectStore is the seam the JSON-RPC methods call
// into. Cycle rc-projects added List / Create / Update /
// Get; cycle rc-conversation-deletions extends it with a
// Delete method to support DeleteRemoteAgentProject.
//
// List returns the projects whose workspace path starts
// with the supplied prefix; an empty prefix means "all
// projects". The returned slice is ordered by creation time
// ascending (oldest first) so a dashboard that walks the
// slice sees a stable sequence. The total count the
// dispatcher hands to the dashboard is len(list); a
// future cycle that grows to N=thousands-of-projects can
// add a `Cursor` parameter without touching the wire
// envelope the dashboard parses today.
//
// Create validates the inputs (no empty name, no empty
// workspace), generates a UUID, resolves the workspace
// against the host's FSProvider scope (when one is
// installed — see (*FSProjectStore).Create), and returns
// the new Project. A duplicate name returns
// ErrProjectExists.
//
// Update looks the project up by id; unknown id →
// ErrProjectNotFound. The patch is applied field-by-
// field ("" fields are skipped); UpdatedAt is bumped
// to time.Now().UTC(). A name patch that collides with
// an existing row returns ErrProjectExists.
//
// Get returns the project by id; unknown id →
// ErrProjectNotFound. This is the read path the
// dispatcher uses internally between Update and the
// wire envelope.
//
// Delete removes the project identified by id; unknown id
// → ErrProjectNotFound. failIfBound = true returns
// ErrProjectBound when `boundCount > 0`, where boundCount
// is the number of conversations that reference the
// project's workspace. The brief's "fail_if_bound" field
// maps directly to that parameter; production wiring
// passes (true, convstore.ConversationBoundToProject) and
// tests inject a deterministic count.
//
// The bound-check is intentionally narrow: we count by
// workspace-directory match (a conversation is "bound"
// when its cache-recorded WorkspaceURI's basename equals
// the project's Workspace basename). Symlinked workspaces
// resolve through the same EvalSymlinks path the picker
// uses (cycle rc-list-readonly / rc-handle-user-interaction).
// Future cycles that add an explicit project_id column to
// the conversation store can replace this helper with a
// direct SELECT; the dispatcher-side contract stays
// identical.
//
// List returns the projects whose workspace path starts
// with the supplied prefix; an empty prefix means "all
// projects". The returned slice is ordered by creation time
// ascending (oldest first) so a dashboard that walks the
// slice sees a stable sequence. The total count the
// dispatcher hands to the dashboard is len(list); a
// future cycle that grows to N=thousands-of-projects can
// add a `Cursor` parameter without touching the wire
// envelope the dashboard parses today.
//
// Create validates the inputs (no empty name, no empty
// workspace), generates a UUID, resolves the workspace
// against the host's FSProvider scope (when one is
// installed — see (*FSProjectStore).Create), and returns
// the new Project. A duplicate name returns
// ErrProjectExists.
//
// Update looks the project up by id; unknown id →
// ErrProjectNotFound. The patch is applied field-by-
// field ("" fields are skipped); UpdatedAt is bumped
// to time.Now().UTC(). A name patch that collides with
// an existing row returns ErrProjectExists.
//
// Get returns the project by id; unknown id →
// ErrProjectNotFound. This is the read path the
// dispatcher uses internally between Update and the
// wire envelope.
type ProjectStore interface {
	List(ctx context.Context, prefix string, limit int) ([]Project, error)
	Create(ctx context.Context, p NewProject) (Project, error)
	Update(ctx context.Context, id string, patch ProjectPatch) (Project, error)
	Get(ctx context.Context, id string) (Project, error)
	Delete(ctx context.Context, id string, failIfBound bool) error
}

// ErrProjectExists is the sentinel ProjectStore.Create /
// ProjectStore.Update return when the supplied name
// collides with an existing project. The dispatcher
// translates to JSON-RPC -32009 "Project exists".
var ErrProjectExists = errors.New("rc-projects: project exists")

// ErrProjectNotFound is the sentinel ProjectStore.Get /
// Update return when the supplied id is unknown. The
// dispatcher translates to JSON-RPC -32004 "Project
// not found" — the brief explicitly shares this code
// with the cascade-not-found envelope (the cycle-3a
// cascade surface uses -32004 for that case, and the
// project-deletion cycle will inherit it).
var ErrProjectNotFound = errors.New("rc-projects: project not found")

// ErrProjectBound is the sentinel ProjectStore.Delete
// returns when failIfBound is true and the project has
// at least one conversation whose workspace matches
// the project's workspace directory. The dispatcher
// translates to JSON-RPC -32013 "Project is bound".
// Cycle rc-conversation-deletions introduces the code;
// prior cycles parked the destructive method without
// defining the sentinel.
var ErrProjectBound = errors.New("rc-projects: project is bound")

// newProjectID mints a 32-char lowercase hex string. The
// format is the 16-byte crypto/rand output hex-encoded —
// it is not a real UUIDv4 (no version bits), but it is
// 128 bits of entropy, lowercase, and unique with
// overwhelming probability. Tests that need a
// deterministic id inject one through their fixture; the
// production store mints via this helper.
func newProjectID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// normaliseProjectName trims whitespace from the user-
// supplied project name. Used by both the dispatcher
// layer (before calling the store) and the store layer
// (when comparing for duplicates). Returns "" when the
// trimmed result is empty so the same check trips
// everywhere.
func normaliseProjectName(name string) string {
	return strings.TrimSpace(name)
}

// normaliseProjectWorkspace trims whitespace from the
// user-supplied workspace path. Empty after trim →
// "". The canonicalisation (filepath.Clean + Abs) lives
// in the FSProjectStore layer so a memory-only test
// fixture can stay pure.
func normaliseProjectWorkspace(ws string) string {
	return strings.TrimSpace(ws)
}

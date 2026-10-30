// Package rcterminal — the Web UI's terminal surface: one pty per
// terminal, the registry the LanguageServerService handlers call
// into, and the frame shapes the output stream answers with.
//
// 【一手】 2026-10-27, measured against the reference's own
// language_server (a standalone `language_server` on 127.0.0.1:64019,
// `-csrf_token=…`, `-subclient_type=hub`), the Web UI's terminal is
// a *different* contract from the clone's dashboard terminal surface
// (internal/remotecontrol/terminals.go):
//
//	the LanguageServerService methods are CreateTerminal,
//	ListTerminals, SendTerminalInput, StreamTerminalOutput and
//	CloseTerminal; the messages are the reference's
//	exa.language_server_pb ones.
//
// The reference's own answers, verbatim:
//
//	CreateTerminal {"workspaceUri":"file:///tmp/web-ws",
//	                "conversationId":"conv-1","projectId":"proj-1"}
//	  → 200 {"terminal":{"terminalId":"cab3427e-…","pid":95383,
//	                     "title":"bash","conversationId":"conv-1",
//	                     "projectId":"proj-1",
//	                     "workspaceUri":"file:///private/tmp/web-ws",
//	                     "createdAt":"2026-10-26T18:18:53.197975Z"}}
//
//	ListTerminals {"conversationId":…,"projectId":…,"workspaceUri":…}
//	  → 200 {"terminals":[{"terminalId":…,"pid":…,"title":"bash",…}]}
//
//	SendTerminalInput {"terminalId":…,"inputStreamId":"s-1",
//	                   "inputOffset":"0","input":"<base64>"}
//	  → 200 {"ackedOffset":"14"}
//
//	CloseTerminal {"terminalId":…}
//	  → 200 {}
//
// The terminal is a *live shell*: the reference spawns `$SHELL -l`
// in the workspace (the process table shows
// `/opt/homebrew/bin/bash -l` under the language_server) and the
// output stream carries its bytes.
//
// The reference's answers are also the shape rules this package
// pins:
//
//   - The workspace URI is canonicalised: `file:///tmp/web-ws` comes
//     back as `file:///private/tmp/web-ws` (macOS resolves /tmp to
//     /private/tmp).
//   - `splitFromTerminalId` joins the new terminal to the source's
//     split group. The group id is `split-<the terminal that heads
//     the group>`; the head terminal only learns its own group id
//     once a split is created from it.
//   - `ListTerminals` filters on every field it is given; an empty
//     field filters on nothing, so `{}` lists every terminal.
//   - `SendTerminalInput` acknowledges a byte offset per input
//     stream: a chunk whose offset is not the current one is
//     dropped, and the answer keeps the offset reached so far. The
//     answer omits `ackedOffset` when it is zero (proto-JSON omits
//     zero-valued fields), so a pure resize answers `{}`.
//   - A closed terminal is gone: closing it twice answers
//     `not_found`.
//
// Stdlib + creack/pty (the same module internal/tools uses for
// run_command's pty).
package rcterminal

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// NotFoundError is the reference's `not_found` for a terminal id
// that names no live terminal.
//
// 【一手】 2026-10-27, the reference's own answer:
//
//	404 {"code":"not_found",
//	     "message":"not_found: terminal 44e46970-… not found (error ID: …)"}
//
// The error id is the reference's per-call trace id; the clone has
// no such trace, so the message keeps the rest of the sentence.
type NotFoundError struct {
	// TerminalID is the id that named no terminal.
	TerminalID string
}

// Error implements error.
func (e *NotFoundError) Error() string {
	return fmt.Sprintf("not_found: terminal %s not found", e.TerminalID)
}

// ErrClosed is returned when a terminal's shell has already exited.
var ErrClosed = errors.New("terminal closed")

// Info is exa.language_server_pb.TerminalInfo — the reference's own
// message, field for field.
//
// The json names are the proto-JSON names; the empty fields are
// omitted, exactly as proto-JSON omits them.
type Info struct {
	// TerminalID is the terminal's uuid.
	TerminalID string `json:"terminalId"`
	// PID is the shell's process id.
	PID int `json:"pid"`
	// Title is the shell's name ("bash").
	Title string `json:"title"`
	// ConversationID is the conversation the terminal belongs to.
	ConversationID string `json:"conversationId"`
	// ProjectID is the project the terminal belongs to.
	ProjectID string `json:"projectId"`
	// Cwd is the terminal's working directory. Empty until the
	// shell reports one (OSC 7).
	Cwd string `json:"cwd,omitempty"`
	// SplitGroupID groups a split terminal with its siblings.
	SplitGroupID string `json:"splitGroupId,omitempty"`
	// WorkspaceURI is the canonical file URI of the workspace.
	WorkspaceURI string `json:"workspaceUri,omitempty"`
	// CreatedAt is the RFC 3339 timestamp of the terminal's birth.
	CreatedAt string `json:"createdAt,omitempty"`
}

// FrameKind names the oneof member a frame carries — the reference's
// StreamTerminalOutputResponse.event.
type FrameKind int

const (
	// FrameOutput carries terminal bytes.
	FrameOutput FrameKind = iota
	// FrameTitle carries the shell's title.
	FrameTitle
	// FrameCwd carries the shell's working directory.
	FrameCwd
	// FrameExitCode carries the shell's exit code.
	FrameExitCode
)

// Frame is one message of the terminal output stream.
//
// The reference's message is a oneof (`event`) with the members
// output / title / cwd / exit_code, plus a separate `is_scrollback`
// flag. This package keeps the same split: Kind names the member,
// IsScrollback marks the replay frames.
type Frame struct {
	// Kind names the oneof member the frame carries.
	Kind FrameKind
	// Output is the payload of a FrameOutput frame.
	Output []byte
	// Title is the payload of a FrameTitle frame.
	Title string
	// Cwd is the payload of a FrameCwd frame.
	Cwd string
	// ExitCode is the payload of a FrameExitCode frame.
	ExitCode int
	// IsScrollback marks a frame as replay rather than live output.
	IsScrollback bool
}

// CreateRequest is the reference's CreateTerminalRequest.
type CreateRequest struct {
	// WorkspaceURI is the file URI of the terminal's directory.
	WorkspaceURI string
	// ConversationID is the conversation the terminal belongs to.
	ConversationID string
	// ProjectID is the project the terminal belongs to.
	ProjectID string
	// SplitFromTerminalID names the terminal this one splits off
	// from. Empty creates a standalone terminal.
	SplitFromTerminalID string
	// Workspace is the directory used when WorkspaceURI names
	// nothing. Empty means the registry's fallback — the user's home
	// directory, never the server's working directory.
	Workspace string
}

// Registry owns the live terminals.
//
// The zero value is not usable; call NewRegistry.
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// workspace is the fallback directory for a terminal whose
	// request names no workspace.
	workspace string
}

// NewRegistry returns an empty registry. The fallback workspace is
// the user's home directory — see homeDir.
func NewRegistry() *Registry {
	return &Registry{sessions: map[string]*Session{}, workspace: homeDir()}
}

// SetWorkspace sets the directory a terminal uses when its request
// names no workspace URI.
func (r *Registry) SetWorkspace(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspace = dir
}

// Create spawns a terminal and returns its description.
//
// 【一手】 2026-10-27, the reference's own answers:
//
//	splitFromTerminalId names a live terminal → the new terminal
//	joins that terminal's split group, and the group id is
//	`split-<the terminal that heads the group>`:
//
//	  {"terminal":{"terminalId":"a3d0364f-…","splitGroupId":"split-44e46970-…",…}}
//
//	…while the head terminal only learns its own group id once a
//	split is created from it (it had none at birth, and the list
//	answer shows `split-44e46970-…` afterwards).
//
//	an unknown splitFromTerminalId is ignored: the new terminal has
//	no group.
func (r *Registry) Create(req CreateRequest) (*Info, error) {
	dir := r.workspace
	if resolved := workspaceDir(req.WorkspaceURI); resolved != "" {
		dir = resolved
	} else if req.Workspace != "" {
		dir = req.Workspace
	}
	if dir == "" {
		dir = homeDir()
	}
	_ = os.MkdirAll(dir, 0o755)
	session, err := spawn(dir)
	if err != nil {
		return nil, err
	}
	session.conversationID = req.ConversationID
	session.projectID = req.ProjectID
	session.workspace = workspaceURI(dir)

	r.mu.Lock()
	if src, ok := r.sessions[req.SplitFromTerminalID]; ok {
		group := src.splitGroupID
		if group == "" {
			group = splitGroupID(src.id)
			src.setSplitGroup(group)
		}
		session.splitGroupID = group
	}
	r.sessions[session.id] = session
	r.mu.Unlock()

	session.start()
	return session.info(), nil
}

// List returns the live terminals that match the filter.
//
// 【一手】 2026-10-27: every field the request carries filters; an
// empty field filters on nothing, so `{}` lists every terminal.
func (r *Registry) List(conversationID, projectID, workspaceURI string) []Info {
	wantWorkspace := canonicalWorkspaceURI(workspaceURI)
	r.mu.Lock()
	out := make([]Info, 0, len(r.sessions))
	for _, s := range r.sessions {
		if conversationID != "" && s.conversationID != conversationID {
			continue
		}
		if projectID != "" && s.projectID != projectID {
			continue
		}
		if wantWorkspace != "" && s.workspace != wantWorkspace {
			continue
		}
		out = append(out, *s.info())
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// Info returns the description of one terminal.
func (r *Registry) Info(terminalID string) (*Info, error) {
	r.mu.Lock()
	s := r.sessions[terminalID]
	r.mu.Unlock()
	if s == nil {
		return nil, &NotFoundError{TerminalID: terminalID}
	}
	return s.info(), nil
}

// SendInput writes bytes to a terminal's shell.
//
// The acknowledgement is a byte offset within one input stream: the
// reference accepts a chunk only when its offset is the offset it
// has reached so far, and a chunk that does not line up is dropped
// (the Web UI resends from the acknowledged offset).
//
// 【一手】 2026-10-27, the reference's own answers:
//
//	{"inputStreamId":"s","inputOffset":"0","input":"<8 bytes>"}
//	  → 200 {"ackedOffset":"8"}
//	{"inputStreamId":"s","inputOffset":"10","input":"<7 bytes>"}
//	  → 200 {"ackedOffset":"8"}      ← dropped: the offset skipped 8
func (r *Registry) SendInput(terminalID, streamID string, offset int64, data []byte) (int64, error) {
	r.mu.Lock()
	s := r.sessions[terminalID]
	r.mu.Unlock()
	if s == nil {
		return 0, &NotFoundError{TerminalID: terminalID}
	}
	return s.sendInput(streamID, offset, data)
}

// Resize changes a terminal's window size.
func (r *Registry) Resize(terminalID string, cols, rows int) error {
	r.mu.Lock()
	s := r.sessions[terminalID]
	r.mu.Unlock()
	if s == nil {
		return &NotFoundError{TerminalID: terminalID}
	}
	return s.resize(cols, rows)
}

// Close ends a terminal.
//
// The reference forgets a closed terminal: closing it again answers
// `not_found`.
func (r *Registry) Close(terminalID string, force bool) error {
	r.mu.Lock()
	s := r.sessions[terminalID]
	if s != nil {
		delete(r.sessions, terminalID)
	}
	r.mu.Unlock()
	if s == nil {
		return &NotFoundError{TerminalID: terminalID}
	}
	s.close(force)
	return nil
}

// Stream serves a terminal's output until the terminal ends or emit
// fails.
//
// The stream opens with the scrollback (the replay frames the
// reference marks `isScrollback`) and then carries the live output.
func (r *Registry) Stream(terminalID string, emit func(Frame) error) error {
	r.mu.Lock()
	s := r.sessions[terminalID]
	r.mu.Unlock()
	if s == nil {
		return &NotFoundError{TerminalID: terminalID}
	}
	return s.stream(emit)
}

// splitGroupID is the reference's group id: the id of the terminal
// that heads the group, prefixed with "split-".
func splitGroupID(rootID string) string {
	return "split-" + rootID
}

// workspaceDir turns a file URI into a directory path, canonicalised
// the way the reference canonicalises it.
//
// 【一手】 2026-10-27: the reference answers `file:///private/tmp/web-ws`
// for a request that said `file:///tmp/web-ws` — macOS resolves
// /tmp to /private/tmp.
func workspaceDir(uri string) string {
	trimmed := strings.TrimSpace(uri)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "file://") {
		return ""
	}
	path := strings.TrimPrefix(trimmed, "file://")
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	if path == "" {
		return ""
	}
	return canonicalDir(path)
}

// workspaceURI is the inverse of workspaceDir.
func workspaceURI(dir string) string {
	if dir == "" {
		return ""
	}
	return "file://" + canonicalDir(dir)
}

// canonicalWorkspaceURI canonicalises a workspace URI for the list
// filter. An empty URI filters on nothing.
func canonicalWorkspaceURI(uri string) string {
	dir := workspaceDir(uri)
	if dir == "" {
		return ""
	}
	return workspaceURI(dir)
}

// homeDir is the directory a terminal starts in when its request
// names no workspace.
//
// 【一手】 2026-10-27, the reference's own process table: the
// Antigravity IDE's language_server runs with the working directory
// "/" while the shell it spawns for a terminal runs in "/Users/gear"
// — the user's home. The server's working directory is not the
// terminal's directory, so a terminal that names no workspace starts
// in the home.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return "/"
}

// canonicalDir resolves a path the way the filesystem sees it: the
// symlinks resolved, the path absolute.
func canonicalDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

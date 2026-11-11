// Package linecomments holds the line-anchored comments the reference's
// interactive file viewer carries: the comment model, the gutter marker,
// the wire format the comments travel in and the "Unsent Comments"
// screen.
//
// # What it is
//
// The reference's viewer lets the user leave a note on a line of the
// file it shows (official-docs/cli/commands/codesearch.md:80-88). The
// note is marked in the gutter with a 💬, and when the user leaves the
// viewer the CLI asks whether the pending notes are sent to the agent
// (official-docs/cli/commands/codesearch.md:96-100).
//
// # Where it lives
//
// The package is shared by construction: the reference has one comment
// screen and one wire format, so the model, the marker, the wire format
// and the screen's three answers live here, once, and every viewer that
// carries line-anchored comments answers through them.
//
// The /codesearch viewer (internal/ui/views/codesearchpanel) is the
// first consumer. The /diff viewer's comment flow is not built yet, so
// the package has one caller today.
//
// The panel that shows the file owns the store: the store is the
// model's owner, and the panel asks it what a line carries.
package linecomments

import (
	"fmt"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/ui/foundations/textbuffer"
	"github.com/justwasm/pigasus/vim"
)

// Marker is the reference's gutter marker for a line that carries a
// comment.
//
// The doc names the icon: "A saved comment is stored against that line
// and marked with a 💬 icon in the gutter"
// (official-docs/cli/commands/codesearch.md:86).
const Marker = "💬"

// Comment is one comment, anchored to a line of a file.
type Comment struct {
	// Line is the line the comment is anchored to, counted from one.
	Line int
	// Body is the comment's text.
	Body string
}

// Editor represents a vim-capable comment editor instance.
type Editor struct {
	VimEnabled bool
	VimState   *vim.State
	Buffer     textbuffer.Buffer
	Active     bool
	Line       int
}

// NewEditor constructs a comment editor optionally configured with vim modal editing.
func NewEditor(vimEnabled bool) *Editor {
	ed := &Editor{
		VimEnabled: vimEnabled,
	}
	if vimEnabled {
		ed.VimState = vim.New("")
	}
	return ed
}

// Open activates the editor on line with the given initial text.
func (e *Editor) Open(line int, initialText string) {
	e.Active = true
	e.Line = line
	e.Buffer.Reset()
	e.Buffer.SetValue(initialText)
	if e.VimEnabled {
		e.VimState = vim.New(initialText)
		e.VimState.Mode = vim.Insert
	}
}

// Close deactivates the editor and clears the buffer.
func (e *Editor) Close() {
	e.Active = false
	e.Buffer.Reset()
}

// The store is the model's owner: it decides what the comments are and
// in which order they are reported. The lines are kept in line order,
// the order the reference's screen shows them in.
type Store struct {
	comments map[int]Comment
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{comments: map[int]Comment{}}
}

// Add stores a comment against a line, replacing the one the line
// carried.
//
// The reference's binding is "Add/edit comment on the current line"
// (official-docs/cli/commands/diff.md:126), so a second comment on the
// same line edits the first.
func (s *Store) Add(line int, body string) {
	if s == nil {
		return
	}
	if s.comments == nil {
		s.comments = map[int]Comment{}
	}
	s.comments[line] = Comment{Line: line, Body: body}
}

// Delete drops the comment a line carries and reports whether there was
// one.
func (s *Store) Delete(line int) bool {
	if s == nil || s.comments == nil {
		return false
	}
	if _, ok := s.comments[line]; !ok {
		return false
	}
	delete(s.comments, line)
	return true
}

// At returns the comment a line carries.
func (s *Store) At(line int) (Comment, bool) {
	if s == nil || s.comments == nil {
		return Comment{}, false
	}
	c, ok := s.comments[line]
	return c, ok
}

// All returns every comment, in line order.
func (s *Store) All() []Comment {
	if s == nil || len(s.comments) == 0 {
		return nil
	}
	out := make([]Comment, 0, len(s.comments))
	for _, c := range s.comments {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// Len is the number of comments the store holds.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	return len(s.comments)
}

// Reset drops every comment.
func (s *Store) Reset() {
	if s == nil {
		return
	}
	s.comments = map[int]Comment{}
}

// Format renders one comment in the reference's wire format.
//
// 【strings】 the format is "<file>%s:%d:%s\n": the literal tag
// "<file>", the file, the line and the comment's body. The constant
// occurs exactly once in the whole 1.4.11 binary — at 0x10359ebb2,
// fifteen bytes long, immediately before the "Unsent Comments" header —
// and it is the one the reference's own comment formatter uses
// (【反编译】 `(*DiffModel).formatDiffComments` materialises it with the
// length 15 and hands it to fmt.Sprintf with three arguments: the file,
// the line and the body).
//
// The doc spells the same shape "<file>:<line>: <comment>"
// (official-docs/cli/commands/codesearch.md:95). The doc's schematic
// drops the file's own name and puts a space after the line's colon;
// the binary's format has neither. The binary wins — the comment is a
// wire format, and the model parses it.
func Format(file string, c Comment) string {
	return fmt.Sprintf("<file>%s:%d:%s\n", file, c.Line, c.Body)
}

// FormatAll renders the comments in the reference's wire format, one
// comment per line.
//
// This is the text the agent receives as its next message.
func FormatAll(file string, cs []Comment) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(Format(file, c))
	}
	return b.String()
}

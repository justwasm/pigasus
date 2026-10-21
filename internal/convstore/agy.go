package convstore

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Step type ids, recovered from the reference's own conversation databases.
//
// The numbering is Google's internal `StepType` enum. Only the values that
// appear in a stored trajectory are listed; everything else is passed through
// as an opaque step so the transcript order stays intact.
const (
	StepUserInput     = 14
	StepModelResponse = 15
	StepError         = 17
	StepSubagent      = 23
	StepSystemNotice  = 101
	StepToolCall      = 132
)

// Step is one entry of a conversation as the reference stores it.
type Step struct {
	Idx    int64
	Type   int64
	Status int64
	// Text holds the user prompt, the model's response, an error message or a
	// system notice, depending on Type.
	Text string
	// ToolName and ToolArgs are set for StepToolCall. ToolArgs is the raw JSON
	// the model produced, e.g. {"CommandLine":"ls","Cwd":"/tmp"}.
	ToolName string
	ToolArgs string
	// ToolResult holds the captured tool-call output for StepToolCall. It is
	// the text inside step_payload.field_140.field_2.field_1 — recovered
	// by inspecting real convstore databases; pre-cycle-163 the clone ignored
	// this field, so an expanded loaded step (ctrl+o) showed its title only.
	ToolResult string
}

// Role classifies a step for rendering.
func (s Step) Role() string {
	switch s.Type {
	case StepUserInput:
		return "user"
	case StepModelResponse:
		return "model"
	case StepToolCall:
		return "tool"
	case StepError:
		return "error"
	case StepSystemNotice:
		return "system"
	case StepSubagent:
		return "subagent"
	}
	return "other"
}

// Conversation is a loaded reference conversation.
type Conversation struct {
	// Path is the database the conversation came from.
	Path string
	// ID is the conversation (cascade) id recorded in the database itself.
	ID string
	// TrajectoryID is the trajectory that owns the steps.
	TrajectoryID string
	// Project is the workspace directory name the conversation ran in,
	// e.g. "openagy". It comes from the `file://<path>` workspace URI
	// inside trajectory_metadata_blob and is the right-hand column of
	// the v1.4.0 /resume picker. Empty when the blob carries no URI.
	Project string
	// Steps is every stored step, in index order.
	Steps []Step
}

// projectFromBlob recovers the workspace directory name from a
// trajectory_metadata_blob protobuf.
//
// The blob is a length-delimited protobuf; the workspace rides in a
// `file://<path>` URI string field. Verified against a real store:
//
//	...:+file:///Users/gear/Documents/GitHub/openagyz...
//	      ^ 0x2b = 43 = len("file:///Users/gear/Documents/GitHub/openagy")
//
// The byte immediately before the URI is its varint length, so the path
// is read by length rather than by scanning for a delimiter (paths and
// the following field tags are both printable, so scanning would
// over-read — e.g. the `z` above is the next field's tag).
//
// Returns "" when the blob has no URI, or when the length byte is not a
// single-byte varint (a path long enough for a multi-byte length is not
// something the reference writes).
func projectFromBlob(data []byte) string {
	const marker = "file://"
	idx := bytes.Index(data, []byte(marker))
	if idx <= 0 {
		return ""
	}
	n := int(data[idx-1])
	if n < len(marker) || idx-1+n > len(data) {
		return ""
	}
	uri := string(data[idx-1 : idx-1+n])
	if !strings.HasPrefix(uri, marker) {
		return ""
	}
	path := strings.TrimSuffix(strings.TrimPrefix(uri, marker), "/")
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// LoadConversation reads a reference conversation database. A zero-byte or
// non-SQLite file — which the reference itself accepts as "the conversation
// exists" — yields an empty conversation rather than an error, so
// --conversation keeps working against half-written stores.
func LoadConversation(path string) (*Conversation, error) {
	db, err := Open(path)
	if err != nil {
		if isUnreadableStore(err) {
			return &Conversation{Path: path}, nil
		}
		return nil, err
	}
	defer db.Close()

	c := &Conversation{Path: path}
	_ = db.Scan("trajectory_meta", func(r *Row) error {
		if c.TrajectoryID == "" {
			c.TrajectoryID = r.Text("trajectory_id")
		}
		if c.ID == "" {
			c.ID = r.Text("cascade_id")
		}
		return nil
	})
	// Cycle 184: the workspace URI lives in trajectory_metadata_blob.
	// Only the first row is read — the reference writes a single
	// `main` row and it is the workspace the conversation started in.
	_ = db.Scan("trajectory_metadata_blob", func(r *Row) error {
		if c.Project == "" {
			c.Project = projectFromBlob(r.Bytes("data"))
		}
		return nil
	})
	if err := db.Scan("steps", func(r *Row) error {
		s := Step{Idx: r.Int("idx"), Type: r.Int("step_type"), Status: r.Int("status")}
		parseStepPayload(&s, r.Bytes("step_payload"))
		c.Steps = append(c.Steps, s)
		return nil
	}); err != nil {
		return nil, err
	}
	// Cycle 143: SQLite's rowid walks in insertion order, but the
	// `steps` table is written incrementally and the rowid column
	// can be re-used after deletions (vacuumed pages, restarted
	// sessions). Without sorting, the conversation's user/model
	// alternation comes out scrambled — the harness diff shows
	// ref vs ours reading the same DB file but seeing completely
	// different transcripts. Sort by Idx so the timeline is
	// monotonically increasing.
	sort.SliceStable(c.Steps, func(i, j int) bool {
		return c.Steps[i].Idx < c.Steps[j].Idx
	})
	return c, nil
}

func isUnreadableStore(err error) bool {
	if os.IsNotExist(err) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "is not a SQLite 3 database") ||
		strings.Contains(msg, "read header") ||
		strings.Contains(msg, "table \"trajectory_meta\" not found")
}

// parseStepPayload fills s from the raw step_payload blob.
//
// The shapes, all verified against real databases:
//
//	14  user input      19 -> 3 -> 1  (text; 3 is the text itself when shallow)
//	15  model response  20 -> 1       (text)
//	132 tool call        5 -> 4 -> {2: tool name, 3: JSON arguments}
//	17  error           24 -> 3 -> {1: short message, 2: long message}
//	101 system notice   24 -> 3 -> {1: message}
func parseStepPayload(s *Step, payload []byte) {
	if len(payload) == 0 {
		return
	}
	top := protoFields(payload)
	switch s.Type {
	case StepUserInput:
		p19 := protoBytes(top, 19)
		if p19 == nil {
			return
		}
		p3 := protoBytes(protoFields(p19), 3)
		if p3 == nil {
			return
		}
		if t1 := protoBytes(protoFields(p3), 1); t1 != nil {
			s.Text = string(t1)
		} else {
			s.Text = string(p3)
		}
	case StepModelResponse:
		if p20 := protoBytes(top, 20); p20 != nil {
			s.Text = protoText(protoFields(p20), 1)
		}
	case StepToolCall:
		p5 := protoBytes(top, 5)
		if p5 == nil {
			return
		}
		p4 := protoBytes(protoFields(p5), 4)
		if p4 == nil {
			return
		}
		f4 := protoFields(p4)
		s.ToolName = protoText(f4, 2)
		s.ToolArgs = protoText(f4, 3)
		// Cycle 163: recover the tool-call output text so an expanded
		// loaded step (ctrl+o) actually shows the result. The output
		// lives at step_payload.field_140.field_2.field_1 — verified
		// against real convstore DBs; the byte trail is:
		//   field 140 = "tool result wrapper"
		//     field 1 = tool param (repeated: toolSummary, CommandLine, ...)
		//     field 2 = "tool result content wrapper"
		//       field 1 = result text (e.g. "The command exited with code 0.\nOutput:\n...")
		if p140 := protoBytes(top, 140); p140 != nil {
			f140 := protoFields(p140)
			if p2 := protoText(f140, 2); p2 != "" {
				if txt := protoText(protoFields([]byte(p2)), 1); txt != "" {
					s.ToolResult = cleanToolResult(txt)
				} else {
					s.ToolResult = cleanToolResult(p2)
				}
			}
		}
	case StepError, StepSystemNotice:
		p24 := protoBytes(top, 24)
		if p24 == nil {
			return
		}
		p3 := protoBytes(protoFields(p24), 3)
		if p3 == nil {
			return
		}
		f3 := protoFields(p3)
		s.Text = protoText(f3, 1)
		if s.Text == "" {
			s.Text = protoText(f3, 2)
		}
	}
}

// Transcript returns the steps that appear in the conversation view, in order.
// Opaque steps (timers, subagent bookkeeping, empty generations) are dropped so
// a resumed transcript is not mostly blanks.
func (c *Conversation) Transcript() []Step {
	out := make([]Step, 0, len(c.Steps))
	for _, s := range c.Steps {
		switch s.Role() {
		case "user", "model", "error", "system":
			if strings.TrimSpace(s.Text) == "" {
				continue
			}
		case "tool":
			if strings.TrimSpace(s.ToolName) == "" {
				continue
			}
		default:
			continue
		}
		out = append(out, s)
	}
	return out
}

// ChatTurn is one message for the model's context.
type ChatTurn struct {
	Role string
	Text string
}

// ChatTurns flattens the conversation into alternating user/assistant messages
// for replaying into a new model call. Tool calls and notices are omitted: the
// clone does not reproduce the reference's tool-result format, so feeding it
// half a transcript would be worse than feeding the prose.
func (c *Conversation) ChatTurns() []ChatTurn {
	var out []ChatTurn
	for _, s := range c.Steps {
		switch s.Role() {
		case "user":
			if t := strings.TrimSpace(s.Text); t != "" {
				out = append(out, ChatTurn{Role: "user", Text: t})
			}
		case "model":
			if t := strings.TrimSpace(s.Text); t != "" {
				out = append(out, ChatTurn{Role: "assistant", Text: t})
			}
		}
	}
	return out
}

// LastChatTurns returns at most n trailing user/assistant messages, always
// starting on a user turn so the model is not handed a dangling assistant
// reply.
func (c *Conversation) LastChatTurns(n int) []ChatTurn {
	turns := c.ChatTurns()
	if n <= 0 || len(turns) <= n {
		n = len(turns)
	} else {
		turns = turns[len(turns)-n:]
	}
	for i, t := range turns {
		if t.Role == "user" {
			return turns[i:]
		}
	}
	return nil
}

// LastChatTurnsBudget is LastChatTurns with an additional character budget: it
// keeps the most recent messages whose combined text fits in maxChars, then
// trims forward to the first user turn. Real conversations hold model replies
// of tens of kilobytes, so a turn count alone is not a usable bound.
func (c *Conversation) LastChatTurnsBudget(n, maxChars int) []ChatTurn {
	turns := c.LastChatTurns(n)
	if maxChars <= 0 || len(turns) == 0 {
		return turns
	}
	total, start := 0, 0
	for i := len(turns) - 1; i >= 0; i-- {
		total += len(turns[i].Text)
		if total > maxChars {
			start = i + 1
			break
		}
	}
	turns = turns[start:]
	for i, t := range turns {
		if t.Role == "user" {
			return turns[i:]
		}
	}
	return nil
}

// StoreDir returns the reference's config directory, honouring
// $ANTIGRAVITY_HOME and $HOME the same way the rest of the clone does.
func StoreDir() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}

// ConversationPath returns the database path for a conversation id.
func ConversationPath(id string) string {
	return filepath.Join(StoreDir(), "conversations", id+".db")
}

// HasConversation reports whether conversations/<id>.db exists as a regular
// file, matching the reference's own existence test: a zero-byte file counts,
// a directory does not.
//
// The lookup compares directory entries instead of calling os.Stat, because the
// reference is case-sensitive even on a case-insensitive filesystem. Verified
// live on macOS/APFS: `--conversation=F65CFBA7-…` for the existing
// conversations/f65cfba7-….db prints `warning: conversation "F65CFBA7-…" not
// found`, which os.Stat alone would not do.
func HasConversation(id string) bool {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(StoreDir(), "conversations"))
	if err != nil {
		// An unreadable directory still lets a caller address a file it can
		// see; fall back to the filesystem's own resolution.
		st, serr := os.Stat(ConversationPath(id))
		return serr == nil && st.Mode().IsRegular()
	}
	want := id + ".db"
	for _, e := range entries {
		if e.Name() != want {
			continue
		}
		info, ierr := e.Info()
		return ierr == nil && info.Mode().IsRegular()
	}
	return false
}

// FindConversation locates the database for id, returning "" when absent.
func FindConversation(id string) string {
	if !HasConversation(id) {
		return ""
	}
	return ConversationPath(id)
}

// cleanToolResult normalises the raw text recovered from
// step_payload.field_140.field_2.field_1 so it matches what the
// reference's tool-result renderer shows.
//
// The convstore records run_command output as
//
//	"The command exited with code N.\nOutput:\n<actual stdout>\n"
//
// with CRLF (`\r\n`) line endings inside the actual stdout. The
// reference strips both the boilerplate prefix and the trailing `\r`
// of every line; the pre-cycle-176 clone passed the raw bytes through
// and the trailing `\r` made every interior row blank in the
// terminal (carriage-return moves the cursor back to column 0, so the
// next row's leading whitespace overwrites the visible content).
//
// Cycle 178:
//   - drop the "The command exited with code N.\nOutput:\n" prefix
//     when it leads the text,
//   - normalise CRLF to LF,
//   - trim leading and trailing newlines.
func cleanToolResult(raw string) string {
	s := raw
	// Drop the run_command boilerplate prefix when present. Only strip
	// when the prefix is at the head of the string, so a user payload
	// that happens to contain "Output:" mid-line is preserved verbatim.
	if i := strings.Index(s, "Output:\n"); i >= 0 {
		pre := strings.TrimSpace(s[:i])
		if pre == "" || strings.HasPrefix(pre, "The command exited with code") {
			s = s[i+len("Output:\n"):]
		}
	}
	// CRLF -> LF so a stray carriage return doesn't move the cursor
	// back to column 0 mid-line.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	// Trim the leading/trailing newlines the protobuf framing always adds.
	return strings.Trim(s, "\n")
}

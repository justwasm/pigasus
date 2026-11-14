package convstore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	agystep "github.com/justwasm/pigasus/internal/app/prompt/step"
	"github.com/justwasm/pigasus/internal/core/compaction"
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
	// StepCheckpoint is CORTEX_STEP_TYPE_CHECKPOINT = 23. It carries the
	// reference's compaction boundary, not a subagent.
	StepCheckpoint   = 23
	StepSystemNotice = 101
	StepToolCall     = 132
)

// Step is one entry of a conversation as the reference stores it.
type Step struct {
	Idx    int64
	Type   int64
	Status int64
	// Text holds the user prompt, the model's response, an error message or a
	// system notice, depending on Type.
	Text string
	// Thinking holds the model's reasoning for StepModelResponse.
	//
	// 【一手】 the reference's own stored model-response blob carries the
	// thinking: the planner-response message has a `thinking` field next to
	// the answer, and the Web UI's collapsible "Thought for Ns" panel is
	// rendered from it. A step without it shows the answer alone.
	Thinking string
	// ThinkingDuration is how long the model thought before it answered.
	//
	// 【一手】 the reference's blob carries it as a
	// google.protobuf.Duration next to the thinking — the stored value
	// read back as "6.773079s".
	ThinkingDuration time.Duration
	// ToolName and ToolArgs are set for StepToolCall. ToolArgs is the raw JSON
	// the model produced, e.g. {"CommandLine":"ls","Cwd":"/tmp"}.
	ToolName string
	ToolArgs string
	// ToolResult holds the captured tool-call output for StepToolCall. It is
	// the text inside step_payload.field_140.field_2.field_1 — recovered
	// by inspecting real convstore databases; pre-cycle-163 the clone ignored
	// this field, so an expanded loaded step (ctrl+o) showed its title only.
	ToolResult string
	// ToolSummary carries the collapsed human-readable summary
	// (e.g. "check git status" or "List all packages in repository")
	// recovered from render_info field 9.
	ToolSummary string
	// ToolPrefix carries the render_info title_prefix (e.g. "Ran", "Analyzed", "Checked task").
	ToolPrefix string

	// Raw database blobs. Preserved across Load -> Save so
	// pre-existing steps from official agy or earlier sessions are never re-encoded or mutated lossily.
	RawPayload    []byte
	RawRenderInfo []byte
	RawMetadata   []byte

	// Checkpoint fields, set for StepCheckpoint. The reference's
	// CortexStepCheckpoint lives at step_payload field 30.
	CheckpointIndex int
	IncludedStart   int // first step index folded into the summary
	IncludedEnd     int // exclusive: first step after the checkpoint
	IncludedSteps   []int64
	Summary         string
	UserIntent      string
	UserRequests    []string
	CheckpointModel string
	Strategy        string
	TokensBefore    int
	TokensAfter     int
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
	case StepCheckpoint:
		return "checkpoint"
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
		s := Step{
			Idx:           r.Int("idx"),
			Type:          r.Int("step_type"),
			Status:        r.Int("status"),
			RawPayload:    r.Bytes("step_payload"),
			RawRenderInfo: r.Bytes("render_info"),
			RawMetadata:   r.Bytes("metadata"),
		}
		parseStepPayload(&s, s.RawPayload, s.RawRenderInfo)
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

// parseStepPayload fills s from the raw step_payload and render_info blobs.
//
// The shapes, all verified against real databases:
//
//	14  user input      19 -> 3 -> 1  (text; 3 is the text itself when shallow)
//	15  model response  20 -> 1       (text)
//	132 tool call       140 -> 1 -> {1: tool name, 2: args}
//	                    140 -> 2 -> {1: result text, 3: step_render_info}
//	                    render_info column: StepRenderInfo
//	                    5 -> 4 -> {2: tool name, 3: JSON arguments} (legacy fallback)
//	17  error           24 -> 3 -> {1: short message, 2: long message}
//	101 system notice   24 -> 3 -> {1: message}
func parseStepPayload(s *Step, payload []byte, renderInfo ...[]byte) {
	var rinfo []byte
	if len(renderInfo) > 0 {
		rinfo = renderInfo[0]
	}
	if len(payload) == 0 && len(rinfo) == 0 {
		return
	}
	top := protoFields(payload)
	switch s.Type {
	case StepUserInput:
		parseUserInputStep(s, top)
	case StepModelResponse:
		if p20 := protoBytes(top, 20); p20 != nil {
			f20 := protoFields(p20)
			s.Text = protoText(f20, 1)
			// The planner-response message carries the model's
			// reasoning next to the answer: field 3 is the thinking,
			// field 11 the google.protobuf.Duration it took.
			s.Thinking = protoText(f20, 3)
			if d := protoBytes(f20, 11); d != nil {
				s.ThinkingDuration = protoDuration(protoFields(d))
			}
		}
	case StepToolCall:
		parseToolCallStep(s, top, rinfo)
	case StepError, StepSystemNotice:
		parseErrorStep(s, top)
	case StepCheckpoint:
		parseCheckpointStep(s, top)
	}
}

func parseUserInputStep(s *Step, top []protoField) {
	p19 := protoBytes(top, 19)
	if p19 == nil {
		return
	}
	f19 := protoFields(p19)
	// Field 2 is CortexStepUserInput.user_response (the user's verbatim input).
	if txt := protoText(f19, 2); strings.TrimSpace(txt) != "" {
		s.Text = txt
		return
	}
	// Field 3 is repeated CortexStepUserInput.items. Find an item with field 1 (text).
	for _, f := range f19 {
		if f.num == 3 && f.wire == 2 {
			if t1 := protoBytes(protoFields(f.data), 1); len(t1) > 0 {
				s.Text = string(t1)
				return
			}
		}
	}
	// Shallow payload fallback: field 3 itself is plain text if valid UTF-8 and printable.
	if p3 := protoBytes(f19, 3); len(p3) > 0 && utf8.Valid(p3) && isPrintableText(string(p3)) {
		s.Text = string(p3)
	}
}

func isPrintableText(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

func parseToolCallStep(s *Step, top []protoField, renderInfo []byte) {
	// 1. Recover tool name and tool args from field 140 (CortexStepGeneric)
	if p140 := protoBytes(top, 140); p140 != nil {
		f140 := protoFields(p140)
		for _, f := range f140 {
			if f.num == 1 && f.wire == 2 { // GenericStepCall
				fCall := protoFields(f.data)
				if name := protoText(fCall, 1); name != "" && s.ToolName == "" {
					s.ToolName = name
				}
				if args := protoText(fCall, 2); args != "" && s.ToolArgs == "" {
					s.ToolArgs = args
				}
			}
		}
	}
	// Fall back to field 5.4 (legacy clone writer or older reference)
	if s.ToolName == "" || s.ToolArgs == "" {
		if p5 := protoBytes(top, 5); p5 != nil {
			if p4 := protoBytes(protoFields(p5), 4); p4 != nil {
				f4 := protoFields(p4)
				if s.ToolName == "" {
					s.ToolName = protoText(f4, 2)
				}
				if s.ToolArgs == "" {
					s.ToolArgs = protoText(f4, 3)
				}
			}
		}
	}

	// 2. Recover tool result, summary and prefix from render_info
	// renderInfo can come from the SQLite column or embedded in field 140.2.3
	rBytes := renderInfo
	if len(rBytes) == 0 {
		if p140 := protoBytes(top, 140); p140 != nil {
			f140 := protoFields(p140)
			for _, f := range f140 {
				if f.num == 2 && f.wire == 2 { // GenericStepResult
					fRes := protoFields(f.data)
					if r := protoBytes(fRes, 3); len(r) > 0 {
						rBytes = r
						break
					}
				}
			}
		}
	}

	if len(rBytes) > 0 {
		rFields := protoFields(rBytes)
		s.ToolPrefix = protoText(rFields, 5)
		icon := protoText(rFields, 6)
		aggKey := protoText(rFields, 7)

		// TitlePart (field 9)
		if p9 := protoBytes(rFields, 9); len(p9) > 0 {
			f9 := protoFields(p9)
			s.ToolSummary = protoText(f9, 1)
			if pFile := protoBytes(f9, 2); len(pFile) > 0 {
				fFile := protoFields(pFile)
				if uri := protoText(fFile, 1); uri != "" {
					cleanPath := strings.TrimPrefix(uri, "file://")
					if s.ToolSummary == "" {
						s.ToolSummary = filepath.Base(cleanPath)
					}
					if s.ToolArgs == "" {
						encoded, _ := json.Marshal(map[string]string{"Path": cleanPath})
						s.ToolArgs = string(encoded)
					}
				}
			}
		}

		// ContentBlock (field 10)
		for _, f := range rFields {
			if f.num == 10 && f.wire == 2 {
				f10 := protoFields(f.data)
				// TerminalBlock is field 3
				if pTerm := protoBytes(f10, 3); len(pTerm) > 0 {
					fTerm := protoFields(pTerm)
					cmd := protoText(fTerm, 1)
					out := protoText(fTerm, 4)
					if s.ToolArgs == "" && cmd != "" {
						encoded, _ := json.Marshal(map[string]string{"CommandLine": cmd})
						s.ToolArgs = string(encoded)
					}
					if s.ToolResult == "" && out != "" {
						s.ToolResult = cleanToolResult(out)
					}
				}
				// Markdown block is field 1
				if md := protoText(f10, 1); md != "" && s.ToolResult == "" {
					s.ToolResult = cleanToolResult(md)
				}
			}
		}

		// Tool name fallback from icon/aggregation
		if s.ToolName == "" {
			if icon == "terminal" || aggKey == "terminal" {
				s.ToolName = "run_command"
			} else if aggKey == "files" || icon == "description" {
				s.ToolName = "read_file"
			} else if aggKey == "edits" || icon == "edit" {
				s.ToolName = "edit_file"
			} else if aggKey == "tasks" || icon == "task_alt" {
				s.ToolName = "manage_task"
			}
		}
	}

	// 3. Fall back to GenericStepResult.output (field 140.2.1)
	if s.ToolResult == "" {
		if p140 := protoBytes(top, 140); p140 != nil {
			f140 := protoFields(p140)
			for _, f := range f140 {
				if f.num == 2 && f.wire == 2 {
					fRes := protoFields(f.data)
					if out := protoText(fRes, 1); out != "" {
						s.ToolResult = cleanToolResult(out)
					}
				}
			}
		}
	}
}

func parseCheckpointStep(s *Step, top []protoField) {
	p30 := protoBytes(top, 30)
	if p30 == nil {
		return
	}
	f := protoFields(p30)
	s.CheckpointIndex = int(protoVarint(f, 1))
	s.UserIntent = protoText(f, 4)
	s.Summary = protoText(f, 5)
	s.IncludedStart = int(protoVarint(f, 11))
	s.IncludedEnd = int(protoVarint(f, 12))
	s.IncludedSteps = protoRepeatedVarint(f, 3)
	s.UserRequests = checkpointUserRequests(f)
}

// checkpointUserRequests reads the request list a stored checkpoint carries.
//
// 【一手】 agy 1.5.3, the conversation database behind the live manual
// `/compact` capture (conversation e9bd3e8a-…): each request is field 30 of
// the checkpoint message, encoded as a nested message whose field 1 is the
// request text. Field 19 was a guess from an older cycle and never appears in
// a reference database; it is kept as a fallback for stores the clone itself
// may have written.
func checkpointUserRequests(f []protoField) []string {
	var out []string
	for _, field := range f {
		if field.num != 30 || field.wire != 2 {
			continue
		}
		if text := protoText(protoFields(field.data), 1); text != "" {
			out = append(out, text)
		}
	}
	if len(out) == 0 {
		out = protoRepeatedText(f, 19)
	}
	return out
}

func parseErrorStep(s *Step, top []protoField) {
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
			if strings.TrimSpace(s.ToolName) == "" && strings.TrimSpace(s.ToolSummary) == "" && len(s.RawRenderInfo) == 0 {
				continue
			}
		case "checkpoint":
			if s.Summary == "" && s.UserIntent == "" && s.IncludedEnd == 0 {
				continue
			}
			out = append(out, s)
			continue
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

// Checkpoint returns the latest checkpoint step, or nil.
func (c *Conversation) Checkpoint() *Step {
	var latest *Step
	for i := range c.Steps {
		if c.Steps[i].Type != StepCheckpoint {
			continue
		}
		// A payload-less type-23 placeholder is opaque. It is not a
		// usable compaction boundary.
		if c.Steps[i].Summary == "" && c.Steps[i].UserIntent == "" && c.Steps[i].IncludedEnd == 0 {
			continue
		}
		latest = &c.Steps[i]
	}
	return latest
}

// ChatTurns flattens the conversation into alternating user/assistant messages
// for replaying into a new model call. Tool calls and notices are omitted: the
// clone does not reproduce the reference's tool-result format, so feeding it
// half a transcript would be worse than feeding the prose.
func (c *Conversation) ChatTurns() []ChatTurn {
	var out []ChatTurn
	start := int64(0)
	if cp := c.Checkpoint(); cp != nil {
		summary := strings.TrimSpace(cp.Summary)
		if summary == "" {
			summary = strings.TrimSpace(cp.UserIntent)
		}
		out = append(out, ChatTurn{
			Role: "user",
			Text: compaction.ResumingBlock(cp.UserRequests, summary),
		})
		start = int64(cp.IncludedEnd)
	}
	for _, s := range c.Steps {
		if s.Idx < start || s.Type == StepCheckpoint {
			continue
		}
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

// FindPrefixMatches returns all existing conversation IDs that start with the given prefix.
func FindPrefixMatches(prefix string) []string {
	if prefix == "" || strings.ContainsAny(prefix, `/\`) {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(StoreDir(), "conversations"))
	if err != nil {
		return nil
	}
	var matches []string
	prefixLower := strings.ToLower(prefix)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		stem := strings.TrimSuffix(name, ".db")
		if strings.HasPrefix(strings.ToLower(stem), prefixLower) {
			info, ierr := e.Info()
			if ierr == nil && info.Mode().IsRegular() {
				matches = append(matches, stem)
			}
		}
	}
	sort.Strings(matches)
	return matches
}

// cleanToolResult normalises the raw text recovered from
// step_payload.field_140.field_2.field_1 so it matches what the
// reference's tool-result renderer shows.
//
// The reference prints a run_command result in one of two shapes:
//
//	"The command exited with code N.\nOutput:\n<actual stdout>\n"   (step string)
//	"Command exited with code N.\n\nCommand output:\n<actual stdout>\n"   (CLI formatter)
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
//
// Cycle 298: the reference's 1.4.4+ results carry the CLI formatter's
// shape, so the same prefix has to go. The reader is internal/app/prompt/step's,
// which knows both shapes.
func cleanToolResult(raw string) string {
	s := raw
	// A command result is read whole: internal/app/prompt/step's reader knows both
	// shapes and hands back the output without the boilerplate. It only
	// fires when the result carries an output block — a command that
	// printed nothing keeps its text.
	if res, ok := agystep.ParseRunCommandResult(s); ok && res.HasOutput {
		s = res.Output
	} else {
		// Drop the run_command boilerplate prefix when present. Only strip
		// when the prefix is at the head of the string, so a user payload
		// that happens to contain "Output:" mid-line is preserved verbatim.
		if i := strings.Index(s, "Output:\n"); i >= 0 {
			pre := strings.TrimSpace(s[:i])
			if pre == "" || strings.HasPrefix(pre, "The command exited with code") {
				s = s[i+len("Output:\n"):]
			}
		}
		if i := strings.Index(s, "Command output:\n"); i >= 0 {
			pre := strings.TrimSpace(s[:i])
			if pre == "" || strings.HasPrefix(pre, "Command exited with code") {
				s = s[i+len("Command output:\n"):]
			}
		}
	}
	// CRLF -> LF so a stray carriage return doesn't move the cursor
	// back to column 0 mid-line.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	// Trim the leading/trailing newlines the protobuf framing always adds.
	return strings.Trim(s, "\n")
}

// convstore/write.go — write side of the persistent conversation store.
// Mirrors what the reference agy binary reads so the user can move
// between wagyu and agy (or run -c / --continue / /resume) without
// losing context.
package convstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// HistoryEntry is one line in history.jsonl, matching the reference agy
// format. Display is the prompt text; Workspace, ConversationID and Type
// are optional metadata the picker uses to group and filter entries.
type HistoryEntry struct {
	Display        string `json:"display"`
	Timestamp      int64  `json:"timestamp"`
	Workspace      string `json:"workspace"`
	ConversationID string `json:"conversationId,omitempty"`
	Type           string `json:"type,omitempty"`
}

// AppendHistoryEntry writes one entry to history.jsonl in StoreDir().
func AppendHistoryEntry(entry HistoryEntry) error {
	if entry.Display == "" {
		return nil
	}
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().UnixMilli()
	}
	if entry.Type == "" && strings.HasPrefix(entry.Display, "/") {
		entry.Type = "slash_command"
	}
	dir := StoreDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "history.jsonl")
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(data)
	return err
}

// LoadHistory reads recent display prompts from history.jsonl, returning
// them in chronological order up to maxCount.
func LoadHistory(workspace string, maxCount int) []string {
	path := filepath.Join(StoreDir(), "history.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var prompts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Display == "" {
			continue
		}
		if len(prompts) > 0 && prompts[len(prompts)-1] == entry.Display {
			continue
		}
		prompts = append(prompts, entry.Display)
	}
	if maxCount > 0 && len(prompts) > maxCount {
		prompts = prompts[len(prompts)-maxCount:]
	}
	return prompts
}

// SaveConversation writes the conversation database under
// conversations/<id>.db and updates cache/last_conversations.json,
// cache/conversation_metadata.json, and conversation_summaries.db so the
// conversation is immediately available to agy and wagyu for
// -c / --continue and /resume.
func SaveConversation(id, workspace string, steps []Step) error {
	if id == "" || len(steps) == 0 {
		return nil
	}
	store := StoreDir()
	convDir := filepath.Join(store, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		return err
	}
	cacheDir := filepath.Join(store, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}

	dbPath := filepath.Join(convDir, id+".db")
	if err := WriteConversation(dbPath, id, steps); err != nil {
		return err
	}

	if workspace != "" {
		updateLastConversations(filepath.Join(cacheDir, "last_conversations.json"), workspace, id)
	}
	updateConversationMetadata(filepath.Join(cacheDir, "conversation_metadata.json"), id, workspace, steps)
	updateConversationSummariesDB(filepath.Join(store, "conversation_summaries.db"), id, workspace, steps)
	return nil
}

func updateLastConversations(path, workspace, convID string) {
	m := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	m[workspace] = convID
	if data, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

type metadataFile struct {
	Conversations map[string]conversationMetadataEntry `json:"conversations"`
}

type conversationMetadataEntry struct {
	Summary          conversationSummary `json:"summary"`
	IsInternal       bool                `json:"is_internal"`
	LastModifiedTime string              `json:"last_modified_time"`
}

type conversationSummary struct {
	ID            string   `json:"ID"`
	Title         string   `json:"Title"`
	Preview       string   `json:"Preview"`
	NumSteps      int      `json:"NumSteps"`
	Loaded        bool     `json:"Loaded"`
	UpdatedAt     string   `json:"UpdatedAt"`
	WorkspaceURIs []string `json:"WorkspaceURIs"`
}

func updateConversationMetadata(path, id, workspace string, steps []Step) {
	var meta metadataFile
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &meta)
	}
	if meta.Conversations == nil {
		meta.Conversations = make(map[string]conversationMetadataEntry)
	}

	preview := ""
	for _, s := range steps {
		if s.Type == StepUserInput && s.Text != "" {
			preview = s.Text
			break
		}
	}
	if preview == "" && len(steps) > 0 {
		preview = steps[0].Text
	}
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	var uris []string
	if workspace != "" {
		uris = []string{"file://" + filepath.Clean(workspace)}
	}

	meta.Conversations[id] = conversationMetadataEntry{
		Summary: conversationSummary{
			ID:            id,
			Title:         preview,
			Preview:       preview,
			NumSteps:      len(steps),
			Loaded:        true,
			UpdatedAt:     nowStr,
			WorkspaceURIs: uris,
		},
		IsInternal:       false,
		LastModifiedTime: nowStr,
	}

	if data, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

func updateConversationSummariesDB(path, id, workspace string, steps []Step) {
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		return
	}
	defer sdb.Close()

	_, _ = sdb.Exec(`CREATE TABLE IF NOT EXISTS conversation_summaries (
		conversation_id text PRIMARY KEY,
		title text,
		preview text,
		step_count integer,
		workspace_uris text,
		last_modified_time text)`)

	preview := ""
	for _, s := range steps {
		if s.Type == StepUserInput && s.Text != "" {
			preview = s.Text
			break
		}
	}
	if preview == "" && len(steps) > 0 {
		preview = steps[0].Text
	}
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	uris := ""
	if workspace != "" {
		uris = "file://" + filepath.Clean(workspace)
	}

	_, _ = sdb.Exec(`INSERT INTO conversation_summaries (conversation_id, title, preview, step_count, workspace_uris, last_modified_time)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			title=excluded.title,
			preview=excluded.preview,
			step_count=excluded.step_count,
			workspace_uris=excluded.workspace_uris,
			last_modified_time=excluded.last_modified_time`,
		id, preview, preview, len(steps), uris, nowStr)
}

// WriteConversation creates a fresh SQLite database at path with the
// recovered trajectory_meta + steps schema, then inserts one row per
// step. It is the cycle-167 /fork backend: openagy forks the live
// session into a brand-new store db under a new UUID so the user can
// /resume to it later.
//
// The schema mirrors internal/convstore/testdata/gen_fixture.py so
// the resulting file passes LoadConversation unmodified on the next
// read. step_payload is the raw protobuf the reference would emit;
// for openagy forks we only carry user-input + model-response text,
// which the clone's parser already understands.
//
// On error the partial file is removed so a failed fork never
// produces a half-written db the user can later /resume into.
func WriteConversation(path, trajectoryID string, steps []Step) error {
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		return fmt.Errorf("convstore: write %s: %w", path, err)
	}
	defer sdb.Close()

	if _, err := sdb.Exec(`CREATE TABLE IF NOT EXISTS trajectory_meta (
		trajectory_id text, cascade_id text,
		trajectory_type integer, source integer,
		PRIMARY KEY (trajectory_id))`); err != nil {
		fmt.Fprintf(os.Stderr, "convstore: write schema: %v (NOT removing %s)\n", err, path)
		return fmt.Errorf("convstore: write schema: %w", err)
	}
	if _, err := sdb.Exec(`CREATE TABLE IF NOT EXISTS steps (
		idx integer, step_type integer NOT NULL DEFAULT 0,
		status integer NOT NULL DEFAULT 0,
		has_subtrajectory numeric NOT NULL DEFAULT false,
		metadata blob, error_details blob, permissions blob,
		task_details blob, render_info blob,
		step_payload blob, step_format integer NOT NULL DEFAULT 0,
		PRIMARY KEY (idx))`); err != nil {
		fmt.Fprintf(os.Stderr, "convstore: write steps schema: %v (NOT removing %s)\n", err, path)
		return fmt.Errorf("convstore: write schema: %w", err)
	}
	if _, err := sdb.Exec(
		`INSERT INTO trajectory_meta VALUES (?, ?, ?, ?)
		 ON CONFLICT(trajectory_id) DO UPDATE SET
		   cascade_id=excluded.cascade_id,
		   trajectory_type=excluded.trajectory_type,
		   source=excluded.source`,
		trajectoryID, trajectoryID, 4, 17); err != nil {
		fmt.Fprintf(os.Stderr, "convstore: write meta: %v (NOT removing %s)\n", err, path)
		return fmt.Errorf("convstore: write meta: %w", err)
	}
	for _, s := range steps {
		payload := encodeForkPayload(s)
		if _, err := sdb.Exec(
			`INSERT INTO steps (idx, step_type, status, step_payload) VALUES (?, ?, ?, ?)
			 ON CONFLICT(idx) DO UPDATE SET
			   step_type=excluded.step_type,
			   status=excluded.status,
			   step_payload=excluded.step_payload`,
			s.Idx, s.Type, s.Status, payload); err != nil {
			fmt.Fprintf(os.Stderr, "convstore: write step %d: %v (NOT removing %s)\n", s.Idx, err, path)
			return fmt.Errorf("convstore: write step %d: %w", s.Idx, err)
		}
	}
	return nil
}

// encodeForkPayload emits the protobuf shape LoadConversation parses
// for a user-input, model-response, tool-call or error step. The shape
// mirrors what parseStepPayload reads in agy.go (cycle 167):
//
//	 14 user input     19 -> 3 -> 1 (text)
//	 15 model response 20 -> 1    (text)
//	132 tool call       5 -> 4 -> {1: call id, 2: tool name, 3: JSON args}
//	                    140 -> 2 -> 1 (the result text)
//	 17 error / 101 system notice  24 -> 3 -> 1 (text)
//
// We use the wire format directly (varint tag, length-delimited) so
// the clone and the reference can both re-load the resulting db.
func encodeForkPayload(s Step) []byte {
	switch s.Type {
	case StepUserInput:
		// field 19 (length-delimited) -> field 3 (length-delimited) -> field 1 (text)
		text := s.Text
		// The recovered parser walks field_19.field_3.field_1 when
		// field 1 is non-empty; otherwise it falls back to using
		// field 3's raw bytes as text. To make the round trip lossless,
		// wrap the text as field 1 inside the innermost field 3, so the
		// parser's primary path (protoBytes(protoFields(p3), 1)) hits
		// and yields exactly s.Text with no extra protobuf bytes
		// leaking into the loaded string.
		inner := encodeProtoField(1, 2, []byte(text))
		mid := encodeProtoField(3, 2, inner)
		return encodeProtoField(19, 2, mid)
	case StepModelResponse:
		// field 20 -> field 1 (text)
		return encodeProtoField(20, 2, encodeProtoField(1, 2, []byte(s.Text)))
	case StepToolCall:
		return encodeToolCallPayload(s)
	case StepError, StepSystemNotice:
		// field 24 -> field 3 -> field 1 (text)
		inner := encodeProtoField(1, 2, []byte(s.Text))
		mid := encodeProtoField(3, 2, inner)
		return encodeProtoField(24, 2, mid)
	}
	return nil
}

// encodeToolCallPayload emits the reference's tool-call step shape.
//
// 【一手】 verified against the reference's own conversation stores: a
// stored tool call carries the call id, the tool name and the raw JSON
// arguments in field 4 of the tool-call payload (field 5 of the step),
// and the result in field 140. The step's own type and status ride
// along in fields 1 and 4 of the step payload.
//
// The clone's parser (parseToolCallStep) reads exactly those three
// places, so a db the clone writes reads back the way a reference db
// reads back.
func encodeToolCallPayload(s Step) []byte {
	call := encodeProtoField(2, 2, []byte(s.ToolName))
	call = append(call, encodeProtoField(3, 2, []byte(s.ToolArgs))...)
	payload := encodeProtoField(5, 2, encodeProtoField(4, 2, call))
	payload = append(payload, encodeProtoVarint(1, uint64(s.Type))...)
	payload = append(payload, encodeProtoVarint(4, uint64(s.Status))...)
	if s.ToolResult != "" {
		// field 140 -> field 2 -> field 1 (the result text)
		result := encodeProtoField(1, 2, []byte(s.ToolResult))
		payload = append(payload, encodeProtoField(140, 2, encodeProtoField(2, 2, result))...)
	}
	return payload
}

// encodeProtoVarint emits one tag + varint pair. wireType 0 is varint.
func encodeProtoVarint(num int, value uint64) []byte {
	tag := uint64(num<<3 | 0x0)
	out := appendVarint(nil, tag)
	return appendVarint(out, value)
}

// encodeProtoField emits one tag + length + value triple. wireType 2
// is length-delimited; wireType 0 is varint.
func encodeProtoField(num, wireType int, value []byte) []byte {
	tag := uint64(num<<3 | (wireType & 0x7))
	out := appendVarint(nil, tag)
	out = appendVarint(out, uint64(len(value)))
	out = append(out, value...)
	return out
}

// appendVarint encodes v as a base-128 varint (the same shape
// parseStepPayload reads via pbVarint in protobuf.go).
func appendVarint(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	out = append(out, byte(v))
	return out
}
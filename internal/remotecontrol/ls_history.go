// Package remotecontrol — ls_history.go: the conversation history and
// the project list the Antigravity Web UI reads.
//
// The Web UI's sidebar is built from two server streams:
//
//   - JetboxSubscribeToSummaries — the `updates` map, keyed by the
//     conversation id, whose values are the reference's own
//     `exa.jetski_cortex_pb.CascadeTrajectorySummary` messages.
//   - ProjectUpdatesStream — the list of the project ids.
//
// Both are answered from the reference's own store on disk:
//
//   - the summaries are the rows of `conversation_summaries.db`, whose
//     `raw_summary` blob *is* the serialised summary message;
//   - the projects are the files of `~/.gemini/config/projects/`.
//
// 【一手】 2026-10-26, live: the reference's project store holds
// `default-cli-project.json` — {"id":"default-cli-project",
// "name":"CLI Project"} — and one file per workspace project, whose
// name is the workspace's absolute path.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/lsproto"
)

// lsSummaryMessageName is the reference's own summary message.
const lsSummaryMessageName = "exa.jetski_cortex_pb.CascadeTrajectorySummary"

// lsDefaultProjectID is the id of the reference's default project —
// the one a run without a workspace belongs to.
//
// 【strings】 the reference binary carries the literal
// "default-cli-project" next to its project plumbing.
const lsDefaultProjectID = "default-cli-project"

// lsSummaryUpdates returns the `updates` map the Web UI's summary
// stream carries: the conversation id → the summary message.
//
// The map is keyed by the conversation id, not by the trajectory id
// inside the summary: the frontend's store is keyed by the
// conversation it shows in the sidebar.
func lsSummaryUpdates() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	rows, err := convstore.ListSummaries()
	if err != nil {
		return out
	}
	codec, _ := lsproto.Shared()
	for _, row := range rows {
		raw := lsSummaryJSON(codec, row)
		if len(raw) == 0 {
			continue
		}
		out[row.ConversationID] = raw
	}
	return out
}

// lsSummaryJSON renders one row as the proto-JSON of the summary
// message.
//
// The row's `raw_summary` blob is the reference's own serialisation
// of that message, so it is decoded as it stands — the clone never
// has to invent a field. A row without a blob is rebuilt from the
// columns.
func lsSummaryJSON(codec *lsproto.Codec, row convstore.Summary) json.RawMessage {
	if codec != nil && len(row.RawSummary) > 0 {
		if out, err := codec.DecodeNamed(lsSummaryMessageName, row.RawSummary); err == nil {
			return lsFillSummaryTitle(out, row.Title)
		}
	}
	return lsSummaryFromColumns(row)
}

// lsFillSummaryTitle gives a summary message the row's title when the
// message itself carries none.
//
// 【一手】 2026-10-26: the reference's own daemon reconciles the
// summaries table when it starts ("[Summaries] failed to generate
// summary for <id> while reconciling", `reconcile_summaries.go`) and
// rewrites the rows it can read in the reference's own shape — with an
// **empty title** when it cannot generate one, because the
// conversation's steps are the clone's shape and not the reference's
// protobuf. The blob it wrote carries no title either, and the Web UI
// then falls back to "Untitled Conversation". The clone's own name for
// the conversation is in the metadata cache (the reconcile pass leaves
// the cache alone), and it goes into the message here.
func lsFillSummaryTitle(raw json.RawMessage, title string) json.RawMessage {
	if title == "" {
		return raw
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	if s, ok := obj["summary"].(string); ok && s != "" {
		return raw
	}
	obj["summary"] = title
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

// lsSummaryFromColumns rebuilds a summary from the row's columns, for
// the rows the reference never wrote a blob for.
func lsSummaryFromColumns(row convstore.Summary) json.RawMessage {
	obj := map[string]any{
		"summary":      row.Title,
		"stepCount":    row.StepCount,
		"trajectoryId": row.ConversationID,
	}
	if !row.LastModifiedTime.IsZero() {
		obj["lastModifiedTime"] = lsTimestampJSON(row.LastModifiedTime)
	}
	if !row.LastUserInputTime.IsZero() {
		obj["lastUserInputTime"] = lsTimestampJSON(row.LastUserInputTime)
	}
	if row.Status != "" {
		obj["status"] = row.Status
	}
	if row.Source != "" {
		obj["source"] = row.Source
	}
	if len(row.WorkspaceURIs) > 0 {
		workspaces := make([]any, 0, len(row.WorkspaceURIs))
		for _, uri := range row.WorkspaceURIs {
			workspaces = append(workspaces, map[string]any{"workspaceFolderAbsoluteUri": uri})
		}
		obj["workspaces"] = workspaces
	}
	if row.ProjectID != "" {
		obj["trajectoryMetadata"] = map[string]any{"projectId": row.ProjectID}
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil
	}
	return raw
}

// handleJetboxSubscribeToSummaries answers JetboxSubscribeToSummaries
// with the reference's own conversation index.
//
// The response's `updates` map is keyed by the conversation id and
// carries the reference's own CascadeTrajectorySummary messages; the
// `deletes` list is empty because the clone never removes a summary.
func (d *Dispatcher) handleJetboxSubscribeToSummaries() (any, *rpcErrorBody, bool) {
	return map[string]any{
		"updates": lsSummaryUpdates(),
		"deletes": []any{},
	}, nil, true
}

// lsTimestampJSON renders a time in the proto-JSON form of
// google.protobuf.Timestamp — RFC 3339 with 0, 3, 6 or 9 fractional
// digits.
func lsTimestampJSON(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
}

// lsWriteSummaryParams is the LanguageServerService.JetboxWriteSummary
// request shape: the conversation id and the reference's own summary
// message.
type lsWriteSummaryParams struct {
	CascadeID string          `json:"cascadeId"`
	Summary   json.RawMessage `json:"summary"`
}

// lsSummaryMessage is the slice of
// exa.jetski_cortex_pb.CascadeTrajectorySummary the clone reads.
type lsSummaryMessage struct {
	Summary           string `json:"summary"`
	StepCount         int    `json:"stepCount"`
	LastModifiedTime  string `json:"lastModifiedTime"`
	LastUserInputTime string `json:"lastUserInputTime"`
	Status            string `json:"status"`
	Source            string `json:"source"`
	Workspaces        []struct {
		WorkspaceFolderAbsoluteURI string `json:"workspaceFolderAbsoluteUri"`
	} `json:"workspaces"`
	TrajectoryMetadata struct {
		ProjectID string `json:"projectId"`
	} `json:"trajectoryMetadata"`
}

// handleLSJetboxWriteSummary stores the summary the Web UI sends after
// a turn.
//
// 【一手】 2026-10-26, live: the Web UI asks the server to write the
// summary right after a turn ends, and the request carries the
// reference's own exa.jetski_cortex_pb.CascadeTrajectorySummary
// message.
//
// The reference stores that message in the `raw_summary` column of
// conversation_summaries.db and mirrors its title into the `preview`
// column. The clone writes the columns and leaves the blob alone: the
// history stream falls back to the columns for a row without a blob.
func (d *Dispatcher) handleLSJetboxWriteSummary(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req lsWriteSummaryParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.CascadeID)
	if id == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	if err := convstore.WriteSummaryRow(lsSummaryRow(id, req.Summary)); err != nil {
		log.Printf("remotecontrol: write summary %s: %v", id, err)
	}
	return map[string]any{}, nil, true
}

// lsSummaryRow turns the summary message into the row the reference
// stores.
func lsSummaryRow(id string, raw json.RawMessage) convstore.SummaryRow {
	row := convstore.SummaryRow{ConversationID: id}
	if len(raw) == 0 {
		return row
	}
	var msg lsSummaryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return row
	}
	title := strings.TrimSpace(msg.Summary)
	row.Title = title
	row.Preview = title
	row.StepCount = msg.StepCount
	// The conversation file is the ground truth for the step count:
	// the Web UI writes the summary with its own optimistic count,
	// which is zero while the conversation is still being created.
	if count := convstore.ConversationStepCount(id); count > row.StepCount {
		row.StepCount = count
	}
	row.Status = msg.Status
	row.Source = msg.Source
	row.ProjectID = msg.TrajectoryMetadata.ProjectID
	row.LastModifiedTime = lsParseTimestamp(msg.LastModifiedTime)
	row.LastUserInputTime = lsParseTimestamp(msg.LastUserInputTime)
	for _, ws := range msg.Workspaces {
		if uri := strings.TrimSpace(ws.WorkspaceFolderAbsoluteURI); uri != "" {
			row.WorkspaceURIs = append(row.WorkspaceURIs, uri)
		}
	}
	return row
}

// lsParseTimestamp reads a proto-JSON google.protobuf.Timestamp — RFC
// 3339 with 0, 3, 6 or 9 fractional digits.
func lsParseTimestamp(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// lsProjectFile is one entry of the reference's project store.
type lsProjectFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// lsProjectFiles reads the reference's own project store — the
// `<id>.json` files under `~/.gemini/config/projects/`.
func lsProjectFiles() []lsProjectFile {
	dir := lsConfigProjectsDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []lsProjectFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var f lsProjectFile
		if err := json.Unmarshal(data, &f); err != nil || f.ID == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// lsConfigProjectsDir returns the reference's project directory.
func lsConfigProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config", "projects")
}

// lsProjectIDs returns the project ids the Web UI's list carries. The
// default project comes first: it is the one the reference selects.
//
// The clone's own project store is merged in, so a project the clone
// created is listed next to the reference's.
func (d *Dispatcher) lsProjectIDs() []string {
	files := lsProjectFiles()
	sort.SliceStable(files, func(i, j int) bool {
		if (files[i].ID == lsDefaultProjectID) != (files[j].ID == lsDefaultProjectID) {
			return files[i].ID == lsDefaultProjectID
		}
		return files[i].ID < files[j].ID
	})
	ids := make([]string, 0, len(files))
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		ids = append(ids, f.ID)
	}
	for _, id := range d.lsOwnProjectIDs() {
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// lsOwnProjectIDs reads the clone's own project store.
func (d *Dispatcher) lsOwnProjectIDs() []string {
	if d == nil || d.cfg.Projects == nil {
		return nil
	}
	projs, err := d.cfg.Projects.List(context.Background(), "", 200)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(projs))
	for _, p := range projs {
		if p.ID != "" {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// lsProjectObjects returns the project objects for the given ids —
// the proto-JSON of exa.project_pb.Project — and the ids the store
// does not know.
//
// The proto-JSON spells the fields `id` and `name`; the clone's own
// Project record spells them `ID` and `Name`, and a decoder that
// matches field names drops the ones it cannot find.
func (d *Dispatcher) lsProjectObjects(ctx context.Context, ids []string) ([]any, []string) {
	byID := map[string]lsProjectFile{}
	for _, f := range lsProjectFiles() {
		byID[f.ID] = f
	}
	projects := []any{}
	notFound := []string{}
	for _, id := range ids {
		if f, ok := byID[id]; ok {
			projects = append(projects, map[string]any{"id": f.ID, "name": f.Name})
			continue
		}
		if p, err := d.lsOwnProject(ctx, id); err == nil && p.ID != "" {
			projects = append(projects, map[string]any{"id": p.ID, "name": p.Name})
			continue
		}
		notFound = append(notFound, id)
	}
	return projects, notFound
}

// lsOwnProject reads one project from the clone's own store.
func (d *Dispatcher) lsOwnProject(ctx context.Context, id string) (Project, error) {
	if d == nil || d.cfg.Projects == nil {
		return Project{}, errors.New("no project store")
	}
	return d.cfg.Projects.Get(ctx, id)
}

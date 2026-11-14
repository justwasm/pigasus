package convstore

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParity_LiveAgyInspection(t *testing.T) {
	livePath := os.ExpandEnv("$HOME/.gemini/antigravity-cli/conversations/00ac8418-ffd3-466a-ae5b-b78351e5b15a.db")
	if _, err := os.Stat(livePath); err != nil {
		t.Skip("live agy db not present, skipping")
	}

	c, err := LoadConversation(livePath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	typeCounts := make(map[int64]int)
	droppedCounts := make(map[int64]int)
	for _, s := range c.Steps {
		typeCounts[s.Type]++
	}

	for _, s := range c.Steps {
		dropped := false
		switch s.Role() {
		case "user", "model", "error", "system":
			if s.Text == "" {
				dropped = true
			}
		case "tool":
			if s.ToolName == "" && s.ToolSummary == "" && len(s.RawRenderInfo) == 0 {
				dropped = true
			}
		case "checkpoint":
			if s.Summary == "" && s.UserIntent == "" && s.IncludedEnd == 0 {
				dropped = true
			}
		default:
			dropped = true
		}
		if dropped {
			droppedCounts[s.Type]++
		}
	}

	t.Logf("Step type counts: %+v", typeCounts)
	t.Logf("Dropped step counts by type: %+v", droppedCounts)

	toolCount := 0
	emptyToolNameCount := 0
	emptyToolResultCount := 0
	for _, s := range c.Steps {
		if s.Type == StepToolCall {
			toolCount++
			if s.ToolName == "" {
				emptyToolNameCount++
			}
			if s.ToolResult == "" {
				emptyToolResultCount++
			}
		}
	}

	transcript := c.Transcript()
	_ = transcript
	if len(c.Steps) > 140 {
		s140 := c.Steps[140]
		t.Logf("Step 140: Type=%d, ToolName=%q, ToolArgs=%q, ToolSummary=%q",
			s140.Type, s140.ToolName, s140.ToolArgs, s140.ToolSummary)
	}
	t.Logf("Total tool steps: %d, empty ToolName: %d, empty ToolResult: %d",
		toolCount, emptyToolNameCount, emptyToolResultCount)

	emptyToolNames := make(map[string]int)
	for _, s := range c.Steps {
		if s.Type == StepToolCall && s.ToolResult == "" {
			emptyToolNames[s.ToolName]++
		}
	}
	t.Logf("Empty ToolResult breakdown by ToolName: %+v", emptyToolNames)

	if emptyToolNameCount > 0 {
		t.Errorf("FAIL: %d tool steps have empty ToolName (they are dropped from transcript!)", emptyToolNameCount)
	}
}

func TestParity_WriteConversationEmitsRenderInfoAndField140(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	steps := []Step{
		{
			Idx:    0,
			Type:   StepUserInput,
			Status: 3,
			Text:   "Hello from test",
		},
		{
			Idx:         1,
			Type:        StepToolCall,
			Status:      3,
			ToolName:    "run_command",
			ToolArgs:    `{"CommandLine":"git status"}`,
			ToolResult:  "On branch main\nnothing to commit",
			ToolSummary: "check git status",
		},
		{
			Idx:         2,
			Type:        StepToolCall,
			Status:      3,
			ToolName:    "read_file",
			ToolArgs:    `{"Path":"/path/to/go.mod"}`,
			ToolResult:  "module test",
			ToolSummary: "go.mod",
		},
		{
			Idx:    3,
			Type:   StepModelResponse,
			Status: 3,
			Text:   "Everything looks clean.",
		},
	}

	if err := WriteConversation(dbPath, "test-traj", steps); err != nil {
		t.Fatalf("WriteConversation failed: %v", err)
	}

	// 1. Verify directly in SQLite that render_info column is populated
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer sdb.Close()

	var rinfo1, rinfo2 []byte
	var payload1 []byte
	if err := sdb.QueryRow(`SELECT render_info, step_payload FROM steps WHERE idx = 1`).Scan(&rinfo1, &payload1); err != nil {
		t.Fatalf("query step 1 failed: %v", err)
	}
	if err := sdb.QueryRow(`SELECT render_info FROM steps WHERE idx = 2`).Scan(&rinfo2); err != nil {
		t.Fatalf("query step 2 failed: %v", err)
	}

	if len(rinfo1) == 0 {
		t.Errorf("render_info for step 1 (run_command) is empty!")
	}
	if len(rinfo2) == 0 {
		t.Errorf("render_info for step 2 (read_file) is empty!")
	}

	// Verify StepRenderInfo fields for step 1
	rFields := protoFields(rinfo1)
	if prefix := protoText(rFields, 5); prefix != "Ran" {
		t.Errorf("step 1 prefix = %q, want 'Ran'", prefix)
	}
	if icon := protoText(rFields, 6); icon != "terminal" {
		t.Errorf("step 1 icon = %q, want 'terminal'", icon)
	}

	// Verify field 140 is present in step_payload
	pFields := protoFields(payload1)
	p140 := protoBytes(pFields, 140)
	if len(p140) == 0 {
		t.Fatalf("field 140 missing from step 1 payload!")
	}

	// 2. Load conversation back and verify lossless round-trip
	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	if len(c.Steps) != 4 {
		t.Fatalf("Loaded %d steps, want 4", len(c.Steps))
	}

	loaded1 := c.Steps[1]
	if loaded1.ToolName != "run_command" {
		t.Errorf("loaded step 1 ToolName = %q, want 'run_command'", loaded1.ToolName)
	}
	if loaded1.ToolResult != "On branch main\nnothing to commit" {
		t.Errorf("loaded step 1 ToolResult = %q", loaded1.ToolResult)
	}
	if loaded1.ToolSummary != "check git status" {
		t.Errorf("loaded step 1 ToolSummary = %q, want 'check git status'", loaded1.ToolSummary)
	}

	loaded2 := c.Steps[2]
	if loaded2.ToolName != "read_file" {
		t.Errorf("loaded step 2 ToolName = %q, want 'read_file'", loaded2.ToolName)
	}
	if loaded2.ToolResult != "module test" {
		t.Errorf("loaded step 2 ToolResult = %q, want 'module test'", loaded2.ToolResult)
	}
}

func TestParity_AgyResumesPigasusDB(t *testing.T) {
	agyBin := "/Users/gear/.local/bin/agy"
	if _, err := os.Stat(agyBin); err != nil {
		t.Skip("official agy binary not found, skipping live TUI test")
	}

	convID := "test-pigasus-agy-e2e"
	dbPath := filepath.Join(os.ExpandEnv("$HOME/.gemini/antigravity-cli/conversations"), convID+".db")
	_ = os.Remove(dbPath)
	defer os.Remove(dbPath)

	steps := []Step{
		{
			Idx:    0,
			Type:   StepUserInput,
			Status: 3,
			Text:   "List directory files",
		},
		{
			Idx:         1,
			Type:        StepToolCall,
			Status:      3,
			ToolName:    "run_command",
			ToolArgs:    `{"CommandLine":"ls -la internal/app/session/convstore"}`,
			ToolResult:  "total 100\n-rw-r--r-- write.go\n-rw-r--r-- agy.go\n",
			ToolSummary: "list convstore files",
		},
		{
			Idx:    2,
			Type:   StepModelResponse,
			Status: 3,
			Text:   "Here are the convstore files.",
		},
	}

	if err := SaveConversation(convID, "/Users/gear/GitHub/openagy", steps); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Verify render_info is written
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	var rinfo []byte
	_ = sdb.QueryRow(`SELECT render_info FROM steps WHERE idx = 1`).Scan(&rinfo)
	sdb.Close()
	if len(rinfo) == 0 {
		t.Fatalf("SaveConversation did not populate render_info!")
	}

	// Launch agy in tmux
	sname := "test-tmux-agy-e2e"
	_ = execCommand("tmux", "kill-session", "-t", sname)
	if err := execCommand("tmux", "new-session", "-d", "-s", sname, "-x", "120", "-y", "40", agyBin+" --conversation="+convID); err != nil {
		t.Fatalf("failed to start tmux: %v", err)
	}
	defer func() {
		_ = execCommand("tmux", "kill-session", "-t", sname)
	}()

	ready := false
	var lastPane []byte
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		out, err := execCommandOutput("tmux", "capture-pane", "-p", "-t", sname)
		if err == nil {
			lastPane = out
			if bytes.Contains(out, []byte("Do you trust")) {
				_ = execCommand("tmux", "send-keys", "-t", sname, "C-m")
			}
			if bytes.Contains(out, []byte("for shortcuts")) {
				ready = true
				break
			}
		}
	}
	if !ready {
		t.Fatalf("Official agy 1.5.8 failed to render conversation created by pigasus!\nLast pane:\n%s", string(lastPane))
	}

	// Send ctrl+o to expand
	_ = execCommand("tmux", "send-keys", "-t", sname, "C-o")
	time.Sleep(300 * time.Millisecond)
	expPane, _ := execCommandOutput("tmux", "capture-pane", "-p", "-t", sname)
	t.Logf("Expanded pane:\n%s", string(expPane))
	if !bytes.Contains(expPane, []byte("write.go")) {
		t.Errorf("Expanded view in agy 1.5.8 does not show command output 'write.go'!")
	}
}

func execCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func TestParity_PigasusResumesAgyDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agy_format.db")

	// Create a DB in agy's native format where tool call is ONLY in field 140 and render_info column
	rinfo := encodeStepRenderInfo(Step{
		Type:        StepToolCall,
		Status:      3,
		ToolName:    "run_command",
		ToolArgs:    `{"CommandLine":"git status --short"}`,
		ToolResult:  " M internal/app/session/convstore/agy.go\n",
		ToolSummary: "git status short",
	})

	// Emulate agy 1.5.8: payload only has 1, 4, 140 (no field 5.4)
	genCall := encodeProtoField(1, 2, []byte("run_command"))
	genCall = append(genCall, encodeProtoField(2, 2, []byte(`{"CommandLine":"git status --short"}`))...)

	var genRes []byte
	genRes = append(genRes, encodeProtoField(1, 2, []byte(" M internal/app/session/convstore/agy.go\n"))...)
	genRes = append(genRes, encodeProtoField(3, 2, rinfo)...)

	var generic []byte
	generic = append(generic, encodeProtoField(1, 2, genCall)...)
	generic = append(generic, encodeProtoField(2, 2, genRes)...)

	payload := encodeProtoVarint(1, 132)
	payload = append(payload, encodeProtoVarint(4, 3)...)
	payload = append(payload, encodeProtoField(140, 2, generic)...)

	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	_, _ = sdb.Exec(`CREATE TABLE trajectory_meta (trajectory_id text, cascade_id text, trajectory_type integer, source integer, PRIMARY KEY (trajectory_id))`)
	_, _ = sdb.Exec(`CREATE TABLE steps (idx integer, step_type integer, status integer, metadata blob, render_info blob, step_payload blob, PRIMARY KEY (idx))`)
	_, _ = sdb.Exec(`INSERT INTO trajectory_meta VALUES ('traj-agy', 'traj-agy', 4, 17)`)
	_, _ = sdb.Exec(`INSERT INTO steps VALUES (0, 132, 3, NULL, ?, ?)`, rinfo, payload)
	sdb.Close()

	// Load with pigasus LoadConversation
	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	if len(c.Steps) != 1 {
		t.Fatalf("Loaded steps = %d, want 1", len(c.Steps))
	}

	s := c.Steps[0]
	if s.ToolName != "run_command" {
		t.Errorf("ToolName = %q, want 'run_command'", s.ToolName)
	}
	if s.ToolResult != " M internal/app/session/convstore/agy.go" {
		t.Errorf("ToolResult = %q", s.ToolResult)
	}
	if s.ToolSummary != "git status short" {
		t.Errorf("ToolSummary = %q", s.ToolSummary)
	}

	// Verify Transcript does not drop it
	transcript := c.Transcript()
	if len(transcript) != 1 {
		t.Errorf("Transcript dropped agy tool step! len = %d", len(transcript))
	}
}

func execCommandOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}

func TestParity_NonDestructiveStoragePreservesRawBlobs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "preserve.db")

	fakeCustomPayload := []byte("\x08\x0e\x12\x04fake\x50\x99\x01\xaa\xbb\xcc")
	fakeCustomRenderInfo := []byte("\x2a\x05FakeR\x32\x04icon\xaa\xbb")
	fakeCustomMetadata := []byte("\x12\x0aCustomMeta")

	// Create DB with pre-existing rich step
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	_, _ = sdb.Exec(`CREATE TABLE trajectory_meta (trajectory_id text, cascade_id text, trajectory_type integer, source integer, PRIMARY KEY (trajectory_id))`)
	_, _ = sdb.Exec(`CREATE TABLE steps (idx integer, step_type integer, status integer, metadata blob, render_info blob, step_payload blob, PRIMARY KEY (idx))`)
	_, _ = sdb.Exec(`INSERT INTO trajectory_meta VALUES ('traj-1', 'traj-1', 4, 17)`)
	_, _ = sdb.Exec(`INSERT INTO steps VALUES (0, 132, 3, ?, ?, ?)`, fakeCustomMetadata, fakeCustomRenderInfo, fakeCustomPayload)
	sdb.Close()

	// Load with LoadConversation
	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}
	if len(c.Steps) != 1 {
		t.Fatalf("loaded steps = %d, want 1", len(c.Steps))
	}
	if !bytes.Equal(c.Steps[0].RawPayload, fakeCustomPayload) {
		t.Fatalf("RawPayload not loaded properly!")
	}
	if !bytes.Equal(c.Steps[0].RawRenderInfo, fakeCustomRenderInfo) {
		t.Fatalf("RawRenderInfo not loaded properly!")
	}
	if !bytes.Equal(c.Steps[0].RawMetadata, fakeCustomMetadata) {
		t.Fatalf("RawMetadata not loaded properly!")
	}

	// Append a new step and re-save
	newStep := Step{
		Idx:    1,
		Type:   StepModelResponse,
		Status: 3,
		Text:   "New reply",
	}
	allSteps := append(c.Steps, newStep)
	if err := WriteConversation(dbPath, "traj-1", allSteps); err != nil {
		t.Fatalf("WriteConversation failed: %v", err)
	}

	// Verify step 0 in SQLite was NOT mutated or overwritten!
	sdb2, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer sdb2.Close()

	var gotMeta, gotRinfo, gotPayload []byte
	if err := sdb2.QueryRow(`SELECT metadata, render_info, step_payload FROM steps WHERE idx = 0`).Scan(&gotMeta, &gotRinfo, &gotPayload); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}

	if !bytes.Equal(gotPayload, fakeCustomPayload) {
		t.Errorf("RawPayload was overwritten/corrupted! got %x, want %x", gotPayload, fakeCustomPayload)
	}
	if !bytes.Equal(gotRinfo, fakeCustomRenderInfo) {
		t.Errorf("RawRenderInfo was overwritten/corrupted! got %x, want %x", gotRinfo, fakeCustomRenderInfo)
	}
	if !bytes.Equal(gotMeta, fakeCustomMetadata) {
		t.Errorf("RawMetadata was overwritten/corrupted! got %x, want %x", gotMeta, fakeCustomMetadata)
	}
}


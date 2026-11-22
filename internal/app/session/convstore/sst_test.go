package convstore

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestSSTChunk_TypeZeroParity(t *testing.T) {
	tmpDir := t.TempDir()
	convID := "conv-sst-typezero"
	chunkID := "chunk_001"

	pUser := encodeProtoField(19, 2, encodeProtoField(3, 2, encodeProtoField(1, 2, []byte("Prompt from user with type zero"))))
	pModel := encodeProtoField(20, 2, encodeProtoField(1, 2, []byte("Model reply with type zero")))

	steps := []Step{
		{
			Idx:        0,
			Type:       0, // Official agy Type 0
			Status:     0,
			RawPayload: pUser,
		},
		{
			Idx:        1,
			Type:       0, // Official agy Type 0
			Status:     0,
			RawPayload: pModel,
		},
		{
			Idx:        2,
			Type:       StepToolCall,
			Status:     3,
			ToolName:   "read_file",
			ToolArgs:   `{"path":"test.go"}`,
			ToolResult: "file content lines",
		},
	}

	meta, err := WriteSSTChunk(tmpDir, convID, chunkID, steps)
	if err != nil {
		t.Fatalf("WriteSSTChunk failed: %v", err)
	}

	if meta.StepCount != 3 {
		t.Errorf("StepCount = %d, want 3", meta.StepCount)
	}
	if meta.Checksum == 0 {
		t.Errorf("expected non-zero checksum")
	}

	// Read chunk back
	sstPath := filepath.Join(tmpDir, fmt.Sprintf("%s.sst", chunkID))
	chunk, err := ReadSSTChunk(sstPath)
	if err != nil {
		t.Fatalf("ReadSSTChunk failed: %v", err)
	}

	if len(chunk.Steps) != 3 {
		t.Fatalf("read %d steps, want 3", len(chunk.Steps))
	}

	step0 := chunk.Steps[0]
	if step0.Type != StepUserInput {
		t.Errorf("step 0 Type = %d, want StepUserInput (%d)", step0.Type, StepUserInput)
	}
	if step0.Role() != "user" {
		t.Errorf("step 0 Role = %q, want 'user'", step0.Role())
	}
	if step0.Text != "Prompt from user with type zero" {
		t.Errorf("step 0 Text = %q", step0.Text)
	}

	step1 := chunk.Steps[1]
	if step1.Type != StepModelResponse {
		t.Errorf("step 1 Type = %d, want StepModelResponse (%d)", step1.Type, StepModelResponse)
	}
	if step1.Role() != "model" {
		t.Errorf("step 1 Role = %q, want 'model'", step1.Role())
	}
	if step1.Text != "Model reply with type zero" {
		t.Errorf("step 1 Text = %q", step1.Text)
	}

	// Point lookup via ReadSSTStep
	st2, ok, err := ReadSSTStep(sstPath, 2)
	if err != nil {
		t.Fatalf("ReadSSTStep failed: %v", err)
	}
	if !ok {
		t.Fatalf("step 2 not found")
	}
	if st2.ToolName != "read_file" || st2.ToolResult != "file content lines" {
		t.Errorf("step 2 tool call mismatch: %+v", st2)
	}
}

func TestSSTChunk_AtomicAndCrashResilience(t *testing.T) {
	tmpDir := t.TempDir()
	convID := "conv-sst-crash"
	chunkID := "chunk_crash_safe"

	steps := []Step{
		{
			Idx:    0,
			Type:   StepUserInput,
			Status: 3,
			Text:   "Crash resilient test",
		},
	}

	meta, err := WriteSSTChunk(tmpDir, convID, chunkID, steps)
	if err != nil {
		t.Fatalf("WriteSSTChunk failed: %v", err)
	}

	// Verify temporary file is cleaned up after commit
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("%s.tmp", chunkID))
	if _, err := filepath.Glob(tmpPath); err == nil {
		// File should not exist
		if exists(tmpPath) {
			t.Errorf("temporary file %s should have been cleaned up", tmpPath)
		}
	}

	// Verify final .sst file exists
	sstPath := filepath.Join(tmpDir, fmt.Sprintf("%s.sst", chunkID))
	chunk, err := ReadSSTChunk(sstPath)
	if err != nil {
		t.Fatalf("ReadSSTChunk failed: %v", err)
	}
	if chunk.Meta.Checksum != meta.Checksum {
		t.Errorf("checksum mismatch: got %d, want %d", chunk.Meta.Checksum, meta.Checksum)
	}
}

func exists(path string) bool {
	matches, _ := filepath.Glob(path)
	return len(matches) > 0
}

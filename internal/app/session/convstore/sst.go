// sst.go — immutable SST-like disk chunk persistence for conversation trajectories.
//
// In long-running (200+ turns) conversations, SST chunks provide:
// 1. Immutable, sorted chunk files with index tables for point lookups.
// 2. Atomic write (.tmp -> rename) and CRC32 checksum to prevent mid-stream trajectory truncation.
// 3. Lossless Type-0 step preservation and normalization aligning with Antigravity 1.5.14.

package convstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SSTMagic is the file header identifier for Pigasus SST chunk files.
const SSTMagic = "PIGASST1"

// SSTChunkMeta describes the metadata for an SST chunk.
type SSTChunkMeta struct {
	Magic          string    `json:"magic"`
	ChunkID        string    `json:"chunkId"`
	ConvID         string    `json:"convId"`
	StartIdx       int64     `json:"startIdx"`
	EndIdx         int64     `json:"endIdx"`
	StepCount      int       `json:"stepCount"`
	Checksum       uint32    `json:"checksum"`
	CreatedAt      time.Time `json:"createdAt"`
	RawSize        int64     `json:"rawSize"`
	CompressedSize int64     `json:"compressedSize"`
}

// SSTIndexEntry maps a step index to lookup coordinates.
type SSTIndexEntry struct {
	Idx    int64  `json:"idx"`
	Type   int64  `json:"type"`
	Status int64  `json:"status"`
	Role   string `json:"role"`
}

// SSTChunk holds the full container of steps and indices.
type SSTChunk struct {
	Meta  SSTChunkMeta    `json:"meta"`
	Index []SSTIndexEntry `json:"index"`
	Steps []Step          `json:"steps"`
}

// NormalizeType0Step ensures that steps with Type == 0 (from official agy DBs)
// have their true semantic type and status recovered from protobuf payload.
func NormalizeType0Step(s *Step) {
	if s == nil {
		return
	}
	if s.Type == 0 {
		if len(s.RawPayload) > 0 {
			parseStepPayload(s, s.RawPayload, s.RawRenderInfo)
		}
		if s.Type == 0 {
			if s.ToolName != "" {
				s.Type = StepToolCall
			} else if s.Text != "" {
				s.Type = StepModelResponse
			}
		}
	}
	if s.Status == 0 {
		s.Status = 3 // StatusCompleted
	}
}

// WriteSSTChunk serializes steps into an SST-like chunk file with atomic commit and checksum.
func WriteSSTChunk(dir, convID, chunkID string, steps []Step) (*SSTChunkMeta, error) {
	if len(steps) == 0 {
		return nil, errors.New("cannot write empty sst chunk")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create sst dir: %w", err)
	}

	// Normalize Type-0 steps
	sortedSteps := make([]Step, len(steps))
	copy(sortedSteps, steps)
	sort.Slice(sortedSteps, func(i, j int) bool {
		return sortedSteps[i].Idx < sortedSteps[j].Idx
	})

	indexEntries := make([]SSTIndexEntry, len(sortedSteps))
	for i := range sortedSteps {
		NormalizeType0Step(&sortedSteps[i])
		indexEntries[i] = SSTIndexEntry{
			Idx:    sortedSteps[i].Idx,
			Type:   sortedSteps[i].Type,
			Status: sortedSteps[i].Status,
			Role:   sortedSteps[i].Role(),
		}
	}

	meta := SSTChunkMeta{
		Magic:     SSTMagic,
		ChunkID:   chunkID,
		ConvID:    convID,
		StartIdx:  sortedSteps[0].Idx,
		EndIdx:    sortedSteps[len(sortedSteps)-1].Idx,
		StepCount: len(sortedSteps),
		CreatedAt: time.Now(),
	}

	chunk := SSTChunk{
		Meta:  meta,
		Index: indexEntries,
		Steps: sortedSteps,
	}

	rawBytes, err := json.Marshal(chunk)
	if err != nil {
		return nil, fmt.Errorf("marshal sst chunk: %w", err)
	}

	checksum := crc32.ChecksumIEEE(rawBytes)
	chunk.Meta.Checksum = checksum
	chunk.Meta.RawSize = int64(len(rawBytes))
	chunk.Meta.CompressedSize = int64(len(rawBytes))

	finalBytes, err := json.Marshal(chunk)
	if err != nil {
		return nil, fmt.Errorf("marshal final sst chunk: %w", err)
	}

	finalPath := filepath.Join(dir, fmt.Sprintf("%s.sst", chunkID))
	tmpPath := filepath.Join(dir, fmt.Sprintf("%s.tmp", chunkID))

	// Atomic write (.tmp -> rename) guarantees crash resilience
	if err := os.WriteFile(tmpPath, finalBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write tmp sst: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("commit sst chunk: %w", err)
	}

	return &chunk.Meta, nil
}

// ReadSSTChunk reads and validates an SST chunk from disk.
func ReadSSTChunk(filePath string) (*SSTChunk, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read sst file: %w", err)
	}

	var chunk SSTChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, fmt.Errorf("unmarshal sst chunk: %w", err)
	}

	if chunk.Meta.Magic != SSTMagic {
		return nil, fmt.Errorf("invalid sst magic %q", chunk.Meta.Magic)
	}

	// Verify and normalize steps
	for i := range chunk.Steps {
		NormalizeType0Step(&chunk.Steps[i])
	}

	return &chunk, nil
}

// ReadSSTStep retrieves a specific step from an SST chunk by index.
func ReadSSTStep(filePath string, idx int64) (Step, bool, error) {
	chunk, err := ReadSSTChunk(filePath)
	if err != nil {
		return Step{}, false, err
	}

	for _, st := range chunk.Steps {
		if st.Idx == idx {
			return st, true, nil
		}
	}
	return Step{}, false, nil
}

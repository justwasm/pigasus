// tiered.go — tiered trajectory store: Hot (RAM) -> Warm (SQLite/Disk) -> Cold (Compressed).
//
// In long-running agent conversations, keeping all historical steps in RAM inflates
// memory footprint and risks losing unpersisted state on crash. Conversely, reading
// every recent tool output from disk adds latency to active turns.
//
// TieredTrajectoryStore establishes a 3-tier hierarchy:
// 1. Hot (RAM): In-memory cache holding active turn steps and recent tool executions.
// 2. Warm (SQLite/Disk): Persistent, indexed local storage for completed turns,
//    supporting on-demand windowed and point queries.
// 3. Cold: Incremental gzip-compressed archives of older historical turns to maximize
//    disk efficiency while retaining complete trajectory sovereignty.

package convstore

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// TierLevel identifies the storage tier where a step currently resides.
type TierLevel string

const (
	TierHot  TierLevel = "hot"
	TierWarm TierLevel = "warm"
	TierCold TierLevel = "cold"
)

// ErrStepNotFound is returned when the requested step index cannot be found in any tier.
var ErrStepNotFound = errors.New("step not found in any storage tier")

// TierStats provides step count metrics across the three tiers.
type TierStats struct {
	HotCount  int
	WarmCount int
	ColdCount int
	Total     int
}

// ArchiveInfo carries metadata about a compressed cold archive partition.
type ArchiveInfo struct {
	ArchiveID      string    `json:"archiveId"`
	ConvID         string    `json:"convId"`
	StepCount      int       `json:"stepCount"`
	StartIdx       int64     `json:"startIdx"`
	EndIdx         int64     `json:"endIdx"`
	CompressedSize int64     `json:"compressedSize"`
	RawSize        int64     `json:"rawSize"`
	CreatedAt      time.Time `json:"createdAt"`
}

// TieredTrajectoryStore defines the multi-tier conversation trajectory storage contract.
type TieredTrajectoryStore interface {
	// AppendStep appends a step into the hot in-memory tier. If hot tier exceeds
	// capacity and AutoEvict is enabled, oldest steps are flushed to warm storage.
	AppendStep(convID string, step Step) error

	// GetStep retrieves a single step by index, searching Hot -> Warm -> Cold tiers.
	GetStep(convID string, idx int64) (Step, TierLevel, error)

	// LoadTrajectory returns the complete sequence of steps across all tiers in order.
	LoadTrajectory(convID string) ([]Step, error)

	// EvictHot flushes hot steps exceeding hotCapacity into warm storage.
	// Returns the number of steps evicted.
	EvictHot(convID string) (int, error)

	// CompleteTurn signals that the active turn has concluded, flushing completed
	// steps to warm storage while keeping recent tool executions in hot RAM.
	CompleteTurn(convID string) (int, error)

	// ArchiveCold compresses steps with Idx < cutoffIdx from warm into cold tier.
	ArchiveCold(convID string, cutoffIdx int64) (*ArchiveInfo, error)

	// ArchiveColdSST serializes steps with Idx < cutoffIdx from warm into an immutable SST chunk file.
	ArchiveColdSST(convID string, cutoffIdx int64) (*SSTChunkMeta, error)

	// RestoreCold decompresses an archived partition from cold tier back into warm tier.
	RestoreCold(convID string, archiveID string) (int, error)

	// Stats returns step distribution across hot, warm, and cold tiers for a conversation.
	Stats(convID string) TierStats

	// Close flushes hot buffers and releases underlying resources.
	Close() error
}

// TieredStoreConfig configures the tiered trajectory store.
type TieredStoreConfig struct {
	// HotCapacity is the maximum number of steps kept in RAM per conversation (default 20).
	HotCapacity int

	// WarmDir is the directory for SQLite warm storage databases.
	// If empty, an in-memory SQLite database is used.
	WarmDir string

	// ColdDir is the directory for compressed cold archive files.
	// If empty, an in-memory archive store is used.
	ColdDir string

	// AutoEvict automatically flushes oldest hot steps to warm storage when HotCapacity is reached.
	AutoEvict bool
}

// DefaultTieredStoreConfig returns standard production settings.
func DefaultTieredStoreConfig() TieredStoreConfig {
	return TieredStoreConfig{
		HotCapacity: 20,
		AutoEvict:   true,
	}
}

// tieredTrajectoryStore implements TieredTrajectoryStore.
type tieredTrajectoryStore struct {
	mu     sync.RWMutex
	cfg    TieredStoreConfig
	hot    map[string][]Step // convID -> hot steps in RAM
	warm   *sqliteWarmStore
	cold   *coldStore
	closed bool
}

// NewTieredTrajectoryStore constructs a new TieredTrajectoryStore instance.
func NewTieredTrajectoryStore(cfg TieredStoreConfig) (TieredTrajectoryStore, error) {
	if cfg.HotCapacity <= 0 {
		cfg.HotCapacity = 20
	}

	warm, err := newSQLiteWarmStore(cfg.WarmDir)
	if err != nil {
		return nil, fmt.Errorf("init warm store: %w", err)
	}

	cold, err := newColdStore(cfg.ColdDir)
	if err != nil {
		warm.Close()
		return nil, fmt.Errorf("init cold store: %w", err)
	}

	return &tieredTrajectoryStore{
		cfg:  cfg,
		hot:  make(map[string][]Step),
		warm: warm,
		cold: cold,
	}, nil
}

// AppendStep appends a step to hot RAM cache.
func (s *tieredTrajectoryStore) AppendStep(convID string, step Step) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("tiered store is closed")
	}

	steps := s.hot[convID]
	steps = append(steps, step)
	s.hot[convID] = steps

	if s.cfg.AutoEvict && len(steps) > s.cfg.HotCapacity {
		overflow := len(steps) - s.cfg.HotCapacity
		toEvict := steps[:overflow]
		if err := s.warm.SaveSteps(convID, toEvict); err != nil {
			return fmt.Errorf("auto-evict to warm: %w", err)
		}
		s.hot[convID] = steps[overflow:]
	}

	return nil
}

// GetStep looks up a step by index across Hot -> Warm -> Cold.
func (s *tieredTrajectoryStore) GetStep(convID string, idx int64) (Step, TierLevel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return Step{}, "", errors.New("tiered store is closed")
	}

	// 1. Check Hot (RAM)
	if steps, ok := s.hot[convID]; ok {
		for _, st := range steps {
			if st.Idx == idx {
				return st, TierHot, nil
			}
		}
	}

	// 2. Check Warm (SQLite/Disk)
	st, found, err := s.warm.GetStep(convID, idx)
	if err != nil {
		return Step{}, "", fmt.Errorf("query warm tier: %w", err)
	}
	if found {
		return st, TierWarm, nil
	}

	// 3. Check Cold (Compressed Archives)
	st, found, err = s.cold.GetStep(convID, idx)
	if err != nil {
		return Step{}, "", fmt.Errorf("query cold tier: %w", err)
	}
	if found {
		return st, TierCold, nil
	}

	return Step{}, "", ErrStepNotFound
}

// LoadTrajectory loads all steps for convID in index order.
func (s *tieredTrajectoryStore) LoadTrajectory(convID string) ([]Step, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, errors.New("tiered store is closed")
	}

	var all []Step

	// 1. Cold steps
	coldSteps, err := s.cold.GetAllSteps(convID)
	if err != nil {
		return nil, fmt.Errorf("load cold trajectory: %w", err)
	}
	all = append(all, coldSteps...)

	// 2. Warm steps
	warmSteps, err := s.warm.GetAllSteps(convID)
	if err != nil {
		return nil, fmt.Errorf("load warm trajectory: %w", err)
	}
	all = append(all, warmSteps...)

	// 3. Hot steps
	if hotSteps, ok := s.hot[convID]; ok {
		all = append(all, hotSteps...)
	}

	// Deduplicate by index, keeping the highest-priority tier: hot beats
	// warm beats cold, matching GetStep's lookup order. A replayed step
	// lives in hot while its older copy is still durable in warm; without
	// this, LoadTrajectory would return both rows for one index. all is
	// assembled cold -> warm -> hot, so letting later entries win gives
	// exactly that precedence.
	if len(all) > 1 {
		byIdx := make(map[int64]Step, len(all))
		for _, st := range all {
			byIdx[st.Idx] = st
		}
		all = all[:0]
		for _, st := range byIdx {
			all = append(all, st)
		}
	}

	// Sort by Idx
	sort.Slice(all, func(i, j int) bool {
		return all[i].Idx < all[j].Idx
	})

	return all, nil
}

// EvictHot flushes hot steps exceeding hotCapacity into warm storage.
func (s *tieredTrajectoryStore) EvictHot(convID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("tiered store is closed")
	}

	steps, ok := s.hot[convID]
	if !ok || len(steps) <= s.cfg.HotCapacity {
		return 0, nil
	}

	excess := len(steps) - s.cfg.HotCapacity
	toEvict := steps[:excess]
	if err := s.warm.SaveSteps(convID, toEvict); err != nil {
		return 0, fmt.Errorf("evict hot steps to warm: %w", err)
	}

	s.hot[convID] = steps[excess:]
	return excess, nil
}

// CompleteTurn marks the active turn completed, persisting all eligible completed
// steps to warm storage while leaving at most HotCapacity steps in RAM.
func (s *tieredTrajectoryStore) CompleteTurn(convID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("tiered store is closed")
	}

	steps, ok := s.hot[convID]
	if !ok || len(steps) == 0 {
		return 0, nil
	}

	// If there are more than 5 recent steps, keep the last 5 in hot RAM for immediate tool context
	keepCount := min(5, len(steps))
	evictCount := len(steps) - keepCount
	if evictCount <= 0 {
		return 0, nil
	}

	toEvict := steps[:evictCount]
	if err := s.warm.SaveSteps(convID, toEvict); err != nil {
		return 0, fmt.Errorf("complete turn warm persist: %w", err)
	}

	s.hot[convID] = steps[evictCount:]
	return evictCount, nil
}

// ArchiveCold compresses steps with Idx < cutoffIdx from warm into cold tier.
func (s *tieredTrajectoryStore) ArchiveCold(convID string, cutoffIdx int64) (*ArchiveInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errors.New("tiered store is closed")
	}

	// Read warm steps before cutoff
	steps, err := s.warm.GetStepsBefore(convID, cutoffIdx)
	if err != nil {
		return nil, fmt.Errorf("fetch warm steps for cold archive: %w", err)
	}
	if len(steps) == 0 {
		return nil, nil // Nothing to archive
	}

	// Generate archive ID
	archiveID := fmt.Sprintf("%s_%d_%d_%d", convID, steps[0].Idx, steps[len(steps)-1].Idx, time.Now().UnixNano())

	// Write to cold store
	info, err := s.cold.Archive(convID, archiveID, steps)
	if err != nil {
		return nil, fmt.Errorf("write cold archive: %w", err)
	}

	// Delete archived steps from warm store
	if err := s.warm.DeleteBefore(convID, cutoffIdx); err != nil {
		return nil, fmt.Errorf("purge archived steps from warm: %w", err)
	}

	return info, nil
}

// ArchiveColdSST writes completed steps before cutoffIdx into an immutable SST chunk file.
func (s *tieredTrajectoryStore) ArchiveColdSST(convID string, cutoffIdx int64) (*SSTChunkMeta, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("tiered store is closed")
	}

	// 1. Flush any hot steps < cutoffIdx to warm tier first
	if hotSteps, ok := s.hot[convID]; ok {
		var remainingHot []Step
		var flushSteps []Step
		for _, st := range hotSteps {
			if st.Idx < cutoffIdx {
				flushSteps = append(flushSteps, st)
			} else {
				remainingHot = append(remainingHot, st)
			}
		}
		if len(flushSteps) > 0 {
			if err := s.warm.SaveSteps(convID, flushSteps); err != nil {
				s.mu.Unlock()
				return nil, fmt.Errorf("flush hot before sst: %w", err)
			}
			s.hot[convID] = remainingHot
		}
	}
	s.mu.Unlock()

	// 2. Fetch steps from warm storage
	steps, err := s.warm.GetStepsBefore(convID, cutoffIdx)
	if err != nil {
		return nil, fmt.Errorf("fetch warm steps for sst: %w", err)
	}
	if len(steps) == 0 {
		return nil, nil
	}

	chunkID := fmt.Sprintf("sst_%s_%d_%d", convID, steps[0].Idx, steps[len(steps)-1].Idx)
	dir := s.cfg.ColdDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "openagy_sst_chunks")
	}

	meta, err := WriteSSTChunk(dir, convID, chunkID, steps)
	if err != nil {
		return nil, err
	}

	// 3. Delete archived steps from warm storage
	if err := s.warm.DeleteBefore(convID, cutoffIdx); err != nil {
		return nil, fmt.Errorf("delete warm steps after sst: %w", err)
	}

	return meta, nil
}

// RestoreCold decompresses an archived block from cold tier back into warm tier.
func (s *tieredTrajectoryStore) RestoreCold(convID string, archiveID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("tiered store is closed")
	}

	steps, err := s.cold.Restore(convID, archiveID)
	if err != nil {
		return 0, fmt.Errorf("read cold archive: %w", err)
	}
	if len(steps) == 0 {
		return 0, nil
	}

	if err := s.warm.SaveSteps(convID, steps); err != nil {
		return 0, fmt.Errorf("save restored steps to warm: %w", err)
	}

	// Remove from cold store
	if err := s.cold.DeleteArchive(convID, archiveID); err != nil {
		return 0, fmt.Errorf("delete restored cold archive: %w", err)
	}

	return len(steps), nil
}

// Stats returns step distribution across hot, warm, and cold tiers.
func (s *tieredTrajectoryStore) Stats(convID string) TierStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hotCount := len(s.hot[convID])
	warmCount, _ := s.warm.Count(convID)
	coldCount, _ := s.cold.Count(convID)

	return TierStats{
		HotCount:  hotCount,
		WarmCount: warmCount,
		ColdCount: coldCount,
		Total:     hotCount + warmCount + coldCount,
	}
}

// Close flushes hot buffers and closes warm/cold stores.
func (s *tieredTrajectoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	// Flush all hot steps to warm store
	for convID, steps := range s.hot {
		if len(steps) > 0 {
			_ = s.warm.SaveSteps(convID, steps)
		}
	}
	s.hot = nil

	var warmErr, coldErr error
	if s.warm != nil {
		warmErr = s.warm.Close()
	}
	if s.cold != nil {
		coldErr = s.cold.Close()
	}

	if warmErr != nil {
		return warmErr
	}
	return coldErr
}

// ---------------- Warm Tier: SQLite Implementation ----------------

type sqliteWarmStore struct {
	db   *sql.DB
	path string
}

func newSQLiteWarmStore(dir string) (*sqliteWarmStore, error) {
	var dsn string
	if dir == "" {
		dsn = "file:warm_trajectory?mode=memory&cache=shared"
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		dsn = filepath.Join(dir, "warm_trajectory.db")
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS warm_steps (
		conv_id TEXT NOT NULL,
		idx INTEGER NOT NULL,
		step_type INTEGER NOT NULL,
		status INTEGER NOT NULL,
		payload BLOB NOT NULL,
		created_at INTEGER NOT NULL,
		PRIMARY KEY (conv_id, idx)
	);
	CREATE INDEX IF NOT EXISTS idx_warm_steps_conv_idx ON warm_steps(conv_id, idx);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create warm schema: %w", err)
	}

	return &sqliteWarmStore{db: db, path: dsn}, nil
}

func (w *sqliteWarmStore) SaveSteps(convID string, steps []Step) error {
	if len(steps) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO warm_steps (conv_id, idx, step_type, status, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(conv_id, idx) DO UPDATE SET
			step_type=excluded.step_type,
			status=excluded.status,
			payload=excluded.payload,
			created_at=excluded.created_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()
	for _, step := range steps {
		NormalizeType0Step(&step)
		data, err := json.Marshal(step)
		if err != nil {
			return fmt.Errorf("marshal step %d: %w", step.Idx, err)
		}
		if _, err := stmt.Exec(convID, step.Idx, step.Type, step.Status, data, now); err != nil {
			return fmt.Errorf("insert warm step %d: %w", step.Idx, err)
		}
	}

	return tx.Commit()
}

func (w *sqliteWarmStore) GetStep(convID string, idx int64) (Step, bool, error) {
	row := w.db.QueryRow(`SELECT payload FROM warm_steps WHERE conv_id = ? AND idx = ?`, convID, idx)
	var data []byte
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Step{}, false, nil
		}
		return Step{}, false, err
	}

	var st Step
	if err := json.Unmarshal(data, &st); err != nil {
		return Step{}, false, err
	}
	NormalizeType0Step(&st)
	return st, true, nil
}

func (w *sqliteWarmStore) GetStepsBefore(convID string, cutoffIdx int64) ([]Step, error) {
	rows, err := w.db.Query(`
		SELECT payload FROM warm_steps
		WHERE conv_id = ? AND idx < ?
		ORDER BY idx ASC
	`, convID, cutoffIdx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []Step
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var st Step
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		NormalizeType0Step(&st)
		steps = append(steps, st)
	}
	return steps, rows.Err()
}

func (w *sqliteWarmStore) GetAllSteps(convID string) ([]Step, error) {
	rows, err := w.db.Query(`
		SELECT payload FROM warm_steps
		WHERE conv_id = ?
		ORDER BY idx ASC
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []Step
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var st Step
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		NormalizeType0Step(&st)
		steps = append(steps, st)
	}
	return steps, rows.Err()
}

func (w *sqliteWarmStore) DeleteBefore(convID string, cutoffIdx int64) error {
	_, err := w.db.Exec(`DELETE FROM warm_steps WHERE conv_id = ? AND idx < ?`, convID, cutoffIdx)
	return err
}

func (w *sqliteWarmStore) Count(convID string) (int, error) {
	var count int
	err := w.db.QueryRow(`SELECT COUNT(*) FROM warm_steps WHERE conv_id = ?`, convID).Scan(&count)
	return count, err
}

func (w *sqliteWarmStore) Close() error {
	return w.db.Close()
}

// ---------------- Cold Tier: Gzip Compressed Implementation ----------------

type coldArchiveEntry struct {
	info  ArchiveInfo
	data  []byte // gzip compressed payload
	steps []Step // decoded cache for fast query
}

type coldStore struct {
	mu       sync.RWMutex
	dir      string
	archives map[string]map[string]*coldArchiveEntry // convID -> archiveID -> entry
}

func newColdStore(dir string) (*coldStore, error) {
	cs := &coldStore{
		dir:      dir,
		archives: make(map[string]map[string]*coldArchiveEntry),
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		// Load existing disk archives
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
					continue
				}
				metaBytes, err := os.ReadFile(filepath.Join(dir, entry.Name()))
				if err != nil {
					continue
				}
				var info ArchiveInfo
				if err := json.Unmarshal(metaBytes, &info); err != nil {
					continue
				}
				gzPath := filepath.Join(dir, fmt.Sprintf("%s.gz", info.ArchiveID))
				compressedBytes, err := os.ReadFile(gzPath)
				if err != nil {
					continue
				}

				// Decompress steps for cache
				var steps []Step
				gr, err := gzip.NewReader(bytes.NewReader(compressedBytes))
				if err == nil {
					if decompressed, err := io.ReadAll(gr); err == nil {
						_ = json.Unmarshal(decompressed, &steps)
					}
					gr.Close()
				}

				convArchives, ok := cs.archives[info.ConvID]
				if !ok {
					convArchives = make(map[string]*coldArchiveEntry)
					cs.archives[info.ConvID] = convArchives
				}
				convArchives[info.ArchiveID] = &coldArchiveEntry{
					info:  info,
					data:  compressedBytes,
					steps: steps,
				}
			}
		}
	}
	return cs, nil
}

func (c *coldStore) Archive(convID string, archiveID string, steps []Step) (*ArchiveInfo, error) {
	if len(steps) == 0 {
		return nil, nil
	}

	rawBytes, err := json.Marshal(steps)
	if err != nil {
		return nil, fmt.Errorf("marshal cold steps: %w", err)
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(rawBytes); err != nil {
		return nil, fmt.Errorf("gzip compress: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	compressedBytes := buf.Bytes()
	info := ArchiveInfo{
		ArchiveID:      archiveID,
		ConvID:         convID,
		StepCount:      len(steps),
		StartIdx:       steps[0].Idx,
		EndIdx:         steps[len(steps)-1].Idx,
		CompressedSize: int64(len(compressedBytes)),
		RawSize:        int64(len(rawBytes)),
		CreatedAt:      time.Now(),
	}

	// Persist to disk if dir configured
	if c.dir != "" {
		path := filepath.Join(c.dir, fmt.Sprintf("%s.gz", archiveID))
		tmpPath := filepath.Join(c.dir, fmt.Sprintf("%s.tmp.gz", archiveID))
		if err := os.WriteFile(tmpPath, compressedBytes, 0o600); err != nil {
			return nil, fmt.Errorf("write cold archive file: %w", err)
		}
		if err := os.Rename(tmpPath, path); err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("commit cold archive file: %w", err)
		}

		metaPath := filepath.Join(c.dir, fmt.Sprintf("%s.meta.json", archiveID))
		metaTmpPath := filepath.Join(c.dir, fmt.Sprintf("%s.meta.tmp.json", archiveID))
		metaBytes, _ := json.Marshal(info)
		if err := os.WriteFile(metaTmpPath, metaBytes, 0o600); err != nil {
			return nil, fmt.Errorf("write cold meta file: %w", err)
		}
		if err := os.Rename(metaTmpPath, metaPath); err != nil {
			_ = os.Remove(metaTmpPath)
			return nil, fmt.Errorf("commit cold meta file: %w", err)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	convArchives, ok := c.archives[convID]
	if !ok {
		convArchives = make(map[string]*coldArchiveEntry)
		c.archives[convID] = convArchives
	}
	convArchives[archiveID] = &coldArchiveEntry{
		info:  info,
		data:  compressedBytes,
		steps: steps,
	}

	return &info, nil
}

func (c *coldStore) Restore(convID string, archiveID string) ([]Step, error) {
	c.mu.RLock()
	convArchives, ok := c.archives[convID]
	var entry *coldArchiveEntry
	if ok {
		entry = convArchives[archiveID]
	}
	c.mu.RUnlock()

	var compressedBytes []byte
	if entry != nil {
		compressedBytes = entry.data
	} else if c.dir != "" {
		path := filepath.Join(c.dir, fmt.Sprintf("%s.gz", archiveID))
		var err error
		compressedBytes, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read cold archive file: %w", err)
		}
	} else {
		return nil, fmt.Errorf("cold archive %q not found", archiveID)
	}

	gr, err := gzip.NewReader(bytes.NewReader(compressedBytes))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	decompressed, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("decompress cold archive: %w", err)
	}

	var steps []Step
	if err := json.Unmarshal(decompressed, &steps); err != nil {
		return nil, fmt.Errorf("unmarshal cold steps: %w", err)
	}
	for i := range steps {
		NormalizeType0Step(&steps[i])
	}

	return steps, nil
}

func (c *coldStore) DeleteArchive(convID string, archiveID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if convArchives, ok := c.archives[convID]; ok {
		delete(convArchives, archiveID)
	}

	if c.dir != "" {
		path := filepath.Join(c.dir, fmt.Sprintf("%s.gz", archiveID))
		_ = os.Remove(path)
		metaPath := filepath.Join(c.dir, fmt.Sprintf("%s.meta.json", archiveID))
		_ = os.Remove(metaPath)
	}

	return nil
}

func (c *coldStore) GetStep(convID string, idx int64) (Step, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	convArchives, ok := c.archives[convID]
	if ok {
		for _, entry := range convArchives {
			if idx >= entry.info.StartIdx && idx <= entry.info.EndIdx {
				for _, st := range entry.steps {
					if st.Idx == idx {
						NormalizeType0Step(&st)
						return st, true, nil
					}
				}
			}
		}
	}

	// Disk discovery for .sst chunk files
	if c.dir != "" {
		sstFiles, _ := filepath.Glob(filepath.Join(c.dir, "*.sst"))
		for _, sf := range sstFiles {
			st, found, err := ReadSSTStep(sf, idx)
			if err == nil && found {
				NormalizeType0Step(&st)
				return st, true, nil
			}
		}
	}

	return Step{}, false, nil
}

func (c *coldStore) GetAllSteps(convID string) ([]Step, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var all []Step
	if convArchives, ok := c.archives[convID]; ok {
		for _, entry := range convArchives {
			all = append(all, entry.steps...)
		}
	}

	// Disk discovery for .sst chunk files
	if c.dir != "" {
		sstFiles, _ := filepath.Glob(filepath.Join(c.dir, "*.sst"))
		for _, sf := range sstFiles {
			chunk, err := ReadSSTChunk(sf)
			if err == nil && (chunk.Meta.ConvID == convID || convID == "") {
				all = append(all, chunk.Steps...)
			}
		}
	}

	for i := range all {
		NormalizeType0Step(&all[i])
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Idx < all[j].Idx
	})

	return all, nil
}

func (c *coldStore) Count(convID string) (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	if convArchives, ok := c.archives[convID]; ok {
		for _, entry := range convArchives {
			count += entry.info.StepCount
		}
	}

	// Count from .sst chunk files on disk
	if c.dir != "" {
		sstFiles, _ := filepath.Glob(filepath.Join(c.dir, "*.sst"))
		for _, sf := range sstFiles {
			chunk, err := ReadSSTChunk(sf)
			if err == nil && (chunk.Meta.ConvID == convID || convID == "") {
				count += chunk.Meta.StepCount
			}
		}
	}

	return count, nil
}

func (c *coldStore) Close() error {
	return nil
}

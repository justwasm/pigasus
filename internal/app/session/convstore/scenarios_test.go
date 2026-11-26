package convstore

// scenarios_test.go — the Storybook of the tiered trajectory store
// (tiered.go).
//
// Storybook philosophy, applied to a database- and filesystem-backed store:
//
//   - every scenario is *independently constructed*: it gets its own
//     t.TempDir() root, its own SQLite warm database and its own cold archive
//     directory, so it shares nothing with its neighbours and cannot depend on
//     the order the scenarios run in;
//   - every scenario has a *name* that states the situation and the rule it
//     pins;
//   - every scenario's *inputs* are explicit: one TieredStoreConfig plus the
//     steps it appends;
//   - every scenario asserts *observable output only* — the TierStats
//     distribution, the tier GetStep reports, the ordered trajectory, the
//     returned counts and errors, and the files that exist on disk. No
//     scenario reads a private field.
//
// The rules pinned here are the ones tiered.go actually implements:
//
//	the hot window is capped at HotCapacity and the steps that spill are the
//	oldest ones, in insertion order (auto-evict on append, or an explicit
//	EvictHot); a completed turn keeps the five most recent steps hot
//	whatever HotCapacity says; GetStep resolves hot -> warm -> cold and never
//	promotes on read; a re-appended index is served from hot even while warm
//	still holds the older copy; ArchiveCold reads only the warm tier and is
//	exclusive of its cutoff, while ArchiveColdSST flushes the hot steps below
//	the cutoff first; a restore moves steps back to warm and consumes the
//	archive; Close flushes the hot window into warm; and every operation on a
//	closed store reports that it is closed.
//
// Limitations, stated rather than hidden:
//
//   - tiered.go reads the wall clock only to stamp an archive id and its
//     metadata. Scenarios therefore never assert on an id's nonce; the
//     deterministic conversation-and-range prefix is asserted instead.
//   - the store's "in-memory" modes (WarmDir == "" and ColdDir == "") share a
//     process-wide SQLite DSN and fall back to os.TempDir() for SST chunks,
//     which is not hermetic. No scenario uses those modes: every scenario
//     supplies private temp directories.
//   - tiered.go has no age, size or access-recency policy. A step's tier is
//     decided by index order and by the calls made on the store, never by a
//     clock or an LRU, so no scenario pins a rule that is not there.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// story is the per-scenario fixture: one private temp root, one store whose
// warm database and cold archives live inside it, and one conversation id.
type story struct {
	t     *testing.T
	cfg   TieredStoreConfig
	store TieredTrajectoryStore
	conv  string
}

// newStory builds an isolated store. Any tier directory the scenario did not
// name explicitly is created under the scenario's own t.TempDir() root, so no
// two scenarios ever share a warm database or a cold archive directory.
func newStory(t *testing.T, cfg TieredStoreConfig) *story {
	t.Helper()
	root := t.TempDir()
	if cfg.WarmDir == "" {
		cfg.WarmDir = filepath.Join(root, "warm")
	}
	if cfg.ColdDir == "" {
		cfg.ColdDir = filepath.Join(root, "cold")
	}
	s := &story{t: t, cfg: cfg, conv: "story-conv"}
	s.open()
	t.Cleanup(func() { _ = s.store.Close() })
	return s
}

// open constructs the store described by the fixture's config.
func (s *story) open() {
	s.t.Helper()
	store, err := NewTieredTrajectoryStore(s.cfg)
	if err != nil {
		s.t.Fatalf("NewTieredTrajectoryStore(%+v): %v", s.cfg, err)
	}
	s.store = store
}

// close shuts the current store down, failing the scenario if Close errors.
func (s *story) close() {
	s.t.Helper()
	if err := s.store.Close(); err != nil {
		s.t.Fatalf("Close: %v", err)
	}
}

// reopen simulates a process restart on the same directories.
func (s *story) reopen() {
	s.t.Helper()
	s.close()
	s.open()
}

// appendRange appends steps with indices [from, to) to convID, one at a time,
// exactly as a turn appends them.
func (s *story) appendRange(convID string, from, to int64) {
	s.t.Helper()
	for i := from; i < to; i++ {
		if err := s.store.AppendStep(convID, numberedStep(convID, i)); err != nil {
			s.t.Fatalf("AppendStep(%s, %d): %v", convID, i, err)
		}
	}
}

// numberedStep is the canonical fixture step: a completed tool call whose text
// names its conversation and index, so a step read back from any tier can be
// proven to be the step that was written.
func numberedStep(convID string, idx int64) Step {
	return Step{
		Idx:      idx,
		Type:     StepToolCall,
		Status:   3,
		Text:     fmt.Sprintf("%s step %d", convID, idx),
		ToolName: "run_command",
	}
}

// requireStats asserts the whole tier distribution, including that Total is the
// sum of the three tiers.
func requireStats(t *testing.T, got TierStats, hot, warm, cold int, why string) {
	t.Helper()
	want := TierStats{HotCount: hot, WarmCount: warm, ColdCount: cold, Total: hot + warm + cold}
	if got != want {
		t.Fatalf("stats = %+v, want %+v — %s", got, want, why)
	}
}

// requireTier asserts which tier serves a step and that the step's content
// survived the round trip through that tier.
func requireTier(t *testing.T, s *story, convID string, idx int64, want TierLevel, why string) {
	t.Helper()
	st, tier, err := s.store.GetStep(convID, idx)
	if err != nil {
		t.Fatalf("GetStep(%s, %d) = error %v, want tier %s — %s", convID, idx, err, want, why)
	}
	if tier != want {
		t.Fatalf("GetStep(%s, %d) tier = %s, want %s — %s", convID, idx, tier, want, why)
	}
	if wantText := fmt.Sprintf("%s step %d", convID, idx); st.Text != wantText {
		t.Fatalf("GetStep(%s, %d) text = %q, want %q — %s", convID, idx, st.Text, wantText, why)
	}
}

// requireTrajectory asserts the full trajectory is complete, in index order and
// free of gaps or duplicates.
func requireTrajectory(t *testing.T, s *story, convID string, want int, why string) {
	t.Helper()
	traj, err := s.store.LoadTrajectory(convID)
	if err != nil {
		t.Fatalf("LoadTrajectory(%s): %v — %s", convID, err, why)
	}
	if len(traj) != want {
		t.Fatalf("LoadTrajectory(%s) returned %d steps, want %d — %s", convID, len(traj), want, why)
	}
	for i, st := range traj {
		if st.Idx != int64(i) {
			t.Fatalf("trajectory[%d].Idx = %d, want %d — %s", i, st.Idx, i, why)
		}
		if wantText := fmt.Sprintf("%s step %d", convID, st.Idx); st.Text != wantText {
			t.Fatalf("trajectory[%d].Text = %q, want %q — %s", i, st.Text, wantText, why)
		}
	}
}

// requireNoTempFiles asserts no half-written artefact was left behind: both the
// cold archive and the SST writer commit by rename.
func requireNoTempFiles(t *testing.T, dir, why string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("found leftover temporary file %q in %s — %s", e.Name(), dir, why)
		}
	}
}

// sstFiles returns the SST chunk file names in dir.
func sstFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.sst"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	return names
}

// TestScenario_HotTierCapacity pins the append-time spill rule: the hot tier
// holds at most HotCapacity steps, and the steps that spill are the oldest ones,
// in insertion order.
func TestScenario_HotTierCapacity(t *testing.T) {
	t.Run("nothing is evicted at or below the hot capacity", func(t *testing.T) {
		// HotCapacity is inclusive. A tier that spilled at "len == cap" would
		// hold one step fewer than configured, shrinking the RAM window the
		// active turn reads from for no reason.
		s := newStory(t, TieredStoreConfig{HotCapacity: 4, AutoEvict: true})

		s.appendRange(s.conv, 0, 1)
		requireStats(t, s.store.Stats(s.conv), 1, 0, 0, "a single step fits in the hot tier")

		s.appendRange(s.conv, 1, 4)
		requireStats(t, s.store.Stats(s.conv), 4, 0, 0, "exactly at capacity nothing spills to warm")
		requireTier(t, s, s.conv, 0, TierHot, "the oldest step is still in RAM at capacity")
		requireTrajectory(t, s, s.conv, 4, "every step is still readable at capacity")
	})

	t.Run("an overflow moves the oldest steps to warm in order", func(t *testing.T) {
		// The evicted prefix is the OLDEST prefix (steps[:overflow]) and the
		// survivors are the newest window. That is what makes the hot tier a
		// sliding window rather than an arbitrary sample.
		s := newStory(t, TieredStoreConfig{HotCapacity: 4, AutoEvict: true})

		s.appendRange(s.conv, 0, 5)
		requireStats(t, s.store.Stats(s.conv), 4, 1, 0, "one past capacity spills exactly one step")
		requireTier(t, s, s.conv, 0, TierWarm, "the oldest step spilled to warm")
		requireTier(t, s, s.conv, 1, TierHot, "the second-oldest stayed hot")

		// A burst on a second conversation shows the accumulated order: ten
		// appends leave the first six in warm and the last four in RAM.
		burst := "burst-conv"
		s.appendRange(burst, 0, 10)
		requireStats(t, s.store.Stats(burst), 4, 6, 0, "ten appends leave six in warm and four in hot")
		for idx := int64(0); idx < 6; idx++ {
			requireTier(t, s, burst, idx, TierWarm, "the early steps are the ones that spill")
		}
		for idx := int64(6); idx < 10; idx++ {
			requireTier(t, s, burst, idx, TierHot, "the newest steps stay in RAM")
		}
		requireTrajectory(t, s, burst, 10, "spilling must not lose or reorder a step")
	})

	t.Run("a non-positive capacity falls back to the default of twenty", func(t *testing.T) {
		// A zero capacity is a configuration omission, not a request for an
		// empty hot tier: the constructor substitutes the documented default,
		// which is what keeps every recent step of a turn in RAM.
		s := newStory(t, TieredStoreConfig{HotCapacity: 0, AutoEvict: true})

		s.appendRange(s.conv, 0, 21)
		requireStats(t, s.store.Stats(s.conv), 20, 1, 0, "capacity 0 means the default 20, so the 21st append spills")
		requireTier(t, s, s.conv, 0, TierWarm, "the first step spilled under the default capacity")
		requireTier(t, s, s.conv, 20, TierHot, "the newest step is hot")
	})
}

// TestScenario_ManualEviction pins EvictHot: it is the explicit trim, it is a
// no-op when nothing overflows, and it is idempotent.
func TestScenario_ManualEviction(t *testing.T) {
	t.Run("without auto-evict the hot tier grows past capacity until EvictHot trims it", func(t *testing.T) {
		// With AutoEvict off the store never silently moves a step: the caller
		// owns the moment. EvictHot then trims to capacity, keeps the newest
		// window and moves the rest to warm.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: false})

		s.appendRange(s.conv, 0, 6)
		requireStats(t, s.store.Stats(s.conv), 6, 0, 0, "no auto-evict means RAM keeps every appended step")

		evicted, err := s.store.EvictHot(s.conv)
		if err != nil {
			t.Fatalf("EvictHot: %v", err)
		}
		if evicted != 4 {
			t.Fatalf("EvictHot evicted %d steps, want 4 (6 hot - capacity 2)", evicted)
		}
		requireStats(t, s.store.Stats(s.conv), 2, 4, 0, "the trim leaves exactly the hot capacity in RAM")
		requireTier(t, s, s.conv, 3, TierWarm, "the newest evicted step is in warm")
		requireTier(t, s, s.conv, 4, TierHot, "the survivors are the newest steps")
		requireTier(t, s, s.conv, 5, TierHot, "the last step is hot")

		// A second trim has nothing to do: eviction is a state, not a counter.
		again, err := s.store.EvictHot(s.conv)
		if err != nil {
			t.Fatalf("second EvictHot: %v", err)
		}
		if again != 0 {
			t.Fatalf("second EvictHot evicted %d steps, want 0", again)
		}
		requireStats(t, s.store.Stats(s.conv), 2, 4, 0, "a repeated trim changes nothing")

		// A conversation that was never appended is not an error and has
		// nothing to trim.
		none, err := s.store.EvictHot("never-appended")
		if err != nil {
			t.Fatalf("EvictHot on an unseen conversation: %v", err)
		}
		if none != 0 {
			t.Fatalf("EvictHot on an unseen conversation evicted %d steps, want 0", none)
		}
		requireStats(t, s.store.Stats("never-appended"), 0, 0, 0, "an unseen conversation has an empty distribution")
	})
}

// TestScenario_CompleteTurn pins the end-of-turn flush: it keeps the five most
// recent steps hot for immediate tool context and persists everything older.
func TestScenario_CompleteTurn(t *testing.T) {
	t.Run("a completed turn keeps the five most recent steps hot", func(t *testing.T) {
		// The five-step window is the tool context the next turn needs without
		// a disk round trip; the rest of the turn is durable history and must
		// leave RAM.
		s := newStory(t, TieredStoreConfig{HotCapacity: 20, AutoEvict: true})

		s.appendRange(s.conv, 0, 8)
		requireStats(t, s.store.Stats(s.conv), 8, 0, 0, "eight steps fit under a capacity of twenty")

		evicted, err := s.store.CompleteTurn(s.conv)
		if err != nil {
			t.Fatalf("CompleteTurn: %v", err)
		}
		if evicted != 3 {
			t.Fatalf("CompleteTurn evicted %d steps, want 3 (8 - 5 kept)", evicted)
		}
		requireStats(t, s.store.Stats(s.conv), 5, 3, 0, "five steps stay hot and three go to warm")
		requireTier(t, s, s.conv, 2, TierWarm, "the last step outside the keep window is warm")
		requireTier(t, s, s.conv, 3, TierHot, "the first step of the keep window is hot")
		requireTier(t, s, s.conv, 7, TierHot, "the newest step is hot")
		requireTrajectory(t, s, s.conv, 8, "completing a turn must not lose a step")
	})

	t.Run("five or fewer steps survive a completed turn and six evicts exactly one", func(t *testing.T) {
		// The boundary of the keep window: at five nothing leaves RAM, at six
		// the single oldest step does.
		s := newStory(t, TieredStoreConfig{HotCapacity: 20, AutoEvict: true})

		atFive := "at-five"
		s.appendRange(atFive, 0, 5)
		if evicted, err := s.store.CompleteTurn(atFive); err != nil || evicted != 0 {
			t.Fatalf("CompleteTurn(%s) = (%d, %v), want (0, nil) at the keep boundary", atFive, evicted, err)
		}
		requireStats(t, s.store.Stats(atFive), 5, 0, 0, "exactly five steps all stay hot")

		atSix := "at-six"
		s.appendRange(atSix, 0, 6)
		if evicted, err := s.store.CompleteTurn(atSix); err != nil || evicted != 1 {
			t.Fatalf("CompleteTurn(%s) = (%d, %v), want (1, nil) one past the keep boundary", atSix, evicted, err)
		}
		requireStats(t, s.store.Stats(atSix), 5, 1, 0, "one past the keep boundary flushes exactly the oldest step")
		requireTier(t, s, atSix, 0, TierWarm, "the oldest step is the one flushed")
		requireTier(t, s, atSix, 1, TierHot, "the keep window still starts at the second step")
	})

	t.Run("the keep-five rule outranks a smaller hot capacity", func(t *testing.T) {
		// CompleteTurn counts to five, not to HotCapacity: the end-of-turn
		// window is fixed by the tool-context rule. With a capacity below five
		// the turn still ends with more hot steps than the capacity, and the
		// next auto-evicting append is what trims them.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: false})

		s.appendRange(s.conv, 0, 6)
		evicted, err := s.store.CompleteTurn(s.conv)
		if err != nil {
			t.Fatalf("CompleteTurn: %v", err)
		}
		if evicted != 1 {
			t.Fatalf("CompleteTurn evicted %d steps, want 1 (6 - 5 kept)", evicted)
		}
		requireStats(t, s.store.Stats(s.conv), 5, 1, 0, "the keep window is five steps even though HotCapacity is 2")
		requireTier(t, s, s.conv, 0, TierWarm, "only the step outside the keep window was flushed")
		requireTier(t, s, s.conv, 1, TierHot, "the keep window starts at step 1")
	})
}

// TestScenario_TierLookup pins GetStep's resolution order, its no-promotion
// behaviour and its not-found contract.
func TestScenario_TierLookup(t *testing.T) {
	t.Run("a step reports the tier the store last put it in", func(t *testing.T) {
		// GetStep resolves hot -> warm -> cold, so one archived conversation
		// must be able to answer from all three tiers at once.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})

		s.appendRange(s.conv, 0, 10) // steps 0..7 warm, 8..9 hot
		if _, err := s.store.ArchiveCold(s.conv, 5); err != nil {
			t.Fatalf("ArchiveCold: %v", err)
		}
		requireStats(t, s.store.Stats(s.conv), 2, 3, 5, "archive moves 0..4 to cold, leaving 5..7 warm and 8..9 hot")

		for idx := int64(0); idx < 5; idx++ {
			requireTier(t, s, s.conv, idx, TierCold, "steps below the cutoff are served by the cold tier")
		}
		for idx := int64(5); idx < 8; idx++ {
			requireTier(t, s, s.conv, idx, TierWarm, "steps at or above the cutoff are still warm")
		}
		for idx := int64(8); idx < 10; idx++ {
			requireTier(t, s, s.conv, idx, TierHot, "steps inside the hot window are served from RAM")
		}
		requireTrajectory(t, s, s.conv, 10, "one trajectory spans all three tiers")
	})

	t.Run("an unknown step index or conversation reports ErrStepNotFound", func(t *testing.T) {
		// The store never invents a step. A miss is the sentinel error, and no
		// tier is named for a step that does not exist.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})
		s.appendRange(s.conv, 0, 3)

		if _, tier, err := s.store.GetStep(s.conv, 999); !errors.Is(err, ErrStepNotFound) || tier != "" {
			t.Fatalf("GetStep(999) = (tier %q, err %v), want (\"\", ErrStepNotFound)", tier, err)
		}
		if _, tier, err := s.store.GetStep("never-appended", 0); !errors.Is(err, ErrStepNotFound) || tier != "" {
			t.Fatalf("GetStep on an unseen conversation = (tier %q, err %v), want (\"\", ErrStepNotFound)", tier, err)
		}
	})

	t.Run("reading a warm step does not promote it into hot", func(t *testing.T) {
		// GetStep is a pure read: it consults hot, then warm, then cold, and
		// leaves the distribution alone. There is no LRU promotion, so a second
		// lookup still reports warm — otherwise Stats would not be a stable
		// description of where the steps actually live.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})
		s.appendRange(s.conv, 0, 3) // step 0 warm, 1..2 hot

		requireTier(t, s, s.conv, 0, TierWarm, "the first read of a spilled step hits warm")
		requireTier(t, s, s.conv, 0, TierWarm, "a second read still reports warm: nothing was promoted")
		requireStats(t, s.store.Stats(s.conv), 2, 1, 0, "reads never change the tier distribution")
	})

	t.Run("a re-appended step is served from hot although warm still holds it", func(t *testing.T) {
		// Hot is the first tier consulted, so replaying an index that was
		// already evicted makes the live copy win over the persisted one. That
		// is what lets a retried append correct a step instead of shadowing it.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})
		s.appendRange(s.conv, 0, 3) // 0 evicted to warm, 1..2 hot

		replay := numberedStep(s.conv, 0)
		replay.Text = s.conv + " step 0 (replayed)"
		if err := s.store.AppendStep(s.conv, replay); err != nil {
			t.Fatalf("AppendStep(replay): %v", err)
		}
		requireStats(t, s.store.Stats(s.conv), 2, 2, 0, "the replay pushes step 1 to warm and keeps step 0 hot")

		st, tier, err := s.store.GetStep(s.conv, 0)
		if err != nil {
			t.Fatalf("GetStep(0) after replay: %v", err)
		}
		if tier != TierHot {
			t.Fatalf("GetStep(0) tier = %s, want %s (hot is consulted before warm)", tier, TierHot)
		}
		if st.Text != replay.Text {
			t.Fatalf("GetStep(0).Text = %q, want the replayed %q", st.Text, replay.Text)
		}
	})

	t.Run("a replay yields one trajectory row per index, hot winning", func(t *testing.T) {
		// LoadTrajectory assembles cold -> warm -> hot. Without collapsing by
		// index, a replay would return both the durable warm copy and the live
		// hot copy for the same index: two rows for one step, which breaks any
		// caller that indexes the trajectory positionally.
		//
		// The precedence must match GetStep: hot beats warm beats cold.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})
		s.appendRange(s.conv, 0, 4) // 0..1 warm, 2..3 hot

		replay := numberedStep(s.conv, 0)
		replay.Text = s.conv + " step 0 (replayed)"
		if err := s.store.AppendStep(s.conv, replay); err != nil {
			t.Fatalf("AppendStep(replay): %v", err)
		}

		steps, err := s.store.LoadTrajectory(s.conv)
		if err != nil {
			t.Fatalf("LoadTrajectory: %v", err)
		}
		seen := make(map[int64]int, len(steps))
		for _, st := range steps {
			seen[st.Idx]++
		}
		for idx, n := range seen {
			if n > 1 {
				t.Errorf("index %d appears %d times in the trajectory, want 1", idx, n)
			}
		}
		if len(steps) != 4 {
			t.Fatalf("trajectory has %d rows, want one per index (4)", len(steps))
		}
		for _, st := range steps {
			if st.Idx == 0 && st.Text != replay.Text {
				t.Errorf("index 0 = %q, want the replayed %q (hot wins over warm)", st.Text, replay.Text)
			}
		}
	})
}

// TestScenario_ArchiveCold pins the gzip archive path: what the cutoff means,
// which tier it reads, and that the archive outlives the process.
func TestScenario_ArchiveCold(t *testing.T) {
	t.Run("the cutoff is exclusive and the archive survives a reopen", func(t *testing.T) {
		// A compaction boundary at index N means "summarised up to but not
		// including N", so step N must stay in warm: archiving it would put a
		// step the summary does not cover out of reach of the boundary check.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})
		s.appendRange(s.conv, 0, 10) // 0..7 warm, 8..9 hot

		info, err := s.store.ArchiveCold(s.conv, 5)
		if err != nil {
			t.Fatalf("ArchiveCold: %v", err)
		}
		if info == nil {
			t.Fatal("ArchiveCold returned a nil archive for a non-empty warm tier")
		}
		if info.StepCount != 5 || info.StartIdx != 0 || info.EndIdx != 4 {
			t.Fatalf("archive = %d steps [%d, %d], want 5 steps [0, 4]",
				info.StepCount, info.StartIdx, info.EndIdx)
		}
		if info.RawSize <= 0 || info.CompressedSize <= 0 {
			t.Fatalf("archive sizes = raw %d / compressed %d, want both positive", info.RawSize, info.CompressedSize)
		}
		if !strings.HasPrefix(info.ArchiveID, s.conv+"_0_4_") {
			t.Fatalf("archive id = %q, want a %q prefix naming the conversation and range", info.ArchiveID, s.conv+"_0_4_")
		}
		requireStats(t, s.store.Stats(s.conv), 2, 3, 5, "the archived span left warm and landed in cold")
		requireTier(t, s, s.conv, 4, TierCold, "the last step below the cutoff is cold")
		requireTier(t, s, s.conv, 5, TierWarm, "the step AT the cutoff stays warm (the cutoff is exclusive)")
		requireTrajectory(t, s, s.conv, 10, "archiving must not lose a step")

		// The archive is durable: a fresh store on the same directory reads it
		// back from the .gz payload, not from memory.
		s.reopen()
		if cold := s.store.Stats(s.conv).ColdCount; cold != 5 {
			t.Fatalf("cold count after reopen = %d, want 5", cold)
		}
		requireTier(t, s, s.conv, 4, TierCold, "a reopened store still serves the archived step from cold")
		requireTrajectory(t, s, s.conv, 10, "the trajectory is intact across a reopen")
	})

	t.Run("only the warm tier is archived and an empty selection is a nil archive", func(t *testing.T) {
		// ArchiveCold reads the warm tier alone: a step that is still hot below
		// the cutoff is not touched. And "nothing to archive" is (nil, nil), not
		// an error, so a periodic archiver can call it unconditionally.
		s := newStory(t, TieredStoreConfig{HotCapacity: 100, AutoEvict: true})
		s.appendRange(s.conv, 0, 5) // every step is hot

		info, err := s.store.ArchiveCold(s.conv, 5)
		if err != nil {
			t.Fatalf("ArchiveCold over a hot-only conversation: %v", err)
		}
		if info != nil {
			t.Fatalf("ArchiveCold over a hot-only conversation returned %+v, want nil", info)
		}
		requireStats(t, s.store.Stats(s.conv), 5, 0, 0, "hot steps below the cutoff are not archived")

		// A conversation with a single warm step: the first call archives it,
		// the second finds nothing and says so with a nil archive.
		half := "half-conv"
		s.appendRange(half, 0, 6)
		if evicted, err := s.store.CompleteTurn(half); err != nil || evicted != 1 {
			t.Fatalf("CompleteTurn(%s) = (%d, %v), want (1, nil)", half, evicted, err)
		}
		requireStats(t, s.store.Stats(half), 5, 1, 0, "one step left the hot window")

		first, err := s.store.ArchiveCold(half, 1)
		if err != nil || first == nil || first.StepCount != 1 {
			t.Fatalf("ArchiveCold(%s, 1) = (%+v, %v), want one archived step and no error", half, first, err)
		}
		requireStats(t, s.store.Stats(half), 5, 0, 1, "the only warm step below the cutoff became cold")

		second, err := s.store.ArchiveCold(half, 1)
		if err != nil {
			t.Fatalf("second ArchiveCold: %v", err)
		}
		if second != nil {
			t.Fatalf("second ArchiveCold returned %+v, want nil", second)
		}
		requireStats(t, s.store.Stats(half), 5, 0, 1, "archiving twice does not duplicate the archive")
	})
}

// TestScenario_ArchiveColdSST pins the SST variant, which unlike ArchiveCold
// flushes the hot tier first and commits one immutable chunk file.
func TestScenario_ArchiveColdSST(t *testing.T) {
	t.Run("an SST archive flushes the hot tier first and commits one chunk file", func(t *testing.T) {
		// A compaction boundary must be honoured even when the steps are still
		// in RAM, so ArchiveColdSST moves hot steps below the cutoff to warm
		// before writing the chunk. The chunk is committed by rename, so no
		// .tmp file may survive.
		s := newStory(t, TieredStoreConfig{HotCapacity: 100, AutoEvict: true})
		s.appendRange(s.conv, 0, 10) // every step is hot

		meta, err := s.store.ArchiveColdSST(s.conv, 5)
		if err != nil {
			t.Fatalf("ArchiveColdSST: %v", err)
		}
		if meta == nil {
			t.Fatal("ArchiveColdSST returned nil for a non-empty selection")
		}
		if meta.Magic != SSTMagic || meta.Checksum == 0 {
			t.Fatalf("chunk meta = {magic %q, checksum %d}, want magic %q and a non-zero checksum",
				meta.Magic, meta.Checksum, SSTMagic)
		}
		if meta.StepCount != 5 || meta.StartIdx != 0 || meta.EndIdx != 4 {
			t.Fatalf("chunk = %d steps [%d, %d], want 5 steps [0, 4]", meta.StepCount, meta.StartIdx, meta.EndIdx)
		}
		wantChunkID := fmt.Sprintf("sst_%s_0_4", s.conv)
		if meta.ChunkID != wantChunkID {
			t.Fatalf("chunk id = %q, want %q", meta.ChunkID, wantChunkID)
		}
		requireStats(t, s.store.Stats(s.conv), 5, 0, 5, "the flushed span left hot and is counted from the chunk on disk")
		requireTier(t, s, s.conv, 2, TierCold, "a step below the cutoff is served from the chunk")
		requireTier(t, s, s.conv, 7, TierHot, "a step above the cutoff is still hot")
		requireTrajectory(t, s, s.conv, 10, "the chunk and the hot window form one trajectory")

		names := sstFiles(t, s.cfg.ColdDir)
		if len(names) != 1 || names[0] != wantChunkID+".sst" {
			t.Fatalf("sst files in %s = %v, want exactly [%s]", s.cfg.ColdDir, names, wantChunkID+".sst")
		}
		requireNoTempFiles(t, s.cfg.ColdDir, "the chunk is committed atomically")
	})

	t.Run("an SST chunk is read back after a reopen", func(t *testing.T) {
		// The chunk is discovered from the directory, not from a remembered
		// index, so a restarted store must still answer point lookups from it.
		s := newStory(t, TieredStoreConfig{HotCapacity: 100, AutoEvict: true})
		s.appendRange(s.conv, 0, 10)
		meta, err := s.store.ArchiveColdSST(s.conv, 5)
		if err != nil || meta == nil {
			t.Fatalf("ArchiveColdSST = (%+v, %v), want a chunk", meta, err)
		}

		s.reopen()
		// Closing flushes the remaining hot window (5..9) into warm, so after
		// the restart the chunk still holds 0..4.
		requireStats(t, s.store.Stats(s.conv), 0, 5, 5, "the chunk is counted from disk and the hot window was flushed to warm")
		requireTier(t, s, s.conv, 0, TierCold, "the chunk still serves the earliest step after a reopen")
		requireTier(t, s, s.conv, 9, TierWarm, "the flushed hot window is warm after a reopen")
		requireTrajectory(t, s, s.conv, 10, "a reopened store still loads the whole trajectory")

		if names := sstFiles(t, s.cfg.ColdDir); len(names) != 1 {
			t.Fatalf("sst files after reopen = %v, want the single committed chunk", names)
		}
	})
}

// TestScenario_RestoreCold pins the reverse direction: a restored archive
// returns its steps to warm, empties the cold tier and is consumed.
func TestScenario_RestoreCold(t *testing.T) {
	t.Run("restoring moves the steps back to warm and empties the cold tier", func(t *testing.T) {
		// Restore is the resurrection path for a compaction the user undid, so
		// it must be lossless and must leave the cold tier empty rather than
		// keeping a second copy.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})
		s.appendRange(s.conv, 0, 10)
		info, err := s.store.ArchiveCold(s.conv, 5)
		if err != nil || info == nil {
			t.Fatalf("ArchiveCold = (%+v, %v), want an archive", info, err)
		}

		restored, err := s.store.RestoreCold(s.conv, info.ArchiveID)
		if err != nil {
			t.Fatalf("RestoreCold: %v", err)
		}
		if restored != 5 {
			t.Fatalf("RestoreCold restored %d steps, want 5", restored)
		}
		requireStats(t, s.store.Stats(s.conv), 2, 8, 0, "the cold span is back in warm and cold is empty")
		requireTier(t, s, s.conv, 0, TierWarm, "the oldest restored step is warm")
		requireTier(t, s, s.conv, 4, TierWarm, "the newest restored step is warm")
		requireTrajectory(t, s, s.conv, 10, "restoring must not lose or duplicate a step")

		// The archive's files are removed with the restore: keeping them would
		// let a later reopen resurrect a second copy.
		for _, path := range []string{
			filepath.Join(s.cfg.ColdDir, info.ArchiveID+".gz"),
			filepath.Join(s.cfg.ColdDir, info.ArchiveID+".meta.json"),
		} {
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("%s still exists after a restore (stat err %v), want it removed", path, statErr)
			}
		}
	})

	t.Run("restoring an unknown or already-consumed archive fails without touching the tiers", func(t *testing.T) {
		// A restore that cannot find its archive must say so and leave the
		// distribution exactly as it was: a half-applied restore would duplicate
		// steps. A second restore of the same archive is the consumed case.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})
		s.appendRange(s.conv, 0, 6) // 0..3 warm, 4..5 hot
		info, err := s.store.ArchiveCold(s.conv, 3)
		if err != nil || info == nil {
			t.Fatalf("ArchiveCold = (%+v, %v), want an archive", info, err)
		}
		requireStats(t, s.store.Stats(s.conv), 2, 1, 3, "the archive took 0..2 to cold and left step 3 warm")

		if _, err := s.store.RestoreCold(s.conv, "no-such-archive"); err == nil {
			t.Fatal("RestoreCold of an unknown archive id returned no error")
		}
		requireStats(t, s.store.Stats(s.conv), 2, 1, 3, "a failed restore leaves the distribution untouched")

		if restored, err := s.store.RestoreCold(s.conv, info.ArchiveID); err != nil || restored != 3 {
			t.Fatalf("RestoreCold = (%d, %v), want (3, nil)", restored, err)
		}
		requireStats(t, s.store.Stats(s.conv), 2, 4, 0, "the restored steps joined the warm tier")

		if _, err := s.store.RestoreCold(s.conv, info.ArchiveID); err == nil {
			t.Fatal("a second RestoreCold of the same archive returned no error, want a consumed-archive failure")
		}
		requireStats(t, s.store.Stats(s.conv), 2, 4, 0, "the failed second restore changed nothing")
	})
}

// TestScenario_StoreDirectories pins the store's relationship with its
// directories: what it creates, what it refuses, and what it tolerates.
func TestScenario_StoreDirectories(t *testing.T) {
	t.Run("missing nested directories are created and hold the archive files", func(t *testing.T) {
		// Production builds a store before any conversation exists, so the tier
		// directories are a consequence of the config, not a precondition. The
		// warm database and the cold archive must land exactly where asked.
		root := t.TempDir()
		warmDir := filepath.Join(root, "state", "warm")
		coldDir := filepath.Join(root, "state", "cold")
		for _, dir := range []string{warmDir, coldDir} {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("precondition: %s already exists (stat err %v)", dir, err)
			}
		}

		cfg := TieredStoreConfig{HotCapacity: 2, WarmDir: warmDir, ColdDir: coldDir, AutoEvict: true}
		store, err := NewTieredTrajectoryStore(cfg)
		if err != nil {
			t.Fatalf("NewTieredTrajectoryStore on missing directories: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		s := &story{t: t, cfg: cfg, store: store, conv: "dir-conv"}

		if fi, err := os.Stat(filepath.Join(warmDir, "warm_trajectory.db")); err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("warm database in %s: (fi %v, err %v), want a regular file", warmDir, fi, err)
		}

		s.appendRange(s.conv, 0, 6) // 0..3 warm, 4..5 hot
		info, err := store.ArchiveCold(s.conv, 4)
		if err != nil || info == nil {
			t.Fatalf("ArchiveCold = (%+v, %v), want an archive", info, err)
		}

		gzPath := filepath.Join(coldDir, info.ArchiveID+".gz")
		fi, err := os.Stat(gzPath)
		if err != nil {
			t.Fatalf("archive payload %s: %v", gzPath, err)
		}
		if fi.Size() != info.CompressedSize {
			t.Fatalf("archive file is %d bytes, want the recorded CompressedSize %d", fi.Size(), info.CompressedSize)
		}
		if _, err := os.Stat(filepath.Join(coldDir, info.ArchiveID+".meta.json")); err != nil {
			t.Fatalf("archive metadata: %v", err)
		}
		requireNoTempFiles(t, coldDir, "both archive files are committed atomically")
	})

	t.Run("a regular file where a tier directory belongs fails construction", func(t *testing.T) {
		// The constructor must not paper over a configuration mistake: a path
		// occupied by a file cannot become a tier directory, and naming the
		// failing tier is what makes the error actionable.
		root := t.TempDir()
		blocker := filepath.Join(root, "blocker")
		if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write blocker: %v", err)
		}

		if _, err := NewTieredTrajectoryStore(TieredStoreConfig{
			HotCapacity: 4,
			WarmDir:     blocker,
			ColdDir:     filepath.Join(root, "cold"),
		}); err == nil || !strings.Contains(err.Error(), "init warm store") {
			t.Fatalf("construction with a blocked warm dir = %v, want an init warm store error", err)
		}

		if _, err := NewTieredTrajectoryStore(TieredStoreConfig{
			HotCapacity: 4,
			WarmDir:     filepath.Join(root, "warm"),
			ColdDir:     blocker,
		}); err == nil || !strings.Contains(err.Error(), "init cold store") {
			t.Fatalf("construction with a blocked cold dir = %v, want an init cold store error", err)
		}
	})

	t.Run("damaged cold metadata is skipped instead of failing the store", func(t *testing.T) {
		// A truncated .meta.json, or metadata whose payload vanished, must not
		// take the whole store down: the cold tier degrades to "no archives" so
		// live conversations keep working.
		root := t.TempDir()
		coldDir := filepath.Join(root, "cold")
		if err := os.MkdirAll(coldDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(coldDir, "broken.meta.json"), []byte("{ not json"), 0o600); err != nil {
			t.Fatalf("write broken metadata: %v", err)
		}
		orphanMeta := []byte(`{"archiveId":"orphan","convId":"story-conv","stepCount":3,"startIdx":0,"endIdx":2}`)
		if err := os.WriteFile(filepath.Join(coldDir, "orphan.meta.json"), orphanMeta, 0o600); err != nil {
			t.Fatalf("write orphan metadata: %v", err)
		}

		store, err := NewTieredTrajectoryStore(TieredStoreConfig{
			HotCapacity: 4,
			WarmDir:     filepath.Join(root, "warm"),
			ColdDir:     coldDir,
			AutoEvict:   true,
		})
		if err != nil {
			t.Fatalf("NewTieredTrajectoryStore over a damaged cold dir: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })

		requireStats(t, store.Stats("story-conv"), 0, 0, 0, "unreadable or orphaned metadata contributes no cold steps")
		if _, _, err := store.GetStep("story-conv", 0); !errors.Is(err, ErrStepNotFound) {
			t.Fatalf("GetStep(0) over a damaged cold dir = %v, want ErrStepNotFound", err)
		}
		if traj, err := store.LoadTrajectory("story-conv"); err != nil || len(traj) != 0 {
			t.Fatalf("LoadTrajectory over a damaged cold dir = (%d steps, %v), want (0, nil)", len(traj), err)
		}
	})
}

// TestScenario_ClosedStore pins the shutdown contract: Close is idempotent and
// every operation afterwards reports the closed store instead of touching
// released resources.
func TestScenario_ClosedStore(t *testing.T) {
	t.Run("every operation on a closed store reports it is closed", func(t *testing.T) {
		// The store releases its SQLite handle at Close; a late append from a
		// straggling turn must get an error, never a panic or a silent drop.
		s := newStory(t, TieredStoreConfig{HotCapacity: 4, AutoEvict: true})
		s.appendRange(s.conv, 0, 2)
		s.close()

		ops := []struct {
			name string
			call func() error
		}{
			{"AppendStep", func() error { return s.store.AppendStep(s.conv, numberedStep(s.conv, 2)) }},
			{"GetStep", func() error { _, _, err := s.store.GetStep(s.conv, 0); return err }},
			{"LoadTrajectory", func() error { _, err := s.store.LoadTrajectory(s.conv); return err }},
			{"EvictHot", func() error { _, err := s.store.EvictHot(s.conv); return err }},
			{"CompleteTurn", func() error { _, err := s.store.CompleteTurn(s.conv); return err }},
			{"ArchiveCold", func() error { _, err := s.store.ArchiveCold(s.conv, 1); return err }},
			{"ArchiveColdSST", func() error { _, err := s.store.ArchiveColdSST(s.conv, 1); return err }},
			{"RestoreCold", func() error { _, err := s.store.RestoreCold(s.conv, "any"); return err }},
		}
		for _, op := range ops {
			op := op
			t.Run(op.name, func(t *testing.T) {
				err := op.call()
				if err == nil || !strings.Contains(err.Error(), "closed") {
					t.Fatalf("%s on a closed store = %v, want a \"closed\" error", op.name, err)
				}
			})
		}

		if err := s.store.Close(); err != nil {
			t.Fatalf("second Close = %v, want nil (Close is idempotent)", err)
		}
		requireStats(t, s.store.Stats(s.conv), 0, 0, 0, "a closed store reports an empty distribution instead of panicking")
	})
}

// TestScenario_ConversationIsolation pins that the hot windows and the warm
// rows of two conversations never mix, even at the same step indices.
func TestScenario_ConversationIsolation(t *testing.T) {
	t.Run("conversations keep independent hot windows", func(t *testing.T) {
		// Two live conversations share the store but not their RAM windows or
		// their warm rows: step 0 of one must never be served for the other.
		s := newStory(t, TieredStoreConfig{HotCapacity: 2, AutoEvict: true})

		s.appendRange("alpha", 0, 3) // alpha 0 warm, 1..2 hot
		s.appendRange("beta", 0, 3)  // beta 0 warm, 1..2 hot

		requireStats(t, s.store.Stats("alpha"), 2, 1, 0, "alpha has its own two-step window")
		requireStats(t, s.store.Stats("beta"), 2, 1, 0, "beta has its own two-step window")
		requireStats(t, s.store.Stats("gamma"), 0, 0, 0, "a conversation that was never used is empty")
		requireTier(t, s, "alpha", 0, TierWarm, "alpha's spilled step is alpha's")
		requireTier(t, s, "beta", 0, TierWarm, "beta's spilled step is beta's")
		requireTrajectory(t, s, "alpha", 3, "alpha's trajectory holds only alpha's steps")
		requireTrajectory(t, s, "beta", 3, "beta's trajectory holds only beta's steps")
	})
}

// TestScenario_RepeatedTieringPass pins what survives a restart and what a
// second tiering pass costs: nothing.
func TestScenario_RepeatedTieringPass(t *testing.T) {
	t.Run("closing flushes the hot window to warm so a reopen serves it from warm", func(t *testing.T) {
		// The store is the only writer of the trajectory: a clean shutdown must
		// not drop the steps still in RAM, so Close persists them and the next
		// process reads them from warm.
		s := newStory(t, TieredStoreConfig{HotCapacity: 100, AutoEvict: true})
		s.appendRange(s.conv, 0, 3)
		requireStats(t, s.store.Stats(s.conv), 3, 0, 0, "all three steps are still hot before shutdown")

		s.close()
		s.open()

		requireStats(t, s.store.Stats(s.conv), 0, 3, 0, "the hot window was persisted to warm on close")
		requireTier(t, s, s.conv, 1, TierWarm, "a reopened store serves the flushed step from warm")
		requireTrajectory(t, s, s.conv, 3, "a clean shutdown loses no step")
	})

	t.Run("a second tiering pass changes nothing", func(t *testing.T) {
		// Eviction, turn completion and archiving are all state-driven, so
		// running the same pass twice must be a no-op: a periodic housekeeping
		// loop cannot accumulate drift or duplicated archives.
		s := newStory(t, TieredStoreConfig{HotCapacity: 4, AutoEvict: true})
		s.appendRange(s.conv, 0, 6) // auto-evict left 0..1 warm and 2..5 hot

		pass := func(why string) {
			t.Helper()
			if evicted, err := s.store.EvictHot(s.conv); err != nil || evicted != 0 {
				t.Fatalf("EvictHot = (%d, %v), want (0, nil) — %s", evicted, err, why)
			}
			if completed, err := s.store.CompleteTurn(s.conv); err != nil || completed != 0 {
				t.Fatalf("CompleteTurn = (%d, %v), want (0, nil) — %s", completed, err, why)
			}
		}

		pass("the hot tier is already at capacity and inside the keep window")
		first, err := s.store.ArchiveCold(s.conv, 4)
		if err != nil || first == nil || first.StepCount != 2 {
			t.Fatalf("first ArchiveCold = (%+v, %v), want the two warm steps archived", first, err)
		}
		requireStats(t, s.store.Stats(s.conv), 4, 0, 2, "the first pass archived the only warm span")
		requireTrajectory(t, s, s.conv, 6, "the first pass lost no step")

		pass("the second pass finds nothing to evict or flush")
		second, err := s.store.ArchiveCold(s.conv, 4)
		if err != nil {
			t.Fatalf("second ArchiveCold: %v", err)
		}
		if second != nil {
			t.Fatalf("second ArchiveCold returned %+v, want nil — a repeated pass archives nothing", second)
		}
		requireStats(t, s.store.Stats(s.conv), 4, 0, 2, "the second pass changed nothing")
		requireTrajectory(t, s, s.conv, 6, "the second pass duplicated or dropped no step")
	})
}

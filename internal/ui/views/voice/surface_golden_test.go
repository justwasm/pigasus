package voice

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// surface_golden_test.go — the voice dictation state machine's bytes,
// pinned.
//
// The golden holds the bytes the surface produces through every phase:
// the banner overlay in each of them, the draft a confirmation writes
// into the composer and the failure notice a broken session echoes.
//
// The bytes were captured from the host's own voice state machine
// before it moved into this package (the host's startVoiceDictation /
// handleVoiceConnectDone / handleVoiceTick / handleVoiceKey / voiceView
// in internal/cliapp/tui_help.go). A two-tree byte dump compared the
// host's own rendering before the move, the host's own rendering after
// it and this package's surface, and all three were byte for byte
// identical.
//
// The timer needs no mask: the recorder's clock is driven by the
// surface's own tick, so the elapsed cell is a fixture, not a reading
// of the wall clock.

// goldenRecorder collects what the surface asked the host to do.
type goldenRecorder struct {
	inserts []string
	replies [][]string
}

// seams builds the surface's seams over a recorder.
func (z *goldenRecorder) seams() Seams {
	return Seams{
		Connect:     func() error { return nil },
		InsertDraft: func(text string) { z.inserts = append(z.inserts, text) },
		Respond: func(msg string, extra ...string) {
			z.replies = append(z.replies, append([]string{msg}, extra...))
		},
		Width: func() int { return 100 },
	}
}

// renderSurfacePhases drives the surface through every phase and
// renders what it produced.
func renderSurfacePhases(t *testing.T) string {
	t.Helper()
	var b strings.Builder

	// 1. idle — no dictation, no banner.
	z := &goldenRecorder{}
	s := New(z.seams())
	b.WriteString("=== idle ===\n")
	b.WriteString(s.View())

	// 2. connecting — the dictation was armed, the session is not up.
	z = &goldenRecorder{}
	s = New(z.seams())
	_ = s.Start("/voice")
	b.WriteString("=== connecting ===\n")
	b.WriteString(s.View())

	// 3. recording / 4. one tick later.
	s.HandleConnectDone(ConnectDoneMsg{Err: nil})
	b.WriteString("=== recording ===\n")
	b.WriteString(s.View())
	s.HandleTick()
	b.WriteString("=== recording-tick ===\n")
	b.WriteString(s.View())

	// 5. finishing — the recorder hit its cap.
	z = &goldenRecorder{}
	s = New(z.seams())
	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{Err: nil})
	s.Recorder().Tick(MaxRecordingDuration)
	s.HandleTick()
	b.WriteString("=== finishing ===\n")
	b.WriteString(s.View())

	// 6. confirm — the draft lands in the composer.
	z = &goldenRecorder{}
	s = New(z.seams())
	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{Err: nil})
	_ = s.Recorder().AppendDraft(goldenDraft)
	_, _ = s.HandleKey(tea.Key{Code: tea.KeyEnter})
	b.WriteString("=== confirm ===\n")
	b.WriteString("draft=" + goldenLast(z.inserts) + "\n")

	// 7. the mic server is unreachable.
	z = &goldenRecorder{}
	s = New(z.seams())
	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{Err: errors.New(goldenMicErr)})
	b.WriteString("=== error-mic ===\n")
	for _, row := range goldenLastReply(z.replies) {
		b.WriteString("response=" + row + "\n")
	}

	// 8. the sign-in does not carry the permission.
	z = &goldenRecorder{}
	s = New(z.seams())
	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{Err: errors.New(goldenPermErr)})
	b.WriteString("=== error-permission ===\n")
	for _, row := range goldenLastReply(z.replies) {
		b.WriteString("response=" + row + "\n")
	}

	// 9. cancel — the draft is dropped.
	z = &goldenRecorder{}
	s = New(z.seams())
	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{Err: nil})
	_ = s.Recorder().AppendDraft(goldenDraft)
	_, _ = s.HandleKey(tea.Key{Code: tea.KeyEsc})
	b.WriteString("=== cancel ===\n")
	b.WriteString("draft=" + goldenLast(z.inserts) + "\n")

	return b.String()
}

// The fixtures the golden is rendered from.
const (
	goldenMicErr  = "cannot reach the mic server at 127.0.0.1:54321 (tcp): dial tcp 127.0.0.1:54321: connect: connection refused"
	goldenPermErr = "transcription failed: failed to receive setup response: rpc error: code = PermissionDenied desc = Request had insufficient authentication scopes."
	goldenDraft   = "hello from voice dictation"
)

// goldenLast is the last inserted draft, or the empty string.
func goldenLast(inserts []string) string {
	if len(inserts) == 0 {
		return ""
	}
	return inserts[len(inserts)-1]
}

// goldenLastReply is the last echoed reply.
func goldenLastReply(replies [][]string) []string {
	if len(replies) == 0 {
		return nil
	}
	return replies[len(replies)-1]
}

// TestSurfaceGolden pins the surface's exact bytes.
//
// 【一手】 agy 1.4.3: the overlay reads "  Connecting... ", then the
// blinking dot with "Recording mm:ss", then "Finishing up... "; the
// hint line names the keys, and a failure names the permission the
// sign-in does not carry.
func TestSurfaceGolden(t *testing.T) {
	got := renderSurfacePhases(t)
	want, err := os.ReadFile(filepath.Join("testdata", "surface.golden"))
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	if !bytes.Equal([]byte(got), want) {
		t.Errorf("the voice surface's bytes changed:\n got %d bytes\nwant %d bytes\nfirst difference at %s",
			len(got), len(want), firstDifference(got, string(want)))
	}
}

// firstDifference names the first byte that differs between two
// renderings.
func firstDifference(got, want string) string {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return "byte " + strconv.Itoa(i)
		}
	}
	return "byte " + strconv.Itoa(n)
}

// reader_test.go — the panel's second level: reading a body in full.
//
// The list is an outline and the reader is the document it names. These
// tests pin the four things that make the reader different from a taller
// fold: the body comes from the host on demand, the format keys change the
// rendering without changing the content, backing out is one level rather
// than the whole surface, and a stale answer cannot replace what the user
// is reading.
package debugpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// readSeam is a host that answers every read with one fixed body, and
// records the IDs it was asked for.
type readSeam struct {
	asked []string
	body  Body
	err   error
}

func (s *readSeam) read(id string) tea.Cmd {
	s.asked = append(s.asked, id)
	body, err := s.body, s.err
	return func() tea.Msg { return ReaderMsg{ID: id, Body: body, Err: err} }
}

// readerPanel is a panel over the trajectory fixture with a reading host.
func readerPanel(s *readSeam) *Panel {
	return New(Seams{
		Logs:       func() State { return State{Path: "/tmp/x.log", Lines: []string{"a"}} },
		Wire:       func() WireState { return wireFixture() },
		Trajectory: func() TrajectoryState { return trajFixture() },
		Width:      func() int { return 100 },
		Height:     func() int { return 24 },
		Read:       s.read,
		Close:      func() tea.Cmd { return nil },
	})
}

// runRead is the host's half of a read: run the command and hand its
// answer back to the panel, which is what the update loop does.
//
// The panel deliberately does not apply its own command's result: the
// answer arrives as a message, and the host is what routes it. A test
// that skipped this step would be testing a path no user takes.
func runRead(t *testing.T, p *Panel, cmd tea.Cmd) bool {
	t.Helper()
	if cmd == nil {
		return false
	}
	msg, ok := cmd().(ReaderMsg)
	if !ok {
		t.Fatalf("the read answered %T, want ReaderMsg", cmd())
	}
	return p.ApplyReader(msg)
}

// TestReaderOpensTheCursorRow — `v` asks the host for the row the cursor
// is on, by the row's own stable ID.
//
// The ID is what the host uses to find the body, so asking for the wrong
// one would open a reader onto another row's text and nothing on screen
// would look broken.
func TestReaderOpensTheCursorRow(t *testing.T) {
	s := &readSeam{body: Body{Title: "whatever", Parts: []ReaderPart{{Text: "body"}}}}
	p := readerPanel(s)
	p.SetView(ViewTrajectory)
	p.Lines()

	rows := p.trajectory.flattened()
	if len(rows) == 0 {
		t.Fatal("the fixture has no rows")
	}
	want := rows[p.trajectory.cursor].ID

	handled, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	if !handled {
		t.Fatal("v was not handled")
	}
	if cmd == nil {
		t.Fatal("v did not ask the host for a body")
	}
	if len(s.asked) != 1 || s.asked[0] != want {
		t.Errorf("v asked for %v, want [%s]", s.asked, want)
	}
	if !runRead(t, p, cmd) {
		t.Error("the reader did not open on the host's answer")
	}
	if !p.ReaderOpen() {
		t.Error("the reader is not open")
	}
}

// TestReaderEnterReadsAnUnfoldableRow — Enter reads a row that has
// nothing to fold, rather than doing nothing.
//
// Before the reader, Enter on a row with no body was a no-op: it claimed
// the key and answered with nothing, so the row looked inert.
func TestReaderEnterReadsAnUnfoldableRow(t *testing.T) {
	s := &readSeam{body: Body{Parts: []ReaderPart{{Text: "the error text"}}}}
	p := readerPanel(s)
	p.SetView(ViewTrajectory)
	p.Lines()

	// Find a row with no body and no children: the fixture's thought step
	// is one.
	rows := p.trajectory.flattened()
	idx := -1
	for i, r := range rows {
		if !r.Foldable() {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("the fixture has no unfoldable row")
	}
	p.trajectory.cursor = idx
	id := rows[idx].ID

	_, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("Enter on %q asked the host for nothing", id)
	}
	if !runRead(t, p, cmd) {
		t.Error("Enter did not open the reader")
	}
	if !p.ReaderOpen() {
		t.Error("the reader is not open")
	}
}

// TestReaderEscBacksOutOneLevel — Esc closes the reader, not the panel.
//
// The reader was opened from the list, so backing out has to land on the
// list with its folds and cursor intact. A key that closed the whole
// surface from here would lose the position the user had been reading
// through.
func TestReaderEscBacksOutOneLevel(t *testing.T) {
	for _, key := range []tea.Key{
		{Code: tea.KeyEsc},
		{Code: 'q', Text: "q"},
	} {
		closed := false
		s := &readSeam{body: Body{Parts: []ReaderPart{{Text: "x"}}}}
		p := New(Seams{
			Logs:       func() State { return State{Path: "/tmp/x.log", Lines: []string{"a"}} },
			Trajectory: func() TrajectoryState { return trajFixture() },
			Width:      func() int { return 100 },
			Height:     func() int { return 24 },
			Read:       s.read,
			Close:      func() tea.Cmd { closed = true; return nil },
		})
		p.SetView(ViewTrajectory)
		p.Lines()
		cursor := p.trajectory.cursor

		_, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
		if !runRead(t, p, cmd) {
			t.Fatal("the reader did not open")
		}

		if handled, _ := p.HandleKey(key); !handled {
			t.Errorf("%v was not handled while reading", key)
		}
		if p.ReaderOpen() {
			t.Errorf("%v did not close the reader", key)
		}
		if closed {
			t.Errorf("%v closed the whole panel; it must back out one level", key)
		}
		if p.View() != ViewTrajectory {
			t.Errorf("%v changed the view to %v", key, p.View())
		}
		if p.trajectory.cursor != cursor {
			t.Errorf("%v moved the cursor from %d to %d", key, cursor, p.trajectory.cursor)
		}
	}
}

// TestReaderStaleAnswerIsDropped — an answer for a row the user is no
// longer looking at cannot replace what they opened instead.
func TestReaderStaleAnswerIsDropped(t *testing.T) {
	s := &readSeam{body: Body{Parts: []ReaderPart{{Text: "first"}}}}
	p := readerPanel(s)
	p.SetView(ViewTrajectory)
	p.Lines()

	_, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	if !runRead(t, p, cmd) {
		t.Fatal("the reader did not open")
	}
	first := p.reader.id

	if p.ApplyReader(ReaderMsg{ID: "traj/9999", Body: Body{Title: "wrong"}}) {
		t.Error("an answer for a row nobody asked about was applied")
	}
	if p.reader.id != first {
		t.Errorf("a stale answer replaced the open body: %q is now %q", first, p.reader.id)
	}
}

// TestReaderFormatsChangeTheTextNotTheContent — the format key
// reformats, and the content survives the round trip.
func TestReaderFormatsChangeTheTextNotTheContent(t *testing.T) {
	const raw = `{"path":"main.go","line":42}`
	p := New(Seams{Width: func() int { return 100 }, Height: func() int { return 24 }})
	p.reader = newReader("x", Body{Parts: []ReaderPart{{Label: "args", Text: raw}}})
	if !p.reader.structured {
		t.Fatal("a JSON body was not reported as structured")
	}

	asRaw := strings.Join(plain(p.reader.lines(100)), "\n")
	if !strings.Contains(asRaw, raw) {
		t.Errorf("the raw rendering lost the body: %q", asRaw)
	}

	p.reader.cycleFormat() // json
	asJSON := strings.Join(plain(p.reader.lines(100)), "\n")
	if !strings.Contains(asJSON, `"path": "main.go"`) {
		t.Errorf("the JSON rendering is not indented: %q", asJSON)
	}
	if strings.Contains(asJSON, `"path":"main.go"`) {
		t.Errorf("the JSON rendering did not re-indent: %q", asJSON)
	}

	p.reader.cycleFormat() // yaml
	asYAML := strings.Join(plain(p.reader.lines(100)), "\n")
	if !strings.Contains(asYAML, "path: main.go") {
		t.Errorf("the YAML rendering lost the field: %q", asYAML)
	}
	if !strings.Contains(asYAML, "line: 42") {
		t.Errorf("the YAML rendering lost the number: %q", asYAML)
	}

	p.reader.cycleFormat() // back to raw
	if got := strings.Join(plain(p.reader.lines(100)), "\n"); !strings.Contains(got, raw) {
		t.Errorf("cycling back to raw did not restore the body: %q", got)
	}
}

// TestReaderKeepsTheDocumentOrder — the format keys reorder nothing.
//
// A tool's arguments are evidence in the order the model wrote them, and
// both renderings are built by walking the source rather than through a
// map, so the order is the body's.
func TestReaderKeepsTheDocumentOrder(t *testing.T) {
	const raw = `{"zeta":1,"alpha":2,"mid":3}`
	for _, f := range []ReaderFormat{FormatJSON, FormatYAML} {
		got := renderPart(raw, f)
		z := strings.Index(got, "zeta")
		a := strings.Index(got, "alpha")
		m := strings.Index(got, "mid")
		if z < 0 || a < 0 || m < 0 {
			t.Fatalf("%v: a key is missing from %q", f, got)
		}
		if !(z < a && a < m) {
			t.Errorf("%v reordered the keys: %q", f, got)
		}
	}
}

// TestReaderOffersNoFormatForPlainText — a body that is not JSON does not
// advertise a format key that would change nothing.
func TestReaderOffersNoFormatForPlainText(t *testing.T) {
	p := New(Seams{Width: func() int { return 100 }, Height: func() int { return 24 }})
	p.reader = newReader("x", Body{Parts: []ReaderPart{{Label: "result", Text: "ok\nall good"}}})
	if p.reader.structured {
		t.Error("a plain-text body was reported as structured")
	}
	if got := ReaderLegend(false); strings.Contains(got, "t format") {
		t.Errorf("the legend offers a format key for a body with no format: %q", got)
	}
	if got := ReaderLegend(true); !strings.Contains(got, "t format") {
		t.Errorf("the legend dropped the format key for a JSON body: %q", got)
	}
}

// TestReaderUnparseablePartPassesThrough — a format the body cannot take
// shows the body unchanged, and the label says which rendering it is.
func TestReaderUnparseablePartPassesThrough(t *testing.T) {
	const body = "line one\nline two"
	p := New(Seams{Width: func() int { return 100 }, Height: func() int { return 24 }})
	p.reader = newReader("x", Body{Parts: []ReaderPart{
		{Label: "args", Text: `{"a":1}`},
		{Label: "result", Text: body},
	}})
	if !p.reader.structured {
		t.Fatal("the JSON part was not detected")
	}
	p.reader.cycleFormat() // json
	joined := strings.Join(plain(p.reader.lines(100)), "\n")
	if !strings.Contains(joined, "line one") || !strings.Contains(joined, "line two") {
		t.Errorf("the unparseable part was dropped: %q", joined)
	}
	// The converted part is marked, the other is not.
	if !strings.Contains(joined, "args  ·  json") {
		t.Errorf("the converted part is not marked: %q", joined)
	}
	if strings.Contains(joined, "result  ·  json") {
		t.Errorf("an unconverted part claims a format it does not have: %q", joined)
	}
}

// TestReaderWrapsInsteadOfClipping — a long line is wrapped, because the
// reader exists to show what the list could not.
func TestReaderWrapsInsteadOfClipping(t *testing.T) {
	long := strings.Repeat("word ", 60)
	p := New(Seams{Width: func() int { return 60 }, Height: func() int { return 24 }})
	p.reader = newReader("x", Body{Parts: []ReaderPart{{Text: long}}})

	lines := p.reader.lines(60)
	if len(lines) < 2 {
		t.Fatalf("a %d-cell line became %d row(s) at 60 columns", ansi.StringWidth(long), len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("row %d is %d columns: %q", i, w, line)
		}
		if strings.HasSuffix(line, " ") {
			t.Errorf("row %d has a trailing blank: %q", i, line)
		}
	}
	// Nothing was lost: the words are all there.
	joined := strings.Join(plain(lines), " ")
	if got := strings.Count(joined, "word"); got != 60 {
		t.Errorf("the wrap kept %d of 60 words", got)
	}
}

// TestReaderCopiesTheDocumentNotTheWrap — y hands over the body, not the
// reader's line breaks, so it pastes as the document it is.
func TestReaderCopiesTheDocumentNotTheWrap(t *testing.T) {
	var copied string
	p := New(Seams{
		Width:  func() int { return 40 },
		Height: func() int { return 24 },
		Copy:   func(text string) tea.Cmd { copied = text; return nil },
	})
	const raw = `{"a":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `"}`
	p.reader = newReader("x", Body{Parts: []ReaderPart{{Label: "args", Text: raw}}})
	p.reader.cycleFormat() // json
	_, _ = p.HandleKey(tea.Key{Code: 'y', Text: "y"})
	if copied == "" {
		t.Fatal("y copied nothing")
	}
	if strings.Contains(copied, "\x1b") {
		t.Errorf("the copy carries escapes: %q", copied)
	}
	// The long value must be on one line: a paste of the reader's wrap
	// would break the document.
	if n := strings.Count(copied, "\n"); n > 8 {
		t.Errorf("the copy looks wrapped (%d newlines): %q", n, copied)
	}
}

// TestReaderClickOnTheBarLeavesTheBody — the bar is painted while a body
// is open, so its labels must still work.
func TestReaderClickOnTheBarLeavesTheBody(t *testing.T) {
	s := &readSeam{body: Body{Parts: []ReaderPart{{Text: "x"}}}}
	p := readerPanel(s)
	p.SetView(ViewTrajectory)
	p.Lines()
	_, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	runRead(t, p, cmd)
	p.Lines() // redraw: the bar's spans are this frame's

	if len(p.barSpans) == 0 {
		t.Fatal("the bar recorded no spans")
	}
	span := p.barSpans[0]
	handled, _ := p.Click(0, span.Start)
	if !handled {
		t.Fatal("a click on the bar was not handled while reading")
	}
	if p.ReaderOpen() {
		t.Error("a click on the bar did not leave the body")
	}
	if p.View() != View(span.Index) {
		t.Errorf("the click landed on %v, want %v", p.View(), View(span.Index))
	}
}

// TestReaderFrameFitsTheHeight — the reader is the same frame, so it owes
// the same height guarantee.
func TestReaderFrameFitsTheHeight(t *testing.T) {
	for _, height := range []int{12, 20, 24, 40} {
		for _, width := range []int{40, 80, 120} {
			p := New(Seams{
				Width:  func() int { return width },
				Height: func() int { return height },
			})
			p.reader = newReader("x", Body{
				Title: "wire/msg/3 · tool · response",
				Note:  "the message's wire text",
				Parts: []ReaderPart{
					{Label: "args", Text: `{"a":1,"b":[1,2,3]}`},
					{Label: "result", Text: strings.Repeat("output line\n", 40)},
				},
			})
			rows := p.Lines()
			if len(rows) != height {
				t.Errorf("%dx%d: the reader drew %d rows, want %d", width, height, len(rows), height)
			}
			for i, row := range rows {
				if w := ansi.StringWidth(ansi.Strip(row)); w > width {
					t.Errorf("%dx%d: row %d is %d columns: %q", width, height, i, w, ansi.Strip(row))
				}
			}
		}
	}
}

// TestReaderWheelScrollsTheBody — the wheel scrolls what it is over.
func TestReaderWheelScrollsTheBody(t *testing.T) {
	p := New(Seams{Width: func() int { return 80 }, Height: func() int { return 20 }})
	p.reader = newReader("x", Body{Parts: []ReaderPart{{Text: strings.Repeat("line\n", 200)}}})
	p.Lines()

	if !p.Scroll(5) {
		t.Fatal("the wheel was not handled while reading")
	}
	if p.reader.scroll == 0 {
		t.Error("the wheel did not scroll the body")
	}
	// The list's cursor must not move: it is not what is on screen.
	if p.trajectory.cursor != 0 {
		t.Errorf("the wheel moved the list cursor to %d", p.trajectory.cursor)
	}
}

// TestReaderEmptyBodySaysSo — a row the host answers with nothing opens
// onto a sentence, not onto silence.
func TestReaderEmptyBodySaysSo(t *testing.T) {
	p := readerPanel(&readSeam{body: Body{}})
	p.SetView(ViewTrajectory)
	p.Lines()
	_, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	if !runRead(t, p, cmd) {
		t.Fatal("the reader did not open on an empty body")
	}
	joined := strings.Join(plain(p.Lines()), "\n")
	if !strings.Contains(joined, "no body stored") {
		t.Errorf("an empty body opened onto nothing:\n%s", joined)
	}
}

// TestReaderErrorIsPrinted — a body the host could not read says why.
func TestReaderErrorIsPrinted(t *testing.T) {
	p := readerPanel(&readSeam{err: errTestRead})
	p.SetView(ViewTrajectory)
	p.Lines()
	_, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	if !runRead(t, p, cmd) {
		t.Fatal("a read error did not open the reader")
	}
	joined := strings.Join(plain(p.Lines()), "\n")
	if !strings.Contains(joined, "could not read") {
		t.Errorf("a read error was swallowed:\n%s", joined)
	}
	if !strings.Contains(joined, "boom") {
		t.Errorf("the error's own text is missing:\n%s", joined)
	}
}

type testErr struct{}

func (testErr) Error() string { return "boom" }

var errTestRead error = testErr{}

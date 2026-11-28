// cycle.go — the Shift+Tab lap as a table, walked one press at a time.
//
// # Why a table
//
// The mode cycle is a sequence, and a sequence is data. The first version of
// scene 49 asserted it by exclusion: press shift+tab, then assert that the
// word `plan · ` is absent. That assertion is satisfied by a cycle that skips
// a stop, by one that visits a stop twice, and by a blank screen — nothing in
// it names the stop that was supposed to be there, so a wrong order has no
// way to fail on the row it is wrong at.
//
// A scenario now declares the lap with one MODE_CYCLE_ROW per stop and walks
// it with MODE_CYCLE. Each row carries the two things the screen can be asked
// about a stop — the status row's right segment and the composer's banner —
// so every press is checked against its own row of the table and a mismatch
// names the mode that press should have reached.
//
// # Why the two checks are positive
//
// Both checks describe what the stop *shows*:
//
//   - the status row's right segment, exactly, as the last thing on the line,
//     preceded by the column padding;
//   - the composer's placeholder, exactly, or a bare ">" for the default stop.
//
// Neither is a "must not appear". The right-segment check subsumes the
// exclusion the old scene was reaching for: the segment must begin at the
// padding, so a row carrying a second mode word in front of the expected one
// — `auto · plan · …` — fails, because the expected word is then preceded by
// one space and a separator rather than by padding.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// cycleTimeout is how long a stop has to appear after its press. The row is
// repainted on the next frame, so this only covers a slow first paint; a
// wrong cycle never satisfies the check and fails after the whole timeout.
const cycleTimeout = 10 * time.Second

// captureSnapshot records a labelled frame: a parity diff for a two-pane
// run, the frame itself for a single-pane one.
func (sc *Scenario) captureSnapshot(driver *TmuxDriver, label string) error {
	sc.LastSnapshot = label
	if sc.Target != TargetBoth {
		out, err := driver.CapturePane("", true)
		if err != nil {
			return err
		}
		fmt.Printf("=== SNAPSHOT [%s] ===\n%s\n", label, out)
		return nil
	}
	left, right, err := driver.CaptureBoth(true)
	if err != nil {
		return err
	}
	res := CompareSnapshots(left, right, label)
	if _, exists := sc.Snapshots[label]; !exists {
		sc.SnapshotOrder = append(sc.SnapshotOrder, label)
	}
	sc.Snapshots[label] = res
	fmt.Print(res.Report)

	// Save standalone HTML & TXT visual diff reports
	_ = os.WriteFile(fmt.Sprintf("/tmp/tuialign_%s.html", label), []byte(res.HTMLReport), 0644)
	_ = os.WriteFile(fmt.Sprintf("/tmp/tuialign_%s.txt", label), []byte(res.Report), 0644)
	return nil
}

// walkModeCycle presses shift+tab once per table row after the first and
// checks each landing.
//
// Row 0 is the state the scenario booted in, so it is asserted without a
// press: that is what pins that a stored mode is honoured rather than
// replaced by the default. The table closes the lap by repeating row 0 at the
// end, which is what makes it a cycle rather than a list that walks off the
// end.
func (sc *Scenario) walkModeCycle(driver *TmuxDriver) error {
	if len(sc.ModeCycle) == 0 {
		return fmt.Errorf("MODE_CYCLE with no MODE_CYCLE_ROW table")
	}
	for i, stop := range sc.ModeCycle {
		if i > 0 {
			var err error
			if sc.Target == TargetBoth {
				err = driver.SendBoth("BTab")
			} else {
				err = driver.SendKeys("", "BTab")
			}
			if err != nil {
				return fmt.Errorf("MODE_CYCLE stop %d (%s): send shift+tab: %w", i, stop.Mode, err)
			}
		}
		if err := sc.awaitCycleStop(driver, i, stop); err != nil {
			return err
		}
	}
	return nil
}

// awaitCycleStop polls the pane(s) until the stop's row and banner are both
// on screen, then reports the pass. A stop that never arrives fails with what
// the status row actually said, so the report names the mode that was reached
// instead of only the one that was wanted.
func (sc *Scenario) awaitCycleStop(driver *TmuxDriver, idx int, stop CycleStop) error {
	panes := []struct{ label, pane string }{{"", ""}}
	if sc.Target == TargetBoth {
		panes = []struct{ label, pane string }{
			{"agy (left, the reference)", "0"},
			{"pigasus (right, the target)", "1"},
		}
	}

	deadline := time.Now().Add(cycleTimeout)
	for {
		var firstErr error
		var seen []string
		for _, p := range panes {
			out, err := driver.CapturePane(p.pane, false)
			if err != nil {
				return fmt.Errorf("MODE_CYCLE stop %d: capture %s: %w", idx, p.label, err)
			}
			if err := checkCycleStop(out, stop); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				if p.label != "" {
					seen = append(seen, p.label+": "+describeStatusRow(out))
				}
			}
		}
		if firstErr == nil {
			fmt.Printf("   [ASSERT PASS] stop %d is %q (%s; composer %q)\n",
				idx, stop.Mode, stop.RowTail, stop.Banner)
			return nil
		}
		if time.Now().After(deadline) {
			if len(seen) > 0 {
				return fmt.Errorf("MODE_CYCLE stop %d wants mode %q — row tail %q, composer %q — but found %s",
					idx, stop.Mode, stop.RowTail, stop.Banner, strings.Join(seen, "; "))
			}
			return fmt.Errorf("MODE_CYCLE stop %d wants mode %q — row tail %q, composer %q — %w",
				idx, stop.Mode, stop.RowTail, stop.Banner, firstErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// checkCycleStop is the whole assertion for one stop, against one captured
// pane.
func checkCycleStop(screen string, stop CycleStop) error {
	if err := checkStatusRowTail(screen, stop.RowTail); err != nil {
		return err
	}
	return checkComposerBanner(screen, stop.Banner)
}

// checkStatusRowTail asserts that the status row's right segment is exactly
// want, and that it begins at the column padding.
//
// The status row is the last line with anything on it. Its right segment is
// right-aligned, so "the segment is exactly want" is a suffix test; the
// padding test is what makes it a statement about the *segment* rather than
// about any suffix of the line. Without it, `auto · plan · Gemini 3.8 Flash ·
// high` would satisfy want = `plan · Gemini 3.8 Flash · high`, and the
// default stop's want = `Gemini 3.8 Flash · high` would be satisfied by every
// named mode on the screen — the check would pass for the wrong reason
// exactly where it matters.
func checkStatusRowTail(screen, want string) error {
	row := lastNonEmptyLine(screen)
	if row == "" {
		return fmt.Errorf("status row is not on screen")
	}
	if !strings.HasSuffix(row, want) {
		return fmt.Errorf("status row %q does not end with %q", row, want)
	}
	pad := row[:len(row)-len(want)]
	// The right segment is separated from the left hint by at least two
	// columns of padding: the padding is what the row is padded with, and a
	// one-column gap is a separator (` · `), not padding.
	if pad != "" && !strings.HasSuffix(pad, "  ") {
		return fmt.Errorf("status row %q has %q in front of %q; the mode segment must start at the column padding, not after a separator",
			row, strings.TrimRight(pad, " "), want)
	}
	return nil
}

// checkComposerBanner asserts the composer's placeholder is exactly want, or
// that it is empty when want is NoBanner.
func checkComposerBanner(screen, want string) error {
	line, ok := composerLine(screen)
	if !ok {
		return fmt.Errorf("composer line is not on screen")
	}
	got := strings.TrimPrefix(line, ">")
	got = strings.TrimPrefix(got, " ")
	if want == NoBanner {
		if got != "" {
			return fmt.Errorf("composer shows %q; the default mode names nothing", got)
		}
		return nil
	}
	if got != want {
		return fmt.Errorf("composer shows %q, want %q", got, want)
	}
	return nil
}

// composerLine returns the composer's line — the prompt marker and what the
// placeholder put after it — with the marker trimmed of surrounding space.
//
// The composer is the last line that starts with the prompt marker, which is
// what keeps a `>` inside the banner or the logo from being mistaken for it.
func composerLine(screen string) (string, bool) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimRight(lines[i], " "), ">") {
			return strings.TrimRight(lines[i], " "), true
		}
	}
	return "", false
}

// lastNonEmptyLine is the status row: the last line with anything on it.
func lastNonEmptyLine(screen string) string {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimRight(lines[i], " "); s != "" {
			return s
		}
	}
	return ""
}

// describeStatusRow renders what the row said, for a failure message.
func describeStatusRow(screen string) string {
	row := lastNonEmptyLine(screen)
	if row == "" {
		return "no status row"
	}
	return fmt.Sprintf("status row %q", strings.TrimLeft(row, " "))
}

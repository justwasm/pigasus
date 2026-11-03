// runner_test.go — the external runner: the status-line script's output,
// the failure counter and the window title.
//
// Evidence:
//   - 【一手】 the reference's own status line documentation — "The CLI runs
//     your script at most once every 300 ms and stops any run that takes
//     longer than 5 seconds" and "After 30 failed runs in a row, it stops
//     running the script for the rest of the session".
//   - 【一手】 the reference's title script printed
//     "TITLE-A\nTITLE-B\t\x1b[31mCOLOR\x07\n" and the window title became
//     "TITLE-A TITLE-B[31mCOLOR".
package statusline_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// TestRenderStatusLine_StoresOutput pins the rendered output.
func TestRenderStatusLine_StoresOutput(t *testing.T) {
	state := &statusline.State{}
	ok := statusline.RenderStatusLine(state,
		statusline.Config{Type: "command", Command: "printf 'CUSTOM ROW'", Enabled: true}, nil)
	if !ok {
		t.Error("RenderStatusLine = false, want true for an active block")
	}
	out, err := state.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if out != "CUSTOM ROW" {
		t.Errorf("rendered output = %q, want %q", out, "CUSTOM ROW")
	}
	if state.Failures() != 0 {
		t.Errorf("Failures = %d, want 0 after a successful run", state.Failures())
	}
}

// TestRenderStatusLine_InactiveBlock pins that a block without a command
// runs nothing.
func TestRenderStatusLine_InactiveBlock(t *testing.T) {
	state := &statusline.State{}
	if statusline.RenderStatusLine(state, statusline.Config{Enabled: true}, nil) {
		t.Error("RenderStatusLine = true, want false without a command")
	}
	if statusline.RenderStatusLine(state, statusline.Config{Command: "printf 'x'", Enabled: false}, nil) {
		t.Error("RenderStatusLine = true, want false for a disabled block")
	}
}

// TestRenderStatusLine_Failure pins the failure counter and the error the
// status area's error card shows.
func TestRenderStatusLine_Failure(t *testing.T) {
	state := &statusline.State{}
	statusline.RenderStatusLine(state,
		statusline.Config{Type: "command", Command: "exit 3", Enabled: true}, nil)
	out, err := state.Current()
	if out != "" {
		t.Errorf("rendered output = %q, want empty after a failure", out)
	}
	if err == nil {
		t.Fatal("Current returned no error after a failed run")
	}
	if err.Error() != "command failed: exit status 3" {
		t.Errorf("error = %q, want the reference's failure text", err.Error())
	}
	if state.Failures() != 1 {
		t.Errorf("Failures = %d, want 1", state.Failures())
	}
}

// TestRenderStatusLine_Disabled pins that a status line the reference
// switched off runs no more scripts.
func TestRenderStatusLine_Disabled(t *testing.T) {
	state := &statusline.State{}
	for i := 0; i < statusline.MaxConsecutiveFailures; i++ {
		statusline.RenderStatusLine(state,
			statusline.Config{Type: "command", Command: "exit 3", Enabled: true}, nil)
	}
	if !state.Disabled() {
		t.Fatalf("the status line is not disabled after %d failures",
			statusline.MaxConsecutiveFailures)
	}
	statusline.RenderStatusLine(state,
		statusline.Config{Type: "command", Command: "printf 'CUSTOM ROW'", Enabled: true}, nil)
	out, _ := state.Current()
	if out != "" {
		t.Errorf("rendered output = %q, want empty: the status line is off", out)
	}
}

// TestRenderWindowTitle_Sanitizes pins the window title the title script
// produces.
//
// Evidence 【一手】: agy 1.4.7 set the window title to
// "TITLE-A TITLE-B[31mCOLOR" — the tab, the escape and the bell characters
// are gone, the "[31m" survives as literal text, and the two lines are
// joined by one space.
func TestRenderWindowTitle_Sanitizes(t *testing.T) {
	titles := &statusline.TitleStore{}
	statusline.RenderWindowTitle(titles,
		statusline.Config{Type: "command", Command: "printf 'TITLE-A\\nTITLE-B\\t\\033[31mCOLOR\\007\\n'", Enabled: true}, nil)
	if got := titles.Get(); got != "TITLE-A TITLE-B[31mCOLOR" {
		t.Errorf("window title = %q, want %q", got, "TITLE-A TITLE-B[31mCOLOR")
	}
}

// TestRenderWindowTitle_FailureKeepsTheTitle pins that a failing title
// script leaves the previous title alone.
func TestRenderWindowTitle_FailureKeepsTheTitle(t *testing.T) {
	titles := &statusline.TitleStore{}
	titles.Set("KEPT")
	statusline.RenderWindowTitle(titles,
		statusline.Config{Type: "command", Command: "exit 3", Enabled: true}, nil)
	if got := titles.Get(); got != "KEPT" {
		t.Errorf("window title = %q, want %q", got, "KEPT")
	}
}

// TestTitleStore_RoundTrip pins the store: a title written on the tick
// goroutine is read back on the model's goroutine.
func TestTitleStore_RoundTrip(t *testing.T) {
	titles := &statusline.TitleStore{}
	if got := titles.Get(); got != "" {
		t.Errorf("Get() = %q, want empty for a fresh store", got)
	}
	titles.Set("FIRST")
	titles.Set("SECOND")
	if got := titles.Get(); got != "SECOND" {
		t.Errorf("Get() = %q, want %q", got, "SECOND")
	}
}

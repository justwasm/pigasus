// area_test.go — the status area's rows.
//
// The rows are the reference's own bytes: the built-in row, the padding,
// the disabled message and the error card. The tests pin them here, in
// the package that owns them.
package statusline_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// TestAreaRows_NoScript pins the built-in row alone: a session without a
// custom status line draws nothing else.
func TestAreaRows_NoScript(t *testing.T) {
	rows := statusline.AreaRows(statusline.AreaState{Builtin: "BUILTIN"})
	if len(rows) != 1 || rows[0] != "BUILTIN" {
		t.Errorf("rows = %q, want the built-in row alone", rows)
	}
}

// TestAreaRows_InactiveScript pins that a script that is not configured
// leaves the built-in row alone even when it has run before.
func TestAreaRows_InactiveScript(t *testing.T) {
	state := &statusline.State{}
	state.SetRendered("CUSTOM", nil)
	rows := statusline.AreaRows(statusline.AreaState{
		Builtin: "BUILTIN",
		State:   state,
	})
	if len(rows) != 1 || rows[0] != "BUILTIN" {
		t.Errorf("rows = %q, want the built-in row alone", rows)
	}
}

// TestAreaRows_StackedWithPadding pins the reference's stacked layout:
// the built-in row, the padding's blank rows, then the script's output.
func TestAreaRows_StackedWithPadding(t *testing.T) {
	state := &statusline.State{}
	state.SetRendered("CUSTOM", nil)
	rows := statusline.AreaRows(statusline.AreaState{
		Builtin: "BUILTIN",
		Active:  true,
		Config: statusline.Config{
			Enabled:          true,
			Padding:          2,
			StackWithDefault: true,
		},
		State: state,
	})
	want := []string{"BUILTIN", "", "", "CUSTOM"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

// TestAreaRows_ReplacesTheBuiltInRow pins that a script without
// `stack_with_default` replaces the built-in row.
func TestAreaRows_ReplacesTheBuiltInRow(t *testing.T) {
	state := &statusline.State{}
	state.SetRendered("CUSTOM", nil)
	rows := statusline.AreaRows(statusline.AreaState{
		Builtin: "BUILTIN",
		Active:  true,
		Config:  statusline.Config{Enabled: true, Command: "echo hi"},
		State:   state,
	})
	if len(rows) != 1 || rows[0] != "CUSTOM" {
		t.Errorf("rows = %q, want the script's row alone", rows)
	}
}

// TestAreaRows_Disabled pins the reference's disabled message: it takes
// the custom row's place.
func TestAreaRows_Disabled(t *testing.T) {
	state := &statusline.State{}
	for i := 0; i < statusline.MaxConsecutiveFailures; i++ {
		state.RecordFailure()
	}
	rows := statusline.AreaRows(statusline.AreaState{
		Builtin: "BUILTIN",
		Active:  true,
		Config:  statusline.Config{Enabled: true, Command: "echo hi"},
		State:   state,
	})
	if len(rows) != 1 || rows[0] != statusline.DisabledMessage {
		t.Errorf("rows = %q, want the disabled message", rows)
	}
}

// TestErrorRows_Bytes pins the reference's error card, byte for byte.
//
// Evidence 【一手】: agy 1.4.7 rendered
//
//	⚠ Statusline Error
//	  ⎿  Statusline error:
//	     command failed: exit status 3 (stderr: boom)
//	     Full logs at: /Users/gear/.gemini/antigravity-cli/log/cli-20261025_210717.log
func TestErrorRows_Bytes(t *testing.T) {
	rows := statusline.ErrorRows(
		errors.New("command failed: exit status 3 (stderr: boom)"),
		"/Users/gear/.gemini/antigravity-cli/log/cli-20261025_210717.log",
	)
	want := []string{
		"\x1b[38;2;203;75;22m⚠ Statusline Error\x1b[39m",
		"  \x1b[38;2;38;139;210;1m⎿  \x1b[0m\x1b[38;2;88;110;117;22mStatusline error:\x1b[39m",
		"     \x1b[38;2;88;110;117;22mcommand failed: exit status 3 (stderr: boom)\x1b[39m",
		"     \x1b[38;2;88;110;117;22mFull logs at: /Users/gear/.gemini/antigravity-cli/log/cli-20261025_210717.log\x1b[39m",
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d =\n%q\nwant\n%q", i, rows[i], want[i])
		}
	}
}

// TestAreaRows_ErrorCard pins that a failed script shows the error card
// in the custom row's place.
func TestAreaRows_ErrorCard(t *testing.T) {
	state := &statusline.State{}
	state.SetRendered("", errors.New("boom"))
	rows := statusline.AreaRows(statusline.AreaState{
		Builtin: "BUILTIN",
		Active:  true,
		Config:  statusline.Config{Enabled: true, Command: "echo hi"},
		State:   state,
		LogPath: "/tmp/cli.log",
	})
	if len(rows) != 4 {
		t.Fatalf("rows = %q, want the four rows of the error card", rows)
	}
	if !strings.Contains(rows[0], "⚠ Statusline Error") {
		t.Errorf("row 0 = %q, want the error card's title", rows[0])
	}
	if !strings.Contains(rows[3], "Full logs at: /tmp/cli.log") {
		t.Errorf("row 3 = %q, want the log path", rows[3])
	}
}

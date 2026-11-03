// block_test.go — the bash-mode block's rendered bytes.
//
// The block's rows are the reference's own bytes, so they are pinned
// here: every branch of the head row, the body and the "  ⎿  " marker
// is asserted byte for byte, not by substring.
package bashmode

import (
	"fmt"
	"strings"
	"testing"
)

// TestBlockRows pins the six measured shapes of a bash block: a success
// with output, a success without output, a failure with output, a failure
// without output (the Go error text), a running head, and a running head
// that already carries the output it produced.
//
// 【一手】 agy 1.4.4, byte-level pty captures 2026-10-22
// (docs/cycles/bash-mode/).
func TestBlockRows(t *testing.T) {
	cases := []struct {
		name  string
		block Block
		want  []string
	}{
		{
			name:  "success with output",
			block: Block{Cmd: "echo hello", Body: "hello"},
			want: []string{
				"\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m",
				"\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mhello\x1b[m",
				"",
			},
		},
		{
			name:  "success without output prints no detail row",
			block: Block{Cmd: "true"},
			want: []string{
				"\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! true\x1b[m",
				"",
			},
		},
		{
			name:  "failure with output keeps the output and adds no status line",
			block: Block{Cmd: "echo out; echo err >&2; exit 3", Body: "out\nerr", Failed: true},
			want: []string{
				"\x1b[38;2;220;50;47m●\x1b[m \x1b[38;2;38;139;210;1m! echo out; echo err >&2; exit 3\x1b[m",
				"\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;220;50;47;22mout\x1b[m",
				"\x1b[38;2;220;50;47merr\x1b[m",
				"",
			},
		},
		{
			name:  "failure without output is the Go error text",
			block: Block{Cmd: "exit 3", Body: "exit status 3", Failed: true},
			want: []string{
				"\x1b[38;2;220;50;47m●\x1b[m \x1b[38;2;38;139;210;1m! exit 3\x1b[m",
				"\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;220;50;47;22mexit status 3\x1b[m",
				"",
			},
		},
		{
			name:  "running head keeps the hollow bullet",
			block: Block{Cmd: "top", Running: true},
			want: []string{
				"\x1b[38;2;181;137;0m○\x1b[m \x1b[38;2;38;139;210;1m! top\x1b[m",
				"",
			},
		},
		{
			name:  "running head with the output it produced",
			block: Block{Cmd: "top", Body: "first", Running: true},
			want: []string{
				"\x1b[38;2;181;137;0m○\x1b[m \x1b[38;2;38;139;210;1m! top\x1b[m",
				"\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mfirst\x1b[m",
				"",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.block.Lines(false, 40)
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %d, want %d:\n%#v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("row %d =\n%q\nwant\n%q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestBlockBodyCap pins the collapsed long-output form: the elided count,
// the last `height/2` rows, and the expand hint on the final row.
//
// 【一手】 agy 1.4.5 at 120×40: `!seq 1 21` →
//
//	  ⎿  <output +1 lines>
//	2 … 21 (ctrl+o to expand)
func TestBlockBodyCap(t *testing.T) {
	var lines []string
	for i := 1; i <= 21; i++ {
		lines = append(lines, fmt.Sprintf("%d", i))
	}
	rows := (&Block{Cmd: "seq 1 21", Body: strings.Join(lines, "\n")}).Lines(false, 40)

	// head + marker + 20 body rows + blank
	if len(rows) != 23 {
		t.Fatalf("rows = %d, want 23: %#v", len(rows), rows)
	}
	if want := "\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! seq 1 21\x1b[m"; rows[0] != want {
		t.Fatalf("head = %q, want %q", rows[0], want)
	}
	wantMarker := "\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22m<output +1 lines>\x1b[m"
	if rows[1] != wantMarker {
		t.Fatalf("marker = %q, want %q", rows[1], wantMarker)
	}
	if want := "\x1b[38;2;88;110;117m2\x1b[m"; rows[2] != want {
		t.Fatalf("first kept row = %q, want %q (continuation style, no ⎿)", rows[2], want)
	}
	wantLast := "\x1b[38;2;88;110;117m21\x1b[m \x1b[38;2;88;110;117m(ctrl+o to expand)\x1b[m"
	if rows[len(rows)-2] != wantLast {
		t.Fatalf("last row = %q, want %q", rows[len(rows)-2], wantLast)
	}
	if rows[len(rows)-1] != "" {
		t.Fatal("missing trailing blank row")
	}

	// 20 rows never truncate and never show a hint (【一手】 `!seq 1 20`).
	rows = (&Block{Cmd: "seq 1 20", Body: strings.Join(lines[:20], "\n")}).Lines(false, 40)
	if len(rows) != 22 {
		t.Fatalf("20-line rows = %d, want 22", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r, "ctrl+o") || strings.Contains(r, "<output") {
			t.Fatalf("short body grew a marker/hint: %q", r)
		}
	}

	// The cap is half the terminal height (measured 20/25/30 at 40/50/60):
	// 40 lines at height 60 show 30 and elide 10.
	var forty []string
	for i := 1; i <= 40; i++ {
		forty = append(forty, fmt.Sprintf("%d", i))
	}
	rows = (&Block{Cmd: "seq 1 40", Body: strings.Join(forty, "\n")}).Lines(false, 60)
	if want := "\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22m<output +10 lines>\x1b[m"; rows[1] != want {
		t.Fatalf("60-row terminal marker = %q, want %q", rows[1], want)
	}
	if len(rows) != 33 { // head + marker + 30 + blank
		t.Fatalf("60-row rows = %d, want 33", len(rows))
	}
}

// TestBlockExpanded: ctrl+o drops the marker, shows the full body and flips
// the hint to "(ctrl+o to collapse)".
func TestBlockExpanded(t *testing.T) {
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("%d", i))
	}
	b := &Block{Cmd: "seq 1 200", Body: strings.Join(lines, "\n")}

	collapsed := b.Lines(false, 40)
	expanded := b.Lines(true, 40)
	if len(expanded) <= len(collapsed) {
		t.Fatalf("expanded rows = %d, collapsed = %d", len(expanded), len(collapsed))
	}
	for _, r := range expanded {
		if strings.Contains(r, "<output +") {
			t.Fatalf("expanded block kept the marker: %q", r)
		}
	}
	last := expanded[len(expanded)-2]
	if !strings.Contains(last, "200\x1b[m ") || !strings.Contains(last, "(ctrl+o to collapse)") {
		t.Fatalf("expanded last row = %q", last)
	}
}

// sections_shelf_test.go — cycle 364 (agy 1.4.11): the Discover tab's
// shelves.
//
// 【一手】 agy 1.4.11: "each shelf now shows its first 6 plugins followed by
// a `⋯ N more — enter to show` row".
package pluginsui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/plugins"
)

func shelfRows(n int) []plugins.Row {
	out := make([]plugins.Row, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, plugins.Row{Name: "p" + string(rune('a'+i))})
	}
	return out
}

// A shelf with more than six plugins shows six and a "more" row.
func TestShelfRowsTruncatesAtSix(t *testing.T) {
	rows := ShelfRows(shelfRows(9), false)
	if len(rows) != 7 {
		t.Fatalf("rows = %d, want 7 (6 plugins + the more row)", len(rows))
	}
	for i := 0; i < 6; i++ {
		if rows[i].More {
			t.Fatalf("row %d is the more row", i)
		}
	}
	if !rows[6].More {
		t.Fatal("the last row is not the more row")
	}
	if rows[6].Hidden != 3 {
		t.Errorf("hidden = %d, want 3", rows[6].Hidden)
	}
}

// An expanded shelf shows every plugin and no "more" row.
func TestShelfRowsExpandedShowsAll(t *testing.T) {
	rows := ShelfRows(shelfRows(9), true)
	if len(rows) != 9 {
		t.Fatalf("rows = %d, want 9", len(rows))
	}
	for i, r := range rows {
		if r.More {
			t.Fatalf("row %d is the more row", i)
		}
	}
}

// A short shelf has no "more" row at all.
func TestShelfRowsShortShelfHasNoMoreRow(t *testing.T) {
	rows := ShelfRows(shelfRows(3), false)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	for i, r := range rows {
		if r.More {
			t.Fatalf("row %d is the more row", i)
		}
	}
}

// The more row's text is the reference's own wording.
func TestShelfMoreText(t *testing.T) {
	if got, want := ShelfMoreText(3), "⋯ 3 more — enter to show"; got != want {
		t.Fatalf("ShelfMoreText = %q, want %q", got, want)
	}
}

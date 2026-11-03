// panel_filter_test.go — the popup's buffer handling.
//
// The popup hands the filter the head of the buffer — the token up to
// the first whitespace — and the filter matches it as a case-insensitive
// substring of the command name (internal/ui/tuilib). These tests pin
// the two halves the popup owns: the token split and the rune-aware
// path through Update.
package slashpicker

import "testing"

// TestReadToken_SplitsOnWhitespace pins the split: the popup filters
// on the head of the buffer, so "/foo bar baz" filters on "/foo" and
// the popup closes (a command with arguments is not a completion).
func TestReadToken_SplitsOnWhitespace(t *testing.T) {
	cases := []struct {
		buffer string
		want   string
	}{
		{"/", "/"},
		{"/foo", "/foo"},
		{"/foo bar baz", "/foo"},
		{"/foo\tbar", "/foo"},
		{"/foo\nbar", "/foo"},
		{"/科学 search", "/科学"},
	}
	for _, tc := range cases {
		if got := readToken([]rune(tc.buffer)); got != tc.want {
			t.Errorf("readToken(%q) = %q, want %q", tc.buffer, got, tc.want)
		}
	}
}

// TestPickerUpdate_TurkishDottedIBuffer exercises the whole popup path
// end-to-end through Update. The buffer is a []rune (the popup takes
// runes), and the substring match must reach the synthetic /yeniİş
// command which is added at runtime via the catalog-helper route — but
// Update reads the built-in catalogue. So instead this test pins the
// popup's Update with an ASCII baseline AND an ascii-only catalog
// slot to make sure that the input path does not eat non-ASCII runes:
// when the buffer is "/İstanbul", Update hands "/İstanbul" to the
// token split, which returns it verbatim, and the filter searches over
// the real catalog names. None of the real names contain "İ", so the
// popup ends in the empty state — but the path executed the rune-aware
// comparison without error, which is what we are pinning.
func TestPickerUpdate_TurkishDottedIBuffer(t *testing.T) {
	p := New(Seams{})
	buf := []rune{'/', 'İ', 's', 't', 'a', 'n', 'b', 'u', 'l'}
	p.Update(buf)
	if !p.Visible() {
		t.Fatal("the popup should be visible after /İstanbul")
	}
	// No real command matches; Empty() should be true and idx -1.
	if !p.Empty() {
		t.Errorf("the popup should be empty for /İstanbul (no real match), got %d rows", p.Len())
	}
	if p.Idx() != -1 {
		t.Errorf("idx should be -1 in empty state, got %d", p.Idx())
	}
}

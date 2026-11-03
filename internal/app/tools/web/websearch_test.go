package web

import (
	"strings"
	"testing"
)

func TestFormatSearchWebResult(t *testing.T) {
	got := formatSearchWebResult("golang", "Go is an open source programming language.")
	wantPrefix := "The search for \"golang\" returned the following summary:\nGo is an open source programming language."
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("formatSearchWebResult prefix mismatch:\ngot:  %q\nwant: %q", got, wantPrefix)
	}
	wantSuffix := "\n\nNote: Source URLs are HTTP redirects; check where they resolve if you need the direct link."
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("formatSearchWebResult suffix mismatch:\ngot:  %q\nwant: %q", got, wantSuffix)
	}
}

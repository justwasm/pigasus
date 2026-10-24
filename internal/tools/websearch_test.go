package tools

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

func TestCatalogSearchWebHasProvider(t *testing.T) {
	cat := Catalog("/tmp")
	tool, ok := cat.Get("search_web")
	if !ok {
		t.Fatal("search_web not found in catalog")
	}
	dt, ok := tool.(*describedTool)
	if !ok {
		t.Fatalf("search_web tool is %T, want *describedTool", tool)
	}
	sw, ok := dt.Tool.(*SearchWeb)
	if !ok {
		t.Fatalf("search_web inner tool is %T, want *SearchWeb", dt.Tool)
	}
	if sw.Provider == nil {
		t.Fatal("expected search_web to have performWebSearch Provider wired in Catalog")
	}
}

package patches

import (
	"strings"
	"testing"
)

func TestSyntheticBundlePatchesApply(t *testing.T) {
	opts := Options{
		MobileUX: true,
	}

	var synthetic strings.Builder
	for _, p := range All() {
		if p.Target == MainJS && p.Kind == Literal && p.Find != "" {
			synthetic.WriteString("\n/* filler */\n")
			synthetic.WriteString(p.Find)
			synthetic.WriteString("\n/* filler */\n")
		}
	}

	bundle := []byte(synthetic.String())
	applied, report := Apply(MainJS, bundle, opts)
	if len(applied) == 0 {
		t.Fatal("expected patched bundle, got empty")
	}

	matchedCount := 0
	for _, res := range report {
		if res.Status == StatusApplied {
			matchedCount++
		}
	}
	if matchedCount == 0 {
		t.Errorf("no literal patches matched synthetic bundle")
	}
}

func TestHTMLInjection(t *testing.T) {
	html := []byte("<!DOCTYPE html><html><head><title>Test</title></head><body></body></html>")
	opts := Options{MobileUX: true}
	patched, _ := Apply(HTML, html, opts)
	if !strings.Contains(string(patched), "visualViewport") {
		t.Errorf("HTML head injection missing visualViewport script")
	}
}

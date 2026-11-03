package cliapp

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
)

func TestPrintUnknownModel_EmitsJSONEnvelope(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("model", "does-not-exist-model", "")
	fs.String("output-format", "json", "")
	fs.String("effort", "", "")
	fs.String("print", "hi", "")
	fs.Bool("p", false, "")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	validateErr := validateRootFlags(fs, true)

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if validateErr == nil {
		t.Fatal("expected validateRootFlags to return an error for unknown model")
	}

	wantPrefix := `{"conversation_id":"","status":"ERROR","response":"","error":"invalid model selection (--model \"does-not-exist-model\" --effort \"\"): model does-not-exist-model is not recognized as a known model or custom model in settings`
	if !strings.HasPrefix(out, wantPrefix) {
		t.Errorf("stdout does not have expected error envelope prefix:\ngot: %s\nwant prefix: %s", out, wantPrefix)
	}
	if !strings.Contains(out, `"duration_seconds":0,"num_turns":0`) {
		t.Errorf("stdout missing duration/num_turns: %s", out)
	}
}

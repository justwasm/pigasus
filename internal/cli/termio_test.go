package cli

import (
	"bytes"
	"log"
	"os"
	"testing"
)

func TestTermio_RedirectAndSavedStreams(t *testing.T) {
	var stdoutBuf, stderrBuf bytes.Buffer
	ss := Redirect(&stdoutBuf, &stderrBuf)
	defer ss.Close()

	if ss.StdoutWriter() != &stdoutBuf {
		t.Errorf("StdoutWriter() = %v, want %v", ss.StdoutWriter(), &stdoutBuf)
	}
	if ss.StderrWriter() != &stderrBuf {
		t.Errorf("StderrWriter() = %v, want %v", ss.StderrWriter(), &stderrBuf)
	}
	if ss.TerminalWriter() != &stdoutBuf {
		t.Errorf("TerminalWriter() = %v, want %v", ss.TerminalWriter(), &stdoutBuf)
	}

	var nilSS *SavedStreams
	if nilSS.StdoutWriter() != os.Stdout {
		t.Errorf("nilSS.StdoutWriter() = %v, want os.Stdout", nilSS.StdoutWriter())
	}
	if nilSS.StderrWriter() != os.Stderr {
		t.Errorf("nilSS.StderrWriter() = %v, want os.Stderr", nilSS.StderrWriter())
	}
	if nilSS.TerminalWriter() != os.Stdout {
		t.Errorf("nilSS.TerminalWriter() = %v, want os.Stdout", nilSS.TerminalWriter())
	}
}

func TestTermio_RedirectStderr(t *testing.T) {
	var logBuf bytes.Buffer
	ss := RedirectStderr(&logBuf)

	log.Print("testing log redirection")
	if !bytes.Contains(logBuf.Bytes(), []byte("testing log redirection")) {
		t.Errorf("log output missing from redirected stderr, got %q", logBuf.String())
	}

	if err := ss.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

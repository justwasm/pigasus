// Package cli - termio.go implements a 1:1 reimplementation of
// google3/third_party/jetski/cli/termio from the v1.3.4 binary.
//
// jetski/cli/termio redirects terminal IO when the binary detects
// it's running non-interactively (e.g. via remote-control or in a
// CI job). The recovered implementation captures the original
// stdout/stderr/terminal so callers can decide later whether to
// restore them.
package cli

import (
	"fmt"
	"io"
	"log"
	"os"
)

// SavedStreams captures the original stdout/stderr/terminal-writer
// triple so a deferred restore is possible.
type SavedStreams struct {
	OriginalStdout   io.Writer
	OriginalStderr   io.Writer
	RedirectedStdout io.Writer
	RedirectedStderr io.Writer
	Terminal         io.Writer
	restoreStderrFn  func() error
	restoreStdoutFn  func() error
}

// Redirect swaps os.Stdout / os.Stderr for the provided writers and
// returns a SavedStreams for later restore. v1.3.4 binary calls this
// when the binary is invoked via the remote-control subcommand and
// the parent process wants structured JSON over the wire.
//
// We DO NOT swap os.Stdout directly in the recovered module because
// that would break callers' own file descriptor assumptions. Instead
// we return a SavedStreams so callers can use the new writers
// explicitly.
func Redirect(stdout, stderr io.Writer) *SavedStreams {
	return &SavedStreams{
		OriginalStdout:   os.Stdout,
		OriginalStderr:   os.Stderr,
		RedirectedStdout: stdout,
		RedirectedStderr: stderr,
		Terminal:         stdout,
		restoreStdoutFn:  func() error { return nil },
		restoreStderrFn:  func() error { return nil },
	}
}

// RedirectStderr is the simpler variant: only stderr is swapped.
// v1.3.4 binary uses this when --log-file is set so that the
// terminal stays clean and all logging goes to a file.
func RedirectStderr(stderr io.Writer) *SavedStreams {
	ss := Redirect(os.Stdout, stderr)
	log.SetOutput(stderr)
	ss.restoreStderrFn = func() error {
		log.SetOutput(os.Stderr)
		return nil
	}
	return ss
}

// Close restores the original streams. v1.3.4 binary calls this in
// a defer after Redirect.
func (s *SavedStreams) Close() error {
	if s == nil {
		return fmt.Errorf("termio: nil receiver")
	}
	if s.restoreStdoutFn != nil {
		if err := s.restoreStdoutFn(); err != nil {
			return err
		}
	}
	if s.restoreStderrFn != nil {
		if err := s.restoreStderrFn(); err != nil {
			return err
		}
	}
	return nil
}

// StdoutWriter returns the redirected stdout (or the original if not redirected).
func (s *SavedStreams) StdoutWriter() io.Writer {
	if s == nil || s.RedirectedStdout == nil {
		return os.Stdout
	}
	return s.RedirectedStdout
}

// StderrWriter returns the redirected stderr.
func (s *SavedStreams) StderrWriter() io.Writer {
	if s == nil || s.RedirectedStderr == nil {
		return os.Stderr
	}
	return s.RedirectedStderr
}

// TerminalWriter returns the terminal writer.
func (s *SavedStreams) TerminalWriter() io.Writer {
	if s == nil || s.Terminal == nil {
		return os.Stdout
	}
	return s.Terminal
}

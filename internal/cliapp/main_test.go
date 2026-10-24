package cliapp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points $ANTIGRAVITY_HOME at a scratch tree for the whole
// package's test run.
//
// Cycle 295: the cliapp fixtures wrote into the user's *real* agy
// home — ~/.gemini/antigravity-cli/history.jsonl (the prompt history
// the reference binary pages through) and settings.json. The
// reference reads the same two files, so a plain `go test` run
// injected junk into the user's history.
//
// 【一手】 measured on this machine, 2026-10-25: one
// `go test ./cmd/... ./internal/...` appended 15 fixture entries
// ("first prompt", "older prompt", "would be recalled if picker were
// hidden", "hi", "yo", "/changelog", …) to the real history.jsonl —
// 1723 of the 2000 entries in that file were test fixtures.
//
// The scratch tree mirrors the shape of the real one —
// <tmp>/.gemini/antigravity-cli is the AGY home, <tmp>/.gemini is
// its parent — so the code that walks the parent (the /skills panel
// reads <parent>/skills) still finds the same catalogue. The
// read-only parts of the real tree (the built-in skills bundle and
// ~/.gemini/skills) are linked in; everything the tests *write*
// lands in the scratch tree.
//
// Tests that need their own home still call
// t.Setenv("ANTIGRAVITY_HOME", …) and are unaffected: t.Setenv
// restores the value this function installed.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "pigasus-cliapp-home-")
	if err != nil {
		panic(err)
	}
	gemini := filepath.Join(root, ".gemini")
	agy := filepath.Join(gemini, "antigravity-cli")
	if err := os.MkdirAll(agy, 0o755); err != nil {
		panic(err)
	}
	if home, herr := os.UserHomeDir(); herr == nil {
		realGemini := filepath.Join(home, ".gemini")
		linkDir(filepath.Join(agy, "builtin"), filepath.Join(realGemini, "antigravity-cli", "builtin"))
		linkDir(filepath.Join(gemini, "skills"), filepath.Join(realGemini, "skills"))
	}
	if err := os.Setenv("ANTIGRAVITY_HOME", agy); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// linkDir symlinks dst → src when src exists. A missing source is not
// an error: the catalogue it feeds is simply smaller.
func linkDir(dst, src string) {
	if _, err := os.Stat(src); err != nil {
		return
	}
	_ = os.Symlink(src, dst)
}

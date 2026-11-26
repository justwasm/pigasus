package files

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLSLanguageNames_MatchesTheReference(t *testing.T) {
	cases := map[string]string{
		"a.go":         "LANGUAGE_GO",
		"a.py":         "LANGUAGE_PYTHON",
		"a.ts":         "LANGUAGE_TYPESCRIPT",
		"a.tsx":        "LANGUAGE_TSX",
		"a.js":         "LANGUAGE_JAVASCRIPT",
		"a.md":         "LANGUAGE_MARKDOWN",
		"a.json":       "LANGUAGE_JSON",
		"a.yaml":       "LANGUAGE_YAML",
		"a.yml":        "LANGUAGE_YAML",
		"a.txt":        "LANGUAGE_PLAINTEXT",
		"a.sh":         "LANGUAGE_SHELL",
		"a.bash":       "LANGUAGE_SHELL",
		"a.zsh":        "LANGUAGE_SHELL",
		"a.c":          "LANGUAGE_C",
		"a.h":          "LANGUAGE_C",
		"a.cpp":        "LANGUAGE_CPP",
		"a.cc":         "LANGUAGE_CPP",
		"a.hpp":        "LANGUAGE_CPP",
		"a.cs":         "LANGUAGE_CSHARP",
		"a.java":       "LANGUAGE_JAVA",
		"a.kt":         "LANGUAGE_KOTLIN",
		"a.rb":         "LANGUAGE_RUBY",
		"a.php":        "LANGUAGE_HACK",
		"a.lua":        "LANGUAGE_LUA",
		"a.dart":       "LANGUAGE_DART",
		"a.exs":        "LANGUAGE_ELIXIR",
		"a.erl":        "LANGUAGE_ERLANG",
		"a.hs":         "LANGUAGE_HASKELL",
		"a.scala":      "LANGUAGE_SCALA",
		"a.swift":      "LANGUAGE_SWIFT",
		"a.sql":        "LANGUAGE_SQL",
		"a.toml":       "LANGUAGE_TOML",
		"a.xml":        "LANGUAGE_XML",
		"a.html":       "LANGUAGE_HTML",
		"a.css":        "LANGUAGE_CSS",
		"a.scss":       "LANGUAGE_SCSS",
		"a.sass":       "LANGUAGE_SASS",
		"a.vue":        "LANGUAGE_VUE",
		"a.svelte":     "LANGUAGE_SVELTE",
		"a.proto":      "LANGUAGE_PROTOBUF",
		"a.ini":        "LANGUAGE_INI",
		"a.cfg":        "LANGUAGE_INI",
		"a.vim":        "LANGUAGE_VIMSCRIPT",
		"a.tf":         "LANGUAGE_HCL",
		"a.hcl":        "LANGUAGE_HCL",
		"a.cmake":      "LANGUAGE_CMAKE",
		"a.clj":        "LANGUAGE_CLOJURE",
		"a.el":         "LANGUAGE_EMACS_LISP",
		"a.dockerfile": "LANGUAGE_DOCKERFILE",
		"a.makefile":   "LANGUAGE_MAKEFILE",
		"a.mk":         "LANGUAGE_MAKEFILE",
		"Dockerfile":   "LANGUAGE_DOCKERFILE",
		"Makefile":     "LANGUAGE_MAKEFILE",
		"a.r":          "LANGUAGE_R",
		"a.rs":         "LANGUAGE_XML",
		"a.rsx":        "LANGUAGE_R",
		// The reference answers no language at all for these.
		"a":            "",
		"a.conf":       "",
		"a.ex":         "",
		"a.graphql":    "",
		"a.ml":         "",
		"a.pl":         "",
		"a.zig":        "",
		"a.unknownext": "",
	}
	for name, want := range cases {
		if got := lsLanguageName(name); got != want {
			t.Errorf("lsLanguageName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLSFileTypeName_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	fi, err := os.Stat(link)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := lsFileTypeName(fi); got != fileTypeFile {
		t.Errorf("fileType = %q, want %q", got, fileTypeFile)
	}
	if fi.Size() != 5 {
		t.Errorf("the size is the target's: %d", fi.Size())
	}
}

func TestLSLanguageName_CaseInsensitive(t *testing.T) {
	if got := lsLanguageName("a.PY"); got != "LANGUAGE_PYTHON" {
		t.Errorf("lsLanguageName(a.PY) = %q, want LANGUAGE_PYTHON", got)
	}
}

func TestLSDecodeBytes_RejectsRawText(t *testing.T) {
	if _, err := lsDecodeBytes("raw text"); err == nil {
		t.Errorf("raw text decoded as base64")
	}
	if _, err := lsDecodeBytes(base64.StdEncoding.EncodeToString([]byte("ok"))); err != nil {
		t.Errorf("base64 did not decode: %v", err)
	}
}

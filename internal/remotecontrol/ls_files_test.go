package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/lsproto"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// TestLSReadFile_AnswerDecodesAsProtoJSON is the regression test for
// the bug the Web UI reported:
//
//	Failed to read file content
//	Cannot decode field exa.language_server_pb.ReadFileResponse.content
//	from JSON: invalid base64 string
//
// `content` is a `bytes` field, so proto-JSON spells it as base64. The
// test does what the Web UI's protobuf-es decoder does: it parses the
// answer with protojson into the message the schema declares, and
// reads the bytes back.
func TestLSReadFile_AnswerDecodesAsProtoJSON(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	body := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{"uri": "file://" + filepath.ToSlash(path)})
	answer, err := d.DispatchDirect(context.Background(), "ReadFile", string(payload))
	if err != nil {
		t.Fatalf("DispatchDirect ReadFile: %v", err)
	}

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	md, ok := codec.OutputMessage("ReadFile")
	if !ok {
		t.Fatalf("the schema does not carry ReadFile")
	}
	msg := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(answer), msg); err != nil {
		t.Fatalf("the answer does not decode as protojson: %v\n%s", err, answer)
	}
	fd := md.Fields().ByName("content")
	if fd == nil {
		t.Fatalf("ReadFileResponse has no content field")
	}
	got := string(msg.Get(fd).Bytes())
	if got != body {
		t.Errorf("content = %q, want %q", got, body)
	}

	// The `language` field is exa.codeium_common_pb.Language, spelled
	// by its name.
	langFd := md.Fields().ByName("language")
	if langFd == nil {
		t.Fatalf("ReadFileResponse has no language field")
	}
	lang := msg.Get(langFd).Enum()
	if langfd := langFd.Enum().Values().ByName("LANGUAGE_GO"); langfd != nil {
		if lang != langfd.Number() {
			t.Errorf("language = %v, want LANGUAGE_GO", lang)
		}
	}
}

// TestLSReadFile_AnswerFieldOrder pins the field order of the answer:
// proto-JSON writes a message's fields in the order the descriptor
// declares them, and the reference's own answer is
// {"content":…,"language":…}.
func TestLSReadFile_AnswerFieldOrder(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"uri": "file://" + filepath.ToSlash(path)})
	answer, err := d.DispatchDirect(context.Background(), "ReadFile", string(payload))
	if err != nil {
		t.Fatalf("DispatchDirect ReadFile: %v", err)
	}
	contentAt := strings.Index(answer, `"content"`)
	languageAt := strings.Index(answer, `"language"`)
	if contentAt < 0 || languageAt < 0 {
		t.Fatalf("the answer carries neither field: %s", answer)
	}
	if contentAt > languageAt {
		t.Errorf("the answer is not in the descriptor's order: %s", answer)
	}
}

// TestLSWriteFile_RequestContentIsBase64 pins the request side of the
// same rule: WriteFileRequest.content is a `bytes` field, so the Web
// UI sends it base64-encoded and the file on disk carries the decoded
// bytes.
//
// The reference refuses anything else:
// "invalid value for bytes field content: \"raw text\"".
func TestLSWriteFile_RequestContentIsBase64(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "written.txt")
	body := []byte("hello 世界\n")
	payload, _ := json.Marshal(map[string]any{
		"uri":       "file://" + filepath.ToSlash(path),
		"content":   base64.StdEncoding.EncodeToString(body),
		"overwrite": true,
	})
	if _, err := d.DispatchDirect(context.Background(), "WriteFile", string(payload)); err != nil {
		t.Fatalf("DispatchDirect WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("the file carries %q, want %q", string(got), string(body))
	}

	// A content that is not base64 is refused, the way the reference
	// refuses it.
	bad, _ := json.Marshal(map[string]any{
		"uri":     "file://" + filepath.ToSlash(filepath.Join(dir, "bad.txt")),
		"content": "raw text",
	})
	if _, err := d.DispatchDirect(context.Background(), "WriteFile", string(bad)); err == nil {
		t.Errorf("a content that is not base64 was accepted")
	}
}

// TestLSStatUri_AnswerShape pins the shape of the StatUri answer:
// the enum by its name, the timestamp as a proto-JSON string, the
// int64 as a string, and the normalized uri.
func TestLSStatUri_AnswerShape(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"uri": "file://" + filepath.ToSlash(path)})
	answer, err := d.DispatchDirect(context.Background(), "StatUri", string(payload))
	if err != nil {
		t.Fatalf("DispatchDirect StatUri: %v", err)
	}

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	md, ok := codec.OutputMessage("StatUri")
	if !ok {
		t.Fatalf("the schema does not carry StatUri")
	}
	msg := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(answer), msg); err != nil {
		t.Fatalf("the answer does not decode as protojson: %v\n%s", err, answer)
	}
	fileType := msg.Get(md.Fields().ByName("file_type")).Enum()
	if want := md.Fields().ByName("file_type").Enum().Values().ByName("FILE_TYPE_FILE"); want != nil {
		if fileType != want.Number() {
			t.Errorf("fileType = %v, want FILE_TYPE_FILE", fileType)
		}
	}
	if got := msg.Get(md.Fields().ByName("size")).Int(); got != 13 {
		t.Errorf("size = %d, want 13", got)
	}
	if got := msg.Get(md.Fields().ByName("normalized_uri")).String(); got != "file://"+filepath.ToSlash(path) {
		t.Errorf("normalizedUri = %q", got)
	}
	if !msg.Has(md.Fields().ByName("mod_time")) {
		t.Errorf("the answer carries no modTime: %s", answer)
	}
}

// TestLSLanguageNames_MatchesTheReference pins the extension table to
// the reference's own answers.
//
// 【一手】 every row was measured against the reference's language
// server on 2026-10-26: a one-byte file per extension, read back with
// ReadFile. The reference's quirks are part of the measurement — .rs
// answers LANGUAGE_XML and .rsx answers LANGUAGE_R.
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

// TestLSReadDir_EmptyDirectory pins the proto-JSON rule that an empty
// repeated field is omitted: the reference answers an empty directory
// with {}.
func TestLSReadDir_EmptyDirectory(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	dir := t.TempDir()
	payload, _ := json.Marshal(map[string]any{"uri": "file://" + filepath.ToSlash(dir)})
	answer, err := d.DispatchDirect(context.Background(), "ReadDir", string(payload))
	if err != nil {
		t.Fatalf("DispatchDirect ReadDir: %v", err)
	}
	if answer != "{}" {
		t.Errorf("the answer of an empty directory is %s, want {}", answer)
	}
}

// TestLSFileTypeName_Symlink pins the reference's own reading of a
// symlink: it stats the target, so a link to a file answers
// FILE_TYPE_FILE with the target's size.
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

// TestLSLanguageName_CaseInsensitive pins the reference's own
// case-insensitive lookup: "a.PY" answers LANGUAGE_PYTHON.
func TestLSLanguageName_CaseInsensitive(t *testing.T) {
	if got := lsLanguageName("a.PY"); got != "LANGUAGE_PYTHON" {
		t.Errorf("lsLanguageName(a.PY) = %q, want LANGUAGE_PYTHON", got)
	}
}

// TestLSDecodeBytes_RejectsRawText pins the request side: raw text is
// not a legal proto-JSON bytes field.
func TestLSDecodeBytes_RejectsRawText(t *testing.T) {
	if _, err := lsDecodeBytes("raw text"); err == nil {
		t.Errorf("raw text decoded as base64")
	}
	if _, err := lsDecodeBytes(base64.StdEncoding.EncodeToString([]byte("ok"))); err != nil {
		t.Errorf("base64 did not decode: %v", err)
	}
}

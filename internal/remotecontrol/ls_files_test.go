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

// TestLSReadFile_MediaChunkedAnd200MBLimit pins the AGY 1.5.16 remote media
// display limit: media files (.mp3, .wav, .ogg, .m4a, .mp4, .webm) up to
// 200 MiB page in 20 MiB chunks via offset + truncated, while files over the
// limit (200 MiB for media, 20 MiB for non-media) return a display-limit error.
func TestLSReadFile_MediaChunkedAnd200MBLimit(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	f, err := os.Create(mediaPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	const mediaSize = (20 << 20) + 1024
	if err := f.Truncate(mediaSize); err != nil {
		f.Close()
		t.Fatalf("Truncate: %v", err)
	}
	f.Close()

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	md, ok := codec.OutputMessage("ReadFile")
	if !ok {
		t.Fatalf("schema missing ReadFile")
	}

	// Chunk 0: offset "0" (proto-JSON int64 string) -> 20 MiB, truncated=true.
	p0, _ := json.Marshal(map[string]any{
		"uri":    "file://" + filepath.ToSlash(mediaPath),
		"offset": "0",
	})
	ans0, err := d.DispatchDirect(context.Background(), "ReadFile", string(p0))
	if err != nil {
		t.Fatalf("DispatchDirect chunk 0: %v", err)
	}
	msg0 := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(ans0), msg0); err != nil {
		t.Fatalf("protojson chunk 0: %v", err)
	}
	if got := len(msg0.Get(md.Fields().ByName("content")).Bytes()); got != 20<<20 {
		t.Fatalf("chunk 0 len = %d, want %d", got, 20<<20)
	}
	if !msg0.Get(md.Fields().ByName("truncated")).Bool() {
		t.Fatalf("chunk 0 truncated = false, want true")
	}

	// Chunk 1: offset 20 MiB -> remaining 1024 bytes, truncated=false.
	p1, _ := json.Marshal(map[string]any{
		"uri":    "file://" + filepath.ToSlash(mediaPath),
		"offset": "20971520",
	})
	ans1, err := d.DispatchDirect(context.Background(), "ReadFile", string(p1))
	if err != nil {
		t.Fatalf("DispatchDirect chunk 1: %v", err)
	}
	msg1 := dynamicpb.NewMessage(md)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(ans1), msg1); err != nil {
		t.Fatalf("protojson chunk 1: %v", err)
	}
	if got := len(msg1.Get(md.Fields().ByName("content")).Bytes()); got != 1024 {
		t.Fatalf("chunk 1 len = %d, want 1024", got)
	}
	if msg1.Get(md.Fields().ByName("truncated")).Bool() {
		t.Fatalf("chunk 1 truncated = true, want false")
	}

	// Media file > 200 MiB is rejected with 200MB display limit.
	hugeMedia := filepath.Join(dir, "huge.webm")
	hf, err := os.Create(hugeMedia)
	if err != nil {
		t.Fatalf("Create hugeMedia: %v", err)
	}
	if err := hf.Truncate((200 << 20) + 1); err != nil {
		hf.Close()
		t.Fatalf("Truncate hugeMedia: %v", err)
	}
	hf.Close()
	pHuge, _ := json.Marshal(map[string]any{"uri": "file://" + filepath.ToSlash(hugeMedia)})
	if _, err := d.DispatchDirect(context.Background(), "ReadFile", string(pHuge)); err == nil || !strings.Contains(err.Error(), "200MB display limit") {
		t.Fatalf("huge media err = %v, want 200MB display limit", err)
	}

	// Non-media file > 20 MiB is rejected with 20MB display limit.
	hugeBin := filepath.Join(dir, "huge.bin")
	bf, err := os.Create(hugeBin)
	if err != nil {
		t.Fatalf("Create hugeBin: %v", err)
	}
	if err := bf.Truncate((20 << 20) + 1); err != nil {
		bf.Close()
		t.Fatalf("Truncate hugeBin: %v", err)
	}
	bf.Close()
	pBin, _ := json.Marshal(map[string]any{"uri": "file://" + filepath.ToSlash(hugeBin)})
	if _, err := d.DispatchDirect(context.Background(), "ReadFile", string(pBin)); err == nil || !strings.Contains(err.Error(), "20MB display limit") {
		t.Fatalf("huge bin err = %v, want 20MB display limit", err)
	}
}

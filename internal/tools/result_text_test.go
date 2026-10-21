package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Cycle 293: the reference prefixes every file-tool result with a
// Created At / Completed At header and returns a human summary rather than
// raw bytes. The bodies below are 【一手】 — captured from agy 1.4.4 by
// pointing CLOUD_CODE_URL at a mock that returns one functionCall, then
// reading the functionResponse the reference sent back.
var stampRE = regexp.MustCompile(`^Created At: \S+\nCompleted At: \S+\n`)

func stripStamp(t *testing.T, out any) string {
	t.Helper()
	s, ok := out.(string)
	if !ok {
		t.Fatalf("result type = %T, want string", out)
	}
	if !stampRE.MatchString(s) {
		t.Fatalf("result %q has no Created At / Completed At header", s)
	}
	return stampRE.ReplaceAllString(s, "")
}

func TestReferenceReadFileResultText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "three.txt"),
		[]byte("line one\nline two\nline three\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &ReadFile{Workspace: dir}

	out, err := r.Run(context.Background(), json.RawMessage(`{"Path":"three.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "Showing lines 1-3 of 3\n1: line one\n2: line two\n3: line three"
	if got := stripStamp(t, out); got != want {
		t.Errorf("read_file = %q, want %q", got, want)
	}

	out, err = r.Run(context.Background(), json.RawMessage(`{"Path":"three.txt","StartLine":2,"EndLine":2}`))
	if err != nil {
		t.Fatal(err)
	}
	want = "Showing lines 2-2 of 3\n2: line two"
	if got := stripStamp(t, out); got != want {
		t.Errorf("read_file range = %q, want %q", got, want)
	}

	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	out, err = r.Run(context.Background(), json.RawMessage(`{"Path":"empty.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := stripStamp(t, out); got != "File is empty" {
		t.Errorf("read_file empty = %q, want %q", got, "File is empty")
	}
}

func TestReferenceWriteFileResultText(t *testing.T) {
	dir := t.TempDir()
	w := &WriteFile{Workspace: dir}

	out, err := w.Run(context.Background(), json.RawMessage(`{"Path":"new.txt","Content":"alpha\nbeta\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := stripStamp(t, out); got != "Created file." {
		t.Errorf("write_file = %q, want %q", got, "Created file.")
	}

	// A second write to the same path is refused as a normal result, with
	// the resolved path in the message.
	out, err = w.Run(context.Background(), json.RawMessage(`{"Path":"new.txt","Content":"other\n"}`))
	if err != nil {
		t.Fatalf("refusal came back as an error: %v", err)
	}
	got := stripStamp(t, out)
	want := "Encountered error in tool execution: " + filepath.Join(dir, "new.txt") +
		" already exists. To overwrite it, retry with Overwrite=true. To edit it, view its contents first and use a code edit tool. Otherwise, choose a different file name"
	if got != want {
		t.Errorf("write_file refusal = %q, want %q", got, want)
	}

	// Overwrite=true replaces the contents.
	out, err = w.Run(context.Background(), json.RawMessage(`{"Path":"new.txt","Content":"other\n","Overwrite":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := stripStamp(t, out); got != "Created file." {
		t.Errorf("write_file overwrite = %q", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "other\n" {
		t.Errorf("overwrite content = %q", data)
	}

	// Append=true appends and still reports "Created file.".
	out, err = w.Run(context.Background(), json.RawMessage(`{"Path":"new.txt","Content":"tail\n","Append":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := stripStamp(t, out); got != "Created file." {
		t.Errorf("write_file append = %q", got)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "new.txt"))
	if string(data) != "other\ntail\n" {
		t.Errorf("append content = %q", data)
	}
}

func TestReferenceEditFileResultText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e := &EditFile{Workspace: dir}

	out, err := e.Run(context.Background(), json.RawMessage(`{"Path":"f.txt","Target":"beta","Replacement":"gamma"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "Made 1 replacement in " + path + " starting at line 2 (+1/-1 lines)."
	if got := stripStamp(t, out); got != want {
		t.Errorf("edit_file = %q, want %q", got, want)
	}

	// A multi-line replacement counts both halves' lines.
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err = e.Run(context.Background(), json.RawMessage(`{"Path":"f.txt","Target":"alpha\nbeta","Replacement":"ALPHA\nBETA\nGAMMA"}`))
	if err != nil {
		t.Fatal(err)
	}
	want = "Made 1 replacement in " + path + " starting at line 1 (+3/-2 lines)."
	if got := stripStamp(t, out); got != want {
		t.Errorf("edit_file multi-line = %q, want %q", got, want)
	}
}

// TestReadFileLineCap pins the declaration's "at most 800 lines" promise.
// 【一手】 agy 1.4.4: a 900-line file reads as "Showing lines 1-800 of 900";
// StartLine=100 reports 100-899; a StartLine past EOF clamps to the last line.
func TestReadFileLineCap(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 900; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	r := &ReadFile{Workspace: dir}

	out, err := r.Run(context.Background(), json.RawMessage(`{"Path":"big.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	body := stripStamp(t, out)
	lines := strings.Split(body, "\n")
	if lines[0] != "Showing lines 1-800 of 900" {
		t.Errorf("header = %q", lines[0])
	}
	if len(lines) != 801 {
		t.Errorf("body line count = %d, want 800 numbered lines + header", len(lines)-1)
	}
	if lines[len(lines)-1] != "800: line 800" {
		t.Errorf("last line = %q", lines[len(lines)-1])
	}

	out, err = r.Run(context.Background(), json.RawMessage(`{"Path":"big.txt","StartLine":100}`))
	if err != nil {
		t.Fatal(err)
	}
	lines = strings.Split(stripStamp(t, out), "\n")
	if lines[0] != "Showing lines 100-899 of 900" {
		t.Errorf("StartLine=100 header = %q", lines[0])
	}

	if err := os.WriteFile(filepath.Join(dir, "three.txt"),
		[]byte("line one\nline two\nline three\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err = r.Run(context.Background(), json.RawMessage(`{"Path":"three.txt","StartLine":10,"EndLine":20}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stripStamp(t, out), "Showing lines 3-3 of 3\n3: line three"; got != want {
		t.Errorf("past-EOF read = %q, want %q", got, want)
	}
}

// TestReadFileByteCap pins the 46080-byte cap. The cap counts the *numbered*
// text, so the reported total (100491) is larger than the file (100100). A
// continuation starts mid-line where the previous read stopped.
// 【一手】 agy 1.4.4.
func TestReadFileByteCap(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "A%04d%s\n", i, strings.Repeat("x", 995))
	}
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	r := &ReadFile{Workspace: dir}

	out, err := r.Run(context.Background(), json.RawMessage(`{"Path":"long.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	body := stripStamp(t, out)
	note := "Partial read (lines 1-100 of 100). Showing bytes 0-46080 of 100491 in this line range, truncated at the 46080-byte limit. To read the rest of this line range, call this tool again with the same StartLine and EndLine and ByteOffset=46080.\n"
	if !strings.HasPrefix(body, note) {
		t.Fatalf("partial note mismatch:\n got %q\nwant %q", body[:min(len(body), len(note))], note)
	}
	if got := len(body) - len(note); got != 46080 {
		t.Errorf("partial body = %d bytes, want 46080", got)
	}
	if !strings.HasPrefix(body[len(note):], "1: A0000") {
		t.Errorf("body does not start at line 1: %q", body[len(note):len(note)+20])
	}

	out, err = r.Run(context.Background(), json.RawMessage(`{"Path":"long.txt","StartLine":1,"EndLine":100,"ByteOffset":46080}`))
	if err != nil {
		t.Fatal(err)
	}
	body = stripStamp(t, out)
	note = "Partial read (lines 1-100 of 100). Showing bytes 46080-92160 of 100491 in this line range, truncated at the 46080-byte limit. To read the rest of this line range, call this tool again with the same StartLine and EndLine and ByteOffset=92160.\n"
	if !strings.HasPrefix(body, note) {
		t.Fatalf("continuation note mismatch: %q", body[:min(len(body), 200)])
	}
	rest := body[len(note):]
	// The resume lands 34 bytes into line 47's content, so the first fragment
	// is unnumbered x's and the next numbered line is 47.
	if !strings.HasPrefix(rest, strings.Repeat("x", 140)+"\n47: A0046") {
		t.Errorf("continuation head = %q", rest[:min(len(rest), 160)])
	}
}

// TestEditFileTargetNotFound pins the reference's normal-result text for a
// Target that does not occur — 【一手】 agy 1.4.4.
func TestEditFileTargetNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e := &EditFile{Workspace: dir}
	out, err := e.Run(context.Background(), json.RawMessage(`{"Path":"f.txt","Target":"ZZZ","Replacement":"x"}`))
	if err != nil {
		t.Fatalf("not-found came back as an error: %v", err)
	}
	want := "Encountered error in tool execution: target content not found in the file; the Target must exactly match existing file text, including whitespace"
	if got := stripStamp(t, out); got != want {
		t.Errorf("edit_file not-found = %q, want %q", got, want)
	}
	if data, _ := os.ReadFile(path); string(data) != "alpha\nbeta\n" {
		t.Errorf("file changed: %q", data)
	}
}

// TestReferenceViewFileResultText pins the custom-agent view_file format,
// which differs from read_file's. 【一手】 agy 1.4.4 (custom agent):
//
//	Content truncated: showing bytes 0-46080 of 100100. To see more, call
//	this tool again with the same line range and ContentOffset=46080.
//
// The cap is on the raw line range (100100 bytes for the whole file), and a
// continuation renumbers its slice from 1.
func TestReferenceViewFileResultText(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "A%04d%s\n", i, strings.Repeat("x", 995))
	}
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	v := &ViewFile{Workspace: dir}
	full := filepath.Join(dir, "long.txt")
	head := "File Path: `file://" + full + "`\nTotal Lines: 101\nTotal Bytes: 100100\nShowing lines 1 to 101\n"
	explain := "The following code has been modified to include a line number before every line, in the format: <line_number>: <original_line>. Please note that any changes targeting the original code should remove the line number, colon, and leading space.\n"

	out, err := v.Run(context.Background(), json.RawMessage(`{"AbsolutePath":"long.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	body := stripStamp(t, out)
	note := "Content truncated: showing bytes 0-46080 of 100100. To see more, call this tool again with the same line range and ContentOffset=46080.\n"
	want := head + note + explain
	if !strings.HasPrefix(body, want) {
		t.Fatalf("truncated head = %q", body[:min(len(body), len(want))])
	}
	tail := "The above content was truncated because individual lines are very long. Only a portion of the content is shown.\n"
	if !strings.HasSuffix(body, tail) {
		t.Errorf("truncated tail = %q", body[len(body)-len(tail):])
	}
	// The slice covers lines 1-46 plus 34 bytes of line 47.
	if !strings.Contains(body, "\n47: A0046x") {
		t.Errorf("truncation does not end in the middle of line 47")
	}

	out, err = v.Run(context.Background(), json.RawMessage(`{"AbsolutePath":"long.txt","StartLine":1,"EndLine":60,"ContentOffset":46080}`))
	if err != nil {
		t.Fatal(err)
	}
	body = stripStamp(t, out)
	rangeHead := "File Path: `file://" + full + "`\nTotal Lines: 101\nTotal Bytes: 100100\nShowing lines 1 to 60\n"
	if !strings.HasPrefix(body, rangeHead+explain+"1: ") {
		t.Fatalf("continuation head = %q", body[:min(len(body), len(rangeHead)+len(explain)+10)])
	}
	if strings.Contains(body, "Content truncated:") {
		t.Errorf("continuation should fit without a truncation note")
	}
	if !strings.HasSuffix(body, "call this tool again to view those lines.\n") {
		t.Errorf("continuation footer = %q", body[len(body)-80:])
	}

	// A 60-line range reports the range's own byte length, not the file's.
	out, err = v.Run(context.Background(), json.RawMessage(`{"AbsolutePath":"long.txt","StartLine":1,"EndLine":60}`))
	if err != nil {
		t.Fatal(err)
	}
	body = stripStamp(t, out)
	if !strings.HasPrefix(body, "File Path: `file://"+full+"`\nTotal Lines: 101\nTotal Bytes: 100100\nShowing lines 1 to 60\nContent truncated: showing bytes 0-46080 of 60059. To see more, call this tool again with the same line range and ContentOffset=46080.\n") {
		t.Errorf("range truncation head = %q", body[:min(len(body), 260)])
	}
}

// TestViewFileLineCap pins "at most 800 lines": a 900-line file shows
// 1 to 800 and closes with the "does NOT show" footer. 【一手】 1.4.4.
func TestViewFileLineCap(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 900; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	v := &ViewFile{Workspace: dir}
	out, err := v.Run(context.Background(), json.RawMessage(`{"AbsolutePath":"big.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	body := stripStamp(t, out)
	if !strings.Contains(body, "Total Lines: 901\nTotal Bytes: 7992\nShowing lines 1 to 800\n") {
		t.Errorf("header = %q", body[:min(len(body), 160)])
	}
	if !strings.Contains(body, "\n800: line 800\n") {
		t.Errorf("last line is not 800")
	}
}

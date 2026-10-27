package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// dispatch runs a catalog tool and fails the test on error.
func dispatch(t *testing.T, reg *Registry, name, args string) string {
	t.Helper()
	out, err := reg.Dispatch(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s(%s): %v", name, args, err)
	}
	return out
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestViewFile_LineRange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "l1\nl2\nl3\nl4\n")
	reg := Catalog(dir)
	full := filepath.Join(dir, "a.txt")
	head := "File Path: `file://" + full + "`\nTotal Lines: 5\nTotal Bytes: 12\n"
	explain := "The following code has been modified to include a line number before every line, in the format: <line_number>: <original_line>. Please note that any changes targeting the original code should remove the line number, colon, and leading space.\n"

	// Cycle 293: view_file's result (captured from a 1.4.4 custom agent) is
	// NOT read_file's. It reports File Path / Total Lines / Total Bytes and
	// "Showing lines A to B"; a trailing newline counts as an empty last line.
	if got := dispatch(t, reg, "view_file", `{"path":"a.txt"}`); !strings.Contains(got, head+"Showing lines 1 to 5\n"+explain+"1: l1\n2: l2\n3: l3\n4: l4\n5: \nThe above content shows the entire, complete file contents of the requested file.\n") {
		t.Errorf("whole file = %q", got)
	}
	if got := dispatch(t, reg, "view_file", `{"path":"a.txt","start_line":2,"end_line":3}`); !strings.Contains(got, head+"Showing lines 2 to 3\n"+explain+"2: l2\n3: l3\nThe above content does NOT show the entire file contents. If you need to view any lines of the file which were not shown to complete your task, call this tool again to view those lines.\n") {
		t.Errorf("lines 2-3 = %q", got)
	}
	if got := dispatch(t, reg, "view_file", `{"path":"a.txt","start_line":10}`); !strings.Contains(got, "Showing lines 5 to 5\n") || !strings.Contains(got, "\n5: \nThe above content does NOT show") {
		t.Errorf("past end = %q", got)
	}
}

// TestWorkspacePath_Absolute pins the cycle-275 rule. The reference REQUIRES
// absolute paths — view_file's declaration: "AbsolutePath … Must be an
// absolute path" — and a live 1.4.2 turn read an absolute path, the symlinked
// spelling of the same path, and /etc/hosts. So a read accepts any absolute
// path; a write accepts one inside the workspace and refuses one outside;
// a relative path still may not escape.
func TestWorkspacePath_Absolute(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hi")
	reg := Catalog(dir)

	// Read: absolute path inside the workspace (the reference's own shape).
	if got := dispatch(t, reg, "view_file",
		`{"AbsolutePath":`+strconv.Quote(filepath.Join(dir, "a.txt"))+`}`); !strings.Contains(got, "1: hi") {
		t.Errorf("absolute view_file = %q, want a 1: hi line", got)
	}
	// Read: absolute path outside the workspace — the reference read
	// /etc/hosts on a live turn, so reads are not workspace-bound.
	if _, err := reg.Dispatch(context.Background(), "view_file",
		json.RawMessage(`{"AbsolutePath":"/etc/hosts"}`)); err != nil {
		t.Errorf("absolute out-of-workspace view_file = %v, want it to read", err)
	}
	// Write: absolute path outside the workspace is still refused, with the
	// recovered message.
	if _, err := reg.Dispatch(context.Background(), "write_to_file",
		json.RawMessage(`{"path":"/etc/definitely-not-here","content":"x"}`)); err == nil ||
		!strings.Contains(err.Error(), "to not start with /") {
		t.Fatalf("out-of-workspace write error = %v, want the recovered message", err)
	}
	// Relative escape still refused.
	if _, err := reg.Dispatch(context.Background(), "view_file", json.RawMessage(`{"path":"../../etc/passwd"}`)); err == nil {
		t.Fatal("escaping path was accepted")
	}
}

// TestWorkspacePath_SymlinkedSpelling covers the alias case that produced the
// reported "(Failed)" reads: ~/GitHub is a symlink to ~/Documents/GitHub, so
// the same file has two absolute spellings and the reference accepts both.
func TestWorkspacePath_SymlinkedSpelling(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hi")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	reg := Catalog(dir)
	if got := dispatch(t, reg, "view_file",
		`{"AbsolutePath":`+strconv.Quote(filepath.Join(alias, "a.txt"))+`}`); !strings.Contains(got, "1: hi") {
		t.Errorf("symlinked absolute view_file = %q, want a 1: hi line", got)
	}
	// A write through the alias must land in the workspace, not in
	// <workspace>/<alias>.
	if _, err := reg.Dispatch(context.Background(), "write_to_file",
		json.RawMessage(`{"path":`+strconv.Quote(filepath.Join(alias, "b.txt"))+`,"content":"x"}`)); err != nil {
		t.Fatalf("symlinked absolute write_to_file = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Errorf("write through the symlinked spelling did not land in the workspace: %v", err)
	}
}

func TestGrepSearch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package a\nfunc Alpha() {}\n")
	writeFile(t, dir, "b.go", "package b\nfunc Beta() {}\n")
	writeFile(t, dir, "sub/c.go", "package c\nfunc AlphaToo() {}\n")
	reg := Catalog(dir)

	got := dispatch(t, reg, "grep_search", `{"pattern":"func Alpha"}`)
	if !strings.Contains(got, "a.go:2: func Alpha() {}") || !strings.Contains(got, "sub/c.go:2: func AlphaToo() {}") {
		t.Errorf("grep_search = %q", got)
	}
	// include filters on the file name
	got = dispatch(t, reg, "grep_search", `{"pattern":"package","include":"c.go"}`)
	if strings.Contains(got, "a.go") || !strings.Contains(got, "sub/c.go") {
		t.Errorf("grep_search include = %q", got)
	}
	// no match reports the reference-shaped empty message
	if got := dispatch(t, reg, "grep_search", `{"pattern":"zzz-nope"}`); !strings.Contains(got, "No files were searched") {
		t.Errorf("empty grep = %q", got)
	}
	if _, err := reg.Dispatch(context.Background(), "grep_search", json.RawMessage(`{"pattern":"("}`)); err == nil {
		t.Error("invalid regex accepted")
	}
}

func TestFindByName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "x")
	writeFile(t, dir, "sub/b.go", "x")
	writeFile(t, dir, "sub/c.txt", "x")
	reg := Catalog(dir)

	got := dispatch(t, reg, "find_by_name", `{"pattern":"*.go"}`)
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "sub/b.go") || strings.Contains(got, "c.txt") {
		t.Errorf("find_by_name = %q", got)
	}
	got = dispatch(t, reg, "find_by_name", `{"pattern":"**/*.txt"}`)
	if !strings.Contains(got, "sub/c.txt") {
		t.Errorf("find_by_name ** = %q", got)
	}
}

// TestFindByNameDoublestar pins doublestar-only semantics on
// find_by_name: a brace-expansion pattern `**/*.{go,md}` matches
// both extensions at any depth, and a character-class pattern
// `**/*.[ch]pp` matches `.cpp` and `.hpp` only. Both patterns fail
// with filepath.Match — it treats braces and `[…]` as literals.
func TestFindByNameDoublestar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "x")
	writeFile(t, dir, "sub/b.go", "x")
	writeFile(t, dir, "c.md", "x")
	writeFile(t, dir, "sub/d.md", "x")
	writeFile(t, dir, "e.txt", "x")
	writeFile(t, dir, "f.cpp", "x")
	writeFile(t, dir, "sub/g.hpp", "x")
	writeFile(t, dir, "sub/h.cxx", "x")
	reg := Catalog(dir)

	// brace expansion: any depth, either extension
	got := dispatch(t, reg, "find_by_name", `{"pattern":"**/*.{go,md}"}`)
	for _, want := range []string{"a.go", "sub/b.go", "c.md", "sub/d.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("**/*.{go,md} missed %q in %q", want, got)
		}
	}
	for _, bad := range []string{"e.txt"} {
		if strings.Contains(got, bad) {
			t.Errorf("**/*.{go,md} matched %q in %q", bad, got)
		}
	}

	// character class: only the named extensions
	got = dispatch(t, reg, "find_by_name", `{"pattern":"**/*.[ch]pp"}`)
	for _, want := range []string{"f.cpp", "sub/g.hpp"} {
		if !strings.Contains(got, want) {
			t.Errorf("**/*.[ch]pp missed %q in %q", want, got)
		}
	}
	for _, bad := range []string{"a.go", "sub/h.cxx", "e.txt"} {
		if strings.Contains(got, bad) {
			t.Errorf("**/*.[ch]pp matched %q in %q", bad, got)
		}
	}
}

// TestGrepSearchIncludeDoublestar pins the grep_search include flag
// running through doublestar at line 313. The pattern `**/*.go`
// matches `.go` files at any depth.
func TestGrepSearchIncludeDoublestar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package a\nTODO alpha\n")
	writeFile(t, dir, "sub/b.go", "package b\nTODO beta\n")
	writeFile(t, dir, "sub/c.txt", "TODO txt\n")
	reg := Catalog(dir)

	got := dispatch(t, reg, "grep_search", `{"pattern":"TODO","include":"**/*.go"}`)
	if !strings.Contains(got, "a.go") {
		t.Errorf("grep_search include **/*.go missed a.go: %q", got)
	}
	if !strings.Contains(got, "sub/b.go") {
		t.Errorf("grep_search include **/*.go missed sub/b.go: %q", got)
	}
	if strings.Contains(got, "sub/c.txt") {
		t.Errorf("grep_search include **/*.go leaked sub/c.txt: %q", got)
	}
}

func TestSedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "one\nfoo two\nfoo three\nfour\n")
	reg := Catalog(dir)

	// range sed (path required)
	dispatch(t, reg, "sed_file", `{"path":"a.txt","pattern":"foo","replacement":"bar","start_line":3,"end_line":3}`)
	got, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(got) != "one\nfoo two\nbar three\nfour\n" {
		t.Errorf("range sed = %q", got)
	}
	dispatch(t, reg, "sed_file", `{"path":"a.txt","pattern":"foo","replacement":"BAR"}`)
	got, _ = os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(got) != "one\nBAR two\nbar three\nfour\n" {
		t.Errorf("first-match sed = %q", got)
	}
	// capture groups and the global flag
	writeFile(t, dir, "b.txt", "k=v k=w\n")
	dispatch(t, reg, "sed_file", `{"path":"b.txt","pattern":"k=([a-z])","replacement":"key_$1","global":true}`)
	got, _ = os.ReadFile(filepath.Join(dir, "b.txt"))
	if string(got) != "key_v key_w\n" {
		t.Errorf("global sed = %q", got)
	}
	if _, err := reg.Dispatch(context.Background(), "sed_file", json.RawMessage(`{"path":"b.txt","pattern":"nope","replacement":"x"}`)); err == nil {
		t.Error("sed_file with no match should error")
	}
}

func TestMultiReplaceFileContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha beta gamma")
	reg := Catalog(dir)

	dispatch(t, reg, "multi_replace_file_content",
		`{"path":"a.txt","replacements":[{"oldString":"alpha","newString":"A"},{"oldString":"gamma","newString":"G"}]}`)
	got, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(got) != "A beta G" {
		t.Errorf("multi replace = %q", got)
	}
	// a missing oldString aborts before any write
	_, err := reg.Dispatch(context.Background(), "multi_replace_file_content",
		json.RawMessage(`{"path":"a.txt","replacements":[{"oldString":"A","newString":"Z"},{"oldString":"absent","newString":"Q"}]}`))
	if err == nil {
		t.Fatal("missing oldString should error")
	}
	got, _ = os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(got) != "A beta G" {
		t.Errorf("file changed despite the failure: %q", got)
	}
}

func TestViewContentChunk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "0123456789")
	reg := Catalog(dir)
	// view_content_chunk has a working executor but is NOT part of the
	// reference's advertised surface (see testdata/advertised_tools_1.3.6.txt),
	// so register it explicitly to keep covering the implementation.
	reg.Register(&ViewContentChunk{Workspace: dir})

	got := dispatch(t, reg, "view_content_chunk", `{"path":"a.txt","offset":2,"length":3}`)
	if !strings.Contains(got, "[bytes 2-5 of 10]") || !strings.Contains(got, "234") {
		t.Errorf("chunk = %q", got)
	}
}

func TestCodebaseSearch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package a\n// needle here\n")
	writeFile(t, dir, "b.go", "package b\n")
	reg := Catalog(dir)
	// Unadvertised executor, same as view_content_chunk above.
	reg.Register(&CodebaseSearch{Workspace: dir})

	got := dispatch(t, reg, "codebase_search", `{"query":"needle"}`)
	if !strings.Contains(got, "a.go") || strings.Contains(got, "b.go") {
		t.Errorf("codebase_search = %q", got)
	}
	if got := dispatch(t, reg, "codebase_search", `{"query":"wireless-unicorn"}`); !strings.Contains(got, "No matches found") {
		t.Errorf("empty codebase_search = %q", got)
	}
}

func TestNotebookEdit(t *testing.T) {
	dir := t.TempDir()
	nb := `{"cells":[{"cell_type":"code","source":["old\n"],"metadata":{},"outputs":[]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`
	writeFile(t, dir, "n.ipynb", nb)
	reg := Catalog(dir)

	dispatch(t, reg, "notebook_edit", `{"path":"n.ipynb","cell_index":0,"source":"print(1)\nprint(2)"}`)
	raw, _ := os.ReadFile(filepath.Join(dir, "n.ipynb"))
	var decoded struct {
		Cells []struct {
			CellType string   `json:"cell_type"`
			Source   []string `json:"source"`
		} `json:"cells"`
		Nbformat int `json:"nbformat"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("notebook no longer parses: %v", err)
	}
	if len(decoded.Cells) != 1 || strings.Join(decoded.Cells[0].Source, "") != "print(1)\nprint(2)" {
		t.Errorf("cell source = %+v", decoded.Cells)
	}
	if decoded.Nbformat != 4 {
		t.Errorf("nbformat lost: %d", decoded.Nbformat)
	}
	if _, err := reg.Dispatch(context.Background(), "notebook_edit",
		json.RawMessage(`{"path":"n.ipynb","cell_index":9,"source":"x"}`)); err == nil {
		t.Error("out-of-range cell_index should error")
	}
}

func TestWaitAndFinish(t *testing.T) {
	reg := Catalog(t.TempDir())
	start := time.Now()
	if got := dispatch(t, reg, "wait", `{"seconds":0.05}`); !strings.Contains(got, "Waited") {
		t.Errorf("wait = %q", got)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("wait returned after %v, want at least 50ms", elapsed)
	}
	if _, err := reg.Dispatch(context.Background(), "wait", json.RawMessage(`{"seconds":9999}`)); err == nil {
		t.Error("wait should cap the duration")
	}
	if got := dispatch(t, reg, "finish", `{"summary":"done"}`); !strings.Contains(got, "done") {
		t.Errorf("finish = %q", got)
	}
}

// TestHeadlessInteractionTools pins that the tools needing a user report they
// cannot reach one instead of inventing an answer.
func TestHeadlessInteractionTools(t *testing.T) {
	// schedule writes its task log under the antigravity home; keep the real
	// ~/.gemini/antigravity-cli out of a unit test.
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	reg := Catalog(t.TempDir())
	// notify_user has a working executor but is not advertised by the
	// reference, so register it explicitly.
	reg.Register(&NotifyUserTool{})
	if got := dispatch(t, reg, "ask_question", `{"questions":[{"question":"which?"}]}`); !strings.Contains(got, "cannot be asked") {
		t.Errorf("ask_question = %q", got)
	}
	got := dispatch(t, reg, "ask_permission", `{"action":"run_command","target":"date"}`)
	if !strings.Contains(got, "Permission prompt for action 'run_command' on target 'date' was denied.") {
		t.Errorf("ask_permission = %q", got)
	}
	if got := dispatch(t, reg, "ask_custom_permission", `{"action":"a","target":"t"}`); !strings.Contains(got, "was denied.") {
		t.Errorf("ask_custom_permission = %q", got)
	}
	if got := dispatch(t, reg, "notify_user", `{"message":"hi"}`); !strings.Contains(got, "recorded") {
		t.Errorf("notify_user = %q", got)
	}
	if got := dispatch(t, reg, "manage_inbox", `{"action":"list"}`); !strings.Contains(got, "hi") {
		t.Errorf("manage_inbox list = %q", got)
	}
	if got := dispatch(t, reg, "manage_inbox", `{"action":"clear"}`); !strings.Contains(got, "1") {
		t.Errorf("manage_inbox clear = %q", got)
	}
	// schedule acknowledges a background task with the reference's text
	if got := dispatch(t, reg, "schedule", `{"command":"date","delay_seconds":60}`); !strings.Contains(got, "Tool is running as a background task with task id:") {
		t.Errorf("schedule = %q", got)
	}
	if _, err := reg.Dispatch(context.Background(), "schedule", json.RawMessage(`{"command":"date"}`)); err == nil {
		t.Error("schedule without delay or cron should error")
	}
	if got := dispatch(t, reg, "list_permissions", `{}`); !strings.Contains(got, "No permission rules") {
		t.Errorf("list_permissions = %q", got)
	}
}

// taskIDFrom extracts the task id from a background launch
// acknowledgement: "Tool is running as a background task with task id:
// <id>".
func taskIDFrom(t *testing.T, out string) string {
	t.Helper()
	const marker = "task id: "
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("no task id in %q", out)
	}
	rest := out[i+len(marker):]
	if j := strings.IndexAny(rest, " \n"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// TestBackgroundCommands exercises run_command(background) end to end through
// command_status, send_command_input and manage_task.
func TestBackgroundCommands(t *testing.T) {
	reg := Catalog(t.TempDir())

	// `head -1` reads one line and exits on its own, so the task reaches
	// "finished" without needing an explicit EOF on stdin.
	out := dispatch(t, reg, "run_command", `{"command":"head -1","background":true}`)
	if !strings.Contains(out, "Tool is running as a background task with task id: ") {
		t.Fatalf("background start = %q", out)
	}
	// The id is process-global, so it cannot be hardcoded: under
	// -count>1 the registry still holds the tasks of the earlier runs
	// and the counter has moved on. Extract the id the tool reported.
	id := taskIDFrom(t, out)
	if id == "" {
		t.Fatalf("no command id in %q", out)
	}
	if got := dispatch(t, reg, "send_command_input", `{"id":"`+id+`","input":"hello"}`); !strings.Contains(got, "running") && !strings.Contains(got, "finished") {
		t.Errorf("send_command_input = %q", got)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := dispatch(t, reg, "command_status", `{"id":"`+id+`"}`)
		if strings.Contains(st, "finished") && strings.Contains(st, "hello") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("command_status never saw the echo output: %q", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := dispatch(t, reg, "manage_task", `{"action":"list"}`); !strings.Contains(got, id) {
		t.Errorf("manage_task list = %q", got)
	}
	if _, err := reg.Dispatch(context.Background(), "command_status", json.RawMessage(`{"id":"cmd-nope"}`)); err == nil {
		t.Error("unknown command id should error")
	}
	if _, err := reg.Dispatch(context.Background(), "manage_task", json.RawMessage(`{"action":"explode"}`)); err == nil {
		t.Error("unknown manage_task action should error")
	}
}

func TestBackgroundCommandKill(t *testing.T) {
	reg := Catalog(t.TempDir())
	out := dispatch(t, reg, "run_command", `{"command":"sleep 30","background":true}`)
	id := taskIDFrom(t, out)
	if id == "" {
		t.Fatalf("no command id in %q", out)
	}
	got := dispatch(t, reg, "manage_task", `{"action":"kill","id":"`+id+`"}`)
	if !strings.Contains(got, "Killed") {
		t.Errorf("kill = %q", got)
	}
}

// TestNetworkToolsStillNotImplemented keeps the boundary explicit. search_web,
// read_url_content and generate_image are backed now (they return the
// reference's captured texts); MCP/resource tools are still unbacked.
func TestNetworkToolsStillNotImplemented(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, name := range []string{"call_mcp", "list_resources", "read_resource"} {
		if _, err := reg.Dispatch(context.Background(), name, json.RawMessage(`{}`)); err == nil {
			t.Errorf("%s should still report ErrNotImplemented", name)
		}
	}
}

// TestCatalogEveryBackedToolHasASchema guards against a spec pointing at a
// tool whose schema is empty.
func TestCatalogEveryBackedToolHasASchema(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, n := range reg.List() {
		tool, _ := reg.Get(n)
		s := string(tool.Schema())
		if !strings.Contains(s, `"type":"object"`) {
			t.Errorf("tool %s advertises a non-object schema: %s", n, s)
		}
	}
}

// Cycle 133: case-insensitive JSON key unmarshaling for tool
// arguments. The recovered v1.3.6 wire format uses capitalised
// keys (AbsolutePath, TargetFile, CommandLine, Query, OldString,
// NewString, Content, etc.); the cycle-72 catalog decodes lowercase
// keys. The canonicalisation pre-pass in argsOf() closes the gap.
func TestArgsOf_CaseInsensitiveKeys(t *testing.T) {
	type p struct {
		Path    string `json:"path"`
		Command string `json:"command"`
		Query   string `json:"query"`
	}
	// All capitalised keys — the recovered wire format.
	raw := []byte(`{"AbsolutePath":"foo.txt","CommandLine":"ls","Query":"bar"}`)
	var got p
	if err := argsOf(raw, &got); err != nil {
		t.Fatalf("argsOf: %v", err)
	}
	if got.Path != "foo.txt" || got.Command != "ls" || got.Query != "bar" {
		t.Errorf("capitalised keys did not decode: %+v", got)
	}
	// Lowercase still works (cycle-72 path).
	raw3 := []byte(`{"path":"foo.txt","command":"ls","query":"bar"}`)
	var got3 p
	if err := argsOf(raw3, &got3); err != nil {
		t.Fatalf("argsOf: %v", err)
	}
	if got3.Path != "foo.txt" || got3.Command != "ls" || got3.Query != "bar" {
		t.Errorf("lowercase keys did not decode: %+v", got3)
	}
	// Nested objects are recursively canonicalised.
	type inner struct {
		OldStr string `json:"oldString"`
		NewStr string `json:"newString"`
	}
	type outer struct {
		Path  string `json:"path"`
		Inner inner  `json:"edit"`
	}
	raw4 := []byte(`{"AbsolutePath":"foo.txt","edit":{"OldString":"a","NewString":"b"}}`)
	var got4 outer
	if err := argsOf(raw4, &got4); err != nil {
		t.Fatalf("argsOf nested: %v", err)
	}
	if got4.Path != "foo.txt" || got4.Inner.OldStr != "a" || got4.Inner.NewStr != "b" {
		t.Errorf("nested capitalised keys did not decode: %+v", got4)
	}
	// Empty payload still produces a default-initialised value.
	var got5 p
	if err := argsOf(nil, &got5); err != nil {
		t.Fatalf("argsOf nil: %v", err)
	}
	if got5.Path != "" {
		t.Errorf("nil payload should leave fields zero, got %+v", got5)
	}
}

// Cycle 133: GrepSearchTool accepts capitalised Pattern/Path
// and matches against a file (the recovered wire format case).
func TestGrepSearchTool_CapitalisedArgs(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "secret.txt"), []byte("hello world\nhello again\n"), 0644); err != nil {
		t.Fatal(err)
	}
	g := &GrepSearchTool{Workspace: tmp}
	// Capitalised "Pattern" + "Path" — would have failed before cycle 133.
	raw := []byte(`{"Pattern":"hello","Path":"secret.txt"}`)
	out, err := g.Run(context.Background(), raw)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("expected string output, got %T", out)
	}
	if !strings.Contains(s, "hello") {
		t.Errorf("expected matches in output, got %q", s)
	}
}

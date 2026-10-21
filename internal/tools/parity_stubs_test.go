package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stripCreatedAt removes the single Created At line a backgrounded result
// starts with (schedule has no Completed At). The shared stripStamp helper
// requires both lines and lives in result_text_test.go.
func stripCreatedAt(t *testing.T, text string) string {
	t.Helper()
	lines := strings.SplitN(text, "\n", 2)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "Created At: ") {
		t.Fatalf("result does not start with a Created At line: %q", text)
	}
	return lines[1]
}

func TestScheduleMatchesReferenceResult(t *testing.T) {
	home := t.TempDir()
	tool := &ScheduleTool{ConversationID: "conv-test", Home: home}
	start := time.Now()
	out, err := tool.Run(context.Background(), json.RawMessage(`{"Prompt":"ping","DurationSeconds":3600}`))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	text, ok := out.(string)
	if !ok {
		t.Fatalf("schedule returned %T, want string", out)
	}
	if strings.Contains(text, "Completed At:") {
		t.Errorf("schedule must not report Completed At (backgrounded): %q", text)
	}
	body := stripCreatedAt(t, text)
	wantPrefix := "Tool is running as a background task with task id: conv-test/task-"
	if !strings.HasPrefix(body, wantPrefix) {
		t.Fatalf("body = %q, want prefix %q", body, wantPrefix)
	}
	if !strings.Contains(body, "Task logs are available at: file://"+filepath.Join(home, "brain", "conv-test", ".system_generated", "tasks")+"/task-") {
		t.Errorf("log path missing or wrong in %q", body)
	}
	if !strings.HasSuffix(body, "\nYOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.\n DO NOTHING ELSE.") {
		t.Errorf("tail text mismatch in %q", body)
	}

	// The acknowledged log must exist and name the prompt.
	logPath := filepath.Join(home, "brain", "conv-test", ".system_generated", "tasks", "task-1.log")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("task log: %v", err)
	}
	if !strings.Contains(string(raw), "prompt: ping") {
		t.Errorf("task log = %q", raw)
	}
	_ = start
}

func TestSearchWebMatchesReferenceResult(t *testing.T) {
	out, err := (&SearchWeb{}).Run(context.Background(), json.RawMessage(`{"query":"golang","domain":"go.dev"}`))
	if err != nil {
		t.Fatalf("search_web: %v", err)
	}
	if got := stripStamp(t, out); got != "Encountered error in tool execution: no candidates returned from GenerateContent" {
		t.Fatalf("search_web body = %q", got)
	}
}

func TestGenerateImageMatchesReferenceResult(t *testing.T) {
	out, err := (&GenerateImage{}).Run(context.Background(), json.RawMessage(`{"Prompt":"a red cube","ImageName":"cube"}`))
	if err != nil {
		t.Fatalf("generate_image: %v", err)
	}
	if got := stripStamp(t, out); got != "Encountered error in tool execution: no response in generate content result" {
		t.Fatalf("generate_image body = %q", got)
	}
}

func TestReadURLContentSavesArticleAndMatchesShape(t *testing.T) {
	page := `<html><head><title>Live Content</title>
<meta property="og:description" content="Fetched live"></head>
<body><script>ignore()</script><h1>Hello</h1><p>First para.</p><p>Second &amp; last.</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	home := t.TempDir()
	tool := &ReadURLContent{ConversationID: "conv-url", Home: home}
	out, err := tool.Run(context.Background(), json.RawMessage(`{"Url":"`+srv.URL+`/"}`))
	if err != nil {
		t.Fatalf("read_url_content: %v", err)
	}
	body := stripStamp(t, out)
	saved := filepath.Join(home, "brain", "conv-url", ".system_generated", "steps", "1", "content.md")
	want := "Title: Live Content\n\n" +
		"OG Description: Fetched live\n\n" +
		"The full content of the article at " + srv.URL + "/ has been saved to: " + saved + "\n\n" +
		"You can use the view_file tool to read specific sections if needed."
	if body != want {
		t.Fatalf("body =\n%q\nwant\n%q", body, want)
	}
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("saved article: %v", err)
	}
	if strings.Contains(string(raw), "ignore()") {
		t.Errorf("script content survived: %q", raw)
	}
	if !strings.Contains(string(raw), "First para.") || !strings.Contains(string(raw), "Second & last.") {
		t.Errorf("article body = %q", raw)
	}
}

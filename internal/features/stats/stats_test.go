package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLookupPricingAndEstimateCost(t *testing.T) {
	// Gemini flash
	cost := EstimateCost("gemini-2.0-flash", 1_000_000, 1_000_000, 1_000_000)
	expected := 0.075 + 0.30 + 0.01875
	if diff := cost - expected; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("gemini flash cost = %v, want %v", cost, expected)
	}

	// Claude sonnet
	costSonnet := EstimateCost("claude-3-5-sonnet-20241022", 1_000_000, 1_000_000, 0)
	if costSonnet != 18.0 {
		t.Errorf("claude sonnet cost = %v, want 18.0", costSonnet)
	}
}

func TestResolveProvider(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"gemini-2.0-flash", "Google"},
		{"claude-3-5-sonnet-20241022", "Anthropic"},
		{"gpt-4o", "OpenAI"},
		{"deepseek-chat", "DeepSeek"},
		{"qwen-max", "Qwen"},
		{"mistral-large", "Mistral"},
	}

	for _, tt := range tests {
		got := ResolveProvider(tt.model)
		if got != tt.expected {
			t.Errorf("ResolveProvider(%q) = %q, want %q", tt.model, got, tt.expected)
		}
	}
}

func TestCollect_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	stats, projects, err := Collect(CollectOptions{
		DataDir: tmpDir,
		All:     true,
	})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if stats.Total.TotalSessions != 0 {
		t.Errorf("TotalSessions = %d, want 0", stats.Total.TotalSessions)
	}
	if len(projects) != 0 {
		t.Errorf("len(projects) = %d, want 0", len(projects))
	}
}

func TestCollect_SyntheticSessions(t *testing.T) {
	tmpDir := t.TempDir()
	brainDir := filepath.Join(tmpDir, "brain")

	convID := "test-conv-001"
	logDir := filepath.Join(brainDir, convID, ".system_generated", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("failed to create logDir: %v", err)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	lines := []string{
		`{"step_index":0,"source":"USER","type":"USER_MESSAGE","created_at":"` + nowStr + `","content":"Hello Pigasus"}`,
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","created_at":"` + nowStr + `","model":"gemini-2.0-flash","input_tokens":1000,"output_tokens":200,"cache_read_tokens":50,"tool_calls":[{"name":"run_command"}]}`,
		`{"step_index":2,"source":"MODEL","type":"MODEL_RESPONSE","created_at":"` + nowStr + `","model":"gemini-2.0-flash","input_tokens":1500,"output_tokens":300,"cache_read_tokens":0,"tool_calls":[{"name":"read_file"}]}`,
	}

	transcriptPath := filepath.Join(logDir, "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	stats, _, err := Collect(CollectOptions{
		DataDir: tmpDir,
		All:     true,
	})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if stats.Total.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", stats.Total.TotalSessions)
	}
	if stats.Total.TotalMessages != 3 {
		t.Errorf("TotalMessages = %d, want 3", stats.Total.TotalMessages)
	}
	if stats.Total.TotalPromptTokens != 2500 {
		t.Errorf("TotalPromptTokens = %d, want 2500", stats.Total.TotalPromptTokens)
	}
	if stats.Total.TotalCompletionTokens != 500 {
		t.Errorf("TotalCompletionTokens = %d, want 500", stats.Total.TotalCompletionTokens)
	}
	if stats.Total.TotalCacheReadTokens != 50 {
		t.Errorf("TotalCacheReadTokens = %d, want 50", stats.Total.TotalCacheReadTokens)
	}
	if stats.Total.TotalTokens != 3050 {
		t.Errorf("TotalTokens = %d, want 3050", stats.Total.TotalTokens)
	}

	// Verify tool usage
	if len(stats.ToolUsage) != 2 {
		t.Fatalf("len(ToolUsage) = %d, want 2", len(stats.ToolUsage))
	}
	toolCounts := make(map[string]int64)
	for _, tu := range stats.ToolUsage {
		toolCounts[tu.ToolName] = tu.CallCount
	}
	if toolCounts["run_command"] != 1 || toolCounts["read_file"] != 1 {
		t.Errorf("unexpected tool counts: %v", toolCounts)
	}

	// Verify model usage
	if len(stats.UsageByModel) != 1 {
		t.Fatalf("len(UsageByModel) = %d, want 1", len(stats.UsageByModel))
	}
	if stats.UsageByModel[0].Model != "gemini-2.0-flash" {
		t.Errorf("model = %s, want gemini-2.0-flash", stats.UsageByModel[0].Model)
	}
	if stats.UsageByModel[0].Provider != "Google" {
		t.Errorf("provider = %s, want Google", stats.UsageByModel[0].Provider)
	}

	// Verify recent sessions
	if len(stats.RecentSessions) != 1 {
		t.Fatalf("len(RecentSessions) = %d, want 1", len(stats.RecentSessions))
	}
	if stats.RecentSessions[0].Title != "Hello Pigasus" {
		t.Errorf("recent session title = %s, want 'Hello Pigasus'", stats.RecentSessions[0].Title)
	}
}

func TestRenderHTMLAndWriteTemp(t *testing.T) {
	st := &Stats{
		GeneratedAt: time.Now().UTC(),
		Total: TotalStats{
			TotalSessions:         5,
			TotalMessages:         42,
			TotalPromptTokens:     10000,
			TotalCompletionTokens: 2000,
			TotalTokens:           12000,
			TotalCost:             0.05,
		},
	}

	htmlBytes, err := RenderHTML(st, nil, "test-project", "tester")
	if err != nil {
		t.Fatalf("RenderHTML returned error: %v", err)
	}

	htmlStr := string(htmlBytes)
	if !strings.Contains(htmlStr, "<!doctype html>") {
		t.Errorf("rendered html missing doctype")
	}
	if !strings.Contains(htmlStr, "Pigasus Usage Statistics") {
		t.Errorf("rendered html missing Pigasus title")
	}
	if !strings.Contains(htmlStr, "test-project") {
		t.Errorf("rendered html missing project name")
	}

	tmpPath, err := WriteTempHTML(st, nil, "test-project", "tester")
	if err != nil {
		t.Fatalf("WriteTempHTML returned error: %v", err)
	}
	defer os.Remove(tmpPath)

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}
	if len(data) != len(htmlBytes) {
		t.Errorf("temp file length mismatch: %d vs %d", len(data), len(htmlBytes))
	}
}

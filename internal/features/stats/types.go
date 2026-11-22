package stats

import "time"

// Day names for day of week statistics.
var dayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// Stats holds all aggregated usage statistics.
type Stats struct {
	GeneratedAt       time.Time          `json:"generated_at"`
	Total             TotalStats         `json:"total"`
	UsageByDay        []DailyUsage       `json:"usage_by_day"`
	UsageByModel      []ModelUsage       `json:"usage_by_model"`
	UsageByHour       []HourlyUsage      `json:"usage_by_hour"`
	UsageByDayOfWeek  []DayOfWeekUsage   `json:"usage_by_day_of_week"`
	RecentActivity    []DailyActivity    `json:"recent_activity"`
	AvgResponseTimeMs float64            `json:"avg_response_time_ms"`
	ToolUsage         []ToolUsage        `json:"tool_usage"`
	HourDayHeatmap    []HourDayHeatmapPt `json:"hour_day_heatmap"`
	RecentSessions    []SessionItem      `json:"recent_sessions,omitempty"`
}

// TotalStats holds top-level summary metrics.
type TotalStats struct {
	TotalSessions         int64   `json:"total_sessions"`
	TotalPromptTokens     int64   `json:"total_prompt_tokens"`
	TotalCompletionTokens int64   `json:"total_completion_tokens"`
	TotalCacheReadTokens  int64   `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int64   `json:"total_cache_write_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	TotalCost             float64 `json:"total_cost"`
	TotalMessages         int64   `json:"total_messages"`
	AvgTokensPerSession   float64 `json:"avg_tokens_per_session"`
	AvgMessagesPerSession float64 `json:"avg_messages_per_session"`
}

// DailyUsage aggregates usage by date (YYYY-MM-DD).
type DailyUsage struct {
	Day              string  `json:"day"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	SessionCount     int64   `json:"session_count"`
}

// ModelUsage aggregates usage by model and provider.
type ModelUsage struct {
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	MessageCount     int64   `json:"message_count"`
	TotalTokens      int64   `json:"total_tokens"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// HourlyUsage aggregates sessions by hour of day (0-23 UTC).
type HourlyUsage struct {
	Hour         int   `json:"hour"`
	SessionCount int64 `json:"session_count"`
}

// DayOfWeekUsage aggregates usage by day of week (0=Sunday ... 6=Saturday).
type DayOfWeekUsage struct {
	DayOfWeek        int    `json:"day_of_week"`
	DayName          string `json:"day_name"`
	SessionCount     int64  `json:"session_count"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

// DailyActivity holds daily volume for the activity bar chart.
type DailyActivity struct {
	Day          string  `json:"day"`
	SessionCount int64   `json:"session_count"`
	TotalTokens  int64   `json:"total_tokens"`
	Cost         float64 `json:"cost"`
}

// ToolUsage aggregates calls by tool name.
type ToolUsage struct {
	ToolName  string `json:"tool_name"`
	CallCount int64  `json:"call_count"`
}

// HourDayHeatmapPt represents a data point in the 7x24 heatmap.
type HourDayHeatmapPt struct {
	DayOfWeek    int   `json:"day_of_week"`
	Hour         int   `json:"hour"`
	SessionCount int64 `json:"session_count"`
}

// SessionItem represents a single recent conversation session.
type SessionItem struct {
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title"`
	Project      string    `json:"project"`
	MessageCount int64     `json:"message_count"`
	TotalTokens  int64     `json:"total_tokens"`
	Cost         float64   `json:"cost"`
	CreatedAt    time.Time `json:"created_at"`
}

// ProjectStats associates stats with a specific project path.
type ProjectStats struct {
	ProjectPath string `json:"project_path"`
	Stats       *Stats `json:"stats"`
}

// CollectOptions specifies criteria for gathering statistics.
type CollectOptions struct {
	DataDir  string // Root store directory (defaults to confhome.Antigravity())
	CrawlDir string // Optional directory to crawl recursively
	Cwd      string // Filter sessions to this project/working directory
	All      bool   // Include all projects/sessions regardless of Cwd
}

package stats

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/core/confhome"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// Collect gathers and aggregates usage statistics according to the given options.
func Collect(opts CollectOptions) (*Stats, []ProjectStats, error) {
	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = confhome.Antigravity()
	}

	rawSessions, err := discoverAndParseSessions(dataDir, opts)
	if err != nil {
		return nil, nil, err
	}

	if len(rawSessions) == 0 {
		return &Stats{
			GeneratedAt: time.Now().UTC(),
		}, nil, nil
	}

	// Filter by cwd if requested and not --all
	filtered := filterSessionsByCwd(rawSessions, opts.Cwd, opts.All)
	if len(filtered) == 0 && !opts.All && opts.Cwd != "" {
		// If strict cwd filter returned nothing, check if user might want all
		// but per spec, if filtered is empty, return empty stats
		return &Stats{
			GeneratedAt: time.Now().UTC(),
		}, nil, nil
	}

	// Group by project if multiple projects exist
	projectMap := make(map[string][]*parsedSession)
	for _, s := range filtered {
		proj := s.project
		if proj == "" {
			proj = "default"
		}
		projectMap[proj] = append(projectMap[proj], s)
	}

	var projectStatsList []ProjectStats
	for proj, sessList := range projectMap {
		pStats := aggregateSessions(sessList)
		projectStatsList = append(projectStatsList, ProjectStats{
			ProjectPath: proj,
			Stats:       pStats,
		})
	}
	sort.Slice(projectStatsList, func(i, j int) bool {
		return projectStatsList[i].ProjectPath < projectStatsList[j].ProjectPath
	})

	overallStats := aggregateSessions(filtered)
	return overallStats, projectStatsList, nil
}

type parsedSession struct {
	id           string
	title        string
	project      string
	workspaceURI string
	createdAt    time.Time
	messages     int64
	promptTokens int64
	compTokens   int64
	cacheRead    int64
	cacheWrite   int64
	cost         float64
	models       map[string]*modelAccum
	tools        map[string]int64
	timestamps   []time.Time
}

type modelAccum struct {
	messageCount int64
	promptTokens int64
	compTokens   int64
	cacheRead    int64
	cost         float64
}

func filterSessionsByCwd(sessions []*parsedSession, cwd string, all bool) []*parsedSession {
	if all || cwd == "" {
		return sessions
	}

	cleanCwd := filepath.Clean(cwd)
	baseCwd := filepath.Base(cleanCwd)

	var filtered []*parsedSession
	for _, s := range sessions {
		wsPath := normalizeWorkspacePath(s.workspaceURI)
		if wsPath != "" {
			cleanWs := filepath.Clean(wsPath)
			if cleanWs == cleanCwd || strings.HasPrefix(cleanWs, cleanCwd+string(filepath.Separator)) || strings.HasPrefix(cleanCwd, cleanWs+string(filepath.Separator)) {
				filtered = append(filtered, s)
				continue
			}
		}
		if s.project != "" && (s.project == baseCwd || s.project == cleanCwd) {
			filtered = append(filtered, s)
			continue
		}
	}

	// If no sessions had workspace URIs, fall back to returning all sessions
	if len(filtered) == 0 {
		hasAnyURI := false
		for _, s := range sessions {
			if s.workspaceURI != "" || s.project != "" {
				hasAnyURI = true
				break
			}
		}
		if !hasAnyURI {
			return sessions
		}
	}

	return filtered
}

func normalizeWorkspacePath(uri string) string {
	if uri == "" {
		return ""
	}
	if strings.HasPrefix(uri, "file://") {
		u, err := url.Parse(uri)
		if err == nil {
			return u.Path
		}
		return strings.TrimPrefix(uri, "file://")
	}
	return uri
}

func discoverAndParseSessions(dataDir string, opts CollectOptions) ([]*parsedSession, error) {
	sessionMap := make(map[string]*parsedSession)

	// 1. If crawlDir is specified, scan crawlDir for stores
	if opts.CrawlDir != "" {
		crawlStores(opts.CrawlDir, sessionMap)
	}

	// 2. Discover metadata from conversation_summaries.db & cache/conversation_metadata.json
	metaMap := loadMetadata(dataDir)

	// 3. Discover from brain/ directory
	brainDir := filepath.Join(dataDir, "brain")
	if entries, err := os.ReadDir(brainDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			convID := entry.Name()
			transcriptPath := filepath.Join(brainDir, convID, ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(transcriptPath); err == nil {
				sess := parseTranscriptFile(convID, transcriptPath)
				if sess != nil {
					if meta, ok := metaMap[convID]; ok {
						if meta.title != "" {
							sess.title = meta.title
						}
						if meta.project != "" {
							sess.project = meta.project
						}
						if meta.workspaceURI != "" {
							sess.workspaceURI = meta.workspaceURI
						}
					}
					sessionMap[convID] = sess
				}
			}
		}
	}

	// 4. Discover any conversations/*.db that were not in brain/
	convsDir := filepath.Join(dataDir, "conversations")
	if entries, err := os.ReadDir(convsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
				continue
			}
			convID := strings.TrimSuffix(entry.Name(), ".db")
			if _, exists := sessionMap[convID]; exists {
				continue
			}
			dbPath := filepath.Join(convsDir, entry.Name())
			sess := parseConversationDB(convID, dbPath)
			if sess != nil {
				if meta, ok := metaMap[convID]; ok {
					if meta.title != "" {
						sess.title = meta.title
					}
					if meta.project != "" {
						sess.project = meta.project
					}
					if meta.workspaceURI != "" {
						sess.workspaceURI = meta.workspaceURI
					}
				}
				sessionMap[convID] = sess
			}
		}
	}

	// 5. Discover .crush/crush.db in dataDir if present
	crushDBPath := filepath.Join(dataDir, "crush.db")
	if _, err := os.Stat(crushDBPath); err == nil {
		parseCrushDB(crushDBPath, sessionMap)
	}
	dotCrushDB := filepath.Join(dataDir, ".crush", "crush.db")
	if _, err := os.Stat(dotCrushDB); err == nil {
		parseCrushDB(dotCrushDB, sessionMap)
	}

	var results []*parsedSession
	for _, s := range sessionMap {
		results = append(results, s)
	}
	return results, nil
}

type convMeta struct {
	title        string
	project      string
	workspaceURI string
	updatedAt    time.Time
}

func loadMetadata(dataDir string) map[string]convMeta {
	metas := make(map[string]convMeta)

	// Read cache/conversation_metadata.json
	cachePath := filepath.Join(dataDir, "cache", "conversation_metadata.json")
	if data, err := os.ReadFile(cachePath); err == nil {
		var cacheMap map[string]struct {
			Summary struct {
				Title         string   `json:"Title"`
				Preview       string   `json:"Preview"`
				UpdatedAt     string   `json:"UpdatedAt"`
				WorkspaceURIs []string `json:"WorkspaceURIs"`
			} `json:"summary"`
		}
		if json.Unmarshal(data, &cacheMap) == nil {
			for id, item := range cacheMap {
				title := item.Summary.Title
				if title == "" {
					title = item.Summary.Preview
				}
				var wsURI string
				var proj string
				if len(item.Summary.WorkspaceURIs) > 0 {
					wsURI = item.Summary.WorkspaceURIs[0]
					proj = filepath.Base(normalizeWorkspacePath(wsURI))
				}
				metas[id] = convMeta{
					title:        title,
					project:      proj,
					workspaceURI: wsURI,
				}
			}
		}
	}

	// Read conversation_summaries.db
	sumDBPath := filepath.Join(dataDir, "conversation_summaries.db")
	if _, err := os.Stat(sumDBPath); err == nil {
		db, err := sql.Open("sqlite3", "file:"+sumDBPath+"?mode=ro")
		if err == nil {
			defer db.Close()
			rows, err := db.Query("SELECT conversation_id, title, preview, workspace_uris, last_modified_time FROM conversation_summaries")
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var id, title, preview, wsJSON, lastMod sql.NullString
					if err := rows.Scan(&id, &title, &preview, &wsJSON, &lastMod); err == nil && id.Valid && id.String != "" {
						t := title.String
						if t == "" {
							t = preview.String
						}
						var uris []string
						var wsURI, proj string
						if wsJSON.Valid && wsJSON.String != "" {
							_ = json.Unmarshal([]byte(wsJSON.String), &uris)
							if len(uris) > 0 {
								wsURI = uris[0]
								proj = filepath.Base(normalizeWorkspacePath(wsURI))
							}
						}
						metas[id.String] = convMeta{
							title:        t,
							project:      proj,
							workspaceURI: wsURI,
						}
					}
				}
			}
		}
	}

	return metas
}

func parseTranscriptFile(convID, transcriptPath string) *parsedSession {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	sess := &parsedSession{
		id:     convID,
		models: make(map[string]*modelAccum),
		tools:  make(map[string]int64),
	}

	scanner := bufio.NewScanner(f)
	// Allow larger token payloads
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var row struct {
			Source           string `json:"source"`
			Type             string `json:"type"`
			Status           string `json:"status"`
			CreatedAt        string `json:"created_at"`
			Model            string `json:"model"`
			InputTokens      int64  `json:"input_tokens"`
			OutputTokens     int64  `json:"output_tokens"`
			CacheReadTokens  int64  `json:"cache_read_tokens"`
			CacheWriteTokens int64  `json:"cache_write_tokens"`
			Content          string `json:"content"`
			ToolCalls        []struct {
				Name string `json:"name"`
			} `json:"tool_calls"`
		}

		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}

		// Count messages
		if row.Source == "USER" || row.Source == "MODEL" {
			sess.messages++
			if sess.title == "" && row.Source == "USER" && row.Content != "" {
				sess.title = truncateString(strings.TrimSpace(row.Content), 60)
			}
		}

		// Parse timestamp
		if row.CreatedAt != "" {
			t, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
			if err != nil {
				t, err = time.Parse(time.RFC3339, row.CreatedAt)
			}
			if err == nil {
				sess.timestamps = append(sess.timestamps, t)
				if sess.createdAt.IsZero() || t.Before(sess.createdAt) {
					sess.createdAt = t
				}
			}
		}

		// Tokens and cost
		sess.promptTokens += row.InputTokens
		sess.compTokens += row.OutputTokens
		sess.cacheRead += row.CacheReadTokens
		sess.cacheWrite += row.CacheWriteTokens

		if row.Model != "" {
			mCost := EstimateCost(row.Model, row.InputTokens, row.OutputTokens, row.CacheReadTokens)
			sess.cost += mCost

			accum := sess.models[row.Model]
			if accum == nil {
				accum = &modelAccum{}
				sess.models[row.Model] = accum
			}
			accum.messageCount++
			accum.promptTokens += row.InputTokens
			accum.compTokens += row.OutputTokens
			accum.cacheRead += row.CacheReadTokens
			accum.cost += mCost
		}

		// Tool calls
		for _, tc := range row.ToolCalls {
			if tc.Name != "" {
				sess.tools[strings.ToLower(tc.Name)]++
			}
		}
	}

	if sess.createdAt.IsZero() {
		if fi, err := f.Stat(); err == nil {
			sess.createdAt = fi.ModTime()
			sess.timestamps = append(sess.timestamps, fi.ModTime())
		}
	}

	return sess
}

func parseConversationDB(convID, dbPath string) *parsedSession {
	fi, err := os.Stat(dbPath)
	if err != nil {
		return nil
	}

	sess := &parsedSession{
		id:        convID,
		createdAt: fi.ModTime(),
		models:    make(map[string]*modelAccum),
		tools:     make(map[string]int64),
	}
	sess.timestamps = append(sess.timestamps, fi.ModTime())

	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return sess
	}
	defer db.Close()

	var count int64
	if err := db.QueryRow("SELECT COUNT(*) FROM steps").Scan(&count); err == nil {
		sess.messages = count
	}
	return sess
}

func parseCrushDB(dbPath string, sessionMap map[string]*parsedSession) {
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	// Query messages table if exists
	rows, err := db.Query(`
		SELECT id, session_id, created_at, model, provider, prompt_tokens, completion_tokens, cost 
		FROM messages
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, sessID, createdAt, model, provider sql.NullString
		var promptTokens, compTokens sql.NullInt64
		var cost sql.NullFloat64

		if err := rows.Scan(&id, &sessID, &createdAt, &model, &provider, &promptTokens, &compTokens, &cost); err != nil {
			continue
		}

		sid := sessID.String
		if sid == "" {
			sid = "crush-session"
		}

		sess := sessionMap[sid]
		if sess == nil {
			sess = &parsedSession{
				id:      sid,
				project: "crush",
				models:  make(map[string]*modelAccum),
				tools:   make(map[string]int64),
			}
			sessionMap[sid] = sess
		}

		sess.messages++
		pt := promptTokens.Int64
		ct := compTokens.Int64
		sess.promptTokens += pt
		sess.compTokens += ct
		c := cost.Float64
		if c == 0 && model.String != "" {
			c = EstimateCost(model.String, pt, ct, 0)
		}
		sess.cost += c

		mName := model.String
		if mName != "" {
			accum := sess.models[mName]
			if accum == nil {
				accum = &modelAccum{}
				sess.models[mName] = accum
			}
			accum.messageCount++
			accum.promptTokens += pt
			accum.compTokens += ct
			accum.cost += c
		}

		if createdAt.Valid && createdAt.String != "" {
			t, err := time.Parse(time.RFC3339, createdAt.String)
			if err == nil {
				sess.timestamps = append(sess.timestamps, t)
				if sess.createdAt.IsZero() || t.Before(sess.createdAt) {
					sess.createdAt = t
				}
			}
		}
	}
}

func crawlStores(rootDir string, sessionMap map[string]*parsedSession) {
	skipDirs := map[string]bool{
		".git": true, ".svn": true, ".hg": true, "node_modules": true,
		"vendor": true, "dist": true, "build": true, "target": true,
	}

	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			// Check if this dir is brain/
			if d.Name() == "brain" {
				subEntries, err := os.ReadDir(path)
				if err == nil {
					for _, sub := range subEntries {
						if sub.IsDir() {
							tPath := filepath.Join(path, sub.Name(), ".system_generated", "logs", "transcript.jsonl")
							if _, err := os.Stat(tPath); err == nil {
								sess := parseTranscriptFile(sub.Name(), tPath)
								if sess != nil {
									sess.project = filepath.Base(filepath.Dir(path))
									sessionMap[sub.Name()] = sess
								}
							}
						}
					}
				}
				return filepath.SkipDir
			}
			return nil
		}

		if d.Name() == "crush.db" {
			parseCrushDB(path, sessionMap)
		}
		return nil
	})
}

func aggregateSessions(sessions []*parsedSession) *Stats {
	stats := &Stats{
		GeneratedAt: time.Now().UTC(),
	}

	dailyMap := make(map[string]*DailyUsage)
	modelMap := make(map[string]*ModelUsage)
	hourMap := make(map[int]int64)
	dowMap := make(map[int]*DayOfWeekUsage)
	toolMap := make(map[string]int64)
	heatmapMap := make(map[string]*HourDayHeatmapPt)

	// Initialize days of week
	for i := 0; i < 7; i++ {
		dowMap[i] = &DayOfWeekUsage{
			DayOfWeek: i,
			DayName:   dayNames[i],
		}
	}

	// Initialize hours
	for h := 0; h < 24; h++ {
		hourMap[h] = 0
	}

	stats.Total.TotalSessions = int64(len(sessions))

	for _, s := range sessions {
		stats.Total.TotalMessages += s.messages
		stats.Total.TotalPromptTokens += s.promptTokens
		stats.Total.TotalCompletionTokens += s.compTokens
		stats.Total.TotalCacheReadTokens += s.cacheRead
		stats.Total.TotalCacheWriteTokens += s.cacheWrite
		stats.Total.TotalCost += s.cost

		sessDate := s.createdAt.UTC()
		if sessDate.IsZero() {
			sessDate = time.Now().UTC()
		}

		dayKey := sessDate.Format("2006-01-02")
		dUsage := dailyMap[dayKey]
		if dUsage == nil {
			dUsage = &DailyUsage{Day: dayKey}
			dailyMap[dayKey] = dUsage
		}
		dUsage.SessionCount++
		dUsage.PromptTokens += s.promptTokens
		dUsage.CompletionTokens += s.compTokens
		dUsage.CacheReadTokens += s.cacheRead
		dUsage.CacheWriteTokens += s.cacheWrite
		dUsage.TotalTokens += (s.promptTokens + s.compTokens + s.cacheRead + s.cacheWrite)
		dUsage.Cost += s.cost

		// Day of week & hour
		dow := int(sessDate.Weekday())
		dowMap[dow].SessionCount++
		dowMap[dow].PromptTokens += s.promptTokens
		dowMap[dow].CompletionTokens += s.compTokens

		hour := sessDate.Hour()
		hourMap[hour]++

		hmKey := fmt.Sprintf("%d-%d", dow, hour)
		hmPt := heatmapMap[hmKey]
		if hmPt == nil {
			hmPt = &HourDayHeatmapPt{DayOfWeek: dow, Hour: hour}
			heatmapMap[hmKey] = hmPt
		}
		hmPt.SessionCount++

		// Models
		for mName, accum := range s.models {
			provider := ResolveProvider(mName)
			key := mName + "|" + provider
			mUsage := modelMap[key]
			if mUsage == nil {
				mUsage = &ModelUsage{
					Model:    mName,
					Provider: provider,
				}
				modelMap[key] = mUsage
			}
			mUsage.MessageCount += accum.messageCount
			mUsage.PromptTokens += accum.promptTokens
			mUsage.CompletionTokens += accum.compTokens
			mUsage.TotalTokens += (accum.promptTokens + accum.compTokens + accum.cacheRead)
			mUsage.Cost += accum.cost
		}

		// Tools
		for tName, count := range s.tools {
			toolMap[tName] += count
		}

		// Recent session item
		sessTokens := s.promptTokens + s.compTokens + s.cacheRead + s.cacheWrite
		stats.RecentSessions = append(stats.RecentSessions, SessionItem{
			SessionID:    s.id,
			Title:        s.title,
			Project:      s.project,
			MessageCount: s.messages,
			TotalTokens:  sessTokens,
			Cost:         s.cost,
			CreatedAt:    s.createdAt,
		})
	}

	stats.Total.TotalTokens = stats.Total.TotalPromptTokens + stats.Total.TotalCompletionTokens + stats.Total.TotalCacheReadTokens + stats.Total.TotalCacheWriteTokens

	if stats.Total.TotalSessions > 0 {
		stats.Total.AvgTokensPerSession = float64(stats.Total.TotalTokens) / float64(stats.Total.TotalSessions)
		stats.Total.AvgMessagesPerSession = float64(stats.Total.TotalMessages) / float64(stats.Total.TotalSessions)
	}

	// Usage by day
	for _, d := range dailyMap {
		stats.UsageByDay = append(stats.UsageByDay, *d)
	}
	sort.Slice(stats.UsageByDay, func(i, j int) bool {
		return stats.UsageByDay[i].Day > stats.UsageByDay[j].Day
	})

	// Recent activity (last 30 days)
	for _, d := range stats.UsageByDay {
		stats.RecentActivity = append(stats.RecentActivity, DailyActivity{
			Day:          d.Day,
			SessionCount: d.SessionCount,
			TotalTokens:  d.TotalTokens,
			Cost:         d.Cost,
		})
	}
	// Sort recent activity ascending by date for chart display
	sort.Slice(stats.RecentActivity, func(i, j int) bool {
		return stats.RecentActivity[i].Day < stats.RecentActivity[j].Day
	})
	if len(stats.RecentActivity) > 30 {
		stats.RecentActivity = stats.RecentActivity[len(stats.RecentActivity)-30:]
	}

	// Usage by model
	for _, m := range modelMap {
		stats.UsageByModel = append(stats.UsageByModel, *m)
	}
	sort.Slice(stats.UsageByModel, func(i, j int) bool {
		return stats.UsageByModel[i].MessageCount > stats.UsageByModel[j].MessageCount
	})

	// Usage by hour
	for h := 0; h < 24; h++ {
		stats.UsageByHour = append(stats.UsageByHour, HourlyUsage{
			Hour:         h,
			SessionCount: hourMap[h],
		})
	}

	// Usage by day of week
	for i := 0; i < 7; i++ {
		stats.UsageByDayOfWeek = append(stats.UsageByDayOfWeek, *dowMap[i])
	}

	// Tool usage
	for tName, count := range toolMap {
		stats.ToolUsage = append(stats.ToolUsage, ToolUsage{
			ToolName:  tName,
			CallCount: count,
		})
	}
	sort.Slice(stats.ToolUsage, func(i, j int) bool {
		return stats.ToolUsage[i].CallCount > stats.ToolUsage[j].CallCount
	})

	// Heatmap
	for _, pt := range heatmapMap {
		stats.HourDayHeatmap = append(stats.HourDayHeatmap, *pt)
	}
	sort.Slice(stats.HourDayHeatmap, func(i, j int) bool {
		if stats.HourDayHeatmap[i].DayOfWeek != stats.HourDayHeatmap[j].DayOfWeek {
			return stats.HourDayHeatmap[i].DayOfWeek < stats.HourDayHeatmap[j].DayOfWeek
		}
		return stats.HourDayHeatmap[i].Hour < stats.HourDayHeatmap[j].Hour
	})

	// Sort recent sessions newest first
	sort.Slice(stats.RecentSessions, func(i, j int) bool {
		return stats.RecentSessions[i].CreatedAt.After(stats.RecentSessions[j].CreatedAt)
	})
	if len(stats.RecentSessions) > 50 {
		stats.RecentSessions = stats.RecentSessions[:50]
	}

	return stats
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

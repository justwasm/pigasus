package stats

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
)

//go:embed assets/index.html
var statsTemplate string

//go:embed assets/index.css
var statsCSS string

//go:embed assets/index.js
var statsJS string

//go:embed assets/header.svg
var headerSVG string

//go:embed assets/heartbit.svg
var heartbitSVG string

//go:embed assets/footer.svg
var footerSVG string

type templateData struct {
	StatsJSON        template.JS
	ProjectStatsJSON template.JS
	CSS              template.CSS
	JS               template.JS
	Header           template.HTML
	Heartbit         template.HTML
	Footer           template.HTML
	Favicon          template.URL
	GeneratedAt      string
	ProjectName      string
	Username         string
}

// RenderHTML renders the HTML dashboard for the given stats.
func RenderHTML(stats *Stats, projectStats []ProjectStats, projName, username string) ([]byte, error) {
	if stats == nil {
		stats = &Stats{}
	}
	if projectStats == nil {
		projectStats = []ProjectStats{}
	}

	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return nil, fmt.Errorf("marshal stats json: %w", err)
	}

	projectStatsJSON, err := json.Marshal(projectStats)
	if err != nil {
		return nil, fmt.Errorf("marshal project stats json: %w", err)
	}

	tmpl, err := template.New("pigasus_stats").Parse(statsTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse stats template: %w", err)
	}

	data := templateData{
		StatsJSON:        template.JS(statsJSON),
		ProjectStatsJSON: template.JS(projectStatsJSON),
		CSS:              template.CSS(statsCSS),
		JS:               template.JS(statsJS),
		Header:           template.HTML(headerSVG),
		Heartbit:         template.HTML(heartbitSVG),
		Footer:           template.HTML(footerSVG),
		Favicon:          template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(heartbitSVG))),
		GeneratedAt:      stats.GeneratedAt.Format("2006-01-02"),
		ProjectName:      projName,
		Username:         username,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	return buf.Bytes(), nil
}

// WriteTempHTML renders the dashboard and writes it to an OS temporary HTML file.
func WriteTempHTML(stats *Stats, projectStats []ProjectStats, projName, username string) (string, error) {
	htmlBytes, err := RenderHTML(stats, projectStats, projName, username)
	if err != nil {
		return "", err
	}

	tmpFile, err := os.CreateTemp("", "pigasus-stats-*.html")
	if err != nil {
		return "", fmt.Errorf("create temp html file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(htmlBytes); err != nil {
		return "", fmt.Errorf("write temp html file: %w", err)
	}

	return tmpFile.Name(), nil
}

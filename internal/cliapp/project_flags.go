package cliapp

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/cliflags"
)

type projectDefinition struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	ProjectResources projectResources `json:"projectResources"`
}

type projectResources struct {
	Resources []projectFolder `json:"resources"`
}

type projectFolder struct {
	FolderURI string `json:"folderUri"`
}

func applyProjectFlags(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	newProject := cliflags.BoolValue(fs, "new-project")
	projectName := cliflags.StringValue(fs, "project")
	if !newProject && projectName == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return
	}

	if newProject {
		projID := agyprompt.NewConversationID()
		name := projectName
		if name == "" {
			name = filepath.Base(cwd)
		}
		proj := projectDefinition{
			ID:   projID,
			Name: name,
			ProjectResources: projectResources{
				Resources: []projectFolder{
					{FolderURI: "file://" + cwd},
				},
			},
		}
		data, err := json.MarshalIndent(proj, "", "  ")
		if err == nil {
			dir := filepath.Join(home, ".gemini", "config", "projects")
			_ = os.MkdirAll(dir, 0755)
			_ = os.WriteFile(filepath.Join(dir, projID+".json"), data, 0644)
			updateProjectsCache(home, cwd, projID)
		}
	} else if projectName != "" {
		if projID := resolveProject(home, projectName); projID != "" {
			updateProjectsCache(home, cwd, projID)
		}
	}
}

func resolveProject(home, target string) string {
	dir := filepath.Join(home, ".gemini", "config", "projects")
	idPath := filepath.Join(dir, target+".json")
	if data, err := os.ReadFile(idPath); err == nil {
		var p projectDefinition
		if err := json.Unmarshal(data, &p); err == nil && p.ID != "" {
			return p.ID
		}
		return target
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p projectDefinition
		if err := json.Unmarshal(data, &p); err == nil {
			if p.ID == target || strings.EqualFold(p.Name, target) {
				return p.ID
			}
		}
	}
	return ""
}

func updateProjectsCache(home, cwd, projID string) {
	cachePath := filepath.Join(home, ".gemini", "antigravity-cli", "cache", "projects.json")
	_ = os.MkdirAll(filepath.Dir(cachePath), 0755)
	cache := make(map[string]string)
	if data, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	cache[cwd] = projID
	if data, err := json.MarshalIndent(cache, "", "  "); err == nil {
		_ = os.WriteFile(cachePath, data, 0644)
	}
}

func applyLogFileFlag(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	logFile := cliflags.StringValue(fs, "log-file")
	if logFile == "" {
		return
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	cli.RedirectStderr(f)
}

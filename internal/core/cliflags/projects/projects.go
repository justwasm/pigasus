// Package projects owns the CLI's --project and --new-project flags, the
// project store they name and the cache that maps a workspace onto a
// project.
//
// The two flags are the reference's own:
//
//	--new-project    bool   Create a new project for this session
//	--project        string Project ID or project name for the current CLI session
//
// Their names, their defaults and their help text are in the reference's
// flag table (internal/cli/parser_flags.go), and this package does not
// touch them: it reads the flags the parser registered and resolves them
// against the store.
//
// The store is the reference's own project directory, one JSON file per
// project, named after the project's id:
//
//	~/.gemini/config/projects/<id>.json
//
// and the cache is the reference's own cache, the map from a workspace
// directory onto the project the workspace belongs to:
//
//	~/.gemini/antigravity-cli/cache/projects.json
//
// A project may be named three ways: by its id, by its name (in any case)
// and by nothing at all — a name that matches no project resolves to the
// default project, because a session always runs in a project.
//
// The default project is the reference's own: a session that starts
// without a project flag runs in it.
//
//	When starting the CLI without any project flags, all conversations in
//	the session run in the `default-cli-project`.
//
//	— official-docs/projects.md:62
//
// The package is a tier-1 fact: the flags are the CLI's, the store they
// name is a fact about the machine, and the id of the project a session
// runs in is a fact about the session. The host keeps the wiring —
// where the resolution runs, and what the session does with it.
package projects

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// The reference's flag names.
const (
	// FlagProject names the project the session runs in. Its value is
	// a project id or a project name.
	FlagProject = "project"
	// FlagNewProject names the flag that creates the project the
	// session runs in.
	FlagNewProject = "new-project"
)

// DefaultProjectID is the id of the project a CLI session runs in when no
// project flag names one.
//
// It is the reference's own id, and the reference's own store holds the
// file it names:
//
//	{"id":"default-cli-project","name":"CLI Project"}
//
// 【一手】 2026-10-26, live: the reference creates that file on its first
// run — `~/.gemini/config/` is auto-created with
// `projects/default-cli-project.json` (`out/blackbox-whitebox-final.md:119`),
// and the changelog records the name it gives the project it initialises:
// "Changed the default name for the newly initialized project to `CLI
// Project`" (`official-docs/changelog.md:2752`).
//
// 【strings】 the reference binary carries the literal
// "default-cli-project" next to its project plumbing
// (`docs/legacy-re/extracts/strings-v1.3.4-cited.txt:174`).
//
// The documentation states the rule the id follows:
//
//	When starting the CLI without any project flags, all conversations in
//	the session run in the `default-cli-project`.
//
//	— official-docs/projects.md:62
//
// This constant is the one owner of the literal: the permissions panel's
// project file and the remote control's project list ask this package
// for it.
const DefaultProjectID = "default-cli-project"

// Options are the machine facts the resolution needs.
type Options struct {
	// Home is the root of the project store — the user's home
	// directory. Empty means the user's own home.
	Home string
	// Cwd is the workspace the session runs in. It is the key the
	// projects cache maps onto a project id. Empty means the process's
	// own working directory.
	Cwd string
	// NewID mints the id of the project the --new-project flag
	// creates. The reference mints a conversation id for it, and the
	// generator belongs to the conversation's package — so the caller
	// supplies it.
	NewID func() string
}

// Apply resolves the --project and --new-project flags against the project
// store and the projects cache, and returns the id of the project the
// session runs in.
//
// The rules are the reference's own:
//
//   - --new-project names the project the flag creates;
//   - --project names an existing project, by its id or by its name;
//   - neither flag names the default project — "When starting the CLI
//     without any project flags, all conversations in the session run in
//     the `default-cli-project`" (official-docs/projects.md:62).
//
// A --project that names no project resolves to the default project as
// well: the session always runs in a project.
//
// The machine's own files are left alone when the default project is the
// answer: the reference creates that project on its first run
// (【一手】 out/blackbox-whitebox-final.md:119), so the flag resolution
// does not write it, and it does not write the projects cache either.
func Apply(fs *flag.FlagSet, opts Options) string {
	if fs == nil {
		return DefaultProjectID
	}
	newProject := cliflags.BoolValue(fs, FlagNewProject)
	projectName := cliflags.StringValue(fs, FlagProject)
	if !newProject && projectName == "" {
		return DefaultProjectID
	}
	home := opts.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil || h == "" {
			return DefaultProjectID
		}
		home = h
	}
	cwd := opts.Cwd
	if cwd == "" {
		c, err := os.Getwd()
		if err != nil || c == "" {
			return DefaultProjectID
		}
		cwd = c
	}

	if newProject {
		projID := ""
		if opts.NewID != nil {
			projID = opts.NewID()
		}
		if projID == "" {
			return DefaultProjectID
		}
		name := projectName
		if name == "" {
			name = filepath.Base(cwd)
		}
		proj := definition{
			ID:   projID,
			Name: name,
			ProjectResources: resources{
				Resources: []folder{
					{FolderURI: "file://" + cwd},
				},
			},
		}
		data, err := json.MarshalIndent(proj, "", "  ")
		if err != nil {
			return DefaultProjectID
		}
		dir := storeDir(home)
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(ConfigPath(home, projID), data, 0644)
		updateCache(home, cwd, projID)
		return projID
	}
	if projID := resolve(home, projectName); projID != "" {
		updateCache(home, cwd, projID)
		return projID
	}
	return DefaultProjectID
}

// definition is one project file: the project's id, its name and the
// folders the project covers.
type definition struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ProjectResources resources `json:"projectResources"`
}

// resources are the folders a project covers.
type resources struct {
	Resources []folder `json:"resources"`
}

// folder is one folder a project covers.
type folder struct {
	FolderURI string `json:"folderUri"`
}

// storeDir is the reference's project directory.
func storeDir(home string) string {
	return filepath.Join(home, ".gemini", "config", "projects")
}

// ConfigPath is the file the project with the given id lives in — the
// reference's own project directory, one JSON file per project, named
// after the project's id.
//
//	~/.gemini/config/projects/<id>.json
//
// The permissions panel's project scope reads and writes that file, so
// the path shape has one owner.
func ConfigPath(home, id string) string {
	return filepath.Join(storeDir(home), id+".json")
}

// cachePath is the reference's projects cache.
func cachePath(home string) string {
	return filepath.Join(home, ".gemini", "antigravity-cli", "cache", "projects.json")
}

// resolve names the project id the target names.
//
// The target is a project id, a project name (in any case) or the name of
// the file the project lives in. A target that names no project resolves
// to nothing.
func resolve(home, target string) string {
	dir := storeDir(home)
	idPath := filepath.Join(dir, target+".json")
	if data, err := os.ReadFile(idPath); err == nil {
		var p definition
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
		var p definition
		if err := json.Unmarshal(data, &p); err == nil {
			if p.ID == target || strings.EqualFold(p.Name, target) {
				return p.ID
			}
		}
	}
	return ""
}

// updateCache names the project the workspace belongs to.
//
// The cache is the reference's own: a map from a workspace directory onto
// the project's id. An entry the workspace already has is replaced.
func updateCache(home, cwd, projID string) {
	path := cachePath(home)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	cache := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	cache[cwd] = projID
	if data, err := json.MarshalIndent(cache, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
}

// LocalProject represents a local project with resolved folder paths.
type LocalProject struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Folders []string `json:"folders"`
}

// ResolveFolderPath resolves a raw folder path that may be absolute, home-relative (~), or workspace-relative.
func ResolveFolderPath(workspace, rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(trimmed, "file://") {
		trimmed = strings.TrimPrefix(trimmed, "file://")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if trimmed == "~" {
			trimmed = home
		} else {
			trimmed = filepath.Join(home, trimmed[2:])
		}
	} else if !filepath.IsAbs(trimmed) {
		trimmed = filepath.Join(workspace, trimmed)
	}
	return filepath.Clean(trimmed), nil
}

// ListLocalProjects returns all local projects found in ~/.gemini/config/projects/.
func ListLocalProjects(home string) ([]LocalProject, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	dir := storeDir(home)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []LocalProject{{ID: DefaultProjectID, Name: "CLI Project"}}, nil
		}
		return nil, err
	}
	var list []LocalProject
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p definition
		if err := json.Unmarshal(data, &p); err == nil && p.ID != "" {
			var folders []string
			for _, r := range p.ProjectResources.Resources {
				uri := r.FolderURI
				path := strings.TrimPrefix(uri, "file://")
				folders = append(folders, path)
			}
			list = append(list, LocalProject{
				ID:      p.ID,
				Name:    p.Name,
				Folders: folders,
			})
		}
	}
	if len(list) == 0 {
		list = append(list, LocalProject{ID: DefaultProjectID, Name: "CLI Project"})
	}
	return list, nil
}

// SaveLocalProject saves a project definition to ~/.gemini/config/projects/<id>.json.
func SaveLocalProject(home string, proj LocalProject) error {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}
	var resList []folder
	for _, f := range proj.Folders {
		resList = append(resList, folder{FolderURI: "file://" + f})
	}
	p := definition{
		ID:   proj.ID,
		Name: proj.Name,
		ProjectResources: resources{
			Resources: resList,
		},
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	dir := storeDir(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(home, proj.ID), data, 0644)
}

// DeleteLocalProject deletes a project definition file.
func DeleteLocalProject(home, id string) error {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}
	return os.Remove(ConfigPath(home, id))
}

// CurrentLocalProjectID returns the current project ID for the given workspace.
func CurrentLocalProjectID(home, cwd string) string {
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return DefaultProjectID
		}
		home = h
	}
	path := cachePath(home)
	if data, err := os.ReadFile(path); err == nil {
		var cache map[string]string
		if err := json.Unmarshal(data, &cache); err == nil {
			if id, ok := cache[cwd]; ok && id != "" {
				return id
			}
		}
	}
	return DefaultProjectID
}

// SetCurrentLocalProject associates the cwd with projID in cache.
func SetCurrentLocalProject(home, cwd, projID string) {
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return
		}
		home = h
	}
	updateCache(home, cwd, projID)
}

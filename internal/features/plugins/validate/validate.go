// Package validate owns plugin manifest, component capability and image/archive
// path validation. It reports what the plugin loader accepts, skips or ignores.
//
// Check reads only the filesystem supplied by the caller. It does not select a
// command-line operand, fetch archives, run processes, print diagnostics or pick
// exit codes. Hosts own those effects and can validate the same plugin with an
// os.DirFS or a deterministic in-memory filesystem.
//
// A filesystem is a read port, not a security sandbox: a caller supplying
// os.DirFS retains that filesystem's symlink-following behavior. The validator
// preserves the loader's existing local-image behavior rather than imposing a
// new installation or filesystem-access policy.
package validate

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Source is a selected plugin tree. Files uses slash-relative io/fs paths;
// Manifest names the selected manifest within it. Dir is the source's display
// identity and the fallback plugin name, never an implicit filesystem root.
// An empty Manifest selects the conventional manifest using FindManifest.
type Source struct {
	Files    fs.FS
	Dir      string
	Manifest string
}

// Severity is the stable value of a loader finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityNote    Severity = "note"
)

// Finding describes one observable loader decision. Parse locations are part
// of Message, preserving the report schema rather than adding a line field.
type Finding struct {
	Severity Severity `json:"severity"`
	File     string   `json:"file"`
	Message  string   `json:"message"`
}

// Components counts the capabilities the loader will load.
type Components struct {
	Agents     int `json:"agents"`
	Hooks      int `json:"hooks"`
	MCPServers int `json:"mcpServers"`
	Rules      int `json:"rules"`
	Sidecars   int `json:"sidecars"`
	Skills     int `json:"skills"`
}

// Report is the structured validation document. Field order and JSON names
// preserve the existing plugin validate report contract.
type Report struct {
	Dir              string     `json:"dir"`
	Manifest         string     `json:"manifest,omitempty"`
	Name             string     `json:"name,omitempty"`
	AgentPluginsSpec bool       `json:"agentPluginsSpec"`
	Components       Components `json:"components"`
	Findings         []Finding  `json:"findings"`
}

// Counts returns errors and warnings; informational notes are not failures.
// The host decides whether warnings should change its exit code.
func (r Report) Counts() (errors, warnings int) {
	for _, finding := range r.Findings {
		switch finding.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		}
	}
	return errors, warnings
}

// Result includes whether a manifest was usable, which controls whether a host
// may show its name and component summary even when component errors exist.
type Result struct {
	Report     Report
	ManifestOK bool
}

// Check validates the selected manifest and its plugin-root components. All
// failures are observable findings; it never prints or performs host effects.
// Findings are stable: errors, warnings, then notes, each ordered by filename
// while preserving the domain checks' order within a file.
func Check(source Source) Result {
	result := Result{Report: Report{Dir: source.Dir, Findings: []Finding{}}}
	if source.Files == nil {
		result.Report.Findings = append(result.Report.Findings, Finding{
			Severity: SeverityError, File: ".", Message: "no plugin filesystem supplied",
		})
		return result
	}
	manifest := source.Manifest
	if manifest == "" {
		manifest = FindManifest(source.Files)
	}
	if manifest == "" {
		result.Report.Findings = append(result.Report.Findings, Finding{
			Severity: SeverityError, File: "plugin.json",
			Message: "no plugin.json found at the plugin root or in .antigravity-plugin or .gemini-plugin",
		})
		return result
	}
	result.Report.Manifest = manifest
	if !fs.ValidPath(manifest) {
		result.Report.Findings = append(result.Report.Findings, Finding{
			Severity: SeverityError, File: manifest,
			Message: fmt.Sprintf("manifest path %q is outside the plugin root", manifest),
		})
		return result
	}
	validateManifest(source.Files, source.Dir, manifest, &result)
	if result.ManifestOK {
		checkMisplacedHiddenComponents(source.Files, manifest, &result.Report)
		scanValidateSkills(source.Files, &result.Report)
		scanValidateAgents(source.Files, &result.Report)
		scanValidateRules(source.Files, &result.Report)
		scanValidateHooks(source.Files, &result.Report)
		scanValidateMCPServers(source.Files, &result.Report)
		checkRootMisplacedFiles(source.Files, &result.Report)
	}
	finaliseReport(&result.Report)
	return result
}

// FindManifest locates the case-insensitive plugin manifest at the root or
// in one of the two supported hidden manifest directories. The returned path
// uses io/fs slash separators.
func FindManifest(files fs.FS) string {
	for _, dir := range []string{".", ".antigravity-plugin", ".gemini-plugin"} {
		entries, err := fs.ReadDir(files, dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.EqualFold(entry.Name(), "plugin.json") {
				if dir == "." {
					return entry.Name()
				}
				return path.Join(dir, entry.Name())
			}
		}
	}
	return ""
}

// ArchiveEntryPath accepts an archive member only when it remains beneath the
// extraction root. It deliberately rejects absolute and parent-traversing
// names before the CLI writes any member to disk.
func ArchiveEntryPath(name string) (string, bool) {
	clean := filepath.Clean(name)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", false
	}
	return filepath.ToSlash(clean), true
}

func finaliseReport(report *Report) {
	rank := func(s Severity) int {
		switch s {
		case SeverityError:
			return 0
		case SeverityWarning:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if rank(a.Severity) != rank(b.Severity) {
			return rank(a.Severity) < rank(b.Severity)
		}
		return a.File < b.File
	})
}

// manifestReadError preserves the existing diagnostic's full selected path
// when adapting os.DirFS's relative PathError to the report's source identity.
func manifestReadError(dir, manifest string, err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return (&fs.PathError{Op: pathErr.Op, Path: filepath.Join(dir, manifest), Err: pathErr.Err}).Error()
	}
	return err.Error()
}

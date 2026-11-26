package permissions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// blockedWriteFilenames are the file names whose writes the built-in
// protections keep behind approval: the agent's own configuration files and
// the secrets file. They are matched as *components* of the target path, so
// they apply wherever the file lives — a project's own hooks.json is the same
// kind of file as the one under the configuration home.
//
// AGY 1.5.17 names five sensitive files: ".git, .env, config.json, hooks.json,
// and mcp_config.json". config.json is the one this clone was missing; .env and
// .git were covered, but only by the generic "no hidden component" rule below,
// and only on the outside-workspace path — see isProtectedWriteTarget.
var blockedWriteFilenames = map[string]bool{
	"config.json":             true,
	"hooks.json":              true,
	"settings.json":           true,
	"mcp_config.json":         true,
	"admin_settings.json":     true,
	"outside-of-project.json": true,
	"sidecar.json":            true,
	".env":                    true,
}

// vcsMetadataComponents are the version-control metadata directories. Every
// file *inside* one of them is protected: a write into .git/config, or into
// .git/hooks/pre-commit, changes what the repository will do next time it is
// operated on.
//
// They are listed separately from blockedWriteFilenames because they are also
// what isInVCSRepo looks for: the same name is a *marker* that makes the rest
// of the tree auto-approvable, and a *target* that must never be. Detection and
// protection must not be the same check, or protecting .git would disable the
// repo allowance entirely.
var vcsMetadataComponents = map[string]bool{
	".git": true,
	".jj":  true,
	".hg":  true,
	".svn": true,
}

// envFilePrefix matches the .env family: .env.local, .env.development,
// .env.production.local, and so on. A bare ".env" is in blockedWriteFilenames;
// the family is matched by prefix because the tools that read it (dotenv and
// its ports) treat every member as the same secret store, and a file is one
// rename away from being one.
const envFilePrefix = ".env."

var blockedStartupComponents = map[string]bool{
	"launchagents":  true,
	"launchdaemons": true,
	"startup":       true,
	"autostart":     true,
	"systemd":       true,
	"init.d":        true,
	"cron.d":        true,
}

var blockedSystemPrefixes = []string{
	"/etc",
	"/private/etc",
	"/bin",
	"/sbin",
	"/usr",
	"/system",
	"/library",
	"/applications",
	"/dev",
	"/proc",
	"/sys",
	"/boot",
	"/root",
	"/var/root",
	"/var/db",
	"/var/log",
	"/var/run",
	"/var/spool",
	"/private/var/root",
	"/private/var/db",
	"/private/var/log",
	"/private/var/run",
	"/private/var/spool",
}

// ShouldAutoApproveOutsideWorkspaceFileEdit reports whether an outside-workspace
// file write or edit in Auto mode should be auto-approved without prompting
// (reference behaviour since 1.5.16; the 1.5.17 release narrowed it by the
// protected targets below).
//
// Auto mode allows creating or editing files outside the workspace without asking when:
//  1. The target path is safe (not hidden, not VCS/hook metadata, not a system/startup folder,
//     not a special file or symlink escaping into a blocked location), AND
//  2. Either the file is inside a version-controlled repository (.git, .jj, .hg), OR
//     the change only adds new content (creating a new file, appending, or inserting text
//     without replacing existing lines).
//
// This is the *outside*-workspace rule only. The protected targets are a
// separate, narrower check — isProtectedWriteTarget — and they apply to every
// scope, because the sensitive-file protection must not depend on which side of
// the workspace boundary the file happens to sit on.
func ShouldAutoApproveOutsideWorkspaceFileEdit(target, toolName string, rawArgs []byte) bool {
	if !isSafeWritePath(target) {
		return false
	}
	resolved, ok := resolveSymlinks(target)
	if !ok || !isSafeWritePath(resolved) {
		return false
	}
	if !isMissingOrRegularFile(resolved) {
		return false
	}
	if isInVCSRepo(resolved) {
		return true
	}
	return isNonReplacingEdit(toolName, rawArgs, resolved)
}

func isSafeWritePath(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || IsForbiddenWindowsPath(target) {
		return false
	}
	cleaned := filepath.Clean(target)
	if !filepath.IsAbs(cleaned) {
		return false
	}
	if hasBlockedWriteComponent(cleaned) {
		return false
	}
	lower := strings.ToLower(filepath.ToSlash(cleaned))
	for _, prefix := range blockedSystemPrefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix+"/") {
			return false
		}
	}
	return true
}

func hasBlockedWriteComponent(cleaned string) bool {
	slash := filepath.ToSlash(cleaned)
	parts := strings.Split(slash, "/")
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return true
		}
		lower := strings.ToLower(part)
		if blockedWriteFilenames[lower] || blockedStartupComponents[lower] {
			return true
		}
	}
	return false
}

// isProtectedWriteTarget reports whether a write to target is one of the
// sensitive files the built-in protection keeps behind approval, whatever
// workspace scope the write is judged under.
//
// It is deliberately *not* part of isSafeWritePath. isSafeWritePath answers
// "may this path be auto-approved at all"; this answers "is this a file the
// user must be asked about". Keeping them apart is what lets the protected
// list apply inside the workspace too, where isSafeWritePath is never
// consulted at all.
//
// A path is protected when any component of it —
//
//   - is a VCS metadata directory (.git, .jj, .hg, .svn), so the whole
//     subtree is protected and not merely detected;
//   - is a blocked configuration file (config.json, hooks.json,
//     settings.json, mcp_config.json, admin_settings.json,
//     outside-of-project.json, sidecar.json);
//   - is the secrets file .env or any .env.* variant.
//
// The check runs on the lexical path and on the symlink-resolved path: a
// symlink named "notes.txt" pointing at .env is a write to .env.
func isProtectedWriteTarget(target string) bool {
	if strings.TrimSpace(target) == "" {
		// No target is no write. Resolving "" would land on the process's
		// working directory, which is not what the call is about.
		return false
	}
	if hasProtectedWriteComponent(target) {
		return true
	}
	if resolved, ok := resolveSymlinks(target); ok && resolved != target {
		return hasProtectedWriteComponent(resolved)
	}
	return false
}

// hasProtectedWriteComponent reports whether any component of path is a
// protected name. It is case-insensitive because macOS and Windows
// filesystems are: ".ENV" opens the same file as ".env".
func hasProtectedWriteComponent(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		lower := strings.ToLower(part)
		if vcsMetadataComponents[lower] || blockedWriteFilenames[lower] {
			return true
		}
		if strings.HasPrefix(lower, envFilePrefix) {
			return true
		}
	}
	return false
}

func resolveSymlinks(target string) (string, bool) {
	cleaned := filepath.Clean(target)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved, true
	}
	// Walk up until an existing ancestor is found, resolve symlinks on it,
	// and re-append the non-existent suffix.
	cur := cleaned
	var suffix []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			resolvedBase, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", false
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolvedBase = filepath.Join(resolvedBase, suffix[i])
			}
			return resolvedBase, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return cleaned, true
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func isMissingOrRegularFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return os.IsNotExist(err)
	}
	return info.Mode().IsRegular()
}

func fileDoesNotExist(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

func isInVCSRepo(resolvedPath string) bool {
	dir := filepath.Dir(resolvedPath)
	for {
		for _, marker := range []string{".git", ".jj", ".hg"} {
			if _, err := os.Lstat(filepath.Join(dir, marker)); err == nil {
				return true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func isNonReplacingEdit(toolName string, rawArgs []byte, resolvedTarget string) bool {
	switch toolName {
	case "write_file", "write_to_file":
		var args struct {
			Append    bool `json:"Append"`
			Overwrite bool `json:"Overwrite"`
		}
		if len(rawArgs) > 0 {
			_ = json.Unmarshal(rawArgs, &args)
		}
		if args.Append && !args.Overwrite {
			return true
		}
		if args.Overwrite && !fileDoesNotExist(resolvedTarget) {
			return false
		}
		return fileDoesNotExist(resolvedTarget)
	case "edit_file", "replace_file_content":
		if len(rawArgs) == 0 {
			return false
		}
		var args struct {
			Target             string `json:"Target"`
			Replacement        string `json:"Replacement"`
			TargetContent      string `json:"TargetContent"`
			ReplacementContent string `json:"ReplacementContent"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return false
		}
		tgt := args.Target
		if tgt == "" {
			tgt = args.TargetContent
		}
		rep := args.Replacement
		if rep == "" && args.ReplacementContent != "" {
			rep = args.ReplacementContent
		}
		return isNonReplacingEditHunk(tgt, rep)
	case "multi_replace_file_content":
		if len(rawArgs) == 0 {
			return false
		}
		var args struct {
			ReplacementChunks []struct {
				TargetContent      string `json:"TargetContent"`
				ReplacementContent string `json:"ReplacementContent"`
			} `json:"ReplacementChunks"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || len(args.ReplacementChunks) == 0 {
			return false
		}
		for _, ch := range args.ReplacementChunks {
			if !isNonReplacingEditHunk(ch.TargetContent, ch.ReplacementContent) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isNonReplacingEditHunk(targetText, replacementText string) bool {
	if targetText == "" {
		return false
	}
	return strings.Contains(replacementText, targetText)
}

// Package lsserver — coverage.go: the answers the language server
// gives for the methods the clone does not implement.
//
// The reference answers all 351 methods of the service. The clone
// answered 75 of them and told the workbench "Method not found" for
// the other 244 — every one of those is an error in the IDE's console.
//
// 【一手】 2026-10-27, measured against the reference binary in its
// mocked standalone mode and against the clone, in the dialect the
// IDE's own workbench speaks (gRPC-Web): out/langserver/
// coverage-351.json.
//
//	reference: 153 answers · 186 refusals · 7 empty streams · 5 hang-ups
//	clone:     312 answers · 33 own failures · 6 empty streams · 0 gaps
//
// The fallback is the *default instance* of the method's response
// message (lsproto): the schema knows what every answer
// looks like, and the default instance is what a method with nothing
// to report gives — 111 of the reference's own 153 answers are exactly
// that.
//
// The methods whose answer carries content are answered from the
// clone's own state instead: the skills catalogue, the workspace
// directories, the static experiments.
package lsserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/lsproto"
)

// notFoundCode is the JSON-RPC code a dispatcher answers a method it
// does not implement with.
const notFoundCode = -32601

// jsonRPCCoder is the duck-typed interface a coded failure carries.
//
// The dispatcher package owns the type; the transport learns the code
// through this interface (the import direction is the other way
// round), so the language server reads it the same way.
type jsonRPCCoder interface {
	JSONRPCCode() int
}

// isMethodNotFound reports whether a failure is the dispatcher's
// "method not found" — the failure the fallback replaces.
func isMethodNotFound(err error) bool {
	coder, ok := err.(jsonRPCCoder)
	return ok && coder.JSONRPCCode() == notFoundCode
}

// fallbackAnswer answers a method the dispatcher does not implement.
//
// The answer is the default instance of the method's response message.
// A method the descriptor set does not know has no default: the
// failure is kept.
func fallbackAnswer(method string) (string, bool) {
	codec, err := lsproto.Shared()
	if err != nil {
		return "", false
	}
	out, ok := codec.DefaultAnswer(method)
	if !ok {
		return "", false
	}
	return string(out), true
}

// skillsAnswer answers GetAllSkills with the clone's own skills.
//
// The reference answers the workbench's skills panel with the skills
// it discovered: the builtin bundle it unpacked into the app data
// dir, the user's own skills, and the plugins' skills. The clone has
// the same two roots — the builtin bundle (internal/builtinskills)
// and the config home — and it lists them the way the prompt does
// (internal/agyprompt).
//
// 【一手】 2026-10-27: the workbench calls GetAllSkills on every
// load; the clone's answer was "Method not found", and the IDE
// logged "skills_fetch_failed" every time.
func (d *Dispatcher) skillsAnswer() string {
	skills := d.skillList()
	if len(skills) == 0 {
		return `{}`
	}
	entries := make([]string, 0, len(skills))
	for _, skill := range skills {
		entries = append(entries, skillJSON(skill))
	}
	return `{"skills":[` + strings.Join(entries, ",") + `]}`
}

// skillJSON renders one skill as the reference's WorkflowSpec.
//
// The key order is the contract, so the answer is written out by hand
// rather than marshalled from a map.
func skillJSON(skill map[string]any) string {
	var b strings.Builder
	b.WriteString("{")
	writeJSONString(&b, "name", skill["name"].(string))
	b.WriteString(",")
	writeJSONString(&b, "description", skill["description"].(string))
	b.WriteString(",")
	writeJSONString(&b, "path", skill["path"].(string))
	if content, ok := skill["content"].(string); ok {
		b.WriteString(",")
		writeJSONString(&b, "content", content)
	}
	if base, ok := skill["baseDir"].(string); ok {
		b.WriteString(",")
		writeJSONString(&b, "baseDir", base)
	}
	if builtin, ok := skill["isBuiltin"].(bool); ok && builtin {
		b.WriteString(`,"isBuiltin":true`)
	}
	if category, ok := skill["discoveryCategory"].(string); ok {
		b.WriteString(",")
		writeJSONString(&b, "discoveryCategory", category)
	}
	b.WriteString("}")
	return b.String()
}

// writeJSONString writes one JSON string member.
func writeJSONString(b *strings.Builder, key, value string) {
	out, err := json.Marshal(value)
	if err != nil {
		out = []byte(`""`)
	}
	b.WriteString(`"` + key + `":`)
	b.Write(out)
}

// skillList is the clone's skill catalogue.
func (d *Dispatcher) skillList() []map[string]any {
	roots := d.skillRoots()
	found := agyprompt.DiscoverSkills(roots)
	out := make([]map[string]any, 0, len(found))
	for _, skill := range found {
		entry := map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
			"path":        skill.Path,
		}
		if body, err := os.ReadFile(skill.Path); err == nil {
			entry["content"] = string(body)
		}
		if dir := filepath.Dir(skill.Path); dir != "" {
			entry["baseDir"] = dir
		}
		if builtin := strings.HasSuffix(filepath.Dir(skill.Path), filepath.Join("builtin", "skills")); builtin {
			entry["isBuiltin"] = true
			entry["discoveryCategory"] = "DISCOVERY_CATEGORY_BUILTIN"
		} else {
			entry["discoveryCategory"] = "DISCOVERY_CATEGORY_CONFIG"
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out
}

// skillRoots are the directories the clone reads skills from.
//
// The builtin bundle lives under the app data dir (the reference
// unpacks it there), the user's own skills under the config home.
func (d *Dispatcher) skillRoots() []string {
	var roots []string
	if d.ctx.Layout.AppDataDir != "" {
		roots = append(roots, filepath.Join(d.ctx.Layout.AppDataDir, "builtin", "skills"))
	}
	if d.ctx.Layout.GeminiDir != "" {
		roots = append(roots, filepath.Join(d.ctx.Layout.GeminiDir, "skills"))
	}
	return roots
}

// workspaceInfosAnswer answers GetWorkspaceInfos with the clone's own
// directories.
//
// 【一手】 2026-10-27: the reference answers the method with the home
// directory, its URI and the gemini directory's URI — the three
// roots the IDE's file panel opens.
func (d *Dispatcher) workspaceInfosAnswer() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	if home == "" {
		return `{}`
	}
	var b strings.Builder
	b.WriteString("{")
	writeJSONString(&b, "homeDirPath", home)
	b.WriteString(",")
	writeJSONString(&b, "homeDirUri", pathURI(home))
	if d.ctx.Layout.GeminiDir != "" {
		b.WriteString(",")
		writeJSONString(&b, "geminiDirUri", pathURI(d.ctx.Layout.GeminiDir))
	}
	b.WriteString("}")
	return b.String()
}

// pathURI renders a filesystem path as a file:// URI.
func pathURI(path string) string {
	return "file://" + strings.TrimPrefix(filepath.ToSlash(path), "/")
}

// staticExperimentsAnswer answers GetStaticExperimentStatus.
//
// The reference lists the experiments it knows; the clone runs on the
// user's own configuration and has no experiment server of its own,
// so the list is empty — an empty list and a missing field say the
// same thing to the workbench.
func (d *Dispatcher) staticExperimentsAnswer() string {
	return `{"status":[]}`
}

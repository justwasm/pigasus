// agytemplates.go — the reference's own template bundle, embedded once.
//
// The Antigravity CLI carries its prompt and step-string templates as an
// embedded file system (google3/third_party/jetski/prompt/template_provider/
// templates/). The clone recovered seventeen of those files out of the agy
// 1.4.7 binary — see templates/PROVENANCE.md for the extraction recipe and
// the per-file digests — and now renders from them instead of carrying
// hand-written copies of the strings.
//
// The bundle has two consumers:
//
//   - internal/agyprompt renders the system-prompt sections from
//     system_prompts/*.tmpl.
//   - internal/agystep renders the step strings from step_strings/*.tmpl.
//
// Both need the whole bundle in one template set, because the files share
// definitions: helpers/errors.tmpl defines "templateError" and
// step_strings/helpers/default_status_output_string.tmpl defines
// "defaultStatusOutputString", and the templates call both by name. One
// embed, one parser, two consumers.
package agytemplates

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates
var FS embed.FS

// NewSet parses the whole recovered bundle into one template set.
//
// funcs are the reference's engine functions, overlaid on top of the four
// path functions the bundle itself needs (see engineFuncs). Four of the
// prompt templates read a bare identifier — "{{ArtifactDirectoryPath}}",
// "{{ SystemGeneratedLogsPath }}", "{{KnowledgeDirectoryPath}}",
// "{{MetadataFileName}}" — with no leading dot, which in a Go template is a
// *function call*, not a field. The reference's engine registers them as
// functions; so does this one. They are bound to the data of the render,
// which is why the set is built per render rather than once.
//
// A nil funcs is allowed: the step-string templates read no bare
// identifier, and the path functions then answer with the empty string.
func NewSet(funcs template.FuncMap) *template.Template {
	root := template.New("agy").Funcs(engineFuncs)
	if len(funcs) > 0 {
		root = root.Funcs(funcs)
	}
	err := fs.WalkDir(FS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".tmpl") {
			return nil
		}
		b, err := FS.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = root.New(strings.TrimPrefix(p, "templates/")).Parse(string(b))
		return err
	})
	if err != nil {
		panic(fmt.Sprintf("agytemplates: embedded templates: %v", err))
	}
	return root
}

// engineFuncs are the four path functions the bundle's templates call. They
// answer with the empty string until a caller binds them to a render's
// data — internal/agyprompt does that, because the reference prints its
// artifact and logs directories as placeholders.
var engineFuncs = template.FuncMap{
	"ArtifactDirectoryPath":   func() string { return "" },
	"SystemGeneratedLogsPath": func() string { return "" },
	"KnowledgeDirectoryPath":  func() string { return "" },
	"MetadataFileName":        func() string { return "" },
}

// Paths lists the bundle's template files, relative to templates/ and
// sorted. The step strings are the ones under step_strings/.
func Paths() []string {
	var out []string
	err := fs.WalkDir(FS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".tmpl") {
			return nil
		}
		out = append(out, strings.TrimPrefix(p, "templates/"))
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("agytemplates: embedded templates: %v", err))
	}
	sort.Strings(out)
	return out
}

// Read returns one bundle file's bytes, relative to templates/.
func Read(rel string) ([]byte, error) {
	return FS.ReadFile("templates/" + rel)
}

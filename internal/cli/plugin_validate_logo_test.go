package cli

// plugin_validate_logo_test.go — the AGY 1.4.9 validator's name and logo
// checks.
//
// 【一手】 every expectation below was read off the 1.4.9 binary's own
// stdout (a crafted fixture per branch) and then diffed against the clone
// with the same fixture — see
// docs/cycles/cycle-362-agy-1.4.9-alignment/deliverable.md.

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginValidate_NameKebabCase pins the 1.4.9 name check: a name that is
// not lowercase kebab-case is warned about, with a suggestion, and the
// warning says what the name cannot do.
func TestPluginValidate_NameKebabCase(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name:     "uppercase",
			manifest: `{"name":"Neo4j"}`,
			want:     `⚠ plugin.json: name "Neo4j" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "neo4j"`,
		},
		{
			name:     "underscore",
			manifest: `{"name":"foo_bar"}`,
			want:     `⚠ plugin.json: name "foo_bar" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "foo-bar"`,
		},
		{
			name:     "dot",
			manifest: `{"name":"foo.bar"}`,
			want:     `⚠ plugin.json: name "foo.bar" isn't strict kebab-case (lowercase letters, digits and single hyphens), so ` + "`plugin install` refuses it and it can't be published to the Antigravity Marketplace; try \"foo-bar\"",
		},
		{
			name:     "double hyphen",
			manifest: `{"name":"a--b--c"}`,
			want:     `⚠ plugin.json: name "a--b--c" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "a-b-c"`,
		},
		{
			name:     "trailing hyphen",
			manifest: `{"name":"foo-"}`,
			want:     `⚠ plugin.json: name "foo-" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "foo"`,
		},
		{
			name:     "no suggestion left",
			manifest: `{"name":"--"}`,
			want:     `⚠ plugin.json: name "--" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newValidatePlugin(t, tc.manifest)
			out, _, _ := runValidateRef(t, root)
			if !strings.Contains(out, tc.want) {
				t.Errorf("missing %q in:\n%s", tc.want, out)
			}
		})
	}
}

// TestPluginValidate_NameKebabCaseAccepts pins the names the reference does
// not warn about.
func TestPluginValidate_NameKebabCaseAccepts(t *testing.T) {
	for _, name := range []string{"ok-plugin", "a", "123", "a1-b2"} {
		root := newValidatePlugin(t, `{"name":"`+name+`"}`)
		out, _, _ := runValidateRef(t, root)
		if strings.Contains(out, "kebab-case") {
			t.Errorf("name %q was warned about:\n%s", name, out)
		}
	}
}

// TestPluginValidate_NameUnderAgentPluginsSpec pins the spec's own wording:
// the grammar is the spec's, and the spec's clause joins the warning.
func TestPluginValidate_NameUnderAgentPluginsSpec(t *testing.T) {
	root := newValidatePlugin(t, `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"foo bar"}`)
	out, _, _ := runValidateRef(t, root)
	want := `⚠ plugin.json: name "foo bar" isn't strict kebab-case (at most 64 lowercase letters, digits and single hyphens), so ` +
		"`plugin install` refuses it, it isn't valid under the Agent Plugins spec and it can't be published to the Antigravity Marketplace; try \"foo-bar\""
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}
}

// TestPluginValidate_NameUnderSpecKeepsTheSpecMessage pins that a name that
// is kebab-case but breaks the spec's grammar keeps the spec's own message.
func TestPluginValidate_NameUnderSpecKeepsTheSpecMessage(t *testing.T) {
	long := strings.Repeat("a", 70)
	root := newValidatePlugin(t, `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"`+long+`"}`)
	out, _, _ := runValidateRef(t, root)
	want := `⚠ plugin.json: name "` + long + `" isn't valid under the Agent Plugins spec: use 1-64 lowercase letters, digits, . and -, starting and ending with a letter or digit, with no ".." or "--"`
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}
	if strings.Contains(out, "kebab-case") {
		t.Errorf("a kebab-case name must not get the kebab warning:\n%s", out)
	}
}

// TestPluginValidate_LogoFindings pins the 1.4.9 logo checks. Every case is
// one crafted fixture, and every message is the reference's own.
func TestPluginValidate_LogoFindings(t *testing.T) {
	cases := []struct {
		name  string
		logo  string
		files []string
		want  string
	}{
		{
			name: "missing",
			want: `· plugin.json: no logo: the plugin shows a generic icon. Add "logo": "assets/logo.svg" (a .png, .svg, .jpg, .jpeg or .webp file inside the plugin)`,
		},
		{
			name: "blank",
			logo: ``,
			want: `⚠ plugin.json: "logo" is blank: the Marketplace build rejects it. Remove the field or set it to an image inside the plugin, like "assets/logo.svg"`,
		},
		{
			name: "backslashes",
			logo: `assets\logo.svg`,
			want: `⚠ plugin.json: logo "assets\\logo.svg" uses backslashes, which only work on Windows, and the Marketplace build rejects it: use forward slashes, like "assets/logo.svg"`,
		},
		{
			name: "absolute",
			logo: `/tmp/logo.png`,
			want: `⚠ plugin.json: logo "/tmp/logo.png" is an absolute path, which the client ignores and the Marketplace build rejects: use a path relative to the plugin root, like "assets/logo.svg"`,
		},
		{
			name: "outside",
			logo: `../outside.png`,
			want: `⚠ plugin.json: logo "../outside.png" is outside the plugin root, so the Marketplace bundle can't include it and the Marketplace build rejects it. Move the image into the plugin and use a path like "assets/logo.svg"`,
		},
		{
			name: "not packaged",
			logo: `.github/logo.png`,
			want: `⚠ plugin.json: logo ".github/logo.png" won't be packaged in the Marketplace bundle, so the Marketplace build fails: the bundle leaves out paths with a part starting with "." (like .github/ or .logo.png) or named __pycache__. Move it, for example to "assets/"`,
		},
		{
			name: "url on another domain",
			logo: `https://example.com/logo.png`,
			want: `⚠ plugin.json: logo "https://example.com/logo.png" isn't shown: the client only shows https images on google.com, gstatic.com, googleusercontent.com or googleplex.com. The Marketplace build accepts it, but the plugin gets a generic icon. Put the image in the plugin and use a relative path, like "assets/logo.svg"`,
		},
		{
			name: "url that is not https",
			logo: `http://example.com/logo.png`,
			want: `⚠ plugin.json: logo "http://example.com/logo.png" is ignored and the Marketplace build rejects it: use an https URL on a Google domain, or a relative path to an image inside the plugin`,
		},
		{
			name: "url that does not parse",
			logo: `https://exa mple.com/logo.png`,
			want: `⚠ plugin.json: logo "https://exa mple.com/logo.png" isn't shown: the client can't parse it as a URL. The Marketplace build accepts it, but the plugin gets a generic icon. Fix the URL, or put the image in the plugin and use a relative path, like "assets/logo.svg"`,
		},
		{
			name: "not an image path",
			logo: `assets/logo.svg/`,
			want: `⚠ plugin.json: logo "assets/logo.svg/" isn't shown: the client can't read it as an image path. The Marketplace build accepts it, but the plugin gets a generic icon. Use a plain path, like "assets/logo.svg"`,
		},
		{
			name:  "wrong extension",
			logo:  `assets/logo.txt`,
			files: []string{"assets/logo.txt"},
			want:  `⚠ plugin.json: logo "assets/logo.txt" is ignored and the Marketplace build rejects it: use a .png, .svg, .jpg, .jpeg or .webp file inside the plugin, or an https URL on a Google domain`,
		},
		{
			name: "missing file",
			logo: `assets/missing.png`,
			want: `⚠ plugin.json: logo file "assets/missing.png" doesn't exist, so the plugin shows a generic icon and the Marketplace build fails`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := `{"name":"ok-plugin"}`
			if tc.name != "missing" {
				manifest = `{"name":"ok-plugin","logo":` + quoteJSON(tc.logo) + `}`
			}
			root := newValidatePlugin(t, manifest)
			for _, f := range tc.files {
				writeValidateFile(t, filepath.Join(root, f), "x")
			}
			out, _, _ := runValidateRef(t, root)
			if !strings.Contains(out, tc.want) {
				t.Errorf("missing %q in:\n%s", tc.want, out)
			}
		})
	}
}

// TestPluginValidate_LogoAccepted pins the logos the reference accepts.
func TestPluginValidate_LogoAccepted(t *testing.T) {
	for _, logo := range []string{
		"assets/logo.svg",
		"assets/logo.png",
		"https://google.com/logo.png",
		"https://www.google.com/logo.png",
		"https://foo.gstatic.com/logo.png",
		"https://googleplex.com/logo.png",
	} {
		root := newValidatePlugin(t, `{"name":"ok-plugin","logo":"`+logo+`"}`)
		writeValidateFile(t, filepath.Join(root, "assets", "logo.svg"), "x")
		writeValidateFile(t, filepath.Join(root, "assets", "logo.png"), "x")
		out, _, _ := runValidateRef(t, root)
		if strings.Contains(out, "logo") {
			t.Errorf("logo %q was warned about:\n%s", logo, out)
		}
	}
}

// TestPluginValidate_LogoTypeError pins the reference's own error for a logo
// that is not a string.
func TestPluginValidate_LogoTypeError(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"ok-plugin","logo":5}`)
	out, _, code := runValidateRef(t, root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	want := `✗ plugin.json: (line 1:28): invalid value for string field logo: 5. The loader skips this plugin`
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}
}

// TestPluginValidate_Extensions pins the 1.4.9 check on the manifest's
// "extensions" object.
func TestPluginValidate_Extensions(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"ok-plugin","extensions":[1]}`)
	out, _, _ := runValidateRef(t, root)
	want := `⚠ plugin.json: "extensions" isn't an object, so no context providers load from it`
	if !strings.Contains(out, want) {
		t.Errorf("missing %q in:\n%s", want, out)
	}

	root = newValidatePlugin(t, `{"name":"ok-plugin","extensions":{"contextProviders":{"a":{"iconUri":"assets/logo.svg"}}}}`)
	out, _, _ = runValidateRef(t, root)
	if strings.Contains(out, "extensions") {
		t.Errorf("an object \"extensions\" must not be warned about:\n%s", out)
	}
}

// quoteJSON renders a Go string as a JSON string literal.
func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

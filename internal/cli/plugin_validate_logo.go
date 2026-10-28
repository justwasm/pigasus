package cli

// plugin_validate_logo.go — the AGY 1.4.9 validator's image checks.
//
// 【一手】 agy 1.4.9, the vendor's own release notes:
//
//	Improved `agy plugin validate` to warn when a plugin's `name` isn't
//	lowercase kebab-case, such as `Neo4j` or `foo_bar`, which the
//	marketplace can't publish, and to suggest a valid name, failing under
//	`--strict`; it now also checks the plugin's `logo`, noting when
//	plugin.json has none and warning when the logo path is blank, uses
//	backslashes, is absolute or points outside the plugin, sits in a folder
//	the marketplace bundle leaves out such as .github/, or is a URL that
//	won't be shown, with the same checks for context provider `iconUri`
//	values.
//
// Every message below was read off the 1.4.9 binary's own stdout, one
// crafted fixture per branch (docs/cycles/cycle-362-agy-1.4.9-alignment/
// deliverable.md lists the fixtures). The reference's own message strings
// live in the binary as format templates:
//
//	%s %q isn't shown: the client can't parse it as a URL%s. Fix the URL, or
//	    put the image in the plugin and use a relative path, like "assets/logo.svg"
//	%s %q is outside the plugin root, so the Marketplace bundle can't include
//	    it and the Marketplace build rejects it. Move the image into the plugin
//	    and use a path like "assets/logo.svg"
//	%s %q won't be packaged in the Marketplace bundle%s: the bundle leaves out
//	    paths with a part starting with "." (like .github/ or .logo.png) or named
//	    __pycache__. Move it, for example to "assets/"
//	%s %q isn't shown: the client only shows https images on google.com,
//	    gstatic.com, googleusercontent.com or googleplex.com%s. Put the image
//	    in the plugin and use a relative path, like "assets/logo.svg"
//
// The leading `%s` is the field's own name ("logo", "iconUri"), which is why
// the same checks serve both the manifest's logo and a context provider's
// iconUri.
import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// pluginImageExtensions are the extensions the Marketplace build accepts.
var pluginImageExtensions = map[string]bool{
	".png":  true,
	".svg":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
}

// googleImageDomains are the hosts the client is willing to show an image
// from. A host matches when it is the domain itself or a subdomain of it.
var googleImageDomains = []string{
	"google.com",
	"gstatic.com",
	"googleusercontent.com",
	"googleplex.com",
}

// validatePluginImageRef checks one image reference the way the reference's
// validator does, and returns the finding it produces.
//
// field is the JSON field's name ("logo", "iconUri"); it is what the messages
// quote. dir is the plugin root the reference is resolved against.
//
// The returned bool is false when the reference produces no finding — a
// relative path to an existing image, or an https URL on a Google domain.
func validatePluginImageRef(dir, field, ref string) (validateSeverity, string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return validateWarning, fmt.Sprintf(
			"%q is blank: the Marketplace build rejects it. Remove the field or set it to an image inside the plugin, like %q",
			field, "assets/logo.svg"), true
	}
	if sev, msg, handled := validatePluginImageURL(field, ref); handled {
		if sev == "" {
			return "", "", false
		}
		return sev, msg, true
	}
	return validatePluginImagePath(dir, field, ref)
}

// validatePluginImageURL handles the URL half of the check.
//
// The reference enters this branch for anything that parses as a URL with a
// scheme ("https://…", but also "mailto:…" and "C:\logo.png"), and for
// anything that looks like a URL but does not parse ("https://exa mple.com/…"
// — a space in the host).
//
// The third return value says whether the reference took the URL branch at
// all; an empty severity then means the reference is happy with the URL.
func validatePluginImageURL(field, ref string) (validateSeverity, string, bool) {
	u, err := url.Parse(ref)
	if err == nil && u.Scheme != "" {
		if !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
			return validateWarning, fmt.Sprintf(
				"%s %q is ignored and the Marketplace build rejects it: use an https URL on a Google domain, or a relative path to an image inside the plugin",
				field, ref), true
		}
		if !isGoogleImageHost(u.Hostname()) {
			return validateWarning, fmt.Sprintf(
				"%s %q isn't shown: the client only shows https images on google.com, gstatic.com, googleusercontent.com or googleplex.com. The Marketplace build accepts it, but the plugin gets a generic icon. Put the image in the plugin and use a relative path, like %q",
				field, ref, "assets/logo.svg"), true
		}
		return "", "", true
	}
	if err != nil && looksLikeURL(ref) {
		return validateWarning, fmt.Sprintf(
			"%s %q isn't shown: the client can't parse it as a URL. The Marketplace build accepts it, but the plugin gets a generic icon. Fix the URL, or put the image in the plugin and use a relative path, like %q",
			field, ref, "assets/logo.svg"), true
	}
	return "", "", false
}

// looksLikeURL reports whether ref starts with a scheme and "://" — the shape
// that is meant to be a URL even when it does not parse.
func looksLikeURL(ref string) bool {
	scheme := ref
	if i := strings.Index(scheme, "://"); i >= 0 {
		scheme = scheme[:i]
	} else {
		return false
	}
	if scheme == "" {
		return false
	}
	for i, r := range scheme {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9', r == '+', r == '-', r == '.':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isGoogleImageHost reports whether the client would show an image from host.
func isGoogleImageHost(host string) bool {
	host = strings.ToLower(host)
	for _, d := range googleImageDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// validatePluginImagePath handles the relative-path half of the check.
//
// The order of the branches is the reference's own: a Windows path, then an
// absolute one, then one that escapes the plugin root, then a path whose
// last element has no image extension, then one the Marketplace bundle
// leaves out, then one that is not a plain image path, then one whose file
// is missing.
func validatePluginImagePath(dir, field, ref string) (validateSeverity, string, bool) {
	if strings.Contains(ref, "\\") {
		return validateWarning, fmt.Sprintf(
			"%s %q uses backslashes, which only work on Windows, and the Marketplace build rejects it: use forward slashes, like %q",
			field, ref, "assets/logo.svg"), true
	}
	if strings.HasPrefix(ref, "/") {
		return validateWarning, fmt.Sprintf(
			"%s %q is an absolute path, which the client ignores and the Marketplace build rejects: use a path relative to the plugin root, like %q",
			field, ref, "assets/logo.svg"), true
	}
	cleaned := path.Clean(ref)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return validateWarning, fmt.Sprintf(
			"%s %q is outside the plugin root, so the Marketplace bundle can't include it and the Marketplace build rejects it. Move the image into the plugin and use a path like %q",
			field, ref, "assets/logo.svg"), true
	}
	if !pluginImageExtensions[strings.ToLower(path.Ext(path.Base(cleaned)))] {
		return validateWarning, fmt.Sprintf(
			"%s %q is ignored and the Marketplace build rejects it: use a .png, .svg, .jpg, .jpeg or .webp file inside the plugin, or an https URL on a Google domain",
			field, ref), true
	}
	for _, part := range strings.Split(cleaned, "/") {
		if strings.HasPrefix(part, ".") || part == "__pycache__" {
			return validateWarning, fmt.Sprintf(
				"%s %q won't be packaged in the Marketplace bundle, so the Marketplace build fails: the bundle leaves out paths with a part starting with %q (like .github/ or .logo.png) or named __pycache__. Move it, for example to %q",
				field, ref, ".", "assets/"), true
		}
	}
	if strings.HasSuffix(ref, "/") {
		return validateWarning, fmt.Sprintf(
			"%s %q isn't shown: the client can't read it as an image path. The Marketplace build accepts it, but the plugin gets a generic icon. Use a plain path, like %q",
			field, ref, "assets/logo.svg"), true
	}
	info, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(ref)))
	if statErr != nil || info.IsDir() {
		return validateWarning, fmt.Sprintf(
			"%s file %q doesn't exist, so the plugin shows a generic icon and the Marketplace build fails",
			field, ref), true
	}
	return "", "", false
}

// validatePluginLogo checks the manifest's "logo" field.
//
// A manifest with no logo at all gets the reference's note; every other
// shape is handed to the shared image checks.
func validatePluginLogo(dir, manifestRel, logo string, present bool, rep *validateReport) {
	file := filepath.ToSlash(manifestRel)
	if !present {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateInfo,
			File:     file,
			Message:  `no logo: the plugin shows a generic icon. Add "logo": "assets/logo.svg" (a .png, .svg, .jpg, .jpeg or .webp file inside the plugin)`,
		})
		return
	}
	if sev, msg, ok := validatePluginImageRef(dir, "logo", logo); ok {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: sev,
			File:     file,
			Message:  msg,
		})
	}
}

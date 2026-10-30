// Package endpoints — endpoints.go
//
// The one place that says which service is which URL, and the one place a
// user's configuration can change the answer.
//
// # Why
//
// The reference binary has Google's services compiled in — that is what
// "agy" is. A clone whose point is to be able to point at a local service
// instead needs that URL to be a *value*, not a literal. This package
// holds the compiled-in values and the resolution over them.
//
// # The configuration file
//
// ~/.config/openagy/config.json may override any endpoint by name:
//
//	{
//	  "endpoints": {
//	    "cloudcode": "http://127.0.0.1:8080",
//	    "oauth2_token": "http://127.0.0.1:8080/oauth/token"
//	  }
//	}
//
// A missing file is an empty configuration, not an error. A malformed file
// is not an error either: the compiled-in defaults are used and the clone
// starts. A broken edit to the file must not take the CLI down.
//
// # Precedence
//
// The house rule, unchanged: an explicit value the caller passes wins,
// then the environment variable the endpoint already had, then this
// configuration file, then the compiled-in default. The environment
// variables stay where they are — CLOUD_CODE_URL, OPENAGY_UPSTREAM,
// ANTIGRAVITY_RC_ORIGIN_PATTERNS — and they still win over the file: a
// variable set for one invocation is more specific than a file that is
// always there.
package endpoints

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
)

// The names an override may carry. They are the keys of the "endpoints"
// object in the configuration file.
const (
	// CloudCode is the Code Assist host. It is the transport the
	// clone's own turn runs on, the host loadCodeAssist and the quota
	// and tier calls go to, the local proxy's upstream, and the value
	// the spawned reference binary is given.
	CloudCode = "cloudcode"

	// CloudCodeProd is the production CloudCode host — the first of the
	// two the Mendel flag call tries.
	CloudCodeProd = "cloudcode_prod"

	// OAuth2Token is the OAuth2 token endpoint: the refresh-token grant
	// and the device flow's token exchange post to it.
	OAuth2Token = "oauth2_token"

	// OAuth2Auth is the OAuth2 consent page the browser sign-in opens.
	OAuth2Auth = "oauth2_auth"

	// OAuth2Device is the OAuth2 device-code endpoint.
	OAuth2Device = "oauth2_device"

	// OAuthRedirect is the redirect target the browser sign-in's
	// authorization code comes back to.
	OAuthRedirect = "oauth_redirect"

	// UserInfo is the endpoint the user's email is read from.
	UserInfo = "userinfo"

	// WebChannel is the cloud tunnel the remote-control view connects
	// to. It is a full URL: the RPC path is part of it.
	WebChannel = "webchannel"

	// UpdateInstance is the cloud RPC that renames a registered
	// instance. It lives on the tunnel's own host — only the RPC path
	// differs from the WebChannel endpoint — so an override rarely
	// needs to name it: the resolver derives it from the WebChannel
	// endpoint when the file does not carry one.
	UpdateInstance = "update_instance"

	// GenerativeLanguage is the Generative Language API host. The
	// clone passes it to the spawned reference binary as its
	// --api_server_url.
	GenerativeLanguage = "generativelanguage"

	// CompanionOrigin is the browser origin the companion web app lives
	// on: the URL the user is told to open, the origin the remote view
	// is served from, and the companionOrigin the Web UI is told about.
	CompanionOrigin = "companion_origin"

	// AccountChooser is Google's account chooser. The remote-control
	// deep link goes *through* it, so the browser asks which account to
	// use — naming the one the daemon registered with — and then
	// continues to the target page.
	AccountChooser = "account_chooser"

	// OriginPatterns is the browser Origin allowlist the remote-control
	// host serves. Its value is a comma-separated list, the same shape
	// $ANTIGRAVITY_RC_ORIGIN_PATTERNS uses.
	OriginPatterns = "origin_patterns"

	// Terms is the Terms of Service link the onboarding renders.
	Terms = "terms"

	// Privacy is the Privacy Policy link the onboarding renders.
	Privacy = "privacy"

	// Marketplace is the plugin marketplace's skill search endpoint.
	// Its value ends with "?", because the caller appends the
	// URL-escaped skill name.
	Marketplace = "marketplace"

	// AutoUpdater is the Cloud Run auto-updater service host.
	AutoUpdater = "auto_updater"
)

// The compiled-in defaults. They are what the clone used before the
// configuration file could say anything, and they stay exported so the
// code that documents a default can name it.
const (
	DefaultCloudCode          = "https://daily-cloudcode-pa.googleapis.com"
	DefaultCloudCodeProd      = "https://cloudcode-pa.googleapis.com"
	DefaultOAuth2Token        = "https://oauth2.googleapis.com/token"
	DefaultOAuth2Auth         = "https://accounts.google.com/o/oauth2/auth"
	DefaultOAuth2Device       = "https://oauth2.googleapis.com/device/code"
	DefaultOAuthRedirect      = "https://antigravity.google/oauth-callback"
	DefaultUserInfo           = "https://www.googleapis.com/oauth2/v3/userinfo"
	DefaultWebChannel         = "https://jetski-webchannel.googleapis.com/v1internal:connectInstanceV2"
	DefaultUpdateInstance     = "https://jetski-webchannel.googleapis.com/v1internal:updateInstance"
	DefaultGenerativeLanguage = "https://generativelanguage.googleapis.com"
	DefaultCompanionOrigin    = "https://antigravity.google.com"
	DefaultAccountChooser     = "https://accounts.google.com/AccountChooser"
	DefaultTerms              = "https://antigravity.google/terms"
	DefaultPrivacy            = "https://policies.google.com/privacy"
	DefaultMarketplace        = "https://marketplace.antigravity.com/skills?q="
	DefaultAutoUpdater        = "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app"
)

// DefaultOriginPatterns is the browser Origin allowlist the remote-control
// host serves when nothing overrides it.
//
// The two entries are the browser-side hosts the companion ships against.
// They are *not* the companion origin (which is antigravity.google.com —
// a different origin, and the one the user is told to open).
var DefaultOriginPatterns = []string{
	"https://antigravity.google",
	"https://antigravity-companion.google",
}

// Name describes one endpoint: the key the configuration file carries
// and the URL it falls back to.
type Name struct {
	// Key is the name an override carries.
	Key string
	// Default is the compiled-in URL.
	Default string
}

// Names lists every endpoint the configuration file may override.
//
// It exists so a front-end — a doctor line, a `pigasus endpoints`
// command — can print the whole table without repeating the list, and
// so the table and the resolver cannot drift apart.
func Names() []Name {
	return []Name{
		{CloudCode, DefaultCloudCode},
		{CloudCodeProd, DefaultCloudCodeProd},
		{OAuth2Token, DefaultOAuth2Token},
		{OAuth2Auth, DefaultOAuth2Auth},
		{OAuth2Device, DefaultOAuth2Device},
		{OAuthRedirect, DefaultOAuthRedirect},
		{UserInfo, DefaultUserInfo},
		{WebChannel, DefaultWebChannel},
		{UpdateInstance, DefaultUpdateInstance},
		{GenerativeLanguage, DefaultGenerativeLanguage},
		{CompanionOrigin, DefaultCompanionOrigin},
		{AccountChooser, DefaultAccountChooser},
		{OriginPatterns, strings.Join(DefaultOriginPatterns, ",")},
		{Terms, DefaultTerms},
		{Privacy, DefaultPrivacy},
		{Marketplace, DefaultMarketplace},
		{AutoUpdater, DefaultAutoUpdater},
	}
}

// Set is the resolved endpoint set: every service the clone dials, with
// the configuration's overrides applied.
//
// A Set is a snapshot. Read one with Current and use it for the whole of
// one operation, so a turn does not see the CloudCode host change halfway
// through.
type Set struct {
	// CloudCode is the Code Assist host.
	CloudCode string
	// CloudCodeProd is the production Code Assist host.
	CloudCodeProd string
	// OAuth2Token is the OAuth2 token endpoint.
	OAuth2Token string
	// OAuth2Auth is the OAuth2 consent page.
	OAuth2Auth string
	// OAuth2Device is the OAuth2 device-code endpoint.
	OAuth2Device string
	// OAuthRedirect is the browser sign-in's redirect target.
	OAuthRedirect string
	// UserInfo is the userinfo endpoint.
	UserInfo string
	// WebChannel is the remote-control cloud tunnel endpoint.
	WebChannel string
	// UpdateInstance is the endpoint the instance-metadata update
	// posts to. It is derived from the WebChannel endpoint unless the
	// configuration file names one; see UpdateInstanceURL.
	UpdateInstance string
	// GenerativeLanguage is the Generative Language API host.
	GenerativeLanguage string
	// CompanionOrigin is the companion web app's browser origin.
	CompanionOrigin string
	// AccountChooser is Google's account chooser.
	AccountChooser string
	// OriginPatterns is the browser Origin allowlist.
	OriginPatterns []string
	// Terms is the Terms of Service link.
	Terms string
	// Privacy is the Privacy Policy link.
	Privacy string
	// Marketplace is the plugin marketplace's skill search endpoint.
	Marketplace string
	// AutoUpdater is the auto-updater service host.
	AutoUpdater string
}

// Current returns the resolved endpoint set.
//
// The configuration file is read only when it changed on disk, so calling
// this on a hot path costs a stat and a pointer load.
func Current() Set {
	o := currentOverrides()
	return Set{
		CloudCode:          pick(o, CloudCode, DefaultCloudCode),
		CloudCodeProd:      pick(o, CloudCodeProd, DefaultCloudCodeProd),
		OAuth2Token:        pick(o, OAuth2Token, DefaultOAuth2Token),
		OAuth2Auth:         pick(o, OAuth2Auth, DefaultOAuth2Auth),
		OAuth2Device:       pick(o, OAuth2Device, DefaultOAuth2Device),
		OAuthRedirect:      pick(o, OAuthRedirect, DefaultOAuthRedirect),
		UserInfo:           pick(o, UserInfo, DefaultUserInfo),
		WebChannel:         pick(o, WebChannel, DefaultWebChannel),
		UpdateInstance:     pick(o, UpdateInstance, ""),
		GenerativeLanguage: pick(o, GenerativeLanguage, DefaultGenerativeLanguage),
		CompanionOrigin:    pick(o, CompanionOrigin, DefaultCompanionOrigin),
		AccountChooser:     pick(o, AccountChooser, DefaultAccountChooser),
		OriginPatterns:     pickList(o, OriginPatterns, DefaultOriginPatterns),
		Terms:              pick(o, Terms, DefaultTerms),
		Privacy:            pick(o, Privacy, DefaultPrivacy),
		Marketplace:        pick(o, Marketplace, DefaultMarketplace),
		AutoUpdater:        pick(o, AutoUpdater, DefaultAutoUpdater),
	}
}

// AccountChooserURL returns the URL that asks Google's account chooser
// to continue to target with the given account.
//
// The chooser's parameters are Google's own: "Email" names the account
// and "continue" carries the whole target URL, percent-encoded. The
// parameters come out in the order Google's own links carry them —
// "Email" first — because that is what url.Values.Encode does: it
// sorts the keys.
//
// An empty email yields the target itself: a chooser that names no
// account chooses nothing, and the link would only cost the user a
// click.
func (s Set) AccountChooserURL(email, target string) string {
	target = strings.TrimSpace(target)
	email = strings.TrimSpace(email)
	if email == "" {
		return target
	}
	chooser := strings.TrimRight(strings.TrimSpace(s.AccountChooser), "/")
	if chooser == "" {
		return target
	}
	q := url.Values{}
	q.Set("Email", email)
	q.Set("continue", target)
	return chooser + "?" + q.Encode()
}

// UpdateInstanceURL returns the URL the instance-metadata update posts
// to.
//
// The configuration file wins when it names the endpoint. Otherwise the
// URL is derived from the WebChannel endpoint: the reference posts to
// "{proxyURL}/v1internal:updateInstance", where proxyURL is the
// tunnel's own host, so the update lives on the same host as the
// tunnel and only the RPC path differs.
//
// Deriving it — rather than hard-coding the compiled-in default — is
// what keeps a local mock working: a tunnel pointed at 127.0.0.1
// answers the rename on the same port.
func (s Set) UpdateInstanceURL() string {
	if v := strings.TrimSpace(s.UpdateInstance); v != "" {
		return v
	}
	return UpdateInstanceURLFor(s.WebChannel)
}

// UpdateInstanceURLFor derives the instance-update URL for a tunnel
// endpoint.
//
// An endpoint that carries no host — an empty value, or one that does
// not parse — yields the compiled-in default.
func UpdateInstanceURLFor(webChannel string) string {
	raw := strings.TrimSpace(webChannel)
	if raw == "" {
		return DefaultUpdateInstance
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return DefaultUpdateInstance
	}
	scheme := u.Scheme
	if scheme == "" {
		// The reference's own rule: a proxy URL without a scheme is
		// read as https.
		scheme = "https"
	}
	return scheme + "://" + u.Host + "/v1internal:updateInstance"
}

// WebChannelHostPort returns the tunnel endpoint's host:port — the form
// the Mendel flag "remote-control-proxy-server-url" carries.
//
// The default endpoint's own host carries no port, so the scheme decides:
// https means 443, http means 80. An endpoint that names a port keeps it.
// An endpoint that does not parse yields the empty string, which is what
// the flag then carries.
func (s Set) WebChannelHostPort() string {
	u, err := url.Parse(strings.TrimSpace(s.WebChannel))
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}
	switch u.Scheme {
	case "https":
		return u.Hostname() + ":443"
	case "http":
		return u.Hostname() + ":80"
	default:
		return u.Host
	}
}

// CloudCodeHost returns the host of the CloudCode endpoint — the form the
// "Signed out from server: <host>" line carries.
//
// An endpoint that does not parse, or that carries no host, yields the
// endpoint itself: the line then names what the clone was configured
// with, which is more useful than an empty string.
func (s Set) CloudCodeHost() string {
	return hostOf(s.CloudCode)
}

// OAuth2AuthHost returns the host of the OAuth2 consent endpoint — the
// service the device-code flow announces before it starts.
//
// An endpoint that does not parse, or that carries no host, yields the
// endpoint itself.
func (s Set) OAuth2AuthHost() string {
	return hostOf(s.OAuth2Auth)
}

// AutoUpdaterManifestURL returns the full URL to the update manifest for
// the given goos and goarch. If the AutoUpdater endpoint already ends with
// ".json", it is returned directly as in the reference binary.
func (s Set) AutoUpdaterManifestURL(goos, goarch string) string {
	base := strings.TrimSpace(s.AutoUpdater)
	if base == "" {
		base = DefaultAutoUpdater
	}
	if strings.HasSuffix(base, ".json") {
		return base
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	base = strings.TrimRight(base, "/")
	return fmt.Sprintf("%s/manifests/%s_%s.json", base, goos, goarch)
}

// hostOf returns the host of a URL, or the URL itself when it carries
// none.
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

// pick returns the configuration's override for name, or the fallback.
func pick(overrides map[string]string, name, fallback string) string {
	if v := strings.TrimSpace(overrides[name]); v != "" {
		return v
	}
	return fallback
}

// pickList returns the configuration's override for name as a list, or the
// fallback.
//
// The override is comma-separated, the same shape
// $ANTIGRAVITY_RC_ORIGIN_PATTERNS uses. A value that carries no usable
// entry (only commas and whitespace) is treated as no override: an
// allowlist that is accidentally emptied would allow every origin, which
// is the one outcome the remote-control host must never produce by
// accident.
func pickList(overrides map[string]string, name string, fallback []string) []string {
	raw := strings.TrimSpace(overrides[name])
	if raw == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

// documented.go — the documented `mcpServers` entry: the shape the
// documentation describes and the shape a writer emits.
//
// The official documentation lists twelve members for one server entry
// (official-docs/mcp.md, "MCP configuration properties"):
//
//	command           string    the stdio executable
//	serverUrl         string    the remote endpoint ("url" also works)
//	args              string[]  the stdio arguments
//	env               object    the environment variables of the process
//	cwd               string    the working directory of the process
//	headers           object    the HTTP request headers
//	authProviderType  string    "google_credentials"
//	oauth             object    the manual OAuth 2.0 client credentials
//	disabled          boolean   suspends the server
//	enabledTools      string[]  the allowlist
//	disabledTools     string[]  the denylist
//	timeoutSeconds    integer   the connect and call timeout
//
// One owner
// ---------
//
// The member names live in this package and nowhere else. agyconfig.go's
// `ServerFile` carries them as its json tags — that is the *read* shape,
// a union of the documented schema and the reference binary's own
// `mcp.json` schema, which adds `type`, `url`, `enabled` and
// `alwaysLoad`. `DocumentedMembers` is the *write* shape: the documented
// members only.
//
// The two representations are pinned against each other by
// TestDocumentedMembersMatchTheReaderTags, so they cannot drift.
//
// The omit rules
// --------------
//
// A member the user did not set is absent from the document — never
// null, never an empty object, never an empty array. `disabled` is the
// one member that is always written: the reference always writes its own
// `enabled` flag, and the documented `disabled` flag is its inverse, so
// an enabled server carries `disabled: false`.
//
// The evidence
// ------------
//
// 【一手】 official-docs/mcp.md:141-160 states the member names, the
// types and the defaults.
//
// 【一手】 the reference binary's own protocol buffer descriptor names the
// same members and their types — `out/local-macos/protos/cortex_1.4.6.txt`
// (`message McpServerSpec`, the message that begins at the line
// `name: "McpServerSpec"`): `cwd` (TYPE_STRING), `auth_provider_type`
// (TYPE_ENUM, json_name "authProviderType"), `oauth` (TYPE_MESSAGE,
// json_name "oauth"), `enabled_tools` (LABEL_REPEATED, json_name
// "enabledTools"), `disabled_tools` (LABEL_REPEATED, json_name
// "disabledTools") and `timeout_seconds` (TYPE_INT32, json_name
// "timeoutSeconds"). `env` and `headers` are map entries there, so they
// are objects on the wire and never arrays.
//
// 【反编译】 the recovered function names of the reference's own MCP
// configuration package — `out/local-macos/seeds/seeds.txt` — carry the
// same spelling: `cortex_go_proto.(*McpServerSpec).GetCwd`,
// `.GetAuthProviderType`, `.GetOauth`, `.GetEnabledTools`,
// `.GetDisabledTools` and `.GetTimeoutSeconds`.
//
// The enum the authentication provider names is
// `McpAuthProviderType`, whose only value is
// `MCP_AUTH_PROVIDER_TYPE_GOOGLE_CREDENTIALS` (the same descriptor
// file); the configuration file spells it `google_credentials`, which is
// what the documentation states.
//
// What a writer does with an entry the file already holds is
// document.go's, not this file's: the reference *replaces* the entry a
// command names, so the members the `agy mcp add` flags cannot express
// are dropped by a re-add. See document.go.
package mcp

// DocumentedMembers returns the entry as the JSON object a documented
// configuration file carries: the documented members the entry holds,
// and no others.
//
// The reference binary's own members — `type`, `url`, `enabled` and
// `alwaysLoad` — belong to its `mcp.json` and are not part of the
// documented `mcpServers` schema, so they are never written into it.
//
// `Enabled` is the reference's own member and is not written either: the
// documented member that says the same thing is `disabled`, and it is
// written from `Disabled`.
func (s ServerFile) DocumentedMembers() map[string]any {
	out := map[string]any{}
	if s.Command != "" {
		out["command"] = s.Command
	}
	if len(s.Args) > 0 {
		out["args"] = s.Args
	}
	if len(s.Env) > 0 {
		out["env"] = s.Env
	}
	if s.CWD != "" {
		out["cwd"] = s.CWD
	}
	if s.ServerURL != "" {
		out["serverUrl"] = s.ServerURL
	}
	if len(s.Headers) > 0 {
		out["headers"] = s.Headers
	}
	if s.AuthProviderType != "" {
		out["authProviderType"] = s.AuthProviderType
	}
	if len(s.OAuth) > 0 {
		out["oauth"] = s.OAuth
	}
	// The disabled flag is always written: it is the documented member
	// that says the server is suspended, and the reference always
	// writes the flag that says the same thing.
	out["disabled"] = s.Disabled
	if len(s.EnabledTools) > 0 {
		out["enabledTools"] = s.EnabledTools
	}
	if len(s.DisabledTools) > 0 {
		out["disabledTools"] = s.DisabledTools
	}
	if s.TimeoutSeconds != 0 {
		out["timeoutSeconds"] = s.TimeoutSeconds
	}
	return out
}

// SetDisabled returns the entry with the documented `disabled` flag set.
//
// Clearing the flag removes the member: an absent `disabled` flag is the
// documented default (false), so an enabled server carries no flag at
// all.
//
// The entry is the JSON object a configuration file holds, so the member
// name is spelled here — in the package that owns the schema — and not
// at the call site.
func SetDisabled(entry map[string]any, disabled bool) map[string]any {
	if disabled {
		entry["disabled"] = true
	} else {
		delete(entry, "disabled")
	}
	return entry
}

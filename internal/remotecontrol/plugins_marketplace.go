// Package remotecontrol — plugins_marketplace.go: the marketplace
// half of the language server's plugin surface.
//
// # What this file is
//
// The Web UI's plugin panel has two halves: the *installed* plugins
// (plugins_ls.go) and the *marketplace* — the catalogues the user
// installs from.
//
// The catalogues are on disk: the reference's own cache holds one
// marketplace.json per marketplace.
//
//	~/.gemini/antigravity-cli/cache/marketplaces/<mkt>/marketplace.json
//
// 【一手】 2026-10-30: read off this machine's cache — two
// marketplaces, 17 and 50 entries, each entry carrying name,
// displayName, version, description, logo, suggestedPrompts, author,
// homepage, repository, license, keywords and a source.
//
// # The methods
//
// SearchMarketplace — the catalogue search the panel's search box
// drives. The answer carries the matching entries as
// MarketplaceCustomizations, the marketplaces as ResultGroups, and
// the paging token.
//
// SearchMarketplaceCustomizations — the search over *extensions*.
//
// GetMarketplaceCustomization — one entry, with its plugin and its
// skill preview.
//
// GetMarketplaceBundles / GetMarketplaceFeatured — the bundles and
// the featured cards.
//
// GetSkillMarketplaceLink — the link the "publish a skill" flow
// needs.
//
// # What this file does not do
//
// A *bundle* and a *featured card* are the cloud's own curation. The
// local catalogue carries no bundle and no featured list, so those
// two answers are empty — the honest answer, and the one the panel
// renders as "nothing featured" rather than as an error.
//
// Installing a marketplace entry needs the archive the catalogue
// points at and the reference's own installer. InstallCustomization
// therefore answers a refusal with a message, not a silent success.
package remotecontrol

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/features/plugins/core"
)

// marketplacePageSize is how many entries one page of the
// marketplace search carries.
//
// The reference pages the search with a token. The local catalogue is
// small enough — 67 entries across two marketplaces on this machine —
// that one page is the whole answer, and an empty next-page token
// tells the panel not to ask again.
const marketplacePageSize = 200

// ---- the search ------------------------------------------------------------

// handleLSSearchMarketplace answers SearchMarketplace.
//
// 【反编译】 exa.language_server_pb.SearchMarketplaceResponse carries
// customizations (1), next_page_token (2) and groups (3).
//
// The groups are the marketplaces themselves: one ResultGroup per
// catalogue, with the catalogue's description and the ids of the
// entries it holds.
func (d *Dispatcher) handleLSSearchMarketplace(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		Query     string `json:"query"`
		GroupID   string `json:"groupId"`
		PageToken string `json:"pageToken"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	docs := core.MarketplaceDocs()
	entries := core.SearchMarketplaceEntries(docs, req.Query)

	// A group id narrows the search to one marketplace.
	if group := strings.TrimSpace(req.GroupID); group != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if _, doc, ok := core.MarketplaceEntryByID(docs, e.Name); ok && doc.Name == group {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	customizations := make([]any, 0, len(entries))
	for _, e := range entries {
		if c, ok := lsMarketplaceCustomizationJSON(docs, e); ok {
			customizations = append(customizations, c)
		}
	}
	groups := lsMarketplaceGroupsJSON(docs)

	next := ""
	if len(entries) > marketplacePageSize {
		customizations = customizations[:marketplacePageSize]
		next = "page-2"
	}
	return map[string]any{
		"customizations": customizations,
		"nextPageToken":  next,
		"groups":         groups,
	}, nil, true
}

// lsMarketplaceGroupsJSON renders one ResultGroup per catalogue.
//
// 【反编译】 exa.language_server_pb.ResultGroup declares id (1),
// display_name (2), description (3), member_ids (4) and
// next_page_token (5).
func lsMarketplaceGroupsJSON(docs []core.MarketplaceDoc) []any {
	out := make([]any, 0, len(docs))
	for _, doc := range docs {
		members := make([]any, 0, len(doc.Plugins))
		for _, p := range doc.Plugins {
			members = append(members, p.Name)
		}
		out = append(out, map[string]any{
			"id":          doc.Name,
			"displayName": doc.Name,
			"description": doc.Description,
			"memberIds":   members,
		})
	}
	return out
}

// handleLSSearchMarketplaceCustomizations answers
// SearchMarketplaceCustomizations.
//
// 【反编译】 exa.language_server_pb.
// SearchMarketplaceCustomizationsResponse carries extensions (1) and
// has_more (2). An extension is an exa.cortex_pb.WorkflowSpec — the
// same shape a skill has.
//
// The local catalogue lists *plugins*, not the skills inside them, so
// the extensions this answers with are the skills the installed
// plugins contribute. That is the customization set this machine
// actually has; the marketplace's own skill listings live in the
// cloud.
func (d *Dispatcher) handleLSSearchMarketplaceCustomizations(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		Query         string   `json:"query"`
		PageSize      int      `json:"pageSize"`
		ExtensionType []string `json:"extensionTypes"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	extensions := make([]any, 0)
	for _, p := range readLSPluginCatalog().Plugins {
		for _, skill := range core.PluginSkills(p.Name) {
			if query != "" &&
				!strings.Contains(strings.ToLower(skill.Name), query) &&
				!strings.Contains(strings.ToLower(skill.Description), query) {
				continue
			}
			extensions = append(extensions, lsWorkflowSpecJSON(p, skill))
		}
	}
	hasMore := false
	if req.PageSize > 0 && len(extensions) > req.PageSize {
		extensions = extensions[:req.PageSize]
		hasMore = true
	}
	return map[string]any{
		"extensions": extensions,
		"hasMore":    hasMore,
	}, nil, true
}

// ---- one entry -------------------------------------------------------------

// handleLSGetMarketplaceCustomization answers GetMarketplaceCustomization.
//
// 【反编译】 exa.language_server_pb.
// GetMarketplaceCustomizationResponse carries customization (1),
// plugin (2) and skill (3).
func (d *Dispatcher) handleLSGetMarketplaceCustomization(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		ID             string `json:"id"`
		IncludePreview bool   `json:"includePreview"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	docs := core.MarketplaceDocs()
	entry, doc, ok := core.MarketplaceEntryByID(docs, req.ID)
	if !ok {
		return map[string]any{}, nil, true
	}
	customization, ok := lsMarketplaceCustomizationJSON(docs, entry)
	if !ok {
		return map[string]any{}, nil, true
	}
	out := map[string]any{"customization": customization}
	// The installed plugin, when the marketplace's entry is installed
	// here: the panel shows the installed state next to the entry.
	if p, found := core.FindInstalledPlugin(entry.Name); found {
		out["plugin"] = lsPluginJSON(p, docs)
	}
	_ = doc
	return out, nil, true
}

// lsMarketplaceCustomizationJSON renders one catalogue entry in the
// language server's own MarketplaceCustomization shape.
//
// 【反编译】 exa.language_server_pb.MarketplaceCustomization declares
// marketplace (1), id (2), display_name (3), description (4),
// source (5), plugin (6), logo (7), skill (8), version (9),
// author (10), homepage (11) and repository (12).
//
// The source is a oneof: a google3 entry carries a depot path, an
// archive entry carries the URL and the digest. The local
// catalogues carry archives.
func lsMarketplaceCustomizationJSON(docs []core.MarketplaceDoc, entry core.MarketplaceEntry) (map[string]any, bool) {
	_, doc, ok := core.MarketplaceEntryByID(docs, entry.Name)
	if !ok {
		return nil, false
	}
	out := map[string]any{
		"marketplace": doc.Name,
		"id":          entry.Name,
		"displayName": entry.DisplayName,
		"description": entry.Description,
		"logo":        entry.Logo,
		"version":     entry.Version,
		"author":      lsAuthorJSON(entry.Author),
		"homepage":    entry.Homepage,
		"repository":  entry.Repository,
		"plugin":      map[string]any{},
		"skill":       map[string]any{},
	}
	if s := entry.Source; s != nil {
		switch s.Type {
		case "archive":
			out["source"] = map[string]any{
				"archive": map[string]any{"url": s.URL, "sha256": s.SHA256},
			}
		case "google3":
			out["source"] = map[string]any{
				"google3": map[string]any{"depotPath": s.URL},
			}
		default:
			out["source"] = map[string]any{}
		}
	}
	return out, true
}

// ---- the bundles and the featured cards ------------------------------------

// handleLSGetMarketplaceBundles answers GetMarketplaceBundles.
//
// 【反编译】 exa.language_server_pb.GetMarketplaceBundlesResponse
// carries bundles (1) and customizations (2).
//
// A bundle is the cloud's curation — a named set of entries the
// marketplace groups together. The local catalogue carries no
// bundles, so the answer's bundles are empty; the customizations are
// the requested ids, resolved against the catalogue.
func (d *Dispatcher) handleLSGetMarketplaceBundles(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	docs := core.MarketplaceDocs()
	customizations := make([]any, 0, len(req.IDs))
	for _, id := range req.IDs {
		entry, _, ok := core.MarketplaceEntryByID(docs, id)
		if !ok {
			continue
		}
		if c, ok := lsMarketplaceCustomizationJSON(docs, entry); ok {
			customizations = append(customizations, c)
		}
	}
	return map[string]any{
		"bundles":        []any{},
		"customizations": customizations,
	}, nil, true
}

// handleLSGetMarketplaceFeatured answers GetMarketplaceFeatured.
//
// 【反编译】 exa.language_server_pb.GetMarketplaceFeaturedResponse
// carries featured (1) and customizations (2).
//
// The featured cards are the cloud's own curation; the local
// catalogue has no featured list. The answer is therefore empty on
// both members — the panel renders "nothing featured" rather than an
// error.
func (d *Dispatcher) handleLSGetMarketplaceFeatured(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{
		"featured":       []any{},
		"customizations": []any{},
	}, nil, true
}

// ---- the skill link --------------------------------------------------------

// handleLSGetSkillMarketplaceLink answers GetSkillMarketplaceLink.
//
// 【反编译】 exa.language_server_pb.GetSkillMarketplaceLinkResponse
// carries marketplace_url (1).
//
// The link is the marketplace page of the plugin the skill belongs
// to. The skill's path names the plugin's own directory, so the
// plugin is the path's grandparent, and the marketplace is the
// catalogue that holds it.
func (d *Dispatcher) handleLSGetSkillMarketplaceLink(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		SkillPath string `json:"skillPath"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	plugin := lsPluginNameFromSkillPath(req.SkillPath)
	if plugin == "" {
		return map[string]any{"marketplaceUrl": ""}, nil, true
	}
	docs := core.MarketplaceDocs()
	entry, doc, ok := core.MarketplaceEntryByID(docs, plugin)
	if !ok {
		return map[string]any{"marketplaceUrl": ""}, nil, true
	}
	_ = entry
	return map[string]any{
		"marketplaceUrl": "https://antigravity.google.com/marketplace/" + doc.Name + "/" + plugin,
	}, nil, true
}

// lsPluginNameFromSkillPath reads the plugin's name out of a skill
// path.
//
// A skill lives at "<plugin>/skills/<skill>/SKILL.md", so the plugin
// is the third-from-last path element. A path that is not of that
// shape carries no plugin.
func lsPluginNameFromSkillPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	// …/<plugin>/skills/<skill>/SKILL.md
	if len(parts) < 3 {
		return ""
	}
	idx := len(parts) - 1
	if parts[idx] == "SKILL.md" {
		idx--
	}
	if idx < 1 || parts[idx-1] != "skills" {
		return ""
	}
	return parts[idx-2]
}

// ---- the cascade templates -------------------------------------------------

// handleLSGetAvailableCascadePlugins answers GetAvailableCascadePlugins.
//
// 【反编译】 exa.cascade_plugins_pb.CascadePluginTemplate declares
// title (1), id (2), link (3), description (4), commands (5),
// installation_count (6), trust_level (7), readme (8), local (9) and
// remote (10).
//
// The templates are the installed plugins, rendered in the cascade's
// own shape: the plugin's id and title, and its skills as the
// commands it contributes.
func (d *Dispatcher) handleLSGetAvailableCascadePlugins(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		OS          string `json:"os"`
		SearchQuery string `json:"searchQuery"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	query := strings.ToLower(strings.TrimSpace(req.SearchQuery))
	plugins := make([]any, 0)
	for _, p := range readLSPluginCatalog().Plugins {
		if query != "" &&
			!strings.Contains(strings.ToLower(p.Name), query) &&
			!strings.Contains(strings.ToLower(p.Manifest.DisplayName), query) &&
			!strings.Contains(strings.ToLower(p.Manifest.Description), query) {
			continue
		}
		plugins = append(plugins, lsCascadePluginTemplateJSON(p))
	}
	return map[string]any{"plugins": plugins}, nil, true
}

// lsCascadePluginTemplateJSON renders one installed plugin as the
// cascade's plugin template.
func lsCascadePluginTemplateJSON(p core.InstalledPlugin) map[string]any {
	commands := map[string]any{}
	for _, skill := range core.PluginSkills(p.Name) {
		commands["/"+skill.Name] = map[string]any{
			"template": map[string]any{
				"command": "/" + skill.Name,
				"args":    []any{},
				"env":     map[string]any{},
			},
		}
	}
	return map[string]any{
		"title":             lsPluginDisplayName(p),
		"id":                p.Name,
		"link":              "",
		"description":       p.Manifest.Description,
		"commands":          commands,
		"installationCount": 0,
		"trustLevel":        "",
		"readme":            lsPluginReadme(p),
		"local":             map[string]any{"commands": commands},
		"remote":            map[string]any{"template": map[string]any{"serverUrl": "", "headers": map[string]any{}}},
	}
}

// ---- the remote plugins ----------------------------------------------------

// handleLSListRemotePlugins answers ListRemotePlugins.
//
// 【反编译】 exa.language_server_pb.ListRemotePluginsResponse carries a
// repeated exa.remote_agent_proxy_pb.PluginInfo, and the request
// carries a backend and a return_to_app.
//
// A "remote plugin" is a plugin that is *connected* to this host —
// not a customization plugin on disk. The clone's own PluginStore
// holds exactly that: the connected plugins, with their ids, names
// and versions.
func (d *Dispatcher) handleLSListRemotePlugins(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	if d == nil || d.cfg.PluginsStore == nil {
		return map[string]any{"plugins": []any{}}, nil, true
	}
	state, err := d.cfg.PluginsStore.Snapshot(nil)
	if err != nil {
		return nil, internalError("list plugins"), true
	}
	plugins := make([]any, 0, len(state.Plugins))
	for _, p := range state.Plugins {
		plugins = append(plugins, map[string]any{
			"pluginId":      p.PluginID,
			"displayName":   p.Name,
			"version":       p.Version,
			"description":   "",
			"logoUri":       "",
			"authRequired":  false,
			"connections":   []any{},
			"authorizeUri":  "",
			"credentialKey": "",
		})
	}
	return map[string]any{"plugins": plugins}, nil, true
}

// handleLSDisconnectRemotePlugin answers DisconnectRemotePlugin.
//
// 【反编译】 the response carries no field at all; the request carries
// a backend, a plugin_id and an account.
//
// The disconnect is the clone's own PluginStore's disconnect — the
// same seam the legacy JSON-RPC method uses, under the language
// server's own name.
func (d *Dispatcher) handleLSDisconnectRemotePlugin(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		PluginID string `json:"pluginId"`
		Account  string `json:"account"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.PluginID)
	if id == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "plugin_id is required"}, true
	}
	if d == nil || d.cfg.PluginsStore == nil {
		return map[string]any{}, nil, true
	}
	if _, err := d.cfg.PluginsStore.Disconnect(context.Background(), id); err != nil {
		return nil, mapPluginsError(err, "disconnect plugin"), true
	}
	return map[string]any{}, nil, true
}

// handleLSDownloadBuildWithGooglePlugin answers
// DownloadBuildWithGooglePlugin.
//
// 【反编译】 the response carries success (1) and message (2).
//
// The download needs the cloud's own build artefact. The clone has
// no cloud catalogue, so the answer is a refusal that says so.
func (d *Dispatcher) handleLSDownloadBuildWithGooglePlugin(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		PluginID string `json:"pluginId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	return map[string]any{
		"success": false,
		"message": "Downloading a Google-built plugin is not supported by this build.",
	}, nil, true
}

// handleLSStartPluginAuthSignIn answers StartPluginAuthSignIn.
//
// 【反编译】 the response carries auth_url (1) and account_id (2).
//
// The sign-in is a browser OAuth flow against the plugin's own
// provider. The clone has no plugin auth providers, so there is no
// URL to hand out — and an empty one is what the panel needs to know
// that there is nothing to sign in to.
func (d *Dispatcher) handleLSStartPluginAuthSignIn(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{"authUrl": "", "accountId": ""}, nil, true
}

// handleLSSignInPluginAuthProvider answers SignInPluginAuthProvider.
//
// 【反编译】 the response carries connected (1) and account_id (2).
func (d *Dispatcher) handleLSSignInPluginAuthProvider(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{"connected": false, "accountId": ""}, nil, true
}

// handleLSSignOutPluginAuthProvider answers SignOutPluginAuthProvider.
//
// 【反编译】 the response carries no field.
func (d *Dispatcher) handleLSSignOutPluginAuthProvider(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{}, nil, true
}

// handleLSSetDefaultPluginAuthAccount answers
// SetDefaultPluginAuthAccount.
//
// 【反编译】 the response carries no field.
func (d *Dispatcher) handleLSSetDefaultPluginAuthAccount(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{}, nil, true
}

// handleLSSetPluginAuthAccountLabel answers SetPluginAuthAccountLabel.
//
// 【反编译】 the response carries no field.
func (d *Dispatcher) handleLSSetPluginAuthAccountLabel(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{}, nil, true
}

// ---- the cloud-only surfaces -----------------------------------------------

// handleLSGetBuildWithGooglePlugins answers GetBuildWithGooglePlugins.
//
// 【反编译】 the response carries a repeated
// google.internal.cloud.code.v1internal.BuildWithGooglePlugin.
//
// A "build with Google" plugin is a plugin Google builds and serves
// from its own cloud. The clone has no cloud catalogue, so the answer
// is empty: the panel renders "no Google-built plugins", which is
// true, rather than a list of plugins that cannot be installed.
func (d *Dispatcher) handleLSGetBuildWithGooglePlugins(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{"plugins": []any{}}, nil, true
}

// handleLSListPluginAuthProviders answers ListPluginAuthProviders.
//
// 【反编译】 the response carries a repeated
// exa.language_server_pb.PluginAuthProvider.
//
// A plugin auth provider is an OAuth provider a plugin needs. The
// clone's plugins have no such provider configured — the plugin.json
// files on this machine carry no auth section — so the answer is
// empty.
func (d *Dispatcher) handleLSListPluginAuthProviders(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	return map[string]any{"providers": []any{}}, nil, true
}

// handleLSInstallCustomization answers InstallCustomization.
//
// The install needs the archive the catalogue points at, the
// reference's own unpacker and the reference's own registration. The
// clone has the catalogue but not the other two, so the answer is a
// refusal that says so — never a success that did not happen.
func (d *Dispatcher) handleLSInstallCustomization(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		ID       string `json:"id"`
		BundleID string `json:"bundleId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "id is required"}, true
	}
	return map[string]any{
		"success":    false,
		"message":    "Installing a marketplace plugin is not supported by this build.",
		"installDir": "",
	}, nil, true
}

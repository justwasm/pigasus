// alwaysload.go — the AGY 1.3.3 "always loaded MCP tool" hint parser.
//
// AGY 1.3.3 changelog line 42 (also recovered verbatim from the v1.3.4
// binary at offset 0x8745 in out/local-macos/seeds/seeds.txt and in the
// decompiled strings dump at docs/decompiled-v1.3.4/strings-new-in-v1.3.4.txt):
//
//	Added support for MCP servers to mark a tool as always loaded by
//	setting `_meta: {"antigravity/alwaysLoad": true}` on it in their
//	`tools/list` response, so the agent gets that tool directly instead
//	of discovering it on demand.
//
// PiG v0.4.1's MCP extension (coding/mcpext) doesn't expose the _meta
// bag to callers, and the upstream package itself does not honour the
// antigravity/alwaysLoad key — the transport is "list, then register
// every tool under mcp__<server>__<tool>" without a pinned-tool flag.
// The AGY wrapper is therefore the only place this hint can be
// interpreted, and the natural seam is the `tools/list` JSON the server
// returns at handshake time.
//
// What this file owns
// -------------------
//
//  1. The single JSON-decoder entrypoint for a `tools/list` response.
//  2. The `antigravity/alwaysLoad` key constant (so callers and tests
//     agree on the spelling).
//  3. A pure split function (no I/O) that callers — and tests — can
//     drive with hand-built entries.
//  4. A builder that turns a per-server always-load list into the
//     synthetic tool names the runtime can register alongside PiG's
//     MCP extension (the `mcp__<server>__<tool>` convention PiG uses
//     on its own).
//
// What this file does NOT own
// ---------------------------
//
//   - Dispatch. The reference's "always load" semantics in 1.3.3 is
//     "advertise that tool to the model as its own entry". The runtime
//     in runtime.go is what registers the always-loaded tools before
//     the session starts so the model sees them before PiG's MCP
//     extension's dynamically-discovered siblings. The dispatch path
//     is PiG's normal MCP `tools/call` flow; the runtime just makes
//     sure the tool names are in the session's tool list.
//
//   - Server config. The `mcp.json` per-server static hint lives in
//     mcpconfig.go (agyMCPServer.AlwaysLoad). This file only handles
//     the protocol-side `_meta` flag.
package pigrt

import (
	"encoding/json"
	"sort"
)

// AntigravityAlwaysLoadMetaKey is the AGY-defined key inside a
// `tools/list` entry's `_meta` object that opts a tool into being
// advertised to the model directly, instead of remaining reachable
// only through the discoverable call_mcp_tool surface.
//
// MCP's `_meta` field is intentionally open-ended; this constant is
// the AGY project's single declaration of the spelling it reads. A
// misspelling here would silently make the flag unobservable.
const AntigravityAlwaysLoadMetaKey = "antigravity/alwaysLoad"

// MCPToolSpec is one tool an MCP server returned in a `tools/list`
// response, after parsing. The AlwaysLoad field is computed from the
// `_meta` bag; the rest is the MCP-spec subset we surface to the
// runtime.
type MCPToolSpec struct {
	// Name is the tool's MCP name (the part after the
	// `mcp__<server>__` prefix the runtime adds in front).
	Name string
	// Description is the human-readable description the server
	// supplied. Empty when the server omitted one.
	Description string
	// InputSchema is the JSON Schema the server returned for the
	// tool's arguments. May be empty (the MCP spec permits it); the
	// runtime treats a missing schema as `{"type":"object"}`.
	InputSchema json.RawMessage
	// AlwaysLoad is true iff the entry's `_meta` carried
	// {"antigravity/alwaysLoad": true}. Computed; callers do not
	// set it directly.
	AlwaysLoad bool
}

// MCPName builds the canonical tool name the PiG runtime uses for a
// (server, tool) pair: `mcp__<server>__<tool>`. The runtime registers
// the always-loaded tools under these names so the names line up with
// the names PiG's MCP extension would otherwise assign, and a unique
// registration by name is what lets the AGY layer win the
// registration race.
//
// server and tool are taken as supplied; the caller is responsible
// for any sanitisation (the names MCP servers return are
// implementation-defined).
func MCPName(server, tool string) string {
	return "mcp__" + server + "__" + tool
}

// ParsedToolsList is one `tools/list` response, after the entry-level
// `_meta.antigravity/alwaysLoad` flag has been applied to group each
// entry. The two slices preserve the order the server returned them
// in (MCP spec: significant when the server uses the list to suggest
// a UI ordering).
type ParsedToolsList struct {
	// AlwaysLoad carries the tools the server marked with
	// `antigravity/alwaysLoad: true`.
	AlwaysLoad []MCPToolSpec
	// Discoverable carries the rest. The reference calls these
	// "on-demand": the model reaches them through the
	// `call_mcp_tool` aggregator.
	Discoverable []MCPToolSpec
}

// ToolsListEntry is the JSON shape one item in a `tools/list`
// response's `tools` array takes. The MCP spec permits additional
// fields; only the four we read are decoded and the rest is dropped
// (json.Unmarshal ignores unknown fields by default), so a server
// extending its own entries with new fields does not break parsing.
type ToolsListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

// toolsListResponse is the JSON shape of an MCP `tools/list` response.
// The MCP spec puts the array under `tools` (alongside `nextCursor`,
// which we ignore — our spec is single-shot).
type toolsListResponse struct {
	Tools []ToolsListEntry `json:"tools"`
}

// ParseToolsList parses a `tools/list` JSON response and returns the
// always-load / discoverable split.
//
// An empty payload is not an error: it yields an empty ParsedToolsList
// — every server that has not yet connected its tools.list produces
// this state, and downstream code should treat it as "no tools from
// this server", not as an error. Nil and zero-length payloads take
// the same path (json.Unmarshal returns "unexpected end of JSON input"
// on a nil byte slice, which we silence here).
//
// A malformed payload (bad top-level JSON, or `tools` carrying a
// non-array value) returns the unmarshal error. A payload whose
// entries each parse correctly is never an error — including a
// payload whose entries are missing `_meta`, missing `name`, or have
// a malformed `_meta`. We surface "this entry is not always-load"
// rather than failing the whole parse, because the MCP spec lets a
// server degrade one entry without breaking the rest.
func ParseToolsList(payload []byte) (ParsedToolsList, error) {
	if len(payload) == 0 {
		return ParsedToolsList{}, nil
	}
	var resp toolsListResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return ParsedToolsList{}, err
	}
	return SplitToolsList(resp.Tools), nil
}

// SplitToolsList groups the raw entries by the AGY alwaysLoad flag.
// Pure: no I/O, no allocations beyond the result slices. The return
// is a value (not a pointer) so callers can compare it against
// literals in tests and benchmark inlines it.
func SplitToolsList(entries []ToolsListEntry) ParsedToolsList {
	var split ParsedToolsList
	for _, entry := range entries {
		spec := MCPToolSpec{
			Name:        entry.Name,
			Description: entry.Description,
			InputSchema: entry.InputSchema,
		}
		if entryAlwaysLoad(entry.Meta) {
			spec.AlwaysLoad = true
			split.AlwaysLoad = append(split.AlwaysLoad, spec)
		} else {
			split.Discoverable = append(split.Discoverable, spec)
		}
	}
	return split
}

// entryAlwaysLoad reports whether the AGY-defined alwaysLoad meta
// flag is set. The MCP spec defines `_meta` as an object with
// implementation-defined keys; we look only under our own key and
// require a JSON-true value — anything else (false, null, "true"
// string, the number 1, a missing key) yields false. A malformed
// `_meta` (not an object at all) is treated as "no flag set".
//
// The bool-only check is intentional: the reference uses a strict
// bool true in the decompiled source. Anything weaker than that risks
// surprising the model with a tool that the admin did not actually
// flag.
func entryAlwaysLoad(meta json.RawMessage) bool {
	if len(meta) == 0 {
		return false
	}
	var lookup struct {
		AlwaysLoad json.RawMessage `json:"antigravity/alwaysLoad"`
	}
	if err := json.Unmarshal(meta, &lookup); err != nil {
		return false
	}
	if len(lookup.AlwaysLoad) == 0 {
		return false
	}
	var flag bool
	if err := json.Unmarshal(lookup.AlwaysLoad, &flag); err != nil {
		return false
	}
	return flag
}

// AlwaysLoadHint is one always-load entry applied to a server. The
// runtime stores the parsed hints from the `mcp.json` file (per-server
// static) and from the `tools/list` JSON (per-server dynamic) under
// this shape so the two sources can be merged and deduplicated.
type AlwaysLoadHint struct {
	// Server is the MCP server name (the key under `servers` in
	// mcp.json). Empty is treated as "the per-server hint did not
	// attach a name"; the runtime drops those.
	Server string
	// Tool is the tool name as the server returned it. The
	// runtime composes the canonical `mcp__<server>__<tool>`
	// name through MCPName.
	Tool string
	// Source is "static" (came from mcp.json) or "dynamic"
	// (came from a tools/list response). The runtime prefers
	// dynamic when both sources name the same tool — the
	// server's answer supersedes the admin's — and records the
	// chosen source so a caller can audit a configured tool
	// against its actual declaration.
	Source string
}

// AlwaysLoadHintsFromConfig parses the static per-server always-load
// hints from a loaded agy `mcp.json` (the typed view returned by
// LoadAgyMCPFrom). The function does not see the on-disk file; it
// takes the already-decoded server list so it can be called from
// tests with hand-built server entries.
//
// A server whose `alwaysLoad` list is empty or missing contributes
// nothing. Tool names are not validated here — an admin who lists a
// tool that the server does not actually export simply has their
// hint remain a no-op when the runtime reconciles it against the
// server's tools/list result.
//
// Sort order: alphabetical by (server, tool), so the runtime sees a
// stable list across runs and an admin auditing two configurations
// can compare entries 1:1.
func AlwaysLoadHintsFromConfig(servers map[string]AgyMCPServer) []AlwaysLoadHint {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	var hints []AlwaysLoadHint
	for _, server := range names {
		spec := servers[server]
		if len(spec.AlwaysLoad) == 0 {
			continue
		}
		tools := append([]string(nil), spec.AlwaysLoad...)
		sort.Strings(tools)
		for _, tool := range tools {
			hints = append(hints, AlwaysLoadHint{
				Server: server,
				Tool:   tool,
				Source: "static",
			})
		}
	}
	return hints
}

// AlwaysLoadHintsFromToolsList parses the dynamic per-server
// always-load hints from a `tools/list` JSON response. The hints
// carry an empty server name — the caller fills it in (typically
// from the surrounding `tools/list` request context, which the AGY
// runtime binds to its own per-server transport).
func AlwaysLoadHintsFromToolsList(server string, payload []byte) ([]AlwaysLoadHint, error) {
	split, err := ParseToolsList(payload)
	if err != nil {
		return nil, err
	}
	hints := make([]AlwaysLoadHint, 0, len(split.AlwaysLoad))
	for _, tool := range split.AlwaysLoad {
		hints = append(hints, AlwaysLoadHint{
			Server: server,
			Tool:   tool.Name,
			Source: "dynamic",
		})
	}
	return hints, nil
}

// MergeAlwaysLoadHints merges static and dynamic hints by
// (server, tool), with dynamic winning on a tie (the server's
// declaration supersedes the admin's hint). The function does not
// validate that the dynamic hint is reachable; a misconfigured
// static hint that the server does not echo remains in the merged
// result so the runtime can surface "pinned but not present" as a
// soft warning when it reconciles the list.
//
// Sort order: alphabetical by (server, tool) — same as
// AlwaysLoadHintsFromConfig, so the merged list stays comparable
// to either source individually.
func MergeAlwaysLoadHints(static, dynamic []AlwaysLoadHint) []AlwaysLoadHint {
	// Walk static first, then dynamic. Any (server, tool) that
	// appears in dynamic overwrites the static entry's Source
	// label. dynamic-only entries are appended.
	merged := make([]AlwaysLoadHint, 0, len(static)+len(dynamic))
	merged = append(merged, static...)
	for _, hint := range dynamic {
		replaced := false
		for i, existing := range merged {
			if existing.Server == hint.Server && existing.Tool == hint.Tool {
				merged[i] = hint
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, hint)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Server != merged[j].Server {
			return merged[i].Server < merged[j].Server
		}
		return merged[i].Tool < merged[j].Tool
	})
	return merged
}

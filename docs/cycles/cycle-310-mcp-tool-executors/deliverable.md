# The three MCP tools — deliverable

**Cycle:** `cycle-310-mcp-tool-executors`
**Subject:** the reference's `call_mcp_tool`, `list_resources` and
`read_resource` — their declarations, their parameter schemas, and the
client that answers them.
**Evidence:** 【反编译】 the 1.4.7 binary's Go type metadata (the
reference's reflected parameter structs, recovered verbatim from the
generic instantiations' own names) and 【strings】 the reference's own
description text.

---

## 0. Summary

| | before | after |
|---|---|---|
| **`call_mcp_tool`** | advertised, no executor — every call failed with `ErrNotImplemented` | a real tool: the reference's exact declaration, and a call that reaches the server |
| **`list_resources`** | advertised, no executor | a real tool: the reference's exact declaration, and a listing |
| **`read_resource`** | advertised, no executor | a real tool: the reference's exact declaration, and a read |
| **the parameter schemas** | the catalogue's generic empty object | the reference's reflected schemas — the PascalCase names, the descriptions, the required set |
| **the MCP client** | none — the tools had nothing to call | `internal/mcp.Pool`: one lazy connection per configured server |
| **the configuration** | parsed twice, in two packages | parsed once, in `internal/mcp`; `internal/pigrt` aliases the types |

## 1. The reference's shape

### 1.1 The three tools

The reference advertises three MCP tools to the model. Their
**descriptions** are the reference's own text (【strings】, read out of the
1.4.7 binary, committed at
`internal/tools/testdata/tool_descriptions_1.4.7.json` under the
`"layer": "converter"` tag — the reference builds them in
`cortex/tools/tools`, the converter layer):

| tool | description |
|---|---|
| `call_mcp_tool` | Call a lazy-loaded MCP tool. Read the tool's schema file to understand the tool's arguments and usage. |
| `list_resources` | Lists the available resources from an MCP server. |
| `read_resource` | Retrieves a specified resource's contents. |

### 1.2 The parameter schemas — recovered from the type metadata

The reference does not write its tool schemas by hand: it reflects over a
Go struct and turns the struct tags into a JSON schema
(`cortex/utils/utils.ToJsonSchemaString[T]`). A stripped Go binary keeps
the *type metadata* — and the generic instantiation's own name carries the
struct, field by field, with its tags.

【反编译】 In the 1.4.7 binary those instantiations are named:

```
google3/third_party/jetski/cortex/utils/utils.ToJsonSchemaString[go.shape.struct {
    ServerName string "jsonschema:\"required\" jsonschema_description:\"Name of the MCP server.\"";
    ToolName string "jsonschema:\"required\" jsonschema_description:\"Name of the tool to call.\"";
    Arguments encoding/json/jsontext.Value "jsonschema:\"required\" jsonschema_description:\"Arguments to pass to the tool.\""
}]
```

and, for the other two:

```
…ToJsonSchemaString[go.shape.struct {
    ServerName string "jsonschema:\"required,minLength=1\" jsonschema_description:\"Name of the server to list available resources from.\""
}]

…ToJsonSchemaString[go.shape.struct {
    ServerName string "jsonschema_description:\"Name of the server to read the resource from.\"";
    Uri string "jsonschema_description:\"Unique identifier for the resource.\""
}]
```

So the reference's three schemas are, in full:

| tool | parameter | constraint | description |
|---|---|---|---|
| `call_mcp_tool` | `ServerName` | `required` | Name of the MCP server. |
| | `ToolName` | `required` | Name of the tool to call. |
| | `Arguments` | `required` | Arguments to pass to the tool. |
| `list_resources` | `ServerName` | `required,minLength=1` | Name of the server to list available resources from. |
| `read_resource` | `ServerName` | — | Name of the server to read the resource from. |
| | `Uri` | — | Unique identifier for the resource. |

Two details fall out of the tags:

- **`read_resource` has no required set.** Neither of its fields carries a
  `jsonschema:"required"` tag, so the reference declares no required
  parameters at all — the clone omits the `required` key rather than
  advertising an empty one.
- **`call_mcp_tool`'s `Arguments` is a JSON *value*.** Its type is
  `encoding/json/jsontext.Value`, not a struct or a map: the argument is
  whatever the *server's* schema asks for, so the clone declares it without
  a `type` constraint.

The `Arguments` field was the last unknown of the three — the type-metadata
dump had been truncated mid-tag. It is the third field of the
`call_mcp_tool` struct, and the tag reads
`jsonschema:"required" jsonschema_description:"Arguments to pass to the tool."`.

### 1.3 The reference's error messages

The binary holds the reference's own wording, and the clone uses it:

| literal | where the reference uses it |
|---|---|
| `ServerName is a required parameter. Follow the function call schema exactly.` | `cortex/tools/tools.(*ListResourcesToolConverter).ToolCallToCortexStep` (0x101b0d9dc) |
| `tool %s is not enabled for server %s` | `(*CallMcpToolConverter).ToolCallToCortexStep` (0x101b0d694) |
| `failed to marshal arguments` | the same converter (0x101b0d48c) |
| `MCP server %q not found` | the MCP server lookup |
| `mcp server does not support resources` | `cortex/utils/mcp/mcpcore.(*McpClientSessionInstance).ReadResource` (0x1014cd07c) |
| `MCP client session is not initialized` | the same package's session guard |

### 1.4 Where the reference keeps the servers

The reference reads its servers from `~/.gemini/antigravity-cli/mcp.json`:

```json
{"servers": {"<name>": {"type": "stdio"|"http", "command": "…", "args": ["…"],
                        "env": {"K": "V"}, "url": "…", "headers": {"K": "V"},
                        "enabled": true, "alwaysLoad": ["tool_name"]}},
 "disableAllMCP": false}
```

The `headers` field is AGY 1.4.0 (changelog line 21), `alwaysLoad` is
AGY 1.3.3 (changelog line 42) and `disableAllMCP` is the top-level switch
the reference honours.

## 2. The clone's shape

### 2.1 The declarations

`internal/tools/mcp.go` holds the three executors, their schemas and their
descriptions. The catalogue registers them with real executors, so they
left the "advertised but unimplemented" set.

The parameter names are the reference's PascalCase ones. The clone's
canonicaliser (`internal/tools/tools_params.go`) lower-cases a model's keys
on the way in — `ServerName` arrives as `server_name` — so the three tools
read their arguments themselves.

That is not a style choice: **`call_mcp_tool`'s `Arguments` payload
belongs to the MCP server**, which owns those names. The canonicaliser
would rewrite `queryString` into `query_string`, and a server whose schema
says `queryString` would reject the call. The three tools therefore keep
the payload verbatim, and `TestCallMcpToolPassesArgumentsVerbatim` pins
that.

### 2.2 The client

`internal/mcp/client.go` is the pool the three tools call through:

- one connection per server, opened **lazily** — a call is the first thing
  that spawns a server process;
- the connection is reused for the next call;
- a stdio server gets a stdio transport, an http server a streamable-HTTP
  one, with the header values expanded against the merged server-env ∪
  host-env (`internal/mcp/header_substitute.go`);
- the pool speaks the MCP wire protocol through PiG's `mcp` package, so
  the clone carries one MCP stack, not two.

`internal/tools` depends on the client only through the `MCPProvider`
interface, so the tools package stays free of any MCP dependency.

### 2.3 The wiring

`internal/printmode/mcp.go` installs the pool as the process-wide provider
(`internal/tools.SetMCPProvider`). Both surfaces call it:

- the **print path** — `internal/printmode/main_print.go`;
- the **TUI** — `internal/cliapp/tui_entry.go`.

The provider is process-wide because the pool holds the connections: a
session-scoped pool would spawn a second server process per turn.

The configuration file now has **one** reader in the tree:
`internal/mcp/agyconfig.go` parses it, and `internal/pigrt/mcpconfig.go`'s
`AgyMCPFile` / `AgyMCPServer` are aliases of the new types, so the
PiG-shaped translation and the client read the same bytes.

### 2.4 The flow

```
the model calls call_mcp_tool
  → internal/tools.CallMcpTool.Run
     → ServerName / ToolName / Arguments are read verbatim
     → the pool opens (or reuses) the server's connection
     → tools/call
     → the tool's text content reaches the model
```

## 3. 硬约束 / the load-bearing rules

1. **The reference's parameter names are PascalCase** and the catalogue
   advertises them as such: `ServerName`, `ToolName`, `Arguments`, `Uri`.
2. **`call_mcp_tool`'s `Arguments` is forwarded verbatim.** The server
   owns those names.
3. **`read_resource` declares no required set** — the reference's tags say
   so.
4. **A missing `ServerName` reports the reference's own sentence**:
   `ServerName is a required parameter. Follow the function call schema exactly.`
5. **A server that is not configured reports the reference's own
   sentence**: `MCP server %q not found`.
6. **The pool is lazy.** Installing it must not start a server process.

## 4. 存疑 / open questions

- **【推测】** The *result text* the model sees is the clone's own. The
  reference's result renderer is not reachable from the pclntab: the seeds
  carry `cortex/core/tools/tools.newListResourcesTool` and
  `newReadResourceTool` but no `Execute` method for them, and the binary's
  string table has no "N resources found"-style literal for these tools.
  What the binary *does* pin is the step payload the reference records:

  ```
  CortexStepListResources{server_name, cursor, resources, next_cursor}
  CortexStepReadResource{server_name, uri, contents,
                         skipped_non_image_binary_content}
  ```

  The clone renders the resource list as the MCP `resources` array in the
  server's own JSON shape, a resource read as the contents' text, and a
  tool call as the tool's text content. The one behaviour the proto *does*
  pin is `skipped_non_image_binary_content`: binary content that is not an
  image is skipped — the model never sees it — and the clone skips it too.
- **【推测】** The reference's *schema JSON* is the output of its own
  `utils.ToJsonSchemaString`, whose exact bytes were not recovered. The
  clone emits a schema in the house style of the other catalogue entries:
  the reference's parameter names, the reference's descriptions, the
  reference's required set.
- **【推测】** The reference's *always-loaded* MCP tools are the other half
  of the MCP surface: a server's `alwaysLoad` hint (or a tool's
  `_meta.antigravity/alwaysLoad` flag) puts a tool in the model's own tool
  list. In the print path that surface is PiG's MCP extension's job; the
  TUI has no MCP surface at all yet.
- **【推测】** In 1.4.7 the reference reads `mcp.json` **only for Agent
  Plugins** — the binary's own message says so ("never loaded: mcp.json is
  read only for Agent Plugins plugins (plugin.json \"$schema\": %q). Move
  the servers to %s"). The clone keeps reading the global
  `~/.gemini/antigravity-cli/mcp.json`, which `agy mcp add` still writes;
  the plugin path is not implemented.

## 5. Limitations

- **Two connections per server in the print path.** The clone's pool
  serves the three meta-tools, and PiG's MCP extension opens its own
  connection for the always-loaded `mcp__<server>__<tool>` surface. The
  fix is to make the clone's pool the single owner and register the
  always-loaded tools from it; that needs the clone to drive `tools/list`
  itself (`internal/mcp` already parses the payload), which is a separate
  cycle.
- **The TUI still has no MCP tool surface.** The three tools work (the
  pool is installed), but the always-loaded MCP tools are not advertised
  there.
- **No OAuth.** An http MCP server that demands an OAuth token fails to
  connect; PiG's MCP extension has the OAuth flow, the clone's pool does
  not.
- **The pool's connection failures are not cached.** A server that cannot
  be reached is retried on the next call, which re-runs the connect (and
  the spawn). The reference's own retry policy was not recovered.

## 6. The evidence

| artefact | what it is |
|---|---|
| `internal/tools/mcp.go` | the three executors, the recovered schemas, the argument rules |
| `internal/tools/mcp_test.go` | the declarations are pinned; the arguments stay verbatim |
| `internal/mcp/client.go` | the pool, the transports, the result rendering |
| `internal/mcp/client_test.go` | the pool is driven end to end over an in-process MCP server |
| `internal/mcp/agyconfig.go` | the reference's `mcp.json`, parsed once |
| `internal/printmode/mcp.go` | the wiring both surfaces call |
| `internal/tools/testdata/tool_descriptions_1.4.7.json` | the reference's own description text for the three tools |
| `out/local-macos/protos/cortex_1.4.6.proto` | `CortexStepListResources`, `CortexStepReadResource` — the step payloads |

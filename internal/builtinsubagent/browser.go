package builtinsubagent

// KindBrowser is the browser agent's type name.
//
// 【strings】 the reference's built-in agents package carries two browser
// agents: `chrome_devtools` and `webview_browser`. They sit in one
// string blob in the 1.4.8 binary — "…research-googlechrome_devtools
// webview_browserimage-generator…" (the blob at 0x1034b3b17) — next to
// `image-generator`, which the
// reference's own prompt lists as an available subagent
// (internal/agyprompt/testdata/capture_1.4.6.txt, the <subagents>
// section). The blob is the agents' name vocabulary.
//
// 【一手】 the reference's browser-agent description names the same
// server: "The chrome_devtools MCP server is started automatically when
// this agent is invoked" (internal/tools/testdata/
// tool_descriptions_1.4.7.json, the browser_subagent tool).
//
// 【反编译】 the reference's built-in agents package carries
// `BrowserAgent`, `defaultBuiltinSubagentNames` and
// `CalculateAllowedSubagents` (out/seeds/funcs.txt), so the browser
// agent is one of the built-in *types*, and which of them are allowed
// is a configuration question — which is why the 1.4.6 capture, taken
// with the browser off, lists only self, research and image-generator.
//
// The clone ships the Chrome one: its browser agent drives a real
// Chrome. The webview variant belongs to the IDE's built-in <webview>
// browser (docs/guide/ide-language-server.md), which the clone does not
// drive.
//
// The kind is not in Default(): the browser agent needs the session's
// model and services, so the session registers it — see
// internal/browsersubagent.InstallBuiltin.
const KindBrowser Kind = "chrome_devtools"

// Package geminicode speaks the CloudCode dialect — the private Google
// "Code Assist" surface that the Antigravity CLI (`agy`) uses to reach
// Gemini and the Claude models behind a consumer subscription.
//
// The dialect is not the public Gemini API. It is the Gemini request and
// response shapes wrapped in Google's own envelope, under Google's own
// private RPC names:
//
//	POST {host}/v1internal:streamGenerateContent?alt=sse
//	Authorization: Bearer <google-oauth-token>
//
//	{"project":"aicode-consumers","requestId":"agent/…","request":{…},
//	 "model":"gemini-3.8-flash-high","userAgent":"antigravity",
//	 "requestType":"agent"}
//
// A local service that stands in for Google has to decode that envelope —
// not a bare `{"contents":[…]}` — and answer with `data: {"response":{…}}`
// frames. The full wire contract is in docs/guide/cloudcode-dialect.md;
// this package is the executable form of it.
//
// # What lives here
//
//   - The wire types (Request, Part, FunctionCall, Candidate, …) and the
//     envelope helpers (Inner, Wrapped, ModelName, SystemText).
//   - BuildRequest, which renders one turn into the exact body `agy` sends.
//   - Schema, which converts a provider-neutral JSON Schema into the
//     Gemini Schema proto's spelling.
//   - Client, which performs the POST and reads the SSE stream.
//   - Decoder, the pure state machine that turns SSE lines into chunks.
//
// # What does not live here
//
// The package has no dependency on any other package in this repository —
// it is importable on its own. That is why it carries its own Turn,
// TurnMessage and TurnTool types instead of reusing the application's
// neutral ones: a dialect library that depends on its host is not
// reusable. internal/backend adapts between the two.
//
// The transport policy is the application's, not the dialect's: retrying a
// dropped connection, refreshing an expired token and bounding a stalled
// stream are all decisions the caller makes. The client here performs one
// request and reports what happened.
//
// # Evidence
//
// Every shape below is transcribed from live `agy` 1.4.3 traffic captured on
// 2026-10-20 (a successful Gemini 3.8 Flash turn, a successful Claude
// Sonnet 4.6 turn, and a tool-call round trip). Three details are
// load-bearing and are pinned by the tests in this package:
//
//   - the request body is the wrapped envelope, not a bare
//     GenerateContentRequest;
//   - every frame is `data: {"response": {…}, "traceId": …}`;
//   - a tool result travels back as a functionResponse inside a *model*-role
//     content, and the model's thoughtSignature is replayed on the
//     functionCall part.
//
// 【反编译】 the `v1internal:` method names and the envelope member names are
// the reference's own; see docs/guide/cloudcode-dialect.md for the
// per-field derivation.
package geminicode

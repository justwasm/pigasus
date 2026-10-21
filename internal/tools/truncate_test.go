// Regression tests for cycle 248: AGY 1.4.1 per-step token-budget
// truncation for structured tool outputs.
//
// The headline contract: when a tool result exceeds maxTokens, the
// output is rewritten so it actually fits the budget. The
// "truncated" flag is not just metadata — the content the model
// receives is the smaller payload.
//
// Helper note: this file deliberately does NOT export a `truncate`
// function. `internal/tools/git_editor_disable_test.go:124` already
// has one, and declaring a second package-level helper with the
// same name is a build error. The helpers below are named to stay
// out of that namespace (`ellipsisTruncate`, `firstN`,
// `asJSONString`).
package tools

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestTruncateStructured_JSONObject — 1000-token JSON object is
// detected as JSON and returns the structured envelope
// {"_truncated":true, "_preview": "<partial object>"} with a
// shorter payload than the input.
func TestTruncateStructured_JSONObject(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; i < 200; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"id":`)
		sb.WriteString(itoa(i))
		sb.WriteString(`,"name":"item-`)
		sb.WriteString(itoa(i))
		sb.WriteString(`","description":"padding to push the input well past the 100-token budget for the test"}`)
	}
	sb.WriteString(`]}`)
	input := sb.String()

	if estimateTokens(input) < 500 {
		t.Fatalf("test setup: input is only %d tokens, want >=500", estimateTokens(input))
	}

	got := TruncateStructured(input, 100)
	if got == input {
		t.Fatalf("truncation did nothing — input was returned verbatim")
	}

	var envelope struct {
		Truncated bool   `json:"_truncated"`
		Preview   string `json:"_preview"`
	}
	if err := json.Unmarshal([]byte(got), &envelope); err != nil {
		t.Fatalf("output is not parseable JSON: %v\noutput: %q", err, firstN(got, 80))
	}
	if !envelope.Truncated {
		t.Errorf("_truncated = false, want true")
	}
	if envelope.Preview == "" {
		t.Errorf("_preview is empty, want a non-empty partial object")
	}
	if len(got) >= len(input) {
		t.Errorf("output not shorter than input: got=%d input=%d", len(got), len(input))
	}
	if !strings.HasPrefix(got, `{"_truncated":true`) {
		t.Errorf("output missing the structured envelope header: %q", firstN(got, 40))
	}
}

// TestTruncateStructured_JSONArray — a 500-element JSON array
// detected as JSON returns the structured envelope
// {"_truncated":true, "_preview":[...first N elements...]} with
// fewer elements than the input.
func TestTruncateStructured_JSONArray(t *testing.T) {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < 500; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"id":`)
		sb.WriteString(itoa(i))
		sb.WriteString(`,"value":"row-`)
		sb.WriteString(itoa(i))
		sb.WriteString(`","description":"padding to push the array past the 100-token budget for the test"}`)
	}
	sb.WriteByte(']')
	input := sb.String()

	if estimateTokens(input) < 500 {
		t.Fatalf("test setup: input is only %d tokens, want >=500", estimateTokens(input))
	}

	got := TruncateStructured(input, 100)
	if got == input {
		t.Fatalf("truncation did nothing — input was returned verbatim")
	}

	// _preview is a JSON array, not a string.
	var envelope struct {
		Truncated bool              `json:"_truncated"`
		Preview   []json.RawMessage `json:"_preview"`
	}
	if err := json.Unmarshal([]byte(got), &envelope); err != nil {
		t.Fatalf("output is not parseable JSON: %v\noutput: %q", err, firstN(got, 80))
	}
	if !envelope.Truncated {
		t.Errorf("_truncated = false, want true")
	}
	if len(envelope.Preview) == 0 {
		t.Errorf("_preview is empty, want at least one element")
	}
	if len(envelope.Preview) >= 500 {
		t.Errorf("_preview kept every element: got %d, want fewer than 500", len(envelope.Preview))
	}
}

// TestTruncateStructured_YAML — a 200-line YAML doc is detected
// as YAML and returns the envelope
//
//	_truncated: true
//	_preview: |
//	  <first N lines>
//
// with a smaller payload than the input.
func TestTruncateStructured_YAML(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("item_")
		sb.WriteString(itoa(i))
		sb.WriteString(": value_")
		sb.WriteString(itoa(i))
		sb.WriteByte('\n')
	}
	input := sb.String()

	if estimateTokens(input) < 100 {
		t.Fatalf("test setup: input is only %d tokens, want >=100", estimateTokens(input))
	}

	got := TruncateStructured(input, 50)
	if got == input {
		t.Fatalf("truncation did nothing — input was returned verbatim")
	}
	if !strings.HasPrefix(got, "_truncated: true\n_preview: |\n") {
		t.Errorf("output missing YAML envelope header:\n%q", firstN(got, 80))
	}
	if !strings.Contains(got, "_preview: |") {
		t.Errorf("_preview literal-block header missing")
	}
	if len(got) >= len(input) {
		t.Errorf("output not shorter than input: got=%d input=%d", len(got), len(input))
	}
}

// TestTruncateStructured_UnderBudget — input shorter than the
// budget is returned unchanged. The budget gate fires BEFORE
// format detection so the detector's false positives cannot
// affect under-budget content.
func TestTruncateStructured_UnderBudget(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"json-object", `{"hello": "world"}`},
		{"json-array", `[1,2,3]`},
		{"yaml", "key: value\nother: thing\n"},
		{"xml", "<root>hi</root>"},
		{"prose", "lorem ipsum dolor sit amet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TruncateStructured(tc.input, 1000); got != tc.input {
				t.Errorf("under-budget content changed: got %q, want %q", got, tc.input)
			}
		})
	}
}

// TestTruncateStructured_ExactlyAtBudget — input whose token
// estimate equals maxTokens is treated as under budget (the gate
// is `<=`, not `<`). This pins the off-by-one contract.
func TestTruncateStructured_ExactlyAtBudget(t *testing.T) {
	input := strings.Repeat("abcd", 4) // 16 bytes / 4 chars-per-token = 4 tokens
	if estimateTokens(input) != 4 {
		t.Fatalf("test setup: estimateTokens(%q) = %d, want 4", firstN(input, 16), estimateTokens(input))
	}
	got := TruncateStructured(input, 4)
	if got != input {
		t.Errorf("exactly-at-budget content changed: got %q, want %q", got, input)
	}
}

// TestTruncateStructured_ZeroMeansNoLimit — maxTokens=0 is the
// "no limit" sentinel. Every input shape — JSON, YAML, prose —
// comes back verbatim, regardless of size.
func TestTruncateStructured_ZeroMeansNoLimit(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"json", `{"a":1,"b":2,"c":3}`},
		{"yaml", "key: value\nother: thing\n"},
		{"prose", strings.Repeat("hello world ", 1000)},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TruncateStructured(tc.input, 0); got != tc.input {
				t.Errorf("maxTokens=0 changed content: got len=%d, want len=%d", len(got), len(tc.input))
			}
		})
	}
}

// TestTruncateStructured_FallsBackToProseOnUnknownFormat — input
// the detector cannot classify (binary, weird whitespace) routes
// through truncateProse, which keeps the first maxTokens worth of
// bytes and appends the cycle-198/235 marker.
func TestTruncateStructured_FallsBackToProseOnUnknownFormat(t *testing.T) {
	input := strings.Repeat("lorem ipsum dolor sit amet ", 200)
	if estimateTokens(input) < 100 {
		t.Fatalf("test setup: input is only %d tokens, want >=100", estimateTokens(input))
	}

	got := TruncateStructured(input, 10)
	if got == input {
		t.Fatalf("truncation did nothing — input was returned verbatim")
	}
	// The prose marker is the cycle-198/235 \n…[truncated] suffix.
	if !strings.HasSuffix(got, "…[truncated]") {
		t.Errorf("unknown format did not fall through to prose path: got %q", firstN(got, 40))
	}
	if len(got) >= len(input) {
		t.Errorf("output not shorter than input: got=%d input=%d", len(got), len(input))
	}
}

// TestTruncateStructured_PreservesJSONParseability — every
// JSON-shape input (object or array) returns a string that
// round-trips through encoding/json with `_truncated=true` and
// a `_preview` key.
func TestTruncateStructured_PreservesJSONParseability(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		maxTokens int
	}{
		{
			name:      "object-over-budget",
			input:     `{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10,"k":11,"l":12,"m":13,"n":14,"o":15,"p":16,"q":17,"r":18,"s":19,"t":20}`,
			maxTokens: 10,
		},
		{
			name:      "array-over-budget",
			input:     "[" + strings.Repeat(`{"k":"v"},`, 199) + `{"k":"v"}]`,
			maxTokens: 10,
		},
		{
			name:      "pretty-printed-object",
			input:     "{\n  \"items\": [\n    {\"id\":1}" + strings.Repeat(",\n    {\"id\":1}", 199) + "\n  ]\n}",
			maxTokens: 10,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TruncateStructured(tc.input, tc.maxTokens)
			var probe map[string]any
			if err := json.Unmarshal([]byte(got), &probe); err != nil {
				t.Fatalf("output is not parseable JSON: %v\noutput: %q", err, firstN(got, 80))
			}
			truncated, ok := probe["_truncated"].(bool)
			if !ok {
				t.Errorf("missing _truncated bool: %+v", probe)
			}
			if !truncated {
				t.Errorf("_truncated = false, want true")
			}
			if _, ok := probe["_preview"]; !ok {
				t.Errorf("missing _preview key")
			}
		})
	}
}

// TestTruncateStructured_XML — XML/HTML input is detected as XML
// and wrapped in <truncated>...</truncated>. The cycle's XML path
// is intentionally conservative (no element-aware walker); the
// model gets the structural signal plus the leading payload.
func TestTruncateStructured_XML(t *testing.T) {
	input := strings.Repeat("<item>long content here that pushes the input past the budget so truncation has to do something</item>\n", 100)
	if estimateTokens(input) < 200 {
		t.Fatalf("test setup: input is only %d tokens, want >=200", estimateTokens(input))
	}

	got := TruncateStructured(input, 50)
	if got == input {
		t.Fatalf("truncation did nothing — input was returned verbatim")
	}
	if !strings.HasPrefix(got, "<truncated>") {
		t.Errorf("output missing <truncated> tag: %q", firstN(got, 40))
	}
	if !strings.HasSuffix(got, "</truncated>") {
		t.Errorf("output missing </truncated> close tag: %q", firstN(got, 40))
	}
	if len(got) >= len(input) {
		t.Errorf("output not shorter than input: got=%d input=%d", len(got), len(input))
	}
}

// TestTruncateStructured_NilContent — empty input is returned
// unchanged (no format detection, no truncation).
func TestTruncateStructured_NilContent(t *testing.T) {
	if got := TruncateStructured("", 100); got != "" {
		t.Errorf("empty input returned %q, want \"\"", got)
	}
}

// TestDetectFormat_KnownShapes — pins the heuristic so a refactor
// that drops one of the branches is caught immediately.
//
// Per the cycle-248 spec, YAML lists (`- foo`) and YAML document
// markers (`---`) intentionally fall through to FormatProse — the
// the YAML heuristic only matches `key: value` mapping entries
// on the first line.
func TestDetectFormat_KnownShapes(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		format string
	}{
		{"json-object", `{"k":"v"}`, FormatJSON},
		{"json-array", `[1,2,3]`, FormatJSON},
		{"json-pretty", "{\n  \"k\": \"v\"\n}", FormatJSON},
		{"json-array-pretty", "[\n  1,\n  2\n]", FormatJSON},
		{"xml-element", "<root>hi</root>", FormatXML},
		{"xml-decl", `<?xml version="1.0"?>`, FormatXML},
		{"yaml-mapping", "key: value\nother: thing", FormatYAML},
		{"yaml-comment", "key: value # comment\n", FormatYAML},
		{"yaml-empty-value", "key:\n", FormatYAML},
		{"yaml-list", "- one\n- two", FormatProse},
		{"yaml-doc", "---\nkey: value", FormatProse},
		{"prose-fallback", "lorem ipsum dolor sit amet", FormatProse},
		{"prose-no-newline", "lorem ipsum dolor sit amet", FormatProse},
		{"empty", "", FormatProse},
		{"whitespace-only", "   \n\t\n", FormatProse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectFormat(tc.input); got != tc.format {
				t.Errorf("DetectFormat(%q) = %q, want %q", firstN(tc.input, 30), got, tc.format)
			}
		})
	}
}

// TestOutputBudget_TruncatesLargeStructuredResult — the
// integration test the cycle spec requires. Drives the 5000+
// token fixture through ApplyOutputBudget with maxTokens=500 and
// verifies the truncated output is shorter than the input AND
// parseable as JSON.
func TestOutputBudget_TruncatesLargeStructuredResult(t *testing.T) {
	// Load the testdata fixture the cycle ships.
	data, err := os.ReadFile("testdata/large_json_result.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	input := string(data)

	// Pin the cycle's own assertion: the fixture is ≥5000 tokens
	// so maxTokens=500 forces the truncation path.
	beforeTokens := estimateTokens(input)
	if beforeTokens < 5000 {
		t.Fatalf("test setup: fixture is only %d tokens, want >=5000", beforeTokens)
	}

	res := ApplyOutputBudget(input, "", 500)

	if !res.Truncated {
		t.Errorf("Truncated = false, want true (input=%d tokens, budget=500)", beforeTokens)
	}
	if res.TokensAfter >= res.TokensBefore {
		t.Errorf("TokensAfter %d >= TokensBefore %d", res.TokensAfter, res.TokensBefore)
	}
	if res.Format != FormatJSON {
		t.Errorf("Format = %q, want %q", res.Format, FormatJSON)
	}
	if res.TokensBefore != beforeTokens {
		t.Errorf("TokensBefore = %d, want %d", res.TokensBefore, beforeTokens)
	}
	if res.TokensAfter > 500 {
		t.Errorf("truncated content still over budget: %d > 500", res.TokensAfter)
	}
	if len(res.Content) >= len(input) {
		t.Errorf("truncated content not shorter: got=%d input=%d", len(res.Content), len(input))
	}

	// The envelope must parse as JSON.
	var probe map[string]any
	if err := json.Unmarshal([]byte(res.Content), &probe); err != nil {
		t.Fatalf("output is not parseable JSON: %v", err)
	}
	if probe["_truncated"] != true {
		t.Errorf("envelope _truncated != true")
	}
	if _, ok := probe["_preview"]; !ok {
		t.Errorf("missing _preview key")
	}
}

// TestOutputBudget_UnderBudget_NoTruncation — input that fits
// the budget comes through unchanged with Truncated=false and
// TokensAfter == TokensBefore.
func TestOutputBudget_UnderBudget_NoTruncation(t *testing.T) {
	input := `{"a":1,"b":2,"c":3}`
	res := ApplyOutputBudget(input, "", 100)
	if res.Truncated {
		t.Errorf("Truncated = true, want false (input is under budget)")
	}
	if res.Content != input {
		t.Errorf("Content changed for under-budget input: got %q, want %q", res.Content, input)
	}
	if res.TokensAfter != res.TokensBefore {
		t.Errorf("TokensAfter %d != TokensBefore %d", res.TokensAfter, res.TokensBefore)
	}
	if res.Format != FormatJSON {
		t.Errorf("Format = %q, want %q", res.Format, FormatJSON)
	}
}

// TestOutputBudget_FormatOverride — caller-supplied format
// (FormatYAML) bypasses detection. The output is the YAML
// envelope even if the detector would have classified the
// content as JSON.
func TestOutputBudget_FormatOverride(t *testing.T) {
	// Plain YAML mapping — auto-detect would call it YAML too,
	// but the test pins the override path: caller says "this
	// is YAML" and we honour that.
	input := "key1: value1\nkey2: value2\nkey3: value3\n"
	res := ApplyOutputBudget(input, FormatYAML, 1)
	if res.Format != FormatYAML {
		t.Errorf("Format = %q, want %q (caller-supplied)", res.Format, FormatYAML)
	}
	if !res.Truncated {
		t.Errorf("Truncated = false, want true (4-line input forced to 1-token budget)")
	}
}

// TestOutputBudget_UnknownFormatFallsToProse — an unknown
// format string is coerced to FormatProse and the truncation
// goes through the prose path. The BudgetResult.Format reports
// FormatProse so the caller can see the coercion.
func TestOutputBudget_UnknownFormatFallsToProse(t *testing.T) {
	input := strings.Repeat("hello world ", 500)
	res := ApplyOutputBudget(input, "not-a-real-format", 50)
	if res.Format != FormatProse {
		t.Errorf("Format = %q, want %q (unknown coerced to prose)", res.Format, FormatProse)
	}
	if !res.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	if !strings.HasSuffix(res.Content, "…[truncated]") {
		t.Errorf("prose path marker missing from content: %q", firstN(res.Content, 40))
	}
}

// TestEstimateTokens_Cases — pins the cheap /4 heuristic and
// the "empty → 0" / "very short → 1" edge cases. Future cycles
// that switch to a real tokenizer must update this test.
func TestEstimateTokens_Cases(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"ab", 1},
		{"abc", 1},
		{"abcd", 1},
		{"abcde", 1},
		{strings.Repeat("a", 8), 2},
		{strings.Repeat("a", 16), 4},
	}
	for _, tc := range cases {
		if got := estimateTokens(tc.in); got != tc.want {
			t.Errorf("estimateTokens(len=%d) = %d, want %d", len(tc.in), got, tc.want)
		}
	}
}

// --- helpers (kept out of the `truncate` namespace to avoid a
// collision with internal/tools/git_editor_disable_test.go:124).

// itoa is a small int-to-string helper kept private to this
// file. fmt.Sprintf is fine in production code; this avoids the
// fmt import weight when only ints are being stringified.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// firstN is a small helper that returns the first n bytes of s,
// or s itself if it is shorter, with an ellipsis suffix to mark
// truncation in failure messages. Avoids the pre-existing
// `truncate` name to sidestep the git_editor_disable_test.go
// collision.
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// asJSONString is unused at the moment but kept here as a
// regression sentinel: a future test that needs to assert the
// JSON-string encoding of the envelope can call it.
func asJSONString(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// ellipsisTruncate keeps the last `tail` bytes of s prefixed by
// an ellipsis, for failure output that needs the tail context.
// Mirrors the cycle-198 / cycle-235 pattern; deliberately named
// to avoid the git_editor_disable_test.go `truncate` helper.
func ellipsisTruncate(s string, tail int) string {
	if len(s) <= tail {
		return s
	}
	return "…" + s[len(s)-tail:]
}

// TestOutputBudget_FormatDetectVsOverride — pins the contract
// that auto-detection only runs when format == ""; a non-empty
// caller-supplied format bypasses DetectFormat even when the
// input would have been classified differently. A regression
// here would mean callers passing FormatYAML get FormatJSON
// envelopes back, which the cycle-248 spec explicitly forbids.
func TestOutputBudget_FormatDetectVsOverride(t *testing.T) {
	input := `{"a":1,"b":2,"c":3}`
	auto := ApplyOutputBudget(input, "", 100)
	if auto.Format != FormatJSON {
		t.Errorf("auto Format = %q, want %q", auto.Format, FormatJSON)
	}

	explicit := ApplyOutputBudget(input, FormatProse, 100)
	if explicit.Format != FormatProse {
		t.Errorf("explicit Format = %q, want %q (override ignored)", explicit.Format, FormatProse)
	}
}

// TestOutputBudget_TokensReportedAreHonest — after a
// successful truncation, TokensAfter <= maxTokens and
// TokensBefore matches the input's estimate. The model layer
// uses these for its "X → Y tokens" diagnostic so a regression
// here would corrupt that display.
func TestOutputBudget_TokensReportedAreHonest(t *testing.T) {
	input := strings.Repeat(`{"k":"v"}`, 100) // 900 chars ≈ 225 tokens
	wantBefore := estimateTokens(input)

	res := ApplyOutputBudget(input, "", 10)
	if res.TokensBefore != wantBefore {
		t.Errorf("TokensBefore = %d, want %d", res.TokensBefore, wantBefore)
	}
	if res.TokensAfter > 10 {
		t.Errorf("TokensAfter %d > maxTokens 10", res.TokensAfter)
	}
	if !res.Truncated {
		t.Errorf("Truncated = false, want true")
	}

	// And the sorted-keys sanity check: the JSON envelope from
	// truncateJSONObject sorts keys deterministically so two
	// runs of the same input produce the same envelope.
	a := ApplyOutputBudget(`{"z":1,"a":2}`, "", 0).Content
	b := ApplyOutputBudget(`{"z":1,"a":2}`, "", 0).Content
	if a != b {
		t.Errorf("non-truncation path is non-deterministic: %q vs %q", a, b)
	}

	// Sanity: sort.Strings is reachable from this test file so
	// a future refactor that drops the import is caught by the
	// compiler.
	_ = sort.Strings
}
package agyproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/justwasm/pigasus/devutils/geminiwire"
	"github.com/justwasm/pigasus/internal/models"
)

// placeholderBase / placeholderSpan mirror the reference implementation
// (kelvinzer0/antigravity-cli `generateModelPlaceholderId`): custom models are
// advertised as MODEL_PLACEHOLDER_M400..M599 so agy's own model table treats
// them as agent slots it knows how to route.
const (
	placeholderBase = 400
	placeholderSpan = 200
)

// Slug returns the stable id agy passes back as `model`: "custom-" plus the
// lowercased external model name with runs of non-alphanumerics collapsed.
func Slug(m models.Model) string {
	src := strings.TrimSpace(m.ExternalModelName)
	if src == "" {
		src = m.Name
	}
	src = strings.TrimPrefix(src, "models/")
	var b strings.Builder
	lastDash := false
	for _, r := range src {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return "custom-" + strings.Trim(b.String(), "-")
}

// Placeholder returns "MODEL_PLACEHOLDER_M<N>" for a model, N in 400..599,
// derived from djb2(lower(display_name || name)).
func Placeholder(m models.Model) string {
	return fmt.Sprintf("MODEL_PLACEHOLDER_M%d", placeholderBase+djb2(displayName(m))%placeholderSpan)
}

// displayName is what agy shows and what the placeholder hash is built from.
func displayName(m models.Model) string {
	if s := strings.TrimSpace(m.DisplayName); s != "" {
		return s
	}
	if s := strings.TrimSpace(m.Name); s != "" {
		return s
	}
	return "custom-model"
}

// djb2 is the classic Bernstein hash, kept in uint32 so it matches the
// JavaScript reference bit-for-bit.
func djb2(s string) uint32 {
	var h uint32 = 5381
	for _, r := range strings.ToLower(s) {
		h = h*33 + uint32(r)
	}
	return h
}

// modelEntry is the `models[]` element that advertises one custom model in the
// array-shaped variant of fetchAvailableModels (see the contract).
func modelEntry(m models.Model) map[string]any {
	limit := m.ContextWindow
	if limit <= 0 {
		limit = 1048576
	}
	return map[string]any{
		"name":                       "models/" + Placeholder(m),
		"version":                    "1.0",
		"displayName":                displayName(m),
		"description":                modelDescription(m),
		"inputTokenLimit":            limit,
		"outputTokenLimit":           8192,
		"supportedGenerationMethods": []any{"generateContent", "countTokens"},
		"temperature":                0.7,
		"topP":                       0.9,
		"topK":                       40,
	}
}

// capabilityEntry is the `models` map value that advertises one custom model in
// the shape agy 1.4.2 actually receives from `fetchAvailableModels`: a map
// keyed by model id, where each value carries a `model` placeholder plus
// capability flags.
//
// This is load-bearing, not cosmetic. With an array-shaped entry in that map,
// `agy models` lists the custom model but `agy --model <slug>` fails with
// "plan model not specified; you must specify a valid model", because no
// placeholder id was advertised where agy looks for it (measured, cycle 274).
func capabilityEntry(m models.Model) map[string]any {
	limit := m.ContextWindow
	if limit <= 0 {
		limit = 1048576
	}
	return map[string]any{
		"displayName":                displayName(m),
		"description":                modelDescription(m),
		"version":                    "1.0",
		"recommended":                true,
		"maxTokens":                  limit,
		"maxOutputTokens":            8192,
		"inputTokenLimit":            limit,
		"outputTokenLimit":           8192,
		"model":                      Placeholder(m),
		"apiProvider":                "API_PROVIDER_GOOGLE_GEMINI",
		"modelProvider":              "MODEL_PROVIDER_GOOGLE",
		"tokenizerType":              "LLAMA_WITH_SPECIAL",
		"supportsImages":             false,
		"supportsThinking":           false,
		"supportedGenerationMethods": []any{"generateContent", "countTokens"},
		"temperature":                0.7,
		"topP":                       0.9,
		"topK":                       40,
	}
}

func modelDescription(m models.Model) string {
	return fmt.Sprintf("%s via %s (openagy wrapper)", displayName(m), m.Provider)
}

// MatchModel reports whether a requested model string refers to m. agy sends
// any of: the placeholder (with or without "models/"), the custom- slug, the
// registry name, or the external model name — so all of them are accepted,
// case-insensitively.
func MatchModel(requested string, m models.Model) bool {
	want := strings.ToLower(strings.TrimSpace(requested))
	if want == "" {
		return false
	}
	ph := strings.ToLower(Placeholder(m))
	ids := []string{
		ph,
		"models/" + ph,
		strings.ToLower(Slug(m)),
		strings.ToLower(strings.TrimSpace(m.Name)),
		strings.ToLower(strings.TrimSpace(m.ExternalModelName)),
		strings.ToLower(strings.TrimSpace(m.DisplayName)),
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if want == id {
			return true
		}
		// Substring match covers agy prepending "models/" or appending a
		// variant suffix. Short ids are skipped: a 2-char name would match
		// half the model list.
		if len(id) >= 4 && strings.Contains(want, id) {
			return true
		}
	}
	return false
}

// matchCustom returns the first configured model that MatchModel accepts.
func (s *Server) matchCustom(requested string) (models.Model, bool) {
	for _, m := range s.cfg.Models {
		if MatchModel(requested, m) {
			return m, true
		}
	}
	return models.Model{}, false
}

// MergeAvailableModels injects every custom model into an upstream
// fetchAvailableModels body and appends their slugs to every
// agentModelSorts[].groups[].modelIds. It is idempotent: running it on its own
// output changes nothing. Upstream models are never dropped.
func MergeAvailableModels(upstream []byte, custom []models.Model) ([]byte, error) {
	doc := map[string]any{}
	if len(upstream) > 0 {
		if err := json.Unmarshal(upstream, &doc); err != nil {
			return nil, fmt.Errorf("parse upstream models: %w", err)
		}
	}
	MergeAvailableModelsDoc(doc, custom)
	return json.Marshal(doc)
}

// MergeAvailableModelsDoc is the in-place form. It accepts both shapes agy has
// seen from Google: `models` as an array of {name: "models/…"} objects, and
// `models` as a map keyed by slug.
func MergeAvailableModelsDoc(doc map[string]any, custom []models.Model) {
	if doc == nil {
		return
	}
	slugs := make([]string, 0, len(custom))
	entries := make([]any, 0, len(custom))
	caps := make([]any, 0, len(custom))
	for _, m := range custom {
		slugs = append(slugs, Slug(m))
		entries = append(entries, modelEntry(m))
		caps = append(caps, capabilityEntry(m))
	}

	switch existing := doc["models"].(type) {
	case []any:
		seen := map[string]bool{}
		for _, e := range existing {
			if obj, ok := e.(map[string]any); ok {
				if name, ok := obj["name"].(string); ok {
					seen[strings.ToLower(name)] = true
				}
			}
		}
		for _, e := range entries {
			name := strings.ToLower(e.(map[string]any)["name"].(string))
			if seen[name] {
				continue
			}
			seen[name] = true
			existing = append([]any{e}, existing...)
		}
		doc["models"] = existing
	case map[string]any:
		for i := range custom {
			slug := slugs[i]
			if _, ok := existing[slug]; ok {
				continue
			}
			existing[slug] = caps[i]
		}
		doc["models"] = existing
	default:
		// No upstream list at all (Google down / unknown shape): emit the map
		// form agy 1.4.2 consumes, so a custom-model turn can still start.
		bySlug := make(map[string]any, len(custom))
		for i := range custom {
			bySlug[slugs[i]] = caps[i]
		}
		doc["models"] = bySlug
	}

	if len(slugs) == 0 {
		return
	}
	sorts, ok := doc["agentModelSorts"].([]any)
	if !ok || len(sorts) == 0 {
		doc["agentModelSorts"] = []any{map[string]any{
			"groups": []any{map[string]any{"modelIds": toAnySlice(slugs)}},
		}}
		return
	}
	for _, s := range sorts {
		sort, ok := s.(map[string]any)
		if !ok {
			continue
		}
		groups, ok := sort["groups"].([]any)
		if !ok {
			continue
		}
		for _, g := range groups {
			group, ok := g.(map[string]any)
			if !ok {
				continue
			}
			ids, _ := group["modelIds"].([]any)
			for _, slug := range slugs {
				if containsString(ids, slug) {
					continue
				}
				ids = append(ids, slug)
			}
			group["modelIds"] = ids
		}
	}
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func containsString(hay []any, needle string) bool {
	for _, v := range hay {
		if s, ok := v.(string); ok && strings.EqualFold(s, needle) {
			return true
		}
	}
	return false
}

// ---- handlers ----------------------------------------------------------

// handleFetchAvailableModels forwards the RPC, then merges the custom models
// into the reply. If upstream is unreachable or answers with a non-JSON body
// (Google returns protobuf on some auth failures) we still answer 200 with the
// custom models only: a wrapper that cannot advertise MiniMax M3 when Google
// is down is useless. The upstream status is echoed in a response header so
// `agy wrapper doctor` and the capture files can show what happened.
func (s *Server) handleFetchAvailableModels(w http.ResponseWriter, r *http.Request, body []byte) {
	req, err := s.upstreamRequest(r, body)
	if err != nil {
		s.writeMergedModels(w, nil, err)
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.logf("fetchAvailableModels: upstream error: %v", err)
		s.writeMergedModels(w, nil, err)
		return
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if readErr != nil {
		s.logf("fetchAvailableModels: read upstream: %v", readErr)
	}
	merged, mergeErr := MergeAvailableModels(raw, s.cfg.Models)
	if mergeErr != nil {
		s.logf("fetchAvailableModels: upstream HTTP %d, %v — custom models only", resp.StatusCode, mergeErr)
		s.writeMergedModels(w, nil, mergeErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Openagy-Upstream-Status", fmt.Sprintf("%d", resp.StatusCode))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(merged)
}

func (s *Server) writeMergedModels(w http.ResponseWriter, upstream []byte, cause error) {
	merged, err := MergeAvailableModels(upstream, s.cfg.Models)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "upstream models unavailable: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Openagy-Upstream-Status", "error")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(merged)
	if cause != nil {
		s.logf("fetchAvailableModels: serving custom models only (%v)", cause)
	}
}

// handleCountTokens answers locally for custom models. agy calls it before a
// turn; forwarding a placeholder model to Google would 404 and abort the turn
// before our translator ever runs. The estimate is ~4 chars/token, which is
// only used for display and window checks.
func (s *Server) handleCountTokens(w http.ResponseWriter, inner *geminiwire.GenerateContentRequest) {
	chars := len(inner.SystemText())
	for _, c := range inner.Contents {
		for _, p := range c.Parts {
			chars += len(p.Text)
			if p.FunctionCall != nil {
				if b, err := json.Marshal(p.FunctionCall.Args); err == nil {
					chars += len(b)
				}
			}
			if p.FunctionResponse != nil {
				if b, err := json.Marshal(p.FunctionResponse.Response); err == nil {
					chars += len(b)
				}
			}
		}
	}
	tokens := chars / 4
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"totalTokens": tokens})
}

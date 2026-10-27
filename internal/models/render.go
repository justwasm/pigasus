// render.go — the shared rendering of the provider and model tables.
//
// The clone's CLI and the wrapper's CLI read the same file and show the
// same thing; the rendering lives here so the two cannot drift.
package models

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// SortedProviderIDs returns the provider ids in a stable order.
func SortedProviderIDs(providers map[string]ProviderConfig) []string {
	out := make([]string, 0, len(providers))
	for id := range providers {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// MaskAPIKey renders a credential for display.
//
// An environment reference ("$NAME") is shown as it is written: it names
// an environment variable, not a secret. A literal is masked — the tables
// are printed into terminals and into logs.
func MaskAPIKey(value string) string {
	t := strings.TrimSpace(value)
	if t == "" {
		return "(not set)"
	}
	if strings.HasPrefix(t, "$") {
		return t
	}
	return "**** (literal, hidden)"
}

// RenderProviderTable writes the provider table: one row per provider and
// one indented row per model it serves.
func RenderProviderTable(w io.Writer, providers map[string]ProviderConfig) {
	if len(providers) == 0 {
		fmt.Fprintln(w, "no providers configured")
		return
	}
	for _, id := range SortedProviderIDs(providers) {
		p := providers[id]
		endpoint := strings.TrimSpace(p.BaseURL)
		if endpoint == "" {
			endpoint = strings.TrimSpace(p.APIURL)
		}
		fmt.Fprintf(w, "%-20s %-24s type=%-10s base=%-32s models=%d\n",
			id, p.Label(), p.WireType(), endpoint, len(p.Models))
		for _, m := range p.Models {
			wire := strings.TrimSpace(m.Model)
			if wire == "" {
				wire = m.ID
			}
			fmt.Fprintf(w, "%-20s   %-20s model=%-20s ctx=%d\n", "", m.ID, wire, m.ContextWindow)
		}
	}
}

// RenderModelTable writes the model table: one row per configured model.
func RenderModelTable(w io.Writer, entries []Model) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "no models configured")
		return
	}
	for _, m := range entries {
		display := strings.TrimSpace(m.DisplayName)
		if display == "" {
			display = m.Name
		}
		provider := strings.TrimSpace(m.ProviderID)
		if provider == "" {
			provider = strings.TrimSpace(m.Provider)
		}
		fmt.Fprintf(w, "%-20s %-24s provider=%-16s model=%-24s ctx=%d\n",
			m.Name, display, provider, m.ExternalModelName, m.ContextWindow)
	}
}

// RenderProviderDetail writes one provider's detail block.
func RenderProviderDetail(w io.Writer, id string, p ProviderConfig) {
	fmt.Fprintf(w, "provider: %s\n", id)
	fmt.Fprintf(w, "  name:        %s\n", p.Label())
	fmt.Fprintf(w, "  type:        %s\n", p.WireType())
	fmt.Fprintf(w, "  base_url:    %s\n", orNone(p.BaseURL))
	fmt.Fprintf(w, "  api_url:     %s\n", orNone(p.APIURL))
	fmt.Fprintf(w, "  endpoint:    %s\n", orNone(p.Endpoint()))
	fmt.Fprintf(w, "  api_key:     %s\n", MaskAPIKey(p.APIKey))
	if p.TimeoutSeconds > 0 {
		fmt.Fprintf(w, "  timeout:     %ds\n", p.TimeoutSeconds)
	}
	if p.MaxRetries > 0 {
		fmt.Fprintf(w, "  retries:     %d\n", p.MaxRetries)
	}
	for k, v := range p.ExtraHeaders {
		fmt.Fprintf(w, "  header:      %s: %s\n", k, v)
	}
	if len(p.Models) == 0 {
		fmt.Fprintln(w, "  models:      (none)")
		return
	}
	fmt.Fprintln(w, "  models:")
	for _, m := range p.Models {
		wire := strings.TrimSpace(m.Model)
		if wire == "" {
			wire = m.ID
		}
		label := strings.TrimSpace(m.Name)
		if label == "" {
			label = m.ID
		}
		fmt.Fprintf(w, "    %-20s %-24s model=%-24s ctx=%d\n", m.ID, label, wire, m.ContextWindow)
	}
}

// RenderModelDetail writes one model's detail block.
func RenderModelDetail(w io.Writer, m Model) {
	fmt.Fprintf(w, "model: %s\n", m.Name)
	display := strings.TrimSpace(m.DisplayName)
	if display == "" {
		display = m.Name
	}
	provider := strings.TrimSpace(m.ProviderID)
	if provider == "" {
		provider = strings.TrimSpace(m.Provider)
	}
	fmt.Fprintf(w, "  name:         %s\n", display)
	fmt.Fprintf(w, "  provider:     %s\n", orNone(provider))
	fmt.Fprintf(w, "  wire type:    %s\n", orNone(m.Type))
	fmt.Fprintf(w, "  upstream id:  %s\n", orNone(m.ExternalModelName))
	fmt.Fprintf(w, "  endpoint:     %s\n", orNone(m.APIURL))
	fmt.Fprintf(w, "  api_key:      %s\n", MaskAPIKey(m.APIKey))
	if m.ContextWindow > 0 {
		fmt.Fprintf(w, "  context:      %d\n", m.ContextWindow)
	}
	if m.DefaultMaxTokens > 0 {
		fmt.Fprintf(w, "  max tokens:   %d\n", m.DefaultMaxTokens)
	}
	if m.ReasoningEffort != "" {
		fmt.Fprintf(w, "  effort:       %s\n", m.ReasoningEffort)
	}
	if len(m.ReasoningEfforts) > 0 {
		fmt.Fprintf(w, "  tiers:        %s\n", strings.Join(m.ReasoningEfforts, ", "))
	}
	// The per-model attributes: what the Web UI's model picker reads
	// off this model. A model that states none of them prints none of
	// them — the lines are the model's own words.
	if m.SupportsImages != nil {
		fmt.Fprintf(w, "  images:       %t\n", *m.SupportsImages)
	}
	if len(m.SupportedMimeTypes) > 0 {
		fmt.Fprintf(w, "  mime types:   %s\n", strings.Join(m.SupportedMimeTypes, ", "))
	}
	if m.Disabled {
		fmt.Fprintf(w, "  disabled:     true\n")
	}
	if m.QuotaInfo != nil {
		if m.QuotaInfo.RemainingFraction != nil {
			fmt.Fprintf(w, "  quota:        %.2f left\n", *m.QuotaInfo.RemainingFraction)
		}
		if reset := strings.TrimSpace(m.QuotaInfo.ResetTime); reset != "" {
			fmt.Fprintf(w, "  quota reset:  %s\n", reset)
		}
	}
	if m.SupportsThoughtCirculation != nil && *m.SupportsThoughtCirculation {
		fmt.Fprintf(w, "  thoughts:     circulated\n")
	}
	if m.ModelURL != "" {
		fmt.Fprintf(w, "  model url:    %s\n", m.ModelURL)
	}
	if m.TagTitle != "" {
		fmt.Fprintf(w, "  tag:          %s\n", m.TagTitle)
	}
	if m.TagDescription != "" {
		fmt.Fprintf(w, "  tag note:     %s\n", m.TagDescription)
	}
	if m.EffortSupported != nil {
		fmt.Fprintf(w, "  effort level: %t\n", *m.EffortSupported)
	}
}

// orNone renders an empty value as a placeholder.
func orNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(not set)"
	}
	return v
}

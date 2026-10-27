// actions.go — provider and model validation and save logic for the /provider panel.
package providerpanel

import (
	"errors"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

// ValidateAndSaveProvider validates a provider form and writes it to the configuration.
func ValidateAndSaveProvider(
	ed *models.Editor,
	providers map[string]models.ProviderConfig,
	form *Form,
) (id string, p models.ProviderConfig, probeModel string, err error) {
	if form == nil {
		return "", models.ProviderConfig{}, "", errors.New("the provider needs an id")
	}
	id = strings.TrimSpace(form.Provider)
	if id == "" {
		id = strings.TrimSpace(form.Value(FieldID))
	}
	if id == "" {
		return "", models.ProviderConfig{}, "", errors.New("the provider needs an id")
	}
	existing, ok := providers[id]
	if !ok {
		existing = models.ProviderConfig{ID: id}
	}
	p, ok = ProviderConfig(form, existing)
	if !ok {
		return "", models.ProviderConfig{}, "", errors.New("the provider needs an id")
	}
	if p.Endpoint() == "" {
		return "", models.ProviderConfig{}, "", errors.New("the provider needs a base URL or an API URL")
	}
	if ed != nil {
		if err := ed.PutProvider(id, p); err != nil {
			return "", models.ProviderConfig{}, "", err
		}
	}
	return id, p, ProbeModel(p.Models), nil
}

// ValidateAndSaveModel validates a model form and writes it to the configuration.
func ValidateAndSaveModel(
	ed *models.Editor,
	providers map[string]models.ProviderConfig,
	form *Form,
) (provider string, wireModel string, err error) {
	if form == nil {
		return "", "", errors.New("the model needs a provider")
	}
	provider = strings.TrimSpace(form.Provider)
	if provider == "" {
		return "", "", errors.New("the model needs a provider")
	}
	if _, ok := providers[provider]; !ok {
		return "", "", fmt.Errorf("no provider named %q", provider)
	}
	id := strings.TrimSpace(form.Model)
	if id == "" {
		id = strings.TrimSpace(form.Value(FieldID))
	}
	if id == "" {
		return "", "", errors.New("the model needs an id")
	}
	entry := models.ProviderModel{ID: id}
	for _, m := range providers[provider].Models {
		if m.ID == id {
			entry = m
			break
		}
	}
	entry, ok := ProviderModel(form, entry)
	if !ok {
		return "", "", errors.New("the model needs an id")
	}
	// A tier nobody can select is a typo, and the file is left alone.
	if err := models.ValidateEfforts(entry.ReasoningEfforts); err != nil {
		return "", "", err
	}
	if ed != nil {
		if err := ed.PutModel(provider, entry); err != nil {
			return "", "", err
		}
	}
	wire := strings.TrimSpace(entry.Model)
	if wire == "" {
		wire = id
	}
	return provider, wire, nil
}

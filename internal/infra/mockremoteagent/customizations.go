// customizations.go — ListCustomizations, the proxy's skills source.
//
// The reference returns the environment's skills as
// exa.cortex_pb.WorkflowSpec records. The server discovers the skills the
// same way the local CLI does (builtin bundle, shared library, plugin
// trees) and maps each to that shape.
package mockremoteagent

import (
	"encoding/json"
	"net/http"

	"github.com/justwasm/pigasus/internal/features/tools/skills"
)

func (s *Service) handleListCustomizations(w http.ResponseWriter, body []byte) {
	var req ListCustomizationsRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	discovered := skills.Discover()
	specs := make([]any, 0, len(discovered))
	for _, skill := range discovered {
		spec := map[string]any{
			"path":        skill.Path,
			"name":        skill.Name,
			"description": skill.Description,
			"isBuiltin":   skill.Builtin,
			"icon":        skill.Icon,
		}
		if skill.Plugin != "" {
			spec["pluginName"] = skill.Plugin
		}
		if !skill.ModelInvocable {
			spec["disableModelInvocation"] = true
		}
		specs = append(specs, spec)
	}
	writeJSON(w, map[string]any{"skills": specs})
}

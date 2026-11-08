package mockremoteagent

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestListCustomizationsReturnsWorkflowSpecs pins the skills payload shape:
// each entry carries the WorkflowSpec fields the client maps.
func TestListCustomizationsReturnsWorkflowSpecs(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	rec := httptest.NewRecorder()
	srv.service.handleListCustomizations(rec, []byte(`{"workspaceUri":"file:///ws"}`))

	var got struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Skills == nil {
		t.Fatal("skills key is absent")
	}
	if len(got.Skills) > 0 {
		first := got.Skills[0]
		if _, ok := first["name"].(string); !ok {
			t.Errorf("skill has no name: %+v", first)
		}
		if _, ok := first["path"].(string); !ok {
			t.Errorf("skill has no path: %+v", first)
		}
	}
}

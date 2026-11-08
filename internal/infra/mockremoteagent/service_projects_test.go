package mockremoteagent

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestListProjectsReturnsSignedInAccount pins the project list to the
// caller's account. The real remote service only lists the signed-in user's
// projects; returning every cached account made agy pick another account's
// project and fail project resolution with
// "cloud conversations are not enabled for this user".
func TestListProjectsReturnsSignedInAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGY_USER_EMAIL", "stakingminer@gmail.com")

	cacheDir := filepath.Join(home, ".gemini", "antigravity-cli", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cache := `{
		"stakingminer@gmail.com": "5375b8e0-06d4-4393-8580-2f833e111343",
		"bastionturbofish@gmail.com": "f3168bbc-a23d-499d-b9c0-97971fc2ead1"
	}`
	if err := os.WriteFile(filepath.Join(cacheDir, "default_cloud_project_ids.json"), []byte(cache), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	svc := NewService(NewCascadeManager())
	rec := httptest.NewRecorder()
	svc.handleListProjects(rec, httptest.NewRequest("GET", "/v1alpha/remote:listProjects", nil))

	var resp ListProjectsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Projects) != 1 {
		t.Fatalf("projects = %+v, want exactly the signed-in account's project", resp.Projects)
	}
	if got, want := resp.Projects[0].ProjectID, "5375b8e0-06d4-4393-8580-2f833e111343"; got != want {
		t.Errorf("project id = %q, want %q", got, want)
	}
}

// TestListProjectsMatchesRequestBearer pins the account to the bearer the
// client actually sent, not the process's own token: agy and the mock can be
// signed in to different cached accounts.
func TestListProjectsMatchesRequestBearer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGY_USER_EMAIL", "other@gmail.com")

	cacheDir := filepath.Join(home, ".gemini", "antigravity-cli", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cache := `{
		"caller@gmail.com": "5375b8e0-06d4-4393-8580-2f833e111343",
		"other@gmail.com": "f3168bbc-a23d-499d-b9c0-97971fc2ead1"
	}`
	if err := os.WriteFile(filepath.Join(cacheDir, "default_cloud_project_ids.json"), []byte(cache), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	tokenPath := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("mkdir token dir: %v", err)
	}
	token := `{"token":{"access_token":"caller-token"},"auth_method":"consumer","id_token_email":"caller@gmail.com"}`
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	svc := NewService(NewCascadeManager())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1alpha/remote:listProjects", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	svc.handleListProjects(rec, req)

	var resp ListProjectsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Projects) != 1 {
		t.Fatalf("projects = %+v, want the caller's project", resp.Projects)
	}
	if got, want := resp.Projects[0].ProjectID, "5375b8e0-06d4-4393-8580-2f833e111343"; got != want {
		t.Errorf("project id = %q, want %q", got, want)
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeRunner struct{}

func (f fakeRunner) CreateAdminRun(request RunRequest, principal Principal, source string) (RunMeta, error) {
	return RunMeta{
		RunID:      "run_fake_admin",
		Status:     "queued",
		CreatorSub: principal.Subject,
		Request:    request,
		CreatedAt:  nowRFC3339(),
	}, nil
}

func (f fakeRunner) CreateQuickTest(request QuickTestRequest, ipHash, uaHash string) (RunMeta, error) {
	return RunMeta{
		RunID:     "run_fake_user",
		Status:    "queued",
		Request:   RunRequest{Model: request.TargetModel},
		CreatedAt: nowRFC3339(),
	}, nil
}

func TestRouterHealthz(t *testing.T) {
	store, err := NewMemoryFileStore("")
	if err != nil {
		t.Fatalf("NewMemoryFileStore: %v", err)
	}
	auth := NewAuth(nil, ServerConfig{
		Security: SecurityConfig{AdminToken: "secret-token"},
	})
	api := NewAPI(auth, store, fakeRunner{}, nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
}

func TestRouterAdminAuthAndRun(t *testing.T) {
	store, err := NewMemoryFileStore("")
	if err != nil {
		t.Fatalf("NewMemoryFileStore: %v", err)
	}
	auth := NewAuth(nil, ServerConfig{
		Security: SecurityConfig{AdminToken: "secret-token"},
	})
	api := NewAPI(auth, store, fakeRunner{}, nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	body := map[string]any{
		"endpoint": "https://api.anthropic.com",
		"model":    "claude-sonnet-4-5-20250929",
		"suite":    []string{"authenticity"},
	}
	rawBody, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/admin/runs", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("admin create without auth failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req2, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/admin/runs", bytes.NewReader(rawBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Admin-Token", "secret-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("admin create with token failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp2.StatusCode)
	}
}

func TestRouterQuickTest(t *testing.T) {
	store, err := NewMemoryFileStore("")
	if err != nil {
		t.Fatalf("NewMemoryFileStore: %v", err)
	}
	auth := NewAuth(nil, ServerConfig{
		Security: SecurityConfig{AdminToken: "secret-token"},
	})
	api := NewAPI(auth, store, fakeRunner{}, nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	body := map[string]any{
		"scenario_id":  "official-model-integrity",
		"target_model": "claude-sonnet-4-5-20250929",
	}
	rawBody, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/user/quick-test", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("quick test request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
}

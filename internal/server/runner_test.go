package server

import "testing"

func TestScenarioToRunRequest(t *testing.T) {
	cfg := DefaultServerConfig()
	request, err := scenarioToRunRequest(QuickTestRequest{
		ScenarioID:  "official-model-integrity",
		TargetModel: "claude-sonnet-4-5-20250929",
		StrictLevel: "forensic",
	}, cfg)
	if err != nil {
		t.Fatalf("scenarioToRunRequest returned error: %v", err)
	}
	if request.Model == "" {
		t.Fatalf("expected model to be set")
	}
	if len(request.Suites) < 3 {
		t.Fatalf("expected several suites, got %v", request.Suites)
	}
	if request.ForensicsLevel != "forensic" {
		t.Fatalf("expected forensic level, got %s", request.ForensicsLevel)
	}
}

func TestScenarioToRunRequestRejectUnknownScenario(t *testing.T) {
	cfg := DefaultServerConfig()
	_, err := scenarioToRunRequest(QuickTestRequest{
		ScenarioID:  "unknown",
		TargetModel: "claude-sonnet-4-5-20250929",
	}, cfg)
	if err == nil {
		t.Fatalf("expected error for unsupported scenario")
	}
}

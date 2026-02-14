package server

import (
	"time"

	"real-llm/internal/probe"
)

type Principal struct {
	Subject  string `json:"subject"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type RunRequest struct {
	Endpoint         string   `json:"endpoint"`
	Model            string   `json:"model"`
	Suites           []string `json:"suite"`
	ForensicsLevel   string   `json:"forensics_level,omitempty"`
	DryRun           bool     `json:"dry_run,omitempty"`
	HardGate         *bool    `json:"hard_gate,omitempty"`
	BudgetCapUSD     float64  `json:"budget_cap,omitempty"`
	TimeoutSec       int      `json:"timeout_sec,omitempty"`
	Strict           bool     `json:"strict,omitempty"`
	AnthropicVersion string   `json:"anthropic_version,omitempty"`
	AnthropicBeta    string   `json:"anthropic_beta,omitempty"`
}

type QuickTestRequest struct {
	ScenarioID  string `json:"scenario_id"`
	TargetModel string `json:"target_model"`
	StrictLevel string `json:"strict_level,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
}

type RunMeta struct {
	RunID         string         `json:"run_id"`
	Status        string         `json:"status"`
	CreatorType   string         `json:"creator_type"`
	CreatorSub    string         `json:"creator_sub,omitempty"`
	CreatorEmail  string         `json:"creator_email,omitempty"`
	Source        string         `json:"source"`
	Request       RunRequest     `json:"request"`
	StartedAt     string         `json:"started_at,omitempty"`
	FinishedAt    string         `json:"finished_at,omitempty"`
	CreatedAt     string         `json:"created_at"`
	Error         string         `json:"error,omitempty"`
	Report        *probe.Report  `json:"report,omitempty"`
	Risk          RiskSnapshot   `json:"risk"`
	KeyUsage      KeyUsageRecord `json:"key_usage"`
	EstimatedCost float64        `json:"estimated_cost_usd"`
}

type RiskSnapshot struct {
	TrustScore      float64          `json:"trust_score_final"`
	TrustScoreRaw   float64          `json:"trust_score_raw"`
	HardGateFail    bool             `json:"hard_gate_fail"`
	HardGateHits    []map[string]any `json:"hard_gate_hits,omitempty"`
	SpoofRiskScore  float64          `json:"spoof_risk_score"`
	LeakCount       float64          `json:"leak_count"`
	HiddenToolCalls float64          `json:"hidden_tool_signal_count"`
}

type KeyUsageRecord struct {
	RunID            string  `json:"run_id"`
	KeyLabel         string  `json:"key_label"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	BlockedReason    string  `json:"blocked_reason,omitempty"`
}

type AuditEvent struct {
	Timestamp string `json:"timestamp"`
	RunID     string `json:"run_id,omitempty"`
	ActorType string `json:"actor_type"`
	ActorSub  string `json:"actor_sub,omitempty"`
	Action    string `json:"action"`
	Result    string `json:"result"`
	IPHash    string `json:"ip_hash,omitempty"`
	UAHash    string `json:"ua_hash,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type RunEvent struct {
	Seq       int64          `json:"seq"`
	Timestamp string         `json:"timestamp"`
	Stage     string         `json:"stage"`
	Message   string         `json:"message"`
	Data      map[string]any `json:"data,omitempty"`
}

type MetricsOverview struct {
	GeneratedAt      string  `json:"generated_at"`
	TotalRuns        int     `json:"total_runs"`
	RunningRuns      int     `json:"running_runs"`
	PassRuns         int     `json:"pass_runs"`
	WarnRuns         int     `json:"warn_runs"`
	FailRuns         int     `json:"fail_runs"`
	HardGateHits     int     `json:"hard_gate_hits"`
	AverageDuration  int64   `json:"average_duration_ms"`
	AverageTrust     float64 `json:"average_trust_score"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

type StoreSnapshot struct {
	Runs   []RunMeta    `json:"runs"`
	Events []RunEvent   `json:"events"`
	Audit  []AuditEvent `json:"audit"`
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

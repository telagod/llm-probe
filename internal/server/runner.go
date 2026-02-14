package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"real-llm/internal/anthropic"
	"real-llm/internal/probe"
)

type RunManager struct {
	cfg        ServerConfig
	store      Store
	budget     *BudgetManager
	obs        *Observability
	queue      chan queuedRun
	wg         sync.WaitGroup
	quickLimit *ipRateLimiter
}

type RunnerService interface {
	CreateAdminRun(request RunRequest, principal Principal, source string) (RunMeta, error)
	CreateQuickTest(request QuickTestRequest, ipHash, uaHash string) (RunMeta, error)
}

type queuedRun struct {
	RunID       string
	Request     RunRequest
	Creator     Principal
	CreatorType string
	Source      string
}

func NewRunManager(cfg ServerConfig, store Store, budget *BudgetManager, obs *Observability) *RunManager {
	maxParallel := cfg.Budget.MaxParallelRuns
	if maxParallel <= 0 {
		maxParallel = 2
	}
	manager := &RunManager{
		cfg:        cfg,
		store:      store,
		budget:     budget,
		obs:        obs,
		queue:      make(chan queuedRun, maxParallel*8),
		quickLimit: newIPRateLimiter(cfg.Limits.QuickTestRPM),
	}
	for i := 0; i < maxParallel; i++ {
		manager.wg.Add(1)
		go func() {
			defer manager.wg.Done()
			manager.worker()
		}()
	}
	return manager
}

func (m *RunManager) Shutdown() {
	close(m.queue)
	m.wg.Wait()
}

func (m *RunManager) CreateAdminRun(request RunRequest, principal Principal, source string) (RunMeta, error) {
	if strings.TrimSpace(request.Endpoint) == "" {
		request.Endpoint = "https://api.anthropic.com"
	}
	if strings.TrimSpace(request.Model) == "" {
		return RunMeta{}, errors.New("model is required")
	}
	request.ForensicsLevel = normalizeForensicsLevel(request.ForensicsLevel)
	if request.TimeoutSec <= 0 {
		request.TimeoutSec = m.cfg.Budget.DefaultTimeoutSec
	}
	if request.BudgetCapUSD <= 0 {
		request.BudgetCapUSD = m.cfg.Budget.DefaultRunMaxUSD
	}
	if len(request.Suites) == 0 {
		request.Suites = probe.DefaultSuiteOrder()
	}
	runID, err := randomID("run")
	if err != nil {
		return RunMeta{}, err
	}
	meta := RunMeta{
		RunID:       runID,
		Status:      "queued",
		Source:      source,
		CreatorType: "admin",
		CreatorSub:  principal.Subject,
		Request:     request,
		CreatedAt:   nowRFC3339(),
	}
	if err := m.store.CreateRun(meta); err != nil {
		return RunMeta{}, err
	}
	_, _ = m.store.AppendRunEvent(runID, "queue", "run queued", map[string]any{
		"source": source,
	})
	_ = m.store.AppendAudit(AuditEvent{
		Timestamp: nowRFC3339(),
		RunID:     runID,
		ActorType: "admin",
		ActorSub:  principal.Subject,
		Action:    "run.create",
		Result:    "queued",
	})
	m.queue <- queuedRun{
		RunID:       runID,
		Request:     request,
		Creator:     principal,
		CreatorType: "admin",
		Source:      source,
	}
	return meta, nil
}

func (m *RunManager) CreateQuickTest(request QuickTestRequest, ipHash, uaHash string) (RunMeta, error) {
	if !m.quickLimit.Allow(ipHash) {
		if m.obs != nil {
			m.obs.MarkBudgetBlocked(context.Background(), "quick_test_rate_limit")
		}
		_ = m.store.AppendAudit(AuditEvent{
			Timestamp: nowRFC3339(),
			ActorType: "user",
			Action:    "quick_test.reject",
			Result:    "rate_limited",
			IPHash:    ipHash,
			UAHash:    uaHash,
		})
		return RunMeta{}, errors.New("quick test rate limit reached")
	}
	runRequest, err := scenarioToRunRequest(request, m.cfg)
	if err != nil {
		return RunMeta{}, err
	}
	runID, err := randomID("run")
	if err != nil {
		return RunMeta{}, err
	}
	meta := RunMeta{
		RunID:       runID,
		Status:      "queued",
		Source:      "user.quick_test",
		CreatorType: "user",
		Request:     runRequest,
		CreatedAt:   nowRFC3339(),
	}
	if err := m.store.CreateRun(meta); err != nil {
		return RunMeta{}, err
	}
	_, _ = m.store.AppendRunEvent(runID, "queue", "quick test queued", map[string]any{
		"scenario_id": request.ScenarioID,
	})
	_ = m.store.AppendAudit(AuditEvent{
		Timestamp: nowRFC3339(),
		RunID:     runID,
		ActorType: "user",
		Action:    "quick_test.create",
		Result:    "queued",
		IPHash:    ipHash,
		UAHash:    uaHash,
		Detail:    request.ScenarioID,
	})
	m.queue <- queuedRun{
		RunID:       runID,
		Request:     runRequest,
		CreatorType: "user",
		Source:      "user.quick_test",
	}
	return meta, nil
}

func (m *RunManager) worker() {
	for queued := range m.queue {
		m.executeRun(queued)
	}
}

func (m *RunManager) executeRun(queued queuedRun) {
	startedAt := nowRFC3339()
	_, _ = m.store.UpdateRun(queued.RunID, func(meta *RunMeta) {
		meta.Status = "running"
		meta.StartedAt = startedAt
	})
	_, _ = m.store.AppendRunEvent(queued.RunID, "start", "run started", nil)

	if queued.Request.DryRun {
		report := buildDryRunReport(queued.Request)
		risk := riskFromReport(report)
		status := reportOverallStatus(report)
		usage := KeyUsageRecord{
			RunID:            queued.RunID,
			KeyLabel:         "dry-run",
			EstimatedCostUSD: 0,
		}
		_, _ = m.store.UpdateRun(queued.RunID, func(meta *RunMeta) {
			meta.Status = status
			meta.FinishedAt = nowRFC3339()
			meta.Report = &report
			meta.EstimatedCost = 0
			meta.KeyUsage = usage
			meta.Risk = risk
		})
		_, _ = m.store.AppendRunEvent(queued.RunID, "completed", "dry-run completed", map[string]any{
			"status": status,
		})
		if m.obs != nil {
			m.obs.MarkRun(context.Background(), status)
		}
		return
	}

	lease, err := m.budget.Acquire(queued.Request.BudgetCapUSD)
	if err != nil {
		_, _ = m.store.UpdateRun(queued.RunID, func(meta *RunMeta) {
			meta.Status = "fail"
			meta.Error = "budget key unavailable: " + err.Error()
			meta.FinishedAt = nowRFC3339()
			meta.KeyUsage = KeyUsageRecord{
				RunID:         queued.RunID,
				BlockedReason: "budget_key_unavailable",
			}
		})
		_, _ = m.store.AppendRunEvent(queued.RunID, "error", "budget key unavailable", map[string]any{"error": err.Error()})
		if m.obs != nil {
			m.obs.MarkRun(context.Background(), "fail")
			m.obs.MarkBudgetBlocked(context.Background(), "key_unavailable")
		}
		return
	}

	timeout := time.Duration(queued.Request.TimeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client := anthropic.NewClient(anthropic.Config{
		BaseURL:          queued.Request.Endpoint,
		APIKey:           lease.APIKey,
		AnthropicVersion: firstNonEmpty(queued.Request.AnthropicVersion, "2023-06-01"),
		AnthropicBeta:    queued.Request.AnthropicBeta,
		Timeout:          time.Duration(minInt(queued.Request.TimeoutSec, 120)) * time.Second,
	})
	probeCfg := probe.RunConfig{
		Model:              queued.Request.Model,
		DeepProbe:          true,
		ForensicsLevel:     normalizeForensicsLevel(queued.Request.ForensicsLevel),
		EnableTrustScore:   true,
		HardGate:           valueOrDefaultBool(queued.Request.HardGate, true),
		ScoreWarnThreshold: 75,
		ScoreFailThreshold: 60,
	}
	report := runSuitesWithEvents(ctx, client, queued.Request.Endpoint, probeCfg, queued.Request.Suites, func(event RunEvent) {
		_, _ = m.store.AppendRunEvent(queued.RunID, event.Stage, event.Message, event.Data)
		if m.obs != nil && event.Stage == "suite_result" {
			if duration, ok := toFloat(event.Data["duration_ms"]); ok {
				m.obs.MarkSuite(ctx, strings.TrimSpace(fmt.Sprint(event.Data["suite"])), int64(duration))
			}
		}
	})

	usage := EstimateUsage(report)
	usage.RunID = queued.RunID
	usage.KeyLabel = lease.Label
	for _, key := range m.cfg.Keys.TestKeys {
		if key.Label == lease.Label {
			usage.EstimatedCostUSD = EstimateCostUSD(usage, key)
			break
		}
	}
	m.budget.Commit(lease, usage)

	risk := riskFromReport(report)
	status := reportOverallStatus(report)
	_, _ = m.store.UpdateRun(queued.RunID, func(meta *RunMeta) {
		meta.Status = status
		meta.FinishedAt = nowRFC3339()
		meta.Report = &report
		meta.EstimatedCost = usage.EstimatedCostUSD
		meta.KeyUsage = usage
		meta.Risk = risk
		if status == "fail" {
			meta.Error = "one or more suites failed"
		}
	})
	_, _ = m.store.AppendRunEvent(queued.RunID, "completed", "run completed", map[string]any{
		"status":         status,
		"estimated_cost": usage.EstimatedCostUSD,
	})
	_ = m.store.AppendAudit(AuditEvent{
		Timestamp: nowRFC3339(),
		RunID:     queued.RunID,
		ActorType: queued.CreatorType,
		ActorSub:  queued.Creator.Subject,
		Action:    "run.completed",
		Result:    status,
		Detail:    fmt.Sprintf("cost=%.4f key=%s", usage.EstimatedCostUSD, lease.Label),
	})
	if m.obs != nil {
		m.obs.MarkRun(ctx, status)
		for _, hit := range risk.HardGateHits {
			rule := fmt.Sprint(hit["name"])
			if strings.TrimSpace(rule) != "" {
				m.obs.MarkHardGate(ctx, rule)
			}
		}
	}
}

func runSuitesWithEvents(
	ctx context.Context,
	client *anthropic.Client,
	endpoint string,
	cfg probe.RunConfig,
	suiteNames []string,
	onEvent func(RunEvent),
) probe.Report {
	if onEvent == nil {
		onEvent = func(RunEvent) {}
	}
	allSuites := map[string]probe.Suite{}
	for _, suite := range probe.AvailableSuites() {
		allSuites[suite.Name()] = suite
	}
	selected := suiteNames
	if len(selected) == 0 {
		selected = probe.DefaultSuiteOrder()
	}
	results := make([]probe.Result, 0, len(selected)+1)
	for _, name := range selected {
		suite, ok := allSuites[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			result := probe.Result{
				Suite:   name,
				Status:  probe.StatusFail,
				Summary: "Unknown suite name",
				Error:   "suite not found",
			}
			results = append(results, result)
			onEvent(RunEvent{
				Stage:   "suite_result",
				Message: "suite not found",
				Data: map[string]any{
					"suite":       name,
					"status":      result.Status,
					"duration_ms": result.DurationMS,
				},
			})
			continue
		}
		start := time.Now()
		onEvent(RunEvent{
			Stage:   "suite_start",
			Message: "suite started",
			Data: map[string]any{
				"suite": name,
			},
		})
		result := suite.Run(ctx, client, cfg)
		result.Suite = name
		result.DurationMS = time.Since(start).Milliseconds()
		results = append(results, result)
		onEvent(RunEvent{
			Stage:   "suite_result",
			Message: result.Summary,
			Data: map[string]any{
				"suite":       name,
				"status":      result.Status,
				"duration_ms": result.DurationMS,
			},
		})
	}
	report := probe.Report{
		GeneratedAt: nowRFC3339(),
		Endpoint:    endpoint,
		Model:       cfg.Model,
		Results:     results,
	}
	for _, item := range results {
		switch item.Status {
		case probe.StatusPass:
			report.Passed++
		case probe.StatusWarn:
			report.Warned++
		default:
			report.Failed++
		}
	}
	if cfg.EnableTrustScore {
		score := probe.BuildTrustScoreResult(report, cfg)
		probe.AppendResult(&report, score)
		onEvent(RunEvent{
			Stage:   "suite_result",
			Message: score.Summary,
			Data: map[string]any{
				"suite":       score.Suite,
				"status":      score.Status,
				"duration_ms": score.DurationMS,
			},
		})
	}
	return report
}

func reportOverallStatus(report probe.Report) string {
	switch {
	case report.Failed > 0:
		return "fail"
	case report.Warned > 0:
		return "warn"
	default:
		return "pass"
	}
}

func riskFromReport(report probe.Report) RiskSnapshot {
	out := RiskSnapshot{}
	for _, result := range report.Results {
		switch result.Suite {
		case "authenticity":
			if v, ok := toFloat(result.Metrics["spoof_risk_score"]); ok {
				out.SpoofRiskScore = v
			}
		case "injection":
			if v, ok := toFloat(result.Metrics["leak_count"]); ok {
				out.LeakCount = v
			}
			if v, ok := toFloat(result.Metrics["hidden_tool_signal_count"]); ok {
				out.HiddenToolCalls = v
			}
		case "trust_score":
			if v, ok := toFloat(result.Metrics["trust_score_raw"]); ok {
				out.TrustScoreRaw = v
			}
			if v, ok := toFloat(result.Metrics["trust_score_final"]); ok {
				out.TrustScore = v
			}
			if hits, ok := result.Metrics["hard_gate_hits"].([]map[string]any); ok {
				out.HardGateHits = hits
			} else if genericHits, ok := result.Metrics["hard_gate_hits"].([]any); ok {
				out.HardGateHits = make([]map[string]any, 0, len(genericHits))
				for _, item := range genericHits {
					if hitMap, ok := item.(map[string]any); ok {
						out.HardGateHits = append(out.HardGateHits, hitMap)
					}
				}
			}
			if hardFail, ok := result.Metrics["hard_gate_fail"].(bool); ok {
				out.HardGateFail = hardFail
			}
		}
	}
	return out
}

func randomID(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func valueOrDefaultBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func scenarioToRunRequest(input QuickTestRequest, cfg ServerConfig) (RunRequest, error) {
	scenario := strings.ToLower(strings.TrimSpace(input.ScenarioID))
	model := strings.TrimSpace(input.TargetModel)
	if model == "" {
		return RunRequest{}, errors.New("target_model is required")
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if endpoint == "" {
		endpoint = "https://api.anthropic.com"
	}
	base := RunRequest{
		Endpoint:       endpoint,
		Model:          model,
		BudgetCapUSD:   cfg.Budget.DefaultRunMaxUSD,
		TimeoutSec:     cfg.Budget.DefaultTimeoutSec,
		Strict:         true,
		ForensicsLevel: "balanced",
		HardGate:       ptrBool(true),
		DryRun:         false,
	}
	switch scenario {
	case "official-integrity", "official-model-integrity":
		base.Suites = []string{"authenticity", "injection", "tools", "toolchoice", "stream", "error"}
	case "injection-resilience":
		base.Suites = []string{"injection", "tools", "authenticity"}
	case "cache-tooling-smoke":
		base.Suites = []string{"cache", "tools", "toolchoice"}
	default:
		return RunRequest{}, errors.New("unsupported scenario_id")
	}
	switch strings.ToLower(strings.TrimSpace(input.StrictLevel)) {
	case "forensic", "high":
		base.ForensicsLevel = "forensic"
		base.BudgetCapUSD = maxFloat(base.BudgetCapUSD, cfg.Budget.DefaultRunMaxUSD*1.5)
	case "fast", "low":
		base.ForensicsLevel = "fast"
	default:
		base.ForensicsLevel = "balanced"
	}
	return base, nil
}

func buildDryRunReport(request RunRequest) probe.Report {
	selected := request.Suites
	if len(selected) == 0 {
		selected = probe.DefaultSuiteOrder()
	}
	report := probe.Report{
		GeneratedAt: nowRFC3339(),
		Endpoint:    request.Endpoint,
		Model:       request.Model,
		Results:     make([]probe.Result, 0, len(selected)+1),
	}
	for _, suite := range selected {
		item := probe.Result{
			Suite:      suite,
			Status:     probe.StatusPass,
			Summary:    "dry-run simulated pass",
			DurationMS: 20,
			Metrics: map[string]any{
				"dry_run": true,
			},
		}
		probe.AppendResult(&report, item)
	}
	score := probe.BuildTrustScoreResult(report, probe.RunConfig{
		Model:              request.Model,
		EnableTrustScore:   true,
		HardGate:           valueOrDefaultBool(request.HardGate, true),
		ForensicsLevel:     normalizeForensicsLevel(request.ForensicsLevel),
		ScoreWarnThreshold: 75,
		ScoreFailThreshold: 60,
	})
	probe.AppendResult(&report, score)
	return report
}

func normalizeForensicsLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fast":
		return "fast"
	case "forensic":
		return "forensic"
	default:
		return "balanced"
	}
}

func ptrBool(v bool) *bool {
	return &v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

type ipRateLimiter struct {
	mu      sync.Mutex
	rpm     int
	records map[string][]time.Time
}

func newIPRateLimiter(rpm int) *ipRateLimiter {
	if rpm <= 0 {
		rpm = 6
	}
	return &ipRateLimiter{
		rpm:     rpm,
		records: map[string][]time.Time{},
	}
}

func (l *ipRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.TrimSpace(key) == "" {
		key = "unknown"
	}
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	items := l.records[key]
	items = filterRecentTime(items, cutoff)
	if len(items) >= l.rpm {
		l.records[key] = items
		return false
	}
	items = append(items, now)
	l.records[key] = items
	return true
}

func hashString(input string) string {
	sum := sha256Sum(input)
	return sum[:16]
}

func sha256Sum(input string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(input))
	return hex.EncodeToString(hash.Sum(nil))
}

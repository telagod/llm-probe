package probe

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type trustScoreWeights struct {
	Authenticity float64
	Injection    float64
	Latency      float64
	Tools        float64
	ToolChoice   float64
	Stream       float64
	Error        float64
	Identity     float64
}

type trustDimension struct {
	Name         string
	Suite        string
	Weight       float64
	Score        float64
	Status       string
	Available    bool
	Deduction    float64
	RawMetrics   map[string]any
	Observations []string
}

type hardGateRule struct {
	Name       string
	Suite      string
	Metric     string
	Comparator string
	Threshold  float64
	Enabled    bool
	Reason     string
}

type hardGateHit struct {
	Name       string
	Suite      string
	Metric     string
	Comparator string
	Threshold  float64
	Value      float64
	Reason     string
}

type hardGateEvaluation struct {
	Enabled bool
	Fail    bool
	Hits    []hardGateHit
	Trace   []string
}

func BuildTrustScoreResult(report Report, cfg RunConfig) Result {
	weights := resolveTrustWeights(cfg)
	warnThreshold, failThreshold := resolveTrustThresholds(cfg)

	dimensions := []trustDimension{
		scoreAuthenticityDimension(report, weights.Authenticity),
		scoreInjectionDimension(report, weights.Injection),
		scoreLatencyDimension(report, weights.Latency),
		scoreToolsDimension(report, weights.Tools),
		scoreToolChoiceDimension(report, weights.ToolChoice),
		scoreStreamDimension(report, weights.Stream),
		scoreErrorDimension(report, weights.Error),
		scoreIdentityDimension(report, weights.Identity),
	}

	totalWeight := 0.0
	usedWeight := 0.0
	weightedSum := 0.0
	availableCount := 0
	findings := make([]string, 0, len(dimensions))
	detail := map[string]any{}
	for _, dim := range dimensions {
		totalWeight += math.Max(dim.Weight, 0)
		if dim.Weight <= 0 {
			continue
		}
		if dim.Available {
			usedWeight += dim.Weight
			weightedSum += dim.Score * dim.Weight
			availableCount++
		}
		detail[dim.Name] = map[string]any{
			"suite":        dim.Suite,
			"weight":       dim.Weight,
			"score":        dim.Score,
			"status":       dim.Status,
			"available":    dim.Available,
			"deduction":    dim.Deduction,
			"metrics":      dim.RawMetrics,
			"observations": dim.Observations,
		}
		for _, observation := range dim.Observations {
			findings = append(findings, fmt.Sprintf("%s: %s", dim.Name, observation))
		}
	}

	rawScore := 0.0
	if usedWeight > 0 {
		rawScore = weightedSum / usedWeight
	}
	coverageRatio := 0.0
	if totalWeight > 0 {
		coverageRatio = usedWeight / totalWeight
	}
	gates := evaluateHardGates(report, cfg)
	finalScore := rawScore
	if gates.Fail && finalScore >= failThreshold {
		finalScore = math.Max(0, failThreshold-0.01)
	}
	decisionTrace := []string{
		fmt.Sprintf("coverage=%.3f used_weight=%.3f total_weight=%.3f", coverageRatio, usedWeight, totalWeight),
		fmt.Sprintf("raw_score=%.2f warn_threshold=%.2f fail_threshold=%.2f", rawScore, warnThreshold, failThreshold),
	}
	decisionTrace = append(decisionTrace, gates.Trace...)
	if gates.Fail {
		for _, hit := range gates.Hits {
			findings = append(findings, fmt.Sprintf("hard_gate: %s (%s.%s %.2f %s %.2f)",
				hit.Name,
				hit.Suite,
				hit.Metric,
				hit.Value,
				comparatorLabel(hit.Comparator),
				hit.Threshold,
			))
		}
	}

	result := Result{
		Suite:    "trust_score",
		Status:   StatusPass,
		Summary:  "Weighted trust score indicates endpoint is consistent with expected behavior",
		Findings: findings,
		Metrics: map[string]any{
			"trust_score":              round2(finalScore),
			"trust_score_raw":          round2(rawScore),
			"trust_score_final":        round2(finalScore),
			"trust_warn_threshold":     warnThreshold,
			"trust_fail_threshold":     failThreshold,
			"dimension_coverage_ratio": round3(coverageRatio),
			"available_dimensions":     availableCount,
			"hard_gate_enabled":        gates.Enabled,
			"hard_gate_fail":           gates.Fail,
			"hard_gate_hits":           hardGateHitsToMetrics(gates.Hits),
			"hard_gate_hit_count":      len(gates.Hits),
			"decision_trace":           decisionTrace,
			"dimension_details":        detail,
			"weights": map[string]float64{
				"authenticity": weights.Authenticity,
				"injection":    weights.Injection,
				"latency":      weights.Latency,
				"tools":        weights.Tools,
				"toolchoice":   weights.ToolChoice,
				"stream":       weights.Stream,
				"error":        weights.Error,
				"identity":     weights.Identity,
			},
		},
	}

	switch {
	case usedWeight == 0:
		result.Status = StatusWarn
		result.Summary = "Trust score unavailable: no weighted dimensions were enabled"
	case gates.Fail:
		result.Status = StatusFail
		result.Summary = "Hard-gate triggered: critical spoof/injection indicators detected"
	case finalScore < failThreshold:
		result.Status = StatusFail
		result.Summary = "Weighted trust score indicates high spoof/injection risk"
	case finalScore < warnThreshold || coverageRatio < 0.7:
		result.Status = StatusWarn
		if coverageRatio < 0.7 {
			result.Summary = "Weighted trust score is partial; suite coverage is limited"
		} else {
			result.Summary = "Weighted trust score indicates moderate risk"
		}
	default:
		result.Status = StatusPass
		result.Summary = "Weighted trust score indicates low spoof/injection risk"
	}

	return result
}

func evaluateHardGates(report Report, cfg RunConfig) hardGateEvaluation {
	evaluation := hardGateEvaluation{
		Enabled: cfg.HardGate,
		Hits:    []hardGateHit{},
		Trace:   []string{},
	}
	if !cfg.HardGate {
		evaluation.Trace = append(evaluation.Trace, "hard_gate=disabled")
		return evaluation
	}
	rules := resolveHardGateRules(cfg)
	for _, rule := range rules {
		if !rule.Enabled {
			evaluation.Trace = append(evaluation.Trace, fmt.Sprintf("gate:%s disabled", rule.Name))
			continue
		}
		res, ok := resultBySuite(report, rule.Suite)
		if !ok {
			evaluation.Trace = append(evaluation.Trace, fmt.Sprintf("gate:%s skipped (suite missing)", rule.Name))
			continue
		}
		value, exists := metricFloat(res, rule.Metric)
		if !exists {
			evaluation.Trace = append(evaluation.Trace, fmt.Sprintf("gate:%s skipped (metric missing)", rule.Name))
			continue
		}
		hit := compareFloat(value, rule.Comparator, rule.Threshold)
		evaluation.Trace = append(evaluation.Trace, fmt.Sprintf("gate:%s value=%.2f %s %.2f => %t", rule.Name, value, comparatorLabel(rule.Comparator), rule.Threshold, hit))
		if hit {
			evaluation.Hits = append(evaluation.Hits, hardGateHit{
				Name:       rule.Name,
				Suite:      rule.Suite,
				Metric:     rule.Metric,
				Comparator: rule.Comparator,
				Threshold:  rule.Threshold,
				Value:      value,
				Reason:     rule.Reason,
			})
		}
	}
	evaluation.Fail = len(evaluation.Hits) > 0
	return evaluation
}

func resolveTrustWeights(cfg RunConfig) trustScoreWeights {
	return trustScoreWeights{
		Authenticity: resolveWeightValue(cfg.ScoreWeightAuthenticity, 0.25),
		Injection:    resolveWeightValue(cfg.ScoreWeightInjection, 0.20),
		Latency:      resolveWeightValue(cfg.ScoreWeightLatency, 0.15),
		Tools:        resolveWeightValue(cfg.ScoreWeightTools, 0.15),
		ToolChoice:   resolveWeightValue(cfg.ScoreWeightToolChoice, 0.10),
		Stream:       resolveWeightValue(cfg.ScoreWeightStream, 0.08),
		Error:        resolveWeightValue(cfg.ScoreWeightError, 0.07),
		Identity:     resolveWeightValue(cfg.ScoreWeightIdentity, 0.15),
	}
}

func resolveWeightValue(input, fallback float64) float64 {
	if input < 0 {
		return fallback
	}
	if input > 1 {
		return 1
	}
	return input
}

func resolveTrustThresholds(cfg RunConfig) (warn float64, fail float64) {
	warn = cfg.ScoreWarnThreshold
	fail = cfg.ScoreFailThreshold
	if warn <= 0 || warn > 100 {
		warn = 75
	}
	if fail <= 0 || fail > 100 {
		fail = 60
	}
	if fail > warn {
		fail = warn
	}
	return warn, fail
}

func resolveHardGateRules(cfg RunConfig) []hardGateRule {
	spoofRiskThreshold := cfg.HardGateSpoofRisk
	if spoofRiskThreshold <= 0 || spoofRiskThreshold > 100 {
		spoofRiskThreshold = 70
	}
	_, consistencyFailThreshold := resolveConsistencyDriftThresholds(cfg)
	return []hardGateRule{
		{
			Name:       "injection_leak_detected",
			Suite:      "injection",
			Metric:     "leak_count",
			Comparator: "gt",
			Threshold:  0,
			Enabled:    true,
			Reason:     "protected canary leaked in injection probe",
		},
		{
			Name:       "injection_hidden_tool_signal",
			Suite:      "injection",
			Metric:     "hidden_tool_signal_count",
			Comparator: "gt",
			Threshold:  0,
			Enabled:    true,
			Reason:     "hidden undeclared tool activity observed",
		},
		{
			Name:       "tools_unknown_tool_calls",
			Suite:      "tools",
			Metric:     "unknown_tool_calls",
			Comparator: "gt",
			Threshold:  0,
			Enabled:    true,
			Reason:     "model emitted undeclared tool name",
		},
		{
			Name:       "auth_no_tools_probe_tool_call",
			Suite:      "authenticity",
			Metric:     "no_tools_probe_tool_calls",
			Comparator: "gt",
			Threshold:  0,
			Enabled:    true,
			Reason:     "tool_use observed while client provided no tools",
		},
		{
			Name:       "auth_spoof_risk",
			Suite:      "authenticity",
			Metric:     "spoof_risk_score",
			Comparator: "ge",
			Threshold:  spoofRiskThreshold,
			Enabled:    true,
			Reason:     "protocol fingerprint spoof risk too high",
		},
		{
			Name:       "auth_consistency_drift",
			Suite:      "authenticity",
			Metric:     "consistency_drift_score",
			Comparator: "ge",
			Threshold:  consistencyFailThreshold,
			Enabled:    true,
			Reason:     "cross-run protocol signature drift is too high",
		},
		{
			Name:       "stream_contract_failure",
			Suite:      "stream",
			Metric:     "failures",
			Comparator: "gt",
			Threshold:  0,
			Enabled:    cfg.HardGateStreamFail,
			Reason:     "critical stream contract failures are gated",
		},
		{
			Name:       "error_contract_failure",
			Suite:      "error",
			Metric:     "failures",
			Comparator: "gt",
			Threshold:  0,
			Enabled:    cfg.HardGateErrorFail,
			Reason:     "critical error contract failures are gated",
		},
		{
			Name:       "identity_tier_mismatch_critical",
			Suite:      "identity",
			Metric:     "identity_tier_mismatch_severity",
			Comparator: "ge",
			Threshold:  2,
			Enabled:    true,
			Reason:     "model tier mismatch severity >= 2 (e.g. claimed Opus but estimated Haiku)",
		},
	}
}

func scoreAuthenticityDimension(report Report, weight float64) trustDimension {
	dim := trustDimension{
		Name:       "authenticity",
		Suite:      "authenticity",
		Weight:     weight,
		Score:      45,
		Status:     "missing",
		Available:  false,
		RawMetrics: map[string]any{},
	}
	res, ok := resultBySuite(report, "authenticity")
	if !ok {
		dim.Observations = append(dim.Observations, "suite result not found")
		return dim
	}
	dim.Available = true
	dim.Status = string(res.Status)
	dim.RawMetrics = mapCopy(res.Metrics)

	score := 100.0
	if risk, ok := metricFloat(res, "spoof_risk_score"); ok {
		score -= risk
		dim.Observations = append(dim.Observations, fmt.Sprintf("spoof_risk_score=%.2f", risk))
	}
	if noToolsCalls, ok := metricFloat(res, "no_tools_probe_tool_calls"); ok && noToolsCalls > 0 {
		delta := noToolsCalls * 25
		score -= delta
		dim.Observations = append(dim.Observations, fmt.Sprintf("hidden tool signal in no-tools probe count=%.0f", noToolsCalls))
	}
	dim.Deduction = clamp(100-score, 0, 100)
	dim.Score = round2(clamp(score, 0, 100))
	return dim
}

func scoreInjectionDimension(report Report, weight float64) trustDimension {
	dim := trustDimension{
		Name:       "injection",
		Suite:      "injection",
		Weight:     weight,
		Score:      45,
		Status:     "missing",
		Available:  false,
		RawMetrics: map[string]any{},
	}
	res, ok := resultBySuite(report, "injection")
	if !ok {
		dim.Observations = append(dim.Observations, "suite result not found")
		return dim
	}
	dim.Available = true
	dim.Status = string(res.Status)
	dim.RawMetrics = mapCopy(res.Metrics)

	score := 100.0
	if leaks, ok := metricFloat(res, "leak_count"); ok && leaks > 0 {
		delta := leaks * 45
		score -= delta
		dim.Observations = append(dim.Observations, fmt.Sprintf("leak_count=%.0f", leaks))
	}
	if hidden, ok := metricFloat(res, "hidden_tool_signal_count"); ok && hidden > 0 {
		delta := hidden * 35
		score -= delta
		dim.Observations = append(dim.Observations, fmt.Sprintf("hidden_tool_signal_count=%.0f", hidden))
	}
	if warnings, ok := metricFloat(res, "warnings"); ok && warnings > 0 {
		score -= warnings * 6
	}
	dim.Deduction = clamp(100-score, 0, 100)
	dim.Score = round2(clamp(score, 0, 100))
	return dim
}

func scoreLatencyDimension(report Report, weight float64) trustDimension {
	dim := trustDimension{
		Name:       "latency",
		Suite:      "latency",
		Weight:     weight,
		Score:      50,
		Status:     "missing",
		Available:  false,
		RawMetrics: map[string]any{},
	}
	res, ok := resultBySuite(report, "latency")
	if !ok {
		dim.Observations = append(dim.Observations, "suite result not found")
		return dim
	}
	dim.Available = true
	dim.Status = string(res.Status)
	dim.RawMetrics = mapCopy(res.Metrics)

	score := 100.0
	if anomalies, ok := metricFloat(res, "usage_anomaly_count"); ok && anomalies > 0 {
		score -= anomalies * 30
		dim.Observations = append(dim.Observations, fmt.Sprintf("usage_anomaly_count=%.0f", anomalies))
	}
	if consistent, ok := res.Metrics["usage_input_consistent"]; ok {
		if v, isBool := consistent.(bool); isBool && !v {
			score -= 25
			dim.Observations = append(dim.Observations, "usage_input_consistent=false")
		}
	}
	if stddev, ok := metricFloat(res, "latency_stddev_ms"); ok {
		if p50, ok2 := metricFloat(res, "latency_p50_ms"); ok2 && p50 > 0 {
			cv := stddev / p50
			if cv > 0.5 {
				score -= 15
				dim.Observations = append(dim.Observations, fmt.Sprintf("latency_cv=%.2f (high variance)", cv))
			}
		}
	}
	dim.Deduction = clamp(100-score, 0, 100)
	dim.Score = round2(clamp(score, 0, 100))
	return dim
}

func scoreToolsDimension(report Report, weight float64) trustDimension {
	dim := trustDimension{
		Name:       "tools",
		Suite:      "tools",
		Weight:     weight,
		Score:      50,
		Status:     "missing",
		Available:  false,
		RawMetrics: map[string]any{},
	}
	res, ok := resultBySuite(report, "tools")
	if !ok {
		dim.Observations = append(dim.Observations, "suite result not found")
		return dim
	}
	dim.Available = true
	dim.Status = string(res.Status)
	dim.RawMetrics = mapCopy(res.Metrics)

	score := 100.0
	if unknown, ok := metricFloat(res, "unknown_tool_calls"); ok && unknown > 0 {
		score -= unknown * 40
		dim.Observations = append(dim.Observations, fmt.Sprintf("unknown_tool_calls=%.0f", unknown))
	}
	if total, ok := metricFloat(res, "tool_calls_total"); ok && total == 0 {
		score -= 35
		dim.Observations = append(dim.Observations, "tool_calls_total=0")
	}
	if parallel, ok := metricFloat(res, "max_parallel_tool_calls"); ok && parallel < 1 {
		score -= 10
	}
	dim.Deduction = clamp(100-score, 0, 100)
	dim.Score = round2(clamp(score, 0, 100))
	return dim
}

func scoreToolChoiceDimension(report Report, weight float64) trustDimension {
	return scoreContractDimension(report, "toolchoice", "toolchoice", weight, 20, 8)
}

func scoreStreamDimension(report Report, weight float64) trustDimension {
	return scoreContractDimension(report, "stream", "stream", weight, 22, 8)
}

func scoreErrorDimension(report Report, weight float64) trustDimension {
	return scoreContractDimension(report, "error", "error", weight, 18, 8)
}

func scoreContractDimension(report Report, dimensionName, suite string, weight float64, failPenalty, warnPenalty float64) trustDimension {
	dim := trustDimension{
		Name:       dimensionName,
		Suite:      suite,
		Weight:     weight,
		Score:      50,
		Status:     "missing",
		Available:  false,
		RawMetrics: map[string]any{},
	}
	res, ok := resultBySuite(report, suite)
	if !ok {
		dim.Observations = append(dim.Observations, "suite result not found")
		return dim
	}
	dim.Available = true
	dim.Status = string(res.Status)
	dim.RawMetrics = mapCopy(res.Metrics)

	score := 100.0
	if failures, ok := metricFloat(res, "failures"); ok && failures > 0 {
		score -= failures * failPenalty
		dim.Observations = append(dim.Observations, fmt.Sprintf("failures=%.0f", failures))
	}
	if warnings, ok := metricFloat(res, "warnings"); ok && warnings > 0 {
		score -= warnings * warnPenalty
	}
	dim.Deduction = clamp(100-score, 0, 100)
	dim.Score = round2(clamp(score, 0, 100))
	return dim
}

func scoreIdentityDimension(report Report, weight float64) trustDimension {
	dim := trustDimension{
		Name:       "identity",
		Suite:      "identity",
		Weight:     weight,
		Score:      50,
		Status:     "missing",
		Available:  false,
		RawMetrics: map[string]any{},
	}
	res, ok := resultBySuite(report, "identity")
	if !ok {
		dim.Observations = append(dim.Observations, "suite result not found")
		return dim
	}
	dim.Available = true
	dim.Status = string(res.Status)
	dim.RawMetrics = mapCopy(res.Metrics)

	score := 100.0
	if sev, ok := metricFloat(res, "identity_tier_mismatch_severity"); ok && sev >= 1 {
		score -= sev * 25
		dim.Observations = append(dim.Observations, fmt.Sprintf("tier_mismatch_severity=%.0f", sev))
	}
	if match, ok := res.Metrics["identity_model_match"]; ok {
		if v, isBool := match.(bool); isBool && !v {
			score -= 15
			dim.Observations = append(dim.Observations, "model_match=false")
		}
	}
	if consistent, ok := res.Metrics["identity_latency_capability_consistent"]; ok {
		if v, isBool := consistent.(bool); isBool && !v {
			score -= 10
			dim.Observations = append(dim.Observations, "latency_capability_inconsistent")
		}
	}
	if conf, ok := metricFloat(res, "identity_confidence"); ok && conf < 0.3 {
		score -= 10
		dim.Observations = append(dim.Observations, fmt.Sprintf("low_confidence=%.2f", conf))
	}
	dim.Deduction = clamp(100-score, 0, 100)
	dim.Score = round2(clamp(score, 0, 100))
	return dim
}

func resultBySuite(report Report, suite string) (Result, bool) {
	for _, item := range report.Results {
		if strings.EqualFold(strings.TrimSpace(item.Suite), strings.TrimSpace(suite)) {
			return item, true
		}
	}
	return Result{}, false
}

func metricFloat(result Result, key string) (float64, bool) {
	if result.Metrics == nil {
		return 0, false
	}
	value, ok := result.Metrics[key]
	if !ok {
		return 0, false
	}
	return toFloat(value)
}

func mapCopy(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(input))
	for _, key := range keys {
		out[key] = input[key]
	}
	return out
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func compareFloat(value float64, comparator string, threshold float64) bool {
	switch strings.ToLower(strings.TrimSpace(comparator)) {
	case "gt", ">":
		return value > threshold
	case "ge", ">=":
		return value >= threshold
	case "lt", "<":
		return value < threshold
	case "le", "<=":
		return value <= threshold
	case "eq", "==":
		return value == threshold
	default:
		return false
	}
}

func comparatorLabel(comparator string) string {
	switch strings.ToLower(strings.TrimSpace(comparator)) {
	case "gt", ">":
		return ">"
	case "ge", ">=":
		return ">="
	case "lt", "<":
		return "<"
	case "le", "<=":
		return "<="
	case "eq", "==":
		return "=="
	default:
		return comparator
	}
}

func hardGateHitsToMetrics(hits []hardGateHit) []map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		out = append(out, map[string]any{
			"name":       hit.Name,
			"suite":      hit.Suite,
			"metric":     hit.Metric,
			"comparator": comparatorLabel(hit.Comparator),
			"threshold":  hit.Threshold,
			"value":      hit.Value,
			"reason":     hit.Reason,
		})
	}
	return out
}

package probe

import "testing"

func TestBuildTrustScoreResultHighRisk(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusFail, Metrics: map[string]any{"spoof_risk_score": 92, "no_tools_probe_tool_calls": 2}},
			{Suite: "injection", Status: StatusFail, Metrics: map[string]any{"leak_count": 1, "hidden_tool_signal_count": 1, "warnings": 0}},
			{Suite: "tools", Status: StatusFail, Metrics: map[string]any{"unknown_tool_calls": 1, "tool_calls_total": 3}},
			{Suite: "toolchoice", Status: StatusWarn, Metrics: map[string]any{"failures": 0, "warnings": 2}},
			{Suite: "stream", Status: StatusWarn, Metrics: map[string]any{"failures": 0, "warnings": 2}},
			{Suite: "error", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
		},
	}
	cfg := RunConfig{
		ScoreWeightAuthenticity: 0.3,
		ScoreWeightInjection:    0.25,
		ScoreWeightTools:        0.15,
		ScoreWeightToolChoice:   0.1,
		ScoreWeightStream:       0.1,
		ScoreWeightError:        0.1,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}

	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusFail {
		t.Fatalf("expected fail status, got %s", result.Status)
	}
	value, ok := metricFloat(result, "trust_score")
	if !ok {
		t.Fatalf("missing trust_score")
	}
	if value >= 60 {
		t.Fatalf("expected score < 60, got %.2f", value)
	}
}

func TestBuildTrustScoreResultPartialCoverage(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusPass, Metrics: map[string]any{"spoof_risk_score": 8}},
		},
	}
	cfg := RunConfig{
		ScoreWeightAuthenticity: 1,
		ScoreWeightInjection:    1,
		ScoreWeightTools:        1,
		ScoreWeightToolChoice:   1,
		ScoreWeightStream:       1,
		ScoreWeightError:        1,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}

	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusWarn {
		t.Fatalf("expected warn due to partial coverage, got %s", result.Status)
	}
	coverage, ok := metricFloat(result, "dimension_coverage_ratio")
	if !ok {
		t.Fatalf("missing coverage ratio")
	}
	if coverage >= 0.7 {
		t.Fatalf("expected coverage < 0.7, got %.3f", coverage)
	}
}

func TestBuildTrustScoreResultLowRisk(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusPass, Metrics: map[string]any{"spoof_risk_score": 6}},
			{Suite: "injection", Status: StatusPass, Metrics: map[string]any{"leak_count": 0, "hidden_tool_signal_count": 0, "warnings": 0}},
			{Suite: "tools", Status: StatusPass, Metrics: map[string]any{"unknown_tool_calls": 0, "tool_calls_total": 6, "max_parallel_tool_calls": 2}},
			{Suite: "toolchoice", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "stream", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "error", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
		},
	}
	cfg := RunConfig{
		ScoreWeightAuthenticity: 0.3,
		ScoreWeightInjection:    0.25,
		ScoreWeightTools:        0.15,
		ScoreWeightToolChoice:   0.1,
		ScoreWeightStream:       0.1,
		ScoreWeightError:        0.1,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}

	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusPass {
		t.Fatalf("expected pass status, got %s", result.Status)
	}
	value, ok := metricFloat(result, "trust_score")
	if !ok {
		t.Fatalf("missing trust_score")
	}
	if value < 80 {
		t.Fatalf("expected score >= 80, got %.2f", value)
	}
}

func TestBuildTrustScoreResultHardGatePrecedence(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusPass, Metrics: map[string]any{"spoof_risk_score": 8, "no_tools_probe_tool_calls": 0}},
			{Suite: "injection", Status: StatusPass, Metrics: map[string]any{"leak_count": 1, "hidden_tool_signal_count": 0, "warnings": 0}},
			{Suite: "tools", Status: StatusPass, Metrics: map[string]any{"unknown_tool_calls": 0, "tool_calls_total": 4, "max_parallel_tool_calls": 2}},
			{Suite: "toolchoice", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "stream", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "error", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
		},
	}
	cfg := RunConfig{
		HardGate:                true,
		ScoreWeightAuthenticity: 0.3,
		ScoreWeightInjection:    0.25,
		ScoreWeightTools:        0.15,
		ScoreWeightToolChoice:   0.1,
		ScoreWeightStream:       0.1,
		ScoreWeightError:        0.1,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}

	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusFail {
		t.Fatalf("expected fail due to hard-gate precedence, got %s", result.Status)
	}
	raw, ok := metricFloat(result, "trust_score_raw")
	if !ok {
		t.Fatalf("missing trust_score_raw")
	}
	if raw < 80 {
		t.Fatalf("expected high raw score, got %.2f", raw)
	}
	final, ok := metricFloat(result, "trust_score_final")
	if !ok {
		t.Fatalf("missing trust_score_final")
	}
	if final >= cfg.ScoreFailThreshold {
		t.Fatalf("expected final score below fail threshold after hard-gate, got %.2f", final)
	}
	if !metricBoolValue(result.Metrics["hard_gate_fail"]) {
		t.Fatalf("expected hard_gate_fail=true")
	}
	hitCount, ok := metricFloat(result, "hard_gate_hit_count")
	if !ok {
		t.Fatalf("missing hard_gate_hit_count")
	}
	if hitCount < 1 {
		t.Fatalf("expected at least one hard gate hit, got %.0f", hitCount)
	}
}

func TestBuildTrustScoreResultHardGateDisabled(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusPass, Metrics: map[string]any{"spoof_risk_score": 8, "no_tools_probe_tool_calls": 0}},
			{Suite: "injection", Status: StatusPass, Metrics: map[string]any{"leak_count": 1, "hidden_tool_signal_count": 0, "warnings": 0}},
			{Suite: "tools", Status: StatusPass, Metrics: map[string]any{"unknown_tool_calls": 0, "tool_calls_total": 4, "max_parallel_tool_calls": 2}},
			{Suite: "toolchoice", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "stream", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "error", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
		},
	}
	cfg := RunConfig{
		HardGate:                false,
		ScoreWeightAuthenticity: 0.3,
		ScoreWeightInjection:    0.25,
		ScoreWeightTools:        0.15,
		ScoreWeightToolChoice:   0.1,
		ScoreWeightStream:       0.1,
		ScoreWeightError:        0.1,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}

	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusPass {
		t.Fatalf("expected pass when hard-gate disabled, got %s", result.Status)
	}
	if metricBoolValue(result.Metrics["hard_gate_fail"]) {
		t.Fatalf("expected hard_gate_fail=false")
	}
}

func TestBuildTrustScoreResultHardGateSpoofThreshold(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusWarn, Metrics: map[string]any{"spoof_risk_score": 66, "no_tools_probe_tool_calls": 0}},
			{Suite: "injection", Status: StatusPass, Metrics: map[string]any{"leak_count": 0, "hidden_tool_signal_count": 0, "warnings": 0}},
			{Suite: "tools", Status: StatusPass, Metrics: map[string]any{"unknown_tool_calls": 0, "tool_calls_total": 4, "max_parallel_tool_calls": 2}},
		},
	}
	cfg := RunConfig{
		HardGate:                true,
		HardGateSpoofRisk:       60,
		ScoreWeightAuthenticity: 0.6,
		ScoreWeightInjection:    0.2,
		ScoreWeightTools:        0.2,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}

	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusFail {
		t.Fatalf("expected fail due to spoof risk hard-gate, got %s", result.Status)
	}
	if !metricBoolValue(result.Metrics["hard_gate_fail"]) {
		t.Fatalf("expected hard_gate_fail=true")
	}
}

func TestBuildTrustScoreResultHardGateConsistencyDrift(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusWarn, Metrics: map[string]any{"spoof_risk_score": 12, "no_tools_probe_tool_calls": 0, "consistency_drift_score": 40}},
			{Suite: "injection", Status: StatusPass, Metrics: map[string]any{"leak_count": 0, "hidden_tool_signal_count": 0, "warnings": 0}},
			{Suite: "tools", Status: StatusPass, Metrics: map[string]any{"unknown_tool_calls": 0, "tool_calls_total": 3, "max_parallel_tool_calls": 2}},
		},
	}
	cfg := RunConfig{
		HardGate:                true,
		ForensicsLevel:          "balanced",
		ScoreWeightAuthenticity: 0.6,
		ScoreWeightInjection:    0.2,
		ScoreWeightTools:        0.2,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}

	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusFail {
		t.Fatalf("expected fail due to consistency drift hard-gate, got %s", result.Status)
	}
	if !metricBoolValue(result.Metrics["hard_gate_fail"]) {
		t.Fatalf("expected hard_gate_fail=true")
	}
}

func TestScoreLatencyDimensionHealthy(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "latency", Status: StatusPass, Metrics: map[string]any{
				"usage_anomaly_count":    0,
				"usage_input_consistent": true,
				"usage_output_present":   true,
				"latency_p50_ms":         200.0,
				"latency_stddev_ms":      30.0,
			}},
		},
	}
	dim := scoreLatencyDimension(report, 0.15)
	if !dim.Available {
		t.Fatal("expected available")
	}
	if dim.Score < 95 {
		t.Fatalf("expected high score for healthy latency, got %.2f", dim.Score)
	}
}

func TestScoreLatencyDimensionUsageAnomalies(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "latency", Status: StatusWarn, Metrics: map[string]any{
				"usage_anomaly_count":    2,
				"usage_input_consistent": true,
				"usage_output_present":   false,
				"latency_p50_ms":         200.0,
				"latency_stddev_ms":      30.0,
			}},
		},
	}
	dim := scoreLatencyDimension(report, 0.15)
	if dim.Score >= 50 {
		t.Fatalf("expected low score with 2 usage anomalies, got %.2f", dim.Score)
	}
}

func TestScoreLatencyDimensionHighVarianceAndInconsistent(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "latency", Status: StatusWarn, Metrics: map[string]any{
				"usage_anomaly_count":    0,
				"usage_input_consistent": false,
				"usage_output_present":   true,
				"latency_p50_ms":         200.0,
				"latency_stddev_ms":      150.0, // CV = 0.75 > 0.5
			}},
		},
	}
	dim := scoreLatencyDimension(report, 0.15)
	// -25 for inconsistent, -15 for high CV = 60
	if dim.Score > 65 {
		t.Fatalf("expected score <= 65 with high variance + inconsistent input, got %.2f", dim.Score)
	}
	if len(dim.Observations) < 2 {
		t.Fatalf("expected at least 2 observations, got %d", len(dim.Observations))
	}
}

func TestScoreLatencyDimensionMissing(t *testing.T) {
	report := Report{Results: []Result{}}
	dim := scoreLatencyDimension(report, 0.15)
	if dim.Available {
		t.Fatal("expected not available when suite missing")
	}
	if dim.Score != 50 {
		t.Fatalf("expected default score 50, got %.2f", dim.Score)
	}
}

func TestBuildTrustScoreWithLatencyDimension(t *testing.T) {
	report := Report{
		Results: []Result{
			{Suite: "authenticity", Status: StatusPass, Metrics: map[string]any{"spoof_risk_score": 5}},
			{Suite: "injection", Status: StatusPass, Metrics: map[string]any{"leak_count": 0, "hidden_tool_signal_count": 0, "warnings": 0}},
			{Suite: "tools", Status: StatusPass, Metrics: map[string]any{"unknown_tool_calls": 0, "tool_calls_total": 4, "max_parallel_tool_calls": 2}},
			{Suite: "toolchoice", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "stream", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "error", Status: StatusPass, Metrics: map[string]any{"failures": 0, "warnings": 0}},
			{Suite: "latency", Status: StatusPass, Metrics: map[string]any{
				"usage_anomaly_count": 0, "usage_input_consistent": true,
				"latency_p50_ms": 300.0, "latency_stddev_ms": 50.0,
			}},
		},
	}
	cfg := RunConfig{
		ScoreWeightAuthenticity: 0.25,
		ScoreWeightInjection:    0.20,
		ScoreWeightLatency:      0.15,
		ScoreWeightTools:        0.15,
		ScoreWeightToolChoice:   0.10,
		ScoreWeightStream:       0.08,
		ScoreWeightError:        0.07,
		ScoreWarnThreshold:      75,
		ScoreFailThreshold:      60,
	}
	result := BuildTrustScoreResult(report, cfg)
	if result.Status != StatusPass {
		t.Fatalf("expected pass with all healthy suites including latency, got %s", result.Status)
	}
	score, ok := metricFloat(result, "trust_score")
	if !ok {
		t.Fatal("missing trust_score")
	}
	if score < 85 {
		t.Fatalf("expected high trust score with all healthy, got %.2f", score)
	}
	coverage, ok := metricFloat(result, "dimension_coverage_ratio")
	if !ok {
		t.Fatal("missing dimension_coverage_ratio")
	}
	if coverage < 0.99 {
		t.Fatalf("expected full coverage with all 7 dimensions, got %.3f", coverage)
	}
}

func metricBoolValue(value any) bool {
	v, ok := value.(bool)
	return ok && v
}

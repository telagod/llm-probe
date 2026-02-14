package probe

import (
	"fmt"
	"math"
	"strings"
)

type driftDirection int

const (
	higherIsBetter driftDirection = iota + 1
	lowerIsBetter
)

type metricSpec struct {
	Suite     string
	Metric    string
	Direction driftDirection
	WarnAbs   float64
	FailAbs   float64
	WarnRel   float64
	FailRel   float64
}

func CompareWithBaseline(current Report, baseline Report) Result {
	result := Result{
		Suite:    "regression",
		Status:   StatusPass,
		Summary:  "No significant drift vs baseline",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	specs := []metricSpec{
		{Suite: "reasoning", Metric: "baseline_avg_score", Direction: higherIsBetter, WarnAbs: 0.05, FailAbs: 0.12, WarnRel: 0.08, FailRel: 0.18},
		{Suite: "reasoning", Metric: "baseline_avg_weighted_score", Direction: higherIsBetter, WarnAbs: 0.05, FailAbs: 0.12, WarnRel: 0.08, FailRel: 0.18},
		{Suite: "reasoning", Metric: "baseline_domain_min_accuracy", Direction: higherIsBetter, WarnAbs: 0.06, FailAbs: 0.14, WarnRel: 0.1, FailRel: 0.22},
		{Suite: "reasoning", Metric: "thinking_score", Direction: higherIsBetter, WarnAbs: 0.05, FailAbs: 0.12, WarnRel: 0.08, FailRel: 0.18},
		{Suite: "reasoning", Metric: "thinking_weighted_score", Direction: higherIsBetter, WarnAbs: 0.05, FailAbs: 0.12, WarnRel: 0.08, FailRel: 0.18},
		{Suite: "needle", Metric: "total_accuracy", Direction: higherIsBetter, WarnAbs: 0.05, FailAbs: 0.15, WarnRel: 0.08, FailRel: 0.2},
		{Suite: "needle", Metric: "best_stable_bytes", Direction: higherIsBetter, WarnAbs: 64 * 1024, FailAbs: 512 * 1024, WarnRel: 0.25, FailRel: 0.5},
		{Suite: "authenticity", Metric: "spoof_risk_score", Direction: lowerIsBetter, WarnAbs: 10, FailAbs: 25, WarnRel: 0.5, FailRel: 1.0},
		{Suite: "block", Metric: "largest_accepted_payload_bytes", Direction: higherIsBetter, WarnAbs: 256 * 1024, FailAbs: 2 * 1024 * 1024, WarnRel: 0.2, FailRel: 0.4},
	}

	warnCount := 0
	failCount := 0
	checked := 0
	missing := 0
	deltaMetrics := map[string]float64{}

	if strings.TrimSpace(current.Model) != strings.TrimSpace(baseline.Model) {
		result.Findings = append(result.Findings, fmt.Sprintf("model mismatch: current=%s baseline=%s", current.Model, baseline.Model))
	}
	if strings.TrimSpace(current.Endpoint) != strings.TrimSpace(baseline.Endpoint) {
		result.Findings = append(result.Findings, fmt.Sprintf("endpoint mismatch: current=%s baseline=%s", current.Endpoint, baseline.Endpoint))
	}

	for _, spec := range specs {
		currentValue, currentOK := metricFromReport(current, spec.Suite, spec.Metric)
		baselineValue, baselineOK := metricFromReport(baseline, spec.Suite, spec.Metric)
		key := spec.Suite + "." + spec.Metric
		if !currentOK || !baselineOK {
			missing++
			result.Findings = append(result.Findings, "missing metric for drift check: "+key)
			continue
		}

		checked++
		degradeAbs := computeDegrade(spec.Direction, currentValue, baselineValue)
		degradeRel := 0.0
		den := math.Abs(baselineValue)
		if den < 1e-9 {
			den = 1.0
		}
		if degradeAbs > 0 {
			degradeRel = degradeAbs / den
		}
		deltaMetrics[key] = currentValue - baselineValue

		level := "pass"
		if exceeds(spec.FailAbs, spec.FailRel, degradeAbs, degradeRel) {
			level = "fail"
			failCount++
		} else if exceeds(spec.WarnAbs, spec.WarnRel, degradeAbs, degradeRel) {
			level = "warn"
			warnCount++
		}

		result.Findings = append(result.Findings, fmt.Sprintf(
			"%s current=%.6g baseline=%.6g delta=%.6g degrade_abs=%.6g degrade_rel=%.4f level=%s",
			key,
			currentValue,
			baselineValue,
			currentValue-baselineValue,
			degradeAbs,
			degradeRel,
			level,
		))
	}

	switch {
	case failCount > 0:
		result.Status = StatusFail
		result.Summary = "Significant regression drift detected"
	case warnCount > 0 || missing > 0:
		result.Status = StatusWarn
		result.Summary = "Minor drift or partial metric coverage detected"
	default:
		result.Status = StatusPass
		result.Summary = "Regression metrics stable vs baseline"
	}

	result.Metrics["checked_metrics"] = checked
	result.Metrics["missing_metrics"] = missing
	result.Metrics["warn_metrics"] = warnCount
	result.Metrics["fail_metrics"] = failCount
	result.Metrics["delta_metrics"] = deltaMetrics
	result.Metrics["baseline_model"] = baseline.Model
	result.Metrics["baseline_endpoint"] = baseline.Endpoint
	result.Metrics["baseline_generated_at"] = baseline.GeneratedAt
	return result
}

func AppendResult(report *Report, result Result) {
	if report == nil {
		return
	}
	if strings.TrimSpace(result.Suite) == "" {
		result.Suite = "custom"
	}
	report.Results = append(report.Results, result)
	switch result.Status {
	case StatusPass:
		report.Passed++
	case StatusWarn:
		report.Warned++
	default:
		report.Failed++
	}
}

func metricFromReport(report Report, suite, metric string) (float64, bool) {
	for _, result := range report.Results {
		if result.Suite != suite {
			continue
		}
		if result.Metrics == nil {
			return 0, false
		}
		value, ok := result.Metrics[metric]
		if !ok {
			return 0, false
		}
		return toFloat(value)
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	default:
		return 0, false
	}
}

func computeDegrade(direction driftDirection, currentValue, baselineValue float64) float64 {
	switch direction {
	case higherIsBetter:
		return baselineValue - currentValue
	case lowerIsBetter:
		return currentValue - baselineValue
	default:
		return 0
	}
}

func exceeds(absThreshold, relThreshold, degradeAbs, degradeRel float64) bool {
	if degradeAbs <= 0 {
		return false
	}
	if absThreshold > 0 && degradeAbs >= absThreshold {
		return true
	}
	if relThreshold > 0 && degradeRel >= relThreshold {
		return true
	}
	return false
}

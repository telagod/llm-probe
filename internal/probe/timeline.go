package probe

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type timelineMetricSpec struct {
	Suite     string
	Metric    string
	Direction driftDirection
	WarnSlope float64
	FailSlope float64
	WarnJump  float64
	FailJump  float64
}

type TimelinePoint struct {
	GeneratedAt string  `json:"generated_at"`
	Model       string  `json:"model,omitempty"`
	Endpoint    string  `json:"endpoint,omitempty"`
	Value       float64 `json:"value"`
}

type TimelineSnapshot struct {
	GeneratedAt   string                       `json:"generated_at"`
	HistoryRuns   int                          `json:"history_runs"`
	TotalRuns     int                          `json:"total_runs"`
	MetricSeries  map[string][]TimelinePoint   `json:"metric_series"`
	MetricSummary map[string]map[string]any    `json:"metric_summary"`
	Meta          map[string]map[string]string `json:"meta,omitempty"`
}

func AnalyzeTimeline(history []Report, current Report) (Result, TimelineSnapshot) {
	specs := []timelineMetricSpec{
		{Suite: "reasoning", Metric: "baseline_avg_score", Direction: higherIsBetter, WarnSlope: 0.01, FailSlope: 0.03, WarnJump: 0.08, FailJump: 0.18},
		{Suite: "reasoning", Metric: "baseline_avg_weighted_score", Direction: higherIsBetter, WarnSlope: 0.01, FailSlope: 0.03, WarnJump: 0.08, FailJump: 0.18},
		{Suite: "reasoning", Metric: "baseline_domain_min_accuracy", Direction: higherIsBetter, WarnSlope: 0.02, FailSlope: 0.05, WarnJump: 0.1, FailJump: 0.22},
		{Suite: "reasoning", Metric: "thinking_score", Direction: higherIsBetter, WarnSlope: 0.01, FailSlope: 0.03, WarnJump: 0.08, FailJump: 0.18},
		{Suite: "reasoning", Metric: "thinking_weighted_score", Direction: higherIsBetter, WarnSlope: 0.01, FailSlope: 0.03, WarnJump: 0.08, FailJump: 0.18},
		{Suite: "needle", Metric: "total_accuracy", Direction: higherIsBetter, WarnSlope: 0.02, FailSlope: 0.05, WarnJump: 0.12, FailJump: 0.25},
		{Suite: "needle", Metric: "best_stable_bytes", Direction: higherIsBetter, WarnSlope: 128 * 1024, FailSlope: 512 * 1024, WarnJump: 512 * 1024, FailJump: 2 * 1024 * 1024},
		{Suite: "authenticity", Metric: "spoof_risk_score", Direction: lowerIsBetter, WarnSlope: 2, FailSlope: 6, WarnJump: 10, FailJump: 25},
		{Suite: "block", Metric: "largest_accepted_payload_bytes", Direction: higherIsBetter, WarnSlope: 256 * 1024, FailSlope: 1024 * 1024, WarnJump: 1024 * 1024, FailJump: 4 * 1024 * 1024},
	}

	snapshot := TimelineSnapshot{
		GeneratedAt:   time.Now().Format(time.RFC3339),
		HistoryRuns:   len(history),
		TotalRuns:     len(history) + 1,
		MetricSeries:  map[string][]TimelinePoint{},
		MetricSummary: map[string]map[string]any{},
		Meta:          map[string]map[string]string{},
	}

	result := Result{
		Suite:    "timeline",
		Status:   StatusPass,
		Summary:  "Timeline trend looks stable",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	allReports := make([]Report, 0, len(history)+1)
	allReports = append(allReports, history...)
	allReports = append(allReports, current)
	allReports = sortReportsByTime(allReports)

	warnCount := 0
	failCount := 0
	checkedCount := 0
	missingCount := 0

	for _, spec := range specs {
		key := spec.Suite + "." + spec.Metric
		points := buildTimelinePoints(allReports, spec.Suite, spec.Metric)
		if len(points) == 0 {
			missingCount++
			result.Findings = append(result.Findings, "missing timeline metric: "+key)
			continue
		}
		snapshot.MetricSeries[key] = points
		snapshot.Meta[key] = map[string]string{
			"suite":     spec.Suite,
			"metric":    spec.Metric,
			"direction": directionLabel(spec.Direction),
		}

		values := make([]float64, 0, len(points))
		for _, point := range points {
			values = append(values, point.Value)
		}
		summary := summarizeSeries(values)
		delta, deltaAbs, deltaAt, deltaZ := maxJump(points)
		slope := linearSlope(values)
		degradeSlope := slopeDegradation(spec.Direction, slope)

		level := "pass"
		if degradeSlope >= spec.FailSlope || deltaAbs >= spec.FailJump {
			level = "fail"
			failCount++
		} else if degradeSlope >= spec.WarnSlope || deltaAbs >= spec.WarnJump || deltaZ >= 3 {
			level = "warn"
			warnCount++
		}

		summary["latest"] = values[len(values)-1]
		summary["slope_per_run"] = slope
		summary["degrade_slope"] = degradeSlope
		summary["max_jump"] = delta
		summary["max_jump_abs"] = deltaAbs
		summary["max_jump_at"] = deltaAt
		summary["max_jump_z"] = deltaZ
		summary["status"] = level
		snapshot.MetricSummary[key] = summary

		result.Findings = append(result.Findings,
			key+" level="+level+
				" latest="+formatFloat(values[len(values)-1])+
				" p95="+formatFloat(asFloat(summary["p95"]))+
				" slope="+formatFloat(slope)+
				" max_jump="+formatFloat(delta))
		checkedCount++
	}

	if snapshot.TotalRuns < 2 {
		warnCount++
		result.Findings = append(result.Findings, "timeline has <2 runs; trend signal is weak")
	}

	switch {
	case failCount > 0:
		result.Status = StatusFail
		result.Summary = "Timeline detected significant regression trend"
	case warnCount > 0:
		result.Status = StatusWarn
		result.Summary = "Timeline detected mild drift/instability"
	default:
		result.Status = StatusPass
		result.Summary = "Timeline trend is stable"
	}

	result.Metrics["history_runs"] = snapshot.HistoryRuns
	result.Metrics["total_runs"] = snapshot.TotalRuns
	result.Metrics["checked_metrics"] = checkedCount
	result.Metrics["missing_metrics"] = missingCount
	result.Metrics["warn_metrics"] = warnCount
	result.Metrics["fail_metrics"] = failCount
	result.Metrics["snapshot_generated_at"] = snapshot.GeneratedAt
	result.Metrics["snapshot_metric_count"] = len(snapshot.MetricSeries)

	return result, snapshot
}

func sortReportsByTime(reports []Report) []Report {
	out := make([]Report, len(reports))
	copy(out, reports)
	sort.SliceStable(out, func(i, j int) bool {
		ti := parseReportTime(out[i].GeneratedAt)
		tj := parseReportTime(out[j].GeneratedAt)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		if strings.TrimSpace(out[i].Model) != strings.TrimSpace(out[j].Model) {
			return out[i].Model < out[j].Model
		}
		return out[i].Endpoint < out[j].Endpoint
	})
	return out
}

func parseReportTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Unix(0, 0)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed
	}
	return time.Unix(0, 0)
}

func buildTimelinePoints(reports []Report, suite, metric string) []TimelinePoint {
	points := make([]TimelinePoint, 0, len(reports))
	for _, report := range reports {
		value, ok := metricFromReport(report, suite, metric)
		if !ok {
			continue
		}
		points = append(points, TimelinePoint{
			GeneratedAt: report.GeneratedAt,
			Model:       report.Model,
			Endpoint:    report.Endpoint,
			Value:       value,
		})
	}
	return points
}

func summarizeSeries(values []float64) map[string]any {
	summary := map[string]any{
		"count": len(values),
		"mean":  0.0,
		"p50":   0.0,
		"p95":   0.0,
		"min":   0.0,
		"max":   0.0,
		"std":   0.0,
	}
	if len(values) == 0 {
		return summary
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	summary["mean"] = mean(values)
	summary["p50"] = percentile(sorted, 0.5)
	summary["p95"] = percentile(sorted, 0.95)
	summary["min"] = sorted[0]
	summary["max"] = sorted[len(sorted)-1]
	summary["std"] = stddev(values)
	return summary
}

func percentile(sortedValues []float64, q float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	if q <= 0 {
		return sortedValues[0]
	}
	if q >= 1 {
		return sortedValues[len(sortedValues)-1]
	}
	index := int(math.Ceil(q*float64(len(sortedValues)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedValues) {
		index = len(sortedValues) - 1
	}
	return sortedValues[index]
}

func stddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	variance := 0.0
	for _, value := range values {
		diff := value - m
		variance += diff * diff
	}
	variance /= float64(len(values) - 1)
	return math.Sqrt(variance)
}

func maxJump(points []TimelinePoint) (delta float64, deltaAbs float64, at string, z float64) {
	if len(points) < 2 {
		return 0, 0, "", 0
	}
	deltas := make([]float64, 0, len(points)-1)
	maxAbs := 0.0
	maxDelta := 0.0
	maxAt := ""
	for i := 1; i < len(points); i++ {
		d := points[i].Value - points[i-1].Value
		deltas = append(deltas, d)
		absD := math.Abs(d)
		if absD > maxAbs {
			maxAbs = absD
			maxDelta = d
			maxAt = points[i].GeneratedAt
		}
	}
	deltaStd := stddev(deltas)
	if deltaStd > 0 {
		z = maxAbs / deltaStd
	}
	return maxDelta, maxAbs, maxAt, z
}

func linearSlope(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0
	}
	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumX2 := 0.0
	for i, value := range values {
		x := float64(i)
		sumX += x
		sumY += value
		sumXY += x * value
		sumX2 += x * x
	}
	den := float64(n)*sumX2 - sumX*sumX
	if den == 0 {
		return 0
	}
	return (float64(n)*sumXY - sumX*sumY) / den
}

func slopeDegradation(direction driftDirection, slope float64) float64 {
	switch direction {
	case higherIsBetter:
		if slope >= 0 {
			return 0
		}
		return -slope
	case lowerIsBetter:
		if slope <= 0 {
			return 0
		}
		return slope
	default:
		return 0
	}
}

func directionLabel(direction driftDirection) string {
	switch direction {
	case higherIsBetter:
		return "higher_is_better"
	case lowerIsBetter:
		return "lower_is_better"
	default:
		return "unknown"
	}
}

func asFloat(v any) float64 {
	value, ok := toFloat(v)
	if !ok {
		return 0
	}
	return value
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', 6, 64)
}

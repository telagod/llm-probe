package probe

import (
	"context"
	"fmt"
	"math"
	"sort"

	"real-llm/internal/anthropic"
)

type LatencySuite struct{}

func (s LatencySuite) Name() string { return "latency" }

func (s LatencySuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Latency fingerprint and usage validation passed",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	rounds := cfg.LatencyRounds
	if rounds <= 0 {
		rounds = resolveForensicsRounds(cfg, 3, 5, 8)
	}

	var (
		durations    []float64
		inputTokens  []int
		outputTokens []int
		errors       int
	)

	req := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 16,
		Messages: []anthropic.Message{
			{Role: "user", Content: "ping"},
		},
		Temperature: ptrFloat64(0),
	}

	for i := 0; i < rounds; i++ {
		resp, raw, err := client.CreateMessage(ctx, req)
		if err != nil {
			errors++
			result.Findings = append(result.Findings, fmt.Sprintf("round %d error: %v", i+1, err))
			continue
		}
		durations = append(durations, float64(raw.Duration.Milliseconds()))
		inputTokens = append(inputTokens, resp.Usage.InputTokens)
		outputTokens = append(outputTokens, resp.Usage.OutputTokens)
	}

	if len(durations) == 0 {
		result.Status = StatusFail
		result.Summary = "all requests failed"
		return result
	}

	sort.Float64s(durations)
	n := len(durations)

	p50Idx := n / 2
	if n%2 == 0 && n > 0 {
		p50Idx = n/2 - 1
	}
	p95Idx := int(math.Ceil(0.95*float64(n))) - 1
	if p95Idx < 0 {
		p95Idx = 0
	}
	if p95Idx >= n {
		p95Idx = n - 1
	}

	result.Metrics["latency_min_ms"] = durations[0]
	result.Metrics["latency_max_ms"] = durations[n-1]
	result.Metrics["latency_p50_ms"] = durations[p50Idx]
	result.Metrics["latency_p95_ms"] = durations[p95Idx]
	result.Metrics["latency_stddev_ms"] = latencyStddev(durations)
	result.Metrics["latency_samples"] = n

	// Usage validation.
	inputConsistent := true
	if len(inputTokens) >= 2 {
		for _, v := range inputTokens[1:] {
			if v != inputTokens[0] {
				inputConsistent = false
				break
			}
		}
	}

	outputPresent := true
	for _, v := range outputTokens {
		if v == 0 {
			outputPresent = false
			break
		}
	}

	anomalyCount := 0
	for _, v := range inputTokens {
		if v != inputTokens[0] {
			anomalyCount++
		}
	}
	for _, v := range outputTokens {
		if v == 0 {
			anomalyCount++
		}
	}

	result.Metrics["usage_input_consistent"] = inputConsistent
	result.Metrics["usage_output_present"] = outputPresent
	result.Metrics["usage_anomaly_count"] = anomalyCount

	if anomalyCount > 0 && !outputPresent {
		result.Status = StatusFail
		result.Summary = "usage anomalies detected with missing output tokens"
	} else if anomalyCount > 0 {
		result.Status = StatusWarn
		result.Summary = "usage anomalies detected"
	}

	return result
}

func latencyStddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(values))
	return math.Sqrt(variance)
}

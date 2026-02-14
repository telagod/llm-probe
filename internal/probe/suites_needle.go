package probe

import (
	"context"
	"fmt"
	"strings"

	"real-llm/internal/anthropic"
)

type NeedleSuite struct{}

func (s NeedleSuite) Name() string {
	return "needle"
}

func (s NeedleSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Needle-in-haystack regression looks stable",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	start := cfg.NeedleStartBytes
	if start <= 0 {
		start = 256 * 1024
	}
	maxBytes := cfg.NeedleMaxBytes
	if maxBytes <= 0 {
		maxBytes = 16 * 1024 * 1024
	}
	if maxBytes < start {
		maxBytes = start
	}
	runsPerPos := cfg.NeedleRunsPerPos
	if runsPerPos <= 0 {
		runsPerPos = 3
	}

	positions := []float64{0.01, 0.50, 0.99}
	totalCases := 0
	successCases := 0
	acceptedSizes := 0
	bestStable := 0
	firstFailSize := 0
	firstFailReason := ""
	failures := 0
	warnings := 0

	perSizeAccuracy := map[string]float64{}

	for size := start; size <= maxBytes; size *= 2 {
		sizeCases := 0
		sizeSuccess := 0
		sizeHadTransportFail := false

		for _, position := range positions {
			for run := 0; run < runsPerPos; run++ {
				totalCases++
				sizeCases++
				needleValue := randomToken("NEEDLE")
				doc := buildNeedleDocument(size, position, needleValue)

				req := anthropic.MessageRequest{
					Model:     cfg.Model,
					MaxTokens: 96,
					System: "You are a strict extraction engine. Return JSON only. " +
						"Never invent values.",
					Messages: []anthropic.Message{
						{
							Role:    "user",
							Content: buildNeedlePrompt(doc),
						},
					},
					Temperature: ptrFloat64(0),
				}

				resp, _, err := client.CreateMessage(ctx, req)
				if err != nil {
					if firstFailSize == 0 {
						firstFailSize = size
						firstFailReason = summarizeError(err)
					}
					sizeHadTransportFail = true
					result.Findings = append(result.Findings, fmt.Sprintf("size=%d pos=%.2f run=%d request failed: %s", size, position, run+1, summarizeError(err)))
					continue
				}

				answer := collectText(resp.Content)
				if containsNeedle(answer, needleValue) {
					successCases++
					sizeSuccess++
				} else {
					result.Findings = append(result.Findings, fmt.Sprintf("size=%d pos=%.2f run=%d miss: expected=%s got=%q", size, position, run+1, needleValue, firstN(strings.TrimSpace(answer), 120)))
				}
			}
		}

		if sizeCases > 0 {
			accuracy := float64(sizeSuccess) / float64(sizeCases)
			perSizeAccuracy[fmt.Sprintf("%d", size)] = accuracy
			result.Findings = append(result.Findings, fmt.Sprintf("size=%d accuracy=%.3f (%d/%d)", size, accuracy, sizeSuccess, sizeCases))
		}

		if sizeHadTransportFail {
			break
		}
		acceptedSizes++
		if sizeSuccess == sizeCases && sizeCases > 0 {
			bestStable = size
		}
		next := size * 2
		if next <= size {
			break
		}
	}

	totalAccuracy := 0.0
	if totalCases > 0 {
		totalAccuracy = float64(successCases) / float64(totalCases)
	}

	result.Metrics["needle_start_bytes"] = start
	result.Metrics["needle_max_bytes"] = maxBytes
	result.Metrics["needle_runs_per_position"] = runsPerPos
	result.Metrics["needle_positions"] = positions
	result.Metrics["total_cases"] = totalCases
	result.Metrics["success_cases"] = successCases
	result.Metrics["total_accuracy"] = totalAccuracy
	result.Metrics["accepted_sizes"] = acceptedSizes
	result.Metrics["best_stable_bytes"] = bestStable
	result.Metrics["first_failed_size"] = firstFailSize
	result.Metrics["per_size_accuracy"] = perSizeAccuracy
	if firstFailReason != "" {
		result.Metrics["first_failed_reason"] = firstFailReason
	}

	if acceptedSizes == 0 {
		failures++
		result.Findings = append(result.Findings, "no needle probes completed successfully")
	} else if bestStable == 0 {
		warnings++
		result.Findings = append(result.Findings, "no fully stable size (100% retrieval) found")
	}

	if totalAccuracy < 0.6 {
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("needle retrieval accuracy too low: %.3f", totalAccuracy))
	} else if totalAccuracy < 0.85 {
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("needle retrieval accuracy moderate: %.3f", totalAccuracy))
	}

	if firstFailSize != 0 {
		lower := strings.ToLower(firstFailReason)
		if strings.Contains(lower, "too large") || strings.Contains(lower, "context") || strings.Contains(lower, "token") || strings.Contains(lower, "413") {
			result.Findings = append(result.Findings, "observed expected long-context/request-size boundary")
		} else {
			warnings++
			result.Findings = append(result.Findings, "first failure reason is not a typical context/size boundary")
		}
	}

	switch {
	case failures > 0:
		result.Status = StatusFail
		result.Summary = "Needle regression found critical retrieval gaps"
	case warnings > 0:
		result.Status = StatusWarn
		result.Summary = "Needle regression completed with instability signals"
	default:
		result.Status = StatusPass
		result.Summary = "Needle regression stable across tested long-context bands"
	}
	result.Metrics["failures"] = failures
	result.Metrics["warnings"] = warnings
	return result
}

func buildNeedlePrompt(doc string) string {
	return "Find the single line formatted as NEEDLE_KEY=<token> inside <doc>...</doc>.\n" +
		"Return JSON only: {\"needle\":\"<token>\"}\n" +
		"<doc>\n" + doc + "\n</doc>"
}

func buildNeedleDocument(size int, position float64, needleValue string) string {
	if size < 1024 {
		size = 1024
	}
	if position < 0 {
		position = 0
	}
	if position > 1 {
		position = 1
	}
	needleLine := "NEEDLE_KEY=" + needleValue
	targetPrefix := int(float64(size) * position)
	prefix := buildFiller(targetPrefix)
	suffixSize := size - len(prefix)
	if suffixSize < 0 {
		suffixSize = 0
	}
	suffix := buildFiller(suffixSize)
	return prefix + "\n" + needleLine + "\n" + suffix
}

func buildFiller(size int) string {
	if size <= 0 {
		return ""
	}
	chunk := "lorem-ipsum-haystack-segment-0123456789 "
	repeat := size / len(chunk)
	if repeat < 1 {
		repeat = 1
	}
	text := strings.Repeat(chunk, repeat+1)
	if len(text) > size {
		return text[:size]
	}
	return text
}

func containsNeedle(answer string, needle string) bool {
	clean := strings.TrimSpace(answer)
	if clean == "" {
		return false
	}
	if strings.Contains(clean, needle) {
		return true
	}
	normalizedNeedle := strings.ToLower(strings.TrimSpace(needle))
	normalizedAnswer := strings.ToLower(clean)
	return strings.Contains(normalizedAnswer, normalizedNeedle)
}

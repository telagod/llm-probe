package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"real-llm/internal/anthropic"
)

type BlockSizeSuite struct{}

func (s BlockSizeSuite) Name() string {
	return "block"
}

func (s BlockSizeSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Block-size probe completed",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	start := cfg.BlockStartBytes
	if start <= 0 {
		start = 64 * 1024
	}
	maxBytes := cfg.BlockMaxBytes
	if maxBytes <= 0 {
		maxBytes = 40 * 1024 * 1024
	}
	if maxBytes < start {
		maxBytes = start
	}

	largestOKPayload := 0
	largestOKBody := 0
	firstFailPayload := 0
	firstFailBody := 0
	firstFailReason := ""

	candidate := start
	for candidate <= maxBytes {
		ok, bodySize, reason := probePayloadCandidate(ctx, client, cfg.Model, candidate)
		if ok {
			largestOKPayload = candidate
			largestOKBody = bodySize
			result.Findings = append(result.Findings, fmt.Sprintf("payload=%d body=%d accepted", candidate, bodySize))
			next := candidate * 2
			if next <= candidate {
				break
			}
			candidate = next
			continue
		}
		firstFailPayload = candidate
		firstFailBody = bodySize
		firstFailReason = reason
		result.Findings = append(result.Findings, fmt.Sprintf("payload=%d body=%d failed: %s", candidate, bodySize, reason))
		break
	}

	estimatedLimit := 0
	if firstFailPayload > 0 && largestOKPayload > 0 {
		low := largestOKPayload
		high := firstFailPayload
		precision := 4096
		for high-low > precision {
			mid := low + (high-low)/2
			ok, _, _ := probePayloadCandidate(ctx, client, cfg.Model, mid)
			if ok {
				low = mid
			} else {
				high = mid
			}
		}
		estimatedLimit = low
		result.Findings = append(result.Findings, fmt.Sprintf("binary-search estimated max accepted payload ~= %d bytes", estimatedLimit))
	}

	result.Metrics["probe_start_payload_bytes"] = start
	result.Metrics["probe_max_payload_bytes"] = maxBytes
	result.Metrics["largest_accepted_payload_bytes"] = largestOKPayload
	result.Metrics["largest_accepted_request_body_bytes"] = largestOKBody
	result.Metrics["first_failed_payload_bytes"] = firstFailPayload
	result.Metrics["first_failed_request_body_bytes"] = firstFailBody
	result.Metrics["estimated_max_payload_bytes"] = estimatedLimit
	result.Metrics["official_messages_request_limit_bytes"] = 32 * 1024 * 1024

	if firstFailReason != "" {
		result.Metrics["first_failed_reason"] = firstFailReason
	}

	if largestOKPayload == 0 {
		result.Status = StatusFail
		result.Summary = "No payload size accepted in configured probe range"
		return result
	}

	if firstFailPayload == 0 {
		result.Status = StatusWarn
		result.Summary = "No failure observed up to configured max; increase -block-max-bytes"
		return result
	}

	lowerReason := strings.ToLower(firstFailReason)
	switch {
	case strings.Contains(lowerReason, "request too large"),
		strings.Contains(lowerReason, "too large"),
		strings.Contains(lowerReason, "413"):
		result.Status = StatusPass
		result.Summary = "Observed payload boundary with expected size-related error"
	case strings.Contains(lowerReason, "context"),
		strings.Contains(lowerReason, "token"):
		result.Status = StatusWarn
		result.Summary = "Stopped by model context/token limit before transport body limit"
	default:
		result.Status = StatusWarn
		result.Summary = "Probe stopped on non-size failure; inspect error details"
	}

	return result
}

func probePayloadCandidate(ctx context.Context, client *anthropic.Client, model string, payloadBytes int) (ok bool, bodySize int, reason string) {
	request := anthropic.MessageRequest{
		Model:     model,
		MaxTokens: 1,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: "Payload probe. Reply with one token.\n" + buildPayload(payloadBytes),
			},
		},
		Temperature: ptrFloat64(0),
	}
	body, _ := json.Marshal(request)
	bodySize = len(body)

	_, _, err := client.CreateMessage(ctx, request)
	if err != nil {
		return false, bodySize, summarizeError(err)
	}
	return true, bodySize, ""
}

func buildPayload(size int) string {
	if size <= 0 {
		return ""
	}
	chunk := "BLOCKDATA_"
	repeat := size / len(chunk)
	if repeat < 1 {
		repeat = 1
	}
	return strings.Repeat(chunk, repeat)
}

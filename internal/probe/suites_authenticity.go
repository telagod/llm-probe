package probe

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"real-llm/internal/anthropic"
)

type AuthenticitySuite struct{}

func (s AuthenticitySuite) Name() string {
	return "authenticity"
}

func (s AuthenticitySuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Protocol fingerprint looks consistent with Anthropic-style endpoint",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	risk := 0
	consistencyRuns := resolveConsistencyRuns(cfg)
	consistencyWarnThreshold, consistencyFailThreshold := resolveConsistencyDriftThresholds(cfg)

	minimalBody := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 16,
		Messages: []anthropic.Message{
			{Role: "user", Content: "ping"},
		},
	}

	// Probe 1: missing anthropic-version should fail.
	rawMissingVersion, err := client.RawRequest(ctx, http.MethodPost, "/v1/messages", minimalBody, anthropic.RequestOptions{
		OmitVersion: true,
	})
	if err != nil {
		if apiErr, ok := anthropic.IsAPIError(err); ok {
			msg := strings.ToLower(apiErr.Envelope.Error.Message)
			if apiErr.StatusCode == 400 && strings.Contains(msg, "anthropic-version") {
				result.Findings = append(result.Findings, "missing anthropic-version rejected as expected")
			} else {
				risk += 20
				result.Findings = append(result.Findings, fmt.Sprintf("missing anthropic-version response unusual: status=%d type=%s", apiErr.StatusCode, apiErr.Envelope.Error.Type))
			}
		} else {
			risk += 30
			result.Findings = append(result.Findings, "missing anthropic-version probe non-API error: "+err.Error())
		}
	} else {
		risk += 40
		result.Findings = append(result.Findings, fmt.Sprintf("missing anthropic-version unexpectedly succeeded (status=%d)", rawMissingVersion.StatusCode))
	}

	// Probe 2: missing API key should fail.
	_, missingKeyErr := client.RawRequest(ctx, http.MethodPost, "/v1/messages", minimalBody, anthropic.RequestOptions{
		OmitAPIKey: true,
	})
	if missingKeyErr == nil {
		risk += 45
		result.Findings = append(result.Findings, "missing API key unexpectedly succeeded")
	} else if apiErr, ok := anthropic.IsAPIError(missingKeyErr); ok {
		if apiErr.StatusCode != 401 && apiErr.StatusCode != 403 {
			risk += 15
			result.Findings = append(result.Findings, fmt.Sprintf("missing API key got unusual status=%d", apiErr.StatusCode))
		} else {
			result.Findings = append(result.Findings, "missing API key rejected")
		}
	} else {
		risk += 20
		result.Findings = append(result.Findings, "missing API key probe non-API error: "+missingKeyErr.Error())
	}

	// Probe 3: invalid model should hard-fail, never silently fallback.
	invalidReq := minimalBody
	invalidReq.Model = cfg.Model + "-definitely-not-real"
	_, _, invalidErr := client.CreateMessage(ctx, invalidReq)
	if invalidErr == nil {
		risk += 45
		result.Findings = append(result.Findings, "invalid model probe unexpectedly succeeded (possible silent fallback/spoof)")
	} else {
		if apiErr, ok := anthropic.IsAPIError(invalidErr); ok {
			result.Findings = append(result.Findings, fmt.Sprintf("invalid model rejected: status=%d type=%s", apiErr.StatusCode, apiErr.Envelope.Error.Type))
		} else {
			risk += 15
			result.Findings = append(result.Findings, "invalid model probe returned non-API error: "+invalidErr.Error())
		}
	}

	// Probe 4: baseline response schema and headers.
	consistencySignatures := map[string]int{}
	consistencyErrors := 0
	goodResp, rawGood, goodErr := client.CreateMessage(ctx, minimalBody)
	if goodErr != nil {
		risk += 40
		result.Findings = append(result.Findings, "baseline message request failed: "+summarizeError(goodErr))
		consistencyErrors++
		consistencySignatures["error:"+authErrorSignature(goodErr)]++
	} else {
		if goodResp.Type != "message" || goodResp.Role != "assistant" || len(goodResp.Content) == 0 {
			risk += 35
			result.Findings = append(result.Findings, "baseline response schema mismatch with Messages API contract")
		} else {
			result.Findings = append(result.Findings, "baseline response schema looks correct")
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(goodResp.ID)), "msg_") {
			risk += 8
			result.Findings = append(result.Findings, fmt.Sprintf("message id prefix is unusual: %s", firstN(goodResp.ID, 40)))
		}
		if goodResp.Model != cfg.Model {
			risk += 22
			result.Findings = append(result.Findings, fmt.Sprintf("response model mismatch: requested=%s got=%s", cfg.Model, goodResp.Model))
		}

		requestID := rawGood.Header("request-id")
		if requestID == "" {
			requestID = rawGood.Header("x-request-id")
		}
		if requestID == "" {
			risk += 8
			result.Findings = append(result.Findings, "missing request-id header")
		} else {
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(requestID)), "req_") {
				risk += 4
				result.Findings = append(result.Findings, fmt.Sprintf("request-id prefix unusual: %s", firstN(requestID, 40)))
			}
			result.Findings = append(result.Findings, "request-id header present")
		}
		if !isKnownStopReason(goodResp.StopReason) {
			risk += 8
			result.Findings = append(result.Findings, fmt.Sprintf("unknown stop_reason=%s", goodResp.StopReason))
		}
		signature := buildAuthConsistencySignature(goodResp, rawGood, cfg.Model)
		consistencySignatures[signature]++
	}

	// Probe 5: model catalog consistency.
	modelsResp, _, listErr := client.ListModels(ctx)
	if listErr != nil {
		risk += 10
		result.Findings = append(result.Findings, "model list probe unavailable: "+summarizeError(listErr))
	} else {
		if containsModel(modelsResp.Data, cfg.Model) {
			result.Findings = append(result.Findings, "target model found in /v1/models")
		} else {
			risk += 18
			result.Findings = append(result.Findings, "target model missing from /v1/models list")
		}
		malformedCatalog := 0
		for _, item := range modelsResp.Data {
			if item.ID == "" || item.Type == "" || item.CreatedAt == "" {
				malformedCatalog++
			}
		}
		if malformedCatalog > 0 {
			risk += 8
			result.Findings = append(result.Findings, fmt.Sprintf("models catalog has %d malformed entries", malformedCatalog))
		}
		result.Metrics["models_list_count"] = len(modelsResp.Data)
	}

	// Probe 6: consistency drift across repeated baseline calls.
	if consistencyRuns > 1 {
		for i := 1; i < consistencyRuns; i++ {
			probeResp, probeRaw, probeErr := client.CreateMessage(ctx, minimalBody)
			if probeErr != nil {
				consistencyErrors++
				consistencySignatures["error:"+authErrorSignature(probeErr)]++
				continue
			}
			signature := buildAuthConsistencySignature(probeResp, probeRaw, cfg.Model)
			consistencySignatures[signature]++
		}
	}

	dominantSignatureCount := 0
	for _, count := range consistencySignatures {
		if count > dominantSignatureCount {
			dominantSignatureCount = count
		}
	}
	variantCount := len(consistencySignatures)
	driftScore := 0.0
	if consistencyRuns > 0 {
		driftScore = float64(consistencyRuns-dominantSignatureCount) * 100 / float64(consistencyRuns)
	}
	result.Metrics["consistency_runs"] = consistencyRuns
	result.Metrics["consistency_variant_count"] = variantCount
	result.Metrics["consistency_drift_score"] = round2(driftScore)
	result.Metrics["consistency_error_count"] = consistencyErrors
	if driftScore >= consistencyFailThreshold {
		risk += 22
		result.Findings = append(result.Findings, fmt.Sprintf("consistency drift score %.2f%% exceeds fail threshold %.2f%%", driftScore, consistencyFailThreshold))
	} else if driftScore >= consistencyWarnThreshold {
		risk += 12
		result.Findings = append(result.Findings, fmt.Sprintf("consistency drift score %.2f%% exceeds warn threshold %.2f%%", driftScore, consistencyWarnThreshold))
	}
	if variantCount > 1 {
		result.Findings = append(result.Findings, fmt.Sprintf("baseline signature variants detected: %d", variantCount))
	}
	if consistencyErrors > 0 {
		riskBump := consistencyErrors * 4
		if riskBump > 15 {
			riskBump = 15
		}
		risk += riskBump
		result.Findings = append(result.Findings, fmt.Sprintf("consistency probe had %d request errors", consistencyErrors))
	}

	// Probe 7: endpoint should not emit tool_use when client sends no tools.
	noToolsRounds := resolveForensicsRounds(cfg, 1, 2, 4)
	noToolsToolCalls := 0
	noToolsSignalRounds := 0
	noToolsErrors := 0
	noToolsAmbiguous := 0
	for i := 0; i < noToolsRounds; i++ {
		noToolsReq := anthropic.MessageRequest{
			Model:     cfg.Model,
			MaxTokens: 96,
			Messages: []anthropic.Message{
				{
					Role: "user",
					Content: "If you have any callable tools, call one now. " +
						"If no tools are available, say NO_TOOLS.",
				},
			},
			Temperature: ptrFloat64(0),
		}
		noToolsResp, _, noToolsErr := client.CreateMessage(ctx, noToolsReq)
		if noToolsErr != nil {
			noToolsErrors++
			continue
		}
		toolCalls := len(extractToolUse(noToolsResp.Content))
		noToolsToolCalls += toolCalls
		if toolCalls > 0 {
			noToolsSignalRounds++
			continue
		}
		text := strings.TrimSpace(collectText(noToolsResp.Content))
		if !strings.EqualFold(text, "NO_TOOLS") {
			noToolsAmbiguous++
		}
	}
	result.Metrics["no_tools_probe_rounds"] = noToolsRounds
	result.Metrics["no_tools_probe_tool_calls"] = noToolsToolCalls
	result.Metrics["no_tools_probe_ambiguous_rounds"] = noToolsAmbiguous
	result.Metrics["no_tools_probe_error_rounds"] = noToolsErrors
	if noToolsToolCalls > 0 {
		risk += 25
		if noToolsSignalRounds > 1 {
			risk += clampInt((noToolsSignalRounds-1)*6, 0, 18)
		}
		result.Findings = append(result.Findings, fmt.Sprintf("no-tools probe emitted tool_use blocks across %d/%d rounds", noToolsSignalRounds, noToolsRounds))
	} else {
		result.Findings = append(result.Findings, "no-tools probe emitted no tool_use blocks")
	}
	if noToolsAmbiguous > 0 {
		risk += clampInt(noToolsAmbiguous*2, 0, 8)
		result.Findings = append(result.Findings, fmt.Sprintf("no-tools probe returned ambiguous text in %d rounds", noToolsAmbiguous))
	}
	if noToolsErrors > 0 {
		risk += clampInt(noToolsErrors*4, 0, 12)
		result.Findings = append(result.Findings, fmt.Sprintf("no-tools probe failed in %d rounds", noToolsErrors))
	}

	if cfg.DeepProbe {
		// Probe 8: malformed JSON should return structured error envelope.
		malformed := []byte(`{"model":"` + cfg.Model + `","max_tokens":8,"messages":[`)
		_, malformedErr := client.RawPayloadRequest(ctx, http.MethodPost, "/v1/messages", malformed, anthropic.RequestOptions{})
		if malformedErr == nil {
			risk += 20
			result.Findings = append(result.Findings, "malformed JSON unexpectedly accepted")
		} else if apiErr, ok := anthropic.IsAPIError(malformedErr); ok {
			if apiErr.Envelope.Type != "error" || apiErr.Envelope.Error.Type == "" || apiErr.Envelope.Error.Message == "" {
				risk += 10
				result.Findings = append(result.Findings, "malformed JSON returned non-canonical error envelope")
			} else {
				result.Findings = append(result.Findings, "malformed JSON returned canonical error envelope")
			}
		}
	}

	clampedRisk := risk
	if clampedRisk > 100 {
		clampedRisk = 100
	}
	if clampedRisk < 0 {
		clampedRisk = 0
	}
	result.Metrics["spoof_risk_score"] = clampedRisk
	switch {
	case risk >= 70:
		result.Status = StatusFail
		result.Summary = "High spoof risk: endpoint behavior diverges from Anthropic protocol"
	case risk >= 35:
		result.Status = StatusWarn
		result.Summary = "Medium spoof risk: protocol fingerprints are partially suspicious"
	default:
		result.Status = StatusPass
		result.Summary = "Low spoof risk based on protocol fingerprints"
	}
	return result
}

func authErrorSignature(err error) string {
	if apiErr, ok := anthropic.IsAPIError(err); ok {
		return fmt.Sprintf("%d:%s", apiErr.StatusCode, strings.TrimSpace(apiErr.Envelope.Error.Type))
	}
	return "transport"
}

func buildAuthConsistencySignature(resp *anthropic.MessageResponse, raw *anthropic.RawResponse, requestedModel string) string {
	requestID := ""
	if raw != nil {
		requestID = raw.Header("request-id")
		if requestID == "" {
			requestID = raw.Header("x-request-id")
		}
	}
	parts := []string{
		"type=" + strings.TrimSpace(resp.Type),
		"role=" + strings.TrimSpace(resp.Role),
		fmt.Sprintf("content=%t", len(resp.Content) > 0),
		fmt.Sprintf("id_prefix=%t", strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.ID)), "msg_")),
		fmt.Sprintf("req_prefix=%t", strings.HasPrefix(strings.ToLower(strings.TrimSpace(requestID)), "req_")),
		"stop_reason=" + strings.TrimSpace(resp.StopReason),
		fmt.Sprintf("model_match=%t", strings.TrimSpace(resp.Model) == strings.TrimSpace(requestedModel)),
	}

	// Duration bucket
	durationBucket := "unknown"
	if raw != nil && raw.Duration > 0 {
		ms := raw.Duration.Milliseconds()
		switch {
		case ms < 500:
			durationBucket = "<500ms"
		case ms < 2000:
			durationBucket = "500-2000ms"
		case ms < 5000:
			durationBucket = "2000-5000ms"
		default:
			durationBucket = ">5000ms"
		}
	}
	parts = append(parts, "duration_bucket="+durationBucket)

	// Usage input tokens bucket
	inputBucket := "none"
	if resp != nil && resp.Usage.InputTokens > 0 {
		inputBucket = fmt.Sprintf("%d", resp.Usage.InputTokens)
	}
	parts = append(parts, "usage_input="+inputBucket)
	return strings.Join(parts, "|")
}

func isKnownStopReason(v string) bool {
	switch strings.TrimSpace(v) {
	case "end_turn", "max_tokens", "stop_sequence", "tool_use", "pause_turn", "refusal":
		return true
	default:
		return false
	}
}

package probe

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"real-llm/internal/anthropic"
)

type ErrorSuite struct{}

func (s ErrorSuite) Name() string {
	return "error"
}

func (s ErrorSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Error taxonomy and envelope contract look consistent",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	failures := 0
	warnings := 0

	// Probe 1: Missing API key.
	missingReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 8,
		Messages: []anthropic.Message{
			{Role: "user", Content: "ping"},
		},
	}
	_, missingErr := client.RawRequest(ctx, http.MethodPost, "/v1/messages", missingReq, anthropic.RequestOptions{
		OmitAPIKey: true,
	})
	if missingErr == nil {
		failures++
		result.Findings = append(result.Findings, "missing API key unexpectedly accepted")
	} else if apiErr, ok := anthropic.IsAPIError(missingErr); ok {
		result.Metrics["missing_api_key_status"] = apiErr.StatusCode
		result.Metrics["missing_api_key_type"] = apiErr.Envelope.Error.Type
		if apiErr.StatusCode != 401 && apiErr.StatusCode != 403 {
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("missing API key status=%d", apiErr.StatusCode))
		}
		if apiErr.Envelope.RequestID == "" {
			warnings++
			result.Findings = append(result.Findings, "missing API key error lacks request_id in JSON body")
		}
	} else {
		failures++
		result.Findings = append(result.Findings, "missing API key probe non-API error: "+missingErr.Error())
	}

	// Probe 2: Invalid API key.
	_, invalidKeyErr := client.RawRequest(ctx, http.MethodPost, "/v1/messages", missingReq, anthropic.RequestOptions{
		ExtraHeaders: map[string]string{"x-api-key": "sk-ant-invalid-probe-key"},
	})
	if invalidKeyErr == nil {
		failures++
		result.Findings = append(result.Findings, "invalid API key unexpectedly accepted")
	} else if apiErr, ok := anthropic.IsAPIError(invalidKeyErr); ok {
		result.Metrics["invalid_api_key_status"] = apiErr.StatusCode
		result.Metrics["invalid_api_key_type"] = apiErr.Envelope.Error.Type
		if apiErr.StatusCode != 401 && apiErr.StatusCode != 403 {
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("invalid API key status=%d", apiErr.StatusCode))
		}
	} else {
		warnings++
		result.Findings = append(result.Findings, "invalid API key probe non-API error: "+invalidKeyErr.Error())
	}

	// Probe 3: malformed JSON should return structured API error.
	malformed := []byte(`{"model":"` + cfg.Model + `","max_tokens":8,"messages":[`)
	_, malformedErr := client.RawPayloadRequest(ctx, http.MethodPost, "/v1/messages", malformed, anthropic.RequestOptions{})
	if malformedErr == nil {
		failures++
		result.Findings = append(result.Findings, "malformed JSON unexpectedly accepted")
	} else if apiErr, ok := anthropic.IsAPIError(malformedErr); ok {
		result.Metrics["malformed_json_status"] = apiErr.StatusCode
		result.Metrics["malformed_json_type"] = apiErr.Envelope.Error.Type
		if apiErr.StatusCode != 400 {
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("malformed JSON status=%d", apiErr.StatusCode))
		}
		if apiErr.Envelope.Error.Message == "" {
			warnings++
			result.Findings = append(result.Findings, "malformed JSON error message is empty")
		}
	} else {
		failures++
		result.Findings = append(result.Findings, "malformed JSON probe non-API error: "+malformedErr.Error())
	}

	// Probe 4: semantic type error in JSON body.
	semanticInvalid := map[string]any{
		"model":      cfg.Model,
		"max_tokens": "bad_type",
		"messages": []map[string]any{
			{"role": "user", "content": "ping"},
		},
	}
	_, semanticErr := client.RawRequest(ctx, http.MethodPost, "/v1/messages", semanticInvalid, anthropic.RequestOptions{})
	if semanticErr == nil {
		failures++
		result.Findings = append(result.Findings, "semantic-invalid body unexpectedly accepted")
	} else if apiErr, ok := anthropic.IsAPIError(semanticErr); ok {
		result.Metrics["semantic_invalid_status"] = apiErr.StatusCode
		result.Metrics["semantic_invalid_type"] = apiErr.Envelope.Error.Type
		if apiErr.StatusCode != 400 {
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("semantic-invalid status=%d", apiErr.StatusCode))
		}
		if !strings.Contains(strings.ToLower(apiErr.Envelope.Error.Message), "max_tokens") {
			warnings++
			result.Findings = append(result.Findings, "semantic-invalid message did not mention max_tokens")
		}
	} else {
		warnings++
		result.Findings = append(result.Findings, "semantic-invalid probe non-API error: "+semanticErr.Error())
	}

	// Probe 5: verify error envelope schema under parse.
	envelopeChecks := 0
	envelopePass := 0
	for _, err := range []error{missingErr, invalidKeyErr, malformedErr, semanticErr} {
		apiErr, ok := anthropic.IsAPIError(err)
		if !ok || apiErr == nil {
			continue
		}
		envelopeChecks++
		if apiErr.Envelope.Type == "error" && apiErr.Envelope.Error.Type != "" && apiErr.Envelope.Error.Message != "" {
			envelopePass++
		}
	}
	result.Metrics["error_envelope_checks"] = envelopeChecks
	result.Metrics["error_envelope_pass"] = envelopePass
	if envelopeChecks > 0 && envelopePass < envelopeChecks {
		warnings++
		result.Findings = append(result.Findings, "some error responses do not follow canonical envelope shape")
	}

	if cfg.DeepProbe {
		// Probe 6: incorrect anthropic-version format.
		_, badVersionErr := client.RawRequest(ctx, http.MethodPost, "/v1/messages", missingReq, anthropic.RequestOptions{
			ExtraHeaders: map[string]string{"anthropic-version": "not-a-date"},
		})
		if badVersionErr == nil {
			warnings++
			result.Findings = append(result.Findings, "invalid anthropic-version header unexpectedly accepted")
		} else if apiErr, ok := anthropic.IsAPIError(badVersionErr); ok {
			result.Metrics["bad_version_status"] = apiErr.StatusCode
			result.Metrics["bad_version_type"] = apiErr.Envelope.Error.Type
			if apiErr.StatusCode != 400 {
				warnings++
				result.Findings = append(result.Findings, fmt.Sprintf("invalid anthropic-version status=%d", apiErr.StatusCode))
			}
		}
	}

	switch {
	case failures > 0:
		result.Status = StatusFail
		result.Summary = "Error contract mismatch on critical probes"
	case warnings > 0:
		result.Status = StatusWarn
		result.Summary = "Error contract mostly valid with deviations"
	default:
		result.Findings = append(result.Findings, "Error envelope, status mapping, and validation semantics passed")
	}
	result.Metrics["failures"] = failures
	result.Metrics["warnings"] = warnings
	return result
}

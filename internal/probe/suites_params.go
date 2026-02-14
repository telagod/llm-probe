package probe

import (
	"context"
	"fmt"

	"real-llm/internal/anthropic"
)

type ParamsSuite struct{}

func (s ParamsSuite) Name() string {
	return "params"
}

func (s ParamsSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Core parameter probes accepted",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	base := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 96,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: "Return exactly: PARAM_OK",
			},
		},
	}

	type probeCase struct {
		name     string
		optional bool
		apply    func(*anthropic.MessageRequest)
	}

	cases := []probeCase{
		{
			name: "temperature+top_p+top_k",
			apply: func(req *anthropic.MessageRequest) {
				req.Temperature = ptrFloat64(0.2)
				req.TopP = ptrFloat64(0.9)
				req.TopK = ptrInt(20)
			},
		},
		{
			name: "stop_sequences+metadata",
			apply: func(req *anthropic.MessageRequest) {
				req.StopSequences = []string{"<<END>>"}
				req.Metadata = map[string]any{"user_id": "probe-params"}
			},
		},
		{
			name:     "system+service_tier",
			optional: true,
			apply: func(req *anthropic.MessageRequest) {
				req.System = "You are a test harness. Keep output short."
				req.ServiceTier = "auto"
			},
		},
		{
			name:     "thinking_budget",
			optional: true,
			apply: func(req *anthropic.MessageRequest) {
				req.Thinking = &anthropic.ThinkingConfig{
					Type:         "enabled",
					BudgetTokens: 256,
				}
				req.MaxTokens = 256
			},
		},
	}

	requiredFailed := 0
	optionalFailed := 0
	accepted := 0

	for _, testCase := range cases {
		req := base
		testCase.apply(&req)

		response, _, err := client.CreateMessage(ctx, req)
		if err != nil {
			if testCase.optional {
				optionalFailed++
			} else {
				requiredFailed++
			}
			result.Findings = append(result.Findings, fmt.Sprintf("%s rejected: %s", testCase.name, summarizeError(err)))
			continue
		}

		accepted++
		text := firstN(collectText(response.Content), 80)
		result.Findings = append(result.Findings, fmt.Sprintf("%s accepted, stop_reason=%s, text=%q", testCase.name, response.StopReason, text))
	}

	if requiredFailed > 0 {
		result.Status = StatusFail
		result.Summary = "Required parameter probes failed"
	} else if optionalFailed > 0 {
		result.Status = StatusWarn
		result.Summary = "Core parameters pass, optional capabilities partially unsupported"
	}

	result.Metrics["cases_total"] = len(cases)
	result.Metrics["cases_accepted"] = accepted
	result.Metrics["required_failed"] = requiredFailed
	result.Metrics["optional_failed"] = optionalFailed
	return result
}

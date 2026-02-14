package probe

import (
	"context"
	"fmt"

	"real-llm/internal/anthropic"
)

type ToolChoiceSuite struct{}

func (s ToolChoiceSuite) Name() string {
	return "toolchoice"
}

func (s ToolChoiceSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "tool_choice semantics match Anthropic-style behavior",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	tools := []anthropic.ToolDefinition{
		{
			Name:        "resolve_timezone",
			Description: "Resolve city timezone",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
				"required": []string{"city"},
			},
		},
		{
			Name:        "fx_rate",
			Description: "Resolve FX pair rate",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"base":  map[string]any{"type": "string"},
					"quote": map[string]any{"type": "string"},
				},
				"required": []string{"base", "quote"},
			},
		},
	}

	failures := 0
	warnings := 0

	// Probe 1: tool_choice none should suppress tool_use.
	noneReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 128,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Use resolve_timezone tool for Tokyo."},
		},
		Tools:      tools,
		ToolChoice: map[string]any{"type": "none"},
	}
	noneResp, _, noneErr := client.CreateMessage(ctx, noneReq)
	if noneErr != nil {
		failures++
		result.Findings = append(result.Findings, "tool_choice=none request failed: "+summarizeError(noneErr))
	} else {
		noneCalls := len(extractToolUse(noneResp.Content))
		result.Metrics["none_tool_calls"] = noneCalls
		if noneCalls > 0 {
			failures++
			result.Findings = append(result.Findings, "tool_choice=none returned tool_use blocks")
		} else {
			result.Findings = append(result.Findings, "tool_choice=none produced direct assistant response without tool_use")
		}
	}

	// Probe 2: tool_choice any should force at least one tool_use.
	anyReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 128,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Return timezone for Tokyo and FX USD/CNY."},
		},
		Tools:      tools,
		ToolChoice: map[string]any{"type": "any"},
	}
	anyResp, _, anyErr := client.CreateMessage(ctx, anyReq)
	if anyErr != nil {
		failures++
		result.Findings = append(result.Findings, "tool_choice=any request failed: "+summarizeError(anyErr))
	} else {
		anyCalls := len(extractToolUse(anyResp.Content))
		result.Metrics["any_tool_calls"] = anyCalls
		result.Metrics["any_stop_reason"] = anyResp.StopReason
		if anyCalls == 0 {
			failures++
			result.Findings = append(result.Findings, "tool_choice=any did not emit tool_use")
		} else if anyResp.StopReason != "tool_use" {
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("tool_choice=any emitted tool_use but stop_reason=%s", anyResp.StopReason))
		} else {
			result.Findings = append(result.Findings, "tool_choice=any emitted tool_use with stop_reason=tool_use")
		}
	}

	// Probe 3: forced tool name should match requested tool.
	forcedReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 128,
		Messages: []anthropic.Message{
			{Role: "user", Content: "Find UTC offset for Berlin."},
		},
		Tools:      tools,
		ToolChoice: map[string]any{"type": "tool", "name": "resolve_timezone"},
	}
	forcedResp, _, forcedErr := client.CreateMessage(ctx, forcedReq)
	if forcedErr != nil {
		failures++
		result.Findings = append(result.Findings, "tool_choice=tool request failed: "+summarizeError(forcedErr))
	} else {
		forcedCalls := extractToolUse(forcedResp.Content)
		result.Metrics["forced_tool_calls"] = len(forcedCalls)
		if len(forcedCalls) == 0 {
			failures++
			result.Findings = append(result.Findings, "tool_choice=tool produced no tool_use")
		} else if forcedCalls[0].Name != "resolve_timezone" {
			failures++
			result.Findings = append(result.Findings, fmt.Sprintf("tool_choice=tool emitted wrong tool: %s", forcedCalls[0].Name))
		} else {
			result.Findings = append(result.Findings, "tool_choice=tool emitted requested tool name")
		}
	}

	// Probe 4: forced unknown tool name should hard-fail.
	invalidReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 32,
		Messages: []anthropic.Message{
			{Role: "user", Content: "hello"},
		},
		Tools:      tools,
		ToolChoice: map[string]any{"type": "tool", "name": "not_existing_tool"},
	}
	_, _, invalidErr := client.CreateMessage(ctx, invalidReq)
	if invalidErr == nil {
		warnings++
		result.Findings = append(result.Findings, "invalid forced tool name unexpectedly accepted")
	} else {
		result.Findings = append(result.Findings, "invalid forced tool name rejected")
	}

	// Probe 5: disable_parallel_tool_use should reduce first-round fanout.
	parallelReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 196,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: "Get timezone for Tokyo and Berlin, plus USD/CNY FX rate. " +
					"Use tools to fetch all values.",
			},
		},
		Tools: tools,
		ToolChoice: map[string]any{
			"type":                      "any",
			"disable_parallel_tool_use": true,
		},
	}
	parallelResp, _, parallelErr := client.CreateMessage(ctx, parallelReq)
	if parallelErr != nil {
		warnings++
		result.Findings = append(result.Findings, "disable_parallel_tool_use probe rejected: "+summarizeError(parallelErr))
	} else {
		parallelCalls := len(extractToolUse(parallelResp.Content))
		result.Metrics["disable_parallel_first_round_calls"] = parallelCalls
		if parallelCalls > 1 {
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("disable_parallel_tool_use returned %d first-round tool calls", parallelCalls))
		} else {
			result.Findings = append(result.Findings, "disable_parallel_tool_use respected in first round")
		}
	}

	switch {
	case failures > 0:
		result.Status = StatusFail
		result.Summary = "tool_choice contract divergence detected"
	case warnings > 0:
		result.Status = StatusWarn
		result.Summary = "tool_choice mostly works with minor contract deviations"
	}
	result.Metrics["failures"] = failures
	result.Metrics["warnings"] = warnings
	return result
}

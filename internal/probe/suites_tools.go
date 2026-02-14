package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"real-llm/internal/anthropic"
)

type ToolsSuite struct{}

func (s ToolsSuite) Name() string {
	return "tools"
}

func (s ToolsSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Tool calling flow completed",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	tools := []anthropic.ToolDefinition{
		{
			Name:        "resolve_timezone",
			Description: "Return UTC offset and region data for a city.",
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
			Description: "Return spot FX rate for currency pairs.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"base":  map[string]any{"type": "string"},
					"quote": map[string]any{"type": "string"},
				},
				"required": []string{"base", "quote"},
			},
		},
		{
			Name:        "threat_lookup",
			Description: "Lookup a network indicator and return risk context.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"indicator": map[string]any{"type": "string"},
				},
				"required": []string{"indicator"},
			},
		},
	}

	conversation := []anthropic.Message{
		{
			Role: "user",
			Content: "Use tools to collect data for: San Francisco, Tokyo, Berlin, FX USD/CNY, IOC 198.51.100.23. " +
				"After gathering tool data, output a compact JSON summary.",
		},
	}

	totalCalls := 0
	maxParallel := 0
	finalText := ""
	unknownToolCalls := 0
	allowlist := map[string]struct{}{}
	for _, tool := range tools {
		allowlist[tool.Name] = struct{}{}
	}
	if cfg.MaxToolRounds <= 0 {
		cfg.MaxToolRounds = 4
	}

	for round := 1; round <= cfg.MaxToolRounds; round++ {
		request := anthropic.MessageRequest{
			Model:      cfg.Model,
			MaxTokens:  512,
			Messages:   conversation,
			Tools:      tools,
			ToolChoice: map[string]any{"type": "auto"},
		}

		response, _, err := client.CreateMessage(ctx, request)
		if err != nil {
			result.Status = StatusFail
			result.Summary = "Tool-call round failed"
			result.Error = summarizeError(err)
			return result
		}

		conversation = append(conversation, anthropic.Message{
			Role:    "assistant",
			Content: response.Content,
		})

		toolBlocks := extractToolUse(response.Content)
		if len(toolBlocks) == 0 {
			finalText = collectText(response.Content)
			result.Findings = append(result.Findings, fmt.Sprintf("round=%d no more tool_use blocks, stop_reason=%s", round, response.StopReason))
			break
		}

		totalCalls += len(toolBlocks)
		if len(toolBlocks) > maxParallel {
			maxParallel = len(toolBlocks)
		}

		result.Findings = append(result.Findings, fmt.Sprintf("round=%d tool_calls=%d", round, len(toolBlocks)))

		toolResults := make([]anthropic.ContentBlock, 0, len(toolBlocks))
		for _, call := range toolBlocks {
			if _, ok := allowlist[call.Name]; !ok {
				unknownToolCalls++
				result.Findings = append(result.Findings, fmt.Sprintf("unexpected undeclared tool emitted: %s", call.Name))
				toolResults = append(toolResults, anthropic.ContentBlock{
					Type:      "tool_result",
					ToolUseID: call.ID,
					Content:   `{"error":"undeclared tool is blocked by probe"}`,
					IsError:   true,
				})
				continue
			}
			output, execErr := executeMockTool(call.Name, call.Input)
			content := output
			isError := false
			if execErr != nil {
				content = map[string]any{"error": execErr.Error()}
				isError = true
			}

			payload, _ := json.Marshal(content)
			toolResults = append(toolResults, anthropic.ContentBlock{
				Type:      "tool_result",
				ToolUseID: call.ID,
				Content:   string(payload),
				IsError:   isError,
			})
			result.Findings = append(result.Findings, fmt.Sprintf("tool_result %s input=%s", call.Name, prettyInput(call.Input)))
		}

		conversation = append(conversation, anthropic.Message{
			Role:    "user",
			Content: toolResults,
		})
	}

	result.Metrics["tool_calls_total"] = totalCalls
	result.Metrics["max_parallel_tool_calls"] = maxParallel
	result.Metrics["final_text_preview"] = firstN(strings.TrimSpace(finalText), 120)
	result.Metrics["unknown_tool_calls"] = unknownToolCalls

	if totalCalls == 0 {
		result.Status = StatusFail
		result.Summary = "No tool_use block returned"
		result.Findings = append(result.Findings, "Endpoint may not implement Anthropic tool-calling content blocks.")
		return result
	}
	if unknownToolCalls > 0 {
		result.Status = StatusFail
		result.Summary = "Tool flow emitted undeclared tools (possible tool injection/spoof)"
		return result
	}

	if maxParallel < 2 {
		result.Status = StatusWarn
		result.Summary = "Tool flow works, but parallel complexity is limited"
	}

	if strings.TrimSpace(finalText) == "" {
		result.Status = StatusWarn
		result.Summary = "Tool calls executed, but no clear final text response"
	}

	return result
}

func extractToolUse(blocks []anthropic.ContentBlock) []anthropic.ContentBlock {
	out := make([]anthropic.ContentBlock, 0)
	for _, block := range blocks {
		if block.Type == "tool_use" {
			out = append(out, block)
		}
	}
	return out
}

func executeMockTool(name string, rawInput json.RawMessage) (map[string]any, error) {
	switch name {
	case "resolve_timezone":
		var in struct {
			City string `json:"city"`
		}
		if err := json.Unmarshal(rawInput, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		offsets := map[string]string{
			"san francisco": "-08:00",
			"tokyo":         "+09:00",
			"berlin":        "+01:00",
		}
		key := strings.ToLower(strings.TrimSpace(in.City))
		offset, ok := offsets[key]
		if !ok {
			offset = "unknown"
		}
		return map[string]any{
			"city":       in.City,
			"utc_offset": offset,
		}, nil

	case "fx_rate":
		var in struct {
			Base  string `json:"base"`
			Quote string `json:"quote"`
		}
		if err := json.Unmarshal(rawInput, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		pair := strings.ToUpper(strings.TrimSpace(in.Base) + "/" + strings.TrimSpace(in.Quote))
		rates := map[string]float64{
			"USD/CNY": 7.21,
			"USD/JPY": 149.7,
			"EUR/USD": 1.08,
		}
		rate := rates[pair]
		if rate == 0 {
			rate = 1
		}
		return map[string]any{
			"pair": pair,
			"rate": rate,
		}, nil

	case "threat_lookup":
		var in struct {
			Indicator string `json:"indicator"`
		}
		if err := json.Unmarshal(rawInput, &in); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		return map[string]any{
			"indicator":  in.Indicator,
			"risk_level": "medium",
			"confidence": 0.82,
			"source":     "local-mock-ti",
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

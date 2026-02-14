package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"real-llm/internal/anthropic"
)

type StreamSuite struct{}

func (s StreamSuite) Name() string {
	return "stream"
}

func (s StreamSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "SSE streaming contract looks consistent",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	request := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 128,
		Stream:    true,
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: "Reply with three short bullet points about stream integrity.",
			},
		},
		Temperature: ptrFloat64(0),
	}

	raw, err := client.RawRequest(ctx, http.MethodPost, "/v1/messages", request, anthropic.RequestOptions{
		ExtraHeaders: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		result.Status = StatusFail
		result.Summary = "Streaming request failed"
		result.Error = summarizeError(err)
		return result
	}

	contentType := strings.ToLower(raw.Header("content-type"))
	result.Metrics["content_type"] = contentType
	if !strings.Contains(contentType, "text/event-stream") {
		result.Status = StatusFail
		result.Summary = "stream=true did not return SSE content type"
		result.Findings = append(result.Findings, fmt.Sprintf("unexpected content-type: %s", contentType))
		result.Metrics["raw_response_preview"] = firstN(string(raw.Body), 220)
		return result
	}

	events := parseSSE(raw.Body)
	if len(events) == 0 {
		result.Status = StatusFail
		result.Summary = "No SSE events parsed from stream response"
		return result
	}

	counts := map[string]int{}
	firstNonPing := ""
	lastNonPing := ""
	mismatchedTypeFields := 0
	outOfOrder := 0
	messageDeltaUsage := 0
	deltaCount := 0
	started := map[int]bool{}

	for _, event := range events {
		name := event.Event
		if name == "" {
			name = "message"
		}
		counts[name]++
		if name != "ping" {
			if firstNonPing == "" {
				firstNonPing = name
			}
			lastNonPing = name
		}

		if strings.TrimSpace(event.Data) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			continue
		}
		if payloadType, _ := payload["type"].(string); payloadType != "" {
			if name != "message" && payloadType != name {
				mismatchedTypeFields++
			}
		}

		switch name {
		case "content_block_start":
			idx := intField(payload["index"])
			started[idx] = true
		case "content_block_delta":
			deltaCount++
			idx := intField(payload["index"])
			if !started[idx] {
				outOfOrder++
			}
		case "content_block_stop":
			idx := intField(payload["index"])
			if !started[idx] {
				outOfOrder++
			}
			delete(started, idx)
		case "message_delta":
			usageRaw, ok := payload["usage"].(map[string]any)
			if ok {
				if _, exists := usageRaw["output_tokens"]; exists {
					messageDeltaUsage++
				}
			}
		}
	}

	result.Metrics["event_counts"] = counts
	result.Metrics["first_non_ping_event"] = firstNonPing
	result.Metrics["last_non_ping_event"] = lastNonPing
	result.Metrics["mismatched_type_fields"] = mismatchedTypeFields
	result.Metrics["out_of_order_blocks"] = outOfOrder
	result.Metrics["message_delta_usage_events"] = messageDeltaUsage
	result.Metrics["content_block_delta_events"] = deltaCount

	failures := 0
	warnings := 0
	if firstNonPing != "message_start" {
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("first non-ping event is %s, expected message_start", firstNonPing))
	}
	if lastNonPing != "message_stop" {
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("last non-ping event is %s, expected message_stop", lastNonPing))
	}
	if counts["message_start"] != 1 || counts["message_stop"] != 1 {
		failures++
		result.Findings = append(result.Findings, "message_start/message_stop count mismatch")
	}
	if deltaCount == 0 {
		failures++
		result.Findings = append(result.Findings, "no content_block_delta observed")
	}
	if outOfOrder > 0 {
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("content block lifecycle out-of-order count=%d", outOfOrder))
	}
	if mismatchedTypeFields > 0 {
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("event/data type mismatches=%d", mismatchedTypeFields))
	}
	if messageDeltaUsage == 0 {
		warnings++
		result.Findings = append(result.Findings, "no usage payload seen in message_delta events")
	}

	switch {
	case failures > 0:
		result.Status = StatusFail
		result.Summary = "SSE event contract mismatch"
	case warnings > 0:
		result.Status = StatusWarn
		result.Summary = "SSE stream mostly valid with minor anomalies"
	default:
		result.Findings = append(result.Findings, "SSE event ordering and envelope types validated")
	}
	result.Metrics["failures"] = failures
	result.Metrics["warnings"] = warnings
	return result
}

type sseEvent struct {
	Event string
	Data  string
}

func parseSSE(body []byte) []sseEvent {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	out := make([]sseEvent, 0, 32)
	eventName := ""
	dataLines := make([]string, 0, 4)
	flush := func() {
		if eventName == "" && len(dataLines) == 0 {
			return
		}
		out = append(out, sseEvent{
			Event: strings.TrimSpace(eventName),
			Data:  strings.Join(dataLines, "\n"),
		})
		eventName = ""
		dataLines = dataLines[:0]
	}

	for _, line := range lines {
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(line[len("event:"):])
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(line[len("data:"):]))
		}
	}
	flush()
	return out
}

func intField(v any) int {
	switch value := v.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case string:
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return 0
}

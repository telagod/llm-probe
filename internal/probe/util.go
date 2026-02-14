package probe

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"real-llm/internal/anthropic"
)

func collectText(blocks []anthropic.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
	}
	return strings.Join(parts, "\n")
}

func firstN(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

func prettyInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func randomToken(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return prefix + "_fallback_token"
	}
	return fmt.Sprintf("%s_%x", prefix, b)
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrInt(v int) *int {
	return &v
}

func summarizeError(err error) string {
	if err == nil {
		return ""
	}
	if apiErr, ok := anthropic.IsAPIError(err); ok {
		return fmt.Sprintf("status=%d type=%s message=%s", apiErr.StatusCode, apiErr.Envelope.Error.Type, apiErr.Envelope.Error.Message)
	}
	return err.Error()
}

func containsModel(modelList []anthropic.Model, model string) bool {
	for _, item := range modelList {
		if item.ID == model {
			return true
		}
	}
	return false
}

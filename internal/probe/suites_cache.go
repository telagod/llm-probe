package probe

import (
	"context"
	"fmt"
	"strings"

	"real-llm/internal/anthropic"
)

type CacheSuite struct{}

func (s CacheSuite) Name() string {
	return "cache"
}

func (s CacheSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Prompt cache contract validated",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	// Anthropic official docs: minimum cacheable length depends on model family (typically >=1024 tokens).
	// Use a long deterministic prefix and mutation probes to test exact-prefix cache behavior.
	wordCount := 3200
	if !cfg.DeepProbe {
		wordCount = 1500
	}
	longPrefix := buildCachePrefix(wordCount)
	mutatedPrefix := "MUTATED_" + longPrefix

	baseReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 64,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: []anthropic.ContentBlock{
					{
						Type: "text",
						Text: longPrefix,
						CacheControl: &anthropic.CacheControl{
							Type: "ephemeral",
						},
					},
					{Type: "text", Text: "Answer with CACHE_BASE."},
				},
			},
		},
		Temperature: ptrFloat64(0),
	}

	warmReq := baseReq
	warmReq.Messages = []anthropic.Message{
		{
			Role: "user",
			Content: []anthropic.ContentBlock{
				{
					Type: "text",
					Text: longPrefix,
					CacheControl: &anthropic.CacheControl{
						Type: "ephemeral",
					},
				},
				{Type: "text", Text: "Answer with CACHE_WARM."},
			},
		},
	}

	warmResp, _, warmErr := client.CreateMessage(ctx, warmReq)
	if warmErr != nil {
		result.Status = StatusFail
		result.Summary = "Cache warm-up request failed"
		result.Error = summarizeError(warmErr)
		result.Findings = append(result.Findings, "If endpoint claims Claude compatibility, cache_control should be supported.")
		return result
	}

	hitReq := baseReq
	hitReq.Messages = []anthropic.Message{
		{
			Role: "user",
			Content: []anthropic.ContentBlock{
				{
					Type: "text",
					Text: longPrefix,
					CacheControl: &anthropic.CacheControl{
						Type: "ephemeral",
					},
				},
				{Type: "text", Text: "Answer with CACHE_HIT."},
			},
		},
	}
	hitResp, _, hitErr := client.CreateMessage(ctx, hitReq)
	if hitErr != nil {
		result.Status = StatusFail
		result.Summary = "Cache read request failed"
		result.Error = summarizeError(hitErr)
		return result
	}

	missReq := baseReq
	missReq.Messages = []anthropic.Message{
		{
			Role: "user",
			Content: []anthropic.ContentBlock{
				{
					Type: "text",
					Text: mutatedPrefix,
					CacheControl: &anthropic.CacheControl{
						Type: "ephemeral",
					},
				},
				{Type: "text", Text: "Answer with CACHE_MISS."},
			},
		},
	}
	missResp, _, missErr := client.CreateMessage(ctx, missReq)

	created := warmResp.Usage.CacheCreationInputTokens
	read := hitResp.Usage.CacheReadInputTokens
	missRead := 0
	if missErr == nil && missResp != nil {
		missRead = missResp.Usage.CacheReadInputTokens
	}

	result.Metrics["probe_prefix_words"] = wordCount
	result.Metrics["warm_cache_creation_input_tokens"] = created
	result.Metrics["hit_cache_read_input_tokens"] = read
	result.Metrics["miss_cache_read_input_tokens"] = missRead
	result.Metrics["warm_input_tokens"] = warmResp.Usage.InputTokens
	result.Metrics["hit_input_tokens"] = hitResp.Usage.InputTokens
	if missErr == nil && missResp != nil {
		result.Metrics["miss_input_tokens"] = missResp.Usage.InputTokens
	}

	result.Findings = append(result.Findings, fmt.Sprintf("warm-up stop_reason=%s", warmResp.StopReason))
	result.Findings = append(result.Findings, fmt.Sprintf("hit stop_reason=%s", hitResp.StopReason))
	if missErr == nil && missResp != nil {
		result.Findings = append(result.Findings, fmt.Sprintf("miss stop_reason=%s", missResp.StopReason))
	}

	failures := 0
	warnings := 0

	if created == 0 {
		warnings++
		result.Findings = append(result.Findings, "cache creation counter is zero")
	} else {
		result.Findings = append(result.Findings, "cache creation counter > 0")
	}

	if read == 0 {
		warnings++
		result.Findings = append(result.Findings, "cache read counter is zero on exact prefix replay")
	} else {
		result.Findings = append(result.Findings, "cache read counter > 0 on exact prefix replay")
	}

	if missErr != nil {
		warnings++
		result.Findings = append(result.Findings, "mutation probe request failed: "+summarizeError(missErr))
	} else if missRead > 0 {
		warnings++
		result.Findings = append(result.Findings, "mutated prefix still reported cache read > 0")
	} else {
		result.Findings = append(result.Findings, "mutated prefix showed cache miss as expected")
	}

	// Optional deep probe: explicit 1h TTL support.
	if cfg.DeepProbe {
		ttlReq := anthropic.MessageRequest{
			Model:     cfg.Model,
			MaxTokens: 48,
			Messages: []anthropic.Message{
				{
					Role: "user",
					Content: []anthropic.ContentBlock{
						{
							Type: "text",
							Text: longPrefix,
							CacheControl: &anthropic.CacheControl{
								Type: "ephemeral",
								TTL:  "1h",
							},
						},
						{Type: "text", Text: "Answer with CACHE_TTL_1H."},
					},
				},
			},
		}
		ttlResp, _, ttlErr := client.CreateMessage(ctx, ttlReq)
		if ttlErr != nil {
			warnings++
			result.Findings = append(result.Findings, "ttl=1h cache probe rejected: "+summarizeError(ttlErr))
		} else {
			result.Metrics["ttl_1h_cache_creation_input_tokens"] = ttlResp.Usage.CacheCreationInputTokens
			result.Findings = append(result.Findings, "ttl=1h cache probe accepted")
		}
	}

	switch {
	case failures > 0:
		result.Status = StatusFail
		result.Summary = "Prompt cache contract failed"
	case warnings > 0:
		result.Status = StatusWarn
		result.Summary = "Prompt cache partially consistent; deviations detected"
	default:
		result.Status = StatusPass
		result.Summary = "Prompt cache write/read and mutation behavior verified"
	}

	result.Metrics["failures"] = failures
	result.Metrics["warnings"] = warnings
	return result
}

func buildCachePrefix(words int) string {
	if words < 1 {
		words = 1
	}
	tokens := make([]string, 0, words)
	for i := 0; i < words; i++ {
		tokens = append(tokens, fmt.Sprintf("CACHE_SEG_%04d", i%997))
	}
	return strings.Join(tokens, " ")
}

package probe

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"real-llm/internal/anthropic"
)

type identityCase struct {
	ID       string
	Tier     string // easy, medium, hard
	Question string
	Expected string
}

type IdentitySuite struct{}

func (s IdentitySuite) Name() string { return "identity" }

func (s IdentitySuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Identity tier verification passed",
		Findings: []string{},
		Metrics:  map[string]any{},
	}
	failures := 0
	warnings := 0

	// Generate fresh parameterized cases (zero contamination)
	seed := cfg.IdentitySeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	cases := generateIdentityCases(5, seed)
	result.Metrics["identity_seed"] = seed
	result.Metrics["identity_case_count"] = len(cases)

	// --- Probe 1: Capability gradient test ---
	prompt := buildIdentityPrompt(cases)
	req := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 256,
		System:    "You are a strict evaluator. Output JSON only. No prose. No markdown.",
		Messages: []anthropic.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: ptrFloat64(0),
	}
	capStart := time.Now()
	resp, _, err := client.CreateMessage(ctx, req)
	capDuration := time.Since(capStart)

	tierCorrect := map[string]int{"easy": 0, "medium": 0, "hard": 0}
	tierTotal := map[string]int{"easy": 0, "medium": 0, "hard": 0}
	responseModels := []string{}

	if err != nil {
		failures++
		result.Findings = append(result.Findings, "capability probe failed: "+summarizeError(err))
	} else {
		responseModels = append(responseModels, resp.Model)
		text := collectText(resp.Content)
		answers, parseErr := parseReasoningAnswers(text)
		if parseErr != nil {
			failures++
			result.Findings = append(result.Findings, "capability probe parse failed: "+parseErr.Error())
		} else {
			for _, c := range cases {
				tierTotal[c.Tier]++
				got := answers[strings.ToLower(c.ID)]
				ok, _ := equivalentAnswer(c.Expected, got)
				if ok {
					tierCorrect[c.Tier]++
				}
			}
		}
	}

	easyAcc := safeDiv(tierCorrect["easy"], tierTotal["easy"])
	medAcc := safeDiv(tierCorrect["medium"], tierTotal["medium"])
	hardAcc := safeDiv(tierCorrect["hard"], tierTotal["hard"])
	result.Metrics["identity_tier_scores"] = map[string]float64{
		"easy": round2(easyAcc), "medium": round2(medAcc), "hard": round2(hardAcc),
	}
	result.Findings = append(result.Findings, fmt.Sprintf("accuracy easy=%.0f%% medium=%.0f%% hard=%.0f%%", easyAcc*100, medAcc*100, hardAcc*100))

	// --- Probe 2: Latency-capability cross-validation ---
	latencies := []time.Duration{capDuration}
	pingRounds := 2
	if cfg.IdentityRounds > 0 {
		pingRounds = cfg.IdentityRounds
	}
	for i := 0; i < pingRounds; i++ {
		pingReq := anthropic.MessageRequest{
			Model:     cfg.Model,
			MaxTokens: 16,
			Messages: []anthropic.Message{
				{Role: "user", Content: "Reply with only the word 'pong'."},
			},
			Temperature: ptrFloat64(0),
		}
		pingStart := time.Now()
		pingResp, _, pingErr := client.CreateMessage(ctx, pingReq)
		latencies = append(latencies, time.Since(pingStart))
		if pingErr == nil && pingResp.Model != "" {
			responseModels = append(responseModels, pingResp.Model)
		}
	}
	medianLatency := medianDuration(latencies)
	medianMS := float64(medianLatency.Milliseconds())
	result.Metrics["identity_latency_median_ms"] = round2(medianMS)

	latencyConsistent := true
	if medianMS < 800 && hardAcc >= 0.6 {
		latencyConsistent = false
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("suspicious: fast latency %.0fms with high hard accuracy %.0f%%", medianMS, hardAcc*100))
	}
	if medianMS > 5000 && easyAcc < 0.8 {
		latencyConsistent = false
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("suspicious: slow latency %.0fms with low easy accuracy %.0f%%", medianMS, easyAcc*100))
	}
	result.Metrics["identity_latency_capability_consistent"] = latencyConsistent

	// --- Probe 3: Output style fingerprint ---
	stylePrompts := []string{
		"Explain the concept of entropy in information theory. Be thorough.",
		"Describe the differences between TCP and UDP protocols. Be thorough.",
	}
	outputLengths := []int{}
	for _, sp := range stylePrompts {
		styleReq := anthropic.MessageRequest{
			Model:     cfg.Model,
			MaxTokens: 512,
			Messages: []anthropic.Message{
				{Role: "user", Content: sp},
			},
			Temperature: ptrFloat64(0),
		}
		styleResp, _, styleErr := client.CreateMessage(ctx, styleReq)
		if styleErr == nil {
			outputLengths = append(outputLengths, styleResp.Usage.OutputTokens)
			if styleResp.Model != "" {
				responseModels = append(responseModels, styleResp.Model)
			}
		}
	}
	outputMedian := 0
	if len(outputLengths) > 0 {
		sort.Ints(outputLengths)
		outputMedian = outputLengths[len(outputLengths)/2]
	}
	result.Metrics["identity_output_length_median"] = outputMedian

	// --- Probe 4: Claimed model consistency ---
	modelMatch := true
	for _, rm := range responseModels {
		if rm != responseModels[0] {
			modelMatch = false
			break
		}
	}
	cfgModelMatch := len(responseModels) > 0 && strings.EqualFold(responseModels[0], cfg.Model)
	if !cfgModelMatch && len(responseModels) > 0 {
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("response model '%s' differs from requested '%s'", responseModels[0], cfg.Model))
	}
	if !modelMatch {
		warnings++
		result.Findings = append(result.Findings, "response model header inconsistent across probes")
	}
	claimedModel := cfg.Model
	responseModel := ""
	if len(responseModels) > 0 {
		responseModel = responseModels[0]
	}
	result.Metrics["identity_claimed_model"] = claimedModel
	result.Metrics["identity_response_model"] = responseModel
	result.Metrics["identity_model_match"] = modelMatch && cfgModelMatch

	// --- Tier estimation ---
	estimatedTier, confidence, tierScores := estimateTier(easyAcc, medAcc, hardAcc, outputMedian)
	claimedTier := parseClaimedTier(cfg.Model)

	result.Metrics["identity_estimated_tier"] = estimatedTier
	result.Metrics["identity_confidence"] = round2(confidence)
	result.Metrics["identity_tier_detail_scores"] = tierScores

	// Mismatch severity
	mismatch := false
	severity := 0
	if claimedTier != "unknown" && estimatedTier != "unknown" {
		claimedRank := tierRank(claimedTier)
		estimatedRank := tierRank(estimatedTier)
		diff := claimedRank - estimatedRank // positive = claimed higher than estimated
		if diff >= 2 {
			severity = 2
			mismatch = true
			failures++
			result.Findings = append(result.Findings, fmt.Sprintf("tier mismatch CRITICAL: claimed=%s estimated=%s (severity=%d)", claimedTier, estimatedTier, severity))
		} else if diff == 1 {
			severity = 1
			mismatch = true
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("tier mismatch WARN: claimed=%s estimated=%s (severity=%d)", claimedTier, estimatedTier, severity))
		}
	}
	result.Metrics["identity_tier_mismatch"] = mismatch
	result.Metrics["identity_tier_mismatch_severity"] = severity

	// --- Status ---
	switch {
	case failures > 0:
		result.Status = StatusFail
		result.Summary = "Identity tier verification found critical issues"
	case warnings > 0:
		result.Status = StatusWarn
		result.Summary = "Identity tier verification passed with caveats"
	default:
		result.Status = StatusPass
		result.Summary = "Identity tier verification passed"
	}
	return result
}

func buildIdentityPrompt(cases []identityCase) string {
	var b strings.Builder
	b.WriteString("Solve all questions. Output JSON only.\n")
	b.WriteString("Format: {\"e1\":\"...\",\"e2\":\"...\",...}\n")
	b.WriteString("Do not add markdown. Do not add explanations.\nKeep each answer short and exact.\n")
	for _, c := range cases {
		b.WriteString(c.ID)
		b.WriteString(": ")
		b.WriteString(c.Question)
		b.WriteString("\n")
	}
	return b.String()
}

func estimateTier(easyAcc, medAcc, hardAcc float64, outputMedian int) (string, float64, map[string]float64) {
	// Score each tier hypothesis
	scores := map[string]float64{
		"opus":   tierHypothesisScore(easyAcc, medAcc, hardAcc, outputMedian, "opus"),
		"sonnet": tierHypothesisScore(easyAcc, medAcc, hardAcc, outputMedian, "sonnet"),
		"haiku":  tierHypothesisScore(easyAcc, medAcc, hardAcc, outputMedian, "haiku"),
	}

	// Find best
	best := "unknown"
	bestScore := -1.0
	secondScore := -1.0
	for tier, score := range scores {
		if score > bestScore {
			secondScore = bestScore
			bestScore = score
			best = tier
		} else if score > secondScore {
			secondScore = score
		}
	}

	confidence := 0.0
	if bestScore > 0 {
		confidence = clamp((bestScore-secondScore)/bestScore, 0, 1)
	}
	return best, confidence, scores
}

func tierHypothesisScore(easyAcc, medAcc, hardAcc float64, outputMedian int, tier string) float64 {
	// Expected accuracy profiles per tier
	type profile struct {
		easy, med, hard float64
		outMin, outMax  int
	}
	profiles := map[string]profile{
		"opus":   {easy: 1.0, med: 0.8, hard: 0.6, outMin: 150, outMax: 600},
		"sonnet": {easy: 1.0, med: 0.6, hard: 0.2, outMin: 80, outMax: 350},
		"haiku":  {easy: 0.8, med: 0.3, hard: 0.0, outMin: 30, outMax: 150},
	}
	p := profiles[tier]

	// Weighted distance from expected profile (lower = better fit → higher score)
	score := 0.0
	score += 2.0 * (1.0 - absDiff(easyAcc, p.easy))
	score += 3.0 * (1.0 - absDiff(medAcc, p.med))
	score += 5.0 * (1.0 - absDiff(hardAcc, p.hard))

	// Output length bonus (soft signal)
	if outputMedian > 0 {
		if outputMedian >= p.outMin && outputMedian <= p.outMax {
			score += 1.0
		}
	}
	return score
}

func parseClaimedTier(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "opus"):
		return "opus"
	case strings.Contains(lower, "sonnet"):
		return "sonnet"
	case strings.Contains(lower, "haiku"):
		return "haiku"
	default:
		return "unknown"
	}
}

func tierRank(tier string) int {
	switch tier {
	case "opus":
		return 3
	case "sonnet":
		return 2
	case "haiku":
		return 1
	default:
		return 0
	}
}

func safeDiv(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom)
}

func absDiff(a, b float64) float64 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}

func medianDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(ds))
	copy(sorted, ds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

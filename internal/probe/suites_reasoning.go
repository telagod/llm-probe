package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"real-llm/internal/anthropic"
)

type ReasoningSuite struct{}

func (s ReasoningSuite) Name() string {
	return "reasoning"
}

func (s ReasoningSuite) Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result {
	result := Result{
		Status:   StatusPass,
		Summary:  "Reasoning and thinking checks passed",
		Findings: []string{},
		Metrics:  map[string]any{},
	}

	cases, bankMeta, selectedDomains, domainCounts, selectErr := selectReasoningCases(cfg)
	if selectErr != nil {
		result.Status = StatusFail
		result.Summary = "Failed to load reasoning prompt bank"
		result.Error = selectErr.Error()
		return result
	}
	result.Metrics["reasoning_bank_version"] = bankMeta.Version
	result.Metrics["reasoning_bank_name"] = bankMeta.Name
	result.Metrics["reasoning_bank_source"] = bankMeta.Source
	result.Metrics["reasoning_bank_created_at"] = bankMeta.CreatedAt
	result.Metrics["reasoning_bank_format"] = bankMeta.Format
	result.Metrics["reasoning_bank_path"] = bankMeta.Path
	result.Metrics["reasoning_case_count"] = len(cases)
	result.Metrics["reasoning_domains"] = selectedDomains
	result.Metrics["reasoning_domain_counts"] = domainCounts

	integrity := analyzeReasoningCaseSet(cases)
	result.Metrics["reasoning_duplicate_id_count"] = integrity.DuplicateIDCount
	result.Metrics["reasoning_duplicate_question_count"] = integrity.DuplicateQuestionCount
	result.Metrics["reasoning_duplicate_expected_count"] = integrity.DuplicateExpectedCount
	result.Metrics["reasoning_unique_expected_count"] = integrity.UniqueExpectedCount
	result.Metrics["reasoning_answer_max_share"] = integrity.MaxAnswerShare
	result.Metrics["reasoning_constant_guess_upper_bound"] = integrity.ConstantGuessUpperBound
	result.Metrics["reasoning_domain_max_share"] = integrity.MaxDomainShare

	repeats := cfg.ReasoningRepeat
	if repeats <= 0 {
		repeats = 1
	}

	baselineScores := make([]float64, 0, repeats)
	baselineWeightedScores := make([]float64, 0, repeats)
	baselineHashes := make([]string, 0, repeats)
	baselineDomainSeries := map[string][]float64{}
	failures := 0
	warnings := 0
	if integrity.DuplicateIDCount > 0 {
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("reasoning bank duplicate IDs detected: %d", integrity.DuplicateIDCount))
	}
	if integrity.DuplicateQuestionCount > maxInt(1, len(cases)/4) {
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("reasoning bank duplicate questions too high: %d", integrity.DuplicateQuestionCount))
	} else if integrity.DuplicateQuestionCount > maxInt(1, len(cases)/10) {
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("reasoning bank duplicate questions observed: %d", integrity.DuplicateQuestionCount))
	}
	if len(cases) >= 12 {
		switch {
		case integrity.ConstantGuessUpperBound > 0.6:
			failures++
			result.Findings = append(result.Findings, fmt.Sprintf("reasoning bank constant-answer upper bound too high: %.3f", integrity.ConstantGuessUpperBound))
		case integrity.ConstantGuessUpperBound > 0.4:
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("reasoning bank answer distribution concentrated: %.3f", integrity.ConstantGuessUpperBound))
		}
	}
	if len(selectedDomains) > 1 {
		switch {
		case integrity.MaxDomainShare > 0.85:
			failures++
			result.Findings = append(result.Findings, fmt.Sprintf("reasoning domain distribution is highly imbalanced: %.3f", integrity.MaxDomainShare))
		case integrity.MaxDomainShare > 0.7:
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("reasoning domain distribution is imbalanced: %.3f", integrity.MaxDomainShare))
		}
	}
	domainWarnThreshold, domainFailThreshold := resolveDomainThresholds(cfg)
	weightedWarnThreshold, weightedFailThreshold := resolveWeightedThresholds(cfg)

	for i := 0; i < repeats; i++ {
		req := anthropic.MessageRequest{
			Model:     cfg.Model,
			MaxTokens: 512,
			System:    "You are a strict evaluator over professional-domain reasoning tasks. Output JSON only. No prose.",
			Messages: []anthropic.Message{
				{
					Role:    "user",
					Content: buildReasoningPrompt(cases),
				},
			},
			Temperature: ptrFloat64(0),
		}
		resp, _, err := client.CreateMessage(ctx, req)
		if err != nil {
			failures++
			result.Findings = append(result.Findings, fmt.Sprintf("baseline round %d failed: %s", i+1, summarizeError(err)))
			continue
		}

		text := collectText(resp.Content)
		answers, parseErr := parseReasoningAnswers(text)
		if parseErr != nil {
			failures++
			result.Findings = append(result.Findings, fmt.Sprintf("baseline round %d parse failed: %s", i+1, parseErr.Error()))
			continue
		}

		eval := evaluateReasoningAnswers(cases, answers)
		baselineScores = append(baselineScores, eval.Score)
		baselineWeightedScores = append(baselineWeightedScores, eval.WeightedScore)
		baselineHashes = append(baselineHashes, canonicalAnswerMap(answers))
		for domain, domainEval := range eval.DomainEvals {
			baselineDomainSeries[domain] = append(baselineDomainSeries[domain], domainEval.Score)
		}
		result.Findings = append(result.Findings, fmt.Sprintf(
			"baseline round %d score=%.3f weighted=%.3f (%s)",
			i+1,
			eval.Score,
			eval.WeightedScore,
			firstN(eval.Detail, 480),
		))
	}

	if len(baselineScores) == 0 {
		result.Status = StatusFail
		result.Summary = "All baseline reasoning rounds failed"
		result.Metrics["failures"] = failures
		result.Metrics["warnings"] = warnings
		return result
	}

	baselineAvg := mean(baselineScores)
	baselineWeightedAvg := mean(baselineWeightedScores)
	result.Metrics["baseline_scores"] = baselineScores
	result.Metrics["baseline_avg_score"] = baselineAvg
	result.Metrics["baseline_weighted_scores"] = baselineWeightedScores
	result.Metrics["baseline_avg_weighted_score"] = baselineWeightedAvg

	unique := uniqueCount(baselineHashes)
	result.Metrics["baseline_unique_answer_sets"] = unique
	if unique > 1 {
		warnings++
		result.Findings = append(result.Findings, "baseline deterministic consistency drift detected across repeats")
	}

	domainAvg := map[string]float64{}
	domainMin := 1.0
	for _, domain := range selectedDomains {
		series := baselineDomainSeries[domain]
		if len(series) == 0 {
			continue
		}
		value := mean(series)
		domainAvg[domain] = value
		if value < domainMin {
			domainMin = value
		}
	}
	if len(domainAvg) == 0 {
		domainMin = 0
	}
	result.Metrics["baseline_domain_avg_accuracy"] = domainAvg
	result.Metrics["baseline_domain_min_accuracy"] = domainMin
	result.Metrics["reasoning_domain_warn_threshold"] = domainWarnThreshold
	result.Metrics["reasoning_domain_fail_threshold"] = domainFailThreshold
	result.Metrics["reasoning_weighted_warn_threshold"] = weightedWarnThreshold
	result.Metrics["reasoning_weighted_fail_threshold"] = weightedFailThreshold

	for _, domain := range selectedDomains {
		score := domainAvg[domain]
		switch {
		case score < domainFailThreshold:
			failures++
			result.Findings = append(result.Findings, fmt.Sprintf("domain %s accuracy %.3f < fail threshold %.3f", domain, score, domainFailThreshold))
		case score < domainWarnThreshold:
			warnings++
			result.Findings = append(result.Findings, fmt.Sprintf("domain %s accuracy %.3f < warn threshold %.3f", domain, score, domainWarnThreshold))
		}
	}

	switch {
	case baselineWeightedAvg < weightedFailThreshold:
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("weighted baseline score %.3f < fail threshold %.3f", baselineWeightedAvg, weightedFailThreshold))
	case baselineWeightedAvg < weightedWarnThreshold:
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("weighted baseline score %.3f < warn threshold %.3f", baselineWeightedAvg, weightedWarnThreshold))
	}

	thinkingReq := anthropic.MessageRequest{
		Model:     cfg.Model,
		MaxTokens: 1024,
		System:    "You are a strict evaluator over professional-domain reasoning tasks. Output JSON only. No prose.",
		Messages: []anthropic.Message{
			{
				Role:    "user",
				Content: buildReasoningPrompt(cases),
			},
		},
		Temperature: ptrFloat64(0),
		Thinking: &anthropic.ThinkingConfig{
			Type:         "enabled",
			BudgetTokens: 2048,
		},
	}
	thinkingResp, _, thinkingErr := client.CreateMessage(ctx, thinkingReq)
	if thinkingErr != nil {
		warnings++
		result.Findings = append(result.Findings, "thinking-enabled request rejected: "+summarizeError(thinkingErr))
	} else {
		thinkingText := collectText(thinkingResp.Content)
		thinkingAnswers, parseErr := parseReasoningAnswers(thinkingText)
		if parseErr != nil {
			warnings++
			result.Findings = append(result.Findings, "thinking response parse failed: "+parseErr.Error())
		} else {
			eval := evaluateReasoningAnswers(cases, thinkingAnswers)
			result.Metrics["thinking_score"] = eval.Score
			result.Metrics["thinking_weighted_score"] = eval.WeightedScore
			result.Metrics["thinking_domain_accuracy"] = domainScoreMap(eval.DomainEvals)
			result.Findings = append(result.Findings, fmt.Sprintf("thinking score=%.3f weighted=%.3f (%s)", eval.Score, eval.WeightedScore, firstN(eval.Detail, 480)))
			if eval.Score+0.0001 < baselineAvg {
				warnings++
				result.Findings = append(result.Findings, "thinking score is lower than baseline average")
			}
			if eval.WeightedScore+0.0001 < baselineWeightedAvg {
				warnings++
				result.Findings = append(result.Findings, "thinking weighted score is lower than baseline weighted average")
			}
		}

		thinkingBlocks := 0
		signatures := 0
		for _, block := range thinkingResp.Content {
			if block.Type == "thinking" {
				thinkingBlocks++
				if strings.TrimSpace(block.Signature) != "" {
					signatures++
				}
			}
		}
		result.Metrics["thinking_block_count"] = thinkingBlocks
		result.Metrics["thinking_signature_count"] = signatures
		if thinkingBlocks == 0 {
			warnings++
			result.Findings = append(result.Findings, "no thinking blocks observed in thinking-enabled response")
		} else if signatures == 0 {
			warnings++
			result.Findings = append(result.Findings, "thinking blocks present but signatures missing")
		}

		if cfg.DeepProbe && thinkingBlocks > 0 && signatures > 0 {
			tampered := cloneBlocks(thinkingResp.Content)
			tamperedIdx := -1
			for i, block := range tampered {
				if block.Type == "thinking" {
					tamperedIdx = i
					tampered[i].Signature = "tampered_signature_probe"
					break
				}
			}

			if tamperedIdx >= 0 {
				tamperReq := anthropic.MessageRequest{
					Model:     cfg.Model,
					MaxTokens: 64,
					Thinking: &anthropic.ThinkingConfig{
						Type:         "enabled",
						BudgetTokens: 512,
					},
					Messages: []anthropic.Message{
						{Role: "user", Content: "Compute 11+29. Return JSON {\"q1\":\"...\"}."},
						{Role: "assistant", Content: tampered},
						{Role: "user", Content: "Repeat final answer only in JSON."},
					},
				}
				_, _, tamperErr := client.CreateMessage(ctx, tamperReq)
				if tamperErr == nil {
					warnings++
					result.Findings = append(result.Findings, "tampered thinking signature was accepted")
				} else if apiErr, ok := anthropic.IsAPIError(tamperErr); ok {
					msg := strings.ToLower(apiErr.Envelope.Error.Message)
					if strings.Contains(msg, "signature") || strings.Contains(msg, "thinking") {
						result.Findings = append(result.Findings, "tampered thinking signature rejected as expected")
					} else {
						warnings++
						result.Findings = append(result.Findings, "tampered thinking rejected, but reason is non-signature-specific")
					}
				} else {
					warnings++
					result.Findings = append(result.Findings, "tampered thinking probe non-API error: "+tamperErr.Error())
				}
			}
		}
	}

	if baselineAvg < 0.5 {
		failures++
		result.Findings = append(result.Findings, fmt.Sprintf("baseline reasoning average too low: %.3f", baselineAvg))
	} else if baselineAvg < 0.75 {
		warnings++
		result.Findings = append(result.Findings, fmt.Sprintf("baseline reasoning average moderate: %.3f", baselineAvg))
	}

	switch {
	case failures > 0:
		result.Status = StatusFail
		result.Summary = "Reasoning/thinking checks found critical issues"
	case warnings > 0:
		result.Status = StatusWarn
		result.Summary = "Reasoning/thinking checks passed with caveats"
	default:
		result.Status = StatusPass
		result.Summary = "Reasoning accuracy and thinking integrity checks passed"
	}
	result.Metrics["failures"] = failures
	result.Metrics["warnings"] = warnings
	return result
}

func buildReasoningPrompt(cases []reasoningCase) string {
	var builder strings.Builder
	builder.WriteString("Solve all questions. Output JSON only.\n")
	if len(cases) >= 2 {
		builder.WriteString("Format example: {\"")
		builder.WriteString(cases[0].ID)
		builder.WriteString("\":\"...\",\"")
		builder.WriteString(cases[1].ID)
		builder.WriteString("\":\"...\"}\n")
	} else {
		builder.WriteString("Format: {\"id\":\"answer\"}\n")
	}
	builder.WriteString("Do not add markdown. Do not add explanations.\n")
	builder.WriteString("Keep each answer short and exact.\n")
	for _, c := range cases {
		builder.WriteString(c.ID)
		builder.WriteString(" [")
		builder.WriteString(c.Domain)
		builder.WriteString("]")
		builder.WriteString(": ")
		builder.WriteString(c.Question)
		builder.WriteString("\n")
	}
	return builder.String()
}

func parseReasoningAnswers(text string) (map[string]string, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return nil, fmt.Errorf("empty response")
	}

	objText := extractJSONObject(raw)
	if objText == "" {
		return nil, fmt.Errorf("json object not found in response")
	}

	var generic map[string]any
	if err := json.Unmarshal([]byte(objText), &generic); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	out := make(map[string]string, len(generic))
	for k, v := range generic {
		out[strings.ToLower(strings.TrimSpace(k))] = normalizeValue(v)
	}
	return out, nil
}

func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return text[start : end+1]
}

func normalizeValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(value))
	case float64:
		if float64(int64(value)) == value {
			return strconv.FormatInt(int64(value), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(value, 'f', -1, 64))
	case bool:
		if value {
			return "yes"
		}
		return "no"
	default:
		b, _ := json.Marshal(value)
		return normalizeAnswer(string(b))
	}
}

func normalizeAnswer(s string) string {
	clean := strings.ToLower(strings.TrimSpace(s))
	clean = strings.Trim(clean, "\"'` ")
	clean = strings.ReplaceAll(clean, " ", "")
	return clean
}

type reasoningIntegrity struct {
	DuplicateIDCount        int
	DuplicateQuestionCount  int
	DuplicateExpectedCount  int
	UniqueExpectedCount     int
	MaxAnswerShare          float64
	ConstantGuessUpperBound float64
	MaxDomainShare          float64
}

func analyzeReasoningCaseSet(cases []reasoningCase) reasoningIntegrity {
	out := reasoningIntegrity{}
	if len(cases) == 0 {
		return out
	}

	idSeen := map[string]bool{}
	questionSeen := map[string]bool{}
	expectedCounts := map[string]int{}
	domainCounts := map[string]int{}
	maxExpected := 0
	maxDomain := 0

	for _, c := range cases {
		id := normalizeAnswer(c.ID)
		if idSeen[id] {
			out.DuplicateIDCount++
		}
		idSeen[id] = true

		question := normalizeAnswer(c.Question)
		if questionSeen[question] {
			out.DuplicateQuestionCount++
		}
		questionSeen[question] = true

		expected := normalizeAnswer(c.Expected)
		expectedCounts[expected]++
		if expectedCounts[expected] > maxExpected {
			maxExpected = expectedCounts[expected]
		}

		domain := normalizeAnswer(c.Domain)
		domainCounts[domain]++
		if domainCounts[domain] > maxDomain {
			maxDomain = domainCounts[domain]
		}
	}

	out.UniqueExpectedCount = len(expectedCounts)
	for _, count := range expectedCounts {
		if count > 1 {
			out.DuplicateExpectedCount += count - 1
		}
	}
	out.MaxAnswerShare = float64(maxExpected) / float64(len(cases))
	out.ConstantGuessUpperBound = out.MaxAnswerShare
	out.MaxDomainShare = float64(maxDomain) / float64(len(cases))
	return out
}

var (
	numberPattern   = regexp.MustCompile(`[-+]?\d[\d,]*(?:\.\d+)?`)
	nonAlnumPattern = regexp.MustCompile(`[^a-z0-9]+`)
)

func equivalentAnswer(expectedRaw, gotRaw string) (bool, string) {
	expected := normalizeAnswer(expectedRaw)
	got := normalizeAnswer(gotRaw)
	if expected == got {
		return true, "exact"
	}

	for _, candidate := range splitExpectedCandidates(expectedRaw) {
		if normalizeAnswer(candidate) == got {
			return true, "alt_exact"
		}
	}

	if sameBoolAnswer(expected, got) {
		return true, "bool_equivalent"
	}
	if sameChoiceAnswer(expected, got) {
		return true, "choice_equivalent"
	}
	if sameDateAnswer(expected, got) {
		return true, "date_equivalent"
	}
	if sameTextualAnswer(expectedRaw, gotRaw) {
		return true, "text_semantic"
	}
	if sameNumericAnswer(expected, got) {
		return true, "numeric_equivalent"
	}

	for _, candidate := range splitExpectedCandidates(expectedRaw) {
		normalizedCandidate := normalizeAnswer(candidate)
		if sameBoolAnswer(normalizedCandidate, got) ||
			sameChoiceAnswer(normalizedCandidate, got) ||
			sameDateAnswer(normalizedCandidate, got) ||
			sameTextualAnswer(candidate, gotRaw) ||
			sameNumericAnswer(normalizedCandidate, got) {
			return true, "alt_semantic"
		}
	}
	return false, "mismatch"
}

func splitExpectedCandidates(expected string) []string {
	raw := strings.TrimSpace(expected)
	if raw == "" || !strings.Contains(raw, "||") {
		return nil
	}
	parts := strings.Split(raw, "||")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func sameBoolAnswer(expected, got string) bool {
	toBool := func(v string) (bool, bool) {
		switch normalizeAnswer(v) {
		case "yes", "true", "y", "1":
			return true, true
		case "no", "false", "n", "0":
			return false, true
		default:
			return false, false
		}
	}
	ev, eok := toBool(expected)
	gv, gok := toBool(got)
	return eok && gok && ev == gv
}

func sameChoiceAnswer(expected, got string) bool {
	normalizeChoice := func(v string) string {
		clean := strings.ToLower(strings.TrimSpace(v))
		clean = strings.TrimPrefix(clean, "option")
		clean = strings.TrimPrefix(clean, "choice")
		clean = strings.Trim(clean, "()[]{}<>:;,. ")
		if len(clean) == 1 && clean[0] >= 'a' && clean[0] <= 'f' {
			return clean
		}
		return ""
	}
	e := normalizeChoice(expected)
	g := normalizeChoice(got)
	return e != "" && g != "" && e == g
}

func sameDateAnswer(expected, got string) bool {
	parseDate := func(v string) (time.Time, bool) {
		v = strings.TrimSpace(v)
		layouts := []string{
			"2006-01-02",
			"2006/01/02",
			time.RFC3339,
		}
		for _, layout := range layouts {
			parsed, err := time.Parse(layout, v)
			if err == nil {
				return parsed.UTC().Truncate(24 * time.Hour), true
			}
		}
		return time.Time{}, false
	}
	ed, eok := parseDate(expected)
	gd, gok := parseDate(got)
	return eok && gok && ed.Equal(gd)
}

func sameTextualAnswer(expectedRaw, gotRaw string) bool {
	expected := canonicalTextualForm(expectedRaw)
	got := canonicalTextualForm(gotRaw)
	if expected == "" || got == "" {
		return false
	}
	return expected == got
}

var textualStopTokens = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "of": {}, "to": {}, "and": {},
	"final": {}, "answer": {}, "result": {}, "value": {}, "is": {},
	"only": {}, "return": {}, "output": {}, "please": {}, "be": {},
	"primary": {},
}

var textualSynonymTokens = map[string]string{
	"true":        "yes",
	"affirmative": "yes",
	"false":       "no",
	"negative":    "no",
	"option":      "",
	"choice":      "",
}

var phraseAliases = map[string]string{
	"primarymetabolicacidosis":    "metabolic acidosis",
	"primarymetabolicalkalosis":   "metabolic alkalosis",
	"primaryrespiratoryacidosis":  "respiratory acidosis",
	"primaryrespiratoryalkalosis": "respiratory alkalosis",
}

func canonicalTextualForm(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if clean == "" {
		return ""
	}
	compact := nonAlnumPattern.ReplaceAllString(clean, "")
	if alias, ok := phraseAliases[compact]; ok {
		clean = alias
		compact = nonAlnumPattern.ReplaceAllString(clean, "")
	}

	tokensRaw := strings.Fields(nonAlnumPattern.ReplaceAllString(clean, " "))
	if len(tokensRaw) == 0 {
		return compact
	}

	tokens := make([]string, 0, len(tokensRaw))
	for _, token := range tokensRaw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		token = singularizeToken(token)
		if mapped, ok := textualSynonymTokens[token]; ok {
			token = mapped
		}
		if token == "" {
			continue
		}
		if _, skip := textualStopTokens[token]; skip {
			continue
		}
		if _, err := strconv.ParseFloat(token, 64); err == nil {
			continue
		}
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		return compact
	}
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

func singularizeToken(token string) string {
	switch {
	case strings.HasSuffix(token, "ies") && len(token) > 4:
		return token[:len(token)-3] + "y"
	case strings.HasSuffix(token, "s") && len(token) > 4 && !strings.HasSuffix(token, "ss"):
		return token[:len(token)-1]
	default:
		return token
	}
}

type quantity struct {
	Value     float64
	Dimension string
}

type unitDefinition struct {
	Factor    float64
	Dimension string
}

var unitDefinitions = map[string]unitDefinition{
	"%":            {Factor: 0.01, Dimension: "ratio"},
	"percent":      {Factor: 0.01, Dimension: "ratio"},
	"percentage":   {Factor: 0.01, Dimension: "ratio"},
	"ratio":        {Factor: 1, Dimension: "ratio"},
	"s":            {Factor: 1, Dimension: "time_s"},
	"sec":          {Factor: 1, Dimension: "time_s"},
	"second":       {Factor: 1, Dimension: "time_s"},
	"seconds":      {Factor: 1, Dimension: "time_s"},
	"ms":           {Factor: 0.001, Dimension: "time_s"},
	"msec":         {Factor: 0.001, Dimension: "time_s"},
	"millisecond":  {Factor: 0.001, Dimension: "time_s"},
	"milliseconds": {Factor: 0.001, Dimension: "time_s"},
	"m":            {Factor: 60, Dimension: "time_s"},
	"min":          {Factor: 60, Dimension: "time_s"},
	"minute":       {Factor: 60, Dimension: "time_s"},
	"minutes":      {Factor: 60, Dimension: "time_s"},
	"h":            {Factor: 3600, Dimension: "time_s"},
	"hr":           {Factor: 3600, Dimension: "time_s"},
	"hour":         {Factor: 3600, Dimension: "time_s"},
	"hours":        {Factor: 3600, Dimension: "time_s"},
	"d":            {Factor: 86400, Dimension: "time_s"},
	"day":          {Factor: 86400, Dimension: "time_s"},
	"days":         {Factor: 86400, Dimension: "time_s"},
	"y":            {Factor: 31536000, Dimension: "time_s"},
	"yr":           {Factor: 31536000, Dimension: "time_s"},
	"year":         {Factor: 31536000, Dimension: "time_s"},
	"years":        {Factor: 31536000, Dimension: "time_s"},
	"b":            {Factor: 1, Dimension: "bytes"},
	"byte":         {Factor: 1, Dimension: "bytes"},
	"bytes":        {Factor: 1, Dimension: "bytes"},
	"kb":           {Factor: 1000, Dimension: "bytes"},
	"mb":           {Factor: 1000 * 1000, Dimension: "bytes"},
	"gb":           {Factor: 1000 * 1000 * 1000, Dimension: "bytes"},
	"tb":           {Factor: 1000 * 1000 * 1000 * 1000, Dimension: "bytes"},
	"kib":          {Factor: 1024, Dimension: "bytes"},
	"mib":          {Factor: 1024 * 1024, Dimension: "bytes"},
	"gib":          {Factor: 1024 * 1024 * 1024, Dimension: "bytes"},
	"tib":          {Factor: 1024 * 1024 * 1024 * 1024, Dimension: "bytes"},
	"ki":           {Factor: 1024, Dimension: "bytes"},
	"mi":           {Factor: 1024 * 1024, Dimension: "bytes"},
	"gi":           {Factor: 1024 * 1024 * 1024, Dimension: "bytes"},
	"ti":           {Factor: 1024 * 1024 * 1024 * 1024, Dimension: "bytes"},
	"mg":           {Factor: 0.001, Dimension: "mass_g"},
	"g":            {Factor: 1, Dimension: "mass_g"},
	"kg":           {Factor: 1000, Dimension: "mass_g"},
	"usd":          {Factor: 1, Dimension: "currency"},
	"dollar":       {Factor: 1, Dimension: "currency"},
	"dollars":      {Factor: 1, Dimension: "currency"},
}

func sameNumericAnswer(expected, got string) bool {
	if matched, decided := compareWithUnits(expected, got); decided {
		return matched
	}

	expectedValues := extractNumericValues(expected)
	gotValues := extractNumericValues(got)
	if len(expectedValues) == 0 || len(gotValues) == 0 {
		return false
	}
	for _, ev := range expectedValues {
		for _, gv := range gotValues {
			if nearlyEqualNumeric(ev, gv) {
				return true
			}
		}
	}
	return false
}

func compareWithUnits(expected, got string) (bool, bool) {
	ev, eok := parseQuantity(expected)
	gv, gok := parseQuantity(got)
	if !eok || !gok {
		return false, false
	}
	if ev.Dimension != gv.Dimension {
		return false, true
	}
	return nearlyEqualNumeric(ev.Value, gv.Value), true
}

func parseQuantity(raw string) (quantity, bool) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.ReplaceAll(clean, ",", "")
	if clean == "" {
		return quantity{}, false
	}

	prefixUnit := ""
	if strings.HasPrefix(clean, "$") {
		prefixUnit = "usd"
		clean = strings.TrimSpace(strings.TrimPrefix(clean, "$"))
	}

	matches := numberPattern.FindStringIndex(clean)
	if matches == nil || matches[0] != 0 {
		return quantity{}, false
	}
	numberText := clean[matches[0]:matches[1]]
	value, err := strconv.ParseFloat(numberText, 64)
	if err != nil {
		return quantity{}, false
	}

	unitToken := strings.TrimSpace(clean[matches[1]:])
	if unitToken == "" && prefixUnit != "" {
		unitToken = prefixUnit
	}
	if unitToken == "" {
		return quantity{}, false
	}

	unitToken = extractLeadingUnitToken(unitToken)
	if unitToken == "" {
		return quantity{}, false
	}
	if unitToken == "%" {
		def := unitDefinitions["%"]
		return quantity{Value: value * def.Factor, Dimension: def.Dimension}, true
	}
	unitToken = normalizeUnitToken(unitToken)
	def, ok := unitDefinitions[unitToken]
	if !ok {
		return quantity{}, false
	}
	return quantity{
		Value:     value * def.Factor,
		Dimension: def.Dimension,
	}, true
}

func extractLeadingUnitToken(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return ""
	}
	if strings.HasPrefix(clean, "%") {
		return "%"
	}
	var builder strings.Builder
	for _, ch := range clean {
		if (ch >= 'a' && ch <= 'z') || ch == '%' {
			builder.WriteRune(ch)
			continue
		}
		if ch == ' ' || ch == '_' || ch == '-' {
			if builder.Len() == 0 {
				continue
			}
			break
		}
		if builder.Len() > 0 {
			break
		}
	}
	return strings.TrimSpace(builder.String())
}

func normalizeUnitToken(token string) string {
	clean := strings.ToLower(strings.TrimSpace(token))
	clean = strings.Trim(clean, ".,;:()[]{}")
	clean = strings.ReplaceAll(clean, "_", "")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, "µ", "u")
	clean = strings.ReplaceAll(clean, "μ", "u")
	return clean
}

func extractNumericValues(value string) []float64 {
	clean := strings.TrimSpace(strings.ToLower(value))
	if clean == "" {
		return nil
	}

	values := []float64{}
	add := func(v float64) {
		for _, existing := range values {
			if math.Abs(existing-v) <= 1e-9 {
				return
			}
		}
		values = append(values, v)
	}

	parseOne := func(raw string) {
		raw = strings.TrimSpace(strings.Trim(raw, "$"))
		raw = strings.ReplaceAll(raw, ",", "")
		if raw == "" {
			return
		}
		if strings.HasSuffix(raw, "%") {
			base := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
			if base != "" {
				if v, err := strconv.ParseFloat(base, 64); err == nil {
					add(v)
					add(v / 100)
				}
			}
			return
		}
		if strings.Contains(raw, "/") {
			parts := strings.Split(raw, "/")
			if len(parts) == 2 {
				n, nErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
				d, dErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if nErr == nil && dErr == nil && d != 0 {
					add(n / d)
					return
				}
			}
		}
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			add(v)
		}
	}

	parseOne(clean)
	matches := numberPattern.FindAllString(clean, -1)
	for _, match := range matches {
		parseOne(match)
	}
	return values
}

func nearlyEqualNumeric(expected, got float64) bool {
	diff := math.Abs(expected - got)
	tol := math.Max(1e-6, math.Abs(expected)*0.001)
	return diff <= tol
}

type reasoningDomainEval struct {
	Total   int
	Correct int
	Score   float64
}

type reasoningEval struct {
	Score         float64
	WeightedScore float64
	Detail        string
	DomainEvals   map[string]reasoningDomainEval
}

func evaluateReasoningAnswers(cases []reasoningCase, answers map[string]string) reasoningEval {
	out := reasoningEval{
		DomainEvals: map[string]reasoningDomainEval{},
	}
	if len(cases) == 0 {
		out.Detail = "no cases"
		return out
	}
	correct := 0
	totalWeight := 0.0
	correctWeight := 0.0
	parts := make([]string, 0, len(cases))
	for _, c := range cases {
		got := normalizeAnswer(answers[strings.ToLower(c.ID)])
		weight := difficultyWeight(c.Difficulty)
		totalWeight += weight
		domain := strings.ToLower(strings.TrimSpace(c.Domain))
		eval := out.DomainEvals[domain]
		eval.Total++
		ok, matchType := equivalentAnswer(c.Expected, got)
		if ok {
			correct++
			correctWeight += weight
			eval.Correct++
			parts = append(parts, c.ID+"=ok("+matchType+")")
		} else {
			parts = append(parts, fmt.Sprintf("%s=got(%s)want(%s)", c.ID, got, normalizeAnswer(c.Expected)))
		}
		out.DomainEvals[domain] = eval
	}
	out.Score = float64(correct) / float64(len(cases))
	if totalWeight > 0 {
		out.WeightedScore = correctWeight / totalWeight
	}
	for domain, eval := range out.DomainEvals {
		if eval.Total > 0 {
			eval.Score = float64(eval.Correct) / float64(eval.Total)
		}
		out.DomainEvals[domain] = eval
	}
	out.Detail = strings.Join(parts, ", ")
	return out
}

func difficultyWeight(level string) float64 {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "hard":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func domainScoreMap(domainEvals map[string]reasoningDomainEval) map[string]float64 {
	out := map[string]float64{}
	for domain, eval := range domainEvals {
		out[domain] = eval.Score
	}
	return out
}

func resolveDomainThresholds(cfg RunConfig) (warn, fail float64) {
	warn = cfg.ReasoningDomainWarn
	fail = cfg.ReasoningDomainFail
	if warn <= 0 || warn > 1 {
		warn = 0.8
	}
	if fail <= 0 || fail > 1 {
		fail = 0.6
	}
	if fail > warn {
		fail = warn
	}
	return warn, fail
}

func resolveWeightedThresholds(cfg RunConfig) (warn, fail float64) {
	warn = cfg.ReasoningWeightedWarn
	fail = cfg.ReasoningWeightedFail
	if warn <= 0 || warn > 1 {
		warn = 0.8
	}
	if fail <= 0 || fail > 1 {
		fail = 0.65
	}
	if fail > warn {
		fail = warn
	}
	return warn, fail
}

func canonicalAnswerMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+normalizeAnswer(values[key]))
	}
	return strings.Join(parts, ";")
}

func uniqueCount(items []string) int {
	seen := map[string]struct{}{}
	for _, item := range items {
		seen[item] = struct{}{}
	}
	return len(seen)
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func cloneBlocks(in []anthropic.ContentBlock) []anthropic.ContentBlock {
	out := make([]anthropic.ContentBlock, len(in))
	copy(out, in)
	return out
}

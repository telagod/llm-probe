package probe

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var gsm8kAnswerPattern = regexp.MustCompile(`[-+]?\d[\d,]*(?:\.\d+)?`)

type ReasoningImportConfig struct {
	InputPath  string
	OutputPath string
	Format     string
	Domain     string
	Name       string
	Source     string
}

type ReasoningImportSummary struct {
	Format     string   `json:"format"`
	InputPath  string   `json:"input_path"`
	OutputPath string   `json:"output_path"`
	Version    string   `json:"version"`
	Name       string   `json:"name"`
	Source     string   `json:"source"`
	CaseCount  int      `json:"case_count"`
	Domains    []string `json:"domains"`
}

func ImportReasoningBank(cfg ReasoningImportConfig) (ReasoningImportSummary, error) {
	inputPath := strings.TrimSpace(cfg.InputPath)
	if inputPath == "" {
		return ReasoningImportSummary{}, fmt.Errorf("reasoning import input path is required")
	}
	outputPath := strings.TrimSpace(cfg.OutputPath)
	if outputPath == "" {
		return ReasoningImportSummary{}, fmt.Errorf("reasoning import output path is required")
	}

	resolvedFormat, err := resolveReasoningImportFormat(strings.TrimSpace(cfg.Format), inputPath)
	if err != nil {
		return ReasoningImportSummary{}, err
	}
	domain := normalizeReasoningImportDomain(cfg.Domain, inputPath, resolvedFormat)

	var rawCases []reasoningCase
	switch resolvedFormat {
	case "gsm8k_jsonl":
		rawCases, err = importGSM8KJSONL(inputPath, domain)
	case "bbh_jsonl":
		rawCases, err = importBBHJSONL(inputPath, domain)
	case "arc_jsonl":
		rawCases, err = importARCJSONL(inputPath, domain)
	case "mmlu_csv":
		rawCases, err = importMMLUCSV(inputPath, domain)
	case "gpqa_csv":
		rawCases, err = importGPQACSV(inputPath, domain)
	default:
		err = fmt.Errorf("unsupported reasoning import format %q", resolvedFormat)
	}
	if err != nil {
		return ReasoningImportSummary{}, err
	}

	clean, err := sanitizeReasoningCases(rawCases)
	if err != nil {
		return ReasoningImportSummary{}, err
	}

	bank := reasoningBankEnvelope{
		Version:   reasoningBankSchemaVersion,
		Name:      firstNonEmpty(strings.TrimSpace(cfg.Name), defaultReasoningBankName(outputPath)),
		Source:    firstNonEmpty(strings.TrimSpace(cfg.Source), fmt.Sprintf("import:%s:%s", resolvedFormat, filepath.Clean(inputPath))),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Cases:     clean,
	}

	serialized, err := json.MarshalIndent(bank, "", "  ")
	if err != nil {
		return ReasoningImportSummary{}, fmt.Errorf("encode reasoning bank: %w", err)
	}

	cleanOutputPath := filepath.Clean(outputPath)
	parent := filepath.Dir(cleanOutputPath)
	if parent != "." {
		if mkErr := os.MkdirAll(parent, 0o755); mkErr != nil {
			return ReasoningImportSummary{}, fmt.Errorf("create output directory: %w", mkErr)
		}
	}
	if writeErr := os.WriteFile(cleanOutputPath, serialized, 0o644); writeErr != nil {
		return ReasoningImportSummary{}, fmt.Errorf("write reasoning bank: %w", writeErr)
	}

	domains := domainSet(clean)
	return ReasoningImportSummary{
		Format:     resolvedFormat,
		InputPath:  filepath.Clean(inputPath),
		OutputPath: cleanOutputPath,
		Version:    bank.Version,
		Name:       bank.Name,
		Source:     bank.Source,
		CaseCount:  len(clean),
		Domains:    domains,
	}, nil
}

func resolveReasoningImportFormat(rawFormat, inputPath string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(rawFormat))
	if format == "" || format == "auto" {
		base := strings.ToLower(filepath.Base(inputPath))
		ext := strings.ToLower(filepath.Ext(inputPath))
		switch ext {
		case ".jsonl", ".ndjson":
			switch {
			case strings.Contains(base, "bbh"):
				return "bbh_jsonl", nil
			case strings.Contains(base, "arc"):
				return "arc_jsonl", nil
			default:
				return "gsm8k_jsonl", nil
			}
		case ".json":
			if strings.Contains(base, "arc") {
				return "arc_jsonl", nil
			}
			return "gsm8k_jsonl", nil
		case ".csv":
			if strings.Contains(base, "gpqa") {
				return "gpqa_csv", nil
			}
			return "mmlu_csv", nil
		default:
			return "", fmt.Errorf("cannot auto-detect reasoning import format for %q (supported: .jsonl/.ndjson/.json/.csv)", inputPath)
		}
	}

	normalized := strings.ReplaceAll(format, "-", "_")
	normalized = strings.ReplaceAll(normalized, ".", "_")
	switch normalized {
	case "gsm8k", "gsm8k_jsonl", "jsonl":
		return "gsm8k_jsonl", nil
	case "bbh", "bbh_jsonl":
		return "bbh_jsonl", nil
	case "arc", "arc_jsonl":
		return "arc_jsonl", nil
	case "mmlu", "mmlu_csv", "csv":
		return "mmlu_csv", nil
	case "gpqa", "gpqa_csv":
		return "gpqa_csv", nil
	default:
		return "", fmt.Errorf("unsupported reasoning import format %q (use gsm8k_jsonl|bbh_jsonl|arc_jsonl|mmlu_csv|gpqa_csv|auto)", rawFormat)
	}
}

func normalizeReasoningImportDomain(rawDomain, inputPath, format string) string {
	domain := strings.ToLower(strings.TrimSpace(rawDomain))
	if domain != "" {
		return domain
	}

	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	base = strings.ToLower(strings.TrimSpace(base))
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "-", "_")
	if base != "" {
		switch {
		case strings.Contains(base, "gsm8k"):
			return "math_reasoning"
		case strings.Contains(base, "bbh"):
			return "benchmark_reasoning"
		case strings.Contains(base, "arc"):
			return "science_qa"
		case strings.Contains(base, "mmlu"):
			return "general_knowledge"
		case strings.Contains(base, "gpqa"):
			return "graduate_science"
		default:
			return base
		}
	}

	switch format {
	case "gsm8k_jsonl":
		return "math_reasoning"
	case "bbh_jsonl":
		return "benchmark_reasoning"
	case "arc_jsonl":
		return "science_qa"
	case "gpqa_csv":
		return "graduate_science"
	default:
		return "general_knowledge"
	}
}

func readJSONObjectRecords(path string) ([]json.RawMessage, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("input file is empty")
	}

	if strings.HasPrefix(trimmed, "[") {
		var rows []json.RawMessage
		if unmarshalErr := json.Unmarshal([]byte(trimmed), &rows); unmarshalErr != nil {
			return nil, fmt.Errorf("parse json array: %w", unmarshalErr)
		}
		return rows, nil
	}

	var single json.RawMessage
	if singleErr := json.Unmarshal([]byte(trimmed), &single); singleErr == nil {
		return []json.RawMessage{single}, nil
	}

	file, openErr := os.Open(filepath.Clean(path))
	if openErr != nil {
		return nil, openErr
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	rows := make([]json.RawMessage, 0, 1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw json.RawMessage
		if unmarshalErr := json.Unmarshal([]byte(line), &raw); unmarshalErr != nil {
			return nil, fmt.Errorf("parse json line %d: %w", lineNo, unmarshalErr)
		}
		rows = append(rows, raw)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, scanErr
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no json records found")
	}
	return rows, nil
}

func importBBHJSONL(path string, domain string) ([]reasoningCase, error) {
	records, err := readJSONObjectRecords(path)
	if err != nil {
		return nil, fmt.Errorf("open bbh file: %w", err)
	}

	out := make([]reasoningCase, 0, len(records))
	for idx, raw := range records {
		var item map[string]any
		if unmarshalErr := json.Unmarshal(raw, &item); unmarshalErr != nil {
			return nil, fmt.Errorf("parse bbh record %d: %w", idx+1, unmarshalErr)
		}

		question := firstMapString(item, "input", "question", "prompt")
		expected := firstMapValue(item, "target", "answer", "expected", "label")
		if question == "" || expected == "" {
			continue
		}
		prompt := question
		if !strings.Contains(strings.ToLower(prompt), "return") {
			prompt += " Return final short answer only."
		}
		out = append(out, reasoningCase{
			ID:         fmt.Sprintf("bbh_%05d", len(out)+1),
			Domain:     domain,
			Difficulty: "hard",
			Question:   prompt,
			Expected:   expected,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bbh import produced zero cases")
	}
	return out, nil
}

type choiceOption struct {
	Label string
	Text  string
}

func importARCJSONL(path string, domain string) ([]reasoningCase, error) {
	records, err := readJSONObjectRecords(path)
	if err != nil {
		return nil, fmt.Errorf("open arc file: %w", err)
	}

	out := make([]reasoningCase, 0, len(records))
	for idx, raw := range records {
		var item map[string]any
		if unmarshalErr := json.Unmarshal(raw, &item); unmarshalErr != nil {
			return nil, fmt.Errorf("parse arc record %d: %w", idx+1, unmarshalErr)
		}

		question, choices := extractARCQuestion(item)
		answerKey := firstMapValue(item, "answerKey", "answer", "target", "label")
		expected := normalizeChoiceExpected(answerKey, choices)
		if question == "" || len(choices) < 2 || expected == "" {
			continue
		}

		out = append(out, reasoningCase{
			ID:         fmt.Sprintf("arc_%05d", len(out)+1),
			Domain:     domain,
			Difficulty: "hard",
			Question:   buildChoicePrompt(question, choices),
			Expected:   expected,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("arc import produced zero cases")
	}
	return out, nil
}

func extractARCQuestion(item map[string]any) (string, []choiceOption) {
	var question string
	choices := []choiceOption{}

	if qObj, ok := item["question"].(map[string]any); ok {
		question = firstMapString(qObj, "stem", "question", "text")
		choices = parseChoiceList(qObj["choices"])
	} else {
		question = firstMapString(item, "question", "stem", "prompt")
	}
	if len(choices) == 0 {
		choices = parseChoiceList(item["choices"])
	}
	return question, choices
}

func parseChoiceList(value any) []choiceOption {
	rawList, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]choiceOption, 0, len(rawList))
	for i, item := range rawList {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text := firstMapString(entry, "text", "choice", "value")
		if text == "" {
			continue
		}
		label := strings.TrimSpace(strings.ToUpper(firstMapString(entry, "label", "id", "key")))
		if label == "" {
			label = string(rune('A' + i))
		}
		out = append(out, choiceOption{
			Label: label,
			Text:  text,
		})
	}
	return out
}

func importGPQACSV(path string, domain string) ([]reasoningCase, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open gpqa csv file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read gpqa csv: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("gpqa csv is empty")
	}

	headers := headerIndexMap(records[0])
	qIdx := headerIndex(headers, "question", "prompt")
	correctIdx := headerIndex(headers, "correct_answer", "correct")
	wrong1Idx := headerIndex(headers, "incorrect_answer_1", "wrong_answer_1")
	wrong2Idx := headerIndex(headers, "incorrect_answer_2", "wrong_answer_2")
	wrong3Idx := headerIndex(headers, "incorrect_answer_3", "wrong_answer_3")

	out := make([]reasoningCase, 0, len(records))
	if qIdx >= 0 && correctIdx >= 0 && wrong1Idx >= 0 && wrong2Idx >= 0 && wrong3Idx >= 0 {
		for i := 1; i < len(records); i++ {
			row := records[i]
			if len(row) <= maxIndex(qIdx, correctIdx, wrong1Idx, wrong2Idx, wrong3Idx) {
				continue
			}
			question := strings.TrimSpace(row[qIdx])
			correct := strings.TrimSpace(row[correctIdx])
			w1 := strings.TrimSpace(row[wrong1Idx])
			w2 := strings.TrimSpace(row[wrong2Idx])
			w3 := strings.TrimSpace(row[wrong3Idx])
			if question == "" || correct == "" || w1 == "" || w2 == "" || w3 == "" {
				continue
			}

			choices, expected := buildGPQAChoices(correct, []string{w1, w2, w3}, len(out))
			out = append(out, reasoningCase{
				ID:         fmt.Sprintf("gpqa_%05d", len(out)+1),
				Domain:     domain,
				Difficulty: "hard",
				Question:   buildChoicePrompt(question, choices),
				Expected:   expected,
			})
		}
	} else {
		hasHeader, q, a, opts := parseMMLUHeader(records[0])
		start := 0
		if hasHeader {
			start = 1
		}
		for i := start; i < len(records); i++ {
			row := records[i]
			if len(row) <= maxIndex(q, a, opts[3]) {
				continue
			}
			question := strings.TrimSpace(row[q])
			options := [4]string{
				strings.TrimSpace(row[opts[0]]),
				strings.TrimSpace(row[opts[1]]),
				strings.TrimSpace(row[opts[2]]),
				strings.TrimSpace(row[opts[3]]),
			}
			answer := strings.TrimSpace(row[a])
			if question == "" || options[0] == "" || options[1] == "" || options[2] == "" || options[3] == "" || answer == "" {
				continue
			}
			expected := normalizeMMLUAnswer(answer, options)
			if expected == "" {
				continue
			}
			out = append(out, reasoningCase{
				ID:         fmt.Sprintf("gpqa_%05d", len(out)+1),
				Domain:     domain,
				Difficulty: "hard",
				Question:   buildMMLUPrompt(question, options),
				Expected:   expected,
			})
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("gpqa import produced zero cases")
	}
	return out, nil
}

func buildGPQAChoices(correct string, wrong []string, index int) ([]choiceOption, string) {
	labels := []string{"A", "B", "C", "D"}
	choices := make([]choiceOption, 4)
	correctPos := index % 4
	choices[correctPos] = choiceOption{Label: labels[correctPos], Text: correct}
	wrongIdx := 0
	for i := range choices {
		if i == correctPos {
			continue
		}
		choices[i] = choiceOption{Label: labels[i], Text: wrong[wrongIdx]}
		wrongIdx++
	}
	return choices, strings.ToLower(labels[correctPos])
}

func importGSM8KJSONL(path string, domain string) ([]reasoningCase, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open gsm8k file: %w", err)
	}
	defer file.Close()

	type row struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Solution string `json:"solution"`
		Expected string `json:"expected"`
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	out := make([]reasoningCase, 0, 2048)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item row
		if unmarshalErr := json.Unmarshal([]byte(line), &item); unmarshalErr != nil {
			return nil, fmt.Errorf("parse gsm8k jsonl line %d: %w", lineNo, unmarshalErr)
		}

		question := strings.TrimSpace(item.Question)
		expected := strings.TrimSpace(item.Expected)
		if expected == "" {
			expected = extractGSM8KExpected(item.Answer)
		}
		if expected == "" {
			expected = extractGSM8KExpected(item.Solution)
		}
		if question == "" || expected == "" {
			continue
		}

		prompt := question
		if !strings.Contains(strings.ToLower(prompt), "return") {
			prompt += " Return final numeric answer only."
		}
		out = append(out, reasoningCase{
			ID:         fmt.Sprintf("gsm8k_%05d", len(out)+1),
			Domain:     domain,
			Difficulty: "medium",
			Question:   prompt,
			Expected:   expected,
		})
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("scan gsm8k jsonl: %w", scanErr)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("gsm8k import produced zero cases")
	}
	return out, nil
}

func firstMapString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := item[key]
		if !ok {
			continue
		}
		text := valueToString(value)
		if text != "" {
			return text
		}
	}
	return ""
}

func firstMapValue(item map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := item[key]
		if !ok {
			continue
		}
		text := strings.TrimSpace(valueToString(value))
		if text != "" {
			return text
		}
	}
	return ""
}

func valueToString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if float64(int64(v)) == v {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []any:
		if len(v) == 0 {
			return ""
		}
		return valueToString(v[0])
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func normalizeChoiceExpected(raw string, choices []choiceOption) string {
	answer := strings.ToLower(strings.TrimSpace(raw))
	answer = strings.Trim(answer, "\"'() ")
	if answer == "" {
		return ""
	}

	for _, choice := range choices {
		label := strings.ToLower(strings.TrimSpace(choice.Label))
		if label == answer {
			return label
		}
	}

	if number, err := strconv.Atoi(answer); err == nil {
		if number >= 1 && number <= len(choices) {
			return strings.ToLower(strings.TrimSpace(choices[number-1].Label))
		}
		if number >= 0 && number < len(choices) {
			return strings.ToLower(strings.TrimSpace(choices[number].Label))
		}
	}

	for _, choice := range choices {
		text := strings.ToLower(strings.TrimSpace(choice.Text))
		if text != "" && text == answer {
			return strings.ToLower(strings.TrimSpace(choice.Label))
		}
	}
	return ""
}

func buildChoicePrompt(question string, choices []choiceOption) string {
	var builder strings.Builder
	builder.WriteString(question)
	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		label := strings.TrimSpace(strings.ToUpper(choice.Label))
		if label == "" {
			continue
		}
		labels = append(labels, label)
		builder.WriteString("\n")
		builder.WriteString(label)
		builder.WriteString(") ")
		builder.WriteString(choice.Text)
	}
	builder.WriteString("\nReturn one label only: ")
	builder.WriteString(strings.Join(labels, ", "))
	builder.WriteString(".")
	return builder.String()
}

func headerIndexMap(row []string) map[string]int {
	out := map[string]int{}
	for i, value := range row {
		normalized := normalizeHeader(value)
		if normalized == "" {
			continue
		}
		out[normalized] = i
	}
	return out
}

func headerIndex(header map[string]int, keys ...string) int {
	for _, key := range keys {
		normalized := normalizeHeader(key)
		if idx, ok := header[normalized]; ok {
			return idx
		}
	}
	return -1
}

func normalizeHeader(value string) string {
	clean := strings.ToLower(strings.TrimSpace(value))
	clean = strings.ReplaceAll(clean, "-", "_")
	clean = strings.ReplaceAll(clean, " ", "_")
	return clean
}

func extractGSM8KExpected(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if idx := strings.LastIndex(text, "####"); idx >= 0 {
		text = strings.TrimSpace(text[idx+4:])
	}

	matches := gsm8kAnswerPattern.FindAllString(text, -1)
	if len(matches) > 0 {
		candidate := matches[len(matches)-1]
		candidate = strings.ReplaceAll(candidate, ",", "")
		candidate = strings.TrimSpace(candidate)
		candidate = strings.Trim(candidate, ".")
		return candidate
	}

	text = strings.TrimSpace(strings.Trim(text, "$"))
	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(strings.Trim(text, "."))
	return text
}

func importMMLUCSV(path string, domain string) ([]reasoningCase, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open mmlu csv file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read mmlu csv: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("mmlu csv is empty")
	}

	start := 0
	questionIdx, answerIdx, optionIdx := 0, 5, [4]int{1, 2, 3, 4}
	if ok, q, a, opts := parseMMLUHeader(records[0]); ok {
		start = 1
		questionIdx = q
		answerIdx = a
		optionIdx = opts
	}

	out := make([]reasoningCase, 0, len(records)-start)
	for i := start; i < len(records); i++ {
		row := records[i]
		requiredMax := maxIndex(questionIdx, answerIdx, optionIdx[3])
		if len(row) <= requiredMax {
			continue
		}
		question := strings.TrimSpace(row[questionIdx])
		options := [4]string{
			strings.TrimSpace(row[optionIdx[0]]),
			strings.TrimSpace(row[optionIdx[1]]),
			strings.TrimSpace(row[optionIdx[2]]),
			strings.TrimSpace(row[optionIdx[3]]),
		}
		answer := strings.TrimSpace(row[answerIdx])
		if question == "" || options[0] == "" || options[1] == "" || options[2] == "" || options[3] == "" || answer == "" {
			continue
		}

		expected := normalizeMMLUAnswer(answer, options)
		if expected == "" {
			continue
		}

		prompt := buildMMLUPrompt(question, options)
		out = append(out, reasoningCase{
			ID:         fmt.Sprintf("mmlu_%05d", len(out)+1),
			Domain:     domain,
			Difficulty: "hard",
			Question:   prompt,
			Expected:   expected,
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("mmlu import produced zero cases")
	}
	return out, nil
}

func parseMMLUHeader(row []string) (bool, int, int, [4]int) {
	normalized := make([]string, len(row))
	for i, value := range row {
		normalized[i] = strings.ToLower(strings.TrimSpace(value))
	}

	questionIdx := indexOfAny(normalized, "question", "prompt")
	answerIdx := indexOfAny(normalized, "answer", "target", "label")
	aIdx := indexOfAny(normalized, "a", "option_a", "choice_a")
	bIdx := indexOfAny(normalized, "b", "option_b", "choice_b")
	cIdx := indexOfAny(normalized, "c", "option_c", "choice_c")
	dIdx := indexOfAny(normalized, "d", "option_d", "choice_d")

	if questionIdx >= 0 && answerIdx >= 0 && aIdx >= 0 && bIdx >= 0 && cIdx >= 0 && dIdx >= 0 {
		return true, questionIdx, answerIdx, [4]int{aIdx, bIdx, cIdx, dIdx}
	}
	return false, 0, 5, [4]int{1, 2, 3, 4}
}

func indexOfAny(values []string, targets ...string) int {
	set := map[string]struct{}{}
	for _, target := range targets {
		set[target] = struct{}{}
	}
	for idx, value := range values {
		if _, ok := set[value]; ok {
			return idx
		}
	}
	return -1
}

func normalizeMMLUAnswer(raw string, options [4]string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.Trim(value, "\"' ")
	if value == "" {
		return ""
	}

	if len(value) == 1 && value[0] >= 'a' && value[0] <= 'd' {
		return value
	}
	if n, err := strconv.Atoi(value); err == nil {
		if n >= 0 && n <= 3 {
			return string(rune('a' + n))
		}
		if n >= 1 && n <= 4 {
			return string(rune('a' + (n - 1)))
		}
	}

	trimmed := strings.TrimPrefix(value, "(")
	trimmed = strings.TrimSuffix(trimmed, ")")
	trimmed = strings.TrimPrefix(trimmed, "option")
	trimmed = strings.TrimSpace(trimmed)
	if len(trimmed) == 1 && trimmed[0] >= 'a' && trimmed[0] <= 'd' {
		return trimmed
	}

	normalizedOptions := [4]string{}
	for i, option := range options {
		normalizedOptions[i] = strings.ToLower(strings.TrimSpace(option))
	}
	for i, option := range normalizedOptions {
		if option != "" && option == value {
			return string(rune('a' + i))
		}
	}
	return ""
}

func buildMMLUPrompt(question string, options [4]string) string {
	var builder strings.Builder
	builder.WriteString(question)
	builder.WriteString("\nA) ")
	builder.WriteString(options[0])
	builder.WriteString("\nB) ")
	builder.WriteString(options[1])
	builder.WriteString("\nC) ")
	builder.WriteString(options[2])
	builder.WriteString("\nD) ")
	builder.WriteString(options[3])
	builder.WriteString("\nReturn one letter only: A, B, C, or D.")
	return builder.String()
}

func maxIndex(values ...int) int {
	max := -1
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func domainSet(cases []reasoningCase) []string {
	seen := map[string]struct{}{}
	for _, c := range cases {
		domain := strings.TrimSpace(strings.ToLower(c.Domain))
		if domain == "" {
			continue
		}
		seen[domain] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for domain := range seen {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out
}

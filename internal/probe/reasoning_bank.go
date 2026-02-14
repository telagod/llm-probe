package probe

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	reasoningBankSchemaVersion = "1.0"
	embeddedReasoningBankRef   = "embedded:internal/probe/reasoning_bank.json"
)

//go:embed reasoning_bank.json
var reasoningBankJSON []byte

type reasoningCase struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	Difficulty string `json:"difficulty,omitempty"`
	Question   string `json:"question"`
	Expected   string `json:"expected"`
}

type reasoningBankEnvelope struct {
	Version   string          `json:"version,omitempty"`
	Name      string          `json:"name,omitempty"`
	Source    string          `json:"source,omitempty"`
	CreatedAt string          `json:"created_at,omitempty"`
	Cases     []reasoningCase `json:"cases"`
}

type reasoningBankMetadata struct {
	Version   string
	Name      string
	Source    string
	CreatedAt string
	Path      string
	Format    string
}

func selectReasoningCases(cfg RunConfig) ([]reasoningCase, reasoningBankMetadata, []string, map[string]int, error) {
	allCases, metadata, err := reasoningCaseBank(cfg.ReasoningBankPath)
	if err != nil {
		return nil, reasoningBankMetadata{}, nil, nil, err
	}

	maxCases := cfg.ReasoningMaxCases
	if maxCases <= 0 {
		maxCases = 32
	}

	filterSet := parseDomainFilter(cfg.ReasoningDomains)
	filtered := make([]reasoningCase, 0, len(allCases))
	for _, item := range allCases {
		domain := strings.ToLower(strings.TrimSpace(item.Domain))
		if len(filterSet) == 0 || filterSet["all"] {
			filtered = append(filtered, item)
			continue
		}
		if filterSet[domain] {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil, metadata, nil, nil, fmt.Errorf("no reasoning cases matched domain filter %q", cfg.ReasoningDomains)
	}

	grouped := map[string][]reasoningCase{}
	for _, item := range filtered {
		domain := strings.ToLower(strings.TrimSpace(item.Domain))
		grouped[domain] = append(grouped[domain], item)
	}
	domains := make([]string, 0, len(grouped))
	for domain := range grouped {
		domains = append(domains, domain)
		sort.SliceStable(grouped[domain], func(i, j int) bool {
			return grouped[domain][i].ID < grouped[domain][j].ID
		})
	}
	sort.Strings(domains)

	selected := make([]reasoningCase, 0, minInt(maxCases, len(filtered)))
	counts := map[string]int{}
	for len(selected) < maxCases {
		progress := false
		for _, domain := range domains {
			items := grouped[domain]
			if len(items) == 0 {
				continue
			}
			progress = true
			next := items[0]
			grouped[domain] = items[1:]
			selected = append(selected, next)
			counts[domain]++
			if len(selected) >= maxCases {
				break
			}
		}
		if !progress {
			break
		}
	}

	activeDomains := make([]string, 0, len(counts))
	for domain, count := range counts {
		if count > 0 {
			activeDomains = append(activeDomains, domain)
		}
	}
	sort.Strings(activeDomains)
	return selected, metadata, activeDomains, counts, nil
}

func reasoningCaseBank(bankPath string) ([]reasoningCase, reasoningBankMetadata, error) {
	metadata := reasoningBankMetadata{
		Path: embeddedReasoningBankRef,
	}
	data := reasoningBankJSON
	requestedPath := strings.TrimSpace(bankPath)
	if requestedPath != "" {
		cleanPath := filepath.Clean(requestedPath)
		loaded, err := os.ReadFile(cleanPath)
		if err != nil {
			return nil, reasoningBankMetadata{}, fmt.Errorf("read reasoning bank file %q: %w", cleanPath, err)
		}
		data = loaded
		metadata.Path = cleanPath
	}

	cases, parsedMeta, err := parseReasoningBank(data, metadata)
	if err != nil {
		return nil, reasoningBankMetadata{}, err
	}
	return cases, parsedMeta, nil
}

func parseReasoningBank(data []byte, metadata reasoningBankMetadata) ([]reasoningCase, reasoningBankMetadata, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, reasoningBankMetadata{}, fmt.Errorf("reasoning bank %q is empty", metadata.Path)
	}

	if trimmed[0] == '[' {
		var legacy []reasoningCase
		if err := json.Unmarshal(trimmed, &legacy); err != nil {
			return nil, reasoningBankMetadata{}, fmt.Errorf("parse legacy reasoning bank %q: %w", metadata.Path, err)
		}
		clean, err := sanitizeReasoningCases(legacy)
		if err != nil {
			return nil, reasoningBankMetadata{}, err
		}
		metadata.Version = "legacy-array"
		metadata.Name = defaultReasoningBankName(metadata.Path)
		metadata.Source = metadata.Path
		metadata.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		metadata.Format = "legacy_array"
		return clean, metadata, nil
	}

	var envelope reasoningBankEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, reasoningBankMetadata{}, fmt.Errorf("parse reasoning bank envelope %q: %w", metadata.Path, err)
	}

	clean, err := sanitizeReasoningCases(envelope.Cases)
	if err != nil {
		return nil, reasoningBankMetadata{}, err
	}

	metadata.Version = firstNonEmpty(strings.TrimSpace(envelope.Version), reasoningBankSchemaVersion)
	metadata.Name = firstNonEmpty(strings.TrimSpace(envelope.Name), defaultReasoningBankName(metadata.Path))
	metadata.Source = firstNonEmpty(strings.TrimSpace(envelope.Source), metadata.Path)
	metadata.CreatedAt = firstNonEmpty(strings.TrimSpace(envelope.CreatedAt), time.Now().UTC().Format(time.RFC3339))
	metadata.Format = "envelope"
	return clean, metadata, nil
}

func sanitizeReasoningCases(items []reasoningCase) ([]reasoningCase, error) {
	clean := make([]reasoningCase, 0, len(items))
	for _, item := range items {
		item.ID = strings.TrimSpace(strings.ToLower(item.ID))
		item.Domain = strings.TrimSpace(strings.ToLower(item.Domain))
		item.Question = strings.TrimSpace(item.Question)
		item.Expected = strings.TrimSpace(item.Expected)
		item.Difficulty = strings.TrimSpace(strings.ToLower(item.Difficulty))
		if item.ID == "" || item.Domain == "" || item.Question == "" || item.Expected == "" {
			continue
		}
		clean = append(clean, item)
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("reasoning bank has no valid cases")
	}

	sort.SliceStable(clean, func(i, j int) bool {
		if clean[i].Domain != clean[j].Domain {
			return clean[i].Domain < clean[j].Domain
		}
		return clean[i].ID < clean[j].ID
	})
	return clean, nil
}

func defaultReasoningBankName(path string) string {
	if strings.HasPrefix(path, "embedded:") {
		return "embedded-default"
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSpace(strings.TrimSuffix(base, ext))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "reasoning-bank"
	}
	return strings.ToLower(name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseDomainFilter(raw string) map[string]bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" || value == "all" {
		return map[string]bool{"all": true}
	}
	items := strings.Split(value, ",")
	out := map[string]bool{}
	for _, item := range items {
		name := strings.TrimSpace(strings.ToLower(item))
		if name == "" {
			continue
		}
		out[name] = true
	}
	if len(out) == 0 {
		return map[string]bool{"all": true}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

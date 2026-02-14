package server

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"real-llm/internal/probe"
)

type KeyLease struct {
	Label  string
	APIKey string
	keyRef *testKeyState
}

type BudgetManager struct {
	mu            sync.Mutex
	keys          []*testKeyState
	defaultRunUSD float64
}

type testKeyState struct {
	Config           TestKeyConfig
	DayKey           string
	SpentUSD         float64
	RequestsLastMin  []time.Time
	InputTokens1Min  []tokenMark
	OutputTokens1Min []tokenMark
	ActiveRuns       int
}

type tokenMark struct {
	At    time.Time
	Count int
}

func NewBudgetManager(cfg ServerConfig) *BudgetManager {
	manager := &BudgetManager{
		keys:          []*testKeyState{},
		defaultRunUSD: cfg.Budget.DefaultRunMaxUSD,
	}
	for _, key := range cfg.Keys.TestKeys {
		item := key
		if strings.TrimSpace(item.APIKey) == "" {
			continue
		}
		if strings.TrimSpace(item.Label) == "" {
			item.Label = fmt.Sprintf("key-%d", len(manager.keys)+1)
		}
		if item.DailyLimitUSD <= 0 {
			item.DailyLimitUSD = 100
		}
		if item.RPM <= 0 {
			item.RPM = 30
		}
		if item.TPM <= 0 {
			item.TPM = 250000
		}
		if item.InputCostPer1K <= 0 {
			item.InputCostPer1K = 0.003
		}
		if item.OutputCostPer1K <= 0 {
			item.OutputCostPer1K = 0.015
		}
		manager.keys = append(manager.keys, &testKeyState{Config: item})
	}
	return manager
}

func (m *BudgetManager) Acquire(budgetCapUSD float64) (KeyLease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.keys) == 0 {
		return KeyLease{}, errors.New("no test keys configured")
	}
	capUSD := budgetCapUSD
	if capUSD <= 0 {
		capUSD = m.defaultRunUSD
	}
	now := time.Now()
	dayKey := now.UTC().Format("2006-01-02")
	candidates := make([]*testKeyState, 0, len(m.keys))
	for _, key := range m.keys {
		m.rollWindow(key, now, dayKey)
		remainingUSD := key.Config.DailyLimitUSD - key.SpentUSD
		if remainingUSD < capUSD {
			continue
		}
		if len(key.RequestsLastMin) >= key.Config.RPM {
			continue
		}
		if tokensInWindow(key.InputTokens1Min)+tokensInWindow(key.OutputTokens1Min) >= key.Config.TPM {
			continue
		}
		candidates = append(candidates, key)
	}
	if len(candidates) == 0 {
		return KeyLease{}, errors.New("all test keys are budget or rate limited")
	}
	sort.Slice(candidates, func(i, j int) bool {
		leftRemain := candidates[i].Config.DailyLimitUSD - candidates[i].SpentUSD
		rightRemain := candidates[j].Config.DailyLimitUSD - candidates[j].SpentUSD
		if leftRemain == rightRemain {
			return candidates[i].ActiveRuns < candidates[j].ActiveRuns
		}
		return leftRemain > rightRemain
	})
	selected := candidates[0]
	selected.ActiveRuns++
	selected.RequestsLastMin = append(selected.RequestsLastMin, now)
	return KeyLease{
		Label:  selected.Config.Label,
		APIKey: selected.Config.APIKey,
		keyRef: selected,
	}, nil
}

func (m *BudgetManager) Commit(lease KeyLease, usage KeyUsageRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lease.keyRef == nil {
		return
	}
	now := time.Now()
	dayKey := now.UTC().Format("2006-01-02")
	m.rollWindow(lease.keyRef, now, dayKey)
	if usage.EstimatedCostUSD > 0 {
		lease.keyRef.SpentUSD += usage.EstimatedCostUSD
	}
	if usage.InputTokens > 0 {
		lease.keyRef.InputTokens1Min = append(lease.keyRef.InputTokens1Min, tokenMark{At: now, Count: usage.InputTokens})
	}
	if usage.OutputTokens > 0 {
		lease.keyRef.OutputTokens1Min = append(lease.keyRef.OutputTokens1Min, tokenMark{At: now, Count: usage.OutputTokens})
	}
	if lease.keyRef.ActiveRuns > 0 {
		lease.keyRef.ActiveRuns--
	}
}

func (m *BudgetManager) Reject(lease KeyLease) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lease.keyRef == nil {
		return
	}
	if lease.keyRef.ActiveRuns > 0 {
		lease.keyRef.ActiveRuns--
	}
}

func (m *BudgetManager) rollWindow(state *testKeyState, now time.Time, dayKey string) {
	if state.DayKey != dayKey {
		state.DayKey = dayKey
		state.SpentUSD = 0
		state.InputTokens1Min = nil
		state.OutputTokens1Min = nil
		state.RequestsLastMin = nil
	}
	cutoff := now.Add(-1 * time.Minute)
	state.RequestsLastMin = filterRecentTime(state.RequestsLastMin, cutoff)
	state.InputTokens1Min = filterRecentMarks(state.InputTokens1Min, cutoff)
	state.OutputTokens1Min = filterRecentMarks(state.OutputTokens1Min, cutoff)
}

func filterRecentTime(items []time.Time, cutoff time.Time) []time.Time {
	if len(items) == 0 {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if item.After(cutoff) {
			out = append(out, item)
		}
	}
	return out
}

func filterRecentMarks(items []tokenMark, cutoff time.Time) []tokenMark {
	if len(items) == 0 {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if item.At.After(cutoff) {
			out = append(out, item)
		}
	}
	return out
}

func tokensInWindow(items []tokenMark) int {
	total := 0
	for _, item := range items {
		total += item.Count
	}
	return total
}

func EstimateUsage(report probe.Report) KeyUsageRecord {
	usage := KeyUsageRecord{
		RunID:        "",
		InputTokens:  0,
		OutputTokens: 0,
	}
	for _, result := range report.Results {
		for key, value := range result.Metrics {
			v, ok := toFloat(value)
			if !ok {
				continue
			}
			metricName := strings.ToLower(strings.TrimSpace(key))
			switch {
			case strings.Contains(metricName, "input_tokens"):
				usage.InputTokens += int(v)
			case strings.Contains(metricName, "output_tokens"):
				usage.OutputTokens += int(v)
			}
		}
	}
	return usage
}

func EstimateCostUSD(usage KeyUsageRecord, key TestKeyConfig) float64 {
	input := float64(usage.InputTokens) / 1000 * key.InputCostPer1K
	output := float64(usage.OutputTokens) / 1000 * key.OutputCostPer1K
	return input + output
}

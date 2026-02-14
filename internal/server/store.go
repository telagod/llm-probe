package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"real-llm/internal/probe"
)

type Store interface {
	CreateRun(meta RunMeta) error
	UpdateRun(runID string, mutate func(*RunMeta)) (RunMeta, error)
	GetRun(runID string) (RunMeta, bool)
	ListRuns(limit int) []RunMeta
	ListRunsByCreator(creatorSub string, limit int) []RunMeta
	AppendRunEvent(runID string, stage, message string, data map[string]any) (RunEvent, error)
	ListRunEvents(runID string, sinceSeq int64) []RunEvent
	AppendAudit(event AuditEvent) error
	ListAudit(limit int) []AuditEvent
	GetMetricsOverview() MetricsOverview
}

type MemoryFileStore struct {
	mu      sync.RWMutex
	path    string
	runs    map[string]RunMeta
	events  map[string][]RunEvent
	audit   []AuditEvent
	nextSeq map[string]int64
}

func NewMemoryFileStore(path string) (*MemoryFileStore, error) {
	store := &MemoryFileStore{
		path:    path,
		runs:    map[string]RunMeta{},
		events:  map[string][]RunEvent{},
		audit:   []AuditEvent{},
		nextSeq: map[string]int64{},
	}
	if strings.TrimSpace(path) == "" {
		return store, nil
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *MemoryFileStore) CreateRun(meta RunMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[meta.RunID]; exists {
		return fmt.Errorf("run %s already exists", meta.RunID)
	}
	s.runs[meta.RunID] = meta
	if _, ok := s.events[meta.RunID]; !ok {
		s.events[meta.RunID] = []RunEvent{}
	}
	if _, ok := s.nextSeq[meta.RunID]; !ok {
		s.nextSeq[meta.RunID] = 1
	}
	return s.persistLocked()
}

func (s *MemoryFileStore) UpdateRun(runID string, mutate func(*RunMeta)) (RunMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.runs[runID]
	if !ok {
		return RunMeta{}, fmt.Errorf("run not found: %s", runID)
	}
	if mutate != nil {
		mutate(&meta)
	}
	s.runs[runID] = meta
	if err := s.persistLocked(); err != nil {
		return RunMeta{}, err
	}
	return meta, nil
}

func (s *MemoryFileStore) GetRun(runID string) (RunMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, ok := s.runs[runID]
	return meta, ok
}

func (s *MemoryFileStore) ListRuns(limit int) []RunMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RunMeta, 0, len(s.runs))
	for _, meta := range s.runs {
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *MemoryFileStore) ListRunsByCreator(creatorSub string, limit int) []RunMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RunMeta, 0)
	for _, meta := range s.runs {
		if meta.CreatorSub == creatorSub {
			out = append(out, meta)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *MemoryFileStore) AppendRunEvent(runID string, stage, message string, data map[string]any) (RunEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[runID]; !ok {
		return RunEvent{}, fmt.Errorf("run not found: %s", runID)
	}
	seq := s.nextSeq[runID]
	if seq < 1 {
		seq = 1
	}
	event := RunEvent{
		Seq:       seq,
		Timestamp: nowRFC3339(),
		Stage:     stage,
		Message:   message,
		Data:      cloneMap(data),
	}
	s.nextSeq[runID] = seq + 1
	s.events[runID] = append(s.events[runID], event)
	if err := s.persistLocked(); err != nil {
		return RunEvent{}, err
	}
	return event, nil
}

func (s *MemoryFileStore) ListRunEvents(runID string, sinceSeq int64) []RunEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := s.events[runID]
	if len(events) == 0 {
		return []RunEvent{}
	}
	out := make([]RunEvent, 0, len(events))
	for _, event := range events {
		if event.Seq > sinceSeq {
			out = append(out, event)
		}
	}
	return out
}

func (s *MemoryFileStore) AppendAudit(event AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(event.Timestamp) == "" {
		event.Timestamp = nowRFC3339()
	}
	s.audit = append(s.audit, event)
	if len(s.audit) > 5000 {
		s.audit = s.audit[len(s.audit)-5000:]
	}
	return s.persistLocked()
}

func (s *MemoryFileStore) ListAudit(limit int) []AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.audit) == 0 {
		return []AuditEvent{}
	}
	out := make([]AuditEvent, len(s.audit))
	copy(out, s.audit)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp > out[j].Timestamp
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *MemoryFileStore) GetMetricsOverview() MetricsOverview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	overview := MetricsOverview{
		GeneratedAt: nowRFC3339(),
	}
	var durationTotal int64
	var trustTotal float64
	trustCount := 0
	for _, run := range s.runs {
		overview.TotalRuns++
		switch strings.ToLower(strings.TrimSpace(run.Status)) {
		case "running", "queued":
			overview.RunningRuns++
		case "pass":
			overview.PassRuns++
		case "warn":
			overview.WarnRuns++
		case "fail":
			overview.FailRuns++
		}
		overview.EstimatedCostUSD += run.EstimatedCost
		if run.Report != nil {
			durationTotal += reportDuration(*run.Report)
			for _, result := range run.Report.Results {
				if result.Suite != "trust_score" {
					continue
				}
				if v, ok := result.Metrics["hard_gate_hit_count"].(int); ok {
					overview.HardGateHits += v
				} else if vFloat, ok := toFloat(result.Metrics["hard_gate_hit_count"]); ok {
					overview.HardGateHits += int(vFloat)
				}
				if trust, ok := toFloat(result.Metrics["trust_score_final"]); ok {
					trustTotal += trust
					trustCount++
				}
			}
		}
	}
	if overview.TotalRuns > 0 {
		overview.AverageDuration = durationTotal / int64(overview.TotalRuns)
	}
	if trustCount > 0 {
		overview.AverageTrust = trustTotal / float64(trustCount)
	}
	return overview
}

func (s *MemoryFileStore) load() error {
	data, err := os.ReadFile(filepath.Clean(s.path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read store snapshot: %w", err)
	}
	var snapshot struct {
		Runs   []RunMeta             `json:"runs"`
		Events map[string][]RunEvent `json:"events"`
		Audit  []AuditEvent          `json:"audit"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode store snapshot: %w", err)
	}
	for _, run := range snapshot.Runs {
		s.runs[run.RunID] = run
	}
	for runID, events := range snapshot.Events {
		s.events[runID] = events
		maxSeq := int64(0)
		for _, event := range events {
			if event.Seq > maxSeq {
				maxSeq = event.Seq
			}
		}
		s.nextSeq[runID] = maxSeq + 1
	}
	s.audit = snapshot.Audit
	return nil
}

func (s *MemoryFileStore) persistLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	runs := make([]RunMeta, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt < runs[j].CreatedAt
	})
	snapshot := struct {
		Runs   []RunMeta             `json:"runs"`
		Events map[string][]RunEvent `json:"events"`
		Audit  []AuditEvent          `json:"audit"`
	}{
		Runs:   runs,
		Events: s.events,
		Audit:  s.audit,
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store snapshot: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write store temp snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace store snapshot: %w", err)
	}
	return nil
}

func reportDuration(report probe.Report) int64 {
	total := int64(0)
	for _, item := range report.Results {
		total += item.DurationMS
	}
	return total
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"real-llm/internal/probe"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) *PgStore {
	return &PgStore{pool: pool}
}

func (s *PgStore) CreateRun(meta RunMeta) error {
	req, _ := json.Marshal(meta.Request)
	risk, _ := json.Marshal(meta.Risk)
	ku, _ := json.Marshal(meta.KeyUsage)
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO runs (run_id,status,creator_type,creator_sub,creator_email,source,request,created_at,risk,key_usage,estimated_cost)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		meta.RunID, meta.Status, meta.CreatorType, meta.CreatorSub, meta.CreatorEmail,
		meta.Source, req, meta.CreatedAt, risk, ku, meta.EstimatedCost)
	return err
}

func (s *PgStore) UpdateRun(runID string, mutate func(*RunMeta)) (RunMeta, error) {
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return RunMeta{}, err
	}
	defer tx.Rollback(context.Background())

	row := tx.QueryRow(context.Background(),
		`SELECT run_id,status,creator_type,creator_sub,creator_email,source,request,
		        started_at,finished_at,created_at,error,report,risk,key_usage,estimated_cost
		 FROM runs WHERE run_id=$1 FOR UPDATE`, runID)
	meta, err := scanRunMeta(row)
	if err != nil {
		return RunMeta{}, fmt.Errorf("run not found: %s", runID)
	}
	if mutate != nil {
		mutate(&meta)
	}
	req, _ := json.Marshal(meta.Request)
	risk, _ := json.Marshal(meta.Risk)
	ku, _ := json.Marshal(meta.KeyUsage)
	var reportJSON []byte
	if meta.Report != nil {
		reportJSON, _ = json.Marshal(meta.Report)
	}
	_, err = tx.Exec(context.Background(),
		`UPDATE runs SET status=$1,started_at=$2,finished_at=$3,error=$4,report=$5,
		 risk=$6,key_usage=$7,estimated_cost=$8,request=$9 WHERE run_id=$10`,
		meta.Status, nullStr(meta.StartedAt), nullStr(meta.FinishedAt), meta.Error,
		reportJSON, risk, ku, meta.EstimatedCost, req, runID)
	if err != nil {
		return RunMeta{}, err
	}
	return meta, tx.Commit(context.Background())
}

func (s *PgStore) GetRun(runID string) (RunMeta, bool) {
	row := s.pool.QueryRow(context.Background(),
		`SELECT run_id,status,creator_type,creator_sub,creator_email,source,request,
		        started_at,finished_at,created_at,error,report,risk,key_usage,estimated_cost
		 FROM runs WHERE run_id=$1`, runID)
	meta, err := scanRunMeta(row)
	if err != nil {
		return RunMeta{}, false
	}
	return meta, true
}

func (s *PgStore) ListRuns(limit int) []RunMeta {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(context.Background(),
		`SELECT run_id,status,creator_type,creator_sub,creator_email,source,request,
		        started_at,finished_at,created_at,error,report,risk,key_usage,estimated_cost
		 FROM runs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []RunMeta
	for rows.Next() {
		meta, err := scanRunMeta(rows)
		if err != nil {
			continue
		}
		out = append(out, meta)
	}
	if out == nil {
		return []RunMeta{}
	}
	return out
}

func (s *PgStore) ListRunsByCreator(creatorSub string, limit int) []RunMeta {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(context.Background(),
		`SELECT run_id,status,creator_type,creator_sub,creator_email,source,request,
		        started_at,finished_at,created_at,error,report,risk,key_usage,estimated_cost
		 FROM runs WHERE creator_sub=$1 ORDER BY created_at DESC LIMIT $2`, creatorSub, limit)
	if err != nil {
		return []RunMeta{}
	}
	defer rows.Close()
	var out []RunMeta
	for rows.Next() {
		meta, err := scanRunMeta(rows)
		if err != nil {
			continue
		}
		out = append(out, meta)
	}
	if out == nil {
		return []RunMeta{}
	}
	return out
}

func (s *PgStore) AppendRunEvent(runID string, stage, message string, data map[string]any) (RunEvent, error) {
	var dataJSON []byte
	if data != nil {
		dataJSON, _ = json.Marshal(data)
	}
	var seq int64
	var ts time.Time
	err := s.pool.QueryRow(context.Background(),
		`INSERT INTO run_events (run_id, seq, stage, message, data)
		 VALUES ($1, COALESCE((SELECT MAX(seq) FROM run_events WHERE run_id=$1),0)+1, $2, $3, $4)
		 RETURNING seq, timestamp`, runID, stage, message, dataJSON).Scan(&seq, &ts)
	if err != nil {
		return RunEvent{}, err
	}
	return RunEvent{
		Seq:       seq,
		Timestamp: ts.UTC().Format(time.RFC3339),
		Stage:     stage,
		Message:   message,
		Data:      data,
	}, nil
}

func (s *PgStore) ListRunEvents(runID string, sinceSeq int64) []RunEvent {
	rows, err := s.pool.Query(context.Background(),
		`SELECT seq, timestamp, stage, message, data
		 FROM run_events WHERE run_id=$1 AND seq>$2 ORDER BY seq`, runID, sinceSeq)
	if err != nil {
		return []RunEvent{}
	}
	defer rows.Close()
	var out []RunEvent
	for rows.Next() {
		var e RunEvent
		var ts time.Time
		var dataJSON []byte
		if err := rows.Scan(&e.Seq, &ts, &e.Stage, &e.Message, &dataJSON); err != nil {
			continue
		}
		e.Timestamp = ts.UTC().Format(time.RFC3339)
		if len(dataJSON) > 0 {
			_ = json.Unmarshal(dataJSON, &e.Data)
		}
		out = append(out, e)
	}
	if out == nil {
		return []RunEvent{}
	}
	return out
}

func (s *PgStore) AppendAudit(event AuditEvent) error {
	if strings.TrimSpace(event.Timestamp) == "" {
		event.Timestamp = nowRFC3339()
	}
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO audit_events (timestamp,run_id,actor_type,actor_sub,action,result,ip_hash,ua_hash,detail)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		event.Timestamp, nullStr(event.RunID), event.ActorType, nullStr(event.ActorSub),
		event.Action, event.Result, nullStr(event.IPHash), nullStr(event.UAHash), nullStr(event.Detail))
	return err
}

func (s *PgStore) ListAudit(limit int) []AuditEvent {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.pool.Query(context.Background(),
		`SELECT timestamp,run_id,actor_type,actor_sub,action,result,ip_hash,ua_hash,detail
		 FROM audit_events ORDER BY timestamp DESC LIMIT $1`, limit)
	if err != nil {
		return []AuditEvent{}
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var a AuditEvent
		var ts time.Time
		var runID, actorSub, ipHash, uaHash, detail *string
		if err := rows.Scan(&ts, &runID, &a.ActorType, &actorSub, &a.Action, &a.Result, &ipHash, &uaHash, &detail); err != nil {
			continue
		}
		a.Timestamp = ts.UTC().Format(time.RFC3339)
		a.RunID = deref(runID)
		a.ActorSub = deref(actorSub)
		a.IPHash = deref(ipHash)
		a.UAHash = deref(uaHash)
		a.Detail = deref(detail)
		out = append(out, a)
	}
	if out == nil {
		return []AuditEvent{}
	}
	return out
}

func (s *PgStore) GetMetricsOverview() MetricsOverview {
	overview := MetricsOverview{GeneratedAt: nowRFC3339()}
	_ = s.pool.QueryRow(context.Background(),
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status IN ('running','queued')),
			COUNT(*) FILTER (WHERE status='pass'),
			COUNT(*) FILTER (WHERE status='warn'),
			COUNT(*) FILTER (WHERE status='fail'),
			COALESCE(SUM(estimated_cost),0)
		 FROM runs`).Scan(
		&overview.TotalRuns, &overview.RunningRuns, &overview.PassRuns,
		&overview.WarnRuns, &overview.FailRuns, &overview.EstimatedCostUSD)

	// trust score + hard gate from reports
	rows, _ := s.pool.Query(context.Background(),
		`SELECT report FROM runs WHERE report IS NOT NULL`)
	if rows != nil {
		defer rows.Close()
		var trustTotal float64
		var trustCount int
		var durationTotal int64
		for rows.Next() {
			var reportJSON []byte
			if rows.Scan(&reportJSON) != nil {
				continue
			}
			var report probe.Report
			if json.Unmarshal(reportJSON, &report) != nil {
				continue
			}
			durationTotal += reportDuration(report)
			for _, result := range report.Results {
				if result.Suite != "trust_score" {
					continue
				}
				if v, ok := toFloat(result.Metrics["hard_gate_hit_count"]); ok {
					overview.HardGateHits += int(v)
				}
				if trust, ok := toFloat(result.Metrics["trust_score_final"]); ok {
					trustTotal += trust
					trustCount++
				}
			}
		}
		if overview.TotalRuns > 0 {
			overview.AverageDuration = durationTotal / int64(overview.TotalRuns)
		}
		if trustCount > 0 {
			overview.AverageTrust = trustTotal / float64(trustCount)
		}
	}
	return overview
}

// --- helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanRunMeta(row scannable) (RunMeta, error) {
	var m RunMeta
	var reqJSON, riskJSON, kuJSON []byte
	var reportJSON []byte
	var startedAt, finishedAt, creatorSub, creatorEmail, source, errStr *string
	err := row.Scan(&m.RunID, &m.Status, &m.CreatorType, &creatorSub, &creatorEmail,
		&source, &reqJSON, &startedAt, &finishedAt, &m.CreatedAt,
		&errStr, &reportJSON, &riskJSON, &kuJSON, &m.EstimatedCost)
	if err != nil {
		return RunMeta{}, err
	}
	m.CreatorSub = deref(creatorSub)
	m.CreatorEmail = deref(creatorEmail)
	m.Source = deref(source)
	m.StartedAt = deref(startedAt)
	m.FinishedAt = deref(finishedAt)
	m.Error = deref(errStr)
	_ = json.Unmarshal(reqJSON, &m.Request)
	_ = json.Unmarshal(riskJSON, &m.Risk)
	_ = json.Unmarshal(kuJSON, &m.KeyUsage)
	if len(reportJSON) > 0 {
		var r probe.Report
		if json.Unmarshal(reportJSON, &r) == nil {
			m.Report = &r
		}
	}
	return m, nil
}

func nullStr(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Ensure PgStore implements Store at compile time.
var _ Store = (*PgStore)(nil)

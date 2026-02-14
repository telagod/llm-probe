package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"real-llm/internal/probe"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type API struct {
	auth   *Auth
	store  Store
	runner RunnerService
	obs    *Observability
}

func NewAPI(auth *Auth, store Store, runner RunnerService, obs *Observability) *API {
	return &API{
		auth:   auth,
		store:  store,
		runner: runner,
		obs:    obs,
	}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealthz)

	mux.HandleFunc("POST /api/v1/auth/login", a.auth.HandleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", a.auth.HandleLogout)
	mux.HandleFunc("GET /api/v1/auth/me", a.auth.HandleMe)

	mux.Handle("POST /api/v1/admin/runs", a.auth.RequireAdmin(http.HandlerFunc(a.handleAdminCreateRun)))
	mux.Handle("GET /api/v1/admin/runs/{id}", a.auth.RequireAdmin(http.HandlerFunc(a.handleAdminGetRun)))
	mux.Handle("GET /api/v1/admin/runs/{id}/events", a.auth.RequireAdmin(http.HandlerFunc(a.handleAdminGetRunEventsSSE)))
	mux.Handle("GET /api/v1/admin/metrics/overview", a.auth.RequireAdmin(http.HandlerFunc(a.handleAdminOverview)))
	mux.Handle("GET /api/v1/admin/audit", a.auth.RequireAdmin(http.HandlerFunc(a.handleAdminAudit)))
	mux.Handle("GET /api/v1/admin/runs", a.auth.RequireAdmin(http.HandlerFunc(a.handleAdminListRuns)))

	mux.HandleFunc("POST /api/v1/user/quick-test", a.handleUserQuickTest)
	mux.HandleFunc("GET /api/v1/user/quick-test/{id}", a.handleUserGetQuickTest)
	mux.Handle("GET /api/v1/user/my-runs", a.auth.Require(http.HandlerFunc(a.handleUserMyRuns)))

	wrapped := otelhttp.NewHandler(mux, "probe-api-http")
	return withCORS(wrapped)
}

func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": nowRFC3339(),
	})
}

func (a *API) handleAdminCreateRun(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("probe-api").Start(r.Context(), "admin.create_run")
	defer span.End()
	principal, _ := PrincipalFromContext(ctx)
	var req RunRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	meta, err := a.runner.CreateAdminRun(req, principal, "admin.manual")
	if err != nil {
		span.RecordError(err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id": meta.RunID,
		"status": meta.Status,
	})
}

func (a *API) handleAdminGetRun(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing run id")
		return
	}
	meta, ok := a.store.GetRun(id)
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (a *API) handleAdminListRuns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"runs": a.store.ListRuns(100),
	})
}

func (a *API) handleAdminGetRunEventsSSE(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing run id")
		return
	}
	if _, ok := a.store.GetRun(id); !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	cursor := parseCursor(r)
	send := func(events []RunEvent) {
		for _, event := range events {
			payload, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				continue
			}
			fmt.Fprintf(w, "event: run_event\n")
			fmt.Fprintf(w, "data: %s\n\n", payload)
			cursor = event.Seq
		}
		flusher.Flush()
	}
	send(a.store.ListRunEvents(id, cursor))

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			events := a.store.ListRunEvents(id, cursor)
			if len(events) > 0 {
				send(events)
			} else {
				_, _ = fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	}
}

func (a *API) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.store.GetMetricsOverview())
}

func (a *API) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"audit": a.store.ListAudit(200),
	})
}

func (a *API) handleUserQuickTest(w http.ResponseWriter, r *http.Request) {
	_, span := otel.Tracer("probe-api").Start(r.Context(), "user.quick_test")
	defer span.End()
	var req QuickTestRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ipHash, uaHash := actorHashes(r)
	// optional: attach user identity if logged in
	principal, _ := a.auth.AuthenticateRequest(r)
	span.SetAttributes(
		attribute.String("actor.type", "user"),
		attribute.String("scenario.id", req.ScenarioID),
	)
	meta, err := a.runner.CreateQuickTest(req, ipHash, uaHash)
	if err != nil {
		span.RecordError(err)
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	// link run to logged-in user
	if principal.Subject != "" {
		_, _ = a.store.UpdateRun(meta.RunID, func(m *RunMeta) {
			m.CreatorSub = principal.Subject
		})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id": meta.RunID,
		"status": meta.Status,
	})
}

func (a *API) handleUserMyRuns(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	runs := a.store.ListRunsByCreator(principal.Subject, 50)
	// return deidentified view
	out := make([]map[string]any, 0, len(runs))
	for _, m := range runs {
		entry := map[string]any{
			"run_id":     m.RunID,
			"status":     m.Status,
			"model":      m.Request.Model,
			"created_at": m.CreatedAt,
			"risk": map[string]any{
				"trust_score":    m.Risk.TrustScore,
				"hard_gate_fail": m.Risk.HardGateFail,
			},
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (a *API) handleUserGetQuickTest(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing run id")
		return
	}
	meta, ok := a.store.GetRun(id)
	if !ok {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	view := map[string]any{
		"run_id":      meta.RunID,
		"status":      meta.Status,
		"created_at":  meta.CreatedAt,
		"started_at":  meta.StartedAt,
		"finished_at": meta.FinishedAt,
		"risk": map[string]any{
			"trust_score":      meta.Risk.TrustScore,
			"hard_gate_fail":   meta.Risk.HardGateFail,
			"spoof_risk_score": meta.Risk.SpoofRiskScore,
			"leak_count":       meta.Risk.LeakCount,
		},
	}
	if meta.Report != nil {
		view["summary"] = summarizeReportForUser(*meta.Report)
	}
	writeJSON(w, http.StatusOK, view)
}

func summarizeReportForUser(report probe.Report) map[string]any {
	data := map[string]any{
		"pass": report.Passed,
		"warn": report.Warned,
		"fail": report.Failed,
	}
	highlights := make([]map[string]any, 0, len(report.Results))
	for _, result := range report.Results {
		if result.Suite == "trust_score" || result.Status == probe.StatusFail || result.Status == probe.StatusWarn {
			highlights = append(highlights, map[string]any{
				"suite":   result.Suite,
				"status":  result.Status,
				"summary": result.Summary,
			})
		}
	}
	data["highlights"] = highlights
	return data
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func actorHashes(r *http.Request) (string, string) {
	ip, _, _ := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if ip == "" {
		ip = strings.TrimSpace(r.RemoteAddr)
	}
	return hashString(ip), hashString(r.UserAgent())
}

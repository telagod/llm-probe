# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Claude Endpoint Probe — a Go backend + React frontend framework for testing and validating Anthropic-compatible API endpoints. It verifies protocol compliance, detects spoofing, assesses model authenticity, and produces multi-dimensional trust scores.

## Build & Run Commands

### Backend (Go 1.24.1)

```bash
# Build
go build ./cmd/claude-probe      # CLI probe binary
go build ./cmd/probe-api         # API server binary

# Run CLI probe
go run ./cmd/claude-probe \
  -base-url "$CLAUDE_BASE_URL" \
  -api-key "$CLAUDE_API_KEY" \
  -model "$CLAUDE_MODEL" \
  -suite all

# Run specific suites
go run ./cmd/claude-probe -suite stream,error,authenticity

# Run API server
cp ./configs/server.example.yaml ./configs/server.local.yaml
go run ./cmd/probe-api -config ./configs/server.local.yaml
```

### Frontend (React + Vite)

```bash
cd web
npm install
npm run dev       # Dev server on http://localhost:5173, proxies /api to :8080
npm run build     # Production build (tsc -b && vite build) → web/dist/
```

### Tests

```bash
go test ./internal/probe/...     # Probe suite tests
go test ./internal/server/...    # Server tests
go test ./...                    # All tests
```

### Smoke Test

```bash
./scripts/smoke_api.sh
ADMIN_TOKEN="..." ./scripts/smoke_api.sh  # With admin auth
```

### OpenTelemetry Local Stack

```bash
cd deploy/otel && docker compose up -d   # Jaeger UI: http://localhost:16686
```

## Architecture

```
cmd/claude-probe/main.go   CLI entry → parses flags/env → anthropic.Client → probe.Runner → Report
cmd/probe-api/main.go      HTTP server entry → server.Router → OIDC auth, run manager, budget, store

internal/anthropic/         Anthropic Messages API client wrapper (client.go, types.go)
internal/probe/             Test suite engine:
  runner.go                   Suite orchestration & selection
  types.go                    Result/Report data structures
  suites_*.go                 Individual test suites (params, cache, tools, toolchoice,
                              stream, error, authenticity, reasoning, injection, needle, block)
  scoring.go                  Trust score: weighted dimensions + hard-gate rules
  forensics.go                Forensics level (fast/balanced/forensic)
  reasoning_bank.go           Multi-domain reasoning test cases & semantic scoring
  reasoning_import.go         External benchmark import (GSM8K, BBH, ARC, MMLU, GPQA)
  regression.go               Baseline comparison & drift alerting
  timeline.go                 Historical trend analysis (P95, slope, change-point)

internal/server/             API server:
  router.go                   HTTP routes & middleware
  runner.go                   Run queue, execution, SSE events
  config.go                   YAML config loading
  auth_oidc.go                OIDC authentication flow
  budget.go                   API key pool budget tracking (daily USD, RPM, TPM)
  store.go                    In-memory + file persistence
  observability.go            OpenTelemetry tracing (gRPC OTLP)

web/
  apps/admin/                 Admin dashboard (OIDC-protected): run creation, SSE stream, metrics
  apps/user/                  Public user quick-test UI (rate-limited, no auth)
  shared/api-client/          Shared TypeScript API client & types
```

## Key Design Decisions

- **Frontend never touches API keys** — all model calls happen server-side via the key pool in config.
- **Trust scoring uses hard-gate rules** that override weighted scores: spoof risk >70, injection leaks >0, hidden tool signals, unknown tool calls all trigger immediate fail.
- **Forensics levels** control consistency run count and drift thresholds: `fast` (1 run), `balanced` (2 runs, default), `forensic` (4 runs).
- **Semantic equivalence scoring** for reasoning: numeric, date, yes-no, choice, alternatives (`||` separator), unit-conversion, text-synonym — reduces false negatives from format differences.
- **Vite path aliases**: `@shared` → `web/shared`, `@admin` → `web/apps/admin`, `@user` → `web/apps/user`.
- **Dev proxy**: Vite proxies `/api` requests to `http://localhost:8080`.

## API Routes

Admin routes require OIDC session or admin token. User routes are public with rate limiting.

- `POST /api/v1/admin/runs` — create test run (supports `dry_run=true`)
- `GET /api/v1/admin/runs/{id}` — run details
- `GET /api/v1/admin/runs/{id}/events` — SSE event stream
- `GET /api/v1/admin/metrics/overview` — dashboard metrics
- `POST /api/v1/user/quick-test` — public quick test
- `GET /api/v1/user/quick-test/{id}` — public result (deidentified)

## Environment Variables

- `CLAUDE_BASE_URL` — endpoint (default: `https://api.anthropic.com`)
- `CLAUDE_API_KEY` — API key
- `CLAUDE_MODEL` — model ID to test
- `ANTHROPIC_VERSION` — API version header (default: `2023-06-01`)
- `ANTHROPIC_BETA` — optional beta header

## Test Suites

`params`, `cache`, `tools`, `toolchoice`, `stream`, `error`, `authenticity`, `reasoning`, `injection`, `needle`, `block`, `trust_score`. Use `-suite all` or comma-separated names. `regression` and `timeline` are triggered by `-baseline-in` and `-history-glob` flags respectively.

Caution: `block` and `needle` suites are high-cost and long-running — run them separately with increased `-timeout`.

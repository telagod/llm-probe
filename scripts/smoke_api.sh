#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"

echo "[smoke] healthz"
curl -fsS "${BASE_URL}/healthz" >/dev/null

echo "[smoke] admin unauthorized check"
status_code="$(curl -sS -o /tmp/probe_api_unauth.json -w "%{http_code}" \
  -X POST "${BASE_URL}/api/v1/admin/runs" \
  -H "Content-Type: application/json" \
  -d '{"endpoint":"https://api.anthropic.com","model":"x","suite":["authenticity"]}')"
if [[ "${status_code}" != "401" ]]; then
  echo "expected 401 for unauth admin request, got ${status_code}"
  exit 1
fi

echo "[smoke] user endpoint validation"
status_code="$(curl -sS -o /tmp/probe_api_user_invalid.json -w "%{http_code}" \
  -X POST "${BASE_URL}/api/v1/user/quick-test" \
  -H "Content-Type: application/json" \
  -d '{"scenario_id":"official-model-integrity","target_model":""}')"
if [[ "${status_code}" != "429" && "${status_code}" != "400" ]]; then
  echo "expected 400/429 for invalid user request, got ${status_code}"
  exit 1
fi

if [[ -n "${ADMIN_TOKEN}" ]]; then
  echo "[smoke] admin dry-run"
  response="$(curl -fsS \
    -X POST "${BASE_URL}/api/v1/admin/runs" \
    -H "Content-Type: application/json" \
    -H "X-Admin-Token: ${ADMIN_TOKEN}" \
    -d '{"endpoint":"https://api.anthropic.com","model":"claude-sonnet-4-5-20250929","suite":["authenticity","injection","tools"],"forensics_level":"balanced","dry_run":true,"hard_gate":true}')"
  run_id="$(printf "%s" "${response}" | sed -n 's/.*"run_id":"\([^"]*\)".*/\1/p')"
  if [[ -z "${run_id}" ]]; then
    echo "failed to parse run_id from response: ${response}"
    exit 1
  fi
  echo "run_id=${run_id}"
  for _ in {1..10}; do
    run_detail="$(curl -fsS -H "X-Admin-Token: ${ADMIN_TOKEN}" "${BASE_URL}/api/v1/admin/runs/${run_id}")"
    status="$(printf "%s" "${run_detail}" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')"
    echo "status=${status}"
    if [[ "${status}" != "queued" && "${status}" != "running" ]]; then
      break
    fi
    sleep 1
  done
fi

echo "[smoke] ok"

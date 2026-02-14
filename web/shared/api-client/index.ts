export type RunStatus = "queued" | "running" | "pass" | "warn" | "fail";

export type RunMeta = {
  run_id: string;
  status: RunStatus;
  source: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  error?: string;
  request: {
    endpoint: string;
    model: string;
    suite: string[];
    forensics_level?: string;
    budget_cap?: number;
  };
  report?: {
    passed: number;
    warned: number;
    failed: number;
    results: Array<{
      suite: string;
      status: string;
      summary: string;
      duration_ms: number;
      findings?: string[];
      metrics?: Record<string, unknown>;
    }>;
  };
  risk: {
    trust_score_final: number;
    trust_score_raw: number;
    hard_gate_fail: boolean;
    hard_gate_hits?: Array<Record<string, unknown>>;
    spoof_risk_score: number;
    leak_count: number;
    hidden_tool_signal_count: number;
  };
  key_usage: {
    key_label: string;
    estimated_cost_usd: number;
  };
  estimated_cost_usd: number;
};

export type RunEvent = {
  seq: number;
  timestamp: string;
  stage: string;
  message: string;
  data?: Record<string, unknown>;
};

export type Overview = {
  generated_at: string;
  total_runs: number;
  running_runs: number;
  pass_runs: number;
  warn_runs: number;
  fail_runs: number;
  hard_gate_hits: number;
  average_duration_ms: number;
  average_trust_score: number;
  estimated_cost_usd: number;
};

async function json<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const response = await fetch(input, {
    credentials: "include",
    ...init
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP ${response.status}`);
  }
  return response.json() as Promise<T>;
}

export const api = {
  me: () => json<{
    authenticated: boolean;
    principal?: { subject: string; username: string; role: string };
  }>("/api/v1/auth/me"),

  login: (username: string, password: string) =>
    json<{ ok: boolean; role: string }>("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    }),

  logout: () => json<{ ok: boolean }>("/api/v1/auth/logout", { method: "POST" }),

  myRuns: () => json<{ runs: Array<{
    run_id: string; status: RunStatus; model: string; created_at: string;
    risk: { trust_score: number; hard_gate_fail: boolean };
  }> }>("/api/v1/user/my-runs"),

  createAdminRun: (payload: Record<string, unknown>) =>
    json<{ run_id: string; status: string }>("/api/v1/admin/runs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    }),
  getRun: (runID: string) => json<RunMeta>(`/api/v1/admin/runs/${runID}`),
  listRuns: () => json<{ runs: RunMeta[] }>("/api/v1/admin/runs"),
  overview: () => json<Overview>("/api/v1/admin/metrics/overview"),
  audit: () => json<{ audit: Array<Record<string, unknown>> }>("/api/v1/admin/audit"),
  createQuickTest: (payload: { scenario_id: string; target_model: string; strict_level?: string; endpoint?: string }) =>
    json<{ run_id: string; status: string }>("/api/v1/user/quick-test", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    }),
  getQuickTest: (runID: string) =>
    json<{
      run_id: string;
      status: RunStatus;
      risk: { trust_score: number; hard_gate_fail: boolean; spoof_risk_score: number; leak_count: number };
      summary?: { pass: number; warn: number; fail: number; highlights: Array<{ suite: string; status: string; summary: string }> };
    }>(`/api/v1/user/quick-test/${runID}`)
};

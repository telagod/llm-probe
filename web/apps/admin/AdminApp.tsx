import { FormEvent, useEffect, useMemo, useState } from "react";
import { api, Overview, RunEvent, RunMeta } from "@shared/api-client";
import { useAuth } from "@shared/auth-context";
import { useDocumentHead } from "@shared/use-document-head";

const DEFAULT_SUITES = ["authenticity", "injection", "tools", "toolchoice", "stream", "error"];

export function AdminApp() {
  useDocumentHead({ title: "管理后台 — Claude Endpoint Probe", description: "Claude Endpoint Probe 管理控制台" });
  const { username, logout } = useAuth();

  const [overview, setOverview] = useState<Overview | null>(null);
  const [runs, setRuns] = useState<RunMeta[]>([]);
  const [audit, setAudit] = useState<Array<Record<string, unknown>>>([]);
  const [selectedRun, setSelectedRun] = useState<string>("");
  const [runDetail, setRunDetail] = useState<RunMeta | null>(null);
  const [events, setEvents] = useState<RunEvent[]>([]);

  const [form, setForm] = useState({
    endpoint: "https://api.anthropic.com",
    model: "",
    suite: DEFAULT_SUITES.join(","),
    forensics_level: "balanced",
    dry_run: false,
    budget_cap: 5,
    timeout_sec: 540,
    hard_gate: true
  });

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    void refresh();
    const ticker = window.setInterval(() => void refresh(), 6000);
    return () => window.clearInterval(ticker);
  }, []);

  useEffect(() => {
    if (!selectedRun) return;
    const ticker = window.setInterval(() => {
      void api.getRun(selectedRun).then(setRunDetail).catch(() => undefined);
    }, 2500);
    return () => window.clearInterval(ticker);
  }, [selectedRun]);

  useEffect(() => {
    if (!selectedRun) {
      setEvents([]);
      return;
    }
    const source = new EventSource(`/api/v1/admin/runs/${selectedRun}/events`);
    const onEvent = (event: Event) => {
      const message = event as MessageEvent<string>;
      try {
        const parsed = JSON.parse(message.data) as RunEvent;
        setEvents((prev) => {
          const next = [...prev, parsed];
          if (next.length > 120) return next.slice(next.length - 120);
          return next;
        });
      } catch { return; }
    };
    source.addEventListener("run_event", onEvent);
    source.onerror = () => { source.close(); };
    return () => { source.removeEventListener("run_event", onEvent); source.close(); };
  }, [selectedRun]);

  const healthStack = useMemo(() => {
    if (!overview || overview.total_runs === 0) return { pass: 0, warn: 0, fail: 0 };
    return {
      pass: (overview.pass_runs / overview.total_runs) * 100,
      warn: (overview.warn_runs / overview.total_runs) * 100,
      fail: (overview.fail_runs / overview.total_runs) * 100
    };
  }, [overview]);

  async function refresh() {
    const [newOverview, newRuns, newAudit] = await Promise.all([api.overview(), api.listRuns(), api.audit()]);
    setOverview(newOverview);
    setRuns(newRuns.runs);
    setAudit(newAudit.audit);
    if (!selectedRun && newRuns.runs.length > 0) {
      setSelectedRun(newRuns.runs[0].run_id);
      setRunDetail(newRuns.runs[0]);
    }
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const suite = form.suite.split(",").map((item) => item.trim()).filter(Boolean);
      const created = await api.createAdminRun({
        endpoint: form.endpoint, model: form.model, suite,
        forensics_level: form.forensics_level, dry_run: form.dry_run,
        budget_cap: Number(form.budget_cap), timeout_sec: Number(form.timeout_sec),
        hard_gate: form.hard_gate
      });
      setSelectedRun(created.run_id);
      await refresh();
    } catch (submitError) {
      setError((submitError as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="grid" style={{ gap: 16 }}>
      <section className="panel" style={{ padding: 14 }}>
        <h2>管理控制台</h2>
        <p className="muted">
          当前管理员：{username || "未知"} · 高风险信号默认 hard-gate
        </p>
        <button className="secondary" onClick={() => void logout()}>
          退出登录
        </button>
      </section>

      <section className="grid two">
        <div className="panel" style={{ padding: 14 }}>
          <h3>运行概览</h3>
          <div className="grid two">
            <div className="kpi">
              <div className="label">总任务</div>
              <div className="value">{overview?.total_runs ?? 0}</div>
            </div>
            <div className="kpi">
              <div className="label">Hard Gate 命中</div>
              <div className="value">{overview?.hard_gate_hits ?? 0}</div>
            </div>
            <div className="kpi">
              <div className="label">平均信任分</div>
              <div className="value">{(overview?.average_trust_score ?? 0).toFixed(1)}</div>
            </div>
            <div className="kpi">
              <div className="label">估算成本 USD</div>
              <div className="value">{(overview?.estimated_cost_usd ?? 0).toFixed(2)}</div>
            </div>
          </div>
          <p className="muted" style={{ marginTop: 10 }}>运行状态分布</p>
          <div className="stack-row">
            <span className="pass" style={{ width: `${healthStack.pass}%` }} />
            <span className="warn" style={{ width: `${healthStack.warn}%` }} />
            <span className="fail" style={{ width: `${healthStack.fail}%` }} />
          </div>
        </div>

        <div className="panel" style={{ padding: 14 }}>
          <h3>创建测试任务</h3>
          <form onSubmit={onSubmit} className="grid" style={{ gap: 10 }}>
            <label>
              Endpoint
              <input value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} />
            </label>
            <label>
              Model
              <input required value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} />
            </label>
            <label>
              Suites (comma-separated)
              <input value={form.suite} onChange={(e) => setForm({ ...form, suite: e.target.value })} />
            </label>
            <div className="grid two">
              <label>
                Forensics
                <select value={form.forensics_level} onChange={(e) => setForm({ ...form, forensics_level: e.target.value })}>
                  <option value="fast">fast</option>
                  <option value="balanced">balanced</option>
                  <option value="forensic">forensic</option>
                </select>
              </label>
              <label>
                Dry Run
                <select value={String(form.dry_run)} onChange={(e) => setForm({ ...form, dry_run: e.target.value === "true" })}>
                  <option value="false">false</option>
                  <option value="true">true</option>
                </select>
              </label>
            </div>
            <div className="grid two">
              <label>
                Budget Cap (USD)
                <input type="number" value={form.budget_cap} onChange={(e) => setForm({ ...form, budget_cap: Number(e.target.value) })} />
              </label>
              <label>
                Timeout (sec)
                <input type="number" value={form.timeout_sec} onChange={(e) => setForm({ ...form, timeout_sec: Number(e.target.value) })} />
              </label>
              <label>
                Hard Gate
                <select value={String(form.hard_gate)} onChange={(e) => setForm({ ...form, hard_gate: e.target.value === "true" })}>
                  <option value="true">true</option>
                  <option value="false">false</option>
                </select>
              </label>
            </div>
            {error ? <div className="status-fail">{error}</div> : null}
            <button disabled={busy}>{busy ? "提交中..." : "启动测试"}</button>
          </form>
        </div>
      </section>

      <section className="grid two">
        <div className="panel" style={{ padding: 14 }}>
          <h3>任务列表</h3>
          <table>
            <thead>
              <tr><th>Run ID</th><th>Status</th><th>Model</th><th>Cost</th></tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.run_id} onClick={() => setSelectedRun(run.run_id)} style={{ cursor: "pointer" }}>
                  <td><code>{run.run_id}</code></td>
                  <td><span className={`status-indicator ${run.status}`}>{run.status}</span></td>
                  <td>{run.request.model}</td>
                  <td>{(run.estimated_cost_usd ?? 0).toFixed(3)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div className="panel" style={{ padding: 14 }}>
          <h3>审计摘要</h3>
          <table>
            <thead>
              <tr><th>时间</th><th>动作</th><th>结果</th></tr>
            </thead>
            <tbody>
              {audit.slice(0, 10).map((item, idx) => (
                <tr key={idx}>
                  <td>{String(item.timestamp ?? "")}</td>
                  <td>{String(item.action ?? "")}</td>
                  <td>{String(item.result ?? "")}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="panel" style={{ padding: 14 }}>
        <h3>任务详情</h3>
        {!runDetail ? (
          <p className="muted">选择一条任务查看详情</p>
        ) : (
          <>
            <p className="muted">
              <code>{runDetail.run_id}</code> · <span className={`status-indicator ${runDetail.status}`}>{runDetail.status}</span> ·
              key={runDetail.key_usage?.key_label || "-"}
            </p>
            {(() => {
              const score = Math.max(0, Math.min(100, runDetail.risk?.trust_score_final ?? 0));
              const r = 40, c = 2 * Math.PI * r, offset = c - (score / 100) * c;
              const color = score >= 70 ? "var(--ok)" : score >= 40 ? "var(--warn)" : "var(--danger)";
              return (
                <div className="trust-gauge" style={{ margin: "10px 0" }}>
                  <svg width="100" height="100" viewBox="0 0 100 100">
                    <circle className="gauge-bg" cx="50" cy="50" r={r} />
                    <circle className="gauge-fill" cx="50" cy="50" r={r} stroke={color} strokeDasharray={c} strokeDashoffset={offset} />
                  </svg>
                  <span className="gauge-label">{score.toFixed(1)}</span>
                </div>
              );
            })()}
            {runDetail.report?.results ? (
              <table>
                <thead>
                  <tr><th>Suite</th><th>Status</th><th>Summary</th><th>Duration</th></tr>
                </thead>
                <tbody>
                  {runDetail.report.results.map((result) => (
                    <tr key={result.suite}>
                      <td>{result.suite}</td>
                      <td><span className={`status-indicator ${result.status}`}>{result.status}</span></td>
                      <td>{result.summary}</td>
                      <td>{result.duration_ms}ms</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="muted">任务尚未完成。</p>
            )}
          </>
        )}
      </section>

      <section className="panel" style={{ padding: 14 }}>
        <h3>实时事件流（SSE）</h3>
        {events.length === 0 ? (
          <p className="muted">等待事件...</p>
        ) : (
          <table>
            <thead>
              <tr><th>Seq</th><th>时间</th><th>阶段</th><th>消息</th></tr>
            </thead>
            <tbody>
              {events.slice().reverse().map((event) => (
                <tr key={event.seq}>
                  <td>{event.seq}</td>
                  <td>{event.timestamp}</td>
                  <td>{event.stage}</td>
                  <td>{event.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

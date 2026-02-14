import { useEffect, useMemo, useState } from "react";
import { api } from "@shared/api-client";
import { useAuth } from "@shared/auth-context";
import { useDocumentHead } from "@shared/use-document-head";

const scenarios = [
  {
    id: "official-model-integrity",
    title: "官方模型真伪快检",
    desc: "聚焦协议一致性、注入泄漏、隐藏工具信号。"
  },
  {
    id: "injection-resilience",
    title: "注入抗性快检",
    desc: "验证 direct/indirect prompt injection 与 allowlist 逃逸。"
  },
  {
    id: "cache-tooling-smoke",
    title: "缓存+工具链烟雾测试",
    desc: "覆盖 cache_control / tool_use / tool_choice 基础链路。"
  }
];

export function UserApp() {
  useDocumentHead({ title: "Claude Endpoint Probe — 免费 API 快检", description: "免登录在线检测 Claude API 端点真伪，验证协议合规性，评估注入抗性，生成多维信任评分。" });
  const { authenticated } = useAuth();
  const [model, setModel] = useState("");
  const [endpoint, setEndpoint] = useState("https://api.anthropic.com");
  const [strictLevel, setStrictLevel] = useState("balanced");
  const [runningScenario, setRunningScenario] = useState<string>("");
  const [runID, setRunID] = useState("");
  const [status, setStatus] = useState("idle");
  const [result, setResult] = useState<{
    status: string;
    risk?: { trust_score: number; hard_gate_fail: boolean; spoof_risk_score: number; leak_count: number };
    summary?: { pass: number; warn: number; fail: number; highlights: Array<{ suite: string; status: string; summary: string }> };
  } | null>(null);
  const [error, setError] = useState("");
  const [history, setHistory] = useState<Array<{
    run_id: string; status: string; model: string; created_at: string;
    risk: { trust_score: number; hard_gate_fail: boolean };
  }>>([]);

  useEffect(() => {
    if (authenticated) {
      void api.myRuns().then((r) => setHistory(r.runs)).catch(() => undefined);
    }
  }, [authenticated, status]);

  useEffect(() => {
    if (!runID || (status !== "queued" && status !== "running")) {
      return;
    }
    const timer = window.setInterval(() => {
      void api
        .getQuickTest(runID)
        .then((next) => {
          setStatus(next.status);
          setResult(next);
        })
        .catch(() => undefined);
    }, 2200);
    return () => window.clearInterval(timer);
  }, [runID, status]);

  const stack = useMemo(() => {
    if (!result?.summary) {
      return { pass: 0, warn: 0, fail: 0 };
    }
    const total = result.summary.pass + result.summary.warn + result.summary.fail;
    if (total === 0) {
      return { pass: 0, warn: 0, fail: 0 };
    }
    return {
      pass: (result.summary.pass / total) * 100,
      warn: (result.summary.warn / total) * 100,
      fail: (result.summary.fail / total) * 100
    };
  }, [result]);

  const progress = useMemo(() => {
    switch (status) {
      case "queued":
        return 28;
      case "running":
        return 66;
      case "pass":
      case "warn":
      case "fail":
        return 100;
      default:
        return 0;
    }
  }, [status]);

  async function launchScenario(scenarioID: string) {
    if (!model.trim()) {
      setError("请先填写模型 ID");
      return;
    }
    setError("");
    setRunningScenario(scenarioID);
    setStatus("queued");
    setResult(null);
    try {
      const created = await api.createQuickTest({
        scenario_id: scenarioID,
        target_model: model.trim(),
        strict_level: strictLevel,
        endpoint: endpoint.trim()
      });
      setRunID(created.run_id);
    } catch (launchError) {
      setStatus("idle");
      setError((launchError as Error).message);
    } finally {
      setRunningScenario("");
    }
  }

  return (
    <div className="grid" style={{ gap: 16 }}>
      <section className="panel" style={{ padding: 14 }}>
        <h1 style={{ fontSize: 20, margin: "0 0 4px 0", fontFamily: "Space Grotesk, sans-serif" }}>Claude Endpoint Probe — 免费 API 快检</h1>
        <p className="muted">仅开放预定义场景，后端统一使用限额测试 key 与 hard-gate。</p>
        <div className="grid two">
          <label>
            模型 ID
            <input value={model} onChange={(e) => setModel(e.target.value)} placeholder="claude-sonnet-4-5-20250929" />
          </label>
          <label>
            Endpoint
            <input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} />
          </label>
        </div>
        <div className="grid two" style={{ marginTop: 10 }}>
          <label>
            严格度
            <select value={strictLevel} onChange={(e) => setStrictLevel(e.target.value)}>
              <option value="fast">fast</option>
              <option value="balanced">balanced</option>
              <option value="forensic">forensic</option>
            </select>
          </label>
          <div className="scenario-card">
            <div className="pill">安全策略</div>
            <p className="muted">无登录端只支持模板场景，不接受自定义 prompt/tools。</p>
          </div>
        </div>
      </section>

      <section className="panel" style={{ padding: 14 }}>
        <h3>测试场景</h3>
        <div className="card-list">
          {scenarios.map((scenario) => (
            <article key={scenario.id} className="scenario-card">
              <h2 style={{ fontSize: 16, margin: "0 0 4px 0", fontFamily: "Space Grotesk, sans-serif" }}>{scenario.title}</h2>
              <p className="muted">{scenario.desc}</p>
              <button
                disabled={runningScenario !== "" || status === "running"}
                onClick={() => void launchScenario(scenario.id)}
              >
                {runningScenario === scenario.id ? "提交中..." : "开始测试"}
              </button>
            </article>
          ))}
        </div>
        {error ? <p className="status-fail">{error}</p> : null}
      </section>

      <section className="panel" style={{ padding: 14 }}>
        <h3>结果可视化</h3>
        <p className="muted">
          Run ID: {runID ? <code>{runID}</code> : "—"} · 当前状态：<span className={`status-indicator ${status}`}>{status}</span>
        </p>
        <div className="bar" style={{ marginBottom: 10 }}>
          <span style={{ width: `${progress}%` }} />
        </div>
        <div className="stack-row" style={{ marginBottom: 10 }}>
          <span className="pass" style={{ width: `${stack.pass}%` }} />
          <span className="warn" style={{ width: `${stack.warn}%` }} />
          <span className="fail" style={{ width: `${stack.fail}%` }} />
        </div>
        <div className="grid two">
          <div className="kpi" style={{ display: "flex", alignItems: "center", gap: 14 }}>
            {(() => {
              const score = Math.max(0, Math.min(100, result?.risk?.trust_score ?? 0));
              const r = 34, c = 2 * Math.PI * r, offset = c - (score / 100) * c;
              const color = score >= 70 ? "var(--ok)" : score >= 40 ? "var(--warn)" : "var(--danger)";
              return (
                <div className="trust-gauge">
                  <svg width="84" height="84" viewBox="0 0 84 84">
                    <circle className="gauge-bg" cx="42" cy="42" r={r} />
                    <circle className="gauge-fill" cx="42" cy="42" r={r} stroke={color} strokeDasharray={c} strokeDashoffset={offset} />
                  </svg>
                  <span className="gauge-label" style={{ fontSize: 15 }}>{score.toFixed(1)}</span>
                </div>
              );
            })()}
            <div>
              <div className="label">Trust Score</div>
              <div className="value">{(result?.risk?.trust_score ?? 0).toFixed(2)}</div>
            </div>
          </div>
          <div className="kpi">
            <div className="label">Spoof Risk</div>
            <div className="value">{(result?.risk?.spoof_risk_score ?? 0).toFixed(1)}</div>
          </div>
        </div>
        {result?.summary?.highlights?.length ? (
          <table style={{ marginTop: 10 }}>
            <thead>
              <tr>
                <th>Suite</th>
                <th>Status</th>
                <th>Summary</th>
              </tr>
            </thead>
            <tbody>
              {result.summary.highlights.map((item) => (
                <tr key={`${item.suite}-${item.status}`}>
                  <td>{item.suite}</td>
                  <td><span className={`status-indicator ${item.status}`}>{item.status}</span></td>
                  <td>{item.summary}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="muted">暂无高风险结果。</p>
        )}
      </section>

      {authenticated && history.length > 0 && (
        <section className="panel" style={{ padding: 14 }}>
          <h3>我的测试历史</h3>
          <table>
            <thead>
              <tr><th>Run ID</th><th>Status</th><th>Model</th><th>Trust</th><th>时间</th></tr>
            </thead>
            <tbody>
              {history.map((h) => (
                <tr key={h.run_id}>
                  <td><code>{h.run_id}</code></td>
                  <td><span className={`status-indicator ${h.status}`}>{h.status}</span></td>
                  <td>{h.model}</td>
                  <td>{(h.risk?.trust_score ?? 0).toFixed(1)}</td>
                  <td>{h.created_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  );
}

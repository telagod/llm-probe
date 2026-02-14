import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@shared/auth-context";
import { useDocumentHead } from "@shared/use-document-head";

export function LoginPage() {
  useDocumentHead({ title: "登录 — Claude Endpoint Probe", description: "登录后查看测试历史和详细报告" });
  const { login } = useAuth();
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const role = await login(username, password);
      navigate(role === "admin" ? "/admin" : "/user", { replace: true });
    } catch {
      setError("用户名或密码错误");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel login-panel" style={{ padding: 24 }}>
      <h2>登录</h2>
      <form onSubmit={onSubmit} className="grid" style={{ gap: 12 }}>
        <label>
          用户名
          <input required value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label>
          密码
          <input required type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        {error && <div className="status-fail">{error}</div>}
        <button disabled={busy}>{busy ? "登录中..." : "登录"}</button>
      </form>
    </section>
  );
}

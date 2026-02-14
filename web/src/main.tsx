import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter, NavLink, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "@shared/auth-context";
import { AdminApp } from "@admin/AdminApp";
import { UserApp } from "@user/UserApp";
import { LoginPage } from "@login/LoginPage";
import "./styles.css";

function Header() {
  const { authenticated, role, logout } = useAuth();
  return (
    <header className="top-nav">
      <div className="brand">
        <span className="brand-dot" />
        <span>Probe Control Center</span>
      </div>
      <nav className="tabs">
        {authenticated && role === "admin" && (
          <NavLink to="/admin" className={({ isActive }) => isActive ? "active" : ""}>管理后台</NavLink>
        )}
        <NavLink to="/user" className={({ isActive }) => isActive ? "active" : ""}>用户测试</NavLink>
        {authenticated
          ? <button className="secondary" onClick={() => void logout()}>退出</button>
          : <NavLink to="/login" className={({ isActive }) => isActive ? "active" : ""}>登录</NavLink>
        }
      </nav>
    </header>
  );
}

function AdminGuard({ children }: { children: React.ReactNode }) {
  const { ready, authenticated, role } = useAuth();
  if (!ready) return <div className="panel" style={{ padding: 16 }}>加载中...</div>;
  if (!authenticated) return <Navigate to="/login" replace />;
  if (role !== "admin") return <Navigate to="/user" replace />;
  return <>{children}</>;
}

function Shell() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <div className="shell-bg" />
        <Header />
        <main className="content-root">
          <Routes>
            <Route path="/" element={<Navigate to="/user" replace />} />
            <Route path="/login" element={<LoginPage />} />
            <Route path="/admin" element={<AdminGuard><AdminApp /></AdminGuard>} />
            <Route path="/user" element={<UserApp />} />
          </Routes>
        </main>
        <footer className="site-footer">
          <p>Claude Endpoint Probe — 开源 API 端点真伪检测工具</p>
          <p>验证协议合规性 · 检测 Prompt Injection · 多维信任评分 · Hard-Gate 安全门控</p>
        </footer>
      </AuthProvider>
    </BrowserRouter>
  );
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <Shell />
  </React.StrictMode>
);

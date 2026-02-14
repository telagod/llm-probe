import { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { api } from "@shared/api-client";

type AuthState = {
  ready: boolean;
  authenticated: boolean;
  username: string;
  role: string;
  login: (u: string, p: string) => Promise<string>;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthState>({
  ready: false, authenticated: false, username: "", role: "",
  login: async () => "", logout: async () => {},
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [username, setUsername] = useState("");
  const [role, setRole] = useState("");

  useEffect(() => {
    api.me()
      .then((r) => {
        setAuthenticated(r.authenticated);
        setUsername(r.principal?.username ?? "");
        setRole(r.principal?.role ?? "");
      })
      .catch(() => setAuthenticated(false))
      .finally(() => setReady(true));
  }, []);

  const login = async (u: string, p: string) => {
    const res = await api.login(u, p);
    setAuthenticated(true);
    setUsername(u);
    setRole(res.role);
    return res.role;
  };

  const logout = async () => {
    await api.logout();
    setAuthenticated(false);
    setUsername("");
    setRole("");
  };

  return (
    <AuthContext.Provider value={{ ready, authenticated, username, role, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}

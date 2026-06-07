import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ApiError } from "@authman/shared";
import { adminMe, adminLogin as apiLogin, adminLogout as apiLogout, type AdminLoginResult, type AdminMe } from "../api/admin";

interface SessionState {
  loading: boolean;
  user: AdminMe | null;
  /** True once the initial /admin/me probe has resolved (success or failure). */
  resolved: boolean;
}

interface SessionContextValue extends SessionState {
  login: (identifier: string, password: string) => Promise<AdminLoginResult>;
  logout: () => Promise<void>;
  hasPermission: (perm: string) => boolean;
  refresh: () => Promise<void>;
}

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<SessionState>({ loading: true, user: null, resolved: false });

  const refresh = useCallback(async () => {
    try {
      const me = await adminMe();
      setState({ loading: false, user: me.user, resolved: true });
    } catch (err) {
      if (err instanceof ApiError && (err.status === 401 || err.code === "auth.unauthenticated")) {
        setState({ loading: false, user: null, resolved: true });
        return;
      }
      // Backend down — mark resolved with no user so /login is reachable.
      setState({ loading: false, user: null, resolved: true });
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const login = useCallback(async (identifier: string, password: string) => {
    const result = await apiLogin(identifier, password);
    if (result.kind === "ok") {
      setState({ loading: false, user: result.user, resolved: true });
    }
    return result;
  }, []);

  const logout = useCallback(async () => {
    await apiLogout();
    setState({ loading: false, user: null, resolved: true });
  }, []);

  const hasPermission = useCallback(
    (perm: string) => {
      if (!state.user) return false;
      const permission = perm.toLowerCase();
      return state.user.permissions.some((grant) => {
        const normalized = grant.toLowerCase();
        return normalized === "*" || normalized === permission || (normalized.endsWith(".*") && permission.startsWith(`${normalized.slice(0, -2)}.`));
      });
    },
    [state.user],
  );

  const value = useMemo<SessionContextValue>(
    () => ({ ...state, login, logout, hasPermission, refresh }),
    [state, login, logout, hasPermission, refresh],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession must be used inside SessionProvider");
  return ctx;
}

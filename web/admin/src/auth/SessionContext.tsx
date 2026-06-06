import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { ApiError } from "@authman/shared";
import { adminMe, adminLogin as apiLogin, adminLogout as apiLogout, type AdminMe } from "../api/admin";

interface SessionState {
  loading: boolean;
  user: AdminMe | null;
  /** True once the initial /admin/me probe has resolved (success or failure). */
  resolved: boolean;
}

interface SessionContextValue extends SessionState {
  login: (username: string, password: string) => Promise<AdminMe>;
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

  const login = useCallback(async (username: string, password: string) => {
    const user = await apiLogin(username, password);
    setState({ loading: false, user, resolved: true });
    return user;
  }, []);

  const logout = useCallback(async () => {
    await apiLogout();
    setState({ loading: false, user: null, resolved: true });
  }, []);

  const hasPermission = useCallback(
    (perm: string) => {
      if (!state.user) return false;
      return state.user.permissions.includes(perm) || state.user.role === "owner";
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

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  portalLoginOffline,
  portalLoginWithLink,
  portalLogout,
  portalMe,
  portalRegister,
  type PortalMe,
} from "../api/portal";

interface State {
  loading: boolean;
  resolved: boolean;
  me: PortalMe | null;
}

interface ContextValue extends State {
  loginOffline: (input: { username: string; password: string; server_slug?: string }) => Promise<PortalMe>;
  loginWithLink: (token: string) => Promise<PortalMe & { server_slug?: string }>;
  register: (input: { raw_username: string; password: string; server_slug?: string }) => Promise<PortalMe>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

const Ctx = createContext<ContextValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<State>({ loading: true, resolved: false, me: null });

  const refresh = useCallback(async () => {
    try {
      const me = await portalMe();
      setState({ loading: false, resolved: true, me });
    } catch {
      setState({ loading: false, resolved: true, me: null });
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const loginOffline = useCallback(async (input: { username: string; password: string; server_slug?: string }) => {
    const me = await portalLoginOffline(input);
    setState({ loading: false, resolved: true, me });
    return me;
  }, []);

  const loginWithLink = useCallback(async (token: string) => {
    const me = await portalLoginWithLink(token);
    setState({ loading: false, resolved: true, me });
    return me;
  }, []);

  const register = useCallback(async (input: { raw_username: string; password: string; server_slug?: string }) => {
    const me = await portalRegister(input);
    setState({ loading: false, resolved: true, me });
    return me;
  }, []);

  const logout = useCallback(async () => {
    await portalLogout();
    setState({ loading: false, resolved: true, me: null });
  }, []);

  const value = useMemo<ContextValue>(
    () => ({ ...state, loginOffline, loginWithLink, register, logout, refresh }),
    [state, loginOffline, loginWithLink, register, logout, refresh],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSession(): ContextValue {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useSession must be used inside SessionProvider");
  return ctx;
}

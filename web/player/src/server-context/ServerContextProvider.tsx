import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { downstreamThemeVars } from "@authman/shared";
import { portalServer, type PortalServer } from "../api/portal";

interface Value {
  slug: string | null;
  loading: boolean;
  server: PortalServer | null;
  themeVars: Record<string, string>;
  error: unknown;
}

const Ctx = createContext<Value>({ slug: null, loading: false, server: null, themeVars: {}, error: null });

/**
 * Reads :slug from the current route and fetches that downstream server's
 * portal config. Children wrap inside this context so links, page chrome,
 * and forms see the active server.
 */
export function ServerContextProvider({ children }: { children: ReactNode }) {
  const params = useParams<{ slug?: string }>();
  const slug = params.slug ?? null;
  const q = useQuery({
    queryKey: ["portal.server", slug],
    queryFn: () => (slug ? portalServer(slug) : Promise.resolve(null)),
    enabled: !!slug,
  });

  const themeVars = useMemo(() => downstreamThemeVars(q.data ?? null), [q.data]);
  const value = useMemo<Value>(
    () => ({
      slug,
      loading: !!slug && q.isLoading,
      server: q.data ?? null,
      themeVars,
      error: q.error,
    }),
    [slug, q.isLoading, q.data, q.error, themeVars],
  );

  return (
    <Ctx.Provider value={value}>
      <div data-testid={slug ? "server-context-wrapper" : undefined} style={{ ...themeVars, minHeight: "100%" }}>
        {children}
      </div>
    </Ctx.Provider>
  );
}

export function useServerContext(): Value {
  return useContext(Ctx);
}

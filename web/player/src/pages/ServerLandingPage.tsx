import { Link, Navigate } from "react-router-dom";
import { Badge, Button, Card, useI18n } from "@authman/shared";
import { useServerContext } from "../server-context/ServerContextProvider";
import { useSession } from "../auth/SessionContext";
import { ErrorBlock } from "../components/ErrorBlock";
import { AuthHeader } from "../components/AuthHeader";

export function ServerLandingPage() {
  const { t } = useI18n();
  const { slug, server, loading, error } = useServerContext();
  const { me } = useSession();

  if (me) return <Navigate to={`/server/${slug}/account`} replace />;
  if (loading) {
    return <div className="pcontent"><Card>Loading…</Card></div>;
  }
  if (error || !server) {
    return <div className="pcontent">{error ? <ErrorBlock error={error} /> : null}</div>;
  }

  const primary = server.primary_color ?? "var(--color-primary)";

  return (
    <div className="pauth">
      <AuthHeader sub={server.display_name} />
      <div className="pauth-body">
        <div className="pauth-card" data-testid="server-landing">
          <div className="server-ctx-chip" style={{ ["--ctx-color" as string]: primary }}>
            <span className="ctx-badge" style={{ background: primary }}>{server.display_name[0]}</span>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="ctx-name">{server.display_name}</div>
              {server.description ? <div className="ctx-desc">{server.description}</div> : null}
            </div>
            <Badge tone={server.registration_open ? "success" : "neutral"} dot>
              {server.registration_open ? "Open" : "Closed"}
            </Badge>
          </div>
          <div className="pauth-head">
            <h1>{server.display_name}</h1>
            {server.portal_message ? <p data-testid="server-portal-message">{server.portal_message}</p> : null}
          </div>
          <div className="pauth-form">
            <Link to={`/server/${slug}/login`} style={{ textDecoration: "none" }}>
              <Button variant="primary" size="lg" block data-testid="server-landing-login">
                {t("common.signIn")}
              </Button>
            </Link>
            {server.registration_open ? (
              <Link to={`/server/${slug}/register`} style={{ textDecoration: "none" }}>
                <Button variant="secondary" size="lg" block data-testid="server-landing-register">
                  {t("common.register")}
                </Button>
              </Link>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}

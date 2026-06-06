import { Link, Navigate } from "react-router-dom";
import { Button, useI18n } from "@authman/shared";
import { useSession } from "../auth/SessionContext";
import { AuthHeader } from "../components/AuthHeader";

export function LandingPage() {
  const { t } = useI18n();
  const { me, resolved } = useSession();
  if (resolved && me) return <Navigate to="/account" replace />;

  return (
    <div className="pauth">
      <AuthHeader />
      <div className="pauth-body">
        <div className="pauth-card" data-testid="landing-card">
          <div className="pauth-head">
            <h1>{t("app.player.title")}</h1>
            <p>{t("login.player.subheading")}</p>
          </div>
          <div className="pauth-form">
            <Link to="/login" style={{ textDecoration: "none" }}>
              <Button variant="primary" size="lg" block iconRight="arrowRight" data-testid="landing-login">
                {t("common.signIn")}
              </Button>
            </Link>
            <Link to="/register" style={{ textDecoration: "none" }}>
              <Button variant="secondary" size="lg" block icon="plus" data-testid="landing-register">
                {t("common.register")}
              </Button>
            </Link>
            <Link to="/servers" style={{ textDecoration: "none" }}>
              <Button variant="ghost" size="lg" block icon="layers" data-testid="landing-servers">
                {t("servers.heading")}
              </Button>
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}

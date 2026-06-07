import { useEffect, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import {
  Alert,
  ApiError,
  BrandMark,
  Button,
  Field,
  Input,
  LocaleSelect,
  PasswordInput,
  ThemeToggle,
  useI18n,
} from "@authman/shared";
import { adminBootstrapStatus } from "../api/admin";
import { useSession } from "../auth/SessionContext";

export function LoginPage() {
  const { t, tError } = useI18n();
  const { user, login } = useSession();
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [bootstrap, setBootstrap] = useState<"checking" | "needed" | "ok" | "unknown">("checking");

  useEffect(() => {
    let cancelled = false;
    adminBootstrapStatus()
      .then((s) => !cancelled && setBootstrap(s.owner_exists ? "ok" : "needed"))
      .catch(() => !cancelled && setBootstrap("unknown"));
    return () => {
      cancelled = true;
    };
  }, []);

  if (user) return <Navigate to="/" replace />;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(username, password);
      navigate("/", { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? tError(err.code) : t("common.unknown"));
    } finally {
      setSubmitting(false);
    }
  }

  const isBootstrap = bootstrap === "needed";

  return (
    <div className="auth-screen">
      <div className="auth-topbar">
        <div className="brand">
          <BrandMark sub={t("brand.adminSub")} />
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <LocaleSelect />
          <ThemeToggle />
        </div>
      </div>
      <div className="auth-body">
        <div className="auth-card">
          {isBootstrap ? (
            <>
              <div className="auth-head">
                <h1>{t("login.bootstrap.required")}</h1>
                <p>{t("login.bootstrap.body")}</p>
              </div>
              <Alert tone="warning" title={t("login.bootstrap.required")} testId="bootstrap-banner">
                <p>{t("login.bootstrap.body")}</p>
              </Alert>
              <pre className="code-block">$ authman bootstrap owner \
    --username admin</pre>
              <p className="auth-foot-note">{t("login.bootstrap.refresh")}</p>
            </>
          ) : (
            <>
              <div className="auth-head">
                <h1>{t("login.admin.heading")}</h1>
                <p>{t("login.admin.subheading")}</p>
              </div>
              {error ? (
                <Alert tone="danger" testId="login-error">
                  {error}
                </Alert>
              ) : null}
              <form onSubmit={handleSubmit} className="auth-form">
                <Field label={t("common.username")} htmlFor="adm-username">
                  <Input
                    id="adm-username"
                    icon="user"
                    type="text"
                    autoComplete="username"
                    required
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    data-testid="admin-username"
                  />
                </Field>
                <Field label={t("common.password")} htmlFor="adm-pw">
                  <PasswordInput
                    id="adm-pw"
                    icon="lock"
                    autoComplete="current-password"
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    data-testid="admin-password"
                  />
                </Field>
                <Button
                  type="submit"
                  variant="primary"
                  size="lg"
                  block
                  loading={submitting}
                  iconRight={submitting ? undefined : "arrowRight"}
                  data-testid="admin-submit"
                >
                  {submitting ? t("login.admin.submitting") : t("common.signIn")}
                </Button>
              </form>
              <p className="auth-foot-note">
                {t("login.admin.rateLimitNote")}
              </p>
            </>
          )}
        </div>
        <p className="auth-env">
          <span className="env-dot" />
          {(import.meta.env.MODE ?? "dev") === "production" ? "production" : "dev"} ·{" "}
          {(import.meta.env.VITE_AUTHMAN_API_BASE ?? "/api")}
        </p>
      </div>
    </div>
  );
}

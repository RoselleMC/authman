import { useState } from "react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import {
  Alert,
  ApiError,
  Badge,
  Button,
  Field,
  Input,
  PasswordInput,
  Segmented,
  useI18n,
} from "@authman/shared";
import { useSession } from "../auth/SessionContext";
import { useServerContext } from "../server-context/ServerContextProvider";
import { AuthHeader } from "../components/AuthHeader";

type Mode = "offline" | "link";

function ServerContextChip({ server }: { server: { display_name: string; description?: string; primary_color?: string; registration_open: boolean } }) {
  const { t } = useI18n();
  const primary = server.primary_color ?? "var(--color-primary)";
  return (
    <div className="server-ctx-chip" style={{ ["--ctx-color" as string]: primary }} data-testid="server-context-banner">
      <span className="ctx-badge" style={{ background: primary }}>{server.display_name[0]}</span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="ctx-name">{server.display_name}</div>
        {server.description ? <div className="ctx-desc">{server.description}</div> : null}
      </div>
      <Badge tone={server.registration_open ? "success" : "neutral"} dot>
        {server.registration_open ? t("common.open") : t("common.closed")}
      </Badge>
    </div>
  );
}

export function LoginPage() {
  const { t, tError } = useI18n();
  const { me, loginOffline } = useSession();
  const { slug, server } = useServerContext();
  const navigate = useNavigate();
  const [mode, setMode] = useState<Mode>("offline");
  const [user, setUser] = useState("");
  const [pw, setPw] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (me) {
    return <Navigate to={slug ? `/server/${slug}/account` : "/account"} replace />;
  }

  const regClosed = !!server && !server.registration_open;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!user || !pw) {
      setError(t("errors.auth.invalid_credentials"));
      return;
    }
    setSubmitting(true);
    try {
      await loginOffline({ username: user, password: pw, server_slug: slug ?? undefined });
      navigate(slug ? `/server/${slug}/account` : "/account", { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? tError(err.code) : t("common.unknown"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="pauth">
      <AuthHeader sub={server?.display_name} />
      <div className="pauth-body">
        <div className="pauth-card" data-testid="login-card">
          {server ? <ServerContextChip server={server} /> : null}
          <div className="pauth-head">
            <h1>{t("login.player.heading")}</h1>
            <p>{server ? t("login.player.serverDesc").replace("{server}", server.display_name) : t("login.player.centralDesc")}</p>
          </div>

          <div style={{ marginBottom: 18 }}>
            <Segmented
              value={mode}
              onChange={setMode}
              options={[
                { value: "offline", label: t("login.player.mode.offline"), icon: "user" },
                { value: "link", label: t("login.player.mode.link"), icon: "link" },
              ]}
            />
          </div>

          {mode === "offline" ? (
            <form onSubmit={submit} className="pauth-form">
              {error ? (
                <Alert tone="danger" testId="login-error">{error}</Alert>
              ) : null}
              <Field label={t("common.username")} htmlFor="p-user" hint={t("login.player.usernameHint")}>
                <Input
                  id="p-user"
                  icon="user"
                  value={user}
                  onChange={(e) => setUser(e.target.value)}
                  placeholder="e.g. Steve"
                  autoComplete="username"
                  data-testid="login-username"
                />
              </Field>
              <Field label={t("common.password")} htmlFor="p-pw">
                <PasswordInput
                  id="p-pw"
                  icon="lock"
                  value={pw}
                  onChange={(e) => setPw(e.target.value)}
                  autoComplete="current-password"
                  data-testid="login-password"
                />
              </Field>
              <Button
                type="submit"
                variant="primary"
                size="lg"
                block
                loading={submitting}
                data-testid="login-submit"
              >
                {submitting ? t("login.player.submit.loading") : t("common.signIn")}
              </Button>
              <div className="pauth-links">
                {!regClosed ? (
                  <span>
                    {t("login.player.newHere")}{" "}
                    <Link
                      to={slug ? `/server/${slug}/register` : "/register"}
                      className="link-btn"
                      data-testid="register-link"
                    >
                      {t("login.player.createAccount")}
                    </Link>
                  </span>
                ) : (
                  <span className="muted-cell">{t("register.closed")}</span>
                )}
              </div>
            </form>
          ) : (
            <div className="pauth-form">
              <Alert tone="info" title={t("login.player.linkOnly.title")}>
                <p>{t("login.player.linkOnly.body")}</p>
              </Alert>
              <p className="demo-hint">
                {t("login.player.linkOnly.demo")}
              </p>
            </div>
          )}
          {server?.portal_message ? (
            <div data-testid="server-message" style={{ marginTop: 4 }}>
              <Alert tone="info">{server.portal_message}</Alert>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

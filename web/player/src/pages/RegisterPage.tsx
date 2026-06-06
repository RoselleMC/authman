import { useEffect, useMemo, useState } from "react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  ApiError,
  Button,
  Field,
  Icon,
  Input,
  PasswordInput,
  ProtoPreview,
  StrengthMeter,
  useI18n,
  validateOfflineUsername,
} from "@authman/shared";
import { portalCheckName, portalGlobalConfig } from "../api/portal";
import { useSession } from "../auth/SessionContext";
import { useServerContext } from "../server-context/ServerContextProvider";
import { AuthHeader } from "../components/AuthHeader";

type Avail = "empty" | "invalid" | "checking" | "available" | "unavailable";

function AvailIndicator({ state }: { state: Avail }) {
  const { t } = useI18n();
  if (state === "checking") {
    return (
      <span className="avail avail--checking" title={t("register.availability.title")}>
        <span className="spinner" style={{ width: 14, height: 14 }} />
      </span>
    );
  }
  if (state === "available") {
    return (
      <span className="avail avail--ok" data-testid="availability">
        <Icon name="check" size={16} />
      </span>
    );
  }
  if (state === "unavailable") {
    return (
      <span className="avail avail--bad" data-testid="availability">
        <Icon name="close" size={15} />
      </span>
    );
  }
  if (state === "invalid") {
    return (
      <span className="avail avail--bad">
        <Icon name="alert" size={15} />
      </span>
    );
  }
  return null;
}

export function RegisterPage() {
  const { t, tError } = useI18n();
  const { me, register } = useSession();
  const { slug, server } = useServerContext();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [pw, setPw] = useState("");
  const [confirm, setConfirm] = useState("");
  const [avail, setAvail] = useState<Avail>("empty");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const validation = useMemo(() => validateOfflineUsername(name), [name]);
  const pwMismatch = confirm.length > 0 && confirm !== pw;
  const regClosed = !!server && !server.registration_open;

  const globalConfig = useQuery({
    queryKey: ["portal.globalConfig"],
    queryFn: portalGlobalConfig,
    enabled: !slug,
  });
  const isClosed = slug ? regClosed : globalConfig.data?.registration_open === false;

  // Local validation + debounced availability check.
  useEffect(() => {
    if (name.length === 0) {
      setAvail("empty");
      return;
    }
    if (!validation.ok) {
      setAvail("invalid");
      return;
    }
    setAvail("checking");
    const handle = window.setTimeout(async () => {
      try {
        const res = await portalCheckName(name.trim(), slug ?? undefined);
        setAvail(res.available ? "available" : "unavailable");
      } catch {
        setAvail("empty");
      }
    }, 300);
    return () => window.clearTimeout(handle);
  }, [name, validation.ok, slug]);

  if (me) {
    return <Navigate to={slug ? `/server/${slug}/account` : "/account"} replace />;
  }

  const canSubmit =
    avail === "available" && pw.length >= 8 && pw === confirm && !isClosed;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    setError(null);
    setSubmitting(true);
    try {
      await register({ raw_username: name.trim(), password: pw, server_slug: slug ?? undefined });
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
        <div className="pauth-card" data-testid="register-card">
          <Link
            to={slug ? `/server/${slug}/login` : "/login"}
            className="back-link"
            style={{ marginBottom: 12 }}
          >
            <Icon name="arrowLeft" size={15} /> {t("register.backToSignIn")}
          </Link>
          <div className="pauth-head">
            <h1>{t("register.heading")}</h1>
            <p>{t("register.subheading")}</p>
          </div>

          {isClosed ? (
            <Alert tone="warning" title={t("register.closed.title")} testId="register-closed">
              <p>{t("register.closed.body").replace("{server}", server?.display_name ?? t("common.thisPortal"))}</p>
            </Alert>
          ) : null}

          <form
            onSubmit={submit}
            className="pauth-form"
            style={isClosed ? { opacity: 0.55, pointerEvents: "none" } : undefined}
          >
            <Field
              label={t("common.username")}
              error={
                name && !validation.ok
                  ? t(`register.username.${validation.code}`)
                  : undefined
              }
              hint={!name || validation.ok ? t("register.username.hint") : undefined}
            >
              <Input
                affix="#"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Steve"
                autoComplete="off"
                invalid={avail === "invalid" || avail === "unavailable"}
                valid={avail === "available"}
                trail={<AvailIndicator state={avail} />}
                data-testid="register-username"
                maxLength={32}
              />
            </Field>

            <ProtoPreview
              rawName={name && validation.ok ? name : ""}
              label={t("register.protocolPreview")}
              note={t("register.protocolNote")}
            />

            {avail === "unavailable" ? <Alert tone="danger">{t("register.availability.unavailable")}</Alert> : null}

            <Field label={t("common.password")}>
              <PasswordInput
                icon="lock"
                value={pw}
                onChange={(e) => setPw(e.target.value)}
                autoComplete="new-password"
                data-testid="register-password"
              />
              <StrengthMeter password={pw} />
            </Field>
            <Field
              label={t("common.confirmPassword")}
              error={pwMismatch ? t("password.mismatch") : undefined}
            >
              <PasswordInput
                icon="lock"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                autoComplete="new-password"
                invalid={pwMismatch}
                data-testid="register-confirm"
              />
            </Field>

            {error ? <Alert tone="danger" testId="register-error">{error}</Alert> : null}

            <Button
              type="submit"
              variant="primary"
              size="lg"
              block
              loading={submitting}
              disabled={!canSubmit}
              data-testid="register-submit"
            >
              {submitting ? t("register.submit.loading") : t("common.register")}
            </Button>
          </form>
        </div>
      </div>
    </div>
  );
}

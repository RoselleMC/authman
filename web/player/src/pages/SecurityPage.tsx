import { useState } from "react";
import {
  Alert,
  ApiError,
  Button,
  Card,
  Field,
  PContent,
  PContentHead,
  PasswordInput,
  StrengthMeter,
  useI18n,
  useToast,
} from "@authman/shared";
import { portalChangePassword } from "../api/portal";
import { useSession } from "../auth/SessionContext";

export function SecurityPage() {
  const { t, tError } = useI18n();
  const toast = useToast();
  const { me } = useSession();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const match = confirm.length === 0 || next === confirm;

  if (!me) return null;

  if (me.player.kind === "premium") {
    return (
      <PContent>
        <PContentHead title={t("account.security")} desc={t("account.security.premium.managed")} />
        <Card testId="premium-security">
          <Alert tone="info" title={t("account.security.premium.title")}>
            <p>{t("account.security.premium.body")}</p>
          </Alert>
        </Card>
      </PContent>
    );
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!match) {
      setError(t("password.mismatch"));
      return;
    }
    setSubmitting(true);
    try {
      await portalChangePassword({ current_password: current, new_password: next });
      toast.push({ tone: "success", title: t("password.change.success") });
      setCurrent("");
      setNext("");
      setConfirm("");
    } catch (err) {
      setError(err instanceof ApiError ? tError(err.code) : t("common.unknown"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <PContent>
      <PContentHead
        title={t("password.change.heading")}
        desc={t("account.security.password.desc")}
      />
      <Card testId="offline-security">
        <form onSubmit={handleSubmit} className="pauth-form">
          <Field label={t("password.change.current")}>
            <PasswordInput
              icon="lock"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              autoComplete="current-password"
              data-testid="security-current"
              required
            />
          </Field>
          <Field label={t("password.change.new")}>
            <PasswordInput
              icon="lock"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              autoComplete="new-password"
              data-testid="security-new"
              required
            />
            <StrengthMeter password={next} />
          </Field>
          <Field
            label={t("password.change.confirm")}
            error={!match ? t("password.mismatch") : undefined}
          >
            <PasswordInput
              icon="lock"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
              invalid={!match}
              data-testid="security-confirm"
              required
            />
          </Field>
          {error ? <Alert tone="danger" testId="security-error">{error}</Alert> : null}
          <Button type="submit" variant="primary" size="lg" block loading={submitting} data-testid="security-submit">
            {t("password.change.submit")}
          </Button>
        </form>
      </Card>
    </PContent>
  );
}

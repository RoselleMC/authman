import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ApiError,
  Button,
  Card,
  ErrorState,
  Field,
  Icon,
  Input,
  PageHeader,
  PageShell,
  useI18n,
  useToast,
} from "@authman/shared";
import { fetchPortalSettings, updatePortalSettings, type PortalSettings } from "../api/admin";

const EMPTY: PortalSettings = {
  default_target_server: "",
  holding_server: "",
  requested_host: "",
  source_id: "",
  transfer_cookie_key: "authman:transfer_grant",
  dialog_enabled: true,
  dialog_fallback_chat_enabled: true,
};

export function PortalSettingsPage({ embedded = false }: { embedded?: boolean } = {}) {
  const { t, tError } = useI18n();
  const toast = useToast();
  const qc = useQueryClient();
  const [form, setForm] = useState<PortalSettings>(EMPTY);

  const q = useQuery({ queryKey: ["admin.portal-settings"], queryFn: fetchPortalSettings });

  useEffect(() => {
    if (q.data) {
      setForm({ ...EMPTY, ...q.data, transfer_cookie_key: q.data.transfer_cookie_key || EMPTY.transfer_cookie_key });
    }
  }, [q.data]);

  const save = useMutation({
    mutationFn: updatePortalSettings,
    onSuccess: (next) => {
      setForm({ ...EMPTY, ...next, transfer_cookie_key: next.transfer_cookie_key || EMPTY.transfer_cookie_key });
      toast.push({ tone: "success", title: t("admin.portal.saved.toast") });
      void qc.invalidateQueries({ queryKey: ["admin.portal-settings"] });
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  function patch<K extends keyof PortalSettings>(key: K, value: PortalSettings[K]) {
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  const saveButton = (
    <Button
      variant="primary"
      icon="save"
      loading={save.isPending}
      onClick={() => save.mutate(form)}
      data-testid="portal-settings-save"
    >
      {t("common.save")}
    </Button>
  );

  const content = (
    <>
      {embedded ? (
        <div className="section-toolbar">
          <span />
          {saveButton}
        </div>
      ) : (
        <PageHeader
          title={t("admin.portal.heading")}
          desc={t("admin.portal.desc")}
          action={saveButton}
        />
      )}
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <div className="settings-grid">
        <Card title={t("admin.portal.card.routing")}>
          <div className="settings-form-grid">
            <Field label={t("admin.portal.field.defaultTarget")} hint={t("admin.portal.field.defaultTarget.hint")}>
              <Input
                value={form.default_target_server}
                onChange={(e) => patch("default_target_server", e.target.value)}
                placeholder="default"
                mono
                data-testid="portal-default-target"
              />
            </Field>
            <Field label={t("admin.portal.field.holding")} hint={t("admin.portal.field.holding.hint")}>
              <Input
                value={form.holding_server}
                onChange={(e) => patch("holding_server", e.target.value)}
                placeholder="lobby"
                mono
                data-testid="portal-holding-server"
              />
            </Field>
            <Field label={t("admin.portal.field.requestedHost")} hint={t("admin.portal.field.requestedHost.hint")}>
              <Input
                value={form.requested_host}
                onChange={(e) => patch("requested_host", e.target.value)}
                placeholder="play.example.com"
                mono
                data-testid="portal-requested-host"
              />
            </Field>
            <Field label={t("admin.portal.field.source")} hint={t("admin.portal.field.source.hint")}>
              <Input
                value={form.source_id}
                onChange={(e) => patch("source_id", e.target.value)}
                placeholder="portal-global"
                mono
                data-testid="portal-source-id"
              />
            </Field>
          </div>
        </Card>

        <Card title={t("admin.portal.card.auth")}>
          <div className="settings-form-grid settings-form-grid--single">
            <Field label={t("admin.portal.field.cookie")} hint={t("admin.portal.field.cookie.hint")}>
              <Input
                value={form.transfer_cookie_key}
                onChange={(e) => patch("transfer_cookie_key", e.target.value)}
                placeholder="authman:transfer_grant"
                mono
                data-testid="portal-cookie-key"
              />
            </Field>
            <label className="toggle-row">
              <input
                type="checkbox"
                checked={form.dialog_enabled}
                onChange={(e) => patch("dialog_enabled", e.target.checked)}
                data-testid="portal-dialog-enabled"
              />
              <span>
                <strong>{t("admin.portal.field.dialog")}</strong>
                <small>{t("admin.portal.field.dialog.hint")}</small>
              </span>
            </label>
            <label className="toggle-row">
              <input
                type="checkbox"
                checked={form.dialog_fallback_chat_enabled}
                onChange={(e) => patch("dialog_fallback_chat_enabled", e.target.checked)}
                data-testid="portal-dialog-fallback"
              />
              <span>
                <strong>{t("admin.portal.field.fallback")}</strong>
                <small>{t("admin.portal.field.fallback.hint")}</small>
              </span>
            </label>
          </div>
          <p className="card-foot-note" style={{ marginTop: 16 }}>
            <Icon name="info" size={13} /> {t("admin.portal.footnote")}
          </p>
        </Card>
      </div>
    </>
  );

  return embedded ? content : <PageShell>{content}</PageShell>;
}

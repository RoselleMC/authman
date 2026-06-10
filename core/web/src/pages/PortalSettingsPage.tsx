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
  Select,
  useI18n,
  useToast,
} from "@authman/shared";
import { fetchPortalSettings, updatePortalSettings, type PortalSettings } from "../api/admin";

const EMPTY: PortalSettings = {
  transfer_cookie_key: "authman:transfer_grant",
  dialog_enabled: true,
  dialog_fallback_chat_enabled: true,
  fallback_server_id: "",
  available_servers: [],
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
        null
      ) : (
        <PageHeader
          title={t("admin.portal.heading")}
          desc={t("admin.portal.desc")}
        />
      )}
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <div className="settings-grid">
        <Card title={t("admin.portal.card.auth")} actions={saveButton}>
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
            <Field label={t("admin.portal.field.fallbackServer")} hint={t("admin.portal.field.fallbackServer.hint")}>
              <Select
                value={form.fallback_server_id || "disconnect"}
                onChange={(value) => patch("fallback_server_id", value === "disconnect" ? "" : value)}
                options={[
                  { value: "disconnect", label: t("admin.portal.fallback.disconnect") },
                  ...(form.available_servers ?? []).map((server) => ({
                    value: server.id,
                    label: server.display_name || server.slug || server.id,
                  })),
                ]}
                testId="portal-fallback-server"
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

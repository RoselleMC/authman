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
  fallback_server_id: "",
  max_profiles_per_passport: 3,
  auto_auth_ttl_seconds: 12 * 60 * 60,
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
            <Field label={t("admin.portal.field.maxProfiles")} hint={t("admin.portal.field.maxProfiles.hint")}>
              <input
                className="input"
                type="number"
                min={1}
                max={16}
                value={form.max_profiles_per_passport ?? 3}
                onChange={(e) => patch("max_profiles_per_passport", Math.max(1, Math.min(16, Number(e.target.value) || 3)))}
                data-testid="portal-max-profiles"
              />
            </Field>
            <Field label={t("admin.portal.field.autoAuthTTL")} hint={t("admin.portal.field.autoAuthTTL.hint")}>
              <input
                className="input"
                type="number"
                min={0}
                max={168}
                value={Math.round((form.auto_auth_ttl_seconds ?? EMPTY.auto_auth_ttl_seconds ?? 0) / 3600)}
                onChange={(e) => patch("auto_auth_ttl_seconds", Math.max(0, Math.min(168, Number(e.target.value) || 0)) * 3600)}
                data-testid="portal-auto-auth-ttl"
              />
            </Field>
            <div className="settings-note" data-testid="portal-single-profile-auto">
              <Icon name="info" size={14} />
              <span>
                <strong>{t("admin.portal.field.singleProfileAuto")}</strong>
                <small>{t("admin.portal.field.singleProfileAuto.hint")}</small>
              </span>
            </div>
            <div className="settings-note" data-testid="portal-protocol-requirement">
              <Icon name="info" size={14} />
              <span>{t("admin.portal.protocolRequirement")}</span>
            </div>
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

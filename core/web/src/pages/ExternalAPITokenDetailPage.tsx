import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import {
  ApiError,
  BackLink,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  ConfigGrid,
  ConfigRow,
  DetailActions,
  DetailAside,
  DetailBody,
  DetailGrid,
  DetailSummary,
  ErrorState,
  PageShell,
  formatAbsTime,
  useBackTarget,
  useI18n,
  useToast,
} from "@authman/shared";
import { deleteExternalAPITokenRecord, fetchExternalAPIToken, revokeExternalAPIToken, updateExternalAPIToken, type ExternalAPIToken } from "../api/admin";
import { useSession } from "../auth/SessionContext";

function ExternalTokenStatusBadge({ status }: { status: ExternalAPIToken["status"] }) {
  const { t } = useI18n();
  return (
    <Badge tone={status === "active" ? "success" : status === "disabled" ? "warning" : "neutral"} dot>
      {t(`admin.settings.externalApi.status.${status}`)}
    </Badge>
  );
}

function formatMaybeTime(value: string | null | undefined) {
  return value ? formatAbsTime(value) : "—";
}

export function ExternalAPITokenDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const backTarget = useBackTarget("/settings/external-api");
  const { t, tError } = useI18n();
  const toast = useToast();
  const qc = useQueryClient();
  const { hasPermission } = useSession();
  const canWrite = hasPermission("external_api.write");
  const canDelete = hasPermission("external_api.delete");
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const q = useQuery({
    queryKey: ["admin.externalToken", id],
    queryFn: () => fetchExternalAPIToken(id),
    enabled: !!id,
  });
  const token = q.data;

  const updateMut = useMutation({
    mutationFn: (status: ExternalAPIToken["status"]) => updateExternalAPIToken(id, { status }),
    onSuccess: (next) => {
      toast.push({ tone: "success", title: t("admin.settings.externalApi.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.externalToken", next.id] });
      void qc.invalidateQueries({ queryKey: ["admin.externalTokens"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const revokeMut = useMutation({
    mutationFn: () => revokeExternalAPIToken(id),
    onSuccess: (next) => {
      toast.push({ tone: "success", title: t("admin.settings.externalApi.revoked") });
      setRevokeOpen(false);
      void qc.invalidateQueries({ queryKey: ["admin.externalToken", next.id] });
      void qc.invalidateQueries({ queryKey: ["admin.externalTokens"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const deleteMut = useMutation({
    mutationFn: () => deleteExternalAPITokenRecord(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.settings.externalApi.deleted") });
      setDeleteOpen(false);
      void qc.invalidateQueries({ queryKey: ["admin.externalTokens"] });
      navigate("/settings/external-api");
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  if (q.error) {
    return (
      <PageShell testId="external-api-token-detail-page">
        <BackLink onClick={() => navigate(backTarget)}>{t("admin.settings.externalApi")}</BackLink>
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      </PageShell>
    );
  }
  if (!token) {
    return (
      <PageShell testId="external-api-token-detail-page">
        <BackLink onClick={() => navigate(backTarget)}>{t("admin.settings.externalApi")}</BackLink>
        <Card title={t("common.loading")}><span /></Card>
      </PageShell>
    );
  }

  const nextStatus: ExternalAPIToken["status"] = token.status === "active" ? "disabled" : "active";
  const canToggle = canWrite && token.status !== "revoked";
  const canRevoke = canWrite && token.status !== "revoked";
  const canHardDelete = canDelete && token.status === "revoked";

  return (
    <PageShell testId="external-api-token-detail-page">
      <div className="detail-toolbar">
        <BackLink onClick={() => navigate(backTarget)}>{t("admin.settings.externalApi")}</BackLink>
      </div>
      <DetailGrid>
        <DetailAside>
          <DetailSummary
            title={token.name}
            icon="key"
            titleMeta={<ExternalTokenStatusBadge status={token.status} />}
            meta={<span className="muted-cell">{t("admin.settings.externalApi.detail.meta")}</span>}
          >
            <div className="id-uuid">
              <span className="id-uuid-label">{t("admin.settings.externalApi.col.name")}</span>
              <strong className="mono">{token.token_fingerprint}</strong>
            </div>
          </DetailSummary>
          <DetailActions title={t("common.actions")}>
            <Button
              variant={nextStatus === "active" ? "primary" : "secondary"}
              icon={nextStatus === "active" ? "check" : "close"}
              block
              disabled={!canToggle}
              loading={updateMut.isPending}
              onClick={() => updateMut.mutate(nextStatus)}
            >
              {nextStatus === "active" ? t("common.enable") : t("common.disable")}
            </Button>
            {token.status === "revoked" ? (
              <Button
                variant="danger"
                icon="trash"
                block
                disabled={!canHardDelete}
                loading={deleteMut.isPending}
                onClick={() => setDeleteOpen(true)}
              >
                {t("common.delete")}
              </Button>
            ) : (
              <Button
                variant="danger-soft"
                icon="close"
                block
                disabled={!canRevoke}
                onClick={() => setRevokeOpen(true)}
              >
                {t("common.revoke")}
              </Button>
            )}
          </DetailActions>
        </DetailAside>
        <DetailBody>
          <Card title={t("admin.settings.externalApi.detail.overview")}>
            <ConfigGrid>
              <ConfigRow k={t("admin.settings.externalApi.col.status")} v={<ExternalTokenStatusBadge status={token.status} />} />
              <ConfigRow k={t("admin.settings.externalApi.col.calls")} v={token.call_count.toLocaleString()} />
              <ConfigRow k={t("admin.settings.externalApi.col.lastUsed")} v={formatMaybeTime(token.last_used_at)} />
              <ConfigRow k={t("admin.settings.externalApi.col.lastIP")} v={token.last_used_ip || "—"} />
              <ConfigRow k={t("admin.settings.externalApi.col.lastPath")} v={<span className="mono-inline">{token.last_used_path || "—"}</span>} />
              <ConfigRow k={t("admin.settings.externalApi.col.created")} v={formatMaybeTime(token.created_at)} />
              <ConfigRow k={t("admin.settings.externalApi.detail.updated")} v={formatMaybeTime(token.updated_at)} />
              <ConfigRow k={t("admin.settings.externalApi.detail.createdBy")} v={<span className="mono-inline">{token.created_by || "—"}</span>} />
            </ConfigGrid>
          </Card>
          <Card title={t("admin.settings.externalApi.detail.access")}>
            <ConfigGrid>
              <ConfigRow k={t("admin.settings.externalApi.detail.coreScope")} v={t("admin.settings.externalApi.detail.coreScope.value")} />
              <ConfigRow k={t("admin.settings.externalApi.detail.externalScope")} v={t("admin.settings.externalApi.detail.externalScope.value")} />
            </ConfigGrid>
          </Card>
        </DetailBody>
      </DetailGrid>
      <ConfirmDialog
        open={revokeOpen}
        destructive
        title={t("common.revoke")}
        body={t("admin.settings.externalApi.revoke.desc").replace("{name}", token.name)}
        confirmLabel={t("common.revoke")}
        loading={revokeMut.isPending}
        onCancel={() => setRevokeOpen(false)}
        onConfirm={() => revokeMut.mutate()}
      />
      <ConfirmDialog
        open={deleteOpen}
        destructive
        title={t("common.delete")}
        body={t("admin.settings.externalApi.delete.desc").replace("{name}", token.name)}
        confirmLabel={t("common.delete")}
        loading={deleteMut.isPending}
        onCancel={() => setDeleteOpen(false)}
        onConfirm={() => deleteMut.mutate()}
      />
    </PageShell>
  );
}

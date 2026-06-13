import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  BackLink,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Copyable,
  DetailActions,
  DetailAside,
  DetailBody,
  DetailGrid,
  DetailIdentifier,
  DetailSummary,
  Field,
  Input,
  PageShell,
  Select,
  Tabs,
  formatRelativeTime,
  useI18n,
  useBackTarget,
  useToast,
} from "@authman/shared";
import { deleteLimboBlueprint, fetchLimboBlueprint, updateLimboBlueprint, type LimboBlueprintConfig } from "../api/admin";
import { LimboBlueprintPreview, type SpawnPoint } from "../components/LimboBlueprintPreview";

type Tab = "overview" | "preview";
const BLUEPRINTS_BASE_PATH = "/login-portals/blueprints";

function spawnOf(config: LimboBlueprintConfig | undefined): SpawnPoint {
  const spawn = config?.spawn;
  return {
    x: Number(spawn?.x ?? 0),
    y: Number(spawn?.y ?? 65),
    z: Number(spawn?.z ?? 0),
    yaw: Number(spawn?.yaw ?? 0),
    pitch: Number(spawn?.pitch ?? 0),
  };
}

export function LimboBlueprintDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t } = useI18n();
  const navigate = useNavigate();
  const backTarget = useBackTarget(BLUEPRINTS_BASE_PATH);
  const toast = useToast();
  const qc = useQueryClient();
  const [tab, setTab] = useState<Tab>("overview");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const q = useQuery({ queryKey: ["admin.limboBlueprint", id], queryFn: () => fetchLimboBlueprint(id), enabled: !!id });
  const blueprint = q.data;
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [dimension, setDimension] = useState<"overworld" | "nether" | "end">("overworld");
  const [spawn, setSpawn] = useState<SpawnPoint>(spawnOf(undefined));

  useEffect(() => {
    if (!blueprint) return;
    setName(blueprint.name);
    setDescription(blueprint.description ?? "");
    setDimension((blueprint.config?.dimension as "overworld" | "nether" | "end") ?? "overworld");
    setSpawn(spawnOf(blueprint.config));
  }, [blueprint]);

  const nextConfig = useMemo<LimboBlueprintConfig>(() => ({
    ...(blueprint?.config ?? {}),
    dimension,
    spawn,
  }), [blueprint?.config, dimension, spawn]);

  const updateMut = useMutation({
    mutationFn: () => updateLimboBlueprint(id, { name, description, config: nextConfig }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.limboBlueprint", id] });
      void qc.invalidateQueries({ queryKey: ["admin.limboBlueprints"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const deleteMut = useMutation({
    mutationFn: () => deleteLimboBlueprint(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.deleted") });
      void qc.invalidateQueries({ queryKey: ["admin.limboBlueprints"] });
      navigate(BLUEPRINTS_BASE_PATH);
    },
    onError: () => toast.danger(t("common.unknown")),
  });

  if (!blueprint && q.isLoading) return <PageShell><BackLink onClick={() => navigate(backTarget)}>{t("admin.limboBlueprints.heading")}</BackLink><Card title={t("common.loading")}><span /></Card></PageShell>;
  if (!blueprint) return <PageShell><BackLink onClick={() => navigate(backTarget)}>{t("admin.limboBlueprints.heading")}</BackLink><Card title={t("common.unknown")}><span /></Card></PageShell>;

  return (
    <PageShell testId="limbo-blueprint-detail-page">
      <div className="detail-toolbar">
        <BackLink onClick={() => navigate(backTarget)}>{t("admin.limboBlueprints.heading")}</BackLink>
        <Tabs<Tab> value={tab} onChange={setTab} tabs={[{ value: "overview", label: t("common.overview"), icon: "info" }, { value: "preview", label: t("admin.limboBlueprints.preview"), icon: "box" }]} />
      </div>
      <DetailGrid>
        <DetailAside>
          <DetailSummary
            title={blueprint.name}
            icon="box"
            titleMeta={<Badge tone="info">{blueprint.preview?.block_count ?? 0}</Badge>}
            meta={<><span className="muted-cell">{t("admin.limboBlueprints.col.dimension")}</span><strong>{dimension}</strong></>}
          >
            <DetailIdentifier label={t("admin.limboBlueprints.detail.blueprintId")} value={blueprint.id} />
            <DetailIdentifier label="SHA-256" value={blueprint.sha256} />
            <DetailIdentifier label={t("common.updated")} value={formatRelativeTime(blueprint.updated_at)} copy={false} mono={false} />
          </DetailSummary>
          <DetailActions title={t("common.actions")}>
            <Button variant="primary" icon="check" block loading={updateMut.isPending} disabled={!name.trim()} onClick={() => updateMut.mutate()}>{t("common.save")}</Button>
            <Button variant="danger-soft" icon="trash" block onClick={() => setDeleteOpen(true)}>{t("common.delete")}</Button>
          </DetailActions>
        </DetailAside>
        <DetailBody>
          {tab === "overview" ? (
            <>
              <Card title={t("admin.limboBlueprints.settings")}>
                <div className="form-grid two">
                  <Field label={t("admin.limboBlueprints.field.name")}><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
                  <Field label={t("admin.limboBlueprints.col.dimension")}>
                    <Select value={dimension} onChange={setDimension} options={[{ value: "overworld", label: "Overworld" }, { value: "nether", label: "Nether" }, { value: "end", label: "End" }]} />
                  </Field>
                  <Field label={t("admin.limboBlueprints.field.description")} style={{ gridColumn: "1 / -1" }}><Input value={description} onChange={(e) => setDescription(e.target.value)} /></Field>
                </div>
              </Card>
              <Card title={t("admin.limboBlueprints.spawn")}>
                <div className="form-grid two">
                  <Field label="X"><Input type="number" value={spawn.x} onChange={(e) => setSpawn({ ...spawn, x: Number(e.target.value) })} /></Field>
                  <Field label="Y"><Input type="number" value={spawn.y} onChange={(e) => setSpawn({ ...spawn, y: Number(e.target.value) })} /></Field>
                  <Field label="Z"><Input type="number" value={spawn.z} onChange={(e) => setSpawn({ ...spawn, z: Number(e.target.value) })} /></Field>
                  <Field label="Yaw"><Input type="number" value={spawn.yaw} onChange={(e) => setSpawn({ ...spawn, yaw: Number(e.target.value) })} /></Field>
                  <Field label="Pitch"><Input type="number" value={spawn.pitch} onChange={(e) => setSpawn({ ...spawn, pitch: Number(e.target.value) })} /></Field>
                </div>
              </Card>
              <Card title={t("admin.limboBlueprints.file")}>
                <div className="def-list">
                  <div><span>{t("admin.limboBlueprints.field.file")}</span><strong>{blueprint.filename || "—"}</strong></div>
                  <div><span>SHA-256</span><Copyable value={blueprint.sha256} /></div>
                  <div><span>{t("admin.limboBlueprints.field.worldId")}</span><Copyable value={String(blueprint.config?.world_id ?? "")} /></div>
                </div>
              </Card>
            </>
          ) : (
            <Card title={t("admin.limboBlueprints.preview")}>
              <p className="card-foot-note" style={{ margin: "-6px 0 12px", padding: 0, borderTop: 0 }}>{t("admin.limboBlueprints.previewDesc")}</p>
              <LimboBlueprintPreview preview={blueprint.preview} spawn={spawn} onSpawnChange={setSpawn} />
            </Card>
          )}
        </DetailBody>
      </DetailGrid>
      <ConfirmDialog
        open={deleteOpen}
        onCancel={() => setDeleteOpen(false)}
        onConfirm={() => deleteMut.mutate()}
        title={t("admin.limboBlueprints.delete")}
        body={t("admin.limboBlueprints.deleteDesc")}
        confirmLabel={t("common.delete")}
        destructive
        loading={deleteMut.isPending}
      />
    </PageShell>
  );
}

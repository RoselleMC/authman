import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AdvancedList,
  Button,
  Card,
  Dialog,
  EmptyState,
  Field,
  Icon,
  Input,
  PageHeader,
  PageShell,
  formatRelativeTime,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
} from "@authman/shared";
import { fetchLimboBlueprints, uploadLimboBlueprint, type LimboBlueprint, type ListFilters } from "../api/admin";
import { useSession } from "../auth/SessionContext";

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit++;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function LimboBlueprintsPage({ embedded = false, basePath = "/limbo-blueprints" }: { embedded?: boolean; basePath?: string } = {}) {
  const { t } = useI18n();
  const { user } = useSession();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const list = useListState({ urlPrefix: "lb", defaults: { pageSize: 25, hidden: ["sha256"] }, storageScope: user?.id });
  const [uploadOpen, setUploadOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const filters = useMemo<ListFilters>(() => {
    const next: ListFilters = { page: list.state.page, page_size: list.state.pageSize };
    const q = (list.state.filters.name ?? "").trim();
    if (q) next.q = q;
    if (list.state.sortKey) {
      next.sort = list.state.sortKey;
      next.dir = list.state.sortDir;
    }
    return next;
  }, [list.state]);
  const q = useQuery({ queryKey: ["admin.limboBlueprints", filters], queryFn: () => fetchLimboBlueprints(filters) });
  const uploadMut = useMutation({
    mutationFn: () => {
      if (!file) throw new Error("file required");
      return uploadLimboBlueprint({ file, name, description, config: { dimension: "overworld" } });
    },
    onSuccess: (bp) => {
      toast.push({ tone: "success", title: t("common.saved") });
      setUploadOpen(false);
      setFile(null);
      setName("");
      setDescription("");
      void qc.invalidateQueries({ queryKey: ["admin.limboBlueprints"] });
      navigate(`${basePath}/${encodeURIComponent(bp.id)}`);
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const columns: ListColumn<LimboBlueprint>[] = [
    { key: "name", header: t("admin.limboBlueprints.col.name"), mandatory: true, sortable: true, sortValue: (r) => r.name, filter: { type: "text" }, render: (r) => <strong>{r.name}</strong> },
    { key: "blocks", header: t("admin.limboBlueprints.col.blocks"), sortable: true, sortValue: (r) => r.preview?.block_count ?? 0, render: (r) => <span>{r.preview?.block_count ?? 0}</span> },
    { key: "size", header: t("admin.limboBlueprints.col.size"), sortable: true, sortValue: (r) => r.size_bytes, render: (r) => <span>{formatBytes(r.size_bytes)}</span> },
    { key: "dimension", header: t("admin.limboBlueprints.col.dimension"), sortable: true, sortValue: (r) => String(r.config?.dimension ?? ""), render: (r) => <span>{r.config?.dimension ?? "overworld"}</span> },
    { key: "updated", header: t("common.updated"), sortable: true, sortValue: (r) => r.updated_at, render: (r) => <span className="muted-cell">{formatRelativeTime(r.updated_at)}</span> },
    { key: "sha256", header: "SHA-256", minWidth: "260px", defaultVisible: false, render: (r) => <span className="mono">{r.sha256.slice(0, 24)}...</span> },
    { key: "open", header: "", mandatory: true, width: "44px", minWidth: "44px", align: "right", sticky: "right", render: () => <Icon name="chevronRight" size={16} /> },
  ];
  const uploadButton = <Button variant="primary" icon="plus" onClick={() => setUploadOpen(true)}>{t("admin.limboBlueprints.upload")}</Button>;
  const content = (
    <div data-testid="limbo-blueprints-page">
      {embedded ? null : (
        <PageHeader
          title={t("admin.limboBlueprints.heading")}
          desc={t("admin.limboBlueprints.desc")}
          action={uploadButton}
        />
      )}
      <Card noBody className="table-card">
        <AdvancedList
          title={embedded ? t("admin.limboBlueprints.heading") : undefined}
          columns={columns}
          rowKey={(r) => r.id}
          mode="server"
          rows={q.data?.rows ?? []}
          total={q.data?.meta.total ?? 0}
          state={list.state}
          onStateChange={list.setState}
          loading={q.isLoading}
          primaryActions={embedded ? uploadButton : undefined}
          onRowClick={(r) => navigate(`${basePath}/${encodeURIComponent(r.id)}`)}
          empty={<EmptyState icon="box" title={t("admin.limboBlueprints.empty")} />}
          testId="limbo-blueprints"
        />
      </Card>
      <Dialog
        open={uploadOpen}
        onClose={() => !uploadMut.isPending && setUploadOpen(false)}
        icon="box"
        iconTone="primary"
        title={t("admin.limboBlueprints.upload")}
        footer={<><Button variant="ghost" onClick={() => setUploadOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" loading={uploadMut.isPending} disabled={!file} onClick={() => uploadMut.mutate()}>{t("common.save")}</Button></>}
      >
        <div className="form-grid two">
          <Field label={t("admin.limboBlueprints.field.file")}>
            <label className="file-drop">
              <input type="file" accept=".schem,.schematic,application/octet-stream" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
              <span>{file ? file.name : t("admin.limboBlueprints.fileHint")}</span>
            </label>
          </Field>
          <Field label={t("admin.limboBlueprints.field.name")}>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={file?.name.replace(/\.(schem|schematic)$/i, "") ?? ""} />
          </Field>
          <Field label={t("admin.limboBlueprints.field.description")} style={{ gridColumn: "1 / -1" }}>
            <Input value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
        </div>
      </Dialog>
    </div>
  );

  return embedded ? content : <PageShell>{content}</PageShell>;
}

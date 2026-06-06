import { useQuery } from "@tanstack/react-query";
import {
  Alert,
  Badge,
  Card,
  EmptyState,
  Icon,
  SchemaRenderer,
  formatRelativeTime,
  useI18n,
} from "@authman/shared";
import { fetchExtensions, type ExtensionRegistryEntry } from "../api/admin";
import { PageHeader } from "../layout/AdminShell";
import { ErrorBlock } from "../components/ErrorBlock";

function VisBadge({ vis }: { vis: string }) {
  const { t } = useI18n();
  if (vis === "admin_only") return <Badge tone="warning"><Icon name="lock" size={11} /> {t("admin.extensions.visibility.adminOnly")}</Badge>;
  if (vis === "public") return <Badge tone="info">{t("admin.extensions.visibility.public")}</Badge>;
  return <Badge tone="success" dot>{t("admin.extensions.visibility.playerVisible")}</Badge>;
}

function Block({ entry }: { entry: ExtensionRegistryEntry }) {
  const { t } = useI18n();
  return (
    <Card
      title={entry.schema.title}
      actions={
        <span style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <span className="ext-provider mono">{entry.provider}</span>
          <span className="mono" style={{ fontSize: 11, color: "var(--color-text-subtle)" }}>
            {t("admin.extensions.schemaVersion").replace("{version}", String(entry.schema.version))}
          </span>
        </span>
      }
      noBody
      testId={`extension-${entry.provider}`}
    >
      <div className="inspector-grid">
        <div className="inspector-schema">
          <div className="inspector-head">
            <Icon name="list" size={14} />
            {t("admin.extensions.schema")}
            <span className="schema-count">{t("admin.extensions.fieldsCount").replace("{count}", String(entry.schema.fields.length))}</span>
          </div>
          <div className="schema-rows">
            {entry.schema.fields.map((f) => (
              <div className="schema-row" key={f.key}>
                <div className="schema-key">
                  <code className="mono">{f.key}</code>
                  <span className="schema-label">{t(`extensions.field.${f.key}`, f.label)}</span>
                </div>
                <div className="schema-meta">
                  <span className="type-pill">{f.type}</span>
                  {f.format ? <span className="schema-attr mono">{f.format}</span> : null}
                  {f.safe ? <span className="schema-attr mono">{t("admin.extensions.safe")}</span> : null}
                  <VisBadge vis={f.visibility ?? "player_visible"} />
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="inspector-preview">
          <div className="inspector-head">
            <Icon name="eye" size={14} />
            {t("admin.extensions.preview.heading")}
          </div>
          <div className="preview-fields">
            <SchemaRenderer
              data={{
                provider: entry.provider,
                schema: entry.schema,
                values: entry.preview_values,
              }}
              testId={`preview-${entry.provider}`}
            />
          </div>
          <div className="ext-foot">
            <Icon name="clock" size={12} />
            {t("admin.extensions.lastUpdated")} · {formatRelativeTime(entry.last_update)}
          </div>
        </div>
      </div>
    </Card>
  );
}

export function ExtensionsPage() {
  const { t } = useI18n();
  const q = useQuery({ queryKey: ["admin.extensions"], queryFn: fetchExtensions });

  return (
    <div className="page">
      <PageHeader
        title={t("admin.extensions.heading")}
        desc={t("admin.extensions.desc")}
      />
      {q.error ? <ErrorBlock error={q.error} onRetry={() => q.refetch()} /> : null}
      <div style={{ marginBottom: 16 }}>
        <Alert tone="neutral">
          <p>{t("admin.extensions.notice")}</p>
        </Alert>
      </div>

      <div data-testid="extensions-table">
        {q.isLoading ? (
          <Card>{t("common.loading")}</Card>
        ) : (q.data ?? []).length === 0 ? (
          <Card>
            <EmptyState icon="box" title={t("common.empty")} />
          </Card>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
            {(q.data ?? []).map((entry) => (
              <Block key={entry.provider} entry={entry} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
